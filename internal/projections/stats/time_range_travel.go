package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
)

var timeRangeTravelAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppModesChanged,
	events.AppExpeditionBoundsChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeTravel caches Flight Operations navigation metrics for bounded
// (after, until] time ranges and optional Travel Flight Mode filters.
type TimeRangeTravel struct {
	database       *sql.DB
	recoveredJumps *projections.RecoveredJumps
	entries        *projection.KeyedCache[string, TravelMetrics]
}

// NewTimeRangeTravel creates a TimeRangeTravel projection and registers it on bus.
func NewTimeRangeTravel(
	dbConn *sql.DB,
	bus *projection.Bus,
	recoveredJumps *projections.RecoveredJumps,
) *TimeRangeTravel {
	timeRangeTravel := &TimeRangeTravel{
		database:       dbConn,
		recoveredJumps: recoveredJumps,
	}
	timeRangeTravel.entries = projection.NewKeyedCache[string, TravelMetrics](
		bus,
		"time_range_travel",
		timeRangeTravel.Triggers(),
	)
	return timeRangeTravel
}

// Triggers declares the journal and application events that invalidate TimeRangeTravel.
func (timeRangeTravel *TimeRangeTravel) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: travelStatsJournalEvents,
		App:     timeRangeTravelAppEvents,
	}
}

// CachedCount returns the number of bounded range travel entries currently cached.
func (timeRangeTravel *TimeRangeTravel) CachedCount() int {
	return timeRangeTravel.entries.Len()
}

// Generation returns the cache invalidation generation counter.
func (timeRangeTravel *TimeRangeTravel) Generation() uint64 {
	return timeRangeTravel.entries.Generation()
}

// Compute returns cached or freshly evaluated travel metrics for timeRange and optional mode filter.
func (timeRangeTravel *TimeRangeTravel) Compute(timeRange Range, filter *Filter) (TravelMetrics, error) {
	return timeRangeTravel.computeWithPreparedFilter(timeRange, cloneUnpreparedFilter(filter))
}

func (timeRangeTravel *TimeRangeTravel) computeWithPreparedFilter(timeRange Range, preparedFilter *Filter) (TravelMetrics, error) {
	if timeRangeTravel.database == nil {
		return TravelMetrics{}, fmt.Errorf("time range travel database is nil")
	}
	cacheKey := buildTravelStatsKey(timeRange, preparedFilter)
	return timeRangeTravel.entries.GetOrLoad(cacheKey, func() (TravelMetrics, error) {
		engine := NewEngineWithProjections(timeRangeTravel.database, nil, timeRangeTravel.recoveredJumps)
		return engine.ComputeTravel(timeRange, preparedFilter)
	})
}

