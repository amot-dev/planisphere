package stats

import (
	"database/sql"
	"sort"
	"strconv"
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	replaystats "planisphere/internal/replay/stats"
)

// TimeRangeStats caches bounded expedition and telemetry window metrics across
// specialized section projections.
type TimeRangeStats struct {
	travel        *TimeRangeTravel
	cartographics *TimeRangeCartographics
	exobiology    *TimeRangeExobiology
	realization   *TimeRangeRealization
}

// NewTimeRangeStats creates and registers all time-windowed section projections on bus.
func NewTimeRangeStats(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
	modeLoader replaystats.ModeTimelineLoader,
) *TimeRangeStats {
	return &TimeRangeStats{
		travel:        NewTimeRangeTravel(dbConn, bus, recoveredJumps, modeLoader),
		cartographics: NewTimeRangeCartographics(dbConn, bus, modeLoader),
		exobiology:    NewTimeRangeExobiology(dbConn, bus, salesLedger, modeLoader),
		realization:   NewTimeRangeRealization(dbConn, bus, salesLedger, modeLoader),
	}
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

// Compute returns the merged metrics and realization breakdown for timeRange
// and optional Flight Mode filter, backed by the independently cached sections.
func (timeRangeStats *TimeRangeStats) Compute(timeRange Range, filter *Filter) (TelemetrySummary, error) {
	travelMetrics, travelError := timeRangeStats.travel.Compute(timeRange, filter)
	if travelError != nil {
		return TelemetrySummary{}, travelError
	}
	cartoMetrics, cartoError := timeRangeStats.cartographics.Compute(timeRange, filter)
	if cartoError != nil {
		return TelemetrySummary{}, cartoError
	}
	exobiologyMetrics, exobiologyError := timeRangeStats.exobiology.Compute(timeRange, filter)
	if exobiologyError != nil {
		return TelemetrySummary{}, exobiologyError
	}
	realizationBreakdown, realizationError := timeRangeStats.realization.Compute(timeRange, filter)
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

// newTimeRangeDomainCache registers standard triggers for a time-window cache,
// preserves closed windows ending strictly before live journal batches unless
// retroactiveJournalEvents intersect the batch, and narrows AppModeWindowsChanged
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
		bus.OnTrigger(projection.Triggers{App: events.NewAppSet(events.AppModeWindowsChanged)}, func(_ projection.Triggers) {
			entries.InvalidateWhere(func(cacheKey string) bool {
				return !strings.HasSuffix(cacheKey, "|all")
			})
		})
	}
	return entries
}

func buildModeDomainStatsKey(timeRange Range, filter *Filter, discoveryDomain bool) string {
	if filter == nil || !filter.Enabled {
		return timeRange.After + "|" + timeRange.Until + "|all"
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
	if len(selectedIDs) == 0 {
		return timeRange.After + "|" + timeRange.Until + "|none"
	}
	var builder strings.Builder
	builder.WriteString(timeRange.After)
	builder.WriteByte('|')
	builder.WriteString(timeRange.Until)
	builder.WriteString("|modes:")
	for index, modeID := range selectedIDs {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatInt(modeID, 10))
	}
	return builder.String()
}
