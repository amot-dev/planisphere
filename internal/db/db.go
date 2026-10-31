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
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			return xdgData, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

// DefaultPath returns <UserDataDir>/Planisphere/planisphere.db.
// (e.g. ~/.local/share/Planisphere/planisphere.db on Linux,
// %LOCALAPPDATA%\Planisphere\planisphere.db on Windows).
func DefaultPath() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AppDirName, DefaultDBFileName), nil
}

type bootstrapConfig struct {
	DBPath string `json:"dbPath"`
}

func bootstrapFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, AppDirName, "bootstrap.json"), nil
}

// ResolveConfiguredPath returns the user's configured database path from
// bootstrap.json if set, or DefaultPath() otherwise.
func ResolveConfiguredPath() (string, error) {
	if cfgFile, err := bootstrapFilePath(); err == nil {
		if data, err := os.ReadFile(cfgFile); err == nil {
			var cfg bootstrapConfig
			if json.Unmarshal(data, &cfg) == nil && strings.TrimSpace(cfg.DBPath) != "" {
				return filepath.Clean(strings.TrimSpace(cfg.DBPath)), nil
			}
		}
	}
	return DefaultPath()
}

// SaveConfiguredPath persists the user's chosen database path to bootstrap.json.
func SaveConfiguredPath(dbPath string) error {
	cfgFile, err := bootstrapFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bootstrapConfig{DBPath: filepath.Clean(strings.TrimSpace(dbPath))}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgFile, append(data, '\n'), 0o644)
}

// Open opens (creating if needed) the database at path, applies the
// WAL / foreign key pragmas on every pooled connection, runs the schema and
// seeds default data.
func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	// Take the write lock at BEGIN so concurrent writers (live tailer + UI)
	// wait on busy_timeout instead of failing with SQLITE_BUSY on upgrade.
	q.Add("_txlock", "immediate")
	dsn := "file:" + path + "?" + q.Encode()
	if path == ":memory:" {
		// A shared in-memory DB so every pooled connection sees the same data (tests).
		dsn = "file:memdb" + fmt.Sprint(time.Now().UnixNano()) + "?mode=memory&cache=shared&" + q.Encode()
	}
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		conn.SetMaxOpenConns(1)
	}
	if _, err := conn.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA synchronous=NORMAL; PRAGMA cache_size=-64000; PRAGMA temp_store=MEMORY;`); err != nil {
		conn.Close()
		return nil, fmt.Errorf("pragmas: %w", err)
	}
	if err := Migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// Migrate applies the embedded schema and seeds default modes. Idempotent.
func Migrate(conn *sql.DB) error {
	// Ensure the `commander` column exists on existing `journal_events` tables
	// before applying schema.sql (which creates idx_journal_events_cmdr).
	_, _ = conn.Exec(`ALTER TABLE journal_events ADD COLUMN commander TEXT`)
	_, _ = conn.Exec(`ALTER TABLE expedition_rows ADD COLUMN fc_arrived INTEGER NOT NULL DEFAULT 0`)
	_, _ = conn.Exec(`ALTER TABLE expedition_rows ADD COLUMN leg_mode_id INTEGER NOT NULL DEFAULT 0`)
	if _, err := conn.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	seeded, err := GetSetting(conn, "default_modes_seeded")
	if err != nil {
		return err
	}
	if seeded == "" {
		for i, m := range DefaultModes {
			if _, err := conn.Exec(`INSERT OR IGNORE INTO modes(name, description, color, is_default, sort_order, created_at)
				VALUES(?, ?, ?, 1, ?, ?)`, m.Name, m.Description, m.Color, i, now); err != nil {
				return fmt.Errorf("seed modes: %w", err)
			}
		}
		if err := SetSetting(conn, "default_modes_seeded", "1"); err != nil {
			return err
		}
	}
	// Schema v2 migration: reset ingested_journals byte offsets once so files
	// where FSDJump/LoadGame float64 FuelLevel lines were previously skipped
	// are re-read idempotently (journal_events deduplicates via event_uid).
	ingestV2, err := GetSetting(conn, "ingest_parser_v2")
	if err != nil {
		return err
	}
	if ingestV2 == "" {
		if _, err := conn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`); err != nil {
			return err
		}
		if err := SetSetting(conn, "ingest_parser_v2", "1"); err != nil {
			return err
		}
	}
	// Schema v2 GEC cleanup: remove legacy GMP entries that lacked poiUrl or
	// overwrote GEC entries with overlapping numeric IDs, and force a GEC refresh.
	gecV2, err := GetSetting(conn, "gec_schema_v2")
	if err != nil {
		return err
	}
	if gecV2 == "" {
		_, _ = conn.Exec(`DELETE FROM gec_pois_cache`)
		_ = SetSetting(conn, "gec_fetched_at", "")
		if err := SetSetting(conn, "gec_schema_v2", "1"); err != nil {
			return err
		}
	}
	// Schema v3 commander tagging: reset ingested_journals byte offsets once so
	// existing events get their commander column populated from each file's LoadGame/Commander event.
	cmdrV3, err := GetSetting(conn, "commander_schema_v3")
	if err != nil {
		return err
	}
	if cmdrV3 == "" {
		_, _ = conn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`)
		_, _ = conn.Exec(`UPDATE expeditions SET baseline_json = json_set(baseline_json, '$.bodiesMapped', 0) WHERE baseline_source = 'statistics'`)
		if err := SetSetting(conn, "commander_schema_v3", "1"); err != nil {
			return err
		}
	}
	// Schema v4 carrier arrival seeding: mark Waypoint #1 (first waypoint) as
	// fc_arrived = 1 on already-started expeditions if it is an fc_stop.
	fcArrV4, err := GetSetting(conn, "fc_arrived_v4")
	if err != nil {
		return err
	}
	if fcArrV4 == "" {
		_, _ = conn.Exec(`UPDATE expedition_rows SET fc_arrived = 1
			WHERE fc_stop = 1 AND visited_at IS NOT NULL AND visited_at != ''
			AND id IN (
				SELECT MIN(r.id) FROM expedition_rows r
				JOIN expeditions e ON e.id = r.expedition_id
				WHERE e.status IN ('active', 'completed') AND r.row_type = 'waypoint'
				GROUP BY r.expedition_id
			)`)
		if err := SetSetting(conn, "fc_arrived_v4", "1"); err != nil {
			return err
		}
	}
	// Schema v5 GEC+GMP cache: support both GEC and GMP POIs from combined.json
	// without ID collisions, storing GMP entries with source = 'GMP'.
	gecV5, err := GetSetting(conn, "gec_schema_v5")
	if err != nil {
		return err
	}
	if gecV5 == "" {
		_, _ = conn.Exec(`ALTER TABLE gec_pois_cache ADD COLUMN source TEXT NOT NULL DEFAULT 'GEC'`)
		_, _ = conn.Exec(`DELETE FROM gec_pois_cache`)
		_, _ = conn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&#39;', '''') WHERE poi_name LIKE '%&#39;%'`)
		_, _ = conn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&amp;', '&') WHERE poi_name LIKE '%&amp;%'`)
		_, _ = conn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&quot;', '"') WHERE poi_name LIKE '%&quot;%'`)
		_ = SetSetting(conn, "gec_fetched_at", "")
		if err := SetSetting(conn, "gec_schema_v5", "1"); err != nil {
			return err
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
func GetSetting(q Queryer, key string) (string, error) {
	var v string
	err := q.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting upserts a setting.
func SetSetting(q Queryer, key, value string) error {
	_, err := q.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// WithTx runs fn inside a transaction.
func WithTx(conn *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
