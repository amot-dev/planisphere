package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"planisphere/internal/core/events"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/settings"
)

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
		}
		if err := testApp.openAndWireDB(targetPath); err != nil {
			t.Errorf("hot-swap to %s failed: %v", targetPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(stop)
	wg.Wait()
}

func TestGetSidebarStatsRealizationExpeditionVsAllTime(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	// Commander Jameson
	if _, insertCommanderError := testApp.db.Exec(`INSERT INTO commanders (name, enabled, first_seen) VALUES ('Jameson', 1, '2024-01-01T00:00:00Z')`); insertCommanderError != nil {
		testingContext.Fatalf("insert commander: %v", insertCommanderError)
	}

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
	if _, updateExpeditionError := testApp.db.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '2025-12-31T23:55:00Z' WHERE id = ?`, expeditionRecord.ID); updateExpeditionError != nil {
		testingContext.Fatalf("update expedition status: %v", updateExpeditionError)
	}
	if _, updateRowError := testApp.db.Exec(`UPDATE expedition_rows SET system_name = 'Sol', visited_at = '2025-12-31T23:55:00Z', departed_at = '2026-01-01T00:00:00Z' WHERE expedition_id = ?`, expeditionRecord.ID); updateRowError != nil {
		testingContext.Fatalf("update expedition origin row: %v", updateRowError)
	}

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

	if _, statsError := testApp.GetSidebarStats(expeditionID, stats.Filter{}); statsError != nil {
		testingContext.Fatalf("GetSidebarStats: %v", statsError)
	}
	if _, carrierErr := testApp.carrierState.LatestStats(); carrierErr != nil {
		testingContext.Fatalf("LatestStats: %v", carrierErr)
	}

	initialClosedRev := testApp.timeRangeStats.ClosedWindowRevision()
	initialIngestStatus, ingestErr := testApp.GetIngestStatus()
	if ingestErr != nil || initialIngestStatus.ClosedWindowRevision != initialClosedRev {
		testingContext.Fatalf("expected GetIngestStatus ClosedWindowRevision %d, got %d (err=%v)",
			initialClosedRev, initialIngestStatus.ClosedWindowRevision, ingestErr)
	}

	// Non-switching app events preserve ClosedWindowRevision while AppDatabaseSwitched invalidates app-wired projections and increments ClosedWindowRevision.
	testApp.publishApp(expeditionID, events.AppExpeditionBoundsChanged, events.AppModesChanged, events.AppExpeditionModesChanged)
	if unchangedStatus, statusErr := testApp.GetIngestStatus(); statusErr != nil || unchangedStatus.ClosedWindowRevision != initialClosedRev {
		testingContext.Fatalf("expected ClosedWindowRevision %d across mode and bounds events, got %d (err=%v)",
			initialClosedRev, unchangedStatus.ClosedWindowRevision, statusErr)
	}

	testApp.publishApp(0, events.AppDatabaseSwitched)
	if testApp.carrierState.IsValid() || testApp.careerStats.IsValid() {
		testingContext.Errorf("expected carrierState and careerStats to invalidate on AppDatabaseSwitched")
	}
	if switchedStatus, switchedErr := testApp.GetIngestStatus(); switchedErr != nil || switchedStatus.ClosedWindowRevision != initialClosedRev+1 {
		testingContext.Errorf("expected ClosedWindowRevision %d after AppDatabaseSwitched, got %d (err=%v)",
			initialClosedRev+1, switchedStatus.ClosedWindowRevision, switchedErr)
	}
}

func TestStartupHookWaitsForDatabaseInitialization(testingContext *testing.T) {
	uninitializedApp := New()
	if _, uninitErr := uninitializedApp.PrepareCarrierRoutePrompt(1, 0); !errors.Is(uninitErr, errorNotReady) {
		testingContext.Fatalf("expected errorNotReady on nil-db PrepareCarrierRoutePrompt, got %v", uninitErr)
	}
	if schemeErr := uninitializedApp.OpenURL("ftp://invalid-scheme"); schemeErr == nil {
		testingContext.Fatalf("expected invalid URL scheme rejection on pre-init App")
	}

	temporaryDirectory := testingContext.TempDir()
	isolateTestUserDirectories(testingContext, temporaryDirectory)
	productionDBPath := filepath.Join(temporaryDirectory, "startup-race.sqlite")

	// Seed a database with an expedition so callers receive real state
	seedDatabaseWithExpedition(testingContext, productionDBPath, "Startup Race Expedition", "Verify concurrent RPCs wait for OnStartup")

	testingContext.Setenv("ED_PLANNER_DB", productionDBPath)

	originalFetchLatestGitHubVersion := fetchLatestGitHubVersion
	fetchLatestGitHubVersion = func(context.Context) (string, error) {
		return "", nil
	}
	testingContext.Cleanup(func() {
		fetchLatestGitHubVersion = originalFetchLatestGitHubVersion
	})

	debugApp := New()
	debugApp.debugMode = true
	registerTestEmitDispatcher(testingContext, debugApp)
	onStartup := StartupHook(debugApp)
	onShutdown := ShutdownHook(debugApp)

	appContext, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	defer onShutdown(appContext)

	// Launch frontend RPC callers BEFORE OnStartup even begins, simulating Wails
	// spawning OnStartup in a goroutine while the WebView immediately requests core state.
	var waitGroup sync.WaitGroup
	waitGroup.Add(5)

	go func() {
		defer waitGroup.Done()
		_ = debugApp.context()
		readyCtx, err := debugApp.readyContext()
		if err != nil || readyCtx == nil {
			testingContext.Errorf("readyContext during startup returned ctx=%v err=%v", readyCtx, err)
		}
	}()

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
	onStartup(appContext)
	if startupError := debugApp.getError(); startupError != "" {
		testingContext.Fatalf("onStartup failed: %s", startupError)
	}

	waitGroup.Wait()
}

func TestJournalDebounceSettingPersistenceAndReload(t *testing.T) {
	testApp := setupTestApp(t)
	eventsRecorder := captureAppEvents(t, testApp)

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
	if !slices.Contains(eventsRecorder.Domains(), "settings") {
		t.Fatalf("expected SetJournalDebounceMs to emit settings domain, got %v", eventsRecorder.Domains())
	}

	if reloadError := testApp.openAndWireDB(testApp.dbPath); reloadError != nil {
		t.Fatalf("openAndWireDB reload: %v", reloadError)
	}
	if testApp.watcher.DebounceDelay() != 5*time.Second {
		t.Fatalf("expected reloaded watcher to restore persisted 5s debounce, got %v", testApp.watcher.DebounceDelay())
	}
}

func TestStartupDoesNotEmitRedundantPostBootSettingsChange(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	isolateTestUserDirectories(testingContext, temporaryDirectory)
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
	registerTestEmitDispatcher(testingContext, startupApp)
	eventsRecorder := captureAppEvents(testingContext, startupApp)

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

	for _, changeEvent := range eventsRecorder.DataChanges() {
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
	eventsRecorder := captureAppEvents(testingContext, testApp)

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
	if !slices.Contains(eventsRecorder.Domains(), "settings") {
		testingContext.Fatalf("expected SetExternalLinks to emit settings domain, got %v", eventsRecorder.Domains())
	}

	reloadedSettings, reloadError := testApp.GetSettings()
	if reloadError != nil ||
		reloadedSettings.ExternalLinkInara != settings.ExternalLinkOff ||
		reloadedSettings.ExternalLinkEDSM != settings.ExternalLinkOn ||
		reloadedSettings.ExternalLinkGEC != settings.ExternalLinkOff {
		testingContext.Fatalf("GetSettings after SetExternalLinks: settings=%+v err=%v", reloadedSettings, reloadError)
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

	eventsRecorder := captureAppEvents(testingContext, testApp)

	assertDeleteOutcome := func(expectedActiveID int64) {
		testingContext.Helper()
		loadedSettings, settingsError := testApp.GetSettings()
		if settingsError != nil {
			testingContext.Fatalf("GetSettings: %v", settingsError)
		}
		if loadedSettings.ActiveExpeditionID != expectedActiveID {
			testingContext.Fatalf("expected active_expedition_id %d, got %d", expectedActiveID, loadedSettings.ActiveExpeditionID)
		}

		capturedEvents := eventsRecorder.TakeDataChanges()
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
