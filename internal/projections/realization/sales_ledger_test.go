package realization_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
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
