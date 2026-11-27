package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/replay"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func setupTestApp(testingContext *testing.T) *App {
	testingContext.Helper()
	temporaryDirectory := testingContext.TempDir()
	databasePath := filepath.Join(temporaryDirectory, "test.sqlite")

	testApp := New()
	applicationContext, cancelFunction := context.WithCancel(context.Background())
	if initializeError := testApp.initialize(applicationContext, databasePath); initializeError != nil {
		cancelFunction()
		testingContext.Fatalf("initialize db: %v", initializeError)
	}
	testingContext.Cleanup(func() {
		cancelFunction()
		testApp.shutdown(applicationContext)
	})
	return testApp
}

func insertTestJournalEvent(testingContext *testing.T, testApp *App, timestamp, eventName, keyIdentifier, rawJSON string) {
	testingContext.Helper()
	var systemPayload struct {
		StarSystem string `json:"StarSystem"`
		SystemName string `json:"SystemName"`
	}
	_ = json.Unmarshal([]byte(rawJSON), &systemPayload)
	systemName := systemPayload.StarSystem
	if systemName == "" {
		systemName = systemPayload.SystemName
	}

	eventUID := journal.ComputeUID(timestamp, eventName, keyIdentifier)
	_, insertError := testApp.db.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, systemName, rawJSON, timestamp,
	)
	if insertError != nil {
		testingContext.Fatalf("insert %s journal event: %v", eventName, insertError)
	}
	if parsedEvent, ok := journal.ParseEventType(eventName); ok {
		testApp.publishJournalBatch(journal.NewEventSet(parsedEvent), "", "")
	}
}

func TestDatabaseHotSwapConcurrent(t *testing.T) {
	tempDir := t.TempDir()
	db1Path := filepath.Join(tempDir, "db1.sqlite")
	db2Path := filepath.Join(tempDir, "db2.sqlite")

	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := testApp.initialize(ctx, db1Path); err != nil {
		t.Fatalf("initialize db1: %v", err)
	}
	defer testApp.shutdown(ctx)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine 1: Continuous GetSettings
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.GetSettings()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Continuous GetModesState
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.GetModesState()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Continuous ListExpeditions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.ListExpeditions()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Continuous GetIngestStatus
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = testApp.GetIngestStatus()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 5: Perform hot swaps back and forth between db1 and db2
	for i := 0; i < 5; i++ {
		targetPath := db2Path
		if i%2 == 0 {
			targetPath = db1Path
		} else {
			targetPath = filepath.Join(tempDir, fmt.Sprintf("db_%d.sqlite", i))
		}
		if err := testApp.openAndWireDB(targetPath); err != nil {
			t.Errorf("hot-swap to %s failed: %v", targetPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(stop)
	wg.Wait()
}

func TestMoveRow_NonexistentRow(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	moveError := testApp.MoveRow(99999, 0)
	if moveError == nil {
		testingContext.Fatalf("expected error when moving nonexistent row, got nil")
	}
}

func TestCareerStartDoubleCheckedLocking(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	// Insert commander record with first_seen timestamp
	expectedCareerStart := "2024-03-15T12:00:00Z"
	_, insertError := testApp.db.Exec(`INSERT INTO commanders (name, enabled, first_seen) VALUES ('Commander Jameson', 1, ?)`, expectedCareerStart)
	if insertError != nil {
		testingContext.Fatalf("insert commander: %v", insertError)
	}

	expeditionRecord, createError := testApp.CreateExpedition("Career Expedition", "Testing career duration")
	if createError != nil {
		testingContext.Fatalf("failed to create expedition: %v", createError)
	}

	filterSettings := stats.Filter{Enabled: false}

	// Concurrently invoke GetSidebarStats to exercise double-checked locking
	concurrentRoutines := 10
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrentRoutines)

	for routineIndex := 0; routineIndex < concurrentRoutines; routineIndex++ {
		go func() {
			defer waitGroup.Done()
			sidebarStats, getStatsError := testApp.GetSidebarStats(expeditionRecord.ID, filterSettings)
			if getStatsError != nil {
				testingContext.Errorf("GetSidebarStats failed: %v", getStatsError)
				return
			}
			if sidebarStats.CareerStart != expectedCareerStart {
				testingContext.Errorf("expected CareerStart %s, got %s", expectedCareerStart, sidebarStats.CareerStart)
			}
			if sidebarStats.CareerStartGalactic == "" {
				testingContext.Errorf("expected non-empty CareerStartGalactic")
			}
			if sidebarStats.CareerDuration == "" {
				testingContext.Errorf("expected non-empty CareerDuration")
			}
		}()
	}
	waitGroup.Wait()

	careerStart, _ := replay.CareerStartScoped(testApp.db, replay.ProfileScope{})
	if careerStart != expectedCareerStart {
		testingContext.Errorf("expected CareerStart %s, got %s", expectedCareerStart, careerStart)
	}
}

func TestGetSidebarStatsRealizationExpeditionVsAllTime(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	// Commander Jameson
	_, _ = testApp.db.Exec(`INSERT INTO commanders (name, enabled, first_seen) VALUES ('Jameson', 1, '2024-01-01T00:00:00Z')`)

	// Scan 1: Before expedition (2025)
	insertTestJournalEvent(testingContext, testApp, "2025-01-01T10:00:00Z", "ScanOrganic", "1",
		`{"timestamp":"2025-01-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":111,"Body":1}`)
	insertTestJournalEvent(testingContext, testApp, "2025-01-01T12:00:00Z", "SellOrganicData", "1",
		`{"timestamp":"2025-01-01T12:00:00Z","event":"SellOrganicData","BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	// Create expedition (initially unstarted)
	expeditionRecord, createError := testApp.CreateExpedition("2026 Survey", "")
	if createError != nil {
		testingContext.Fatalf("create expedition: %v", createError)
	}

	unstartedSidebarStats, unstartedStatsError := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{Enabled: false})
	if unstartedStatsError != nil {
		testingContext.Fatalf("GetSidebarStats (unstarted) failed: %v", unstartedStatsError)
	}
	if unstartedSidebarStats.Realization.TotalRealized != 0 {
		testingContext.Errorf("expected unstarted expedition Realization.TotalRealized 0, got %d", unstartedSidebarStats.Realization.TotalRealized)
	}
	if unstartedSidebarStats.AllJournalsRealization.TotalRealized != 19010800 {
		testingContext.Errorf("expected unstarted AllJournalsRealization.TotalRealized 19,010,800, got %d", unstartedSidebarStats.AllJournalsRealization.TotalRealized)
	}

	// Start expedition at 2025-12-31T23:55:00Z and depart WP #1 at 2026-01-01T00:00:00Z.
	_, _ = testApp.db.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '2025-12-31T23:55:00Z' WHERE id = ?`, expeditionRecord.ID)
	_, _ = testApp.db.Exec(`UPDATE expedition_rows SET system_name = 'Sol', visited_at = '2025-12-31T23:55:00Z', departed_at = '2026-01-01T00:00:00Z' WHERE expedition_id = ?`, expeditionRecord.ID)

	// Scan 2: During expedition (2026)
	insertTestJournalEvent(testingContext, testApp, "2026-03-01T10:00:00Z", "ScanOrganic", "2",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":222,"Body":1}`)
	insertTestJournalEvent(testingContext, testApp, "2026-03-01T12:00:00Z", "SellOrganicData", "2",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	sidebarStats, statsError := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{Enabled: false})
	if statsError != nil {
		testingContext.Fatalf("GetSidebarStats failed: %v", statsError)
	}

	// Expedition realization should reflect only Scan 2 (19,010,800)
	if sidebarStats.Realization.TotalRealized != 19010800 {
		testingContext.Errorf("expected expedition Realization.TotalRealized 19,010,800, got %d", sidebarStats.Realization.TotalRealized)
	}

	// All journals realization should reflect Scan 1 + Scan 2 (38,021,600)
	if sidebarStats.AllJournalsRealization.TotalRealized != 38021600 {
		testingContext.Errorf("expected AllJournalsRealization.TotalRealized 38,021,600, got %d", sidebarStats.AllJournalsRealization.TotalRealized)
	}
}

func seedDatabaseWithExpedition(testingContext *testing.T, databasePath, expeditionName, expeditionDescription string, settingPairs ...[2]string) {
	testingContext.Helper()
	seedApp := New()
	seedApp.debugMode = false
	seedContext, cancelSeed := context.WithCancel(context.Background())
	if initError := seedApp.initialize(seedContext, databasePath); initError != nil {
		cancelSeed()
		testingContext.Fatalf("seed initialize %s: %v", databasePath, initError)
	}
	for _, settingPair := range settingPairs {
		if _, setError := seedApp.db.Exec(
			`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			settingPair[0], settingPair[1],
		); setError != nil {
			cancelSeed()
			seedApp.shutdown(seedContext)
			testingContext.Fatalf("seed setting %s: %v", settingPair[0], setError)
		}
	}
	if _, createError := seedApp.CreateExpedition(expeditionName, expeditionDescription); createError != nil {
		cancelSeed()
		seedApp.shutdown(seedContext)
		testingContext.Fatalf("seed CreateExpedition %q: %v", expeditionName, createError)
	}
	cancelSeed()
	seedApp.shutdown(seedContext)
}

func startExpeditionAtOrigin(testingContext *testing.T, targetApp *App, expeditionID int64, originSystemName string) int64 {
	testingContext.Helper()
	planView, planError := targetApp.GetPlan(expeditionID)
	if planError != nil || len(planView.Rows) == 0 {
		testingContext.Fatalf("GetPlan(%d): len=%d err=%v", expeditionID, len(planView.Rows), planError)
	}
	originRowID := planView.Rows[0].Row.ID
	if updateError := targetApp.UpdateRow(expedition.RowPatch{ID: originRowID, SystemName: &originSystemName}); updateError != nil {
		testingContext.Fatalf("UpdateRow SystemName: %v", updateError)
	}
	if startError := targetApp.StartExpedition(expeditionID, "2026-01-01T00:00:00Z", 0); startError != nil {
		testingContext.Fatalf("StartExpedition: %v", startError)
	}
	return originRowID
}

func createAndStartExpedition(testingContext *testing.T, targetApp *App, expeditionName, originSystemName string) int64 {
	testingContext.Helper()
	createdExpedition, createError := targetApp.CreateExpedition(expeditionName, "")
	if createError != nil {
		testingContext.Fatalf("CreateExpedition %q: %v", expeditionName, createError)
	}
	startExpeditionAtOrigin(testingContext, targetApp, createdExpedition.ID, originSystemName)
	return createdExpedition.ID
}

func TestDebugModeDatabaseIsolation(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	productionDBPath := filepath.Join(temporaryDirectory, "production.sqlite")
	seedDatabaseWithExpedition(testingContext, productionDBPath, "Production Expedition", "Saved in production DB")

	// Launch a debug-mode app pointing at the seeded production database path
	debugApp := New()
	debugApp.debugMode = true
	debugContext, cancelDebug := context.WithCancel(context.Background())
	defer cancelDebug()

	if initDebugError := debugApp.initialize(debugContext, productionDBPath); initDebugError != nil {
		testingContext.Fatalf("initialize debug app: %v", initDebugError)
	}

	settings, settingsError := debugApp.GetSettings()
	if settingsError != nil {
		debugApp.shutdown(debugContext)
		testingContext.Fatalf("GetSettings in debug app: %v", settingsError)
	}
	if settings.DBPath == productionDBPath {
		debugApp.shutdown(debugContext)
		testingContext.Fatalf("expected debug app DBPath to point to temporary snapshot, got %s", settings.DBPath)
	}
	if !strings.Contains(filepath.Base(settings.DBPath), "planisphere-debug-") {
		debugApp.shutdown(debugContext)
		testingContext.Fatalf("expected debug snapshot filename to contain 'planisphere-debug-', got %s", settings.DBPath)
	}
	snapshotPath := settings.DBPath

	// Verify production data is present in the debug snapshot
	expeditionsInDebug, listError := debugApp.ListExpeditions()
	if listError != nil {
		debugApp.shutdown(debugContext)
		testingContext.Fatalf("ListExpeditions in debug app: %v", listError)
	}
	foundProductionExpedition := false
	for _, expeditionItem := range expeditionsInDebug {
		if expeditionItem.Name == "Production Expedition" {
			foundProductionExpedition = true
			break
		}
	}
	if !foundProductionExpedition {
		debugApp.shutdown(debugContext)
		testingContext.Fatalf("expected 'Production Expedition' to be present in debug snapshot")
	}

	// Mutate data inside debug app and shut down
	if _, createDebugError := debugApp.CreateExpedition("Debug Scratch Expedition", "Must not persist to production DB"); createDebugError != nil {
		debugApp.shutdown(debugContext)
		testingContext.Fatalf("create expedition in debug app: %v", createDebugError)
	}
	debugApp.shutdown(debugContext)
	if _, statError := os.Stat(snapshotPath); !os.IsNotExist(statError) {
		testingContext.Errorf("expected debug snapshot %s to be removed on shutdown, stat err: %v", snapshotPath, statError)
	}

	// Reopen production database in non-debug mode and confirm scratch mutations did not persist
	verifyApp := New()
	verifyApp.debugMode = false
	verifyContext, cancelVerify := context.WithCancel(context.Background())
	defer cancelVerify()
	if initVerifyError := verifyApp.initialize(verifyContext, productionDBPath); initVerifyError != nil {
		testingContext.Fatalf("reopen production app: %v", initVerifyError)
	}
	defer verifyApp.shutdown(verifyContext)

	productionExpeditions, verifyListError := verifyApp.ListExpeditions()
	if verifyListError != nil {
		testingContext.Fatalf("ListExpeditions in reopened production app: %v", verifyListError)
	}
	for _, expeditionItem := range productionExpeditions {
		if expeditionItem.Name == "Debug Scratch Expedition" {
			testingContext.Fatalf("debug scratch expedition leaked into production database")
		}
	}
}

func TestCacheInvalidationGenerationCounters(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	expeditionID := createAndStartExpedition(testingContext, testApp, "Generation Test", "Sol")
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:05:00Z", "FSDJump", "gen-depart-sol",
		`{"timestamp":"2026-01-01T00:05:00Z","event":"FSDJump","StarSystem":"Transit","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`)

	modeFilter := stats.Filter{
		Enabled: true,
		Modes:   []stats.ModeSelection{{ModeID: 0, Travel: true, Discovery: true}},
	}
	if _, statsError := testApp.GetSidebarStats(expeditionID, modeFilter); statsError != nil {
		testingContext.Fatalf("GetSidebarStats: %v", statsError)
	}
	if _, carrierErr := testApp.carrierState.LatestStats(); carrierErr != nil {
		testingContext.Fatalf("LatestStats: %v", carrierErr)
	}

	initialTimeRangeGen := testApp.timeRangeStats.Generation()
	initialClosedRev := testApp.timeRangeStats.ClosedWindowRevision()
	initialCarrierGen := testApp.carrierState.Generation()
	initialCareerGen := testApp.careerStats.Generation()

	initialIngestStatus, ingestErr := testApp.GetIngestStatus()
	if ingestErr != nil || initialIngestStatus.ClosedWindowRevision != initialClosedRev {
		testingContext.Fatalf("expected GetIngestStatus ClosedWindowRevision %d, got %d (err=%v)",
			initialClosedRev, initialIngestStatus.ClosedWindowRevision, ingestErr)
	}

	if testApp.timeRangeStats.CachedCount() == 0 {
		testingContext.Fatal("expected timeRangeStats to cache an entry after GetSidebarStats")
	}
	if !testApp.carrierState.IsValid() || !testApp.careerStats.IsValid() {
		testingContext.Fatalf("expected single-value projections to be valid after warmup")
	}

	// AppExpeditionBoundsChanged and AppModesChanged preserve TimeRangeStats while keeping lifetime and carrier projections valid.
	testApp.publishApp(expeditionID, events.AppExpeditionBoundsChanged, events.AppModesChanged)

	if testApp.timeRangeStats.Generation() != initialTimeRangeGen {
		testingContext.Errorf("expected timeRangeStats generation %d after AppExpeditionBoundsChanged and AppModesChanged, got %d",
			initialTimeRangeGen, testApp.timeRangeStats.Generation())
	}
	if testApp.timeRangeStats.CachedCount() == 0 {
		testingContext.Errorf("expected timeRangeStats cache to remain populated after AppExpeditionBoundsChanged and AppModesChanged")
	}
	if testApp.carrierState.Generation() != initialCarrierGen || !testApp.carrierState.IsValid() {
		testingContext.Errorf("expected carrierState to remain valid across AppExpeditionBoundsChanged and AppModesChanged")
	}
	if testApp.careerStats.Generation() != initialCareerGen || !testApp.careerStats.IsValid() {
		testingContext.Errorf("expected careerStats to remain valid across AppExpeditionBoundsChanged and AppModesChanged")
	}

	// AppExpeditionModesChanged clears mode-filtered TimeRangeStats entries while preserving lifetime and carrier projections.
	testApp.publishApp(0, events.AppExpeditionModesChanged)

	if testApp.timeRangeStats.Generation() != initialTimeRangeGen+1 {
		testingContext.Errorf("expected timeRangeStats generation %d after AppExpeditionModesChanged, got %d",
			initialTimeRangeGen+1, testApp.timeRangeStats.Generation())
	}
	if testApp.timeRangeStats.CachedCount() != 0 {
		testingContext.Errorf("expected timeRangeStats cache to be empty after AppExpeditionModesChanged, got %d", testApp.timeRangeStats.CachedCount())
	}
	if testApp.carrierState.Generation() != initialCarrierGen || !testApp.carrierState.IsValid() {
		testingContext.Errorf("expected carrierState to remain valid across AppExpeditionModesChanged")
	}
	if testApp.careerStats.Generation() != initialCareerGen || !testApp.careerStats.IsValid() {
		testingContext.Errorf("expected careerStats to remain valid across AppExpeditionModesChanged")
	}

	// Re-warm timeRangeStats, then publish AppDatabaseSwitched to invalidate every projection.
	if _, statsErr := testApp.GetSidebarStats(expeditionID, stats.Filter{}); statsErr != nil {
		testingContext.Fatalf("second GetSidebarStats: %v", statsErr)
	}
	testApp.publishApp(0, events.AppDatabaseSwitched)

	if testApp.carrierState.Generation() != initialCarrierGen+1 || testApp.carrierState.IsValid() {
		testingContext.Errorf("expected carrierState to invalidate on AppDatabaseSwitched")
	}
	if testApp.careerStats.Generation() != initialCareerGen+1 || testApp.careerStats.IsValid() {
		testingContext.Errorf("expected careerStats to invalidate on AppDatabaseSwitched")
	}
	if testApp.timeRangeStats.Generation() != initialTimeRangeGen+2 || testApp.timeRangeStats.CachedCount() != 0 {
		testingContext.Errorf("expected timeRangeStats generation %d and empty cache after AppDatabaseSwitched, got gen=%d count=%d",
			initialTimeRangeGen+2, testApp.timeRangeStats.Generation(), testApp.timeRangeStats.CachedCount())
	}
	if switchedStatus, switchedErr := testApp.GetIngestStatus(); switchedErr != nil || switchedStatus.ClosedWindowRevision != initialClosedRev+1 {
		testingContext.Errorf("expected ClosedWindowRevision %d after AppDatabaseSwitched, got %d (err=%v)",
			initialClosedRev+1, switchedStatus.ClosedWindowRevision, switchedErr)
	}
}

func TestStartupHookWaitsForDatabaseInitialization(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	productionDBPath := filepath.Join(temporaryDirectory, "startup-race.sqlite")

	// Seed a database with an expedition so callers receive real state
	seedDatabaseWithExpedition(testingContext, productionDBPath, "Startup Race Expedition", "Verify concurrent RPCs wait for OnStartup")

	testingContext.Setenv("ED_PLANNER_DB", productionDBPath)

	debugApp := New()
	debugApp.debugMode = true
	onStartup := StartupHook(debugApp)
	onShutdown := ShutdownHook(debugApp)

	appContext, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	defer onShutdown(appContext)

	// Launch frontend RPC callers BEFORE OnStartup even begins, simulating Wails
	// spawning OnStartup in a goroutine while the WebView immediately requests core state.
	var waitGroup sync.WaitGroup
	waitGroup.Add(4)

	go func() {
		defer waitGroup.Done()
		settings, err := debugApp.GetSettings()
		if err != nil {
			testingContext.Errorf("GetSettings during startup returned error: %v", err)
			return
		}
		if settings.DBPath == "" {
			testingContext.Errorf("expected non-empty DBPath from GetSettings after startup")
		}
	}()

	go func() {
		defer waitGroup.Done()
		modesState, err := debugApp.GetModesState()
		if err != nil {
			testingContext.Errorf("GetModesState during startup returned error: %v", err)
			return
		}
		if len(modesState.Modes) == 0 || modesState.DefaultModeID <= 0 {
			testingContext.Errorf("expected seeded modes and positive DefaultModeID from GetModesState after startup, got %+v", modesState)
		}
	}()

	go func() {
		defer waitGroup.Done()
		expeditionsList, err := debugApp.ListExpeditions()
		if err != nil {
			testingContext.Errorf("ListExpeditions during startup returned error: %v", err)
			return
		}
		if len(expeditionsList) == 0 {
			testingContext.Errorf("expected expeditions from ListExpeditions after startup")
		}
	}()

	go func() {
		defer waitGroup.Done()
		ingestStatus, err := debugApp.GetIngestStatus()
		if err != nil {
			testingContext.Errorf("GetIngestStatus during startup returned error: %v", err)
			return
		}
		if ingestStatus.LastError != "" {
			testingContext.Errorf("expected empty LastError from GetIngestStatus, got %q", ingestStatus.LastError)
		}
	}()

	// Delay startup slightly so RPCs are guaranteed to hit ready() while app.db == nil
	time.Sleep(15 * time.Millisecond)
	debugApp.emit = func(string, ...any) {}
	if initError := debugApp.initialize(appContext, os.Getenv("ED_PLANNER_DB")); initError != nil {
		debugApp.setError(initError.Error())
		debugApp.markStartupComplete()
		testingContext.Fatalf("debugApp.initialize failed: %v", initError)
	}
	debugApp.markStartupComplete()
	_ = onStartup

	waitGroup.Wait()
}

func TestJournalDebounceSettingPersistenceAndReload(t *testing.T) {
	testApp := setupTestApp(t)

	initialSettings, getSettingsError := testApp.GetSettings()
	if getSettingsError != nil {
		t.Fatalf("GetSettings: %v", getSettingsError)
	}
	if initialSettings.JournalDebounceMs != 2000 || initialSettings.DefaultJournalDebounceMs != 2000 {
		t.Fatalf("expected default 2000ms debounce, got current=%d default=%d", initialSettings.JournalDebounceMs, initialSettings.DefaultJournalDebounceMs)
	}

	updatedSettings, setError := testApp.SetJournalDebounceMs(5000)
	if setError != nil {
		t.Fatalf("SetJournalDebounceMs(5000): %v", setError)
	}
	if updatedSettings.JournalDebounceMs != 5000 {
		t.Fatalf("expected 5000ms debounce, got %d", updatedSettings.JournalDebounceMs)
	}
	if testApp.watcher.DebounceDelay() != 5*time.Second {
		t.Fatalf("expected live watcher delay 5s, got %v", testApp.watcher.DebounceDelay())
	}

	lowClampedSettings, lowClampError := testApp.SetJournalDebounceMs(50)
	if lowClampError != nil {
		t.Fatalf("SetJournalDebounceMs(50): %v", lowClampError)
	}
	if lowClampedSettings.JournalDebounceMs != 300 {
		t.Fatalf("expected lower clamp 300ms, got %d", lowClampedSettings.JournalDebounceMs)
	}

	highClampedSettings, highClampError := testApp.SetJournalDebounceMs(120000)
	if highClampError != nil {
		t.Fatalf("SetJournalDebounceMs(120000): %v", highClampError)
	}
	if highClampedSettings.JournalDebounceMs != 30000 {
		t.Fatalf("expected upper clamp 30000ms, got %d", highClampedSettings.JournalDebounceMs)
	}

	if reloadError := testApp.openAndWireDB(testApp.dbPath); reloadError != nil {
		t.Fatalf("openAndWireDB reload: %v", reloadError)
	}
	if testApp.watcher.DebounceDelay() != 30*time.Second {
		t.Fatalf("expected reloaded watcher to restore persisted 30s debounce, got %v", testApp.watcher.DebounceDelay())
	}
}

func TestStartupDoesNotEmitRedundantPostBootSettingsChange(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	seededDatabasePath := filepath.Join(temporaryDirectory, "startup-events.sqlite")
	emptyJournalDirectory := filepath.Join(temporaryDirectory, "journals")
	if mkdirError := os.MkdirAll(emptyJournalDirectory, 0o755); mkdirError != nil {
		testingContext.Fatalf("create journal dir: %v", mkdirError)
	}

	seedDatabaseWithExpedition(
		testingContext,
		seededDatabasePath,
		"Boot Sync Survey",
		"",
		[2]string{"setup_completed", "1"},
		[2]string{"online_lookups", "0"},
		[2]string{"journal_dir", emptyJournalDirectory},
	)

	testingContext.Setenv("ED_PLANNER_DB", seededDatabasePath)

	originalFetchLatestGitHubVersion := fetchLatestGitHubVersion
	fetchLatestGitHubVersion = func(context.Context) (string, error) {
		return "", nil
	}
	testingContext.Cleanup(func() {
		fetchLatestGitHubVersion = originalFetchLatestGitHubVersion
	})

	startupApp := New()
	startupApp.debugMode = false

	var emittedMutex sync.Mutex
	var emittedChanges []DataChanged
	startupApp.emit = func(eventName string, payload ...any) {
		if eventName != EventDataChanged || len(payload) == 0 {
			return
		}
		if dataChangedPayload, ok := payload[0].(DataChanged); ok {
			emittedMutex.Lock()
			emittedChanges = append(emittedChanges, dataChangedPayload)
			emittedMutex.Unlock()
		}
	}

	onStartup := StartupHook(startupApp)
	onShutdown := ShutdownHook(startupApp)
	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()
	defer onShutdown(applicationContext)

	onStartup(applicationContext)

	loadedSettings, settingsError := startupApp.GetSettings()
	if settingsError != nil {
		testingContext.Fatalf("GetSettings after startup: %v", settingsError)
	}
	if !loadedSettings.SetupCompleted {
		testingContext.Fatalf("expected SetupCompleted to be true after startup")
	}

	emittedMutex.Lock()
	defer emittedMutex.Unlock()
	for _, changeEvent := range emittedChanges {
		if slices.Contains(changeEvent.Domains, "settings") {
			testingContext.Errorf("unexpected startup %q event emitted with Domains=%v", EventDataChanged, changeEvent.Domains)
		}
	}
}

func TestShutdownWaitsForBackgroundWorkersBeforeClosingDatabase(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	productionDBPath := filepath.Join(temporaryDirectory, "shutdown-sync.sqlite")
	seedDatabaseWithExpedition(testingContext, productionDBPath, "Shutdown Sync Expedition", "")

	debugApp := New()
	debugApp.debugMode = true
	applicationContext, cancelApplication := context.WithCancel(context.Background())
	defer cancelApplication()
	if initError := debugApp.initialize(applicationContext, productionDBPath); initError != nil {
		testingContext.Fatalf("debug initialize: %v", initError)
	}

	loadedSettings, settingsError := debugApp.GetSettings()
	if settingsError != nil {
		debugApp.shutdown(applicationContext)
		testingContext.Fatalf("GetSettings: %v", settingsError)
	}
	snapshotPath := loadedSettings.DBPath

	workerStarted := make(chan struct{})
	allowWorkerFinish := make(chan struct{})
	shutdownDone := make(chan struct{})
	workerQueryError := make(chan error, 1)

	testingContext.Cleanup(func() {
		select {
		case <-allowWorkerFinish:
		default:
			close(allowWorkerFinish)
		}
		select {
		case <-shutdownDone:
		default:
			debugApp.shutdown(applicationContext)
		}
	})

	spawned := debugApp.spawnBackground(func() {
		close(workerStarted)
		<-debugApp.context().Done()

		debugApp.dbMu.RLock()
		var workerError error
		if debugApp.db == nil {
			workerError = fmt.Errorf("database was closed before background worker finished")
		} else {
			var expeditionCount int
			if queryError := debugApp.db.QueryRow(`SELECT COUNT(*) FROM expeditions`).Scan(&expeditionCount); queryError != nil {
				workerError = fmt.Errorf("query expeditions during shutdown: %w", queryError)
			} else if _, statError := os.Stat(snapshotPath); statError != nil {
				workerError = fmt.Errorf("expected debug snapshot to remain intact during shutdown: %w", statError)
			}
		}
		debugApp.dbMu.RUnlock()

		workerQueryError <- workerError
		<-allowWorkerFinish
	})
	if !spawned {
		testingContext.Fatalf("expected spawnBackground to launch worker before shutdown")
	}

	<-workerStarted
	go func() {
		debugApp.shutdown(applicationContext)
		close(shutdownDone)
	}()

	select {
	case workerError := <-workerQueryError:
		if workerError != nil {
			testingContext.Fatalf("background worker observed premature cleanup: %v", workerError)
		}
	case <-time.After(2 * time.Second):
		testingContext.Fatal("timed out waiting for background worker to query database after context cancellation")
	}

	select {
	case <-shutdownDone:
		testingContext.Fatal("shutdown returned before background worker completed")
	default:
	}

	if debugApp.spawnBackground(func() {}) {
		testingContext.Fatal("expected spawnBackground to reject new tasks once shutdown has begun")
	}

	close(allowWorkerFinish)
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		testingContext.Fatal("timed out waiting for shutdown to complete after background worker exited")
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statError := os.Stat(snapshotPath + suffix); !os.IsNotExist(statError) {
			testingContext.Errorf("expected debug snapshot %s%s to be removed after shutdown, stat err: %v", snapshotPath, suffix, statError)
		}
	}
}

func TestExternalLinksSettings(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	initialSettings, initialError := testApp.GetSettings()
	if initialError != nil {
		testingContext.Fatalf("GetSettings initial: %v", initialError)
	}
	if initialSettings.ExternalLinkInara != settings.ExternalLinkOn ||
		initialSettings.ExternalLinkEDSM != settings.ExternalLinkIfNoGEC ||
		initialSettings.ExternalLinkGEC != settings.ExternalLinkOn {
		testingContext.Fatalf(
			"unexpected default external link settings: inara=%q edsm=%q gec=%q",
			initialSettings.ExternalLinkInara,
			initialSettings.ExternalLinkEDSM,
			initialSettings.ExternalLinkGEC,
		)
	}

	updatedSettings, updateError := testApp.SetExternalLinks("off", "on", "off")
	if updateError != nil {
		testingContext.Fatalf("SetExternalLinks: %v", updateError)
	}
	if updatedSettings.ExternalLinkInara != settings.ExternalLinkOff ||
		updatedSettings.ExternalLinkEDSM != settings.ExternalLinkOn ||
		updatedSettings.ExternalLinkGEC != settings.ExternalLinkOff {
		testingContext.Fatalf(
			"unexpected persisted external link settings: inara=%q edsm=%q gec=%q",
			updatedSettings.ExternalLinkInara,
			updatedSettings.ExternalLinkEDSM,
			updatedSettings.ExternalLinkGEC,
		)
	}

	normalizedSettings, normalizeError := testApp.SetExternalLinks("invalid", "unknown", "OFF")
	if normalizeError != nil {
		testingContext.Fatalf("SetExternalLinks normalize: %v", normalizeError)
	}
	if normalizedSettings.ExternalLinkInara != settings.ExternalLinkOn ||
		normalizedSettings.ExternalLinkEDSM != settings.ExternalLinkIfNoGEC ||
		normalizedSettings.ExternalLinkGEC != settings.ExternalLinkOff {
		testingContext.Fatalf(
			"unexpected normalized external link settings: inara=%q edsm=%q gec=%q",
			normalizedSettings.ExternalLinkInara,
			normalizedSettings.ExternalLinkEDSM,
			normalizedSettings.ExternalLinkGEC,
		)
	}
}

func TestDeleteActiveExpeditionSelectsSurvivingAndEmitsTargetedDomains(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	initialExpeditions, listError := testApp.ListExpeditions()
	if listError != nil || len(initialExpeditions) != 1 {
		testingContext.Fatalf("expected 1 seeded expedition, got len=%d err=%v", len(initialExpeditions), listError)
	}
	survivingExpedition := initialExpeditions[0]

	activeExpedition, createSecondError := testApp.CreateExpedition("Active Survey", "")
	if createSecondError != nil {
		testingContext.Fatalf("create active expedition: %v", createSecondError)
	}

	var emittedMutex sync.Mutex
	var emittedEvents []DataChanged
	testApp.emit = func(eventName string, payload ...any) {
		if eventName != EventDataChanged || len(payload) == 0 {
			return
		}
		if dataChangedEvent, ok := payload[0].(DataChanged); ok {
			emittedMutex.Lock()
			emittedEvents = append(emittedEvents, dataChangedEvent)
			emittedMutex.Unlock()
		}
	}

	assertDeleteOutcome := func(expectedActiveID int64) {
		testingContext.Helper()
		loadedSettings, settingsError := testApp.GetSettings()
		if settingsError != nil {
			testingContext.Fatalf("GetSettings: %v", settingsError)
		}
		if loadedSettings.ActiveExpeditionID != expectedActiveID {
			testingContext.Fatalf("expected active_expedition_id %d, got %d", expectedActiveID, loadedSettings.ActiveExpeditionID)
		}

		emittedMutex.Lock()
		capturedEvents := append([]DataChanged(nil), emittedEvents...)
		emittedEvents = nil
		emittedMutex.Unlock()

		if len(capturedEvents) != 1 {
			testingContext.Fatalf("expected 1 DataChanged event, got %d (%+v)", len(capturedEvents), capturedEvents)
		}
		changeEvent := capturedEvents[0]
		if changeEvent.ExpeditionID != 0 {
			testingContext.Fatalf("expected expeditionId 0, got %+v", changeEvent)
		}
		if !slices.Equal(changeEvent.Domains, []string{"settings", "expeditions"}) {
			testingContext.Fatalf("expected domains [settings expeditions], got %v", changeEvent.Domains)
		}
	}

	if deleteError := testApp.DeleteExpedition(activeExpedition.ID); deleteError != nil {
		testingContext.Fatalf("DeleteExpedition(active): %v", deleteError)
	}
	assertDeleteOutcome(survivingExpedition.ID)

	if deleteLastError := testApp.DeleteExpedition(survivingExpedition.ID); deleteLastError != nil {
		testingContext.Fatalf("DeleteExpedition(last): %v", deleteLastError)
	}
	assertDeleteOutcome(0)
}
