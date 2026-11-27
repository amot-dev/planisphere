package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"sort"
	"strings"
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
	{
		key:         "realization_events_v6",
		description: "Reset ingested_journals byte offsets for Died, Resurrect, NpcCrewPaidWage, and SellExplorationData",
		apply:       migrateRealizationEventsV6,
	},
	{
		key:         "source_file_index_v7",
		description: "Add index on journal_events source_file column to accelerate commander backfills",
		apply:       migrateSourceFileIndexV7,
	},
	{
		key:         "sell_organic_uid_dedup_v8",
		description: "Deduplicate SellOrganicData events and normalize their canonical event_uid",
		apply:       migrateSellOrganicUIDsV8,
	},
	{
		key:         "unarm_expeditions_v9",
		description: "Convert legacy armed expeditions lacking a start timestamp back to planning",
		apply:       migrateUnarmExpeditionsV9,
	},
	{
		key:         "disconnect_events_v10",
		description: "Reset ingested_journals byte offsets for StartJump, JetConeBoost, and FSD Synthesis",
		apply:       migrateDisconnectEventsV10,
	},
	{
		key:         "leg_modes_v11",
		description: "Backfill per-expedition active_mode_id and visited leg_mode_id from legacy mode_windows",
		apply:       migrateLegModesV11,
	},
	{
		key:         "departed_at_v12",
		description: "Backfill waypoint departed_at timestamps and normalize completed expedition end timestamps to final in-system activity",
		apply:       migrateDepartedAtV12,
	},
	{
		key:         "commander_profiles_v13",
		description: "Backfill journal FID and universe metadata and initialize commander save-slot profiles",
		apply:       migrateCommanderProfilesV13,
	},
	{
		key:         "single_expedition_commander_v14",
		description: "Backfill single commander_profile_id per expedition from selected scope or latest commander",
		apply:       migrateSingleExpeditionCommanderV14,
	},
}

// Migrate applies the embedded schema and runs all pending migrations. Idempotent.
func Migrate(dbConn *sql.DB) error {
	// Ensure the `commander` column exists on existing `journal_events` tables
	// before applying schema.sql (which creates idx_journal_events_cmdr).
	_, _ = dbConn.Exec(`ALTER TABLE journal_events ADD COLUMN commander TEXT`)
	_, _ = dbConn.Exec(`ALTER TABLE journal_events ADD COLUMN fid TEXT NOT NULL DEFAULT ''`)
	_, _ = dbConn.Exec(`ALTER TABLE journal_events ADD COLUMN universe TEXT NOT NULL DEFAULT 'live'`)
	_, _ = dbConn.Exec(`ALTER TABLE journal_events ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN part INTEGER NOT NULL DEFAULT 1`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN game_version TEXT NOT NULL DEFAULT ''`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN build TEXT NOT NULL DEFAULT ''`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN universe TEXT NOT NULL DEFAULT 'live'`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN continued_part INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN last_commander TEXT NOT NULL DEFAULT ''`)
	_, _ = dbConn.Exec(`ALTER TABLE ingested_journals ADD COLUMN last_fid TEXT NOT NULL DEFAULT ''`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN fc_arrived INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN leg_mode_id INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE expedition_rows ADD COLUMN departed_at TEXT`)
	_, _ = dbConn.Exec(`ALTER TABLE expeditions ADD COLUMN carrier_type TEXT NOT NULL DEFAULT 'player'`)
	_, _ = dbConn.Exec(`ALTER TABLE expeditions ADD COLUMN carrier_weekly_upkeep INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE expeditions ADD COLUMN active_mode_id INTEGER NOT NULL DEFAULT 0`)
	_, _ = dbConn.Exec(`ALTER TABLE expeditions ADD COLUMN commander_scope TEXT NOT NULL DEFAULT 'all_enabled'`)
	_, _ = dbConn.Exec(`ALTER TABLE expeditions ADD COLUMN commander_profile_id INTEGER NOT NULL DEFAULT 0`)
	if _, schemaError := dbConn.Exec(schemaSQL); schemaError != nil {
		return fmt.Errorf("apply schema: %w", schemaError)
	}

	for _, migrationStep := range migrations {
		settingValue, checkError := getMetaSetting(dbConn, migrationStep.key)
		if checkError != nil {
			return checkError
		}
		if settingValue != "" {
			continue
		}
		if applyError := migrationStep.apply(dbConn); applyError != nil {
			return applyError
		}
		if setSettingError := setMetaSetting(dbConn, migrationStep.key, "1"); setSettingError != nil {
			return setSettingError
		}
	}

	if healError := healDowngradedJournalIngest(dbConn); healError != nil {
		return healError
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
		_ = setMetaSetting(dbConn, "active_expedition_id", fmt.Sprint(expeditionID))
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
	if _, deleteError := dbConn.Exec(`DELETE FROM gec_pois_cache`); deleteError != nil {
		return fmt.Errorf("clear gec_pois_cache (v2): %w", deleteError)
	}
	if setSettingError := setMetaSetting(dbConn, "gec_fetched_at", ""); setSettingError != nil {
		return fmt.Errorf("reset gec_fetched_at (v2): %w", setSettingError)
	}
	return nil
}

func migrateCommanderSchemaV3(dbConn *sql.DB) error {
	if _, resetOffsetsError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`); resetOffsetsError != nil {
		return fmt.Errorf("reset ingested_journals offsets (v3): %w", resetOffsetsError)
	}
	if _, resetBaselineError := dbConn.Exec(`UPDATE expeditions SET baseline_json = json_set(baseline_json, '$.bodiesMapped', 0) WHERE baseline_source = 'statistics'`); resetBaselineError != nil {
		return fmt.Errorf("reset statistics bodiesMapped baseline (v3): %w", resetBaselineError)
	}
	return nil
}

func migrateFCArrivedV4(dbConn *sql.DB) error {
	if _, updateError := dbConn.Exec(`UPDATE expedition_rows SET fc_arrived = 1
		WHERE fc_stop = 1 AND visited_at IS NOT NULL AND visited_at != ''
		AND id IN (
			SELECT MIN(expedition_rows.id) FROM expedition_rows
			JOIN expeditions ON expeditions.id = expedition_rows.expedition_id
			WHERE expeditions.status IN ('active', 'completed') AND expedition_rows.row_type = 'waypoint'
			GROUP BY expedition_rows.expedition_id
		)`); updateError != nil {
		return fmt.Errorf("seed fc_arrived on initial waypoints (v4): %w", updateError)
	}
	return nil
}

func migrateGECSchemaV5(dbConn *sql.DB) error {
	if _, alterError := dbConn.Exec(`ALTER TABLE gec_pois_cache ADD COLUMN source TEXT NOT NULL DEFAULT 'GEC'`); alterError != nil && !strings.Contains(strings.ToLower(alterError.Error()), "duplicate column name") {
		return fmt.Errorf("add gec_pois_cache source column (v5): %w", alterError)
	}
	if _, deleteError := dbConn.Exec(`DELETE FROM gec_pois_cache`); deleteError != nil {
		return fmt.Errorf("clear gec_pois_cache (v5): %w", deleteError)
	}
	entityReplacements := []string{
		`UPDATE expedition_rows SET poi_name = replace(poi_name, '&#39;', '''') WHERE poi_name LIKE '%&#39;%'`,
		`UPDATE expedition_rows SET poi_name = replace(poi_name, '&amp;', '&') WHERE poi_name LIKE '%&amp;%'`,
		`UPDATE expedition_rows SET poi_name = replace(poi_name, '&quot;', '"') WHERE poi_name LIKE '%&quot;%'`,
	}
	for _, updateQuery := range entityReplacements {
		if _, updateError := dbConn.Exec(updateQuery); updateError != nil {
			return fmt.Errorf("unescape expedition_rows poi_name entities (v5): %w", updateError)
		}
	}
	if setSettingError := setMetaSetting(dbConn, "gec_fetched_at", ""); setSettingError != nil {
		return fmt.Errorf("reset gec_fetched_at (v5): %w", setSettingError)
	}
	return nil
}

func migrateRealizationEventsV6(dbConn *sql.DB) error {
	if _, updateError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`); updateError != nil {
		return updateError
	}
	return nil
}

func migrateSourceFileIndexV7(dbConn *sql.DB) error {
	if _, createIndexError := dbConn.Exec(`CREATE INDEX IF NOT EXISTS idx_journal_events_source ON journal_events(source_file)`); createIndexError != nil {
		return fmt.Errorf("create idx_journal_events_source: %w", createIndexError)
	}
	return nil
}

func migrateDisconnectEventsV10(dbConn *sql.DB) error {
	if _, updateError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0`); updateError != nil {
		return fmt.Errorf("reset ingested_journals offsets (v10): %w", updateError)
	}
	return nil
}

type legacyModeWindow struct {
	modeID int64
	start  string
	end    string
}

func resolveLegacyModeAt(windows []legacyModeWindow, fallbackModeID int64, targetTimestamp string) int64 {
	candidateIndex := sort.Search(len(windows), func(index int) bool {
		return windows[index].start > targetTimestamp
	}) - 1
	if candidateIndex >= 0 {
		windowItem := windows[candidateIndex]
		if targetTimestamp >= windowItem.start && (windowItem.end == "" || targetTimestamp < windowItem.end) {
			return windowItem.modeID
		}
	}
	return fallbackModeID
}

func resolveMigrationFallbackModeID(queryer Queryer) int64 {
	var modeID int64
	if queryError := queryer.QueryRow(`SELECT id FROM modes WHERE name = 'Travel' COLLATE NOCASE LIMIT 1`).Scan(&modeID); queryError == nil && modeID > 0 {
		return modeID
	}
	if queryError := queryer.QueryRow(`SELECT id FROM modes ORDER BY is_default DESC, sort_order ASC, id ASC LIMIT 1`).Scan(&modeID); queryError == nil && modeID > 0 {
		return modeID
	}
	return 0
}

func loadLegacyModeWindows(queryer Queryer) ([]legacyModeWindow, int64, error) {
	rows, queryError := queryer.Query(`SELECT mode_id, start_timestamp, COALESCE(end_timestamp, '') FROM mode_windows ORDER BY start_timestamp, id`)
	if queryError != nil {
		return nil, 0, fmt.Errorf("query legacy mode_windows (v11): %w", queryError)
	}
	defer rows.Close()

	var windows []legacyModeWindow
	var activeOpenModeID int64
	for rows.Next() {
		var windowItem legacyModeWindow
		if scanError := rows.Scan(&windowItem.modeID, &windowItem.start, &windowItem.end); scanError != nil {
			return nil, 0, fmt.Errorf("scan legacy mode_windows (v11): %w", scanError)
		}
		windows = append(windows, windowItem)
		if windowItem.end == "" {
			activeOpenModeID = windowItem.modeID
		}
	}
	return windows, activeOpenModeID, rows.Err()
}

func migrateLegModesV11(dbConn *sql.DB) error {
	fallbackModeID := resolveMigrationFallbackModeID(dbConn)
	if fallbackModeID == 0 {
		return nil
	}
	legacyWindows, activeOpenModeID, loadError := loadLegacyModeWindows(dbConn)
	if loadError != nil {
		return loadError
	}
	expeditionActiveModeID := fallbackModeID
	if activeOpenModeID > 0 {
		expeditionActiveModeID = activeOpenModeID
	}

	type pendingRowMode struct {
		rowID  int64
		modeID int64
	}
	rows, queryError := dbConn.Query(`
		SELECT r.id, r.visited_at
		FROM expedition_rows r
		WHERE r.row_type = 'waypoint'
		  AND COALESCE(r.visited_at, '') != ''
		  AND COALESCE(r.leg_mode_id, 0) = 0
		  AND r.id NOT IN (
			  SELECT first_rows.id
			  FROM (
				  SELECT id,
				         ROW_NUMBER() OVER (PARTITION BY expedition_id ORDER BY position, id) AS rn
				  FROM expedition_rows
				  WHERE row_type = 'waypoint'
			  ) first_rows
			  WHERE first_rows.rn = 1
		  )`)
	if queryError != nil {
		return fmt.Errorf("query unmapped visited rows (v11): %w", queryError)
	}
	var pendingUpdates []pendingRowMode
	for rows.Next() {
		var rowID int64
		var visitedAt string
		if scanError := rows.Scan(&rowID, &visitedAt); scanError != nil {
			rows.Close()
			return fmt.Errorf("scan unmapped visited row (v11): %w", scanError)
		}
		pendingUpdates = append(pendingUpdates, pendingRowMode{
			rowID:  rowID,
			modeID: resolveLegacyModeAt(legacyWindows, fallbackModeID, visitedAt),
		})
	}
	if iterationError := rows.Err(); iterationError != nil {
		rows.Close()
		return iterationError
	}
	if closeError := rows.Close(); closeError != nil {
		return closeError
	}

	return WithTx(dbConn, func(transaction *sql.Tx) error {
		if _, updateExpError := transaction.Exec(
			`UPDATE expeditions SET active_mode_id = ? WHERE COALESCE(active_mode_id, 0) = 0`,
			expeditionActiveModeID,
		); updateExpError != nil {
			return fmt.Errorf("backfill expedition active_mode_id (v11): %w", updateExpError)
		}
		for _, pending := range pendingUpdates {
			if _, updateRowError := transaction.Exec(
				`UPDATE expedition_rows SET leg_mode_id = ? WHERE id = ?`,
				pending.modeID, pending.rowID,
			); updateRowError != nil {
				return fmt.Errorf("backfill row %d leg_mode_id (v11): %w", pending.rowID, updateRowError)
			}
		}
		return nil
	})
}

func migrateUnarmExpeditionsV9(dbConn *sql.DB) error {
	if _, deleteRoutesError := dbConn.Exec(`DELETE FROM settings
		WHERE key IN (
			SELECT 'active_carrier_route_' || id FROM expeditions
			WHERE status = 'active' AND COALESCE(start_timestamp, '') = ''
		)`); deleteRoutesError != nil {
		return fmt.Errorf("clear carrier routes on armed expeditions (v9): %w", deleteRoutesError)
	}
	if _, resetRowsError := dbConn.Exec(`UPDATE expedition_rows
		SET fc_arrived = 0, visited_at = NULL, visit_event_uid = NULL, leg_mode_id = 0
		WHERE expedition_id IN (
			SELECT id FROM expeditions WHERE status = 'active' AND COALESCE(start_timestamp, '') = ''
		)`); resetRowsError != nil {
		return fmt.Errorf("reset rows on armed expeditions (v9): %w", resetRowsError)
	}
	if _, unarmError := dbConn.Exec(`UPDATE expeditions
		SET status = 'planning', armed_at = NULL
		WHERE status = 'active' AND COALESCE(start_timestamp, '') = ''`); unarmError != nil {
		return fmt.Errorf("migrate armed expeditions to planning (v9): %w", unarmError)
	}
	return nil
}

