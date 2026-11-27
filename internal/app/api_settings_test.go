package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"planisphere/internal/store/settings"
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
	watcherStarted := false
	for attempt := 0; attempt < 50; attempt++ {
		status, statusErr := testApp.GetIngestStatus()
		if statusErr != nil {
			testingContext.Fatalf("GetIngestStatus: %v", statusErr)
		}
		if status.WatcherActive {
			watcherStarted = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !watcherStarted {
		testingContext.Fatalf("expected live journal watcher to become active after SetupConfigureJournals")
	}
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
