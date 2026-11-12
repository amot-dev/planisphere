// Package stats computes expedition metrics from persisted journal events.
// Everything is derived from journal_events.raw_json, so metrics can be
// recomputed at any time (with updated formulas) even after the original
// journal files are gone.
package stats

import (
	"database/sql"
	"encoding/json"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
)

// Metrics are the tracked expedition statistics.
type Metrics struct {
	DistanceLy         float64 `json:"distanceLy"`
	ShipDistanceLy     float64 `json:"shipDistanceLy"`     // subset: hyperspace FSD jumps
	CarrierPassengerLy float64 `json:"carrierPassengerLy"` // subset: travelled while aboard a carrier
	Jumps              int     `json:"jumps"`
	CarrierJumps       int     `json:"carrierJumps"`
	CarrierDistanceLy  float64 `json:"carrierDistanceLy"`
	TritiumUsed        *int    `json:"tritiumUsed"` // nil when N/A (not recorded/estimated)
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

func (metrics *Metrics) addScan(scanVal ed.ScanValue) {
	metrics.ScanValueTotal += scanVal.Total
	metrics.ScanValueBase += scanVal.Base
	metrics.ScanValueBonus += scanVal.Bonus
}

func (metrics *Metrics) addOrganic(organicVal ed.ScanValue) {
	metrics.OrganicValueTotal += organicVal.Total
	metrics.OrganicValueBase += organicVal.Base
	metrics.OrganicValueBonus += organicVal.Bonus
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

// Engine computes Metrics from the database.
type Engine struct {
	DB      db.Queryer
	Odyssey bool
}

// NewEngine creates an engine (Odyssey mapping bonus enabled).
func NewEngine(queryer db.Queryer) *Engine { return &Engine{DB: queryer, Odyssey: true} }

type eventRow struct {
	eventUID      string
	timestamp     string
	eventType     string
	systemAddress sql.NullInt64
	bodyID        sql.NullInt64
	starPosition  *ed.Vec3
	rawJSON       string
}

var metricEvents = []string{"LoadGame", "Location", "FSDJump", "CarrierJump", "Scan", "SAAScanComplete", "ScanOrganic"}

func (engine *Engine) hasDisabledCommanders() bool {
	var hasDisabled int
	if queryError := engine.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM commanders WHERE enabled = 0)`).Scan(&hasDisabled); queryError != nil {
		return true
	}
	return hasDisabled != 0
}

func (engine *Engine) loadEvents(afterTimestamp, untilTimestamp string, eventNames []string) ([]eventRow, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(eventNames)), ",")
	args := make([]any, 0, len(eventNames)+2)
	for _, name := range eventNames {
		args = append(args, name)
	}
	args = append(args, afterTimestamp, untilTimestamp)

	// When loading a broad event set from the beginning of time, unary +event
	// prevents SQLite from union-scanning idx_journal_events_event_ts and sorting
	// ~180k rows in a temporary B-tree; instead it streams idx_journal_events_ts
	// in (timestamp, id) order.
	eventColumn := "event"
	if afterTimestamp == "" && len(eventNames) >= 6 {
		eventColumn = "+event"
	}
	commanderPredicate := ""
	if engine.hasDisabledCommanders() {
		commanderPredicate = " AND " + journal.CmdrFilterSQL
	}
	query := `SELECT event_uid, timestamp, event, system_address, body_id, star_x, star_y, star_z, raw_json
		FROM journal_events WHERE ` + eventColumn + ` IN (` + placeholders + `) AND timestamp > ? AND timestamp <= ?` + commanderPredicate + `
		ORDER BY timestamp, id`

	rows, queryError := engine.DB.Query(query, args...)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var eventItem eventRow
		var starX, starY, starZ sql.NullFloat64
		if scanError := rows.Scan(&eventItem.eventUID, &eventItem.timestamp, &eventItem.eventType, &eventItem.systemAddress, &eventItem.bodyID, &starX, &starY, &starZ, &eventItem.rawJSON); scanError != nil {
			return nil, scanError
		}
		if starX.Valid && starY.Valid && starZ.Valid {
			eventItem.starPosition = &ed.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}
		}
		out = append(out, eventItem)
	}
	return out, rows.Err()
}

type bodyKey struct{ systemAddress, bodyID int64 }

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
		return nil, queryError
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
		chunk := keys[startIndex:endIndex]
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

		rows, queryError := engine.DB.Query(query, args...)
		if queryError != nil {
			return nil, queryError
		}
		for rows.Next() {
			var systemAddress, bodyID int64
			var rawJSON string
			if scanError := rows.Scan(&systemAddress, &bodyID, &rawJSON); scanError != nil {
				rows.Close()
				return nil, scanError
			}
			var scanEvent journal.ScanEvent
			if json.Unmarshal([]byte(rawJSON), &scanEvent) == nil {
				result[bodyKey{systemAddress, bodyID}] = &scanEvent
			}
		}
		if iterationError := rows.Err(); iterationError != nil {
			rows.Close()
			return nil, iterationError
		}
		rows.Close()
	}
	return result, nil
}

// Compute returns metrics for events in timeRange, optionally filtered by mode.
func (engine *Engine) Compute(timeRange Range, filter *Filter) (Metrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB)
	if prepareError != nil {
		return metrics, prepareError
	}
	events, loadError := engine.loadEvents(after, until, metricEvents)
	if loadError != nil {
		return metrics, loadError
	}

	session := newSessionState(engine.Odyssey, len(events)/8)
	if seedError := engine.seedLoadGameBefore(after, session); seedError != nil {
		return metrics, seedError
	}
	if preloadPopError := engine.preloadSystemPopulations(after, events, until, session); preloadPopError != nil {
		return metrics, preloadPopError
	}

	scans, missingScans, preloadScanError := engine.prepareComputeScans(after, events)
	if preloadScanError != nil {
		return metrics, preloadScanError
	}

	accumulator := &computeAccumulator{
		engine:        engine,
		after:         after,
		until:         until,
		filterSet:     filterSet,
		session:       session,
		scans:         scans,
		missingScans:  missingScans,
		scannedBodies: make(map[bodyKey]bool, len(events)/2),
		mappedBodies:  make(map[bodyKey]bool, len(events)/8),
	}
	for _, rowItem := range events {
		if stepError := accumulator.processEvent(rowItem, &metrics); stepError != nil {
			return metrics, stepError
		}
	}
	return metrics, nil
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
