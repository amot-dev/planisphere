package db_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"planisphere/internal/core/db"
)

func TestCreateAndRemoveDebugSnapshot(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	sourceDBPath := filepath.Join(temporaryDirectory, "production.db")

	sourceConn, openSourceError := db.Open(sourceDBPath)
	if openSourceError != nil {
		testingContext.Fatalf("Open source DB: %v", openSourceError)
	}
	if setError := setSettingForTest(sourceConn, "snapshot_marker", "original_value"); setError != nil {
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
	markerValue, getMarkerError := getSettingForTest(snapshotConn, "snapshot_marker")
	if getMarkerError != nil {
		snapshotConn.Close()
		testingContext.Fatalf("getSettingForTest on snapshot DB: %v", getMarkerError)
	}
	if markerValue != "original_value" {
		snapshotConn.Close()
		testingContext.Fatalf("expected snapshot_marker 'original_value', got %q", markerValue)
	}

	if mutateError := setSettingForTest(snapshotConn, "snapshot_marker", "mutated_in_debug"); mutateError != nil {
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
	zeroBytePath := filepath.Join(temporaryDirectory, "empty.db")
	if writeEmptyError := os.WriteFile(zeroBytePath, nil, 0o600); writeEmptyError != nil {
		testingContext.Fatalf("WriteFile zeroBytePath: %v", writeEmptyError)
	}
	for _, emptySourceCandidate := range []string{"", ":memory:", filepath.Join(temporaryDirectory, "nonexistent.db"), zeroBytePath} {
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

func TestSnapshotTo(testingContext *testing.T) {
	temporaryDirectory := testingContext.TempDir()
	sourceDBPath := filepath.Join(temporaryDirectory, "live.db")
	destinationPath := filepath.Join(temporaryDirectory, "nested", "snap.db")

	// Keep the WAL-mode source connection open so writes sit in the WAL while snapshotting.
	sourceConn, openSourceError := db.Open(sourceDBPath)
	if openSourceError != nil {
		testingContext.Fatalf("Open source DB: %v", openSourceError)
	}
	defer sourceConn.Close()

	if setError := setSettingForTest(sourceConn, "wal_marker", "first_value"); setError != nil {
		testingContext.Fatalf("setSettingForTest first write: %v", setError)
	}

	if snapshotError := db.SnapshotTo(sourceDBPath, destinationPath); snapshotError != nil {
		testingContext.Fatalf("SnapshotTo initial: %v", snapshotError)
	}

	for _, sidecarSuffix := range []string{"-wal", "-shm"} {
		if _, statError := os.Stat(destinationPath + sidecarSuffix); !os.IsNotExist(statError) {
			testingContext.Fatalf("expected %s%s to be absent after SnapshotTo, stat err: %v", destinationPath, sidecarSuffix, statError)
		}
	}

	readOnlyConn, openReadOnlyError := sql.Open("sqlite", "file:"+destinationPath+"?mode=ro")
	if openReadOnlyError != nil {
		testingContext.Fatalf("open snapshot read-only: %v", openReadOnlyError)
	}
	initialValue, getInitialError := getSettingForTest(readOnlyConn, "wal_marker")
	_ = readOnlyConn.Close()
	if getInitialError != nil {
		testingContext.Fatalf("read initial snapshot value: %v", getInitialError)
	}
	if initialValue != "first_value" {
		testingContext.Fatalf("expected wal_marker 'first_value', got %q", initialValue)
	}

	// Overwriting an existing snapshot cleanly replaces it with newer writes.
	if setError := setSettingForTest(sourceConn, "wal_marker", "second_value"); setError != nil {
		testingContext.Fatalf("setSettingForTest second write: %v", setError)
	}
	if overwriteError := db.SnapshotTo(sourceDBPath, destinationPath); overwriteError != nil {
		testingContext.Fatalf("SnapshotTo overwrite: %v", overwriteError)
	}

	reopenedConn, reopenError := sql.Open("sqlite", "file:"+destinationPath+"?mode=ro")
	if reopenError != nil {
		testingContext.Fatalf("reopen overwritten snapshot: %v", reopenError)
	}
	updatedValue, getUpdatedError := getSettingForTest(reopenedConn, "wal_marker")
	_ = reopenedConn.Close()
	if getUpdatedError != nil {
		testingContext.Fatalf("read overwritten snapshot value: %v", getUpdatedError)
	}
	if updatedValue != "second_value" {
		testingContext.Fatalf("expected updated wal_marker 'second_value', got %q", updatedValue)
	}

	// Reject same-file overwrite, missing source, and empty source.
	if samePathError := db.SnapshotTo(sourceDBPath, sourceDBPath); samePathError == nil {
		testingContext.Fatalf("expected error when destination matches source")
	}
	if missingError := db.SnapshotTo(filepath.Join(temporaryDirectory, "missing.db"), destinationPath); missingError == nil {
		testingContext.Fatalf("expected error for missing source database")
	}
	zeroBytePath := filepath.Join(temporaryDirectory, "zero.db")
	if writeError := os.WriteFile(zeroBytePath, nil, 0o600); writeError != nil {
		testingContext.Fatalf("WriteFile zeroBytePath: %v", writeError)
	}
	if emptyError := db.SnapshotTo(zeroBytePath, destinationPath); emptyError == nil {
		testingContext.Fatalf("expected error for empty source database")
	}
}
