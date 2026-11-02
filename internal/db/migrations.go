package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"
)

//go:embed schema.sql
var schemaSQL string

// DefaultModes are seeded on first initialisation.
var DefaultModes = []struct {
	Name, Description, Color string
}{
	{"Exploration", "Dedicated deep-space surveying, system scanning, mapping, and exobiology in the black.", "#00B3FF"},
	{"Tourism", "Visiting scenic POIs, galactic wonders, memorials, and sightseeing destinations.", "#C77DFF"},
	{"Travel", "High-speed transit, neutron highway sprints, and Bubble commutes.", "#FF7100"},
}

// DefaultExpeditionName and DefaultExpeditionDescription are seeded on first initialisation.
const (
	DefaultExpeditionName        = "Deep Space Expedition I"
	DefaultExpeditionDescription = "Deep Space Exploration and Scientific Survey Route"
)

type migration struct {
	key         string
	description string
	apply       func(dbConn *sql.DB) error
}

var migrations = []migration{
	{
		key:         "default_modes_seeded",
		description: "Seed default exploration, tourism, and travel modes",
		apply:       migrateDefaultModesSeeded,
	},
	{
		key:         "default_expedition_seeded",
		description: "Seed initial default expedition if none exist",
		apply:       migrateDefaultExpeditionSeeded,
	},
	{
		key:         "ingest_parser_v2",
		description: "Reset ingested_journals byte offsets once for float64 FuelLevel re-read",
		apply:       migrateIngestParserV2,
	},
	{
		key:         "gec_schema_v2",
		description: "Remove legacy GMP entries lacking poiUrl and force refresh",
		apply:       migrateGECSchemaV2,
	},
	{
		key:         "commander_schema_v3",
		description: "Reset ingested_journals byte offsets for commander tagging and reset statistics baseline",
		apply:       migrateCommanderSchemaV3,
	},
	{
		key:         "fc_arrived_v4",
		description: "Seed carrier arrival on first waypoint of active/completed expeditions",
		apply:       migrateFCArrivedV4,
	},
	{
		key:         "gec_schema_v5",
		description: "Add source column to gec_pois_cache, unescape HTML entities in expedition rows, and reset gec cache",
		apply:       migrateGECSchemaV5,
	},
}

// Migrate applies the embedded schema and runs all pending migrations. Idempotent.
func Migrate(dbConn *sql.DB) error {
	// Ensure the `commander` column exists on existing `journal_events` tables
	// before applying schema.sql (which creates idx_journal_events_cmdr).
	_, _ = dbConn.Exec(`ALTER TABLE journal_events ADD COLUMN commander TEXT`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN fc_arrived INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN leg_mode_id INTEGER NOT NULL DEFAULT 0`)
	if _, schemaError := dbConn.Exec(schemaSQL); schemaError != nil {
		return fmt.Errorf("apply schema: %w", schemaError)
	}

	for _, m := range migrations {
		val, checkError := GetSetting(dbConn, m.key)
		if checkError != nil {
			return checkError
		}
		if val != "" {
			continue
		}
		if applyError := m.apply(dbConn); applyError != nil {
			return applyError
		}
		if setSettingError := SetSetting(dbConn, m.key, "1"); setSettingError != nil {
			return setSettingError
		}
	}

	return nil
}

func migrateDefaultModesSeeded(dbConn *sql.DB) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	for i, defaultMode := range DefaultModes {
		if _, seedError := dbConn.Exec(`INSERT OR IGNORE INTO modes(name, description, color, is_default, sort_order, created_at)
			VALUES(?, ?, ?, 1, ?, ?)`, defaultMode.Name, defaultMode.Description, defaultMode.Color, i, now); seedError != nil {
			return fmt.Errorf("seed modes: %w", seedError)
		}
	}
	return nil
}

func migrateDefaultExpeditionSeeded(dbConn *sql.DB) error {
	var count int
	if err := dbConn.QueryRow(`SELECT count(*) FROM expeditions`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		insertResult, insertError := dbConn.Exec(`INSERT INTO expeditions(name, description, status, created_at, updated_at)
			VALUES(?, ?, 'planning', ?, ?)`,
			DefaultExpeditionName, DefaultExpeditionDescription, now, now)
		if insertError != nil {
			return fmt.Errorf("seed expedition: %w", insertError)
		}
		expeditionID, lastInsertError := insertResult.LastInsertId()
		if lastInsertError != nil {
			return fmt.Errorf("seed expedition id: %w", lastInsertError)
		}
		if _, rowError := dbConn.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at)
			VALUES(?, 1, 'waypoint', ?, ?)`, expeditionID, now, now); rowError != nil {
			return fmt.Errorf("seed expedition row: %w", rowError)
		}
		_ = SetSetting(dbConn, "active_expedition_id", fmt.Sprint(expeditionID))
	}
	return nil
}

func migrateIngestParserV2(dbConn *sql.DB) error {
	if _, updateError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`); updateError != nil {
		return updateError
	}
	return nil
}

func migrateGECSchemaV2(dbConn *sql.DB) error {
	_, _ = dbConn.Exec(`DELETE FROM gec_pois_cache`)
	_ = SetSetting(dbConn, "gec_fetched_at", "")
	return nil
}

func migrateCommanderSchemaV3(dbConn *sql.DB) error {
	_, _ = dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`)
	_, _ = dbConn.Exec(`UPDATE expeditions SET baseline_json = json_set(baseline_json, '$.bodiesMapped', 0) WHERE baseline_source = 'statistics'`)
	return nil
}

func migrateFCArrivedV4(dbConn *sql.DB) error {
	_, _ = dbConn.Exec(`UPDATE expedition_rows SET fc_arrived = 1
		WHERE fc_stop = 1 AND visited_at IS NOT NULL AND visited_at != ''
		AND id IN (
			SELECT MIN(expedition_rows.id) FROM expedition_rows
			JOIN expeditions ON expeditions.id = expedition_rows.expedition_id
			WHERE expeditions.status IN ('active', 'completed') AND expedition_rows.row_type = 'waypoint'
			GROUP BY expedition_rows.expedition_id
		)`)
	return nil
}

func migrateGECSchemaV5(dbConn *sql.DB) error {
	_, _ = dbConn.Exec(`ALTER TABLE gec_pois_cache ADD COLUMN source TEXT NOT NULL DEFAULT 'GEC'`)
	_, _ = dbConn.Exec(`DELETE FROM gec_pois_cache`)
	_, _ = dbConn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&#39;', '''') WHERE poi_name LIKE '%&#39;%'`)
	_, _ = dbConn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&amp;', '&') WHERE poi_name LIKE '%&amp;%'`)
	_, _ = dbConn.Exec(`UPDATE expedition_rows SET poi_name = replace(poi_name, '&quot;', '"') WHERE poi_name LIKE '%&quot;%'`)
	_ = SetSetting(dbConn, "gec_fetched_at", "")
	return nil
}
