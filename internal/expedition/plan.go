package expedition

import (
	"database/sql"
	"errors"
	"sort"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/modes"
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
	PlannedLy          float64      `json:"plannedLy"`          // sum of straight-line distances across all planned waypoint legs
	CoveredLy          float64      `json:"coveredLy"`          // sum of straight-line distances across visited waypoint legs
	RemainingLy        float64      `json:"remainingLy"`        // straight-line distance from current player position to next waypoint plus remaining unvisited legs
	PendingTransitions []Transition `json:"pendingTransitions"` // ships flown since the current waypoint
	ActiveModeID       int64        `json:"activeModeId"`
	ActiveModeName     string       `json:"activeModeName"`
	ActiveModeColor    string       `json:"activeModeColor"`
	Modes              []modes.Mode `json:"modes"`
}

// GECLookup resolves GEC entries for a system.
type GECLookup func(systemName string, systemAddress int64) *GECRef

type planBuildContext struct {
	expeditionRecord  Expedition
	currentWaypoint   *Row
	playerPosition    *ed.Vec3
	nextWaypointRowID int64
	shipTimeline      []shipState
	jumpTimestamps    []string
	modeTimeline      *modes.Timeline
	modeList          []modes.Mode
	modeByID          map[int64]modes.Mode
	defaultModeID     int64
	previousVisitByID map[int64]string
	gecLookup         GECLookup
}

func (buildCtx *planBuildContext) resolveMode(modeID int64) (int64, string, string) {
	if modeItem, ok := buildCtx.modeByID[modeID]; ok {
		return modeItem.ID, modeItem.Name, modeItem.Color
	}
	if modeItem, ok := buildCtx.modeByID[buildCtx.defaultModeID]; ok {
		return modeItem.ID, modeItem.Name, modeItem.Color
	}
	if len(buildCtx.modeList) > 0 {
		return buildCtx.modeList[0].ID, buildCtx.modeList[0].Name, buildCtx.modeList[0].Color
	}
	return 0, "Travel", "#FF7100"
}

// buildPreviousVisitMap indexes visited waypoints sorted by visit time for leg transitions.
func buildPreviousVisitMap(rows []Row) map[int64]string {
	previousVisitByID := map[int64]string{}
	var visitedRows []Row
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" {
			visitedRows = append(visitedRows, rowItem)
		}
	}
	sort.SliceStable(visitedRows, func(firstIndex, secondIndex int) bool {
		return visitedRows[firstIndex].VisitedAt < visitedRows[secondIndex].VisitedAt
	})
	for i := 1; i < len(visitedRows); i++ {
		previousVisitByID[visitedRows[i].ID] = visitedRows[i-1].VisitedAt
	}
	return previousVisitByID
}

func loadPlanBuildContext(queryer db.Queryer, expeditionRecord Expedition, rows []Row, gecLookup GECLookup, planView *PlanView) (*planBuildContext, error) {
	buildCtx := &planBuildContext{
		expeditionRecord:  expeditionRecord,
		modeByID:          map[int64]modes.Mode{},
		previousVisitByID: buildPreviousVisitMap(rows),
		gecLookup:         gecLookup,
	}

	// Current waypoint is the visited waypoint with the latest visit time.
	for i := range rows {
		rowItem := &rows[i]
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" && (buildCtx.currentWaypoint == nil || rowItem.VisitedAt > buildCtx.currentWaypoint.VisitedAt) {
			buildCtx.currentWaypoint = rowItem
		}
	}
	if buildCtx.currentWaypoint != nil {
		planView.CurrentRowID = buildCtx.currentWaypoint.ID
	}

	playerPosition, playerSystem, playerLocationError := LatestPlayerLocation(queryer)
	if playerLocationError != nil && !errors.Is(playerLocationError, sql.ErrNoRows) {
		return nil, playerLocationError
	}
	if playerPosition == nil && buildCtx.currentWaypoint != nil && buildCtx.currentWaypoint.HasCoords {
		coords, _ := buildCtx.currentWaypoint.Coords()
		playerPosition = &coords
		playerSystem = buildCtx.currentWaypoint.SystemName
	}
	buildCtx.playerPosition = playerPosition
	planView.PlayerSystem = playerSystem

	if expeditionRecord.Status == StatusActive {
		for _, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt == "" {
				buildCtx.nextWaypointRowID = rowItem.ID
				break
			}
		}
	}
	planView.NextWaypointRowID = buildCtx.nextWaypointRowID

	var modeError error
	if buildCtx.modeTimeline, modeError = modes.LoadTimeline(queryer); modeError != nil {
		return nil, modeError
	}
	var listError error
	if buildCtx.modeList, listError = modes.List(queryer); listError != nil {
		return nil, listError
	}
	planView.Modes = buildCtx.modeList
	for _, modeItem := range buildCtx.modeList {
		buildCtx.modeByID[modeItem.ID] = modeItem
	}
	buildCtx.defaultModeID = modes.DefaultFallbackModeID(queryer)

	if expeditionRecord.StartTimestamp != "" {
		var shipError, jumpError error
		if buildCtx.shipTimeline, shipError = loadShipTimeline(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp); shipError != nil {
			return nil, shipError
		}
		if buildCtx.jumpTimestamps, jumpError = loadJumpTimestamps(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp); jumpError != nil {
			return nil, jumpError
		}
	}

	return buildCtx, nil
}

// accumulateRemainingDistanceLy accumulates remaining light years across unvisited waypoints:
// When planning, accumulate distance from previous waypoint for all unvisited waypoints.
// When active, use distance from player (falling back to previous waypoint) for the next waypoint,
// and distance from previous waypoint for subsequent unvisited legs.
func accumulateRemainingDistanceLy(expeditionStatus string, rowItem Row, rowViewItem RowView) float64 {
	if rowItem.VisitedAt != "" {
		return 0
	}
	switch expeditionStatus {
	case StatusPlanning:
		if rowViewItem.DistFromPrevLy != nil {
			return *rowViewItem.DistFromPrevLy
		}
	case StatusActive:
		if rowViewItem.IsNextWaypoint {
			if rowViewItem.DistFromCurrentLy != nil {
				return *rowViewItem.DistFromCurrentLy
			}
			if rowViewItem.DistFromPrevLy != nil {
				return *rowViewItem.DistFromPrevLy
			}
		} else if rowViewItem.DistFromPrevLy != nil {
			return *rowViewItem.DistFromPrevLy
		}
	}
	return 0
}

func buildWaypointRowView(buildCtx *planBuildContext, rowItem Row, waypointNumber int, previousWaypoint *Row, planView *PlanView) RowView {
	rowViewItem := RowView{Row: rowItem, Number: waypointNumber, Transitions: []Transition{}}
	if rowItem.FCStop && !rowItem.FCArrived && planView.NextFCStopRowID == 0 {
		planView.NextFCStopRowID = rowItem.ID
	}
	if rowItem.HasCoords {
		if region, ok := ed.RegionAt(rowItem.X, rowItem.Y, rowItem.Z); ok {
			rowViewItem.DetectedRegionID, rowViewItem.DetectedRegion = region.ID, region.Name
		}
	}

	switch {
	case buildCtx.currentWaypoint != nil && rowItem.ID == buildCtx.currentWaypoint.ID:
		rowViewItem.VisitState = VisitCurrent
	case rowItem.VisitedAt != "":
		rowViewItem.VisitState = VisitVisited
	default:
		rowViewItem.VisitState = VisitFuture
	}

	if rowItem.VisitedAt != "" {
		planView.VisitedCount++
		rowViewItem.VisitedAtGalactic = ed.ToGalactic(rowItem.VisitedAt)
		if afterTimestamp, ok := buildCtx.previousVisitByID[rowItem.ID]; ok && buildCtx.shipTimeline != nil {
			rowViewItem.Transitions = shipsUsedOnLeg(buildCtx.shipTimeline, buildCtx.jumpTimestamps, afterTimestamp, rowItem.VisitedAt)
			modeID := rowItem.LegModeID
			if modeID == 0 && buildCtx.modeTimeline != nil {
				modeID = buildCtx.modeTimeline.ModeAt(rowItem.VisitedAt)
			}
			rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildCtx.resolveMode(modeID)
		}
	} else if waypointNumber > 1 && rowItem.LegModeID > 0 {
		rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildCtx.resolveMode(rowItem.LegModeID)
	}

	if previousWaypoint != nil && previousWaypoint.HasCoords && rowItem.HasCoords {
		startPosition, _ := previousWaypoint.Coords()
		endPosition, _ := rowItem.Coords()
		legDistanceLy := ed.Distance(startPosition, endPosition)
		rowViewItem.DistFromPrevLy = &legDistanceLy
		planView.PlannedLy += legDistanceLy
		if rowItem.VisitedAt != "" {
			planView.CoveredLy += legDistanceLy
		}
	}

	if buildCtx.nextWaypointRowID > 0 && rowItem.ID == buildCtx.nextWaypointRowID {
		rowViewItem.IsNextWaypoint = true
		if buildCtx.playerPosition != nil && rowItem.HasCoords {
			endPosition, _ := rowItem.Coords()
			playerDistanceLy := ed.Distance(*buildCtx.playerPosition, endPosition)
			rowViewItem.DistFromCurrentLy = &playerDistanceLy
		} else if rowViewItem.DistFromPrevLy != nil {
			rowViewItem.DistFromCurrentLy = rowViewItem.DistFromPrevLy
		}
	}

	planView.RemainingLy += accumulateRemainingDistanceLy(buildCtx.expeditionRecord.Status, rowItem, rowViewItem)

	if buildCtx.gecLookup != nil && rowItem.SystemName != "" {
		rowViewItem.GEC = buildCtx.gecLookup(rowItem.SystemName, rowItem.SystemAddress)
	}
	return rowViewItem
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
	planView := PlanView{
		Expedition:    expeditionRecord,
		StartGalactic: ed.ToGalactic(expeditionRecord.StartTimestamp),
		EndGalactic:   ed.ToGalactic(expeditionRecord.EndTimestamp),
	}

	buildCtx, contextError := loadPlanBuildContext(queryer, expeditionRecord, rows, gecLookup, &planView)
	if contextError != nil {
		return planView, contextError
	}

	waypointNumber := 0
	var previousWaypoint *Row
	for i := range rows {
		rowItem := rows[i]
		if rowItem.RowType == RowRegion {
			rowViewItem := RowView{Row: rowItem, Transitions: []Transition{}}
			if region, ok := ed.RegionByID(rowItem.RegionID); ok {
				rowViewItem.RegionName = region.Name
			}
			planView.Rows = append(planView.Rows, rowViewItem)
			continue
		}
		waypointNumber++
		planView.WaypointCount++
		rowViewItem := buildWaypointRowView(buildCtx, rowItem, waypointNumber, previousWaypoint, &planView)
		rowCopy := rowItem
		previousWaypoint = &rowCopy
		planView.Rows = append(planView.Rows, rowViewItem)
	}
	if planView.Rows == nil {
		planView.Rows = []RowView{}
	}

	planView.PendingTransitions = []Transition{}
	if buildCtx.currentWaypoint != nil && buildCtx.shipTimeline != nil {
		untilTimestamp := expeditionRecord.EndTimestamp
		if untilTimestamp == "" {
			untilTimestamp = ed.NowISO()
		}
		planView.PendingTransitions = shipsUsedOnLeg(buildCtx.shipTimeline, buildCtx.jumpTimestamps, buildCtx.currentWaypoint.VisitedAt, untilTimestamp)
		modeID := buildCtx.modeTimeline.ModeAt(untilTimestamp)
		planView.ActiveModeID, planView.ActiveModeName, planView.ActiveModeColor = buildCtx.resolveMode(modeID)
	} else {
		planView.ActiveModeID, planView.ActiveModeName, planView.ActiveModeColor = buildCtx.resolveMode(buildCtx.modeTimeline.ModeAt(ed.NowISO()))
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
		previousTimestamp := ""
		for _, candidateRow := range rows {
			if candidateRow.IsWaypoint() && candidateRow.VisitedAt != "" && candidateRow.VisitedAt < targetRow.VisitedAt && candidateRow.VisitedAt > previousTimestamp {
				previousTimestamp = candidateRow.VisitedAt
			}
		}
		if previousTimestamp != "" {
			startISO := addOneSecond(previousTimestamp)
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
