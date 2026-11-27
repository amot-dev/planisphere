package journal_test

import (
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
)

func appendJournalLines(testingContext *testing.T, logPath string, lines ...string) {
	testingContext.Helper()
	handle, openError := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if openError != nil {
		testingContext.Fatal(openError)
	}
	if _, writeError := handle.WriteString(strings.Join(lines, "\n") + "\n"); writeError != nil {
		if closeError := handle.Close(); closeError != nil {
			testingContext.Logf("close after write failure: %v", closeError)
		}
		testingContext.Fatal(writeError)
	}
	if closeError := handle.Close(); closeError != nil {
		testingContext.Fatal(closeError)
	}
}

func snapshotCommanderTablesState(testingContext *testing.T, databaseConnection *sql.DB) string {
	testingContext.Helper()
	var builder strings.Builder
	queries := []string{
		`SELECT id, event, COALESCE(commander, ''), fid, universe, profile_id FROM journal_events ORDER BY id ASC`,
		`SELECT id, profile_key, display_name, fid, universe, era_index, is_archived_career, has_manual_override, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '') FROM commander_profiles ORDER BY id ASC`,
		`SELECT profile_id, raw_name, fid, universe, start_timestamp, end_timestamp, event_count, is_current, source FROM commander_aliases ORDER BY profile_id ASC, raw_name ASC`,
		`SELECT name, fid, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '') FROM commanders ORDER BY name ASC`,
	}
	for queryIndex, sqlQuery := range queries {
		appendQuerySnapshot(testingContext, databaseConnection, &builder, queryIndex, sqlQuery)
		builder.WriteString("---\n")
	}
	return builder.String()
}

func appendQuerySnapshot(
	testingContext *testing.T,
	databaseConnection *sql.DB,
	builder *strings.Builder,
	queryIndex int,
	sqlQuery string,
) {
	testingContext.Helper()
	rows, queryError := databaseConnection.Query(sqlQuery)
	if queryError != nil {
		testingContext.Fatalf("snapshot query %d: %v", queryIndex, queryError)
	}
	defer rows.Close()

	columnNames, columnsError := rows.Columns()
	if columnsError != nil {
		testingContext.Fatalf("snapshot columns %d: %v", queryIndex, columnsError)
	}
	columnValues := make([]any, len(columnNames))
	columnPointers := make([]any, len(columnNames))
	for columnIndex := range columnValues {
		columnPointers[columnIndex] = &columnValues[columnIndex]
	}
	for rows.Next() {
		if scanError := rows.Scan(columnPointers...); scanError != nil {
			testingContext.Fatal(scanError)
		}
		for columnIndex, columnValue := range columnValues {
			if columnIndex > 0 {
				builder.WriteByte('|')
			}
			if byteSlice, ok := columnValue.([]byte); ok {
				builder.Write(byteSlice)
			} else {
				builder.WriteString(fmt.Sprint(columnValue))
			}
		}
		builder.WriteByte('\n')
	}
	if rowsError := rows.Err(); rowsError != nil {
		testingContext.Fatal(rowsError)
	}
}

func connRunResolveChanged(databaseConnection *sql.DB, changed *bool) error {
	return db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		var resolveError error
		*changed, resolveError = db.ResolveCommandersChanged(transaction)
		return resolveError
	})
}

func seedLiveAppendSession(testingContext *testing.T) (*sql.DB, *journal.Ingester, string, string) {
	testingContext.Helper()
	databaseConnection := dbtest.OpenMemory(testingContext)
	temporaryDirectory := testingContext.TempDir()
	logPath := filepath.Join(temporaryDirectory, "Journal.2026-05-01T100000.01.log")
	ingester := journal.NewIngester(databaseConnection)

	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:00:01Z","event":"Commander","Name":"CmdrFast","FID":"F500"}`,
		`{"timestamp":"2026-05-01T10:00:02Z","event":"LoadGame","Commander":"CmdrFast","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:00:03Z","event":"Statistics","Exploration":{"Systems_Visited":80,"Total_Hyperspace_Jumps":75}}`,
		`{"timestamp":"2026-05-01T10:00:04Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
	)
	initialResult, ingestError := ingester.IngestFile(logPath)
	if ingestError != nil || initialResult.NewEvents != 4 || !initialResult.ProfilesChanged {
		testingContext.Fatalf("initial IngestFile = (%+v, %v), want NewEvents=4 ProfilesChanged=true", initialResult, ingestError)
	}
	return databaseConnection, ingester, temporaryDirectory, logPath
}

func TestIngestFileLiveAppendFastPathEquivalenceAndDirectProbe(testingContext *testing.T) {
	databaseConnection, ingester, _, logPath := seedLiveAppendSession(testingContext)

	// Single-event FSDJump and Scan live appends hit the O(added) fast path and match full ResolveCommanders across all 4 tables.
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,2,3],"JumpDist":4.3}`,
	)
	jumpResult, jumpError := ingester.IngestFile(logPath)
	if jumpError != nil || jumpResult.NewEvents != 1 || jumpResult.CommandersBackfilled != 0 || jumpResult.ProfilesChanged {
		testingContext.Fatalf("live FSDJump IngestFile = (%+v, %v)", jumpResult, jumpError)
	}
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:06:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Alpha Centauri A 1","BodyID":1,"StarSystem":"Alpha Centauri","SystemAddress":1002,"PlanetClass":"Rocky body"}`,
	)
	scanResult, scanError := ingester.IngestFile(logPath)
	if scanError != nil || scanResult.NewEvents != 1 || scanResult.CommandersBackfilled != 0 || scanResult.ProfilesChanged {
		testingContext.Fatalf("live Scan IngestFile = (%+v, %v)", scanResult, scanError)
	}

	stateAfterFastPath := snapshotCommanderTablesState(testingContext, databaseConnection)
	var changedOnFullVerify bool
	if verifyError := connRunResolveChanged(databaseConnection, &changedOnFullVerify); verifyError != nil {
		testingContext.Fatalf("full ResolveCommandersChanged verification: %v", verifyError)
	}
	stateAfterFullResolve := snapshotCommanderTablesState(testingContext, databaseConnection)
	if changedOnFullVerify || stateAfterFastPath != stateAfterFullResolve {
		testingContext.Fatalf("fast-path DB state diverged from full ResolveCommanders:\nfast:\n%s\nfull:\n%s", stateAfterFastPath, stateAfterFullResolve)
	}

	// Verify TryResolveLiveAppend directly reports matched=true on valid tip append and matched=false with an unbound expedition.
	probeTransaction, beginError := databaseConnection.Begin()
	if beginError != nil {
		testingContext.Fatal(beginError)
	}
	defer func() {
		if rollbackError := probeTransaction.Rollback(); rollbackError != nil && !errors.Is(rollbackError, sql.ErrTxDone) {
			testingContext.Errorf("rollback probe transaction: %v", rollbackError)
		}
	}()

	probeInsert, insertError := probeTransaction.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, raw_json, ingested_at) VALUES ('probe-1', '2026-05-01T10:07:00Z', 'Scan', 'CmdrFast', 'F500', 'live', '{}', '2026-05-01T10:07:00Z')`,
	)
	if insertError != nil {
		testingContext.Fatal(insertError)
	}
	probeID, lastIDError := probeInsert.LastInsertId()
	if lastIDError != nil {
		testingContext.Fatal(lastIDError)
	}
	var probeBatch db.LiveAppendBatch
	probeBatch.ObserveInserted(probeID, "Scan", "CmdrFast", "F500", "live", "2026-05-01T10:07:00Z")
	if matched, fastError := db.TryResolveLiveAppend(probeTransaction, probeBatch); fastError != nil || !matched {
		testingContext.Fatalf("expected TryResolveLiveAppend matched=true, got (%v, %v)", matched, fastError)
	}
	if _, expeditionError := probeTransaction.Exec(
		`INSERT INTO expeditions (name, status, commander_profile_id, created_at, updated_at) VALUES ('Unbound', 'planning', 0, '2026-05-01T10:07:00Z', '2026-05-01T10:07:00Z')`,
	); expeditionError != nil {
		testingContext.Fatal(expeditionError)
	}
	if matchedUnbound, fastError := db.TryResolveLiveAppend(probeTransaction, probeBatch); fastError != nil || matchedUnbound {
		testingContext.Fatalf("expected TryResolveLiveAppend matched=false with unbound expedition, got (%v, %v)", matchedUnbound, fastError)
	}
}

func TestIngestFileLiveAppendFallbacksRenameAndCareerWipe(testingContext *testing.T) {
	databaseConnection, ingester, temporaryDirectory, logPath := seedLiveAppendSession(testingContext)

	// Routine non-splitting Statistics falls back cleanly without flagging ProfilesChanged.
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:10:00Z","event":"Statistics","Exploration":{"Systems_Visited":82,"Total_Hyperspace_Jumps":76}}`,
	)
	routineStatsResult, statsError := ingester.IngestFile(logPath)
	if statsError != nil || routineStatsResult.NewEvents != 1 || routineStatsResult.ProfilesChanged {
		testingContext.Fatalf("routine Statistics IngestFile = (%+v, %v), want ProfilesChanged=false", routineStatsResult, statsError)
	}

	// Out-of-order historical timestamp (< last_seen) falls back cleanly and stamps profile_id accurately.
	backfillPath := filepath.Join(temporaryDirectory, "Journal.2026-05-01T095000.01.log")
	appendJournalLines(testingContext, backfillPath,
		`{"timestamp":"2026-05-01T10:02:00Z","event":"LoadGame","Commander":"CmdrFast","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:02:30Z","event":"FSDJump","StarSystem":"Sirius","SystemAddress":1003,"StarPos":[3,4,5],"JumpDist":8.6}`,
	)
	outOfOrderResult, outOfOrderError := ingester.IngestFile(backfillPath)
	if outOfOrderError != nil || outOfOrderResult.NewEvents != 2 || outOfOrderResult.ProfilesChanged {
		testingContext.Fatalf("out-of-order IngestFile = (%+v, %v)", outOfOrderResult, outOfOrderError)
	}

	// Commander rename alias on same FID falls back to full ResolveCommanders and reports ProfilesChanged=true.
	appendJournalLines(testingContext, logPath,
		`{"timestamp":"2026-05-01T10:20:00Z","event":"LoadGame","Commander":"CmdrFastRenamed","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
	)
	renameResult, renameError := ingester.IngestFile(logPath)
	if renameError != nil || renameResult.NewEvents != 1 || !renameResult.ProfilesChanged {
		testingContext.Fatalf("rename IngestFile = (%+v, %v), want ProfilesChanged=true", renameResult, renameError)
	}

	// Live Statistics career-wipe drop falls back to full ResolveCommanders, splits eras, and reports ProfilesChanged=true.
	wipeLogPath := filepath.Join(temporaryDirectory, "Journal.2026-05-01T110000.01.log")
	appendJournalLines(testingContext, wipeLogPath,
		`{"timestamp":"2026-05-01T11:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-05-01T11:00:01Z","event":"Commander","Name":"CmdrFastRenamed","FID":"F500"}`,
		`{"timestamp":"2026-05-01T11:00:02Z","event":"LoadGame","Commander":"CmdrFastRenamed","FID":"F500","Ship":"SideWinder","Odyssey":true}`,
		`{"timestamp":"2026-05-01T11:00:03Z","event":"Statistics","Exploration":{"Systems_Visited":1,"Total_Hyperspace_Jumps":0}}`,
	)
	wipeResult, wipeError := ingester.IngestFile(wipeLogPath)
	if wipeError != nil || wipeResult.NewEvents != 3 || !wipeResult.ProfilesChanged {
		testingContext.Fatalf("Statistics career-wipe IngestFile = (%+v, %v), want NewEvents=3 ProfilesChanged=true", wipeResult, wipeError)
	}
	var eraCount int
	if queryError := databaseConnection.QueryRow(`SELECT COUNT(*) FROM commander_profiles WHERE fid = 'F500'`).Scan(&eraCount); queryError != nil || eraCount != 2 {
		testingContext.Fatalf("expected 2 commander_profiles eras after Statistics wipe drop, got %d (err=%v)", eraCount, queryError)
	}
}
