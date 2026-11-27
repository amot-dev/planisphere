package eddiscovery_test

import (
	"database/sql"
	"path/filepath"
	"testing"
)

type eddCommanderSeed struct {
	ID      int64
	Name    string
	Deleted int
}

type eddTravelLogSeed struct {
	ID          int64
	Name        string
	Path        string
	CommanderID int64
}

type eddJournalEntrySeed struct {
	ID          int64
	TravelLogID int64
	EventType   string
	EventData   string
}

type eddArchiveSeed struct {
	FileName   string
	Commanders []eddCommanderSeed
	TravelLogs []eddTravelLogSeed
	Entries    []eddJournalEntrySeed
}

func buildEDDArchive(testingContext *testing.T, seed eddArchiveSeed) string {
	testingContext.Helper()
	archiveFileName := seed.FileName
	if archiveFileName == "" {
		archiveFileName = "EDD.sqlite"
	}
	archivePath := filepath.Join(testingContext.TempDir(), archiveFileName)

	eddConn, openError := sql.Open("sqlite", archivePath)
	if openError != nil {
		testingContext.Fatalf("open synthetic EDD sqlite %s: %v", archivePath, openError)
	}
	defer eddConn.Close()

	if len(seed.Commanders) > 0 {
		if _, err := eddConn.Exec(`
			CREATE TABLE Commanders (Id INTEGER PRIMARY KEY, Name TEXT, Deleted INTEGER);
			CREATE TABLE TravelLogUnit (Id INTEGER PRIMARY KEY, Name TEXT, Path TEXT, CommanderId INTEGER);
			CREATE TABLE JournalEntries (Id INTEGER PRIMARY KEY, TravelLogId INTEGER, EventType TEXT, EventData TEXT);
		`); err != nil {
			testingContext.Fatalf("create EDD schema with Commanders: %v", err)
		}
		for _, commander := range seed.Commanders {
			if _, err := eddConn.Exec(`INSERT INTO Commanders (Id, Name, Deleted) VALUES (?, ?, ?)`, commander.ID, commander.Name, commander.Deleted); err != nil {
				testingContext.Fatalf("insert EDD commander %d: %v", commander.ID, err)
			}
		}
		for _, travelLog := range seed.TravelLogs {
			if _, err := eddConn.Exec(`INSERT INTO TravelLogUnit (Id, Name, Path, CommanderId) VALUES (?, ?, ?, ?)`, travelLog.ID, travelLog.Name, travelLog.Path, travelLog.CommanderID); err != nil {
				testingContext.Fatalf("insert EDD travel log %d: %v", travelLog.ID, err)
			}
		}
	} else {
		if _, err := eddConn.Exec(`
			CREATE TABLE TravelLogUnit (Id INTEGER PRIMARY KEY, Name TEXT, Path TEXT);
			CREATE TABLE JournalEntries (Id INTEGER PRIMARY KEY, TravelLogId INTEGER, EventType TEXT, EventData TEXT);
		`); err != nil {
			testingContext.Fatalf("create EDD schema: %v", err)
		}
		for _, travelLog := range seed.TravelLogs {
			if _, err := eddConn.Exec(`INSERT INTO TravelLogUnit (Id, Name, Path) VALUES (?, ?, ?)`, travelLog.ID, travelLog.Name, travelLog.Path); err != nil {
				testingContext.Fatalf("insert EDD travel log %d: %v", travelLog.ID, err)
			}
		}
	}

	for _, entry := range seed.Entries {
		if _, err := eddConn.Exec(
			`INSERT INTO JournalEntries (Id, TravelLogId, EventType, EventData) VALUES (?, ?, ?, ?)`,
			entry.ID, entry.TravelLogID, entry.EventType, entry.EventData,
		); err != nil {
			testingContext.Fatalf("insert EDD journal entry %d: %v", entry.ID, err)
		}
	}
	if closeError := eddConn.Close(); closeError != nil {
		testingContext.Fatalf("close synthetic EDD sqlite %s: %v", archivePath, closeError)
	}
	return archivePath
}
