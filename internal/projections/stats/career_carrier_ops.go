package stats

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/sources/journal"
)

var (
	carrierOpsJournalEvents = journal.NewEventSet(
		journal.EventStatistics,
	)
	carrierOpsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CarrierOpsMetrics holds lifetime Personal Carrier Operations jump and distance totals.
type CarrierOpsMetrics struct {
	CarrierJumps      int     `json:"carrierJumps"`
	CarrierDistanceLy float64 `json:"carrierDistanceLy"`
}

func (carrierOps CarrierOpsMetrics) applyTo(metrics *Metrics) {
	metrics.CarrierJumps = carrierOps.CarrierJumps
	metrics.CarrierDistanceLy = carrierOps.CarrierDistanceLy
}

// CareerCarrierOps caches all-time Personal Carrier Operations totals from
// Frontier Statistics events so game-launch Statistics events do not invalidate
// travel, cartographics, exobiology, or realization caches.
type CareerCarrierOps struct {
	database *sql.DB
	cache    *projection.Cache[CarrierOpsMetrics]
}

// NewCareerCarrierOps creates a CareerCarrierOps projection and registers it on bus.
func NewCareerCarrierOps(dbConn *sql.DB, bus *projection.Bus) *CareerCarrierOps {
	carrierOps := &CareerCarrierOps{database: dbConn}
	carrierOps.cache = projection.NewCache(
		bus,
		"career_carrier_ops",
		carrierOps.Triggers(),
		carrierOps.load,
	)
	return carrierOps
}

// Triggers declares the journal and application events that invalidate CareerCarrierOps.
func (carrierOps *CareerCarrierOps) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: carrierOpsJournalEvents,
		App:     carrierOpsAppEvents,
	}
}

// Get returns the cached all-time personal carrier jump and distance totals.
func (carrierOps *CareerCarrierOps) Get() (CarrierOpsMetrics, error) {
	return carrierOps.cache.Get()
}

// IsValid reports whether all-time personal carrier totals are currently cached.
func (carrierOps *CareerCarrierOps) IsValid() bool {
	return carrierOps.cache.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (carrierOps *CareerCarrierOps) Generation() uint64 {
	return carrierOps.cache.Generation()
}

func (carrierOps *CareerCarrierOps) load() (CarrierOpsMetrics, error) {
	if carrierOps.database == nil {
		return CarrierOpsMetrics{}, fmt.Errorf("career carrier ops database is nil")
	}
	var rawStatistics string
	queryError := carrierOps.database.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'Statistics' AND json_extract(raw_json, '$.FLEETCARRIER') IS NOT NULL AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawStatistics)
	if errors.Is(queryError, sql.ErrNoRows) {
		return CarrierOpsMetrics{}, nil
	}
	if queryError != nil {
		return CarrierOpsMetrics{}, fmt.Errorf("query career carrier statistics: %w", queryError)
	}
	if rawStatistics == "" {
		return CarrierOpsMetrics{}, nil
	}
	var statisticsEvent journal.StatisticsEvent
	if json.Unmarshal([]byte(rawStatistics), &statisticsEvent) != nil {
		return CarrierOpsMetrics{}, nil
	}
	return CarrierOpsMetrics{
		CarrierJumps:      int(statisticsEvent.FleetCarrierNumber("FLEETCARRIER_TOTAL_JUMPS")),
		CarrierDistanceLy: statisticsEvent.FleetCarrierNumber("FLEETCARRIER_DISTANCE_TRAVELLED"),
	}, nil
}

