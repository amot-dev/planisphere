package realization_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

func insertLedgerTestEvent(testingContext *testing.T, databaseConnection *sql.DB, timestamp, eventName, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, rawJSON)
	_, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, rawJSON, timestamp,
	)
	if execError != nil {
		testingContext.Fatalf("insert %s event: %v", eventName, execError)
	}
}

func TestSalesLedgerCachingAndBusInvalidation(testingContext *testing.T) {
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("open test database: %v", openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})

	insertLedgerTestEvent(testingContext, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)

	firstResult, firstError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if firstError != nil {
		testingContext.Fatalf("first Breakdown failed: %v", firstError)
	}
	if firstResult.PendingValue <= 0 {
		testingContext.Fatalf("expected positive PendingValue, got %d", firstResult.PendingValue)
	}

	insertLedgerTestEvent(testingContext, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`)

	// Unrelated and session-context-only events must leave cached SalesLedger untouched.
	initialLedgerGeneration := salesLedger.Generation()
	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventFSDTarget,
		journal.EventFSSDiscoveryScan,
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventDocked,
		journal.EventUndocked,
	))
	if salesLedger.Generation() != initialLedgerGeneration {
		testingContext.Fatalf("expected session-context events not to invalidate SalesLedger, gen %d->%d", initialLedgerGeneration, salesLedger.Generation())
	}
	cachedResult, cachedError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if cachedError != nil {
		testingContext.Fatalf("cached Breakdown failed: %v", cachedError)
	}
	if cachedResult.PendingValue != firstResult.PendingValue {
		testingContext.Fatalf("expected identical cached pending value %d, got %d", firstResult.PendingValue, cachedResult.PendingValue)
	}

	// Publishing EventSellOrganicData invalidates SalesLedger so the sale appears.
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellOrganicData))
	refreshedResult, refreshedError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if refreshedError != nil {
		testingContext.Fatalf("refreshed Breakdown failed: %v", refreshedError)
	}
	if refreshedResult.PendingValue != 0 || refreshedResult.TotalRealized != 95054000 {
		testingContext.Fatalf("unexpected refreshed breakdown: %+v", refreshedResult)
	}

	// Publishing AppCommanderFilterChanged also invalidates the cached ledger.
	if _, execError := databaseConnection.Exec(`UPDATE journal_events SET commander = 'CmdrAlt'`); execError != nil {
		testingContext.Fatal(execError)
	}
	if _, execError := databaseConnection.Exec(`INSERT INTO commanders(name, enabled) VALUES ('CmdrAlt', 0)`); execError != nil {
		testingContext.Fatal(execError)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)
	filteredResult, filteredError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if filteredError != nil {
		testingContext.Fatalf("filtered Breakdown failed: %v", filteredError)
	}
	if filteredResult.TotalRealized != 0 {
		testingContext.Fatalf("expected TotalRealized 0 after disabling commander, got %d", filteredResult.TotalRealized)
	}
}

func insertScopedLedgerEvent(testingContext *testing.T, databaseConnection *sql.DB, profileID int64, commander, timestamp, eventName, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, rawJSON+"|"+commander)
	_, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, profile_id, commander, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, profileID, commander, rawJSON, timestamp,
	)
	if execError != nil {
		testingContext.Fatalf("insert scoped %s event: %v", eventName, execError)
	}
}

func TestSalesLedgerPerProfileIsolationAndDisabledScope(testingContext *testing.T) {
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("open test database: %v", openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})

	if _, err := databaseConnection.Exec(`
		INSERT INTO commander_profiles (id, profile_key, display_name, fid, universe, era_index, is_archived_career, enabled, first_seen, last_seen) VALUES
		(1, 'live:f1:era:0', 'CmdrOne', 'F1', 'live', 0, 0, 1, '2026-04-01T10:00:00Z', '2026-04-01T12:00:00Z'),
		(2, 'live:f2:era:0', 'CmdrTwo', 'F2', 'live', 0, 0, 1, '2026-04-01T10:00:00Z', '2026-04-01T12:00:00Z'),
		(3, 'live:f1:era:0_old', 'CmdrArchived', 'F3', 'live', 0, 1, 0, '2025-01-01T10:00:00Z', '2025-01-01T12:00:00Z')
	`); err != nil {
		testingContext.Fatalf("seed commander_profiles: %v", err)
	}

	// Profile 1 (enabled) scans an Earthlike body and Stratum Tectonicas and leaves both unsold in transit.
	insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrOne", "2026-04-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"AlphaSys","SystemAddress":1001,"BodyID":1,"BodyName":"AlphaSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrOne", "2026-04-01T10:05:00Z", "ScanOrganic",
		`{"timestamp":"2026-04-01T10:05:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1001,"Body":1}`)

	// Profile 2 (enabled) scans the same species and an Earthlike body, suffers ship destruction (rebuy), then scans and sells new data.
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T10:10:00Z", "Scan",
		`{"timestamp":"2026-04-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"AlphaSys","SystemAddress":1001,"BodyID":1,"BodyName":"AlphaSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T10:15:00Z", "ScanOrganic",
		`{"timestamp":"2026-04-01T10:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1001,"Body":1}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T10:30:00Z", "Resurrect",
		`{"timestamp":"2026-04-01T10:30:00Z","event":"Resurrect","Option":"rebuy","Cost":1200000,"Bankrupt":false}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T11:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-04-01T11:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":2002,"Body":2}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T11:30:00Z", "SellOrganicData",
		`{"timestamp":"2026-04-01T11:30:00Z","event":"SellOrganicData","MarketID":500,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T11:30:01Z", "MultiSellExplorationData",
		`{"timestamp":"2026-04-01T11:30:01Z","event":"MultiSellExplorationData","BaseValue":500000,"Bonus":0,"TotalEarnings":500000,"Discovered":[{"SystemName":"AlphaSys","NumBodies":1}]}`)

	// Profile 3 (disabled archived career) has its own sold organic sample.
	insertScopedLedgerEvent(testingContext, databaseConnection, 3, "CmdrArchived", "2025-01-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2025-01-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":3003,"Body":1}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 3, "CmdrArchived", "2025-01-01T11:00:00Z", "SellOrganicData",
		`{"timestamp":"2025-01-01T11:00:00Z","event":"SellOrganicData","MarketID":600,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)

	prof1Breakdown, err := salesLedger.BreakdownScoped("", "", replay.NewProfileScope([]int64{1}), nil)
	if err != nil {
		testingContext.Fatalf("BreakdownScoped(prof 1): %v", err)
	}
	if prof1Breakdown.PendingValue <= 95054000 || prof1Breakdown.CartoLostDeath != 0 || prof1Breakdown.OrganicLostDeath != 0 || prof1Breakdown.TotalRealized != 0 {
		testingContext.Fatalf("expected Profile 1 scans 100%% pending and untouched by Profile 2 death/sales, got %+v", prof1Breakdown)
	}

	prof2Breakdown, err := salesLedger.BreakdownScoped("", "", replay.NewProfileScope([]int64{2}), nil)
	if err != nil {
		testingContext.Fatalf("BreakdownScoped(prof 2): %v", err)
	}
	if prof2Breakdown.CartoLostDeath <= 0 || prof2Breakdown.OrganicLostDeath != 95054000 || prof2Breakdown.TotalRealized != 19510800 || prof2Breakdown.PendingValue != 0 {
		testingContext.Fatalf("unexpected Profile 2 breakdown: %+v", prof2Breakdown)
	}

	combinedBreakdown, err := salesLedger.Breakdown("", "", nil)
	if err != nil {
		testingContext.Fatalf("combined Breakdown: %v", err)
	}
	expectedCombined := prof1Breakdown.Add(prof2Breakdown)
	if combinedBreakdown != expectedCombined {
		testingContext.Fatalf("expected combined AllEnabled breakdown %+v, got %+v", expectedCombined, combinedBreakdown)
	}

	genBeforeArchived := salesLedger.Generation()
	prof3Breakdown, err := salesLedger.BreakdownScoped("", "", replay.NewProfileScope([]int64{3}), nil)
	if err != nil {
		testingContext.Fatalf("BreakdownScoped(disabled prof 3): %v", err)
	}
	if prof3Breakdown.TotalRealized != 95054000 || prof3Breakdown.PendingValue != 0 || salesLedger.Generation() != genBeforeArchived {
		testingContext.Fatalf("expected disabled archived Profile 3 breakdown TotalRealized=95054000 from warm ledger, got %+v", prof3Breakdown)
	}
}
