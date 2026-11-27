package stats

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"sync"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
)

var closedWindowRevisionAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeStats caches bounded expedition and telemetry window metrics across
// specialized section projections.
type TimeRangeStats struct {
	modeLoader           replaystats.ModeLookupLoader
	travel               *TimeRangeTravel
	cartographics        *TimeRangeCartographics
	exobiology           *TimeRangeExobiology
	realization          *TimeRangeRealization
	watermarkMu          sync.Mutex
	closedWindowRevision uint64
	telemetryRevision    uint64
	highWaterTimestamp   string
}

// NewTimeRangeStats creates and registers all time-windowed section projections on bus.
func NewTimeRangeStats(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
	modeLoader replaystats.ModeLookupLoader,
) *TimeRangeStats {
	initialHighWater := ""
	if dbConn != nil {
		initialHighWater = replay.LatestEventTimestamp(dbConn)
	}
	timeRangeStats := &TimeRangeStats{
		modeLoader:           modeLoader,
		travel:               NewTimeRangeTravel(dbConn, bus, recoveredJumps, modeLoader),
		cartographics:        NewTimeRangeCartographics(dbConn, bus, modeLoader),
		exobiology:           NewTimeRangeExobiology(dbConn, bus, salesLedger, modeLoader),
		realization:          NewTimeRangeRealization(dbConn, bus, salesLedger, modeLoader),
		closedWindowRevision: 1,
		telemetryRevision:    1,
		highWaterTimestamp:   initialHighWater,
	}
	if bus != nil {
		bus.OnTrigger(
			timeRangeStats.Triggers().
				WithJournal(
					journal.EventCommander,
					journal.EventLoadGame,
					journal.EventShipyardSwap,
					journal.EventClearSavedGame,
					journal.EventNewCommander,
				).
				WithApp(events.AppExpeditionModesChanged),
			timeRangeStats.observeTrigger,
		)
	}
	return timeRangeStats
}

// Travel returns the time-windowed Flight Operations projection.
func (timeRangeStats *TimeRangeStats) Travel() *TimeRangeTravel {
	return timeRangeStats.travel
}

// Cartographics returns the time-windowed Universal Cartographics projection.
func (timeRangeStats *TimeRangeStats) Cartographics() *TimeRangeCartographics {
	return timeRangeStats.cartographics
}

// Exobiology returns the time-windowed Vista Genomics exobiology projection.
func (timeRangeStats *TimeRangeStats) Exobiology() *TimeRangeExobiology {
	return timeRangeStats.exobiology
}

// Realization returns the time-windowed Realized Value projection.
func (timeRangeStats *TimeRangeStats) Realization() *TimeRangeRealization {
	return timeRangeStats.realization
}

// Triggers returns the combined journal and app triggers across all section projections.
func (timeRangeStats *TimeRangeStats) Triggers() projection.Triggers {
	return projection.TriggersFrom(
		timeRangeStats.travel,
		timeRangeStats.cartographics,
		timeRangeStats.exobiology,
		timeRangeStats.realization,
	)
}

// CachedCount returns the maximum number of bounded range entries currently cached
// across the time-windowed section projections.
func (timeRangeStats *TimeRangeStats) CachedCount() int {
	return max(
		timeRangeStats.travel.CachedCount(),
		timeRangeStats.cartographics.CachedCount(),
		timeRangeStats.exobiology.CachedCount(),
		timeRangeStats.realization.CachedCount(),
	)
}

// Generation returns the highest invalidation generation counter across all
// time-windowed section projections.
func (timeRangeStats *TimeRangeStats) Generation() uint64 {
	return max(
		timeRangeStats.travel.Generation(),
		timeRangeStats.cartographics.Generation(),
		timeRangeStats.exobiology.Generation(),
		timeRangeStats.realization.Generation(),
	)
}

// ClosedWindowRevision returns a monotonic watermark that advances only when
// closed historical telemetry windows may change due to retroactive settlements,
// commander or database switches, or out-of-order backfills.
func (timeRangeStats *TimeRangeStats) ClosedWindowRevision() uint64 {
	timeRangeStats.watermarkMu.Lock()
	defer timeRangeStats.watermarkMu.Unlock()
	return timeRangeStats.closedWindowRevision
}

// EnsureMinClosedWindowRevision raises closedWindowRevision to at least minRevision
// so replacing TimeRangeStats across a hot database switch never resets or collides
// with an earlier watermark.
func (timeRangeStats *TimeRangeStats) EnsureMinClosedWindowRevision(minRevision uint64) {
	timeRangeStats.watermarkMu.Lock()
	defer timeRangeStats.watermarkMu.Unlock()
	if timeRangeStats.closedWindowRevision < minRevision {
		timeRangeStats.closedWindowRevision = minRevision
	}
}

// TelemetryRevision returns a monotonic revision counter that advances whenever
// travel, discovery, realization, vessel, identity, or expedition mode triggers
// may affect open waypoint telemetry drawers.
func (timeRangeStats *TimeRangeStats) TelemetryRevision() uint64 {
	timeRangeStats.watermarkMu.Lock()
	defer timeRangeStats.watermarkMu.Unlock()
	return timeRangeStats.telemetryRevision
}

// EnsureMinTelemetryRevision raises telemetryRevision to at least minRevision
// so replacing TimeRangeStats across a hot database switch never resets or collides
// with an earlier revision.
func (timeRangeStats *TimeRangeStats) EnsureMinTelemetryRevision(minRevision uint64) {
	timeRangeStats.watermarkMu.Lock()
	defer timeRangeStats.watermarkMu.Unlock()
	if timeRangeStats.telemetryRevision < minRevision {
		timeRangeStats.telemetryRevision = minRevision
	}
}

func (timeRangeStats *TimeRangeStats) observeTrigger(observed projection.Triggers) {
	timeRangeStats.watermarkMu.Lock()
	defer timeRangeStats.watermarkMu.Unlock()
	timeRangeStats.telemetryRevision++
	if observed.App.Intersects(closedWindowRevisionAppEvents) ||
		(!observed.Journal.IsEmpty() &&
			(observed.Journal.Intersects(realizationRetroactiveJournalEvents) ||
				observed.EarliestTimestamp == "" ||
				observed.EarliestTimestamp <= timeRangeStats.highWaterTimestamp)) {
		timeRangeStats.closedWindowRevision++
	}
	latestObserved := max(observed.LatestTimestamp, observed.EarliestTimestamp)
	if latestObserved > timeRangeStats.highWaterTimestamp {
		timeRangeStats.highWaterTimestamp = latestObserved
	}
}

// Compute returns the merged metrics and realization breakdown for timeRange
// and optional Flight Mode filter, backed by the independently cached sections.
func (timeRangeStats *TimeRangeStats) Compute(timeRange Range, filter *Filter) (TelemetrySummary, error) {
	modeLoader := timeRangeStats.modeLoader
	if filter != nil && filter.Enabled {
		modeLoader = memoizeModeLookupLoader(modeLoader)
	}
	travelMetrics, travelError := timeRangeStats.travel.computeWithModeLoader(timeRange, filter, modeLoader)
	if travelError != nil {
		return TelemetrySummary{}, travelError
	}
	cartoMetrics, cartoError := timeRangeStats.cartographics.computeWithModeLoader(timeRange, filter, modeLoader)
	if cartoError != nil {
		return TelemetrySummary{}, cartoError
	}
	exobiologyMetrics, exobiologyError := timeRangeStats.exobiology.computeWithModeLoader(timeRange, filter, modeLoader)
	if exobiologyError != nil {
		return TelemetrySummary{}, exobiologyError
	}
	realizationBreakdown, realizationError := timeRangeStats.realization.computeWithModeLoader(timeRange, filter, modeLoader)
	if realizationError != nil {
		return TelemetrySummary{}, realizationError
	}

	var combinedMetrics Metrics
	travelMetrics.ApplyTo(&combinedMetrics)
	cartoMetrics.ApplyTo(&combinedMetrics)
	exobiologyMetrics.ApplyTo(&combinedMetrics)
	return TelemetrySummary{
		Metrics:     combinedMetrics,
		Realization: realizationBreakdown,
	}, nil
}

func memoizeModeLookupLoader(baseLoader replaystats.ModeLookupLoader) replaystats.ModeLookupLoader {
	if baseLoader == nil {
		return nil
	}
	var (
		loadOnce     sync.Once
		cachedLookup replaystats.ModeLookup
		cachedError  error
	)
	return func(queryer db.Queryer, expeditionID int64) (replaystats.ModeLookup, error) {
		loadOnce.Do(func() {
			cachedLookup, cachedError = baseLoader(queryer, expeditionID)
		})
		return cachedLookup, cachedError
	}
}

// newTimeRangeDomainCache registers standard triggers for a time-window cache,
// preserves closed windows ending strictly before live journal batches unless
// retroactiveJournalEvents intersect the batch, and narrows AppExpeditionModesChanged
// invalidation to mode-filtered keys.
func newTimeRangeDomainCache[V any](
	bus *projection.Bus,
	projectionName string,
	triggers projection.Triggers,
	retroactiveJournalEvents events.JournalEventSet,
) *projection.KeyedCache[string, V] {
	entries := projection.NewKeyedCache[string, V](bus, projectionName, triggers)
	entries.SetInvalidationFilter(func(observed projection.Triggers, cacheKey string) bool {
		if !observed.App.IsEmpty() {
			return true
		}
		if !retroactiveJournalEvents.IsEmpty() && observed.Journal.Intersects(retroactiveJournalEvents) {
			return true
		}
		return !projection.WindowEndsBefore(cacheKey, observed.EarliestTimestamp)
	})
	if bus != nil {
		bus.OnTrigger(projection.Triggers{App: events.NewAppSet(events.AppExpeditionModesChanged)}, func(_ projection.Triggers) {
			entries.InvalidateWhere(func(cacheKey string) bool {
				return strings.Contains(cacheKey, "|exp:")
			})
		})
	}
	return entries
}

func buildModeDomainStatsKey(timeRange Range, filter *Filter, discoveryDomain bool) string {
	var scopeSuffix string
	if filter != nil {
		scopeSuffix = filter.Scope.CacheKeySuffix()
	}
	if filter == nil || !filter.Enabled {
		return timeRange.After + "|" + timeRange.Until + "|all" + scopeSuffix
	}
	seenIDs := make(map[int64]bool, len(filter.Modes))
	selectedIDs := make([]int64, 0, len(filter.Modes))
	for _, selection := range filter.Modes {
		included := selection.Travel
		if discoveryDomain {
			included = selection.Discovery
		}
		if !included || seenIDs[selection.ModeID] {
			continue
		}
		seenIDs[selection.ModeID] = true
		selectedIDs = append(selectedIDs, selection.ModeID)
	}
	sort.Slice(selectedIDs, func(leftIndex, rightIndex int) bool {
		return selectedIDs[leftIndex] < selectedIDs[rightIndex]
	})
	var builder strings.Builder
	builder.WriteString(timeRange.After)
	builder.WriteByte('|')
	builder.WriteString(timeRange.Until)
	builder.WriteString("|exp:")
	builder.WriteString(strconv.FormatInt(filter.ExpeditionID, 10))
	if len(selectedIDs) == 0 {
		builder.WriteString("|none")
		builder.WriteString(scopeSuffix)
		return builder.String()
	}
	builder.WriteString("|modes:")
	for index, modeID := range selectedIDs {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatInt(modeID, 10))
	}
	builder.WriteString(scopeSuffix)
	return builder.String()
}
