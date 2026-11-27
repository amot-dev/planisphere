package expedition_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestZeroDataLossAndSequentialProgressionWithBoundedMove(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)
	_, proj := expeditiontest.NewProjections(dbConn)
	lines := []string{
		// Pre-expedition visit to Waypoint #3 (must be ignored because it is before expedition_start_timestamp).
		`{"timestamp":"2026-01-10T10:00:00Z","event":"FSDJump","StarSystem":"Outotz LS-K d8-3","SystemAddress":1003,"StarPos":[100,0,100],"JumpDist":45.0}`,
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"Test","FID":"F1","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		// Arrival at Waypoint #1 (expedition start).
		`{"timestamp":"2026-01-18T19:42:10Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		// Leg 1 -> 2: jump to Colonia Bridge stop, neutron boost, scan + map an undiscovered ELW, analyse Stratum Tectonicas.
		`{"timestamp":"2026-01-18T20:10:00Z","event":"FSDJump","StarSystem":"Dryooe Prou FF-Z d696","SystemAddress":1002,"StarPos":[0,0,200],"JumpDist":200.0,"BoostUsed":4}`,
		`{"timestamp":"2026-01-18T20:15:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Dryooe Prou FF-Z d696 A 1","BodyID":11,"StarSystem":"Dryooe Prou FF-Z d696","SystemAddress":1002,"PlanetClass":"Earthlike body","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-01-18T20:18:00Z","event":"SAAScanComplete","BodyName":"Dryooe Prou FF-Z d696 A 1","SystemAddress":1002,"BodyID":11,"ProbesUsed":5,"EfficiencyTarget":6}`,
		`{"timestamp":"2026-01-18T20:25:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Stratum_Genus_Name;","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1002,"Body":11}`,
		// Ship switch during the next leg, and jump directly to Waypoint #5 ("Beagle Point"), skipping #3 and #4!
		`{"timestamp":"2026-01-18T21:00:00Z","event":"ShipyardSwap","ShipType":"explorer_nx","ShipType_Localised":"Caspian Explorer","ShipID":2,"StoreOldShip":"mandalay","StoreShipID":1}`,
		`{"timestamp":"2026-01-18T21:30:00Z","event":"FSDJump","StarSystem":"Beagle Point","SystemAddress":1005,"StarPos":[0,0,65000],"JumpDist":60.5,"BoostUsed":3}`,
	}
	journalDirectory := t.TempDir()
	journalFile := filepath.Join(journalDirectory, "Journal.2026-01-18T190000.01.log")
	if writeError := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		t.Fatal(writeError)
	}
	if scanResult, scanError := journal.NewIngester(dbConn).ScanDir(context.Background(), journalDirectory); scanError != nil || scanResult.NewEvents != len(lines) {
		t.Fatalf("ScanDir: events=%d want=%d err=%v", scanResult.NewEvents, len(lines), scanError)
	}

	// Create an expedition with 5 waypoints + a region header.
	expeditionRecord, originRow := createExpeditionWithOriginRow(t, dbConn, "Distant Worlds III", "Deep space survey")
	setRowSystemName(t, dbConn, originRow.ID, "Sol")

	waypoint2 := mustInsert(t, dbConn, expeditionRecord.ID, originRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, waypoint2.ID, "Dryooe Prou FF-Z d696")
	regionHeader := mustInsert(t, dbConn, expeditionRecord.ID, waypoint2.ID, expedition.RowRegion, 1)
	waypoint3 := mustInsert(t, dbConn, expeditionRecord.ID, regionHeader.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, waypoint3.ID, "Outotz LS-K d8-3")
	waypoint4 := mustInsert(t, dbConn, expeditionRecord.ID, waypoint3.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, waypoint4.ID, "Sagittarius A*")
	waypoint5 := mustInsert(t, dbConn, expeditionRecord.ID, waypoint4.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, waypoint5.ID, "Beagle Point")

	// Start expedition at 2026-01-18T19:42:10Z.
	if startTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-01-18T19:42:10Z", 0)
	}); startTxError != nil {
		t.Fatal(startTxError)
	}

	// Without automatic out-of-order reordering, Beagle Point remains at #5 (future) while #3 is current.
	initialPlan, initialPlanErr := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if initialPlanErr != nil {
		t.Fatal(initialPlanErr)
	}
	if initialPlan.VisitedWaypointCount != 2 {
		t.Fatalf("initial visited count = %d, want 2", initialPlan.VisitedWaypointCount)
	}
	beaglePreviews, beaglePreviewErr := expedition.ComputeRowDropPreviews(dbConn, waypoint5.ID, proj)
	if beaglePreviewErr != nil {
		t.Fatal(beaglePreviewErr)
	}
	if len(beaglePreviews) != 6 || beaglePreviews[2].Status != expedition.DropSlotValidVisited {
		t.Fatalf("expected Beagle Point drop preview at index 2 to be valid_visited, got %+v", beaglePreviews)
	}

	// Manually reorder Beagle Point to index 2 (immediately after Dryooe Prou FF-Z d696).
	if moveTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if _, moveErr := expedition.MoveRow(transaction, waypoint5.ID, 2); moveErr != nil {
			return moveErr
		}
		_, reevalErr := expedition.Reevaluate(transaction, expeditionRecord.ID)
		return reevalErr
	}); moveTxError != nil {
		t.Fatal(moveTxError)
	}

	plan, planError := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if planError != nil {
		t.Fatal(planError)
	}
	// Expected waypoint sequence after user reorders Beagle Point to #3:
	// #1 Sol (visited, departed) -> #2 Dryooe Prou FF-Z d696 (visited, departed) -> #3 Beagle Point (visited, still in system) -> #4 Outotz LS-K d8-3 (future until departing #3) -> #5 Sagittarius A* (future).
	var waypointSystems []string
	var waypointNumbers []int
	var waypointStates []string
	for _, planRow := range plan.Rows {
		if planRow.Row.RowType == expedition.RowWaypoint {
			waypointSystems = append(waypointSystems, planRow.Row.SystemName)
			waypointNumbers = append(waypointNumbers, planRow.Number)
			waypointStates = append(waypointStates, planRow.VisitState)
		}
	}
	wantSystems := []string{"Sol", "Dryooe Prou FF-Z d696", "Beagle Point", "Outotz LS-K d8-3", "Sagittarius A*"}
	if strings.Join(waypointSystems, "|") != strings.Join(wantSystems, "|") {
		t.Fatalf("waypoint order = %v, want %v", waypointSystems, wantSystems)
	}
	for i, number := range waypointNumbers {
		if number != i+1 {
			t.Fatalf("waypoint numbering = %v", waypointNumbers)
		}
	}
	wantStates := []string{expedition.VisitVisited, expedition.VisitVisited, expedition.VisitVisited, expedition.VisitFuture, expedition.VisitFuture}
	if strings.Join(waypointStates, "|") != strings.Join(wantStates, "|") {
		t.Fatalf("visit states = %v, want %v", waypointStates, wantStates)
	}

	// Verify in-system exploration of WP #2 (Dryooe Prou FF-Z d696) lands in WP #2's drawer rather than WP #3's drawer.
	dryooeView := plan.Rows[1]
	dryooeDetails, dryooeDetailsError := expedition.Details(dbConn, dryooeView.Row.ID, proj)
	if dryooeDetailsError != nil {
		t.Fatalf("Details for Dryooe Prou failed: %v", dryooeDetailsError)
	}
	if dryooeDetails.Metrics.BodiesScanned != 1 || dryooeDetails.Metrics.BodiesMapped != 1 || dryooeDetails.Metrics.OrganicSamples != 1 {
		t.Fatalf("expected Dryooe Prou in-system exploration in WP #2 drawer, got %+v", dryooeDetails.Metrics)
	}

	// Verify vessel flown on leg details between #2 and #3, and that Beagle Point is currently in-system.
	beagleView := plan.Rows[2]
	beagleDetails, beagleDetailsError := expedition.Details(dbConn, beagleView.Row.ID, proj)
	if beagleDetailsError != nil {
		t.Fatalf("Details for Beagle Point failed: %v", beagleDetailsError)
	}
	if !beagleDetails.InSystem || beagleDetails.Metrics.BodiesScanned != 0 {
		t.Fatalf("expected Beagle Point in-system with 0 Dryooe scans, got inSystem=%v metrics=%+v", beagleDetails.InSystem, beagleDetails.Metrics)
	}
	if len(beagleDetails.ShipsUsed) != 1 || beagleDetails.ShipsUsed[0] != "Caspian Explorer" {
		t.Fatalf("expected Caspian Explorer flown on Beagle Point leg, got %+v", beagleDetails.ShipsUsed)
	}
}

func TestBuildPlanShipTimelineClampingAndCaching(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)
	_, proj := expeditiontest.NewProjections(dbConn)

	lines := []string{
		// Historical ship swaps before expedition
		`{"timestamp":"2026-01-01T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"anaconda","Ship_Localised":"Anaconda","ShipID":1}`,
		// On-foot suit load game event (should be ignored by ship filter)
		`{"timestamp":"2026-01-10T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		// Latest ship before expedition start
		`{"timestamp":"2026-01-15T12:00:00Z","event":"ShipyardSwap","Commander":"Cmdr Test","FID":"F1","ShipType":"diamondbackxl","ShipType_Localised":"Diamondback Explorer","ShipID":10}`,
		// Consecutive on-foot suit and Nomad LoadGame events between ship swap and expedition start
		`{"timestamp":"2026-01-16T01:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T09:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"lander01","Ship_Localised":"Nomad","ShipID":41}`,
		// Expedition start and arrival at Sol
		`{"timestamp":"2026-01-18T10:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":25.0}`,
		// Mid-expedition ship swap and ship-launched Nomad relog (Nomad must be ignored by ship timeline)
		`{"timestamp":"2026-01-19T14:00:00Z","event":"ShipyardSwap","Commander":"Cmdr Test","FID":"F1","ShipType":"krait_phantom","ShipType_Localised":"Krait Phantom","ShipID":20}`,
		`{"timestamp":"2026-01-20T12:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"lander01","Ship_Localised":"Nomad","ShipID":41}`,
		// Arrival at Alpha Centauri
		`{"timestamp":"2026-01-20T16:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`,
		// Post-expedition ship swap (should be clamped out)
		`{"timestamp":"2026-01-25T12:00:00Z","event":"ShipyardSwap","Commander":"Cmdr Test","FID":"F1","ShipType":"empire_trader","ShipType_Localised":"Imperial Cutter","ShipID":30}`,
	}
	journaltest.IngestLines(t, dbConn, lines...)

	exp, originRow := createExpeditionWithOriginRow(t, dbConn, "Ship Clamping Test", "")
	setRowSystemName(t, dbConn, originRow.ID, "Sol")
	alpha := mustInsert(t, dbConn, exp.ID, originRow.ID, expedition.RowWaypoint, 0)
	setRowSystemName(t, dbConn, alpha.ID, "Alpha Centauri")

	// Start at 2026-01-18T10:00:00Z, complete at final visited waypoint (2026-01-20T16:00:00Z)
	if startErr := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, exp.ID, "2026-01-18T10:00:00Z", 0)
	}); startErr != nil {
		t.Fatalf("Start failed: %v", startErr)
	}
	if reevalErr := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		_, err := expedition.Reevaluate(transaction, exp.ID)
		return err
	}); reevalErr != nil {
		t.Fatalf("Reevaluate failed: %v", reevalErr)
	}
	if completeErr := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Complete(transaction, exp.ID)
	}); completeErr != nil {
		t.Fatalf("Complete failed: %v", completeErr)
	}

	if _, err := expedition.BuildPlan(dbConn, exp.ID, nil, proj); err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify vessels flown on leg details arriving at Alpha Centauri.
	alphaDetails, alphaDetailsError := expedition.Details(dbConn, alpha.ID, proj)
	if alphaDetailsError != nil {
		t.Fatalf("Details for Alpha Centauri failed: %v", alphaDetailsError)
	}
	if len(alphaDetails.ShipsUsed) == 0 {
		t.Fatalf("expected ships flown arriving at Alpha Centauri, got none")
	}
	foundPhantom := false
	for _, hullName := range alphaDetails.ShipsUsed {
		if hullName == "Krait Phantom" {
			foundPhantom = true
		}
		if hullName == "Diamondback Explorer" {
			t.Errorf("Diamondback Explorer should have been filtered out (no jump made before swap)")
		}
		if hullName == "Imperial Cutter" {
			t.Errorf("Imperial Cutter should have been clamped out (occurred after expedition end)")
		}
		if hullName == "Nomad" {
			t.Errorf("Nomad ship-launched lander should have been filtered out of ships flown")
		}
	}
	if !foundPhantom {
		t.Errorf("expected Krait Phantom flown, got: %+v", alphaDetails.ShipsUsed)
	}
}

func TestBuildPlanNextWaypointPlayerDistanceAndRemainingLy(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)
	_, proj := expeditiontest.NewProjections(dbConn)

	expeditionRecord, firstWaypoint := createExpeditionWithOriginRow(t, dbConn, "Route Telemetry Test", "")
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = 'Sol', x = 0, y = 0, z = 0 WHERE id = ?`, firstWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	secondWaypoint := mustInsert(t, dbConn, expeditionRecord.ID, firstWaypoint.ID, expedition.RowWaypoint, 0)
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = 'Alpha Centauri', x = 100, y = 0, z = 0 WHERE id = ?`, secondWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	thirdWaypoint := mustInsert(t, dbConn, expeditionRecord.ID, secondWaypoint.ID, expedition.RowWaypoint, 0)
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = 'Barnard''s Star', x = 250, y = 0, z = 0 WHERE id = ?`, thirdWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	// In planning status:
	// PlannedLy = 100 + 150 = 250 Ly.
	// RemainingLy = 250 Ly.
	// No row is next waypoint.
	planPlanning, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if err != nil {
		t.Fatal(err)
	}
	if planPlanning.PlannedLy != 250.0 {
		t.Errorf("expected planned ly 250.0, got %v", planPlanning.PlannedLy)
	}
	if planPlanning.CoveredLy != 0.0 {
		t.Errorf("expected covered ly 0.0 in planning, got %v", planPlanning.CoveredLy)
	}
	if planPlanning.RemainingLy != 250.0 {
		t.Errorf("expected remaining ly 250.0 in planning, got %v", planPlanning.RemainingLy)
	}
	for _, waypointRowView := range planPlanning.Rows {
		if waypointRowView.IsNextWaypoint {
			t.Errorf("expected no next waypoint in planning status, row %d had IsNextWaypoint=true", waypointRowView.Number)
		}
	}

	// Start expedition at firstWaypoint:
	// Visit firstWaypoint at 2026-03-01T10:00:00Z.
	if transactionError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-03-01T10:00:00Z", 0)
	}); transactionError != nil {
		t.Fatal(transactionError)
	}
	visitedAt := "2026-03-01T10:00:00Z"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET visited_at = ? WHERE id = ?`, visitedAt, firstWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	// Player is initially at Sol (0, 0, 0)
	journaltest.InsertParsedLines(t, dbConn,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Location","StarSystem":"Sol","StarPos":[0,0,0]}`,
	)

	planAtOrigin, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if err != nil {
		t.Fatal(err)
	}
	// Before departing origin WP #1, NextWaypointRowID stays 0 and WP #2 stays future while RemainingLy is still 250.
	if planAtOrigin.NextWaypointRowID != 0 {
		t.Errorf("expected NextWaypointRowID 0 before departing origin, got %d", planAtOrigin.NextWaypointRowID)
	}
	if planAtOrigin.RemainingLy != 250.0 {
		t.Errorf("expected remaining ly 250.0, got %v", planAtOrigin.RemainingLy)
	}

	var secondWaypointRow, thirdWaypointRow expedition.RowView
	for _, waypointRowView := range planAtOrigin.Rows {
		if waypointRowView.Row.ID == secondWaypoint.ID {
			secondWaypointRow = waypointRowView
		}
		if waypointRowView.Row.ID == thirdWaypoint.ID {
			thirdWaypointRow = waypointRowView
		}
	}
	if secondWaypointRow.IsNextWaypoint || secondWaypointRow.VisitState != expedition.VisitFuture {
		t.Errorf("expected secondWaypoint to stay future before leaving origin, got state=%q next=%v", secondWaypointRow.VisitState, secondWaypointRow.IsNextWaypoint)
	}
	if secondWaypointRow.DistFromPrevLy == nil || *secondWaypointRow.DistFromPrevLy != 100.0 {
		t.Errorf("expected secondWaypoint dist from prev 100.0, got %v", secondWaypointRow.DistFromPrevLy)
	}
	if thirdWaypointRow.IsNextWaypoint {
		t.Errorf("expected thirdWaypoint NOT to be next waypoint")
	}
	if thirdWaypointRow.DistFromPrevLy == nil || *thirdWaypointRow.DistFromPrevLy != 150.0 {
		t.Errorf("expected thirdWaypoint dist from prev 150.0, got %v", thirdWaypointRow.DistFromPrevLy)
	}

	// Player departs Sol and jumps closer to secondWaypoint: "Interim System" at (60, 0, 0).
	journaltest.InsertParsedLines(t, dbConn,
		`{"timestamp":"2026-03-01T11:00:00Z","event":"FSDJump","StarSystem":"Interim System","StarPos":[60,0,0],"JumpDist":60.0}`,
	)
	if _, reevalError := expedition.ReevaluateTx(dbConn, expeditionRecord.ID); reevalError != nil {
		t.Fatalf("ReevaluateTx after origin departure: %v", reevalError)
	}

	planAfterFirstJump, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if err != nil {
		t.Fatal(err)
	}
	if planAfterFirstJump.NextWaypointRowID != secondWaypoint.ID {
		t.Errorf("expected NextWaypointRowID %d after departing origin, got %d", secondWaypoint.ID, planAfterFirstJump.NextWaypointRowID)
	}
	// Distance from player (60,0,0) to secondWaypoint (100,0,0) is 40.0 Ly.
	// secondWaypoint to thirdWaypoint is 150.0 Ly.
	// RemainingLy = 40.0 + 150.0 = 190.0 Ly!
	// PlannedLy stays 250.0 Ly!
	if planAfterFirstJump.PlannedLy != 250.0 {
		t.Errorf("expected planned ly to remain 250.0, got %v", planAfterFirstJump.PlannedLy)
	}
	if planAfterFirstJump.RemainingLy != 190.0 {
		t.Errorf("expected remaining ly 190.0 after jump, got %v", planAfterFirstJump.RemainingLy)
	}
	for _, waypointRowView := range planAfterFirstJump.Rows {
		if waypointRowView.Row.ID == secondWaypoint.ID {
			if waypointRowView.DistFromCurrentLy == nil || *waypointRowView.DistFromCurrentLy != 40.0 {
				t.Errorf("expected secondWaypoint dist from player 40.0, got %v", waypointRowView.DistFromCurrentLy)
			}
			if waypointRowView.DistFromPrevLy == nil || *waypointRowView.DistFromPrevLy != 100.0 {
				t.Errorf("expected secondWaypoint dist from prev (planned leg) to remain 100.0, got %v", waypointRowView.DistFromPrevLy)
			}
		}
	}
	if planAfterFirstJump.CoveredLy != 0.0 {
		t.Errorf("expected covered ly 0.0 before secondWaypoint visit, got %v", planAfterFirstJump.CoveredLy)
	}

	// Visit secondWaypoint
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET visited_at = '2026-03-01T12:00:00Z' WHERE id = ?`, secondWaypoint.ID); err != nil {
		t.Fatal(err)
	}
	planAfterSecondWaypointVisit, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if err != nil {
		t.Fatal(err)
	}
	if planAfterSecondWaypointVisit.CoveredLy != 100.0 {
		t.Errorf("expected covered ly 100.0 after secondWaypoint visit, got %v", planAfterSecondWaypointVisit.CoveredLy)
	}

	// Visit thirdWaypoint
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET visited_at = '2026-03-01T13:00:00Z' WHERE id = ?`, thirdWaypoint.ID); err != nil {
		t.Fatal(err)
	}
	planAfterFinalWaypointVisit, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil, proj)
	if err != nil {
		t.Fatal(err)
	}
	if planAfterFinalWaypointVisit.CoveredLy != 250.0 {
		t.Errorf("expected covered ly 250.0 after thirdWaypoint visit, got %v", planAfterFinalWaypointVisit.CoveredLy)
	}
}

func TestBuildPlanVisitCountAndEDSMToJournalUpgrade(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	eventBus, proj := expeditiontest.NewProjections(databaseConnection)

	expeditionRecord, originWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Visit Count Plan", "")
	setRowSystemName(testingContext, databaseConnection, originWaypoint.ID, "Sol")
	if setCoordsError := expedition.SetRowCoords(databaseConnection, originWaypoint.ID, "Sol", 10477373803, 27, &reference.Vec3{X: 0, Y: 0, Z: 0}, "edsm"); setCoordsError != nil {
		testingContext.Fatal(setCoordsError)
	}

	initialPlan, initialPlanError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, proj)
	if initialPlanError != nil {
		testingContext.Fatal(initialPlanError)
	}
	if initialPlan.Rows[0].Row.CoordsSource != "edsm" || initialPlan.Rows[0].VisitCount != 0 {
		testingContext.Fatalf("expected initial EDSM source with 0 visits, got source=%q visits=%d", initialPlan.Rows[0].Row.CoordsSource, initialPlan.Rows[0].VisitCount)
	}

	scanResult := journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay"}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-01-18T19:40:00Z","event":"CarrierJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
	)
	eventBus.PublishJournal(scanResult.Events)

	updatedPlan, updatedPlanError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, proj)
	if updatedPlanError != nil {
		testingContext.Fatal(updatedPlanError)
	}
	if updatedPlan.Rows[0].Row.CoordsSource != "journal" || updatedPlan.Rows[0].VisitCount != 2 {
		testingContext.Fatalf("expected upgraded journal source with 2 visits (FSDJump + CarrierJump, ignoring Location relogs), got source=%q visits=%d", updatedPlan.Rows[0].Row.CoordsSource, updatedPlan.Rows[0].VisitCount)
	}
}

func TestDisconnectedJumpLegTransitionsAndRepeatVisitCount(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, proj := expeditiontest.NewProjections(databaseConnection)

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-11-17T01:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX","Ship_Localised":"Caspian Explorer","ShipID":36}`,
		`{"timestamp":"2026-11-17T01:01:00Z","event":"FSDJump","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321,"StarPos":[-9962.1875,-1.3125,-7604.875],"JumpDist":75.0}`,
		`{"timestamp":"2026-11-17T01:10:00Z","event":"FSDJump","StarSystem":"Hypo Aeb ST-B b54-0","SystemAddress":538078878161,"StarPos":[-9894.84375,-5.5625,-7569.25],"JumpDist":76.305}`,
		`{"timestamp":"2026-11-17T01:40:45Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321}`,
		`{"timestamp":"2026-11-17T01:41:42Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX","Ship_Localised":"Caspian Explorer","ShipID":36}`,
		`{"timestamp":"2026-11-17T01:41:53Z","event":"Location","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321,"StarPos":[-9962.1875,-1.3125,-7604.875],"Docked":false}`,
	)

	expeditionRecord, originWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Disconnect Leg Test", "")
	setRowSystemName(testingContext, databaseConnection, originWaypoint.ID, "Hypo Aeb ST-B b54-0")
	if originCoordsError := expedition.SetRowCoords(databaseConnection, originWaypoint.ID, "Hypo Aeb ST-B b54-0", 538078878161, 0, &reference.Vec3{X: -9894.84375, Y: -5.5625, Z: -7569.25}, "journal"); originCoordsError != nil {
		testingContext.Fatal(originCoordsError)
	}

	destinationWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, originWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, destinationWaypoint.ID, "Hypo Aeb JS-D b53-0")
	if destCoordsError := expedition.SetRowCoords(databaseConnection, destinationWaypoint.ID, "Hypo Aeb JS-D b53-0", 537273637321, 0, &reference.Vec3{X: -9962.1875, Y: -1.3125, Z: -7604.875}, "journal"); destCoordsError != nil {
		testingContext.Fatal(destCoordsError)
	}

	if startError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-11-17T01:10:00Z", 0)
	}); startError != nil {
		testingContext.Fatal(startError)
	}

	planView, planError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, proj)
	if planError != nil {
		testingContext.Fatal(planError)
	}
	destView := planView.Rows[1]
	if destView.VisitState != expedition.VisitVisited {
		testingContext.Fatalf("expected destination waypoint to be visited via Location, got %q", destView.VisitState)
	}
	destDetails, destDetailsError := expedition.Details(databaseConnection, destinationWaypoint.ID, proj)
	if destDetailsError != nil {
		testingContext.Fatalf("Details for destination waypoint failed: %v", destDetailsError)
	}
	if len(destDetails.ShipsUsed) != 1 || destDetails.ShipsUsed[0] != "Caspian Explorer" {
		testingContext.Fatalf("expected single disconnected-jump leg to record Caspian Explorer flown, got %+v", destDetails.ShipsUsed)
	}
	if destView.VisitCount != 2 {
		testingContext.Fatalf("expected VisitCount=2 (1 FSDJump + 1 recovered disconnect jump), got %d", destView.VisitCount)
	}
}
