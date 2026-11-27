package settings_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
)

func TestSettingsRoundTripsAndNormalization(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	// Generic CRUD
	if initialValue, getError := settings.Get(dbConn, "custom_key"); getError != nil || initialValue != "" {
		testingContext.Fatalf("expected empty initial custom_key, got %q err=%v", initialValue, getError)
	}
	if setError := settings.Set(dbConn, "custom_key", "custom_value"); setError != nil {
		testingContext.Fatalf("Set custom_key: %v", setError)
	}
	if loadedValue, getError := settings.Get(dbConn, "custom_key"); getError != nil || loadedValue != "custom_value" {
		testingContext.Fatalf("expected custom_value, got %q err=%v", loadedValue, getError)
	}
	if deleteError := settings.Delete(dbConn, "custom_key"); deleteError != nil {
		testingContext.Fatalf("Delete custom_key: %v", deleteError)
	}
	if deletedValue, getDeletedError := settings.Get(dbConn, "custom_key"); getDeletedError != nil || deletedValue != "" {
		testingContext.Fatalf("expected empty after delete, got %q err=%v", deletedValue, getDeletedError)
	}

	// External links defaults and normalization
	inara, edsm, gec := settings.ExternalLinkModes(dbConn)
	if inara != settings.ExternalLinkOn || edsm != settings.ExternalLinkIfNoGEC || gec != settings.ExternalLinkOn {
		testingContext.Fatalf("unexpected defaults: inara=%q edsm=%q gec=%q", inara, edsm, gec)
	}
	if saveError := settings.SetExternalLinkModes(dbConn, "invalid", "unknown", "OFF"); saveError != nil {
		testingContext.Fatalf("SetExternalLinkModes: %v", saveError)
	}
	inara, edsm, gec = settings.ExternalLinkModes(dbConn)
	if inara != settings.ExternalLinkOn || edsm != settings.ExternalLinkIfNoGEC || gec != settings.ExternalLinkOff {
		testingContext.Fatalf("unexpected normalized values: inara=%q edsm=%q gec=%q", inara, edsm, gec)
	}

	// Debounce clamping
	if defaultDebounce := settings.JournalDebounceMs(dbConn); defaultDebounce != int(reference.DefaultDebounceDelay.Milliseconds()) {
		testingContext.Fatalf("expected default debounce %d, got %d", reference.DefaultDebounceDelay.Milliseconds(), defaultDebounce)
	}
	if saveError := settings.SetJournalDebounceMs(dbConn, 1); saveError != nil {
		testingContext.Fatalf("SetJournalDebounceMs: %v", saveError)
	}
	if clampedMin := settings.JournalDebounceMs(dbConn); clampedMin != int(reference.MinDebounceDelay.Milliseconds()) {
		testingContext.Fatalf("expected clamped min %d, got %d", reference.MinDebounceDelay.Milliseconds(), clampedMin)
	}

	// Tritium price default and floor
	if defaultPrice := settings.TritiumPricePerTonne(dbConn); defaultPrice != reference.DefaultTritiumPricePerTonne {
		testingContext.Fatalf("expected default tritium price, got %d", defaultPrice)
	}
	if saveError := settings.SetTritiumPricePerTonne(dbConn, -50); saveError != nil {
		testingContext.Fatalf("SetTritiumPricePerTonne: %v", saveError)
	}
	if floorPrice := settings.TritiumPricePerTonne(dbConn); floorPrice != 0 {
		testingContext.Fatalf("expected clamped 0 tritium price, got %d", floorPrice)
	}

	// Career baseline scope default, round-trip, and validation
	if defaultScope := settings.CareerBaselineScope(dbConn); defaultScope != settings.CareerScopeExpedition {
		testingContext.Fatalf("expected default career baseline scope %q, got %q", settings.CareerScopeExpedition, defaultScope)
	}
	if err := settings.SetCareerBaselineScope(dbConn, settings.CareerScopeAllEnabled); err != nil {
		testingContext.Fatalf("SetCareerBaselineScope all_enabled: %v", err)
	}
	if gotScope := settings.CareerBaselineScope(dbConn); gotScope != settings.CareerScopeAllEnabled {
		testingContext.Fatalf("expected %q, got %q", settings.CareerScopeAllEnabled, gotScope)
	}
	if err := settings.SetCareerBaselineScope(dbConn, "invalid_scope"); err == nil {
		testingContext.Fatal("expected error for invalid career baseline scope")
	}
	if err := settings.Set(dbConn, "career_baseline_scope", "corrupt"); err != nil {
		testingContext.Fatal(err)
	}
	if fallbackScope := settings.CareerBaselineScope(dbConn); fallbackScope != settings.CareerScopeExpedition {
		testingContext.Fatalf("expected fallback %q on invalid stored value, got %q", settings.CareerScopeExpedition, fallbackScope)
	}

	// Legacy commander toggle
	if _, execError := dbConn.Exec(`INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrAlpha', 'F1', 1)`); execError != nil {
		testingContext.Fatalf("insert commander: %v", execError)
	}
	if toggleError := settings.SetCommanderEnabled(dbConn, "cmdralpha", false); toggleError != nil {
		testingContext.Fatalf("SetCommanderEnabled: %v", toggleError)
	}
	var enabledFlag int
	if scanError := dbConn.QueryRow(`SELECT enabled FROM commanders WHERE name = 'CmdrAlpha'`).Scan(&enabledFlag); scanError != nil || enabledFlag != 0 {
		testingContext.Fatalf("expected enabled=0, got %d err=%v", enabledFlag, scanError)
	}
	if missingError := settings.SetCommanderEnabled(dbConn, "CmdrUnknown", false); missingError == nil {
		testingContext.Fatal("expected error for unknown commander")
	}
}

func listProfileSummaries(testingContext *testing.T, dbConn *sql.DB) map[string]struct {
	id                int64
	displayName       string
	enabled           int
	hasManualOverride int
} {
	testingContext.Helper()
	rows, err := dbConn.Query(`SELECT id, profile_key, display_name, enabled, has_manual_override FROM commander_profiles`)
	if err != nil {
		testingContext.Fatalf("query commander_profiles: %v", err)
	}
	defer rows.Close()
	result := make(map[string]struct {
		id                int64
		displayName       string
		enabled           int
		hasManualOverride int
	})
	for rows.Next() {
		var key, displayName string
		var id int64
		var enabled, manual int
		if err := rows.Scan(&id, &key, &displayName, &enabled, &manual); err != nil {
			testingContext.Fatal(err)
		}
		result[key] = struct {
			id                int64
			displayName       string
			enabled           int
			hasManualOverride int
		}{id: id, displayName: displayName, enabled: enabled, hasManualOverride: manual}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		testingContext.Fatalf("iterate commander_profiles: %v", rowsErr)
	}
	return result
}

func TestCommanderOverridesAndProfileToggleRoundTrip(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	// Shared FID F400 with rename OldAlias -> MainCmdr, plus pre-FID NetLogCmdr (empty FID), plus LoadGames on 2025-06-15.
	dbtest.InsertSeedEvents(testingContext, dbConn,
		dbtest.SeedEventRow{UID: "e1", Timestamp: "2024-01-10T10:00:00Z", Event: "LoadGame", Commander: "OldAlias", FID: "F400"},
		dbtest.SeedEventRow{UID: "e2", Timestamp: "2025-01-10T10:00:00Z", Event: "LoadGame", Commander: "MainCmdr", FID: "F400"},
		dbtest.SeedEventRow{UID: "e3", Timestamp: "2025-06-15T08:00:00Z", Event: "LoadGame", Commander: "MainCmdr", FID: "F400"},
		dbtest.SeedEventRow{UID: "e4", Timestamp: "2025-06-15T18:30:00Z", Event: "LoadGame", Commander: "MainCmdr", FID: "F400"},
		dbtest.SeedEventRow{UID: "e5", Timestamp: "2023-05-01T12:00:00Z", Event: "FSDJump", Commander: "NetLogCmdr", FID: ""},
	)
	if err := db.WithTx(dbConn, db.ResolveCommanders); err != nil {
		testingContext.Fatalf("initial ResolveCommanders: %v", err)
	}

	// SetCommanderEnabled works by both profileKey and display_name and syncs legacy commanders.
	if err := settings.SetCommanderEnabled(dbConn, "live:f400:era:0", false); err != nil {
		testingContext.Fatalf("SetCommanderEnabled by profileKey: %v", err)
	}
	if profiles := listProfileSummaries(testingContext, dbConn); profiles["live:f400:era:0"].enabled != 0 {
		testingContext.Fatalf("expected live:f400:era:0 disabled, got %+v", profiles["live:f400:era:0"])
	}
	if err := settings.SetCommanderEnabled(dbConn, "MainCmdr", true); err != nil {
		testingContext.Fatalf("SetCommanderEnabled by display_name: %v", err)
	}
	if profiles := listProfileSummaries(testingContext, dbConn); profiles["live:f400:era:0"].enabled != 1 {
		testingContext.Fatalf("expected live:f400:era:0 re-enabled, got %+v", profiles["live:f400:era:0"])
	}

	// Attach NetLogCmdr to an expedition_commanders row, merge NetLogCmdr into MainCmdr, and verify expedition_commanders remaps to MainCmdr's profile ID.
	initialProfiles := listProfileSummaries(testingContext, dbConn)
	if _, err := dbConn.Exec(
		`INSERT INTO expeditions (id, name, status, commander_scope, created_at, updated_at) VALUES (99, 'Scoped', 'planning', 'selected', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		 INSERT INTO expedition_commanders (expedition_id, profile_id) VALUES (99, ?)`,
		initialProfiles["live:name:netlogcmdr:era:0"].id,
	); err != nil {
		testingContext.Fatalf("seed expedition_commanders: %v", err)
	}

	if err := settings.MergeCommanders(dbConn, "live:name:netlogcmdr:era:0", "live:f400:era:0"); err != nil {
		testingContext.Fatalf("MergeCommanders manual: %v", err)
	}
	afterMerge := listProfileSummaries(testingContext, dbConn)
	if len(afterMerge) != 1 || afterMerge["live:f400:era:0"].hasManualOverride != 1 {
		testingContext.Fatalf("expected 1 merged profile with manual override, got %+v", afterMerge)
	}
	var remappedProfileID int64
	if err := dbConn.QueryRow(`SELECT profile_id FROM expedition_commanders WHERE expedition_id = 99`).Scan(&remappedProfileID); err != nil || remappedProfileID != afterMerge["live:f400:era:0"].id {
		testingContext.Fatalf("expected expedition_commanders remapped to %d, got %d (err=%v)", afterMerge["live:f400:era:0"].id, remappedProfileID, err)
	}

	// Unmerge manually merged alias NetLogCmdr (restores live:name:netlogcmdr:era:0 without orphan unmerge rule) and auto FID-merged alias OldAlias (creates unmerged:live:f400:oldalias:era:0).
	if err := settings.UnmergeCommanderAlias(dbConn, "NetLogCmdr", "live:f400:era:0"); err != nil {
		testingContext.Fatalf("UnmergeCommanderAlias manual: %v", err)
	}
	if err := settings.UnmergeCommanderAlias(dbConn, "OldAlias", "live:f400:era:0"); err != nil {
		testingContext.Fatalf("UnmergeCommanderAlias auto FID: %v", err)
	}
	afterUnmerge := listProfileSummaries(testingContext, dbConn)
	if _, ok := afterUnmerge["live:name:netlogcmdr:era:0"]; !ok {
		testingContext.Fatalf("expected restored NetLogCmdr profile, got %+v", afterUnmerge)
	}
	if unmergedOld, ok := afterUnmerge["unmerged:live:f400:oldalias:era:0"]; !ok || unmergedOld.displayName != "OldAlias" {
		testingContext.Fatalf("expected unmerged OldAlias profile, got %+v", afterUnmerge)
	}

	// Split MainCmdr first by YYYY-MM-DD date and then by exact ISO timestamp; verify expedition_commanders follows anchor events across SplitCommander, era-collapse MergeCommanders (with higher era shifting down), single-alias manual-split UnmergeCommanderAlias rejection, and ResetCommanderOverrides.
	if _, err := dbConn.Exec(
		`INSERT INTO expeditions (id, name, status, start_timestamp, commander_scope, created_at, updated_at) VALUES
		 (101, 'Era1 Trip', 'active', '2025-06-15T08:00:00Z', 'selected', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		 (102, 'Era2 Trip', 'active', '2025-06-15T18:30:00Z', 'selected', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
		 INSERT INTO expedition_commanders (expedition_id, profile_id) VALUES (101, ?), (102, ?)`,
		afterUnmerge["live:f400:era:0"].id, afterUnmerge["live:f400:era:0"].id,
	); err != nil {
		testingContext.Fatalf("seed split-anchor expeditions: %v", err)
	}
	if err := settings.SplitCommander(dbConn, "live:f400:era:0", "2025-06-15"); err != nil {
		testingContext.Fatalf("SplitCommander YYYY-MM-DD: %v", err)
	}
	if err := settings.SplitCommander(dbConn, "live:f400:era:1", "2025-06-15T18:30:00Z"); err != nil {
		testingContext.Fatalf("SplitCommander ISO: %v", err)
	}
	afterSplits := listProfileSummaries(testingContext, dbConn)
	if _, ok := afterSplits["live:f400:era:2"]; !ok {
		testingContext.Fatalf("expected 3 eras (era:0..era:2) after date and ISO splits, got %+v", afterSplits)
	}
	var exp101Profile, exp102Profile int64
	if err := dbConn.QueryRow(`SELECT profile_id FROM expedition_commanders WHERE expedition_id = 101`).Scan(&exp101Profile); err != nil || exp101Profile != afterSplits["live:f400:era:1"].id {
		testingContext.Fatalf("expected exp 101 bound to era:1 (%d) after splits, got %d (err=%v)", afterSplits["live:f400:era:1"].id, exp101Profile, err)
	}
	if err := dbConn.QueryRow(`SELECT profile_id FROM expedition_commanders WHERE expedition_id = 102`).Scan(&exp102Profile); err != nil || exp102Profile != afterSplits["live:f400:era:2"].id {
		testingContext.Fatalf("expected exp 102 bound to era:2 (%d) after splits, got %d (err=%v)", afterSplits["live:f400:era:2"].id, exp102Profile, err)
	}

	// Single-alias manual-split profile must reject UnmergeCommanderAlias without inserting a bogus unmerge rule.
	if err := settings.UnmergeCommanderAlias(dbConn, "MainCmdr", "live:f400:era:1"); err == nil {
		testingContext.Fatal("expected error calling UnmergeCommanderAlias on single-alias manual-split profile")
	}

	// Collapse era:0 + era:1 -> era:0 so old era:2 shifts down to era:1; verify exp 101 rebinds to era:0 and exp 102 shifts cleanly to new era:1.
	if err := settings.MergeCommanders(dbConn, "live:f400:era:0", "live:f400:era:1"); err != nil {
		testingContext.Fatalf("MergeCommanders era:0 + era:1 collapse: %v", err)
	}
	afterEraMerge := listProfileSummaries(testingContext, dbConn)
	if _, hasEra2 := afterEraMerge["live:f400:era:2"]; hasEra2 {
		testingContext.Fatalf("expected era:2 to shift down to era:1 after merging era:0 and era:1, got %+v", afterEraMerge)
	}
	if err := dbConn.QueryRow(`SELECT profile_id FROM expedition_commanders WHERE expedition_id = 101`).Scan(&exp101Profile); err != nil || exp101Profile != afterEraMerge["live:f400:era:0"].id {
		testingContext.Fatalf("expected exp 101 rebound to collapsed era:0 (%d), got %d (err=%v)", afterEraMerge["live:f400:era:0"].id, exp101Profile, err)
	}
	if err := dbConn.QueryRow(`SELECT profile_id FROM expedition_commanders WHERE expedition_id = 102`).Scan(&exp102Profile); err != nil || exp102Profile != afterEraMerge["live:f400:era:1"].id {
		testingContext.Fatalf("expected exp 102 shifted to new era:1 (%d), got %d (err=%v)", afterEraMerge["live:f400:era:1"].id, exp102Profile, err)
	}

	// Resetting overrides on live:f400:era:0 clears split and unmerge rules and reconciles exp 101 and exp 102 back to natural live:f400:era:0.
	if err := settings.ResetCommanderOverrides(dbConn, "live:f400:era:0"); err != nil {
		testingContext.Fatalf("ResetCommanderOverrides: %v", err)
	}
	afterReset := listProfileSummaries(testingContext, dbConn)
	if len(afterReset) != 2 || afterReset["live:f400:era:0"].hasManualOverride != 0 {
		testingContext.Fatalf("expected clean 2-profile natural state after ResetCommanderOverrides, got %+v", afterReset)
	}
	if err := dbConn.QueryRow(`SELECT profile_id FROM expedition_commanders WHERE expedition_id = 102`).Scan(&exp102Profile); err != nil || exp102Profile != afterReset["live:f400:era:0"].id {
		testingContext.Fatalf("expected exp 102 reconciled to natural era:0 (%d) after reset, got %d (err=%v)", afterReset["live:f400:era:0"].id, exp102Profile, err)
	}
}

func TestSingleAliasManualEraMergeUnmergesCleanlyWithoutBogusUnmergeRule(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	dbtest.InsertSeedEvents(testingContext, dbConn,
		dbtest.SeedEventRow{UID: "w1", Timestamp: "2024-03-01T10:00:00Z", Event: "LoadGame", Commander: "SameName", FID: "F777"},
		dbtest.SeedEventRow{UID: "w2", Timestamp: "2024-03-01T12:00:00Z", Event: "ClearSavedGame", Commander: "SameName", FID: "F777"},
		dbtest.SeedEventRow{UID: "w3", Timestamp: "2024-03-01T12:00:05Z", Event: "NewCommander", Commander: "SameName", FID: "F777"},
		dbtest.SeedEventRow{UID: "w4", Timestamp: "2024-03-01T12:10:00Z", Event: "FSDJump", Commander: "SameName", FID: "F777"},
	)
	if err := db.WithTx(dbConn, db.ResolveCommanders); err != nil {
		testingContext.Fatal(err)
	}

	if err := settings.MergeCommanders(dbConn, "live:f777:era:0", "live:f777:era:1"); err != nil {
		testingContext.Fatalf("MergeCommanders same-name eras: %v", err)
	}
	if merged := listProfileSummaries(testingContext, dbConn); len(merged) != 1 {
		testingContext.Fatalf("expected 1 merged profile, got %+v", merged)
	}

	if err := settings.UnmergeCommanderAlias(dbConn, "SameName", "live:f777:era:1"); err != nil {
		testingContext.Fatalf("UnmergeCommanderAlias same-name era merge: %v", err)
	}
	restored := listProfileSummaries(testingContext, dbConn)
	if _, hasEra0 := restored["live:f777:era:0"]; !hasEra0 {
		testingContext.Fatalf("expected live:f777:era:0 restored without unmerged prefix, got %+v", restored)
	}
	if _, hasEra1 := restored["live:f777:era:1"]; !hasEra1 {
		testingContext.Fatalf("expected live:f777:era:1 restored without unmerged prefix, got %+v", restored)
	}
	var unmergeRuleCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM commander_overrides WHERE rule_type = 'unmerge'`).Scan(&unmergeRuleCount); err != nil || unmergeRuleCount != 0 {
		testingContext.Fatalf("expected 0 unmerge rules, got %d (err=%v)", unmergeRuleCount, err)
	}
}

func TestEDDRecoveryStateProgressionAndMetadataSettings(testingContext *testing.T) {
	if nilRec, nilDisc, nilPath := settings.EDDRecoveryState(nil); nilRec || nilDisc || nilPath != "" {
		testingContext.Fatalf("expected zero EDDRecoveryState for nil queryer, got (%v, %v, %q)", nilRec, nilDisc, nilPath)
	}

	dbConn := dbtest.OpenMemory(testingContext)
	recovered, disconnectsDone, eddPath := settings.EDDRecoveryState(dbConn)
	if recovered || disconnectsDone || eddPath != "" {
		testingContext.Fatalf("expected initial EDDRecoveryState (false, false, empty), got (%v, %v, %q)", recovered, disconnectsDone, eddPath)
	}

	if err := settings.MarkEDDDisconnectsRecovered(dbConn); err != nil {
		testingContext.Fatalf("MarkEDDDisconnectsRecovered: %v", err)
	}
	recovered, disconnectsDone, eddPath = settings.EDDRecoveryState(dbConn)
	if recovered || !disconnectsDone || eddPath != "" {
		testingContext.Fatalf("expected disconnectsDone=true before full recovery, got (%v, %v, %q)", recovered, disconnectsDone, eddPath)
	}

	if err := settings.Set(dbConn, "edd_history_disconnects_v7", "0"); err != nil {
		testingContext.Fatalf("reset disconnects key: %v", err)
	}
	const savedEDDPath = "/tmp/test-edd/EDDUser.sqlite"
	if err := settings.MarkEDDRecovered(dbConn, savedEDDPath); err != nil {
		testingContext.Fatalf("MarkEDDRecovered: %v", err)
	}
	recovered, disconnectsDone, eddPath = settings.EDDRecoveryState(dbConn)
	if !recovered || !disconnectsDone || eddPath != savedEDDPath {
		testingContext.Fatalf("expected full EDDRecoveryState (true, true, %q), got (%v, %v, %q)", savedEDDPath, recovered, disconnectsDone, eddPath)
	}

	// Verify direct round-trips for remaining typed settings helpers.
	if gotDir := settings.JournalDir(dbConn, "/default/journals"); gotDir != "/default/journals" {
		testingContext.Fatalf("expected default JournalDir, got %q", gotDir)
	}
	if err := settings.SetJournalDir(dbConn, "/custom/journals"); err != nil || settings.JournalDir(dbConn, "/default") != "/custom/journals" {
		testingContext.Fatalf("SetJournalDir/JournalDir round-trip failed: %v", err)
	}
	if !settings.OnlineLookups(dbConn) {
		testingContext.Fatal("expected OnlineLookups default true")
	}
	if err := settings.SetOnlineLookups(dbConn, false); err != nil || settings.OnlineLookups(dbConn) {
		testingContext.Fatalf("SetOnlineLookups(false) failed: %v", err)
	}
	if err := settings.SetActiveExpeditionID(dbConn, 42); err != nil || settings.ActiveExpeditionID(dbConn) != 42 {
		testingContext.Fatalf("SetActiveExpeditionID(42) failed: %v", err)
	}
	if err := settings.SetActiveExpeditionID(dbConn, 0); err != nil || settings.ActiveExpeditionID(dbConn) != 0 {
		testingContext.Fatalf("SetActiveExpeditionID(0) clear failed: %v", err)
	}
	if settings.SetupCompleted(dbConn) {
		testingContext.Fatal("expected SetupCompleted default false")
	}
	if err := settings.SetSetupCompleted(dbConn, true); err != nil || !settings.SetupCompleted(dbConn) {
		testingContext.Fatalf("SetSetupCompleted(true) failed: %v", err)
	}
	if err := settings.SetLastScanAt(dbConn, "2026-05-01T10:00:00Z"); err != nil || settings.LastScanAt(dbConn) != "2026-05-01T10:00:00Z" {
		testingContext.Fatalf("LastScanAt round-trip failed: %v", err)
	}
	if err := settings.SetGECFetchedAt(dbConn, "2026-05-01T11:00:00Z"); err != nil || settings.GECFetchedAt(dbConn) != "2026-05-01T11:00:00Z" {
		testingContext.Fatalf("GECFetchedAt round-trip failed: %v", err)
	}
}
