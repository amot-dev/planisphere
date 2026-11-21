package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateDebugSnapshot allocates a unique temporary database path in the OS
// temporary directory and, if sourcePath exists and is non-empty, copies
// sourcePath (along with any non-empty WAL companion file) using read-only
// file descriptors so the source database is never opened or mutated by SQLite.
func CreateDebugSnapshot(sourcePath string) (string, error) {
	trimmedSourcePath := strings.TrimSpace(sourcePath)
	hasNonEmptySource := false
	if trimmedSourcePath != "" && trimmedSourcePath != ":memory:" {
		sourceInfo, statError := os.Stat(trimmedSourcePath)
		if statError != nil && !errors.Is(statError, os.ErrNotExist) {
			return "", fmt.Errorf("stat source database %s: %w", trimmedSourcePath, statError)
		}
		hasNonEmptySource = statError == nil && sourceInfo.Size() > 0
	}

	tempFile, createTempError := os.CreateTemp("", "planisphere-debug-*.db")
	if createTempError != nil {
		return "", fmt.Errorf("create debug snapshot temp file: %w", createTempError)
	}
	tempPath := tempFile.Name()

	if !hasNonEmptySource {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return tempPath, nil
	}

	if copyError := copyToOpenFile(trimmedSourcePath, tempFile); copyError != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("copy source database %s to debug snapshot: %w", trimmedSourcePath, copyError)
	}

	// Copy any non-empty -wal companion file alongside the main database file so
	// uncheckpointed WAL frames are preserved in the snapshot. The -shm companion
	// file is intentionally omitted because SQLite reconstructs the shared-memory
	// WAL index from -wal on open, and copying a live -shm file could carry stale
	// lock state.
	sourceWALPath := trimmedSourcePath + "-wal"
	if walInfo, walStatError := os.Stat(sourceWALPath); walStatError == nil && walInfo.Size() > 0 {
		if walCopyError := copyFileReadOnly(sourceWALPath, tempPath+"-wal"); walCopyError != nil {
			RemoveDebugSnapshot(tempPath)
			return "", fmt.Errorf("copy source WAL %s to debug snapshot: %w", sourceWALPath, walCopyError)
		}
	}

	return tempPath, nil
}

// RemoveDebugSnapshot deletes a temporary debug snapshot database along with
// any SQLite WAL or shared-memory companion files.
func RemoveDebugSnapshot(tempPath string) {
	trimmedTempPath := strings.TrimSpace(tempPath)
	if trimmedTempPath == "" || trimmedTempPath == ":memory:" {
		return
	}
	_ = os.Remove(trimmedTempPath)
	_ = os.Remove(trimmedTempPath + "-wal")
	_ = os.Remove(trimmedTempPath + "-shm")
}

// SnapshotTo copies an existing SQLite database at sourcePath (along with any
// non-empty WAL companion file) to destinationPath using read-only file
// descriptors, then checkpoints and switches the copy to DELETE journal mode
// so destinationPath is a single self-contained database file.
func SnapshotTo(sourcePath, destinationPath string) error {
	trimmedSourcePath := strings.TrimSpace(sourcePath)
	trimmedDestinationPath := strings.TrimSpace(destinationPath)
	if trimmedSourcePath == "" || trimmedSourcePath == ":memory:" {
		return fmt.Errorf("source database path must be a non-empty file path")
	}
	if trimmedDestinationPath == "" || trimmedDestinationPath == ":memory:" {
		return fmt.Errorf("destination snapshot path must be a non-empty file path")
	}

	sourceInfo, statError := os.Stat(trimmedSourcePath)
	if statError != nil {
		return fmt.Errorf("stat source database %s: %w", trimmedSourcePath, statError)
	}
	if sourceInfo.Size() == 0 {
		return fmt.Errorf("source database %s is empty", trimmedSourcePath)
	}

	sameFile, sameFileError := pathsReferToSameFile(trimmedSourcePath, trimmedDestinationPath, sourceInfo)
	if sameFileError != nil {
		return sameFileError
	}
	if sameFile {
		return fmt.Errorf("destination snapshot path %s must differ from source database %s", trimmedDestinationPath, trimmedSourcePath)
	}

	if mkdirError := os.MkdirAll(filepath.Dir(trimmedDestinationPath), 0o755); mkdirError != nil {
		return fmt.Errorf("create snapshot directory for %s: %w", trimmedDestinationPath, mkdirError)
	}

	// Remove any stale destination database and WAL/SHM sidecars before copying
	// so leftover frames from an earlier snapshot cannot corrupt the new copy.
	RemoveDebugSnapshot(trimmedDestinationPath)

	if copyError := copyFileReadOnly(trimmedSourcePath, trimmedDestinationPath); copyError != nil {
		RemoveDebugSnapshot(trimmedDestinationPath)
		return fmt.Errorf("copy source database %s to %s: %w", trimmedSourcePath, trimmedDestinationPath, copyError)
	}

	sourceWALPath := trimmedSourcePath + "-wal"
	if walInfo, walStatError := os.Stat(sourceWALPath); walStatError == nil && walInfo.Size() > 0 {
		if walCopyError := copyFileReadOnly(sourceWALPath, trimmedDestinationPath+"-wal"); walCopyError != nil {
			RemoveDebugSnapshot(trimmedDestinationPath)
			return fmt.Errorf("copy source WAL %s to %s: %w", sourceWALPath, trimmedDestinationPath+"-wal", walCopyError)
		}
	}

	if consolidateError := consolidateSnapshotWAL(trimmedDestinationPath); consolidateError != nil {
		RemoveDebugSnapshot(trimmedDestinationPath)
		return fmt.Errorf("consolidate snapshot WAL %s: %w", trimmedDestinationPath, consolidateError)
	}
	return nil
}

func pathsReferToSameFile(sourcePath, destinationPath string, sourceInfo os.FileInfo) (bool, error) {
	absSource, absSourceError := filepath.Abs(sourcePath)
	if absSourceError != nil {
		return false, fmt.Errorf("resolve source path %s: %w", sourcePath, absSourceError)
	}
	absDestination, absDestinationError := filepath.Abs(destinationPath)
	if absDestinationError != nil {
		return false, fmt.Errorf("resolve destination path %s: %w", destinationPath, absDestinationError)
	}
	if absSource == absDestination {
		return true, nil
	}
	if destInfo, destStatError := os.Stat(destinationPath); destStatError == nil {
		return os.SameFile(sourceInfo, destInfo), nil
	}
	return false, nil
}

func consolidateSnapshotWAL(destinationPath string) error {
	snapshotConn, openError := sql.Open("sqlite", destinationPath)
	if openError != nil {
		return openError
	}
	snapshotConn.SetMaxOpenConns(1)

	_, pragmaError := snapshotConn.Exec(`PRAGMA wal_checkpoint(TRUNCATE); PRAGMA journal_mode=DELETE;`)
	closeError := snapshotConn.Close()
	if pragmaError != nil {
		return pragmaError
	}
	if closeError != nil {
		return closeError
	}

	_ = os.Remove(destinationPath + "-wal")
	_ = os.Remove(destinationPath + "-shm")
	return nil
}

func copyToOpenFile(sourcePath string, destinationFile *os.File) error {
	sourceFile, openSourceError := os.Open(sourcePath)
	if openSourceError != nil {
		_ = destinationFile.Close()
		return openSourceError
	}
	defer sourceFile.Close()

	if _, copyError := io.Copy(destinationFile, sourceFile); copyError != nil {
		_ = destinationFile.Close()
		return copyError
	}
	if syncError := destinationFile.Sync(); syncError != nil {
		_ = destinationFile.Close()
		return syncError
	}
	return destinationFile.Close()
}

func copyFileReadOnly(sourcePath, destinationPath string) error {
	destinationFile, openDestinationError := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if openDestinationError != nil {
		return openDestinationError
	}
	return copyToOpenFile(sourcePath, destinationFile)
}
