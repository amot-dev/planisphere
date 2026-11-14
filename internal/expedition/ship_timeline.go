package expedition

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
)

// shipState is one ship switch.
type shipState struct {
	timestamp string
	id        int64
	name      string
}

var (
	shipCacheMu      sync.Mutex
	shipCacheMap           = make(map[string][]shipState)
	lastMaxJournalID int64 = -1
)

// InvalidateShipCache clears the cached ship timeline states and origin metrics.
func InvalidateShipCache() {
	shipCacheMu.Lock()
	lastMaxJournalID = -1
	shipCacheMap = make(map[string][]shipState)
	shipCacheMu.Unlock()
	InvalidateOriginMetricsCache()
}

func parseShipState(timestamp, eventType, rawJSON string) (shipState, bool) {
	var state shipState
	state.timestamp = timestamp
	switch eventType {
	case "LoadGame":
		var loadGameEvent journal.LoadGameEvent
		if json.Unmarshal([]byte(rawJSON), &loadGameEvent) != nil || loadGameEvent.Ship == "" || !isShip(loadGameEvent.Ship) {
			return shipState{}, false
		}
		state.id, state.name = loadGameEvent.ShipID, ed.ShipDisplayName(loadGameEvent.Ship, loadGameEvent.ShipLocalised)
	case "ShipyardSwap":
		var swapEvent journal.ShipyardSwapEvent
		if json.Unmarshal([]byte(rawJSON), &swapEvent) != nil || swapEvent.ShipType == "" {
			return shipState{}, false
		}
		state.id, state.name = swapEvent.ShipID, ed.ShipDisplayName(swapEvent.ShipType, swapEvent.ShipTypeLocalised)
	default:
		return shipState{}, false
	}
	return state, true
}

func appendQueriedShipStates(queryer db.Queryer, states []shipState, query string, queryArgs ...any) ([]shipState, error) {
	rows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	for rows.Next() {
		var timestamp, eventType, rawJSON string
		if scanError := rows.Scan(&timestamp, &eventType, &rawJSON); scanError != nil {
			return nil, scanError
		}
		if state, parsedOK := parseShipState(timestamp, eventType, rawJSON); parsedOK {
			states = append(states, state)
		}
	}
	return states, rows.Err()
}

// fetchShipStates clamps timeline scans to the expedition window plus the latest active ship beforehand.
func fetchShipStates(queryer db.Queryer, startTimestamp, endTimestamp string) ([]shipState, error) {
	var states []shipState

	if startTimestamp != "" {
		shipFilter := `(event = 'ShipyardSwap' OR (json_extract(raw_json, '$.Ship') IS NOT NULL AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%suit%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%buggy%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%srv%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%fighter%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%lander%'))`
		seedQuery := `SELECT timestamp, event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap') AND timestamp <= ? AND ` + shipFilter + ` AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp DESC, id DESC LIMIT 1`
		var seedError error
		states, seedError = appendQueriedShipStates(queryer, states, seedQuery, startTimestamp)
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
	return appendQueriedShipStates(queryer, states, query, queryArgs...)
}

func loadShipTimeline(queryer db.Queryer, startTimestamp, endTimestamp string) ([]shipState, error) {
	var maxJournalID int64
	queryError := queryer.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM journal_events`).Scan(&maxJournalID)
	if queryError != nil {
		// Non-standard queryer or table not available; bypass cache.
		return fetchShipStates(queryer, startTimestamp, endTimestamp)
	}

	cacheKey := fmt.Sprintf("%s|%s", startTimestamp, endTimestamp)

	shipCacheMu.Lock()
	if maxJournalID != lastMaxJournalID {
		shipCacheMap = make(map[string][]shipState)
		lastMaxJournalID = maxJournalID
	} else if cachedTimeline, ok := shipCacheMap[cacheKey]; ok {
		shipCacheMu.Unlock()
		return cachedTimeline, nil
	}
	shipCacheMu.Unlock()

	states, fetchError := fetchShipStates(queryer, startTimestamp, endTimestamp)
	if fetchError != nil {
		return nil, fetchError
	}

	shipCacheMu.Lock()
	if maxJournalID == lastMaxJournalID {
		shipCacheMap[cacheKey] = states
	}
	shipCacheMu.Unlock()

	return states, nil
}

// isShip filters out on-foot suits, SRVs, fighters, and ship-launched landers reported by LoadGame.
func isShip(internalName string) bool {
	lowercaseName := strings.ToLower(internalName)
	return !strings.Contains(lowercaseName, "suit") && !strings.Contains(lowercaseName, "buggy") &&
		!strings.Contains(lowercaseName, "srv") && !strings.Contains(lowercaseName, "fighter") &&
		!strings.Contains(lowercaseName, "lander")
}

func shipAt(timeline []shipState, timestamp string) *shipState {
	segmentIndex := sort.Search(len(timeline), func(i int) bool { return timeline[i].timestamp > timestamp }) - 1
	if segmentIndex < 0 {
		return nil
	}
	return &timeline[segmentIndex]
}

func loadJumpTimestamps(queryer db.Queryer, afterTimestamp, untilTimestamp string) ([]string, error) {
	query := `SELECT timestamp FROM journal_events WHERE event = 'FSDJump' AND timestamp >= ?`
	queryArgs := []any{afterTimestamp}
	if untilTimestamp != "" {
		query += ` AND timestamp <= ?`
		queryArgs = append(queryArgs, untilTimestamp)
	}
	query += ` AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp, id`
	rows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var timestamps []string
	for rows.Next() {
		var timestamp string
		if scanError := rows.Scan(&timestamp); scanError != nil {
			return nil, scanError
		}
		timestamps = append(timestamps, timestamp)
	}
	return timestamps, rows.Err()
}

func shipsUsedOnLeg(timeline []shipState, jumpTimestamps []string, afterTimestamp, untilTimestamp string) []Transition {
	var transitions []Transition
	seenShipNames := map[string]bool{}
	startIndex := sort.Search(len(jumpTimestamps), func(i int) bool { return jumpTimestamps[i] > afterTimestamp })
	for i := startIndex; i < len(jumpTimestamps); i++ {
		jumpTimestamp := jumpTimestamps[i]
		if jumpTimestamp > untilTimestamp {
			break
		}
		state := shipAt(timeline, jumpTimestamp)
		if state == nil || state.name == "" || seenShipNames[state.name] {
			continue
		}
		seenShipNames[state.name] = true
		transitions = append(transitions, Transition{
			Kind:       "ship",
			At:         jumpTimestamp,
			AtGalactic: ed.ToGalactic(jumpTimestamp),
			To:         state.name,
		})
	}
	return transitions
}

// LatestPlayerLocation queries the most recent coordinates and star system for the active commander.
func LatestPlayerLocation(queryer db.Queryer) (*ed.Vec3, string, error) {
	var (
		starX, starY, starZ sql.NullFloat64
		systemName          sql.NullString
	)
	query := `SELECT star_x, star_y, star_z, system_name FROM (
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'FSDJump'
			  AND ` + journal.CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'Location'
			  AND ` + journal.CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'CarrierJump'
			  AND ` + journal.CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
	) ORDER BY timestamp DESC, id DESC LIMIT 1`
	queryError := queryer.QueryRow(query).Scan(&starX, &starY, &starZ, &systemName)
	if queryError != nil {
		if errors.Is(queryError, sql.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", queryError
	}
	if !starX.Valid || !starY.Valid || !starZ.Valid {
		return nil, systemName.String, nil
	}
	return &ed.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}, systemName.String, nil
}

var (
	visitCacheMu      sync.Mutex
	visitCacheQueryer db.Queryer
	visitCacheMap     = make(map[string]int)
)

// InvalidateVisitCache clears cached per-system journal visit counts.
func InvalidateVisitCache() {
	visitCacheMu.Lock()
	visitCacheQueryer = nil
	visitCacheMap = make(map[string]int)
	visitCacheMu.Unlock()
}

func normalizeSystemKey(systemName string) string {
	return strings.ToLower(strings.Join(strings.Fields(systemName), " "))
}

func queryUncachedSystemVisits(queryer db.Queryer, missingNames []string) (map[string]int, error) {
	predicates := make([]string, len(missingNames))
	queryArgs := make([]any, len(missingNames))
	for i, name := range missingNames {
		predicates[i] = `system_name = ? COLLATE NOCASE`
		queryArgs[i] = name
	}
	query := `SELECT system_name, COUNT(*), COALESCE(SUM(CASE WHEN event IN ('FSDJump', 'CarrierJump') THEN 1 ELSE 0 END), 0)
		FROM journal_events
		WHERE (` + strings.Join(predicates, " OR ") + `)
		  AND (event IN ('FSDJump', 'Location', 'CarrierJump') OR star_x IS NOT NULL)
		  AND ` + journal.CmdrFilterSQL + `
		GROUP BY LOWER(TRIM(system_name))`
	rows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	totalBySystem := make(map[string]int, len(missingNames))
	jumpsBySystem := make(map[string]int, len(missingNames))
	for rows.Next() {
		var matchedSystem string
		var totalEvents, jumpEvents int
		if scanError := rows.Scan(&matchedSystem, &totalEvents, &jumpEvents); scanError != nil {
			return nil, scanError
		}
		normalizedName := normalizeSystemKey(matchedSystem)
		totalBySystem[normalizedName] += totalEvents
		jumpsBySystem[normalizedName] += jumpEvents
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(missingNames))
	for _, name := range missingNames {
		if totalBySystem[name] > 0 {
			visitCount := jumpsBySystem[name]
			if visitCount == 0 {
				visitCount = 1
			}
			counts[name] = visitCount
		} else {
			counts[name] = 0
		}
	}
	return counts, nil
}

func loadSystemVisitCounts(queryer db.Queryer, rows []Row) map[string]int {
	uniqueNames := make([]string, 0, len(rows))
	seenNames := make(map[string]bool, len(rows))
	for _, rowItem := range rows {
		if !rowItem.IsWaypoint() {
			continue
		}
		normalizedName := normalizeSystemKey(rowItem.SystemName)
		if normalizedName != "" && !seenNames[normalizedName] {
			seenNames[normalizedName] = true
			uniqueNames = append(uniqueNames, normalizedName)
		}
	}
	if len(uniqueNames) == 0 {
		return nil
	}

	visitCacheMu.Lock()
	if visitCacheQueryer != queryer {
		visitCacheQueryer = queryer
		visitCacheMap = make(map[string]int)
	}
	var missingNames []string
	visitCounts := make(map[string]int, len(uniqueNames))
	for _, normalizedName := range uniqueNames {
		if cachedCount, exists := visitCacheMap[normalizedName]; exists {
			visitCounts[normalizedName] = cachedCount
		} else {
			missingNames = append(missingNames, normalizedName)
		}
	}
	visitCacheMu.Unlock()

	if len(missingNames) == 0 {
		return visitCounts
	}

	queriedCounts, queryError := queryUncachedSystemVisits(queryer, missingNames)
	if queryError != nil {
		return visitCounts
	}

	visitCacheMu.Lock()
	if visitCacheQueryer == queryer {
		for normalizedName, visitCount := range queriedCounts {
			visitCacheMap[normalizedName] = visitCount
		}
	}
	visitCacheMu.Unlock()

	for normalizedName, visitCount := range queriedCounts {
		visitCounts[normalizedName] = visitCount
	}
	return visitCounts
}

