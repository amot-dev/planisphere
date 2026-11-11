// Package db owns the embedded SQLite database (pure-Go modernc.org/sqlite).
package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (CGO-free)

	"planisphere/internal/version"
)

// AppDirName is the per-user directory holding the database and bootstrap configuration.
var AppDirName = version.ProductName

// DefaultDBFileName is the default database file name.
var DefaultDBFileName = version.BinaryName + ".db"

// UserDataDir returns the standard OS directory for user application data (databases, persistent state):
// - Linux/Unix: $XDG_DATA_HOME or ~/.local/share
// - Windows: %LOCALAPPDATA% or %USERPROFILE%\AppData\Local
// - macOS: ~/Library/Application Support
func UserDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return local, nil
		}
		home, homeDirError := os.UserHomeDir()
		if homeDirError != nil {
			return "", homeDirError
		}
		return filepath.Join(home, "AppData", "Local"), nil
	case "darwin":
		home, homeDirError := os.UserHomeDir()
		if homeDirError != nil {
			return "", homeDirError
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			return xdgData, nil
		}
		home, homeDirError := os.UserHomeDir()
		if homeDirError != nil {
			return "", homeDirError
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

// DefaultPath returns <UserDataDir>/Planisphere/planisphere.db.
// (e.g. ~/.local/share/Planisphere/planisphere.db on Linux,
// %LOCALAPPDATA%\Planisphere\planisphere.db on Windows).
func DefaultPath() (string, error) {
	dataDir, dataDirError := UserDataDir()
	if dataDirError != nil {
		return "", dataDirError
	}
	return filepath.Join(dataDir, AppDirName, DefaultDBFileName), nil
}

type bootstrapConfig struct {
	DBPath string `json:"dbPath"`
}

func bootstrapFilePath() (string, error) {
	configDir, configDirError := os.UserConfigDir()
	if configDirError != nil {
		return "", configDirError
	}
	return filepath.Join(configDir, AppDirName, "bootstrap.json"), nil
}

// ResolveConfiguredPath returns the user's configured database path from
// bootstrap.json if set, or DefaultPath() otherwise.
func ResolveConfiguredPath() (string, error) {
	if configFilePath, bootstrapError := bootstrapFilePath(); bootstrapError == nil {
		if data, readError := os.ReadFile(configFilePath); readError == nil {
			var config bootstrapConfig
			if json.Unmarshal(data, &config) == nil && strings.TrimSpace(config.DBPath) != "" {
				return filepath.Clean(strings.TrimSpace(config.DBPath)), nil
			}
		}
	}
	return DefaultPath()
}

// SaveConfiguredPath persists the user's chosen database path to bootstrap.json.
func SaveConfiguredPath(dbPath string) error {
	cfgFile, bootstrapError := bootstrapFilePath()
	if bootstrapError != nil {
		return bootstrapError
	}
	if mkdirError := os.MkdirAll(filepath.Dir(cfgFile), 0o755); mkdirError != nil {
		return mkdirError
	}
	data, marshalError := json.MarshalIndent(bootstrapConfig{DBPath: filepath.Clean(strings.TrimSpace(dbPath))}, "", "  ")
	if marshalError != nil {
		return marshalError
	}
	return os.WriteFile(cfgFile, append(data, '\n'), 0o644)
}

// CreateDebugSnapshot allocates a unique temporary database path in the OS
// temporary directory and, if sourcePath exists and is non-empty, copies
// sourcePath (along with any non-empty WAL companion file) using read-only
// file descriptors so the source database is never opened or mutated by SQLite.
func CreateDebugSnapshot(sourcePath string) (string, error) {
	tempFile, createTempError := os.CreateTemp("", "planisphere-debug-*.db")
	if createTempError != nil {
		return "", fmt.Errorf("create debug snapshot temp file: %w", createTempError)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()

	trimmedSourcePath := strings.TrimSpace(sourcePath)
	if trimmedSourcePath == "" || trimmedSourcePath == ":memory:" {
		_ = os.Remove(tempPath)
		return tempPath, nil
	}

	sourceInfo, statError := os.Stat(trimmedSourcePath)
	if errors.Is(statError, os.ErrNotExist) {
		_ = os.Remove(tempPath)
		return tempPath, nil
	}
	if statError != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("stat source database %s: %w", trimmedSourcePath, statError)
	}
	if sourceInfo.Size() == 0 {
		_ = os.Remove(tempPath)
		return tempPath, nil
	}

	if copyError := copyFileReadOnly(trimmedSourcePath, tempPath); copyError != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("copy source database %s to debug snapshot: %w", trimmedSourcePath, copyError)
	}

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

func copyFileReadOnly(sourcePath, destinationPath string) error {
	sourceFile, openSourceError := os.Open(sourcePath)
	if openSourceError != nil {
		return openSourceError
	}
	defer sourceFile.Close()

	destinationFile, openDestError := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if openDestError != nil {
		return openDestError
	}

	if _, copyError := io.Copy(destinationFile, sourceFile); copyError != nil {
		_ = destinationFile.Close()
		return copyError
	}
	return destinationFile.Close()
}

// Open opens (creating if needed) the database at path, applies the
// WAL / foreign key pragmas on every pooled connection, runs the schema and
// seeds default data.
func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if mkdirError := os.MkdirAll(filepath.Dir(path), 0o755); mkdirError != nil {
			return nil, fmt.Errorf("create db dir: %w", mkdirError)
		}
	}
	queryValues := url.Values{}
	queryValues.Add("_pragma", "journal_mode(WAL)")
	queryValues.Add("_pragma", "foreign_keys(1)")
	queryValues.Add("_pragma", "busy_timeout(10000)")
	queryValues.Add("_pragma", "synchronous(NORMAL)")
	// Take the write lock at BEGIN so concurrent writers (live tailer + UI)
	// wait on busy_timeout instead of failing with SQLITE_BUSY on upgrade.
	queryValues.Add("_txlock", "immediate")
	dsn := "file:" + path + "?" + queryValues.Encode()
	if path == ":memory:" {
		// A shared in-memory DB so every pooled connection sees the same data (tests).
		dsn = "file:memdb" + fmt.Sprint(time.Now().UnixNano()) + "?mode=memory&cache=shared&" + queryValues.Encode()
	}
	dbConn, openError := sql.Open("sqlite", dsn)
	if openError != nil {
		return nil, openError
	}
	if path == ":memory:" {
		dbConn.SetMaxOpenConns(1)
	} else {
		// Limit open connections to serialize writers in Go memory and avoid SQLite file-lock contention.
		dbConn.SetMaxOpenConns(4)
		dbConn.SetMaxIdleConns(4)
	}
	if _, pragmaError := dbConn.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA cache_size=-64000; PRAGMA temp_store=MEMORY;`); pragmaError != nil {
		dbConn.Close()
		return nil, fmt.Errorf("pragmas: %w", pragmaError)
	}
	if migrateError := Migrate(dbConn); migrateError != nil {
		dbConn.Close()
		return nil, migrateError
	}
	return dbConn, nil
}

// Queryer is satisfied by *sql.DB and *sql.Tx.
type Queryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Prepare(query string) (*sql.Stmt, error)
}

// GetSetting returns a setting value or "" when unset.
func GetSetting(queryer Queryer, key string) (string, error) {
	var settingValue string
	queryError := queryer.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&settingValue)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", nil
	}
	return settingValue, queryError
}

// SetSetting upserts a setting.
func SetSetting(queryer Queryer, key, value string) error {
	_, execError := queryer.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return execError
}

// WithTx runs txFunc inside a transaction.
func WithTx(dbConn *sql.DB, txFunc func(transaction *sql.Tx) error) (returnedError error) {
	transaction, beginError := dbConn.Begin()
	if beginError != nil {
		return beginError
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			_ = transaction.Rollback()
			panic(panicValue)
		} else if returnedError != nil {
			_ = transaction.Rollback()
		}
	}()

	if txError := txFunc(transaction); txError != nil {
		return txError
	}
	return transaction.Commit()
}
