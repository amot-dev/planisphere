package journal_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
)

func waitForActiveWatcher(testingContext *testing.T, watcherInstance *journal.Watcher) {
	testingContext.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErrorMessage string
	for time.Now().Before(deadline) {
		isActive, watcherError := watcherInstance.Active()
		if isActive {
			return
		}
		lastErrorMessage = watcherError
		time.Sleep(15 * time.Millisecond)
	}
	testingContext.Fatalf("timed out waiting for journal watcher to become active (last error: %s)", lastErrorMessage)
}

func TestWatcherPauseCoalescesPendingFilesInChronologicalOrder(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journalDirectory := testingContext.TempDir()
	ingesterInstance := journal.NewIngester(databaseConnection)

	batchChannel := make(chan journal.Result, 4)
	pendingSignalChannel := make(chan struct{}, 4)
	var pendingCallbackCount atomic.Int32

	watcherInstance := journal.NewWatcher(ingesterInstance, func(batchResult journal.Result) {
		batchChannel <- batchResult
	})
	watcherInstance.SetDebounceDelay(reference.MinDebounceDelay)
	watcherInstance.SetOnPending(func() {
		pendingCallbackCount.Add(1)
		pendingSignalChannel <- struct{}{}
	})
	watcherInstance.SetPaused(true)
	if !watcherInstance.Paused() {
		testingContext.Fatalf("expected watcher.Paused() to report true")
	}

	parentContext, cancelContext := context.WithCancel(context.Background())
	defer cancelContext()
	watcherInstance.Start(parentContext, journalDirectory)
	testingContext.Cleanup(watcherInstance.Stop)
	waitForActiveWatcher(testingContext, watcherInstance)

	laterFile := filepath.Join(journalDirectory, "Journal.2026-04-01T110000.01.log")
	earlierFile := filepath.Join(journalDirectory, "Journal.2026-04-01T100000.01.log")
	earlierContent := `{"timestamp":"2026-04-01T10:00:00Z","event":"LoadGame","Commander":"CmdrOrder","FID":"F99","Ship":"mandalay"}` + "\n"
	laterFirstLine := `{"timestamp":"2026-04-01T11:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}` + "\n"
	laterSecondLine := `{"timestamp":"2026-04-01T11:05:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":2832631665362,"StarPos":[-3.03125,1.375,-4.9375],"JumpDist":5.95}` + "\n"

	if writeError := os.WriteFile(laterFile, []byte(laterFirstLine), 0o644); writeError != nil {
		testingContext.Fatalf("write later journal file: %v", writeError)
	}

	select {
	case <-pendingSignalChannel:
	case <-time.After(2 * time.Second):
		testingContext.Fatalf("timed out waiting for onPending callback while watcher is paused")
	}

	// Write a second file with an earlier timestamp and append to the later file while still paused.
	if writeError := os.WriteFile(earlierFile, []byte(earlierContent), 0o644); writeError != nil {
		testingContext.Fatalf("write earlier journal file: %v", writeError)
	}
	if writeError := os.WriteFile(laterFile, []byte(laterFirstLine+laterSecondLine), 0o644); writeError != nil {
		testingContext.Fatalf("append later journal file: %v", writeError)
	}
	time.Sleep(reference.MinDebounceDelay + 120*time.Millisecond)

	if gotPendingCalls := pendingCallbackCount.Load(); gotPendingCalls != 1 {
		testingContext.Fatalf("expected onPending to fire once across paused writes, got %d", gotPendingCalls)
	}
	select {
	case unexpectedBatch := <-batchChannel:
		testingContext.Fatalf("unexpected batch flush while paused: %+v", unexpectedBatch)
	default:
	}

	var storedCountWhilePaused int
	if queryError := databaseConnection.QueryRow(`SELECT COUNT(*) FROM journal_events`).Scan(&storedCountWhilePaused); queryError != nil {
		testingContext.Fatalf("query journal_events while paused: %v", queryError)
	}
	if storedCountWhilePaused != 0 {
		testingContext.Fatalf("expected 0 ingested events in SQLite while paused, got %d", storedCountWhilePaused)
	}

	watcherInstance.SetPaused(false)
	select {
	case resumedBatch := <-batchChannel:
		if resumedBatch.NewEvents != 3 {
			testingContext.Fatalf("expected 3 events flushed on resume, got %d", resumedBatch.NewEvents)
		}
	case <-time.After(2 * time.Second):
		testingContext.Fatalf("timed out waiting for paused files to flush on SetPaused(false)")
	}

	rows, rowsError := databaseConnection.Query(`SELECT file_name FROM ingested_journals ORDER BY rowid ASC`)
	if rowsError != nil {
		testingContext.Fatalf("query ingested_journals order: %v", rowsError)
	}
	defer rows.Close()
	var ingestedFileNames []string
	for rows.Next() {
		var fileName string
		if scanError := rows.Scan(&fileName); scanError != nil {
			testingContext.Fatalf("scan file_name: %v", scanError)
		}
		ingestedFileNames = append(ingestedFileNames, fileName)
	}
	if rowsError := rows.Err(); rowsError != nil {
		testingContext.Fatalf("iterate ingested_journals: %v", rowsError)
	}
	expectedOrder := []string{
		"Journal.2026-04-01T100000.01.log",
		"Journal.2026-04-01T110000.01.log",
	}
	if !slices.Equal(ingestedFileNames, expectedOrder) {
		testingContext.Fatalf("expected chronological file ingestion order %v, got %v", expectedOrder, ingestedFileNames)
	}
}

func TestWatcherClearPendingSuppressesStaleFlushOnResume(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journalDirectory := testingContext.TempDir()
	ingesterInstance := journal.NewIngester(databaseConnection)

	batchChannel := make(chan journal.Result, 4)
	pendingSignalChannel := make(chan struct{}, 4)

	watcherInstance := journal.NewWatcher(ingesterInstance, func(batchResult journal.Result) {
		batchChannel <- batchResult
	})
	watcherInstance.SetDebounceDelay(reference.MinDebounceDelay)
	watcherInstance.SetOnPending(func() {
		pendingSignalChannel <- struct{}{}
	})
	watcherInstance.SetPaused(true)

	parentContext, cancelContext := context.WithCancel(context.Background())
	defer cancelContext()
	watcherInstance.Start(parentContext, journalDirectory)
	testingContext.Cleanup(watcherInstance.Stop)
	waitForActiveWatcher(testingContext, watcherInstance)

	journalFile := filepath.Join(journalDirectory, "Journal.2026-04-02T080000.01.log")
	lineOne := `{"timestamp":"2026-04-02T08:00:00Z","event":"LoadGame","Commander":"CmdrClear","FID":"F77","Ship":"cobra"}` + "\n"
	if writeError := os.WriteFile(journalFile, []byte(lineOne), 0o644); writeError != nil {
		testingContext.Fatalf("write journal file: %v", writeError)
	}

	select {
	case <-pendingSignalChannel:
	case <-time.After(2 * time.Second):
		testingContext.Fatalf("timed out waiting for initial onPending signal")
	}

	watcherInstance.ClearPending()
	if _, scanError := ingesterInstance.ScanDir(parentContext, journalDirectory); scanError != nil {
		testingContext.Fatalf("ScanDir: %v", scanError)
	}

	// Subsequent write while still paused must re-arm and fire onPending again after ClearPending.
	lineTwo := `{"timestamp":"2026-04-02T08:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}` + "\n"
	if writeError := os.WriteFile(journalFile, []byte(lineOne+lineTwo), 0o644); writeError != nil {
		testingContext.Fatalf("append journal file after ClearPending: %v", writeError)
	}
	select {
	case <-pendingSignalChannel:
	case <-time.After(2 * time.Second):
		testingContext.Fatalf("expected onPending to fire again for writes after ClearPending")
	}

	watcherInstance.ClearPending()
	if _, scanError := ingesterInstance.ScanDir(parentContext, journalDirectory); scanError != nil {
		testingContext.Fatalf("second ScanDir: %v", scanError)
	}
	watcherInstance.SetPaused(false)
	time.Sleep(reference.MinDebounceDelay + 80*time.Millisecond)

	select {
	case unexpectedBatch := <-batchChannel:
		testingContext.Fatalf("expected no stale batch after ClearPending and ScanDir, got %+v", unexpectedBatch)
	default:
	}
}

