package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
)

var timeRangeRealizationAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppModesChanged,
	events.AppExpeditionBoundsChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeRealization caches Realized Value profit and loss breakdowns for
// bounded (after, until] time ranges and optional Discovery Flight Mode filters.
type TimeRangeRealization struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	entries     *projection.KeyedCache[string, RealizationBreakdown]
}

// NewTimeRangeRealization creates a TimeRangeRealization projection and registers it on bus.
func NewTimeRangeRealization(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
) *TimeRangeRealization {
	timeRangeReal := &TimeRangeRealization{
		database:    dbConn,
		salesLedger: salesLedger,
	}
	timeRangeReal.entries = projection.NewKeyedCache[string, RealizationBreakdown](
		bus,
		"time_range_realization",
		timeRangeReal.Triggers(),
	)
	return timeRangeReal
}

// Triggers declares the journal and application events that invalidate TimeRangeRealization.
func (timeRangeReal *TimeRangeRealization) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: realizationStatsJournalEvents,
		App:     timeRangeRealizationAppEvents,
	}
}

// CachedCount returns the number of bounded range realization entries currently cached.
func (timeRangeReal *TimeRangeRealization) CachedCount() int {
	return timeRangeReal.entries.Len()
}

// Generation returns the cache invalidation generation counter.
func (timeRangeReal *TimeRangeRealization) Generation() uint64 {
	return timeRangeReal.entries.Generation()
}

// Compute returns cached or freshly evaluated realization breakdown for timeRange and optional mode filter.
func (timeRangeReal *TimeRangeRealization) Compute(timeRange Range, filter *Filter) (RealizationBreakdown, error) {
	return timeRangeReal.computeWithPreparedFilter(timeRange, cloneUnpreparedFilter(filter))
}

func (timeRangeReal *TimeRangeRealization) computeWithPreparedFilter(timeRange Range, preparedFilter *Filter) (RealizationBreakdown, error) {
	if timeRangeReal.database == nil {
		return RealizationBreakdown{}, fmt.Errorf("time range realization database is nil")
	}
	cacheKey := buildDiscoveryStatsKey(timeRange, preparedFilter)
	return timeRangeReal.entries.GetOrLoad(cacheKey, func() (RealizationBreakdown, error) {
		engine := NewEngineWithProjections(timeRangeReal.database, timeRangeReal.salesLedger, nil)
		return engine.ComputeRealization(timeRange, preparedFilter)
	})
}

