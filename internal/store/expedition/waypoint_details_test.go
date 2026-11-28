package expedition_test

import (
	"testing"

	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestDetailsOriginRealizationCacheAndErrorPropagation(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	eventBus, proj := expeditiontest.NewProjections(databaseConnection)

	if _, execError := databaseConnection.Exec(`INSERT INTO commanders (name, enabled, first_seen) VALUES ('Jameson', 1, '2026-01-01T00:00:00Z')`); execError != nil {
		testingContext.Fatal(execError)
	}

	// Insert pre-expedition organic scan and sale plus Sol and Acamar arrivals so origin realization is non-zero.
	journaltest.InsertParsedLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-01T10:00:00Z","event":"ScanOrganic","StarSystem":"Sol","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":10477373803,"Body":1}`,
		`{"timestamp":"2026-01-01T11:00:00Z","event":"SellOrganicData","StarSystem":"Sol","SystemAddress":10477373803,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`,
		`{"timestamp":"2026-01-01T12:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-02T12:00:00Z","event":"FSDJump","StarSystem":"Acamar","SystemAddress":2002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	)

	expeditionRecord := mustCreateWithWaypoints(testingContext, databaseConnection, "Realization Cache Test", "",
		expedition.SeedWaypoint{SystemName: "Sol"},
		expedition.SeedWaypoint{SystemName: "Acamar"},
	)
	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-01-01T12:00:00Z", 0); startError != nil {
		testingContext.Fatalf("StartTx: %v", startError)
	}
	seededRows, listRowsError := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listRowsError != nil || len(seededRows) != 2 {
		testingContext.Fatalf("ListRows: len=%d err=%v", len(seededRows), listRowsError)
	}
	originWaypoint, secondWaypoint := seededRows[0], seededRows[1]

	// First call populates the origin projection cache (both Metrics and Realization).
	firstOriginDetails, firstDetailsError := expedition.Details(databaseConnection, originWaypoint.ID, proj)
	if firstDetailsError != nil {
		testingContext.Fatalf("first Details call for origin waypoint failed: %v", firstDetailsError)
	}
	if firstOriginDetails.Realization.TotalRealized != 19010800 {
		testingContext.Fatalf("expected origin TotalRealized 19010800, got %d", firstOriginDetails.Realization.TotalRealized)
	}

	// Insert a corrupt SellExplorationData row with non-integer system_address.
	// Engine.Compute ignores SellExplorationData, whereas Engine.ComputeRealization scans it and returns a scan error.
	if _, execError := databaseConnection.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, raw_json, ingested_at)
		VALUES ('ev-corrupt-sale', '2026-01-02T00:00:00Z', 'SellExplorationData', 'Acamar', 'not-an-integer', '{}', '2026-01-02T00:00:00Z')`); execError != nil {
		testingContext.Fatal(execError)
	}

	// Because origin telemetry is cached in TimeRangeStats, calling Details on originWaypoint succeeds from cache.
	cachedOriginDetails, cachedDetailsError := expedition.Details(databaseConnection, originWaypoint.ID, proj)
	if cachedDetailsError != nil {
		testingContext.Fatalf("expected cached origin Details to succeed without recomputing realization, got error: %v", cachedDetailsError)
	}
	if cachedOriginDetails.Realization.TotalRealized != 19010800 {
		testingContext.Errorf("expected cached origin TotalRealized 19010800, got %d", cachedOriginDetails.Realization.TotalRealized)
	}

	// Calling Details without projections fails loudly (Approach B strict Bus injection).
	if _, nilProjError := expedition.Details(databaseConnection, secondWaypoint.ID, nil); nilProjError == nil {
		testingContext.Errorf("expected Details with nil projections to fail, got nil")
	}

	// After publishing SellExplorationData on the bus, calling Details on both leg and origin waypoints
	// must invalidate SalesLedger/TimeRangeStats, recompute, and propagate the ComputeRealization error.
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellExplorationData))
	if _, legDetailsError := expedition.Details(databaseConnection, secondWaypoint.ID, proj); legDetailsError == nil {
		testingContext.Errorf("expected Details on leg waypoint after cache invalidation to propagate ComputeRealization error, got nil")
	}
	if _, recomputedOriginError := expedition.Details(databaseConnection, originWaypoint.ID, proj); recomputedOriginError == nil {
		testingContext.Errorf("expected Details on origin waypoint after cache invalidation to propagate ComputeRealization error, got nil")
	}

	// Clean up corrupt row and invalidate projections so subsequent checks start cleanly.
	if _, execError := databaseConnection.Exec(`DELETE FROM journal_events WHERE event_uid = 'ev-corrupt-sale'`); execError != nil {
		testingContext.Fatal(execError)
	}
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellExplorationData))
}

func TestWaypointDetailsAndBatchPlayDurationsAdditiveEquality(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, proj := expeditiontest.NewProjections(databaseConnection)

	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		// Pre-expedition origin play session (1h) + departure from Sol at 11:00
		"Journal.2026-06-10T100000.01.log": {
			`{"timestamp":"2026-06-10T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-10T10:00:00Z","event":"LoadGame","Commander":"CmdrLeg","FID":"F801","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-10T10:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
			`{"timestamp":"2026-06-10T11:00:00Z","event":"FSDJump","StarSystem":"TransitOne","SystemAddress":1002,"StarPos":[10,0,0],"JumpDist":10.0}`,
			`{"timestamp":"2026-06-10T11:40:00Z","event":"FSDJump","StarSystem":"Acamar","SystemAddress":2002,"StarPos":[20,0,0],"JumpDist":10.0}`,
			`{"timestamp":"2026-06-10T12:00:00Z","event":"Shutdown"}`,
		},
		// Next day session in Acamar (30m in-system) then depart toward Colonia (20m in transit, active leg)
		"Journal.2026-06-11T090000.01.log": {
			`{"timestamp":"2026-06-11T09:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-11T09:00:00Z","event":"LoadGame","Commander":"CmdrLeg","FID":"F801","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-11T09:30:00Z","event":"FSDJump","StarSystem":"TransitTwo","SystemAddress":2003,"StarPos":[30,0,0],"JumpDist":10.0}`,
			`{"timestamp":"2026-06-11T09:50:00Z","event":"FSDJump","StarSystem":"TransitThree","SystemAddress":2004,"StarPos":[40,0,0],"JumpDist":10.0}`,
		},
	})

	expeditionRecord := mustCreateWithWaypoints(testingContext, databaseConnection, "Play Time Route", "",
		expedition.SeedWaypoint{SystemName: "Sol"},
		expedition.SeedWaypoint{SystemName: "Acamar"},
		expedition.SeedWaypoint{SystemName: "Colonia"},
	)
	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-06-10T10:05:00Z", 0); startError != nil {
		testingContext.Fatalf("StartTx: %v", startError)
	}
	planView, planError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, proj)
	if planError != nil {
		testingContext.Fatalf("BuildPlan: %v", planError)
	}

	originBatch, legsBatch, batchError := expedition.DetailsBatch(databaseConnection, planView, proj)
	if batchError != nil {
		testingContext.Fatalf("DetailsBatch: %v", batchError)
	}
	if originBatch == nil || originBatch.InGameSeconds == nil || *originBatch.InGameSeconds != 3600 || originBatch.InGameDuration != "1h 00m" {
		testingContext.Fatalf("unexpected origin baseline play duration: %+v", originBatch)
	}
	if len(legsBatch) != 2 {
		testingContext.Fatalf("expected 2 legs in batch (Acamar visited + Colonia active), got %d", len(legsBatch))
	}

	// Leg #2 (Acamar): transit 11:00->11:40 (40m = 2400s), in-system 11:40->12:00 (20m) + 09:00->09:30 (30m) = 50m (3000s), full = 90m (5400s).
	acamarSingle, singleErr := expedition.Details(databaseConnection, legsBatch[0].RowID, proj)
	if singleErr != nil {
		testingContext.Fatalf("Details(Acamar): %v", singleErr)
	}
	for label, leg := range map[string]expedition.WaypointDetails{"Details": acamarSingle, "DetailsBatch": legsBatch[0]} {
		if leg.TransitElapsed != "0h 40m" {
			testingContext.Errorf("%s Acamar TransitElapsed = %q, want 0h 40m", label, leg.TransitElapsed)
		}
		if leg.TransitInGameSeconds == nil || *leg.TransitInGameSeconds != 2400 || leg.TransitInGameDuration != "40m" {
			testingContext.Errorf("%s Acamar TransitInGame = (%v, %q), want (2400, 40m)", label, leg.TransitInGameSeconds, leg.TransitInGameDuration)
		}
		if leg.InSystemInGameSeconds == nil || *leg.InSystemInGameSeconds != 3000 || leg.InSystemInGameDuration != "50m" {
			testingContext.Errorf("%s Acamar InSystemInGame = (%v, %q), want (3000, 50m)", label, leg.InSystemInGameSeconds, leg.InSystemInGameDuration)
		}
		if leg.InGameSeconds == nil || *leg.InGameSeconds != *leg.TransitInGameSeconds+*leg.InSystemInGameSeconds || leg.InGameDuration != "1h 30m" {
			testingContext.Errorf("%s Acamar Full InGame = (%v, %q), want exact additive 5400 / 1h 30m", label, leg.InGameSeconds, leg.InGameDuration)
		}
	}

	// Leg #3 (Colonia active unvisited): transit 09:30->09:50 (20m = 1200s), no in-system split yet.
	coloniaLeg := legsBatch[1]
	if coloniaLeg.TransitInGameSeconds == nil || *coloniaLeg.TransitInGameSeconds != 1200 || coloniaLeg.TransitInGameDuration != "20m" {
		testingContext.Errorf("Colonia active TransitInGame = (%v, %q), want (1200, 20m)", coloniaLeg.TransitInGameSeconds, coloniaLeg.TransitInGameDuration)
	}
	if coloniaLeg.InGameSeconds == nil || *coloniaLeg.InGameSeconds != 1200 || coloniaLeg.InSystemInGameSeconds != nil {
		testingContext.Errorf("Colonia active InGame/InSystem = (%v, %v), want (1200, nil)", coloniaLeg.InGameSeconds, coloniaLeg.InSystemInGameSeconds)
	}
}
