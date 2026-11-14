package app

import (
	"os"
	"path/filepath"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/expedition"
	"planisphere/internal/stats"
)

func TestRescanJournalsSynchronousCompletionAndSelectiveCacheInvalidation(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDirectory := testingContext.TempDir()
	journalFilePath := filepath.Join(journalDirectory, "Journal.2026-01-01T000000.01.log")

	initialJournalContent := `{"timestamp":"2026-01-01T00:00:00Z","event":"LoadGame","Commander":"CMDR Test","FID":"F123","Ship":"Anaconda","Odyssey":true}` + "\n" +
		`{"timestamp":"2026-01-01T00:01:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"JumpDist":12.5}` + "\n"
	if writeError := os.WriteFile(journalFilePath, []byte(initialJournalContent), 0o600); writeError != nil {
		testingContext.Fatalf("write initial journal: %v", writeError)
	}

	if setSettingError := db.SetSetting(testApp.db, settingJournalDir, journalDirectory); setSettingError != nil {
		testingContext.Fatalf("set journal directory: %v", setSettingError)
	}
	if setSetupError := db.SetSetting(testApp.db, settingSetupCompleted, "1"); setSetupError != nil {
		testingContext.Fatalf("set setup completed: %v", setSetupError)
	}

	var observedScanningStates []bool
	var journalChangedCount int
	testApp.emit = func(event string, data ...any) {
		switch event {
		case EventIngest:
			observedScanningStates = append(observedScanningStates, testApp.scanning.Load())
		case EventDataChanged:
			if payload, ok := data[0].(DataChanged); ok && payload.Scope == "journal" {
				journalChangedCount++
			}
		}
	}

	testApp.prepareInitialState()

	if len(observedScanningStates) != 2 || !observedScanningStates[0] || observedScanningStates[1] {
		testingContext.Fatalf("expected EventIngest scanning states [true, false], got %v", observedScanningStates)
	}
	if journalChangedCount != 1 {
		testingContext.Fatalf("expected 1 journal data:changed emission on initial scan, got %d", journalChangedCount)
	}

	testApp.allTimeMu.Lock()
	warmedAllTimeMetrics := testApp.allTimeValid
	warmedAllTimeRealization := testApp.allTimeRealizationValid
	testApp.allTimeMu.Unlock()
	if !warmedAllTimeMetrics || !warmedAllTimeRealization {
		testingContext.Fatalf("expected prepareInitialState to warm allTime caches, got metrics=%v realization=%v", warmedAllTimeMetrics, warmedAllTimeRealization)
	}

	// Append a non-relevant journal line (Music) so LinesRead > 0 while NewEvents == 0.
	nonRelevantLine := `{"timestamp":"2026-01-01T00:02:00Z","event":"Music","MusicTrack":"Exploration"}` + "\n"
	if writeError := os.WriteFile(journalFilePath, []byte(initialJournalContent+nonRelevantLine), 0o600); writeError != nil {
		testingContext.Fatalf("append non-relevant line: %v", writeError)
	}

	observedScanningStates = nil
	journalChangedCount = 0
	if rescanError := testApp.RescanJournals(); rescanError != nil {
		testingContext.Fatalf("RescanJournals: %v", rescanError)
	}

	if len(observedScanningStates) != 2 || !observedScanningStates[0] || observedScanningStates[1] {
		testingContext.Fatalf("expected RescanJournals EventIngest scanning states [true, false], got %v", observedScanningStates)
	}
	if journalChangedCount != 0 {
		testingContext.Fatalf("expected 0 journal data:changed emissions when NewEvents == 0, got %d", journalChangedCount)
	}

	testApp.allTimeMu.Lock()
	stillValidAfterNoOpRescan := testApp.allTimeValid && testApp.allTimeRealizationValid
	testApp.allTimeMu.Unlock()
	if !stillValidAfterNoOpRescan {
		testingContext.Fatalf("expected all-time caches to remain valid when RescanJournals finds no new relevant events")
	}

	ingestStatus, statusError := testApp.GetIngestStatus()
	if statusError != nil {
		testingContext.Fatalf("GetIngestStatus: %v", statusError)
	}
	if ingestStatus.Scanning {
		testingContext.Fatalf("expected ingestStatus.Scanning to be false after RescanJournals returns")
	}
	if ingestStatus.EventsStored != 2 || ingestStatus.FilesTracked != 1 {
		testingContext.Fatalf("expected 2 stored events across 1 tracked file, got events=%d files=%d", ingestStatus.EventsStored, ingestStatus.FilesTracked)
	}

	previousEmit := testApp.emit
	acquiredInCompletionCallback := false
	testApp.emit = func(event string, data ...any) {
		if event == EventIngest && !testApp.scanning.Load() {
			acquiredInCompletionCallback = testApp.scanning.CompareAndSwap(false, true)
		}
		previousEmit(event, data...)
	}
	if rescanError := testApp.RescanJournals(); rescanError != nil {
		testingContext.Fatalf("RescanJournals with completion handoff: %v", rescanError)
	}
	testApp.emit = previousEmit
	if !acquiredInCompletionCallback || !testApp.scanning.Load() {
		testingContext.Fatalf("expected scanning flag to stay acquired after completion callback handoff (acquired=%v scanning=%v)", acquiredInCompletionCallback, testApp.scanning.Load())
	}
	testApp.scanning.Store(false)
}

func TestWarmInitialStatsCanonicalCacheKeyAndCosmeticPlanEdits(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	expeditionID := createAndStartExpedition(testingContext, testApp, "Cache Key Test", "Sol")

	testApp.warmInitialStats()

	testApp.liveStatsMu.Lock()
	warmedCacheSize := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if warmedCacheSize != 1 {
		testingContext.Fatalf("expected 1 cached entry after warmInitialStats, got %d", warmedCacheSize)
	}

	for _, filterVariant := range []stats.Filter{
		{Enabled: false, Modes: []stats.ModeSelection{}},
		{Enabled: false, Modes: []stats.ModeSelection{{ModeID: 1, Travel: true}}},
	} {
		if _, statsError := testApp.GetSidebarStats(expeditionID, filterVariant); statsError != nil {
			testingContext.Fatalf("GetSidebarStats unfiltered: %v", statsError)
		}
	}
	testApp.liveStatsMu.Lock()
	afterUnfilteredSize := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if afterUnfilteredSize != 1 {
		testingContext.Fatalf("expected unfiltered variants to reuse warmed cache entry, got %d entries", afterUnfilteredSize)
	}

	for _, filterVariant := range []stats.Filter{
		{Enabled: true, Modes: []stats.ModeSelection{{ModeID: 1, Travel: true}, {ModeID: 2, Discovery: true}}},
		{Enabled: true, Modes: []stats.ModeSelection{{ModeID: 99, Travel: false, Discovery: false}, {ModeID: 2, Discovery: true}, {ModeID: 1, Travel: true}}},
	} {
		if _, statsError := testApp.GetSidebarStats(expeditionID, filterVariant); statsError != nil {
			testingContext.Fatalf("GetSidebarStats filtered: %v", statsError)
		}
	}
	testApp.liveStatsMu.Lock()
	afterFilteredSize := len(testApp.liveStatsCache)
	generationBeforeCosmetic := testApp.liveStatsGen
	testApp.liveStatsMu.Unlock()
	if afterFilteredSize != 2 {
		testingContext.Fatalf("expected canonical filtered variants to share 1 entry (total 2), got %d entries", afterFilteredSize)
	}

	planView, planError := testApp.GetPlan(expeditionID)
	if planError != nil || len(planView.Rows) == 0 {
		testingContext.Fatalf("GetPlan: len=%d err=%v", len(planView.Rows), planError)
	}
	noteText := "Updated scenic notes"
	poiText := "Sol Origin"
	if updateError := testApp.UpdateRow(expedition.RowPatch{ID: planView.Rows[0].Row.ID, Notes: &noteText, POIName: &poiText}); updateError != nil {
		testingContext.Fatalf("UpdateRow cosmetic: %v", updateError)
	}
	testApp.liveStatsMu.Lock()
	sizeAfterCosmetic := len(testApp.liveStatsCache)
	generationAfterCosmetic := testApp.liveStatsGen
	testApp.liveStatsMu.Unlock()
	if sizeAfterCosmetic != 2 || generationAfterCosmetic != generationBeforeCosmetic {
		testingContext.Fatalf("expected cosmetic plan edit to preserve cache (size=%d gen=%d vs %d)", sizeAfterCosmetic, generationAfterCosmetic, generationBeforeCosmetic)
	}

	if completeError := testApp.CompleteExpedition(expeditionID); completeError != nil {
		testingContext.Fatalf("CompleteExpedition: %v", completeError)
	}
	testApp.liveStatsMu.Lock()
	sizeAfterComplete := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if sizeAfterComplete != 0 {
		testingContext.Fatalf("expected CompleteExpedition to invalidate liveStatsCache, got %d entries", sizeAfterComplete)
	}
}

