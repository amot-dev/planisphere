package expedition_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
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

func TestDetailsOriginRealizationCacheAndErrorPropagation(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	eventBus, proj := expeditiontest.NewProjections(databaseConnection)

	if _, execError := databaseConnection.Exec(`INSERT INTO commanders (name, enabled, first_seen) VALUES ('Jameson', 1, '2026-01-01T00:00:00Z')`); execError != nil {
		testingContext.Fatal(execError)
	}

	// Insert pre-expedition organic scan and sale so origin realization is non-zero.
	journaltest.InsertParsedLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-01T10:00:00Z","event":"ScanOrganic","StarSystem":"Sol","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":10477373803,"Body":1}`,
		`{"timestamp":"2026-01-01T11:00:00Z","event":"SellOrganicData","StarSystem":"Sol","SystemAddress":10477373803,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`,
	)

	expeditionRecord, originWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Realization Cache Test", "")
	if _, execError := databaseConnection.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '2026-01-01T12:00:00Z' WHERE id = ?`, expeditionRecord.ID); execError != nil {
		testingContext.Fatal(execError)
	}
	secondWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, originWaypoint.ID, expedition.RowWaypoint, 18)

	originVisitedAt := "2026-01-01T12:00:00Z"
	secondVisitedAt := "2026-01-02T12:00:00Z"
	if _, execError := databaseConnection.Exec(`UPDATE expedition_rows SET system_name = 'Sol', visited_at = ? WHERE id = ?`, originVisitedAt, originWaypoint.ID); execError != nil {
		testingContext.Fatal(execError)
	}
	if _, execError := databaseConnection.Exec(`UPDATE expedition_rows SET system_name = 'Acamar', visited_at = ? WHERE id = ?`, secondVisitedAt, secondWaypoint.ID); execError != nil {
		testingContext.Fatal(execError)
	}

	// First call populates the origin projection cache (both Metrics and Realization).
	firstOriginDetails, firstDetailsError := expedition.Details(databaseConnection, originWaypoint.ID, proj)
	if firstDetailsError != nil {
		testingContext.Fatalf("first Details call for origin waypoint failed: %v", firstDetailsError)
	}
	if firstOriginDetails.Realization.TotalRealized != 19010800 {
		testingContext.Fatalf("expected origin TotalRealized 19010800, got %d", firstOriginDetails.Realization.TotalRealized)
	}

	// Insert a corrupt SellExplorationData row with non-integer system_address.
	// Engine.Compute ignores SellExplorationData, whereas Engine.ComputeRealization scans it and returns a scan error.
	if _, execError := databaseConnection.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, raw_json, ingested_at)
		VALUES ('ev-corrupt-sale', '2026-01-02T00:00:00Z', 'SellExplorationData', 'Acamar', 'not-an-integer', '{}', '2026-01-02T00:00:00Z')`); execError != nil {
		testingContext.Fatal(execError)
	}

	// Because origin telemetry is cached in TimeRangeStats, calling Details on originWaypoint succeeds from cache.
	cachedOriginDetails, cachedDetailsError := expedition.Details(databaseConnection, originWaypoint.ID, proj)
	if cachedDetailsError != nil {
		testingContext.Fatalf("expected cached origin Details to succeed without recomputing realization, got error: %v", cachedDetailsError)
	}
	if cachedOriginDetails.Realization.TotalRealized != 19010800 {
		testingContext.Errorf("expected cached origin TotalRealized 19010800, got %d", cachedOriginDetails.Realization.TotalRealized)
	}

	// Calling Details without projections fails loudly (Approach B strict Bus injection).
	if _, nilProjError := expedition.Details(databaseConnection, secondWaypoint.ID, nil); nilProjError == nil {
		testingContext.Errorf("expected Details with nil projections to fail, got nil")
	}

	// After publishing SellExplorationData on the bus, calling Details on both leg and origin waypoints
	// must invalidate SalesLedger/TimeRangeStats, recompute, and propagate the ComputeRealization error.
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellExplorationData))
	if _, legDetailsError := expedition.Details(databaseConnection, secondWaypoint.ID, proj); legDetailsError == nil {
		testingContext.Errorf("expected Details on leg waypoint after cache invalidation to propagate ComputeRealization error, got nil")
	}
	if _, recomputedOriginError := expedition.Details(databaseConnection, originWaypoint.ID, proj); recomputedOriginError == nil {
		testingContext.Errorf("expected Details on origin waypoint after cache invalidation to propagate ComputeRealization error, got nil")
	}

	// Clean up corrupt row and invalidate projections so subsequent checks start cleanly.
	if _, execError := databaseConnection.Exec(`DELETE FROM journal_events WHERE event_uid = 'ev-corrupt-sale'`); execError != nil {
		testingContext.Fatal(execError)
	}
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellExplorationData))
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

func TestCarrierFrontierDropBoundsAndArrivalOverride(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, proj := expeditiontest.NewProjections(databaseConnection)

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T11:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1004,"StarPos":[5,0,0],"JumpDist":5.0}`,
		`{"timestamp":"2026-01-18T12:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[4,0,0],"JumpDist":4.0}`,
		`{"timestamp":"2026-01-18T13:00:00Z","event":"FSDJump","StarSystem":"Sirius","SystemAddress":1003,"StarPos":[8,0,0],"JumpDist":8.0}`,
		`{"timestamp":"2026-01-18T14:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1004,"StarPos":[5,0,0],"JumpDist":5.0}`,
	)

	expeditionRecord, solWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Carrier Frontier Test", "")
	setRowSystemName(testingContext, databaseConnection, solWaypoint.ID, "Sol")
	alphaCentauriWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, solWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, alphaCentauriWaypoint.ID, "Alpha Centauri")
	siriusWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, alphaCentauriWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, siriusWaypoint.ID, "Sirius")
	barnardWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, siriusWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, barnardWaypoint.ID, "Barnard's Star")

	fcTrue := true
	for _, stopRowID := range []int64{solWaypoint.ID, siriusWaypoint.ID, barnardWaypoint.ID} {
		if _, updateStopErr := expedition.UpdateRow(databaseConnection, expedition.RowPatch{ID: stopRowID, FCStop: &fcTrue}); updateStopErr != nil {
			testingContext.Fatalf("set FCStop on row %d: %v", stopRowID, updateStopErr)
		}
	}

	if startErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		return expedition.Start(tx, expeditionRecord.ID, "2026-01-18T10:00:00Z", 0)
	}); startErr != nil {
		testingContext.Fatal(startErr)
	}
	startedOriginRow, startOriginErr := expedition.GetRow(databaseConnection, solWaypoint.ID)
	if startOriginErr != nil {
		testingContext.Fatal(startOriginErr)
	}
	if startedOriginRow.FCArrived {
		testingContext.Fatalf("expected FC stops to start as pending (FCArrived=false) on Engage")
	}

	// Mark siriusWaypoint (Sirius) as carrier arrived via SetCarrierArrivalOverride; solWaypoint and siriusWaypoint are now ARRIVED, barnardWaypoint is PENDING.
	if overrideErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		_, err := expedition.SetCarrierArrivalOverride(tx, siriusWaypoint.ID, true)
		return err
	}); overrideErr != nil {
		testingContext.Fatal(overrideErr)
	}

	barnardDropPreviews, barnardPreviewError := expedition.ComputeRowDropPreviews(databaseConnection, barnardWaypoint.ID, proj)
	if barnardPreviewError != nil {
		testingContext.Fatal(barnardPreviewError)
	}
	// barnardWaypoint ("Barnard's Star") was visited at 11:00 (between solWaypoint Sol@10:00 and alphaCentauriWaypoint Alpha Centauri@12:00),
	// so index 1 is chronologically valid, but blocked_fc because barnardWaypoint is a PENDING FC stop and siriusWaypoint (at index 2) is ARRIVED!
	if barnardDropPreviews[1].Status != expedition.DropSlotBlockedFC {
		testingContext.Fatalf("expected barnardWaypoint drop preview at index 1 to be blocked_fc, got %+v", barnardDropPreviews[1])
	}
	siriusDropPreviews, siriusPreviewError := expedition.ComputeRowDropPreviews(databaseConnection, siriusWaypoint.ID, proj)
	if siriusPreviewError != nil {
		testingContext.Fatal(siriusPreviewError)
	}
	// siriusWaypoint ("Sirius", ARRIVED) moved to index 3 (below PENDING barnardWaypoint) sets ResetsFCArrived = true.
	if !siriusDropPreviews[3].ResetsFCArrived {
		testingContext.Fatalf("expected siriusWaypoint drop preview at index 3 to set ResetsFCArrived=true, got %+v", siriusDropPreviews[3])
	}

	// Add an unvisited non-FC waypoint vegaWaypoint ("Vega") before PENDING barnardWaypoint; moving ARRIVED siriusWaypoint into the unvisited
	// slot after vegaWaypoint (but still before PENDING barnardWaypoint) must NOT set ResetsFCArrived.
	vegaWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, siriusWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, vegaWaypoint.ID, "Vega")
	if reevalErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		_, err := expedition.Reevaluate(tx, expeditionRecord.ID)
		return err
	}); reevalErr != nil {
		testingContext.Fatal(reevalErr)
	}
	siriusPreviewsWithUnvisited, siriusUnvisitedPreviewError := expedition.ComputeRowDropPreviews(databaseConnection, siriusWaypoint.ID, proj)
	if siriusUnvisitedPreviewError != nil {
		testingContext.Fatal(siriusUnvisitedPreviewError)
	}
	// Moving siriusWaypoint (index 2, ARRIVED) to index 3 (after unvisited Vega, but before barnardWaypoint at index 4) makes siriusWaypoint unvisited
	// without crossing any PENDING FC stop, so ResetsFCArrived must be false.
	if siriusPreviewsWithUnvisited[3].Status != expedition.DropSlotValidUnvisited || siriusPreviewsWithUnvisited[3].ResetsFCArrived {
		testingContext.Fatalf("expected siriusWaypoint drop preview at unvisited index 3 before pending stop to keep ResetsFCArrived=false, got %+v", siriusPreviewsWithUnvisited[3])
	}
	if moveBeforePendingErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		if _, err := expedition.MoveRow(tx, siriusWaypoint.ID, 3); err != nil {
			return err
		}
		_, err := expedition.Reevaluate(tx, expeditionRecord.ID)
		return err
	}); moveBeforePendingErr != nil {
		testingContext.Fatal(moveBeforePendingErr)
	}
	movedRow3, movedRowErr := expedition.GetRow(databaseConnection, siriusWaypoint.ID)
	if movedRowErr != nil {
		testingContext.Fatalf("GetRow moved siriusWaypoint: %v", movedRowErr)
	}
	if !movedRow3.FCArrived {
		testingContext.Fatalf("expected siriusWaypoint to remain FCArrived=true when moved into unvisited slot before first pending stop")
	}

	// Cascading override: setting solWaypoint to arrived=false cascades to siriusWaypoint as well.
	if cascadeErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		_, err := expedition.SetCarrierArrivalOverride(tx, solWaypoint.ID, false)
		return err
	}); cascadeErr != nil {
		testingContext.Fatal(cascadeErr)
	}
	reloadedAfterCascade, reloadedCascadeErr := expedition.GetRow(databaseConnection, siriusWaypoint.ID)
	if reloadedCascadeErr != nil {
		testingContext.Fatalf("GetRow reloaded siriusWaypoint: %v", reloadedCascadeErr)
	}
	if reloadedAfterCascade.FCArrived {
		testingContext.Fatalf("expected SetCarrierArrivalOverride(solWaypoint, false) to cascade to siriusWaypoint")
	}

	// Active Spansh route to siriusWaypoint (now at index 3) locks siriusWaypoint against moves/deletes, blocks moving PENDING barnardWaypoint above siriusWaypoint,
	// and is automatically cleared if barnardWaypoint is manually overridden to ARRIVED.
	spanshRouteKey := fmt.Sprintf("active_carrier_route_%d", expeditionRecord.ID)
	spanshRouteJSON := fmt.Sprintf(`{"expeditionId":%d,"targetRowId":%d,"manualMode":false,"route":{"manualMode":false,"jumps":[{"systemName":"Sirius"}]}}`, expeditionRecord.ID, siriusWaypoint.ID)
	if setRouteError := settings.Set(databaseConnection, spanshRouteKey, spanshRouteJSON); setRouteError != nil {
		testingContext.Fatal(setRouteError)
	}
	barnardSpanshBlocked, barnardBlockedError := expedition.ComputeRowDropPreviews(databaseConnection, barnardWaypoint.ID, proj)
	if barnardBlockedError != nil {
		testingContext.Fatal(barnardBlockedError)
	}
	if barnardSpanshBlocked[2].Status != expedition.DropSlotBlockedFC {
		testingContext.Fatalf("expected barnardWaypoint drop before active Spansh target siriusWaypoint to be blocked_fc, got %+v", barnardSpanshBlocked[2])
	}
	if overrideClearError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		_, overrideError := expedition.SetCarrierArrivalOverride(transaction, barnardWaypoint.ID, true)
		return overrideError
	}); overrideClearError != nil {
		testingContext.Fatal(overrideClearError)
	}
	persistedRoute, getRouteError := settings.Get(databaseConnection, spanshRouteKey)
	if getRouteError != nil {
		testingContext.Fatal(getRouteError)
	}
	if persistedRoute != "" {
		testingContext.Fatalf("expected active Spansh route to clear when target stop overridden to arrived, got %q", persistedRoute)
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

func TestRecordCarrierJumpAndApplyTo(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	createdExpedition, createError := expedition.Create(databaseConnection, "Carrier Stats Test", "")
	if createError != nil {
		testingContext.Fatal(createError)
	}

	if err := expedition.RecordCarrierJump(databaseConnection, createdExpedition.ID, 498.5, 95); err != nil {
		testingContext.Fatalf("first RecordCarrierJump: %v", err)
	}
	if err := expedition.RecordCarrierJump(databaseConnection, createdExpedition.ID, 120.0, 25); err != nil {
		testingContext.Fatalf("second RecordCarrierJump: %v", err)
	}

	reloaded, getError := expedition.Get(databaseConnection, createdExpedition.ID)
	if getError != nil {
		testingContext.Fatal(getError)
	}
	if reloaded.Adjustments.CarrierJumps != 2 || reloaded.Adjustments.CarrierDistanceLy != 618.5 ||
		reloaded.Adjustments.TritiumUsed == nil || *reloaded.Adjustments.TritiumUsed != 120 {
		testingContext.Fatalf("unexpected Adjustments: %+v", reloaded.Adjustments)
	}

	var combinedMetrics replaystats.Metrics
	reloaded.Adjustments.ApplyTo(&combinedMetrics)
	if combinedMetrics.CarrierJumps != 2 || combinedMetrics.CarrierDistanceLy != 618.5 ||
		combinedMetrics.TritiumUsed == nil || *combinedMetrics.TritiumUsed != 120 {
		testingContext.Fatalf("unexpected ApplyTo result: %+v", combinedMetrics)
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

func TestCreateWithWaypoints(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	// Empty waypoint slice falls back to a single blank waypoint row.
	emptyExpedition, emptyError := expedition.CreateWithWaypoints(databaseConnection, "Blank Seed", "", []expedition.SeedWaypoint{
		{SystemName: "   "},
	})
	if emptyError != nil {
		testingContext.Fatalf("CreateWithWaypoints empty: %v", emptyError)
	}
	emptyRows, listEmptyError := expedition.ListRows(databaseConnection, emptyExpedition.ID)
	if listEmptyError != nil {
		testingContext.Fatalf("ListRows empty: %v", listEmptyError)
	}
	if len(emptyRows) != 1 || emptyRows[0].RowType != expedition.RowWaypoint || emptyRows[0].SystemName != "" {
		testingContext.Fatalf("expected 1 blank waypoint row, got %+v", emptyRows)
	}

	// Non-empty waypoints preserve order, normalize whitespace, skip blanks, and persist pre-resolved metadata.
	seededExpedition, seedError := expedition.CreateWithWaypoints(databaseConnection, "  Xibalba Run  ", "  Imported route  ", []expedition.SeedWaypoint{
		{
			SystemName:    "  Shinrarta   Dezhra ",
			POIName:       "  Founders World  ",
			SystemAddress: 3932277478106,
			EDSMID:        4345,
			HasCoords:     true,
			X:             55.71875,
			Y:             17.59375,
			Z:             27.15625,
			CoordsSource:  "journal",
		},
		{SystemName: "\t  "},
		{SystemName: "3 Geminorum"},
		{SystemName: "Shinrarta Dezhra"},
	})
	if seedError != nil {
		testingContext.Fatalf("CreateWithWaypoints seeded: %v", seedError)
	}
	if seededExpedition.Status != expedition.StatusPlanning || seededExpedition.Name != "Xibalba Run" || seededExpedition.Description != "Imported route" {
		testingContext.Fatalf("unexpected seeded expedition metadata: %+v", seededExpedition)
	}

	seededRows, listSeededError := expedition.ListRows(databaseConnection, seededExpedition.ID)
	if listSeededError != nil {
		testingContext.Fatalf("ListRows seeded: %v", listSeededError)
	}
	if len(seededRows) != 3 {
		testingContext.Fatalf("expected 3 seeded rows, got %d: %+v", len(seededRows), seededRows)
	}
	if seededRows[0].Position != 1 || seededRows[0].SystemName != "Shinrarta Dezhra" || seededRows[0].POIName != "Founders World" ||
		seededRows[0].SystemAddress != 3932277478106 || seededRows[0].EDSMID != 4345 || !seededRows[0].HasCoords ||
		seededRows[0].X != 55.71875 || seededRows[0].CoordsSource != "journal" {
		testingContext.Fatalf("unexpected row 0: %+v", seededRows[0])
	}
	if seededRows[1].Position != 2 || seededRows[1].SystemName != "3 Geminorum" || seededRows[1].HasCoords {
		testingContext.Fatalf("unexpected row 1: %+v", seededRows[1])
	}
	if seededRows[2].Position != 3 || seededRows[2].SystemName != "Shinrarta Dezhra" {
		testingContext.Fatalf("unexpected row 2: %+v", seededRows[2])
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

func TestScanExpeditionAdjustmentsDecodeError(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	createdExpedition, createError := expedition.Create(databaseConnection, "Adjustments Check", "")
	if createError != nil {
		testingContext.Fatalf("Create: %v", createError)
	}

	if _, blankError := databaseConnection.Exec(`UPDATE expeditions SET adjustments_json = '   ' WHERE id = ?`, createdExpedition.ID); blankError != nil {
		testingContext.Fatalf("set blank adjustments_json: %v", blankError)
	}
	if _, getBlankError := expedition.Get(databaseConnection, createdExpedition.ID); getBlankError != nil {
		testingContext.Fatalf("expected blank adjustments_json to succeed, got %v", getBlankError)
	}

	if _, corruptError := databaseConnection.Exec(`UPDATE expeditions SET adjustments_json = '{corrupt' WHERE id = ?`, createdExpedition.ID); corruptError != nil {
		testingContext.Fatalf("set corrupt adjustments_json: %v", corruptError)
	}
	if _, getCorruptError := expedition.Get(databaseConnection, createdExpedition.ID); getCorruptError == nil || !strings.Contains(getCorruptError.Error(), "decode expedition") {
		testingContext.Fatalf("expected decode error for corrupt adjustments_json, got %v", getCorruptError)
	}
}
