package app

import (
	"slices"
	"testing"

	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/journaltest"
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

func TestExpeditionFacadeMetadataRegionOptionsCandidatesAndLifecycle(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	journaltest.InsertParsedLines(
		testingContext,
		testApp.db,
		`{"timestamp":"2026-01-01T00:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-02T00:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":3230,"StarPos":[-9530.5,-910.28,19808.13],"JumpDist":22000.4}`,
	)
	testApp.publishJournalBatch(journal.NewEventSet(journal.EventLocation, journal.EventFSDJump), "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z")

	createdExp, createErr := testApp.createExpeditionWithWaypoints("Draft Plan", "Old notes", []string{"Sol", "Colonia", "Beagle Point"})
	if createErr != nil {
		testingContext.Fatalf("createExpeditionWithWaypoints: %v", createErr)
	}
	testApp.backgroundWg.Wait()

	eventsRecorder := captureAppEvents(testingContext, testApp)

	if err := testApp.UpdateExpedition(createdExp.ID, "Renamed Plan", "Updated notes"); err != nil {
		testingContext.Fatalf("UpdateExpedition: %v", err)
	}
	renameChanges := eventsRecorder.TakeDataChanges()
	if len(renameChanges) != 1 || renameChanges[0].ExpeditionID != createdExp.ID ||
		!slices.Contains(renameChanges[0].Domains, "plan") || !slices.Contains(renameChanges[0].Domains, "expeditions") {
		testingContext.Fatalf("expected UpdateExpedition to emit plan and expeditions domains, got %+v", renameChanges)
	}
	if plan := mustGetPlan(testingContext, testApp, createdExp.ID); plan.Expedition.Name != "Renamed Plan" || plan.Expedition.Description != "Updated notes" {
		testingContext.Fatalf("unexpected expedition meta after UpdateExpedition: %+v", plan.Expedition)
	}

	candidates, candErr := testApp.GetStartCandidates(createdExp.ID)
	if candErr != nil || candidates.SystemName != "Sol" || len(candidates.Visits) != 1 {
		testingContext.Fatalf("GetStartCandidates = %+v err=%v", candidates, candErr)
	}

	// Add a region header at the top and verify GetRegionOptions across top insert, header edit, and post-header insert.
	regionRow, addRegionErr := testApp.AddRow(createdExp.ID, expedition.InsertAtTop, expedition.RowRegion, 15)
	if addRegionErr != nil {
		testingContext.Fatalf("AddRow region: %v", addRegionErr)
	}
	topRegionOpts, topOptsErr := testApp.GetRegionOptions(createdExp.ID, expedition.InsertAtTop, false)
	if topOptsErr != nil || !topRegionOpts.AnchorIsDefault || topRegionOpts.AnchorID != reference.InnerOrionSpurID || len(topRegionOpts.Options) != 42 {
		testingContext.Fatalf("unexpected top GetRegionOptions: %+v err=%v", topRegionOpts, topOptsErr)
	}
	editRegionOpts, editOptsErr := testApp.GetRegionOptions(createdExp.ID, regionRow.ID, true)
	if editOptsErr != nil || !editRegionOpts.AnchorIsDefault || editRegionOpts.SuggestedRegionID != reference.InnerOrionSpurID {
		testingContext.Fatalf("unexpected editing GetRegionOptions: %+v err=%v", editRegionOpts, editOptsErr)
	}
	afterRegionOpts, afterOptsErr := testApp.GetRegionOptions(createdExp.ID, regionRow.ID, false)
	if afterOptsErr != nil || afterRegionOpts.AnchorIsDefault || afterRegionOpts.AnchorID != 15 {
		testingContext.Fatalf("unexpected post-header GetRegionOptions: %+v err=%v", afterRegionOpts, afterOptsErr)
	}

	// Mark Colonia as FCStop and verify OverrideCarrierStopArrival emits plan/expeditions and integrates with undo.
	planBeforeStart := mustGetPlan(testingContext, testApp, createdExp.ID)
	coloniaRowID := planBeforeStart.Rows[2].Row.ID
	beagleRowID := planBeforeStart.Rows[3].Row.ID
	fcStop := true
	if err := testApp.UpdateRow(expedition.RowPatch{ID: coloniaRowID, FCStop: &fcStop}); err != nil {
		testingContext.Fatalf("UpdateRow FCStop: %v", err)
	}
	eventsRecorder.Reset()

	if err := testApp.OverrideCarrierStopArrival(coloniaRowID, true); err != nil {
		testingContext.Fatalf("OverrideCarrierStopArrival: %v", err)
	}
	overrideChanges := eventsRecorder.TakeDataChanges()
	if len(overrideChanges) != 1 || !slices.Contains(overrideChanges[0].Domains, "plan") || !slices.Contains(overrideChanges[0].Domains, "expeditions") {
		testingContext.Fatalf("expected OverrideCarrierStopArrival to emit plan and expeditions, got %+v", overrideChanges)
	}
	if undoOverride, undoErr := testApp.UndoExpedition(createdExp.ID); undoErr != nil || !undoOverride.Applied || undoOverride.Label != "Override Carrier Arrival" {
		testingContext.Fatalf("Undo OverrideCarrierStopArrival: res=%+v err=%v", undoOverride, undoErr)
	}
	if mustGetPlan(testingContext, testApp, createdExp.ID).Rows[2].Row.FCArrived {
		testingContext.Fatal("expected FCArrived reverted to false after undo")
	}

	// Engage expedition so Sol and Colonia evaluate as visited, then verify GetRowDropPreviews, GetWaypointDetails, CompleteExpedition, and ReopenExpedition.
	if err := testApp.StartExpedition(createdExp.ID, "2026-01-01T00:00:00Z", 0); err != nil {
		testingContext.Fatalf("StartExpedition: %v", err)
	}
	previews, previewErr := testApp.GetRowDropPreviews(beagleRowID)
	if previewErr != nil || len(previews) == 0 {
		testingContext.Fatalf("GetRowDropPreviews = (len=%d, %v), want non-empty previews", len(previews), previewErr)
	}
	details, detailsErr := testApp.GetWaypointDetails(coloniaRowID)
	if detailsErr != nil || details.RowID != coloniaRowID || details.Kind != "leg" || details.Metrics.Jumps != 1 {
		testingContext.Fatalf("GetWaypointDetails = %+v err=%v", details, detailsErr)
	}

	if err := testApp.CompleteExpedition(createdExp.ID); err != nil {
		testingContext.Fatalf("CompleteExpedition: %v", err)
	}
	eventsRecorder.Reset()

	if err := testApp.ReopenExpedition(createdExp.ID); err != nil {
		testingContext.Fatalf("ReopenExpedition: %v", err)
	}
	reopenChanges := eventsRecorder.TakeDataChanges()
	if len(reopenChanges) != 1 || !slices.Contains(reopenChanges[0].Domains, "plan") ||
		!slices.Contains(reopenChanges[0].Domains, "stats") || !slices.Contains(reopenChanges[0].Domains, "expeditions") {
		testingContext.Fatalf("expected ReopenExpedition to emit plan, stats, and expeditions, got %+v", reopenChanges)
	}
	if reopenedPlan := mustGetPlan(testingContext, testApp, createdExp.ID); reopenedPlan.Expedition.Status != expedition.StatusActive {
		testingContext.Fatalf("expected active status after ReopenExpedition, got %q", reopenedPlan.Expedition.Status)
	}
	if undoReopen, undoErr := testApp.UndoExpedition(createdExp.ID); undoErr != nil || !undoReopen.Applied || undoReopen.Label != "Reopen Expedition" {
		testingContext.Fatalf("Undo ReopenExpedition: res=%+v err=%v", undoReopen, undoErr)
	}
}
