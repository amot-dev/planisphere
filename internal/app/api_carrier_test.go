package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	stats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/spansh"
	"planisphere/internal/store/expedition"
)

func TestLiveStatsCacheOnCarrierJump(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	expeditionID := createAndStartExpedition(testingContext, testApp, "Stats Cache Test", "Sol")

	filter := stats.Filter{Enabled: false}

	// Before departing WP #1, sidebar reports Started=true, Departed=false and skips TimeRangeStats.
	preDepartureSidebar, preErr := testApp.GetSidebarStats(expeditionID, filter)
	if preErr != nil {
		testingContext.Fatalf("GetSidebarStats pre-departure failed: %v", preErr)
	}
	if !preDepartureSidebar.Started || preDepartureSidebar.Departed {
		testingContext.Fatalf("expected Started=true Departed=false before leaving WP #1, got %+v", preDepartureSidebar)
	}

	// Depart WP #1 so expedition telemetry begins.
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:05:00Z", "FSDJump", "depart-sol",
		`{"timestamp":"2026-01-01T00:05:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`)

	// First call to GetSidebarStats after departure should populate liveStatsCache
	sidebar1, err := testApp.GetSidebarStats(expeditionID, filter)
	if err != nil {
		testingContext.Fatalf("GetSidebarStats failed: %v", err)
	}
	if !sidebar1.Departed || sidebar1.Start != "2026-01-01T00:05:00Z" {
		testingContext.Fatalf("expected Departed=true and Start=2026-01-01T00:05:00Z, got departed=%v start=%q", sidebar1.Departed, sidebar1.Start)
	}
	if sidebar1.Expedition.CarrierJumps != 0 {
		testingContext.Errorf("expected 0 carrier jumps, got %d", sidebar1.Expedition.CarrierJumps)
	}

	initialCacheEntryCount := testApp.timeRangeStats.CachedCount()
	initialGeneration := testApp.timeRangeStats.Generation()
	if initialCacheEntryCount == 0 {
		testingContext.Fatal("expected timeRangeStats to have at least 1 cached entry")
	}

	// Record carrier jump via manual carrier route RPC flow
	if _, err := testApp.OpenManualCarrierRoute(expeditionID); err != nil {
		testingContext.Fatalf("OpenManualCarrierRoute failed: %v", err)
	}
	if _, err := testApp.LogManualCarrierJump(expeditionID, 500.0, 100); err != nil {
		testingContext.Fatalf("LogManualCarrierJump failed: %v", err)
	}

	// timeRangeStats should NOT have been cleared because AppAdjustmentsChanged preserves cached journal telemetry
	if testApp.timeRangeStats.CachedCount() != initialCacheEntryCount || testApp.timeRangeStats.Generation() != initialGeneration {
		testingContext.Errorf("expected timeRangeStats count %d (gen %d) to be preserved, got %d (gen %d)",
			initialCacheEntryCount, initialGeneration, testApp.timeRangeStats.CachedCount(), testApp.timeRangeStats.Generation())
	}

	// Second call to GetSidebarStats uses the cached live metrics and combines with new adjustments
	sidebar2, err := testApp.GetSidebarStats(expeditionID, filter)
	if err != nil {
		testingContext.Fatalf("second GetSidebarStats failed: %v", err)
	}
	if sidebar2.Expedition.CarrierJumps != 1 {
		testingContext.Errorf("expected 1 carrier jump, got %d", sidebar2.Expedition.CarrierJumps)
	}
	if sidebar2.Expedition.CarrierDistanceLy != 500.0 {
		testingContext.Errorf("expected 500.0 carrier distance, got %f", sidebar2.Expedition.CarrierDistanceLy)
	}
	if sidebar2.Adjustments.CarrierJumps != 1 {
		testingContext.Errorf("expected 1 adjustment carrier jump, got %d", sidebar2.Adjustments.CarrierJumps)
	}
}

func TestExpeditionCarrierCostAndConfig(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	initialSettings, settingsErr := testApp.GetSettings()
	if settingsErr != nil {
		testingContext.Fatalf("GetSettings failed: %v", settingsErr)
	}
	if initialSettings.TritiumPricePerTonne != 50000 || initialSettings.DefaultTritiumPricePerTonne != 50000 {
		testingContext.Fatalf("expected default tritium price 50000, got %d (default %d)",
			initialSettings.TritiumPricePerTonne, initialSettings.DefaultTritiumPricePerTonne)
	}

	expeditionRecord, createErr := testApp.CreateExpedition("Deep Space Carrier", "Testing carrier cost")
	if createErr != nil {
		testingContext.Fatalf("CreateExpedition failed: %v", createErr)
	}
	if expeditionRecord.CarrierType != "player" || expeditionRecord.CarrierWeeklyUpkeep != 0 {
		testingContext.Fatalf("expected default carrierType 'player' and upkeep 0, got %q and %d",
			expeditionRecord.CarrierType, expeditionRecord.CarrierWeeklyUpkeep)
	}

	// Unstarted expedition with 0 jumps and 0 upkeep has 0 cost and TotalEstimated = false.
	unstartedSidebar, err := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{})
	if err != nil {
		testingContext.Fatalf("GetSidebarStats failed: %v", err)
	}
	if unstartedSidebar.CarrierCost.TotalCost != 0 || unstartedSidebar.CarrierCost.TotalEstimated {
		testingContext.Fatalf("expected 0 unstarted carrier cost, got %+v", unstartedSidebar.CarrierCost)
	}

	// Update carrier config to squadron carrier with 14,000,000 Cr/week upkeep and custom tritium price 52,000 Cr/t.
	if err := testApp.UpdateExpeditionCarrierConfig(expeditionRecord.ID, "squadron", 14_000_000); err != nil {
		testingContext.Fatalf("UpdateExpeditionCarrierConfig failed: %v", err)
	}
	updatedSettings, setPriceErr := testApp.SetTritiumPricePerTonne(52000)
	if setPriceErr != nil || updatedSettings.TritiumPricePerTonne != 52000 {
		testingContext.Fatalf("SetTritiumPricePerTonne failed: %+v, %v", updatedSettings, setPriceErr)
	}

	// Clone expedition preserves carrier_type and carrier_weekly_upkeep.
	clonedExpedition, cloneErr := testApp.CloneExpedition(expeditionRecord.ID)
	if cloneErr != nil {
		testingContext.Fatalf("CloneExpedition failed: %v", cloneErr)
	}
	if clonedExpedition.CarrierType != "squadron" || clonedExpedition.CarrierWeeklyUpkeep != 14_000_000 {
		testingContext.Fatalf("expected cloned expedition to preserve squadron/14000000, got %q/%d",
			clonedExpedition.CarrierType, clonedExpedition.CarrierWeeklyUpkeep)
	}

	// Start and complete expedition spanning exactly 7 days from WP #1 departure
	// (2026-01-01T00:00:00Z) to final arrival (2026-01-08T00:00:00Z), and log 2 carrier jumps consuming 150t of tritium.
	planView, planErr := testApp.GetPlan(expeditionRecord.ID)
	if planErr != nil || len(planView.Rows) == 0 {
		testingContext.Fatalf("GetPlan failed: len=%d err=%v", len(planView.Rows), planErr)
	}
	originRowID := planView.Rows[0].Row.ID
	originName := "Sol"
	if updateErr := testApp.UpdateRow(expedition.RowPatch{ID: originRowID, SystemName: &originName}); updateErr != nil {
		testingContext.Fatalf("UpdateRow origin failed: %v", updateErr)
	}
	if startErr := testApp.StartExpedition(expeditionRecord.ID, "2025-12-31T23:55:00Z", 0); startErr != nil {
		testingContext.Fatalf("StartExpedition failed: %v", startErr)
	}
	destinationRow, insertErr := testApp.AddRow(expeditionRecord.ID, originRowID, expedition.RowWaypoint, 0)
	if insertErr != nil {
		testingContext.Fatalf("AddRow failed: %v", insertErr)
	}
	destinationName := "Alpha Centauri"
	if updateErr := testApp.UpdateRow(expedition.RowPatch{ID: destinationRow.ID, SystemName: &destinationName}); updateErr != nil {
		testingContext.Fatalf("UpdateRow destination failed: %v", updateErr)
	}
	if _, execErr := testApp.db.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, raw_json, source_file, ingested_at)
		 VALUES ('ev-carrier-cost-depart', '2026-01-01T00:00:00Z', 'FSDJump', 'Transit', 1005, '{"timestamp":"2026-01-01T00:00:00Z","event":"FSDJump","StarSystem":"Transit","SystemAddress":1005,"StarPos":[0.5,0,0],"JumpDist":2.0}', 'test.log', '2026-01-01T00:00:00Z'),
		        ('ev-carrier-cost-dest', '2026-01-08T00:00:00Z', 'FSDJump', 'Alpha Centauri', 1002, '{"timestamp":"2026-01-08T00:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}', 'test.log', '2026-01-08T00:00:00Z')`,
	); execErr != nil {
		testingContext.Fatalf("insert departure and destination arrivals failed: %v", execErr)
	}
	if _, reevalErr := expedition.ReevaluateTx(testApp.db, expeditionRecord.ID); reevalErr != nil {
		testingContext.Fatalf("ReevaluateTx failed: %v", reevalErr)
	}
	if _, err := testApp.OpenManualCarrierRoute(expeditionRecord.ID); err != nil {
		testingContext.Fatalf("OpenManualCarrierRoute failed: %v", err)
	}
	if _, err := testApp.LogManualCarrierJump(expeditionRecord.ID, 490.0, 80); err != nil {
		testingContext.Fatalf("first LogManualCarrierJump failed: %v", err)
	}
	if err := testApp.CompleteCarrierRouteArrival(expeditionRecord.ID, 0, 410.0, 70); err != nil {
		testingContext.Fatalf("CompleteCarrierRouteArrival failed: %v", err)
	}
	if err := expedition.CompleteTx(testApp.db, expeditionRecord.ID); err != nil {
		testingContext.Fatalf("Complete failed: %v", err)
	}

	completedSidebar, err := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{})
	if err != nil {
		testingContext.Fatalf("GetSidebarStats completed failed: %v", err)
	}
	cost := completedSidebar.CarrierCost
	// Tritium: 150 * 52,000 = 7,800,000 Cr
	// Jumps: 2 * 100,000 = 200,000 Cr
	// Upkeep: 7 days @ 14,000,000/wk = 14,000,000 Cr
	// Total: 22,000,000 Cr
	if cost.CarrierType != "squadron" || cost.WeeklyUpkeep != 14_000_000 || cost.TritiumPrice != 52000 {
		testingContext.Fatalf("unexpected carrier cost metadata: %+v", cost)
	}
	if cost.TritiumCost != 7_800_000 || cost.JumpCost != 200_000 || cost.UpkeepCost != 14_000_000 || cost.TotalCost != 22_000_000 || !cost.TotalEstimated {
		testingContext.Fatalf("unexpected carrier cost breakdown: %+v", cost)
	}
}

func TestCarrierRoutePromptCancellationAndPlanEmissions(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	originalSpanshQuery := querySpanshCarrierRoute
	testingContext.Cleanup(func() { querySpanshCarrierRoute = originalSpanshQuery })

	insertTestJournalEvent(testingContext, testApp, "2026-02-01T09:00:00Z", "CarrierStats", "cs-1",
		`{"event":"CarrierStats","CarrierID":444001,"Callsign":"K1A-99Z","FuelLevel":720,"SpaceUsage":{"TotalCapacity":25000,"FreeSpace":18000,"Cargo":1200}}`)
	insertTestJournalEvent(testingContext, testApp, "2026-02-01T09:05:00Z", "Location", "loc-player",
		`{"timestamp":"2026-02-01T09:05:00Z","event":"Location","StarSystem":"Shinrarta Dezhra","SystemAddress":3932277478106,"StarPos":[55.7,17.6,27.1]}`)

	expOne, err := testApp.createExpeditionWithWaypoints("Carrier Prompt One", "", []string{"Sol", "Rohini", "Colonia"})
	if err != nil {
		testingContext.Fatalf("createExpeditionWithWaypoints: %v", err)
	}
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
	activeExp, err := testApp.createExpeditionWithWaypoints("Arrival Zero Target", "", []string{"Sol", "Beagle Point"})
	if err != nil {
		testingContext.Fatalf("createExpeditionWithWaypoints activeExp: %v", err)
	}
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
