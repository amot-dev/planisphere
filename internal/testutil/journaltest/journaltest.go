// Package journaltest provides shared journal ingestion and parsed-line seeding helpers for backend tests.
package journaltest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"planisphere/internal/core/projection"
	"planisphere/internal/sources/journal"
)

var ingestFileCounter atomic.Uint64

// IngestLines writes lines into a temporary journal log file and ingests them via ScanDir.
func IngestLines(testingContext *testing.T, conn *sql.DB, lines ...string) journal.Result {
	testingContext.Helper()
	fileNumber := ingestFileCounter.Add(1)
	fileName := fmt.Sprintf("Journal.2026-01-18T190000-%d.01.log", fileNumber)
	return IngestFiles(testingContext, conn, map[string][]string{fileName: lines})
}

// IngestFiles writes named journal log files into a temporary directory and runs ScanDir.
func IngestFiles(testingContext *testing.T, conn *sql.DB, filesByName map[string][]string) journal.Result {
	testingContext.Helper()
	temporaryDirectory := testingContext.TempDir()
	for fileName, lines := range filesByName {
		content := ""
		if len(lines) > 0 {
			content = strings.Join(lines, "\n") + "\n"
		}
		targetPath := filepath.Join(temporaryDirectory, filepath.Base(fileName))
		if writeError := os.WriteFile(targetPath, []byte(content), 0o644); writeError != nil {
			testingContext.Fatalf("write journal file %s: %v", fileName, writeError)
		}
	}
	scanResult, scanError := journal.NewIngester(conn).ScanDir(context.Background(), temporaryDirectory)
	if scanError != nil {
		testingContext.Fatalf("ScanDir: %v", scanError)
	}
	return scanResult
}

func lookupExistingProfileID(conn *sql.DB, commanderName, frontierID, universe string) int64 {
	var profileID int64
	if frontierID != "" {
		query := `SELECT id FROM commander_profiles WHERE fid = ? COLLATE NOCASE AND universe = ? ORDER BY era_index DESC, id DESC LIMIT 1`
		if conn.QueryRow(query, frontierID, universe).Scan(&profileID) == nil {
			return profileID
		}
	}
	if commanderName != "" {
		query := `SELECT id FROM commander_profiles WHERE display_name = ? COLLATE NOCASE AND universe = ? ORDER BY era_index DESC, id DESC LIMIT 1`
		if conn.QueryRow(query, commanderName, universe).Scan(&profileID) == nil {
			return profileID
		}
	}
	return 0
}

// InsertParsedLines parses raw JSON journal lines with journal.ParseLine and inserts full columns into journal_events.
func InsertParsedLines(testingContext *testing.T, conn *sql.DB, lines ...string) []journal.EventType {
	testingContext.Helper()
	if len(lines) == 0 {
		return nil
	}
	var activeCommander, activeFID, activeUniverse string
	var activeProfileID int64
	latestContextQuery := `SELECT COALESCE(commander, ''), fid, universe, profile_id FROM journal_events ORDER BY id DESC LIMIT 1`
	if scanError := conn.QueryRow(latestContextQuery).Scan(&activeCommander, &activeFID, &activeUniverse, &activeProfileID); scanError != nil && scanError != sql.ErrNoRows {
		testingContext.Fatalf("query latest journal context: %v", scanError)
	}
	if activeUniverse == "" {
		activeUniverse = "live"
	}

	insertedTypes := make([]journal.EventType, 0, len(lines))
	for _, rawLine := range lines {
		parsedEvent, keepEvent, parseError := journal.ParseLine([]byte(rawLine), "journaltest.log")
		if parseError != nil || !keepEvent {
			testingContext.Fatalf("ParseLine(%s): keep=%v err=%v", rawLine, keepEvent, parseError)
		}
		universe := parsedEvent.Universe
		if universe == "" {
			universe = activeUniverse
		}
		rowProfileID := activeProfileID
		switch {
		case parsedEvent.Type == journal.EventClearSavedGame:
			if parsedEvent.Commander == "" {
				parsedEvent.Commander = activeCommander
			}
			if parsedEvent.FID == "" {
				parsedEvent.FID = activeFID
			}
			if rowProfileID == 0 {
				rowProfileID = lookupExistingProfileID(conn, parsedEvent.Commander, parsedEvent.FID, universe)
			}
			activeCommander, activeFID, activeProfileID = "", "", 0
		case parsedEvent.Commander != "":
			activeCommander = parsedEvent.Commander
			if parsedEvent.FID != "" {
				activeFID = parsedEvent.FID
			} else if activeFID != "" && parsedEvent.Type != journal.EventNewCommander {
				parsedEvent.FID = activeFID
			} else {
				activeFID = ""
			}
			activeProfileID = lookupExistingProfileID(conn, activeCommander, activeFID, universe)
			rowProfileID = activeProfileID
		default:
			parsedEvent.Commander = activeCommander
			if parsedEvent.FID == "" {
				parsedEvent.FID = activeFID
			}
		}

		var starX, starY, starZ any
		if parsedEvent.StarPos != nil {
			starX, starY, starZ = parsedEvent.StarPos.X, parsedEvent.StarPos.Y, parsedEvent.StarPos.Z
		}
		insertQuery := `INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, body_id, body_name, star_x, star_y, star_z, commander, fid, universe, profile_id, source_file, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'journaltest.log', ?, ?)`
		if _, execError := conn.Exec(
			insertQuery,
			parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event,
			journal.NullStringOrNil(parsedEvent.SystemName), parsedEvent.SystemAddress, parsedEvent.BodyID,
			journal.NullStringOrNil(parsedEvent.BodyName), starX, starY, starZ,
			journal.NullStringOrNil(parsedEvent.Commander), parsedEvent.FID, universe, rowProfileID,
			parsedEvent.Raw, parsedEvent.Timestamp,
		); execError != nil {
			testingContext.Fatalf("insert parsed event %s: %v", parsedEvent.Event, execError)
		}
		insertedTypes = append(insertedTypes, parsedEvent.Type)
	}
	return insertedTypes
}

// PublishParsedLines inserts parsed journal lines and publishes their event types on bus.
func PublishParsedLines(testingContext *testing.T, conn *sql.DB, bus *projection.Bus, lines ...string) {
	testingContext.Helper()
	insertedTypes := InsertParsedLines(testingContext, conn, lines...)
	if bus != nil && len(insertedTypes) > 0 {
		bus.PublishJournal(journal.NewEventSet(insertedTypes...))
	}
}
