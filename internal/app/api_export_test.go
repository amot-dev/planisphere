package app

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"planisphere/internal/exporter"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
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

func decodeZipEntrySnapshot(testingContext *testing.T, zipArchivePath, entryName string) exporter.ExpeditionExport {
	testingContext.Helper()
	zipReader, openError := zip.OpenReader(zipArchivePath)
	if openError != nil {
		testingContext.Fatalf("zip.OpenReader(%q): %v", zipArchivePath, openError)
	}
	defer zipReader.Close()
	for _, archiveEntry := range zipReader.File {
		if archiveEntry.Name != entryName {
			continue
		}
		entryReader, entryOpenError := archiveEntry.Open()
		if entryOpenError != nil {
			testingContext.Fatalf("open zip entry %q: %v", entryName, entryOpenError)
		}
		defer entryReader.Close()
		snapshotBytes, readError := io.ReadAll(entryReader)
		if readError != nil {
			testingContext.Fatalf("read zip entry %q: %v", entryName, readError)
		}
		var decodedSnapshot exporter.ExpeditionExport
		if unmarshalError := json.Unmarshal(snapshotBytes, &decodedSnapshot); unmarshalError != nil {
			testingContext.Fatalf("unmarshal zip entry %q: %v", entryName, unmarshalError)
		}
		return decodedSnapshot
	}
	testingContext.Fatalf("zip entry %q not found in %q", entryName, zipArchivePath)
	return exporter.ExpeditionExport{}
}

func TestAppExportFacadeEndToEnd(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	currentExportTime := time.Date(2026, 4, 1, 10, 30, 0, 0, time.UTC)
	previousTimeNow := timeNow
	timeNow = func() time.Time { return currentExportTime }
	testingContext.Cleanup(func() { timeNow = previousTimeNow })

	if _, seedProfileError := testApp.db.Exec(
		`INSERT INTO commander_profiles (profile_key, display_name, fid, universe, enabled) VALUES ('live:f410:era:0', 'CMDR Facade', 'F410', 'live', 1)`,
	); seedProfileError != nil {
		testingContext.Fatalf("seed commander_profiles: %v", seedProfileError)
	}
	publishTestJournalLines(
		testingContext,
		testApp,
		"2026-04-01T09:00:00Z",
		"2026-04-01T10:00:00Z",
		`{"timestamp":"2026-04-01T09:00:00Z","event":"LoadGame","Commander":"CMDR Facade","FID":"F410","Ship":"Mandalay"}`,
		`{"timestamp":"2026-04-01T09:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":2001,"StarPos":[6,0,0],"JumpDist":6.0}`,
	)

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

	// Trigger mid-session jump ingestion, background profile sync, settings/commander mutations, and clock advance while Export Studio is open.
	publishTestJournalLines(
		testingContext,
		testApp,
		"2026-04-01T10:15:00Z",
		"2026-04-01T10:15:00Z",
		`{"timestamp":"2026-04-01T10:15:00Z","event":"FSDJump","StarSystem":"Wolf 359","SystemAddress":2002,"StarPos":[10,0,0],"JumpDist":4.0}`,
	)
	currentExportTime = time.Date(2026, 4, 1, 10, 45, 0, 0, time.UTC)
	if _, scopeError := testApp.SetCareerBaselineScope(settings.CareerScopeAllEnabled); scopeError != nil {
		testingContext.Fatalf("SetCareerBaselineScope: %v", scopeError)
	}
	if _, toggleError := testApp.SetCommanderEnabled("live:f410:era:0", true); toggleError != nil {
		testingContext.Fatalf("SetCommanderEnabled: %v", toggleError)
	}
	if scopedUpdateError := testApp.UpdateExpeditionScoped(createdExpedition.ID, "Facade Export", "Updated mid-session", 0); scopedUpdateError != nil {
		testingContext.Fatalf("UpdateExpeditionScoped: %v", scopedUpdateError)
	}
	testApp.publishIngestResult(journal.Result{
		Events:            journal.NewEventSet(journal.EventLoadGame),
		ProfilesChanged:   true,
		EarliestTimestamp: "2026-04-01T10:20:00Z",
		LatestTimestamp:   "2026-04-01T10:20:00Z",
	})

	tempDir := testingContext.TempDir()
	zipBundlePath := filepath.Join(tempDir, "facade-export.zip")
	savedZipPath, zipWriteError := testApp.WriteExportOutput(zipBundlePath, []exporter.BundleFile{
		{
			Name:        "facade-export-report.html",
			ContentText: "<!doctype html><title>T_open Report</title>",
		},
		{
			Name:                      "facade-export-snapshot.json",
			CanonicalJSONExpeditionID: createdExpedition.ID,
			CanonicalJSONExportedAt:   exportData.ExportedAt,
		},
	})
	if zipWriteError != nil || savedZipPath != zipBundlePath {
		testingContext.Fatalf("WriteExportOutput zip: path=%q err=%v", savedZipPath, zipWriteError)
	}
	bundledSnapshot := decodeZipEntrySnapshot(testingContext, zipBundlePath, "facade-export-snapshot.json")
	if bundledSnapshot.SchemaVersion != exporter.SchemaVersion ||
		bundledSnapshot.ExportedAt != exportData.ExportedAt ||
		bundledSnapshot.Live.Jumps != 1 ||
		bundledSnapshot.Plan.Expedition.Description != "End to end" ||
		len(bundledSnapshot.HighlightsFullOrder) != 9 {
		testingContext.Fatalf("expected pinned zip bundle to preserve modal snapshot without drift, got exportedAt=%q jumps=%d desc=%q",
			bundledSnapshot.ExportedAt, bundledSnapshot.Live.Jumps, bundledSnapshot.Plan.Expedition.Description)
	}

	unpinnedSnapshot := writeAndDecodeCanonicalExport(
		testingContext,
		testApp,
		filepath.Join(tempDir, "facade-export-unpinned.json"),
		createdExpedition.ID,
		"",
	)
	if unpinnedSnapshot.ExportedAt != "2026-04-01T10:45:00Z" ||
		unpinnedSnapshot.Live.Jumps != 2 ||
		unpinnedSnapshot.Plan.Expedition.Description != "Updated mid-session" {
		testingContext.Fatalf("expected unpinned CanonicalJSONExportedAt to rebuild fresh telemetry, got exportedAt=%q jumps=%d desc=%q",
			unpinnedSnapshot.ExportedAt, unpinnedSnapshot.Live.Jumps, unpinnedSnapshot.Plan.Expedition.Description)
	}

	currentExportTime = time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC)
	rebuiltSnapshot := writeAndDecodeCanonicalExport(
		testingContext,
		testApp,
		filepath.Join(tempDir, "facade-export-rebuilt.json"),
		createdExpedition.ID,
		"2026-04-01T00:00:00Z",
	)
	if rebuiltSnapshot.ExportedAt != "2026-04-01T11:00:00Z" || rebuiltSnapshot.Live.Jumps != 2 {
		testingContext.Fatalf("expected non-matching CanonicalJSONExportedAt to rebuild fresh telemetry, got exportedAt=%q jumps=%d",
			rebuiltSnapshot.ExportedAt, rebuiltSnapshot.Live.Jumps)
	}

	if _, switchError := testApp.SetDatabasePath(filepath.Join(tempDir, "switched.sqlite")); switchError != nil {
		testingContext.Fatalf("SetDatabasePath: %v", switchError)
	}
	if _, matchedAfterSwitch := testApp.matchingExportSnapshot(createdExpedition.ID, rebuiltSnapshot.ExportedAt); matchedAfterSwitch {
		testingContext.Fatalf("expected database switch to clear cached export snapshot")
	}
}
