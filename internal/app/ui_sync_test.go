package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func TestCacheInvalidationGenerationCounters(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	expeditionID := createAndStartExpedition(testingContext, testApp, "Generation Test", "Sol")
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:05:00Z", "FSDJump", "gen-depart-sol",
		`{"timestamp":"2026-01-01T00:05:00Z","event":"FSDJump","StarSystem":"Transit","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`)

	if _, statsError := testApp.GetSidebarStats(expeditionID, stats.Filter{}, true); statsError != nil {
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

	filterTargets := testApp.bus.PublishApp(events.AppCommanderFilterChanged)
	if !filterTargets.Has(events.UITargetPlan) || !filterTargets.Has(events.UITargetSettings) || !filterTargets.Has(events.UITargetStats) {
		testingContext.Fatalf("expected UITargetPlan, UITargetSettings, and UITargetStats on AppCommanderFilterChanged, got %v", filterTargets.Names())
	}
}

func TestQuietHUDIngestSubscriptionAndLiveIngestStatus(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:00:00Z", "LoadGame", "hud-load-game",
		`{"timestamp":"2026-01-01T00:00:00Z","event":"LoadGame","Commander":"CMDR Hud","FID":"F42","Ship":"mandalay","ShipName":"Quiet Star","Odyssey":true}`)
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:05:00Z", "FSDJump", "hud-jump-sol",
		`{"timestamp":"2026-01-01T00:05:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"JumpDist":12.5}`)

	fullStatus, fullError := testApp.GetIngestStatus()
	if fullError != nil {
		testingContext.Fatalf("GetIngestStatus: %v", fullError)
	}
	if !fullStatus.ArchiveLoaded || fullStatus.EventsStored != 2 || fullStatus.CurrentSystem != "Sol" || fullStatus.CurrentShip == "" {
		testingContext.Fatalf("unexpected full IngestStatus: %+v", fullStatus)
	}

	liveStatus, liveError := testApp.GetLiveIngestStatus()
	if liveError != nil {
		testingContext.Fatalf("GetLiveIngestStatus: %v", liveError)
	}
	if liveStatus.ArchiveLoaded || liveStatus.FilesTracked != 0 || liveStatus.EventsStored != 0 || liveStatus.FirstEventAt != "" || liveStatus.LastEventAt != "" {
		testingContext.Fatalf("expected GetLiveIngestStatus to skip archive counts, got %+v", liveStatus)
	}
	if liveStatus.CurrentSystem != fullStatus.CurrentSystem ||
		liveStatus.CurrentShip != fullStatus.CurrentShip ||
		liveStatus.ClosedWindowRevision != fullStatus.ClosedWindowRevision ||
		liveStatus.TelemetryRevision != fullStatus.TelemetryRevision {
		testingContext.Fatalf("expected live HUD and revision fields to match full status: live=%+v full=%+v", liveStatus, fullStatus)
	}

	// Ambient persisted events (such as FSDTarget, Docked, StartJump, FSSDiscoveryScan) must not emit UITargetIngest or bump TelemetryRevision.
	revisionBeforeAmbient := testApp.timeRangeStats.TelemetryRevision()
	ambientTargets := testApp.publishJournalBatch(
		journal.NewEventSet(
			journal.EventFSDTarget,
			journal.EventDocked,
			journal.EventUndocked,
			journal.EventStartJump,
			journal.EventFSSDiscoveryScan,
		),
		"2026-01-01T00:10:00Z",
		"2026-01-01T00:10:00Z",
	)
	if !ambientTargets.IsEmpty() {
		testingContext.Fatalf("expected empty UITargetSet for ambient journal events, got %v", ambientTargets.Names())
	}
	if testApp.timeRangeStats.TelemetryRevision() != revisionBeforeAmbient {
		testingContext.Fatalf("expected ambient journal events to preserve TelemetryRevision %d, got %d",
			revisionBeforeAmbient, testApp.timeRangeStats.TelemetryRevision())
	}

	// Telemetry, arrival, and vessel events must emit UITargetIngest and increment TelemetryRevision monotonically.
	expectedTelemetryRevision := revisionBeforeAmbient
	for _, telemetryEvent := range []journal.EventType{
		journal.EventScan,
		journal.EventFSDJump,
		journal.EventShipyardSwap,
		journal.EventLoadGame,
	} {
		targets := testApp.publishJournalBatch(
			journal.NewEventSet(telemetryEvent),
			"2026-01-01T00:20:00Z",
			"2026-01-01T00:20:00Z",
		)
		expectedTelemetryRevision++
		if !targets.Has(events.UITargetIngest) {
			testingContext.Fatalf("expected %s to emit UITargetIngest, got %v", journal.EventName(telemetryEvent), targets.Names())
		}
		if gotRevision := testApp.timeRangeStats.TelemetryRevision(); gotRevision != expectedTelemetryRevision {
			testingContext.Fatalf("expected %s to advance TelemetryRevision to %d, got %d",
				journal.EventName(telemetryEvent), expectedTelemetryRevision, gotRevision)
		}
	}

	// Rewiring projections across a hot database switch must advance TelemetryRevision above its prior watermark.
	testApp.wireProjections(testApp.db)
	if gotSwitchedRevision := testApp.timeRangeStats.TelemetryRevision(); gotSwitchedRevision <= expectedTelemetryRevision {
		testingContext.Fatalf("expected wireProjections to advance TelemetryRevision past %d, got %d",
			expectedTelemetryRevision, gotSwitchedRevision)
	}
}

func TestLiveSyncPauseAllowsUserMutationsAndDefersBackgroundUpdates(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDirectory := testingContext.TempDir()
	if setDirError := settings.SetJournalDir(testApp.db, journalDirectory); setDirError != nil {
		testingContext.Fatalf("set journal directory: %v", setDirError)
	}
	if setSetupError := settings.SetSetupCompleted(testApp.db, true); setSetupError != nil {
		testingContext.Fatalf("set setup completed: %v", setSetupError)
	}
	testApp.watcher.SetDebounceDelay(reference.MinDebounceDelay)
	testApp.prepareInitialState()

	expeditionRecord, createError := testApp.CreateExpedition("Paused Mutation Check", "")
	if createError != nil {
		testingContext.Fatalf("CreateExpedition: %v", createError)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)

	testApp.SetLiveSyncPaused(true)

	// Explicit user mutations must still emit DataChanged immediately while live sync is paused.
	if _, addError := testApp.AddRow(expeditionRecord.ID, 0, expedition.RowWaypoint, 0); addError != nil {
		testingContext.Fatalf("AddRow while paused: %v", addError)
	}
	if _, priceError := testApp.SetTritiumPricePerTonne(45000); priceError != nil {
		testingContext.Fatalf("SetTritiumPricePerTonne while paused: %v", priceError)
	}
	if deleteError := testApp.DeleteExpedition(expeditionRecord.ID); deleteError != nil {
		testingContext.Fatalf("DeleteExpedition while paused: %v", deleteError)
	}
	userChanges, pendingAfterUserMutations := eventsRecorder.TakeAll()
	if len(userChanges) != 3 {
		testingContext.Fatalf("expected 3 immediate DataChanged emissions for user mutations while paused, got %d (%+v)", len(userChanges), userChanges)
	}
	if len(pendingAfterUserMutations) != 0 {
		testingContext.Fatalf("expected 0 sync:pending signals from user mutations, got %v", pendingAfterUserMutations)
	}

	// Unsolicited background updates must coalesce into deferredTargets and emit sync:pending(true) once.
	testApp.onJournalBatch(journal.Result{
		NewEvents:         1,
		Events:            journal.NewEventSet(journal.EventFSDJump),
		EarliestTimestamp: "2026-04-03T12:00:00Z",
		LatestTimestamp:   "2026-04-03T12:00:00Z",
	})
	testApp.publishBackgroundApp(0, events.AppSettingsChanged)
	deferredChanges, pendingAfterBackground := eventsRecorder.TakeAll()
	if len(deferredChanges) != 0 {
		testingContext.Fatalf("expected 0 DataChanged emissions for background updates while paused, got %+v", deferredChanges)
	}
	if !slices.Equal(pendingAfterBackground, []bool{true}) {
		testingContext.Fatalf("expected single sync:pending [true] transition, got %v", pendingAfterBackground)
	}

	// Resuming live sync clears sync:pending and emits one coalesced DataChanged union.
	testApp.SetLiveSyncPaused(false)
	flushedChanges, pendingAfterResume := eventsRecorder.TakeAll()
	if !slices.Equal(pendingAfterResume, []bool{false}) {
		testingContext.Fatalf("expected sync:pending [false] on resume, got %v", pendingAfterResume)
	}
	if len(flushedChanges) != 1 {
		testingContext.Fatalf("expected 1 coalesced DataChanged flush on resume, got %+v", flushedChanges)
	}
	for _, expectedDomain := range []string{"ingest", "stats", "plan", "settings"} {
		if !slices.Contains(flushedChanges[0].Domains, expectedDomain) {
			testingContext.Fatalf("expected flushed domains %v to include %q", flushedChanges[0].Domains, expectedDomain)
		}
	}

	// Pause again, write a live journal file, verify SQLite stays untouched until explicit RescanJournals.
	testApp.SetLiveSyncPaused(true)
	liveJournalPath := filepath.Join(journalDirectory, "Journal.2026-04-03T130000.01.log")
	liveLine := `{"timestamp":"2026-04-03T13:00:00Z","event":"LoadGame","Commander":"CMDR Test","FID":"F1","Ship":"Anaconda"}` + "\n"
	if writeError := os.WriteFile(liveJournalPath, []byte(liveLine), 0o600); writeError != nil {
		testingContext.Fatalf("write live journal while paused: %v", writeError)
	}
	select {
	case <-eventsRecorder.PendingTrue():
	case <-time.After(2 * time.Second):
		testingContext.Fatalf("timed out waiting for watcher sync:pending while paused")
	}

	statusWhilePaused, statusError := testApp.GetIngestStatus()
	if statusError != nil {
		testingContext.Fatalf("GetIngestStatus while paused: %v", statusError)
	}
	if statusWhilePaused.EventsStored != 0 {
		testingContext.Fatalf("expected 0 stored events while watcher is paused, got %d", statusWhilePaused.EventsStored)
	}
	_, _ = eventsRecorder.TakeAll()

	if rescanError := testApp.RescanJournals(); rescanError != nil {
		testingContext.Fatalf("RescanJournals while paused: %v", rescanError)
	}
	rescanChanges, pendingAfterRescan := eventsRecorder.TakeAll()
	if len(rescanChanges) != 1 {
		testingContext.Fatalf("expected explicit RescanJournals to emit DataChanged while paused, got %+v", rescanChanges)
	}
	if !slices.Equal(pendingAfterRescan, []bool{false}) {
		testingContext.Fatalf("expected RescanJournals to clear sync:pending [false], got %v", pendingAfterRescan)
	}
}
