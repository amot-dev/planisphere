// Package stats computes expedition and lifetime metrics from persisted journal
// events. Everything is derived from journal_events.raw_json, so metrics can be
// recomputed at any time (with updated formulas) even after original journal
// files are gone.
package stats

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/modes"
)

// RealizationBreakdown re-exports realization.Breakdown for existing callers
// and Wails TypeScript bindings.
type RealizationBreakdown = realization.Breakdown

// Metrics are the tracked expedition statistics.
type Metrics struct {
	DistanceLy         float64 `json:"distanceLy"`
	ShipDistanceLy     float64 `json:"shipDistanceLy"`     // subset: hyperspace FSD jumps
	CarrierPassengerLy float64 `json:"carrierPassengerLy"` // subset: travelled while aboard a carrier
	Jumps              int     `json:"jumps"`
	CarrierJumps       int     `json:"carrierJumps"`
	CarrierDistanceLy  float64 `json:"carrierDistanceLy"`
	TritiumUsed        *int    `json:"tritiumUsed"` // nil when N/A (not recorded or estimated)
	JetConeBoosts      int     `json:"jetConeBoosts"`
	PremiumBoosts      int     `json:"premiumBoosts"`
	StandardBoosts     int     `json:"standardBoosts"`
	BasicBoosts        int     `json:"basicBoosts"`
	BodiesScanned      int     `json:"bodiesScanned"`
	BodiesMapped       int     `json:"bodiesMapped"`
	ScanValueTotal     int64   `json:"scanValueTotal"`
	ScanValueBase      int64   `json:"scanValueBase"`
	ScanValueBonus     int64   `json:"scanValueBonus"`
	FirstDiscoveries   int     `json:"firstDiscoveries"`
	FirstMapped        int     `json:"firstMapped"`
	OrganicSamples     int     `json:"organicSamples"`
	OrganicValueTotal  int64   `json:"organicValueTotal"`
	OrganicValueBase   int64   `json:"organicValueBase"`
	OrganicValueBonus  int64   `json:"organicValueBonus"`
	FirstLogged        int     `json:"firstLogged"`
}

// Add returns the field-wise sum of metrics and otherMetrics.
func (metrics Metrics) Add(otherMetrics Metrics) Metrics {
	var tritiumTotal *int
	if metrics.TritiumUsed != nil || otherMetrics.TritiumUsed != nil {
		tritiumSum := 0
		if metrics.TritiumUsed != nil {
			tritiumSum += *metrics.TritiumUsed
		}
		if otherMetrics.TritiumUsed != nil {
			tritiumSum += *otherMetrics.TritiumUsed
		}
		tritiumTotal = &tritiumSum
	}
	return Metrics{
		DistanceLy:         metrics.DistanceLy + otherMetrics.DistanceLy,
		ShipDistanceLy:     metrics.ShipDistanceLy + otherMetrics.ShipDistanceLy,
		CarrierPassengerLy: metrics.CarrierPassengerLy + otherMetrics.CarrierPassengerLy,
		Jumps:              metrics.Jumps + otherMetrics.Jumps,
		CarrierJumps:       metrics.CarrierJumps + otherMetrics.CarrierJumps,
		CarrierDistanceLy:  metrics.CarrierDistanceLy + otherMetrics.CarrierDistanceLy,
		TritiumUsed:        tritiumTotal,
		JetConeBoosts:      metrics.JetConeBoosts + otherMetrics.JetConeBoosts,
		PremiumBoosts:      metrics.PremiumBoosts + otherMetrics.PremiumBoosts,
		StandardBoosts:     metrics.StandardBoosts + otherMetrics.StandardBoosts,
		BasicBoosts:        metrics.BasicBoosts + otherMetrics.BasicBoosts,
		BodiesScanned:      metrics.BodiesScanned + otherMetrics.BodiesScanned,
		BodiesMapped:       metrics.BodiesMapped + otherMetrics.BodiesMapped,
		ScanValueTotal:     metrics.ScanValueTotal + otherMetrics.ScanValueTotal,
		ScanValueBase:      metrics.ScanValueBase + otherMetrics.ScanValueBase,
		ScanValueBonus:     metrics.ScanValueBonus + otherMetrics.ScanValueBonus,
		FirstDiscoveries:   metrics.FirstDiscoveries + otherMetrics.FirstDiscoveries,
		FirstMapped:        metrics.FirstMapped + otherMetrics.FirstMapped,
		OrganicSamples:     metrics.OrganicSamples + otherMetrics.OrganicSamples,
		OrganicValueTotal:  metrics.OrganicValueTotal + otherMetrics.OrganicValueTotal,
		OrganicValueBase:   metrics.OrganicValueBase + otherMetrics.OrganicValueBase,
		OrganicValueBonus:  metrics.OrganicValueBonus + otherMetrics.OrganicValueBonus,
		FirstLogged:        metrics.FirstLogged + otherMetrics.FirstLogged,
	}
}

func (metrics *Metrics) addScan(scanValuation reference.ScanValue) {
	metrics.ScanValueTotal += scanValuation.Total
	metrics.ScanValueBase += scanValuation.Base
	metrics.ScanValueBonus += scanValuation.Bonus
}

func (metrics *Metrics) addOrganic(organicValuation reference.ScanValue) {
	metrics.OrganicValueTotal += organicValuation.Total
	metrics.OrganicValueBase += organicValuation.Base
	metrics.OrganicValueBonus += organicValuation.Bonus
}

// Range is the half-open time range (After, Until]. Empty After means "from
// the beginning of time"; empty Until means "up to now".
type Range struct {
	After string `json:"after"`
	Until string `json:"until"`
}

func (timeRange Range) bounds() (string, string) {
	until := timeRange.Until
	if until == "" {
		until = "9999-12-31T23:59:59Z"
	}
	return timeRange.After, until
}

// ModeSelection controls which modes contribute to which metric family.
type ModeSelection struct {
	ModeID    int64 `json:"modeId"`    // modes.UnassignedID (0) = time not covered by a window
	Travel    bool  `json:"travel"`    // jumps, distance, boosts, carrier passenger travel (carrier operations ignore mode)
	Discovery bool  `json:"discovery"` // scans, mapping, values, exobiology
}

// Filter restricts metrics to events that occurred while selected modes were
// active. A nil Filter (or Enabled == false) includes everything.
type Filter struct {
	Enabled      bool            `json:"enabled"`
	Modes        []ModeSelection `json:"modes"`
	preparedSets *filterSets
	preparedDB   db.Queryer
}

type filterSets struct {
	travel, discovery map[int64]bool
	timeline          *modes.Timeline
}

func (filter *Filter) prepare(queryer db.Queryer) (*filterSets, error) {
	if filter == nil || !filter.Enabled {
		return nil, nil
	}
	if filter.preparedSets != nil && filter.preparedDB == queryer {
		return filter.preparedSets, nil
	}
	timeline, loadError := modes.LoadTimeline(queryer)
	if loadError != nil {
		return nil, loadError
	}
	sets := &filterSets{travel: map[int64]bool{}, discovery: map[int64]bool{}, timeline: timeline}
	for _, modeSelection := range filter.Modes {
		if modeSelection.Travel {
			sets.travel[modeSelection.ModeID] = true
		}
		if modeSelection.Discovery {
			sets.discovery[modeSelection.ModeID] = true
		}
	}
	filter.preparedSets = sets
	filter.preparedDB = queryer
	return sets, nil
}

func (sets *filterSets) allow(timestamp string, discovery bool) bool {
	if sets == nil {
		return true
	}
	mode := sets.timeline.ModeAt(timestamp)
	if discovery {
		return sets.discovery[mode]
	}
	return sets.travel[mode]
}

// Engine computes Metrics and RealizationBreakdown from persisted journal events.
type Engine struct {
	DB             db.Queryer
	Odyssey        bool
	salesLedger    *realization.SalesLedger
	recoveredJumps *projections.RecoveredJumps
}

// NewEngine creates an uncached stats computation engine with Odyssey mapping bonus enabled.
func NewEngine(queryer db.Queryer) *Engine {
	return &Engine{DB: queryer, Odyssey: true}
}

// NewEngineWithProjections creates a stats engine backed by shared projection caches.
func NewEngineWithProjections(
	queryer db.Queryer,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
) *Engine {
	return &Engine{
		DB:             queryer,
		Odyssey:        true,
		salesLedger:    salesLedger,
		recoveredJumps: recoveredJumps,
	}
}

var computeMetricEvents = journal.NewEventSet(
	journal.EventLoadGame,
	journal.EventLocation,
	journal.EventFSDJump,
	journal.EventCarrierJump,
	journal.EventScan,
	journal.EventSAAScanComplete,
	journal.EventScanOrganic,
)

type bodyKey struct{ systemAddress, bodyID int64 }

// CarrierLegs simulates carrier history up to untilTimestamp using projections.CarrierLegs.
func (engine *Engine) CarrierLegs(untilTimestamp string) (map[string]projections.CarrierLeg, error) {
	return projections.CarrierLegs(engine.DB, untilTimestamp)
}

// CarrierLegsRange simulates carrier jumps in (afterTimestamp, untilTimestamp].
func (engine *Engine) CarrierLegsRange(afterTimestamp, untilTimestamp string) (map[string]projections.CarrierLeg, error) {
	return projections.CarrierLegsRange(engine.DB, afterTimestamp, untilTimestamp)
}

func (engine *Engine) loadLedger() (*realization.Ledger, error) {
	if engine.salesLedger != nil {
		return engine.salesLedger.Get()
	}
	return realization.BuildLedger(engine.DB, engine.Odyssey)
}

func (engine *Engine) loadRecoveredJumps(afterTimestamp, untilTimestamp string) ([]projections.RecoveredJump, error) {
	if engine.recoveredJumps != nil {
		return engine.recoveredJumps.InRange(afterTimestamp, untilTimestamp)
	}
	return projections.ComputeRecoveredJumpsInRange(engine.DB, afterTimestamp, untilTimestamp)
}

// lookupScan finds the most recent Scan of a body at or before timestamp.
// Unary +event forces SQLite to seek idx_journal_events_body (system_address, body_id)
// rather than scanning backwards through idx_journal_events_event_ts.
func (engine *Engine) lookupScan(key bodyKey, timestamp string) (*journal.ScanEvent, error) {
	var rawJSON string
	queryError := engine.DB.QueryRow(`SELECT raw_json FROM journal_events WHERE +event = 'Scan' AND system_address = ? AND body_id = ?
		AND timestamp <= ? AND `+journal.CmdrFilterSQL+` ORDER BY timestamp DESC LIMIT 1`, key.systemAddress, key.bodyID, timestamp).Scan(&rawJSON)
	if queryError == sql.ErrNoRows {
		return nil, nil
	}
	if queryError != nil {
		return nil, fmt.Errorf("lookup scan for body (%d, %d): %w", key.systemAddress, key.bodyID, queryError)
	}
	var scanEvent journal.ScanEvent
	if unmarshalError := json.Unmarshal([]byte(rawJSON), &scanEvent); unmarshalError != nil {
		return nil, nil
	}
	return &scanEvent, nil
}

// preloadScans batch-loads the latest Scan event for multiple bodies in chunks to avoid N+1 queries.
func (engine *Engine) preloadScans(keys []bodyKey, untilTimestamp string) (map[bodyKey]*journal.ScanEvent, error) {
	if len(keys) == 0 {
		return map[bodyKey]*journal.ScanEvent{}, nil
	}
	result := make(map[bodyKey]*journal.ScanEvent, len(keys))
	chunkSize := 200
	for startIndex := 0; startIndex < len(keys); startIndex += chunkSize {
		endIndex := min(startIndex+chunkSize, len(keys))
		if chunkError := engine.preloadScanChunk(keys[startIndex:endIndex], untilTimestamp, result); chunkError != nil {
			return nil, chunkError
		}
	}
	return result, nil
}

func (engine *Engine) preloadScanChunk(chunk []bodyKey, untilTimestamp string, result map[bodyKey]*journal.ScanEvent) error {
	conditions := make([]string, 0, len(chunk))
	args := make([]any, 0, len(chunk)*2+1)
	args = append(args, untilTimestamp)
	for _, key := range chunk {
		conditions = append(conditions, "(system_address = ? AND body_id = ?)")
		args = append(args, key.systemAddress, key.bodyID)
	}
	query := `SELECT system_address, body_id, raw_json FROM (
		SELECT system_address, body_id, raw_json,
		       ROW_NUMBER() OVER (PARTITION BY system_address, body_id ORDER BY timestamp DESC) as rn
		FROM journal_events
		WHERE +event = 'Scan' AND timestamp <= ? AND ` + journal.CmdrFilterSQL + `
		  AND (` + strings.Join(conditions, " OR ") + `)
	) WHERE rn = 1`

	databaseRows, queryError := engine.DB.Query(query, args...)
	if queryError != nil {
		return fmt.Errorf("query scan chunk: %w", queryError)
	}
	defer databaseRows.Close()
	for databaseRows.Next() {
		var systemAddress, bodyID int64
		var rawJSON string
		if scanError := databaseRows.Scan(&systemAddress, &bodyID, &rawJSON); scanError != nil {
			return fmt.Errorf("scan preloaded body scan: %w", scanError)
		}
		var scanEvent journal.ScanEvent
		if json.Unmarshal([]byte(rawJSON), &scanEvent) == nil {
			result[bodyKey{systemAddress, bodyID}] = &scanEvent
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return fmt.Errorf("iterate scan chunk rows: %w", iterationError)
	}
	return nil
}

// Compute returns metrics for events in timeRange, optionally filtered by mode.
func (engine *Engine) Compute(timeRange Range, filter *Filter) (Metrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB)
	if prepareError != nil {
		return metrics, fmt.Errorf("prepare stats filter: %w", prepareError)
	}
	loadedEvents, loadError := journal.LoadEvents(engine.DB, after, until, computeMetricEvents)
	if loadError != nil {
		return metrics, fmt.Errorf("load stats events: %w", loadError)
	}

	session := projections.NewSessionState(engine.Odyssey, len(loadedEvents)/8)
	if seedError := projections.SeedLoadGameBefore(engine.DB, after, session); seedError != nil {
		return metrics, fmt.Errorf("seed initial loadgame state: %w", seedError)
	}
	if preloadPopError := projections.PreloadSystemPopulations(engine.DB, after, loadedEvents, until, session); preloadPopError != nil {
		return metrics, fmt.Errorf("preload system populations: %w", preloadPopError)
	}

	scans, missingScans, preloadScanError := engine.prepareComputeScans(after, loadedEvents)
	if preloadScanError != nil {
		return metrics, fmt.Errorf("prepare compute scans: %w", preloadScanError)
	}

	accumulator := &computeAccumulator{
		engine:        engine,
		after:         after,
		until:         until,
		filterSet:     filterSet,
		session:       session,
		scans:         scans,
		missingScans:  missingScans,
		scannedBodies: make(map[bodyKey]bool, len(loadedEvents)/2),
		mappedBodies:  make(map[bodyKey]bool, len(loadedEvents)/8),
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processEvent(rowItem, &metrics); stepError != nil {
			return metrics, stepError
		}
	}
	recoveredJumps, recoverError := engine.loadRecoveredJumps(after, until)
	if recoverError != nil {
		return metrics, fmt.Errorf("recover disconnected jumps: %w", recoverError)
	}
	for _, recoveredJump := range recoveredJumps {
		creditJump(recoveredJump.Timestamp, recoveredJump.JumpDist, recoveredJump.BoostUsed, filterSet, &metrics)
	}
	return metrics, nil
}

// ComputeRealization computes the realization breakdown for the given time range.
func (engine *Engine) ComputeRealization(timeRange Range, filter *Filter) (RealizationBreakdown, error) {
	ledger, buildError := engine.loadLedger()
	if buildError != nil {
		return RealizationBreakdown{}, buildError
	}

	after, until := timeRange.bounds()
	var allowDiscovery func(timestamp string) bool
	if filter != nil && filter.Enabled {
		filterSet, prepareError := filter.prepare(engine.DB)
		if prepareError != nil {
			return RealizationBreakdown{}, prepareError
		}
		allowDiscovery = func(timestamp string) bool {
			return filterSet.allow(timestamp, true)
		}
	}
	return ledger.Breakdown(after, until, allowDiscovery), nil
}

// MetricsFromStatistics maps a lifetime Statistics event onto Metrics for use
// as a "before expedition" baseline.
//
// Note: Frontier's Statistics event does not record DSS-mapped body count
// (Planets_Scanned_To_Level_2 and Planets_Scanned_To_Level_3 are pre-3.3
// Horizons DSS scan level counters and are identical in modern journals), so
// BodiesMapped is left 0 rather than duplicating BodiesScanned.
func MetricsFromStatistics(statsEvent journal.StatisticsEvent) Metrics {
	return Metrics{
		DistanceLy:        statsEvent.Exploration.TotalHyperspaceDist,
		ShipDistanceLy:    statsEvent.Exploration.TotalHyperspaceDist,
		Jumps:             int(statsEvent.Exploration.TotalHyperspaceJumps),
		BodiesScanned:     int(statsEvent.Exploration.PlanetsScannedToLevel3),
		BodiesMapped:      0,
		ScanValueTotal:    int64(statsEvent.Exploration.ExplorationProfits),
		ScanValueBase:     int64(statsEvent.Exploration.ExplorationProfits),
		OrganicSamples:    int(statsEvent.Exobiology.OrganicData),
		OrganicValueTotal: int64(statsEvent.Exobiology.OrganicDataProfits),
		OrganicValueBase:  int64(statsEvent.Exobiology.OrganicDataProfits - statsEvent.Exobiology.FirstLoggedProfits),
		OrganicValueBonus: int64(statsEvent.Exobiology.FirstLoggedProfits),
		FirstLogged:       int(statsEvent.Exobiology.FirstLogged),
		CarrierJumps:      int(statsEvent.FleetCarrierNumber("FLEETCARRIER_TOTAL_JUMPS")),
		CarrierDistanceLy: statsEvent.FleetCarrierNumber("FLEETCARRIER_DISTANCE_TRAVELLED"),
		TritiumUsed:       nil,
	}
}

