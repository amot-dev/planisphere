package db_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/db"
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
