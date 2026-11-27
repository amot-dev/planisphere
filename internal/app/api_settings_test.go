package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/eddtest"
)

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

func waitForWatcherActive(testingContext *testing.T, testApp *App) IngestStatus {
	testingContext.Helper()
	var lastStatus IngestStatus
	for attempt := 0; attempt < 50; attempt++ {
		status, statusErr := testApp.GetIngestStatus()
		if statusErr != nil {
			testingContext.Fatalf("GetIngestStatus: %v", statusErr)
		}
		lastStatus = status
		if status.WatcherActive {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	testingContext.Fatalf("expected live journal watcher to become active, last status: %+v", lastStatus)
	return lastStatus
}

func TestSetupConfigureJournalsAvoidsDuplicateEmissionsAndPreservesPostSetupSync(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDirectory := testingContext.TempDir()
	journalPath := filepath.Join(journalDirectory, "Journal.2026-02-01T000000.01.log")
	journalLine := `{"timestamp":"2026-02-01T00:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}` + "\n"
	if writeError := os.WriteFile(journalPath, []byte(journalLine), 0o600); writeError != nil {
		testingContext.Fatalf("write setup journal: %v", writeError)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)

	initialStatus, setupError := testApp.SetupConfigureJournals(journalDirectory)
	if setupError != nil || initialStatus.EventsStored != 1 {
		testingContext.Fatalf("SetupConfigureJournals initial: status=%+v err=%v", initialStatus, setupError)
	}
	waitForWatcherActive(testingContext, testApp)
	if preSetupEvents := eventsRecorder.EventNames(); len(preSetupEvents) != 0 {
		testingContext.Fatalf("expected 0 redundant emissions during pre-completion setup scan, got %v", preSetupEvents)
	}

	if setCompleteErr := settings.SetSetupCompleted(testApp.db, true); setCompleteErr != nil {
		testingContext.Fatalf("mark setup completed: %v", setCompleteErr)
	}
	if _, postSetupErr := testApp.SetupConfigureJournals(journalDirectory); postSetupErr != nil {
		testingContext.Fatalf("SetupConfigureJournals post-setup: %v", postSetupErr)
	}
	emittedEventNames := eventsRecorder.EventNames()
	emittedChanges := eventsRecorder.DataChanges()
	if !slices.Equal(emittedEventNames, []string{EventDataChanged}) || len(emittedChanges) != 1 ||
		!slices.Contains(emittedChanges[0].Domains, "ingest") || !slices.Contains(emittedChanges[0].Domains, "settings") {
		testingContext.Fatalf("expected 1 post-setup DataChanged event with ingest and settings, got events=%v changes=%+v", emittedEventNames, emittedChanges)
	}
}

func TestSetCommanderEnabledSetJournalDirAndImportEDDiscoveryHistory(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if err := settings.SetSetupCompleted(testApp.db, true); err != nil {
		testingContext.Fatalf("SetSetupCompleted: %v", err)
	}

	if _, badDirErr := testApp.SetJournalDir(filepath.Join(testingContext.TempDir(), "missing-dir")); badDirErr == nil {
		testingContext.Fatal("expected SetJournalDir to reject non-existent directory")
	}

	journalDirectory := testingContext.TempDir()
	journalContent := `{"timestamp":"2026-03-01T08:00:00Z","event":"LoadGame","Commander":"CmdrToggle","FID":"F888","Ship":"Mandalay","Odyssey":true}` + "\n" +
		`{"timestamp":"2026-03-01T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}` + "\n"
	if err := os.WriteFile(filepath.Join(journalDirectory, "Journal.2026-03-01T080000.01.log"), []byte(journalContent), 0o600); err != nil {
		testingContext.Fatalf("write journal file: %v", err)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)
	dirSettings, setDirErr := testApp.SetJournalDir(journalDirectory)
	if setDirErr != nil || dirSettings.JournalDir != journalDirectory {
		testingContext.Fatalf("SetJournalDir = (%+v, %v)", dirSettings, setDirErr)
	}
	testApp.backgroundWg.Wait()
	if !slices.Contains(eventsRecorder.Domains(), "settings") {
		testingContext.Fatalf("expected SetJournalDir to emit settings domain, got %v", eventsRecorder.Domains())
	}
	ingestStatus := waitForWatcherActive(testingContext, testApp)
	if ingestStatus.EventsStored != 2 {
		testingContext.Fatalf("expected scanAndStartWatcher to store 2 events, got %+v", ingestStatus)
	}

	loadedSettings, getSettingsErr := testApp.GetSettings()
	if getSettingsErr != nil || len(loadedSettings.Commanders) != 1 || !loadedSettings.Commanders[0].Enabled {
		testingContext.Fatalf("expected 1 enabled commander after scan, got %+v err=%v", loadedSettings.Commanders, getSettingsErr)
	}
	eventsRecorder.Reset()

	toggledSettings, toggleErr := testApp.SetCommanderEnabled(loadedSettings.Commanders[0].ProfileKey, false)
	if toggleErr != nil || len(toggledSettings.Commanders) != 1 || toggledSettings.Commanders[0].Enabled {
		testingContext.Fatalf("SetCommanderEnabled(false) = (%+v, %v)", toggledSettings.Commanders, toggleErr)
	}
	toggleDomains := eventsRecorder.Domains()
	for _, expectedDomain := range []string{"ingest", "stats", "plan", "settings"} {
		if !slices.Contains(toggleDomains, expectedDomain) {
			testingContext.Fatalf("expected SetCommanderEnabled to emit %q domain, got %v", expectedDomain, toggleDomains)
		}
	}

	eddPath := eddtest.BuildMinimalArchive(testingContext, [][2]string{
		{"Fileheader", `{"timestamp":"2026-03-02T09:00:00Z","event":"Fileheader","part":1,"Odyssey":true}`},
		{"LoadGame", `{"timestamp":"2026-03-02T09:00:05Z","event":"LoadGame","Commander":"CmdrEDD","FID":"F999","Ship":"Anaconda"}`},
		{"FSDJump", `{"timestamp":"2026-03-02T09:05:00Z","event":"FSDJump","StarSystem":"Achenar","SystemAddress":2002,"StarPos":[67.5,-119.4,24.8],"JumpDist":139.4}`},
	})
	eventsRecorder.Reset()

	linesRead, importErr := testApp.ImportEDDiscoveryHistory(eddPath)
	if importErr != nil || linesRead != 3 {
		testingContext.Fatalf("ImportEDDiscoveryHistory = (%d, %v), want 3 lines", linesRead, importErr)
	}

	progressEvents := eventsRecorder.EDDProgress()
	sawActive := slices.ContainsFunc(progressEvents, func(item EDDImportProgress) bool { return item.Active })
	sawFinalInactive := len(progressEvents) > 0 && !progressEvents[len(progressEvents)-1].Active
	observedDomains := eventsRecorder.Domains()
	if !sawActive || !sawFinalInactive {
		testingContext.Fatalf("expected active and final inactive EDDImportProgress events, got %+v", progressEvents)
	}
	if !slices.Contains(observedDomains, "ingest") || !slices.Contains(observedDomains, "settings") {
		testingContext.Fatalf("expected ImportEDDiscoveryHistory to emit ingest and settings domains, got %v", observedDomains)
	}
	recovered, disconnectsDone, savedPath := settings.EDDRecoveryState(testApp.db)
	if !recovered || !disconnectsDone || savedPath != eddPath {
		testingContext.Fatalf("unexpected persisted EDDRecoveryState: (%v, %v, %q)", recovered, disconnectsDone, savedPath)
	}
}
