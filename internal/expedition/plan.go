package expedition

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
	"planisphere/internal/stats"
)

// Row visit states.
const (
	VisitVisited = "visited"
	VisitCurrent = "current"
	VisitFuture  = "future"
)

// Transition is a ship or mode switch that happened during a leg.
type Transition struct {
	Kind       string `json:"kind"` // "mode" | "ship"
	At         string `json:"at"`
	AtGalactic string `json:"atGalactic"`
	From       string `json:"from"`
	To         string `json:"to"`
	FromColor  string `json:"fromColor"`
	ToColor    string `json:"toColor"`
}

// GECRef links a system to an EDAstro Galactic Exploration Catalog entry.
type GECRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Count int    `json:"count"` // number of GEC POIs in the system
}

// RowView is a row enriched for display.
type RowView struct {
	Row               Row          `json:"row"`
	Number            int          `json:"number"` // waypoint # (0 for region headers)
	RegionName        string       `json:"regionName"`
	DetectedRegionID  int          `json:"detectedRegionId"` // region containing the coordinates
	DetectedRegion    string       `json:"detectedRegion"`
	VisitState        string       `json:"visitState"`
	VisitedAtGalactic string       `json:"visitedAtGalactic"`
	DistFromPrevLy    *float64     `json:"distFromPrevLy,omitempty"`
	DistFromCurrentLy *float64     `json:"distFromCurrentLy,omitempty"`
	IsNextWaypoint    bool         `json:"isNextWaypoint,omitempty"`
	Transitions       []Transition `json:"transitions"` // ships flown during the leg arriving at this waypoint
	LegModeID         int64        `json:"legModeId"`
	LegModeName       string       `json:"legModeName"`
	LegModeColor      string       `json:"legModeColor"`
	GEC               *GECRef      `json:"gec,omitempty"`
}

// PlanView is the full planner sheet.
type PlanView struct {
	Expedition         Expedition   `json:"expedition"`
	StartGalactic      string       `json:"startGalactic"`
	EndGalactic        string       `json:"endGalactic"`
	Rows               []RowView    `json:"rows"`
	CurrentRowID       int64        `json:"currentRowId"`
	NextWaypointRowID  int64        `json:"nextWaypointRowId,omitempty"`
	NextFCStopRowID    int64        `json:"nextFcStopRowId"`
	PlayerSystem       string       `json:"playerSystem,omitempty"`
	WaypointCount      int          `json:"waypointCount"`
	VisitedCount       int          `json:"visitedCount"`
	PlannedLy          float64      `json:"plannedLy"`
	CoveredLy          float64      `json:"coveredLy"`
	RemainingLy        float64      `json:"remainingLy"`
	PendingTransitions []Transition `json:"pendingTransitions"` // ships flown since the current waypoint
	ActiveModeID       int64        `json:"activeModeId"`
	ActiveModeName     string       `json:"activeModeName"`
	ActiveModeColor    string       `json:"activeModeColor"`
	Modes              []modes.Mode `json:"modes"`
}

// GECLookup resolves GEC entries for a system.
type GECLookup func(systemName string, systemAddress int64) *GECRef

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

	originMetricsMu    sync.Mutex
	originMetricsCache = make(map[string]stats.Metrics)
)

// InvalidateOriginMetricsCache clears the cached origin waypoint metrics.
func InvalidateOriginMetricsCache() {
	originMetricsMu.Lock()
	originMetricsCache = make(map[string]stats.Metrics)
	originMetricsMu.Unlock()
}

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

// fetchShipStates clamps timeline scans to the expedition window plus the latest active ship beforehand.
func fetchShipStates(queryer db.Queryer, startTimestamp, endTimestamp string) ([]shipState, error) {
	var states []shipState

	if startTimestamp != "" {
		shipFilter := `(event = 'ShipyardSwap' OR (json_extract(raw_json, '$.Ship') IS NOT NULL AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%suit%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%buggy%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%srv%' AND LOWER(json_extract(raw_json, '$.Ship')) NOT LIKE '%fighter%'))`
		rows, err := queryer.Query(`SELECT timestamp, event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap') AND timestamp <= ? AND `+shipFilter+` AND `+journal.CmdrFilterSQL+` ORDER BY timestamp DESC, id DESC LIMIT 1`, startTimestamp)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var timestamp, eventType, rawJSON string
			if scanErr := rows.Scan(&timestamp, &eventType, &rawJSON); scanErr != nil {
				return nil, scanErr
			}
			if state, ok := parseShipState(timestamp, eventType, rawJSON); ok {
				states = append(states, state)
			}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return nil, rowsErr
		}
	}

	query := `SELECT timestamp, event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap')`
	var args []any
	if startTimestamp != "" {
		query += ` AND timestamp > ?`
		args = append(args, startTimestamp)
	}
	if endTimestamp != "" {
		query += ` AND timestamp <= ?`
		args = append(args, endTimestamp)
	}
	query += ` AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp, id`
	rows, err := queryer.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var timestamp, eventType, rawJSON string
		if scanErr := rows.Scan(&timestamp, &eventType, &rawJSON); scanErr != nil {
			return nil, scanErr
		}
		if state, ok := parseShipState(timestamp, eventType, rawJSON); ok {
			states = append(states, state)
		}
	}
	return states, rows.Err()
}

func loadShipTimeline(queryer db.Queryer, startTimestamp, endTimestamp string) ([]shipState, error) {
	var maxID int64
	err := queryer.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM journal_events`).Scan(&maxID)
	if err != nil {
		// Non-standard queryer or table not available; bypass cache
		return fetchShipStates(queryer, startTimestamp, endTimestamp)
	}

	cacheKey := fmt.Sprintf("%s|%s", startTimestamp, endTimestamp)

	shipCacheMu.Lock()
	if maxID != lastMaxJournalID {
		shipCacheMap = make(map[string][]shipState)
		lastMaxJournalID = maxID
	} else if cached, ok := shipCacheMap[cacheKey]; ok {
		shipCacheMu.Unlock()
		return cached, nil
	}
	shipCacheMu.Unlock()

	states, err := fetchShipStates(queryer, startTimestamp, endTimestamp)
	if err != nil {
		return nil, err
	}

	shipCacheMu.Lock()
	if maxID == lastMaxJournalID {
		shipCacheMap[cacheKey] = states
	}
	shipCacheMu.Unlock()

	return states, nil
}

// isShip filters out on-foot suits, SRVs and fighters reported by LoadGame.
func isShip(internal string) bool {
	lower := strings.ToLower(internal)
	return !strings.Contains(lower, "suit") && !strings.Contains(lower, "buggy") && !strings.Contains(lower, "srv") &&
		!strings.Contains(lower, "fighter")
}

func shipAt(timeline []shipState, timestamp string) *shipState {
	segmentIndex := sort.Search(len(timeline), func(i int) bool { return timeline[i].timestamp > timestamp }) - 1
	if segmentIndex < 0 {
		return nil
	}
	return &timeline[segmentIndex]
}

func loadJumpTimestamps(queryer db.Queryer, after, until string) ([]string, error) {
	query := `SELECT timestamp FROM journal_events WHERE event = 'FSDJump' AND timestamp >= ?`
	args := []any{after}
	if until != "" {
		query += ` AND timestamp <= ?`
		args = append(args, until)
	}
	query += ` AND ` + journal.CmdrFilterSQL + ` ORDER BY timestamp, id`
	rows, queryError := queryer.Query(query, args...)
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

func shipsUsedOnLeg(timeline []shipState, jumps []string, after, until string) []Transition {
	var transitions []Transition
	seen := map[string]bool{}
	startIndex := sort.Search(len(jumps), func(i int) bool { return jumps[i] > after })
	for i := startIndex; i < len(jumps); i++ {
		jumpTimestamp := jumps[i]
		if jumpTimestamp > until {
			break
		}
		state := shipAt(timeline, jumpTimestamp)
		if state == nil || state.name == "" || seen[state.name] {
			continue
		}
		seen[state.name] = true
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
	err := queryer.QueryRow(query).Scan(&starX, &starY, &starZ, &systemName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
	}
	if !starX.Valid || !starY.Valid || !starZ.Valid {
		return nil, systemName.String, nil
	}
	return &ed.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}, systemName.String, nil
}

// BuildPlan assembles the planner view for an expedition.
func BuildPlan(queryer db.Queryer, expeditionID int64, gecLookup GECLookup) (PlanView, error) {
	expeditionRecord, getExpeditionError := Get(queryer, expeditionID)
	if getExpeditionError != nil {
		return PlanView{}, getExpeditionError
	}
	rows, rowsError := ListRows(queryer, expeditionID)
	if rowsError != nil {
		return PlanView{}, rowsError
	}
	planView := PlanView{Expedition: expeditionRecord, StartGalactic: ed.ToGalactic(expeditionRecord.StartTimestamp), EndGalactic: ed.ToGalactic(expeditionRecord.EndTimestamp)}

	// Current waypoint = visited waypoint with the latest visit time.
	var currentWaypoint *Row
	for i := range rows {
		rowItem := &rows[i]
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" && (currentWaypoint == nil || rowItem.VisitedAt > currentWaypoint.VisitedAt) {
			currentWaypoint = rowItem
		}
	}
	if currentWaypoint != nil {
		planView.CurrentRowID = currentWaypoint.ID
	}

	playerPos, playerSystem, playerLocErr := LatestPlayerLocation(queryer)
	if playerLocErr != nil && !errors.Is(playerLocErr, sql.ErrNoRows) {
		return planView, playerLocErr
	}
	if playerPos == nil && currentWaypoint != nil && currentWaypoint.HasCoords {
		coords, _ := currentWaypoint.Coords()
		playerPos = &coords
		playerSystem = currentWaypoint.SystemName
	}
	planView.PlayerSystem = playerSystem

	var nextWaypointRowID int64
	if expeditionRecord.Status == StatusActive {
		for _, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt == "" {
				nextWaypointRowID = rowItem.ID
				break
			}
		}
	}
	planView.NextWaypointRowID = nextWaypointRowID

	var shipTimeline []shipState
	var jumpTimestamps []string
	modeTimeline, modeError := modes.LoadTimeline(queryer)
	if modeError != nil {
		return planView, modeError
	}
	modeList, listError := modes.List(queryer)
	if listError != nil {
		return planView, listError
	}
	planView.Modes = modeList
	modeByID := map[int64]modes.Mode{}
	for _, modeItem := range modeList {
		modeByID[modeItem.ID] = modeItem
	}
	defaultModeID := modes.DefaultFallbackModeID(queryer)
	resolveMode := func(id int64) (int64, string, string) {
		if modeItem, ok := modeByID[id]; ok {
			return modeItem.ID, modeItem.Name, modeItem.Color
		}
		if modeItem, ok := modeByID[defaultModeID]; ok {
			return modeItem.ID, modeItem.Name, modeItem.Color
		}
		if len(modeList) > 0 {
			return modeList[0].ID, modeList[0].Name, modeList[0].Color
		}
		return 0, "Travel", "#FF7100"
	}

	if expeditionRecord.StartTimestamp != "" {
		var shipError, jumpError error
		if shipTimeline, shipError = loadShipTimeline(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp); shipError != nil {
			return planView, shipError
		}
		if jumpTimestamps, jumpError = loadJumpTimestamps(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp); jumpError != nil {
			return planView, jumpError
		}
	}

	// Visited waypoints sorted by visit time for leg transitions.
	prevVisit := map[int64]string{}
	var visited []Row
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" {
			visited = append(visited, rowItem)
		}
	}
	sort.SliceStable(visited, func(firstIndex, secondIndex int) bool {
		return visited[firstIndex].VisitedAt < visited[secondIndex].VisitedAt
	})
	for i := 1; i < len(visited); i++ {
		prevVisit[visited[i].ID] = visited[i-1].VisitedAt
	}

	waypointNumber := 0
	var prevWaypoint *Row
	for i := range rows {
		rowItem := rows[i]
		rowViewItem := RowView{Row: rowItem, Transitions: []Transition{}}
		if rowItem.RowType == RowRegion {
			if region, ok := ed.RegionByID(rowItem.RegionID); ok {
				rowViewItem.RegionName = region.Name
			}
			planView.Rows = append(planView.Rows, rowViewItem)
			continue
		}
		waypointNumber++
		planView.WaypointCount++
		rowViewItem.Number = waypointNumber
		if rowItem.FCStop && !rowItem.FCArrived && planView.NextFCStopRowID == 0 {
			planView.NextFCStopRowID = rowItem.ID
		}
		if rowItem.HasCoords {
			if region, ok := ed.RegionAt(rowItem.X, rowItem.Y, rowItem.Z); ok {
				rowViewItem.DetectedRegionID, rowViewItem.DetectedRegion = region.ID, region.Name
			}
		}
		switch {
		case currentWaypoint != nil && rowItem.ID == currentWaypoint.ID:
			rowViewItem.VisitState = VisitCurrent
		case rowItem.VisitedAt != "":
			rowViewItem.VisitState = VisitVisited
		default:
			rowViewItem.VisitState = VisitFuture
		}
		if rowItem.VisitedAt != "" {
			planView.VisitedCount++
			rowViewItem.VisitedAtGalactic = ed.ToGalactic(rowItem.VisitedAt)
			if after, ok := prevVisit[rowItem.ID]; ok && shipTimeline != nil {
				rowViewItem.Transitions = shipsUsedOnLeg(shipTimeline, jumpTimestamps, after, rowItem.VisitedAt)
				modeID := rowItem.LegModeID
				if modeID == 0 && modeTimeline != nil {
					modeID = modeTimeline.ModeAt(rowItem.VisitedAt)
				}
				rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = resolveMode(modeID)
			}
		} else if waypointNumber > 1 && rowItem.LegModeID > 0 {
			rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = resolveMode(rowItem.LegModeID)
		}
		if prevWaypoint != nil && prevWaypoint.HasCoords && rowItem.HasCoords {
			startPos, _ := prevWaypoint.Coords()
			endPos, _ := rowItem.Coords()
			legDistanceLy := ed.Distance(startPos, endPos)
			rowViewItem.DistFromPrevLy = &legDistanceLy
			planView.PlannedLy += legDistanceLy
			if rowItem.VisitedAt != "" {
				planView.CoveredLy += legDistanceLy
			}
		}
		if nextWaypointRowID > 0 && rowItem.ID == nextWaypointRowID {
			rowViewItem.IsNextWaypoint = true
			if playerPos != nil && rowItem.HasCoords {
				endPos, _ := rowItem.Coords()
				playerDistanceLy := ed.Distance(*playerPos, endPos)
				rowViewItem.DistFromCurrentLy = &playerDistanceLy
			} else if rowViewItem.DistFromPrevLy != nil {
				rowViewItem.DistFromCurrentLy = rowViewItem.DistFromPrevLy
			}
		}
		// Accumulate remaining light years:
		// When planning, accumulate distance from previous waypoint for all unvisited waypoints.
		// When active, use distance from player (falling back to previous waypoint) for the next waypoint,
		// and distance from previous waypoint for subsequent unvisited legs.
		if rowItem.VisitedAt == "" {
			switch expeditionRecord.Status {
			case StatusPlanning:
				if rowViewItem.DistFromPrevLy != nil {
					planView.RemainingLy += *rowViewItem.DistFromPrevLy
				}
			case StatusActive:
				if rowViewItem.IsNextWaypoint {
					if rowViewItem.DistFromCurrentLy != nil {
						planView.RemainingLy += *rowViewItem.DistFromCurrentLy
					} else if rowViewItem.DistFromPrevLy != nil {
						planView.RemainingLy += *rowViewItem.DistFromPrevLy
					}
				} else if rowViewItem.DistFromPrevLy != nil {
					planView.RemainingLy += *rowViewItem.DistFromPrevLy
				}
			}
		}
		if gecLookup != nil && rowItem.SystemName != "" {
			rowViewItem.GEC = gecLookup(rowItem.SystemName, rowItem.SystemAddress)
		}
		rowCopy := rowItem
		prevWaypoint = &rowCopy
		planView.Rows = append(planView.Rows, rowViewItem)
	}
	if planView.Rows == nil {
		planView.Rows = []RowView{}
	}
	planView.PendingTransitions = []Transition{}
	if currentWaypoint != nil && shipTimeline != nil {
		until := expeditionRecord.EndTimestamp
		if until == "" {
			until = ed.NowISO()
		}
		planView.PendingTransitions = shipsUsedOnLeg(shipTimeline, jumpTimestamps, currentWaypoint.VisitedAt, until)
		modeID := modeTimeline.ModeAt(until)
		planView.ActiveModeID, planView.ActiveModeName, planView.ActiveModeColor = resolveMode(modeID)
	} else {
		planView.ActiveModeID, planView.ActiveModeName, planView.ActiveModeColor = resolveMode(modeTimeline.ModeAt(ed.NowISO()))
	}
	return planView, nil
}

func addOneSecond(isoTimestamp string) string {
	parsedTime, parseError := ed.ParseISO(isoTimestamp)
	if parseError != nil {
		return isoTimestamp
	}
	return parsedTime.Add(time.Second).UTC().Format("2006-01-02T15:04:05Z")
}

// SetLegMode updates the single mode assigned to the leg arriving at rowID.
// When the waypoint has been visited (after a previous visited waypoint), it
// also updates the corresponding time window (prevVisit, rowVisit] in mode_windows
// so filtered expedition and all-time stats immediately reflect the change.
func SetLegMode(transaction *sql.Tx, rowID int64, modeID int64) (int64, error) {
	targetRow, rowError := GetRow(transaction, rowID)
	if rowError != nil {
		return 0, rowError
	}
	if _, updateError := UpdateRow(transaction, RowPatch{ID: rowID, LegModeID: &modeID}); updateError != nil {
		return 0, updateError
	}
	if targetRow.VisitedAt != "" {
		rows, listError := ListRows(transaction, targetRow.ExpeditionID)
		if listError != nil {
			return targetRow.ExpeditionID, listError
		}
		prevTimestamp := ""
		for _, candidateRow := range rows {
			if candidateRow.IsWaypoint() && candidateRow.VisitedAt != "" && candidateRow.VisitedAt < targetRow.VisitedAt && candidateRow.VisitedAt > prevTimestamp {
				prevTimestamp = candidateRow.VisitedAt
			}
		}
		if prevTimestamp != "" {
			startISO := addOneSecond(prevTimestamp)
			endISO := addOneSecond(targetRow.VisitedAt)
			if endISO > startISO {
				if windowError := modes.SetWindow(transaction, 0, modeID, startISO, endISO); windowError != nil {
					return targetRow.ExpeditionID, windowError
				}
			}
		}
	}
	return targetRow.ExpeditionID, nil
}

// SetActiveLegMode sets the mode for the current in-progress leg (from the
// latest visited waypoint's timestamp forward).
func SetActiveLegMode(transaction *sql.Tx, expeditionID int64, modeID int64) error {
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return listError
	}
	latestTimestamp := ""
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" && rowItem.VisitedAt > latestTimestamp {
			latestTimestamp = rowItem.VisitedAt
		}
	}
	if latestTimestamp != "" {
		return modes.SetWindow(transaction, 0, modeID, addOneSecond(latestTimestamp), "")
	}
	return modes.Switch(transaction, modeID, "")
}

// WaypointDetails is the content of a waypoint's expandable drawer.
type WaypointDetails struct {
	RowID        int64         `json:"rowId"`
	Kind         string        `json:"kind"` // "origin" (waypoint #1), "leg" (visited N>1), "future"
	Number       int           `json:"number"`
	From         string        `json:"from"`
	To           string        `json:"to"`
	FromGalactic string        `json:"fromGalactic"`
	ToGalactic   string        `json:"toGalactic"`
	Elapsed      string        `json:"elapsed"`
	PrevSystem   string        `json:"prevSystem"`
	Metrics      stats.Metrics `json:"metrics"`
	Realization  stats.RealizationBreakdown `json:"realization"`

	EstDistFromPrevLy    *float64 `json:"estDistFromPrevLy,omitempty"`
	EstDistFromCurrentLy *float64 `json:"estDistFromCurrentLy,omitempty"`
	CurrentSystem        string   `json:"currentSystem"`
	MissingCoords        bool     `json:"missingCoords"`
}

// Details computes the drawer content for a waypoint row.
func Details(conn *sql.DB, rowID int64) (WaypointDetails, error) {
	targetRow, rowError := GetRow(conn, rowID)
	if rowError != nil {
		return WaypointDetails{}, rowError
	}
	if !targetRow.IsWaypoint() {
		return WaypointDetails{}, nil
	}

	rows, listError := ListRows(conn, targetRow.ExpeditionID)
	if listError != nil {
		return WaypointDetails{}, listError
	}

	var (
		waypointNumber  int
		selfNumber      int
		prevWaypoint    *Row
		currentWaypoint *Row
	)

	for i := range rows {
		rowItem := &rows[i]
		if !rowItem.IsWaypoint() {
			continue
		}
		waypointNumber++
		if rowItem.ID == rowID {
			selfNumber = waypointNumber
		} else if selfNumber == 0 {
			prevCopy := *rowItem
			prevWaypoint = &prevCopy
		}
		if rowItem.VisitedAt != "" && (currentWaypoint == nil || rowItem.VisitedAt > currentWaypoint.VisitedAt) {
			currentWaypoint = rowItem
		}
	}

	if selfNumber == 0 {
		return WaypointDetails{}, nil
	}

	waypointDetails := WaypointDetails{
		RowID:  rowID,
		Number: selfNumber,
	}
	if prevWaypoint != nil {
		waypointDetails.PrevSystem = prevWaypoint.SystemName
	}

	if targetRow.VisitedAt == "" {
		waypointDetails.Kind = "future"
		if prevWaypoint != nil && prevWaypoint.HasCoords && targetRow.HasCoords {
			startPos, _ := prevWaypoint.Coords()
			endPos, _ := targetRow.Coords()
			dist := ed.Distance(startPos, endPos)
			waypointDetails.EstDistFromPrevLy = &dist
		}
		if currentWaypoint != nil && currentWaypoint.HasCoords && targetRow.HasCoords {
			currentPos, _ := currentWaypoint.Coords()
			endPos, _ := targetRow.Coords()
			dist := ed.Distance(currentPos, endPos)
			waypointDetails.EstDistFromCurrentLy = &dist
		}
		if currentWaypoint != nil {
			waypointDetails.CurrentSystem = currentWaypoint.SystemName
		}
		waypointDetails.MissingCoords = !targetRow.HasCoords || (prevWaypoint != nil && !prevWaypoint.HasCoords)
		return waypointDetails, nil
	}

	engine := stats.NewEngine(conn)

	if selfNumber == 1 {
		waypointDetails.Kind = "origin"
		waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, ed.ToGalactic(targetRow.VisitedAt)
		var first sql.NullString
		_ = conn.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&first)
		waypointDetails.From, waypointDetails.FromGalactic = first.String, ed.ToGalactic(first.String)

		cacheKey := fmt.Sprintf("%d|%s|%s", targetRow.ExpeditionID, first.String, targetRow.VisitedAt)
		originMetricsMu.Lock()
		cached, ok := originMetricsCache[cacheKey]
		originMetricsMu.Unlock()
		if ok {
			waypointDetails.Metrics = cached
		} else {
			computed, computeError := engine.Compute(stats.Range{After: "", Until: targetRow.VisitedAt}, nil)
			if computeError != nil {
				return waypointDetails, computeError
			}
			waypointDetails.Metrics = computed
			originMetricsMu.Lock()
			originMetricsCache[cacheKey] = computed
			originMetricsMu.Unlock()
		}
		waypointDetails.Realization, _ = engine.ComputeRealization(stats.Range{After: "", Until: targetRow.VisitedAt}, nil)
	} else {
		waypointDetails.Kind = "leg"
		// The leg starts at the visit of the waypoint visited immediately before this one.
		afterTimestamp := ""
		for _, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt != "" && rowItem.VisitedAt < targetRow.VisitedAt && rowItem.VisitedAt > afterTimestamp {
				afterTimestamp = rowItem.VisitedAt
				waypointDetails.PrevSystem = rowItem.SystemName
			}
		}
		if afterTimestamp == "" {
			var startTs sql.NullString
			_ = conn.QueryRow(`SELECT COALESCE(start_timestamp, '') FROM expeditions WHERE id = ?`, targetRow.ExpeditionID).Scan(&startTs)
			afterTimestamp = startTs.String
		}
		waypointDetails.From, waypointDetails.FromGalactic = afterTimestamp, ed.ToGalactic(afterTimestamp)
		waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, ed.ToGalactic(targetRow.VisitedAt)
		var computeError error
		waypointDetails.Metrics, computeError = engine.Compute(stats.Range{After: afterTimestamp, Until: targetRow.VisitedAt}, nil)
		if computeError != nil {
			return waypointDetails, computeError
		}
		waypointDetails.Realization, _ = engine.ComputeRealization(stats.Range{After: afterTimestamp, Until: targetRow.VisitedAt}, nil)
	}
	if startTime, parseStartError := ed.ParseISO(waypointDetails.From); parseStartError == nil {
		if endTime, parseEndError := ed.ParseISO(waypointDetails.To); parseEndError == nil {
			waypointDetails.Elapsed = ed.DurationLabel(endTime.Sub(startTime))
		}
	}
	return waypointDetails, nil
}

// Elapsed returns a human label between two ISO timestamps (now when toTimestamp is empty).
// Uses rounded days and years without hours/minutes.
func Elapsed(fromTimestamp, toTimestamp string) string {
	startTime, parseError := ed.ParseISO(fromTimestamp)
	if parseError != nil {
		return ""
	}
	endTime := time.Now().UTC()
	if toTimestamp != "" {
		if parsedTime, parseError := ed.ParseISO(toTimestamp); parseError == nil {
			endTime = parsedTime
		}
	}
	return ed.DurationDaysLabel(endTime.Sub(startTime))
}
