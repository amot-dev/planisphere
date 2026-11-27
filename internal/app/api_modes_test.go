package app

import (
	"slices"
	"strings"
	"testing"
)

func TestAppModesCRUDUsageDomainsAndUndo(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	modesState, stateError := testApp.GetModesState()
	if stateError != nil || len(modesState.Modes) < 3 || modesState.DefaultModeID <= 0 {
		testingContext.Fatalf("GetModesState() = %+v, err=%v", modesState, stateError)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)

	createdMode, createError := testApp.CreateMode("Deep Survey", "High value targets", "#12AB34")
	if createError != nil || createdMode.ID <= 0 || createdMode.Name != "Deep Survey" {
		testingContext.Fatalf("CreateMode() = %+v, err=%v", createdMode, createError)
	}
	createChanges := eventsRecorder.TakeDataChanges()
	if len(createChanges) != 1 || !slices.Contains(createChanges[0].Domains, "modes") || !slices.Contains(createChanges[0].Domains, "plan") {
		testingContext.Fatalf("expected CreateMode to emit modes and plan domains, got %+v", createChanges)
	}

	if updateError := testApp.UpdateMode(createdMode.ID, "Deep Survey V2", "Updated targets", "#34CD56"); updateError != nil {
		testingContext.Fatalf("UpdateMode: %v", updateError)
	}
	updateChanges := eventsRecorder.TakeDataChanges()
	if len(updateChanges) != 1 || !slices.Contains(updateChanges[0].Domains, "modes") || !slices.Contains(updateChanges[0].Domains, "plan") || slices.Contains(updateChanges[0].Domains, "stats") {
		testingContext.Fatalf("expected UpdateMode to emit modes and plan without stats, got %+v", updateChanges)
	}

	initialUsage, emptyUsageError := testApp.GetModeUsage(createdMode.ID)
	if emptyUsageError != nil || initialUsage == nil || len(initialUsage) != 0 {
		testingContext.Fatalf("expected empty non-nil GetModeUsage slice, got %+v err=%v", initialUsage, emptyUsageError)
	}

	expeditionList, listError := testApp.ListExpeditions()
	if listError != nil || len(expeditionList) == 0 {
		testingContext.Fatalf("ListExpeditions: %+v err=%v", expeditionList, listError)
	}
	targetExpeditionID := expeditionList[0].ID

	if setModeError := testApp.SetExpeditionFlightMode(targetExpeditionID, createdMode.ID); setModeError != nil {
		testingContext.Fatalf("SetExpeditionFlightMode: %v", setModeError)
	}
	flightModeChanges := eventsRecorder.TakeDataChanges()
	if len(flightModeChanges) != 1 || !slices.Contains(flightModeChanges[0].Domains, "stats") || !slices.Contains(flightModeChanges[0].Domains, "plan") {
		testingContext.Fatalf("expected SetExpeditionFlightMode to emit stats and plan, got %+v", flightModeChanges)
	}

	undoFlightMode, undoError := testApp.UndoExpedition(targetExpeditionID)
	if undoError != nil || !undoFlightMode.Applied || undoFlightMode.Label != "Change Flight Mode" {
		testingContext.Fatalf("Undo SetExpeditionFlightMode: res=%+v err=%v", undoFlightMode, undoError)
	}
	if redoFlightMode, redoError := testApp.RedoExpedition(targetExpeditionID); redoError != nil || !redoFlightMode.Applied {
		testingContext.Fatalf("Redo SetExpeditionFlightMode: res=%+v err=%v", redoFlightMode, redoError)
	}
	eventsRecorder.Reset()

	activeUsage, usageError := testApp.GetModeUsage(createdMode.ID)
	if usageError != nil || len(activeUsage) != 1 || activeUsage[0].ExpeditionID != targetExpeditionID || !activeUsage[0].IsActiveMode {
		testingContext.Fatalf("unexpected GetModeUsage for active mode: %+v err=%v", activeUsage, usageError)
	}

	defaultDeleteErr := testApp.DeleteMode(modesState.DefaultModeID)
	if defaultDeleteErr == nil || !strings.Contains(defaultDeleteErr.Error(), "default modes cannot be deleted") {
		testingContext.Fatalf("expected default-mode deletion rejection, got %v", defaultDeleteErr)
	}
	if rejectedChanges := eventsRecorder.TakeDataChanges(); len(rejectedChanges) != 0 {
		testingContext.Fatalf("expected failed DeleteMode not to emit DataChanged, got %+v", rejectedChanges)
	}

	if deleteError := testApp.DeleteMode(createdMode.ID); deleteError != nil {
		testingContext.Fatalf("DeleteMode(custom): %v", deleteError)
	}
	deleteChanges := eventsRecorder.TakeDataChanges()
	if len(deleteChanges) != 1 || !slices.Contains(deleteChanges[0].Domains, "modes") ||
		!slices.Contains(deleteChanges[0].Domains, "plan") || !slices.Contains(deleteChanges[0].Domains, "stats") {
		testingContext.Fatalf("expected DeleteMode to emit modes, plan, and stats domains, got %+v", deleteChanges)
	}

	postDeleteUsage, postUsageErr := testApp.GetModeUsage(createdMode.ID)
	if postUsageErr != nil || len(postDeleteUsage) != 0 {
		testingContext.Fatalf("expected 0 usages after DeleteMode, got %+v err=%v", postDeleteUsage, postUsageErr)
	}
	if plan := mustGetPlan(testingContext, testApp, targetExpeditionID); plan.Expedition.ActiveModeID != modesState.DefaultModeID {
		testingContext.Fatalf("expected expedition active mode reassigned to %d, got %d", modesState.DefaultModeID, plan.Expedition.ActiveModeID)
	}
}
