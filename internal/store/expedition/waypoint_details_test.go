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
