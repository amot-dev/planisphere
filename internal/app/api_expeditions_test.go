package app

import (
	"slices"
	"testing"
)

func TestDeleteActiveExpeditionSelectsSurvivingAndEmitsTargetedDomains(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	initialExpeditions, listError := testApp.ListExpeditions()
	if listError != nil || len(initialExpeditions) != 1 {
		testingContext.Fatalf("expected 1 seeded expedition, got len=%d err=%v", len(initialExpeditions), listError)
	}
	survivingExpedition := initialExpeditions[0]

	activeExpedition, createSecondError := testApp.CreateExpedition("Active Survey", "")
	if createSecondError != nil {
		testingContext.Fatalf("create active expedition: %v", createSecondError)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)

	assertDeleteOutcome := func(expectedActiveID int64) {
		testingContext.Helper()
		loadedSettings, settingsError := testApp.GetSettings()
		if settingsError != nil {
			testingContext.Fatalf("GetSettings: %v", settingsError)
		}
		if loadedSettings.ActiveExpeditionID != expectedActiveID {
			testingContext.Fatalf("expected active_expedition_id %d, got %d", expectedActiveID, loadedSettings.ActiveExpeditionID)
		}

		capturedEvents := eventsRecorder.TakeDataChanges()
		if len(capturedEvents) != 1 {
			testingContext.Fatalf("expected 1 DataChanged event, got %d (%+v)", len(capturedEvents), capturedEvents)
		}
		changeEvent := capturedEvents[0]
		if changeEvent.ExpeditionID != 0 {
			testingContext.Fatalf("expected expeditionId 0, got %+v", changeEvent)
		}
		if !slices.Equal(changeEvent.Domains, []string{"settings", "expeditions"}) {
			testingContext.Fatalf("expected domains [settings expeditions], got %v", changeEvent.Domains)
		}
	}

	if deleteError := testApp.DeleteExpedition(activeExpedition.ID); deleteError != nil {
		testingContext.Fatalf("DeleteExpedition(active): %v", deleteError)
	}
	assertDeleteOutcome(survivingExpedition.ID)

	if deleteLastError := testApp.DeleteExpedition(survivingExpedition.ID); deleteLastError != nil {
		testingContext.Fatalf("DeleteExpedition(last): %v", deleteLastError)
	}
	assertDeleteOutcome(0)
}
