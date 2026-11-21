package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	replaystats "planisphere/internal/replay/stats"
)

var timeRangeRealizationAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeRealization caches Realized Value profit and loss breakdowns for
// bounded (after, until] time ranges and optional Discovery Flight Mode filters.
type TimeRangeRealization struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	modeLoader  replaystats.ModeTimelineLoader
	entries     *projection.KeyedCache[string, RealizationBreakdown]
}

// NewTimeRangeRealization creates a TimeRangeRealization projection and registers it on bus.
func NewTimeRangeRealization(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	modeLoader replaystats.ModeTimelineLoader,
) *TimeRangeRealization {
	triggers := projection.Triggers{
		Journal: realizationStatsJournalEvents,
		App:     timeRangeRealizationAppEvents,
	}
	return &TimeRangeRealization{
		database:    dbConn,
		salesLedger: salesLedger,
		modeLoader:  modeLoader,
		entries:     newTimeRangeDomainCache[RealizationBreakdown](bus, "TimeRangeRealization", triggers),
	}
}

// Triggers reports the journal and app events that invalidate TimeRangeRealization.
func (timeRangeReal *TimeRangeRealization) Triggers() projection.Triggers {
	return timeRangeReal.entries.Triggers()
}

// CachedCount returns the number of bounded range realization entries currently cached.
func (timeRangeReal *TimeRangeRealization) CachedCount() int {
	return timeRangeReal.entries.Len()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (timeRangeReal *TimeRangeRealization) Generation() uint64 {
	return timeRangeReal.entries.Generation()
}

// Compute returns Realized Value breakdowns for timeRange and filter, caching per discovery filter.
func (timeRangeReal *TimeRangeRealization) Compute(timeRange Range, filter *Filter) (RealizationBreakdown, error) {
	if timeRangeReal.database == nil {
		return RealizationBreakdown{}, nil
	}
	cacheKey := buildModeDomainStatsKey(timeRange, filter, true)
	return timeRangeReal.entries.GetOrLoad(cacheKey, func() (RealizationBreakdown, error) {
		engine := replaystats.NewEngineWithSources(
			timeRangeReal.database,
			timeRangeReal.salesLedger,
			nil,
			timeRangeReal.modeLoader,
		)
		return engine.ComputeRealization(timeRange, filter)
	})
}
