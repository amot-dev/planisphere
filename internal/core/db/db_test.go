package db_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/testutil/dbtest"
)

func getSettingForTest(queryer db.Queryer, key string) (string, error) {
	var settingValue string
	queryError := queryer.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&settingValue)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", nil
	}
	return settingValue, queryError
}

func setSettingForTest(queryer db.Queryer, key, value string) error {
	_, execError := queryer.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return execError
}

func TestConfiguredDatabasePathResolutionAndFallback(testingContext *testing.T) {
	isolatedRoot := dbtest.IsolateUserDirs(testingContext)

	expectedDefaultPath, defaultError := db.DefaultPath()
	if defaultError != nil {
		testingContext.Fatalf("DefaultPath in isolated env: %v", defaultError)
	}

	// Missing bootstrap.json falls back to DefaultPath.
	initialResolved, initialError := db.ResolveConfiguredPath()
	if initialError != nil || initialResolved != expectedDefaultPath {
		testingContext.Fatalf("ResolveConfiguredPath missing bootstrap = (%q, %v), want %q", initialResolved, initialError, expectedDefaultPath)
	}

	// Custom configured path round-trips with whitespace trimmed and cleaned.
	customDatabasePath := filepath.Join(isolatedRoot, "custom-dir", "commander.sqlite")
	if saveError := db.SaveConfiguredPath("  " + customDatabasePath + "  "); saveError != nil {
		testingContext.Fatalf("SaveConfiguredPath custom: %v", saveError)
	}
	configuredResolved, configuredError := db.ResolveConfiguredPath()
	if configuredError != nil || configuredResolved != customDatabasePath {
		testingContext.Fatalf("ResolveConfiguredPath custom = (%q, %v), want %q", configuredResolved, configuredError, customDatabasePath)
	}

	// Blank dbPath in bootstrap.json falls back to DefaultPath, including when XDG_DATA_HOME is unset.
	testingContext.Setenv("XDG_DATA_HOME", "")
	homeFallbackDefault, homeDefaultError := db.DefaultPath()
	if homeDefaultError != nil || homeFallbackDefault == "" {
		testingContext.Fatalf("DefaultPath without XDG_DATA_HOME = (%q, %v)", homeFallbackDefault, homeDefaultError)
	}
	userConfigDir, configDirError := os.UserConfigDir()
	if configDirError != nil {
		testingContext.Fatalf("UserConfigDir: %v", configDirError)
	}
	bootstrapPath := filepath.Join(userConfigDir, db.AppDirName, "bootstrap.json")
	if writeBlankErr := os.WriteFile(bootstrapPath, []byte(`{"dbPath":"   "}`), 0o644); writeBlankErr != nil {
		testingContext.Fatalf("write blank bootstrap.json: %v", writeBlankErr)
	}
	blankResolved, blankError := db.ResolveConfiguredPath()
	if blankError != nil || blankResolved != homeFallbackDefault {
		testingContext.Fatalf("ResolveConfiguredPath blank = (%q, %v), want %q", blankResolved, blankError, homeFallbackDefault)
	}
}

func TestWithTxRollbackOnPanic(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)

	var recoveredPanic any
	func() {
		defer func() {
			recoveredPanic = recover()
		}()
		_ = db.WithTx(dbConn, func(transaction *sql.Tx) error {
			if setError := setSettingForTest(transaction, "panic_rollback_key", "uncommitted_value"); setError != nil {
				return setError
			}
			panic("simulated panic")
		})
	}()

	if recoveredPanic == nil {
		t.Fatalf("expected panic to propagate, but none occurred")
	}
	if recoveredPanic != "simulated panic" {
		t.Fatalf("unexpected panic value: %v", recoveredPanic)
	}

	// Verify that the uncommitted change was rolled back
	persistedValue, getSettingError := getSettingForTest(dbConn, "panic_rollback_key")
	if getSettingError != nil {
		t.Fatalf("getSettingForTest after panic: %v", getSettingError)
	}
	if persistedValue != "" {
		t.Fatalf("expected uncommitted setting to be rolled back, got %q", persistedValue)
	}

	// Verify that the connection is not locked and subsequent transactions succeed
	secondTransactionError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return setSettingForTest(transaction, "panic_rollback_key", "committed_value")
	})
	if secondTransactionError != nil {
		t.Fatalf("subsequent WithTx failed: %v", secondTransactionError)
	}

	finalValue, getFinalSettingError := getSettingForTest(dbConn, "panic_rollback_key")
	if getFinalSettingError != nil {
		t.Fatalf("getSettingForTest after subsequent transaction: %v", getFinalSettingError)
	}
	if finalValue != "committed_value" {
		t.Fatalf("expected setting to be %q, got %q", "committed_value", finalValue)
	}
}

func TestMigrateSessionEventsV14ResetsOffsetsAndIsIdempotent(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	initialMigrationFlag, flagError := getSettingForTest(databaseConnection, "session_events_v14")
	if flagError != nil {
		testingContext.Fatalf("getSettingForTest(session_events_v14): %v", flagError)
	}
	if initialMigrationFlag != "1" {
		testingContext.Fatalf("session_events_v14 setting = %q, want 1", initialMigrationFlag)
	}

	if _, seedError := databaseConnection.Exec(`
		INSERT INTO ingested_journals
			(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
		VALUES
			('Journal.2026-05-01T100000.01.log', '/tmp/a.log', 4096, 42, 4096, 12, '2026-05-01T10:00:00Z', '2026-05-01T10:30:00Z', '2026-05-01T10:30:00Z'),
			('Journal.2026-05-02T120000.01.log', '/tmp/b.log', 8192, 99, 8192, 25, '2026-05-02T12:00:00Z', '2026-05-02T13:00:00Z', '2026-05-02T13:00:00Z');
		DELETE FROM settings WHERE key = 'session_events_v14';
	`); seedError != nil {
		testingContext.Fatalf("seed pre-v14 ingested_journals: %v", seedError)
	}

	if migrateError := db.Migrate(databaseConnection); migrateError != nil {
		testingContext.Fatalf("db.Migrate session_events_v14: %v", migrateError)
	}

	var offsetA, linesA, storedA int64
	var firstA, lastA string
	if scanError := databaseConnection.QueryRow(`
		SELECT byte_offset, line_count, events_stored, COALESCE(first_timestamp, ''), COALESCE(last_timestamp, '')
		FROM ingested_journals WHERE file_name = 'Journal.2026-05-01T100000.01.log'`,
	).Scan(&offsetA, &linesA, &storedA, &firstA, &lastA); scanError != nil {
		testingContext.Fatalf("query migrated journal row: %v", scanError)
	}
	if offsetA != 0 || linesA != 0 || storedA != 12 || firstA != "2026-05-01T10:00:00Z" || lastA != "2026-05-01T10:30:00Z" {
		testingContext.Fatalf("post-migration row = (offset=%d lines=%d stored=%d first=%q last=%q), want (0, 0, 12, 2026-05-01T10:00:00Z, 2026-05-01T10:30:00Z)",
			offsetA, linesA, storedA, firstA, lastA)
	}

	// Simulate subsequent ingestion advancing offsets, then re-run Migrate to confirm idempotency.
	if _, updateError := databaseConnection.Exec(`
		UPDATE ingested_journals
		SET byte_offset = 2048, line_count = 25
		WHERE file_name = 'Journal.2026-05-01T100000.01.log'`); updateError != nil {
		testingContext.Fatalf("advance offset before second Migrate: %v", updateError)
	}
	if secondMigrateError := db.Migrate(databaseConnection); secondMigrateError != nil {
		testingContext.Fatalf("second db.Migrate: %v", secondMigrateError)
	}
	if scanError := databaseConnection.QueryRow(`
		SELECT byte_offset, line_count
		FROM ingested_journals WHERE file_name = 'Journal.2026-05-01T100000.01.log'`,
	).Scan(&offsetA, &linesA); scanError != nil {
		testingContext.Fatalf("query journal row after second Migrate: %v", scanError)
	}
	if offsetA != 2048 || linesA != 25 {
		testingContext.Fatalf("expected second Migrate to preserve byte_offset=2048 line_count=25, got (%d, %d)", offsetA, linesA)
	}
}
