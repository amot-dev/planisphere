package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

var (
	shipHistoryJournalEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventShipyardSwap,
	)
	shipHistoryAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

const playerShipSQLFilter = `(event = 'ShipyardSwap' OR (json_extract(raw_json, '$.Ship') IS NOT NULL AND TRIM(json_extract(raw_json, '$.Ship')) != '' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%suit%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%buggy%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%srv%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%fighter%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%lander%'))`

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

// ShipHistory caches time-windowed active player vessel timelines (LoadGame,
// ShipyardSwap) for expedition leg drawers and the cockpit header.
type ShipHistory struct {
	database       *sql.DB
	recoveredJumps *RecoveredJumps
	timelines      *projection.KeyedCache[string, []ShipSwitch]
}

// NewShipHistory creates a ShipHistory projection and registers it on bus.
func NewShipHistory(dbConn *sql.DB, bus *projection.Bus, recoveredJumps *RecoveredJumps) *ShipHistory {
	history := &ShipHistory{
		database:       dbConn,
		recoveredJumps: recoveredJumps,
	}
	history.timelines = projection.NewKeyedCache[string, []ShipSwitch](
		bus,
		"ship_history",
		history.Triggers(),
	)
	return history
}

// Triggers declares the journal and application events that invalidate ShipHistory.
func (history *ShipHistory) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: shipHistoryJournalEvents,
		App:     shipHistoryAppEvents,
	}
}

// Timeline returns cached chronological player ship switches covering
// [startTimestamp, endTimestamp], seeded with the active ship at or before startTimestamp.
func (history *ShipHistory) Timeline(startTimestamp, endTimestamp string) ([]ShipSwitch, error) {
	if history.database == nil {
		return nil, fmt.Errorf("ship history database is nil")
	}
	cacheKey := fmt.Sprintf("%s|%s", startTimestamp, endTimestamp)
	return history.timelines.GetOrLoad(cacheKey, func() ([]ShipSwitch, error) {
		return QueryShipTimeline(history.database, startTimestamp, endTimestamp)
	})
}

// LoadJumpTimestamps returns sorted FSDJump and crash-recovered jump timestamps
// within [afterTimestamp, untilTimestamp].
func (history *ShipHistory) LoadJumpTimestamps(afterTimestamp, untilTimestamp string) ([]string, error) {
	if history.database == nil {
		return nil, fmt.Errorf("ship history database is nil")
	}
	var (
		recovered    []RecoveredJump
		recoverError error
	)
	if history.recoveredJumps != nil {
		recovered, recoverError = history.recoveredJumps.InRange(afterTimestamp, untilTimestamp)
	} else {
		recovered, recoverError = ComputeRecoveredJumpsInRange(history.database, afterTimestamp, untilTimestamp)
	}
	if recoverError != nil {
		return nil, recoverError
	}
	return QueryJumpTimestamps(history.database, afterTimestamp, untilTimestamp, recovered)
}

// ShipsUsedOnLeg resolves distinct starships jumped inside (afterTimestamp, untilTimestamp].
func (history *ShipHistory) ShipsUsedOnLeg(
	timeline []ShipSwitch,
	jumpTimestamps []string,
	afterTimestamp, untilTimestamp string,
) []ShipTransition {
	return ExtractShipsUsedOnLeg(timeline, jumpTimestamps, afterTimestamp, untilTimestamp)
}

// CurrentShip resolves the latest player starship hull and optional custom ship name
// (ignoring on-foot suits, SRVs, fighters, and landers) for the cockpit top bar.
func (history *ShipHistory) CurrentShip() (string, error) {
	if history.database == nil {
		return "", fmt.Errorf("ship history database is nil")
	}
	return QueryCurrentShip(history.database)
}

// QueryShipTimeline queries chronological player ship switches directly against
// queryer for [startTimestamp, endTimestamp] without using the cache.
func QueryShipTimeline(queryer db.Queryer, startTimestamp, endTimestamp string) ([]ShipSwitch, error) {
	var switches []ShipSwitch
	if startTimestamp != "" {
		seedQuery := `SELECT timestamp, event, raw_json FROM journal_events
			WHERE event IN ('LoadGame','ShipyardSwap') AND timestamp <= ? AND ` + playerShipSQLFilter + ` AND ` + journal.CmdrFilterSQL + `
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
	query += ` AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp, id`
	return appendQueriedShipSwitches(queryer, switches, query, queryArgs...)
}

// QueryJumpTimestamps loads chronological hyperspace jump timestamps in
// [afterTimestamp, untilTimestamp], merging FSDJump rows with recovered.
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
	query += ` AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp, id`
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
// ShipyardSwap), ignoring on-foot suits, SRVs, fighters, and landers.
func QueryCurrentShip(queryer db.Queryer) (string, error) {
	query := `SELECT event, raw_json FROM journal_events
		WHERE event IN ('LoadGame','ShipyardSwap') AND ` + playerShipSQLFilter + ` AND ` + journal.CmdrFilterSQL + `
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
			if json.Unmarshal([]byte(rawJSON), &loadGame) == nil && journal.IsPlayerShip(loadGame.Ship) {
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
		WHERE event = 'LoadGame' AND json_extract(raw_json, '$.ShipID') = ? AND ` + playerShipSQLFilter + ` AND ` + journal.CmdrFilterSQL + `
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
		if json.Unmarshal([]byte(rawJSON), &loadGameEvent) != nil || !journal.IsPlayerShip(loadGameEvent.Ship) {
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

