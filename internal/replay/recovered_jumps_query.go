package replay

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

type arrivalSnapshot struct {
	id            int64
	timestamp     string
	systemName    string
	systemAddress int64
	starPosition  reference.Vec3
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
		starPosition:  reference.Vec3{X: columns.starX.Float64, Y: columns.starY.Float64, Z: columns.starZ.Float64},
		valid:         true,
	}
}

type primedDisconnectCandidate struct {
	loadGameID        int64
	loadGameTimestamp string
	commander         string
	locationID        int64
	locationUID       string
	locationTimestamp string
	locationSystem    string
	locationAddress   int64
	locationPosition  reference.Vec3
	previousArrival   arrivalSnapshot
}

type preDisconnectEvent struct {
	id            int64
	timestamp     string
	commander     string
	eventType     journal.EventType
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

func disconnectSystemsMatch(firstAddress int64, firstName string, secondAddress int64, secondName string) bool {
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

func sameCommanderAsSQL(alias, referenceCommanderExpr string) string {
	return ` AND (COALESCE(` + alias + `.commander, '') = '' OR COALESCE(` + referenceCommanderExpr + `, '') = '' OR ` + alias + `.commander = ` + referenceCommanderExpr + ` COLLATE NOCASE)`
}

// buildPrimedCandidatesSQL builds the candidate query pairing each LoadGame with
// its next Location and latest prior StartJump when no FSDJump completed between
// them. prev_loc_id stops strictly before LoadGame so a same-system relog cuts
// off pre-relog boosts, whereas prev_fsd_id and prev_cj_id scan up to Location
// so normal jumps logged between LoadGame and Location advance previousArrival
// past LoadGame and cause evaluatePrimedRow to reject the candidate.
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
			COALESCE(lg.commander, '') AS lg_cmdr,
			(SELECT loc.id FROM journal_events loc
			 WHERE loc.event = 'Location' AND (loc.timestamp, loc.id) > (lg.timestamp, lg.id)` + nextLocFilter + sameCommanderAsSQL("loc", "lg.commander") + `
			 ORDER BY loc.timestamp ASC, loc.id ASC LIMIT 1) AS next_loc_id,
			(SELECT sj.id FROM journal_events sj
			 WHERE sj.event = 'StartJump' AND (sj.timestamp, sj.id) < (lg.timestamp, lg.id)` + startJumpFilter + sameCommanderAsSQL("sj", "lg.commander") + `
			 ORDER BY sj.timestamp DESC, sj.id DESC LIMIT 1) AS last_sj_id
		FROM journal_events lg
		WHERE lg.event = 'LoadGame'` + loadGameFilter + `
	),
	uncompleted AS (
		SELECT
			u.lg_id, u.lg_ts, u.lg_cmdr, u.next_loc_id,
			(SELECT l2.id FROM journal_events l2
			 WHERE l2.event = 'Location' AND (l2.timestamp, l2.id) < (u.lg_ts, u.lg_id)` + prevLocFilter + sameCommanderAsSQL("l2", "u.lg_cmdr") + `
			 ORDER BY l2.timestamp DESC, l2.id DESC LIMIT 1) AS prev_loc_id,
			(SELECT f.id FROM journal_events f
			 WHERE f.event = 'FSDJump' AND (f.timestamp, f.id) < (loc.timestamp, loc.id)` + prevFSDFilter + sameCommanderAsSQL("f", "u.lg_cmdr") + `
			 ORDER BY f.timestamp DESC, f.id DESC LIMIT 1) AS prev_fsd_id,
			(SELECT c.id FROM journal_events c
			 WHERE c.event = 'CarrierJump' AND (c.timestamp, c.id) < (loc.timestamp, loc.id)` + prevCarrierFilter + sameCommanderAsSQL("c", "u.lg_cmdr") + `
			 ORDER BY c.timestamp DESC, c.id DESC LIMIT 1) AS prev_cj_id
		FROM lg_uncompleted_sj u
		JOIN journal_events loc ON loc.id = u.next_loc_id
		JOIN journal_events sj ON sj.id = u.last_sj_id
		WHERE NOT EXISTS (
			SELECT 1 FROM journal_events f2
			WHERE f2.event = 'FSDJump'
			  AND (f2.timestamp, f2.id) > (sj.timestamp, sj.id)
			  AND (f2.timestamp, f2.id) < (loc.timestamp, loc.id)` + completedFSDFilter + sameCommanderAsSQL("f2", "u.lg_cmdr") + `
		)
	)
	SELECT
		u.lg_id, u.lg_ts, u.lg_cmdr, lg.raw_json,
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
	loadGameCommander string,
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
	var loadGameEvent journal.LoadGameEvent
	if json.Unmarshal([]byte(loadGameJSON), &loadGameEvent) != nil || !reference.IsPlayerShip(loadGameEvent.Ship) {
		return primedDisconnectCandidate{}, false
	}
	if !previousArrival.valid || !locX.Valid || !locY.Valid || !locZ.Valid {
		return primedDisconnectCandidate{}, false
	}
	if !eventOrderedBefore(previousArrival.timestamp, previousArrival.id, loadGameTimestamp, loadGameID) {
		return primedDisconnectCandidate{}, false
	}
	if disconnectSystemsMatch(previousArrival.systemAddress, previousArrival.systemName, locationAddress, locationSystem) {
		return primedDisconnectCandidate{}, false
	}
	var locationEvent journal.LocationEvent
	if json.Unmarshal([]byte(locationJSON), &locationEvent) != nil {
		return primedDisconnectCandidate{}, false
	}
	if locationEvent.Docked || locationEvent.OnFoot || locationEvent.InSRV || locationEvent.Taxi || locationEvent.Multicrew {
		return primedDisconnectCandidate{}, false
	}
	return primedDisconnectCandidate{
		loadGameID:        loadGameID,
		loadGameTimestamp: loadGameTimestamp,
		commander:         loadGameCommander,
		locationID:        locationID,
		locationUID:       locationUID,
		locationTimestamp: locationTimestamp,
		locationSystem:    locationSystem,
		locationAddress:   locationAddress,
		locationPosition:  reference.Vec3{X: locX.Float64, Y: locY.Float64, Z: locZ.Float64},
		previousArrival:   previousArrival,
	}, true
}

func queryPrimedCandidates(queryer db.Queryer, applyCommanderFilter bool) ([]primedDisconnectCandidate, error) {
	databaseRows, queryError := queryer.Query(buildPrimedCandidatesSQL(applyCommanderFilter))
	if queryError != nil {
		return nil, fmt.Errorf("query primed disconnect candidates: %w", queryError)
	}
	defer databaseRows.Close()

	var candidates []primedDisconnectCandidate
	for databaseRows.Next() {
		var (
			loadGameID, locationID, locationAddress                                            int64
			loadGameTimestamp, loadGameCommander, loadGameJSON, locationUID, locationTimestamp string
			locationSystem, locationJSON                                                       string
			locX, locY, locZ                                                                   sql.NullFloat64
			prevFSD, prevLoc, prevCarrier                                                      nullableArrivalColumns
		)
		scanError := databaseRows.Scan(
			&loadGameID, &loadGameTimestamp, &loadGameCommander, &loadGameJSON,
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
			loadGameID, loadGameTimestamp, loadGameCommander, loadGameJSON,
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
	if iterationError := databaseRows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate primed disconnect candidates: %w", iterationError)
	}
	return candidates, nil
}

// loadPreDisconnectChunk loads verification events across [previousArrival, locationTimestamp]
// rather than stopping at loadGameTimestamp so Resurrect events between LoadGame and
// Location still abort recovery, while boost and StartJump attribution is bounded at LoadGame in Go.
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
	query := `SELECT je.id, je.timestamp, COALESCE(je.commander, ''), je.event, COALESCE(je.system_name, ''), COALESCE(je.system_address, 0), je.raw_json
		FROM journal_events je
		WHERE +je.event IN ('Resurrect', 'ShipyardSwap', 'StartJump', 'JetConeBoost', 'Synthesis')` +
		commanderFilterForAlias("je", applyCommanderFilter) +
		` AND (` + strings.Join(rangePredicates, " OR ") + `)`

	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, fmt.Errorf("query pre-disconnect verification events: %w", queryError)
	}
	defer databaseRows.Close()

	var loadedEvents []preDisconnectEvent
	for databaseRows.Next() {
		var item preDisconnectEvent
		var eventName string
		if scanError := databaseRows.Scan(&item.id, &item.timestamp, &item.commander, &eventName, &item.systemName, &item.systemAddress, &item.rawJSON); scanError != nil {
			return nil, fmt.Errorf("scan pre-disconnect verification event: %w", scanError)
		}
		if parsedType, knownType := journal.ParseEventType(eventName); knownType {
			item.eventType = parsedType
		}
		loadedEvents = append(loadedEvents, item)
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate pre-disconnect verification events: %w", iterationError)
	}
	slices.SortFunc(loadedEvents, func(left, right preDisconnectEvent) int {
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
	return loadedEvents, nil
}
