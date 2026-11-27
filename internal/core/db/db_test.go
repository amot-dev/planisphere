package db_test

import (
	"database/sql"
	"errors"
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

func TestDefaultPath(t *testing.T) {
	defaultPath, pathError := db.DefaultPath()
	if pathError != nil {
		t.Fatalf("DefaultPath: %v", pathError)
	}
	if defaultPath == "" {
		t.Fatalf("expected non-empty default path")
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
