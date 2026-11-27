package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
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

func TestExpeditionResolutionWorkerBatchOfflineAndCoalescedOnlineEmissions(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if setOnlineErr := settings.SetOnlineLookups(testApp.db, true); setOnlineErr != nil {
		testingContext.Fatalf("enable online lookups: %v", setOnlineErr)
	}
	if setGECFetchErr := settings.SetGECFetchedAt(testApp.db, reference.NowISO()); setGECFetchErr != nil {
		testingContext.Fatalf("set GEC timestamp: %v", setGECFetchErr)
	}

	if _, execError := testApp.db.Exec(`
		INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, raw_json, ingested_at)
		VALUES ('res-sol', '2026-01-01T00:00:00Z', 'FSDJump', 'Sol', 10477373803, 0, 0, 0, '{}', '2026-01-01T00:00:00Z');
		INSERT INTO system_cache (name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at)
		VALUES ('colonia', 'Colonia', 3238296097059, 43805, -9530.5, -910.28125, 19808.125, 'edsm', '2026-01-01T00:00:00Z');
		INSERT INTO gec_pois_cache (id, name, system_name, system_address, poi_type, region, summary, poi_url, raw_json, source, fetched_at) VALUES
		(101, 'Jaques Station', 'Colonia', 3238296097059, 'Historical', 'Colonia', '', 'https://edastro.com/gec/view/101', '{}', 'GEC', '2026-01-01T00:00:00Z'),
		(102, 'Distant View in Blue', 'Beagle Point', 827689533218, 'Vista', 'The Abyss', '', 'https://edastro.com/gec/view/102', '{}', 'GEC', '2026-01-01T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed resolution fixtures: %v", execError)
	}
	if reloadError := testApp.gec.Reload(); reloadError != nil {
		testingContext.Fatalf("reload GEC: %v", reloadError)
	}

	createdExpedition, createError := expedition.CreateWithWaypoints(testApp.db, "Coalesced Worker Route", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Colonia"},
		{SystemName: "Barnards Loop"},
		{SystemName: "Sgr A Star"},
		{SystemName: "Beagle Point"},
	})
	if createError != nil {
		testingContext.Fatalf("create seeded expedition: %v", createError)
	}

	var emittedMutex sync.Mutex
	var planEmitCount int
	testApp.emit = func(eventName string, payload ...any) {
		if eventName == EventDataChanged && len(payload) > 0 {
			if dataChanged, ok := payload[0].(DataChanged); ok && slices.Contains(dataChanged.Domains, "plan") {
				emittedMutex.Lock()
				planEmitCount++
				emittedMutex.Unlock()
			}
		}
	}

	var onlineRequests atomic.Int32
	offlineVerifiedBeforeFirstHTTP := atomic.Bool{}
	mockServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestNumber := onlineRequests.Add(1)
		if requestNumber == 1 {
			rowsBeforeOnline, _ := expedition.ListRows(testApp.db, createdExpedition.ID)
			emittedMutex.Lock()
			emitsSoFar := planEmitCount
			emittedMutex.Unlock()
			if len(rowsBeforeOnline) == 5 && rowsBeforeOnline[0].HasCoords && rowsBeforeOnline[1].HasCoords && rowsBeforeOnline[1].POIName == "Jaques Station" && emitsSoFar == 1 {
				offlineVerifiedBeforeFirstHTTP.Store(true)
			}
		}
		if requestNumber == 3 {
			time.Sleep(150 * time.Millisecond)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Query().Get("systemName") {
		case "Barnards Loop":
			_, _ = writer.Write([]byte(`{"name":"Barnards Loop","id":10,"id64":5001,"coords":{"x":100,"y":-200,"z":-1300}}`))
		case "Beagle Point":
			_, _ = writer.Write([]byte(`{"name":"Beagle Point","id":20,"id64":827689533218,"coords":{"x":-1111.5,"y":-134.2,"z":65269.75}}`))
		default:
			_, _ = writer.Write([]byte(`[]`))
		}
	}))
	testingContext.Cleanup(mockServer.Close)
	testApp.resolver.EDSMBase = mockServer.URL
	testApp.resolver.HTTP = mockServer.Client()

	testApp.runExpeditionResolutionWorker(testApp.db, createdExpedition.ID, false)

	if !offlineVerifiedBeforeFirstHTTP.Load() {
		testingContext.Fatalf("expected offline batch resolution and single emit to finish before first online EDSM request")
	}
	if onlineRequests.Load() != 3 {
		testingContext.Fatalf("expected 3 online EDSM requests, got %d", onlineRequests.Load())
	}
	emittedMutex.Lock()
	totalPlanEmits := planEmitCount
	planEmitCount = 0
	emittedMutex.Unlock()
	if totalPlanEmits != 2 {
		testingContext.Fatalf("expected 2 coalesced plan emissions (offline pass + online completion), got %d", totalPlanEmits)
	}

	// When OnlineLookups is disabled, unresolved waypoints commit notfound + GEC POI in one batch with 0 online calls and 1 emit.
	if setOfflineErr := settings.SetOnlineLookups(testApp.db, false); setOfflineErr != nil {
		testingContext.Fatalf("disable online lookups: %v", setOfflineErr)
	}
	offlineExpedition, offlineCreateErr := expedition.CreateWithWaypoints(testApp.db, "Offline Batch Route", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Uncached Frontier"},
	})
	if offlineCreateErr != nil {
		testingContext.Fatalf("create offline expedition: %v", offlineCreateErr)
	}
	onlineRequests.Store(0)
	testApp.runExpeditionResolutionWorker(testApp.db, offlineExpedition.ID, false)
	offlineRows, _ := expedition.ListRows(testApp.db, offlineExpedition.ID)
	emittedMutex.Lock()
	offlinePlanEmits := planEmitCount
	emittedMutex.Unlock()
	if onlineRequests.Load() != 0 || offlinePlanEmits != 1 || !offlineRows[0].HasCoords || offlineRows[1].CoordsSource != "notfound" {
		testingContext.Fatalf("expected single transactional offline batch (0 HTTP, 1 emit), got http=%d emits=%d rows=%+v", onlineRequests.Load(), offlinePlanEmits, offlineRows)
	}

	// Verify transactional populateMissingPOIs backfills blank POIs and guards system_name against TOCTOU edits.
	if _, execError := testApp.db.Exec(`
		UPDATE expedition_rows SET poi_name = '' WHERE expedition_id = ? AND system_name = 'Colonia';
		UPDATE expedition_rows SET poi_name = 'Custom Beagle Label' WHERE expedition_id = ? AND system_name = 'Beagle Point'
	`, createdExpedition.ID, createdExpedition.ID); execError != nil {
		testingContext.Fatalf("prepare missing POI rows: %v", execError)
	}
	testApp.populateMissingPOIs()
	updatedRows, _ := expedition.ListRows(testApp.db, createdExpedition.ID)
	if updatedRows[1].POIName != "Jaques Station" || updatedRows[4].POIName != "Custom Beagle Label" {
		testingContext.Fatalf("unexpected POI backfill: colonia=%q beagle=%q", updatedRows[1].POIName, updatedRows[4].POIName)
	}
	if staleUpdateErr := expedition.SetMissingRowPOIsTx(testApp.db, []expedition.RowPOIUpdate{
		{RowID: updatedRows[3].ID, SystemName: "Stale System Name", POIName: "Should Not Apply"},
	}); staleUpdateErr != nil {
		testingContext.Fatalf("SetMissingRowPOIsTx stale system guard: %v", staleUpdateErr)
	}
	guardedRow, _ := expedition.GetRow(testApp.db, updatedRows[3].ID)
	if guardedRow.POIName != "" {
		testingContext.Fatalf("expected mismatched system_name POI update to be skipped, got %q", guardedRow.POIName)
	}
}

func TestSetupConfigureJournalsAvoidsDuplicateEmissionsAndPreservesPostSetupSync(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDirectory := testingContext.TempDir()
	journalPath := filepath.Join(journalDirectory, "Journal.2026-02-01T000000.01.log")
	journalLine := `{"timestamp":"2026-02-01T00:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}` + "\n"
	if writeError := os.WriteFile(journalPath, []byte(journalLine), 0o600); writeError != nil {
		testingContext.Fatalf("write setup journal: %v", writeError)
	}

	var emittedMutex sync.Mutex
	var emittedEventNames []string
	var emittedDomains [][]string
	testApp.emit = func(eventName string, payload ...any) {
		emittedMutex.Lock()
		defer emittedMutex.Unlock()
		emittedEventNames = append(emittedEventNames, eventName)
		if eventName == EventDataChanged && len(payload) > 0 {
			if dataChanged, ok := payload[0].(DataChanged); ok {
				emittedDomains = append(emittedDomains, dataChanged.Domains)
			}
		}
	}

	initialStatus, setupError := testApp.SetupConfigureJournals(journalDirectory)
	if setupError != nil || initialStatus.EventsStored != 1 {
		testingContext.Fatalf("SetupConfigureJournals initial: status=%+v err=%v", initialStatus, setupError)
	}
	watcherStarted := false
	for attempt := 0; attempt < 50; attempt++ {
		if status, _ := testApp.GetIngestStatus(); status.WatcherActive {
			watcherStarted = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !watcherStarted {
		testingContext.Fatalf("expected live journal watcher to become active after SetupConfigureJournals")
	}
	emittedMutex.Lock()
	if len(emittedEventNames) != 0 {
		testingContext.Fatalf("expected 0 redundant emissions during pre-completion setup scan, got %v", emittedEventNames)
	}
	emittedMutex.Unlock()

	if setCompleteErr := settings.SetSetupCompleted(testApp.db, true); setCompleteErr != nil {
		testingContext.Fatalf("mark setup completed: %v", setCompleteErr)
	}
	if _, postSetupErr := testApp.SetupConfigureJournals(journalDirectory); postSetupErr != nil {
		testingContext.Fatalf("SetupConfigureJournals post-setup: %v", postSetupErr)
	}
	emittedMutex.Lock()
	defer emittedMutex.Unlock()
	if !slices.Equal(emittedEventNames, []string{EventDataChanged}) || len(emittedDomains) != 1 ||
		!slices.Contains(emittedDomains[0], "ingest") || !slices.Contains(emittedDomains[0], "settings") {
		testingContext.Fatalf("expected 1 post-setup DataChanged event with ingest and settings, got events=%v domains=%v", emittedEventNames, emittedDomains)
	}
}

func TestConcurrentAppContextAndRPCsDuringInitialize(testingContext *testing.T) {
	// Verify typed-nil (*sql.DB)(nil) safety on system query and canonical name helpers,
	// and on PrepareCarrierRoutePrompt when startup completed with app.db == nil.
	var typedNilDB *sql.DB
	if coords, coordsErr := replay.QueryJournalSystemCoords(typedNilDB, "Sol"); coordsErr != nil || coords != nil {
		testingContext.Fatalf("expected nil result for typed-nil *sql.DB in QueryJournalSystemCoords, got %+v err=%v", coords, coordsErr)
	}
	if gotCanonical := edsm.CanonicalSystemName(typedNilDB, "  Sol ", nil); gotCanonical != "Sol" {
		testingContext.Fatalf("expected trimmed fallback for typed-nil *sql.DB in CanonicalSystemName, got %q", gotCanonical)
	}
	uninitializedApp := New()
	if _, uninitErr := uninitializedApp.PrepareCarrierRoutePrompt(1, 0); !errors.Is(uninitErr, errorNotReady) {
		testingContext.Fatalf("expected errorNotReady on nil-db PrepareCarrierRoutePrompt, got %v", uninitErr)
	}

	temporaryDirectory := testingContext.TempDir()
	databasePath := filepath.Join(temporaryDirectory, "ctx-race.sqlite")
	seedDatabaseWithExpedition(testingContext, databasePath, "Concurrent Init Route", "")

	raceApp := New()
	raceApp.debugMode = false
	_ = StartupHook(raceApp)
	onShutdown := ShutdownHook(raceApp)

	appContext, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	defer onShutdown(appContext)

	var workers sync.WaitGroup
	stopReaders := make(chan struct{})
	for workerIndex := 0; workerIndex < 4; workerIndex++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				_ = raceApp.context()
				switch index % 4 {
				case 0:
					_, _ = raceApp.readyContext()
				case 1:
					_, _ = raceApp.GetSettings()
				case 2:
					_, _ = raceApp.PrepareCarrierRoutePrompt(1, 0)
				case 3:
					_ = raceApp.OpenURL("ftp://invalid-scheme")
				}
				time.Sleep(time.Millisecond)
			}
		}(workerIndex)
	}

	time.Sleep(10 * time.Millisecond)
	if initError := raceApp.initialize(appContext, databasePath); initError != nil {
		close(stopReaders)
		workers.Wait()
		testingContext.Fatalf("initialize: %v", initError)
	}
	raceApp.markStartupComplete()

	readyCtx, readyErr := raceApp.readyContext()
	if readyErr != nil || readyCtx == nil {
		close(stopReaders)
		workers.Wait()
		testingContext.Fatalf("expected non-nil readyContext after initialize, got ctx=%v err=%v", readyCtx, readyErr)
	}
	close(stopReaders)
	workers.Wait()
}

func TestOnlineResolutionAbortsAcrossDatabaseSwitch(testingContext *testing.T) {
	tempDir := testingContext.TempDir()
	testingContext.Setenv("XDG_CONFIG_HOME", tempDir)
	testingContext.Setenv("APPDATA", tempDir)
	testApp := setupTestApp(testingContext)
	enteredHTTP, releaseHTTP, httpCalls := make(chan struct{}), make(chan struct{}), atomic.Int32{}
	mockServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if httpCalls.Add(1) == 1 {
			close(enteredHTTP)
			<-releaseHTTP
		}
		if _, writeErr := writer.Write([]byte(`{"name":"Deep One","id":1,"id64":9001,"coords":{"x":1,"y":2,"z":3}}`)); writeErr != nil {
			testingContext.Errorf("write mock EDSM: %v", writeErr)
		}
	}))
	testingContext.Cleanup(mockServer.Close)
	testApp.resolver.EDSMBase, testApp.resolver.HTTP = mockServer.URL, mockServer.Client()
	oldExp, oldErr := expedition.CreateWithWaypoints(testApp.db, "Old", "", []expedition.SeedWaypoint{{SystemName: "Deep One"}, {SystemName: "Deep Two"}})
	if onlineErr := settings.SetOnlineLookups(testApp.db, true); onlineErr != nil || oldErr != nil {
		testingContext.Fatalf("prepare old expedition: onlineErr=%v oldErr=%v", onlineErr, oldErr)
	}
	testApp.kickResolution(oldExp.ID, false)
	<-enteredHTTP
	_, switchErr := testApp.SetDatabasePath(filepath.Join(tempDir, "switched.sqlite"))
	newExp, newErr := expedition.CreateWithWaypoints(testApp.db, "New", "", []expedition.SeedWaypoint{{SystemName: "Deep One"}})
	close(releaseHTTP)
	testApp.backgroundWg.Wait()
	newRows, listErr := expedition.ListRows(testApp.db, newExp.ID)
	if switchErr != nil || newErr != nil || listErr != nil || httpCalls.Load() != 1 || len(newRows) != 1 || newRows[0].CoordsSource != "" || newRows[0].HasCoords {
		testingContext.Fatalf("expected switched DB untouched and 1 HTTP call, got switchErr=%v newErr=%v listErr=%v calls=%d rows=%+v", switchErr, newErr, listErr, httpCalls.Load(), newRows)
	}
}

func TestAsyncResolutionVisitedAddressMatchEmitsStatsDomain(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if _, execErr := testApp.db.Exec(`INSERT INTO journal_events (event_uid,timestamp,event,system_name,system_address,star_x,star_y,star_z,raw_json,ingested_at) VALUES ('addr-sol','2026-03-01T10:00:00Z','Location','Sol',10477373803,0,0,0,'{}','2026-03-01T10:00:00Z'),('addr-col','2026-03-01T11:00:00Z','FSDJump','Colonia',3238296097059,-9530.5,-910.28125,19808.125,'{}','2026-03-01T11:00:00Z'); INSERT INTO system_cache (name_key,system_name,system_address,edsm_id,x,y,z,source,fetched_at) VALUES ('eol prou rs-t d3-94','Eol Prou RS-T d3-94',3238296097059,43805,-9530.5,-910.28125,19808.125,'edsm','2026-03-01T10:00:00Z')`); execErr != nil {
		testingContext.Fatalf("seed resolution fixtures: %v", execErr)
	}
	createdExp, createErr := testApp.createExpeditionWithWaypoints("Address Visit", "", []string{"Sol"})
	addedRow, addErr := testApp.AddRow(createdExp.ID, 0, expedition.RowWaypoint, 0)
	if createErr != nil || addErr != nil || testApp.StartExpedition(createdExp.ID, "2026-03-01T10:00:00Z", 0) != nil {
		testingContext.Fatalf("prepare active expedition: createErr=%v addErr=%v", createErr, addErr)
	}
	aliasName, sawPlanAndStats := "Eol Prou RS-T d3-94", atomic.Bool{}
	testApp.emit = func(eventName string, payload ...any) {
		if eventName == EventDataChanged && len(payload) > 0 {
			if changed, ok := payload[0].(DataChanged); ok && slices.Contains(changed.Domains, "plan") && slices.Contains(changed.Domains, "stats") {
				sawPlanAndStats.Store(true)
			}
		}
	}
	updateErr := testApp.UpdateRow(expedition.RowPatch{ID: addedRow.ID, SystemName: &aliasName})
	testApp.backgroundWg.Wait()
	resolvedRow, rowErr := expedition.GetRow(testApp.db, addedRow.ID)
	if updateErr != nil || rowErr != nil || resolvedRow.VisitedAt != "2026-03-01T11:00:00Z" || !sawPlanAndStats.Load() {
		testingContext.Fatalf("expected visited match and plan+stats emit, got updateErr=%v rowErr=%v row=%+v sawBoth=%v", updateErr, rowErr, resolvedRow, sawPlanAndStats.Load())
	}
}
