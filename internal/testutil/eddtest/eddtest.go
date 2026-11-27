// Package eddtest builds synthetic EDDiscovery SQLite archives for importer and application tests.
package eddtest

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// CommanderSeed describes a row in an EDDiscovery Commanders table.
type CommanderSeed struct {
	ID      int64
	Name    string
	Deleted int
}

// TravelLogSeed describes a row in an EDDiscovery TravelLogUnit table.
type TravelLogSeed struct {
	ID          int64
	Name        string
	Path        string
	CommanderID int64
}

// JournalEntrySeed describes a row in an EDDiscovery JournalEntries table.
type JournalEntrySeed struct {
	ID          int64
	TravelLogID int64
	EventType   string
	EventData   string
}

// ArchiveSeed configures a synthetic EDDiscovery SQLite database file.
type ArchiveSeed struct {
	FileName   string
	Commanders []CommanderSeed
	TravelLogs []TravelLogSeed
	Entries    []JournalEntrySeed
}

// BuildArchive creates a temporary EDDiscovery SQLite database from seed and returns its file path.
func BuildArchive(testingContext *testing.T, seed ArchiveSeed) string {
	testingContext.Helper()
	archiveFileName := seed.FileName
	if archiveFileName == "" {
		archiveFileName = "EDD.sqlite"
	}
	archivePath := filepath.Join(testingContext.TempDir(), archiveFileName)

	eddConnection, openError := sql.Open("sqlite", archivePath)
	if openError != nil {
		testingContext.Fatalf("open synthetic EDD sqlite %s: %v", archivePath, openError)
	}
	defer eddConnection.Close()

	if len(seed.Commanders) > 0 {
		if _, err := eddConnection.Exec(`
			CREATE TABLE Commanders (Id INTEGER PRIMARY KEY, Name TEXT, Deleted INTEGER);
			CREATE TABLE TravelLogUnit (Id INTEGER PRIMARY KEY, Name TEXT, Path TEXT, CommanderId INTEGER);
			CREATE TABLE JournalEntries (Id INTEGER PRIMARY KEY, TravelLogId INTEGER, EventType TEXT, EventData TEXT);
		`); err != nil {
			testingContext.Fatalf("create EDD schema with Commanders: %v", err)
		}
		for _, commander := range seed.Commanders {
			if _, err := eddConnection.Exec(`INSERT INTO Commanders (Id, Name, Deleted) VALUES (?, ?, ?)`, commander.ID, commander.Name, commander.Deleted); err != nil {
				testingContext.Fatalf("insert EDD commander %d: %v", commander.ID, err)
			}
		}
		for _, travelLog := range seed.TravelLogs {
			if _, err := eddConnection.Exec(`INSERT INTO TravelLogUnit (Id, Name, Path, CommanderId) VALUES (?, ?, ?, ?)`, travelLog.ID, travelLog.Name, travelLog.Path, travelLog.CommanderID); err != nil {
				testingContext.Fatalf("insert EDD travel log %d: %v", travelLog.ID, err)
			}
		}
	} else {
		if _, err := eddConnection.Exec(`
			CREATE TABLE TravelLogUnit (Id INTEGER PRIMARY KEY, Name TEXT, Path TEXT);
			CREATE TABLE JournalEntries (Id INTEGER PRIMARY KEY, TravelLogId INTEGER, EventType TEXT, EventData TEXT);
		`); err != nil {
			testingContext.Fatalf("create EDD schema: %v", err)
		}
		for _, travelLog := range seed.TravelLogs {
			if _, err := eddConnection.Exec(`INSERT INTO TravelLogUnit (Id, Name, Path) VALUES (?, ?, ?)`, travelLog.ID, travelLog.Name, travelLog.Path); err != nil {
				testingContext.Fatalf("insert EDD travel log %d: %v", travelLog.ID, err)
			}
		}
	}

	for _, entry := range seed.Entries {
		if _, err := eddConnection.Exec(
			`INSERT INTO JournalEntries (Id, TravelLogId, EventType, EventData) VALUES (?, ?, ?, ?)`,
			entry.ID, entry.TravelLogID, entry.EventType, entry.EventData,
		); err != nil {
			testingContext.Fatalf("insert EDD journal entry %d: %v", entry.ID, err)
		}
	}
	if closeError := eddConnection.Close(); closeError != nil {
		testingContext.Fatalf("close synthetic EDD sqlite %s: %v", archivePath, closeError)
	}
	return archivePath
}

// BuildMinimalArchive creates a single-log EDDiscovery SQLite database from (eventType, eventData) pairs.
func BuildMinimalArchive(testingContext *testing.T, entries [][2]string) string {
	testingContext.Helper()
	journalEntries := make([]JournalEntrySeed, len(entries))
	for index, entry := range entries {
		journalEntries[index] = JournalEntrySeed{
			ID:          int64(index + 1),
			TravelLogID: 1,
			EventType:   entry[0],
			EventData:   entry[1],
		}
	}
	return BuildArchive(testingContext, ArchiveSeed{
		FileName: "EDDUser.sqlite",
		TravelLogs: []TravelLogSeed{
			{
				ID:   1,
				Name: "Journal.2026-03-01T000000.01.log",
				Path: "/journals/Journal.2026-03-01T000000.01.log",
			},
		},
		Entries: journalEntries,
	})
}
