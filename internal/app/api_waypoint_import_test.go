package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"planisphere/internal/sources/edsm"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func TestCreateExpeditionWithWaypointsOfflineResolutionAndGEC(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if _, onlineError := testApp.SetOnlineLookups(false); onlineError != nil {
		testingContext.Fatalf("disable online lookups: %v", onlineError)
	}

	// Seed a visited journal system with coordinates so offline resolution matches it immediately.
	_, insertJournalError := testApp.db.Exec(`INSERT INTO journal_events (
		event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, raw_json, ingested_at
	) VALUES (
		'ev-sol-import', '2026-01-01T10:00:00Z', 'FSDJump', 'Sol', 10477373803, 0.0, 0.0, 0.0, '{}', '2026-01-01T10:00:00Z'
	)`)
	if insertJournalError != nil {
		testingContext.Fatalf("insert journal event: %v", insertJournalError)
	}

	// Seed a cached EDSM system and a GEC POI to verify cache reuse and POI auto-population.
	_, insertCacheError := testApp.db.Exec(`INSERT INTO system_cache (
		name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at
	) VALUES (
		'omega sector ve-q b5-15', 'Omega Sector VE-Q b5-15', 999001, 777, 100.5, -20.25, 5400.0, 'edsm', '2026-01-01T10:00:00Z'
	)`)
	if insertCacheError != nil {
		testingContext.Fatalf("insert system_cache: %v", insertCacheError)
	}
	_, insertGECError := testApp.db.Exec(`INSERT INTO gec_pois_cache (
		id, name, system_name, system_address, poi_type, region, summary, poi_url, raw_json, source, fetched_at
	) VALUES (
		42, 'Omega Nebula', 'Omega Sector VE-Q b5-15', 999001, 'Nebula', 'Inner Orion Spur', '', 'https://edastro.com/gec/view/42', '{}', 'GEC', '2026-01-01T10:00:00Z'
	)`)
	if insertGECError != nil {
		testingContext.Fatalf("insert gec_pois_cache: %v", insertGECError)
	}
	if reloadError := testApp.gec.Reload(); reloadError != nil {
		testingContext.Fatalf("reload GEC catalog: %v", reloadError)
	}

	createdExpedition, createError := testApp.createExpeditionWithWaypoints(
		"Historical Import",
		"Imported from list",
		[]string{"  sol  ", "omega sector ve-q b5-15", "   ", "SOL"},
	)
	if createError != nil {
		testingContext.Fatalf("createExpeditionWithWaypoints: %v", createError)
	}
	if settings.ActiveExpeditionID(testApp.db) != createdExpedition.ID {
		testingContext.Fatalf("expected active expedition ID %d, got %d", createdExpedition.ID, settings.ActiveExpeditionID(testApp.db))
	}

	planView, planError := testApp.GetPlan(createdExpedition.ID)
	if planError != nil {
		testingContext.Fatalf("GetPlan: %v", planError)
	}
	if len(planView.Rows) != 3 {
		testingContext.Fatalf("expected 3 imported rows, got %d", len(planView.Rows))
	}

	firstRow := planView.Rows[0].Row
	if firstRow.SystemName != "Sol" || !firstRow.HasCoords || firstRow.CoordsSource != "journal" || firstRow.SystemAddress != 10477373803 {
		testingContext.Fatalf("unexpected row 0: %+v", firstRow)
	}

	secondRow := planView.Rows[1].Row
	if secondRow.SystemName != "Omega Sector VE-Q b5-15" || !secondRow.HasCoords || secondRow.CoordsSource != "edsm" || secondRow.EDSMID != 777 {
		testingContext.Fatalf("unexpected row 1 coords: %+v", secondRow)
	}
	if secondRow.POIName != "Omega Nebula" {
		testingContext.Fatalf("expected GEC POI name 'Omega Nebula', got %q", secondRow.POIName)
	}

	thirdRow := planView.Rows[2].Row
	if thirdRow.SystemName != "Sol" || !thirdRow.HasCoords || thirdRow.SystemAddress != 10477373803 {
		testingContext.Fatalf("expected revisited row 2 to reuse resolved Sol coordinates, got %+v", thirdRow)
	}
}

func TestReadWaypointImportFileValidation(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()

	validFilePath := filepath.Join(temporaryDirectory, "Xibalba.route.csv")
	csvContent := "\"System Name\",\"Body Name\"\n\"Sol\",\"Sol 3\"\n\"sol\",\"Sol 4\"\n\"Colonia\",\"Colonia 1\"\n"
	if writeError := os.WriteFile(validFilePath, []byte(csvContent), 0o600); writeError != nil {
		testingContext.Fatalf("write valid file: %v", writeError)
	}
	loadedFile, loadError := readWaypointImportFile(validFilePath)
	if loadError != nil {
		testingContext.Fatalf("readWaypointImportFile valid: %v", loadError)
	}
	if loadedFile.FileName != "Xibalba.route.csv" ||
		loadedFile.SuggestedExpeditionName != "Xibalba.route" ||
		loadedFile.NormalizedText != "Sol\nColonia" ||
		loadedFile.Parsed.Format != expedition.WaypointImportSpanshCSV ||
		!slices.Equal(loadedFile.Parsed.Systems, []string{"Sol", "Colonia"}) ||
		loadedFile.Parsed.CollapsedDuplicateCount != 1 {
		testingContext.Fatalf("unexpected loaded file: %+v", loadedFile)
	}
	cancelledFile := emptyWaypointImportFile()
	if cancelledFile.Parsed.Format != expedition.WaypointImportEmpty || cancelledFile.Parsed.Systems == nil || len(cancelledFile.Parsed.Systems) != 0 {
		testingContext.Fatalf("expected non-nil empty systems on cancelled file, got %+v", cancelledFile)
	}

	binaryFilePath := filepath.Join(temporaryDirectory, "workbook.xlsx")
	if writeError := os.WriteFile(binaryFilePath, []byte{'P', 'K', 0x03, 0x04, 0x00, 0x01}, 0o600); writeError != nil {
		testingContext.Fatalf("write binary file: %v", writeError)
	}
	if _, binaryError := readWaypointImportFile(binaryFilePath); binaryError == nil || !strings.Contains(binaryError.Error(), "plain text or CSV") {
		testingContext.Fatalf("expected binary file rejection, got %v", binaryError)
	}

	oversizeFilePath := filepath.Join(temporaryDirectory, "too-large.txt")
	oversizeBytes := make([]byte, maxWaypointImportFileBytes+1)
	for byteIndex := range oversizeBytes {
		oversizeBytes[byteIndex] = 'A'
	}
	if writeError := os.WriteFile(oversizeFilePath, oversizeBytes, 0o600); writeError != nil {
		testingContext.Fatalf("write oversize file: %v", writeError)
	}
	if _, sizeError := readWaypointImportFile(oversizeFilePath); sizeError == nil || !strings.Contains(sizeError.Error(), "5 MiB") {
		testingContext.Fatalf("expected oversize file rejection, got %v", sizeError)
	}
}

func TestParseWaypointImportAndCreateExpeditionFromImport(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if _, onlineError := testApp.SetOnlineLookups(false); onlineError != nil {
		testingContext.Fatalf("disable online lookups: %v", onlineError)
	}

	uninitializedParsed := New().ParseWaypointImport("1. Sol\n# skip\n2. Colonia")
	if uninitializedParsed.Format != expedition.WaypointImportPlain ||
		!slices.Equal(uninitializedParsed.Systems, []string{"Sol", "Colonia"}) ||
		uninitializedParsed.SkippedLineCount != 1 {
		testingContext.Fatalf("unexpected ParseWaypointImport result: %+v", uninitializedParsed)
	}

	if _, emptyError := testApp.CreateExpeditionFromImport("Empty Import", "", "# comment only\n   \n"); emptyError == nil ||
		!strings.Contains(emptyError.Error(), "no valid star systems") {
		testingContext.Fatalf("expected empty import error, got %v", emptyError)
	}

	createdExpedition, createError := testApp.CreateExpeditionFromImport(
		"Spansh Import",
		"From CSV",
		"\"System Name\",\"Jumps\"\n\"Sol\",\"0\"\n\"Sol\",\"0\"\n\"Colonia\",\"12\"\n",
	)
	if createError != nil {
		testingContext.Fatalf("CreateExpeditionFromImport: %v", createError)
	}
	planView, planError := testApp.GetPlan(createdExpedition.ID)
	if planError != nil || len(planView.Rows) != 2 || planView.Rows[0].Row.SystemName != "Sol" || planView.Rows[1].Row.SystemName != "Colonia" {
		testingContext.Fatalf("expected 2 imported rows [Sol, Colonia], got err=%v rows=%+v", planError, planView.Rows)
	}
}

func TestBuildSeedWaypointsBatchSinglePassAndDisabledCommanderFilter(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if _, execError := testApp.db.Exec(`
		INSERT INTO commanders (name, fid, enabled) VALUES ('CmdrEnabled', 'F1', 1), ('CmdrDisabled', 'F2', 0);
		INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, commander, raw_json, ingested_at) VALUES
		('seed-alpha', '2026-01-01T00:00:00Z', 'FSDJump', 'Alpha Centauri', 1002, 1, 2, 3, 'CmdrEnabled', '{}', '2026-01-01T00:00:00Z'),
		('seed-muted', '2026-01-01T00:01:00Z', 'FSDJump', 'Disabled Haven', 9009, 9, 9, 9, 'CmdrDisabled', '{}', '2026-01-01T00:01:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed journal events: %v", execError)
	}

	var batchCalls, singleCalls atomic.Int32
	originalBatch := testApp.resolver.JournalBatchLookup
	originalSingle := testApp.resolver.JournalLookup
	testApp.resolver.JournalBatchLookup = func(systemNames []string) (map[string]*edsm.Info, error) {
		batchCalls.Add(1)
		return originalBatch(systemNames)
	}
	testApp.resolver.JournalLookup = func(systemName string) (*edsm.Info, error) {
		singleCalls.Add(1)
		return originalSingle(systemName)
	}

	testApp.dbMu.RLock()
	seedWaypoints, hasUnresolved := testApp.buildSeedWaypoints([]string{"  alpha centauri ", "Disabled Haven", "ALPHA CENTAURI", "Unvisited Sector"})
	testApp.dbMu.RUnlock()

	if batchCalls.Load() != 1 || singleCalls.Load() != 0 {
		testingContext.Fatalf("expected 1 batch lookup and 0 single lookups, got batch=%d single=%d", batchCalls.Load(), singleCalls.Load())
	}
	if !hasUnresolved || len(seedWaypoints) != 4 || !seedWaypoints[0].HasCoords || !seedWaypoints[2].HasCoords || seedWaypoints[1].HasCoords {
		testingContext.Fatalf("unexpected seed waypoints: %+v (unresolved=%v)", seedWaypoints, hasUnresolved)
	}
}
