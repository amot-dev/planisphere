package replay_test

import (
	"fmt"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/replay"
	"planisphere/internal/testutil/dbtest"
)

func TestListCommandersEventCounts(testingContext *testing.T) {
	testCases := []struct {
		name           string
		commanders     []string
		eventValues    []any
		expectedCounts map[string]int
	}{
		{
			name:           "single commander with mixed case and unattributed rows",
			commanders:     []string{"CmdrSolo"},
			eventValues:    []any{"CmdrSolo", "cmdrsolo", nil, ""},
			expectedCounts: map[string]int{"CmdrSolo": 2},
		},
		{
			name:           "single commander with zero events",
			commanders:     []string{"CmdrEmpty"},
			eventValues:    nil,
			expectedCounts: map[string]int{"CmdrEmpty": 0},
		},
		{
			name:       "multiple commanders skewed counts with unattributed rows",
			commanders: []string{"CmdrAlpha", "CmdrBeta", "CmdrGamma"},
			eventValues: []any{
				"CmdrAlpha", "cmdralpha",
				"CmdrBeta", "CMDRBETA", "CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta",
				"CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta", "CmdrBeta",
				"CmdrGamma", nil, "",
			},
			expectedCounts: map[string]int{"CmdrAlpha": 2, "CmdrBeta": 12, "CmdrGamma": 1},
		},
		{
			name:       "unlisted commanders before between and after trigger fallback",
			commanders: []string{"CmdrB", "CmdrD"},
			eventValues: []any{
				"CmdrA", "CmdrB", "cmdrb", "CmdrC", "CmdrD", "CmdrE",
			},
			expectedCounts: map[string]int{"CmdrB": 2, "CmdrD": 1},
		},
		{
			name:       "plurality unattributed rows fall back cleanly",
			commanders: []string{"CmdrAlpha", "CmdrBeta"},
			eventValues: []any{
				nil, nil, nil, nil, nil, nil, nil, nil, "", "",
				"CmdrAlpha", "CmdrBeta",
			},
			expectedCounts: map[string]int{"CmdrAlpha": 1, "CmdrBeta": 1},
		},
		{
			name:       "non-text commander storage classes trigger fallback",
			commanders: []string{"CmdrSolo"},
			eventValues: []any{
				"CmdrSolo", "cmdrsolo", []byte{0x01, 0x02},
			},
			expectedCounts: map[string]int{"CmdrSolo": 2},
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(subTest *testing.T) {
			databaseConnection := dbtest.OpenMemory(subTest)
			for index, commanderName := range testCase.commanders {
				timestamp := fmt.Sprintf("2026-04-0%dT00:00:00Z", index+1)
				if _, execError := databaseConnection.Exec(
					`INSERT INTO commanders (name, fid, enabled, first_seen, last_seen) VALUES (?, ?, 1, ?, ?)`,
					commanderName, fmt.Sprintf("F%d", index+1), timestamp, timestamp,
				); execError != nil {
					subTest.Fatalf("insert commander %s: %v", commanderName, execError)
				}
			}
			for index, commanderValue := range testCase.eventValues {
				if _, execError := databaseConnection.Exec(
					`INSERT INTO journal_events (event_uid, timestamp, event, commander, source_file, raw_json, ingested_at)
					 VALUES (?, ?, 'FSDJump', ?, 'test.log', '{}', '2026-04-10T00:00:00Z')`,
					fmt.Sprintf("uid-%d", index),
					fmt.Sprintf("2026-04-01T00:00:%02dZ", index),
					commanderValue,
				); execError != nil {
					subTest.Fatalf("insert event %d: %v", index, execError)
				}
			}

			listedCommanders, listError := replay.ListCommanders(databaseConnection)
			if listError != nil {
				subTest.Fatalf("ListCommanders: %v", listError)
			}
			if len(listedCommanders) != len(testCase.expectedCounts) {
				subTest.Fatalf("expected %d commanders, got %d", len(testCase.expectedCounts), len(listedCommanders))
			}
			for _, listedCommander := range listedCommanders {
				expectedCount := testCase.expectedCounts[listedCommander.Name]
				if listedCommander.EventCount != expectedCount {
					subTest.Errorf("commander %s: expected count %d, got %d", listedCommander.Name, expectedCount, listedCommander.EventCount)
				}
			}
		})
	}
}

func TestListCommanderNamesForScopeLegacyFallback(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	emptyNames, emptyError := replay.ListCommanderNamesForScope(databaseConnection, replay.ProfileScope{})
	if emptyError != nil || emptyNames == nil || len(emptyNames) != 0 {
		testingContext.Fatalf("expected non-nil empty slice when no commanders exist, got %v err=%v", emptyNames, emptyError)
	}

	if _, execError := databaseConnection.Exec(`
		INSERT INTO commanders (name, fid, enabled, first_seen, last_seen) VALUES
		('CmdrZulu', 'FZ', 1, '2026-04-02T00:00:00Z', '2026-04-02T00:00:00Z'),
		('CmdrDisabled', 'FD', 0, '2026-04-01T00:00:00Z', '2026-04-01T00:00:00Z'),
		('   ', 'FB', 1, '2026-04-01T00:00:00Z', '2026-04-01T00:00:00Z'),
		('CmdrBravo', 'F2', 1, '2026-04-01T00:00:00Z', '2026-04-01T00:00:00Z'),
		('CmdrAlpha', 'F1', 1, '2026-04-01T00:00:00Z', '2026-04-01T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed commanders: %v", execError)
	}
	// Dropping journal_events verifies ListCommanderNamesForScope skips event count queries.
	if _, dropEventsError := databaseConnection.Exec(`DROP TABLE journal_events`); dropEventsError != nil {
		testingContext.Fatalf("drop journal_events: %v", dropEventsError)
	}

	enabledNames, listError := replay.ListCommanderNamesForScope(databaseConnection, replay.ProfileScope{})
	if listError != nil {
		testingContext.Fatalf("ListCommanderNamesForScope: %v", listError)
	}
	if strings.Join(enabledNames, "|") != "CmdrAlpha|CmdrBravo|CmdrZulu" {
		testingContext.Fatalf("unexpected enabled commander order: %v", enabledNames)
	}

	if _, dropCmdrsError := databaseConnection.Exec(`DROP TABLE commanders`); dropCmdrsError != nil {
		testingContext.Fatalf("drop commanders: %v", dropCmdrsError)
	}
	if _, missingTableError := replay.ListCommanderNamesForScope(databaseConnection, replay.ProfileScope{}); missingTableError == nil {
		testingContext.Fatal("expected error when commanders table is missing")
	}
}

func TestListCommandersIncludesNamelessFIDEventsWithoutBlankAliases(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	dbtest.InsertSeedEvents(testingContext, databaseConnection,
		dbtest.SeedEventRow{UID: "n1", Timestamp: "2026-01-01T10:00:00Z", Event: "LoadGame", Commander: "CmdrMain", FID: "F101", Universe: "live"},
		dbtest.SeedEventRow{UID: "n2", Timestamp: "2026-01-01T10:05:00Z", Event: "FSDJump", Commander: "", FID: "F101", Universe: "live"},
		dbtest.SeedEventRow{UID: "n3", Timestamp: "2026-01-02T10:00:00Z", Event: "FSDJump", Commander: "", FID: "F202", Universe: "live"},
		dbtest.SeedEventRow{UID: "n4", Timestamp: "2026-01-02T10:05:00Z", Event: "FSDJump", Commander: "", FID: "", Universe: "live"},
	)
	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, source_file, raw_json, ingested_at)
		 VALUES ('n5', '2026-01-02T10:06:00Z', 'FSDJump', NULL, 'F202', 'live', 'test.log', '{}', '2026-01-02T10:06:00Z')`,
	); execError != nil {
		testingContext.Fatalf("insert null-commander FID event: %v", execError)
	}
	if err := db.WithTx(databaseConnection, db.ResolveCommanders); err != nil {
		testingContext.Fatalf("ResolveCommanders: %v", err)
	}

	listed, err := replay.ListCommanders(databaseConnection)
	if err != nil || len(listed) != 2 {
		testingContext.Fatalf("expected 2 resolved commander profiles, got %+v err=%v", listed, err)
	}
	if listed[0].FID != "F101" || listed[0].EventCount != 2 || len(listed[0].Aliases) != 1 || listed[0].Aliases[0].Name != "CmdrMain" {
		testingContext.Fatalf("expected F101 EventCount=2 (1 named + 1 nameless) and 1 alias, got %+v", listed[0])
	}
	if listed[1].FID != "F202" || listed[1].EventCount != 2 || len(listed[1].Aliases) != 0 {
		testingContext.Fatalf("expected nameless-only F202 EventCount=2 (empty + NULL commander) and 0 blank aliases, got %+v", listed[1])
	}
}

func TestNormalizeAllTimeProfileScope(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	if _, execError := databaseConnection.Exec(`
		INSERT INTO commander_profiles (id, profile_key, display_name, fid, universe, era_index, is_archived_career, enabled, first_seen, last_seen) VALUES
		(1, 'live:f1:era:0', 'ArchivedWipe', 'F1', 'live', 0, 1, 0, '2025-01-01T00:00:00Z', '2025-06-01T00:00:00Z'),
		(2, 'live:f1:era:1', 'ActiveMain', 'F1', 'live', 1, 0, 1, '2026-01-01T00:00:00Z', '2026-02-01T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed commander_profiles: %v", execError)
	}
	if _, execError := databaseConnection.Exec(`
		INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, profile_id, source_file, raw_json, ingested_at) VALUES
		('e1', '2025-01-01T00:00:00Z', 'LoadGame', 'ArchivedWipe', 'F1', 'live', 1, 'a.log', '{}', '2026-02-01T00:00:00Z'),
		('e2', '2026-01-01T00:00:00Z', 'LoadGame', 'ActiveMain', 'F1', 'live', 2, 'b.log', '{}', '2026-02-01T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed journal_events: %v", execError)
	}

	// Sole enabled profile alongside a disabled archived profile collapses to AllEnabled.
	collapsed := replay.NormalizeAllTimeProfileScope(databaseConnection, replay.NewProfileScope([]int64{2}))
	if !collapsed.IsAllEnabled() {
		testingContext.Fatalf("expected sole enabled profile scope to collapse to AllEnabled, got %+v", collapsed)
	}

	// Disabled target profile must preserve its explicit scope so archived expeditions stay isolated.
	disabledTarget := replay.NormalizeAllTimeProfileScope(databaseConnection, replay.NewProfileScope([]int64{1}))
	if disabledTarget.IsAllEnabled() || !disabledTarget.ContainsProfileID(1) {
		testingContext.Fatalf("expected disabled target scope to remain explicit, got %+v", disabledTarget)
	}

	// Unresolved profile_id=0 event must prevent collapsing because AllEnabled falls back to legacy rows.
	if _, execError := databaseConnection.Exec(`
		INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, profile_id, source_file, raw_json, ingested_at)
		VALUES ('e-unresolved', '2026-02-02T00:00:00Z', 'FSDJump', '', '', 'live', 0, 'c.log', '{}', '2026-02-02T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("insert profile_id=0 event: %v", execError)
	}
	withUnresolved := replay.NormalizeAllTimeProfileScope(databaseConnection, replay.NewProfileScope([]int64{2}))
	if withUnresolved.IsAllEnabled() || !withUnresolved.ContainsProfileID(2) {
		testingContext.Fatalf("expected profile_id=0 row to preserve explicit scope, got %+v", withUnresolved)
	}
	if _, execError := databaseConnection.Exec(`DELETE FROM journal_events WHERE event_uid = 'e-unresolved'`); execError != nil {
		testingContext.Fatalf("delete profile_id=0 event: %v", execError)
	}

	// Second enabled profile prevents collapsing so multi-career archives scope accurately.
	if _, execError := databaseConnection.Exec(`
		INSERT INTO commander_profiles (id, profile_key, display_name, fid, universe, era_index, is_archived_career, enabled, first_seen, last_seen)
		VALUES (3, 'live:f2:era:0', 'AltCmdr', 'F2', 'live', 0, 0, 1, '2026-02-03T00:00:00Z', '2026-02-04T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("insert second enabled profile: %v", execError)
	}
	multiEnabled := replay.NormalizeAllTimeProfileScope(databaseConnection, replay.NewProfileScope([]int64{2}))
	if multiEnabled.IsAllEnabled() || !multiEnabled.ContainsProfileID(2) {
		testingContext.Fatalf("expected multi-enabled archive to preserve explicit scope, got %+v", multiEnabled)
	}
}
