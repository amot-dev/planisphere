package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	replaystats "planisphere/internal/replay/stats"
)

var timeRangeCartographicsAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeCartographics caches Universal Cartographics metrics for bounded
// (after, until] time ranges and optional Discovery Flight Mode filters.
type TimeRangeCartographics struct {
	database   *sql.DB
	modeLoader replaystats.ModeLookupLoader
	entries    *projection.KeyedCache[string, CartographicsMetrics]
}

// NewTimeRangeCartographics creates a TimeRangeCartographics projection and registers it on bus.
func NewTimeRangeCartographics(
	dbConn *sql.DB,
	bus *projection.Bus,
	modeLoader replaystats.ModeLookupLoader,
) *TimeRangeCartographics {
	triggers := projection.Triggers{
		Journal: cartographicsJournalEvents,
		App:     timeRangeCartographicsAppEvents,
	}
	return &TimeRangeCartographics{
		database:   dbConn,
		modeLoader: modeLoader,
		entries:    newTimeRangeDomainCache[CartographicsMetrics](bus, "TimeRangeCartographics", triggers, 0),
	}
}

// Triggers reports the journal and app events that invalidate TimeRangeCartographics.
func (timeRangeCarto *TimeRangeCartographics) Triggers() projection.Triggers {
	return timeRangeCarto.entries.Triggers()
}

// CachedCount returns the number of bounded range cartographics entries currently cached.
func (timeRangeCarto *TimeRangeCartographics) CachedCount() int {
	return timeRangeCarto.entries.Len()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (timeRangeCarto *TimeRangeCartographics) Generation() uint64 {
	return timeRangeCarto.entries.Generation()
}

// Compute returns Universal Cartographics metrics for timeRange and filter, caching per discovery filter.
func (timeRangeCarto *TimeRangeCartographics) Compute(timeRange Range, filter *Filter) (CartographicsMetrics, error) {
	return timeRangeCarto.computeWithModeLoader(timeRange, filter, timeRangeCarto.modeLoader)
}

func (timeRangeCarto *TimeRangeCartographics) computeWithModeLoader(timeRange Range, filter *Filter, modeLoader replaystats.ModeLookupLoader) (CartographicsMetrics, error) {
	if timeRangeCarto.database == nil {
		return CartographicsMetrics{}, nil
	}
	cacheKey := buildModeDomainStatsKey(timeRange, filter, true)
	return timeRangeCarto.entries.GetOrLoad(cacheKey, func() (CartographicsMetrics, error) {
		engine := replaystats.NewEngineWithSources(
			timeRangeCarto.database,
			nil,
			nil,
			modeLoader,
		)
		return engine.ComputeCartographics(timeRange, filter)
	})
}
