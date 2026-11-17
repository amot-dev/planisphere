package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
)

var timeRangeCartographicsAppEvents = events.NewAppSet(
	events.AppCommanderFilterChanged,
	events.AppModesChanged,
	events.AppExpeditionBoundsChanged,
	events.AppDatabaseSwitched,
)

// TimeRangeCartographics caches Universal Cartographics metrics for bounded
// (after, until] time ranges and optional Discovery Flight Mode filters.
type TimeRangeCartographics struct {
	database *sql.DB
	entries  *projection.KeyedCache[string, CartographicsMetrics]
}

// NewTimeRangeCartographics creates a TimeRangeCartographics projection and registers it on bus.
func NewTimeRangeCartographics(dbConn *sql.DB, bus *projection.Bus) *TimeRangeCartographics {
	timeRangeCarto := &TimeRangeCartographics{database: dbConn}
	timeRangeCarto.entries = projection.NewKeyedCache[string, CartographicsMetrics](
		bus,
		"time_range_cartographics",
		timeRangeCarto.Triggers(),
	)
	return timeRangeCarto
}

// Triggers declares the journal and application events that invalidate TimeRangeCartographics.
func (timeRangeCarto *TimeRangeCartographics) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: cartographicsJournalEvents,
		App:     timeRangeCartographicsAppEvents,
	}
}

// CachedCount returns the number of bounded range cartographics entries currently cached.
func (timeRangeCarto *TimeRangeCartographics) CachedCount() int {
	return timeRangeCarto.entries.Len()
}

// Generation returns the cache invalidation generation counter.
func (timeRangeCarto *TimeRangeCartographics) Generation() uint64 {
	return timeRangeCarto.entries.Generation()
}

// Compute returns cached or freshly evaluated cartographics metrics for timeRange and optional mode filter.
func (timeRangeCarto *TimeRangeCartographics) Compute(timeRange Range, filter *Filter) (CartographicsMetrics, error) {
	return timeRangeCarto.computeWithPreparedFilter(timeRange, cloneUnpreparedFilter(filter))
}

func (timeRangeCarto *TimeRangeCartographics) computeWithPreparedFilter(timeRange Range, preparedFilter *Filter) (CartographicsMetrics, error) {
	if timeRangeCarto.database == nil {
		return CartographicsMetrics{}, fmt.Errorf("time range cartographics database is nil")
	}
	cacheKey := buildDiscoveryStatsKey(timeRange, preparedFilter)
	return timeRangeCarto.entries.GetOrLoad(cacheKey, func() (CartographicsMetrics, error) {
		engine := NewEngine(timeRangeCarto.database)
		return engine.ComputeCartographics(timeRange, preparedFilter)
	})
}

