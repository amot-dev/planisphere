package journal_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func seedByteZeroBackfillFixture(testingContext *testing.T, logFilename, locationTimestamp, loadGameTimestamp string) (*sql.DB, *journal.Ingester, string) {
	testingContext.Helper()
	databaseConnection := dbtest.OpenMemory(testingContext)
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
	conn := dbtest.OpenMemory(t)

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
	conn := dbtest.OpenMemory(t)

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

func TestScanDir_ContextCancellation(t *testing.T) {
	temporaryDatabaseConnection := dbtest.OpenMemory(t)

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

func TestReingestFromByteZeroBackfillsUnattributedEvents(t *testing.T) {
	conn, ingester, logFile := seedByteZeroBackfillFixture(
		t,
		"Journal.2026-03-01T120000.01.log",
		"2026-03-01T12:00:00Z",
		"2026-03-01T12:01:00Z",
	)

	reingestResult, ingestError := ingester.IngestFile(logFile)
	if ingestError != nil {
		t.Fatalf("ingest file: %v", ingestError)
	}
	if reingestResult.NewEvents != 0 || reingestResult.CommandersBackfilled == 0 || !reingestResult.ProfilesChanged {
		t.Fatalf("expected NewEvents=0 CommandersBackfilled>0 ProfilesChanged=true, got %+v", reingestResult)
	}

	var commanderName string
	if queryError := conn.QueryRow(`SELECT COALESCE(commander, '') FROM journal_events WHERE event = 'Location'`).Scan(&commanderName); queryError != nil {
		t.Fatal(queryError)
	}
	if commanderName != "CmdrAlpha" {
		t.Fatalf("expected unattributed early event to be backfilled to CmdrAlpha, got %q", commanderName)
	}
}

func TestIngestMidFileClearSavedGameAndNewCommander(testingContext *testing.T) {
	conn := dbtest.OpenMemory(testingContext)
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
	conn := dbtest.OpenMemory(testingContext)

	// Seed BOTH a newer 2026 Live rollover session (which sorts BEFORE Journal.23... in ASCII)
	// and an older 2023 Legacy 2-digit-year rollover session so ScanDir verifies timestamp-bounded
	// rollover resolution and universe preservation.
	journaltest.IngestFiles(testingContext, conn, map[string][]string{
		"Journal.2026-04-12T180000.01.log": {
			`{"timestamp":"2026-04-12T18:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-04-12T18:01:00Z","event":"LoadGame","Commander":"CmdrRollover","FID":"F707","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-04-12T19:59:59Z","event":"Continued","Part":2}`,
		},
		"Journal.2026-04-12T200000.02.log": {
			`{"timestamp":"2026-04-12T20:00:00Z","event":"Fileheader","part":2,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-04-12T20:05:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":5001,"StarPos":[10,20,30],"JumpDist":65.2}`,
		},
		"Journal.230501100000.01.log": {
			`{"timestamp":"2023-05-01T10:00:00Z","event":"Fileheader","part":1,"gameversion":"3.8.0.404","build":"r280100/r0 "}`,
			`{"timestamp":"2023-05-01T10:01:00Z","event":"LoadGame","Commander":"CmdrLegacyRoll","FID":"F308","Ship":"Asp"}`,
			`{"timestamp":"2023-05-01T11:59:59Z","event":"Continued","Part":2}`,
		},
		"Journal.230501120000.02.log": {
			`{"timestamp":"2023-05-01T12:00:00Z","event":"Fileheader","part":2}`,
			`{"timestamp":"2023-05-01T12:05:00Z","event":"FSDJump","StarSystem":"Sagittarius A*","SystemAddress":6001,"StarPos":[25,0,25900],"JumpDist":35.0}`,
		},
	})

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

func appendJournalLines(testingContext *testing.T, logPath string, lines ...string) {
	testingContext.Helper()
	handle, openErr := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if openErr != nil {
		testingContext.Fatal(openErr)
	}
	if _, writeErr := handle.WriteString(strings.Join(lines, "\n") + "\n"); writeErr != nil {
		_ = handle.Close()
		testingContext.Fatal(writeErr)
	}
	if closeErr := handle.Close(); closeErr != nil {
		testingContext.Fatal(closeErr)
	}
}

func snapshotCommanderTablesState(testingContext *testing.T, conn *sql.DB) string {
	testingContext.Helper()
	var builder strings.Builder
	queries := []string{
		`SELECT id, event, COALESCE(commander, ''), fid, universe, profile_id FROM journal_events ORDER BY id ASC`,
		`SELECT id, profile_key, display_name, fid, universe, era_index, is_archived_career, has_manual_override, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '') FROM commander_profiles ORDER BY id ASC`,
		`SELECT profile_id, raw_name, fid, universe, start_timestamp, end_timestamp, event_count, is_current, source FROM commander_aliases ORDER BY profile_id ASC, raw_name ASC`,
		`SELECT name, fid, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '') FROM commanders ORDER BY name ASC`,
	}
	for queryIdx, sqlQuery := range queries {
		rows, err := conn.Query(sqlQuery)
		if err != nil {
			testingContext.Fatalf("snapshot query %d: %v", queryIdx, err)
		}
		cols, _ := rows.Columns()
		rawVals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for colIdx := range rawVals {
			ptrs[colIdx] = &rawVals[colIdx]
		}
		for rows.Next() {
			if scanErr := rows.Scan(ptrs...); scanErr != nil {
				_ = rows.Close()
				testingContext.Fatal(scanErr)
			}
			for colIdx, val := range rawVals {
				if colIdx > 0 {
					builder.WriteByte('|')
				}
				if byteSlice, ok := val.([]byte); ok {
					builder.Write(byteSlice)
				} else {
					builder.WriteString(fmt.Sprint(val))
				}
			}
			builder.WriteByte('\n')
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			testingContext.Fatal(rowsErr)
		}
		_ = rows.Close()
		builder.WriteString("---\n")
	}
	return builder.String()
}

func connRunResolveChanged(conn *sql.DB, changed *bool) error {
	return db.WithTx(conn, func(transaction *sql.Tx) error {
		var err error
		*changed, err = db.ResolveCommandersChanged(transaction)
		return err
	})
}

func TestIngestFileLiveAppendFastPathEquivalenceAndFallbacks(testingContext *testing.T) {
	conn := dbtest.OpenMemory(testingContext)
	tempDir := testingContext.TempDir()
	logPath := filepath.Join(tempDir, "Journal.2026-05-01T100000.01.log")
	ingester := journal.NewIngester(conn)

	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:00:01Z","event":"Commander","Name":"CmdrFast","FID":"F500"}`,
		`{"timestamp":"2026-05-01T10:00:02Z","event":"LoadGame","Commander":"CmdrFast","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:00:03Z","event":"Statistics","Exploration":{"Systems_Visited":80,"Total_Hyperspace_Jumps":75}}`,
		`{"timestamp":"2026-05-01T10:00:04Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
	)
	initialRes, err := ingester.IngestFile(logPath)
	if err != nil || initialRes.NewEvents != 4 || !initialRes.ProfilesChanged {
		testingContext.Fatalf("initial IngestFile = (%+v, %v), want NewEvents=4 ProfilesChanged=true", initialRes, err)
	}

	// Single-event FSDJump and Scan live appends hit the O(added) fast path and match full ResolveCommanders across all 4 tables.
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,2,3],"JumpDist":4.3}`,
	)
	jumpRes, err := ingester.IngestFile(logPath)
	if err != nil || jumpRes.NewEvents != 1 || jumpRes.CommandersBackfilled != 0 || jumpRes.ProfilesChanged {
		testingContext.Fatalf("live FSDJump IngestFile = (%+v, %v)", jumpRes, err)
	}
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:06:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Alpha Centauri A 1","BodyID":1,"StarSystem":"Alpha Centauri","SystemAddress":1002,"PlanetClass":"Rocky body"}`,
	)
	scanRes, err := ingester.IngestFile(logPath)
	if err != nil || scanRes.NewEvents != 1 || scanRes.CommandersBackfilled != 0 || scanRes.ProfilesChanged {
		testingContext.Fatalf("live Scan IngestFile = (%+v, %v)", scanRes, err)
	}

	stateAfterFastPath := snapshotCommanderTablesState(testingContext, conn)
	var changedOnFullVerify bool
	if verifyErr := connRunResolveChanged(conn, &changedOnFullVerify); verifyErr != nil {
		testingContext.Fatalf("full ResolveCommandersChanged verification: %v", verifyErr)
	}
	stateAfterFullResolve := snapshotCommanderTablesState(testingContext, conn)
	if changedOnFullVerify || stateAfterFastPath != stateAfterFullResolve {
		testingContext.Fatalf("fast-path DB state diverged from full ResolveCommanders:\nfast:\n%s\nfull:\n%s", stateAfterFastPath, stateAfterFullResolve)
	}

	// Verify TryResolveLiveAppend directly reports matched=true on valid tip append and matched=false with an unbound expedition.
	probeTx, beginErr := conn.Begin()
	if beginErr != nil {
		testingContext.Fatal(beginErr)
	}
	probeInsert, insErr := probeTx.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, raw_json, ingested_at) VALUES ('probe-1', '2026-05-01T10:07:00Z', 'Scan', 'CmdrFast', 'F500', 'live', '{}', '2026-05-01T10:07:00Z')`,
	)
	if insErr != nil {
		_ = probeTx.Rollback()
		testingContext.Fatal(insErr)
	}
	probeID, _ := probeInsert.LastInsertId()
	var probeBatch db.LiveAppendBatch
	probeBatch.ObserveInserted(probeID, "Scan", "CmdrFast", "F500", "live", "2026-05-01T10:07:00Z")
	if matched, fastErr := db.TryResolveLiveAppend(probeTx, probeBatch); fastErr != nil || !matched {
		_ = probeTx.Rollback()
		testingContext.Fatalf("expected TryResolveLiveAppend matched=true, got (%v, %v)", matched, fastErr)
	}
	if _, expErr := probeTx.Exec(`INSERT INTO expeditions (name, status, commander_profile_id, created_at, updated_at) VALUES ('Unbound', 'planning', 0, '2026-05-01T10:07:00Z', '2026-05-01T10:07:00Z')`); expErr != nil {
		_ = probeTx.Rollback()
		testingContext.Fatal(expErr)
	}
	if matchedUnbound, fastErr := db.TryResolveLiveAppend(probeTx, probeBatch); fastErr != nil || matchedUnbound {
		_ = probeTx.Rollback()
		testingContext.Fatalf("expected TryResolveLiveAppend matched=false with unbound expedition, got (%v, %v)", matchedUnbound, fastErr)
	}
	_ = probeTx.Rollback()

	// Routine non-splitting Statistics falls back cleanly without flagging ProfilesChanged.
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:10:00Z","event":"Statistics","Exploration":{"Systems_Visited":82,"Total_Hyperspace_Jumps":76}}`,
	)
	routineStatsRes, err := ingester.IngestFile(logPath)
	if err != nil || routineStatsRes.NewEvents != 1 || routineStatsRes.ProfilesChanged {
		testingContext.Fatalf("routine Statistics IngestFile = (%+v, %v), want ProfilesChanged=false", routineStatsRes, err)
	}

	// Out-of-order historical timestamp (< last_seen) falls back cleanly and stamps profile_id accurately.
	backfillPath := filepath.Join(tempDir, "Journal.2026-05-01T095000.01.log")
	appendJournalLines(testingContext, backfillPath,
		`{"timestamp":"2026-05-01T10:02:00Z","event":"LoadGame","Commander":"CmdrFast","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:02:30Z","event":"FSDJump","StarSystem":"Sirius","SystemAddress":1003,"StarPos":[3,4,5],"JumpDist":8.6}`,
	)
	outOfOrderRes, err := ingester.IngestFile(backfillPath)
	if err != nil || outOfOrderRes.NewEvents != 2 || outOfOrderRes.ProfilesChanged {
		testingContext.Fatalf("out-of-order IngestFile = (%+v, %v)", outOfOrderRes, err)
	}

	// Commander rename alias on same FID falls back to full ResolveCommanders and reports ProfilesChanged=true.
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:20:00Z","event":"LoadGame","Commander":"CmdrFastRenamed","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
	)
	renameRes, err := ingester.IngestFile(logPath)
	if err != nil || renameRes.NewEvents != 1 || !renameRes.ProfilesChanged {
		testingContext.Fatalf("rename IngestFile = (%+v, %v), want ProfilesChanged=true", renameRes, err)
	}

	// Live Statistics career-wipe drop falls back to full ResolveCommanders, splits eras, and reports ProfilesChanged=true.
	wipeLogPath := filepath.Join(tempDir, "Journal.2026-05-01T110000.01.log")
	appendJournalLines(testingContext, wipeLogPath,
		`{"timestamp":"2026-05-01T11:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-05-01T11:00:01Z","event":"Commander","Name":"CmdrFastRenamed","FID":"F500"}`,
		`{"timestamp":"2026-05-01T11:00:02Z","event":"LoadGame","Commander":"CmdrFastRenamed","FID":"F500","Ship":"SideWinder","Odyssey":true}`,
		`{"timestamp":"2026-05-01T11:00:03Z","event":"Statistics","Exploration":{"Systems_Visited":1,"Total_Hyperspace_Jumps":0}}`,
	)
	wipeRes, err := ingester.IngestFile(wipeLogPath)
	if err != nil || wipeRes.NewEvents != 3 || !wipeRes.ProfilesChanged {
		testingContext.Fatalf("Statistics career-wipe IngestFile = (%+v, %v), want NewEvents=3 ProfilesChanged=true", wipeRes, err)
	}
	var eraCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM commander_profiles WHERE fid = 'F500'`).Scan(&eraCount); err != nil || eraCount != 2 {
		testingContext.Fatalf("expected 2 commander_profiles eras after Statistics wipe drop, got %d (err=%v)", eraCount, err)
	}
}
