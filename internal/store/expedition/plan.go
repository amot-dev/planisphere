package expedition

import (
	"database/sql"
	"errors"
	"sort"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/store/modes"
)

// Row visit states.
const (
	VisitVisited = "visited"
	VisitCurrent = "current"
	VisitFuture  = "future"
)

// Transition is a ship switch that happened during a leg.
type Transition = replay.ShipTransition

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
	Row               Row               `json:"row"`
	Number            int               `json:"number"` // waypoint # (0 for region headers)
	RegionName        string            `json:"regionName"`
	DetectedRegionID  int               `json:"detectedRegionId"` // region containing the coordinates
	DetectedRegion    string            `json:"detectedRegion"`
	VisitCount        int               `json:"visitCount"`
	VisitState        string            `json:"visitState"`
	VisitedAtGalactic string            `json:"visitedAtGalactic"`
	DistFromPrevLy    *float64          `json:"distFromPrevLy,omitempty"`
	DistFromCurrentLy *float64          `json:"distFromCurrentLy,omitempty"`
	IsNextWaypoint    bool              `json:"isNextWaypoint,omitempty"`
	Transitions       []Transition      `json:"transitions"` // ships flown during the leg arriving at this waypoint
	LegModeID         int64        `json:"legModeId"`
	LegModeName       string       `json:"legModeName"`
	LegModeColor      string       `json:"legModeColor"`
	GEC               *GECRef      `json:"gec,omitempty"`
}

// PlanView is the full planner sheet.
type PlanView struct {
	Expedition        Expedition   `json:"expedition"`
	StartGalactic     string       `json:"startGalactic"`
	EndGalactic       string       `json:"endGalactic"`
	Rows              []RowView    `json:"rows"`
	CurrentRowID      int64        `json:"currentRowId"`
	NextWaypointRowID int64        `json:"nextWaypointRowId,omitempty"`
	NextFCStopRowID   int64        `json:"nextFcStopRowId"`
	PlayerSystem      string       `json:"playerSystem,omitempty"`
	WaypointCount        int          `json:"waypointCount"`
	VisitedWaypointCount int          `json:"visitedWaypointCount"`
	PlannedLy            float64      `json:"plannedLy"`   // sum of straight-line distances across all planned waypoint legs
	CoveredLy         float64      `json:"coveredLy"`   // sum of straight-line distances across visited waypoint legs
	RemainingLy       float64      `json:"remainingLy"` // straight-line distance from current player position to next waypoint plus remaining unvisited legs
	Modes             []modes.Mode `json:"modes"`
}

// GECLookup resolves GEC entries for a system.
type GECLookup func(systemName string, systemAddress int64) *GECRef

type planBuildContext struct {
	expeditionRecord   Expedition
	currentWaypoint    *Row
	playerPosition     *reference.Vec3
	nextWaypointRowID  int64
	shipTimeline       []replay.ShipSwitch
	jumpTimestamps     []string
	modeTimeline       *modes.Timeline
	modeList           []modes.Mode
	modeByID           map[int64]modes.Mode
	defaultModeID      int64
	previousVisitByID  map[int64]string
	visitCountBySystem map[string]int
	gecLookup          GECLookup
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

func resolvePlanVisitCounts(rows []Row, proj *Projections) (map[string]int, error) {
	waypointSystems := make([]string, 0, len(rows))
	seenKeys := make(map[string]bool, len(rows))
	for _, rowItem := range rows {
		if !rowItem.IsWaypoint() {
			continue
		}
		normalizedKey := replay.NormalizeSystemKey(rowItem.SystemName)
		if normalizedKey != "" && !seenKeys[normalizedKey] {
			seenKeys[normalizedKey] = true
			waypointSystems = append(waypointSystems, rowItem.SystemName)
		}
	}
	if len(waypointSystems) == 0 {
		return nil, nil
	}
	return proj.VisitCounts.CountsForSystems(waypointSystems)
}

func loadPlanShipContext(queryer db.Queryer, expeditionRecord Expedition, buildCtx *planBuildContext, proj *Projections) error {
	var shipError error
	if buildCtx.shipTimeline, shipError = proj.ShipHistory.Timeline(expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp); shipError != nil {
		return shipError
	}
	recoveredJumps, recoverError := proj.RecoveredJumps.InRange(expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	if recoverError != nil {
		return recoverError
	}
	var jumpError error
	buildCtx.jumpTimestamps, jumpError = replay.QueryJumpTimestamps(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp, recoveredJumps)
	return jumpError
}

func loadPlanBuildContext(queryer db.Queryer, expeditionRecord Expedition, rows []Row, gecLookup GECLookup, planView *PlanView, proj *Projections) (*planBuildContext, error) {
	visitCountBySystem, visitCountsError := resolvePlanVisitCounts(rows, proj)
	if visitCountsError != nil {
		return nil, visitCountsError
	}
	buildCtx := &planBuildContext{
		expeditionRecord:   expeditionRecord,
		modeByID:           map[int64]modes.Mode{},
		previousVisitByID:  buildPreviousVisitMap(rows),
		visitCountBySystem: visitCountBySystem,
		gecLookup:          gecLookup,
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

	playerPosition, playerSystem, playerLocationError := replay.QueryLatestPlayerLocation(queryer)
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
		if shipContextError := loadPlanShipContext(queryer, expeditionRecord, buildCtx, proj); shipContextError != nil {
			return nil, shipContextError
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

func populateWaypointLegTelemetry(buildCtx *planBuildContext, rowItem Row, waypointNumber int, rowViewItem *RowView) {
	switch rowViewItem.VisitState {
	case VisitVisited:
		rowViewItem.VisitedAtGalactic = reference.ToGalactic(rowItem.VisitedAt)
		if afterTimestamp, ok := buildCtx.previousVisitByID[rowItem.ID]; ok && buildCtx.shipTimeline != nil {
			rowViewItem.Transitions = replay.ExtractShipsUsedOnLeg(buildCtx.shipTimeline, buildCtx.jumpTimestamps, afterTimestamp, rowItem.VisitedAt)
			modeID := rowItem.LegModeID
			if modeID == 0 && buildCtx.modeTimeline != nil {
				modeID = buildCtx.modeTimeline.ModeAt(rowItem.VisitedAt)
			}
			rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildCtx.resolveMode(modeID)
		}
	case VisitCurrent:
		untilTimestamp := buildCtx.expeditionRecord.EndTimestamp
		if untilTimestamp == "" {
			untilTimestamp = reference.NowISO()
		}
		if buildCtx.shipTimeline != nil {
			rowViewItem.Transitions = replay.ExtractShipsUsedOnLeg(buildCtx.shipTimeline, buildCtx.jumpTimestamps, buildCtx.currentWaypoint.VisitedAt, untilTimestamp)
		}
		var modeID int64
		if buildCtx.modeTimeline != nil {
			modeID = buildCtx.modeTimeline.ModeAt(untilTimestamp)
		}
		rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildCtx.resolveMode(modeID)
	default:
		if waypointNumber > 1 && rowItem.LegModeID > 0 {
			rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildCtx.resolveMode(rowItem.LegModeID)
		}
	}
	if rowViewItem.Transitions == nil {
		rowViewItem.Transitions = []Transition{}
	}
}

func buildWaypointRowView(buildCtx *planBuildContext, rowItem Row, waypointNumber int, previousWaypoint *Row, planView *PlanView) RowView {
	rowViewItem := RowView{Row: rowItem, Number: waypointNumber, Transitions: []Transition{}}
	if rowItem.FCStop && !rowItem.FCArrived && planView.NextFCStopRowID == 0 {
		planView.NextFCStopRowID = rowItem.ID
	}
	if rowItem.HasCoords {
		if region, ok := reference.RegionAt(rowItem.X, rowItem.Y, rowItem.Z); ok {
			rowViewItem.DetectedRegionID, rowViewItem.DetectedRegion = region.ID, region.Name
		}
		visitCount := buildCtx.visitCountBySystem[replay.NormalizeSystemKey(rowItem.SystemName)]
		rowViewItem.VisitCount = visitCount
		if visitCount > 0 && rowViewItem.Row.CoordsSource != "journal" {
			rowViewItem.Row.CoordsSource = "journal"
		} else if visitCount == 0 && rowViewItem.Row.CoordsSource == "journal" {
			rowViewItem.Row.CoordsSource = "edsm"
		}
	}

	switch {
	case rowItem.VisitedAt != "":
		rowViewItem.VisitState = VisitVisited
		planView.VisitedWaypointCount++
	case buildCtx.currentWaypoint != nil && buildCtx.nextWaypointRowID > 0 && rowItem.ID == buildCtx.nextWaypointRowID:
		rowViewItem.VisitState = VisitCurrent
	default:
		rowViewItem.VisitState = VisitFuture
	}

	populateWaypointLegTelemetry(buildCtx, rowItem, waypointNumber, &rowViewItem)

	if previousWaypoint != nil && previousWaypoint.HasCoords && rowItem.HasCoords {
		startPosition, _ := previousWaypoint.Coords()
		endPosition, _ := rowItem.Coords()
		legDistanceLy := reference.Distance(startPosition, endPosition)
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
			playerDistanceLy := reference.Distance(*buildCtx.playerPosition, endPosition)
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
func BuildPlan(queryer db.Queryer, expeditionID int64, gecLookup GECLookup, proj *Projections) (PlanView, error) {
	if proj == nil || proj.VisitCounts == nil || proj.ShipHistory == nil || proj.RecoveredJumps == nil {
		return PlanView{}, errors.New("expedition VisitCounts, ShipHistory, and RecoveredJumps projections are required")
	}
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
		StartGalactic: reference.ToGalactic(expeditionRecord.StartTimestamp),
		EndGalactic:   reference.ToGalactic(expeditionRecord.EndTimestamp),
	}

	buildCtx, contextError := loadPlanBuildContext(queryer, expeditionRecord, rows, gecLookup, &planView, proj)
	if contextError != nil {
		return planView, contextError
	}

	waypointNumber := 0
	var previousWaypoint *Row
	for i := range rows {
		rowItem := rows[i]
		if rowItem.RowType == RowRegion {
			rowViewItem := RowView{Row: rowItem, Transitions: []Transition{}}
			if region, ok := reference.RegionByID(rowItem.RegionID); ok {
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
	return planView, nil
}
