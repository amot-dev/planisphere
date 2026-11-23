package replay_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

func openReplayTestDB(testingContext *testing.T) *sql.DB {
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

func TestLoadEventsAndQueryLatestEvent(testingContext *testing.T) {
	databaseConnection := openReplayTestDB(testingContext)

	seedLines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","event":"Commander","Commander":"CmdrEnabled","FID":"F1"}`,
		`{"timestamp":"2026-01-01T00:01:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-01T00:02:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,2,3],"JumpDist":4.3}`,
		`{"timestamp":"2026-01-01T00:03:00Z","event":"Commander","Commander":"CmdrDisabled","FID":"F2"}`,
		`{"timestamp":"2026-01-01T00:04:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":1003,"StarPos":[10,20,30],"JumpDist":50.0}`,
	}
	statement, prepError := databaseConnection.Prepare(journal.InsertJournalEventPrefixSQL)
	if prepError != nil {
		testingContext.Fatalf("prepare insert: %v", prepError)
	}
	defer statement.Close()

	activeCommander := ""
	for _, rawLine := range seedLines {
		parsedEvent, ok, parseError := journal.ParseLine([]byte(rawLine), "Journal.test.log")
		if parseError != nil || !ok {
			testingContext.Fatalf("parse line %s: ok=%v err=%v", rawLine, ok, parseError)
		}
		if parsedEvent.Commander != "" {
			activeCommander = parsedEvent.Commander
		} else {
			parsedEvent.Commander = activeCommander
		}
		if _, execError := journal.ExecJournalEvent(statement, &parsedEvent, "Journal.test.log", "2026-01-01T00:05:00Z"); execError != nil {
			testingContext.Fatalf("insert event: %v", execError)
		}
	}
	if _, execError := databaseConnection.Exec(
		`INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrEnabled', 'F1', 1), ('CmdrDisabled', 'F2', 0)`,
	); execError != nil {
		testingContext.Fatalf("insert commanders: %v", execError)
	}

	arrivalSet := journal.NewEventSet(journal.EventLocation, journal.EventFSDJump)
	loadedRows, loadError := replay.LoadEvents(databaseConnection, "2026-01-01T00:01:00Z", "", arrivalSet)
	if loadError != nil {
		testingContext.Fatalf("LoadEvents: %v", loadError)
	}
	if len(loadedRows) != 1 || loadedRows[0].Type != journal.EventFSDJump || loadedRows[0].SystemName != "Alpha Centauri" || loadedRows[0].StarPos == nil {
		testingContext.Fatalf("unexpected loadedRows: %+v", loadedRows)
	}

	latestRow, latestError := replay.QueryLatestEvent(databaseConnection, arrivalSet)
	if latestError != nil || latestRow == nil {
		testingContext.Fatalf("QueryLatestEvent: row=%v err=%v", latestRow, latestError)
	}
	if latestRow.Type != journal.EventFSDJump || latestRow.SystemName != "Alpha Centauri" {
		testingContext.Fatalf("expected latest enabled arrival Alpha Centauri, got %+v", latestRow)
	}

	emptyRows, emptyError := replay.LoadEvents(databaseConnection, "", "", journal.NewEventSet())
	if emptyError != nil || len(emptyRows) != 0 {
		testingContext.Fatalf("expected empty result for empty EventSet, got rows=%v err=%v", emptyRows, emptyError)
	}
	missingLatest, missingError := replay.QueryLatestEvent(databaseConnection, journal.NewEventSet(journal.EventCarrierStats))
	if missingError != nil || missingLatest != nil {
		testingContext.Fatalf("expected nil for absent CarrierStats event, got row=%v err=%v", missingLatest, missingError)
	}
}

func TestArchiveStatusPropagatesDisabledCommanderQueryError(testingContext *testing.T) {
	conn := openReplayTestDB(testingContext)

	if _, dropError := conn.Exec(`DROP TABLE commanders`); dropError != nil {
		testingContext.Fatal(dropError)
	}

	_, statusError := replay.ArchiveStatus(conn)
	if statusError == nil {
		testingContext.Fatal("expected ArchiveStatus to fail when commanders table is missing")
	}
	if !strings.Contains(statusError.Error(), "check disabled commanders") {
		testingContext.Fatalf("expected error wrapping check disabled commanders, got %v", statusError)
	}
}

func TestListCommandersAfterByteZeroBackfill(testingContext *testing.T) {
	conn := openReplayTestDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	logFilePath := filepath.Join(temporaryDirectory, "Journal.2026-04-01T120000.01.log")
	earlyLine := `{"timestamp":"2026-04-01T12:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`
	laterLine := `{"timestamp":"2026-04-01T12:01:00Z","event":"LoadGame","Commander":"CmdrAlpha","FID":"F10","Ship":"mandalay"}`
	if writeError := os.WriteFile(logFilePath, []byte(earlyLine+"\n"+laterLine+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}

	earlyEvent, _, parseEarlyError := journal.ParseLine([]byte(earlyLine), "Journal.2026-04-01T120000.01.log")
	if parseEarlyError != nil {
		testingContext.Fatal(parseEarlyError)
	}
	loadEvent, _, parseLoadError := journal.ParseLine([]byte(laterLine), "Journal.2026-04-01T120000.01.log")
	if parseLoadError != nil {
		testingContext.Fatal(parseLoadError)
	}

	if _, execError := conn.Exec(
		`INSERT INTO ingested_journals (file_name, file_path, byte_offset, updated_at) VALUES (?, ?, 0, ?)`,
		"Journal.2026-04-01T120000.01.log", logFilePath, "2026-04-01T12:01:00Z",
	); execError != nil {
		testingContext.Fatal(execError)
	}
	if _, execError := conn.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, commander, source_file, raw_json, ingested_at)
		 VALUES (?, '2026-04-01T12:00:00Z', 'Location', 'Sol', 1001, NULL, ?, '{}', '2026-04-01T12:01:00Z'),
		        (?, '2026-04-01T12:01:00Z', 'LoadGame', '', 0, 'cmdralpha', ?, '{}', '2026-04-01T12:01:00Z')`,
		earlyEvent.UID, "Journal.2026-04-01T120000.01.log",
		loadEvent.UID, "Journal.2026-04-01T120000.01.log",
	); execError != nil {
		testingContext.Fatal(execError)
	}
	ingester := journal.NewIngester(conn)

	if _, execError := conn.Exec(
		`INSERT INTO commanders (name, fid, enabled, first_seen, last_seen)
		 VALUES ('CmdrAlpha', 'F10', 1, '2026-04-01T12:00:00Z', '2026-04-01T12:01:00Z'),
		        ('CmdrBeta', 'F20', 1, '2026-04-02T12:00:00Z', '2026-04-02T12:00:00Z')`,
	); execError != nil {
		testingContext.Fatal(execError)
	}
	if _, execError := conn.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, commander, source_file, raw_json, ingested_at)
		 VALUES ('uid-beta', '2026-04-02T12:00:00Z', 'FSDJump', 'Achenar', 1002, 'CmdrBeta', 'other.log', '{}', '2026-04-02T12:00:00Z')`,
	); execError != nil {
		testingContext.Fatal(execError)
	}

	assertCounts := func(expectedAlpha, expectedBeta int) {
		testingContext.Helper()
		commanders, listError := replay.ListCommanders(conn)
		if listError != nil {
			testingContext.Fatalf("ListCommanders: %v", listError)
		}
		if len(commanders) != 2 {
			testingContext.Fatalf("expected 2 commanders, got %d", len(commanders))
		}
		if commanders[0].Name != "CmdrAlpha" || commanders[0].EventCount != expectedAlpha {
			testingContext.Fatalf("expected CmdrAlpha eventCount=%d, got %+v", expectedAlpha, commanders[0])
		}
		if commanders[1].Name != "CmdrBeta" || commanders[1].EventCount != expectedBeta {
			testingContext.Fatalf("expected CmdrBeta eventCount=%d, got %+v", expectedBeta, commanders[1])
		}
	}

	assertCounts(1, 1)

	if _, ingestError := ingester.IngestFile(logFilePath); ingestError != nil {
		testingContext.Fatalf("IngestFile: %v", ingestError)
	}

	assertCounts(2, 1)
}

func TestListCommandersEventCounts(testingContext *testing.T) {
	testCases := []struct {
		name           string
		commanders     []string
		eventValues    []any
		expectedCounts map[string]int
	}{
		{
			name:           "single commander with mixed case and unattributed rows",
			commanders:     []string{"CmdrSolo"},
			eventValues:    []any{"CmdrSolo", "cmdrsolo", nil, ""},
			expectedCounts: map[string]int{"CmdrSolo": 2},
		},
		{
			name:           "single commander with zero events",
			commanders:     []string{"CmdrEmpty"},
			eventValues:    nil,
			expectedCounts: map[string]int{"CmdrEmpty": 0},
		},
		{
			name:       "multiple commanders skewed counts with unattributed rows",
			commanders: []string{"CmdrAlpha", "CmdrBeta", "CmdrGamma"},
			eventValues: []any{
				"CmdrAlpha", "cmdralpha",
				"CmdrBeta", "CMDRBETA", "CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta",
				"CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta",
				"CmdrGamma", nil, "",
			},
			expectedCounts: map[string]int{"CmdrAlpha": 2, "CmdrBeta": 12, "CmdrGamma": 1},
		},
		{
			name:       "unlisted commanders before between and after trigger fallback",
			commanders: []string{"CmdrB", "CmdrD"},
			eventValues: []any{
				"CmdrA", "CmdrB", "cmdrb", "CmdrC", "CmdrD", "CmdrE",
			},
			expectedCounts: map[string]int{"CmdrB": 2, "CmdrD": 1},
		},
		{
			name:       "plurality unattributed rows fall back cleanly",
			commanders: []string{"CmdrAlpha", "CmdrBeta"},
			eventValues: []any{
				nil, nil, nil, nil, nil, nil, nil, nil, "", "",
				"CmdrAlpha", "CmdrBeta",
			},
			expectedCounts: map[string]int{"CmdrAlpha": 1, "CmdrBeta": 1},
		},
		{
			name:       "non-text commander storage classes trigger fallback",
			commanders: []string{"CmdrSolo"},
			eventValues: []any{
				"CmdrSolo", "cmdrsolo", []byte{0x01, 0x02},
			},
			expectedCounts: map[string]int{"CmdrSolo": 2},
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(subTest *testing.T) {
			databaseConnection := openReplayTestDB(subTest)
			for index, commanderName := range testCase.commanders {
				timestamp := fmt.Sprintf("2026-04-0%dT00:00:00Z", index+1)
				if _, execError := databaseConnection.Exec(
					`INSERT INTO commanders (name, fid, enabled, first_seen, last_seen) VALUES (?, ?, 1, ?, ?)`,
					commanderName, fmt.Sprintf("F%d", index+1), timestamp, timestamp,
				); execError != nil {
					subTest.Fatalf("insert commander %s: %v", commanderName, execError)
				}
			}
			for index, commanderValue := range testCase.eventValues {
				if _, execError := databaseConnection.Exec(
					`INSERT INTO journal_events (event_uid, timestamp, event, commander, source_file, raw_json, ingested_at)
					 VALUES (?, ?, 'FSDJump', ?, 'test.log', '{}', '2026-04-10T00:00:00Z')`,
					fmt.Sprintf("uid-%d", index),
					fmt.Sprintf("2026-04-01T00:00:%02dZ", index),
					commanderValue,
				); execError != nil {
					subTest.Fatalf("insert event %d: %v", index, execError)
				}
			}

			listedCommanders, listError := replay.ListCommanders(databaseConnection)
			if listError != nil {
				subTest.Fatalf("ListCommanders: %v", listError)
			}
			if len(listedCommanders) != len(testCase.expectedCounts) {
				subTest.Fatalf("expected %d commanders, got %d", len(testCase.expectedCounts), len(listedCommanders))
			}
			for _, listedCommander := range listedCommanders {
				expectedCount := testCase.expectedCounts[listedCommander.Name]
				if listedCommander.EventCount != expectedCount {
					subTest.Errorf("commander %s: expected count %d, got %d", listedCommander.Name, expectedCount, listedCommander.EventCount)
				}
			}
		})
	}
}

func TestMulticrewIgnoredAcrossArrivalsLocationAndVisitCounts(testingContext *testing.T) {
	databaseConnection := openReplayTestDB(testingContext)
	if _, execError := databaseConnection.Exec(`
		INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, commander, source_file, raw_json, ingested_at) VALUES
		('mc-sol', '2026-06-01T10:00:00Z', 'Location', 'Sol', 1001, 0, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-06-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}', '2026-06-01T10:00:00Z'),
		('mc-taxi', '2026-06-01T10:10:00Z', 'FSDJump', 'Alpha Centauri', 1002, 4, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-06-01T10:10:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[4,0,0],"Taxi":true}', '2026-06-01T10:10:00Z'),
		('mc-loc', '2026-06-01T10:20:00Z', 'Location', 'Colonia', 1003, 9000, 0, 9000, 'CmdrA', 't.log', '{"timestamp":"2026-06-01T10:20:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":1003,"StarPos":[9000,0,9000],"Multicrew":true}', '2026-06-01T10:20:00Z'),
		('mc-jump', '2026-06-01T10:30:00Z', 'FSDJump', 'Beagle Point', 1004, 0, 0, 65000, 'CmdrA', 't.log', '{"timestamp":"2026-06-01T10:30:00Z","event":"FSDJump","StarSystem":"Beagle Point","SystemAddress":1004,"StarPos":[0,0,65000],"Multicrew":true}', '2026-06-01T10:30:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed multicrew and taxi events: %v", execError)
	}

	windowArrivals, windowError := replay.QueryArrivalsInWindow(databaseConnection, "2026-06-01T09:00:00Z", "")
	if windowError != nil {
		testingContext.Fatalf("QueryArrivalsInWindow: %v", windowError)
	}
	if len(windowArrivals) != 2 || windowArrivals[0].SystemName != "Sol" || windowArrivals[1].SystemName != "Alpha Centauri" || windowArrivals[0].Commander != "CmdrA" {
		testingContext.Fatalf("expected only physical Sol and Taxi Alpha Centauri arrivals, got %+v", windowArrivals)
	}

	beagleCandidates, candError := replay.QuerySystemArrivalCandidates(databaseConnection, "Beagle Point", 1004)
	if candError != nil || len(beagleCandidates) != 0 {
		testingContext.Fatalf("expected Multicrew Beagle Point excluded from StartCandidates, got %+v err=%v", beagleCandidates, candError)
	}

	latestPos, latestSys, locError := replay.QueryLatestPlayerLocation(databaseConnection)
	if locError != nil || latestPos == nil || latestSys != "Alpha Centauri" || latestPos.X != 4 {
		testingContext.Fatalf("expected latest physical location Alpha Centauri (4,0,0), got sys=%q pos=%+v err=%v", latestSys, latestPos, locError)
	}

	counts, countError := replay.QuerySystemVisitCounts(databaseConnection, []string{"sol", "alpha centauri", "colonia", "beagle point"}, nil)
	if countError != nil {
		testingContext.Fatalf("QuerySystemVisitCounts: %v", countError)
	}
	if counts["sol"] != 1 || counts["alpha centauri"] != 1 || counts["colonia"] != 0 || counts["beagle point"] != 0 {
		testingContext.Fatalf("expected multicrew systems to have 0 visits and taxi/sol 1 visit, got %+v", counts)
	}
}
