package expedition_test

import (
	"strings"
	"testing"

	"planisphere/internal/store/expedition"
)

func TestNormalizeRecalculateRefillAndSwitchCarrierRouteState(testingContext *testing.T) {
	if expedition.NormalizeActiveCarrierRoute(nil) != nil {
		testingContext.Fatalf("expected nil for nil state")
	}
	if expedition.NormalizeActiveCarrierRoute(&expedition.ActiveCarrierRouteState{}) != nil {
		testingContext.Fatalf("expected nil for zero-value carrier route state")
	}

	drainedState := &expedition.ActiveCarrierRouteState{
		ExpeditionID: 1,
		TargetRowID:  10,
		JumpIndex:    9,
		Route: expedition.CarrierRouteResult{
			DepotTritium: 0,
			Jumps:        []expedition.CarrierRouteJump{{SystemName: "Rohini", DistanceLy: 400, TritiumUsed: 60}},
		},
	}
	normalizedDrained := expedition.NormalizeActiveCarrierRoute(drainedState)
	if normalizedDrained == nil || normalizedDrained.Route.DepotTritium != 0 || normalizedDrained.JumpIndex != 1 {
		testingContext.Fatalf("expected drained 0t depot preserved and JumpIndex clamped to 1, got %+v", normalizedDrained)
	}

	negativeAndExcessStates := []struct {
		rawDepot  int
		wantDepot int
	}{
		{rawDepot: -250, wantDepot: 0},
		{rawDepot: 1800, wantDepot: 1000},
	}
	for _, testCase := range negativeAndExcessStates {
		candidate := *drainedState
		candidate.JumpIndex = -3
		candidate.Route.DepotTritium = testCase.rawDepot
		normalized := expedition.NormalizeActiveCarrierRoute(&candidate)
		if normalized == nil || normalized.Route.DepotTritium != testCase.wantDepot || normalized.JumpIndex != 0 {
			testingContext.Fatalf("rawDepot=%d: got %+v", testCase.rawDepot, normalized)
		}
	}

	// Squadron carrier (up to 60,000t) recalculates only hops >= fromJumpIndex and clamps excess capacity.
	excessCapacity := 75000
	squadronRoute := expedition.CarrierRouteResult{
		CarrierType:  "squadron",
		CapacityUsed: 10000,
		DepotTritium: 500,
		Jumps: []expedition.CarrierRouteJump{
			{SystemName: "Hop 1", DistanceLy: 500, TritiumUsed: 95, FuelInTank: 500},
			{SystemName: "Hop 2", DistanceLy: 500, TritiumUsed: 80, FuelInTank: 500},
			{SystemName: "Hop 3", DistanceLy: 494.89, TritiumUsed: 79, FuelInTank: 500},
		},
	}
	recalculatedSquadron := expedition.RecalculateRouteDepotFuel(squadronRoute, 1, 1000, &excessCapacity)
	if recalculatedSquadron.CapacityUsed != 60000 || recalculatedSquadron.DepotTritium != 1000 {
		testingContext.Fatalf("expected squadron capacity 60000 and depot 1000, got %+v", recalculatedSquadron)
	}
	if recalculatedSquadron.Jumps[0].TritiumUsed != 95 || recalculatedSquadron.Jumps[1].TritiumUsed != 220 || recalculatedSquadron.Jumps[2].TritiumUsed != 218 {
		testingContext.Fatalf("unexpected squadron hop fuel recalculation: %+v", recalculatedSquadron.Jumps)
	}
	if recalculatedSquadron.TotalTritium != 95+220+218 {
		testingContext.Fatalf("expected TotalTritium=533, got %d", recalculatedSquadron.TotalTritium)
	}

	// RefillCarrierDepotState handles full tank no-op, drained 0t refill from cargo, partial cargo transfer, and zero-cargo top-up.
	fullState := expedition.ActiveCarrierRouteState{
		Route: expedition.CarrierRouteResult{CarrierType: "player", CapacityUsed: 5000, DepotTritium: 1000},
	}
	if _, changed := expedition.RefillCarrierDepotState(fullState); changed {
		testingContext.Fatalf("expected full depot refill to report changed=false")
	}

	emptyDepotWithCargo := expedition.ActiveCarrierRouteState{
		JumpIndex: 0,
		Route: expedition.CarrierRouteResult{
			CarrierType:  "player",
			CapacityUsed: 5000,
			DepotTritium: 0,
			Jumps:        []expedition.CarrierRouteJump{{SystemName: "Colonia", DistanceLy: 500}},
		},
	}
	refilledFromCargo, changed := expedition.RefillCarrierDepotState(emptyDepotWithCargo)
	if !changed || refilledFromCargo.Route.DepotTritium != 1000 || refilledFromCargo.Route.CapacityUsed != 4000 {
		testingContext.Fatalf("expected 0->1000 depot and 5000->4000 capacity, got changed=%v state=%+v", changed, refilledFromCargo.Route)
	}

	partialCargoState := expedition.ActiveCarrierRouteState{
		Route: expedition.CarrierRouteResult{CarrierType: "player", CapacityUsed: 250, DepotTritium: 500},
	}
	partiallyRefilled, partialChanged := expedition.RefillCarrierDepotState(partialCargoState)
	if !partialChanged || partiallyRefilled.Route.DepotTritium != 750 || partiallyRefilled.Route.CapacityUsed != 0 {
		testingContext.Fatalf("expected partial cargo transfer 500->750 and capacity 0, got %+v", partiallyRefilled.Route)
	}

	zeroCargoState := expedition.ActiveCarrierRouteState{
		Route: expedition.CarrierRouteResult{CarrierType: "player", CapacityUsed: 0, DepotTritium: 400},
	}
	zeroCargoRefilled, zeroChanged := expedition.RefillCarrierDepotState(zeroCargoState)
	if !zeroChanged || zeroCargoRefilled.Route.DepotTritium != 1000 || zeroCargoRefilled.Route.CapacityUsed != 0 {
		testingContext.Fatalf("expected zero-cargo depot top-up to 1000 with capacity 0, got %+v", zeroCargoRefilled.Route)
	}

	// SwitchRouteStateToManual seeds rounded hop distance and tritium or clears when past end.
	switchedMidRoute := expedition.SwitchRouteStateToManual(expedition.ActiveCarrierRouteState{
		JumpIndex: 0,
		Route: expedition.CarrierRouteResult{
			Jumps: []expedition.CarrierRouteJump{{SystemName: "Rohini", DistanceLy: 495.276, TritiumUsed: 90}},
		},
	})
	if !switchedMidRoute.ManualMode || switchedMidRoute.ManualDistanceInput != "495.28" || switchedMidRoute.ManualTritiumInput != "90" {
		testingContext.Fatalf("unexpected seeded manual state: %+v", switchedMidRoute)
	}
	switchedAtEnd := expedition.SwitchRouteStateToManual(expedition.ActiveCarrierRouteState{
		JumpIndex:           1,
		ManualDistanceInput: "stale",
		ManualTritiumInput:  "99",
		Route:               switchedMidRoute.Route,
	})
	if !switchedAtEnd.ManualMode || switchedAtEnd.ManualDistanceInput != "" || switchedAtEnd.ManualTritiumInput != "" {
		testingContext.Fatalf("expected manual inputs cleared when JumpIndex >= len(Jumps), got %+v", switchedAtEnd)
	}
}

func TestResolveStartSystemStopLocksAndBuildPlanRouteEmbedding(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	_, expeditionProjections := newExpeditionProjections(databaseConnection)

	ingestJournalLines(testingContext, databaseConnection, []string{
		`{"timestamp":"2026-02-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
	})

	expeditionRecord, originRow := createExpeditionWithOriginRow(testingContext, databaseConnection, "Route Locks", "")
	setRowSystemName(testingContext, databaseConnection, originRow.ID, "Sol")
	midRow := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, originRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, midRow.ID, "Rohini")
	firstStopRow := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, midRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, firstStopRow.ID, "Gandharvi")
	secondStopRow := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, firstStopRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, secondStopRow.ID, "Colonia")

	flagTrue := true
	flagFalse := false
	for _, rowID := range []int64{firstStopRow.ID, secondStopRow.ID} {
		if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: rowID, FCStop: &flagTrue}); err != nil {
			testingContext.Fatal(err)
		}
	}
	if startErr := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-02-01T10:00:00Z", 0); startErr != nil {
		testingContext.Fatal(startErr)
	}

	rows, listErr := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listErr != nil {
		testingContext.Fatal(listErr)
	}

	// Verify exact 4-tier ResolveCarrierStartSystem fallback chain.
	if got := expedition.ResolveCarrierStartSystem(rows, 3, "  Docked Carrier Sys ", "Player Sys"); got != "Docked Carrier Sys" {
		testingContext.Fatalf("tier 1 carrier location = %q, want Docked Carrier Sys", got)
	}
	if got := expedition.ResolveCarrierStartSystem(rows, 3, " ", "Player Sys"); got != "Gandharvi" {
		testingContext.Fatalf("tier 2 preceding FC stop = %q, want Gandharvi", got)
	}
	if got := expedition.ResolveCarrierStartSystem(rows, 2, "", "Player Sys"); got != "Rohini" {
		testingContext.Fatalf("tier 3 preceding waypoint = %q, want Rohini", got)
	}
	if got := expedition.ResolveCarrierStartSystem(rows, 0, "", "  Explorer Anchorage "); got != "Explorer Anchorage" {
		testingContext.Fatalf("tier 4 player system = %q, want Explorer Anchorage", got)
	}

	// TargetRowID == 0 on an active Spansh route falls back to nextFCStopRowID in both PlanView and row_repo validation.
	targetlessSpansh := expedition.ActiveCarrierRouteState{
		ExpeditionID: expeditionRecord.ID,
		TargetRowID:  0,
		Route: expedition.CarrierRouteResult{
			Origin:       "Sol",
			Destination:  "Gandharvi",
			DepotTritium: 900,
			Jumps:        []expedition.CarrierRouteJump{{SystemName: "Gandharvi", DistanceLy: 450, TritiumUsed: 70}},
		},
	}
	if err := expedition.SaveActiveCarrierRoute(databaseConnection, targetlessSpansh); err != nil {
		testingContext.Fatal(err)
	}

	planView, planErr := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, expeditionProjections)
	if planErr != nil {
		testingContext.Fatal(planErr)
	}
	if planView.ActiveCarrierRoute == nil || planView.NextFCStopRowID != firstStopRow.ID || planView.RoutedCarrierStopRowID != firstStopRow.ID {
		testingContext.Fatalf("expected BuildPlan to embed route and fallback RoutedCarrierStopRowID=%d, got next=%d routed=%d route=%+v",
			firstStopRow.ID, planView.NextFCStopRowID, planView.RoutedCarrierStopRowID, planView.ActiveCarrierRoute)
	}
	if gotArrivalID := expedition.ResolveArrivalTargetRowID(rows, planView.ActiveCarrierRoute); gotArrivalID != firstStopRow.ID {
		testingContext.Fatalf("ResolveArrivalTargetRowID fallback = %d, want %d", gotArrivalID, firstStopRow.ID)
	}

	// validateFCStopAgainstActiveRoute blocks toggling FCStop at or before firstStopRow even when TargetRowID == 0.
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: firstStopRow.ID, FCStop: &flagFalse}); err == nil || !strings.Contains(err.Error(), "active routed stop") {
		testingContext.Fatalf("expected FCStop removal on fallback Spansh target to fail, got %v", err)
	}
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: midRow.ID, FCStop: &flagTrue}); err == nil || !strings.Contains(err.Error(), "active routed stop") {
		testingContext.Fatalf("expected FCStop addition before fallback Spansh target to fail, got %v", err)
	}
}

func TestCompleteSpanshJumpLogManualJumpAndDraftInputsTx(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	expeditionRecord, originRow := createExpeditionWithOriginRow(testingContext, databaseConnection, "Carrier Tx Ops", "")
	setRowSystemName(testingContext, databaseConnection, originRow.ID, "Sol")
	stopRow := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, originRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, stopRow.ID, "Colonia")
	flagTrue := true
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: stopRow.ID, FCStop: &flagTrue}); err != nil {
		testingContext.Fatal(err)
	}

	// No active route: SaveCarrierManualDraftInputs no-ops, CompleteSpanshJumpTx returns !applied, LogManualCarrierJumpTx errors.
	if err := expedition.SaveCarrierManualDraftInputs(databaseConnection, expeditionRecord.ID, "100", "20"); err != nil {
		testingContext.Fatalf("SaveCarrierManualDraftInputs absent: %v", err)
	}
	if next, applied, recorded, err := expedition.CompleteSpanshJumpTx(databaseConnection, expeditionRecord.ID); err != nil || applied || recorded || next != nil {
		testingContext.Fatalf("expected CompleteSpanshJumpTx no-op when absent, got next=%+v applied=%v recorded=%v err=%v", next, applied, recorded, err)
	}
	if next, applied, err := expedition.LogManualCarrierJumpTx(databaseConnection, expeditionRecord.ID, 0, -5); err != nil || applied || next != nil {
		testingContext.Fatalf("expected non-positive manual jump to no-op, got next=%+v applied=%v err=%v", next, applied, err)
	}
	if _, _, err := expedition.LogManualCarrierJumpTx(databaseConnection, expeditionRecord.ID, 250, 40); err == nil {
		testingContext.Fatalf("expected LogManualCarrierJumpTx to fail when no active route exists")
	}

	// Manual route draft persistence and jump logging.
	manualState := expedition.ActiveCarrierRouteState{
		ExpeditionID: expeditionRecord.ID,
		TargetRowID:  stopRow.ID,
		ManualMode:   true,
		Route:        expedition.CarrierRouteResult{Origin: "Sol", Destination: "Colonia", ManualMode: true},
	}
	if err := expedition.SaveActiveCarrierRoute(databaseConnection, manualState); err != nil {
		testingContext.Fatal(err)
	}
	if err := expedition.SaveCarrierManualDraftInputs(databaseConnection, expeditionRecord.ID, "312.5", "48"); err != nil {
		testingContext.Fatalf("SaveCarrierManualDraftInputs: %v", err)
	}
	loggedManual, didLog, manualErr := expedition.LogManualCarrierJumpTx(databaseConnection, expeditionRecord.ID, 312.5, 48)
	if manualErr != nil || !didLog || loggedManual == nil || loggedManual.ManualLegJumps != 1 ||
		loggedManual.ManualLegDistance != 312.5 || loggedManual.ManualLegTritium != 48 ||
		loggedManual.ManualDistanceInput != "" || loggedManual.ManualTritiumInput != "" {
		testingContext.Fatalf("unexpected LogManualCarrierJumpTx outcome: state=%+v didLog=%v err=%v", loggedManual, didLog, manualErr)
	}

	// Spansh route progression: insufficient fuel rejection, intermediate hop fuel deduction, final arrival, and already-recorded arrival.
	lowFuelSpansh := expedition.ActiveCarrierRouteState{
		ExpeditionID: expeditionRecord.ID,
		TargetRowID:  stopRow.ID,
		JumpIndex:    0,
		Route: expedition.CarrierRouteResult{
			Origin:       "Sol",
			Destination:  "Colonia",
			CarrierType:  "player",
			CapacityUsed: 0,
			DepotTritium: 50,
			Jumps: []expedition.CarrierRouteJump{
				{SystemName: "Rohini", DistanceLy: 500, TritiumUsed: 70, FuelInTank: 50},
				{SystemName: "Colonia", DistanceLy: 500, TritiumUsed: 70, FuelInTank: 50},
			},
		},
	}
	if err := expedition.SaveActiveCarrierRoute(databaseConnection, lowFuelSpansh); err != nil {
		testingContext.Fatal(err)
	}
	if _, _, _, err := expedition.CompleteSpanshJumpTx(databaseConnection, expeditionRecord.ID); err == nil || err.Error() != "insufficient depot tritium for jump" {
		testingContext.Fatalf("expected insufficient depot tritium error, got %v", err)
	}

	fueledSpansh := lowFuelSpansh
	fueledSpansh.Route.DepotTritium = 200
	if err := expedition.SaveActiveCarrierRoute(databaseConnection, fueledSpansh); err != nil {
		testingContext.Fatal(err)
	}
	hopOneState, hopOneApplied, hopOneRecorded, hopOneErr := expedition.CompleteSpanshJumpTx(databaseConnection, expeditionRecord.ID)
	if hopOneErr != nil || !hopOneApplied || !hopOneRecorded || hopOneState == nil ||
		hopOneState.JumpIndex != 1 || hopOneState.Route.DepotTritium != 130 || hopOneState.Route.Jumps[1].FuelInTank != 130 {
		testingContext.Fatalf("unexpected intermediate Spansh hop state: state=%+v applied=%v recorded=%v err=%v", hopOneState, hopOneApplied, hopOneRecorded, hopOneErr)
	}

	arrivalState, arrivalApplied, arrivalRecorded, arrivalErr := expedition.CompleteSpanshJumpTx(databaseConnection, expeditionRecord.ID)
	if arrivalErr != nil || !arrivalApplied || !arrivalRecorded || arrivalState != nil {
		testingContext.Fatalf("expected final Spansh hop to arrive and clear route, got state=%+v applied=%v recorded=%v err=%v", arrivalState, arrivalApplied, arrivalRecorded, arrivalErr)
	}
	arrivedRow, rowErr := expedition.GetRow(databaseConnection, stopRow.ID)
	if rowErr != nil || !arrivedRow.FCArrived {
		testingContext.Fatalf("expected stopRow marked arrived, got row=%+v err=%v", arrivedRow, rowErr)
	}

	// Already-recorded Spansh route (JumpIndex >= len(Jumps)) marks arrival with recordedJump=false.
	flagFalse := false
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: stopRow.ID, FCArrived: &flagFalse}); err != nil {
		testingContext.Fatal(err)
	}
	alreadyRecorded := fueledSpansh
	alreadyRecorded.JumpIndex = 2
	if err := expedition.SaveActiveCarrierRoute(databaseConnection, alreadyRecorded); err != nil {
		testingContext.Fatal(err)
	}
	beforeExp := mustGetExpedition(testingContext, databaseConnection, expeditionRecord.ID)
	doneState, doneApplied, doneRecorded, doneErr := expedition.CompleteSpanshJumpTx(databaseConnection, expeditionRecord.ID)
	afterExp := mustGetExpedition(testingContext, databaseConnection, expeditionRecord.ID)
	if doneErr != nil || !doneApplied || doneRecorded || doneState != nil || afterExp.Adjustments.CarrierJumps != beforeExp.Adjustments.CarrierJumps {
		testingContext.Fatalf("expected already-recorded arrival with zero extra jump stats, got state=%+v applied=%v recorded=%v err=%v before=%d after=%d",
			doneState, doneApplied, doneRecorded, doneErr, beforeExp.Adjustments.CarrierJumps, afterExp.Adjustments.CarrierJumps)
	}
}
