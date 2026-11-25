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
	eventType     journal.EventType
	systemName    string
	systemAddress int64
	starPosition  reference.Vec3
	rawJSON       string
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
	rawJSON       sql.NullString
}

func (columns nullableArrivalColumns) toSnapshot(eventType journal.EventType) arrivalSnapshot {
	if !columns.id.Valid || !columns.timestamp.Valid || !columns.starX.Valid || !columns.starY.Valid || !columns.starZ.Valid {
		return arrivalSnapshot{}
	}
	return arrivalSnapshot{
		id:            columns.id.Int64,
		timestamp:     columns.timestamp.String,
		eventType:     eventType,
		systemName:    columns.systemName.String,
		systemAddress: columns.systemAddress.Int64,
		starPosition:  reference.Vec3{X: columns.starX.Float64, Y: columns.starY.Float64, Z: columns.starZ.Float64},
		rawJSON:       columns.rawJSON.String,
		valid:         true,
	}
}

type primedDisconnectCandidate struct {
	loadGameID         int64
	loadGameTimestamp  string
	profileID          int64
	commander          string
	locationID         int64
	locationUID        string
	locationTimestamp  string
	locationSystem     string
	locationAddress    int64
	locationPosition   reference.Vec3
	previousArrival    arrivalSnapshot
	isCarrier          bool
	carrierDocked      bool
	carrierMarketID    int64
	carrierStationName string
}

type preDisconnectEvent struct {
	id            int64
	timestamp     string
	profileID     int64
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

func sameActorAsSQL(alias, refProfileExpr, refCommanderExpr string) string {
	return ` AND ((COALESCE(` + refProfileExpr + `, 0) > 0 AND (` + alias + `.profile_id = ` + refProfileExpr + ` OR (COALESCE(` + alias + `.profile_id, 0) = 0 AND COALESCE(` + alias + `.commander, '') = ''))) OR (COALESCE(` + refProfileExpr + `, 0) = 0 AND COALESCE(` + alias + `.profile_id, 0) = 0 AND (COALESCE(` + alias + `.commander, '') = '' OR COALESCE(` + refCommanderExpr + `, '') = '' OR ` + alias + `.commander = ` + refCommanderExpr + ` COLLATE NOCASE)))`
}

func isCarrierConcourseLocation(locationEvent journal.LocationEvent) bool {
	return locationEvent.OnFoot && !locationEvent.Docked &&
		!locationEvent.Multicrew && !locationEvent.Taxi && !locationEvent.InSRV &&
		!strings.EqualFold(locationEvent.BodyType, "Station") &&
		locationEvent.DistFromStarLS == nil
}

// buildPrimedCandidatesSQL builds the candidate query pairing each LoadGame with
// its next Location and preceding arrival for either an uncompleted hyperspace
// StartJump or a potential aboard-carrier Location transition. prev_loc_id stops
// strictly before LoadGame so a same-system relog cuts off pre-relog boosts,
// whereas prev_fsd_id and prev_cj_id scan up to Location so normal jumps logged
// between LoadGame and Location advance previousArrival past LoadGame and cause
// evaluatePrimedRow to reject the candidate.
func buildPrimedCandidatesSQL() string {
	actorLG := func(alias string) string { return sameActorAsSQL(alias, "lg.profile_id", "lg.commander") }
	actorU := func(alias string) string { return sameActorAsSQL(alias, "u.lg_pid", "u.lg_cmdr") }

	return `WITH lg_uncompleted_sj AS (
		SELECT
			lg.id AS lg_id, lg.timestamp AS lg_ts, COALESCE(lg.profile_id, 0) AS lg_pid, COALESCE(lg.commander, '') AS lg_cmdr,
			(SELECT loc.id FROM journal_events loc
			 WHERE loc.event = 'Location' AND (loc.timestamp, loc.id) > (lg.timestamp, lg.id)` + actorLG("loc") + `
			 ORDER BY loc.timestamp ASC, loc.id ASC LIMIT 1) AS next_loc_id,
			(SELECT sj.id FROM journal_events sj
			 WHERE sj.event = 'StartJump' AND (sj.timestamp, sj.id) < (lg.timestamp, lg.id)` + actorLG("sj") + `
			 ORDER BY sj.timestamp DESC, sj.id DESC LIMIT 1) AS last_sj_id
		FROM journal_events lg
		WHERE lg.event = 'LoadGame'
	),
	uncompleted AS (
		SELECT
			u.lg_id, u.lg_ts, u.lg_pid, u.lg_cmdr, u.next_loc_id,
			(SELECT l2.id FROM journal_events l2
			 WHERE l2.event = 'Location' AND (l2.timestamp, l2.id) < (u.lg_ts, u.lg_id)` + actorU("l2") + `
			 ORDER BY l2.timestamp DESC, l2.id DESC LIMIT 1) AS prev_loc_id,
			(SELECT f.id FROM journal_events f
			 WHERE f.event = 'FSDJump' AND (f.timestamp, f.id) < (loc.timestamp, loc.id)` + actorU("f") + `
			 ORDER BY f.timestamp DESC, f.id DESC LIMIT 1) AS prev_fsd_id,
			(SELECT c.id FROM journal_events c
			 WHERE c.event = 'CarrierJump' AND (c.timestamp, c.id) < (loc.timestamp, loc.id)` + actorU("c") + `
			 ORDER BY c.timestamp DESC, c.id DESC LIMIT 1) AS prev_cj_id
		FROM lg_uncompleted_sj u
		JOIN journal_events loc ON loc.id = u.next_loc_id
		LEFT JOIN journal_events sj ON sj.id = u.last_sj_id
		WHERE (
			instr(loc.raw_json, '"FleetCarrier"') > 0
			OR instr(loc.raw_json, '"OnFoot"') > 0
			OR (u.last_sj_id IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM journal_events f2
				WHERE f2.event = 'FSDJump' AND (f2.timestamp, f2.id) > (sj.timestamp, sj.id)
				  AND (f2.timestamp, f2.id) < (loc.timestamp, loc.id)` + actorU("f2") + `
			))
		)
	)
	SELECT
		u.lg_id, u.lg_ts, u.lg_pid, u.lg_cmdr, lg.raw_json,
		loc.id, loc.event_uid, loc.timestamp, COALESCE(loc.system_name, ''), COALESCE(loc.system_address, 0),
		loc.star_x, loc.star_y, loc.star_z, loc.raw_json,
		prev_f.id, prev_f.timestamp, prev_f.system_name, prev_f.system_address, prev_f.star_x, prev_f.star_y, prev_f.star_z,
		prev_l.id, prev_l.timestamp, prev_l.system_name, prev_l.system_address, prev_l.star_x, prev_l.star_y, prev_l.star_z, prev_l.raw_json,
		prev_c.id, prev_c.timestamp, prev_c.system_name, prev_c.system_address, prev_c.star_x, prev_c.star_y, prev_c.star_z, prev_c.raw_json
	FROM uncompleted u
	JOIN journal_events lg ON lg.id = u.lg_id
	JOIN journal_events loc ON loc.id = u.next_loc_id
	LEFT JOIN journal_events prev_f ON prev_f.id = u.prev_fsd_id
	LEFT JOIN journal_events prev_l ON prev_l.id = u.prev_loc_id
	LEFT JOIN journal_events prev_c ON prev_c.id = u.prev_cj_id
	ORDER BY u.lg_ts ASC, u.lg_id ASC`
}

func classifyLocationCandidate(locationJSON, loadGameJSON string, candidate *primedDisconnectCandidate) bool {
	var locationEvent journal.LocationEvent
	if json.Unmarshal([]byte(locationJSON), &locationEvent) != nil {
		return false
	}
	if locationEvent.InSRV || locationEvent.Taxi || locationEvent.Multicrew {
		return false
	}
	if locationEvent.Docked {
		if !strings.EqualFold(locationEvent.StationType, reference.FleetCarrierStationType) {
			return false
		}
		candidate.isCarrier = true
		candidate.carrierDocked = true
		candidate.carrierMarketID = locationEvent.MarketID
		candidate.carrierStationName = locationEvent.StationName
		return true
	}
	if locationEvent.OnFoot {
		if !isCarrierConcourseLocation(locationEvent) {
			return false
		}
		candidate.isCarrier = true
		return true
	}
	var loadGameEvent journal.LoadGameEvent
	return json.Unmarshal([]byte(loadGameJSON), &loadGameEvent) == nil && reference.IsPlayerShip(loadGameEvent.Ship)
}

func evaluatePrimedRow(
	loadGameID, loadGameProfileID, locationID, locationAddress int64,
	loadGameTimestamp, loadGameCommander, loadGameJSON, locationUID, locationTimestamp string,
	locationSystem, locationJSON string,
	locX, locY, locZ sql.NullFloat64,
	previousArrival arrivalSnapshot,
) (primedDisconnectCandidate, bool) {
	if !previousArrival.valid || !locX.Valid || !locY.Valid || !locZ.Valid {
		return primedDisconnectCandidate{}, false
	}
	if !eventOrderedBefore(previousArrival.timestamp, previousArrival.id, loadGameTimestamp, loadGameID) {
		return primedDisconnectCandidate{}, false
	}
	if disconnectSystemsMatch(previousArrival.systemAddress, previousArrival.systemName, locationAddress, locationSystem) {
		return primedDisconnectCandidate{}, false
	}
	candidate := primedDisconnectCandidate{
		loadGameID:        loadGameID,
		loadGameTimestamp: loadGameTimestamp,
		profileID:         loadGameProfileID,
		commander:         loadGameCommander,
		locationID:        locationID,
		locationUID:       locationUID,
		locationTimestamp: locationTimestamp,
		locationSystem:    locationSystem,
		locationAddress:   locationAddress,
		locationPosition:  reference.Vec3{X: locX.Float64, Y: locY.Float64, Z: locZ.Float64},
		previousArrival:   previousArrival,
	}
	if !classifyLocationCandidate(locationJSON, loadGameJSON, &candidate) {
		return primedDisconnectCandidate{}, false
	}
	return candidate, true
}

func queryPrimedCandidates(queryer db.Queryer) ([]primedDisconnectCandidate, error) {
	databaseRows, queryError := queryer.Query(buildPrimedCandidatesSQL())
	if queryError != nil {
		return nil, fmt.Errorf("query primed disconnect candidates: %w", queryError)
	}
	defer databaseRows.Close()

	var candidates []primedDisconnectCandidate
	for databaseRows.Next() {
		var (
			loadGameID, loadGameProfileID, locationID, locationAddress                         int64
			loadGameTimestamp, loadGameCommander, loadGameJSON, locationUID, locationTimestamp string
			locationSystem, locationJSON                                                       string
			locX, locY, locZ                                                                   sql.NullFloat64
			prevFSD, prevLoc, prevCarrier                                                      nullableArrivalColumns
		)
		scanError := databaseRows.Scan(
			&loadGameID, &loadGameTimestamp, &loadGameProfileID, &loadGameCommander, &loadGameJSON,
			&locationID, &locationUID, &locationTimestamp, &locationSystem, &locationAddress,
			&locX, &locY, &locZ, &locationJSON,
			&prevFSD.id, &prevFSD.timestamp, &prevFSD.systemName, &prevFSD.systemAddress, &prevFSD.starX, &prevFSD.starY, &prevFSD.starZ,
			&prevLoc.id, &prevLoc.timestamp, &prevLoc.systemName, &prevLoc.systemAddress, &prevLoc.starX, &prevLoc.starY, &prevLoc.starZ, &prevLoc.rawJSON,
			&prevCarrier.id, &prevCarrier.timestamp, &prevCarrier.systemName, &prevCarrier.systemAddress, &prevCarrier.starX, &prevCarrier.starY, &prevCarrier.starZ, &prevCarrier.rawJSON,
		)
		if scanError != nil {
			return nil, fmt.Errorf("scan primed disconnect candidate: %w", scanError)
		}
		latestPrevious := selectLatestArrival(
			prevFSD.toSnapshot(journal.EventFSDJump),
			prevLoc.toSnapshot(journal.EventLocation),
			prevCarrier.toSnapshot(journal.EventCarrierJump),
		)
		candidate, isCandidate := evaluatePrimedRow(
			loadGameID, loadGameProfileID, locationID, locationAddress,
			loadGameTimestamp, loadGameCommander, loadGameJSON, locationUID, locationTimestamp,
			locationSystem, locationJSON,
			locX, locY, locZ, latestPrevious,
		)
		// Multiple LoadGame rows resolving to the same Location keep only the final login state.
		candidates = slices.DeleteFunc(candidates, func(existing primedDisconnectCandidate) bool {
			return existing.locationID == locationID
		})
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
) ([]preDisconnectEvent, error) {
	rangePredicates := make([]string, 0, len(chunk))
	queryArgs := make([]any, 0, len(chunk)*2)
	for _, candidate := range chunk {
		rangePredicates = append(rangePredicates, "(je.timestamp >= ? AND je.timestamp <= ?)")
		queryArgs = append(queryArgs, candidate.previousArrival.timestamp, candidate.locationTimestamp)
	}
	// Omit SQL ORDER BY across MULTI-INDEX OR ranges to avoid a temporary B-tree sort; sort the tiny slice in Go.
	query := `SELECT je.id, je.timestamp, COALESCE(je.profile_id, 0), COALESCE(je.commander, ''), je.event, COALESCE(je.system_name, ''), COALESCE(je.system_address, 0), je.raw_json
		FROM journal_events je
		WHERE +je.event IN ('Resurrect', 'ShipyardSwap', 'StartJump', 'JetConeBoost', 'Synthesis', 'Docked', 'Undocked')
		  AND (` + strings.Join(rangePredicates, " OR ") + `)`

	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, fmt.Errorf("query pre-disconnect verification events: %w", queryError)
	}
	defer databaseRows.Close()

	var loadedEvents []preDisconnectEvent
	for databaseRows.Next() {
		var item preDisconnectEvent
		var eventName string
		if scanError := databaseRows.Scan(&item.id, &item.timestamp, &item.profileID, &item.commander, &eventName, &item.systemName, &item.systemAddress, &item.rawJSON); scanError != nil {
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
		return int(left.id - right.id)
	})
	return loadedEvents, nil
}

// queryPriorCarrierBoardingState walks backward along idx_journal_events_ts past
// transparent on-foot concourse Location relogs to determine whether the commander
// was already aboard a Fleet Carrier at arrival.
func queryPriorCarrierBoardingState(
	queryer db.Queryer,
	arrival arrivalSnapshot,
	profileID int64,
	commander string,
) (carrierStayState, error) {
	actorClause := ` AND COALESCE(je.profile_id, 0) = 0 AND (COALESCE(je.commander, '') = '' OR ? = '' OR je.commander = ? COLLATE NOCASE)`
	queryArgs := []any{arrival.timestamp, arrival.id, commander, commander}
	if profileID > 0 {
		actorClause = ` AND (+je.profile_id = ? OR (COALESCE(je.profile_id, 0) = 0 AND COALESCE(je.commander, '') = ''))`
		queryArgs = []any{arrival.timestamp, arrival.id, profileID}
	}
	query := `SELECT je.event, je.raw_json
		FROM journal_events je
		WHERE +je.event IN ('Docked', 'Undocked', 'FSDJump', 'CarrierJump', 'Resurrect', 'Location')
		  AND (je.timestamp, je.id) < (?, ?)` +
		actorClause + `
		ORDER BY je.timestamp DESC, je.id DESC`
	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return carrierStayState{}, fmt.Errorf("query prior carrier boarding state: %w", queryError)
	}
	defer databaseRows.Close()

	for databaseRows.Next() {
		var eventName, rawJSON string
		if scanError := databaseRows.Scan(&eventName, &rawJSON); scanError != nil {
			return carrierStayState{}, fmt.Errorf("scan prior carrier boarding state: %w", scanError)
		}
		if stay, decided := evaluatePriorBoardingRow(eventName, rawJSON); decided {
			return stay, nil
		}
	}
	return carrierStayState{}, databaseRows.Err()
}

func evaluatePriorBoardingRow(eventName, rawJSON string) (carrierStayState, bool) {
	switch eventName {
	case "Docked":
		var dockedEvent journal.DockedEvent
		if json.Unmarshal([]byte(rawJSON), &dockedEvent) != nil || dockedEvent.Multicrew {
			return carrierStayState{}, false
		}
		if strings.EqualFold(dockedEvent.StationType, reference.FleetCarrierStationType) {
			return carrierStayState{aboard: true, marketID: dockedEvent.MarketID, stationName: dockedEvent.StationName}, true
		}
		return carrierStayState{}, true
	case "CarrierJump":
		stay := carrierStayState{aboard: true}
		var carrierJumpEvent journal.CarrierJumpEvent
		if json.Unmarshal([]byte(rawJSON), &carrierJumpEvent) == nil {
			stay.marketID = carrierJumpEvent.MarketID
			stay.stationName = carrierJumpEvent.StationName
		}
		return stay, true
	case "Location":
		var locationEvent journal.LocationEvent
		if json.Unmarshal([]byte(rawJSON), &locationEvent) != nil || locationEvent.Multicrew {
			return carrierStayState{}, false
		}
		if locationEvent.Docked && strings.EqualFold(locationEvent.StationType, reference.FleetCarrierStationType) {
			return carrierStayState{aboard: true, marketID: locationEvent.MarketID, stationName: locationEvent.StationName}, true
		}
		return carrierStayState{}, !isCarrierConcourseLocation(locationEvent)
	default:
		var multicrewFlag struct {
			Multicrew bool `json:"Multicrew"`
		}
		if json.Unmarshal([]byte(rawJSON), &multicrewFlag) == nil && multicrewFlag.Multicrew {
			return carrierStayState{}, false
		}
		return carrierStayState{}, true
	}
}
