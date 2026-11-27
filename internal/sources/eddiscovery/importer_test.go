package eddiscovery_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"planisphere/internal/sources/eddiscovery"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
)

func TestImportProgressSuppressesCompleteOnFailure(testingContext *testing.T) {
	eddPath := buildEDDArchive(testingContext, eddArchiveSeed{
		FileName: "EDDProgress.sqlite",
		TravelLogs: []eddTravelLogSeed{
			{ID: 1, Name: "Journal.2026-02-01T100000.01.log", Path: "/logs/Journal.2026-02-01T100000.01.log"},
		},
		Entries: []eddJournalEntrySeed{
			{ID: 1, TravelLogID: 1, EventType: "Location", EventData: `{"timestamp":"2026-02-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`},
		},
	})

	destConn := dbtest.OpenMemory(testingContext)
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
