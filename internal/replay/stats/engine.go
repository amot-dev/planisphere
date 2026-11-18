package stats

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/replay"
	"planisphere/internal/replay/realization"
	"planisphere/internal/sources/journal"
)

// LedgerSource supplies the cached or lazily built realization ledger.
type LedgerSource interface {
	Get() (*realization.Ledger, error)
}

// RecoveredJumpsSource supplies crash-recovered hyperspace jumps within a time window.
type RecoveredJumpsSource interface {
	InRange(afterTimestamp, untilTimestamp string) ([]replay.RecoveredJump, error)
}

// Engine computes Metrics and RealizationBreakdown from persisted journal events.
type Engine struct {
	DB             db.Queryer
	Odyssey        bool
	ModeLoader     ModeTimelineLoader
	salesLedger    LedgerSource
	recoveredJumps RecoveredJumpsSource
}

// NewEngine creates an uncached stats computation engine with Odyssey mapping bonus enabled.
func NewEngine(queryer db.Queryer) *Engine {
	return &Engine{DB: queryer, Odyssey: true}
}

// NewEngineWithSources creates a stats engine backed by shared projection sources.
func NewEngineWithSources(
	queryer db.Queryer,
	salesLedger LedgerSource,
	recoveredJumps RecoveredJumpsSource,
	modeLoader ModeTimelineLoader,
) *Engine {
	return &Engine{
		DB:             queryer,
		Odyssey:        true,
		ModeLoader:     modeLoader,
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

func (engine *Engine) loadLedger() (*realization.Ledger, error) {
	if engine.salesLedger != nil {
		return engine.salesLedger.Get()
	}
	return realization.BuildLedger(engine.DB, engine.Odyssey)
}

func (engine *Engine) loadRecoveredJumps(afterTimestamp, untilTimestamp string) ([]replay.RecoveredJump, error) {
	if engine.recoveredJumps != nil {
		return engine.recoveredJumps.InRange(afterTimestamp, untilTimestamp)
	}
	return replay.ComputeRecoveredJumpsInRange(engine.DB, afterTimestamp, untilTimestamp)
}

// lookupScan finds the most recent Scan of a body at or before timestamp.
// Unary +event forces SQLite to seek idx_journal_events_body (system_address, body_id)
// rather than scanning backwards through idx_journal_events_event_ts.
func (engine *Engine) lookupScan(key bodyKey, timestamp string) (*journal.ScanEvent, error) {
	var rawJSON string
	queryError := engine.DB.QueryRow(`SELECT raw_json FROM journal_events WHERE +event = 'Scan' AND system_address = ? AND body_id = ?
		AND timestamp <= ? AND `+replay.CmdrFilterSQL+` ORDER BY timestamp DESC LIMIT 1`, key.systemAddress, key.bodyID, timestamp).Scan(&rawJSON)
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

// preloadChronologicalScans batch-loads all Scan events for multiple bodies in chronological
// order, chunked to avoid SQLite parameter limits and N+1 queries.
func (engine *Engine) preloadChronologicalScans(keys []bodyKey, untilTimestamp string) (map[bodyKey][]*journal.ScanEvent, error) {
	if len(keys) == 0 {
		return map[bodyKey][]*journal.ScanEvent{}, nil
	}
	scansByBody := make(map[bodyKey][]*journal.ScanEvent, len(keys))
	chunkSize := 200
	for startIndex := 0; startIndex < len(keys); startIndex += chunkSize {
		endIndex := min(startIndex+chunkSize, len(keys))
		if chunkError := engine.preloadChronologicalScanChunk(keys[startIndex:endIndex], untilTimestamp, scansByBody); chunkError != nil {
			return nil, chunkError
		}
	}
	return scansByBody, nil
}

func (engine *Engine) preloadScanChunk(chunk []bodyKey, untilTimestamp string, result map[bodyKey]*journal.ScanEvent) error {
	return engine.queryBodyScanChunk(chunk, untilTimestamp, func(conditionsSQL string) string {
		return `SELECT system_address, body_id, raw_json FROM (
			SELECT system_address, body_id, raw_json,
			       ROW_NUMBER() OVER (PARTITION BY system_address, body_id ORDER BY timestamp DESC) as rn
			FROM journal_events
			WHERE +event = 'Scan' AND timestamp <= ? AND ` + replay.CmdrFilterSQL + `
			  AND (` + conditionsSQL + `)
		) WHERE rn = 1`
	}, func(key bodyKey, scanEvent *journal.ScanEvent) {
		result[key] = scanEvent
	})
}

func (engine *Engine) preloadChronologicalScanChunk(chunk []bodyKey, untilTimestamp string, scansByBody map[bodyKey][]*journal.ScanEvent) error {
	return engine.queryBodyScanChunk(chunk, untilTimestamp, func(conditionsSQL string) string {
		return `SELECT system_address, body_id, raw_json
			FROM journal_events
			WHERE +event = 'Scan' AND timestamp <= ? AND ` + replay.CmdrFilterSQL + `
			  AND (` + conditionsSQL + `)
			ORDER BY timestamp ASC, id ASC`
	}, func(key bodyKey, scanEvent *journal.ScanEvent) {
		scansByBody[key] = append(scansByBody[key], scanEvent)
	})
}

func (engine *Engine) queryBodyScanChunk(
	chunk []bodyKey,
	untilTimestamp string,
	buildQuery func(conditionsSQL string) string,
	visitScan func(bodyKey, *journal.ScanEvent),
) error {
	conditions := make([]string, 0, len(chunk))
	args := make([]any, 0, len(chunk)*2+1)
	args = append(args, untilTimestamp)
	for _, key := range chunk {
		conditions = append(conditions, "(system_address = ? AND body_id = ?)")
		args = append(args, key.systemAddress, key.bodyID)
	}
	databaseRows, queryError := engine.DB.Query(buildQuery(strings.Join(conditions, " OR ")), args...)
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
			visitScan(bodyKey{systemAddress, bodyID}, &scanEvent)
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
	filterSet, prepareError := filter.prepare(engine.DB, engine.ModeLoader)
	if prepareError != nil {
		return metrics, fmt.Errorf("prepare stats filter: %w", prepareError)
	}
	loadedEvents, loadError := replay.LoadEvents(engine.DB, after, until, computeMetricEvents)
	if loadError != nil {
		return metrics, fmt.Errorf("load stats events: %w", loadError)
	}

	session := replay.NewSessionState(engine.Odyssey, len(loadedEvents)/8)
	if seedError := replay.SeedLoadGameBefore(engine.DB, after, session); seedError != nil {
		return metrics, fmt.Errorf("seed initial loadgame state: %w", seedError)
	}
	if preloadPopError := replay.PreloadSystemPopulations(engine.DB, after, loadedEvents, until, session); preloadPopError != nil {
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
		filterSet, prepareError := filter.prepare(engine.DB, engine.ModeLoader)
		if prepareError != nil {
			return RealizationBreakdown{}, prepareError
		}
		allowDiscovery = func(timestamp string) bool {
			return filterSet.allow(timestamp, true)
		}
	}
	return ledger.Breakdown(after, until, allowDiscovery), nil
}

