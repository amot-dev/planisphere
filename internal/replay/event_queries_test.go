package replay_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/replay/realization"
	"planisphere/internal/replay/stats"
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

func TestLoadEventsRawJSONSlabIsolationAndReplayParity(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	baseTime := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	timestampAt := func(offsetSeconds int) string {
		return baseTime.Add(time.Duration(offsetSeconds) * time.Second).Format(time.RFC3339)
	}

	paddingComment := strings.Repeat("x", 320)
	expectedPayloads := make([]string, 0, 310)
	appendLine := func(rawLine string) {
		expectedPayloads = append(expectedPayloads, rawLine)
	}

	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"LoadGame","Commander":"CmdrSlab","FID":"F777","Odyssey":true}`, timestampAt(0)))
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"Location","StarSystem":"SlabOrigin","SystemAddress":7000,"StarPos":[0,0,0],"Population":0}`, timestampAt(1)))
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"FSDJump","StarSystem":"SlabAlpha","SystemAddress":7001,"StarPos":[30,40,0],"JumpDist":50.0,"Population":0}`, timestampAt(2)))
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"CarrierJump","StarSystem":"SlabBeta","SystemAddress":7002,"StarPos":[30,40,120],"Population":0}`, timestampAt(3)))

	// Insert enough padded Scan rows to span multiple 4 KiB and 64 KiB byte slabs and slice growths.
	for bodyNumber := 1; bodyNumber <= 300; bodyNumber++ {
		appendLine(fmt.Sprintf(
			`{"timestamp":%q,"event":"Scan","ScanType":"Detailed","StarSystem":"SlabBeta","SystemAddress":7002,"BodyID":%d,"BodyName":"SlabBeta %d","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false,"Note":%q}`,
			timestampAt(10+bodyNumber),
			bodyNumber,
			bodyNumber,
			fmt.Sprintf("row-%03d-%s", bodyNumber, paddingComment),
		))
	}
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"SAAScanComplete","SystemAddress":7002,"BodyID":1,"BodyName":"SlabBeta 1","ProbesUsed":3,"EfficiencyTarget":4}`, timestampAt(400)))
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":7002,"Body":1}`, timestampAt(405)))
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"SellExplorationData","Systems":["SlabBeta"],"Discovered":["SlabBeta 1"],"BaseValue":600000,"Bonus":200000,"TotalEarnings":800000}`, timestampAt(410)))
	appendLine(fmt.Sprintf(`{"timestamp":%q,"event":"SellOrganicData","MarketID":8800,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":76043200}]}`, timestampAt(415)))

	journaltest.InsertParsedLines(testingContext, databaseConnection, expectedPayloads...)

	replayEventSet := realization.SalesLedgerReplayEvents
	allRows, loadAllError := replay.LoadEventsAllProfiles(databaseConnection, "", "", replayEventSet)
	if loadAllError != nil {
		testingContext.Fatalf("LoadEventsAllProfiles: %v", loadAllError)
	}
	if len(allRows) != len(expectedPayloads) {
		testingContext.Fatalf("expected %d rows, got %d", len(expectedPayloads), len(allRows))
	}

	tailRows, loadTailError := replay.LoadEventsAllProfilesAfterID(databaseConnection, 0, replayEventSet)
	if loadTailError != nil || len(tailRows) != len(expectedPayloads) {
		testingContext.Fatalf("LoadEventsAllProfilesAfterID: len=%d want=%d err=%v", len(tailRows), len(expectedPayloads), loadTailError)
	}

	for rowIndex, expectedJSON := range expectedPayloads {
		if !bytes.Equal(allRows[rowIndex].RawJSON, []byte(expectedJSON)) {
			testingContext.Fatalf("allRows[%d] RawJSON clobbered or mismatched: got %q want %q", rowIndex, string(allRows[rowIndex].RawJSON), expectedJSON)
		}
		if !bytes.Equal(tailRows[rowIndex].RawJSON, []byte(expectedJSON)) {
			testingContext.Fatalf("tailRows[%d] RawJSON clobbered or mismatched across 4 KiB slabs: got %q want %q", rowIndex, string(tailRows[rowIndex].RawJSON), expectedJSON)
		}
		if cap(allRows[rowIndex].RawJSON) != len(allRows[rowIndex].RawJSON) {
			testingContext.Fatalf("allRows[%d] RawJSON cap=%d must equal len=%d to prevent cross-row overwrite", rowIndex, cap(allRows[rowIndex].RawJSON), len(allRows[rowIndex].RawJSON))
		}
	}

	// Appending to one row's RawJSON must allocate a new slice rather than mutating the next row in the slab.
	nextRowSnapshot := string(allRows[5].RawJSON)
	_ = append(allRows[4].RawJSON, '!')
	if string(allRows[5].RawJSON) != nextRowSnapshot {
		testingContext.Fatalf("appending to allRows[4].RawJSON overwrote adjacent allRows[5].RawJSON")
	}

	if allRows[0].HasStarPos {
		testingContext.Fatalf("LoadGame row must have HasStarPos=false, got StarPos=%+v", allRows[0].StarPos)
	}
	expectedJumpCoords := reference.Vec3{X: 30, Y: 40, Z: 0}
	if !allRows[2].HasStarPos || allRows[2].StarPos != expectedJumpCoords {
		testingContext.Fatalf("FSDJump row expected HasStarPos=true and StarPos=%+v, got has=%v pos=%+v", expectedJumpCoords, allRows[2].HasStarPos, allRows[2].StarPos)
	}

	latestSaleRow, latestError := replay.QueryLatestEvent(databaseConnection, journal.NewEventSet(journal.EventSellOrganicData))
	if latestError != nil || latestSaleRow == nil || !bytes.Equal(latestSaleRow.RawJSON, []byte(expectedPayloads[len(expectedPayloads)-1])) {
		testingContext.Fatalf("QueryLatestEvent RawJSON mismatch: row=%+v err=%v", latestSaleRow, latestError)
	}

	engine := stats.NewEngine(databaseConnection)
	travelMetrics, travelError := engine.ComputeTravel(stats.Range{}, nil)
	if travelError != nil || travelMetrics.Jumps != 1 || travelMetrics.ShipDistanceLy != 50.0 || travelMetrics.CarrierPassengerLy != 120.0 {
		testingContext.Fatalf("ComputeTravel mismatch: metrics=%+v err=%v", travelMetrics, travelError)
	}
	cartoMetrics, cartoError := engine.ComputeCartographics(stats.Range{}, nil)
	if cartoError != nil || cartoMetrics.BodiesScanned != 300 || cartoMetrics.BodiesMapped != 1 || cartoMetrics.ScanValueTotal <= 0 {
		testingContext.Fatalf("ComputeCartographics mismatch: metrics=%+v err=%v", cartoMetrics, cartoError)
	}
	exoMetrics, exoError := engine.ComputeExobiology(stats.Range{}, nil)
	if exoError != nil || exoMetrics.OrganicSamples != 1 || exoMetrics.FirstLogged != 1 || exoMetrics.OrganicValueTotal != 95054000 {
		testingContext.Fatalf("ComputeExobiology mismatch: metrics=%+v err=%v", exoMetrics, exoError)
	}
	realizationBreakdown, realizationError := engine.ComputeRealization(stats.Range{}, nil)
	if realizationError != nil || realizationBreakdown.TotalRealized != 800000+95054000 || realizationBreakdown.PendingValue != 0 {
		testingContext.Fatalf("ComputeRealization mismatch: breakdown=%+v err=%v", realizationBreakdown, realizationError)
	}
}
