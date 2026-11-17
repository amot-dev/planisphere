package stats

import (
	"database/sql"
	"slices"
	"strconv"
	"strings"

	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
)

// TimeRangeStats coordinates the four time-windowed statistics projections
// (Travel, Cartographics, Exobiology, and Realization) for bounded (after, until]
// ranges and optional Flight Mode filters, serving both the active Expedition
// sidebar and Waypoint #1 pre-expedition baselines.
type TimeRangeStats struct {
	travel        *TimeRangeTravel
	cartographics *TimeRangeCartographics
	exobiology    *TimeRangeExobiology
	realization   *TimeRangeRealization
}

// NewTimeRangeStats creates a TimeRangeStats facade and registers its four
// section projections on bus.
func NewTimeRangeStats(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
) *TimeRangeStats {
	return &TimeRangeStats{
		travel:        NewTimeRangeTravel(dbConn, bus, recoveredJumps),
		cartographics: NewTimeRangeCartographics(dbConn, bus),
		exobiology:    NewTimeRangeExobiology(dbConn, bus, salesLedger),
		realization:   NewTimeRangeRealization(dbConn, bus, salesLedger),
	}
}

// Travel returns the time-windowed Flight Operations travel projection.
func (timeRangeStats *TimeRangeStats) Travel() *TimeRangeTravel {
	return timeRangeStats.travel
}

// Cartographics returns the time-windowed Universal Cartographics valuation projection.
func (timeRangeStats *TimeRangeStats) Cartographics() *TimeRangeCartographics {
	return timeRangeStats.cartographics
}

// Exobiology returns the time-windowed Vista Genomics exobiology valuation projection.
func (timeRangeStats *TimeRangeStats) Exobiology() *TimeRangeExobiology {
	return timeRangeStats.exobiology
}

// Realization returns the time-windowed Realized Value breakdown projection.
func (timeRangeStats *TimeRangeStats) Realization() *TimeRangeRealization {
	return timeRangeStats.realization
}

// Triggers returns the union of journal and application events across all four
// time-windowed statistics projections.
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

// Generation returns the highest cache invalidation generation counter across
// the time-windowed section projections.
func (timeRangeStats *TimeRangeStats) Generation() uint64 {
	return max(
		timeRangeStats.travel.Generation(),
		timeRangeStats.cartographics.Generation(),
		timeRangeStats.exobiology.Generation(),
		timeRangeStats.realization.Generation(),
	)
}

// Compute returns cached or freshly evaluated metrics and realization breakdown
// for timeRange and optional mode filter across all four section projections.
func (timeRangeStats *TimeRangeStats) Compute(timeRange Range, filter *Filter) (TelemetrySummary, error) {
	preparedFilter := cloneUnpreparedFilter(filter)

	travelMetrics, travelError := timeRangeStats.travel.computeWithPreparedFilter(timeRange, preparedFilter)
	if travelError != nil {
		return TelemetrySummary{}, travelError
	}
	cartoMetrics, cartoError := timeRangeStats.cartographics.computeWithPreparedFilter(timeRange, preparedFilter)
	if cartoError != nil {
		return TelemetrySummary{}, cartoError
	}
	exoMetrics, exoError := timeRangeStats.exobiology.computeWithPreparedFilter(timeRange, preparedFilter)
	if exoError != nil {
		return TelemetrySummary{}, exoError
	}
	realizationBreakdown, realizationError := timeRangeStats.realization.computeWithPreparedFilter(timeRange, preparedFilter)
	if realizationError != nil {
		return TelemetrySummary{}, realizationError
	}

	var combinedMetrics Metrics
	travelMetrics.applyTo(&combinedMetrics)
	cartoMetrics.applyTo(&combinedMetrics)
	exoMetrics.applyTo(&combinedMetrics)

	return TelemetrySummary{
		Metrics:     combinedMetrics,
		Realization: realizationBreakdown,
	}, nil
}

func cloneUnpreparedFilter(filter *Filter) *Filter {
	if filter == nil {
		return nil
	}
	copied := *filter
	copied.preparedSets = nil
	copied.preparedDB = nil
	return &copied
}

func buildTravelStatsKey(timeRange Range, filter *Filter) string {
	return buildModeDomainStatsKey(timeRange, filter, false)
}

func buildDiscoveryStatsKey(timeRange Range, filter *Filter) string {
	return buildModeDomainStatsKey(timeRange, filter, true)
}

func buildModeDomainStatsKey(timeRange Range, filter *Filter, discoveryDomain bool) string {
	var keyBuilder strings.Builder
	keyBuilder.Grow(64)
	keyBuilder.WriteString(timeRange.After)
	keyBuilder.WriteByte('|')
	keyBuilder.WriteString(timeRange.Until)
	keyBuilder.WriteByte('|')
	if filter == nil || !filter.Enabled {
		keyBuilder.WriteString("unfiltered")
		return keyBuilder.String()
	}
	keyBuilder.WriteString("filtered:")
	activeModeIDs := make([]int64, 0, len(filter.Modes))
	for _, selection := range filter.Modes {
		matched := selection.Travel
		if discoveryDomain {
			matched = selection.Discovery
		}
		if matched {
			activeModeIDs = append(activeModeIDs, selection.ModeID)
		}
	}
	slices.Sort(activeModeIDs)
	activeModeIDs = slices.Compact(activeModeIDs)
	for index, modeID := range activeModeIDs {
		if index > 0 {
			keyBuilder.WriteByte(',')
		}
		keyBuilder.WriteString(strconv.FormatInt(modeID, 10))
	}
	return keyBuilder.String()
}
