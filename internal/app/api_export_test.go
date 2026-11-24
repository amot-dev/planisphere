package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"planisphere/internal/exporter"
	"planisphere/internal/store/expedition"
)

func writeAndDecodeCanonicalExport(
	testingContext *testing.T,
	testApp *App,
	targetPath string,
	expeditionID int64,
	exportedAt string,
) exporter.ExpeditionExport {
	testingContext.Helper()
	savedPath, writeError := testApp.WriteExportOutput(targetPath, []exporter.BundleFile{
		{
			Name:                      filepath.Base(targetPath),
			CanonicalJSONExpeditionID: expeditionID,
			CanonicalJSONExportedAt:   exportedAt,
		},
	})
	if writeError != nil || savedPath != targetPath {
		testingContext.Fatalf("WriteExportOutput(%q): path=%q err=%v", targetPath, savedPath, writeError)
	}
	writtenBytes, readError := os.ReadFile(targetPath)
	if readError != nil {
		testingContext.Fatalf("ReadFile(%q): %v", targetPath, readError)
	}
	var decodedSnapshot exporter.ExpeditionExport
	if unmarshalError := json.Unmarshal(writtenBytes, &decodedSnapshot); unmarshalError != nil {
		testingContext.Fatalf("unmarshal written canonical json: %v", unmarshalError)
	}
	return decodedSnapshot
}

func TestAppExportFacadeEndToEnd(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	currentExportTime := time.Date(2026, 4, 1, 10, 30, 0, 0, time.UTC)
	previousTimeNow := timeNow
	timeNow = func() time.Time { return currentExportTime }
	testingContext.Cleanup(func() { timeNow = previousTimeNow })

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
	if !exportData.Departed || exportData.Live.Jumps != 1 || len(exportData.RowDetails) != 1 || len(exportData.HighlightsFullOrder) != 9 {
		testingContext.Fatalf("unexpected export snapshot: departed=%v jumps=%d rows=%d highlights=%d",
			exportData.Departed, exportData.Live.Jumps, len(exportData.RowDetails), len(exportData.HighlightsFullOrder))
	}

	// Ingest a new jump and advance the export clock after modal snapshot assembly.
	insertTestJournalEvent(testingContext, testApp, "2026-04-01T10:15:00Z", "FSDJump", "dest-2", `{"timestamp":"2026-04-01T10:15:00Z","event":"FSDJump","StarSystem":"Wolf 359","SystemAddress":2002,"StarPos":[10,0,0],"JumpDist":4.0}`)
	currentExportTime = time.Date(2026, 4, 1, 10, 45, 0, 0, time.UTC)

	tempDir := testingContext.TempDir()
	cachedSnapshot := writeAndDecodeCanonicalExport(
		testingContext,
		testApp,
		filepath.Join(tempDir, "facade-export-cached.json"),
		createdExpedition.ID,
		exportData.ExportedAt,
	)
	if cachedSnapshot.SchemaVersion != exporter.SchemaVersion ||
		cachedSnapshot.ExportedAt != exportData.ExportedAt ||
		cachedSnapshot.Live.Jumps != 1 ||
		len(cachedSnapshot.HighlightsFullOrder) != 9 {
		testingContext.Fatalf("expected matching CanonicalJSONExportedAt to reuse cached snapshot, got exportedAt=%q jumps=%d",
			cachedSnapshot.ExportedAt, cachedSnapshot.Live.Jumps)
	}

	rebuiltSnapshot := writeAndDecodeCanonicalExport(
		testingContext,
		testApp,
		filepath.Join(tempDir, "facade-export-rebuilt.json"),
		createdExpedition.ID,
		"2026-04-01T00:00:00Z",
	)
	if rebuiltSnapshot.ExportedAt != "2026-04-01T10:45:00Z" || rebuiltSnapshot.Live.Jumps != 2 {
		testingContext.Fatalf("expected non-matching CanonicalJSONExportedAt to rebuild fresh telemetry, got exportedAt=%q jumps=%d",
			rebuiltSnapshot.ExportedAt, rebuiltSnapshot.Live.Jumps)
	}
}
