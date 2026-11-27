package expedition_test

import (
	"slices"
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestUndoRowDeletePreservesIDCoordsCarryOverAndPOIGuard(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(testingContext, dbConn,
		`{"timestamp":"2026-04-01T08:00:00Z","event":"LoadGame","Commander":"CMDR Undo","FID":"F1","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-04-01T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-01T09:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[-9530,-910,19808],"JumpDist":220.0}`,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"FSDJump","StarSystem":"Transit","SystemAddress":3003,"StarPos":[-9500,-900,19900],"JumpDist":40.0}`,
	)

	expeditionRecord, err := expedition.CreateWithWaypoints(dbConn, "Undo Coords", "Test", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, X: 0, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "Colonia", SystemAddress: 2002, EDSMID: 42, HasCoords: true, X: -9530, Y: -910, Z: 19808, CoordsSource: "edsm"},
		{SystemName: "Sagittarius A*"},
		{SystemName: "Beagle Point"},
	})
	if err != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", err)
	}
	if err := expedition.StartTx(dbConn, expeditionRecord.ID, "2026-04-01T08:05:00Z", 0); err != nil {
		testingContext.Fatalf("StartTx: %v", err)
	}

	initialRows, err := expedition.ListRows(dbConn, expeditionRecord.ID)
	if err != nil || len(initialRows) != 4 {
		testingContext.Fatalf("ListRows: len=%d err=%v", len(initialRows), err)
	}
	coloniaRow, sagARow, beagleRow := initialRows[1], initialRows[2], initialRows[3]
	if coloniaRow.DepartedAt != "2026-04-01T10:00:00Z" || !coloniaRow.HasCoords {
		testingContext.Fatalf("expected Colonia visited+departed with coords, got %+v", coloniaRow)
	}

	history := expedition.NewUndoHistory(expedition.DefaultMaxUndoEntries)

	// Deleting a visited waypoint and undoing must restore exact original row ID, departed_at, and coordinates.
	beforeDelete := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	if _, err := expedition.DeleteRowTx(dbConn, coloniaRow.ID); err != nil {
		testingContext.Fatalf("DeleteRowTx: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Delete Waypoint", Snapshot: beforeDelete})

	undoDelete, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoDelete.Applied {
		testingContext.Fatalf("Undo delete: outcome=%+v err=%v", undoDelete, err)
	}
	restoredColonia := mustGetRow(testingContext, dbConn, coloniaRow.ID)
	if restoredColonia != coloniaRow {
		testingContext.Fatalf("restored row mismatch:\ngot  %+v\nwant %+v", restoredColonia, coloniaRow)
	}
	if !undoDelete.ChangedEvents.Has(events.AppExpeditionModesChanged) || !undoDelete.ChangedEvents.Has(events.AppExpeditionPlanChanged) {
		testingContext.Fatalf("expected plan and modes events on visited-row undo, got %v", undoDelete.ChangedEvents.Names())
	}

	// Redo removes Colonia again, then Undo brings it back once more.
	redoDelete, err := history.Redo(dbConn, expeditionRecord.ID)
	if err != nil || !redoDelete.Applied {
		testingContext.Fatalf("Redo delete: outcome=%+v err=%v", redoDelete, err)
	}
	if _, err := expedition.GetRow(dbConn, coloniaRow.ID); err == nil {
		testingContext.Fatalf("expected Colonia row %d to be deleted after Redo", coloniaRow.ID)
	}
	if _, err := history.Undo(dbConn, expeditionRecord.ID); err != nil {
		testingContext.Fatalf("second Undo delete: %v", err)
	}

	// Coordinate carry-over: editing notes before async coords arrive preserves the resolved coordinates on undo.
	beforeNotes := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	noteText := "Check supermassive black hole"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: sagARow.ID, Notes: &noteText}); err != nil {
		testingContext.Fatalf("UpdateRowTx notes: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Edit Notes", Snapshot: beforeNotes})

	sagCoords := reference.Vec3{X: 25.21875, Y: -20.90625, Z: 25899.96875}
	if err := expedition.SetRowCoords(dbConn, sagARow.ID, "Sagittarius A*", 999999, 777, &sagCoords, "edsm"); err != nil {
		testingContext.Fatalf("SetRowCoords: %v", err)
	}

	undoNotes, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoNotes.Applied {
		testingContext.Fatalf("Undo notes: outcome=%+v err=%v", undoNotes, err)
	}
	revertedSagA := mustGetRow(testingContext, dbConn, sagARow.ID)
	if revertedSagA.Notes != "" || !revertedSagA.HasCoords || revertedSagA.CoordsSource != "edsm" || revertedSagA.SystemAddress != 999999 {
		testingContext.Fatalf("expected notes reverted and resolved coords carried over, got %+v", revertedSagA)
	}
	if !undoNotes.NeedsCoordResolution {
		testingContext.Fatalf("expected NeedsCoordResolution=true because Beagle Point still lacks coords")
	}

	// Changing a system name and reverting to an unresolved system must NOT carry over the other system's coords.
	beforeRename := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	replacementSystem := "Alpha Centauri"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: beagleRow.ID, SystemName: &replacementSystem}); err != nil {
		testingContext.Fatalf("UpdateRowTx system: %v", err)
	}
	alphaCoords := reference.Vec3{X: 3.03125, Y: -0.09375, Z: 3.15625}
	if err := expedition.SetRowCoords(dbConn, beagleRow.ID, replacementSystem, 5555, 88, &alphaCoords, "edsm"); err != nil {
		testingContext.Fatalf("SetRowCoords Alpha Centauri: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Edit System", Snapshot: beforeRename})

	undoRename, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoRename.Applied || !undoRename.NeedsCoordResolution {
		testingContext.Fatalf("Undo rename: outcome=%+v err=%v", undoRename, err)
	}
	revertedBeagle := mustGetRow(testingContext, dbConn, beagleRow.ID)
	if revertedBeagle.SystemName != "Beagle Point" || revertedBeagle.HasCoords || revertedBeagle.CoordsSource != "" {
		testingContext.Fatalf("expected Beagle Point without Alpha Centauri coords, got %+v", revertedBeagle)
	}

	// SetRowPOIIfBlank rejects a stale system_name from an in-flight GEC worker after Ctrl-Z.
	if err := expedition.SetRowPOIIfBlank(dbConn, beagleRow.ID, "Alpha Centauri", "Hutton Orbital"); err != nil {
		testingContext.Fatalf("SetRowPOIIfBlank stale: %v", err)
	}
	afterStalePOI := mustGetRow(testingContext, dbConn, beagleRow.ID)
	if afterStalePOI.POIName != "" {
		testingContext.Fatalf("expected stale system_name POI update ignored, got poi=%q", afterStalePOI.POIName)
	}
	if err := expedition.SetRowPOIIfBlank(dbConn, beagleRow.ID, "Beagle Point", "Beagle Point"); err != nil {
		testingContext.Fatalf("SetRowPOIIfBlank matching: %v", err)
	}
	if err := expedition.SetRowPOIIfBlank(dbConn, beagleRow.ID, "Beagle Point", "Overwrite Attempt"); err != nil {
		testingContext.Fatalf("SetRowPOIIfBlank non-blank: %v", err)
	}
	afterMatchingPOI := mustGetRow(testingContext, dbConn, beagleRow.ID)
	if afterMatchingPOI.POIName != "Beagle Point" {
		testingContext.Fatalf("expected matching blank POI set once, got poi=%q", afterMatchingPOI.POIName)
	}
}

func TestRestoreSnapshotRemapsDeletedCustomModeIDs(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(testingContext, dbConn,
		`{"timestamp":"2026-04-04T08:00:00Z","event":"LoadGame","Commander":"CMDR Custom","FID":"F4","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-04-04T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-04T09:00:00Z","event":"FSDJump","StarSystem":"SysA","SystemAddress":1002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	)

	customMode, err := modes.Create(dbConn, "Exobiology Survey", "Custom sampling mode", "#00CCAA")
	if err != nil {
		testingContext.Fatalf("modes.Create: %v", err)
	}
	fallbackModeID := expeditiontest.LookupDefaultModes(testingContext, dbConn).Fallback
	if fallbackModeID <= 0 || fallbackModeID == customMode.ID {
		testingContext.Fatalf("expected distinct fallback mode ID, got fallback=%d custom=%d", fallbackModeID, customMode.ID)
	}

	expeditionRecord, err := expedition.CreateWithWaypoints(dbConn, "Deleted Mode Restore", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysA", SystemAddress: 1002, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysB", HasCoords: true, CoordsSource: "edsm"},
	})
	if err != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", err)
	}
	if err := expedition.StartTx(dbConn, expeditionRecord.ID, "2026-04-04T08:05:00Z", customMode.ID); err != nil {
		testingContext.Fatalf("StartTx: %v", err)
	}
	rows, err := expedition.ListRows(dbConn, expeditionRecord.ID)
	if err != nil || len(rows) != 3 {
		testingContext.Fatalf("ListRows: len=%d err=%v", len(rows), err)
	}
	originRow, sysARow, sysBRow := rows[0], rows[1], rows[2]
	if sysARow.LegModeID != customMode.ID || originRow.LegModeID != 0 || sysBRow.LegModeID != 0 {
		testingContext.Fatalf("expected customMode on SysA and 0 on origin/unvisited, got origin=%d SysA=%d SysB=%d",
			originRow.LegModeID, sysARow.LegModeID, sysBRow.LegModeID)
	}

	// Capture a snapshot referencing the custom mode, edit notes, then delete the custom mode before restoring.
	snapshotWithDeletedMode := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	updatedNote := "After snapshot"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: sysBRow.ID, Notes: &updatedNote}); err != nil {
		testingContext.Fatalf("UpdateRowTx note: %v", err)
	}
	if err := modes.DeleteTx(dbConn, customMode.ID); err != nil {
		testingContext.Fatalf("modes.DeleteTx: %v", err)
	}

	if _, err := expedition.RestoreSnapshotTx(dbConn, snapshotWithDeletedMode); err != nil {
		testingContext.Fatalf("RestoreSnapshotTx after mode delete: %v", err)
	}
	restoredExp, err := expedition.Get(dbConn, expeditionRecord.ID)
	if err != nil {
		testingContext.Fatalf("Get expedition after restore: %v", err)
	}
	restoredOrigin := mustGetRow(testingContext, dbConn, originRow.ID)
	restoredSysA := mustGetRow(testingContext, dbConn, sysARow.ID)
	restoredSysB := mustGetRow(testingContext, dbConn, sysBRow.ID)
	if restoredExp.ActiveModeID != fallbackModeID {
		testingContext.Fatalf("expected deleted ActiveModeID remapped to fallback %d, got %d", fallbackModeID, restoredExp.ActiveModeID)
	}
	if restoredSysA.LegModeID != fallbackModeID {
		testingContext.Fatalf("expected deleted SysA LegModeID remapped to fallback %d, got %d", fallbackModeID, restoredSysA.LegModeID)
	}
	if restoredOrigin.LegModeID != 0 || restoredSysB.LegModeID != 0 || restoredSysB.Notes != "" {
		testingContext.Fatalf("expected origin/unvisited LegModeID=0 and SysB notes cleared, got origin=%d SysB=%+v",
			restoredOrigin.LegModeID, restoredSysB)
	}
}

func TestUndoStartScopedOverrideCarrierAndLiveReevaluate(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(testingContext, dbConn,
		`{"timestamp":"2026-04-03T08:00:00Z","event":"LoadGame","Commander":"CMDR Main","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-04-03T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-03T09:00:00Z","event":"FSDJump","StarSystem":"SysA","SystemAddress":1002,"StarPos":[100,0,0],"JumpDist":100.0}`,
		`{"timestamp":"2026-04-03T09:30:00Z","event":"LoadGame","Commander":"CMDR Alt","FID":"F202","Ship":"cobramkiii","Ship_Localised":"Cobra Mk III","ShipID":2,"Odyssey":true}`,
		`{"timestamp":"2026-04-03T09:35:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
	)
	idsByName := expeditiontest.ProfileIDsByName(testingContext, dbConn)
	if len(idsByName) != 2 {
		testingContext.Fatalf("ProfileIDsByName: len=%d map=%+v", len(idsByName), idsByName)
	}
	mainProfileID := idsByName["CMDR Main"]

	expeditionRecord, err := expedition.CreateWithWaypoints(dbConn, "Scoped Carrier Live", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysA", SystemAddress: 1002, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysB", SystemAddress: 1003, HasCoords: true, CoordsSource: "edsm"},
		{SystemName: "SysC", SystemAddress: 1004, HasCoords: true, CoordsSource: "edsm"},
	})
	if err != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", err)
	}
	history := expedition.NewUndoHistory(expedition.DefaultMaxUndoEntries)

	// StartScopedTx undo restores planning status, commander_scope, and expedition_commanders.
	beforeStart := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	if err := expedition.StartScopedTx(dbConn, expeditionRecord.ID, "2026-04-03T08:05:00Z", 0, expedition.ScopeSelected, []int64{mainProfileID}); err != nil {
		testingContext.Fatalf("StartScopedTx: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Engage Expedition", Snapshot: beforeStart})

	undoStart, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoStart.Applied {
		testingContext.Fatalf("Undo StartScopedTx: outcome=%+v err=%v", undoStart, err)
	}
	if !undoStart.ChangedEvents.Has(events.AppExpeditionBoundsChanged) || !undoStart.ChangedEvents.Has(events.AppExpeditionModesChanged) {
		testingContext.Fatalf("expected bounds and modes events on StartScopedTx undo, got %v", undoStart.ChangedEvents.Names())
	}
	revertedExp, err := expedition.Get(dbConn, expeditionRecord.ID)
	if err != nil || revertedExp.Status != expedition.StatusPlanning || revertedExp.CommanderScope != expedition.ScopeAllEnabled || len(revertedExp.CommanderProfileIDs) != 0 {
		testingContext.Fatalf("expected reverted planning scope, got %+v (%v)", revertedExp, err)
	}
	if _, err := history.Redo(dbConn, expeditionRecord.ID); err != nil {
		testingContext.Fatalf("Redo StartScopedTx: %v", err)
	}
	restartedExp, err := expedition.Get(dbConn, expeditionRecord.ID)
	if err != nil || restartedExp.CommanderScope != expedition.ScopeSelected || !slices.Equal(restartedExp.CommanderProfileIDs, []int64{mainProfileID}) {
		testingContext.Fatalf("expected selected scope restored on redo, got %+v (%v)", restartedExp, err)
	}

	// OverrideCarrierStopArrival undo restores cascaded fc_arrived flags and cleared active_carrier_route.
	rows, err := expedition.ListRows(dbConn, expeditionRecord.ID)
	if err != nil || len(rows) != 4 {
		testingContext.Fatalf("ListRows: len=%d err=%v", len(rows), err)
	}
	sysARow, sysBRow, sysCRow := rows[1], rows[2], rows[3]
	fcStopTrue := true
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: sysARow.ID, FCStop: &fcStopTrue}); err != nil {
		testingContext.Fatalf("enable SysA FCStop: %v", err)
	}
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: sysBRow.ID, FCStop: &fcStopTrue}); err != nil {
		testingContext.Fatalf("enable SysB FCStop: %v", err)
	}
	routeState := expedition.ActiveCarrierRouteState{
		ExpeditionID: expeditionRecord.ID,
		TargetRowID:  sysARow.ID,
		Route: expedition.CarrierRouteResult{
			Origin:      "Sol",
			Destination: "SysA",
			TotalJumps:  1,
			Jumps:       []expedition.CarrierRouteJump{{SystemName: "SysA", DistanceLy: 100, TritiumUsed: 30}},
		},
	}
	if err := expedition.SaveActiveCarrierRoute(dbConn, routeState); err != nil {
		testingContext.Fatalf("SaveActiveCarrierRoute: %v", err)
	}

	beforeOverride := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	if _, err := expedition.SetCarrierArrivalOverrideTx(dbConn, sysBRow.ID, true); err != nil {
		testingContext.Fatalf("SetCarrierArrivalOverrideTx: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Override Carrier Arrival", Snapshot: beforeOverride})

	if activeRoute := mustGetActiveCarrierRoute(testingContext, dbConn, expeditionRecord.ID); activeRoute != nil {
		testingContext.Fatalf("expected active carrier route cleared after override, got %+v", activeRoute)
	}
	undoOverride, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoOverride.Applied || !undoOverride.CarrierRouteRestored {
		testingContext.Fatalf("Undo override: outcome=%+v err=%v", undoOverride, err)
	}
	afterUndoSysA := mustGetRow(testingContext, dbConn, sysARow.ID)
	afterUndoSysB := mustGetRow(testingContext, dbConn, sysBRow.ID)
	restoredRoute := mustGetActiveCarrierRoute(testingContext, dbConn, expeditionRecord.ID)
	if afterUndoSysA.FCArrived || afterUndoSysB.FCArrived || restoredRoute == nil || restoredRoute.TargetRowID != sysARow.ID {
		testingContext.Fatalf("expected cascaded fc_arrived and route restored, got A=%v B=%v route=%+v",
			afterUndoSysA.FCArrived, afterUndoSysB.FCArrived, restoredRoute)
	}

	// Active expedition undo re-evaluates a live FSDJump ingested after the snapshot so live progress is never lost.
	beforeNote := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	sysCNote := "Temporary note"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: sysCRow.ID, Notes: &sysCNote}); err != nil {
		testingContext.Fatalf("UpdateRowTx SysC note: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Edit Notes", Snapshot: beforeNote})

	journaltest.IngestLines(testingContext, dbConn,
		`{"timestamp":"2026-04-03T11:00:00Z","event":"LoadGame","Commander":"CMDR Main","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-04-03T11:10:00Z","event":"FSDJump","StarSystem":"SysB","SystemAddress":1003,"StarPos":[200,0,0],"JumpDist":100.0}`,
	)
	if changed, err := expedition.ReevaluateTx(dbConn, expeditionRecord.ID); err != nil || !changed {
		testingContext.Fatalf("ReevaluateTx after live FSDJump: changed=%v err=%v", changed, err)
	}

	undoLiveNote, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoLiveNote.Applied {
		testingContext.Fatalf("Undo note after live FSDJump: outcome=%+v err=%v", undoLiveNote, err)
	}
	finalSysB := mustGetRow(testingContext, dbConn, sysBRow.ID)
	finalSysC := mustGetRow(testingContext, dbConn, sysCRow.ID)
	if finalSysB.VisitedAt != "2026-04-03T11:10:00Z" || finalSysC.Notes != "" {
		testingContext.Fatalf("expected live SysB visit preserved and SysC note reverted, got SysB.VisitedAt=%q SysC.Notes=%q",
			finalSysB.VisitedAt, finalSysC.Notes)
	}
	if undoLiveNote.ChangedEvents != events.NewAppSet(events.AppExpeditionPlanChanged) {
		testingContext.Fatalf("expected ONLY AppExpeditionPlanChanged when live visit stayed identical, got %v", undoLiveNote.ChangedEvents.Names())
	}
}
