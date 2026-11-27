package expedition_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
)

func TestPopulatePlanFrontiersAndPermissions_PlanningActiveAndCompleted(testingContext *testing.T) {
	planningPlan := expedition.PlanView{
		Expedition:      expedition.Expedition{ID: 1, Status: expedition.StatusPlanning},
		NextFCStopRowID: 12,
		Rows: []expedition.RowView{
			{Row: expedition.Row{ID: 10, RowType: expedition.RowRegion, RegionID: 18}},
			{Row: expedition.Row{ID: 11, RowType: expedition.RowWaypoint, SystemName: "Sol"}, Number: 1, VisitState: expedition.VisitFuture},
			{Row: expedition.Row{ID: 12, RowType: expedition.RowWaypoint, SystemName: "Colonia", FCStop: true}, Number: 2, VisitState: expedition.VisitFuture},
		},
	}
	expedition.PopulatePlanFrontiersAndPermissions(&planningPlan)

	if planningPlan.LastVisitedIndex != -1 || planningPlan.LastFCArrivedIndex != -1 {
		testingContext.Fatalf("planning frontiers = (%d, %d), want (-1, -1)", planningPlan.LastVisitedIndex, planningPlan.LastFCArrivedIndex)
	}
	if !planningPlan.Rows[0].AllowInsertWaypoint || planningPlan.Rows[0].Permissions != nil {
		testingContext.Fatalf("planning region row unexpected state: %+v", planningPlan.Rows[0])
	}
	originPlanningPerms := planningPlan.Rows[1].Permissions
	if originPlanningPerms == nil || originPlanningPerms.SystemLocked || !originPlanningPerms.CanReorder || !originPlanningPerms.CanAddFCStop || originPlanningPerms.CanOverrideFCBadge || originPlanningPerms.HasDrawer {
		testingContext.Fatalf("planning origin permissions mismatch: %+v", originPlanningPerms)
	}
	fcPlanningPerms := planningPlan.Rows[2].Permissions
	if fcPlanningPerms == nil || !fcPlanningPerms.IsNextFCStop || !fcPlanningPerms.CanRemoveFCStop || fcPlanningPerms.OverrideFCCascadeCount != 0 {
		testingContext.Fatalf("planning FC stop permissions mismatch: %+v", fcPlanningPerms)
	}

	// Active expedition with mixed visited, arrived FC, pending FC, region headers, and cascade counts:
	// Index 0: Region (before lastVisitedIndex=3 -> AllowInsertWaypoint=false)
	// Index 1: WP #1 Sol (visited, FCStop+FCArrived, Origin)
	// Index 2: WP #2 Rohini (visited, no FCStop -> before lastFCArrivedIndex=4 so CanAddFCStop=false, CanCascadeLegMode=true)
	// Index 3: WP #3 Gandharvi (visited, FCStop+FCArrived -> lastVisitedIndex=3 so CanCascadeLegMode=false)
	// Index 4: WP #4 Colonia (future, FCStop+FCArrived -> lastFCArrivedIndex=4, SystemLockReason="Locked while Fleet Carrier is marked arrived at this waypoint")
	// Index 5: Region (after lastVisitedIndex=3 -> AllowInsertWaypoint=true)
	// Index 6: WP #5 Sagittarius A* (current, FCStop pending -> NextFCStopRowID=16, OverrideFCCascadeCount=0)
	// Index 7: WP #6 Beagle Point (future, FCStop pending -> OverrideFCCascadeCount=1 because index 6 is pending)
	activePlan := expedition.PlanView{
		Expedition:      expedition.Expedition{ID: 2, Status: expedition.StatusActive},
		NextFCStopRowID: 16,
		Rows: []expedition.RowView{
			{Row: expedition.Row{ID: 10, RowType: expedition.RowRegion, RegionID: 18}},
			{Row: expedition.Row{ID: 11, RowType: expedition.RowWaypoint, SystemName: "Sol", FCStop: true, FCArrived: true, VisitedAt: "2026-01-01T00:00:00Z"}, Number: 1, VisitState: expedition.VisitVisited},
			{Row: expedition.Row{ID: 12, RowType: expedition.RowWaypoint, SystemName: "Rohini", VisitedAt: "2026-01-02T00:00:00Z"}, Number: 2, VisitState: expedition.VisitVisited},
			{Row: expedition.Row{ID: 13, RowType: expedition.RowWaypoint, SystemName: "Gandharvi", FCStop: true, FCArrived: true, VisitedAt: "2026-01-03T00:00:00Z"}, Number: 3, VisitState: expedition.VisitVisited},
			{Row: expedition.Row{ID: 14, RowType: expedition.RowWaypoint, SystemName: "Colonia", FCStop: true, FCArrived: true}, Number: 4, VisitState: expedition.VisitFuture},
			{Row: expedition.Row{ID: 15, RowType: expedition.RowRegion, RegionID: 1}},
			{Row: expedition.Row{ID: 16, RowType: expedition.RowWaypoint, SystemName: "Sagittarius A*", FCStop: true, FCArrived: false}, Number: 5, VisitState: expedition.VisitCurrent},
			{Row: expedition.Row{ID: 17, RowType: expedition.RowWaypoint, SystemName: "Beagle Point", FCStop: true, FCArrived: false}, Number: 6, VisitState: expedition.VisitFuture},
		},
	}
	expedition.PopulatePlanFrontiersAndPermissions(&activePlan)

	if activePlan.LastVisitedIndex != 3 || activePlan.LastFCArrivedIndex != 4 {
		testingContext.Fatalf("active frontiers = (%d, %d), want (3, 4)", activePlan.LastVisitedIndex, activePlan.LastFCArrivedIndex)
	}
	if activePlan.Rows[0].AllowInsertWaypoint || !activePlan.Rows[5].AllowInsertWaypoint {
		testingContext.Fatalf("region AllowInsertWaypoint = (%v, %v), want (false, true)", activePlan.Rows[0].AllowInsertWaypoint, activePlan.Rows[5].AllowInsertWaypoint)
	}

	originPerms := activePlan.Rows[1].Permissions
	if !originPerms.SystemLocked || originPerms.SystemLockReason != "" || originPerms.CanReorder ||
		originPerms.DeleteDisabledReason != "Origin waypoint cannot be deleted" ||
		originPerms.ReorderDisabledReason != "Origin waypoint cannot be reordered" ||
		!originPerms.CanCascadeLegMode || originPerms.OverrideFCCascadeCount != 2 {
		testingContext.Fatalf("origin active permissions mismatch: %+v", originPerms)
	}

	rohiniPerms := activePlan.Rows[2].Permissions
	if !rohiniPerms.SystemLocked || !rohiniPerms.CanReorder || rohiniPerms.AllowInsertWaypoint ||
		rohiniPerms.CanAddFCStop || !rohiniPerms.CanCascadeLegMode || !rohiniPerms.HasDrawer {
		testingContext.Fatalf("rohini permissions mismatch: %+v", rohiniPerms)
	}

	gandharviPerms := activePlan.Rows[3].Permissions
	if !gandharviPerms.AllowInsertWaypoint || gandharviPerms.CanCascadeLegMode || gandharviPerms.OverrideFCCascadeCount != 1 {
		testingContext.Fatalf("gandharvi permissions mismatch: %+v", gandharviPerms)
	}

	coloniaPerms := activePlan.Rows[4].Permissions
	if !coloniaPerms.SystemLocked || coloniaPerms.SystemLockReason != "Locked while Fleet Carrier is marked arrived at this waypoint" ||
		coloniaPerms.CanRemoveFCStop || !coloniaPerms.CanOverrideFCBadge || coloniaPerms.OverrideFCCascadeCount != 0 || coloniaPerms.HasDrawer {
		testingContext.Fatalf("colonia permissions mismatch: %+v", coloniaPerms)
	}

	sagAPerms := activePlan.Rows[6].Permissions
	if sagAPerms.SystemLocked || !sagAPerms.HasDrawer || !sagAPerms.IsNextFCStop ||
		!sagAPerms.CanRemoveFCStop || !sagAPerms.CanOverrideFCBadge || sagAPerms.OverrideFCCascadeCount != 0 {
		testingContext.Fatalf("sagittarius A* permissions mismatch: %+v", sagAPerms)
	}

	beaglePerms := activePlan.Rows[7].Permissions
	if beaglePerms.IsNextFCStop || beaglePerms.OverrideFCCascadeCount != 1 || !beaglePerms.CanAddFCStop {
		testingContext.Fatalf("beagle point permissions mismatch: %+v", beaglePerms)
	}

	// Completed status locks all structural edits and disables overrides/cascades.
	completedPlan := activePlan
	completedPlan.Expedition.Status = expedition.StatusCompleted
	expedition.PopulatePlanFrontiersAndPermissions(&completedPlan)
	for _, rowView := range completedPlan.Rows {
		if !rowView.Row.IsWaypoint() {
			continue
		}
		perms := rowView.Permissions
		if !perms.SystemLocked || perms.CanReorder || perms.CanAddFCStop || perms.CanRemoveFCStop || perms.CanOverrideFCBadge || perms.OverrideFCCascadeCount != 0 {
			testingContext.Fatalf("completed row %d permissions not locked: %+v", rowView.Row.ID, perms)
		}
	}
}

func TestPopulatePlanFrontiersAndPermissions_ManualVsSpanshCarrierRoutes(testingContext *testing.T) {
	baseRows := func() []expedition.RowView {
		return []expedition.RowView{
			{Row: expedition.Row{ID: 1, RowType: expedition.RowWaypoint, SystemName: "Sol", VisitedAt: "2026-01-01T00:00:00Z"}, Number: 1, VisitState: expedition.VisitVisited},
			{Row: expedition.Row{ID: 2, RowType: expedition.RowWaypoint, SystemName: "Rohini", FCStop: true, FCArrived: false}, Number: 2, VisitState: expedition.VisitFuture},
			{Row: expedition.Row{ID: 3, RowType: expedition.RowWaypoint, SystemName: "Colonia", FCStop: true, FCArrived: false}, Number: 3, VisitState: expedition.VisitFuture},
		}
	}

	// Manual route targeting next FC stop (row 2 at index 1):
	// Index 0 (< 1) has IsFcStopLocked=true; index 1 (== 1) stays editable with HasActiveCarrierRoute=true and IsNextFCStop=false.
	manualPlan := expedition.PlanView{
		Expedition:             expedition.Expedition{ID: 5, Status: expedition.StatusActive},
		NextFCStopRowID:        2,
		RoutedCarrierStopRowID: 2,
		ActiveCarrierRoute: &expedition.ActiveCarrierRouteState{
			ExpeditionID: 5,
			TargetRowID:  2,
			ManualMode:   true,
			Route:        expedition.CarrierRouteResult{ManualMode: true},
		},
		Rows: baseRows(),
	}
	expedition.PopulatePlanFrontiersAndPermissions(&manualPlan)

	if !manualPlan.Rows[0].Permissions.IsFcStopLocked || manualPlan.Rows[0].Permissions.CanAddFCStop {
		testingContext.Fatalf("expected row before manual target to lock FC stop changes: %+v", manualPlan.Rows[0].Permissions)
	}
	manualTarget := manualPlan.Rows[1].Permissions
	if manualTarget.IsFcStopLocked || manualTarget.SystemLocked || !manualTarget.HasActiveCarrierRoute || manualTarget.IsNextFCStop || manualTarget.IsSpanshRoutedFCStop || !manualTarget.CanRemoveFCStop {
		testingContext.Fatalf("manual target stop permissions mismatch: %+v", manualTarget)
	}

	// Spansh plotted route targeting row 3 at index 2:
	// Indices 0..2 (<= 2) have IsFcStopLocked=true; index 2 is Spansh-locked with active route lock reasons.
	spanshPlan := expedition.PlanView{
		Expedition:             expedition.Expedition{ID: 5, Status: expedition.StatusActive},
		NextFCStopRowID:        2,
		RoutedCarrierStopRowID: 3,
		ActiveCarrierRoute: &expedition.ActiveCarrierRouteState{
			ExpeditionID: 5,
			TargetRowID:  3,
			Route: expedition.CarrierRouteResult{
				Jumps: []expedition.CarrierRouteJump{{SystemName: "Colonia", DistanceLy: 400}},
			},
		},
		Rows: baseRows(),
	}
	expedition.PopulatePlanFrontiersAndPermissions(&spanshPlan)

	if !spanshPlan.Rows[1].Permissions.IsFcStopLocked || spanshPlan.Rows[1].Permissions.CanRemoveFCStop || spanshPlan.Rows[1].Permissions.IsNextFCStop {
		testingContext.Fatalf("expected preceding FC stop to lock during Spansh route: %+v", spanshPlan.Rows[1].Permissions)
	}
	spanshTarget := spanshPlan.Rows[2].Permissions
	wantRouteReason := "Locked while Fleet Carrier route to this waypoint is active"
	if !spanshTarget.IsSpanshRoutedFCStop || !spanshTarget.HasActiveCarrierRoute || !spanshTarget.IsFcStopLocked ||
		!spanshTarget.SystemLocked || spanshTarget.CanReorder ||
		spanshTarget.SystemLockReason != wantRouteReason ||
		spanshTarget.DeleteDisabledReason != wantRouteReason ||
		spanshTarget.ReorderDisabledReason != wantRouteReason {
		testingContext.Fatalf("spansh target permissions mismatch: %+v", spanshTarget)
	}
}

func TestBuildPlanPopulatesDefaultModeAndPermissions(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	_, proj := expeditiontest.NewProjections(dbConn)

	expeditionRecord, originRow := createExpeditionWithOriginRow(testingContext, dbConn, "BuildPlan Permissions", "")
	setRowSystemName(testingContext, dbConn, originRow.ID, "Sol")
	wp2 := mustInsert(testingContext, dbConn, expeditionRecord.ID, originRow.ID, expedition.RowWaypoint, 0)
	fcTrue := true
	sysName := "Colonia"
	if _, err := expedition.UpdateRow(dbConn, expedition.RowPatch{ID: wp2.ID, SystemName: &sysName, FCStop: &fcTrue}); err != nil {
		testingContext.Fatal(err)
	}
	if err := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-01-18T10:00:00Z", 0)
	}); err != nil {
		testingContext.Fatal(err)
	}

	planView, planErr := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if planErr != nil {
		testingContext.Fatalf("BuildPlan: %v", planErr)
	}
	wantDefaultModeID := modes.DefaultFallbackModeID(dbConn)
	if planView.DefaultModeID != wantDefaultModeID || wantDefaultModeID <= 0 {
		testingContext.Fatalf("DefaultModeID = %d, want %d", planView.DefaultModeID, wantDefaultModeID)
	}
	if planView.LastVisitedIndex != 0 || planView.LastFCArrivedIndex != -1 {
		testingContext.Fatalf("frontiers = (%d, %d), want (0, -1)", planView.LastVisitedIndex, planView.LastFCArrivedIndex)
	}
	if planView.Rows[0].Permissions == nil || !planView.Rows[0].Permissions.SystemLocked || !planView.Rows[1].Permissions.IsNextFCStop {
		testingContext.Fatalf("unexpected row permissions from BuildPlan: row0=%+v row1=%+v", planView.Rows[0].Permissions, planView.Rows[1].Permissions)
	}
}
