package journal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
)

func TestPeekTimestamp(testingContext *testing.T) {
	testCases := []struct {
		name     string
		rawLine  []byte
		expected string
	}{
		{
			name:     "standard journal line",
			rawLine:  []byte(`{"timestamp":"2026-06-01T14:22:33Z","event":"FuelScoop","Scooped":2.5}`),
			expected: "2026-06-01T14:22:33Z",
		},
		{
			name:     "utf8 bom and flexible whitespace",
			rawLine:  []byte("\xef\xbb\xbf  { \"timestamp\" : \"2026-06-01T14:22:33Z\" , \"event\" : \"Shutdown\" } \r\n"),
			expected: "2026-06-01T14:22:33Z",
		},
		{
			name:     "missing timestamp field",
			rawLine:  []byte(`{"event":"Shutdown"}`),
			expected: "",
		},
		{
			name:     "non-string timestamp value",
			rawLine:  []byte(`{"timestamp":null,"event":"Shutdown"}`),
			expected: "",
		},
		{
			name:     "non-json line",
			rawLine:  []byte(`not a json object`),
			expected: "",
		},
		{
			name:     "timestamp literal inside earlier string value",
			rawLine:  []byte(`{"note":"see \"timestamp\"","timestamp":"2026-06-01T15:00:00Z","event":"FuelScoop"}`),
			expected: "2026-06-01T15:00:00Z",
		},
	}

	for _, testCase := range testCases {
		actualTimestamp := journal.PeekTimestamp(testCase.rawLine)
		if actualTimestamp != testCase.expected {
			testingContext.Errorf("%s: PeekTimestamp = %q, want %q", testCase.name, actualTimestamp, testCase.expected)
		}
	}
}

func TestStatisticsEventTimePlayedUnmarshal(testingContext *testing.T) {
	rawStatisticsJSON := []byte(`{"timestamp":"2026-06-01T10:00:00Z","event":"Statistics","Exploration":{"Systems_Visited":420,"Total_Hyperspace_Jumps":512,"Time_Played":1234567}}`)
	var statisticsEvent journal.StatisticsEvent
	if unmarshalError := json.Unmarshal(rawStatisticsJSON, &statisticsEvent); unmarshalError != nil {
		testingContext.Fatalf("unmarshal StatisticsEvent: %v", unmarshalError)
	}
	if statisticsEvent.Exploration.TimePlayed != 1234567 {
		testingContext.Fatalf("TimePlayed = %v, want 1234567", statisticsEvent.Exploration.TimePlayed)
	}
	if statisticsEvent.Exploration.SystemsVisited != 420 || statisticsEvent.Exploration.TotalHyperspaceJumps != 512 {
		testingContext.Fatalf("unexpected sibling Exploration fields: %+v", statisticsEvent.Exploration)
	}
}

func TestIngestSessionExitEventsAndMusicFilter(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	temporaryDirectory := testingContext.TempDir()
	logFilePath := filepath.Join(temporaryDirectory, "Journal.2026-06-02T180000.01.log")
	journalLines := []string{
		`{"timestamp":"2026-06-02T18:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-06-02T18:00:01Z","event":"Commander","Name":"CmdrExit","FID":"F900"}`,
		`{"timestamp":"2026-06-02T18:00:02Z","event":"LoadGame","Commander":"CmdrExit","FID":"F900","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-06-02T18:05:00Z","event":"Music","MusicTrack":"Exploration"}`,
		`{"timestamp":"2026-06-02T18:10:00Z","event":"Music","MusicTrack":"NoTrack"}`,
		`{"timestamp":"2026-06-02T18:15:00Z","event":"Music","MusicTrack":"MainMenu"}`,
		`{"timestamp":"2026-06-02T18:16:00Z","event":"Shutdown"}`,
	}
	if writeError := os.WriteFile(logFilePath, []byte(strings.Join(journalLines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}

	ingesterInstance := journal.NewIngester(databaseConnection)
	ingestResult, ingestError := ingesterInstance.IngestFile(logFilePath)
	if ingestError != nil {
		testingContext.Fatalf("IngestFile: %v", ingestError)
	}
	if ingestResult.NewEvents != 4 {
		testingContext.Fatalf("NewEvents = %d, want 4 (Commander, LoadGame, Music MainMenu, Shutdown)", ingestResult.NewEvents)
	}

	rows, queryError := databaseConnection.Query(`
		SELECT event, event_uid, COALESCE(commander, ''), fid, profile_id
		FROM journal_events
		ORDER BY timestamp ASC, id ASC`)
	if queryError != nil {
		testingContext.Fatal(queryError)
	}
	defer rows.Close()

	var observedEvents []string
	expectedMainMenuUID := journal.ComputeUID("2026-06-02T18:15:00Z", "Music", "mainmenu")
	expectedShutdownUID := journal.ComputeUID("2026-06-02T18:16:00Z", "Shutdown")
	for rows.Next() {
		var eventName, eventUID, commanderName, frontierID string
		var profileID int64
		if scanError := rows.Scan(&eventName, &eventUID, &commanderName, &frontierID, &profileID); scanError != nil {
			testingContext.Fatal(scanError)
		}
		observedEvents = append(observedEvents, eventName)
		if commanderName != "CmdrExit" || frontierID != "F900" || profileID <= 0 {
			testingContext.Errorf("event %s attribution = (%q, %q, profile=%d), want (CmdrExit, F900, >0)", eventName, commanderName, frontierID, profileID)
		}
		if eventName == "Music" && eventUID != expectedMainMenuUID {
			testingContext.Errorf("Music UID = %q, want %q", eventUID, expectedMainMenuUID)
		}
		if eventName == "Shutdown" && eventUID != expectedShutdownUID {
			testingContext.Errorf("Shutdown UID = %q, want %q", eventUID, expectedShutdownUID)
		}
	}
	if rowsError := rows.Err(); rowsError != nil {
		testingContext.Fatal(rowsError)
	}
	expectedEvents := []string{"Commander", "LoadGame", "Music", "Shutdown"}
	if !slices.Equal(observedEvents, expectedEvents) {
		testingContext.Fatalf("stored events = %v, want %v", observedEvents, expectedEvents)
	}
}

func TestIngestNonPersistedTrailingEventAdvancesRawLastTimestamp(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	temporaryDirectory := testingContext.TempDir()
	logFileName := "Journal.2026-06-03T100000.01.log"
	logFilePath := filepath.Join(temporaryDirectory, logFileName)

	appendJournalLines(testingContext, logFilePath,
		`{"timestamp":"2026-06-03T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-06-03T10:00:05Z","event":"LoadGame","Commander":"CmdrCrash","FID":"F901","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-06-03T10:01:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-06-03T10:15:00Z","event":"FuelScoop","Scooped":1.4,"Total":32.0}`,
	)

	ingesterInstance := journal.NewIngester(databaseConnection)
	initialResult, initialError := ingesterInstance.IngestFile(logFilePath)
	if initialError != nil {
		testingContext.Fatalf("initial IngestFile: %v", initialError)
	}
	if initialResult.NewEvents != 2 {
		testingContext.Fatalf("initial NewEvents = %d, want 2", initialResult.NewEvents)
	}

	assertJournalMetadata := func(wantStored int64, wantFirst, wantLast string) {
		testingContext.Helper()
		var eventsStored, persistedCount int64
		var firstTimestamp, lastTimestamp string
		if scanError := databaseConnection.QueryRow(`
			SELECT events_stored, COALESCE(first_timestamp, ''), COALESCE(last_timestamp, '')
			FROM ingested_journals WHERE file_name = ?`, logFileName,
		).Scan(&eventsStored, &firstTimestamp, &lastTimestamp); scanError != nil {
			testingContext.Fatalf("query ingested_journals: %v", scanError)
		}
		if countError := databaseConnection.QueryRow(`SELECT COUNT(*) FROM journal_events WHERE source_file = ?`, logFileName).Scan(&persistedCount); countError != nil {
			testingContext.Fatalf("count journal_events: %v", countError)
		}
		if eventsStored != wantStored || persistedCount != wantStored || firstTimestamp != wantFirst || lastTimestamp != wantLast {
			testingContext.Fatalf("journal metadata = (stored=%d rows=%d first=%q last=%q), want (stored=%d rows=%d first=%q last=%q)",
				eventsStored, persistedCount, firstTimestamp, lastTimestamp, wantStored, wantStored, wantFirst, wantLast)
		}
	}

	assertJournalMetadata(2, "2026-06-03T10:00:05Z", "2026-06-03T10:15:00Z")

	// Append another non-persisted event to verify live tailing advances last_timestamp without inserting a row.
	appendJournalLines(testingContext, logFilePath,
		`{"timestamp":"2026-06-03T10:42:30Z","event":"SupercruiseExit","StarSystem":"Sol","SystemAddress":1001,"Body":"Sol"}`,
	)
	trailingResult, trailingError := ingesterInstance.IngestFile(logFilePath)
	if trailingError != nil {
		testingContext.Fatalf("trailing IngestFile: %v", trailingError)
	}
	if trailingResult.NewEvents != 0 || trailingResult.HasChanges() {
		testingContext.Fatalf("expected trailing non-persisted line to report NewEvents=0 and HasChanges=false, got %+v", trailingResult)
	}
	assertJournalMetadata(2, "2026-06-03T10:00:05Z", "2026-06-03T10:42:30Z")
}
