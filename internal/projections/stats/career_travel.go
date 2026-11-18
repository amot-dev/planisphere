package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
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

// CareerTravel caches lifetime unfiltered Flight Operations metrics.
type CareerTravel struct {
	database       *sql.DB
	recoveredJumps *projections.RecoveredJumps
	cache          *projection.Cache[TravelMetrics]
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
	careerTravel.cache = projection.NewCache[TravelMetrics](
		bus,
		"CareerTravel",
		triggers,
		careerTravel.load,
	)
	return careerTravel
}

// Triggers reports the journal and app events that invalidate CareerTravel.
func (careerTravel *CareerTravel) Triggers() projection.Triggers {
	return careerTravel.cache.Triggers()
}

// Get returns the cached all-time travel metrics.
func (careerTravel *CareerTravel) Get() (TravelMetrics, error) {
	return careerTravel.cache.Get()
}

// IsValid reports whether CareerTravel currently holds a warm cached value.
func (careerTravel *CareerTravel) IsValid() bool {
	return careerTravel.cache.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerTravel *CareerTravel) Generation() uint64 {
	return careerTravel.cache.Generation()
}

func (careerTravel *CareerTravel) load() (TravelMetrics, error) {
	if careerTravel.database == nil {
		return TravelMetrics{}, nil
	}
	engine := replaystats.NewEngineWithSources(careerTravel.database, nil, careerTravel.recoveredJumps, nil)
	return engine.ComputeTravel(Range{}, nil)
}
