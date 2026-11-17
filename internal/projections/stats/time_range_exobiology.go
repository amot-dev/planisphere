package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
)

var timeRangeExobiologyAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppModesChanged,
	events.AppExpeditionBoundsChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeExobiology caches Vista Genomics exobiology metrics for bounded
// (after, until] time ranges and optional Discovery Flight Mode filters.
type TimeRangeExobiology struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	entries     *projection.KeyedCache[string, ExobiologyMetrics]
}

// NewTimeRangeExobiology creates a TimeRangeExobiology projection and registers it on bus.
func NewTimeRangeExobiology(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
) *TimeRangeExobiology {
	timeRangeExo := &TimeRangeExobiology{
		database:    dbConn,
		salesLedger: salesLedger,
	}
	timeRangeExo.entries = projection.NewKeyedCache[string, ExobiologyMetrics](
		bus,
		"time_range_exobiology",
		timeRangeExo.Triggers(),
	)
	return timeRangeExo
}

// Triggers declares the journal and application events that invalidate TimeRangeExobiology.
func (timeRangeExo *TimeRangeExobiology) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: exobiologyJournalEvents,
		App:     timeRangeExobiologyAppEvents,
	}
}

// CachedCount returns the number of bounded range exobiology entries currently cached.
func (timeRangeExo *TimeRangeExobiology) CachedCount() int {
	return timeRangeExo.entries.Len()
}

// Generation returns the cache invalidation generation counter.
func (timeRangeExo *TimeRangeExobiology) Generation() uint64 {
	return timeRangeExo.entries.Generation()
}

// Compute returns cached or freshly evaluated exobiology metrics for timeRange and optional mode filter.
func (timeRangeExo *TimeRangeExobiology) Compute(timeRange Range, filter *Filter) (ExobiologyMetrics, error) {
	return timeRangeExo.computeWithPreparedFilter(timeRange, cloneUnpreparedFilter(filter))
}

func (timeRangeExo *TimeRangeExobiology) computeWithPreparedFilter(timeRange Range, preparedFilter *Filter) (ExobiologyMetrics, error) {
	if timeRangeExo.database == nil {
		return ExobiologyMetrics{}, fmt.Errorf("time range exobiology database is nil")
	}
	cacheKey := buildDiscoveryStatsKey(timeRange, preparedFilter)
	return timeRangeExo.entries.GetOrLoad(cacheKey, func() (ExobiologyMetrics, error) {
		engine := NewEngineWithProjections(timeRangeExo.database, timeRangeExo.salesLedger, nil)
		return engine.ComputeExobiology(timeRange, preparedFilter)
	})
}

