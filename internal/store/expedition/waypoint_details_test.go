package expedition_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func parseDurationLabelMinutes(label string) (int, error) {
	fields := strings.Fields(label)
	if len(fields) < 2 || len(fields) > 4 || strings.Join(fields, " ") != label {
		return 0, fmt.Errorf("invalid duration label %q", label)
	}
	minuteToken := fields[len(fields)-1]
	hourToken := fields[len(fields)-2]
	if len(minuteToken) != 3 || !strings.HasSuffix(minuteToken, "m") || !strings.HasSuffix(hourToken, "h") {
		return 0, fmt.Errorf("missing hours/minutes in duration label %q", label)
	}
	minutes, minErr := strconv.Atoi(strings.TrimSuffix(minuteToken, "m"))
	hours, hourErr := strconv.Atoi(strings.TrimSuffix(hourToken, "h"))
	if minErr != nil || hourErr != nil || minutes < 0 || minutes >= 60 || hours < 0 || hours >= 24 {
		return 0, fmt.Errorf("out-of-range hours/minutes in duration label %q", label)
	}
	years, days := 0, 0
	for _, prefixToken := range fields[:len(fields)-2] {
		switch {
		case strings.HasSuffix(prefixToken, "y") && years == 0 && days == 0:
			parsedYears, err := strconv.Atoi(strings.TrimSuffix(prefixToken, "y"))
			if err != nil || parsedYears <= 0 {
				return 0, fmt.Errorf("invalid years in duration label %q", label)
			}
			years = parsedYears
		case strings.HasSuffix(prefixToken, "d") && days == 0:
			parsedDays, err := strconv.Atoi(strings.TrimSuffix(prefixToken, "d"))
			if err != nil || parsedDays <= 0 || parsedDays >= 365 {
				return 0, fmt.Errorf("invalid days in duration label %q", label)
			}
			days = parsedDays
		default:
			return 0, fmt.Errorf("unexpected token %q in duration label %q", prefixToken, label)
		}
	}
	totalDays := years*365 + days
	return (totalDays*24+hours)*60 + minutes, nil
}

func normalizeOpenWindowElapsed(
	testingContext *testing.T,
	label string,
	batchDetail *expedition.WaypointDetails,
	expectedDetail expedition.WaypointDetails,
) {
	testingContext.Helper()
	if batchDetail.To != "" {
		return
	}
	if batchDetail.From == "" && expectedDetail.From == "" {
		return
	}
	batchMinutes, batchParseErr := parseDurationLabelMinutes(batchDetail.Elapsed)
	expectedMinutes, expectedParseErr := parseDurationLabelMinutes(expectedDetail.Elapsed)
	if batchParseErr != nil || expectedParseErr != nil {
		testingContext.Fatalf("%s open-window Elapsed parse failure: batch=%q (%v) single=%q (%v)",
			label, batchDetail.Elapsed, batchParseErr, expectedDetail.Elapsed, expectedParseErr)
	}
	if batchDetail.Elapsed != expectedDetail.Elapsed {
		minuteDelta := batchMinutes - expectedMinutes
		if minuteDelta < 0 {
			minuteDelta = -minuteDelta
		}
		if minuteDelta > 1 {
			testingContext.Fatalf("%s open-window Elapsed mismatch exceeds 1m rollover tolerance: batch=%q (%dm) single=%q (%dm)",
				label, batchDetail.Elapsed, batchMinutes, expectedDetail.Elapsed, expectedMinutes)
		}
		batchDetail.Elapsed = expectedDetail.Elapsed
	}
}

func assertBatchMatchesIndividualDetails(
	testingContext *testing.T,
	databaseConnection *sql.DB,
	planView expedition.PlanView,
	expeditionProjections *expedition.Projections,
) (*expedition.WaypointDetails, []expedition.WaypointDetails) {
	testingContext.Helper()

	originBaseline, batchRowDetails, batchError := expedition.DetailsBatch(databaseConnection, planView, expeditionProjections)
	if batchError != nil {
		testingContext.Fatalf("DetailsBatch: %v", batchError)
	}

	var expectedOrigin *expedition.WaypointDetails
	expectedLegs := []expedition.WaypointDetails{}
	for _, rowView := range planView.Rows {
		singleDetails, detailsError := expedition.Details(databaseConnection, rowView.Row.ID, expeditionProjections)
		if detailsError != nil {
			testingContext.Fatalf("Details(%d): %v", rowView.Row.ID, detailsError)
		}
		if singleDetails.RowID == 0 {
			if rowView.DrawerFrom != "" || rowView.DrawerTo != "" {
				testingContext.Fatalf("ineligible row %d expected empty drawer bounds, got from=%q to=%q",
					rowView.Row.ID, rowView.DrawerFrom, rowView.DrawerTo)
			}
			continue
		}
		if singleDetails.Kind == "origin" {
			if rowView.DrawerFrom != "" || rowView.DrawerTo != singleDetails.To {
				testingContext.Fatalf("origin row %d drawer bounds mismatch: got (%q,%q) want (%q,%q)",
					rowView.Row.ID, rowView.DrawerFrom, rowView.DrawerTo, "", singleDetails.To)
			}
			originCopy := singleDetails
			expectedOrigin = &originCopy
			continue
		}
		if rowView.DrawerFrom != singleDetails.From || rowView.DrawerTo != singleDetails.To {
			testingContext.Fatalf("leg row %d drawer bounds mismatch: got (%q,%q) want (%q,%q)",
				rowView.Row.ID, rowView.DrawerFrom, rowView.DrawerTo, singleDetails.From, singleDetails.To)
		}
		expectedLegs = append(expectedLegs, singleDetails)
	}

	if (originBaseline == nil) != (expectedOrigin == nil) {
		testingContext.Fatalf("originBaseline presence mismatch: batch=%v single=%v", originBaseline, expectedOrigin)
	}
	if originBaseline != nil {
		normalizedOrigin := *originBaseline
		normalizeOpenWindowElapsed(testingContext, "originBaseline", &normalizedOrigin, *expectedOrigin)
		if !reflect.DeepEqual(normalizedOrigin, *expectedOrigin) {
			testingContext.Fatalf("originBaseline mismatch:\nbatch:  %+v\nsingle: %+v", normalizedOrigin, *expectedOrigin)
		}
	}

	if len(batchRowDetails) != len(expectedLegs) {
		testingContext.Fatalf("rowDetails length mismatch: batch=%d single=%d", len(batchRowDetails), len(expectedLegs))
	}
	for index := range batchRowDetails {
		normalizedBatch := batchRowDetails[index]
		normalizeOpenWindowElapsed(testingContext, fmt.Sprintf("leg[%d]", index), &normalizedBatch, expectedLegs[index])
		if !reflect.DeepEqual(normalizedBatch, expectedLegs[index]) {
			testingContext.Fatalf("leg[%d] mismatch:\nbatch:  %+v\nsingle: %+v", index, normalizedBatch, expectedLegs[index])
		}
	}
	return originBaseline, batchRowDetails
}

func TestDetailsBatchParityAndEffectiveStartFromRows(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, expeditionProjections := expeditiontest.NewProjections(databaseConnection)

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-02-01T08:00:00Z","event":"LoadGame","Commander":"CmdrBatch","FID":"FB1","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-02-01T08:30:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-02-01T08:45:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Sol","SystemAddress":1001,"BodyID":1,"BodyName":"Sol 3","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`,
	)

	expeditionRecord, createError := expedition.CreateWithWaypoints(databaseConnection, "Batch Route", "Parity check", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Alpha Leg"},
		{SystemName: "Beta Carrier"},
		{SystemName: "Gamma Swap"},
		{SystemName: "Delta Current"},
		{SystemName: "Epsilon Future"},
	})
	if createError != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", createError)
	}
	initialRows, listInitialError := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listInitialError != nil {
		testingContext.Fatalf("ListRows: %v", listInitialError)
	}
	// Insert a region header between Alpha Leg and Beta Carrier to exercise non-waypoint row numbering.
	mustInsert(testingContext, databaseConnection, expeditionRecord.ID, initialRows[1].ID, expedition.RowRegion, 18)

	planningPlan, planBuildError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, expeditionProjections)
	if planBuildError != nil {
		testingContext.Fatalf("BuildPlan planning: %v", planBuildError)
	}
	if after, start, departed := expedition.EffectiveStartFromRows(planningPlan.Expedition, planningPlan.Rows); after != "" || start != "" || departed {
		testingContext.Fatalf("expected empty EffectiveStartFromRows for planning, got after=%q start=%q departed=%v", after, start, departed)
	}
	planningOrigin, planningLegs := assertBatchMatchesIndividualDetails(testingContext, databaseConnection, planningPlan, expeditionProjections)
	if planningOrigin != nil || planningLegs == nil || len(planningLegs) != 0 {
		testingContext.Fatalf("expected nil origin and empty non-nil legs for planning, got origin=%v legs=%v", planningOrigin, planningLegs)
	}

	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-02-01T08:30:00Z", 0); startError != nil {
		testingContext.Fatalf("StartTx: %v", startError)
	}
	stagedPlan, stagedPlanError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, expeditionProjections)
	if stagedPlanError != nil {
		testingContext.Fatalf("BuildPlan staged: %v", stagedPlanError)
	}
	if after, start, departed := expedition.EffectiveStartFromRows(stagedPlan.Expedition, stagedPlan.Rows); after != "" || start != "" || departed {
		testingContext.Fatalf("expected un-departed EffectiveStartFromRows while staged, got after=%q start=%q departed=%v", after, start, departed)
	}
	stagedOrigin, stagedLegs := assertBatchMatchesIndividualDetails(testingContext, databaseConnection, stagedPlan, expeditionProjections)
	if stagedOrigin == nil || !stagedOrigin.InSystem || len(stagedLegs) != 0 {
		testingContext.Fatalf("expected staged InSystem origin and 0 legs, got origin=%+v legs=%v", stagedOrigin, stagedLegs)
	}

	// Leg 2 (Alpha Leg): single-hull fast path (Mandalay).
	// Leg 3 (Beta Carrier): 0 piloted jumps via CarrierJump (ShipsUsed stays empty).
	// Leg 4 (Gamma Swap): multi-ship leg (Mandalay jump, ShipyardSwap to Anaconda, Anaconda jump).
	// Leg 5 (Delta Current): departed Gamma Swap into open space with Cobra Mk III jump (VisitCurrent).
	journaltest.InsertParsedLines(testingContext, databaseConnection,
		`{"timestamp":"2026-02-01T09:00:00Z","event":"FSDJump","StarSystem":"Alpha Leg","SystemAddress":2002,"StarPos":[40,0,0],"JumpDist":40.0}`,
		`{"timestamp":"2026-02-01T10:00:00Z","event":"CarrierJump","StarSystem":"Beta Carrier","SystemAddress":3003,"StarPos":[540,0,0]}`,
		`{"timestamp":"2026-02-01T11:00:00Z","event":"FSDJump","StarSystem":"Mid Swap","SystemAddress":4001,"StarPos":[570,0,0],"JumpDist":30.0}`,
		`{"timestamp":"2026-02-01T11:10:00Z","event":"ShipyardSwap","ShipType":"anaconda","ShipType_Localised":"Anaconda","ShipID":2}`,
		`{"timestamp":"2026-02-01T11:20:00Z","event":"FSDJump","StarSystem":"Gamma Swap","SystemAddress":4004,"StarPos":[620,0,0],"JumpDist":50.0}`,
		`{"timestamp":"2026-02-01T11:25:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Gamma Swap","SystemAddress":4004,"BodyID":1,"BodyName":"Gamma Swap 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-02-01T11:50:00Z","event":"ShipyardSwap","ShipType":"cobramkiii","ShipType_Localised":"Cobra Mk III","ShipID":3}`,
		`{"timestamp":"2026-02-01T12:00:00Z","event":"FSDJump","StarSystem":"Outpost Hop","SystemAddress":5001,"StarPos":[640,0,0],"JumpDist":20.0}`,
	)
	if _, reevalError := expedition.ReevaluateTx(databaseConnection, expeditionRecord.ID); reevalError != nil {
		testingContext.Fatalf("ReevaluateTx: %v", reevalError)
	}

	_, freshActiveProjections := expeditiontest.NewProjections(databaseConnection)
	activePlan, activePlanError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, freshActiveProjections)
	if activePlanError != nil {
		testingContext.Fatalf("BuildPlan active: %v", activePlanError)
	}
	dbQueryAfter, dbDisplayStart, dbDeparted, effectiveError := expedition.EffectiveStart(databaseConnection, activePlan.Expedition)
	if effectiveError != nil {
		testingContext.Fatalf("EffectiveStart: %v", effectiveError)
	}
	rowsQueryAfter, rowsDisplayStart, rowsDeparted := expedition.EffectiveStartFromRows(activePlan.Expedition, activePlan.Rows)
	if rowsQueryAfter != dbQueryAfter || rowsDisplayStart != dbDisplayStart || rowsDeparted != dbDeparted {
		testingContext.Fatalf("EffectiveStartFromRows mismatch: got (%q,%q,%v) want (%q,%q,%v)",
			rowsQueryAfter, rowsDisplayStart, rowsDeparted, dbQueryAfter, dbDisplayStart, dbDeparted)
	}

	_, activeLegs := assertBatchMatchesIndividualDetails(testingContext, databaseConnection, activePlan, freshActiveProjections)
	if len(activeLegs) != 4 {
		testingContext.Fatalf("expected 4 active legs (3 visited + 1 VisitCurrent), got %d", len(activeLegs))
	}
	if !reflect.DeepEqual(activeLegs[0].ShipsUsed, []string{"Mandalay"}) ||
		!reflect.DeepEqual(activeLegs[1].ShipsUsed, []string{}) ||
		!reflect.DeepEqual(activeLegs[2].ShipsUsed, []string{"Mandalay", "Anaconda"}) ||
		!reflect.DeepEqual(activeLegs[3].ShipsUsed, []string{"Cobra Mk III"}) {
		testingContext.Fatalf("unexpected ShipsUsed across active legs: %+v, %+v, %+v, %+v",
			activeLegs[0].ShipsUsed, activeLegs[1].ShipsUsed, activeLegs[2].ShipsUsed, activeLegs[3].ShipsUsed)
	}

	if completeError := expedition.CompleteTx(databaseConnection, expeditionRecord.ID); completeError != nil {
		testingContext.Fatalf("CompleteTx: %v", completeError)
	}
	_, freshCompletedProjections := expeditiontest.NewProjections(databaseConnection)
	completedPlan, completedPlanError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, freshCompletedProjections)
	if completedPlanError != nil {
		testingContext.Fatalf("BuildPlan completed: %v", completedPlanError)
	}
	_, completedLegs := assertBatchMatchesIndividualDetails(testingContext, databaseConnection, completedPlan, freshCompletedProjections)
	if len(completedLegs) != 3 || completedLegs[2].To != "2026-02-01T11:25:00Z" || completedLegs[2].InSystem {
		testingContext.Fatalf("expected 3 completed legs with final leg clamped to 11:25:00Z, got %+v", completedLegs)
	}

	// Verify same-VisitedAt group tie-breaking matches findPrecedingVisitedWaypoint (first row of preceding timestamp group wins).
	if _, tieExecError := databaseConnection.Exec(
		`UPDATE expedition_rows SET visited_at = '2026-02-01T09:00:00Z' WHERE system_name = 'Beta Carrier'`,
	); tieExecError != nil {
		testingContext.Fatalf("set tied visited_at: %v", tieExecError)
	}
	tiedPlan, tiedPlanError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, freshCompletedProjections)
	if tiedPlanError != nil {
		testingContext.Fatalf("BuildPlan tied visited_at: %v", tiedPlanError)
	}
	_, tiedLegs := assertBatchMatchesIndividualDetails(testingContext, databaseConnection, tiedPlan, freshCompletedProjections)
	if tiedLegs[1].PrevSystem != "Sol" || tiedLegs[2].PrevSystem != "Alpha Leg" {
		testingContext.Fatalf("expected tied-timestamp PrevSystem Sol and Alpha Leg, got %q and %q", tiedLegs[1].PrevSystem, tiedLegs[2].PrevSystem)
	}

	// Verify DetailsBatch executes zero queries against expeditions or expedition_rows on cold projections.
	_, coldProjections := expeditiontest.NewProjections(databaseConnection)
	if _, dropTablesError := databaseConnection.Exec(`DROP TABLE expedition_rows; DROP TABLE expeditions`); dropTablesError != nil {
		testingContext.Fatalf("drop expedition tables: %v", dropTablesError)
	}
	if _, noTableLegs, noTableError := expedition.DetailsBatch(databaseConnection, completedPlan, coldProjections); noTableError != nil || len(noTableLegs) != 3 {
		testingContext.Fatalf("expected cold DetailsBatch to succeed without expedition tables, got len=%d err=%v", len(noTableLegs), noTableError)
	}
}
