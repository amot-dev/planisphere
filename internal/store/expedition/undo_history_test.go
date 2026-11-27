package expedition_test

import (
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestUndoFlightModesLegModesAndMinimalChangedEvents(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(testingContext, dbConn,
		`{"timestamp":"2026-04-02T08:00:00Z","event":"LoadGame","Commander":"CMDR Modes","FID":"F2","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-04-02T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-02T09:00:00Z","event":"FSDJump","StarSystem":"SysA","SystemAddress":1002,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-04-02T10:00:00Z","event":"FSDJump","StarSystem":"SysB","SystemAddress":1003,"StarPos":[20,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-04-02T11:00:00Z","event":"FSDJump","StarSystem":"SysC","SystemAddress":1004,"StarPos":[30,0,0],"JumpDist":10.0}`,
	)

	defaultModes := expeditiontest.LookupDefaultModes(testingContext, dbConn)
	modeOneID, modeTwoID, modeThreeID := defaultModes.Exploration, defaultModes.Tourism, defaultModes.Travel

	expeditionRecord, err := expedition.CreateWithWaypoints(dbConn, "Mode Undo", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysA", SystemAddress: 1002, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysB", SystemAddress: 1003, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysC", SystemAddress: 1004, HasCoords: true, CoordsSource: "journal"},
		{SystemName: "SysD", HasCoords: true, CoordsSource: "edsm"},
	})
	if err != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", err)
	}
	if err := expedition.StartTx(dbConn, expeditionRecord.ID, "2026-04-02T08:05:00Z", modeOneID); err != nil {
		testingContext.Fatalf("StartTx: %v", err)
	}
	rows, err := expedition.ListRows(dbConn, expeditionRecord.ID)
	if err != nil || len(rows) != 5 {
		testingContext.Fatalf("ListRows: len=%d err=%v", len(rows), err)
	}
	sysARow, sysBRow, sysCRow, sysDRow := rows[1], rows[2], rows[3], rows[4]

	history := expedition.NewUndoHistory(expedition.DefaultMaxUndoEntries)

	// Plain unvisited row note edit must emit ONLY AppExpeditionPlanChanged on undo.
	beforeNote := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	note := "Planned fuel stop"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: sysDRow.ID, Notes: &note}); err != nil {
		testingContext.Fatalf("UpdateRowTx note: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Edit Notes", Snapshot: beforeNote})

	undoNote, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoNote.Applied {
		testingContext.Fatalf("Undo note: outcome=%+v err=%v", undoNote, err)
	}
	if undoNote.ChangedEvents != events.NewAppSet(events.AppExpeditionPlanChanged) {
		testingContext.Fatalf("expected ONLY AppExpeditionPlanChanged on plain note undo, got %v", undoNote.ChangedEvents.Names())
	}

	// SetActiveModeTx undo and redo restore active_mode_id and emit AppExpeditionModesChanged.
	beforeActiveMode := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	if err := expedition.SetActiveModeTx(dbConn, expeditionRecord.ID, modeTwoID); err != nil {
		testingContext.Fatalf("SetActiveModeTx: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Change Flight Mode", Snapshot: beforeActiveMode})

	undoActiveMode, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoActiveMode.Applied || !undoActiveMode.ChangedEvents.Has(events.AppExpeditionModesChanged) {
		testingContext.Fatalf("Undo SetActiveMode: outcome=%+v err=%v", undoActiveMode, err)
	}
	reloadedExp, err := expedition.Get(dbConn, expeditionRecord.ID)
	if err != nil || reloadedExp.ActiveModeID != modeOneID {
		testingContext.Fatalf("expected ActiveModeID=%d after undo, got %d (%v)", modeOneID, reloadedExp.ActiveModeID, err)
	}
	redoActiveMode, err := history.Redo(dbConn, expeditionRecord.ID)
	if err != nil || !redoActiveMode.Applied || !redoActiveMode.ChangedEvents.Has(events.AppExpeditionModesChanged) {
		testingContext.Fatalf("Redo SetActiveMode: outcome=%+v err=%v", redoActiveMode, err)
	}
	if reloadedExp, err = expedition.Get(dbConn, expeditionRecord.ID); err != nil || reloadedExp.ActiveModeID != modeTwoID {
		testingContext.Fatalf("expected ActiveModeID=%d after redo, got %d (%v)", modeTwoID, reloadedExp.ActiveModeID, err)
	}

	// Single leg mode edit (cascade=false) undo and redo.
	beforeSingleLeg := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	if _, err := expedition.SetLegModeTx(dbConn, sysBRow.ID, modeTwoID, false); err != nil {
		testingContext.Fatalf("SetLegModeTx single: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Change Leg Mode", Snapshot: beforeSingleLeg})

	undoSingleLeg, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoSingleLeg.Applied || !undoSingleLeg.ChangedEvents.Has(events.AppExpeditionModesChanged) {
		testingContext.Fatalf("Undo single SetLegMode: outcome=%+v err=%v", undoSingleLeg, err)
	}
	if reloadedSysB := mustGetRow(testingContext, dbConn, sysBRow.ID); reloadedSysB.LegModeID != modeOneID {
		testingContext.Fatalf("expected SysB LegModeID=%d after undo, got %d", modeOneID, reloadedSysB.LegModeID)
	}
	if _, err := history.Redo(dbConn, expeditionRecord.ID); err != nil {
		testingContext.Fatalf("Redo single SetLegMode: %v", err)
	}

	// Cascaded leg mode edit (cascade=true) overwrites SysA, SysB, SysC; undo restores all distinct previous modes.
	beforeCascade := mustCaptureSnapshot(testingContext, dbConn, expeditionRecord.ID)
	if _, err := expedition.SetLegModeTx(dbConn, sysARow.ID, modeThreeID, true); err != nil {
		testingContext.Fatalf("SetLegModeTx cascade: %v", err)
	}
	history.Push(expeditionRecord.ID, expedition.UndoEntry{Label: "Change Leg Mode", Snapshot: beforeCascade})

	undoCascade, err := history.Undo(dbConn, expeditionRecord.ID)
	if err != nil || !undoCascade.Applied || !undoCascade.ChangedEvents.Has(events.AppExpeditionModesChanged) {
		testingContext.Fatalf("Undo cascade SetLegMode: outcome=%+v err=%v", undoCascade, err)
	}
	afterUndoSysA := mustGetRow(testingContext, dbConn, sysARow.ID)
	afterUndoSysB := mustGetRow(testingContext, dbConn, sysBRow.ID)
	afterUndoSysC := mustGetRow(testingContext, dbConn, sysCRow.ID)
	if afterUndoSysA.LegModeID != modeOneID || afterUndoSysB.LegModeID != modeTwoID || afterUndoSysC.LegModeID != modeOneID {
		testingContext.Fatalf("unexpected cascaded undo leg modes: A=%d B=%d C=%d",
			afterUndoSysA.LegModeID, afterUndoSysB.LegModeID, afterUndoSysC.LegModeID)
	}
	redoCascade, err := history.Redo(dbConn, expeditionRecord.ID)
	if err != nil || !redoCascade.Applied {
		testingContext.Fatalf("Redo cascade SetLegMode: outcome=%+v err=%v", redoCascade, err)
	}
	afterRedoSysA := mustGetRow(testingContext, dbConn, sysARow.ID)
	afterRedoSysB := mustGetRow(testingContext, dbConn, sysBRow.ID)
	afterRedoSysC := mustGetRow(testingContext, dbConn, sysCRow.ID)
	if afterRedoSysA.LegModeID != modeThreeID || afterRedoSysB.LegModeID != modeThreeID || afterRedoSysC.LegModeID != modeThreeID {
		testingContext.Fatalf("unexpected cascaded redo leg modes: A=%d B=%d C=%d",
			afterRedoSysA.LegModeID, afterRedoSysB.LegModeID, afterRedoSysC.LegModeID)
	}
}

func TestUndoHistoryCapacityRedoClearIsolationAndClearAll(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	expA, rowA := createExpeditionWithOriginRow(testingContext, dbConn, "Expedition A", "")
	expB, rowB := createExpeditionWithOriginRow(testingContext, dbConn, "Expedition B", "")

	history := expedition.NewUndoHistory(3)

	// Push 4 edits on expA to verify oldest entry eviction at capacity 3.
	for step := 1; step <= 4; step++ {
		before := mustCaptureSnapshot(testingContext, dbConn, expA.ID)
		note := string(rune('A' + step - 1))
		if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: rowA.ID, Notes: &note}); err != nil {
			testingContext.Fatalf("UpdateRowTx expA step %d: %v", step, err)
		}
		history.Push(expA.ID, expedition.UndoEntry{Label: "Edit " + note, Snapshot: before})
	}

	// Push 1 edit on expB to verify per-expedition stack isolation.
	beforeB := mustCaptureSnapshot(testingContext, dbConn, expB.ID)
	noteB := "Isolated B"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: rowB.ID, Notes: &noteB}); err != nil {
		testingContext.Fatalf("UpdateRowTx expB: %v", err)
	}
	history.Push(expB.ID, expedition.UndoEntry{Label: "Edit B", Snapshot: beforeB})

	for expectedRemaining := 2; expectedRemaining >= 0; expectedRemaining-- {
		outcome, err := history.Undo(dbConn, expA.ID)
		if err != nil || !outcome.Applied {
			testingContext.Fatalf("Undo expA (remaining %d): outcome=%+v err=%v", expectedRemaining, outcome, err)
		}
		if outcome.CanUndo != (expectedRemaining > 0) || !outcome.CanRedo {
			testingContext.Fatalf("unexpected canUndo/canRedo at remaining %d: %+v", expectedRemaining, outcome)
		}
	}
	fourthUndo, err := history.Undo(dbConn, expA.ID)
	if err != nil || fourthUndo.Applied {
		testingContext.Fatalf("expected capacity eviction after 3 undos, got outcome=%+v err=%v", fourthUndo, err)
	}
	reloadedA := mustGetRow(testingContext, dbConn, rowA.ID)
	if reloadedA.Notes != "A" {
		testingContext.Fatalf("expected expA note 'A' after capacity limit, got %q", reloadedA.Notes)
	}

	canUndoB, canRedoB := history.CanUndoRedo(expB.ID)
	if !canUndoB || canRedoB {
		testingContext.Fatalf("expected expB stack isolated (true, false), got (%v, %v)", canUndoB, canRedoB)
	}

	beforeBranch := mustCaptureSnapshot(testingContext, dbConn, expA.ID)
	branchNote := "Branch"
	if _, err := expedition.UpdateRowTx(dbConn, expedition.RowPatch{ID: rowA.ID, Notes: &branchNote}); err != nil {
		testingContext.Fatalf("UpdateRowTx branch: %v", err)
	}
	history.Push(expA.ID, expedition.UndoEntry{Label: "Branch", Snapshot: beforeBranch})
	if canUndoA, canRedoA := history.CanUndoRedo(expA.ID); !canUndoA || canRedoA {
		testingContext.Fatalf("expected redo stack cleared on forward push, got canUndo=%v canRedo=%v", canUndoA, canRedoA)
	}

	history.ClearAll()
	for _, expID := range []int64{expA.ID, expB.ID} {
		if canUndo, canRedo := history.CanUndoRedo(expID); canUndo || canRedo {
			testingContext.Fatalf("expected expedition %d cleared after ClearAll, got (%v, %v)", expID, canUndo, canRedo)
		}
		if outcome, err := history.Undo(dbConn, expID); err != nil || outcome.Applied {
			testingContext.Fatalf("Undo after ClearAll(%d): outcome=%+v err=%v", expID, outcome, err)
		}
	}
}
