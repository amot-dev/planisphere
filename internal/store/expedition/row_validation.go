package expedition

import (
	"database/sql"
	"errors"
	"fmt"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// WaypointPermissions holds precomputed edit, reorder, insert, drawer, and
// Fleet Carrier action permissions and lock reasons for a planner waypoint row.
type WaypointPermissions struct {
	SystemLocked           bool   `json:"systemLocked"`
	SystemLockReason       string `json:"systemLockReason,omitempty"`
	AllowInsertWaypoint    bool   `json:"allowInsertWaypoint"`
	CanReorder             bool   `json:"canReorder"`
	DeleteDisabledReason   string `json:"deleteDisabledReason,omitempty"`
	ReorderDisabledReason  string `json:"reorderDisabledReason,omitempty"`
	CanAddFCStop           bool   `json:"canAddFcStop"`
	CanRemoveFCStop        bool   `json:"canRemoveFcStop"`
	CanOverrideFCBadge     bool   `json:"canOverrideFcBadge"`
	OverrideFCCascadeCount int    `json:"overrideFcCascadeCount,omitempty"`
	CanCascadeLegMode      bool   `json:"canCascadeLegMode,omitempty"`
	HasDrawer              bool   `json:"hasDrawer"`
	IsNextFCStop           bool   `json:"isNextFcStop,omitempty"`
	IsFcStopLocked         bool   `json:"isFcStopLocked,omitempty"`
	HasActiveCarrierRoute  bool   `json:"hasActiveCarrierRoute,omitempty"`
	IsSpanshRoutedFCStop   bool   `json:"isSpanshRoutedFcStop,omitempty"`
}

type planPermissionContext struct {
	isPlanning         bool
	isCompleted        bool
	lastVisitedIndex   int
	lastFCArrivedIndex int
	nextFCStopRowID    int64
	hasRoute           bool
	isManualRoute      bool
	activeTargetID     int64
	routedStopIndex    int
}

func indexOfRowView(rows []RowView, targetRowID int64) int {
	if targetRowID <= 0 {
		return -1
	}
	for i := range rows {
		if rows[i].Row.ID == targetRowID {
			return i
		}
	}
	return -1
}

func buildWaypointPermissions(
	permissionContext planPermissionContext,
	rowView *RowView,
	index int,
	allowInsertWaypoint bool,
	overrideFCCascadeCount int,
) *WaypointPermissions {
	row := rowView.Row
	isVisited := rowView.VisitState == VisitVisited
	isOriginRow := !permissionContext.isPlanning && rowView.Number == 1
	isFCArrivedStop := !permissionContext.isPlanning && row.FCStop && row.FCArrived
	rawIsNextFCStop := row.FCStop && !row.FCArrived && permissionContext.nextFCStopRowID > 0 && row.ID == permissionContext.nextFCStopRowID
	hasActiveCarrierRoute := permissionContext.hasRoute &&
		((permissionContext.isManualRoute && rawIsNextFCStop) || (!permissionContext.isManualRoute && permissionContext.activeTargetID == row.ID))
	isSpanshRoutedFCStop := permissionContext.hasRoute && !permissionContext.isManualRoute && permissionContext.activeTargetID == row.ID
	isFcStopLocked := permissionContext.hasRoute && permissionContext.routedStopIndex >= 0 &&
		((permissionContext.isManualRoute && index < permissionContext.routedStopIndex) || (!permissionContext.isManualRoute && index <= permissionContext.routedStopIndex))

	systemLocked := permissionContext.isCompleted || isVisited || isOriginRow || isSpanshRoutedFCStop || isFCArrivedStop
	activeRouteLockReason := ""
	if isSpanshRoutedFCStop {
		activeRouteLockReason = "Locked while Fleet Carrier route to this waypoint is active"
	}
	systemLockReason := activeRouteLockReason
	if systemLockReason == "" && isFCArrivedStop && !isVisited && !isOriginRow {
		systemLockReason = "Locked while Fleet Carrier is marked arrived at this waypoint"
	}
	deleteDisabledReason := activeRouteLockReason
	reorderDisabledReason := activeRouteLockReason
	if isOriginRow {
		deleteDisabledReason = "Origin waypoint cannot be deleted"
		reorderDisabledReason = "Origin waypoint cannot be reordered"
	}

	return &WaypointPermissions{
		SystemLocked:           systemLocked,
		SystemLockReason:       systemLockReason,
		AllowInsertWaypoint:    allowInsertWaypoint,
		CanReorder:             !permissionContext.isCompleted && !isOriginRow && !isSpanshRoutedFCStop,
		DeleteDisabledReason:   deleteDisabledReason,
		ReorderDisabledReason:  reorderDisabledReason,
		CanAddFCStop:           !permissionContext.isCompleted && !isFcStopLocked && (permissionContext.isPlanning || index > permissionContext.lastFCArrivedIndex),
		CanRemoveFCStop:        !permissionContext.isCompleted && !isFcStopLocked && (permissionContext.isPlanning || !row.FCArrived),
		CanOverrideFCBadge:     !permissionContext.isPlanning && !permissionContext.isCompleted && !isFcStopLocked,
		OverrideFCCascadeCount: overrideFCCascadeCount,
		CanCascadeLegMode:      isVisited && index < permissionContext.lastVisitedIndex,
		HasDrawer:              !permissionContext.isPlanning && rowView.VisitState != VisitFuture,
		IsNextFCStop:           rawIsNextFCStop && !permissionContext.hasRoute,
		IsFcStopLocked:         isFcStopLocked,
		HasActiveCarrierRoute:  hasActiveCarrierRoute,
		IsSpanshRoutedFCStop:   isSpanshRoutedFCStop,
	}
}

// PopulatePlanFrontiersAndPermissions computes LastVisitedIndex,
// LastFCArrivedIndex, per-row AllowInsertWaypoint, and per-waypoint
// WaypointPermissions in memory from already-loaded PlanView fields.
func PopulatePlanFrontiersAndPermissions(planView *PlanView) {
	if planView == nil {
		return
	}
	status := planView.Expedition.Status
	isPlanning := status == StatusPlanning
	isCompleted := status == StatusCompleted

	lastVisitedIndex := -1
	lastFCArrivedIndex := -1
	totalArrivedFCStops := 0
	if !isPlanning {
		for i := range planView.Rows {
			rowView := &planView.Rows[i]
			if rowView.VisitState == VisitVisited {
				lastVisitedIndex = i
			}
			if rowView.Row.IsWaypoint() && rowView.Row.FCStop && rowView.Row.FCArrived {
				lastFCArrivedIndex = i
				totalArrivedFCStops++
			}
		}
	}
	planView.LastVisitedIndex = lastVisitedIndex
	planView.LastFCArrivedIndex = lastFCArrivedIndex

	hasRoute := planView.ActiveCarrierRoute != nil
	permissionContext := planPermissionContext{
		isPlanning:         isPlanning,
		isCompleted:        isCompleted,
		lastVisitedIndex:   lastVisitedIndex,
		lastFCArrivedIndex: lastFCArrivedIndex,
		nextFCStopRowID:    planView.NextFCStopRowID,
		hasRoute:           hasRoute,
		isManualRoute:      hasRoute && planView.ActiveCarrierRoute.IsManualMode(),
		routedStopIndex:    -1,
	}
	if hasRoute {
		permissionContext.activeTargetID = planView.ActiveCarrierRoute.TargetRowID
		permissionContext.routedStopIndex = indexOfRowView(planView.Rows, planView.RoutedCarrierStopRowID)
	}

	precedingPendingFCStops := 0
	seenArrivedFCStops := 0
	for index := range planView.Rows {
		rowView := &planView.Rows[index]
		allowInsertWaypoint := isPlanning || index >= lastVisitedIndex
		rowView.AllowInsertWaypoint = allowInsertWaypoint
		if !rowView.Row.IsWaypoint() {
			rowView.Permissions = nil
			continue
		}
		overrideFCCascadeCount := 0
		if rowView.Row.FCStop {
			if rowView.Row.FCArrived {
				seenArrivedFCStops++
				if !isPlanning && !isCompleted {
					overrideFCCascadeCount = totalArrivedFCStops - seenArrivedFCStops
				}
			} else {
				if !isPlanning && !isCompleted {
					overrideFCCascadeCount = precedingPendingFCStops
				}
				precedingPendingFCStops++
			}
		}
		rowView.Permissions = buildWaypointPermissions(permissionContext, rowView, index, allowInsertWaypoint, overrideFCCascadeCount)
	}
}

func validateFCStopAgainstActiveRoute(rows []Row, currentRowIndex int, activeRoute *ActiveCarrierRouteState) error {
	routedStopIndex := ResolveRoutedCarrierStopIndex(rows, 0, activeRoute)
	if routedStopIndex < 0 {
		return nil
	}
	if activeRoute.IsManualMode() {
		if currentRowIndex < routedStopIndex {
			return errors.New("fleet carrier stop cannot be changed before the active manual carrier stop")
		}
		return nil
	}
	if currentRowIndex <= routedStopIndex {
		return errors.New("fleet carrier stop cannot be changed at or before an active routed stop")
	}
	return nil
}

func validateActiveSystemNameEdit(queryer db.Queryer, currentRow Row, loadRows func() ([]Row, error)) error {
	if currentRow.VisitedAt != "" {
		return errors.New("star system cannot be changed on a visited waypoint")
	}
	if currentRow.FCStop && currentRow.FCArrived {
		return errors.New("star system cannot be changed while fleet carrier is marked arrived")
	}
	rows, listError := loadRows()
	if listError != nil {
		return listError
	}
	if firstWaypointIndex := firstWaypoint(rows); firstWaypointIndex >= 0 && rows[firstWaypointIndex].ID == currentRow.ID {
		return errors.New("waypoint #1 star system cannot be changed after the expedition has started")
	}
	if lockedTargetID := readLockedSpanshTargetRowID(queryer, currentRow.ExpeditionID); lockedTargetID > 0 && currentRow.ID == lockedTargetID {
		return errors.New("star system cannot be changed while fleet carrier route is active")
	}
	return nil
}

func validateActiveFCStopEdit(queryer db.Queryer, currentRow Row, nextFCStop bool, loadRows func() ([]Row, error)) error {
	if !nextFCStop && currentRow.FCArrived {
		return errors.New("arrived fleet carrier stop cannot be removed")
	}
	rows, listError := loadRows()
	if listError != nil {
		return listError
	}
	currentRowIndex := indexOf(rows, currentRow.ID)
	if currentRowIndex < 0 {
		return fmt.Errorf("row %d not found", currentRow.ID)
	}
	lastFCArrivedIndex := -1
	for i, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.FCStop && rowItem.FCArrived {
			lastFCArrivedIndex = i
		}
	}
	if nextFCStop && currentRowIndex <= lastFCArrivedIndex {
		return errors.New("fleet carrier stop cannot be added at or before an arrived carrier stop")
	}
	activeRoute, routeError := GetActiveCarrierRoute(queryer, currentRow.ExpeditionID)
	if routeError != nil {
		return routeError
	}
	if activeRoute == nil {
		return nil
	}
	return validateFCStopAgainstActiveRoute(rows, currentRowIndex, activeRoute)
}

func validateRowStructuralEdits(queryer db.Queryer, currentRow Row, patch RowPatch, systemChanging bool) error {
	if patch.RegionID != nil && currentRow.RowType == RowRegion {
		if _, ok := reference.RegionByID(*patch.RegionID); !ok {
			return fmt.Errorf("invalid region %d", *patch.RegionID)
		}
	}

	regionChanging := patch.RegionID != nil && currentRow.RowType == RowRegion && *patch.RegionID != currentRow.RegionID
	fcStopChanging := patch.FCStop != nil && *patch.FCStop != currentRow.FCStop
	fcArrivedChanging := patch.FCArrived != nil && *patch.FCArrived != currentRow.FCArrived
	if !regionChanging && !fcStopChanging && !fcArrivedChanging && !systemChanging {
		return nil
	}

	expeditionRecord, expError := Get(queryer, currentRow.ExpeditionID)
	if expError != nil {
		return expError
	}
	if expeditionRecord.Status == StatusCompleted {
		return errors.New("completed expedition structure cannot be modified")
	}
	if expeditionRecord.Status != StatusActive {
		return nil
	}

	var cachedRows []Row
	rowsLoaded := false
	loadRows := func() ([]Row, error) {
		if rowsLoaded {
			return cachedRows, nil
		}
		loadedRows, listError := ListRows(queryer, currentRow.ExpeditionID)
		if listError != nil {
			return nil, listError
		}
		cachedRows = loadedRows
		rowsLoaded = true
		return cachedRows, nil
	}

	if systemChanging {
		if systemError := validateActiveSystemNameEdit(queryer, currentRow, loadRows); systemError != nil {
			return systemError
		}
	}
	if fcStopChanging {
		return validateActiveFCStopEdit(queryer, currentRow, *patch.FCStop, loadRows)
	}
	return nil
}

// SetCarrierArrivalOverride manually sets the Fleet Carrier arrival status on rowID
// and cascades the change across preceding (when arrived is true) or subsequent
// (when arrived is false) carrier stops so arrived stops always precede pending stops.
func SetCarrierArrivalOverride(transaction *sql.Tx, rowID int64, arrived bool) (int64, error) {
	targetRow, getError := GetRow(transaction, rowID)
	if getError != nil {
		return 0, getError
	}
	if !targetRow.IsWaypoint() || !targetRow.FCStop {
		return targetRow.ExpeditionID, errors.New("row is not a designated fleet carrier stop")
	}
	lockedSpanshTargetRowID := readLockedSpanshTargetRowID(transaction, targetRow.ExpeditionID)
	now := reference.NowISO()
	if arrived {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 1, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position <= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
		}
		if lockedSpanshTargetRowID > 0 {
			routedRow, routedError := GetRow(transaction, lockedSpanshTargetRowID)
			if routedError == nil && routedRow.FCArrived {
				if clearRouteError := ClearActiveCarrierRoute(transaction, targetRow.ExpeditionID); clearRouteError != nil {
					return targetRow.ExpeditionID, fmt.Errorf("clear active carrier route: %w", clearRouteError)
				}
			}
		}
	} else {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 0, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position >= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
		}
	}
	if touchError := touch(transaction, targetRow.ExpeditionID); touchError != nil {
		return targetRow.ExpeditionID, touchError
	}
	return targetRow.ExpeditionID, nil
}

// SetCarrierArrivalOverrideTx runs SetCarrierArrivalOverride inside a database transaction on dbConn.
func SetCarrierArrivalOverrideTx(dbConn *sql.DB, rowID int64, arrived bool) (int64, error) {
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var overrideError error
		expeditionID, overrideError = SetCarrierArrivalOverride(transaction, rowID, arrived)
		return overrideError
	})
	return expeditionID, txError
}
