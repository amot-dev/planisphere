package stats

import (
	"database/sql"

	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
)

// TelemetrySummary pairs computed navigation/discovery metrics with their
// realized payout and deduction breakdown.
type TelemetrySummary struct {
	Metrics     Metrics
	Realization RealizationBreakdown
}

// CareerStats coordinates the five all-time unfiltered career statistics
// projections (Travel, CarrierOps, Cartographics, Exobiology, and Realization)
// so each sidebar section invalidates and recomputes only when its own journal
// or application triggers fire.
type CareerStats struct {
	travel        *CareerTravel
	carrierOps    *CareerCarrierOps
	cartographics *CareerCartographics
	exobiology    *CareerExobiology
	realization   *CareerRealization
}

// NewCareerStats creates a CareerStats facade and registers its five section
// projections on bus.
func NewCareerStats(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
) *CareerStats {
	return &CareerStats{
		travel:        NewCareerTravel(dbConn, bus, recoveredJumps),
		carrierOps:    NewCareerCarrierOps(dbConn, bus),
		cartographics: NewCareerCartographics(dbConn, bus),
		exobiology:    NewCareerExobiology(dbConn, bus, salesLedger),
		realization:   NewCareerRealization(dbConn, bus, salesLedger),
	}
}

// Travel returns the all-time Flight Operations travel projection.
func (careerStats *CareerStats) Travel() *CareerTravel {
	return careerStats.travel
}

// CarrierOps returns the all-time Personal Carrier Operations projection.
func (careerStats *CareerStats) CarrierOps() *CareerCarrierOps {
	return careerStats.carrierOps
}

// Cartographics returns the all-time Universal Cartographics valuation projection.
func (careerStats *CareerStats) Cartographics() *CareerCartographics {
	return careerStats.cartographics
}

// Exobiology returns the all-time Vista Genomics exobiology valuation projection.
func (careerStats *CareerStats) Exobiology() *CareerExobiology {
	return careerStats.exobiology
}

// Realization returns the all-time Realized Value breakdown projection.
func (careerStats *CareerStats) Realization() *CareerRealization {
	return careerStats.realization
}

// Triggers returns the union of journal and application events across all five
// career statistics projections.
func (careerStats *CareerStats) Triggers() projection.Triggers {
	return projection.TriggersFrom(
		careerStats.travel,
		careerStats.carrierOps,
		careerStats.cartographics,
		careerStats.exobiology,
		careerStats.realization,
	)
}

// Get assembles the lifetime telemetry metrics and realization breakdown across
// all five career section projections, recomputing only sections that were invalidated.
func (careerStats *CareerStats) Get() (TelemetrySummary, error) {
	travelMetrics, travelError := careerStats.travel.Get()
	if travelError != nil {
		return TelemetrySummary{}, travelError
	}
	carrierMetrics, carrierError := careerStats.carrierOps.Get()
	if carrierError != nil {
		return TelemetrySummary{}, carrierError
	}
	cartoMetrics, cartoError := careerStats.cartographics.Get()
	if cartoError != nil {
		return TelemetrySummary{}, cartoError
	}
	exoMetrics, exoError := careerStats.exobiology.Get()
	if exoError != nil {
		return TelemetrySummary{}, exoError
	}
	realizationBreakdown, realizationError := careerStats.realization.Get()
	if realizationError != nil {
		return TelemetrySummary{}, realizationError
	}

	var combinedMetrics Metrics
	travelMetrics.applyTo(&combinedMetrics)
	carrierMetrics.applyTo(&combinedMetrics)
	cartoMetrics.applyTo(&combinedMetrics)
	exoMetrics.applyTo(&combinedMetrics)

	return TelemetrySummary{
		Metrics:     combinedMetrics,
		Realization: realizationBreakdown,
	}, nil
}

// IsValid reports whether all five career statistics section projections are currently cached.
func (careerStats *CareerStats) IsValid() bool {
	return careerStats.travel.IsValid() &&
		careerStats.carrierOps.IsValid() &&
		careerStats.cartographics.IsValid() &&
		careerStats.exobiology.IsValid() &&
		careerStats.realization.IsValid()
}

// Generation returns the highest cache invalidation generation counter across
// the career statistics section projections.
func (careerStats *CareerStats) Generation() uint64 {
	return max(
		careerStats.travel.Generation(),
		careerStats.carrierOps.Generation(),
		careerStats.cartographics.Generation(),
		careerStats.exobiology.Generation(),
		careerStats.realization.Generation(),
	)
}
