package eddiscovery_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/sources/eddiscovery"
	"planisphere/internal/sources/journal"
)

func createTestEDDArchive(testingContext *testing.T) string {
	testingContext.Helper()
	eddPath := filepath.Join(testingContext.TempDir(), "EDDProgress.sqlite")

	eddConn, openEDDError := sql.Open("sqlite", eddPath)
	if openEDDError != nil {
		testingContext.Fatal(openEDDError)
	}
	defer eddConn.Close()

	if _, schemaError := eddConn.Exec(`
		CREATE TABLE TravelLogUnit (Id INTEGER PRIMARY KEY, Name TEXT, Path TEXT);
		CREATE TABLE JournalEntries (Id INTEGER PRIMARY KEY, TravelLogId INTEGER, EventType TEXT, EventData TEXT);
		INSERT INTO TravelLogUnit (Id, Name, Path) VALUES (1, 'Journal.2026-02-01T100000.01.log', '/logs/Journal.2026-02-01T100000.01.log');
		INSERT INTO JournalEntries (Id, TravelLogId, EventType, EventData) VALUES
			(1, 1, 'Location', '{"timestamp":"2026-02-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}');
	`); schemaError != nil {
		testingContext.Fatal(schemaError)
	}
	if closeError := eddConn.Close(); closeError != nil {
		testingContext.Fatal(closeError)
	}
	return eddPath
}

func TestImportProgressSuppressesCompleteOnFailure(testingContext *testing.T) {
	eddPath := createTestEDDArchive(testingContext)

	destConn, openDestError := db.Open(":memory:")
	if openDestError != nil {
		testingContext.Fatal(openDestError)
	}
	defer destConn.Close()

	ingester := journal.NewIngester(destConn)

	// Cancel mid-import right as importEntriesTx begins so txError is non-nil.
	cancellableContext, cancelImport := context.WithCancel(context.Background())
	defer cancelImport()

	var failedStages []string
	_, failedError := eddiscovery.Import(cancellableContext, ingester, eddPath, func(_, _, _ int, stage string) {
		failedStages = append(failedStages, stage)
		if stage == "Ingesting historical journal events into SQLite archive..." {
			cancelImport()
		}
	})
	if !errors.Is(failedError, context.Canceled) {
		testingContext.Fatalf("expected context.Canceled on mid-import abort, got %v", failedError)
	}
	if slices.Contains(failedStages, "Complete") {
		testingContext.Fatalf("expected failed import to omit Complete stage, got %v", failedStages)
	}

	// Valid import against the same archive succeeds and emits Complete.
	var succeededStages []string
	importResult, importError := eddiscovery.Import(context.Background(), ingester, eddPath, func(_, _, _ int, stage string) {
		succeededStages = append(succeededStages, stage)
	})
	if importError != nil {
		testingContext.Fatalf("expected valid import to succeed, got %v", importError)
	}
	if importResult.LinesRead != 1 {
		testingContext.Fatalf("expected 1 imported line, got %d", importResult.LinesRead)
	}
	if len(succeededStages) == 0 || succeededStages[len(succeededStages)-1] != "Complete" {
		testingContext.Fatalf("expected final progress stage Complete on success, got %v", succeededStages)
	}
}
