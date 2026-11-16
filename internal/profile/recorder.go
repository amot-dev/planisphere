package profile

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

type buildingNode struct {
	span                *SpanNode
	directTotalDuration time.Duration
	directSQLDuration   time.Duration
	directSQLQueries    int
	distinctQueries     []string
	seenQueryMap        map[string]bool
	childrenByKey       map[string]*buildingNode
	orderedChildren     []*buildingNode
}

func newBuildingNode(name, location, category string) *buildingNode {
	return &buildingNode{
		span: &SpanNode{
			Name:     name,
			Location: location,
			Category: category,
		},
		seenQueryMap:  make(map[string]bool),
		childrenByKey: make(map[string]*buildingNode),
	}
}

func (node *buildingNode) getOrCreateChild(name, location, category string) *buildingNode {
	if existing, ok := node.childrenByKey[name]; ok {
		if existing.span.Location == "" && location != "" {
			existing.span.Location = location
		}
		return existing
	}
	created := newBuildingNode(name, location, category)
	node.childrenByKey[name] = created
	node.orderedChildren = append(node.orderedChildren, created)
	return created
}

type recordedSubSpan struct {
	label    string
	location string
	items    int
	unit     string
	duration time.Duration
}

type treeRecorder struct {
	mu             sync.Mutex
	recording      atomic.Bool
	activeSubSpan  string
	sqlEvents      []rawSQLEvent
	subSpans       []recordedSubSpan
}

func newTreeRecorder() *treeRecorder {
	return &treeRecorder{
		sqlEvents: make([]rawSQLEvent, 0, 256),
		subSpans:  make([]recordedSubSpan, 0, 32),
	}
}

func (recorder *treeRecorder) isRecording() bool {
	return recorder.recording.Load()
}

func (recorder *treeRecorder) start() {
	recorder.mu.Lock()
	recorder.sqlEvents = recorder.sqlEvents[:0]
	recorder.subSpans = recorder.subSpans[:0]
	recorder.activeSubSpan = ""
	recorder.mu.Unlock()
	recorder.recording.Store(true)
}

func (recorder *treeRecorder) stop() {
	recorder.recording.Store(false)
}

func (recorder *treeRecorder) currentSubSpan() string {
	recorder.mu.Lock()
	label := recorder.activeSubSpan
	recorder.mu.Unlock()
	return label
}

func (recorder *treeRecorder) withSubSpan(label, location string, items int, unit string, operation func() error) error {
	recorder.mu.Lock()
	previousLabel := recorder.activeSubSpan
	recorder.activeSubSpan = label
	recorder.mu.Unlock()

	startTime := time.Now()
	opError := operation()
	elapsed := time.Since(startTime)

	recorder.mu.Lock()
	recorder.activeSubSpan = previousLabel
	recorder.subSpans = append(recorder.subSpans, recordedSubSpan{
		label:    label,
		location: location,
		items:    items,
		unit:     unit,
		duration: elapsed,
	})
	recorder.mu.Unlock()
	return opError
}

func (recorder *treeRecorder) recordSQLEvent(event rawSQLEvent) {
	if !recorder.recording.Load() {
		return
	}
	recorder.mu.Lock()
	recorder.sqlEvents = append(recorder.sqlEvents, event)
	recorder.mu.Unlock()
}

func (recorder *treeRecorder) buildTree(
	database *sql.DB,
	rootName string,
	itemsProcessed int,
	itemUnit string,
	elapsed time.Duration,
	allocBytes uint64,
	mallocs uint64,
) *SpanNode {
	rootBuilder := newBuildingNode(rootName, "", "root")
	rootBuilder.span.Calls = 1
	rootBuilder.span.ItemsProcessed = itemsProcessed
	rootBuilder.span.ItemUnit = itemUnit

	for _, sub := range recorder.subSpans {
		child := rootBuilder.getOrCreateChild(sub.label, sub.location, "subspan")
		child.span.Calls++
		child.span.ItemsProcessed += sub.items
		child.span.ItemUnit = sub.unit
		child.directTotalDuration += sub.duration
	}

	for _, event := range recorder.sqlEvents {
		frames := resolveInternalStackFrames(event.programCounters[:event.counterCount])
		current := rootBuilder
		if event.subSpanLabel != "" {
			current = rootBuilder.getOrCreateChild(event.subSpanLabel, "", "subspan")
		}
		startIndex := 0
		if len(frames) > 0 {
			if rootBuilder.span.Location == "" {
				rootBuilder.span.Location = frames[0].location
			}
			if strings.HasSuffix(frames[0].functionName, rootName) || frames[0].functionName == rootName {
				startIndex = 1
			}
		}
		for frameIndex := startIndex; frameIndex < len(frames); frameIndex++ {
			frame := frames[frameIndex]
			current = current.getOrCreateChild(frame.functionName, frame.location, "sql")
			if frameIndex < len(frames)-1 {
				current.span.Calls = max(current.span.Calls, 1)
			}
		}
		current.span.Calls++
		current.directSQLQueries++
		current.span.RowsScanned += event.rowsScanned
		current.span.ItemsProcessed += event.rowsScanned
		if current.span.ItemUnit == "" {
			current.span.ItemUnit = "rows"
		}
		current.directTotalDuration += event.totalDuration
		current.directSQLDuration += event.sqlDuration
		if event.query != "" && !current.seenQueryMap[event.query] && len(current.distinctQueries) < 3 {
			current.seenQueryMap[event.query] = true
			current.distinctQueries = append(current.distinctQueries, event.query)
		}
	}

	finalizeBuildingTree(database, rootBuilder, true, elapsed, allocBytes, mallocs)
	return rootBuilder.span
}

func finalizeBuildingTree(
	database *sql.DB,
	builder *buildingNode,
	isRoot bool,
	rootElapsed time.Duration,
	rootAllocBytes uint64,
	rootMallocs uint64,
) {
	var childrenDurationSum time.Duration
	var childrenSQLDurationSum time.Duration
	var childrenSQLQueriesSum int
	var childrenRowsSum int

	for _, childBuilder := range builder.orderedChildren {
		finalizeBuildingTree(database, childBuilder, false, 0, 0, 0)
		childrenDurationSum += childBuilder.span.TotalDuration
		childrenSQLDurationSum += childBuilder.span.SQLDuration
		childrenSQLQueriesSum += childBuilder.span.SQLQueries
		childrenRowsSum += childBuilder.span.RowsScanned
		builder.span.Children = append(builder.span.Children, childBuilder.span)
	}

	if isRoot {
		builder.span.TotalDuration = rootElapsed
		builder.span.AllocBytes = rootAllocBytes
		builder.span.Mallocs = rootMallocs
	} else if builder.span.Category == "subspan" && builder.directTotalDuration >= childrenDurationSum {
		builder.span.TotalDuration = builder.directTotalDuration
	} else {
		builder.span.TotalDuration = builder.directTotalDuration + childrenDurationSum
	}

	builder.span.SQLDuration = builder.directSQLDuration + childrenSQLDurationSum
	builder.span.SQLQueries = builder.directSQLQueries + childrenSQLQueriesSum
	if builder.span.RowsScanned == 0 {
		builder.span.RowsScanned = childrenRowsSum
	}
	if builder.span.TotalDuration > childrenDurationSum {
		builder.span.SelfDuration = builder.span.TotalDuration - childrenDurationSum
	}
	if builder.span.TotalDuration > builder.span.SQLDuration {
		builder.span.GoCPUDuration = builder.span.TotalDuration - builder.span.SQLDuration
	}
	if builder.directSQLQueries > 5 {
		builder.span.HasNPlusOne = true
	}

	if database != nil {
		for _, queryText := range builder.distinctQueries {
			planText, hasFullScan, hasTempBTree := explainQueryPlan(database, queryText)
			if planText != "" {
				builder.span.QueryPlans = append(builder.span.QueryPlans, planText)
			}
			builder.span.HasFullScan = builder.span.HasFullScan || hasFullScan
			builder.span.HasTempBTree = builder.span.HasTempBTree || hasTempBTree
		}
	}

	if isRoot && len(builder.span.Children) > 0 && builder.span.SelfDuration > 50*time.Microsecond {
		inMemoryNode := &SpanNode{
			Name:           "[In-Memory Go CPU (JSON + Domain Logic)]",
			Location:       builder.span.Location,
			Category:       "cpu",
			Calls:          1,
			ItemsProcessed: builder.span.ItemsProcessed,
			ItemUnit:       builder.span.ItemUnit,
			TotalDuration:  builder.span.SelfDuration,
			SelfDuration:   builder.span.SelfDuration,
			GoCPUDuration:  builder.span.SelfDuration,
		}
		builder.span.Children = append(builder.span.Children, inMemoryNode)
	}
}

type singleExecOutcome struct {
	value         any
	summary       string
	items         int
	secondary     int
	errorReturned error
}

func executeMeasuredRun(
	database *sql.DB,
	recorder *treeRecorder,
	mode string,
	rootSpanName string,
	itemUnit string,
	iterations int,
	beforeEach func(),
	invoke func() (singleExecOutcome, error),
) (ProbeRun, int, int, error) {
	if iterations < 1 {
		iterations = 1
	}
	durations := make([]time.Duration, 0, iterations)
	var lastOutcome singleExecOutcome
	var lastAllocBytes uint64
	var lastMallocs uint64
	var lastRootSpan *SpanNode

	for iter := 0; iter < iterations; iter++ {
		if beforeEach != nil {
			beforeEach()
		}
		var memBefore, memAfter runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		recorder.start()
		startTime := time.Now()
		outcome, invokeError := invoke()
		elapsed := time.Since(startTime)
		recorder.stop()

		runtime.ReadMemStats(&memAfter)
		if invokeError != nil {
			return ProbeRun{}, 0, 0, invokeError
		}
		durations = append(durations, elapsed)
		lastOutcome = outcome
		lastAllocBytes = memAfter.TotalAlloc - memBefore.TotalAlloc
		lastMallocs = memAfter.Mallocs - memBefore.Mallocs
		lastRootSpan = recorder.buildTree(database, rootSpanName, outcome.items, itemUnit, elapsed, lastAllocBytes, lastMallocs)
	}

	slices.Sort(durations)
	medianDuration := durations[len(durations)/2]
	minDuration := durations[0]
	maxDuration := durations[len(durations)-1]

	var nsPerItem, itemsPerSec, bytesPerItem, allocsPerItem float64
	if lastOutcome.items > 0 {
		nsPerItem = float64(medianDuration.Nanoseconds()) / float64(lastOutcome.items)
		if medianDuration > 0 {
			itemsPerSec = float64(lastOutcome.items) / medianDuration.Seconds()
		}
		bytesPerItem = float64(lastAllocBytes) / float64(lastOutcome.items)
		allocsPerItem = float64(lastMallocs) / float64(lastOutcome.items)
	}

	return ProbeRun{
		Mode:                mode,
		Iterations:          iterations,
		MedianDuration:      medianDuration,
		MinDuration:         minDuration,
		MaxDuration:         maxDuration,
		NormalizedNsPerItem: nsPerItem,
		ItemsPerSecond:      itemsPerSec,
		AllocBytes:          lastAllocBytes,
		BytesPerItem:        bytesPerItem,
		Mallocs:             lastMallocs,
		AllocsPerItem:       allocsPerItem,
		SQLQueries:          lastRootSpan.SQLQueries,
		SQLDuration:         lastRootSpan.SQLDuration,
		GoCPUDuration:       lastRootSpan.GoCPUDuration,
		ResultHash:          hashResultValue(lastOutcome.value),
		ResultSummary:       lastOutcome.summary,
		RootSpan:            lastRootSpan,
	}, lastOutcome.items, lastOutcome.secondary, nil
}

func hashResultValue(value any) string {
	encoded, marshalError := json.Marshal(value)
	if marshalError != nil {
		return "error"
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:8])
}

func invalidateAllBackendCaches() {
	stats.InvalidateLedgerCache()
	expedition.InvalidateShipCache()
	journal.InvalidateCommanderCountsCache()
}

func runColdWarmProbe(
	database *sql.DB,
	recorder *treeRecorder,
	name string,
	category string,
	description string,
	rootSpanName string,
	itemUnit string,
	secondaryUnit string,
	iterations int,
	invoke func() (singleExecOutcome, error),
) (ProbeResult, error) {
	coldRun, items, secondary, coldError := executeMeasuredRun(
		database,
		recorder,
		"Cold",
		rootSpanName,
		itemUnit,
		iterations,
		invalidateAllBackendCaches,
		invoke,
	)
	if coldError != nil {
		return ProbeResult{}, coldError
	}

	warmRun, _, _, warmError := executeMeasuredRun(
		database,
		recorder,
		"Warm",
		rootSpanName,
		itemUnit,
		iterations,
		nil,
		invoke,
	)
	if warmError != nil {
		return ProbeResult{}, warmError
	}

	var speedupPct float64
	if coldRun.MedianDuration > 0 {
		speedupPct = (float64(coldRun.MedianDuration-warmRun.MedianDuration) / float64(coldRun.MedianDuration)) * 100.0
	}

	return ProbeResult{
		Name:           name,
		Category:       category,
		Description:    description,
		ItemsProcessed: items,
		ItemUnit:       itemUnit,
		SecondaryCount: secondary,
		SecondaryUnit:  secondaryUnit,
		Cold:           coldRun,
		Warm:           warmRun,
		HasWarm:        true,
		ColdWarmMatch:  coldRun.ResultHash == warmRun.ResultHash,
		WarmSpeedupPct: speedupPct,
	}, nil
}