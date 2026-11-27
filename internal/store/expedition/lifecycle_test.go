package expedition_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestPruneEmptyRowsOnComplete(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)
	expeditionRecord, originRow := createExpeditionWithOriginRow(t, dbConn, "Prune Test", "Test pruning empty rows on complete")
	setRowSystemName(t, dbConn, originRow.ID, "Sol")

	// Header 1: Inner Orion Spur (followed by non-empty waypoints)
	header1 := mustInsert(t, dbConn, expeditionRecord.ID, originRow.ID, expedition.RowRegion, 1)
	waypoint2 := mustInsert(t, dbConn, expeditionRecord.ID, header1.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, waypoint2.ID, "Alpha Centauri")

	// Header 2: Elysian Shore (followed only by empty waypoints)
	header2 := mustInsert(t, dbConn, expeditionRecord.ID, waypoint2.ID, expedition.RowRegion, 2)
	emptyWaypoint1 := mustInsert(t, dbConn, expeditionRecord.ID, header2.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, emptyWaypoint1.ID, "")
	emptyWaypoint2 := mustInsert(t, dbConn, expeditionRecord.ID, emptyWaypoint1.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, emptyWaypoint2.ID, "   ")

	// Header 3: Empty header at end (no waypoints at all)
	_ = mustInsert(t, dbConn, expeditionRecord.ID, emptyWaypoint2.ID, expedition.RowRegion, 3)

	journaltest.IngestLines(t, dbConn,
		`{"timestamp":"2026-01-18T19:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-01-18T20:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`,
	)

	// Start expedition
	if startTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-01-18T19:00:00Z", 0)
	}); startTxError != nil {
		t.Fatal(startTxError)
	}

	// Complete expedition
	if completeTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Complete(transaction, expeditionRecord.ID)
	}); completeTxError != nil {
		t.Fatal(completeTxError)
	}

	survivingRows, survivingError := expedition.ListRows(dbConn, expeditionRecord.ID)
	if survivingError != nil {
		t.Fatal(survivingError)
	}

	// Surviving should be:
	// - Sol (waypoint)
	// - Region 1 (header1)
	// - Alpha Centauri (waypoint)
	// All empty waypoints and headers 2 and 3 must be deleted.
	if len(survivingRows) != 3 {
		t.Fatalf("expected 3 surviving rows, got %d: %+v", len(survivingRows), survivingRows)
	}
	if survivingRows[0].SystemName != "Sol" || survivingRows[0].Position != 1 {
		t.Errorf("row 0 mismatch: %+v", survivingRows[0])
	}
	if survivingRows[1].RowType != expedition.RowRegion || survivingRows[1].RegionID != 1 || survivingRows[1].Position != 2 {
		t.Errorf("row 1 mismatch: %+v", survivingRows[1])
	}
	if survivingRows[2].SystemName != "Alpha Centauri" || survivingRows[2].Position != 3 {
		t.Errorf("row 2 mismatch: %+v", survivingRows[2])
	}
}

func TestCompleteRejectsActiveCarrierRouteAndDeleteClearsRoute(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	expeditionRecord, originRow := createExpeditionWithOriginRow(testingContext, databaseConnection, "Carrier Route Lifecycle Guard", "")
	setRowSystemName(testingContext, databaseConnection, originRow.ID, "Sol")

	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2020-01-18T10:00:00Z", 0); startError != nil {
		testingContext.Fatal(startError)
	}
	routeState := expedition.ActiveCarrierRouteState{
		ExpeditionID: expeditionRecord.ID,
		ManualMode:   true,
		Route:        expedition.CarrierRouteResult{Origin: "Sol", ManualMode: true},
	}
	if saveRouteError := expedition.SaveActiveCarrierRoute(databaseConnection, routeState); saveRouteError != nil {
		testingContext.Fatal(saveRouteError)
	}

	completeError := expedition.CompleteTx(databaseConnection, expeditionRecord.ID)
	if completeError == nil || completeError.Error() != "close or finish the active fleet carrier route before completing the expedition" {
		testingContext.Fatalf("expected CompleteTx to reject active carrier route, got %v", completeError)
	}
	reloadedExpedition, reloadError := expedition.Get(databaseConnection, expeditionRecord.ID)
	if reloadError != nil {
		testingContext.Fatal(reloadError)
	}
	if reloadedExpedition.Status != expedition.StatusActive {
		testingContext.Fatalf("expected expedition status to remain active, got %q", reloadedExpedition.Status)
	}

	if deleteError := expedition.Delete(databaseConnection, expeditionRecord.ID); deleteError != nil {
		testingContext.Fatal(deleteError)
	}
	persistedRoute, getSettingError := settings.Get(databaseConnection, fmt.Sprintf("active_carrier_route_%d", expeditionRecord.ID))
	if getSettingError != nil {
		testingContext.Fatal(getSettingError)
	}
	if persistedRoute != "" {
		testingContext.Fatalf("expected active carrier route setting to be cleared on Delete, got %q", persistedRoute)
	}
}

func TestDepartureBoundariesCompletionChainedAndMultiCommander(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, expeditionProjections := expeditiontest.NewProjections(databaseConnection)

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-09-01T09:00:00Z","event":"LoadGame","Commander":"CmdrA","FID":"FA","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-09-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":5001,"StarPos":[0,0,0]}`,
		// Pre-departure scan inside WP #1 (must remain in Historical Baseline once WP #1 departs).
		`{"timestamp":"2026-09-01T10:10:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Sol A 1","BodyID":1,"StarSystem":"Sol","SystemAddress":5001,"PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		// Telepresence multicrew arrival elsewhere must be ignored completely.
		`{"timestamp":"2026-09-01T10:12:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":9999,"StarPos":[9000,0,9000],"Multicrew":true}`,
	)

	expeditionRecord, createError := expedition.CreateWithWaypoints(databaseConnection, "Departure Flow", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Alpha"},
		{SystemName: "Beta"},
	})
	if createError != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", createError)
	}
	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-09-01T10:00:00Z", 0); startError != nil {
		testingContext.Fatalf("StartTx: %v", startError)
	}
	if completeEarlyError := expedition.CompleteTx(databaseConnection, expeditionRecord.ID); completeEarlyError == nil || completeEarlyError.Error() != "at least 2 visited waypoints are required to complete an expedition" {
		testingContext.Fatalf("expected CompleteTx to require at least 2 visited waypoints, got %v", completeEarlyError)
	}

	rowsBeforeDepart, listBeforeErr := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listBeforeErr != nil {
		testingContext.Fatalf("ListRows before depart: %v", listBeforeErr)
	}
	if rowsBeforeDepart[0].DepartedAt != "" {
		testingContext.Fatalf("expected WP #1 DepartedAt empty before leaving Sol (multicrew ignored), got %q", rowsBeforeDepart[0].DepartedAt)
	}
	planBeforeDepart, planBeforeErr := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, expeditionProjections)
	if planBeforeErr != nil {
		testingContext.Fatalf("BuildPlan before depart: %v", planBeforeErr)
	}
	if planBeforeDepart.NextWaypointRowID != 0 || planBeforeDepart.Rows[1].VisitState != expedition.VisitFuture {
		testingContext.Fatalf("expected WP #2 to remain future before leaving WP #1, got next=%d state=%q", planBeforeDepart.NextWaypointRowID, planBeforeDepart.Rows[1].VisitState)
	}
	wp1PreDetails, wp1PreErr := expedition.Details(databaseConnection, rowsBeforeDepart[0].ID, expeditionProjections)
	if wp1PreErr != nil {
		testingContext.Fatalf("Details WP #1 pre-depart: %v", wp1PreErr)
	}
	if !wp1PreDetails.InSystem || wp1PreDetails.Metrics.BodiesScanned != 1 {
		testingContext.Fatalf("expected WP #1 Historical Baseline InSystem=true with pre-departure scan, got inSystem=%v metrics=%+v", wp1PreDetails.InSystem, wp1PreDetails.Metrics)
	}
	wp2PreDetails, wp2PreErr := expedition.Details(databaseConnection, rowsBeforeDepart[1].ID, expeditionProjections)
	if wp2PreErr != nil {
		testingContext.Fatalf("Details WP #2 pre-depart: %v", wp2PreErr)
	}
	if wp2PreDetails.Kind != "" {
		testingContext.Fatalf("expected WP #2 drawer empty before WP #1 departs, got %+v", wp2PreDetails)
	}

	// Depart Sol directly to Alpha, scan Alpha while Commander B logs in elsewhere (must not depart CmdrA),
	// resurrect inside Alpha (must not depart), swap ships inside Alpha, then jump straight to Beta.
	// At Beta, scan a planet, scan a belt cluster later (must not extend end), depart Beta to SideHop,
	// return to Beta and scan again (must stay excluded from first-stay Complete).
	if _, execError := databaseConnection.Exec(`
		INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrA', 'FA', 1), ('CmdrB', 'FB', 1)
		ON CONFLICT(name) DO UPDATE SET enabled = 1`); execError != nil {
		testingContext.Fatalf("seed commanders: %v", execError)
	}
	if _, execError := databaseConnection.Exec(`
		INSERT INTO journal_events(event_uid, timestamp, event, system_name, system_address, body_id, body_name, star_x, star_y, star_z, commander, source_file, raw_json, ingested_at) VALUES
		('dep-alpha-arr', '2026-09-01T11:00:00Z', 'FSDJump', 'Alpha', 5002, NULL, '', 10, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":5002,"StarPos":[10,0,0],"JumpDist":10.0}', '2026-09-01T11:00:00Z'),
		('dep-cmdrb-loc', '2026-09-01T11:05:00Z', 'Location', 'OtherSys', 8001, NULL, '', 500, 0, 0, 'CmdrB', 't.log', '{"timestamp":"2026-09-01T11:05:00Z","event":"Location","StarSystem":"OtherSys","SystemAddress":8001,"StarPos":[500,0,0]}', '2026-09-01T11:05:00Z'),
		('dep-alpha-resurrect', '2026-09-01T11:08:00Z', 'Location', 'Alpha', 5002, NULL, '', 10, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T11:08:00Z","event":"Location","StarSystem":"Alpha","SystemAddress":5002,"StarPos":[10,0,0]}', '2026-09-01T11:08:00Z'),
		('dep-alpha-scan', '2026-09-01T11:10:00Z', 'Scan', 'Alpha', 5002, 1, 'Alpha 1', NULL, NULL, NULL, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T11:10:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Alpha 1","BodyID":1,"StarSystem":"Alpha","SystemAddress":5002,"PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}', '2026-09-01T11:10:00Z'),
		('dep-alpha-swap', '2026-09-01T11:15:00Z', 'ShipyardSwap', '', NULL, NULL, '', NULL, NULL, NULL, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T11:15:00Z","event":"ShipyardSwap","ShipType":"anaconda","ShipType_Localised":"Anaconda","ShipID":2}', '2026-09-01T11:15:00Z'),
		('dep-beta-arr', '2026-09-01T12:00:00Z', 'FSDJump', 'Beta', 5003, NULL, '', 30, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T12:00:00Z","event":"FSDJump","StarSystem":"Beta","SystemAddress":5003,"StarPos":[30,0,0],"JumpDist":20.0}', '2026-09-01T12:00:00Z'),
		('dep-beta-scan1', '2026-09-01T12:20:00Z', 'Scan', 'Beta', 5003, 2, 'Beta 2', NULL, NULL, NULL, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T12:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Beta 2","BodyID":2,"StarSystem":"Beta","SystemAddress":5003,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}', '2026-09-01T12:20:00Z'),
		('dep-beta-belt', '2026-09-01T12:25:00Z', 'Scan', 'Beta', 5003, 9, 'Beta A Belt Cluster 1', NULL, NULL, NULL, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T12:25:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Beta A Belt Cluster 1","BodyID":9,"StarSystem":"Beta","SystemAddress":5003}', '2026-09-01T12:25:00Z'),
		('dep-beta-sidehop', '2026-09-01T12:30:00Z', 'FSDJump', 'SideHop', 5004, NULL, '', 35, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T12:30:00Z","event":"FSDJump","StarSystem":"SideHop","SystemAddress":5004,"StarPos":[35,0,0],"JumpDist":5.0}', '2026-09-01T12:30:00Z'),
		('dep-beta-return', '2026-09-01T13:00:00Z', 'FSDJump', 'Beta', 5003, NULL, '', 30, 0, 0, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T13:00:00Z","event":"FSDJump","StarSystem":"Beta","SystemAddress":5003,"StarPos":[30,0,0],"JumpDist":5.0}', '2026-09-01T13:00:00Z'),
		('dep-beta-scan2', '2026-09-01T13:10:00Z', 'Scan', 'Beta', 5003, 3, 'Beta 3', NULL, NULL, NULL, 'CmdrA', 't.log', '{"timestamp":"2026-09-01T13:10:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Beta 3","BodyID":3,"StarSystem":"Beta","SystemAddress":5003,"PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}', '2026-09-01T13:10:00Z')
	`); execError != nil {
		testingContext.Fatalf("insert departure test events: %v", execError)
	}
	if _, reevalError := expedition.ReevaluateTx(databaseConnection, expeditionRecord.ID); reevalError != nil {
		testingContext.Fatalf("ReevaluateTx: %v", reevalError)
	}

	rowsAfter, listAfterErr := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listAfterErr != nil {
		testingContext.Fatalf("ListRows after depart: %v", listAfterErr)
	}
	if rowsAfter[0].DepartedAt != "2026-09-01T11:00:00Z" || rowsAfter[1].DepartedAt != "2026-09-01T12:00:00Z" || rowsAfter[2].DepartedAt != "2026-09-01T12:30:00Z" {
		testingContext.Fatalf("unexpected DepartedAt sequence: wp1=%q wp2=%q wp3=%q", rowsAfter[0].DepartedAt, rowsAfter[1].DepartedAt, rowsAfter[2].DepartedAt)
	}

	wp1Details, wp1Err := expedition.Details(databaseConnection, rowsAfter[0].ID, expeditionProjections)
	if wp1Err != nil {
		testingContext.Fatalf("Details WP #1: %v", wp1Err)
	}
	if wp1Details.To != "2026-09-01T11:00:00Z" || wp1Details.Metrics.BodiesScanned != 1 || wp1Details.Metrics.Jumps != 0 {
		testingContext.Fatalf("expected WP #1 baseline to keep Sol scan and exclude departure jump, got to=%q metrics=%+v", wp1Details.To, wp1Details.Metrics)
	}
	wp2Details, wp2Err := expedition.Details(databaseConnection, rowsAfter[1].ID, expeditionProjections)
	if wp2Err != nil {
		testingContext.Fatalf("Details WP #2: %v", wp2Err)
	}
	if wp2Details.Metrics.Jumps != 1 || wp2Details.Metrics.BodiesScanned != 1 || strings.Join(wp2Details.ShipsUsed, "|") != "Mandalay" {
		testingContext.Fatalf("expected WP #2 to include direct jump, Alpha scan (ignoring CmdrB), and Mandalay only, got metrics=%+v ships=%v", wp2Details.Metrics, wp2Details.ShipsUsed)
	}
	wp3ActiveDetails, wp3ActiveErr := expedition.Details(databaseConnection, rowsAfter[2].ID, expeditionProjections)
	if wp3ActiveErr != nil {
		testingContext.Fatalf("Details WP #3 active: %v", wp3ActiveErr)
	}
	if wp3ActiveDetails.Metrics.Jumps != 1 || wp3ActiveDetails.Metrics.BodiesScanned != 1 || strings.Join(wp3ActiveDetails.ShipsUsed, "|") != "Anaconda" {
		testingContext.Fatalf("expected WP #3 to include Beta jump, Beta 2 scan only (first stay), and Anaconda, got metrics=%+v ships=%v", wp3ActiveDetails.Metrics, wp3ActiveDetails.ShipsUsed)
	}

	// Complete locks end_timestamp to Beta 2 (12:20), ignoring belt cluster (12:25) and post-departure return scan (13:10).
	if completeError := expedition.CompleteTx(databaseConnection, expeditionRecord.ID); completeError != nil {
		testingContext.Fatalf("CompleteTx: %v", completeError)
	}
	completedExp := mustGetExpedition(testingContext, databaseConnection, expeditionRecord.ID)
	if completedExp.EndTimestamp != "2026-09-01T12:20:00Z" {
		testingContext.Fatalf("expected completed EndTimestamp=2026-09-01T12:20:00Z, got %q", completedExp.EndTimestamp)
	}
	wp3CompletedDetails, wp3CompletedErr := expedition.Details(databaseConnection, rowsAfter[2].ID, expeditionProjections)
	if wp3CompletedErr != nil {
		testingContext.Fatalf("Details WP #3 completed: %v", wp3CompletedErr)
	}
	if wp3CompletedDetails.To != "2026-09-01T12:20:00Z" || wp3CompletedDetails.Metrics.BodiesScanned != 1 || wp3CompletedDetails.InSystem {
		testingContext.Fatalf("expected completed WP #3 drawer clamped to 12:20 with 1 scan, got to=%q inSystem=%v metrics=%+v",
			wp3CompletedDetails.To, wp3CompletedDetails.InSystem, wp3CompletedDetails.Metrics)
	}

	// Chained expedition starting at Beta (12:00) begins at Beta departure (12:30), causing zero overlap with Beta 2 (12:20).
	chainedExp, createChainErr := expedition.CreateWithWaypoints(databaseConnection, "Chained Leg 2", "", []expedition.SeedWaypoint{
		{SystemName: "Beta"},
		{SystemName: "SideHop"},
	})
	if createChainErr != nil {
		testingContext.Fatalf("CreateWithWaypoints chained: %v", createChainErr)
	}
	if startChainErr := expedition.StartTx(databaseConnection, chainedExp.ID, "2026-09-01T12:00:00Z", 0); startChainErr != nil {
		testingContext.Fatalf("StartTx chained: %v", startChainErr)
	}
	chainQueryAfter, chainDisplayStart, chainDeparted, chainStartErr := expedition.EffectiveStart(databaseConnection, mustGetExpedition(testingContext, databaseConnection, chainedExp.ID))
	if chainStartErr != nil || !chainDeparted || chainDisplayStart != "2026-09-01T12:30:00Z" || chainQueryAfter != "2026-09-01T12:29:59Z" {
		testingContext.Fatalf("unexpected chained EffectiveStart: after=%q display=%q departed=%v err=%v", chainQueryAfter, chainDisplayStart, chainDeparted, chainStartErr)
	}
	chainSummary, chainComputeErr := expeditionProjections.TimeRangeStats.Compute(replaystats.Range{After: chainQueryAfter, Until: "2026-09-01T12:59:59Z"}, nil)
	if chainComputeErr != nil {
		testingContext.Fatalf("Compute chained summary: %v", chainComputeErr)
	}
	if chainSummary.Metrics.Jumps != 1 || chainSummary.Metrics.BodiesScanned != 0 {
		testingContext.Fatalf("expected chained expedition to exclude Exp 1 Beta scan and include SideHop departure jump, got %+v", chainSummary.Metrics)
	}

	// Reopening clears EndTimestamp, and adding visited SideHop advances completion to 12:30.
	if reopenError := expedition.ReopenTx(databaseConnection, expeditionRecord.ID); reopenError != nil {
		testingContext.Fatalf("ReopenTx: %v", reopenError)
	}
	reopenedExp := mustGetExpedition(testingContext, databaseConnection, expeditionRecord.ID)
	if reopenedExp.Status != expedition.StatusActive || reopenedExp.EndTimestamp != "" {
		testingContext.Fatalf("expected active status and empty EndTimestamp after ReopenTx, got status=%q end=%q", reopenedExp.Status, reopenedExp.EndTimestamp)
	}
	sideHopRow := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, rowsAfter[2].ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, sideHopRow.ID, "SideHop")
	if _, reevalReopenedErr := expedition.ReevaluateTx(databaseConnection, expeditionRecord.ID); reevalReopenedErr != nil {
		testingContext.Fatalf("ReevaluateTx after ReopenTx: %v", reevalReopenedErr)
	}
	if recompleteErr := expedition.CompleteTx(databaseConnection, expeditionRecord.ID); recompleteErr != nil {
		testingContext.Fatalf("CompleteTx after ReopenTx: %v", recompleteErr)
	}
	if finalExp := mustGetExpedition(testingContext, databaseConnection, expeditionRecord.ID); finalExp.EndTimestamp != "2026-09-01T12:30:00Z" {
		testingContext.Fatalf("expected final EndTimestamp=2026-09-01T12:30:00Z after ReopenTx, got %q", finalExp.EndTimestamp)
	}
}
