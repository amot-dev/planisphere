package app

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/eddtest"
	"planisphere/internal/testutil/httptestutil"
	"planisphere/internal/testutil/journaltest"
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
	initialPlan := mustGetPlan(testingContext, testApp, expeditionID)
	secondRow := addTestWaypoint(testingContext, testApp, expeditionID, initialPlan.Rows[0].Row.ID, "Alpha Centauri")
	journaltest.InsertParsedLines(
		testingContext,
		testApp.db,
		`{"timestamp":"2026-01-02T00:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`,
	)
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
		if _, statsError := testApp.GetSidebarStats(expeditionID, filterVariant, false); statsError != nil {
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
		if _, statsError := testApp.GetSidebarStats(expeditionID, filterVariant, false); statsError != nil {
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
	if sizeAfterComplete != 1 {
		testingContext.Fatalf("expected CompleteExpedition to evict mode-filtered entry while preserving unfiltered window, got %d entries", sizeAfterComplete)
	}

	completedSidebar, completedSidebarError := testApp.GetSidebarStats(expeditionID, stats.Filter{}, false)
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
	if sizeAfterCompletedQuery != 2 {
		testingContext.Fatalf("expected completed bounds query to cache new entry alongside preserved unfiltered window, got %d entries", sizeAfterCompletedQuery)
	}

	// Verify Complete -> Reopen mode-filtered cache invalidation when final waypoint DepartedAt changes.
	if reopenError := testApp.ReopenExpedition(expeditionID); reopenError != nil {
		testingContext.Fatalf("ReopenExpedition: %v", reopenError)
	}
	if modeErr := testApp.SetLegMode(secondRow.ID, 1, false); modeErr != nil {
		testingContext.Fatalf("SetLegMode(1): %v", modeErr)
	}
	if activeModeErr := testApp.SetExpeditionFlightMode(expeditionID, 2); activeModeErr != nil {
		testingContext.Fatalf("SetExpeditionFlightMode(2): %v", activeModeErr)
	}
	journaltest.InsertParsedLines(
		testingContext,
		testApp.db,
		`{"timestamp":"2026-01-03T00:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1003,"StarPos":[2,2,2],"JumpDist":6.0}`,
	)
	if _, reevalError := expedition.ReevaluateTx(testApp.db, expeditionID); reevalError != nil {
		testingContext.Fatalf("ReevaluateTx post-destination: %v", reevalError)
	}
	modeOneFilter := stats.Filter{Enabled: true, Modes: []stats.ModeSelection{{ModeID: 1, Travel: true}}}
	activeModeOneStats, activeErr := testApp.GetSidebarStats(expeditionID, modeOneFilter, false)
	if activeErr != nil || activeModeOneStats.Live.Jumps != 1 {
		testingContext.Fatalf("expected 1 Mode-1 jump while active (outbound jump in Mode 2), got %+v err=%v", activeModeOneStats.Live, activeErr)
	}
	if completeError := testApp.CompleteExpedition(expeditionID); completeError != nil {
		testingContext.Fatalf("second CompleteExpedition: %v", completeError)
	}
	expRecord, getExpErr := expedition.Get(testApp.db, expeditionID)
	if getExpErr != nil {
		testingContext.Fatalf("expedition.Get: %v", getExpErr)
	}
	queryAfter, _, _, startErr := expedition.EffectiveStart(testApp.db, expRecord)
	if startErr != nil {
		testingContext.Fatalf("EffectiveStart: %v", startErr)
	}
	// While Completed, secondRow.DepartedAt is empty, so an open-ended Mode-1 query spans 9999-12-31 and counts both jumps.
	completedOpenSummary, compOpenErr := testApp.timeRangeStats.Compute(
		stats.Range{After: queryAfter, Until: ""},
		&stats.Filter{Enabled: true, ExpeditionID: expeditionID, Modes: modeOneFilter.Modes, Scope: expRecord.ProfileScope()},
	)
	if compOpenErr != nil || completedOpenSummary.Metrics.Jumps != 2 {
		testingContext.Fatalf("expected 2 jumps on open Mode-1 range while completed, got %+v err=%v", completedOpenSummary.Metrics, compOpenErr)
	}
	if reopenError := testApp.ReopenExpedition(expeditionID); reopenError != nil {
		testingContext.Fatalf("second ReopenExpedition: %v", reopenError)
	}
	reopenedModeOneStats, reopenedErr := testApp.GetSidebarStats(expeditionID, modeOneFilter, false)
	if reopenedErr != nil || reopenedModeOneStats.Live.Jumps != 1 {
		testingContext.Fatalf("expected ReopenExpedition to evict stale open Mode-1 cache (want Jumps=1, got %d, err=%v)", reopenedModeOneStats.Live.Jumps, reopenedErr)
	}
}

func TestRefreshGEC(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	httptestutil.StubDefaultTransport(testingContext, func(_ *http.Request) (*http.Response, error) {
		payload := `[{"id":1,"source":"GEC","type":"Historical","name":"Cradle of Humanity","galMapSearch":"Sol","coordinates":[0,0,0]}]`
		return httptestutil.Response(http.StatusOK, payload), nil
	})

	createdExp := mustCreateExpeditionWithWaypoints(testingContext, testApp, "GEC Backfill", "", "Sol")

	// Verify in-flight RefreshGEC guard and uninitialized GEC catalog error handling.
	testApp.gecRefreshing.Store(true)
	_, busyErr := testApp.RefreshGEC()
	testApp.gecRefreshing.Store(false)
	if busyErr == nil || !strings.Contains(busyErr.Error(), "already in progress") {
		testingContext.Fatalf("expected concurrent RefreshGEC error, got %v", busyErr)
	}

	savedCatalog := testApp.gec
	testApp.gec = nil
	_, nilCatalogErr := testApp.RefreshGEC()
	testApp.gec = savedCatalog
	if nilCatalogErr == nil || !strings.Contains(nilCatalogErr.Error(), "not initialized") {
		testingContext.Fatalf("expected nil readyGECCatalog error, got %v", nilCatalogErr)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)
	importedCount, refreshErr := testApp.RefreshGEC()
	if refreshErr != nil || importedCount != 1 {
		testingContext.Fatalf("RefreshGEC() = (%d, %v), want (1, nil)", importedCount, refreshErr)
	}
	refreshDomains := eventsRecorder.Domains()
	for _, expectedDomain := range []string{"plan", "expeditions", "settings"} {
		if !slices.Contains(refreshDomains, expectedDomain) {
			testingContext.Fatalf("expected RefreshGEC to emit %q domain, got %v", expectedDomain, refreshDomains)
		}
	}
	loadedSettings, settingsErr := testApp.GetSettings()
	if settingsErr != nil || loadedSettings.GECCount != 1 || loadedSettings.GECFetchedAt == "" {
		testingContext.Fatalf("unexpected settings after RefreshGEC: count=%d fetchedAt=%q err=%v",
			loadedSettings.GECCount, loadedSettings.GECFetchedAt, settingsErr)
	}
	refreshedPlan := mustGetPlan(testingContext, testApp, createdExp.ID)
	if refreshedPlan.Rows[0].Row.POIName != "Cradle of Humanity" {
		testingContext.Fatalf("expected RefreshGEC to backfill Sol POIName, got %q", refreshedPlan.Rows[0].Row.POIName)
	}
}

func TestMaybeCatchUpEDD(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	eddPath := eddtest.BuildMinimalArchive(testingContext, [][2]string{
		{"Fileheader", `{"timestamp":"2026-04-01T10:00:00Z","event":"Fileheader","part":1,"Odyssey":true}`},
		{"Location", `{"timestamp":"2026-04-01T10:01:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":3230,"StarPos":[-9530.5,-910.28,19808.13]}`},
	})
	if err := settings.Set(testApp.db, "edd_history_recovered_v1", "1"); err != nil {
		testingContext.Fatal(err)
	}
	if err := settings.Set(testApp.db, "edd_db_path", eddPath); err != nil {
		testingContext.Fatal(err)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)
	testApp.maybeCatchUpEDD()

	progressEvents := eventsRecorder.EDDProgress()
	domainsAfterCatchUp := eventsRecorder.Domains()
	if len(progressEvents) == 0 || !slices.Contains(domainsAfterCatchUp, "ingest") {
		testingContext.Fatalf("expected maybeCatchUpEDD to emit progress and ingest domain, got progress=%d domains=%v",
			len(progressEvents), domainsAfterCatchUp)
	}
	if _, disconnectsDone, _ := settings.EDDRecoveryState(testApp.db); !disconnectsDone {
		testingContext.Fatal("expected maybeCatchUpEDD to mark disconnectsDone=true")
	}

	// Second invocation is an immediate no-op once disconnectsDone is persisted.
	eventsRecorder.Reset()
	testApp.maybeCatchUpEDD()
	if repeatedDomains := eventsRecorder.Domains(); len(repeatedDomains) != 0 {
		testingContext.Fatalf("expected second maybeCatchUpEDD call to be a no-op, got domains=%v", repeatedDomains)
	}
}
