package app

import (
	"path/filepath"
	"testing"

	"planisphere/internal/export"
	"planisphere/internal/store/expedition"
)

func TestAppExportFacadeEndToEnd(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	insertTestJournalEvent(testingContext, testApp, "2026-04-01T09:00:00Z", "LoadGame", "cmdr", `{"timestamp":"2026-04-01T09:00:00Z","event":"LoadGame","Commander":"CMDR Facade","Ship":"Mandalay"}`)
	insertTestJournalEvent(testingContext, testApp, "2026-04-01T09:05:00Z", "Location", "sol", `{"timestamp":"2026-04-01T09:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`)
	insertTestJournalEvent(testingContext, testApp, "2026-04-01T10:00:00Z", "FSDJump", "dest", `{"timestamp":"2026-04-01T10:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":2001,"StarPos":[6,0,0],"JumpDist":6.0}`)

	createdExpedition, createError := expedition.CreateWithWaypoints(
		testApp.db,
		"Facade Export",
		"End to end",
		[]expedition.SeedWaypoint{{SystemName: "Sol"}, {SystemName: "Barnard's Star"}},
	)
	if createError != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", createError)
	}
	if startError := testApp.StartExpedition(createdExpedition.ID, "2026-04-01T09:05:00Z", 0); startError != nil {
		testingContext.Fatalf("StartExpedition: %v", startError)
	}

	exportData, exportError := testApp.GetExpeditionExportData(createdExpedition.ID)
	if exportError != nil {
		testingContext.Fatalf("GetExpeditionExportData: %v", exportError)
	}
	if !exportData.Departed || exportData.Live.Jumps != 1 || len(exportData.RowDetails) != 1 {
		testingContext.Fatalf("unexpected export snapshot: departed=%v jumps=%d rows=%d", exportData.Departed, exportData.Live.Jumps, len(exportData.RowDetails))
	}

	customRange, rangeError := testApp.ComputeExportRangeStats(createdExpedition.ID, "2026-04-01T10:00:00Z", "", 0)
	if rangeError != nil {
		testingContext.Fatalf("ComputeExportRangeStats: %v", rangeError)
	}
	if customRange.Metrics.Jumps != 1 {
		testingContext.Fatalf("expected 1 jump in custom range, got %d", customRange.Metrics.Jumps)
	}

	outputPath := filepath.Join(testingContext.TempDir(), "snapshot.json")
	savedPath, writeError := testApp.WriteExportOutput(outputPath, []export.BundleFile{
		{Name: "snapshot.json", ContentText: `{"schemaVersion":1}`},
	})
	if writeError != nil || savedPath != outputPath {
		testingContext.Fatalf("WriteExportOutput: path=%q err=%v", savedPath, writeError)
	}
}
