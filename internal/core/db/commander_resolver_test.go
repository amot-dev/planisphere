package db_test

import (
	"database/sql"
	"fmt"
	"testing"

	"planisphere/internal/core/db"
)

type seedEventRow struct {
	uid        string
	timestamp  string
	event      string
	commander  string
	fid        string
	universe   string
	sourceFile string
	rawJSON    string
}

type gotProfile struct {
	id                int64
	profileKey        string
	displayName       string
	fid               string
	universe          string
	eraIndex          int
	isArchivedCareer  int
	hasManualOverride int
	enabled           int
	firstSeen         string
	lastSeen          string
}

type gotAlias struct {
	profileKey string
	rawName    string
	fid        string
	universe   string
	startTS    string
	endTS      string
	eventCount int
	isCurrent  int
	source     string
}

func openResolverTestDB(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	conn, err := db.Open(":memory:")
	if err != nil {
		testingContext.Fatalf("Open: %v", err)
	}
	testingContext.Cleanup(func() { _ = conn.Close() })
	return conn
}

func insertSeedEvents(testingContext *testing.T, conn *sql.DB, events []seedEventRow) {
	testingContext.Helper()
	for index, item := range events {
		uid := item.uid
		if uid == "" {
			uid = fmt.Sprintf("uid-%d-%s-%s", index, item.event, item.timestamp)
		}
		universe := item.universe
		if universe == "" {
			universe = "live"
		}
		rawJSON := item.rawJSON
		if rawJSON == "" {
			rawJSON = "{}"
		}
		var cmdrValue any
		if item.commander != "" {
			cmdrValue = item.commander
		}
		if _, err := conn.Exec(`
			INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, source_file, raw_json, ingested_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uid, item.timestamp, item.event, cmdrValue, item.fid, universe, item.sourceFile, rawJSON, item.timestamp,
		); err != nil {
			testingContext.Fatalf("insert seed event %s: %v", uid, err)
		}
	}
}

func runResolveCommanders(testingContext *testing.T, conn *sql.DB) {
	testingContext.Helper()
	if err := db.WithTx(conn, db.ResolveCommanders); err != nil {
		testingContext.Fatalf("ResolveCommanders: %v", err)
	}
}

func queryProfiles(testingContext *testing.T, conn *sql.DB) []gotProfile {
	testingContext.Helper()
	rows, err := conn.Query(`
		SELECT id, profile_key, display_name, fid, universe, era_index, is_archived_career, has_manual_override, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '')
		FROM commander_profiles ORDER BY id ASC`)
	if err != nil {
		testingContext.Fatalf("query commander_profiles: %v", err)
	}
	defer rows.Close()
	var profiles []gotProfile
	for rows.Next() {
		var profileRow gotProfile
		if scanErr := rows.Scan(&profileRow.id, &profileRow.profileKey, &profileRow.displayName, &profileRow.fid, &profileRow.universe, &profileRow.eraIndex, &profileRow.isArchivedCareer, &profileRow.hasManualOverride, &profileRow.enabled, &profileRow.firstSeen, &profileRow.lastSeen); scanErr != nil {
			testingContext.Fatalf("scan profile: %v", scanErr)
		}
		profiles = append(profiles, profileRow)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		testingContext.Fatalf("iterate commander_profiles: %v", rowsErr)
	}
	return profiles
}

func queryAliases(testingContext *testing.T, conn *sql.DB) []gotAlias {
	testingContext.Helper()
	rows, err := conn.Query(`
		SELECT cp.profile_key, ca.raw_name, ca.fid, ca.universe, ca.start_timestamp, ca.end_timestamp, ca.event_count, ca.is_current, ca.source
		FROM commander_aliases ca
		JOIN commander_profiles cp ON cp.id = ca.profile_id
		ORDER BY cp.profile_key ASC, ca.start_timestamp ASC, ca.raw_name ASC`)
	if err != nil {
		testingContext.Fatalf("query commander_aliases: %v", err)
	}
	defer rows.Close()
	var aliases []gotAlias
	for rows.Next() {
		var aliasRow gotAlias
		if scanErr := rows.Scan(&aliasRow.profileKey, &aliasRow.rawName, &aliasRow.fid, &aliasRow.universe, &aliasRow.startTS, &aliasRow.endTS, &aliasRow.eventCount, &aliasRow.isCurrent, &aliasRow.source); scanErr != nil {
			testingContext.Fatalf("scan alias: %v", scanErr)
		}
		aliases = append(aliases, aliasRow)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		testingContext.Fatalf("iterate commander_aliases: %v", rowsErr)
	}
	return aliases
}

func TestResolveCommandersScenarios(testingContext *testing.T) {
	testCases := []struct {
		name         string
		events       []seedEventRow
		wantProfiles []gotProfile
		wantAliases  []gotAlias
	}{
		{
			name: "distinct FID alt accounts stay separate live profiles",
			events: []seedEventRow{
				{timestamp: "2025-01-01T10:00:00Z", event: "LoadGame", commander: "CmdrMain", fid: "F101", universe: "live"},
				{timestamp: "2025-01-01T10:05:00Z", event: "FSDJump", commander: "CmdrMain", fid: "F101", universe: "live"},
				{timestamp: "2025-02-01T10:00:00Z", event: "LoadGame", commander: "CmdrAlt", fid: "F202", universe: "live"},
				{timestamp: "2025-02-01T10:05:00Z", event: "FSDJump", commander: "CmdrAlt", fid: "F202", universe: "live"},
			},
			wantProfiles: []gotProfile{
				{profileKey: "live:f101:era:0", displayName: "CmdrMain", fid: "F101", universe: "live", eraIndex: 0, isArchivedCareer: 0, enabled: 1},
				{profileKey: "live:f202:era:0", displayName: "CmdrAlt", fid: "F202", universe: "live", eraIndex: 0, isArchivedCareer: 0, enabled: 1},
			},
		},
		{
			name: "same FID rename with pre-v3 empty FID bridging unifies into one profile",
			events: []seedEventRow{
				{timestamp: "2017-06-01T10:00:00Z", event: "LoadGame", commander: "Trebor Johnson", fid: "", universe: "live"},
				{timestamp: "2017-06-01T10:10:00Z", event: "FSDJump", commander: "Trebor Johnson", fid: "", universe: "live"},
				{timestamp: "2020-03-01T10:00:00Z", event: "LoadGame", commander: "Trebor Johnson", fid: "F4293989", universe: "live"},
				{timestamp: "2025-05-01T10:00:00Z", event: "LoadGame", commander: "Danielle Hudson", fid: "F4293989", universe: "live"},
				{timestamp: "2025-05-01T10:15:00Z", event: "FSDJump", commander: "Danielle Hudson", fid: "F4293989", universe: "live"},
			},
			wantProfiles: []gotProfile{
				{profileKey: "live:f4293989:era:0", displayName: "Danielle Hudson", fid: "F4293989", universe: "live", eraIndex: 0, isArchivedCareer: 0, enabled: 1},
			},
			wantAliases: []gotAlias{
				{profileKey: "live:f4293989:era:0", rawName: "Trebor Johnson", fid: "F4293989", universe: "live", eventCount: 3, isCurrent: 0, source: "auto"},
				{profileKey: "live:f4293989:era:0", rawName: "Danielle Hudson", fid: "F4293989", universe: "live", eventCount: 2, isCurrent: 1, source: "auto"},
			},
		},
		{
			name: "initial NewCommander stays era 0 and later ClearSavedGame plus NewCommander splits rename career",
			events: []seedEventRow{
				{uid: "init-new", timestamp: "2024-01-01T10:00:00Z", event: "NewCommander", commander: "StarterCmdr", fid: "F555", universe: "live"},
				{uid: "init-load", timestamp: "2024-01-01T10:01:00Z", event: "LoadGame", commander: "StarterCmdr", fid: "F555", universe: "live"},
				{uid: "init-jump", timestamp: "2024-01-01T10:10:00Z", event: "FSDJump", commander: "StarterCmdr", fid: "F555", universe: "live"},
				{uid: "wipe-clear", timestamp: "2025-01-01T12:00:00Z", event: "ClearSavedGame", commander: "StarterCmdr", fid: "F555", universe: "live"},
				{uid: "wipe-new", timestamp: "2025-01-01T12:00:00Z", event: "NewCommander", commander: "RebornCmdr", fid: "F555", universe: "live"},
				{uid: "reborn-jump", timestamp: "2025-01-01T12:05:00Z", event: "FSDJump", commander: "RebornCmdr", fid: "F555", universe: "live"},
			},
			wantProfiles: []gotProfile{
				{profileKey: "live:f555:era:0", displayName: "StarterCmdr", fid: "F555", universe: "live", eraIndex: 0, isArchivedCareer: 1, enabled: 0},
				{profileKey: "live:f555:era:1", displayName: "RebornCmdr", fid: "F555", universe: "live", eraIndex: 1, isArchivedCareer: 0, enabled: 1},
			},
			wantAliases: []gotAlias{
				{profileKey: "live:f555:era:0", rawName: "StarterCmdr", fid: "F555", universe: "live", eventCount: 4, isCurrent: 1, source: "auto"},
				{profileKey: "live:f555:era:1", rawName: "RebornCmdr", fid: "F555", universe: "live", eventCount: 2, isCurrent: 1, source: "auto"},
			},
		},
		{
			name: "ClearSavedGame plus same-name NewCommander splits archived and active eras cleanly",
			events: []seedEventRow{
				{timestamp: "2024-03-01T10:00:00Z", event: "LoadGame", commander: "SameName", fid: "F777", universe: "live"},
				{timestamp: "2024-03-01T12:00:00Z", event: "ClearSavedGame", commander: "SameName", fid: "F777", universe: "live"},
				{timestamp: "2024-03-01T12:00:05Z", event: "NewCommander", commander: "SameName", fid: "F777", universe: "live"},
				{timestamp: "2024-03-01T12:10:00Z", event: "FSDJump", commander: "SameName", fid: "F777", universe: "live"},
			},
			wantProfiles: []gotProfile{
				{profileKey: "live:f777:era:0", displayName: "SameName", fid: "F777", universe: "live", eraIndex: 0, isArchivedCareer: 1, enabled: 0},
				{profileKey: "live:f777:era:1", displayName: "SameName", fid: "F777", universe: "live", eraIndex: 1, isArchivedCareer: 0, enabled: 1},
			},
		},
		{
			name: "unlogged Statistics drop splits at preceding session LoadGame",
			events: []seedEventRow{
				{timestamp: "2024-01-01T10:00:00Z", event: "LoadGame", commander: "StatsWipe", fid: "F888", universe: "live", sourceFile: "s1.log"},
				{timestamp: "2024-01-01T10:01:00Z", event: "Statistics", commander: "StatsWipe", fid: "F888", universe: "live", sourceFile: "s1.log",
					rawJSON: `{"Exploration":{"Systems_Visited":120,"Total_Hyperspace_Jumps":115}}`},
				{timestamp: "2024-06-01T10:00:00Z", event: "Commander", commander: "StatsWipe", fid: "F888", universe: "live", sourceFile: "s2.log"},
				{timestamp: "2024-06-01T10:00:01Z", event: "LoadGame", commander: "StatsWipe", fid: "F888", universe: "live", sourceFile: "s2.log"},
				{timestamp: "2024-06-01T10:00:02Z", event: "Location", commander: "StatsWipe", fid: "F888", universe: "live", sourceFile: "s2.log"},
				{timestamp: "2024-06-01T10:00:03Z", event: "Statistics", commander: "StatsWipe", fid: "F888", universe: "live", sourceFile: "s2.log",
					rawJSON: `{"Exploration":{"Systems_Visited":1,"Total_Hyperspace_Jumps":0}}`},
			},
			wantProfiles: []gotProfile{
				{profileKey: "live:f888:era:0", displayName: "StatsWipe", fid: "F888", universe: "live", eraIndex: 0, isArchivedCareer: 1, enabled: 0},
				{profileKey: "live:f888:era:1", displayName: "StatsWipe", fid: "F888", universe: "live", eraIndex: 1, isArchivedCareer: 0, enabled: 1},
			},
			wantAliases: []gotAlias{
				{profileKey: "live:f888:era:0", rawName: "StatsWipe", fid: "F888", universe: "live", eventCount: 2, isCurrent: 1, source: "auto"},
				{profileKey: "live:f888:era:1", rawName: "StatsWipe", fid: "F888", universe: "live", eventCount: 4, isCurrent: 1, source: "auto"},
			},
		},
		{
			name: "live legacy and beta universes receive distinct profiles and default enabled states",
			events: []seedEventRow{
				{timestamp: "2025-01-01T10:00:00Z", event: "LoadGame", commander: "MultiVerse", fid: "F900", universe: "live"},
				{timestamp: "2025-02-01T10:00:00Z", event: "LoadGame", commander: "MultiVerse", fid: "F900", universe: "legacy"},
				{timestamp: "2025-03-01T10:00:00Z", event: "LoadGame", commander: "MultiVerse", fid: "F900", universe: "beta"},
			},
			wantProfiles: []gotProfile{
				{profileKey: "live:f900:era:0", displayName: "MultiVerse", fid: "F900", universe: "live", eraIndex: 0, isArchivedCareer: 0, enabled: 1},
				{profileKey: "legacy:f900:era:0", displayName: "MultiVerse", fid: "F900", universe: "legacy", eraIndex: 0, isArchivedCareer: 0, enabled: 1},
				{profileKey: "beta:f900:era:0", displayName: "MultiVerse", fid: "F900", universe: "beta", eraIndex: 0, isArchivedCareer: 0, enabled: 0},
			},
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(subtest *testing.T) {
			conn := openResolverTestDB(subtest)
			insertSeedEvents(subtest, conn, testCase.events)
			runResolveCommanders(subtest, conn)
			runResolveCommanders(subtest, conn)

			gotProfiles := queryProfiles(subtest, conn)
			if len(gotProfiles) != len(testCase.wantProfiles) {
				subtest.Fatalf("got %d profiles (%+v), want %d", len(gotProfiles), gotProfiles, len(testCase.wantProfiles))
			}
			for i, want := range testCase.wantProfiles {
				got := gotProfiles[i]
				if got.profileKey != want.profileKey || got.displayName != want.displayName || got.fid != want.fid ||
					got.universe != want.universe || got.eraIndex != want.eraIndex ||
					got.isArchivedCareer != want.isArchivedCareer || got.enabled != want.enabled {
					subtest.Errorf("profile[%d] = %+v, want %+v", i, got, want)
				}
			}

			if len(testCase.wantAliases) > 0 {
				gotAliases := queryAliases(subtest, conn)
				if len(gotAliases) != len(testCase.wantAliases) {
					subtest.Fatalf("got %d aliases (%+v), want %d", len(gotAliases), gotAliases, len(testCase.wantAliases))
				}
				for i, want := range testCase.wantAliases {
					got := gotAliases[i]
					if got.profileKey != want.profileKey || got.rawName != want.rawName || got.fid != want.fid ||
						got.universe != want.universe || got.eventCount != want.eventCount ||
						got.isCurrent != want.isCurrent || got.source != want.source {
						subtest.Errorf("alias[%d] = %+v, want %+v", i, got, want)
					}
				}
			}

			var zeroProfileCount int
			if err := conn.QueryRow(`SELECT COUNT(*) FROM journal_events WHERE profile_id = 0`).Scan(&zeroProfileCount); err != nil {
				subtest.Fatal(err)
			}
			if zeroProfileCount != 0 {
				subtest.Errorf("expected all seeded events to have non-zero profile_id, found %d with 0", zeroProfileCount)
			}
		})
	}
}

func TestClassifyJournalUniverse(testingContext *testing.T) {
	testCases := []struct {
		name               string
		timestamp          string
		gameVersion        string
		build              string
		sourceFile         string
		loadGamePresent    bool
		loadGameHasModern  bool
		wantUniverse       string
	}{
		{"modern 4.0 live", "2026-01-18T19:00:00Z", "4.0.0.1800", "r301234/r0 ", "Journal.2026-01-18T190000.01.log", true, true, "live"},
		{"pre-split 3.8 stays live", "2022-11-28T23:59:59Z", "3.8.0.404", "r280100/r0 ", "Journal.221128235959.01.log", true, false, "live"},
		{"post-split 3.8 version is legacy", "2022-11-30T10:00:00Z", "3.8.0.404", "r280100/r0 ", "Journal.2022-11-30T100000.01.log", true, false, "legacy"},
		{"post-split 2-digit year filename is legacy", "2023-04-10T10:00:00Z", "", "", "Journal.230410100000.01.log", false, false, "legacy"},
		{"post-split LoadGame lacking Odyssey and gameversion is legacy", "2023-06-01T10:00:00Z", "", "", "EDD-99", true, false, "legacy"},
		{"post-split EDD source without LoadGame is NOT falsely marked legacy", "2024-02-01T10:00:00Z", "", "", "EDD-100", false, false, "live"},
		{"beta version classified beta", "2024-09-01T10:00:00Z", "4.0.0.100 (Beta)", "r300000", "Journal.2024-09-01T100000.01.log", true, true, "beta"},
		{"alpha build classified beta", "2021-04-01T10:00:00Z", "4.0.0.10", "Odyssey Alpha Phase 1", "Journal.210401100000.01.log", true, true, "beta"},
	}
	for _, testCase := range testCases {
		got := db.ClassifyJournalUniverse(testCase.timestamp, testCase.gameVersion, testCase.build, testCase.sourceFile, testCase.loadGamePresent, testCase.loadGameHasModern)
		if got != testCase.wantUniverse {
			testingContext.Errorf("%s: ClassifyJournalUniverse = %q, want %q", testCase.name, got, testCase.wantUniverse)
		}
	}
}

func TestResolveCommandersDeclarativeOverridesAndIdempotency(testingContext *testing.T) {
	conn := openResolverTestDB(testingContext)
	insertSeedEvents(testingContext, conn, []seedEventRow{
		// Same FID shared by two names that user wants to unmerge, plus a manual split on Main, plus transitive merge A -> B -> Main with a cycle back to A.
		{timestamp: "2024-01-01T10:00:00Z", event: "LoadGame", commander: "SharedOld", fid: "F300", universe: "live"},
		{timestamp: "2024-06-01T10:00:00Z", event: "LoadGame", commander: "SharedMain", fid: "F300", universe: "live"},
		{timestamp: "2025-01-01T10:00:00Z", event: "FSDJump", commander: "SharedMain", fid: "F300", universe: "live"},
		{timestamp: "2023-01-01T10:00:00Z", event: "FSDJump", commander: "NetLogA", fid: "", universe: "live"},
		{timestamp: "2023-06-01T10:00:00Z", event: "FSDJump", commander: "NetLogB", fid: "", universe: "live"},
	})

	if _, err := conn.Exec(`
		INSERT INTO commander_overrides (rule_type, source_key, target_key, split_timestamp, created_at) VALUES
		('unmerge', 'live:f300:sharedold', '', '', '2026-01-01T00:00:00Z'),
		('split', 'live:f300:era:0', '', '2024-12-01T00:00:00Z', '2026-01-01T00:00:01Z'),
		('merge', 'live:name:netloga:era:0', 'live:name:netlogb:era:0', '', '2026-01-01T00:00:02Z'),
		('merge', 'live:name:netlogb:era:0', 'live:f300:era:1', '', '2026-01-01T00:00:03Z'),
		('merge', 'live:f300:era:1', 'live:name:netloga:era:0', '', '2026-01-01T00:00:04Z')
	`); err != nil {
		testingContext.Fatalf("insert overrides: %v", err)
	}

	runResolveCommanders(testingContext, conn)

	// Toggle unmerged profile to disabled and verify repeated ResolveCommanders preserves enabled = 0.
	if _, err := conn.Exec(`UPDATE commander_profiles SET enabled = 0 WHERE profile_key = 'unmerged:live:f300:sharedold:era:0'`); err != nil {
		testingContext.Fatal(err)
	}
	runResolveCommanders(testingContext, conn)

	profiles := queryProfiles(testingContext, conn)
	if len(profiles) != 3 {
		testingContext.Fatalf("expected 3 profiles after unmerge, split, and cycle-guarded transitive merge, got %+v", profiles)
	}

	byKey := make(map[string]gotProfile, len(profiles))
	for _, profileItem := range profiles {
		byKey[profileItem.profileKey] = profileItem
	}
	unmerged := byKey["unmerged:live:f300:sharedold:era:0"]
	if unmerged.displayName != "SharedOld" || unmerged.hasManualOverride != 1 || unmerged.enabled != 0 {
		testingContext.Errorf("unexpected unmerged profile: %+v", unmerged)
	}
	splitEra0 := byKey["live:f300:era:0"]
	if splitEra0.isArchivedCareer != 1 || splitEra0.hasManualOverride != 1 || splitEra0.enabled != 0 {
		testingContext.Errorf("unexpected split era 0 profile: %+v", splitEra0)
	}
	splitEra1 := byKey["live:f300:era:1"]
	if splitEra1.displayName != "SharedMain" || splitEra1.isArchivedCareer != 0 || splitEra1.hasManualOverride != 1 || splitEra1.enabled != 1 {
		testingContext.Errorf("unexpected merged target era 1 profile: %+v", splitEra1)
	}

	aliases := queryAliases(testingContext, conn)
	manualCount := 0
	for _, alias := range aliases {
		if alias.profileKey == "live:f300:era:1" && (alias.rawName == "NetLogA" || alias.rawName == "NetLogB") {
			if alias.source != "manual" || alias.isCurrent != 0 {
				testingContext.Errorf("expected transitive merged alias %s to have source=manual and is_current=0, got %+v", alias.rawName, alias)
			}
			manualCount++
		}
	}
	if manualCount != 2 {
		testingContext.Errorf("expected 2 manual aliases on live:f300:era:1, got %d (%+v)", manualCount, aliases)
	}
}

func TestMigrateCommanderProfilesV13ZeroRescanBackfill(testingContext *testing.T) {
	conn := openResolverTestDB(testingContext)

	// Simulate pre-v13 archive: commander_profiles empty, byte_offset > 0, unbackfilled FID/universe, .02.log rollover, and disabled commander.
	if _, err := conn.Exec(`
		DELETE FROM commander_profiles;
		DELETE FROM settings WHERE key = 'commander_profiles_v13';
		INSERT INTO commanders (name, fid, enabled, first_seen, last_seen) VALUES
			('CmdrLive', '', 1, '2026-01-10T10:00:00Z', '2026-01-10T11:00:00Z'),
			('CmdrDisabledLegacy', '', 0, '2023-05-01T10:00:00Z', '2023-05-01T10:10:00Z');
		INSERT INTO ingested_journals (file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at) VALUES
			('Journal.2026-01-10T100000.01.log', '/logs/j1.01.log', 4096, 10, 4096, 2, '2026-01-10T10:00:00Z', '2026-01-10T10:30:00Z', '2026-01-10T10:30:00Z'),
			('Journal.2026-01-10T100000.02.log', '/logs/j1.02.log', 2048, 5, 2048, 1, '2026-01-10T10:35:00Z', '2026-01-10T10:35:00Z', '2026-01-10T10:35:00Z'),
			('Journal.230501100000.01.log', '/logs/leg.01.log', 1024, 4, 1024, 2, '2023-05-01T10:00:00Z', '2023-05-01T10:10:00Z', '2023-05-01T10:10:00Z');
		INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, source_file, raw_json, ingested_at) VALUES
			('v13-1', '2026-01-10T10:00:00Z', 'LoadGame', 'CmdrLive', '', 'live', 'Journal.2026-01-10T100000.01.log',
			 '{"timestamp":"2026-01-10T10:00:00Z","event":"LoadGame","Commander":"CmdrLive","FID":"F123","Odyssey":true,"gameversion":"4.0.0.1800"}', '2026-01-10T10:30:00Z'),
			('v13-2', '2026-01-10T10:30:00Z', 'FSDJump', 'CmdrLive', '', 'live', 'Journal.2026-01-10T100000.01.log',
			 '{"timestamp":"2026-01-10T10:30:00Z","event":"FSDJump","StarSystem":"Sol"}', '2026-01-10T10:30:00Z'),
			('v13-3', '2026-01-10T10:35:00Z', 'FSDJump', NULL, '', 'live', 'Journal.2026-01-10T100000.02.log',
			 '{"timestamp":"2026-01-10T10:35:00Z","event":"FSDJump","StarSystem":" Barnard''s Star"}', '2026-01-10T10:35:00Z'),
			('v13-4', '2023-05-01T10:00:00Z', 'LoadGame', 'CmdrDisabledLegacy', '', 'live', 'Journal.230501100000.01.log',
			 '{"timestamp":"2023-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrDisabledLegacy","FID":"F999"}', '2023-05-01T10:10:00Z');
	`); err != nil {
		testingContext.Fatalf("seed pre-v13 DB: %v", err)
	}

	if err := db.Migrate(conn); err != nil {
		testingContext.Fatalf("db.Migrate v13: %v", err)
	}

	// Verify byte_offset was NOT reset on any file.
	var zeroOffsets int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM ingested_journals WHERE byte_offset = 0`).Scan(&zeroOffsets); err != nil {
		testingContext.Fatal(err)
	}
	if zeroOffsets != 0 {
		testingContext.Fatalf("expected zero-rescan v13 migration to preserve byte_offset, got %d reset rows", zeroOffsets)
	}

	// Verify .02.log rollover inherited CmdrLive, F123, and live profile_id.
	var rolloverCmdr, rolloverFID string
	var rolloverProfileID int64
	if err := conn.QueryRow(`SELECT COALESCE(commander, ''), fid, profile_id FROM journal_events WHERE event_uid = 'v13-3'`).Scan(&rolloverCmdr, &rolloverFID, &rolloverProfileID); err != nil {
		testingContext.Fatal(err)
	}
	if rolloverCmdr != "CmdrLive" || rolloverFID != "F123" || rolloverProfileID == 0 {
		testingContext.Fatalf("expected .02.log event to inherit CmdrLive/F123 and non-zero profile_id, got cmdr=%q fid=%q profile=%d", rolloverCmdr, rolloverFID, rolloverProfileID)
	}

	profiles := queryProfiles(testingContext, conn)
	if len(profiles) != 2 {
		testingContext.Fatalf("expected 2 migrated profiles, got %+v", profiles)
	}
	byKey := map[string]gotProfile{profiles[0].profileKey: profiles[0], profiles[1].profileKey: profiles[1]}
	legacyProfile := byKey["legacy:f999:era:0"]
	if legacyProfile.universe != "legacy" || legacyProfile.enabled != 0 {
		testingContext.Fatalf("expected legacy profile to classify as legacy and inherit enabled=0, got %+v", legacyProfile)
	}
	liveProfile := byKey["live:f123:era:0"]
	if liveProfile.universe != "live" || liveProfile.enabled != 1 || liveProfile.id != rolloverProfileID {
		testingContext.Fatalf("unexpected live profile after v13: %+v", liveProfile)
	}
}
