package app

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"planisphere/internal/exporter"
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
	if !exportData.Departed || exportData.Live.Jumps != 1 || len(exportData.RowDetails) != 1 || len(exportData.HighlightsFullOrder) != 9 {
		testingContext.Fatalf("unexpected export snapshot: departed=%v jumps=%d rows=%d highlights=%d",
			exportData.Departed, exportData.Live.Jumps, len(exportData.RowDetails), len(exportData.HighlightsFullOrder))
	}

	tempDir := testingContext.TempDir()
	singleJSONPath := filepath.Join(tempDir, "facade-export-snapshot.json")
	savedSinglePath, singleWriteError := testApp.WriteExportOutput(singleJSONPath, []exporter.BundleFile{
		{Name: "facade-export-snapshot.json", CanonicalJSONExpeditionID: createdExpedition.ID},
	})
	if singleWriteError != nil || savedSinglePath != singleJSONPath {
		testingContext.Fatalf("WriteExportOutput canonical json: path=%q err=%v", savedSinglePath, singleWriteError)
	}
	writtenJSONBytes, readError := os.ReadFile(singleJSONPath)
	if readError != nil {
		testingContext.Fatalf("ReadFile(%q): %v", singleJSONPath, readError)
	}
	var decodedSnapshot exporter.ExpeditionExport
	if unmarshalError := json.Unmarshal(writtenJSONBytes, &decodedSnapshot); unmarshalError != nil {
		testingContext.Fatalf("unmarshal written canonical json: %v", unmarshalError)
	}
	if decodedSnapshot.SchemaVersion != exporter.SchemaVersion || len(decodedSnapshot.HighlightsFullOrder) != 9 {
		testingContext.Fatalf("unexpected written canonical json: schema=%d highlights=%d",
			decodedSnapshot.SchemaVersion, len(decodedSnapshot.HighlightsFullOrder))
	}

	zipBundlePath := filepath.Join(tempDir, "facade-export.zip")
	if _, zipWriteError := testApp.WriteExportOutput(zipBundlePath, []exporter.BundleFile{
		{Name: "facade-export-report.html", ContentText: "<!DOCTYPE html><title>Facade</title>"},
		{Name: "facade-export-snapshot.json", CanonicalJSONExpeditionID: createdExpedition.ID},
	}); zipWriteError != nil {
		testingContext.Fatalf("WriteExportOutput zip with canonical json: %v", zipWriteError)
	}
	zipArchive, openZipError := zip.OpenReader(zipBundlePath)
	if openZipError != nil {
		testingContext.Fatalf("OpenReader(%q): %v", zipBundlePath, openZipError)
	}
	defer zipArchive.Close()
	foundCanonicalEntry := false
	for _, entry := range zipArchive.File {
		if entry.Name != "facade-export-snapshot.json" {
			continue
		}
		foundCanonicalEntry = true
		entryReader, openEntryError := entry.Open()
		if openEntryError != nil {
			testingContext.Fatalf("open zip entry: %v", openEntryError)
		}
		entryBytes, readEntryError := io.ReadAll(entryReader)
		entryReader.Close()
		if readEntryError != nil || len(entryBytes) == 0 {
			testingContext.Fatalf("read canonical zip entry: len=%d err=%v", len(entryBytes), readEntryError)
		}
	}
	if !foundCanonicalEntry {
		testingContext.Fatalf("expected facade-export-snapshot.json inside %q", zipBundlePath)
	}
}
