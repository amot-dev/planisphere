package replay_test

import (
	"fmt"
	"strings"
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestLoadEventsAllProfilesAfterIDUsesPrimaryKeyAndOrdersTail(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	for index := 1; index <= 200; index++ {
		timestamp := fmt.Sprintf("2026-01-01T00:%02d:%02dZ", index/60, index%60)
		rawLine := fmt.Sprintf(`{"timestamp":%q,"event":"Scan","ScanType":"AutoScan","StarSystem":"HistSys","SystemAddress":1000,"BodyID":%d,"BodyName":"Hist %d","PlanetClass":"Rocky body","MassEM":0.1}`, timestamp, index, index)
		journaltest.InsertParsedLines(testingContext, databaseConnection, rawLine)
	}
	if _, analyzeErr := databaseConnection.Exec(`ANALYZE`); analyzeErr != nil {
		testingContext.Fatalf("analyze journal_events: %v", analyzeErr)
	}

	targetSet := journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventFSDJump,
		journal.EventScan,
		journal.EventSAAScanComplete,
		journal.EventScanOrganic,
		journal.EventSellExplorationData,
	)
	explainSQL := `EXPLAIN QUERY PLAN SELECT id, timestamp, event, raw_json
		FROM journal_events WHERE id > ? AND +event IN (` + journal.FormatSQLInClause(targetSet) + `)
		ORDER BY +timestamp, id`
	planRows, planErr := databaseConnection.Query(explainSQL, 195)
	if planErr != nil {
		testingContext.Fatalf("explain tail query plan: %v", planErr)
	}
	defer planRows.Close()

	var planDetails []string
	for planRows.Next() {
		var selectID, order, from int
		var detail string
		if scanErr := planRows.Scan(&selectID, &order, &from, &detail); scanErr != nil {
			testingContext.Fatalf("scan query plan row: %v", scanErr)
		}
		planDetails = append(planDetails, detail)
	}
	joinedPlan := strings.Join(planDetails, " | ")
	if !strings.Contains(joinedPlan, "SEARCH journal_events USING INTEGER PRIMARY KEY (rowid>?)") {
		testingContext.Fatalf("expected INTEGER PRIMARY KEY tail search plan, got: %s", joinedPlan)
	}
	if strings.Contains(joinedPlan, "idx_journal_events_event_ts") || strings.Contains(joinedPlan, "idx_journal_events_ts") {
		testingContext.Fatalf("tail query must not scan secondary timestamp or event indexes, got: %s", joinedPlan)
	}

	emptyRows, emptyErr := replay.LoadEventsAllProfilesAfterID(databaseConnection, 0, 0)
	if emptyErr != nil || len(emptyRows) != 0 {
		testingContext.Fatalf("expected empty eventSet to return nil, got len=%d err=%v", len(emptyRows), emptyErr)
	}

	var watermarkID int64
	if err := databaseConnection.QueryRow(`SELECT MAX(id) FROM journal_events`).Scan(&watermarkID); err != nil {
		testingContext.Fatalf("query watermark id: %v", err)
	}

	// Insert disabled-profile, unmatched-event, and out-of-timestamp-order tail rows above watermarkID.
	if _, err := databaseConnection.Exec(`INSERT INTO commanders(name, enabled) VALUES ('CmdrDisabled', 0)`); err != nil {
		testingContext.Fatalf("insert disabled commander: %v", err)
	}
	insertTail := func(commander, timestamp, eventName, rawJSON string) {
		uid := journal.ComputeUID(timestamp, eventName, rawJSON+"|"+commander)
		if _, err := databaseConnection.Exec(
			`INSERT INTO journal_events(event_uid, timestamp, event, commander, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?)`,
			uid, timestamp, eventName, commander, rawJSON, timestamp,
		); err != nil {
			testingContext.Fatalf("insert tail %s: %v", eventName, err)
		}
	}
	insertTail("CmdrLive", "2026-02-01T10:00:05Z", "Scan", `{"timestamp":"2026-02-01T10:00:05Z","event":"Scan"}`)
	insertTail("CmdrLive", "2026-02-01T10:00:02Z", "FuelScoop", `{"timestamp":"2026-02-01T10:00:02Z","event":"FuelScoop"}`)
	insertTail("CmdrDisabled", "2026-02-01T10:00:01Z", "FSDJump", `{"timestamp":"2026-02-01T10:00:01Z","event":"FSDJump"}`)

	tailRows, tailErr := replay.LoadEventsAllProfilesAfterID(databaseConnection, watermarkID, targetSet)
	if tailErr != nil {
		testingContext.Fatalf("LoadEventsAllProfilesAfterID: %v", tailErr)
	}
	if len(tailRows) != 2 {
		testingContext.Fatalf("expected 2 matching tail rows across all profiles, got %d", len(tailRows))
	}
	if tailRows[0].Type != journal.EventFSDJump || tailRows[1].Type != journal.EventScan {
		testingContext.Fatalf("expected chronological (timestamp, id) order [FSDJump, Scan], got [%v, %v]", tailRows[0].Type, tailRows[1].Type)
	}
}
