package expedition_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestInsertRowPositionsAndReevaluateConsolidatedUpdates(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)
	expeditionRecord, wp1 := createExpeditionWithOriginRow(t, dbConn, "Batch Test", "Test position updates")

	// Insert at end
	wp2 := mustInsert(t, dbConn, expeditionRecord.ID, expedition.InsertAtEnd, expedition.RowWaypoint, 0)
	if wp2.Position != 2 {
		t.Errorf("expected wp2 position 2, got %d", wp2.Position)
	}
	wp3 := mustInsert(t, dbConn, expeditionRecord.ID, wp2.ID, expedition.RowWaypoint, 0)
	if wp3.Position != 3 {
		t.Errorf("expected wp3 position 3, got %d", wp3.Position)
	}

	// Verify all 3 positions
	rows, listErr := expedition.ListRows(dbConn, expeditionRecord.ID)
	if listErr != nil {
		t.Fatalf("ListRows after end inserts: %v", listErr)
	}
	if len(rows) != 3 || rows[0].Position != 1 || rows[1].Position != 2 || rows[2].Position != 3 {
		t.Fatalf("unexpected rows after end inserts: %+v", rows)
	}

	// Insert in middle (after wp1, before wp2)
	wpMid := mustInsert(t, dbConn, expeditionRecord.ID, wp1.ID, expedition.RowWaypoint, 0)
	if wpMid.Position != 2 {
		t.Errorf("expected wpMid position 2, got %d", wpMid.Position)
	}

	rows, listErr = expedition.ListRows(dbConn, expeditionRecord.ID)
	if listErr != nil {
		t.Fatalf("ListRows after middle insert: %v", listErr)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if rows[0].ID != wp1.ID || rows[0].Position != 1 {
		t.Errorf("wp1 position altered: %+v", rows[0])
	}
	if rows[1].ID != wpMid.ID || rows[1].Position != 2 {
		t.Errorf("wpMid position mismatch: %+v", rows[1])
	}
	if rows[2].ID != wp2.ID || rows[2].Position != 3 {
		t.Errorf("wp2 position should shift to 3: %+v", rows[2])
	}
	if rows[3].ID != wp3.ID || rows[3].Position != 4 {
		t.Errorf("wp3 position should shift to 4: %+v", rows[3])
	}

	// Insert at top
	wpTop := mustInsert(t, dbConn, expeditionRecord.ID, expedition.InsertAtTop, expedition.RowWaypoint, 0)
	if wpTop.Position != 1 {
		t.Errorf("expected wpTop position 1, got %d", wpTop.Position)
	}

	rows, listErr = expedition.ListRows(dbConn, expeditionRecord.ID)
	if listErr != nil {
		t.Fatalf("ListRows after top insert: %v", listErr)
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	for i, r := range rows {
		if r.Position != i+1 {
			t.Errorf("row %d position is %d, want %d", i, r.Position, i+1)
		}
	}

	// Test sequential Reevaluate, duplicate system protection, and chronological MoveRow bounds
	setRowSystemName(t, dbConn, wpTop.ID, "Alpha")
	setRowSystemName(t, dbConn, wp1.ID, "Beta")
	setRowSystemName(t, dbConn, wpMid.ID, "Gamma")
	setRowSystemName(t, dbConn, wp2.ID, "Delta")
	setRowSystemName(t, dbConn, wp3.ID, "Alpha")

	// Start expedition
	if startErr := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-02-01T10:00:00Z", 0)
	}); startErr != nil {
		t.Fatalf("Start failed: %v", startErr)
	}

	// Commander jumps Alpha -> Interim -> Alpha (early return before Beta) -> Beta -> Gamma.
	// The early return to Alpha must NOT consume the final return waypoint wp3 ("Alpha"),
	// and Beta + Gamma must be marked visited in sequence.
	_, err := dbConn.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at)
		VALUES ('ev-start', '2026-02-01T10:00:00Z', 'FSDJump', 'Alpha', '{}', '2026-02-01T10:00:00Z'),
		       ('ev-interim', '2026-02-01T10:30:00Z', 'FSDJump', 'Interim', '{}', '2026-02-01T10:30:00Z'),
		       ('ev-alpha-early', '2026-02-01T11:00:00Z', 'FSDJump', 'Alpha', '{}', '2026-02-01T11:00:00Z'),
		       ('ev-beta', '2026-02-01T12:00:00Z', 'FSDJump', 'Beta', '{}', '2026-02-01T12:00:00Z'),
		       ('ev-gamma', '2026-02-01T13:00:00Z', 'FSDJump', 'Gamma', '{}', '2026-02-01T13:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	changed, reevalErr := expedition.Reevaluate(dbConn, expeditionRecord.ID)
	if reevalErr != nil {
		t.Fatalf("Reevaluate failed: %v", reevalErr)
	}
	if !changed {
		t.Fatalf("expected Reevaluate to report changes")
	}

	rows, listErr = expedition.ListRows(dbConn, expeditionRecord.ID)
	if listErr != nil {
		t.Fatalf("ListRows after Reevaluate: %v", listErr)
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	expectedOrder := []string{"Alpha", "Beta", "Gamma", "Delta", "Alpha"}
	for i, expectedSys := range expectedOrder {
		if rows[i].SystemName != expectedSys {
			t.Errorf("row %d system %s, want %s", i, rows[i].SystemName, expectedSys)
		}
		if rows[i].Position != i+1 {
			t.Errorf("row %d position %d, want %d", i, rows[i].Position, i+1)
		}
	}
	if rows[0].VisitedAt != "2026-02-01T10:00:00Z" {
		t.Errorf("Alpha origin visited_at mismatch: %q", rows[0].VisitedAt)
	}
	if rows[1].VisitedAt != "2026-02-01T12:00:00Z" || rows[1].VisitEventUID != "ev-beta" {
		t.Errorf("Beta visit mismatch: %q / %q", rows[1].VisitedAt, rows[1].VisitEventUID)
	}
	if rows[2].VisitedAt != "2026-02-01T13:00:00Z" || rows[2].VisitEventUID != "ev-gamma" {
		t.Errorf("Gamma visit mismatch: %q / %q", rows[2].VisitedAt, rows[2].VisitEventUID)
	}
	if rows[4].VisitedAt != "" {
		t.Errorf("expected final return Alpha waypoint to remain unvisited, got %q", rows[4].VisitedAt)
	}

	// Moving wp3 ("Alpha", visited at 11:00 between Alpha@10:00 and Beta@12:00) to index 1 (before Beta) is valid!
	// Moving wp2 ("Delta", never visited) to index 1 (before Beta) must be rejected as out of chronological bounds.
	if invalidMoveErr := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		_, moveErr := expedition.MoveRow(transaction, wp2.ID, 1)
		return moveErr
	}); invalidMoveErr == nil {
		t.Fatalf("expected moving unvisited Delta before visited Beta to fail")
	}
	if validMoveErr := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if _, moveErr := expedition.MoveRow(transaction, wp3.ID, 1); moveErr != nil {
			return moveErr
		}
		_, err := expedition.Reevaluate(transaction, expeditionRecord.ID)
		return err
	}); validMoveErr != nil {
		t.Fatalf("expected moving Alpha to its 11:00 historical window to succeed, got %v", validMoveErr)
	}
	rows, listErr = expedition.ListRows(dbConn, expeditionRecord.ID)
	if listErr != nil {
		t.Fatalf("ListRows after MoveRow: %v", listErr)
	}
	if rows[1].ID != wp3.ID || rows[1].VisitedAt != "2026-02-01T11:00:00Z" {
		t.Fatalf("expected moved Alpha at index 1 to bind to 2026-02-01T11:00:00Z, got %+v", rows[1])
	}

	// Second reevaluation with no new events should be a no-op (changed = false)
	changedAgain, reevalErr2 := expedition.Reevaluate(dbConn, expeditionRecord.ID)
	if reevalErr2 != nil {
		t.Fatalf("second Reevaluate failed: %v", reevalErr2)
	}
	if changedAgain {
		t.Errorf("expected second Reevaluate to be idempotent (changed = false), got true")
	}
}

func TestHubRevisitMovePreservesLegMode(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	defaultModes := expeditiontest.LookupDefaultModes(testingContext, databaseConnection)
	explorationModeID := defaultModes.Exploration
	travelModeID := defaultModes.Travel

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":2001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-03-01T11:00:00Z","event":"FSDJump","StarSystem":"Hub","SystemAddress":2002,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-03-01T11:30:00Z","event":"FSDJump","StarSystem":"Outpost","SystemAddress":2003,"StarPos":[20,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"FSDJump","StarSystem":"Hub","SystemAddress":2002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	)

	expeditionRecord, solWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Hub Loop Test", "")
	setRowSystemName(testingContext, databaseConnection, solWaypoint.ID, "Sol")
	outpostWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, solWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, outpostWaypoint.ID, "Outpost")
	hubWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, outpostWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, hubWaypoint.ID, "Hub")
	vegaWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, hubWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, vegaWaypoint.ID, "Vega")

	if startError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-03-01T10:00:00Z", travelModeID)
	}); startError != nil {
		testingContext.Fatal(startError)
	}
	if _, setModeError := expedition.SetLegModeTx(databaseConnection, hubWaypoint.ID, explorationModeID, false); setModeError != nil {
		testingContext.Fatal(setModeError)
	}

	initialHub, getInitialHubError := expedition.GetRow(databaseConnection, hubWaypoint.ID)
	if getInitialHubError != nil {
		testingContext.Fatal(getInitialHubError)
	}
	if initialHub.VisitedAt != "2026-03-01T12:00:00Z" || initialHub.LegModeID != explorationModeID {
		testingContext.Fatalf("expected Hub initially at 12:00 with exploration mode %d, got visitedAt=%q mode=%d", explorationModeID, initialHub.VisitedAt, initialHub.LegModeID)
	}

	// Moving visited Outpost down into the unvisited section (index 3, below Vega) must succeed,
	// clearing Outpost's leg mode and re-binding Hub to its earlier 11:00 arrival while preserving Hub's mode.
	if moveOutpostError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		if _, moveError := expedition.MoveRow(transaction, outpostWaypoint.ID, 3); moveError != nil {
			return moveError
		}
		_, reevalError := expedition.Reevaluate(transaction, expeditionRecord.ID)
		return reevalError
	}); moveOutpostError != nil {
		testingContext.Fatalf("expected moving Outpost to unvisited to succeed across Hub revisit, got %v", moveOutpostError)
	}

	reboundHub, getReboundHubError := expedition.GetRow(databaseConnection, hubWaypoint.ID)
	if getReboundHubError != nil {
		testingContext.Fatal(getReboundHubError)
	}
	if reboundHub.VisitedAt != "2026-03-01T11:00:00Z" || reboundHub.LegModeID != explorationModeID {
		testingContext.Fatalf("expected Hub to rebind to 11:00 preserving exploration mode %d, got visitedAt=%q mode=%d", explorationModeID, reboundHub.VisitedAt, reboundHub.LegModeID)
	}
	unvisitedOutpost, getOutpostError := expedition.GetRow(databaseConnection, outpostWaypoint.ID)
	if getOutpostError != nil {
		testingContext.Fatal(getOutpostError)
	}
	if unvisitedOutpost.VisitedAt != "" || unvisitedOutpost.LegModeID != 0 {
		testingContext.Fatalf("expected unvisited Outpost to clear LegModeID, got visitedAt=%q mode=%d", unvisitedOutpost.VisitedAt, unvisitedOutpost.LegModeID)
	}
}

func TestUpdateRowTxRollbackAndCompletedRestrictions(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	expeditionRecord, originWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Row Guard Completed Test", "")
	setRowSystemName(testingContext, databaseConnection, originWaypoint.ID, "Sol")
	regionRow := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, originWaypoint.ID, expedition.RowRegion, 1)
	secondWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, regionRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, secondWaypoint.ID, "Colonia")

	editedPOI := "Should Roll Back"
	invalidRegionID := 999999
	if _, updateError := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{
		ID:       regionRow.ID,
		POIName:  &editedPOI,
		RegionID: &invalidRegionID,
	}); updateError == nil {
		testingContext.Fatalf("expected invalid RegionID to fail")
	}
	reloadedRegion, reloadRegionError := expedition.GetRow(databaseConnection, regionRow.ID)
	if reloadRegionError != nil {
		testingContext.Fatal(reloadRegionError)
	}
	if reloadedRegion.POIName != "" || reloadedRegion.RegionID != 1 {
		testingContext.Fatalf("expected UpdateRowTx to roll back POIName on invalid RegionID, got %+v", reloadedRegion)
	}

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T10:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-01-18T12:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,100],"JumpDist":50.0}`,
	)
	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-01-18T10:00:00Z", 0); startError != nil {
		testingContext.Fatal(startError)
	}
	if completeError := expedition.CompleteTx(databaseConnection, expeditionRecord.ID); completeError != nil {
		testingContext.Fatal(completeError)
	}

	replacementSystem := "Beagle Point"
	flagTrue := true
	newRegionID := 2
	completedRejections := []struct {
		label string
		patch expedition.RowPatch
	}{
		{"SystemName", expedition.RowPatch{ID: secondWaypoint.ID, POIName: &editedPOI, SystemName: &replacementSystem}},
		{"FCStop", expedition.RowPatch{ID: secondWaypoint.ID, FCStop: &flagTrue}},
		{"FCArrived", expedition.RowPatch{ID: secondWaypoint.ID, FCArrived: &flagTrue}},
		{"RegionID", expedition.RowPatch{ID: regionRow.ID, RegionID: &newRegionID}},
	}
	for _, testCase := range completedRejections {
		if _, err := expedition.UpdateRowTx(databaseConnection, testCase.patch); err == nil {
			testingContext.Fatalf("expected completed expedition to reject %s edit", testCase.label)
		}
	}
	reloadedSecond, reloadSecondError := expedition.GetRow(databaseConnection, secondWaypoint.ID)
	if reloadSecondError != nil {
		testingContext.Fatal(reloadSecondError)
	}
	if reloadedSecond.POIName != "" || reloadedSecond.SystemName != "Colonia" {
		testingContext.Fatalf("expected rejected SystemName patch to leave POIName untouched, got %+v", reloadedSecond)
	}

	cosmeticPOI := "Jaques Station"
	cosmeticNotes := "Final survey"
	unchangedSystem := "Colonia"
	systemChanged, cosmeticError := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{
		ID:         secondWaypoint.ID,
		POIName:    &cosmeticPOI,
		Notes:      &cosmeticNotes,
		SystemName: &unchangedSystem,
	})
	if cosmeticError != nil || systemChanged {
		testingContext.Fatalf("expected cosmetic and unchanged updates on completed expedition to succeed without systemChanged, got changed=%v err=%v", systemChanged, cosmeticError)
	}
	updatedSecond, reloadUpdatedError := expedition.GetRow(databaseConnection, secondWaypoint.ID)
	if reloadUpdatedError != nil {
		testingContext.Fatal(reloadUpdatedError)
	}
	if updatedSecond.POIName != cosmeticPOI || updatedSecond.Notes != cosmeticNotes {
		testingContext.Fatalf("expected POIName and Notes to persist on completed expedition, got %+v", updatedSecond)
	}
}

func TestUpdateRowActiveStructuralGuards(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T11:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[4,0,0],"JumpDist":4.0}`,
	)

	expeditionRecord, originWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Active Guards Test", "")
	setRowSystemName(testingContext, databaseConnection, originWaypoint.ID, "Sol")
	visitedWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, originWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, visitedWaypoint.ID, "Alpha Centauri")
	arrivedStopWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, visitedWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, arrivedStopWaypoint.ID, "Sirius")
	unvisitedWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, arrivedStopWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, unvisitedWaypoint.ID, "Barnard's Star")
	routedStopWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, unvisitedWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, routedStopWaypoint.ID, "Vega")
	trailingWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, routedStopWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, trailingWaypoint.ID, "Altair")

	flagTrue := true
	flagFalse := false
	for _, stopID := range []int64{originWaypoint.ID, arrivedStopWaypoint.ID, routedStopWaypoint.ID} {
		if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: stopID, FCStop: &flagTrue}); err != nil {
			testingContext.Fatal(err)
		}
	}
	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-01-18T10:00:00Z", 0); startError != nil {
		testingContext.Fatal(startError)
	}
	if _, overrideError := expedition.SetCarrierArrivalOverrideTx(databaseConnection, arrivedStopWaypoint.ID, true); overrideError != nil {
		testingContext.Fatal(overrideError)
	}

	spanshRoute := expedition.ActiveCarrierRouteState{
		ExpeditionID: expeditionRecord.ID,
		TargetRowID:  routedStopWaypoint.ID,
		ManualMode:   false,
		Route: expedition.CarrierRouteResult{
			Origin:      "Sirius",
			Destination: "Vega",
			Jumps:       []expedition.CarrierRouteJump{{SystemName: "Vega"}},
		},
	}
	if saveRouteError := expedition.SaveActiveCarrierRoute(databaseConnection, spanshRoute); saveRouteError != nil {
		testingContext.Fatal(saveRouteError)
	}

	newSystem := "Ross 154"
	systemGuardCases := []struct {
		label string
		rowID int64
	}{
		{"origin waypoint", originWaypoint.ID},
		{"visited waypoint", visitedWaypoint.ID},
		{"arrived carrier stop", arrivedStopWaypoint.ID},
		{"active Spansh target", routedStopWaypoint.ID},
	}
	for _, testCase := range systemGuardCases {
		if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: testCase.rowID, SystemName: &newSystem}); err == nil {
			testingContext.Fatalf("expected active expedition to reject SystemName edit on %s", testCase.label)
		}
	}

	if changed, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: unvisitedWaypoint.ID, SystemName: &newSystem}); err != nil || !changed {
		testingContext.Fatalf("expected unvisited non-target SystemName edit to succeed, got changed=%v err=%v", changed, err)
	}

	fcGuardCases := []struct {
		label string
		patch expedition.RowPatch
	}{
		{"clear arrived FCStop", expedition.RowPatch{ID: arrivedStopWaypoint.ID, FCStop: &flagFalse}},
		{"add FCStop before arrival frontier", expedition.RowPatch{ID: visitedWaypoint.ID, FCStop: &flagTrue}},
		{"add FCStop before Spansh target", expedition.RowPatch{ID: unvisitedWaypoint.ID, FCStop: &flagTrue}},
		{"clear FCStop on Spansh target", expedition.RowPatch{ID: routedStopWaypoint.ID, FCStop: &flagFalse}},
	}
	for _, testCase := range fcGuardCases {
		if _, err := expedition.UpdateRowTx(databaseConnection, testCase.patch); err == nil {
			testingContext.Fatalf("expected active expedition to reject %s", testCase.label)
		}
	}

	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: trailingWaypoint.ID, FCStop: &flagTrue}); err != nil {
		testingContext.Fatalf("expected adding FCStop after Spansh target to succeed, got %v", err)
	}

	spanshRoute.ManualMode = true
	if saveManualError := expedition.SaveActiveCarrierRoute(databaseConnection, spanshRoute); saveManualError != nil {
		testingContext.Fatal(saveManualError)
	}
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: unvisitedWaypoint.ID, FCStop: &flagTrue}); err == nil {
		testingContext.Fatalf("expected manual carrier route to reject adding FCStop before next pending stop")
	}
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: routedStopWaypoint.ID, FCStop: &flagFalse}); err != nil {
		testingContext.Fatalf("expected manual carrier route to allow clearing FCStop on active manual target, got %v", err)
	}

	if _, clearOriginVisitError := databaseConnection.Exec(`UPDATE expedition_rows SET visited_at = NULL WHERE id = ?`, originWaypoint.ID); clearOriginVisitError != nil {
		testingContext.Fatal(clearOriginVisitError)
	}
	if _, err := expedition.UpdateRowTx(databaseConnection, expedition.RowPatch{ID: originWaypoint.ID, SystemName: &newSystem}); err == nil {
		testingContext.Fatalf("expected active expedition to reject SystemName edit on unvisited origin waypoint #1")
	}
}

func TestInsertRegionRowAutoFillsSuggestedRegion(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)
	coloniaRegion, ok := reference.RegionAt(-9530, -910, 19808)
	if !ok || coloniaRegion.ID == reference.InnerOrionSpurID {
		t.Fatalf("expected Colonia coordinates to resolve outside Inner Orion Spur, got %+v ok=%v", coloniaRegion, ok)
	}

	expeditionRecord := mustCreateWithWaypoints(
		t,
		dbConn,
		"Suggested Region Insert",
		"Verify region header auto-fill on insert",
		expedition.SeedWaypoint{SystemName: "Sol", HasCoords: true, X: 0, Y: 0, Z: 0, CoordsSource: "edsm"},
		expedition.SeedWaypoint{SystemName: "Unresolved Stop"},
		expedition.SeedWaypoint{SystemName: "Colonia", HasCoords: true, X: -9530, Y: -910, Z: 19808, CoordsSource: "edsm"},
	)
	initialRows, listErr := expedition.ListRows(dbConn, expeditionRecord.ID)
	if listErr != nil || len(initialRows) != 3 {
		t.Fatalf("ListRows = %d err=%v", len(initialRows), listErr)
	}
	solRow, unresolvedRow, coloniaRow := initialRows[0], initialRows[1], initialRows[2]

	// Inserting before an unresolved waypoint skips it and auto-fills from Colonia.
	beforeUnresolved := mustInsert(t, dbConn, expeditionRecord.ID, solRow.ID, expedition.RowRegion, 0)
	if beforeUnresolved.RegionID != coloniaRegion.ID {
		t.Fatalf("expected auto-filled region %d (%s), got %d", coloniaRegion.ID, coloniaRegion.Name, beforeUnresolved.RegionID)
	}

	// Inserting directly before Colonia also auto-fills Colonia's region.
	beforeColonia := mustInsert(t, dbConn, expeditionRecord.ID, unresolvedRow.ID, expedition.RowRegion, 0)
	if beforeColonia.RegionID != coloniaRegion.ID {
		t.Fatalf("expected auto-filled region %d before Colonia, got %d", coloniaRegion.ID, beforeColonia.RegionID)
	}

	// Appending at the end has no downstream waypoint, so it falls back to Inner Orion Spur.
	atEnd := mustInsert(t, dbConn, expeditionRecord.ID, expedition.InsertAtEnd, expedition.RowRegion, 0)
	if atEnd.RegionID != reference.InnerOrionSpurID {
		t.Fatalf("expected fallback region %d at end, got %d", reference.InnerOrionSpurID, atEnd.RegionID)
	}

	// Passing an explicit valid region ID overrides the downstream suggestion.
	explicitHeader := mustInsert(t, dbConn, expeditionRecord.ID, coloniaRow.ID, expedition.RowRegion, reference.InnerOrionSpurID)
	if explicitHeader.RegionID != reference.InnerOrionSpurID {
		t.Fatalf("expected explicit region %d to be preserved, got %d", reference.InnerOrionSpurID, explicitHeader.RegionID)
	}
}
