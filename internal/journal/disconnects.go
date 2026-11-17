package journal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"planisphere/internal/db"
	"planisphere/internal/ed"
)

// RecoveredJump represents a hyperspace jump completed server-side during a
// client disconnect, reconstructed from the subsequent LoadGame and Location sequence.
type RecoveredJump struct {
	LocationEventUID string
	Timestamp        string
	SystemName       string
	SystemAddress    int64
	StarPos          ed.Vec3
	JumpDist         float64
	BoostUsed        int
}

var (
	recoveredJumpsMu    sync.Mutex
	recoveredJumpsSlice []RecoveredJump
	recoveredJumpsDB    *sql.DB
	recoveredJumpsMaxID int64 = -1
	recoveredJumpsValid bool
	recoveredJumpsGen   uint64
)

// InvalidateRecoveredJumpsCache clears the cached disconnected-jump recovery slice.
func InvalidateRecoveredJumpsCache() {
	recoveredJumpsMu.Lock()
	defer recoveredJumpsMu.Unlock()
	recoveredJumpsSlice = nil
	recoveredJumpsDB = nil
	recoveredJumpsMaxID = -1
	recoveredJumpsValid = false
	recoveredJumpsGen++
}

type arrivalSnapshot struct {
	id            int64
	timestamp     string
	systemName    string
	systemAddress int64
	starPosition  ed.Vec3
	valid         bool
}

type nullableArrivalColumns struct {
	id            sql.NullInt64
	timestamp     sql.NullString
	systemName    sql.NullString
	systemAddress sql.NullInt64
	starX         sql.NullFloat64
	starY         sql.NullFloat64
	starZ         sql.NullFloat64
}

func (columns nullableArrivalColumns) toSnapshot() arrivalSnapshot {
	if !columns.id.Valid || !columns.timestamp.Valid || !columns.starX.Valid || !columns.starY.Valid || !columns.starZ.Valid {
		return arrivalSnapshot{}
	}
	return arrivalSnapshot{
		id:            columns.id.Int64,
		timestamp:     columns.timestamp.String,
		systemName:    columns.systemName.String,
		systemAddress: columns.systemAddress.Int64,
		starPosition:  ed.Vec3{X: columns.starX.Float64, Y: columns.starY.Float64, Z: columns.starZ.Float64},
		valid:         true,
	}
}

type primedDisconnectCandidate struct {
	loadGameID        int64
	loadGameTimestamp string
	locationID        int64
	locationUID       string
	locationTimestamp string
	locationSystem    string
	locationAddress   int64
	locationPosition  ed.Vec3
	previousArrival   arrivalSnapshot
}

type preDisconnectEvent struct {
	id            int64
	timestamp     string
	eventType     string
	systemName    string
	systemAddress int64
	rawJSON       string
}

func eventOrderedBefore(firstTimestamp string, firstID int64, secondTimestamp string, secondID int64) bool {
	if firstTimestamp != secondTimestamp {
		return firstTimestamp < secondTimestamp
	}
	return firstID < secondID
}

func systemsMatch(firstAddress int64, firstName string, secondAddress int64, secondName string) bool {
	if firstAddress != 0 && secondAddress != 0 {
		return firstAddress == secondAddress
	}
	trimmedFirst := strings.TrimSpace(firstName)
	trimmedSecond := strings.TrimSpace(secondName)
	return trimmedFirst != "" && strings.EqualFold(trimmedFirst, trimmedSecond)
}

func selectLatestArrival(candidates ...arrivalSnapshot) arrivalSnapshot {
	var chosen arrivalSnapshot
	for _, candidate := range candidates {
		if !candidate.valid {
			continue
		}
		if !chosen.valid || eventOrderedBefore(chosen.timestamp, chosen.id, candidate.timestamp, candidate.id) {
			chosen = candidate
		}
	}
	return chosen
}

func commanderFilterForAlias(alias string, applyFilter bool) string {
	if !applyFilter {
		return ""
	}
	return ` AND (` + alias + `.commander IS NULL OR ` + alias + `.commander = '' OR NOT EXISTS (SELECT 1 FROM commanders c WHERE c.name = ` + alias + `.commander COLLATE NOCASE AND c.enabled = 0))`
}

func buildPrimedCandidatesSQL(applyCommanderFilter bool) string {
	loadGameFilter := commanderFilterForAlias("lg", applyCommanderFilter)
	nextLocFilter := commanderFilterForAlias("loc", applyCommanderFilter)
	startJumpFilter := commanderFilterForAlias("sj", applyCommanderFilter)
	completedFSDFilter := commanderFilterForAlias("f2", applyCommanderFilter)
	prevLocFilter := commanderFilterForAlias("l2", applyCommanderFilter)
	prevFSDFilter := commanderFilterForAlias("f", applyCommanderFilter)
	prevCarrierFilter := commanderFilterForAlias("c", applyCommanderFilter)

	return `WITH lg_uncompleted_sj AS (
		SELECT
			lg.id AS lg_id,
			lg.timestamp AS lg_ts,
			(SELECT loc.id FROM journal_events loc
			 WHERE loc.event = 'Location' AND (loc.timestamp, loc.id) > (lg.timestamp, lg.id)` + nextLocFilter + `
			 ORDER BY loc.timestamp ASC, loc.id ASC LIMIT 1) AS next_loc_id,
			(SELECT sj.id FROM journal_events sj
			 WHERE sj.event = 'StartJump' AND (sj.timestamp, sj.id) < (lg.timestamp, lg.id)` + startJumpFilter + `
			 ORDER BY sj.timestamp DESC, sj.id DESC LIMIT 1) AS last_sj_id
		FROM journal_events lg
		WHERE lg.event = 'LoadGame'` + loadGameFilter + `
	),
	uncompleted AS (
		SELECT
			u.lg_id, u.lg_ts, u.next_loc_id,
			(SELECT l2.id FROM journal_events l2
			 WHERE l2.event = 'Location' AND (l2.timestamp, l2.id) < (u.lg_ts, u.lg_id)` + prevLocFilter + `
			 ORDER BY l2.timestamp DESC, l2.id DESC LIMIT 1) AS prev_loc_id,
			(SELECT f.id FROM journal_events f
			 WHERE f.event = 'FSDJump' AND (f.timestamp, f.id) < (loc.timestamp, loc.id)` + prevFSDFilter + `
			 ORDER BY f.timestamp DESC, f.id DESC LIMIT 1) AS prev_fsd_id,
			(SELECT c.id FROM journal_events c
			 WHERE c.event = 'CarrierJump' AND (c.timestamp, c.id) < (loc.timestamp, loc.id)` + prevCarrierFilter + `
			 ORDER BY c.timestamp DESC, c.id DESC LIMIT 1) AS prev_cj_id
		FROM lg_uncompleted_sj u
		JOIN journal_events loc ON loc.id = u.next_loc_id
		JOIN journal_events sj ON sj.id = u.last_sj_id
		WHERE NOT EXISTS (
			SELECT 1 FROM journal_events f2
			WHERE f2.event = 'FSDJump'
			  AND (f2.timestamp, f2.id) > (sj.timestamp, sj.id)
			  AND (f2.timestamp, f2.id) < (loc.timestamp, loc.id)` + completedFSDFilter + `
		)
	)
	SELECT
		u.lg_id, u.lg_ts, lg.raw_json,
		loc.id, loc.event_uid, loc.timestamp, COALESCE(loc.system_name, ''), COALESCE(loc.system_address, 0),
		loc.star_x, loc.star_y, loc.star_z, loc.raw_json,
		prev_f.id, prev_f.timestamp, prev_f.system_name, prev_f.system_address, prev_f.star_x, prev_f.star_y, prev_f.star_z,
		prev_l.id, prev_l.timestamp, prev_l.system_name, prev_l.system_address, prev_l.star_x, prev_l.star_y, prev_l.star_z,
		prev_c.id, prev_c.timestamp, prev_c.system_name, prev_c.system_address, prev_c.star_x, prev_c.star_y, prev_c.star_z
	FROM uncompleted u
	JOIN journal_events lg ON lg.id = u.lg_id
	JOIN journal_events loc ON loc.id = u.next_loc_id
	LEFT JOIN journal_events prev_f ON prev_f.id = u.prev_fsd_id
	LEFT JOIN journal_events prev_l ON prev_l.id = u.prev_loc_id
	LEFT JOIN journal_events prev_c ON prev_c.id = u.prev_cj_id
	ORDER BY u.lg_ts ASC, u.lg_id ASC`
}

func evaluatePrimedRow(
	loadGameID int64,
	loadGameTimestamp string,
	loadGameJSON string,
	locationID int64,
	locationUID string,
	locationTimestamp string,
	locationSystem string,
	locationAddress int64,
	locX, locY, locZ sql.NullFloat64,
	locationJSON string,
	previousArrival arrivalSnapshot,
) (primedDisconnectCandidate, bool) {
	var loadGameEvent LoadGameEvent
	if json.Unmarshal([]byte(loadGameJSON), &loadGameEvent) != nil || !IsPlayerShip(loadGameEvent.Ship) {
		return primedDisconnectCandidate{}, false
	}
	if !previousArrival.valid || !locX.Valid || !locY.Valid || !locZ.Valid {
		return primedDisconnectCandidate{}, false
	}
	if !eventOrderedBefore(previousArrival.timestamp, previousArrival.id, loadGameTimestamp, loadGameID) {
		return primedDisconnectCandidate{}, false
	}
	if systemsMatch(previousArrival.systemAddress, previousArrival.systemName, locationAddress, locationSystem) {
		return primedDisconnectCandidate{}, false
	}
	var locationEvent LocationEvent
	if json.Unmarshal([]byte(locationJSON), &locationEvent) != nil {
		return primedDisconnectCandidate{}, false
	}
	if locationEvent.Docked || locationEvent.OnFoot || locationEvent.InSRV || locationEvent.Taxi || locationEvent.Multicrew {
		return primedDisconnectCandidate{}, false
	}
	return primedDisconnectCandidate{
		loadGameID:        loadGameID,
		loadGameTimestamp: loadGameTimestamp,
		locationID:        locationID,
		locationUID:       locationUID,
		locationTimestamp: locationTimestamp,
		locationSystem:    locationSystem,
		locationAddress:   locationAddress,
		locationPosition:  ed.Vec3{X: locX.Float64, Y: locY.Float64, Z: locZ.Float64},
		previousArrival:   previousArrival,
	}, true
}

func queryPrimedCandidates(queryer db.Queryer, applyCommanderFilter bool) ([]primedDisconnectCandidate, error) {
	rows, queryError := queryer.Query(buildPrimedCandidatesSQL(applyCommanderFilter))
	if queryError != nil {
		return nil, fmt.Errorf("query primed disconnect candidates: %w", queryError)
	}
	defer rows.Close()

	var candidates []primedDisconnectCandidate
	for rows.Next() {
		var (
			loadGameID, locationID, locationAddress                         int64
			loadGameTimestamp, loadGameJSON, locationUID, locationTimestamp string
			locationSystem, locationJSON                                    string
			locX, locY, locZ                                                sql.NullFloat64
			prevFSD, prevLoc, prevCarrier                                   nullableArrivalColumns
		)
		scanError := rows.Scan(
			&loadGameID, &loadGameTimestamp, &loadGameJSON,
			&locationID, &locationUID, &locationTimestamp, &locationSystem, &locationAddress,
			&locX, &locY, &locZ, &locationJSON,
			&prevFSD.id, &prevFSD.timestamp, &prevFSD.systemName, &prevFSD.systemAddress, &prevFSD.starX, &prevFSD.starY, &prevFSD.starZ,
			&prevLoc.id, &prevLoc.timestamp, &prevLoc.systemName, &prevLoc.systemAddress, &prevLoc.starX, &prevLoc.starY, &prevLoc.starZ,
			&prevCarrier.id, &prevCarrier.timestamp, &prevCarrier.systemName, &prevCarrier.systemAddress, &prevCarrier.starX, &prevCarrier.starY, &prevCarrier.starZ,
		)
		if scanError != nil {
			return nil, fmt.Errorf("scan primed disconnect candidate: %w", scanError)
		}
		latestPrevious := selectLatestArrival(prevFSD.toSnapshot(), prevLoc.toSnapshot(), prevCarrier.toSnapshot())
		candidate, isCandidate := evaluatePrimedRow(
			loadGameID, loadGameTimestamp, loadGameJSON,
			locationID, locationUID, locationTimestamp, locationSystem, locationAddress,
			locX, locY, locZ, locationJSON, latestPrevious,
		)
		// Multiple consecutive LoadGame rows resolving to the same Location keep only the final login state.
		if len(candidates) > 0 && candidates[len(candidates)-1].locationID == locationID {
			candidates = candidates[:len(candidates)-1]
		}
		if isCandidate {
			candidates = append(candidates, candidate)
		}
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate primed disconnect candidates: %w", iterationError)
	}
	return candidates, nil
}

func loadPreDisconnectChunk(
	queryer db.Queryer,
	chunk []primedDisconnectCandidate,
	applyCommanderFilter bool,
) ([]preDisconnectEvent, error) {
	rangePredicates := make([]string, 0, len(chunk))
	queryArgs := make([]any, 0, len(chunk)*2)
	for _, candidate := range chunk {
		rangePredicates = append(rangePredicates, "(je.timestamp >= ? AND je.timestamp <= ?)")
		queryArgs = append(queryArgs, candidate.previousArrival.timestamp, candidate.locationTimestamp)
	}
	// Omit SQL ORDER BY across MULTI-INDEX OR ranges to avoid a temporary B-tree sort; sort the tiny slice in Go.
	query := `SELECT je.id, je.timestamp, je.event, COALESCE(je.system_name, ''), COALESCE(je.system_address, 0), je.raw_json
		FROM journal_events je
		WHERE +je.event IN ('Resurrect', 'ShipyardSwap', 'StartJump', 'JetConeBoost', 'Synthesis')` +
		commanderFilterForAlias("je", applyCommanderFilter) +
		` AND (` + strings.Join(rangePredicates, " OR ") + `)`

	rows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, fmt.Errorf("query pre-disconnect verification events: %w", queryError)
	}
	defer rows.Close()

	var events []preDisconnectEvent
	for rows.Next() {
		var item preDisconnectEvent
		if scanError := rows.Scan(&item.id, &item.timestamp, &item.eventType, &item.systemName, &item.systemAddress, &item.rawJSON); scanError != nil {
			return nil, fmt.Errorf("scan pre-disconnect verification event: %w", scanError)
		}
		events = append(events, item)
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate pre-disconnect verification events: %w", iterationError)
	}
	slices.SortFunc(events, func(left, right preDisconnectEvent) int {
		if left.timestamp != right.timestamp {
			return strings.Compare(left.timestamp, right.timestamp)
		}
		if left.id < right.id {
			return -1
		}
		if left.id > right.id {
			return 1
		}
		return 0
	})
	return events, nil
}

func applyPreDisconnectEvent(item preDisconnectEvent, boostUsed *int, latestStartJump **StartJumpEvent) {
	switch item.eventType {
	case "ShipyardSwap":
		*boostUsed = 0
	case "JetConeBoost":
		var jetBoost JetConeBoostEvent
		if json.Unmarshal([]byte(item.rawJSON), &jetBoost) == nil && jetBoost.BoostValue > 1.0 {
			*boostUsed = 4
		}
	case "Synthesis":
		var synthesis SynthesisEvent
		if json.Unmarshal([]byte(item.rawJSON), &synthesis) == nil {
			if tier := FSDSynthesisBoostTier(synthesis.Name); tier > 0 {
				*boostUsed = tier
			}
		}
	case "StartJump":
		var startJump StartJumpEvent
		if json.Unmarshal([]byte(item.rawJSON), &startJump) == nil && strings.EqualFold(strings.TrimSpace(startJump.JumpType), "Hyperspace") {
			*latestStartJump = &startJump
		}
	}
}

func resolveCandidateFromSlice(candidate primedDisconnectCandidate, windowEvents []preDisconnectEvent) (RecoveredJump, bool) {
	var (
		latestStartJump *StartJumpEvent
		boostUsed       int
	)
	for _, item := range windowEvents {
		if !eventOrderedBefore(candidate.previousArrival.timestamp, candidate.previousArrival.id, item.timestamp, item.id) {
			continue
		}
		if eventOrderedBefore(candidate.locationTimestamp, candidate.locationID, item.timestamp, item.id) {
			continue
		}
		if item.eventType == "Resurrect" {
			return RecoveredJump{}, false
		}
		if !eventOrderedBefore(candidate.loadGameTimestamp, candidate.loadGameID, item.timestamp, item.id) {
			applyPreDisconnectEvent(item, &boostUsed, &latestStartJump)
		}
	}
	if latestStartJump == nil || latestStartJump.Taxi || latestStartJump.Multicrew {
		return RecoveredJump{}, false
	}
	if !systemsMatch(latestStartJump.SystemAddress, latestStartJump.StarSystem, candidate.locationAddress, candidate.locationSystem) {
		return RecoveredJump{}, false
	}
	jumpDistance := ed.Distance(candidate.previousArrival.starPosition, candidate.locationPosition)
	if jumpDistance <= 0 {
		return RecoveredJump{}, false
	}
	return RecoveredJump{
		LocationEventUID: candidate.locationUID,
		Timestamp:        candidate.locationTimestamp,
		SystemName:       candidate.locationSystem,
		SystemAddress:    candidate.locationAddress,
		StarPos:          candidate.locationPosition,
		JumpDist:         jumpDistance,
		BoostUsed:        boostUsed,
	}, true
}

func computeAllRecoveredJumps(queryer db.Queryer) ([]RecoveredJump, error) {
	hasDisabled, checkError := HasDisabledCommanders(queryer)
	if checkError != nil {
		return nil, fmt.Errorf("check disabled commanders for disconnect recovery: %w", checkError)
	}
	candidates, queryError := queryPrimedCandidates(queryer, hasDisabled)
	if queryError != nil || len(candidates) == 0 {
		return nil, queryError
	}

	const candidateChunkSize = 100
	var recovered []RecoveredJump
	for chunkStart := 0; chunkStart < len(candidates); chunkStart += candidateChunkSize {
		chunkEnd := min(chunkStart+candidateChunkSize, len(candidates))
		chunk := candidates[chunkStart:chunkEnd]
		events, loadError := loadPreDisconnectChunk(queryer, chunk, hasDisabled)
		if loadError != nil {
			return nil, loadError
		}
		cursor := 0
		for _, candidate := range chunk {
			for cursor < len(events) && eventOrderedBefore(events[cursor].timestamp, events[cursor].id, candidate.previousArrival.timestamp, candidate.previousArrival.id) {
				cursor++
			}
			sliceEnd := cursor
			for sliceEnd < len(events) && !eventOrderedBefore(candidate.locationTimestamp, candidate.locationID, events[sliceEnd].timestamp, events[sliceEnd].id) {
				sliceEnd++
			}
			if jump, ok := resolveCandidateFromSlice(candidate, events[cursor:sliceEnd]); ok {
				recovered = append(recovered, jump)
			}
		}
	}
	return recovered, nil
}

func loadCachedRecoveredJumps(queryer db.Queryer) ([]RecoveredJump, error) {
	sqlDB, isSQLDB := queryer.(*sql.DB)
	if !isSQLDB {
		return computeAllRecoveredJumps(queryer)
	}
	var maxJournalID int64
	if maxError := sqlDB.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM journal_events`).Scan(&maxJournalID); maxError != nil {
		return computeAllRecoveredJumps(queryer)
	}

	recoveredJumpsMu.Lock()
	if recoveredJumpsValid && recoveredJumpsDB == sqlDB && recoveredJumpsMaxID == maxJournalID {
		cached := recoveredJumpsSlice
		recoveredJumpsMu.Unlock()
		return cached, nil
	}
	capturedGeneration := recoveredJumpsGen
	recoveredJumpsMu.Unlock()

	computedJumps, computeError := computeAllRecoveredJumps(queryer)
	if computeError != nil {
		return nil, computeError
	}

	recoveredJumpsMu.Lock()
	if recoveredJumpsGen == capturedGeneration {
		recoveredJumpsSlice = computedJumps
		recoveredJumpsDB = sqlDB
		recoveredJumpsMaxID = maxJournalID
		recoveredJumpsValid = true
	}
	recoveredJumpsMu.Unlock()
	return computedJumps, nil
}

// RecoveredJumpsInRange returns all server-completed hyperspace jumps recovered
// from post-disconnect LoadGame and Location sequences within (afterTimestamp, untilTimestamp].
// Empty afterTimestamp starts from the beginning of history; empty untilTimestamp runs up to now.
func RecoveredJumpsInRange(queryer db.Queryer, afterTimestamp, untilTimestamp string) ([]RecoveredJump, error) {
	allRecovered, loadError := loadCachedRecoveredJumps(queryer)
	if loadError != nil || len(allRecovered) == 0 {
		return nil, loadError
	}
	startIndex := 0
	if afterTimestamp != "" {
		startIndex = sort.Search(len(allRecovered), func(i int) bool {
			return allRecovered[i].Timestamp > afterTimestamp
		})
	}
	endIndex := len(allRecovered)
	if untilTimestamp != "" {
		endIndex = sort.Search(len(allRecovered), func(i int) bool {
			return allRecovered[i].Timestamp > untilTimestamp
		})
	}
	if startIndex >= endIndex {
		return nil, nil
	}
	return allRecovered[startIndex:endIndex], nil
}
