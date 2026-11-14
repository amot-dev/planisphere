package journal_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/journal"
)

func openJournalTestDB(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatal(openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})
	return databaseConnection
}

func seedByteZeroBackfillFixture(testingContext *testing.T, logFilename, locationTimestamp, loadGameTimestamp string) (*sql.DB, *journal.Ingester, string) {
	testingContext.Helper()
	databaseConnection := openJournalTestDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	logFilePath := filepath.Join(temporaryDirectory, logFilename)
	earlyLine := `{"timestamp":"` + locationTimestamp + `","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`
	loadLine := `{"timestamp":"` + loadGameTimestamp + `","event":"LoadGame","Commander":"CmdrAlpha","FID":"F10","Ship":"mandalay"}`
	if writeError := os.WriteFile(logFilePath, []byte(earlyLine+"\n"+loadLine+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}

	earlyEvent, _, parseEarlyError := journal.ParseLine([]byte(earlyLine), logFilename)
	if parseEarlyError != nil {
		testingContext.Fatal(parseEarlyError)
	}
	loadEvent, _, parseLoadError := journal.ParseLine([]byte(loadLine), logFilename)
	if parseLoadError != nil {
		testingContext.Fatal(parseLoadError)
	}

	if _, execError := databaseConnection.Exec(
		`INSERT INTO ingested_journals (file_name, file_path, byte_offset, updated_at) VALUES (?, ?, 0, ?)`,
		logFilename, logFilePath, loadGameTimestamp,
	); execError != nil {
		testingContext.Fatal(execError)
	}
	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, commander, source_file, raw_json, ingested_at)
		 VALUES (?, ?, 'Location', 'Sol', 1001, NULL, ?, '{}', ?),
		        (?, ?, 'LoadGame', '', 0, 'cmdralpha', ?, '{}', ?)`,
		earlyEvent.UID, locationTimestamp, logFilename, loadGameTimestamp,
		loadEvent.UID, loadGameTimestamp, logFilename, loadGameTimestamp,
	); execError != nil {
		testingContext.Fatal(execError)
	}

	return databaseConnection, journal.NewIngester(databaseConnection), logFilePath
}

func TestIngestFile(t *testing.T) {
	conn := openJournalTestDB(t)

	temporaryDir := t.TempDir()
	logFile := filepath.Join(temporaryDir, "Journal.2026-01-18T190000.01.log")
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay"}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100],"JumpDist":4.3}`,
	}
	if err := os.WriteFile(logFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := journal.NewIngester(conn)
	scanResult, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if scanResult.NewEvents != 3 {
		t.Fatalf("expected 3 new events, got %d", scanResult.NewEvents)
	}
	if scanResult.LinesRead != 3 {
		t.Fatalf("expected 3 lines read, got %d", scanResult.LinesRead)
	}

	// Ingesting the same file again should produce 0 new events (idempotent)
	secondScanResult, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if secondScanResult.NewEvents != 0 {
		t.Fatalf("expected 0 new events on re-ingest, got %d", secondScanResult.NewEvents)
	}
}

func TestIngestFilePartialLine(t *testing.T) {
	conn := openJournalTestDB(t)

	temporaryDir := t.TempDir()
	logFile := filepath.Join(temporaryDir, "Journal.2026-01-18T200000.01.log")

	line1 := `{"timestamp":"2026-01-18T20:01:00Z","event":"LoadGame","Commander":"CmdrPartial","FID":"F2","Ship":"cobra"}` + "\n"
	line2 := `{"timestamp":"2026-01-18T20:02:00Z","event":"Location","StarSystem":"Sol","SystemAddress":2001,"StarPos":[0,0,0]}` + "\n"
	partialLine := `{"timestamp":"2026-01-18T20:03:00Z","event":"FSDJump","Star`

	if err := os.WriteFile(logFile, []byte(line1+line2+partialLine), 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := journal.NewIngester(conn)
	firstScanResult, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if firstScanResult.LinesRead != 2 {
		t.Fatalf("expected 2 complete lines read, got %d", firstScanResult.LinesRead)
	}
	if firstScanResult.NewEvents != 2 {
		t.Fatalf("expected 2 new events, got %d", firstScanResult.NewEvents)
	}

	var storedOffset int64
	name := filepath.Base(logFile)
	if err := conn.QueryRow("SELECT byte_offset FROM ingested_journals WHERE file_name = ?", name).Scan(&storedOffset); err != nil {
		t.Fatal(err)
	}
	expectedOffset := int64(len(line1) + len(line2))
	if storedOffset != expectedOffset {
		t.Fatalf("expected byte_offset %d, got %d", expectedOffset, storedOffset)
	}

	// Complete the third line
	remainingLine := `System":"Alpha Centauri","SystemAddress":2002,"StarPos":[0,0,100],"JumpDist":4.3}` + "\n"
	fileHandle, err := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileHandle.WriteString(remainingLine); err != nil {
		fileHandle.Close()
		t.Fatal(err)
	}
	fileHandle.Close()

	secondScanResult, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if secondScanResult.LinesRead != 1 {
		t.Fatalf("expected 1 line read on second pass, got %d", secondScanResult.LinesRead)
	}
	if secondScanResult.NewEvents != 1 {
		t.Fatalf("expected 1 new event on second pass, got %d", secondScanResult.NewEvents)
	}

	if err := conn.QueryRow("SELECT byte_offset FROM ingested_journals WHERE file_name = ?", name).Scan(&storedOffset); err != nil {
		t.Fatal(err)
	}
	expectedFinalOffset := expectedOffset + int64(len(partialLine)+len(remainingLine))
	if storedOffset != expectedFinalOffset {
		t.Fatalf("expected final byte_offset %d, got %d", expectedFinalOffset, storedOffset)
	}
}

func TestWatcherStartStopSynchronization(t *testing.T) {
	temporaryDirectoryOne := t.TempDir()
	temporaryDirectoryTwo := t.TempDir()

	watcherInstance := journal.NewWatcher(nil, nil)

	parentContext, cancelParentContext := context.WithCancel(context.Background())
	defer cancelParentContext()

	watcherInstance.Start(parentContext, temporaryDirectoryOne)
	watcherInstance.Start(parentContext, temporaryDirectoryTwo)

	time.Sleep(50 * time.Millisecond)

	isActive, watcherErrorMessage := watcherInstance.Active()
	if !isActive {
		t.Fatalf("expected watcher to remain active after sequential Start calls, got inactive with error: %s", watcherErrorMessage)
	}

	watcherInstance.Stop()

	isActiveAfterStop, _ := watcherInstance.Active()
	if isActiveAfterStop {
		t.Fatalf("expected watcher to be inactive after explicit Stop")
	}
}

func TestWatcherReconnectWhenDirectoryCreated(t *testing.T) {
	baseTemporaryDirectory := t.TempDir()
	targetJournalDirectory := filepath.Join(baseTemporaryDirectory, "journals")

	watcherInstance := journal.NewWatcher(nil, nil)

	parentContext, cancelParentContext := context.WithCancel(context.Background())
	defer cancelParentContext()

	watcherInstance.Start(parentContext, targetJournalDirectory)

	initiallyActive, _ := watcherInstance.Active()
	if initiallyActive {
		t.Fatalf("expected watcher to be initially inactive because directory does not exist")
	}

	if directoryCreationError := os.Mkdir(targetJournalDirectory, 0o755); directoryCreationError != nil {
		t.Fatalf("failed to create target journal directory: %v", directoryCreationError)
	}

	deadlineTimer := time.Now().Add(4 * time.Second)
	reconnectedSuccessfully := false
	for time.Now().Before(deadlineTimer) {
		activeStatus, _ := watcherInstance.Active()
		if activeStatus {
			reconnectedSuccessfully = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !reconnectedSuccessfully {
		t.Fatalf("expected watcher to automatically reconnect after directory was created")
	}

	watcherInstance.Stop()
}

func TestScanDir_ContextCancellation(t *testing.T) {
	temporaryDatabaseConnection := openJournalTestDB(t)

	temporaryDirectory := t.TempDir()
	journalFilePath := filepath.Join(temporaryDirectory, "Journal.2026-01-18T190000.01.log")
	journalContent := `{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay"}` + "\n"
	if writeError := os.WriteFile(journalFilePath, []byte(journalContent), 0o644); writeError != nil {
		t.Fatal(writeError)
	}

	ingesterInstance := journal.NewIngester(temporaryDatabaseConnection)

	cancelledContext, cancelFunction := context.WithCancel(context.Background())
	cancelFunction() // Cancel immediately

	_, scanError := ingesterInstance.ScanDir(cancelledContext, temporaryDirectory)
	if !errors.Is(scanError, context.Canceled) {
		t.Fatalf("expected context.Canceled error from ScanDir, got %v", scanError)
	}
}

func TestComputeUIDDeterministicAndConcurrent(t *testing.T) {
	initialUID := journal.ComputeUID("2026-01-18T19:05:00Z", "FSDJump", "1001", "Sol")
	if len(initialUID) != 64 {
		t.Fatalf("expected 64-char sha256 hex string, got %d (%s)", len(initialUID), initialUID)
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < 20; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for iteration := 0; iteration < 100; iteration++ {
				computedUID := journal.ComputeUID("2026-01-18T19:05:00Z", "FSDJump", "1001", "Sol")
				if computedUID != initialUID {
					t.Errorf("expected deterministic UID %s, got %s", initialUID, computedUID)
				}
			}
		}()
	}
	waitGroup.Wait()
}

func TestWatcherDebounceConfigurationAndBatching(t *testing.T) {
	temporaryDatabaseConnection := openJournalTestDB(t)

	temporaryJournalDirectory := t.TempDir()
	ingesterInstance := journal.NewIngester(temporaryDatabaseConnection)

	batchNotificationChan := make(chan journal.Result, 4)
	watcherInstance := journal.NewWatcher(ingesterInstance, func(batchResult journal.Result) {
		batchNotificationChan <- batchResult
	})

	if watcherInstance.DebounceDelay() != journal.DefaultDebounceDelay {
		t.Fatalf("expected initial debounce %v, got %v", journal.DefaultDebounceDelay, watcherInstance.DebounceDelay())
	}
	watcherInstance.SetDebounceDelay(90 * time.Second)
	if watcherInstance.DebounceDelay() != journal.MaxDebounceDelay {
		t.Fatalf("expected upper-clamped debounce %v, got %v", journal.MaxDebounceDelay, watcherInstance.DebounceDelay())
	}
	watcherInstance.SetDebounceDelay(50 * time.Millisecond)
	if watcherInstance.DebounceDelay() != journal.MinDebounceDelay {
		t.Fatalf("expected lower-clamped debounce %v, got %v", journal.MinDebounceDelay, watcherInstance.DebounceDelay())
	}

	parentContext, cancelParentContext := context.WithCancel(context.Background())
	defer cancelParentContext()
	watcherInstance.Start(parentContext, temporaryJournalDirectory)
	defer watcherInstance.Stop()

	activeDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(activeDeadline) {
		isActive, _ := watcherInstance.Active()
		if isActive {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	journalFilePath := filepath.Join(temporaryJournalDirectory, "Journal.2026-02-01T100000.01.log")
	firstLine := `{"timestamp":"2026-02-01T10:00:01Z","event":"Scan","StarSystem":"Sol","SystemAddress":10477373803,"BodyName":"Sol A 1","BodyID":1,"PlanetClass":"High metal content body"}` + "\n"
	secondLine := `{"timestamp":"2026-02-01T10:00:02Z","event":"Scan","StarSystem":"Sol","SystemAddress":10477373803,"BodyName":"Sol A 2","BodyID":2,"PlanetClass":"Rocky body"}` + "\n"

	fileHandle, openError := os.OpenFile(journalFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openError != nil {
		t.Fatalf("open journal file: %v", openError)
	}
	if _, writeError := fileHandle.WriteString(firstLine); writeError != nil {
		_ = fileHandle.Close()
		t.Fatalf("write first scan line: %v", writeError)
	}
	time.Sleep(50 * time.Millisecond)
	if _, writeError := fileHandle.WriteString(secondLine); writeError != nil {
		_ = fileHandle.Close()
		t.Fatalf("write second scan line: %v", writeError)
	}
	if closeError := fileHandle.Close(); closeError != nil {
		t.Fatalf("close journal file: %v", closeError)
	}

	select {
	case coalescedBatch := <-batchNotificationChan:
		if coalescedBatch.NewEvents != 2 {
			t.Fatalf("expected 2 coalesced events in single debounced batch, got %d", coalescedBatch.NewEvents)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for debounced watcher batch")
	}
}

func TestReingestFromByteZeroBackfillsUnattributedEvents(t *testing.T) {
	conn, ingester, logFile := seedByteZeroBackfillFixture(
		t,
		"Journal.2026-03-01T120000.01.log",
		"2026-03-01T12:00:00Z",
		"2026-03-01T12:01:00Z",
	)

	if _, ingestError := ingester.IngestFile(logFile); ingestError != nil {
		t.Fatalf("ingest file: %v", ingestError)
	}

	var commanderName string
	if queryError := conn.QueryRow(`SELECT COALESCE(commander, '') FROM journal_events WHERE event = 'Location'`).Scan(&commanderName); queryError != nil {
		t.Fatal(queryError)
	}
	if commanderName != "CmdrAlpha" {
		t.Fatalf("expected unattributed early event to be backfilled to CmdrAlpha, got %q", commanderName)
	}
}

func TestArchiveStatusPropagatesDisabledCommanderQueryError(t *testing.T) {
	conn := openJournalTestDB(t)

	if _, dropError := conn.Exec(`DROP TABLE commanders`); dropError != nil {
		t.Fatal(dropError)
	}

	_, statusError := journal.ArchiveStatus(conn)
	if statusError == nil {
		t.Fatal("expected ArchiveStatus to fail when commanders table is missing")
	}
	if !strings.Contains(statusError.Error(), "check disabled commanders") {
		t.Fatalf("expected error wrapping check disabled commanders, got %v", statusError)
	}
}

func TestListCommandersEventCountCachingAndBackfillInvalidation(t *testing.T) {
	conn, ingester, logFilePath := seedByteZeroBackfillFixture(
		t,
		"Journal.2026-04-01T120000.01.log",
		"2026-04-01T12:00:00Z",
		"2026-04-01T12:01:00Z",
	)

	if _, execError := conn.Exec(
		`INSERT INTO commanders (name, fid, enabled, first_seen, last_seen)
		 VALUES ('CmdrAlpha', 'F10', 1, '2026-04-01T12:00:00Z', '2026-04-01T12:01:00Z'),
		        ('CmdrBeta', 'F20', 1, '2026-04-02T12:00:00Z', '2026-04-02T12:00:00Z')`,
	); execError != nil {
		t.Fatal(execError)
	}
	if _, execError := conn.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, commander, source_file, raw_json, ingested_at)
		 VALUES ('uid-beta', '2026-04-02T12:00:00Z', 'FSDJump', 'Achenar', 1002, 'CmdrBeta', 'other.log', '{}', '2026-04-02T12:00:00Z')`,
	); execError != nil {
		t.Fatal(execError)
	}

	assertCounts := func(expectedAlpha, expectedBeta int) {
		t.Helper()
		commanders, listError := journal.ListCommanders(conn)
		if listError != nil {
			t.Fatalf("ListCommanders: %v", listError)
		}
		if len(commanders) != 2 {
			t.Fatalf("expected 2 commanders, got %d", len(commanders))
		}
		if commanders[0].Name != "CmdrAlpha" || commanders[0].EventCount != expectedAlpha {
			t.Fatalf("expected CmdrAlpha eventCount=%d, got %+v", expectedAlpha, commanders[0])
		}
		if commanders[1].Name != "CmdrBeta" || commanders[1].EventCount != expectedBeta {
			t.Fatalf("expected CmdrBeta eventCount=%d, got %+v", expectedBeta, commanders[1])
		}
	}

	// Populate and hit the commander counts cache before backfill.
	assertCounts(1, 1)
	assertCounts(1, 1)

	if _, ingestError := ingester.IngestFile(logFilePath); ingestError != nil {
		t.Fatalf("IngestFile: %v", ingestError)
	}

	// MAX(id) did not increase (existing UIDs), so cache invalidation must surface the backfill.
	assertCounts(2, 1)
}
