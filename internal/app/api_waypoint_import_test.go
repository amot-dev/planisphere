package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	createdExpedition, createError := testApp.CreateExpeditionWithWaypoints(
		"Historical Import",
		"Imported from list",
		[]string{"  sol  ", "omega sector ve-q b5-15", "   ", "SOL"},
	)
	if createError != nil {
		testingContext.Fatalf("CreateExpeditionWithWaypoints: %v", createError)
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

	validFilePath := filepath.Join(temporaryDirectory, "Xibalba.txt")
	if writeError := os.WriteFile(validFilePath, []byte("Sol\nColonia\n"), 0o600); writeError != nil {
		testingContext.Fatalf("write valid file: %v", writeError)
	}
	loadedFile, loadError := readWaypointImportFile(validFilePath)
	if loadError != nil {
		testingContext.Fatalf("readWaypointImportFile valid: %v", loadError)
	}
	if loadedFile.FileName != "Xibalba.txt" || loadedFile.Content != "Sol\nColonia\n" {
		testingContext.Fatalf("unexpected loaded file: %+v", loadedFile)
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

