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

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
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
	if firstScanResult.EarliestTimestamp != "2026-01-18T20:01:00Z" {
		t.Fatalf("expected first EarliestTimestamp 2026-01-18T20:01:00Z, got %q", firstScanResult.EarliestTimestamp)
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
	if secondScanResult.EarliestTimestamp != "2026-01-18T20:03:00Z" {
		t.Fatalf("expected second EarliestTimestamp 2026-01-18T20:03:00Z, got %q", secondScanResult.EarliestTimestamp)
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
	t.Cleanup(watcherInstance.Stop)
	watcherInstance.Start(parentContext, temporaryDirectoryTwo)
	t.Cleanup(watcherInstance.Stop)

	waitForActiveWatcher(t, watcherInstance)

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
	t.Cleanup(watcherInstance.Stop)

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

	if watcherInstance.DebounceDelay() != reference.DefaultDebounceDelay {
		t.Fatalf("expected initial debounce %v, got %v", reference.DefaultDebounceDelay, watcherInstance.DebounceDelay())
	}
	watcherInstance.SetDebounceDelay(90 * time.Second)
	if watcherInstance.DebounceDelay() != reference.MaxDebounceDelay {
		t.Fatalf("expected upper-clamped debounce %v, got %v", reference.MaxDebounceDelay, watcherInstance.DebounceDelay())
	}
	watcherInstance.SetDebounceDelay(50 * time.Millisecond)
	if watcherInstance.DebounceDelay() != reference.MinDebounceDelay {
		t.Fatalf("expected lower-clamped debounce %v, got %v", reference.MinDebounceDelay, watcherInstance.DebounceDelay())
	}

	parentContext, cancelParentContext := context.WithCancel(context.Background())
	defer cancelParentContext()
	watcherInstance.Start(parentContext, temporaryJournalDirectory)
	t.Cleanup(watcherInstance.Stop)

	waitForActiveWatcher(t, watcherInstance)

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

func TestParseLinePreJumpFilters(testingContext *testing.T) {
	testCases := []struct {
		name     string
		rawLine  string
		wantKeep bool
	}{
		{
			name:     "hyperspace StartJump kept",
			rawLine:  `{"timestamp":"2026-11-17T01:40:45Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321}`,
			wantKeep: true,
		},
		{
			name:     "supercruise StartJump ignored",
			rawLine:  `{"timestamp":"2026-11-17T01:41:00Z","event":"StartJump","JumpType":"Supercruise"}`,
			wantKeep: false,
		},
		{
			name:     "JetConeBoost kept",
			rawLine:  `{"timestamp":"2026-11-17T01:39:00Z","event":"JetConeBoost","BoostValue":4.0}`,
			wantKeep: true,
		},
		{
			name:     "FSD synthesis kept",
			rawLine:  `{"timestamp":"2026-11-17T01:39:10Z","event":"Synthesis","Name":"FSD Premium"}`,
			wantKeep: true,
		},
		{
			name:     "non-FSD synthesis ignored",
			rawLine:  `{"timestamp":"2026-11-17T01:39:20Z","event":"Synthesis","Name":"Limpet Basic"}`,
			wantKeep: false,
		},
	}

	for _, testCase := range testCases {
		_, gotKeep, parseError := journal.ParseLine([]byte(testCase.rawLine), "Journal.test.log")
		if parseError != nil {
			testingContext.Fatalf("%s: unexpected ParseLine error: %v", testCase.name, parseError)
		}
		if gotKeep != testCase.wantKeep {
			testingContext.Errorf("%s: got keep=%v, want %v", testCase.name, gotKeep, testCase.wantKeep)
		}
	}
}

func TestIngestMidFileClearSavedGameAndNewCommander(testingContext *testing.T) {
	conn := openJournalTestDB(testingContext)
	tempDir := testingContext.TempDir()
	logPath := filepath.Join(tempDir, "Journal.2026-04-10T120000.01.log")
	preWipeLines := []string{
		`{"timestamp":"2026-04-10T12:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-04-10T12:00:01Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-10T12:00:02Z","event":"LoadGame","Commander":"CmdrPreWipe","FID":"F42","Ship":"SideWinder","Odyssey":true}`,
		`{"timestamp":"2026-04-10T12:05:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1002,"StarPos":[1,2,3],"JumpDist":5.9}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(preWipeLines, "\n")+"\n"), 0o644); err != nil {
		testingContext.Fatal(err)
	}

	ingester := journal.NewIngester(conn)
	if _, err := ingester.IngestFile(logPath); err != nil {
		testingContext.Fatalf("initial IngestFile: %v", err)
	}
	var initialArchived, initialEnabled int
	if err := conn.QueryRow(`SELECT is_archived_career, enabled FROM commander_profiles WHERE profile_key = 'live:f42:era:0'`).Scan(&initialArchived, &initialEnabled); err != nil {
		testingContext.Fatalf("query initial era 0 state: %v", err)
	}
	if initialArchived != 0 || initialEnabled != 1 {
		testingContext.Fatalf("expected initial live:f42:era:0 to have archived=0 enabled=1, got archived=%d enabled=%d", initialArchived, initialEnabled)
	}

	postWipeLines := []string{
		`{"timestamp":"2026-04-10T12:10:00Z","event":"ClearSavedGame","Name":"CmdrPreWipe","FID":"F42"}`,
		`{"timestamp":"2026-04-10T12:10:05Z","event":"NewCommander","Name":"CmdrPostWipe","FID":"F42","Package":"Sidewinder"}`,
		`{"timestamp":"2026-04-10T12:10:10Z","event":"LoadGame","Commander":"CmdrPostWipe","FID":"F42","Ship":"SideWinder","Odyssey":true}`,
		`{"timestamp":"2026-04-10T12:15:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1003,"StarPos":[2,3,4],"JumpDist":4.3}`,
	}
	appendHandle, openErr := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		testingContext.Fatal(openErr)
	}
	if _, writeErr := appendHandle.WriteString(strings.Join(postWipeLines, "\n") + "\n"); writeErr != nil {
		_ = appendHandle.Close()
		testingContext.Fatal(writeErr)
	}
	_ = appendHandle.Close()

	if _, err := ingester.IngestFile(logPath); err != nil {
		testingContext.Fatalf("incremental IngestFile after wipe: %v", err)
	}

	rows, err := conn.Query(`
		SELECT je.event, COALESCE(je.commander, ''), je.fid, cp.profile_key, cp.is_archived_career, cp.enabled
		FROM journal_events je
		JOIN commander_profiles cp ON cp.id = je.profile_id
		ORDER BY je.timestamp ASC, je.id ASC`)
	if err != nil {
		testingContext.Fatal(err)
	}
	defer rows.Close()

	type wantEventRow struct {
		event      string
		commander  string
		profileKey string
		archived   int
		enabled    int
	}
	wantRows := []wantEventRow{
		{"Location", "CmdrPreWipe", "live:f42:era:0", 1, 0},
		{"LoadGame", "CmdrPreWipe", "live:f42:era:0", 1, 0},
		{"FSDJump", "CmdrPreWipe", "live:f42:era:0", 1, 0},
		{"ClearSavedGame", "CmdrPreWipe", "live:f42:era:0", 1, 0},
		{"NewCommander", "CmdrPostWipe", "live:f42:era:1", 0, 1},
		{"LoadGame", "CmdrPostWipe", "live:f42:era:1", 0, 1},
		{"FSDJump", "CmdrPostWipe", "live:f42:era:1", 0, 1},
	}

	rowIndex := 0
	for rows.Next() {
		var eventName, cmdr, fid, key string
		var archived, enabled int
		if scanErr := rows.Scan(&eventName, &cmdr, &fid, &key, &archived, &enabled); scanErr != nil {
			testingContext.Fatal(scanErr)
		}
		if rowIndex >= len(wantRows) {
			testingContext.Fatalf("extra row %s (%s)", eventName, cmdr)
		}
		want := wantRows[rowIndex]
		if eventName != want.event || cmdr != want.commander || fid != "F42" || key != want.profileKey || archived != want.archived || enabled != want.enabled {
			testingContext.Errorf("row %d = (%s, %s, %s, %s, arch=%d, en=%d), want %+v",
				rowIndex, eventName, cmdr, fid, key, archived, enabled, want)
		}
		rowIndex++
	}
	if rowsError := rows.Err(); rowsError != nil {
		testingContext.Fatalf("iterate journal_events: %v", rowsError)
	}
	if rowIndex != len(wantRows) {
		testingContext.Fatalf("got %d rows, want %d", rowIndex, len(wantRows))
	}
}

func TestIngestContinuedRolloverInheritsIdentityAndProfile(testingContext *testing.T) {
	conn := openJournalTestDB(testingContext)
	tempDir := testingContext.TempDir()

	// Seed BOTH a newer 2026 Live rollover session (which sorts BEFORE Journal.23... in ASCII)
	// and an older 2023 Legacy 2-digit-year rollover session so ScanDir verifies timestamp-bounded
	// rollover resolution and universe preservation.
	live2026PartOne := filepath.Join(tempDir, "Journal.2026-04-12T180000.01.log")
	live2026PartTwo := filepath.Join(tempDir, "Journal.2026-04-12T200000.02.log")
	legacy2023PartOne := filepath.Join(tempDir, "Journal.230501100000.01.log")
	legacy2023PartTwo := filepath.Join(tempDir, "Journal.230501120000.02.log")

	filesToWrite := map[string][]string{
		live2026PartOne: {
			`{"timestamp":"2026-04-12T18:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-04-12T18:01:00Z","event":"LoadGame","Commander":"CmdrRollover","FID":"F707","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-04-12T19:59:59Z","event":"Continued","Part":2}`,
		},
		live2026PartTwo: {
			`{"timestamp":"2026-04-12T20:00:00Z","event":"Fileheader","part":2,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-04-12T20:05:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":5001,"StarPos":[10,20,30],"JumpDist":65.2}`,
		},
		legacy2023PartOne: {
			`{"timestamp":"2023-05-01T10:00:00Z","event":"Fileheader","part":1,"gameversion":"3.8.0.404","build":"r280100/r0 "}`,
			`{"timestamp":"2023-05-01T10:01:00Z","event":"LoadGame","Commander":"CmdrLegacyRoll","FID":"F308","Ship":"Asp"}`,
			`{"timestamp":"2023-05-01T11:59:59Z","event":"Continued","Part":2}`,
		},
		legacy2023PartTwo: {
			`{"timestamp":"2023-05-01T12:00:00Z","event":"Fileheader","part":2}`,
			`{"timestamp":"2023-05-01T12:05:00Z","event":"FSDJump","StarSystem":"Sagittarius A*","SystemAddress":6001,"StarPos":[25,0,25900],"JumpDist":35.0}`,
		},
	}
	for filePath, lines := range filesToWrite {
		if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			testingContext.Fatal(err)
		}
	}

	ingester := journal.NewIngester(conn)
	if _, err := ingester.ScanDir(context.Background(), tempDir); err != nil {
		testingContext.Fatalf("ScanDir: %v", err)
	}

	assertJump := func(starSystem, wantCmdr, wantFID, wantUniverse, wantKey string) {
		testingContext.Helper()
		var gotCmdr, gotFID, gotUniverse, gotKey string
		if err := conn.QueryRow(`
			SELECT COALESCE(je.commander, ''), je.fid, je.universe, cp.profile_key
			FROM journal_events je
			JOIN commander_profiles cp ON cp.id = je.profile_id
			WHERE je.event = 'FSDJump' AND je.system_name = ?`, starSystem,
		).Scan(&gotCmdr, &gotFID, &gotUniverse, &gotKey); err != nil {
			testingContext.Fatalf("query FSDJump %s: %v", starSystem, err)
		}
		if gotCmdr != wantCmdr || gotFID != wantFID || gotUniverse != wantUniverse || gotKey != wantKey {
			testingContext.Fatalf("%s attribution = (%q, %q, %q, %q), want (%q, %q, %q, %q)",
				starSystem, gotCmdr, gotFID, gotUniverse, gotKey, wantCmdr, wantFID, wantUniverse, wantKey)
		}
	}
	assertJump("Colonia", "CmdrRollover", "F707", "live", "live:f707:era:0")
	assertJump("Sagittarius A*", "CmdrLegacyRoll", "F308", "legacy", "legacy:f308:era:0")
}
