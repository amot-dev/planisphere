package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"planisphere/internal/sources/spansh"
	"planisphere/internal/store/expedition"
)

func TestCarrierRoutePromptCancellationAndPlanEmissions(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	originalSpanshQuery := querySpanshCarrierRoute
	testingContext.Cleanup(func() { querySpanshCarrierRoute = originalSpanshQuery })

	insertTestJournalEvent(testingContext, testApp, "2026-02-01T09:00:00Z", "CarrierStats", "cs-1",
		`{"event":"CarrierStats","CarrierID":444001,"Callsign":"K1A-99Z","FuelLevel":720,"SpaceUsage":{"TotalCapacity":25000,"FreeSpace":18000,"Cargo":1200}}`)
	insertTestJournalEvent(testingContext, testApp, "2026-02-01T09:05:00Z", "Location", "loc-player",
		`{"timestamp":"2026-02-01T09:05:00Z","event":"Location","StarSystem":"Shinrarta Dezhra","SystemAddress":3932277478106,"StarPos":[55.7,17.6,27.1]}`)

	expOne := mustCreateExpeditionWithWaypoints(testingContext, testApp, "Carrier Prompt One", "", "Sol", "Rohini", "Colonia")
	if err := testApp.UpdateExpeditionCarrierConfig(expOne.ID, "squadron", 0); err != nil {
		testingContext.Fatalf("UpdateExpeditionCarrierConfig: %v", err)
	}
	planOne, err := testApp.GetPlan(expOne.ID)
	if err != nil {
		testingContext.Fatalf("GetPlan: %v", err)
	}
	fcStop := true
	stopRowID := planOne.Rows[2].Row.ID
	if err := testApp.UpdateRow(expedition.RowPatch{ID: stopRowID, FCStop: &fcStop}); err != nil {
		testingContext.Fatalf("UpdateRow FCStop: %v", err)
	}

	// PrepareCarrierRoutePrompt resolves preceding waypoint, squadron type, and journal depot/capacity defaults.
	promptDefaults, err := testApp.PrepareCarrierRoutePrompt(expOne.ID, stopRowID)
	if err != nil {
		testingContext.Fatalf("PrepareCarrierRoutePrompt target: %v", err)
	}
	if promptDefaults.TargetRowID != stopRowID || promptDefaults.Origin != "Rohini" || promptDefaults.Destination != "Colonia" ||
		promptDefaults.CarrierType != "squadron" || promptDefaults.CapacityUsed != 7000 || promptDefaults.DepotTritium != 720 {
		testingContext.Fatalf("unexpected PrepareCarrierRoutePrompt defaults: %+v", promptDefaults)
	}
	manualDefaults, err := testApp.PrepareCarrierRoutePrompt(expOne.ID, 0)
	if err != nil || manualDefaults.TargetRowID != stopRowID || manualDefaults.Origin != "Rohini" || manualDefaults.Destination != "Colonia" {
		testingContext.Fatalf("unexpected manual PrepareCarrierRoutePrompt defaults: %+v err=%v", manualDefaults, err)
	}

	// Verify CancelCarrierRoutePlot, SetActiveExpedition, and SetDatabasePath each abort an in-flight Spansh poll and return nil, nil without saving.
	expTwo, err := testApp.CreateExpedition("Carrier Prompt Two", "")
	if err != nil {
		testingContext.Fatalf("CreateExpedition two: %v", err)
	}
	cancellationTriggers := []struct {
		name   string
		cancel func()
	}{
		{name: "CancelCarrierRoutePlot", cancel: testApp.CancelCarrierRoutePlot},
		{name: "SetActiveExpedition", cancel: func() {
			if setErr := testApp.SetActiveExpedition(expTwo.ID); setErr != nil {
				testingContext.Errorf("SetActiveExpedition: %v", setErr)
			}
		}},
		{name: "SetDatabasePath", cancel: func() {
			if _, switchErr := testApp.SetDatabasePath(filepath.Join(testingContext.TempDir(), "switched.db")); switchErr != nil {
				testingContext.Errorf("SetDatabasePath: %v", switchErr)
				return
			}
			if _, disableErr := testApp.SetOnlineLookups(false); disableErr != nil {
				testingContext.Errorf("SetOnlineLookups(false): %v", disableErr)
			}
		}},
	}
	for _, trigger := range cancellationTriggers {
		enteredPoll := make(chan struct{})
		querySpanshCarrierRoute = func(ctx context.Context, _, _ string, _, _ int) ([]spansh.RouteJump, error) {
			close(enteredPoll)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		targetExpID := expOne.ID
		currentList, listBeforeErr := testApp.ListExpeditions()
		if listBeforeErr != nil {
			testingContext.Fatalf("ListExpeditions before %s: %v", trigger.name, listBeforeErr)
		}
		if len(currentList) > 0 {
			targetExpID = currentList[0].ID
		} else {
			seeded, seedErr := testApp.CreateExpedition("Switched Exp", "")
			if seedErr != nil {
				testingContext.Fatalf("seed expedition: %v", seedErr)
			}
			targetExpID = seeded.ID
		}
		type plotOutcome struct {
			state *ActiveCarrierRouteState
			err   error
		}
		outcomeCh := make(chan plotOutcome, 1)
		go func() {
			state, startErr := testApp.StartCarrierRoute(targetExpID, 0, "Sol", "Colonia", 5000, "player", 1000)
			outcomeCh <- plotOutcome{state: state, err: startErr}
		}()
		<-enteredPoll
		trigger.cancel()
		outcome := <-outcomeCh
		if outcome.err != nil || outcome.state != nil {
			testingContext.Fatalf("%s: expected cancelled StartCarrierRoute to return (nil, nil), got (%+v, %v)", trigger.name, outcome.state, outcome.err)
		}
		afterList, listAfterErr := testApp.ListExpeditions()
		if listAfterErr != nil {
			testingContext.Fatalf("ListExpeditions after %s: %v", trigger.name, listAfterErr)
		}
		if len(afterList) > 0 {
			if activeRoute := mustGetActiveCarrierRoute(testingContext, testApp, afterList[0].ID); activeRoute != nil {
				testingContext.Fatalf("%s: expected cancelled plot not to persist route, got %+v", trigger.name, activeRoute)
			}
		}
	}

	// Non-cancellation Spansh errors fall back to ManualMode: true, ClearActiveCarrierRouteWithUndo emits plan domain,
	// and CompleteCarrierRouteArrival with targetRowID=0 resolves the next unarrived FC stop inside the tx.
	activeExp := mustCreateExpeditionWithWaypoints(testingContext, testApp, "Arrival Zero Target", "", "Sol", "Beagle Point")
	activePlan := mustGetPlan(testingContext, testApp, activeExp.ID)
	beagleStopID := activePlan.Rows[1].Row.ID
	if err := testApp.UpdateRow(expedition.RowPatch{ID: beagleStopID, FCStop: &fcStop}); err != nil {
		testingContext.Fatalf("UpdateRow beagleStopID: %v", err)
	}

	querySpanshCarrierRoute = func(_ context.Context, _, _ string, _, _ int) ([]spansh.RouteJump, error) {
		return nil, errors.New("Spansh upstream error")
	}
	fallbackState, err := testApp.StartCarrierRoute(activeExp.ID, beagleStopID, "Sol", "Beagle Point", 5000, "player", 1000)
	if err != nil || fallbackState == nil || !fallbackState.ManualMode || fallbackState.Route.Error == "" {
		testingContext.Fatalf("expected non-cancellation Spansh error to persist manual fallback, got state=%+v err=%v", fallbackState, err)
	}

	testApp.backgroundWg.Wait()
	eventsRecorder := captureAppEvents(testingContext, testApp)
	if err := testApp.ClearActiveCarrierRouteWithUndo(activeExp.ID); err != nil {
		testingContext.Fatalf("ClearActiveCarrierRouteWithUndo: %v", err)
	}
	if len(eventsRecorder.DataChanges()) != 1 || eventsRecorder.CountDomain("plan") != 1 {
		testingContext.Fatalf("expected ClearActiveCarrierRouteWithUndo to emit plan domain, got %+v", eventsRecorder.DataChanges())
	}

	if _, err := testApp.OpenManualCarrierRoute(activeExp.ID); err != nil {
		testingContext.Fatalf("OpenManualCarrierRoute: %v", err)
	}
	if err := testApp.CompleteCarrierRouteArrival(activeExp.ID, 0, 0, 0); err != nil {
		testingContext.Fatalf("CompleteCarrierRouteArrival(0): %v", err)
	}
	resolvedArrivalPlan := mustGetPlan(testingContext, testApp, activeExp.ID)
	if !resolvedArrivalPlan.Rows[1].Row.FCArrived || resolvedArrivalPlan.ActiveCarrierRoute != nil {
		testingContext.Fatalf("expected targetRowID=0 arrival to mark Beagle Point arrived and clear route, got row=%+v route=%+v",
			resolvedArrivalPlan.Rows[1].Row, resolvedArrivalPlan.ActiveCarrierRoute)
	}
}

func TestAppUndoRedoCarrierRouteJumpsAndArrival(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	originalSpanshQuery := querySpanshCarrierRoute
	testingContext.Cleanup(func() { querySpanshCarrierRoute = originalSpanshQuery })
	querySpanshCarrierRoute = func(_ context.Context, _, _ string, _, depotTritium int) ([]spansh.RouteJump, error) {
		return []spansh.RouteJump{
			{SystemName: "Midway", DistanceLy: 480, TritiumUsed: 90, FuelInTank: depotTritium},
			{SystemName: "Colonia", DistanceLy: 420, TritiumUsed: 80, FuelInTank: depotTritium},
		}, nil
	}

	createdExp, err := testApp.CreateExpedition("Carrier Undo", "")
	if err != nil {
		testingContext.Fatalf("CreateExpedition: %v", err)
	}

	// Clearing a non-existent active carrier route or completing an empty no-op arrival pushes nothing.
	if err := testApp.ClearActiveCarrierRouteWithUndo(createdExp.ID); err != nil {
		testingContext.Fatalf("ClearActiveCarrierRouteWithUndo absent: %v", err)
	}
	if err := testApp.CompleteCarrierRouteArrival(createdExp.ID, 0, 0, 0); err != nil {
		testingContext.Fatalf("CompleteCarrierRouteArrival no-op: %v", err)
	}

	plan := mustGetPlan(testingContext, testApp, createdExp.ID)
	stopRow, err := testApp.AddRow(createdExp.ID, plan.Rows[0].Row.ID, expedition.RowWaypoint, 0)
	if err != nil {
		testingContext.Fatalf("AddRow stop: %v", err)
	}
	fcStop := true
	stopSystem := "Colonia"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: stopRow.ID, SystemName: &stopSystem, FCStop: &fcStop}); err != nil {
		testingContext.Fatalf("UpdateRow stop: %v", err)
	}

	plottedState, err := testApp.StartCarrierRoute(createdExp.ID, stopRow.ID, "Sol", "Colonia", 5000, "player", 500)
	if err != nil || plottedState == nil || plottedState.Route.TotalJumps != 2 {
		testingContext.Fatalf("StartCarrierRoute: state=%+v err=%v", plottedState, err)
	}
	if _, err := testApp.StartCarrierRoute(createdExp.ID, stopRow.ID, "Sol", "Colonia", 5000, "player", 500); err != nil {
		testingContext.Fatalf("StartCarrierRoute no-op: %v", err)
	}

	refilledState, err := testApp.RefillCarrierDepot(createdExp.ID)
	if err != nil || refilledState == nil || refilledState.Route.DepotTritium != 1000 {
		testingContext.Fatalf("RefillCarrierDepot: state=%+v err=%v", refilledState, err)
	}
	if undoRefill, undoErr := testApp.UndoExpedition(createdExp.ID); undoErr != nil || !undoRefill.Applied || undoRefill.Label != "Refill Carrier Depot" {
		testingContext.Fatalf("Undo RefillCarrierDepot: res=%+v err=%v", undoRefill, undoErr)
	}

	hopState, err := testApp.CompleteSpanshCarrierJump(createdExp.ID)
	if err != nil || hopState == nil || hopState.JumpIndex != 1 {
		testingContext.Fatalf("CompleteSpanshCarrierJump hop 1: state=%+v err=%v", hopState, err)
	}
	hop1Tritium := hopState.Route.Jumps[1].TritiumUsed
	finalState, err := testApp.CompleteSpanshCarrierJump(createdExp.ID)
	if err != nil || finalState != nil {
		testingContext.Fatalf("CompleteSpanshCarrierJump final arrival: state=%+v err=%v", finalState, err)
	}

	arrivedPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if arrivedPlan.Expedition.Adjustments.CarrierJumps != 2 || arrivedPlan.Expedition.Adjustments.CarrierDistanceLy != 900 ||
		arrivedPlan.Expedition.Adjustments.TritiumUsed == nil || *arrivedPlan.Expedition.Adjustments.TritiumUsed != 90+hop1Tritium ||
		!arrivedPlan.Rows[1].Row.FCArrived || arrivedPlan.ActiveCarrierRoute != nil {
		testingContext.Fatalf("unexpected arrived state: adj=%+v arrived=%v route=%+v",
			arrivedPlan.Expedition.Adjustments, arrivedPlan.Rows[1].Row.FCArrived, arrivedPlan.ActiveCarrierRoute)
	}

	undoArrival, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoArrival.Applied || undoArrival.Label != "Complete Carrier Arrival" {
		testingContext.Fatalf("Undo arrival: res=%+v err=%v", undoArrival, err)
	}
	afterUndoArrivalPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if afterUndoArrivalPlan.Expedition.Adjustments.CarrierJumps != 1 || afterUndoArrivalPlan.Rows[1].Row.FCArrived ||
		afterUndoArrivalPlan.ActiveCarrierRoute == nil || afterUndoArrivalPlan.ActiveCarrierRoute.JumpIndex != 1 {
		testingContext.Fatalf("unexpected state after undoing arrival: jumps=%d arrived=%v route=%+v",
			afterUndoArrivalPlan.Expedition.Adjustments.CarrierJumps, afterUndoArrivalPlan.Rows[1].Row.FCArrived, afterUndoArrivalPlan.ActiveCarrierRoute)
	}

	undoHop, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoHop.Applied || undoHop.Label != "Log Carrier Jump" {
		testingContext.Fatalf("Undo hop: res=%+v err=%v", undoHop, err)
	}
	afterUndoHopPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if afterUndoHopPlan.Expedition.Adjustments.CarrierJumps != 0 || afterUndoHopPlan.Expedition.Adjustments.TritiumUsed != nil ||
		afterUndoHopPlan.ActiveCarrierRoute == nil || afterUndoHopPlan.ActiveCarrierRoute.JumpIndex != 0 {
		testingContext.Fatalf("unexpected state after undoing hop: adj=%+v route=%+v",
			afterUndoHopPlan.Expedition.Adjustments, afterUndoHopPlan.ActiveCarrierRoute)
	}

	undoPlot, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoPlot.Applied || undoPlot.Label != "Start Carrier Route" {
		testingContext.Fatalf("Undo plot: res=%+v err=%v", undoPlot, err)
	}
	if activeRoute := mustGetActiveCarrierRoute(testingContext, testApp, createdExp.ID); activeRoute != nil {
		testingContext.Fatalf("expected active route cleared after undoing plot, got %+v", activeRoute)
	}

	// Next undo must revert the row edit (not the no-op CompleteCarrierRouteArrival).
	undoRowEdit, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoRowEdit.Applied || undoRowEdit.Label == "Complete Carrier Arrival" {
		testingContext.Fatalf("expected no-op CompleteCarrierRouteArrival skipped on undo stack, got res=%+v err=%v", undoRowEdit, err)
	}
	if _, err := testApp.RedoExpedition(createdExp.ID); err != nil {
		testingContext.Fatalf("Redo row edit: %v", err)
	}

	for step := 0; step < 3; step++ {
		redoRes, redoErr := testApp.RedoExpedition(createdExp.ID)
		if redoErr != nil || !redoRes.Applied {
			testingContext.Fatalf("Redo carrier step %d: res=%+v err=%v", step, redoRes, redoErr)
		}
	}
	reArrivedPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if reArrivedPlan.Expedition.Adjustments.CarrierJumps != 2 || !reArrivedPlan.Rows[1].Row.FCArrived {
		testingContext.Fatalf("expected re-arrived state after 3 redos, got jumps=%d arrived=%v",
			reArrivedPlan.Expedition.Adjustments.CarrierJumps, reArrivedPlan.Rows[1].Row.FCArrived)
	}

	// Verify OpenManualCarrierRoute, LogManualCarrierJump, and ClearActiveCarrierRouteWithUndo undo/redo cycle.
	manualRoute, err := testApp.OpenManualCarrierRoute(createdExp.ID)
	if err != nil || manualRoute == nil || !manualRoute.ManualMode {
		testingContext.Fatalf("OpenManualCarrierRoute: state=%+v err=%v", manualRoute, err)
	}
	if _, err := testApp.LogManualCarrierJump(createdExp.ID, 350.5, 55); err != nil {
		testingContext.Fatalf("LogManualCarrierJump: %v", err)
	}
	if err := testApp.ClearActiveCarrierRouteWithUndo(createdExp.ID); err != nil {
		testingContext.Fatalf("ClearActiveCarrierRouteWithUndo present: %v", err)
	}
	undoClear, err := testApp.UndoExpedition(createdExp.ID)
	if err != nil || !undoClear.Applied || undoClear.Label != "Clear Carrier Route" {
		testingContext.Fatalf("Undo clear route: res=%+v err=%v", undoClear, err)
	}
	if restoredRoute := mustGetActiveCarrierRoute(testingContext, testApp, createdExp.ID); restoredRoute == nil || restoredRoute.ManualLegJumps != 1 {
		testingContext.Fatalf("expected manual route restored with 1 jump after undoing clear, got %+v", restoredRoute)
	}
}

func TestSwitchCarrierRouteToManualAndSaveDraftInputs(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	originalSpanshQuery := querySpanshCarrierRoute
	testingContext.Cleanup(func() { querySpanshCarrierRoute = originalSpanshQuery })
	querySpanshCarrierRoute = func(_ context.Context, _, _ string, _, depotTritium int) ([]spansh.RouteJump, error) {
		return []spansh.RouteJump{
			{SystemName: "Midway", DistanceLy: 480.5, TritiumUsed: 90, FuelInTank: depotTritium},
			{SystemName: "Colonia", DistanceLy: 420.0, TritiumUsed: 80, FuelInTank: depotTritium},
		}, nil
	}

	createdExp := mustCreateExpeditionWithWaypoints(testingContext, testApp, "Manual Switch Test", "", "Sol", "Colonia")
	plan := mustGetPlan(testingContext, testApp, createdExp.ID)
	fcStop := true
	stopRowID := plan.Rows[1].Row.ID
	if err := testApp.UpdateRow(expedition.RowPatch{ID: stopRowID, FCStop: &fcStop}); err != nil {
		testingContext.Fatalf("UpdateRow FCStop: %v", err)
	}
	if _, startErr := testApp.StartCarrierRoute(createdExp.ID, stopRowID, "Sol", "Colonia", 5000, "player", 800); startErr != nil {
		testingContext.Fatalf("StartCarrierRoute: %v", startErr)
	}

	testApp.backgroundWg.Wait()
	eventsRecorder := captureAppEvents(testingContext, testApp)

	manualState, switchErr := testApp.SwitchCarrierRouteToManual(createdExp.ID)
	if switchErr != nil || manualState == nil || !manualState.ManualMode ||
		manualState.ManualDistanceInput != "480.5" || manualState.ManualTritiumInput != "90" {
		testingContext.Fatalf("SwitchCarrierRouteToManual = (%+v, %v), want ManualMode with seeded hop inputs", manualState, switchErr)
	}
	switchChanges := eventsRecorder.TakeDataChanges()
	if len(switchChanges) != 1 || switchChanges[0].ExpeditionID != createdExp.ID || !slices.Contains(switchChanges[0].Domains, "plan") {
		testingContext.Fatalf("expected SwitchCarrierRouteToManual to emit plan DataChanged for %d, got %+v", createdExp.ID, switchChanges)
	}

	if draftErr := testApp.SaveCarrierManualDraftInputs(createdExp.ID, "412.25", "76"); draftErr != nil {
		testingContext.Fatalf("SaveCarrierManualDraftInputs: %v", draftErr)
	}
	if draftChanges := eventsRecorder.TakeDataChanges(); len(draftChanges) != 0 {
		testingContext.Fatalf("expected SaveCarrierManualDraftInputs not to emit DataChanged spam, got %+v", draftChanges)
	}
	persistedDraft := mustGetActiveCarrierRoute(testingContext, testApp, createdExp.ID)
	if persistedDraft == nil || persistedDraft.ManualDistanceInput != "412.25" || persistedDraft.ManualTritiumInput != "76" {
		testingContext.Fatalf("expected persisted draft inputs (412.25, 76), got %+v", persistedDraft)
	}

	// Undo must immediately revert SwitchCarrierRouteToManual rather than draft keystrokes.
	undoSwitch, undoErr := testApp.UndoExpedition(createdExp.ID)
	if undoErr != nil || !undoSwitch.Applied || undoSwitch.Label != "Switch Carrier Route to Manual Mode" {
		testingContext.Fatalf("Undo SwitchCarrierRouteToManual: res=%+v err=%v", undoSwitch, undoErr)
	}
	revertedRoute := mustGetActiveCarrierRoute(testingContext, testApp, createdExp.ID)
	if revertedRoute == nil || revertedRoute.ManualMode {
		testingContext.Fatalf("expected route reverted to Spansh mode after undo, got %+v", revertedRoute)
	}
}
