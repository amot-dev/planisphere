package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	replaystats "planisphere/internal/replay/stats"
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
	modeLoader  replaystats.ModeTimelineLoader
	entries     *projection.KeyedCache[string, ExobiologyMetrics]
}

// NewTimeRangeExobiology creates a TimeRangeExobiology projection and registers it on bus.
func NewTimeRangeExobiology(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	modeLoader replaystats.ModeTimelineLoader,
) *TimeRangeExobiology {
	triggers := projection.Triggers{
		Journal: exobiologyJournalEvents,
		App:     timeRangeExobiologyAppEvents,
	}
	return &TimeRangeExobiology{
		database:    dbConn,
		salesLedger: salesLedger,
		modeLoader:  modeLoader,
		entries:     projection.NewKeyedCache[string, ExobiologyMetrics](bus, "TimeRangeExobiology", triggers),
	}
}

// Triggers reports the journal and app events that invalidate TimeRangeExobiology.
func (timeRangeExo *TimeRangeExobiology) Triggers() projection.Triggers {
	return timeRangeExo.entries.Triggers()
}

// CachedCount returns the number of bounded range exobiology entries currently cached.
func (timeRangeExo *TimeRangeExobiology) CachedCount() int {
	return timeRangeExo.entries.Len()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (timeRangeExo *TimeRangeExobiology) Generation() uint64 {
	return timeRangeExo.entries.Generation()
}

// Compute returns Vista Genomics metrics for timeRange and filter, caching per discovery filter.
func (timeRangeExo *TimeRangeExobiology) Compute(timeRange Range, filter *Filter) (ExobiologyMetrics, error) {
	if timeRangeExo.database == nil {
		return ExobiologyMetrics{}, nil
	}
	cacheKey := buildDiscoveryStatsKey(timeRange, filter)
	return timeRangeExo.entries.GetOrLoad(cacheKey, func() (ExobiologyMetrics, error) {
		engine := replaystats.NewEngineWithSources(
			timeRangeExo.database,
			timeRangeExo.salesLedger,
			nil,
			timeRangeExo.modeLoader,
		)
		return engine.ComputeExobiology(timeRange, replaystats.CloneUnprepared(filter))
	})
}
