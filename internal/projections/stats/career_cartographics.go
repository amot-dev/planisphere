package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
)

var (
	// cartographicsJournalEvents restricts cache invalidation to body scan and
	// surface mapping events; session-context events are already persisted in
	// SQLite before any subsequent scan or mapping occurs in that system.
	cartographicsJournalEvents = journal.NewEventSet(
		journal.EventScan,
		journal.EventSAAScanComplete,
	)
	careerCartographicsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CareerCartographics caches lifetime unfiltered Universal Cartographics metrics.
type CareerCartographics struct {
	database *sql.DB
	cache    *projection.Cache[CartographicsMetrics]
}

// NewCareerCartographics creates a CareerCartographics projection and registers it on bus.
func NewCareerCartographics(dbConn *sql.DB, bus *projection.Bus) *CareerCartographics {
	careerCarto := &CareerCartographics{database: dbConn}
	triggers := projection.Triggers{
		Journal: cartographicsJournalEvents,
		App:     careerCartographicsAppEvents,
	}
	careerCarto.cache = projection.NewCache[CartographicsMetrics](
		bus,
		"CareerCartographics",
		triggers,
		careerCarto.load,
	)
	return careerCarto
}

// Triggers reports the journal and app events that invalidate CareerCartographics.
func (careerCarto *CareerCartographics) Triggers() projection.Triggers {
	return careerCarto.cache.Triggers()
}

// Get returns the cached all-time cartographics metrics.
func (careerCarto *CareerCartographics) Get() (CartographicsMetrics, error) {
	return careerCarto.cache.Get()
}

// IsValid reports whether CareerCartographics currently holds a warm cached value.
func (careerCarto *CareerCartographics) IsValid() bool {
	return careerCarto.cache.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerCarto *CareerCartographics) Generation() uint64 {
	return careerCarto.cache.Generation()
}

func (careerCarto *CareerCartographics) load() (CartographicsMetrics, error) {
	if careerCarto.database == nil {
		return CartographicsMetrics{}, nil
	}
	return replaystats.NewEngine(careerCarto.database).ComputeCartographics(Range{}, nil)
}
