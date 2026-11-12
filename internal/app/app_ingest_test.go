package app

import (
	"os"
	"path/filepath"
	"testing"

	"planisphere/internal/db"
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
}

