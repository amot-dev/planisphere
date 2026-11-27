package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

func TestAppUndoRedoRowsModesAndCompletePrune(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	eventsRecorder := captureAppEvents(testingContext, testApp)

	insertTestJournalEvent(testingContext, testApp, "2026-04-05T08:00:00Z", "LoadGame", "lg1",
		`{"timestamp":"2026-04-05T08:00:00Z","event":"LoadGame","Commander":"CMDR Facade","FID":"F1","Ship":"mandalay"}`)
	insertTestJournalEvent(testingContext, testApp, "2026-04-05T08:05:00Z", "Location", "loc1",
		`{"timestamp":"2026-04-05T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`)
	insertTestJournalEvent(testingContext, testApp, "2026-04-05T09:00:00Z", "FSDJump", "jmp1",
		`{"timestamp":"2026-04-05T09:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[-9530,-910,19808],"JumpDist":220.0}`)

	createdExp, err := testApp.CreateExpedition("Facade Undo", "End to end")
	if err != nil {
		testingContext.Fatalf("CreateExpedition: %v", err)
	}
	initialPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if len(initialPlan.Rows) != 1 {
		testingContext.Fatalf("expected 1 initial row, got %d", len(initialPlan.Rows))
	}
	originRowID := initialPlan.Rows[0].Row.ID
	solName := "Sol"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: originRowID, SystemName: &solName}); err != nil {
		testingContext.Fatalf("UpdateRow origin: %v", err)
	}

	coloniaRow, err := testApp.AddRow(createdExp.ID, originRowID, expedition.RowWaypoint, 0)
	if err != nil {
		testingContext.Fatalf("AddRow Colonia: %v", err)
	}
	coloniaName := "Colonia"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: coloniaRow.ID, SystemName: &coloniaName}); err != nil {
		testingContext.Fatalf("UpdateRow Colonia: %v", err)
	}

	// Edit notes and repeat an identical no-op edit; no-op must push no entry and emit no DataChanged event.
	noteValue := "Refuel stop"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: coloniaRow.ID, Notes: &noteValue}); err != nil {
		testingContext.Fatalf("UpdateRow notes: %v", err)
	}
	testApp.backgroundWg.Wait()
	eventsRecorder.Reset()
	if err := testApp.UpdateRow(expedition.RowPatch{ID: coloniaRow.ID, Notes: &noteValue}); err != nil {
		testingContext.Fatalf("UpdateRow no-op notes: %v", err)
	}
	if noopChanges := eventsRecorder.DataChanges(); len(noopChanges) != 0 {
		testingContext.Fatalf("expected no DataChanged event on no-op UpdateRow, got %+v", noopChanges)
	}

	modeList, err := modes.List(testApp.db)
	if err != nil || len(modeList) < 2 {
		testingContext.Fatalf("modes.List: len=%d err=%v", len(modeList), err)
	}
	modeOneID, modeTwoID := modeList[0].ID, modeList[1].ID
	if err := testApp.StartExpedition(createdExp.ID, "2026-04-05T08:05:00Z", modeOneID); err != nil {
		testingContext.Fatalf("StartExpedition: %v", err)
	}

	if err := testApp.SetExpeditionFlightMode(createdExp.ID, modeTwoID); err != nil {
		testingContext.Fatalf("SetExpeditionFlightMode: %v", err)
	}
	testApp.backgroundWg.Wait()
	eventsRecorder.Reset()
	if err := testApp.SetExpeditionFlightMode(createdExp.ID, modeTwoID); err != nil {
		testingContext.Fatalf("SetExpeditionFlightMode no-op: %v", err)
	}
	if noopChanges := eventsRecorder.DataChanges(); len(noopChanges) != 0 {
		testingContext.Fatalf("expected no DataChanged event on no-op SetExpeditionFlightMode, got %+v", noopChanges)
	}

	if err := testApp.SetLegMode(coloniaRow.ID, modeTwoID, false); err != nil {
		testingContext.Fatalf("SetLegMode: %v", err)
	}

	testApp.backgroundWg.Wait()
	eventsRecorder.Reset()

	undoLegMode, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoLegMode.Applied || undoLegMode.Label != "Change Leg Mode" {
		testingContext.Fatalf("UndoExpedition leg mode: res=%+v err=%v", undoLegMode, err)
	}
	undoFlightMode, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoFlightMode.Applied || undoFlightMode.Label != "Change Flight Mode" {
		testingContext.Fatalf("UndoExpedition flight mode: res=%+v err=%v", undoFlightMode, err)
	}
	modeUndoChanges := eventsRecorder.DataChanges()
	if len(modeUndoChanges) != 2 || !slices.Contains(modeUndoChanges[0].Domains, "stats") || !slices.Contains(modeUndoChanges[0].Domains, "plan") {
		testingContext.Fatalf("expected mode undo to emit stats and plan domains, got %+v", modeUndoChanges)
	}

	// Add an empty region header and empty waypoint, then CompleteExpedition (which prunes both) and verify Undo restores them.
	emptyRegion, err := testApp.AddRow(createdExp.ID, coloniaRow.ID, expedition.RowRegion, 1)
	if err != nil {
		testingContext.Fatalf("AddRow empty region: %v", err)
	}
	emptyWaypoint, err := testApp.AddRow(createdExp.ID, emptyRegion.ID, expedition.RowWaypoint, 0)
	if err != nil {
		testingContext.Fatalf("AddRow empty waypoint: %v", err)
	}
	if err := testApp.CompleteExpedition(createdExp.ID); err != nil {
		testingContext.Fatalf("CompleteExpedition: %v", err)
	}
	completedPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if completedPlan.Expedition.Status != expedition.StatusCompleted || len(completedPlan.Rows) != 2 {
		testingContext.Fatalf("expected completed expedition with 2 pruned rows, got status=%q rows=%d",
			completedPlan.Expedition.Status, len(completedPlan.Rows))
	}

	undoComplete, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoComplete.Applied || undoComplete.Label != "Complete Expedition" {
		testingContext.Fatalf("UndoExpedition complete: res=%+v err=%v", undoComplete, err)
	}
	reopenedPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if reopenedPlan.Expedition.Status != expedition.StatusActive || reopenedPlan.Expedition.EndTimestamp != "" || len(reopenedPlan.Rows) != 4 {
		testingContext.Fatalf("expected active status and 4 rows after undoing complete, got status=%q end=%q rows=%d",
			reopenedPlan.Expedition.Status, reopenedPlan.Expedition.EndTimestamp, len(reopenedPlan.Rows))
	}
	if reopenedPlan.Rows[2].Row.ID != emptyRegion.ID || reopenedPlan.Rows[3].Row.ID != emptyWaypoint.ID {
		testingContext.Fatalf("expected pruned row IDs (%d, %d) restored, got (%d, %d)",
			emptyRegion.ID, emptyWaypoint.ID, reopenedPlan.Rows[2].Row.ID, reopenedPlan.Rows[3].Row.ID)
	}
	redoComplete, err := testApp.RedoExpedition(createdExp.ID)
	if err != nil || !redoComplete.Applied {
		testingContext.Fatalf("RedoExpedition complete: res=%+v err=%v", redoComplete, err)
	}
	if reCompletedPlan := mustGetPlan(testingContext, testApp, createdExp.ID); reCompletedPlan.Expedition.Status != expedition.StatusCompleted || len(reCompletedPlan.Rows) != 2 {
		testingContext.Fatalf("expected completed status and 2 rows after redo, got status=%q rows=%d",
			reCompletedPlan.Expedition.Status, len(reCompletedPlan.Rows))
	}
}

func TestAppUndoInvalidatesExportSnapshotScopeChangeAndDBSwitch(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	currentExportTime := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	previousTimeNow := timeNow
	timeNow = func() time.Time { return currentExportTime }
	testingContext.Cleanup(func() { timeNow = previousTimeNow })

	insertTestJournalEvent(testingContext, testApp, "2026-04-06T09:00:00Z", "LoadGame", "cmdr1",
		`{"timestamp":"2026-04-06T09:00:00Z","event":"LoadGame","Commander":"CMDR Alpha","FID":"F101","Ship":"Mandalay"}`)
	insertTestJournalEvent(testingContext, testApp, "2026-04-06T09:05:00Z", "Location", "sol1",
		`{"timestamp":"2026-04-06T09:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`)
	insertTestJournalEvent(testingContext, testApp, "2026-04-06T09:30:00Z", "FSDJump", "dest1",
		`{"timestamp":"2026-04-06T09:30:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":2001,"StarPos":[6,0,0],"JumpDist":6.0}`)

	createdExp, err := testApp.CreateExpedition("Export Scope DB", "")
	if err != nil {
		testingContext.Fatalf("CreateExpedition: %v", err)
	}
	plan := mustGetPlan(testingContext, testApp, createdExp.ID)
	originID := plan.Rows[0].Row.ID
	solName := "Sol"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: originID, SystemName: &solName}); err != nil {
		testingContext.Fatalf("UpdateRow Sol: %v", err)
	}
	destRow, err := testApp.AddRow(createdExp.ID, originID, expedition.RowWaypoint, 0)
	if err != nil {
		testingContext.Fatalf("AddRow dest: %v", err)
	}
	destName := "Barnard's Star"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: destRow.ID, SystemName: &destName}); err != nil {
		testingContext.Fatalf("UpdateRow dest: %v", err)
	}
	if err := testApp.StartExpedition(createdExp.ID, "2026-04-06T09:05:00Z", 0); err != nil {
		testingContext.Fatalf("StartExpedition: %v", err)
	}
	noteText := "Stale cached note"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: originID, Notes: &noteText}); err != nil {
		testingContext.Fatalf("UpdateRow note: %v", err)
	}

	exportBeforeUndo, err := testApp.GetExpeditionExportData(createdExp.ID)
	if err != nil {
		testingContext.Fatalf("GetExpeditionExportData: %v", err)
	}
	if _, matched := testApp.matchingExportSnapshot(createdExp.ID, exportBeforeUndo.ExportedAt); !matched {
		testingContext.Fatalf("expected export snapshot cached before undo")
	}

	undoRes, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoRes.Applied {
		testingContext.Fatalf("UndoExpedition before export check: res=%+v err=%v", undoRes, err)
	}
	if _, matched := testApp.matchingExportSnapshot(createdExp.ID, exportBeforeUndo.ExportedAt); matched {
		testingContext.Fatalf("expected UndoExpedition to invalidate cached export snapshot")
	}
	rebuiltExport := writeAndDecodeCanonicalExport(
		testingContext,
		testApp,
		filepath.Join(testingContext.TempDir(), "after-undo.json"),
		createdExp.ID,
		exportBeforeUndo.ExportedAt,
	)
	if rebuiltExport.Plan.Rows[0].Row.Notes != "" {
		testingContext.Fatalf("expected rebuilt export to contain undone empty notes, got %q", rebuiltExport.Plan.Rows[0].Row.Notes)
	}

	res, execErr := testApp.db.Exec(
		`INSERT INTO commander_profiles (profile_key, display_name, fid, universe, enabled) VALUES ('live:f202:era:0', 'CMDR AltSwitch', 'F202', 'live', 1)`,
	)
	if execErr != nil {
		testingContext.Fatalf("insert commander_profile: %v", execErr)
	}
	profileID, idError := res.LastInsertId()
	if idError != nil {
		testingContext.Fatalf("read inserted commander_profile id: %v", idError)
	}

	updatedNote := "Before scope change"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: originID, Notes: &updatedNote}); err != nil {
		testingContext.Fatalf("UpdateRow before scope change: %v", err)
	}
	if err := testApp.UpdateExpeditionScoped(createdExp.ID, "Export Scope DB", "Scoped", profileID); err != nil {
		testingContext.Fatalf("UpdateExpeditionScoped: %v", err)
	}
	afterScopeUndo, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || afterScopeUndo.Applied {
		testingContext.Fatalf("expected UpdateExpeditionScoped scope change to clear undo stack, got res=%+v err=%v", afterScopeUndo, err)
	}

	if _, err := testApp.AddRow(createdExp.ID, originID, expedition.RowWaypoint, 0); err != nil {
		testingContext.Fatalf("AddRow before DB switch: %v", err)
	}
	secondDBPath := filepath.Join(testingContext.TempDir(), "second.sqlite")
	if _, err := testApp.SetDatabasePath(secondDBPath); err != nil {
		testingContext.Fatalf("SetDatabasePath: %v", err)
	}
	afterSwitchUndo, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || afterSwitchUndo.Applied {
		testingContext.Fatalf("expected SetDatabasePath to clear all undo history, got res=%+v err=%v", afterSwitchUndo, err)
	}
}

func TestAppConcurrentCommanderOverrideScopeChangeAndUndo(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDir := testingContext.TempDir()

	mainLog := strings.Join([]string{
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Fileheader","part":1,"language":"English/UK","Odyssey":true,"gameversion":"4.0.0.1800","build":"r301234/r0 "}`,
		`{"timestamp":"2026-03-01T10:01:00Z","event":"LoadGame","Commander":"CMDR Primary","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-03-01T10:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
	}, "\n") + "\n"
	aliasLog := strings.Join([]string{
		`{"timestamp":"2025-01-10T08:59:00Z","event":"Commander","Name":"LegacyAlias"}`,
		`{"timestamp":"2025-01-10T09:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[3,0,3],"JumpDist":4.3}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2026-03-01T100000.01.log"), []byte(mainLog), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2025-01-10T085900.01.log"), []byte(aliasLog), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.SetupConfigureJournals(journalDir); err != nil {
		testingContext.Fatalf("SetupConfigureJournals: %v", err)
	}
	if _, err := testApp.CompleteSetup(); err != nil {
		testingContext.Fatalf("CompleteSetup: %v", err)
	}

	createdExp, err := testApp.CreateExpedition("Concurrent Race", "")
	if err != nil {
		testingContext.Fatalf("CreateExpedition: %v", err)
	}
	plan := mustGetPlan(testingContext, testApp, createdExp.ID)
	originID := plan.Rows[0].Row.ID

	var workers sync.WaitGroup
	workers.Add(3)

	// Worker A: repeatedly records row edits and executes undo/redo.
	go func() {
		defer workers.Done()
		for iteration := 0; iteration < 12; iteration++ {
			note := fmt.Sprintf("note-%d", iteration)
			if updateError := testApp.UpdateRow(expedition.RowPatch{ID: originID, Notes: &note}); updateError != nil {
				testingContext.Errorf("concurrent UpdateRow(%d): %v", iteration, updateError)
				return
			}
			if _, undoError := testApp.UndoExpedition(createdExp.ID); undoError != nil {
				testingContext.Errorf("concurrent UndoExpedition(%d): %v", iteration, undoError)
				return
			}
			if _, redoError := testApp.RedoExpedition(createdExp.ID); redoError != nil {
				testingContext.Errorf("concurrent RedoExpedition(%d): %v", iteration, redoError)
				return
			}
		}
	}()

	// Worker B: repeatedly switches commander save slot via UpdateExpeditionScoped.
	go func() {
		defer workers.Done()
		for iteration := 0; iteration < 8; iteration++ {
			settingsNow, settingsError := testApp.GetSettings()
			if settingsError != nil {
				testingContext.Errorf("concurrent GetSettings: %v", settingsError)
				return
			}
			if len(settingsNow.Commanders) > 0 {
				targetCommander := settingsNow.Commanders[iteration%len(settingsNow.Commanders)]
				if scopeError := testApp.UpdateExpeditionScoped(createdExp.ID, "Concurrent Race", "", targetCommander.ID); scopeError != nil && !strings.Contains(scopeError.Error(), "does not exist") {
					testingContext.Errorf("concurrent UpdateExpeditionScoped(%d): %v", targetCommander.ID, scopeError)
					return
				}
			}
		}
	}()

	// Worker C: repeatedly applies and resets commander overrides under runUndoExclusive.
	go func() {
		defer workers.Done()
		for iteration := 0; iteration < 6; iteration++ {
			if _, mergeError := testApp.MergeCommanders("live:name:legacyalias:era:0", "live:f101:era:0"); mergeError != nil {
				testingContext.Errorf("concurrent MergeCommanders(%d): %v", iteration, mergeError)
				return
			}
			if _, resetError := testApp.ResetCommanderOverrides("live:f101:era:0"); resetError != nil {
				testingContext.Errorf("concurrent ResetCommanderOverrides(%d): %v", iteration, resetError)
				return
			}
		}
	}()

	workers.Wait()

	// Final override with no subsequent edits leaves undo history completely empty.
	if _, err := testApp.MergeCommanders("live:name:legacyalias:era:0", "live:f101:era:0"); err != nil {
		testingContext.Fatalf("final MergeCommanders: %v", err)
	}
	finalUndo, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || finalUndo.Applied {
		testingContext.Fatalf("expected empty undo history after final MergeCommanders, got res=%+v err=%v", finalUndo, err)
	}
}
