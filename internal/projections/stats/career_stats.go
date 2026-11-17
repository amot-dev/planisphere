package stats

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/sources/journal"
)

var (
	careerStatsJournalEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventCarrierStats,
		journal.EventDocked,
		journal.EventUndocked,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
		journal.EventScan,
		journal.EventSAAScanComplete,
		journal.EventScanOrganic,
		journal.EventSellExplorationData,
		journal.EventMultiSellExplorationData,
		journal.EventSellOrganicData,
		journal.EventResurrect,
		journal.EventNpcCrewPaidWage,
		journal.EventStatistics,
	)
	careerStatsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// TelemetrySummary pairs computed navigation/discovery metrics with their
// realized payout and deduction breakdown.
type TelemetrySummary struct {
	Metrics     Metrics
	Realization RealizationBreakdown
}

// CareerStats caches all-time unfiltered career travel and exploration telemetry
// across all journals, overlaying lifetime Fleet Carrier jump and distance totals
// from Frontier Statistics events.
type CareerStats struct {
	database       *sql.DB
	salesLedger    *realization.SalesLedger
	recoveredJumps *projections.RecoveredJumps
	summary        *projection.Cache[TelemetrySummary]
}

// NewCareerStats creates a CareerStats projection and registers it on bus.
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
	}
	careerStats.summary = projection.NewCache(
		bus,
		"career_stats",
		careerStats.Triggers(),
		careerStats.loadSummary,
	)
	return careerStats
}

// Triggers declares the journal and application events that invalidate CareerStats.
func (careerStats *CareerStats) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: careerStatsJournalEvents,
		App:     careerStatsAppEvents,
	}
}

// Get returns the cached lifetime telemetry metrics and realization breakdown.
func (careerStats *CareerStats) Get() (TelemetrySummary, error) {
	return careerStats.summary.Get()
}

// IsValid reports whether the lifetime telemetry summary is currently cached.
func (careerStats *CareerStats) IsValid() bool {
	return careerStats.summary.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (careerStats *CareerStats) Generation() uint64 {
	return careerStats.summary.Generation()
}

func (careerStats *CareerStats) loadSummary() (TelemetrySummary, error) {
	if careerStats.database == nil {
		return TelemetrySummary{}, fmt.Errorf("career stats database is nil")
	}
	engine := NewEngineWithProjections(careerStats.database, careerStats.salesLedger, careerStats.recoveredJumps)
	allMetrics, computeError := engine.Compute(Range{}, nil)
	if computeError != nil {
		return TelemetrySummary{}, computeError
	}

	// All-time Personal Carrier Operations come strictly from Frontier Statistics
	// (0 when no owned carrier stats exist) so passenger CarrierJump rides never leak in.
	allMetrics.CarrierJumps = 0
	allMetrics.CarrierDistanceLy = 0
	allMetrics.TritiumUsed = nil

	var rawStatistics string
	queryError := careerStats.database.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'Statistics' AND json_extract(raw_json, '$.FLEETCARRIER') IS NOT NULL AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawStatistics)
	if queryError == nil && rawStatistics != "" {
		var statisticsEvent journal.StatisticsEvent
		if json.Unmarshal([]byte(rawStatistics), &statisticsEvent) == nil {
			allMetrics.CarrierJumps = int(statisticsEvent.FleetCarrierNumber("FLEETCARRIER_TOTAL_JUMPS"))
			allMetrics.CarrierDistanceLy = statisticsEvent.FleetCarrierNumber("FLEETCARRIER_DISTANCE_TRAVELLED")
		}
	}

	allRealization, realizationError := engine.ComputeRealization(Range{}, nil)
	if realizationError != nil {
		return TelemetrySummary{}, realizationError
	}
	return TelemetrySummary{
		Metrics:     allMetrics,
		Realization: allRealization,
	}, nil
}

