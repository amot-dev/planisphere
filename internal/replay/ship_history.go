package replay

import (
	"encoding/json"
	"sort"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

const pilotedFSDJumpSQLFilter = `NOT COALESCE(json_extract(raw_json, '$.Taxi'), 0) AND NOT COALESCE(json_extract(raw_json, '$.Multicrew'), 0)`

var playerShipSQLFilter = buildPlayerShipSQLFilter()

func buildPlayerShipSQLFilter() string {
	var clauseBuilder strings.Builder
	clauseBuilder.WriteString(`(event = 'ShipyardSwap' OR (json_extract(raw_json, '$.Ship') IS NOT NULL AND TRIM(json_extract(raw_json, '$.Ship')) != ''`)
	for _, keyword := range reference.NonPlayerVesselKeywords {
		clauseBuilder.WriteString(` AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%`)
		clauseBuilder.WriteString(keyword)
		clauseBuilder.WriteString(`%'`)
	}
	clauseBuilder.WriteString(`))`)
	return clauseBuilder.String()
}

// ShipSwitch is one player starship switch in chronological timeline order.
type ShipSwitch struct {
	Timestamp string
	ShipID    int64
	HullName  string
}

// ShipTransition records a distinct vessel flown during an expedition waypoint leg.
type ShipTransition struct {
	Kind       string `json:"kind"` // "ship"
	At         string `json:"at"`
	AtGalactic string `json:"atGalactic"`
	To         string `json:"to"`
}

// QueryShipTimeline queries chronological player ship switches directly against
// queryer for [startTimestamp, endTimestamp].
func QueryShipTimeline(queryer db.Queryer, startTimestamp, endTimestamp string) ([]ShipSwitch, error) {
	var switches []ShipSwitch
	if startTimestamp != "" {
		seedQuery := `SELECT timestamp, event, raw_json FROM journal_events
			WHERE event IN ('LoadGame','ShipyardSwap') AND timestamp <= ? AND ` + playerShipSQLFilter + ` AND ` + CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1`
		var seedError error
		switches, seedError = appendQueriedShipSwitches(queryer, switches, seedQuery, startTimestamp)
		if seedError != nil {
			return nil, seedError
		}
	}

	query := `SELECT timestamp, event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap')`
	var queryArgs []any
	if startTimestamp != "" {
		query += ` AND timestamp > ?`
		queryArgs = append(queryArgs, startTimestamp)
	}
	if endTimestamp != "" {
		query += ` AND timestamp <= ?`
		queryArgs = append(queryArgs, endTimestamp)
	}
	query += ` AND ` + CmdrFilterSQL + ` ORDER BY timestamp, id`
	return appendQueriedShipSwitches(queryer, switches, query, queryArgs...)
}

// QueryJumpTimestamps loads chronological piloted hyperspace jump timestamps in
// [afterTimestamp, untilTimestamp], excluding Taxi and Multicrew FSDJump rows
// and merging remaining jumps with recovered.
func QueryJumpTimestamps(
	queryer db.Queryer,
	afterTimestamp, untilTimestamp string,
	recovered []RecoveredJump,
) ([]string, error) {
	query := `SELECT timestamp FROM journal_events WHERE event = 'FSDJump' AND timestamp >= ?`
	queryArgs := []any{afterTimestamp}
	if untilTimestamp != "" {
		query += ` AND timestamp <= ?`
		queryArgs = append(queryArgs, untilTimestamp)
	}
	query += ` AND ` + pilotedFSDJumpSQLFilter + ` AND ` + CmdrFilterSQL + ` ORDER BY timestamp, id`
	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()

	var timestamps []string
	for databaseRows.Next() {
		var timestamp string
		if scanError := databaseRows.Scan(&timestamp); scanError != nil {
			return nil, scanError
		}
		timestamps = append(timestamps, timestamp)
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return nil, iterationError
	}
	if len(recovered) > 0 {
		for _, recoveredJump := range recovered {
			timestamps = append(timestamps, recoveredJump.Timestamp)
		}
		sort.Strings(timestamps)
	}
	return timestamps, nil
}

// ExtractShipsUsedOnLeg resolves distinct starships jumped inside (afterTimestamp, untilTimestamp].
func ExtractShipsUsedOnLeg(
	timeline []ShipSwitch,
	jumpTimestamps []string,
	afterTimestamp, untilTimestamp string,
) []ShipTransition {
	var transitions []ShipTransition
	seenHullNames := map[string]bool{}
	startIndex := sort.Search(len(jumpTimestamps), func(i int) bool { return jumpTimestamps[i] > afterTimestamp })
	for i := startIndex; i < len(jumpTimestamps); i++ {
		jumpTimestamp := jumpTimestamps[i]
		if jumpTimestamp > untilTimestamp {
			break
		}
		activeSwitch := shipAtTimestamp(timeline, jumpTimestamp)
		if activeSwitch == nil || activeSwitch.HullName == "" || seenHullNames[activeSwitch.HullName] {
			continue
		}
		seenHullNames[activeSwitch.HullName] = true
		transitions = append(transitions, ShipTransition{
			Kind:       "ship",
			At:         jumpTimestamp,
			AtGalactic: reference.ToGalactic(jumpTimestamp),
			To:         activeSwitch.HullName,
		})
	}
	return transitions
}

// QueryCurrentShip returns the latest player starship display name and custom ship
// name (looking up custom ShipName by ShipID across prior LoadGame events after a
// ShipyardSwap), ignoring non-starship vessels.
func QueryCurrentShip(queryer db.Queryer) (string, error) {
	query := `SELECT event, raw_json FROM journal_events
		WHERE event IN ('LoadGame','ShipyardSwap') AND ` + playerShipSQLFilter + ` AND ` + CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 10`
	databaseRows, queryError := queryer.Query(query)
	if queryError != nil {
		return "", queryError
	}

	var (
		matchedLoadGame     *journal.LoadGameEvent
		matchedShipyardSwap *journal.ShipyardSwapEvent
	)
	for databaseRows.Next() {
		var eventName, rawJSON string
		if scanError := databaseRows.Scan(&eventName, &rawJSON); scanError != nil {
			_ = databaseRows.Close()
			return "", scanError
		}
		switch eventName {
		case "LoadGame":
			var loadGame journal.LoadGameEvent
			if json.Unmarshal([]byte(rawJSON), &loadGame) == nil && reference.IsPlayerShip(loadGame.Ship) {
				matchedLoadGame = &loadGame
			}
		case "ShipyardSwap":
			var shipyardSwap journal.ShipyardSwapEvent
			if json.Unmarshal([]byte(rawJSON), &shipyardSwap) == nil && shipyardSwap.ShipType != "" {
				matchedShipyardSwap = &shipyardSwap
			}
		}
		if matchedLoadGame != nil || matchedShipyardSwap != nil {
			break
		}
	}
	iterError := databaseRows.Err()
	_ = databaseRows.Close()
	if iterError != nil {
		return "", iterError
	}
	if matchedLoadGame != nil {
		return formatCurrentShipName(
			reference.ShipDisplayName(matchedLoadGame.Ship, matchedLoadGame.ShipLocalised),
			matchedLoadGame.ShipName,
		), nil
	}
	if matchedShipyardSwap != nil {
		hullDisplayName := reference.ShipDisplayName(matchedShipyardSwap.ShipType, matchedShipyardSwap.ShipTypeLocalised)
		customShipName := lookupShipNameByShipID(queryer, matchedShipyardSwap.ShipID, matchedShipyardSwap.ShipType)
		return formatCurrentShipName(hullDisplayName, customShipName), nil
	}
	return "", nil
}

func formatCurrentShipName(hullDisplayName, customShipName string) string {
	if strings.TrimSpace(customShipName) == "" {
		return hullDisplayName
	}
	return hullDisplayName + " \u201c" + customShipName + "\u201d"
}

func lookupShipNameByShipID(queryer db.Queryer, shipID int64, shipType string) string {
	query := `SELECT raw_json FROM journal_events
		WHERE event = 'LoadGame' AND json_extract(raw_json, '$.ShipID') = ? AND ` + playerShipSQLFilter + ` AND ` + CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`
	var rawLoadGame string
	if queryer.QueryRow(query, shipID).Scan(&rawLoadGame) != nil {
		return ""
	}
	var priorLoadGame journal.LoadGameEvent
	if json.Unmarshal([]byte(rawLoadGame), &priorLoadGame) != nil {
		return ""
	}
	if shipID == 0 && !strings.EqualFold(priorLoadGame.Ship, shipType) {
		return ""
	}
	return priorLoadGame.ShipName
}

func parseShipSwitch(timestamp, eventName, rawJSON string) (ShipSwitch, bool) {
	var parsedSwitch ShipSwitch
	parsedSwitch.Timestamp = timestamp
	switch eventName {
	case "LoadGame":
		var loadGameEvent journal.LoadGameEvent
		if json.Unmarshal([]byte(rawJSON), &loadGameEvent) != nil || !reference.IsPlayerShip(loadGameEvent.Ship) {
			return ShipSwitch{}, false
		}
		parsedSwitch.ShipID = loadGameEvent.ShipID
		parsedSwitch.HullName = reference.ShipDisplayName(loadGameEvent.Ship, loadGameEvent.ShipLocalised)
	case "ShipyardSwap":
		var swapEvent journal.ShipyardSwapEvent
		if json.Unmarshal([]byte(rawJSON), &swapEvent) != nil || swapEvent.ShipType == "" {
			return ShipSwitch{}, false
		}
		parsedSwitch.ShipID = swapEvent.ShipID
		parsedSwitch.HullName = reference.ShipDisplayName(swapEvent.ShipType, swapEvent.ShipTypeLocalised)
	default:
		return ShipSwitch{}, false
	}
	return parsedSwitch, true
}

func appendQueriedShipSwitches(
	queryer db.Queryer,
	switches []ShipSwitch,
	query string,
	queryArgs ...any,
) ([]ShipSwitch, error) {
	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()
	for databaseRows.Next() {
		var timestamp, eventName, rawJSON string
		if scanError := databaseRows.Scan(&timestamp, &eventName, &rawJSON); scanError != nil {
			return nil, scanError
		}
		if parsedSwitch, parsedOK := parseShipSwitch(timestamp, eventName, rawJSON); parsedOK {
			switches = append(switches, parsedSwitch)
		}
	}
	return switches, databaseRows.Err()
}

func shipAtTimestamp(timeline []ShipSwitch, timestamp string) *ShipSwitch {
	segmentIndex := sort.Search(len(timeline), func(i int) bool { return timeline[i].Timestamp > timestamp }) - 1
	if segmentIndex < 0 {
		return nil
	}
	return &timeline[segmentIndex]
}
