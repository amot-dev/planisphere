package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
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

// CareerCartographics caches lifetime unfiltered Universal Cartographics metrics per profile scope.
type CareerCartographics struct {
	database *sql.DB
	cache    *projection.KeyedCache[string, CartographicsMetrics]
}

// NewCareerCartographics creates a CareerCartographics projection and registers it on bus.
func NewCareerCartographics(dbConn *sql.DB, bus *projection.Bus) *CareerCartographics {
	careerCarto := &CareerCartographics{database: dbConn}
	triggers := projection.Triggers{
		Journal: cartographicsJournalEvents,
		App:     careerCartographicsAppEvents,
	}
	careerCarto.cache = projection.NewKeyedCache[string, CartographicsMetrics](
		bus,
		"CareerCartographics",
		triggers,
	)
	return careerCarto
}

// Triggers reports the journal and app events that invalidate CareerCartographics.
func (careerCarto *CareerCartographics) Triggers() projection.Triggers {
	return careerCarto.cache.Triggers()
}

// Get returns the cached all-time cartographics metrics for globally enabled profiles.
func (careerCarto *CareerCartographics) Get() (CartographicsMetrics, error) {
	return careerCarto.GetScoped(replay.ProfileScope{})
}

// GetScoped returns cached all-time cartographics metrics for scope.
func (careerCarto *CareerCartographics) GetScoped(scope replay.ProfileScope) (CartographicsMetrics, error) {
	return careerCarto.cache.GetOrLoad(scope.CacheKeySuffix(), func() (CartographicsMetrics, error) {
		return careerCarto.load(scope)
	})
}

// IsValid reports whether CareerCartographics currently holds a warm cached value.
func (careerCarto *CareerCartographics) IsValid() bool {
	return careerCarto.cache.Len() > 0
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerCarto *CareerCartographics) Generation() uint64 {
	return careerCarto.cache.Generation()
}

func (careerCarto *CareerCartographics) load(scope replay.ProfileScope) (CartographicsMetrics, error) {
	if careerCarto.database == nil {
		return CartographicsMetrics{}, nil
	}
	engine := replaystats.NewEngine(careerCarto.database)
	engine.Scope = scope
	return engine.ComputeCartographics(Range{}, nil)
}
