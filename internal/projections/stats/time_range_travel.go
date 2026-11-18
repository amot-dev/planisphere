package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	replaystats "planisphere/internal/replay/stats"
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
	modeLoader     replaystats.ModeTimelineLoader
	entries        *projection.KeyedCache[string, TravelMetrics]
}

// NewTimeRangeTravel creates a TimeRangeTravel projection and registers it on bus.
func NewTimeRangeTravel(
	dbConn *sql.DB,
	bus *projection.Bus,
	recoveredJumps *projections.RecoveredJumps,
	modeLoader replaystats.ModeTimelineLoader,
) *TimeRangeTravel {
	triggers := projection.Triggers{
		Journal: travelStatsJournalEvents,
		App:     timeRangeTravelAppEvents,
	}
	return &TimeRangeTravel{
		database:       dbConn,
		recoveredJumps: recoveredJumps,
		modeLoader:     modeLoader,
		entries:        projection.NewKeyedCache[string, TravelMetrics](bus, "TimeRangeTravel", triggers),
	}
}

// Triggers reports the journal and app events that invalidate TimeRangeTravel.
func (timeRangeTravel *TimeRangeTravel) Triggers() projection.Triggers {
	return timeRangeTravel.entries.Triggers()
}

// CachedCount returns the number of bounded range travel entries currently cached.
func (timeRangeTravel *TimeRangeTravel) CachedCount() int {
	return timeRangeTravel.entries.Len()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (timeRangeTravel *TimeRangeTravel) Generation() uint64 {
	return timeRangeTravel.entries.Generation()
}

// Compute returns Flight Operations metrics for timeRange and filter, caching per travel filter.
func (timeRangeTravel *TimeRangeTravel) Compute(timeRange Range, filter *Filter) (TravelMetrics, error) {
	if timeRangeTravel.database == nil {
		return TravelMetrics{}, nil
	}
	cacheKey := buildTravelStatsKey(timeRange, filter)
	return timeRangeTravel.entries.GetOrLoad(cacheKey, func() (TravelMetrics, error) {
		engine := replaystats.NewEngineWithSources(
			timeRangeTravel.database,
			nil,
			timeRangeTravel.recoveredJumps,
			timeRangeTravel.modeLoader,
		)
		return engine.ComputeTravel(timeRange, replaystats.CloneUnprepared(filter))
	})
}
