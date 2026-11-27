// Package dbtest provides shared SQLite in-memory database openers and row seeders for backend tests.
package dbtest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"planisphere/internal/core/db"
)

var seedBatchCounter atomic.Uint64

// IsolateUserDirs points HOME, XDG, and Windows AppData environment variables at fresh subdirectories under a temporary root.
func IsolateUserDirs(testingContext *testing.T) string {
	testingContext.Helper()
	rootDirectory := testingContext.TempDir()
	environmentDirectories := [][2]string{
		{"HOME", filepath.Join(rootDirectory, "home")},
		{"XDG_CONFIG_HOME", filepath.Join(rootDirectory, "xdg-config")},
		{"XDG_DATA_HOME", filepath.Join(rootDirectory, "xdg-data")},
		{"APPDATA", filepath.Join(rootDirectory, "appdata")},
		{"LOCALAPPDATA", filepath.Join(rootDirectory, "localappdata")},
	}
	for _, entry := range environmentDirectories {
		if mkdirError := os.MkdirAll(entry[1], 0o755); mkdirError != nil {
			testingContext.Fatalf("create isolated %s directory: %v", entry[0], mkdirError)
		}
		testingContext.Setenv(entry[0], entry[1])
	}
	return rootDirectory
}

// OpenMemory opens a migrated in-memory SQLite database and registers cleanup on testingContext.
func OpenMemory(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	conn, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("open in-memory test db: %v", openError)
	}
	testingContext.Cleanup(func() {
		_ = conn.Close()
	})
	return conn
}

// SeedEventRow describes a synthetic journal_events row for direct SQL seeding.
type SeedEventRow struct {
	UID        string
	Timestamp  string
	Event      string
	Commander  string
	FID        string
	Universe   string
	SourceFile string
	RawJSON    string
}

// InsertSeedEvents inserts synthetic journal_events rows with deterministic defaults.
func InsertSeedEvents(testingContext *testing.T, conn *sql.DB, rows ...SeedEventRow) {
	testingContext.Helper()
	if len(rows) == 0 {
		return
	}
	batchID := seedBatchCounter.Add(1)
	for index, item := range rows {
		eventUID := item.UID
		if eventUID == "" {
			eventUID = fmt.Sprintf("seed-uid-%d-%d-%s-%s", batchID, index, item.Event, item.Timestamp)
		}
		universe := item.Universe
		if universe == "" {
			universe = "live"
		}
		sourceFile := item.SourceFile
		if sourceFile == "" {
			sourceFile = "seed.log"
		}
		rawJSON := item.RawJSON
		if rawJSON == "" {
			rawJSON = "{}"
		}
		var commanderValue any
		if strings.TrimSpace(item.Commander) != "" {
			commanderValue = item.Commander
		}
		if _, execError := conn.Exec(
			`INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, source_file, raw_json, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			eventUID, item.Timestamp, item.Event, commanderValue, item.FID, universe, sourceFile, rawJSON, item.Timestamp,
		); execError != nil {
			testingContext.Fatalf("insert seed event %s: %v", eventUID, execError)
		}
	}
}
