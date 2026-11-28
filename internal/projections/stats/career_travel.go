package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
)

var (
	travelStatsJournalEvents = journal.NewEventSet(
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
	)
	careerTravelAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CareerTravel caches lifetime unfiltered Flight Operations metrics per profile scope.
type CareerTravel struct {
	database       *sql.DB
	recoveredJumps *projections.RecoveredJumps
	cache          *projection.KeyedCache[string, TravelMetrics]
}

// NewCareerTravel creates a CareerTravel projection and registers it on bus.
func NewCareerTravel(
	dbConn *sql.DB,
	bus *projection.Bus,
	recoveredJumps *projections.RecoveredJumps,
) *CareerTravel {
	careerTravel := &CareerTravel{
		database:       dbConn,
		recoveredJumps: recoveredJumps,
	}
	triggers := projection.Triggers{
		Journal: travelStatsJournalEvents,
		App:     careerTravelAppEvents,
	}
	careerTravel.cache = projection.NewKeyedCache[string, TravelMetrics](
		bus,
		"CareerTravel",
		triggers,
	)
	return careerTravel
}

// Triggers reports the journal and app events that invalidate CareerTravel.
func (careerTravel *CareerTravel) Triggers() projection.Triggers {
	return careerTravel.cache.Triggers()
}

// Get returns the cached all-time travel metrics for globally enabled profiles.
func (careerTravel *CareerTravel) Get() (TravelMetrics, error) {
	return careerTravel.GetScoped(replay.ProfileScope{})
}

// GetScoped returns cached all-time travel metrics for scope.
func (careerTravel *CareerTravel) GetScoped(scope replay.ProfileScope) (TravelMetrics, error) {
	return careerTravel.cache.GetOrLoad(scope.CacheKeySuffix(), func() (TravelMetrics, error) {
		return careerTravel.load(scope)
	})
}

// IsValid reports whether CareerTravel currently holds a warm cached value.
func (careerTravel *CareerTravel) IsValid() bool {
	return careerTravel.cache.Len() > 0
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerTravel *CareerTravel) Generation() uint64 {
	return careerTravel.cache.Generation()
}

func (careerTravel *CareerTravel) load(scope replay.ProfileScope) (TravelMetrics, error) {
	if careerTravel.database == nil {
		return TravelMetrics{}, nil
	}
	engine := replaystats.NewEngineWithSources(careerTravel.database, nil, careerTravel.recoveredJumps, nil)
	engine.Scope = scope
	return engine.ComputeTravel(Range{}, nil)
}
