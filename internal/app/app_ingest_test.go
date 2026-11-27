package app

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
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

	if setSettingError := settings.SetJournalDir(testApp.db, journalDirectory); setSettingError != nil {
		testingContext.Fatalf("set journal directory: %v", setSettingError)
	}
	if setSetupError := settings.SetSetupCompleted(testApp.db, true); setSetupError != nil {
		testingContext.Fatalf("set setup completed: %v", setSetupError)
	}

	var emitMutex sync.Mutex
	var observedScanningStates []bool
	var journalChangedCount int
	setTestEmitHook(testingContext, testApp, func(event string, data ...any) {
		emitMutex.Lock()
		defer emitMutex.Unlock()
		switch event {
		case EventIngest:
			observedScanningStates = append(observedScanningStates, testApp.scanning.Load())
		case EventDataChanged:
			if payload, ok := data[0].(DataChanged); ok && slices.Contains(payload.Domains, "ingest") {
				journalChangedCount++
			}
		}
	})

	testApp.prepareInitialState()

	emitMutex.Lock()
	initialStates := append([]bool(nil), observedScanningStates...)
	initialJournalChanges := journalChangedCount
	emitMutex.Unlock()
	if len(initialStates) != 2 || !initialStates[0] || initialStates[1] {
		testingContext.Fatalf("expected EventIngest scanning states [true, false], got %v", initialStates)
	}
	if initialJournalChanges != 1 {
		testingContext.Fatalf("expected 1 journal data:changed emission on initial scan, got %d", initialJournalChanges)
	}

	if !testApp.careerStats.IsValid() || !testApp.salesLedger.IsValid() {
		testingContext.Fatalf("expected prepareInitialState to warm careerStats and salesLedger caches, got career=%v ledger=%v",
			testApp.careerStats.IsValid(), testApp.salesLedger.IsValid())
	}
	careerGenerationBeforeNoOp := testApp.careerStats.Generation()

	// Append a non-relevant journal line (Music) so LinesRead > 0 while NewEvents == 0.
	nonRelevantLine := `{"timestamp":"2026-01-01T00:02:00Z","event":"Music","MusicTrack":"Exploration"}` + "\n"
	if writeError := os.WriteFile(journalFilePath, []byte(initialJournalContent+nonRelevantLine), 0o600); writeError != nil {
		testingContext.Fatalf("append non-relevant line: %v", writeError)
	}

	emitMutex.Lock()
	observedScanningStates = nil
	journalChangedCount = 0
	emitMutex.Unlock()
	if rescanError := testApp.RescanJournals(); rescanError != nil {
		testingContext.Fatalf("RescanJournals: %v", rescanError)
	}

	emitMutex.Lock()
	rescanStates := append([]bool(nil), observedScanningStates...)
	rescanJournalChanges := journalChangedCount
	emitMutex.Unlock()
	if len(rescanStates) != 2 || !rescanStates[0] || rescanStates[1] {
		testingContext.Fatalf("expected RescanJournals EventIngest scanning states [true, false], got %v", rescanStates)
	}
	if rescanJournalChanges != 0 {
		testingContext.Fatalf("expected 0 journal data:changed emissions when NewEvents == 0, got %d", rescanJournalChanges)
	}

	if !testApp.careerStats.IsValid() || !testApp.salesLedger.IsValid() || testApp.careerStats.Generation() != careerGenerationBeforeNoOp {
		testingContext.Fatalf("expected lifetime projection caches to remain valid when RescanJournals finds no new relevant events")
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

	var previousEmit func(event string, data ...any)
	acquiredInCompletionCallback := false
	previousEmit = setTestEmitHook(testingContext, testApp, func(event string, data ...any) {
		if event == EventIngest && !testApp.scanning.Load() {
			emitMutex.Lock()
			acquiredInCompletionCallback = testApp.scanning.CompareAndSwap(false, true)
			emitMutex.Unlock()
		}
		if previousEmit != nil {
			previousEmit(event, data...)
		}
	})
	if rescanError := testApp.RescanJournals(); rescanError != nil {
		testingContext.Fatalf("RescanJournals with completion handoff: %v", rescanError)
	}
	setTestEmitHook(testingContext, testApp, previousEmit)
	emitMutex.Lock()
	wasAcquired := acquiredInCompletionCallback
	emitMutex.Unlock()
	if !wasAcquired || !testApp.scanning.Load() {
		testingContext.Fatalf("expected scanning flag to stay acquired after completion callback handoff (acquired=%v scanning=%v)", wasAcquired, testApp.scanning.Load())
	}
	testApp.scanning.Store(false)
}

func TestWarmInitialStatsCanonicalCacheKeyAndCosmeticPlanEdits(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	expeditionID := createAndStartExpedition(testingContext, testApp, "Cache Key Test", "Sol")
	initialPlan, initialPlanError := testApp.GetPlan(expeditionID)
	if initialPlanError != nil || len(initialPlan.Rows) == 0 {
		testingContext.Fatalf("initial GetPlan: len=%d err=%v", len(initialPlan.Rows), initialPlanError)
	}
	secondRow, insertError := testApp.AddRow(expeditionID, initialPlan.Rows[0].Row.ID, expedition.RowWaypoint, 0)
	if insertError != nil {
		testingContext.Fatalf("AddRow: %v", insertError)
	}
	secondSystem := "Alpha Centauri"
	if updateError := testApp.UpdateRow(expedition.RowPatch{ID: secondRow.ID, SystemName: &secondSystem}); updateError != nil {
		testingContext.Fatalf("UpdateRow second waypoint: %v", updateError)
	}
	if _, execError := testApp.db.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, raw_json, source_file, ingested_at)
		 VALUES ('ev-cache-key-dest', '2026-01-02T00:00:00Z', 'FSDJump', 'Alpha Centauri', 1002, '{"timestamp":"2026-01-02T00:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}', 'test.log', '2026-01-02T00:00:00Z')`,
	); execError != nil {
		testingContext.Fatalf("insert destination arrival: %v", execError)
	}
	if _, reevalError := expedition.ReevaluateTx(testApp.db, expeditionID); reevalError != nil {
		testingContext.Fatalf("ReevaluateTx: %v", reevalError)
	}

	testApp.warmInitialStats()

	warmedCacheSize := testApp.timeRangeStats.CachedCount()
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
	afterUnfilteredSize := testApp.timeRangeStats.CachedCount()
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
	afterFilteredSize := testApp.timeRangeStats.CachedCount()
	generationBeforeCosmetic := testApp.timeRangeStats.Generation()
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
	sizeAfterCosmetic := testApp.timeRangeStats.CachedCount()
	generationAfterCosmetic := testApp.timeRangeStats.Generation()
	if sizeAfterCosmetic != 2 || generationAfterCosmetic != generationBeforeCosmetic {
		testingContext.Fatalf("expected cosmetic plan edit to preserve cache (size=%d gen=%d vs %d)", sizeAfterCosmetic, generationAfterCosmetic, generationBeforeCosmetic)
	}

	if completeError := testApp.CompleteExpedition(expeditionID); completeError != nil {
		testingContext.Fatalf("CompleteExpedition: %v", completeError)
	}
	sizeAfterComplete := testApp.timeRangeStats.CachedCount()
	if sizeAfterComplete != 2 {
		testingContext.Fatalf("expected CompleteExpedition to preserve prior cached windows, got %d entries", sizeAfterComplete)
	}

	completedSidebar, completedSidebarError := testApp.GetSidebarStats(expeditionID, stats.Filter{})
	if completedSidebarError != nil {
		testingContext.Fatalf("GetSidebarStats completed: %v", completedSidebarError)
	}
	if completedSidebar.EndIsNow {
		testingContext.Fatalf("expected completed expedition sidebar EndIsNow=false")
	}
	if completedSidebar.End != "2026-01-02T00:00:00Z" {
		testingContext.Fatalf("expected completed expedition sidebar End=2026-01-02T00:00:00Z, got %q", completedSidebar.End)
	}
	sizeAfterCompletedQuery := testApp.timeRangeStats.CachedCount()
	if sizeAfterCompletedQuery != 3 {
		testingContext.Fatalf("expected completed bounds query to cache new entry alongside prior 2 windows, got %d entries", sizeAfterCompletedQuery)
	}
}
