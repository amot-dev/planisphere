// Package stats maintains reactive in-memory telemetry metric caches over
// replayed Elite Dangerous journal statistics.
package stats

import (
	"database/sql"

	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
)

// Type aliases preserve Wails and downstream package imports cleanly while
// keeping replay calculations decoupled in internal/replay/stats.
type (
	RealizationBreakdown = replaystats.RealizationBreakdown
	Metrics              = replaystats.Metrics
	TelemetrySummary     = replaystats.TelemetrySummary
	Range                = replaystats.Range
	ModeSelection        = replaystats.ModeSelection
	ModeLookup           = replaystats.ModeLookup
	ModeLookupLoader     = replaystats.ModeLookupLoader
	Filter               = replaystats.Filter
	TravelMetrics        = replaystats.TravelMetrics
	CarrierOpsMetrics    = replaystats.CarrierOpsMetrics
	CartographicsMetrics = replaystats.CartographicsMetrics
	ExobiologyMetrics    = replaystats.ExobiologyMetrics
)

// CareerStats caches all-time unfiltered commander telemetry across specialized
// section projections and per-scope keyed summaries.
type CareerStats struct {
	database       *sql.DB
	salesLedger    *realization.SalesLedger
	recoveredJumps *projections.RecoveredJumps
	travel         *CareerTravel
	carrierOps     *CareerCarrierOps
	cartographics  *CareerCartographics
	exobiology     *CareerExobiology
	realization    *CareerRealization
	scoped         *projection.KeyedCache[string, TelemetrySummary]
}

// NewCareerStats creates and registers all career telemetry section projections on bus.
func NewCareerStats(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
) *CareerStats {
	careerStats := &CareerStats{
		database:       dbConn,
		salesLedger:    salesLedger,
		recoveredJumps: recoveredJumps,
		travel:         NewCareerTravel(dbConn, bus, recoveredJumps),
		carrierOps:     NewCareerCarrierOps(dbConn, bus),
		cartographics:  NewCareerCartographics(dbConn, bus),
		exobiology:     NewCareerExobiology(dbConn, bus, salesLedger),
		realization:    NewCareerRealization(dbConn, bus, salesLedger),
	}
	careerStats.scoped = projection.NewKeyedCache[string, TelemetrySummary](
		bus,
		"CareerStatsScoped",
		careerStats.Triggers(),
	)
	return careerStats
}

// Travel returns the all-time Flight Operations travel projection.
func (careerStats *CareerStats) Travel() *CareerTravel {
	return careerStats.travel
}

// CarrierOps returns the all-time Personal Carrier Operations projection.
func (careerStats *CareerStats) CarrierOps() *CareerCarrierOps {
	return careerStats.carrierOps
}

// Cartographics returns the all-time Universal Cartographics projection.
func (careerStats *CareerStats) Cartographics() *CareerCartographics {
	return careerStats.cartographics
}

// Exobiology returns the all-time Vista Genomics exobiology projection.
func (careerStats *CareerStats) Exobiology() *CareerExobiology {
	return careerStats.exobiology
}

// Realization returns the all-time Realized Value projection.
func (careerStats *CareerStats) Realization() *CareerRealization {
	return careerStats.realization
}

// Triggers returns the combined journal and app triggers across all career section projections.
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
// the independently cached career section projections for globally enabled profiles.
func (careerStats *CareerStats) Get() (TelemetrySummary, error) {
	travelMetrics, travelError := careerStats.travel.Get()
	if travelError != nil {
		return TelemetrySummary{}, travelError
	}
	carrierOpsMetrics, carrierOpsError := careerStats.carrierOps.Get()
	if carrierOpsError != nil {
		return TelemetrySummary{}, carrierOpsError
	}
	cartoMetrics, cartoError := careerStats.cartographics.Get()
	if cartoError != nil {
		return TelemetrySummary{}, cartoError
	}
	exobiologyMetrics, exobiologyError := careerStats.exobiology.Get()
	if exobiologyError != nil {
		return TelemetrySummary{}, exobiologyError
	}
	realizationBreakdown, realizationError := careerStats.realization.Get()
	if realizationError != nil {
		return TelemetrySummary{}, realizationError
	}

	var combinedMetrics Metrics
	travelMetrics.ApplyTo(&combinedMetrics)
	carrierOpsMetrics.ApplyTo(&combinedMetrics)
	cartoMetrics.ApplyTo(&combinedMetrics)
	exobiologyMetrics.ApplyTo(&combinedMetrics)
	return TelemetrySummary{
		Metrics:     combinedMetrics,
		Realization: realizationBreakdown,
	}, nil
}

// GetScoped returns all-time telemetry for scope, delegating to Get for
// AllEnabled and caching explicit profile scopes in a bus-invalidated KeyedCache.
func (careerStats *CareerStats) GetScoped(scope replay.ProfileScope) (TelemetrySummary, error) {
	if scope.IsAllEnabled() {
		return careerStats.Get()
	}
	if careerStats.database == nil {
		return TelemetrySummary{}, nil
	}
	return careerStats.scoped.GetOrLoad(scope.CacheKeySuffix(), func() (TelemetrySummary, error) {
		engine := replaystats.NewEngineWithSources(
			careerStats.database,
			careerStats.salesLedger,
			careerStats.recoveredJumps,
			nil,
		)
		engine.Scope = scope
		return engine.ComputeAllTime(nil)
	})
}

// IsValid reports whether all lifetime section caches currently hold warm values.
func (careerStats *CareerStats) IsValid() bool {
	return careerStats.travel.IsValid() &&
		careerStats.carrierOps.IsValid() &&
		careerStats.cartographics.IsValid() &&
		careerStats.exobiology.IsValid() &&
		careerStats.realization.IsValid()
}

// Generation returns the highest invalidation generation counter across all section caches.
func (careerStats *CareerStats) Generation() uint64 {
	return max(
		careerStats.travel.Generation(),
		careerStats.carrierOps.Generation(),
		careerStats.cartographics.Generation(),
		careerStats.exobiology.Generation(),
		careerStats.realization.Generation(),
		careerStats.scoped.Generation(),
	)
}
