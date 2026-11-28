package eddiscovery_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"planisphere/internal/sources/eddiscovery"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/eddtest"
)

func TestImportEDDiscoveryDB(t *testing.T) {
	eddPath := eddtest.BuildArchive(t, eddtest.ArchiveSeed{
		FileName: "EDD.sqlite",
		TravelLogs: []eddtest.TravelLogSeed{
			{ID: 1, Name: "Journal.2026-01-18T190000.01.log", Path: `C:\ED\Journal.2026-01-18T190000.01.log`},
			{ID: 2, Name: "Journal.2026-01-19T200000.01.log", Path: `C:\ED\Journal.2026-01-19T200000.01.log`},
		},
		Entries: []eddtest.JournalEntrySeed{
			{ID: 1, TravelLogID: 1, EventType: "Location", EventData: `{"timestamp":"2026-01-18T19:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`},
			{ID: 2, TravelLogID: 1, EventType: "LoadGame", EventData: `{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrOne","FID":"F001","Ship":"mandalay"}`},
			{ID: 3, TravelLogID: 1, EventType: "FSDJump", EventData: `{"timestamp":"2026-01-18T19:20:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100],"JumpDist":4.3}`},
			{ID: 4, TravelLogID: 1, EventType: "Music", EventData: `{"timestamp":"2026-01-18T19:25:00Z","event":"Music","MusicTrack":"Exploration"}`},
			{ID: 5, TravelLogID: 2, EventType: "Commander", EventData: `{"timestamp":"2026-01-19T20:00:00Z","event":"Commander","Commander":"CmdrTwo","FID":"F002"}`},
			{ID: 6, TravelLogID: 2, EventType: "FSDJump", EventData: `{"timestamp":"2026-01-19T20:30:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2001,"StarPos":[100,200,300]}`},
		},
	})

	destConn := dbtest.OpenMemory(t)
	ingester := journal.NewIngester(destConn)

	var lastTotal int
	progressCalled := false
	importResult, err := eddiscovery.Import(context.Background(), ingester, eddPath, func(current, total, files int, stage string) {
		progressCalled = true
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("ImportEDDiscoveryDB failed: %v", err)
	}

	if !progressCalled {
		t.Fatal("expected onProgress to be called")
	}
	// Total filtered events should be 5 (excluding 'Music')
	if lastTotal != 5 {
		t.Fatalf("expected total 5, got %d", lastTotal)
	}
	if importResult.LinesRead != 5 {
		t.Fatalf("expected 5 lines read, got %d", importResult.LinesRead)
	}
	if importResult.FilesScanned != 2 {
		t.Fatalf("expected 2 files scanned, got %d", importResult.FilesScanned)
	}

	// Verify all 5 events in journal_events have the correct commander set,
	// especially event 1 (Location) which arrived before LoadGame in file 1.
	rows, err := destConn.Query(`SELECT event, commander FROM journal_events ORDER BY timestamp ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	expectedCommanders := []struct {
		event     string
		commander string
	}{
		{"Location", "CmdrOne"},
		{"LoadGame", "CmdrOne"},
		{"FSDJump", "CmdrOne"},
		{"Commander", "CmdrTwo"},
		{"FSDJump", "CmdrTwo"},
	}

	index := 0
	for rows.Next() {
		var eventRow, commander sql.NullString
		if err := rows.Scan(&eventRow, &commander); err != nil {
			t.Fatal(err)
		}
		if index >= len(expectedCommanders) {
			t.Fatalf("extra row returned: %s, %s", eventRow.String, commander.String)
		}
		if eventRow.String != expectedCommanders[index].event {
			t.Errorf("row %d: expected event %s, got %s", index, expectedCommanders[index].event, eventRow.String)
		}
		if commander.String != expectedCommanders[index].commander {
			t.Errorf("row %d (%s): expected commander %s, got %s", index, eventRow.String, expectedCommanders[index].commander, commander.String)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate journal_events: %v", err)
	}
	if index != len(expectedCommanders) {
		t.Fatalf("expected %d rows, got %d", len(expectedCommanders), index)
	}

	// Verify commanders table
	commanderRows, err := destConn.Query(`SELECT name, fid, first_seen, last_seen FROM commanders ORDER BY name ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer commanderRows.Close()

	type commanderRecord struct {
		name, fid, firstSeen, lastSeen string
	}
	var commanders []commanderRecord
	for commanderRows.Next() {
		var commander commanderRecord
		if err := commanderRows.Scan(&commander.name, &commander.fid, &commander.firstSeen, &commander.lastSeen); err != nil {
			t.Fatal(err)
		}
		commanders = append(commanders, commander)
	}
	if err := commanderRows.Err(); err != nil {
		t.Fatalf("iterate commanders: %v", err)
	}
	if len(commanders) != 2 {
		t.Fatalf("expected 2 commanders, got %d", len(commanders))
	}
	if commanders[0].name != "CmdrOne" || commanders[0].fid != "F001" || commanders[0].firstSeen != "2026-01-18T19:00:00Z" || commanders[0].lastSeen != "2026-01-18T19:20:00Z" {
		t.Errorf("unexpected CmdrOne record: %+v", commanders[0])
	}
	if commanders[1].name != "CmdrTwo" || commanders[1].fid != "F002" || commanders[1].firstSeen != "2026-01-19T20:00:00Z" || commanders[1].lastSeen != "2026-01-19T20:30:00Z" {
		t.Errorf("unexpected CmdrTwo record: %+v", commanders[1])
	}
}

func TestImportEDDiscoveryDB_ContextCancellation(t *testing.T) {
	temporaryDatabaseConnection := dbtest.OpenMemory(t)
	ingesterInstance := journal.NewIngester(temporaryDatabaseConnection)

	cancelledContext, cancelFunction := context.WithCancel(context.Background())
	cancelFunction() // Cancel immediately

	_, importError := eddiscovery.Import(cancelledContext, ingesterInstance, "nonexistent.sqlite", nil)
	if !errors.Is(importError, context.Canceled) {
		t.Fatalf("expected context.Canceled error from ImportEDDiscoveryDB, got %v", importError)
	}
}

func TestImportEDDiscoveryCommanderFallbackAndRollover(testingContext *testing.T) {
	eddPath := eddtest.BuildArchive(testingContext, eddtest.ArchiveSeed{
		FileName: "EDDFallback.sqlite",
		Commanders: []eddtest.CommanderSeed{
			{ID: 1, Name: "Trebor Johnson", Deleted: 0},
			{ID: 2, Name: "DeletedCmdr", Deleted: 1},
		},
		TravelLogs: []eddtest.TravelLogSeed{
			{ID: 10, Name: "netLog.1605011200.01.log", Path: `C:\ED\Logs\netLog.1605011200.01.log`, CommanderID: 1},
			{ID: 11, Name: "Journal.2026-03-01T100000.01.log", Path: `C:\ED\Journal.2026-03-01T100000.01.log`, CommanderID: 1},
			{ID: 12, Name: "Journal.2026-03-01T120000.02.log", Path: `C:\ED\Journal.2026-03-01T120000.02.log`, CommanderID: 2},
		},
		Entries: []eddtest.JournalEntrySeed{
			{ID: 1, TravelLogID: 10, EventType: "FSDJump", EventData: `{"timestamp":"2016-05-01T12:05:00Z","event":"FSDJump","StarSystem":"Beagle Point","StarPos":[-1111.56,-134.21,65269.75]}`},
			{ID: 2, TravelLogID: 11, EventType: "Fileheader", EventData: `{"timestamp":"2026-03-01T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`},
			{ID: 3, TravelLogID: 11, EventType: "LoadGame", EventData: `{"timestamp":"2026-03-01T10:01:00Z","event":"LoadGame","Commander":"Trebor Johnson","FID":"F4293989","Ship":"Mandalay","Odyssey":true}`},
			{ID: 4, TravelLogID: 11, EventType: "Continued", EventData: `{"timestamp":"2026-03-01T11:59:59Z","event":"Continued","Part":2}`},
			{ID: 5, TravelLogID: 12, EventType: "Fileheader", EventData: `{"timestamp":"2026-03-01T12:00:00Z","event":"Fileheader","part":2,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`},
			{ID: 6, TravelLogID: 12, EventType: "FSDJump", EventData: `{"timestamp":"2026-03-01T12:10:00Z","event":"FSDJump","StarSystem":"Ishum's Reach","SystemAddress":9001,"StarPos":[-500,0,65600]}`},
		},
	})

	destConn := dbtest.OpenMemory(testingContext)

	if _, err := eddiscovery.Import(context.Background(), journal.NewIngester(destConn), eddPath, nil); err != nil {
		testingContext.Fatalf("Import failed: %v", err)
	}

	// Both pre-2.2 netLog FSDJump (via CommanderId fallback + pre-v3 FID bridging) and part 2 rollover FSDJump resolve into live:f4293989:era:0.
	var profileCount int
	var profileKey, displayName string
	if err := destConn.QueryRow(`SELECT COUNT(*), MIN(profile_key), MIN(display_name) FROM commander_profiles`).Scan(&profileCount, &profileKey, &displayName); err != nil {
		testingContext.Fatal(err)
	}
	if profileCount != 1 || profileKey != "live:f4293989:era:0" || displayName != "Trebor Johnson" {
		testingContext.Fatalf("expected 1 unified profile live:f4293989:era:0 (Trebor Johnson), got count=%d key=%q name=%q", profileCount, profileKey, displayName)
	}

	var aliasEventCount int
	if err := destConn.QueryRow(`SELECT event_count FROM commander_aliases WHERE raw_name = 'Trebor Johnson'`).Scan(&aliasEventCount); err != nil {
		testingContext.Fatal(err)
	}
	if aliasEventCount != 3 {
		testingContext.Fatalf("expected 3 events attributed to Trebor Johnson across netLog, part 1, and part 2, got %d", aliasEventCount)
	}
}

func TestImportProgressSuppressesCompleteOnFailure(testingContext *testing.T) {
	eddPath := eddtest.BuildArchive(testingContext, eddtest.ArchiveSeed{
		FileName: "EDDProgress.sqlite",
		TravelLogs: []eddtest.TravelLogSeed{
			{ID: 1, Name: "Journal.2026-02-01T100000.01.log", Path: "/logs/Journal.2026-02-01T100000.01.log"},
		},
		Entries: []eddtest.JournalEntrySeed{
			{ID: 1, TravelLogID: 1, EventType: "Location", EventData: `{"timestamp":"2026-02-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`},
		},
	})

	destConn := dbtest.OpenMemory(testingContext)
	ingester := journal.NewIngester(destConn)

	// Cancel mid-import right as importEntriesTx begins so txError is non-nil.
	cancellableContext, cancelImport := context.WithCancel(context.Background())
	defer cancelImport()

	var failedStages []string
	_, failedError := eddiscovery.Import(cancellableContext, ingester, eddPath, func(_, _, _ int, stage string) {
		failedStages = append(failedStages, stage)
		if stage == "Ingesting historical journal events into SQLite archive..." {
			cancelImport()
		}
	})
	if !errors.Is(failedError, context.Canceled) {
		testingContext.Fatalf("expected context.Canceled on mid-import abort, got %v", failedError)
	}
	if slices.Contains(failedStages, "Complete") {
		testingContext.Fatalf("expected failed import to omit Complete stage, got %v", failedStages)
	}

	// Valid import against the same archive succeeds and emits Complete.
	var succeededStages []string
	importResult, importError := eddiscovery.Import(context.Background(), ingester, eddPath, func(_, _, _ int, stage string) {
		succeededStages = append(succeededStages, stage)
	})
	if importError != nil {
		testingContext.Fatalf("expected valid import to succeed, got %v", importError)
	}
	if importResult.LinesRead != 1 {
		testingContext.Fatalf("expected 1 imported line, got %d", importResult.LinesRead)
	}
	if len(succeededStages) == 0 || succeededStages[len(succeededStages)-1] != "Complete" {
		testingContext.Fatalf("expected final progress stage Complete on success, got %v", succeededStages)
	}
}

func TestImportEDDiscoverySessionExitEvents(testingContext *testing.T) {
	eddPath := eddtest.BuildArchive(testingContext, eddtest.ArchiveSeed{
		FileName: "EDDSessionExits.sqlite",
		TravelLogs: []eddtest.TravelLogSeed{
			{ID: 1, Name: "Journal.2026-06-05T100000.01.log", Path: "/logs/Journal.2026-06-05T100000.01.log"},
		},
		Entries: []eddtest.JournalEntrySeed{
			{ID: 1, TravelLogID: 1, EventType: "LoadGame", EventData: `{"timestamp":"2026-06-05T10:00:00Z","event":"LoadGame","Commander":"CmdrEDD","FID":"F555","Ship":"Mandalay","Odyssey":true}`},
			{ID: 2, TravelLogID: 1, EventType: "Location", EventData: `{"timestamp":"2026-06-05T10:01:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`},
			{ID: 3, TravelLogID: 1, EventType: "Music", EventData: `{"timestamp":"2026-06-05T10:05:00Z","event":"Music","MusicTrack":"Exploration"}`},
			{ID: 4, TravelLogID: 1, EventType: "Music", EventData: `{"timestamp":"2026-06-05T10:06:00Z","event":"Music","MusicTrack":"NoTrack"}`},
			{ID: 5, TravelLogID: 1, EventType: "Music", EventData: `{"timestamp":"2026-06-05T10:07:00Z","event":"Music","MusicTrack":"MainMenu_Extra"}`},
			{ID: 6, TravelLogID: 1, EventType: "Music", EventData: `{"timestamp":"2026-06-05T10:10:00Z","event":"Music","MusicTrack":"MainMenu"}`},
			{ID: 7, TravelLogID: 1, EventType: "Shutdown", EventData: `{"timestamp":"2026-06-05T10:11:00Z","event":"Shutdown"}`},
		},
	})

	destConn := dbtest.OpenMemory(testingContext)
	importResult, importError := eddiscovery.Import(context.Background(), journal.NewIngester(destConn), eddPath, nil)
	if importError != nil {
		testingContext.Fatalf("Import failed: %v", importError)
	}
	// Exploration and NoTrack Music rows are filtered out in SQL; MainMenu_Extra is filtered out by journal.ParseLine.
	if importResult.LinesRead != 5 {
		testingContext.Fatalf("LinesRead = %d, want 5", importResult.LinesRead)
	}

	rows, queryError := destConn.Query(`SELECT event, COALESCE(commander, ''), timestamp FROM journal_events ORDER BY timestamp ASC, id ASC`)
	if queryError != nil {
		testingContext.Fatal(queryError)
	}
	defer rows.Close()

	type importedRow struct {
		eventName, commanderName, timestamp string
	}
	var actualRows []importedRow
	for rows.Next() {
		var row importedRow
		if scanError := rows.Scan(&row.eventName, &row.commanderName, &row.timestamp); scanError != nil {
			testingContext.Fatal(scanError)
		}
		actualRows = append(actualRows, row)
	}
	if rowsError := rows.Err(); rowsError != nil {
		testingContext.Fatal(rowsError)
	}
	expectedRows := []importedRow{
		{"LoadGame", "CmdrEDD", "2026-06-05T10:00:00Z"},
		{"Location", "CmdrEDD", "2026-06-05T10:01:00Z"},
		{"Music", "CmdrEDD", "2026-06-05T10:10:00Z"},
		{"Shutdown", "CmdrEDD", "2026-06-05T10:11:00Z"},
	}
	if !slices.Equal(actualRows, expectedRows) {
		testingContext.Fatalf("imported rows = %+v, want %+v", actualRows, expectedRows)
	}
}
