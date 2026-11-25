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

type scopedRecoveredJumpsSource interface {
	InRangeScoped(afterTimestamp, untilTimestamp string, scope replay.ProfileScope) ([]replay.RecoveredJump, error)
}

// Engine computes Metrics and RealizationBreakdown from persisted journal events.
type Engine struct {
	DB             db.Queryer
	Odyssey        bool
	Scope          replay.ProfileScope
	ModeLoader     ModeLookupLoader
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
	modeLoader ModeLookupLoader,
) *Engine {
	return &Engine{
		DB:             queryer,
		Odyssey:        true,
		ModeLoader:     modeLoader,
		salesLedger:    salesLedger,
		recoveredJumps: recoveredJumps,
	}
}

type bodyKey struct{ systemAddress, bodyID int64 }

func effectiveScope(engineScope replay.ProfileScope, filter *Filter) replay.ProfileScope {
	if filter != nil && !filter.Scope.IsAllEnabled() {
		return filter.Scope
	}
	return engineScope
}

func (engine *Engine) loadLedger() (*realization.Ledger, error) {
	if engine.salesLedger != nil {
		return engine.salesLedger.Get()
	}
	return realization.BuildLedger(engine.DB, engine.Odyssey)
}

func (engine *Engine) loadRecoveredJumps(afterTimestamp, untilTimestamp string, scope replay.ProfileScope) ([]replay.RecoveredJump, error) {
	if engine.recoveredJumps != nil {
		if scopedSource, ok := engine.recoveredJumps.(scopedRecoveredJumpsSource); ok {
			return scopedSource.InRangeScoped(afterTimestamp, untilTimestamp, scope)
		}
		if scope.IsAllEnabled() {
			return engine.recoveredJumps.InRange(afterTimestamp, untilTimestamp)
		}
	}
	return replay.ComputeRecoveredJumpsInRangeScoped(engine.DB, afterTimestamp, untilTimestamp, scope)
}

// lookupScan finds the most recent Scan of a body at or before timestamp.
// Unary +event forces SQLite to seek idx_journal_events_body (system_address, body_id)
// rather than scanning backwards through idx_journal_events_event_ts.
func (engine *Engine) lookupScan(key bodyKey, timestamp string, scope replay.ProfileScope) (*journal.ScanEvent, error) {
	var rawJSON string
	queryError := engine.DB.QueryRow(`SELECT raw_json FROM journal_events WHERE +event = 'Scan' AND system_address = ? AND body_id = ?
		AND timestamp <= ? AND `+scope.SQLPredicate("")+` ORDER BY timestamp DESC LIMIT 1`, key.systemAddress, key.bodyID, timestamp).Scan(&rawJSON)
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
func (engine *Engine) preloadScans(keys []bodyKey, untilTimestamp string, scope replay.ProfileScope) (map[bodyKey]*journal.ScanEvent, error) {
	if len(keys) == 0 {
		return map[bodyKey]*journal.ScanEvent{}, nil
	}
	result, scopePredicate := make(map[bodyKey]*journal.ScanEvent, len(keys)), scope.SQLPredicate("")
	for startIndex := 0; startIndex < len(keys); startIndex += 200 {
		if err := engine.preloadScanChunk(keys[startIndex:min(startIndex+200, len(keys))], untilTimestamp, scopePredicate, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// preloadChronologicalScans batch-loads all Scan events for multiple bodies in chronological
// order, chunked to avoid SQLite parameter limits and N+1 queries.
func (engine *Engine) preloadChronologicalScans(keys []bodyKey, untilTimestamp string, scope replay.ProfileScope) (map[bodyKey][]*journal.ScanEvent, error) {
	if len(keys) == 0 {
		return map[bodyKey][]*journal.ScanEvent{}, nil
	}
	scansByBody, scopePredicate := make(map[bodyKey][]*journal.ScanEvent, len(keys)), scope.SQLPredicate("")
	for startIndex := 0; startIndex < len(keys); startIndex += 200 {
		if err := engine.preloadChronologicalScanChunk(keys[startIndex:min(startIndex+200, len(keys))], untilTimestamp, scopePredicate, scansByBody); err != nil {
			return nil, err
		}
	}
	return scansByBody, nil
}

func (engine *Engine) preloadScanChunk(chunk []bodyKey, untilTimestamp, scopePredicate string, result map[bodyKey]*journal.ScanEvent) error {
	return engine.queryBodyScanChunk(chunk, untilTimestamp, func(conditionsSQL string) string {
		return `SELECT system_address, body_id, raw_json FROM (
			SELECT system_address, body_id, raw_json,
			       ROW_NUMBER() OVER (PARTITION BY system_address, body_id ORDER BY timestamp DESC) as rn
			FROM journal_events
			WHERE +event = 'Scan' AND timestamp <= ? AND ` + scopePredicate + `
			  AND (` + conditionsSQL + `)
		) WHERE rn = 1`
	}, func(key bodyKey, scanEvent *journal.ScanEvent) {
		result[key] = scanEvent
	})
}

func (engine *Engine) preloadChronologicalScanChunk(chunk []bodyKey, untilTimestamp, scopePredicate string, scansByBody map[bodyKey][]*journal.ScanEvent) error {
	return engine.queryBodyScanChunk(chunk, untilTimestamp, func(conditionsSQL string) string {
		return `SELECT system_address, body_id, raw_json
			FROM journal_events
			WHERE +event = 'Scan' AND timestamp <= ? AND ` + scopePredicate + `
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
	return ledger.BreakdownForScope(after, until, effectiveScope(engine.Scope, filter), allowDiscovery), nil
}

// ComputeAllTime computes all-time navigation, carrier operations, discovery,
// and realization telemetry across the effective profile scope.
func (engine *Engine) ComputeAllTime(filter *Filter) (TelemetrySummary, error) {
	travel, err := engine.ComputeTravel(Range{}, filter)
	if err != nil {
		return TelemetrySummary{}, err
	}
	carrierOps, err := engine.ComputeCarrierOps(filter)
	if err != nil {
		return TelemetrySummary{}, err
	}
	carto, err := engine.ComputeCartographics(Range{}, filter)
	if err != nil {
		return TelemetrySummary{}, err
	}
	exo, err := engine.ComputeExobiology(Range{}, filter)
	if err != nil {
		return TelemetrySummary{}, err
	}
	realized, err := engine.ComputeRealization(Range{}, filter)
	if err != nil {
		return TelemetrySummary{}, err
	}
	var combined Metrics
	travel.ApplyTo(&combined)
	carrierOps.ApplyTo(&combined)
	carto.ApplyTo(&combined)
	exo.ApplyTo(&combined)
	return TelemetrySummary{Metrics: combined, Realization: realized}, nil
}
