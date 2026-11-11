package db_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/version"
)

func TestDefaultPath(t *testing.T) {
	defaultPath, pathError := db.DefaultPath()
	if pathError != nil {
		t.Fatalf("DefaultPath: %v", pathError)
	}
	if defaultPath == "" {
		t.Fatalf("expected non-empty default path")
	}
}

func TestMigrateIdempotency(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatalf("Open: %v", openError)
	}
	defer dbConn.Close()

	// Calling Migrate a second time must succeed without errors
	if err := db.Migrate(dbConn); err != nil {
		t.Fatalf("second Migrate call failed: %v", err)
	}

	// Verify expected migration keys were recorded in settings
	expectedKeys := []string{
		"default_modes_seeded",
		"default_expedition_seeded",
		"ingest_parser_v2",
		"gec_schema_v2",
		"commander_schema_v3",
		"fc_arrived_v4",
		"gec_schema_v5",
		"realization_events_v6",
		"source_file_index_v7",
		"sell_organic_uid_dedup_v8",
	}

	for _, key := range expectedKeys {
		val, err := db.GetSetting(dbConn, key)
		if err != nil {
			t.Fatalf("GetSetting(%q): %v", key, err)
		}
		if val != "1" {
			t.Errorf("expected setting %q to be '1', got %q", key, val)
		}
	}

	// Verify default expedition was seeded
	var expName string
	if err := dbConn.QueryRow("SELECT name FROM expeditions LIMIT 1").Scan(&expName); err != nil {
		t.Fatalf("query default expedition: %v", err)
	}
	if expName != db.DefaultExpeditionName {
		t.Errorf("expected expedition %q, got %q", db.DefaultExpeditionName, expName)
	}

	// Verify idx_journal_events_source index exists
	var indexCount int
	if indexQueryError := dbConn.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_journal_events_source'").Scan(&indexCount); indexQueryError != nil {
		t.Fatalf("query sqlite_master for idx_journal_events_source: %v", indexQueryError)
	}
	if indexCount != 1 {
		t.Fatalf("expected index idx_journal_events_source to exist, found count %d", indexCount)
	}
}

func TestWithTxRollbackOnPanic(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatalf("Open: %v", openError)
	}
	defer dbConn.Close()

	var recoveredPanic any
	func() {
		defer func() {
			recoveredPanic = recover()
		}()
		_ = db.WithTx(dbConn, func(transaction *sql.Tx) error {
			if setError := db.SetSetting(transaction, "panic_rollback_key", "uncommitted_value"); setError != nil {
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
	persistedValue, getSettingError := db.GetSetting(dbConn, "panic_rollback_key")
	if getSettingError != nil {
		t.Fatalf("GetSetting after panic: %v", getSettingError)
	}
	if persistedValue != "" {
		t.Fatalf("expected uncommitted setting to be rolled back, got %q", persistedValue)
	}

	// Verify that the connection is not locked and subsequent transactions succeed
	secondTransactionError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return db.SetSetting(transaction, "panic_rollback_key", "committed_value")
	})
	if secondTransactionError != nil {
		t.Fatalf("subsequent WithTx failed: %v", secondTransactionError)
	}

	finalValue, getFinalSettingError := db.GetSetting(dbConn, "panic_rollback_key")
	if getFinalSettingError != nil {
		t.Fatalf("GetSetting after subsequent transaction: %v", getFinalSettingError)
	}
	if finalValue != "committed_value" {
		t.Fatalf("expected setting to be %q, got %q", "committed_value", finalValue)
	}
}

func TestCreateAndRemoveDebugSnapshot(testingContext *testing.T) {
	tempDir := testingContext.TempDir()
	sourceDBPath := filepath.Join(tempDir, "production.db")

	sourceConn, openSourceError := db.Open(sourceDBPath)
	if openSourceError != nil {
		testingContext.Fatalf("Open source DB: %v", openSourceError)
	}
	if setError := db.SetSetting(sourceConn, "snapshot_marker", "original_value"); setError != nil {
		sourceConn.Close()
		testingContext.Fatalf("SetSetting on source DB: %v", setError)
	}
	if closeError := sourceConn.Close(); closeError != nil {
		testingContext.Fatalf("Close source DB: %v", closeError)
	}

	originalSourceBytes, readSourceError := os.ReadFile(sourceDBPath)
	if readSourceError != nil {
		testingContext.Fatalf("ReadFile source DB: %v", readSourceError)
	}

	snapshotPath, snapshotError := db.CreateDebugSnapshot(sourceDBPath)
	if snapshotError != nil {
		testingContext.Fatalf("CreateDebugSnapshot: %v", snapshotError)
	}
	defer db.RemoveDebugSnapshot(snapshotPath)

	if snapshotPath == sourceDBPath {
		testingContext.Fatalf("expected snapshotPath to differ from sourceDBPath")
	}

	snapshotConn, openSnapshotError := db.Open(snapshotPath)
	if openSnapshotError != nil {
		testingContext.Fatalf("Open snapshot DB: %v", openSnapshotError)
	}
	markerValue, getMarkerError := db.GetSetting(snapshotConn, "snapshot_marker")
	if getMarkerError != nil {
		snapshotConn.Close()
		testingContext.Fatalf("GetSetting on snapshot DB: %v", getMarkerError)
	}
	if markerValue != "original_value" {
		snapshotConn.Close()
		testingContext.Fatalf("expected snapshot_marker 'original_value', got %q", markerValue)
	}

	if mutateError := db.SetSetting(snapshotConn, "snapshot_marker", "mutated_in_debug"); mutateError != nil {
		snapshotConn.Close()
		testingContext.Fatalf("mutate snapshot DB: %v", mutateError)
	}
	_ = snapshotConn.Close()

	afterSourceBytes, readAfterError := os.ReadFile(sourceDBPath)
	if readAfterError != nil {
		testingContext.Fatalf("ReadFile source DB after debug mutation: %v", readAfterError)
	}
	if !bytes.Equal(originalSourceBytes, afterSourceBytes) {
		testingContext.Fatalf("expected source database file bytes to remain completely unchanged")
	}

	db.RemoveDebugSnapshot(snapshotPath)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, statError := os.Stat(snapshotPath + suffix); !os.IsNotExist(statError) {
			testingContext.Errorf("expected %s%s to be removed, stat err: %v", snapshotPath, suffix, statError)
		}
	}

	// Empty, :memory:, missing, or zero-byte source paths allocate a removed temp path ready for db.Open.
	zeroBytePath := filepath.Join(tempDir, "empty.db")
	if writeEmptyError := os.WriteFile(zeroBytePath, nil, 0o600); writeEmptyError != nil {
		testingContext.Fatalf("WriteFile zeroBytePath: %v", writeEmptyError)
	}
	for _, emptySourceCandidate := range []string{"", ":memory:", filepath.Join(tempDir, "nonexistent.db"), zeroBytePath} {
		candidateSnapshotPath, candidateError := db.CreateDebugSnapshot(emptySourceCandidate)
		if candidateError != nil {
			testingContext.Fatalf("CreateDebugSnapshot(%q): %v", emptySourceCandidate, candidateError)
		}
		if _, statError := os.Stat(candidateSnapshotPath); !os.IsNotExist(statError) {
			db.RemoveDebugSnapshot(candidateSnapshotPath)
			testingContext.Fatalf("expected temp file %s for source %q to be removed before db.Open, stat err: %v", candidateSnapshotPath, emptySourceCandidate, statError)
		}
	}

	// A stat error other than ErrNotExist (such as a file component in the directory path) fails immediately without creating a snapshot.
	invalidNestedPath := filepath.Join(sourceDBPath, "nested.db")
	if failedSnapshotPath, invalidStatError := db.CreateDebugSnapshot(invalidNestedPath); invalidStatError == nil {
		db.RemoveDebugSnapshot(failedSnapshotPath)
		testingContext.Fatalf("expected error when stating invalid source path %s, got snapshot %s", invalidNestedPath, failedSnapshotPath)
	}
}

func TestDowngradeRoundTripSelfHealing(testingContext *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("Open: %v", openError)
	}
	defer dbConn.Close()

	lastVersion, versionError := db.GetSetting(dbConn, db.LastAppVersionKey)
	if versionError != nil {
		testingContext.Fatalf("GetSetting(%q): %v", db.LastAppVersionKey, versionError)
	}
	if lastVersion != version.Version {
		testingContext.Fatalf("expected %s to be %q, got %q", db.LastAppVersionKey, version.Version, lastVersion)
	}

	// Set a known watermark representing when v0.4.0 last ingested journals
	if setWatermarkError := db.SetSetting(dbConn, db.IngestSchemaWatermarkKey, "2026-05-01T00:00:00Z"); setWatermarkError != nil {
		testingContext.Fatalf("SetSetting watermark: %v", setWatermarkError)
	}

	// Insert one journal ingested before the watermark and one ingested after (while downgraded)
	_, insertOldJournalError := dbConn.Exec(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, updated_at)
		VALUES ('Journal.2026-04-01T000000.01.log', '/tmp/old.log', 4096, 50, 4096, 10, '2026-04-01T00:00:00Z')`)
	if insertOldJournalError != nil {
		testingContext.Fatalf("insert old journal: %v", insertOldJournalError)
	}

	_, insertDowngradedJournalError := dbConn.Exec(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, updated_at)
		VALUES ('Journal.2099-01-01T000000.01.log', '/tmp/downgraded.log', 8192, 100, 8192, 20, '2099-01-01T00:00:00Z')`)
	if insertDowngradedJournalError != nil {
		testingContext.Fatalf("insert downgraded journal: %v", insertDowngradedJournalError)
	}

	// Insert duplicate SellOrganicData events (one legacy raw_json UID + one canonical UID)
	rawOrganicJSON := `{"timestamp":"2026-04-15T12:00:00Z","event":"SellOrganicData","MarketID":12345,"BioData":[{"Species":"Stratum","Value":19000000,"Bonus":0}]}`
	_, insertLegacyOrganicError := dbConn.Exec(`INSERT INTO journal_events
		(event_uid, timestamp, event, raw_json, ingested_at)
		VALUES ('legacy-uid-from-v032', '2026-04-15T12:00:00Z', 'SellOrganicData', ?, '2099-01-01T00:00:00Z')`, rawOrganicJSON)
	if insertLegacyOrganicError != nil {
		testingContext.Fatalf("insert legacy SellOrganicData: %v", insertLegacyOrganicError)
	}
	_, insertDuplicateOrganicError := dbConn.Exec(`INSERT INTO journal_events
		(event_uid, timestamp, event, raw_json, ingested_at)
		VALUES ('second-legacy-uid-from-v032', '2026-04-15T12:00:00Z', 'SellOrganicData', ?, '2099-01-01T00:00:00Z')`, rawOrganicJSON)
	if insertDuplicateOrganicError != nil {
		testingContext.Fatalf("insert duplicate SellOrganicData: %v", insertDuplicateOrganicError)
	}

	// Re-running Migrate simulates upgrading back to v0.4.0+
	if migrateError := db.Migrate(dbConn); migrateError != nil {
		testingContext.Fatalf("Migrate on re-upgrade: %v", migrateError)
	}

	var oldOffset, oldLines int64
	if scanOldError := dbConn.QueryRow(`SELECT byte_offset, line_count FROM ingested_journals WHERE file_name = 'Journal.2026-04-01T000000.01.log'`).Scan(&oldOffset, &oldLines); scanOldError != nil {
		testingContext.Fatalf("query old journal offset: %v", scanOldError)
	}
	if oldOffset != 4096 || oldLines != 50 {
		testingContext.Errorf("expected pre-watermark journal offset/lines to remain 4096/50, got %d/%d", oldOffset, oldLines)
	}

	var healedOffset, healedLines int64
	if scanHealedError := dbConn.QueryRow(`SELECT byte_offset, line_count FROM ingested_journals WHERE file_name = 'Journal.2099-01-01T000000.01.log'`).Scan(&healedOffset, &healedLines); scanHealedError != nil {
		testingContext.Fatalf("query healed journal offset: %v", scanHealedError)
	}
	if healedOffset != 0 || healedLines != 0 {
		testingContext.Errorf("expected downgraded journal offset/lines to be reset to 0/0, got %d/%d", healedOffset, healedLines)
	}

	var organicEventCount int
	var survivingUID string
	if countOrganicError := dbConn.QueryRow(`SELECT COUNT(*), MIN(event_uid) FROM journal_events WHERE event = 'SellOrganicData'`).Scan(&organicEventCount, &survivingUID); countOrganicError != nil {
		testingContext.Fatalf("query SellOrganicData count: %v", countOrganicError)
	}
	if organicEventCount != 1 {
		testingContext.Errorf("expected duplicate SellOrganicData rows to be deduplicated to 1 row, got %d", organicEventCount)
	}
	if survivingUID == "legacy-uid-from-v032" || survivingUID == "second-legacy-uid-from-v032" {
		testingContext.Errorf("expected surviving SellOrganicData UID to be normalized to canonical hash, got %q", survivingUID)
	}

	updatedWatermark, getUpdatedWatermarkError := db.GetSetting(dbConn, db.IngestSchemaWatermarkKey)
	if getUpdatedWatermarkError != nil {
		testingContext.Fatalf("GetSetting updated watermark: %v", getUpdatedWatermarkError)
	}
	if updatedWatermark != "2099-01-01T00:00:00Z" {
		testingContext.Errorf("expected watermark to advance to '2099-01-01T00:00:00Z', got %q", updatedWatermark)
	}
}
