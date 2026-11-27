package db_test

import (
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/version"
)

func TestMigrateIdempotency(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)

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
		"sell_organic_uid_dedup_v8",
		"disconnect_events_v10",
		"commander_profiles_v13",
	}

	for _, key := range expectedKeys {
		settingValue, err := getSettingForTest(dbConn, key)
		if err != nil {
			t.Fatalf("getSettingForTest(%q): %v", key, err)
		}
		if settingValue != "1" {
			t.Errorf("expected setting %q to be '1', got %q", key, settingValue)
		}
	}

	// Verify default expedition was seeded
	var expeditionName string
	if err := dbConn.QueryRow("SELECT name FROM expeditions LIMIT 1").Scan(&expeditionName); err != nil {
		t.Fatalf("query default expedition: %v", err)
	}
	if expeditionName != db.DefaultExpeditionName {
		t.Errorf("expected expedition %q, got %q", db.DefaultExpeditionName, expeditionName)
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

func TestDowngradeRoundTripSelfHealing(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	lastVersion, versionError := getSettingForTest(dbConn, db.LastAppVersionKey)
	if versionError != nil {
		testingContext.Fatalf("getSettingForTest(%q): %v", db.LastAppVersionKey, versionError)
	}
	if lastVersion != version.Version {
		testingContext.Fatalf("expected %s to be %q, got %q", db.LastAppVersionKey, version.Version, lastVersion)
	}

	// Set a known watermark representing when v0.4.0 last ingested journals
	if setWatermarkError := setSettingForTest(dbConn, db.IngestSchemaWatermarkKey, "2026-05-01T00:00:00Z"); setWatermarkError != nil {
		testingContext.Fatalf("SetSetting watermark: %v", setWatermarkError)
	}

	// Insert one journal ingested before the watermark and one ingested after (while downgraded)
	_, insertOldJournalError := dbConn.Exec(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, updated_at)
		VALUES ('Journal.2026-04-01T000000.01.log', '/tmp/old.log', 4096, 50, 4096, 10, '2026-04-01T00:00:00Z')`)
	if insertOldJournalError != nil {
		testingContext.Fatalf("insert old journal: %v", insertOldJournalError)
	}

	_, insertDowngradedJournalError := dbConn.Exec(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, updated_at)
		VALUES ('Journal.2099-01-01T000000.01.log', '/tmp/downgraded.log', 8192, 100, 8192, 20, '2099-01-01T00:00:00Z')`)
	if insertDowngradedJournalError != nil {
		testingContext.Fatalf("insert downgraded journal: %v", insertDowngradedJournalError)
	}

	// Insert duplicate SellOrganicData events (one legacy raw_json UID + one canonical UID)
	rawOrganicJSON := `{"timestamp":"2026-04-15T12:00:00Z","event":"SellOrganicData","MarketID":12345,"BioData":[{"Species":"Stratum","Value":19000000,"Bonus":0}]}`
	_, insertLegacyOrganicError := dbConn.Exec(`INSERT INTO journal_events
		(event_uid, timestamp, event, raw_json, ingested_at)
		VALUES ('legacy-uid-from-v032', '2026-04-15T12:00:00Z', 'SellOrganicData', ?, '2099-01-01T00:00:00Z')`, rawOrganicJSON)
	if insertLegacyOrganicError != nil {
		testingContext.Fatalf("insert legacy SellOrganicData: %v", insertLegacyOrganicError)
	}
	_, insertDuplicateOrganicError := dbConn.Exec(`INSERT INTO journal_events
		(event_uid, timestamp, event, raw_json, ingested_at)
		VALUES ('second-legacy-uid-from-v032', '2026-04-15T12:00:00Z', 'SellOrganicData', ?, '2099-01-01T00:00:00Z')`, rawOrganicJSON)
	if insertDuplicateOrganicError != nil {
		testingContext.Fatalf("insert duplicate SellOrganicData: %v", insertDuplicateOrganicError)
	}

	// Insert a legacy armed expedition (status='active', start_timestamp=NULL, armed_at!=''); Migrate must revert it to planning and clear fc_arrived.
	armedInsertResult, insertArmedError := dbConn.Exec(`INSERT INTO expeditions (name, status, start_timestamp, armed_at, created_at, updated_at)
		VALUES ('Legacy Armed', 'active', NULL, '2026-04-20T00:00:00Z', '2026-04-20T00:00:00Z', '2026-04-20T00:00:00Z')`)
	if insertArmedError != nil {
		testingContext.Fatalf("insert armed expedition: %v", insertArmedError)
	}
	armedExpID, lastIDError := armedInsertResult.LastInsertId()
	if lastIDError != nil {
		testingContext.Fatalf("armed expedition LastInsertId: %v", lastIDError)
	}
	if _, insertArmedRowError := dbConn.Exec(`INSERT INTO expedition_rows (expedition_id, position, row_type, system_name, fc_stop, fc_arrived, created_at, updated_at)
		VALUES (?, 1, 'waypoint', 'Rohini', 1, 1, '2026-04-20T00:00:00Z', '2026-04-20T00:00:00Z')`, armedExpID); insertArmedRowError != nil {
		testingContext.Fatalf("insert armed expedition row: %v", insertArmedRowError)
	}

	// Re-running Migrate simulates upgrading back to v0.4.0+
	if migrateError := db.Migrate(dbConn); migrateError != nil {
		testingContext.Fatalf("Migrate on re-upgrade: %v", migrateError)
	}

	var migratedStatus string
	var migratedFcArrived int
	if scanArmedErr := dbConn.QueryRow(`SELECT expeditions.status, expedition_rows.fc_arrived
		FROM expeditions JOIN expedition_rows ON expedition_rows.expedition_id = expeditions.id
		WHERE expeditions.id = ?`, armedExpID).Scan(&migratedStatus, &migratedFcArrived); scanArmedErr != nil {
		testingContext.Fatalf("query migrated armed expedition: %v", scanArmedErr)
	}
	if migratedStatus != "planning" || migratedFcArrived != 0 {
		testingContext.Errorf("expected legacy armed expedition to migrate to planning with fc_arrived=0, got status=%q fc_arrived=%d", migratedStatus, migratedFcArrived)
	}

	var oldOffset, oldLines int64
	if scanOldError := dbConn.QueryRow(`SELECT byte_offset, line_count FROM ingested_journals WHERE file_name = 'Journal.2026-04-01T000000.01.log'`).Scan(&oldOffset, &oldLines); scanOldError != nil {
		testingContext.Fatalf("query old journal offset: %v", scanOldError)
	}
	if oldOffset != 4096 || oldLines != 50 {
		testingContext.Errorf("expected pre-watermark journal offset/lines to remain 4096/50, got %d/%d", oldOffset, oldLines)
	}

	var healedOffset, healedLines int64
	if scanHealedError := dbConn.QueryRow(`SELECT byte_offset, line_count FROM ingested_journals WHERE file_name = 'Journal.2099-01-01T000000.01.log'`).Scan(&healedOffset, &healedLines); scanHealedError != nil {
		testingContext.Fatalf("query healed journal offset: %v", scanHealedError)
	}
	if healedOffset != 0 || healedLines != 0 {
		testingContext.Errorf("expected downgraded journal offset/lines to be reset to 0/0, got %d/%d", healedOffset, healedLines)
	}

	var organicEventCount int
	var survivingUID string
	if countOrganicError := dbConn.QueryRow(`SELECT COUNT(*), MIN(event_uid) FROM journal_events WHERE event = 'SellOrganicData'`).Scan(&organicEventCount, &survivingUID); countOrganicError != nil {
		testingContext.Fatalf("query SellOrganicData count: %v", countOrganicError)
	}
	if organicEventCount != 1 {
		testingContext.Errorf("expected duplicate SellOrganicData rows to be deduplicated to 1 row, got %d", organicEventCount)
	}
	if survivingUID == "legacy-uid-from-v032" || survivingUID == "second-legacy-uid-from-v032" {
		testingContext.Errorf("expected surviving SellOrganicData UID to be normalized to canonical hash, got %q", survivingUID)
	}

	updatedWatermark, getUpdatedWatermarkError := getSettingForTest(dbConn, db.IngestSchemaWatermarkKey)
	if getUpdatedWatermarkError != nil {
		testingContext.Fatalf("getSettingForTest updated watermark: %v", getUpdatedWatermarkError)
	}
	if updatedWatermark != "2099-01-01T00:00:00Z" {
		testingContext.Errorf("expected watermark to advance to '2099-01-01T00:00:00Z', got %q", updatedWatermark)
	}
}

func TestMigrateLegModesV11BackfillFromLegacyModeWindows(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	lookupDefaultModeID := func(modeName string) int64 {
		testingContext.Helper()
		var modeID int64
		if err := databaseConnection.QueryRow(`SELECT id FROM modes WHERE name = ? AND is_default = 1`, modeName).Scan(&modeID); err != nil {
			testingContext.Fatalf("query default flight mode %q: %v", modeName, err)
		}
		return modeID
	}
	explorationID := lookupDefaultModeID("Exploration")
	tourismID := lookupDefaultModeID("Tourism")
	travelID := lookupDefaultModeID("Travel")

	insertExpResult, insertExpError := databaseConnection.Exec(`
		INSERT INTO expeditions(name, status, start_timestamp, active_mode_id, created_at, updated_at)
		VALUES ('Legacy Upgrade', 'active', '2026-08-01T10:00:00Z', 0, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`)
	if insertExpError != nil {
		testingContext.Fatalf("insert Legacy Upgrade expedition: %v", insertExpError)
	}
	expeditionID, lastIDError := insertExpResult.LastInsertId()
	if lastIDError != nil {
		testingContext.Fatalf("LastInsertId Legacy Upgrade: %v", lastIDError)
	}

	// Simulate a pre-v11 database state with legacy mode_windows and unmapped active_mode_id / leg_mode_id.
	if _, execError := databaseConnection.Exec(`
		INSERT INTO expedition_rows(expedition_id, position, row_type, system_name, visited_at, leg_mode_id, created_at, updated_at) VALUES
		(?, 1, 'waypoint', 'Sol', '2026-08-01T10:00:00Z', 0, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
		(?, 2, 'waypoint', 'Alpha', '2026-08-01T11:00:00Z', 0, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
		(?, 3, 'waypoint', 'Beta', '2026-08-01T12:00:00Z', 0, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`,
		expeditionID, expeditionID, expeditionID); execError != nil {
		testingContext.Fatalf("seed legacy expedition_rows: %v", execError)
	}
	if _, execError := databaseConnection.Exec(`
		INSERT INTO mode_windows(mode_id, start_timestamp, end_timestamp) VALUES
		(?, '2026-08-01T10:30:00Z', '2026-08-01T11:30:00Z'),
		(?, '2026-08-01T13:00:00Z', NULL)`,
		explorationID, tourismID); execError != nil {
		testingContext.Fatalf("seed legacy mode_windows: %v", execError)
	}
	if _, execError := databaseConnection.Exec(`DELETE FROM settings WHERE key = 'leg_modes_v11'`); execError != nil {
		testingContext.Fatalf("clear leg_modes_v11 migration flag: %v", execError)
	}

	if migrateError := db.Migrate(databaseConnection); migrateError != nil {
		testingContext.Fatalf("db.Migrate v11: %v", migrateError)
	}

	var migratedActiveModeID int64
	if err := databaseConnection.QueryRow(`SELECT COALESCE(active_mode_id, 0) FROM expeditions WHERE id = ?`, expeditionID).Scan(&migratedActiveModeID); err != nil {
		testingContext.Fatalf("query migrated active_mode_id: %v", err)
	}
	if migratedActiveModeID != tourismID {
		testingContext.Fatalf("expected migrated active_mode_id=%d from open mode_window, got %d", tourismID, migratedActiveModeID)
	}

	rows, queryErr := databaseConnection.Query(`SELECT COALESCE(leg_mode_id, 0) FROM expedition_rows WHERE expedition_id = ? ORDER BY position`, expeditionID)
	if queryErr != nil {
		testingContext.Fatalf("query migrated expedition_rows: %v", queryErr)
	}
	defer rows.Close()
	var legModes []int64
	for rows.Next() {
		var legModeID int64
		if err := rows.Scan(&legModeID); err != nil {
			testingContext.Fatalf("scan leg_mode_id: %v", err)
		}
		legModes = append(legModes, legModeID)
	}
	if err := rows.Err(); err != nil {
		testingContext.Fatalf("iterate migrated expedition_rows: %v", err)
	}
	if len(legModes) != 3 || legModes[0] != 0 || legModes[1] != explorationID || legModes[2] != travelID {
		testingContext.Fatalf("expected migrated leg modes [0, %d, %d], got %v", explorationID, travelID, legModes)
	}
}

func TestMigrateDepartedAtV12(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	// Seed journal history: Sol -> Alpha (with scan) -> Beta (with scan + later departure + post-departure scan) + multicrew noise.
	if _, execError := dbConn.Exec(`
		INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, body_id, body_name, commander, source_file, raw_json, ingested_at) VALUES
		('v12-sol', '2026-08-01T10:00:00Z', 'Location', 'Sol', 1001, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001}', '2026-08-01T10:00:00Z'),
		('v12-mc', '2026-08-01T10:05:00Z', 'FSDJump', 'Colonia', 9999, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T10:05:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":9999,"Multicrew":true}', '2026-08-01T10:05:00Z'),
		('v12-alpha', '2026-08-01T11:00:00Z', 'FSDJump', 'Alpha', 1002, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":1002}', '2026-08-01T11:00:00Z'),
		('v12-beta', '2026-08-01T12:00:00Z', 'FSDJump', 'Beta', 1003, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:00:00Z","event":"FSDJump","StarSystem":"Beta","SystemAddress":1003}', '2026-08-01T12:00:00Z'),
		('v12-beta-scan', '2026-08-01T12:25:00Z', 'Scan', 'Beta', 1003, 1, 'Beta 1', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:25:00Z","event":"Scan","BodyName":"Beta 1","PlanetClass":"Water world"}', '2026-08-01T12:25:00Z'),
		('v12-after', '2026-08-05T18:00:00Z', 'FSDJump', 'Gamma', 1004, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-05T18:00:00Z","event":"FSDJump","StarSystem":"Gamma","SystemAddress":1004}', '2026-08-05T18:00:00Z'),
		('v12-after-scan', '2026-08-05T18:10:00Z', 'Scan', 'Gamma', 1004, 1, 'Gamma 1', 'CmdrA', 't.log', '{"timestamp":"2026-08-05T18:10:00Z","event":"Scan","BodyName":"Gamma 1","PlanetClass":"Earthlike body"}', '2026-08-05T18:10:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed v12 journal events: %v", execError)
	}

	// Create 3 pre-v12 expeditions: active, completed post-7fabd35 (end == arrival), completed pre-7fabd35 (end > departure, with trailing unvisited row).
	insertExpedition := func(name, status, startTS, endTS string) int64 {
		testingContext.Helper()
		res, err := dbConn.Exec(`INSERT INTO expeditions(name, status, start_timestamp, end_timestamp, created_at, updated_at) VALUES(?, ?, ?, NULLIF(?, ''), '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`,
			name, status, startTS, endTS)
		if err != nil {
			testingContext.Fatalf("insert expedition %s: %v", name, err)
		}
		id, lastIDError := res.LastInsertId()
		if lastIDError != nil {
			testingContext.Fatalf("last insert id for expedition %s: %v", name, lastIDError)
		}
		return id
	}
	activeID := insertExpedition("V12 Active", "active", "2026-08-01T10:00:00Z", "")
	compArrivalID := insertExpedition("V12 Completed Arrival", "completed", "2026-08-01T10:00:00Z", "2026-08-01T12:00:00Z")
	compLegacyID := insertExpedition("V12 Completed Legacy", "completed", "2026-08-01T10:00:00Z", "2026-08-06T00:00:00Z")

	seedRows := func(expID int64, includeTrailingUnvisited bool) {
		testingContext.Helper()
		if _, err := dbConn.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, system_name, system_address, visited_at, departed_at, created_at, updated_at) VALUES
			(?, 1, 'waypoint', 'Sol', 1001, '2026-08-01T10:00:00Z', NULL, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
			(?, 2, 'waypoint', 'Alpha', 1002, '2026-08-01T11:00:00Z', NULL, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
			(?, 3, 'waypoint', 'Beta', 1003, '2026-08-01T12:00:00Z', NULL, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`,
			expID, expID, expID); err != nil {
			testingContext.Fatalf("seed rows exp %d: %v", expID, err)
		}
		if includeTrailingUnvisited {
			if _, err := dbConn.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, system_name, system_address, visited_at, departed_at, created_at, updated_at) VALUES
				(?, 4, 'waypoint', 'Gamma', 1004, NULL, NULL, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`, expID); err != nil {
				testingContext.Fatalf("seed unvisited row exp %d: %v", expID, err)
			}
		}
	}
	seedRows(activeID, false)
	seedRows(compArrivalID, false)
	seedRows(compLegacyID, true)

	// Re-trigger migration v12.
	if _, err := dbConn.Exec(`DELETE FROM settings WHERE key = 'departed_at_v12'`); err != nil {
		testingContext.Fatalf("delete departed_at_v12 key: %v", err)
	}
	if migrateError := db.Migrate(dbConn); migrateError != nil {
		testingContext.Fatalf("db.Migrate v12: %v", migrateError)
	}

	readDepartures := func(expID int64) (endTS string, departures []string, visited []string) {
		testingContext.Helper()
		if scanEndErr := dbConn.QueryRow(`SELECT COALESCE(end_timestamp, '') FROM expeditions WHERE id = ?`, expID).Scan(&endTS); scanEndErr != nil {
			testingContext.Fatalf("query end_timestamp exp %d: %v", expID, scanEndErr)
		}
		rows, queryErr := dbConn.Query(`SELECT COALESCE(visited_at, ''), COALESCE(departed_at, '') FROM expedition_rows WHERE expedition_id = ? ORDER BY position`, expID)
		if queryErr != nil {
			testingContext.Fatalf("query expedition_rows exp %d: %v", expID, queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var vis, dep string
			if scanErr := rows.Scan(&vis, &dep); scanErr != nil {
				testingContext.Fatalf("scan expedition_row exp %d: %v", expID, scanErr)
			}
			visited = append(visited, vis)
			departures = append(departures, dep)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			testingContext.Fatalf("iterate expedition_rows exp %d: %v", expID, rowsErr)
		}
		return endTS, departures, visited
	}

	// Active expedition keeps all 3 departures (ignoring multicrew at 10:05).
	activeEnd, activeDeps, _ := readDepartures(activeID)
	if activeEnd != "" || len(activeDeps) != 3 || activeDeps[0] != "2026-08-01T11:00:00Z" || activeDeps[1] != "2026-08-01T12:00:00Z" || activeDeps[2] != "2026-08-05T18:00:00Z" {
		testingContext.Fatalf("unexpected active v12 result: end=%q deps=%v", activeEnd, activeDeps)
	}

	// Both completed expeditions normalize end_timestamp to Beta 1 scan (12:25), clear post-end departure on Beta, and leave trailing Gamma unvisited.
	for _, completedID := range []int64{compArrivalID, compLegacyID} {
		compEnd, compDeps, compVisited := readDepartures(completedID)
		if compEnd != "2026-08-01T12:25:00Z" {
			testingContext.Fatalf("exp %d expected normalized end_timestamp=2026-08-01T12:25:00Z, got %q", completedID, compEnd)
		}
		if compDeps[0] != "2026-08-01T11:00:00Z" || compDeps[1] != "2026-08-01T12:00:00Z" || compDeps[2] != "" {
			testingContext.Fatalf("exp %d unexpected completed departures: %v", completedID, compDeps)
		}
		if completedID == compLegacyID && (len(compVisited) != 4 || compVisited[3] != "" || compDeps[3] != "") {
			testingContext.Fatalf("exp %d trailing unvisited row must remain untouched: visited=%v deps=%v", completedID, compVisited, compDeps)
		}
	}

	testingContext.Run("open_final_stay_scoping", func(subtestContext *testing.T) {
		openStayConn := dbtest.OpenMemory(subtestContext)

		if _, execError := openStayConn.Exec(`
			INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrA', 'FA', 1), ('CmdrB', 'FB', 1);
			INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, body_id, body_name, commander, source_file, raw_json, ingested_at) VALUES
			('open-sol', '2026-08-01T10:00:00Z', 'Location', 'Sol', 1001, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001}', '2026-08-01T10:00:00Z'),
			('open-beta', '2026-08-01T12:00:00Z', 'FSDJump', 'Beta', 1003, NULL, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:00:00Z","event":"FSDJump","StarSystem":"Beta","SystemAddress":1003}', '2026-08-01T12:00:00Z'),
			('open-beta-saa', '2026-08-01T12:15:00Z', 'SAAScanComplete', NULL, 1003, 1, 'Beta 1', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:15:00Z","event":"SAAScanComplete","BodyName":"Beta 1","SystemAddress":1003,"BodyID":1,"EfficiencyTarget":6}', '2026-08-01T12:15:00Z'),
			('open-other-sys', '2026-08-01T12:30:00Z', 'Scan', 'Gamma', 1004, 1, 'Gamma 1', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:30:00Z","event":"Scan","BodyName":"Gamma 1","PlanetClass":"Water world"}', '2026-08-01T12:30:00Z'),
			('open-other-cmdr', '2026-08-01T12:35:00Z', 'SAAScanComplete', NULL, 1003, 2, 'Beta 2', 'CmdrB', 't.log', '{"timestamp":"2026-08-01T12:35:00Z","event":"SAAScanComplete","BodyName":"Beta 2","SystemAddress":1003,"BodyID":2,"EfficiencyTarget":6}', '2026-08-01T12:35:00Z'),
			('open-multicrew', '2026-08-01T12:40:00Z', 'ScanOrganic', NULL, 1003, 1, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:40:00Z","event":"ScanOrganic","ScanType":"Analyse","SystemAddress":1003,"Body":1,"Multicrew":true}', '2026-08-01T12:40:00Z');
		`); execError != nil {
			subtestContext.Fatalf("seed open final stay events: %v", execError)
		}
		insertResult, expInsertError := openStayConn.Exec(`
			INSERT INTO expeditions(name, status, start_timestamp, end_timestamp, created_at, updated_at)
			VALUES ('Open Stay', 'completed', '2026-08-01T10:00:00Z', '2026-08-01T12:00:00Z', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')`)
		if expInsertError != nil {
			subtestContext.Fatalf("insert open stay expedition: %v", expInsertError)
		}
		openStayID, lastIDError := insertResult.LastInsertId()
		if lastIDError != nil {
			subtestContext.Fatalf("LastInsertId: %v", lastIDError)
		}
		if _, execError := openStayConn.Exec(`
			INSERT INTO expedition_rows(expedition_id, position, row_type, system_name, system_address, visited_at, departed_at, created_at, updated_at) VALUES
			(?, 1, 'waypoint', 'Sol', 1001, '2026-08-01T10:00:00Z', NULL, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
			(?, 2, 'waypoint', 'Beta', NULL, '2026-08-01T12:00:00Z', NULL, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z');
			DELETE FROM settings WHERE key = 'departed_at_v12';
		`, openStayID, openStayID); execError != nil {
			subtestContext.Fatalf("seed open final stay rows: %v", execError)
		}
		if subMigrateError := db.Migrate(openStayConn); subMigrateError != nil {
			subtestContext.Fatalf("db.Migrate open stay SAAScanComplete: %v", subMigrateError)
		}
		var endAfterSAA string
		if scanError := openStayConn.QueryRow(`SELECT COALESCE(end_timestamp, '') FROM expeditions WHERE id = ?`, openStayID).Scan(&endAfterSAA); scanError != nil {
			subtestContext.Fatalf("query end_timestamp after SAAScanComplete: %v", scanError)
		}
		if endAfterSAA != "2026-08-01T12:15:00Z" {
			subtestContext.Fatalf("end_timestamp after SAAScanComplete = %q, want %q", endAfterSAA, "2026-08-01T12:15:00Z")
		}

		if _, execError := openStayConn.Exec(`
			INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, body_id, body_name, commander, source_file, raw_json, ingested_at) VALUES
			('open-beta-org', '2026-08-01T12:20:00Z', 'ScanOrganic', NULL, 1003, 1, '', 'CmdrA', 't.log', '{"timestamp":"2026-08-01T12:20:00Z","event":"ScanOrganic","ScanType":"Analyse","SystemAddress":1003,"Body":1}', '2026-08-01T12:20:00Z');
			DELETE FROM settings WHERE key = 'departed_at_v12';
		`); execError != nil {
			subtestContext.Fatalf("insert in-system ScanOrganic: %v", execError)
		}
		if subMigrateError := db.Migrate(openStayConn); subMigrateError != nil {
			subtestContext.Fatalf("db.Migrate open stay ScanOrganic: %v", subMigrateError)
		}
		var endAfterOrganic string
		if scanError := openStayConn.QueryRow(`SELECT COALESCE(end_timestamp, '') FROM expeditions WHERE id = ?`, openStayID).Scan(&endAfterOrganic); scanError != nil {
			subtestContext.Fatalf("query end_timestamp after ScanOrganic: %v", scanError)
		}
		if endAfterOrganic != "2026-08-01T12:20:00Z" {
			subtestContext.Fatalf("end_timestamp after ScanOrganic = %q, want %q", endAfterOrganic, "2026-08-01T12:20:00Z")
		}
	})
}
