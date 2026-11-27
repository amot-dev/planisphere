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

func TestDefaultPath(t *testing.T) {
	defaultPath, pathError := db.DefaultPath()
	if pathError != nil {
		t.Fatalf("DefaultPath: %v", pathError)
	}
	if defaultPath == "" {
		t.Fatalf("expected non-empty default path")
	}
}

func TestConfiguredDatabasePathResolutionAndFallback(testingContext *testing.T) {
	isolatedRoot := testingContext.TempDir()
	homeDir := filepath.Join(isolatedRoot, "home")
	xdgConfigDir := filepath.Join(isolatedRoot, "xdg-config")
	xdgDataDir := filepath.Join(isolatedRoot, "xdg-data")
	appDataDir := filepath.Join(isolatedRoot, "appdata")
	localAppDataDir := filepath.Join(isolatedRoot, "localappdata")
	for _, dirPath := range []string{homeDir, xdgConfigDir, xdgDataDir, appDataDir, localAppDataDir} {
		if mkdirError := os.MkdirAll(dirPath, 0o755); mkdirError != nil {
			testingContext.Fatalf("mkdir %s: %v", dirPath, mkdirError)
		}
	}
	testingContext.Setenv("HOME", homeDir)
	testingContext.Setenv("XDG_CONFIG_HOME", xdgConfigDir)
	testingContext.Setenv("XDG_DATA_HOME", xdgDataDir)
	testingContext.Setenv("APPDATA", appDataDir)
	testingContext.Setenv("LOCALAPPDATA", localAppDataDir)

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
