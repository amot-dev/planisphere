package expedition

import (
	"database/sql"
	"encoding/json"
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
	NextFCStopRowID    int64        `json:"nextFcStopRowId"`
	WaypointCount      int          `json:"waypointCount"`
	VisitedCount       int          `json:"visitedCount"`
	PlannedLy          float64      `json:"plannedLy"`
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
	shipCacheMap     = make(map[string][]shipState)
	lastMaxJournalID int64 = -1
)

// InvalidateShipCache clears the cached ship timeline states.
func InvalidateShipCache() {
	shipCacheMu.Lock()
	lastMaxJournalID = -1
	shipCacheMap = make(map[string][]shipState)
	shipCacheMu.Unlock()
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
		for rows.Next() {
			var timestamp, eventType, rawJSON string
			if scanErr := rows.Scan(&timestamp, &eventType, &rawJSON); scanErr != nil {
				rows.Close()
				return nil, scanErr
			}
			if state, ok := parseShipState(timestamp, eventType, rawJSON); ok {
				states = append(states, state)
			}
		}
		if closeErr := rows.Close(); closeErr != nil {
			return nil, closeErr
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
	var currentPos *ed.Vec3
	if currentWaypoint != nil && currentWaypoint.HasCoords {
		coords, _ := currentWaypoint.Coords()
		currentPos = &coords
	}
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
			dist := ed.Distance(startPos, endPos)
			rowViewItem.DistFromPrevLy = &dist
			planView.PlannedLy += dist
			if rowItem.VisitedAt == "" {
				planView.RemainingLy += dist
			}
		}
		if rowItem.VisitedAt == "" && currentPos != nil && rowItem.HasCoords {
			endPos, _ := rowItem.Coords()
			dist := ed.Distance(*currentPos, endPos)
			rowViewItem.DistFromCurrentLy = &dist
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
	planView, planError := BuildPlan(conn, targetRow.ExpeditionID, nil)
	if planError != nil {
		return WaypointDetails{}, planError
	}
	var selfView *RowView
	var prevView *RowView
	var currentView *RowView
	for i := range planView.Rows {
		rowViewItem := &planView.Rows[i]
		if rowViewItem.Row.RowType != RowWaypoint {
			continue
		}
		if rowViewItem.Row.ID == planView.CurrentRowID {
			currentView = rowViewItem
		}
		if rowViewItem.Row.ID == rowID {
			selfView = rowViewItem
		} else if selfView == nil {
			prevView = rowViewItem
		}
	}
	if selfView == nil {
		return WaypointDetails{}, nil
	}
	waypointDetails := WaypointDetails{RowID: rowID, Number: selfView.Number}
	if prevView != nil {
		waypointDetails.PrevSystem = prevView.Row.SystemName
	}
	engine := stats.NewEngine(conn)

	if selfView.Row.VisitedAt == "" {
		waypointDetails.Kind = "future"
		waypointDetails.EstDistFromPrevLy = selfView.DistFromPrevLy
		waypointDetails.EstDistFromCurrentLy = selfView.DistFromCurrentLy
		if currentView != nil {
			waypointDetails.CurrentSystem = currentView.Row.SystemName
		}
		waypointDetails.MissingCoords = !selfView.Row.HasCoords || (prevView != nil && !prevView.Row.HasCoords)
		return waypointDetails, nil
	}

	if selfView.Number == 1 {
		waypointDetails.Kind = "origin"
		waypointDetails.To, waypointDetails.ToGalactic = selfView.Row.VisitedAt, ed.ToGalactic(selfView.Row.VisitedAt)
		var first sql.NullString
		_ = conn.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE `+journal.CmdrFilterSQL).Scan(&first)
		waypointDetails.From, waypointDetails.FromGalactic = first.String, ed.ToGalactic(first.String)
		var computeError error
		waypointDetails.Metrics, computeError = engine.Compute(stats.Range{After: "", Until: selfView.Row.VisitedAt}, nil)
		if computeError != nil {
			return waypointDetails, computeError
		}
	} else {
		waypointDetails.Kind = "leg"
		// The leg starts at the visit of the waypoint visited immediately before this one.
		afterTimestamp := ""
		for i := range planView.Rows {
			rowViewItem := planView.Rows[i]
			if rowViewItem.Row.RowType == RowWaypoint && rowViewItem.Row.VisitedAt != "" && rowViewItem.Row.VisitedAt < selfView.Row.VisitedAt && rowViewItem.Row.VisitedAt > afterTimestamp {
				afterTimestamp = rowViewItem.Row.VisitedAt
				waypointDetails.PrevSystem = rowViewItem.Row.SystemName
			}
		}
		if afterTimestamp == "" {
			afterTimestamp = planView.Expedition.StartTimestamp
		}
		waypointDetails.From, waypointDetails.FromGalactic = afterTimestamp, ed.ToGalactic(afterTimestamp)
		waypointDetails.To, waypointDetails.ToGalactic = selfView.Row.VisitedAt, ed.ToGalactic(selfView.Row.VisitedAt)
		var computeError error
		waypointDetails.Metrics, computeError = engine.Compute(stats.Range{After: afterTimestamp, Until: selfView.Row.VisitedAt}, nil)
		if computeError != nil {
			return waypointDetails, computeError
		}
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
