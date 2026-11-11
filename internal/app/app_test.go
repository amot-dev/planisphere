package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
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
		stats.InvalidateLedgerCache()
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

func TestResolveRowSync_ContextCancellation(t *testing.T) {
	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	testApp.ctx = ctx
	testApp.cancel = cancel

	// Fill semaphore so resolveRowSync has to wait
	testApp.resolveSem <- struct{}{}

	// Cancel context
	cancel()

	done := make(chan struct{})
	go func() {
		resolved, updated := testApp.resolveRowSync(expedition.Row{ID: 1, SystemName: "Sol"}, true)
		if resolved || updated {
			t.Errorf("expected false, false, got %v, %v", resolved, updated)
		}
		close(done)
	}()

	select {
	case <-done:
		// success: returned promptly due to ctx cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("resolveRowSync blocked waiting for semaphore after context was cancelled")
	}
}

func TestResolveRowSync_SemaphoreReleased(t *testing.T) {
	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	testApp.ctx = ctx
	testApp.cancel = cancel

	// resolveRowSync with online=true, but resolver is nil
	// Should acquire semaphore and release it via defer
	resolved, updated := testApp.resolveRowSync(expedition.Row{ID: 1, SystemName: "Sol"}, true)
	if resolved || updated {
		t.Errorf("expected false, false on nil db/resolver, got %v, %v", resolved, updated)
	}

	// Verify semaphore is available (can acquire without blocking)
	select {
	case testApp.resolveSem <- struct{}{}:
		// Semaphore token was successfully released!
		<-testApp.resolveSem
	default:
		t.Fatal("semaphore was not released by resolveRowSync")
	}
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

	testApp.careerStartMu.Lock()
	if !testApp.careerStartValid {
		testingContext.Errorf("expected careerStartValid to be true")
	}
	if testApp.careerStartTimestamp != expectedCareerStart {
		testingContext.Errorf("expected careerStartTimestamp %s, got %s", expectedCareerStart, testApp.careerStartTimestamp)
	}
	testApp.careerStartMu.Unlock()
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

	// Start expedition at 2026-01-01T00:00:00Z
	_, _ = testApp.db.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '2026-01-01T00:00:00Z' WHERE id = ?`, expeditionRecord.ID)

	// Scan 2: During expedition (2026)
	insertTestJournalEvent(testingContext, testApp, "2026-03-01T10:00:00Z", "ScanOrganic", "2",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":222,"Body":1}`)
	insertTestJournalEvent(testingContext, testApp, "2026-03-01T12:00:00Z", "SellOrganicData", "2",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)
	stats.InvalidateLedgerCache()
	testApp.invalidateAllTimeCache()

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
	tempDir := testingContext.TempDir()
	productionDBPath := filepath.Join(tempDir, "production.sqlite")

	// Seed a production database with an existing expedition
	productionApp := New()
	productionApp.debugMode = false
	prodCtx, prodCancel := context.WithCancel(context.Background())
	if initError := productionApp.initialize(prodCtx, productionDBPath); initError != nil {
		prodCancel()
		testingContext.Fatalf("initialize production app: %v", initError)
	}
	if _, createError := productionApp.CreateExpedition("Production Expedition", "Saved in production DB"); createError != nil {
		prodCancel()
		productionApp.shutdown(prodCtx)
		testingContext.Fatalf("create production expedition: %v", createError)
	}
	prodCancel()
	productionApp.shutdown(prodCtx)

	originalProdBytes, readOrigError := os.ReadFile(productionDBPath)
	if readOrigError != nil {
		testingContext.Fatalf("read production DB bytes: %v", readOrigError)
	}

	// Launch a debug-mode app pointing at the same production database path
	debugApp := New()
	debugApp.debugMode = true
	debugCtx, debugCancel := context.WithCancel(context.Background())
	defer debugCancel()

	if initDebugError := debugApp.initialize(debugCtx, productionDBPath); initDebugError != nil {
		testingContext.Fatalf("initialize debug app: %v", initDebugError)
	}

	settings, settingsError := debugApp.GetSettings()
	if settingsError != nil {
		debugApp.shutdown(debugCtx)
		testingContext.Fatalf("GetSettings in debug app: %v", settingsError)
	}
	if settings.DBPath == productionDBPath {
		debugApp.shutdown(debugCtx)
		testingContext.Fatalf("expected debug app DBPath to point to temporary snapshot, got %s", settings.DBPath)
	}
	if !strings.Contains(filepath.Base(settings.DBPath), "planisphere-debug-") {
		debugApp.shutdown(debugCtx)
		testingContext.Fatalf("expected debug snapshot filename to contain 'planisphere-debug-', got %s", settings.DBPath)
	}
	snapshotPath := settings.DBPath

	// Verify production data is present in the debug snapshot
	expeditionsInDebug, listError := debugApp.ListExpeditions()
	if listError != nil {
		debugApp.shutdown(debugCtx)
		testingContext.Fatalf("ListExpeditions in debug app: %v", listError)
	}
	foundProdExpedition := false
	for _, expItem := range expeditionsInDebug {
		if expItem.Name == "Production Expedition" {
			foundProdExpedition = true
			break
		}
	}
	if !foundProdExpedition {
		debugApp.shutdown(debugCtx)
		testingContext.Fatalf("expected 'Production Expedition' to be present in debug snapshot")
	}

	// Mutate data inside debug app
	if _, createDebugError := debugApp.CreateExpedition("Debug Scratch Expedition", "Must not persist to production DB"); createDebugError != nil {
		debugApp.shutdown(debugCtx)
		testingContext.Fatalf("create expedition in debug app: %v", createDebugError)
	}

	// Shut down debug app and verify temporary snapshot is deleted
	debugApp.shutdown(debugCtx)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statError := os.Stat(snapshotPath + suffix); !os.IsNotExist(statError) {
			testingContext.Errorf("expected debug snapshot %s%s to be removed on shutdown, stat err: %v", snapshotPath, suffix, statError)
		}
	}

	// Verify production DB file on disk was not modified
	afterProdBytes, readAfterError := os.ReadFile(productionDBPath)
	if readAfterError != nil {
		testingContext.Fatalf("read production DB bytes after debug shutdown: %v", readAfterError)
	}
	if !bytes.Equal(originalProdBytes, afterProdBytes) {
		testingContext.Fatalf("expected production DB bytes to remain untouched after debug session")
	}
}

func TestCacheInvalidationGenerationCounters(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	testApp.liveStatsMu.Lock()
	initialLiveStatsGeneration := testApp.liveStatsGen
	testApp.liveStatsCache["sample-key"] = cachedLiveStats{}
	testApp.liveStatsMu.Unlock()

	testApp.invalidateLiveStatsCache()

	testApp.liveStatsMu.Lock()
	afterLiveInvalidationGeneration := testApp.liveStatsGen
	liveCacheSizeAfterLiveInvalidation := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()

	if afterLiveInvalidationGeneration != initialLiveStatsGeneration+1 {
		testingContext.Errorf("expected liveStatsGen to increment from %d to %d, got %d", initialLiveStatsGeneration, initialLiveStatsGeneration+1, afterLiveInvalidationGeneration)
	}
	if liveCacheSizeAfterLiveInvalidation != 0 {
		testingContext.Errorf("expected liveStatsCache to be empty after invalidateLiveStatsCache, got %d entries", liveCacheSizeAfterLiveInvalidation)
	}

	testApp.careerStartMu.Lock()
	initialCareerStartGeneration := testApp.careerStartGen
	testApp.careerStartTimestamp = "2025-01-01T00:00:00Z"
	testApp.careerStartGalactic = "01 JAN 3311"
	testApp.careerStartValid = true
	testApp.careerStartMu.Unlock()

	testApp.liveStatsMu.Lock()
	testApp.liveStatsCache["another-key"] = cachedLiveStats{}
	testApp.liveStatsMu.Unlock()

	testApp.invalidateAllTimeCache()

	testApp.careerStartMu.Lock()
	afterAllTimeCareerGeneration := testApp.careerStartGen
	careerStartValidAfterInvalidation := testApp.careerStartValid
	careerStartTimestampAfterInvalidation := testApp.careerStartTimestamp
	careerStartGalacticAfterInvalidation := testApp.careerStartGalactic
	testApp.careerStartMu.Unlock()

	if afterAllTimeCareerGeneration != initialCareerStartGeneration+1 {
		testingContext.Errorf("expected careerStartGen to increment from %d to %d, got %d", initialCareerStartGeneration, initialCareerStartGeneration+1, afterAllTimeCareerGeneration)
	}
	if careerStartValidAfterInvalidation {
		testingContext.Errorf("expected careerStartValid to be false after invalidateAllTimeCache")
	}
	if careerStartTimestampAfterInvalidation != "" || careerStartGalacticAfterInvalidation != "" {
		testingContext.Errorf("expected careerStart cached strings to be cleared, got timestamp=%q galactic=%q", careerStartTimestampAfterInvalidation, careerStartGalacticAfterInvalidation)
	}

	testApp.liveStatsMu.Lock()
	afterAllTimeLiveGeneration := testApp.liveStatsGen
	liveCacheSizeAfterAllTimeInvalidation := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()

	if afterAllTimeLiveGeneration != afterLiveInvalidationGeneration+1 {
		testingContext.Errorf("expected liveStatsGen to increment from %d to %d on invalidateAllTimeCache, got %d", afterLiveInvalidationGeneration, afterLiveInvalidationGeneration+1, afterAllTimeLiveGeneration)
	}
	if liveCacheSizeAfterAllTimeInvalidation != 0 {
		testingContext.Errorf("expected liveStatsCache to be empty after invalidateAllTimeCache, got %d entries", liveCacheSizeAfterAllTimeInvalidation)
	}
}



