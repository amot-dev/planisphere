// Package db owns the embedded SQLite database (pure-Go modernc.org/sqlite).
package db

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (CGO-free)

	"planisphere/internal/version"
)

//go:embed schema.sql
var schemaSQL string

// AppDirName is the per-user directory holding the database and bootstrap configuration.
var AppDirName = version.ProductName

// DefaultDBFileName is the default database file name.
var DefaultDBFileName = version.BinaryName + ".db"

// DefaultModes are seeded on first initialisation.
var DefaultModes = []struct {
	Name, Description, Color string
}{
	{"Exploration", "Dedicated deep-space surveying, system scanning, mapping, and exobiology in the black.", "#00B3FF"},
	{"Tourism", "Visiting scenic POIs, galactic wonders, memorials, and sightseeing destinations.", "#C77DFF"},
	{"Travel", "High-speed transit, neutron highway sprints, and Bubble commutes.", "#FF7100"},
}

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

// Migrate applies the embedded schema and seeds default modes. Idempotent.
func Migrate(dbConn *sql.DB) error {
	// Ensure the `commander` column exists on existing `journal_events` tables
	// before applying schema.sql (which creates idx_journal_events_cmdr).
	_, _ = dbConn.Exec(`ALTER TABLE journal_events ADD COLUMN commander TEXT`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN fc_arrived INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN leg_mode_id INTEGER NOT NULL DEFAULT 0`)
	if _, schemaError := dbConn.Exec(schemaSQL); schemaError != nil {
		return fmt.Errorf("apply schema: %w", schemaError)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	seeded, seedCheckError := GetSetting(dbConn, "default_modes_seeded")
	if seedCheckError != nil {
		return seedCheckError
	}
	if seeded == "" {
		for i, defaultMode := range DefaultModes {
			if _, seedError := dbConn.Exec(`INSERT OR IGNORE INTO modes(name, description, color, is_default, sort_order, created_at)
				VALUES(?, ?, ?, 1, ?, ?)`, defaultMode.Name, defaultMode.Description, defaultMode.Color, i, now); seedError != nil {
				return fmt.Errorf("seed modes: %w", seedError)
			}
		}
		if setSettingError := SetSetting(dbConn, "default_modes_seeded", "1"); setSettingError != nil {
			return setSettingError
		}
	}
	// Schema v2 migration: reset ingested_journals byte offsets once so files
	// where FSDJump/LoadGame float64 FuelLevel lines were previously skipped
	// are re-read idempotently (journal_events deduplicates via event_uid).
	ingestV2, ingestCheckError := GetSetting(dbConn, "ingest_parser_v2")
	if ingestCheckError != nil {
		return ingestCheckError
	}
	if ingestV2 == "" {
		if _, updateError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`); updateError != nil {
			return updateError
		}
		if setSettingError := SetSetting(dbConn, "ingest_parser_v2", "1"); setSettingError != nil {
			return setSettingError
		}
	}
	// Schema v2 GEC cleanup: remove legacy GMP entries that lacked poiUrl or
	// overwrote GEC entries with overlapping numeric IDs, and force a GEC refresh.
	gecV2, gecCheckError := GetSetting(dbConn, "gec_schema_v2")
	if gecCheckError != nil {
		return gecCheckError
	}
	if gecV2 == "" {
		_, _ = dbConn.Exec(`DELETE FROM gec_pois_cache`)
		_ = SetSetting(dbConn, "gec_fetched_at", "")
		if setSettingError := SetSetting(dbConn, "gec_schema_v2", "1"); setSettingError != nil {
			return setSettingError
		}
	}
	// Schema v3 commander tagging: reset ingested_journals byte offsets once so
	// existing events get their commander column populated from each file's LoadGame/Commander event.
	cmdrV3, cmdrCheckError := GetSetting(dbConn, "commander_schema_v3")
	if cmdrCheckError != nil {
		return cmdrCheckError
	}
	if cmdrV3 == "" {
		_, _ = dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`)
		_, _ = dbConn.Exec(`UPDATE expeditions SET baseline_json = json_set(baseline_json, '$.bodiesMapped', 0) WHERE baseline_source = 'statistics'`)
		if setSettingError := SetSetting(dbConn, "commander_schema_v3", "1"); setSettingError != nil {
			return setSettingError
		}
	}
	// Schema v4 carrier arrival seeding: mark Waypoint #1 (first waypoint) as
	// fc_arrived = 1 on already-started expeditions if it is an fc_stop.
	fcArrivedSetting, fcArrivedCheckError := GetSetting(dbConn, "fc_arrived_v4")
	if fcArrivedCheckError != nil {
		return fcArrivedCheckError
	}
	if fcArrivedSetting == "" {
		_, _ = dbConn.Exec(`UPDATE expedition_rows SET fc_arrived = 1
			WHERE fc_stop = 1 AND visited_at IS NOT NULL AND visited_at != ''
			AND id IN (
				SELECT MIN(expedition_rows.id) FROM expedition_rows
				JOIN expeditions ON expeditions.id = expedition_rows.expedition_id
				WHERE expeditions.status IN ('active', 'completed') AND expedition_rows.row_type = 'waypoint'
				GROUP BY expedition_rows.expedition_id
			)`)
		if setSettingError := SetSetting(dbConn, "fc_arrived_v4", "1"); setSettingError != nil {
			return setSettingError
		}
	}
	// Schema v5 GEC+GMP cache: support both GEC and GMP POIs from combined.json
	// without ID collisions, storing GMP entries with source = 'GMP'.
	gecSchemaSetting, gecSchemaCheckError := GetSetting(dbConn, "gec_schema_v5")
	if gecSchemaCheckError != nil {
		return gecSchemaCheckError
	}
	if gecSchemaSetting == "" {
		_, _ = dbConn.Exec(`ALTER TABLE gec_pois_cache ADD COLUMN source TEXT NOT NULL DEFAULT 'GEC'`)
		_, _ = dbConn.Exec(`DELETE FROM gec_pois_cache`)
		_, _ = dbConn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&#39;', '''') WHERE poi_name LIKE '%&#39;%'`)
		_, _ = dbConn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&amp;', '&') WHERE poi_name LIKE '%&amp;%'`)
		_, _ = dbConn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&quot;', '"') WHERE poi_name LIKE '%&quot;%'`)
		_ = SetSetting(dbConn, "gec_fetched_at", "")
		if setSettingError := SetSetting(dbConn, "gec_schema_v5", "1"); setSettingError != nil {
			return setSettingError
		}
	}
	return nil
}

// Queryer is satisfied by *sql.DB and *sql.Tx.
type Queryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
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
func WithTx(dbConn *sql.DB, txFunc func(transaction *sql.Tx) error) error {
	transaction, beginError := dbConn.Begin()
	if beginError != nil {
		return beginError
	}
	if txError := txFunc(transaction); txError != nil {
		_ = transaction.Rollback()
		return txError
	}
	return transaction.Commit()
}
