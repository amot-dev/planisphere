package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
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

// TravelMetrics holds Flight Operations navigation and jump metrics.
type TravelMetrics struct {
	DistanceLy         float64 `json:"distanceLy"`
	ShipDistanceLy     float64 `json:"shipDistanceLy"`
	CarrierPassengerLy float64 `json:"carrierPassengerLy"`
	Jumps              int     `json:"jumps"`
	JetConeBoosts      int     `json:"jetConeBoosts"`
	PremiumBoosts      int     `json:"premiumBoosts"`
	StandardBoosts     int     `json:"standardBoosts"`
	BasicBoosts        int     `json:"basicBoosts"`
}

func extractTravelMetrics(metrics Metrics) TravelMetrics {
	return TravelMetrics{
		DistanceLy:         metrics.DistanceLy,
		ShipDistanceLy:     metrics.ShipDistanceLy,
		CarrierPassengerLy: metrics.CarrierPassengerLy,
		Jumps:              metrics.Jumps,
		JetConeBoosts:      metrics.JetConeBoosts,
		PremiumBoosts:      metrics.PremiumBoosts,
		StandardBoosts:     metrics.StandardBoosts,
		BasicBoosts:        metrics.BasicBoosts,
	}
}

func (travel TravelMetrics) applyTo(metrics *Metrics) {
	metrics.DistanceLy = travel.DistanceLy
	metrics.ShipDistanceLy = travel.ShipDistanceLy
	metrics.CarrierPassengerLy = travel.CarrierPassengerLy
	metrics.Jumps = travel.Jumps
	metrics.JetConeBoosts = travel.JetConeBoosts
	metrics.PremiumBoosts = travel.PremiumBoosts
	metrics.StandardBoosts = travel.StandardBoosts
	metrics.BasicBoosts = travel.BasicBoosts
}

// CareerTravel caches all-time unfiltered Flight Operations navigation metrics.
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
	careerTravel.cache = projection.NewCache(
		bus,
		"career_travel",
		careerTravel.Triggers(),
		careerTravel.load,
	)
	return careerTravel
}

// Triggers declares the journal and application events that invalidate CareerTravel.
func (careerTravel *CareerTravel) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: travelStatsJournalEvents,
		App:     careerTravelAppEvents,
	}
}

// Get returns the cached all-time travel metrics.
func (careerTravel *CareerTravel) Get() (TravelMetrics, error) {
	return careerTravel.cache.Get()
}

// IsValid reports whether all-time travel metrics are currently cached.
func (careerTravel *CareerTravel) IsValid() bool {
	return careerTravel.cache.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (careerTravel *CareerTravel) Generation() uint64 {
	return careerTravel.cache.Generation()
}

func (careerTravel *CareerTravel) load() (TravelMetrics, error) {
	if careerTravel.database == nil {
		return TravelMetrics{}, fmt.Errorf("career travel database is nil")
	}
	engine := NewEngineWithProjections(careerTravel.database, nil, careerTravel.recoveredJumps)
	return engine.ComputeTravel(Range{}, nil)
}

// ComputeTravel returns Flight Operations navigation metrics for events in timeRange,
// loading only arrival events (Location, FSDJump, CarrierJump) and recovered jumps.
func (engine *Engine) ComputeTravel(timeRange Range, filter *Filter) (TravelMetrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB)
	if prepareError != nil {
		return TravelMetrics{}, fmt.Errorf("prepare travel stats filter: %w", prepareError)
	}
	loadedEvents, loadError := journal.LoadEvents(engine.DB, after, until, travelStatsJournalEvents)
	if loadError != nil {
		return TravelMetrics{}, fmt.Errorf("load travel stats events: %w", loadError)
	}
	accumulator := &computeAccumulator{
		engine:    engine,
		after:     after,
		until:     until,
		filterSet: filterSet,
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processEvent(rowItem, &metrics); stepError != nil {
			return TravelMetrics{}, stepError
		}
	}
	recoveredJumps, recoverError := engine.loadRecoveredJumps(after, until)
	if recoverError != nil {
		return TravelMetrics{}, fmt.Errorf("recover disconnected jumps: %w", recoverError)
	}
	for _, recoveredJump := range recoveredJumps {
		creditJump(recoveredJump.Timestamp, recoveredJump.JumpDist, recoveredJump.BoostUsed, filterSet, &metrics)
	}
	return extractTravelMetrics(metrics), nil
}

