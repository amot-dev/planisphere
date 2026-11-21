package profile

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

type profileProjections struct {
	database       *sql.DB
	bus            *projection.Bus
	recoveredJumps *projections.RecoveredJumps
	systemArrivals *projections.SystemArrivals
	visitCounts    *projections.SystemVisitCounts
	shipHistory    *projections.ShipHistory
	carrierState   *projections.CarrierState
	salesLedger    *realization.SalesLedger
	careerStats    *stats.CareerStats
	timeRangeStats *stats.TimeRangeStats
}

func newProfileProjections(database *sql.DB) *profileProjections {
	bus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(database, bus)
	salesLedger := realization.NewSalesLedger(database, bus)
	return &profileProjections{
		database:       database,
		bus:            bus,
		recoveredJumps: recoveredJumps,
		systemArrivals: projections.NewSystemArrivals(database, bus),
		visitCounts:    projections.NewSystemVisitCounts(database, bus, recoveredJumps),
		shipHistory:    projections.NewShipHistory(database, bus),
		carrierState:   projections.NewCarrierState(database, bus),
		salesLedger:    salesLedger,
		careerStats:    stats.NewCareerStats(database, bus, salesLedger, recoveredJumps),
		timeRangeStats: stats.NewTimeRangeStats(database, bus, salesLedger, recoveredJumps, modes.LoadModeLookup),
	}
}

func (proj *profileProjections) expeditionProjections() *expedition.Projections {
	return &expedition.Projections{
		Arrivals:       proj.systemArrivals,
		VisitCounts:    proj.visitCounts,
		ShipHistory:    proj.shipHistory,
		TimeRangeStats: proj.timeRangeStats,
		SalesLedger:    proj.salesLedger,
		RecoveredJumps: proj.recoveredJumps,
	}
}

func (proj *profileProjections) invalidate() {
	if proj != nil && proj.bus != nil {
		proj.bus.PublishApp(events.AppDatabaseSwitched)
	}
}

// warmBaseProjections populates SalesLedger and RecoveredJumps after a full bus
// reset so bounded TimeRangeStats Cold runs measure per-window section replay
// without attributing lifetime ledger setup to each individual window.
func (proj *profileProjections) warmBaseProjections() error {
	if _, ledgerError := proj.salesLedger.Get(); ledgerError != nil {
		return fmt.Errorf("warm sales ledger: %w", ledgerError)
	}
	if _, jumpsError := proj.recoveredJumps.All(); jumpsError != nil {
		return fmt.Errorf("warm recovered jumps: %w", jumpsError)
	}
	return nil
}

func (proj *profileProjections) prepareJournalInvalidation(
	expeditionRange stats.Range,
	hasExpeditionRange bool,
	eventTypes ...journal.EventType,
) error {
	if _, careerError := proj.careerStats.Get(); careerError != nil {
		return fmt.Errorf("warm career stats before journal invalidation: %w", careerError)
	}
	if hasExpeditionRange {
		if _, rangeError := proj.timeRangeStats.Compute(expeditionRange, nil); rangeError != nil {
			return fmt.Errorf("warm expedition time range before journal invalidation: %w", rangeError)
		}
	}
	proj.bus.PublishJournal(journal.NewEventSet(eventTypes...))
	return nil
}

// RunSnapshotSuite creates a temporary read-only snapshot of the configured
// database, executes the live-data profiler suite against the snapshot, and
// removes the snapshot before returning.
func RunSnapshotSuite(ctx context.Context, options Options) (Report, error) {
	if options.Iterations <= 0 {
		options.Iterations = 3
	}
	sourcePath, resolveError := resolveSourceDBPath(options.DBPath)
	if resolveError != nil {
		return Report{}, resolveError
	}

	snapshotPath, snapshotError := db.CreateDebugSnapshot(sourcePath)
	if snapshotError != nil {
		return Report{}, fmt.Errorf("create debug snapshot from %s: %w", sourcePath, snapshotError)
	}
	defer db.RemoveDebugSnapshot(snapshotPath)

	database, openError := openTracedDatabase(snapshotPath)
	if openError != nil {
		return Report{}, fmt.Errorf("open traced snapshot %s: %w", snapshotPath, openError)
	}
	defer database.Close()

	dataset, inspectError := inspectDataset(database, sourcePath, snapshotPath)
	if inspectError != nil {
		return Report{}, fmt.Errorf("inspect dataset: %w", inspectError)
	}

	recorder := newTreeRecorder()
	setActiveRecorder(recorder)
	defer setActiveRecorder(nil)

	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Dataset:     dataset,
	}

	proj := newProfileProjections(database)

	statsProbes, scalingModels, statsError := runStatsProbes(database, proj, recorder, dataset, options)
	if statsError != nil {
		return report, statsError
	}
	report.Probes = append(report.Probes, statsProbes...)
	report.ScalingModels = append(report.ScalingModels, scalingModels...)

	expProbes, expError := runExpeditionProbes(database, proj, recorder, dataset, options)
	if expError != nil {
		return report, expError
	}
	report.Probes = append(report.Probes, expProbes...)

	archiveProbes, archiveError := runArchiveProbes(database, proj, recorder, dataset, options)
	if archiveError != nil {
		return report, archiveError
	}
	report.Probes = append(report.Probes, archiveProbes...)

	frontendProbes, frontendError := runFrontendProbes(ctx, database, proj, dataset, options)
	if frontendError != nil {
		return report, frontendError
	}
	report.Probes = append(report.Probes, frontendProbes...)

	populateReportDiagnostics(&report)
	return report, nil
}

func resolveSourceDBPath(explicitPath string) (string, error) {
	if trimmed := strings.TrimSpace(explicitPath); trimmed != "" {
		return trimmed, nil
	}
	if envPath := strings.TrimSpace(os.Getenv("ED_PLANNER_DB")); envPath != "" {
		return envPath, nil
	}
	return db.ResolveConfiguredPath()
}

func matchesFilter(probeName, filterPattern string) bool {
	trimmed := strings.TrimSpace(filterPattern)
	if trimmed == "" {
		return true
	}
	return strings.Contains(strings.ToLower(probeName), strings.ToLower(trimmed))
}

func populateReportDiagnostics(report *Report) {
	var leafSpans []HotspotEntry
	var sqlAnomalies []HotspotEntry
	var warmRemnants []HotspotEntry
	var memoryEntries []HotspotEntry

	for _, probe := range report.Probes {
		if probe.Cold.AllocBytes > 0 {
			memoryEntries = append(memoryEntries, HotspotEntry{
				ProbeName:     probe.Name,
				RunMode:       "Cold",
				SpanName:      probe.Cold.RootSpan.Name,
				Location:      probe.Cold.RootSpan.Location,
				SelfDuration:  probe.Cold.MedianDuration,
				TotalDuration: probe.Cold.MedianDuration,
				AllocBytes:    probe.Cold.AllocBytes,
				Finding:       fmt.Sprintf("%.1f B/%s (%.1f allocs/%s)", probe.Cold.BytesPerItem, probe.ItemUnit, probe.Cold.AllocsPerItem, probe.ItemUnit),
			})
		}
		if probe.Cold.RootSpan != nil {
			collectSpanDiagnostics(probe.Name, "Cold", probe.Cold.RootSpan, probe.Cold.MedianDuration, &leafSpans, &sqlAnomalies, nil)
		}
		if probe.HasWarm && probe.Warm.RootSpan != nil {
			collectSpanDiagnostics(probe.Name, "Warm", probe.Warm.RootSpan, probe.Warm.MedianDuration, nil, &sqlAnomalies, &warmRemnants)
		}
	}

	slices.SortFunc(leafSpans, func(left, right HotspotEntry) int {
		return cmp.Compare(right.SelfDuration, left.SelfDuration)
	})
	if len(leafSpans) > 6 {
		leafSpans = leafSpans[:6]
	}

	slices.SortFunc(memoryEntries, func(left, right HotspotEntry) int {
		return cmp.Compare(right.AllocBytes, left.AllocBytes)
	})
	if len(memoryEntries) > 5 {
		memoryEntries = memoryEntries[:5]
	}

	report.TopBySelfTime = leafSpans
	report.TopByMemory = memoryEntries
	report.SQLAnomalies = deduplicateHotspots(sqlAnomalies)
	report.WarmRemnants = deduplicateHotspots(warmRemnants)
}

func deduplicateHotspots(entries []HotspotEntry) []HotspotEntry {
	seen := make(map[string]bool, len(entries))
	var unique []HotspotEntry
	for _, entry := range entries {
		key := entry.SpanName + "|" + entry.Location + "|" + entry.Finding
		if !seen[key] {
			seen[key] = true
			unique = append(unique, entry)
		}
	}
	return unique
}

func collectSpanDiagnostics(
	probeName string,
	runMode string,
	node *SpanNode,
	rootDuration time.Duration,
	leafSpans *[]HotspotEntry,
	sqlAnomalies *[]HotspotEntry,
	warmRemnants *[]HotspotEntry,
) {
	isLeaf := len(node.Children) == 0
	if isLeaf && leafSpans != nil && node.SelfDuration > 0 {
		*leafSpans = append(*leafSpans, HotspotEntry{
			ProbeName:     probeName,
			RunMode:       runMode,
			SpanName:      node.Name,
			Location:      node.Location,
			SelfDuration:  node.SelfDuration,
			TotalDuration: node.TotalDuration,
			SQLQueries:    node.SQLQueries,
			RowsScanned:   node.RowsScanned,
		})
	}
	if sqlAnomalies != nil && (node.HasNPlusOne || node.HasFullScan || node.HasTempBTree) {
		var flags []string
		if node.HasNPlusOne {
			flags = append(flags, fmt.Sprintf("N+1 loop (%d SQL queries)", node.SQLQueries))
		}
		if node.HasFullScan {
			flags = append(flags, "Full Table Scan")
		}
		if node.HasTempBTree {
			flags = append(flags, "Temp B-Tree Sort")
		}
		*sqlAnomalies = append(*sqlAnomalies, HotspotEntry{
			ProbeName:     probeName,
			RunMode:       runMode,
			SpanName:      node.Name,
			Location:      node.Location,
			SelfDuration:  node.SelfDuration,
			TotalDuration: node.TotalDuration,
			SQLQueries:    node.SQLQueries,
			RowsScanned:   node.RowsScanned,
			Finding:       strings.Join(flags, ", "),
		})
	}
	if warmRemnants != nil && isLeaf && node.Category == "sql" && node.SelfDuration >= 250*time.Microsecond && rootDuration > 0 {
		pct := float64(node.SelfDuration) / float64(rootDuration) * 100.0
		if pct >= 25.0 {
			*warmRemnants = append(*warmRemnants, HotspotEntry{
				ProbeName:     probeName,
				RunMode:       runMode,
				SpanName:      node.Name,
				Location:      node.Location,
				SelfDuration:  node.SelfDuration,
				TotalDuration: node.TotalDuration,
				SQLQueries:    node.SQLQueries,
				RowsScanned:   node.RowsScanned,
				Finding:       fmt.Sprintf("Uncached SQL on Warm pass (%.1f%% of warm time, %d rows)", pct, node.RowsScanned),
			})
		}
	}
	for _, child := range node.Children {
		collectSpanDiagnostics(probeName, runMode, child, rootDuration, leafSpans, sqlAnomalies, warmRemnants)
	}
}

// SaveReport writes a profiling report as formatted JSON to filePath.
func SaveReport(filePath string, report Report) error {
	encoded, marshalError := json.MarshalIndent(report, "", "  ")
	if marshalError != nil {
		return marshalError
	}
	return os.WriteFile(filePath, append(encoded, '\n'), 0o644)
}

// LoadReport reads a previously saved JSON profiling report from filePath.
func LoadReport(filePath string) (Report, error) {
	rawBytes, readError := os.ReadFile(filePath)
	if readError != nil {
		return Report{}, readError
	}
	var report Report
	if unmarshalError := json.Unmarshal(rawBytes, &report); unmarshalError != nil {
		return Report{}, unmarshalError
	}
	return report, nil
}

// CompareReports computes per-probe timing, allocation, and result-hash deltas
// between a baseline report and a current report.
func CompareReports(baseline, current Report) DiffReport {
	baselineByName := make(map[string]ProbeResult, len(baseline.Probes))
	for _, probe := range baseline.Probes {
		baselineByName[probe.Name] = probe
	}

	diff := DiffReport{BaselineGeneratedAt: baseline.GeneratedAt}
	for _, currentProbe := range current.Probes {
		baseProbe, exists := baselineByName[currentProbe.Name]
		if !exists {
			continue
		}
		hashMatched := baseProbe.Cold.ResultHash == currentProbe.Cold.ResultHash
		if !hashMatched {
			diff.AnyResultMismatch = true
		}
		hasWarmBoth := currentProbe.HasWarm && baseProbe.HasWarm
		var warmDelta float64
		if hasWarmBoth {
			warmDelta = percentDelta(float64(baseProbe.Warm.MedianDuration), float64(currentProbe.Warm.MedianDuration))
		}
		diff.Entries = append(diff.Entries, DiffEntry{
			ProbeName:         currentProbe.Name,
			HasWarm:           hasWarmBoth,
			ColdBaseline:      baseProbe.Cold.MedianDuration,
			ColdCurrent:       currentProbe.Cold.MedianDuration,
			ColdDeltaPct:      percentDelta(float64(baseProbe.Cold.MedianDuration), float64(currentProbe.Cold.MedianDuration)),
			WarmBaseline:      baseProbe.Warm.MedianDuration,
			WarmCurrent:       currentProbe.Warm.MedianDuration,
			WarmDeltaPct:      warmDelta,
			AllocBaseline:     baseProbe.Cold.AllocBytes,
			AllocCurrent:      currentProbe.Cold.AllocBytes,
			AllocDeltaPct:     percentDelta(float64(baseProbe.Cold.AllocBytes), float64(currentProbe.Cold.AllocBytes)),
			ResultHashMatched: hashMatched,
		})
	}
	return diff
}

func percentDelta(baselineValue, currentValue float64) float64 {
	if baselineValue <= 0 {
		return 0
	}
	return ((currentValue - baselineValue) / baselineValue) * 100.0
}
