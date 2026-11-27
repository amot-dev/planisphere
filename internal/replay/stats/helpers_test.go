package stats_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func seedStatsDB(testingContext *testing.T, journalLines ...string) *sql.DB {
	testingContext.Helper()
	databaseConnection := dbtest.OpenMemory(testingContext)
	if len(journalLines) > 0 {
		scanResult := journaltest.IngestLines(testingContext, databaseConnection, journalLines...)
		if scanResult.NewEvents != len(journalLines) {
			testingContext.Fatalf("ingested %d events, want %d", scanResult.NewEvents, len(journalLines))
		}
	}
	return databaseConnection
}

func newEngineWithModes(queryer db.Queryer) *stats.Engine {
	fallbackModeID := modes.DefaultFallbackModeID(queryer)
	return newEngineWithModeLookup(queryer, func(_ string) int64 { return fallbackModeID })
}

func newEngineWithModeLookup(queryer db.Queryer, lookup stats.ModeLookup) *stats.Engine {
	return stats.NewEngineWithSources(queryer, nil, nil, func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
		return lookup, nil
	})
}

func insertRawUIDEvent(testingContext *testing.T, databaseConnection *sql.DB, timestamp, eventName, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, rawJSON)
	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, rawJSON, timestamp,
	); execError != nil {
		testingContext.Fatalf("insert raw-UID %s event: %v", eventName, execError)
	}
}
