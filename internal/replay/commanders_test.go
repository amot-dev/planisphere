package replay_test

import (
	"fmt"
	"strings"
	"testing"

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
