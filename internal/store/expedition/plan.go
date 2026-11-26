package expedition

import (
	"database/sql"
	"errors"
	"fmt"
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
	Row                 Row                  `json:"row"`
	Number              int                  `json:"number"` // waypoint # (0 for region headers)
	RegionName          string               `json:"regionName"`
	DetectedRegionID    int                  `json:"detectedRegionId"` // region containing the coordinates
	DetectedRegion      string               `json:"detectedRegion"`
	VisitCount          int                  `json:"visitCount"`
	VisitState          string               `json:"visitState"`
	VisitedAtGalactic   string               `json:"visitedAtGalactic"`
	DepartedAtGalactic  string               `json:"departedAtGalactic,omitempty"`
	DrawerFrom          string               `json:"drawerFrom,omitempty"`
	DrawerTo            string               `json:"drawerTo,omitempty"`
	DistFromPrevLy      *float64             `json:"distFromPrevLy,omitempty"`
	DistFromCurrentLy   *float64             `json:"distFromCurrentLy,omitempty"`
	IsNextWaypoint      bool                 `json:"isNextWaypoint,omitempty"`
	LegModeID           int64                `json:"legModeId"`
	LegModeName         string               `json:"legModeName"`
	LegModeColor        string               `json:"legModeColor"`
	GEC                 *GECRef              `json:"gec,omitempty"`
	AllowInsertWaypoint bool                 `json:"allowInsertWaypoint"`
	Permissions         *WaypointPermissions `json:"permissions,omitempty"`
}

// PlanView is the full planner sheet.
type PlanView struct {
	Expedition             Expedition               `json:"expedition"`
	StartGalactic          string                   `json:"startGalactic"`
	EndGalactic            string                   `json:"endGalactic"`
	Rows                   []RowView                `json:"rows"`
	CurrentRowID           int64                    `json:"currentRowId"`
	NextWaypointRowID      int64                    `json:"nextWaypointRowId,omitempty"`
	NextFCStopRowID        int64                    `json:"nextFcStopRowId"`
	RoutedCarrierStopRowID int64                    `json:"routedCarrierStopRowId,omitempty"`
	ActiveCarrierRoute     *ActiveCarrierRouteState `json:"activeCarrierRoute,omitempty"`
	LastVisitedIndex       int                      `json:"lastVisitedIndex"`
	LastFCArrivedIndex     int                      `json:"lastFcArrivedIndex"`
	DefaultModeID          int64                    `json:"defaultModeId"`
	PlayerSystem           string                   `json:"playerSystem,omitempty"`
	WaypointCount          int                      `json:"waypointCount"`
	VisitedWaypointCount   int                      `json:"visitedWaypointCount"`
	PlannedLy              float64                  `json:"plannedLy"`   // sum of straight-line distances across all planned waypoint legs
	CoveredLy              float64                  `json:"coveredLy"`   // sum of straight-line distances across visited waypoint legs
	RemainingLy            float64                  `json:"remainingLy"` // straight-line distance from current player position to next waypoint plus remaining unvisited legs
	Modes                  []modes.Mode             `json:"modes"`
}

// GECLookup resolves GEC entries for a system.
type GECLookup func(systemName string, systemAddress int64) *GECRef

type planBuildContext struct {
	expeditionRecord     Expedition
	currentWaypoint      *Row
	playerPosition       *reference.Vec3
	nextWaypointRowID    int64
	modeList             []modes.Mode
	modeByID             map[int64]modes.Mode
	defaultModeID        int64
	activeModeID         int64
	previousVisitByID    map[int64]string
	precedingVisitedByID map[int64]*Row
	visitCountBySystem   map[string]int
	gecLookup            GECLookup
}

func (buildContext *planBuildContext) resolveMode(modeID int64) (int64, string, string) {
	if modeItem, ok := buildContext.modeByID[modeID]; ok {
		return modeItem.ID, modeItem.Name, modeItem.Color
	}
	if modeItem, ok := buildContext.modeByID[buildContext.defaultModeID]; ok {
		return modeItem.ID, modeItem.Name, modeItem.Color
	}
	if len(buildContext.modeList) > 0 {
		return buildContext.modeList[0].ID, buildContext.modeList[0].Name, buildContext.modeList[0].Color
	}
	return 0, "Travel", "#FF7100"
}

// buildPreviousVisitMap indexes non-origin visited waypoints sorted by visit time.
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

func resolvePlanVisitCounts(rows []Row, scope replay.ProfileScope, expeditionProjections *Projections) (map[string]int, error) {
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
	return expeditionProjections.VisitCounts.CountsForSystemsScoped(waypointSystems, scope)
}

func loadPlanBuildContext(queryer db.Queryer, expeditionRecord Expedition, rows []Row, gecLookup GECLookup, planView *PlanView, expeditionProjections *Projections) (*planBuildContext, error) {
	expScope := expeditionRecord.ProfileScope()
	visitCountBySystem, visitCountsError := resolvePlanVisitCounts(rows, expScope, expeditionProjections)
	if visitCountsError != nil {
		return nil, visitCountsError
	}
	buildContext := &planBuildContext{
		expeditionRecord:   expeditionRecord,
		modeByID:           map[int64]modes.Mode{},
		previousVisitByID:  buildPreviousVisitMap(rows),
		visitCountBySystem: visitCountBySystem,
		gecLookup:          gecLookup,
	}

	if firstIndex := firstWaypoint(rows); firstIndex >= 0 && rows[firstIndex].DepartedAt != "" {
		planView.StartGalactic = reference.ToGalactic(rows[firstIndex].DepartedAt)
	}

	// Current waypoint is the visited waypoint with the latest visit time.
	buildContext.currentWaypoint, _ = latestVisitedWaypoint(rows)
	if buildContext.currentWaypoint != nil {
		planView.CurrentRowID = buildContext.currentWaypoint.ID
	}

	playerPosition, playerSystem, playerLocationError := replay.QueryLatestPlayerLocationScoped(queryer, expScope)
	if playerLocationError != nil && !errors.Is(playerLocationError, sql.ErrNoRows) {
		return nil, playerLocationError
	}
	if playerPosition == nil && buildContext.currentWaypoint != nil && buildContext.currentWaypoint.HasCoords {
		coords, _ := buildContext.currentWaypoint.Coords()
		playerPosition = &coords
		playerSystem = buildContext.currentWaypoint.SystemName
	}
	buildContext.playerPosition = playerPosition
	planView.PlayerSystem = playerSystem

	// Advance the active current leg only once the commander has departed the latest visited waypoint.
	if expeditionRecord.Status == StatusActive && buildContext.currentWaypoint != nil && buildContext.currentWaypoint.DepartedAt != "" {
		for _, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt == "" {
				buildContext.nextWaypointRowID = rowItem.ID
				break
			}
		}
	}
	planView.NextWaypointRowID = buildContext.nextWaypointRowID

	var listError error
	if buildContext.modeList, listError = modes.List(queryer); listError != nil {
		return nil, listError
	}
	planView.Modes = buildContext.modeList
	for _, modeItem := range buildContext.modeList {
		buildContext.modeByID[modeItem.ID] = modeItem
	}
	buildContext.defaultModeID = modes.DefaultFallbackModeID(queryer)
	planView.DefaultModeID = buildContext.defaultModeID
	resolvedActiveModeID, _, _ := buildContext.resolveMode(expeditionRecord.ActiveModeID)
	buildContext.activeModeID = resolvedActiveModeID
	planView.Expedition.ActiveModeID = resolvedActiveModeID

	return buildContext, nil
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

func populateWaypointDrawerBounds(buildContext *planBuildContext, rowItem Row, waypointNumber int, rowViewItem *RowView) {
	if rowViewItem.VisitState != VisitVisited && rowViewItem.VisitState != VisitCurrent {
		return
	}
	if waypointNumber == 1 {
		rowViewItem.DrawerTo = rowItem.DepartedAt
		return
	}
	precedingRow := buildContext.currentWaypoint
	if rowItem.VisitedAt != "" {
		precedingRow = buildContext.precedingVisitedByID[rowItem.ID]
	}
	if precedingRow == nil {
		rowViewItem.DrawerFrom = buildContext.expeditionRecord.StartTimestamp
	} else if precedingRow.DepartedAt != "" {
		rowViewItem.DrawerFrom = precedingRow.DepartedAt
	} else {
		rowViewItem.DrawerFrom = precedingRow.VisitedAt
	}
	if rowItem.DepartedAt != "" {
		rowViewItem.DrawerTo = rowItem.DepartedAt
	} else if rowItem.VisitedAt != "" && buildContext.expeditionRecord.Status == StatusCompleted && buildContext.expeditionRecord.EndTimestamp != "" {
		rowViewItem.DrawerTo = buildContext.expeditionRecord.EndTimestamp
	}
}

func populateWaypointLegTelemetry(buildContext *planBuildContext, rowItem Row, waypointNumber int, rowViewItem *RowView) {
	populateWaypointDrawerBounds(buildContext, rowItem, waypointNumber, rowViewItem)
	switch rowViewItem.VisitState {
	case VisitVisited:
		rowViewItem.VisitedAtGalactic = reference.ToGalactic(rowItem.VisitedAt)
		if rowItem.DepartedAt != "" {
			rowViewItem.DepartedAtGalactic = reference.ToGalactic(rowItem.DepartedAt)
		}
		if _, hasPreviousVisit := buildContext.previousVisitByID[rowItem.ID]; hasPreviousVisit {
			rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildContext.resolveMode(rowItem.LegModeID)
		}
	case VisitCurrent:
		rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildContext.resolveMode(buildContext.activeModeID)
	default:
		if waypointNumber > 1 && rowItem.LegModeID > 0 {
			rowViewItem.LegModeID, rowViewItem.LegModeName, rowViewItem.LegModeColor = buildContext.resolveMode(rowItem.LegModeID)
		}
	}
}

func buildWaypointRowView(buildContext *planBuildContext, rowItem Row, waypointNumber int, previousWaypoint *Row, planView *PlanView) RowView {
	rowViewItem := RowView{Row: rowItem, Number: waypointNumber}
	if rowItem.FCStop && !rowItem.FCArrived && planView.NextFCStopRowID == 0 {
		planView.NextFCStopRowID = rowItem.ID
	}
	if rowItem.HasCoords {
		if region, ok := reference.RegionAt(rowItem.X, rowItem.Y, rowItem.Z); ok {
			rowViewItem.DetectedRegionID, rowViewItem.DetectedRegion = region.ID, region.Name
		}
		visitCount := buildContext.visitCountBySystem[replay.NormalizeSystemKey(rowItem.SystemName)]
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
	case buildContext.currentWaypoint != nil && buildContext.nextWaypointRowID > 0 && rowItem.ID == buildContext.nextWaypointRowID:
		rowViewItem.VisitState = VisitCurrent
	default:
		rowViewItem.VisitState = VisitFuture
	}

	populateWaypointLegTelemetry(buildContext, rowItem, waypointNumber, &rowViewItem)

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

	if buildContext.nextWaypointRowID > 0 && rowItem.ID == buildContext.nextWaypointRowID {
		rowViewItem.IsNextWaypoint = true
		if buildContext.playerPosition != nil && rowItem.HasCoords {
			endPosition, _ := rowItem.Coords()
			playerDistanceLy := reference.Distance(*buildContext.playerPosition, endPosition)
			rowViewItem.DistFromCurrentLy = &playerDistanceLy
		} else if rowViewItem.DistFromPrevLy != nil {
			rowViewItem.DistFromCurrentLy = rowViewItem.DistFromPrevLy
		}
	}

	planView.RemainingLy += accumulateRemainingDistanceLy(buildContext.expeditionRecord.Status, rowItem, rowViewItem)

	if buildContext.gecLookup != nil && rowItem.SystemName != "" {
		rowViewItem.GEC = buildContext.gecLookup(rowItem.SystemName, rowItem.SystemAddress)
	}
	return rowViewItem
}

// BuildPlan assembles the planner view for an expedition.
func BuildPlan(queryer db.Queryer, expeditionID int64, gecLookup GECLookup, expeditionProjections *Projections) (PlanView, error) {
	if expeditionProjections == nil || expeditionProjections.VisitCounts == nil {
		return PlanView{}, errors.New("expedition VisitCounts projection is required")
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
		Expedition:  expeditionRecord,
		EndGalactic: reference.ToGalactic(expeditionRecord.EndTimestamp),
	}

	buildContext, contextError := loadPlanBuildContext(queryer, expeditionRecord, rows, gecLookup, &planView, expeditionProjections)
	if contextError != nil {
		return planView, contextError
	}
	buildContext.precedingVisitedByID = indexPrecedingVisitedWaypoints(rows)

	waypointNumber := 0
	var previousWaypoint *Row
	for i := range rows {
		rowItem := rows[i]
		if rowItem.RowType == RowRegion {
			rowViewItem := RowView{Row: rowItem}
			if region, ok := reference.RegionByID(rowItem.RegionID); ok {
				rowViewItem.RegionName = region.Name
			}
			planView.Rows = append(planView.Rows, rowViewItem)
			continue
		}
		waypointNumber++
		planView.WaypointCount++
		rowViewItem := buildWaypointRowView(buildContext, rowItem, waypointNumber, previousWaypoint, &planView)
		rowCopy := rowItem
		previousWaypoint = &rowCopy
		planView.Rows = append(planView.Rows, rowViewItem)
	}
	if planView.Rows == nil {
		planView.Rows = []RowView{}
	}
	rawRoute, routeError := GetActiveCarrierRoute(queryer, expeditionID)
	if routeError != nil {
		return planView, fmt.Errorf("load active carrier route: %w", routeError)
	}
	planView.ActiveCarrierRoute = NormalizeActiveCarrierRoute(rawRoute)
	planView.RoutedCarrierStopRowID = ResolveRoutedCarrierStopRowID(rows, planView.NextFCStopRowID, planView.ActiveCarrierRoute)
	PopulatePlanFrontiersAndPermissions(&planView)
	return planView, nil
}
