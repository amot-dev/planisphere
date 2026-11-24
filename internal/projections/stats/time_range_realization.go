package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
)

var (
	timeRangeRealizationAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
	// Sales, ship destruction, and crew wage payments retroactively affect earlier scan windows.
	realizationRetroactiveJournalEvents = journal.NewEventSet(
		journal.EventSellExplorationData,
		journal.EventMultiSellExplorationData,
		journal.EventSellOrganicData,
		journal.EventResurrect,
		journal.EventNpcCrewPaidWage,
	)
)

// TimeRangeRealization caches Realized Value profit and loss breakdowns for
// bounded (after, until] time ranges and optional Discovery Flight Mode filters.
type TimeRangeRealization struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	modeLoader  replaystats.ModeLookupLoader
	entries     *projection.KeyedCache[string, RealizationBreakdown]
}

// NewTimeRangeRealization creates a TimeRangeRealization projection and registers it on bus.
func NewTimeRangeRealization(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	modeLoader replaystats.ModeLookupLoader,
) *TimeRangeRealization {
	triggers := projection.Triggers{
		Journal: realizationStatsJournalEvents,
		App:     timeRangeRealizationAppEvents,
	}
	return &TimeRangeRealization{
		database:    dbConn,
		salesLedger: salesLedger,
		modeLoader:  modeLoader,
		entries:     newTimeRangeDomainCache[RealizationBreakdown](bus, "TimeRangeRealization", triggers, realizationRetroactiveJournalEvents),
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
	return timeRangeReal.computeWithModeLoader(timeRange, filter, timeRangeReal.modeLoader)
}

func (timeRangeReal *TimeRangeRealization) computeWithModeLoader(timeRange Range, filter *Filter, modeLoader replaystats.ModeLookupLoader) (RealizationBreakdown, error) {
	if timeRangeReal.database == nil {
		return RealizationBreakdown{}, nil
	}
	cacheKey := buildModeDomainStatsKey(timeRange, filter, true)
	return timeRangeReal.entries.GetOrLoad(cacheKey, func() (RealizationBreakdown, error) {
		engine := replaystats.NewEngineWithSources(
			timeRangeReal.database,
			timeRangeReal.salesLedger,
			nil,
			modeLoader,
		)
		return engine.ComputeRealization(timeRange, filter)
	})
}
