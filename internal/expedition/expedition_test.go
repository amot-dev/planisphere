package expedition_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
	"planisphere/internal/stats"
)

func mustInsert(t *testing.T, dbConn *sql.DB, expeditionID, afterID int64, rowType string, regionID int) expedition.Row {
	t.Helper()
	var insertedRow expedition.Row
	if txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var insertError error
		insertedRow, insertError = expedition.InsertRow(transaction, expeditionID, afterID, rowType, regionID)
		return insertError
	}); txError != nil {
		t.Fatal(txError)
	}
	return insertedRow
}

func TestZeroDataLossAndOutOfOrderReordering(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer dbConn.Close()

	tmpDir := t.TempDir()
	journalFile := filepath.Join(tmpDir, "Journal.2026-01-18T190000.01.log")

	lines := []string{
		// Pre-expedition visit to Waypoint #3 (must be ignored because it is before expedition_start_timestamp).
		`{"timestamp":"2026-01-10T10:00:00Z","event":"FSDJump","StarSystem":"Outotz LS-K d8-3","SystemAddress":1003,"StarPos":[100,0,100],"JumpDist":45.0}`,
		// Earliest Statistics snapshot before expedition start.
		`{"timestamp":"2026-01-18T19:00:00Z","event":"Statistics","Exploration":{"Exploration_Profits":5000000,"Planets_Scanned_To_Level_2":120,"Planets_Scanned_To_Level_3":30,"Total_Hyperspace_Distance":10000,"Total_Hyperspace_Jumps":250},"Exobiology":{"Organic_Data":5,"Organic_Data_Profits":25000000,"First_Logged":2,"First_Logged_Profits":16000000}}`,
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
	if writeError := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		t.Fatal(writeError)
	}

	ingester := journal.NewIngester(dbConn)
	scanResult, scanError := ingester.ScanDir(context.Background(), tmpDir)
	if scanError != nil {
		t.Fatal(scanError)
	}
	if scanResult.NewEvents != len(lines) {
		t.Fatalf("ingested %d events, want %d", scanResult.NewEvents, len(lines))
	}

	// Re-scanning the same directory is idempotent (0 new events).
	rescanResult, rescanError := ingester.ScanDir(context.Background(), tmpDir)
	if rescanError != nil {
		t.Fatal(rescanError)
	}
	if rescanResult.NewEvents != 0 {
		t.Fatalf("expected 0 new events on idempotent rescan, got %d", rescanResult.NewEvents)
	}

	// Delete the journal file from disk: zero data may be lost.
	if removeError := os.Remove(journalFile); removeError != nil {
		t.Fatal(removeError)
	}

	// Create an expedition with 5 waypoints + a region header.
	expeditionRecord, createError := expedition.Create(dbConn, "Distant Worlds III", "Deep space survey")
	if createError != nil {
		t.Fatal(createError)
	}
	rows, listError := expedition.ListRows(dbConn, expeditionRecord.ID)
	if listError != nil || len(rows) != 1 {
		t.Fatalf("expected 1 seeded row, got %d (%v)", len(rows), listError)
	}
	setName := func(rowID int64, systemName string) {
		if _, updateError := expedition.UpdateRow(dbConn, expedition.RowPatch{ID: rowID, SystemName: &systemName}); updateError != nil {
			t.Fatal(updateError)
		}
	}
	setName(rows[0].ID, "Sol")

	waypoint2 := mustInsert(t, dbConn, expeditionRecord.ID, rows[0].ID, expedition.RowWaypoint, 0)
	setName(waypoint2.ID, "Dryooe Prou FF-Z d696")
	regionHeader := mustInsert(t, dbConn, expeditionRecord.ID, waypoint2.ID, expedition.RowRegion, 1)
	waypoint3 := mustInsert(t, dbConn, expeditionRecord.ID, regionHeader.ID, expedition.RowWaypoint, 0)
	setName(waypoint3.ID, "Outotz LS-K d8-3")
	waypoint4 := mustInsert(t, dbConn, expeditionRecord.ID, waypoint3.ID, expedition.RowWaypoint, 0)
	setName(waypoint4.ID, "Sagittarius A*")
	waypoint5 := mustInsert(t, dbConn, expeditionRecord.ID, waypoint4.ID, expedition.RowWaypoint, 0)
	setName(waypoint5.ID, "Beagle Point")

	// Start expedition at 2026-01-18T19:42:10Z.
	if startTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-01-18T19:42:10Z")
	}); startTxError != nil {
		t.Fatal(startTxError)
	}

	plan, planError := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil)
	if planError != nil {
		t.Fatal(planError)
	}
	// Expected waypoint sequence after auto-reordering:
	// #1 Sol (visited) -> #2 Dryooe Prou FF-Z d696 (visited) -> #3 Beagle Point (current, moved up!) -> #4 Outotz LS-K d8-3 (future; pre-start visit ignored) -> #5 Sagittarius A* (future).
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
	wantStates := []string{expedition.VisitVisited, expedition.VisitVisited, expedition.VisitCurrent, expedition.VisitFuture, expedition.VisitFuture}
	if strings.Join(waypointStates, "|") != strings.Join(wantStates, "|") {
		t.Fatalf("visit states = %v, want %v", waypointStates, wantStates)
	}

	// Verify ship switch transition banner between #2 and #3.
	beagleView := plan.Rows[2]
	if len(beagleView.Transitions) != 1 || beagleView.Transitions[0].Kind != "ship" || beagleView.Transitions[0].To != "Caspian Explorer" {
		t.Fatalf("expected ship transition on Beagle Point leg, got %+v", beagleView.Transitions)
	}

	// Verify mode filtering with the Travel scan/exobio override.
	modesList, modesError := modes.List(dbConn)
	if modesError != nil || len(modesList) < 3 {
		t.Fatalf("expected 3 default modes, got %v (%v)", modesList, modesError)
	}
	var travelModeID int64
	for _, modeItem := range modesList {
		if modeItem.Name == "Travel" {
			travelModeID = modeItem.ID
		}
	}
	if modeTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return modes.SetWindow(transaction, 0, travelModeID, "2026-01-18T19:40:00Z", "2026-01-18T22:00:00Z")
	}); modeTxError != nil {
		t.Fatal(modeTxError)
	}

	statsEngine := stats.NewEngine(dbConn)
	// Hide Travel jumps/Ly, but include Travel scans/exobio!
	filtered, computeError := statsEngine.Compute(stats.Range{After: "2026-01-18T19:42:10Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: travelModeID, Travel: false, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}
	if filtered.Jumps != 0 || filtered.DistanceLy != 0 {
		t.Fatalf("expected Travel jumps/distance to be hidden, got %d jumps / %.1f Ly", filtered.Jumps, filtered.DistanceLy)
	}
	if filtered.BodiesScanned != 1 || filtered.BodiesMapped != 1 || filtered.OrganicSamples != 1 || filtered.OrganicValueTotal != 95054000 {
		t.Fatalf("expected Travel scans/exobio override to retain discoveries, got %+v", filtered)
	}
}

func TestPruneEmptyRowsOnComplete(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer dbConn.Close()

	expeditionRecord, createError := expedition.Create(dbConn, "Prune Test", "Test pruning empty rows on complete")
	if createError != nil {
		t.Fatal(createError)
	}
	initialRows, listError := expedition.ListRows(dbConn, expeditionRecord.ID)
	if listError != nil || len(initialRows) != 1 {
		t.Fatalf("expected 1 initial row, got %d", len(initialRows))
	}
	setSys := func(rowID int64, systemName string) {
		if _, updateError := expedition.UpdateRow(dbConn, expedition.RowPatch{ID: rowID, SystemName: &systemName}); updateError != nil {
			t.Fatal(updateError)
		}
	}
	setSys(initialRows[0].ID, "Sol")

	// Header 1: Inner Orion Spur (followed by non-empty waypoints)
	header1 := mustInsert(t, dbConn, expeditionRecord.ID, initialRows[0].ID, expedition.RowRegion, 1)
	waypoint2 := mustInsert(t, dbConn, expeditionRecord.ID, header1.ID, expedition.RowWaypoint, 0)
	setSys(waypoint2.ID, "Alpha Centauri")

	// Header 2: Elysian Shore (followed only by empty waypoints)
	header2 := mustInsert(t, dbConn, expeditionRecord.ID, waypoint2.ID, expedition.RowRegion, 2)
	emptyWaypoint1 := mustInsert(t, dbConn, expeditionRecord.ID, header2.ID, expedition.RowWaypoint, 0)
	setSys(emptyWaypoint1.ID, "")
	emptyWaypoint2 := mustInsert(t, dbConn, expeditionRecord.ID, emptyWaypoint1.ID, expedition.RowWaypoint, 0)
	setSys(emptyWaypoint2.ID, "   ")

	// Header 3: Empty header at end (no waypoints at all)
	_ = mustInsert(t, dbConn, expeditionRecord.ID, emptyWaypoint2.ID, expedition.RowRegion, 3)

	// Start expedition
	if startTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Start(transaction, expeditionRecord.ID, "2026-01-18T19:00:00Z")
	}); startTxError != nil {
		t.Fatal(startTxError)
	}

	// Complete expedition
	if completeTxError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return expedition.Complete(transaction, expeditionRecord.ID, "2026-01-18T20:00:00Z")
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
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer dbConn.Close()

	expedition.InvalidateShipCache()

	tmpDir := t.TempDir()
	journalFile := filepath.Join(tmpDir, "Journal.2026-01-18T190000.01.log")
	lines := []string{
		// Historical ship swaps before expedition
		`{"timestamp":"2026-01-01T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"anaconda","Ship_Localised":"Anaconda","ShipID":1}`,
		// On-foot suit load game event (should be ignored by ship filter)
		`{"timestamp":"2026-01-10T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		// Latest ship before expedition start
		`{"timestamp":"2026-01-15T12:00:00Z","event":"ShipyardSwap","Commander":"Cmdr Test","FID":"F1","ShipType":"diamondbackxl","ShipType_Localised":"Diamondback Explorer","ShipID":10}`,
		// 15 consecutive on-foot suit LoadGame events between ship swap and expedition start
		`{"timestamp":"2026-01-16T01:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-16T02:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-16T03:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-16T04:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-16T05:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T01:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T02:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T03:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T04:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T05:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T06:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T07:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T08:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T09:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		`{"timestamp":"2026-01-17T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"utilitysuit_class1","Ship_Localised":"Maverick Suit","ShipID":2}`,
		// Expedition start and arrival at Sol
		`{"timestamp":"2026-01-18T10:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":25.0}`,
		// Mid-expedition ship swap
		`{"timestamp":"2026-01-19T14:00:00Z","event":"ShipyardSwap","Commander":"Cmdr Test","FID":"F1","ShipType":"krait_phantom","ShipType_Localised":"Krait Phantom","ShipID":20}`,
		// Arrival at Alpha Centauri
		`{"timestamp":"2026-01-20T16:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`,
		// Post-expedition ship swap (should be clamped out)
		`{"timestamp":"2026-01-25T12:00:00Z","event":"ShipyardSwap","Commander":"Cmdr Test","FID":"F1","ShipType":"empire_trader","ShipType_Localised":"Imperial Cutter","ShipID":30}`,
	}
	if err := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	ingester := journal.NewIngester(dbConn)
	if _, err := ingester.ScanDir(context.Background(), tmpDir); err != nil {
		t.Fatal(err)
	}

	exp, err := expedition.Create(dbConn, "Ship Clamping Test", "")
	if err != nil {
		t.Fatal(err)
	}
	initialRows, err := expedition.ListRows(dbConn, exp.ID)
	if err != nil || len(initialRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(initialRows))
	}
	solName := "Sol"
	if _, err := expedition.UpdateRow(dbConn, expedition.RowPatch{ID: initialRows[0].ID, SystemName: &solName}); err != nil {
		t.Fatal(err)
	}
	alpha := mustInsert(t, dbConn, exp.ID, initialRows[0].ID, expedition.RowWaypoint, 0)
	alphaName := "Alpha Centauri"
	if _, err := expedition.UpdateRow(dbConn, expedition.RowPatch{ID: alpha.ID, SystemName: &alphaName}); err != nil {
		t.Fatal(err)
	}

	// Start at 2026-01-18T10:00:00Z, complete at 2026-01-21T10:00:00Z
	_ = db.WithTx(dbConn, func(tx *sql.Tx) error {
		return expedition.Start(tx, exp.ID, "2026-01-18T10:00:00Z")
	})
	_ = db.WithTx(dbConn, func(tx *sql.Tx) error {
		_, err := expedition.Reevaluate(tx, exp.ID)
		return err
	})
	_ = db.WithTx(dbConn, func(tx *sql.Tx) error {
		return expedition.Complete(tx, exp.ID, "2026-01-21T10:00:00Z")
	})

	// Build plan
	plan, err := expedition.BuildPlan(dbConn, exp.ID, nil)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify transitions on leg arriving at Alpha Centauri
	var alphaTransitions []expedition.Transition
	for _, r := range plan.Rows {
		if r.Row.SystemName == "Alpha Centauri" {
			alphaTransitions = r.Transitions
		}
	}
	if len(alphaTransitions) == 0 {
		t.Fatalf("expected transitions arriving at Alpha Centauri, got none")
	}
	// It should have Krait Phantom
	foundPhantom := false
	for _, tr := range alphaTransitions {
		if tr.To == "Krait Phantom" {
			foundPhantom = true
		}
		if tr.To == "Imperial Cutter" {
			t.Errorf("Imperial Cutter should have been clamped out (occurred after expedition end)")
		}
	}
	if !foundPhantom {
		t.Errorf("expected Krait Phantom transition, got: %+v", alphaTransitions)
	}

	// Verify cache hit
	plan2, err := expedition.BuildPlan(dbConn, exp.ID, nil)
	if err != nil {
		t.Fatalf("second BuildPlan failed: %v", err)
	}
	if len(plan2.Rows) != len(plan.Rows) {
		t.Errorf("cache hit plan mismatch")
	}
}

func TestDetailsLightweightAndOriginCache(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer dbConn.Close()

	expedition.InvalidateShipCache()

	tmpDir := t.TempDir()
	journalFile := filepath.Join(tmpDir, "Journal.2026-01-18T190000.01.log")
	lines := []string{
		`{"timestamp":"2026-01-01T10:00:00Z","event":"LoadGame","Commander":"Cmdr Test","FID":"F1","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1}`,
		`{"timestamp":"2026-01-18T10:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":20.0}`,
		`{"timestamp":"2026-01-19T12:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	}
	if err := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	ingester := journal.NewIngester(dbConn)
	if _, err := ingester.ScanDir(context.Background(), tmpDir); err != nil {
		t.Fatal(err)
	}

	exp, err := expedition.Create(dbConn, "Details Test Expedition", "")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := expedition.ListRows(dbConn, exp.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	wp1 := rows[0]
	solName := "Sol"
	solVis := "2026-01-18T10:00:00Z"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = ?, visited_at = ?, x = 0, y = 0, z = 0 WHERE id = ?`, solName, solVis, wp1.ID); err != nil {
		t.Fatal(err)
	}

	// Insert region header row
	regionRow := mustInsert(t, dbConn, exp.ID, wp1.ID, expedition.RowRegion, 1)

	// Insert WP2 (visited leg)
	wp2 := mustInsert(t, dbConn, exp.ID, regionRow.ID, expedition.RowWaypoint, 0)
	alphaName := "Alpha Centauri"
	alphaVis := "2026-01-19T12:00:00Z"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = ?, visited_at = ?, x = 10, y = 0, z = 0 WHERE id = ?`, alphaName, alphaVis, wp2.ID); err != nil {
		t.Fatal(err)
	}

	// Insert WP3 (future waypoint)
	wp3 := mustInsert(t, dbConn, exp.ID, wp2.ID, expedition.RowWaypoint, 0)
	barnardName := "Barnard's Star"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = ?, x = 16, y = 0, z = 0 WHERE id = ?`, barnardName, wp3.ID); err != nil {
		t.Fatal(err)
	}

	// Details for region header: should return empty details
	regDetails, err := expedition.Details(dbConn, regionRow.ID)
	if err != nil {
		t.Fatalf("Details on region row returned error: %v", err)
	}
	if regDetails.Number != 0 || regDetails.Kind != "" {
		t.Errorf("expected empty details for region row, got %+v", regDetails)
	}

	// Details for WP1 (origin)
	wp1Details, err := expedition.Details(dbConn, wp1.ID)
	if err != nil {
		t.Fatalf("Details for wp1 failed: %v", err)
	}
	if wp1Details.Kind != "origin" {
		t.Errorf("expected wp1 kind 'origin', got %q", wp1Details.Kind)
	}
	if wp1Details.Number != 1 {
		t.Errorf("expected wp1 number 1, got %d", wp1Details.Number)
	}
	if wp1Details.To != solVis {
		t.Errorf("expected wp1 To %q, got %q", solVis, wp1Details.To)
	}
	if wp1Details.From != "2026-01-01T10:00:00Z" {
		t.Errorf("expected wp1 From '2026-01-01T10:00:00Z', got %q", wp1Details.From)
	}

	// Second call for WP1 should hit origin metrics cache
	wp1DetailsCached, err := expedition.Details(dbConn, wp1.ID)
	if err != nil {
		t.Fatalf("cached Details for wp1 failed: %v", err)
	}
	if wp1DetailsCached.Metrics != wp1Details.Metrics {
		t.Errorf("expected cached metrics to match, got %+v vs %+v", wp1DetailsCached.Metrics, wp1Details.Metrics)
	}

	// Invalidate cache and verify it still succeeds
	expedition.InvalidateOriginMetricsCache()
	wp1DetailsRecomputed, err := expedition.Details(dbConn, wp1.ID)
	if err != nil {
		t.Fatalf("Details after cache invalidation failed: %v", err)
	}
	if wp1DetailsRecomputed.Metrics != wp1Details.Metrics {
		t.Errorf("metrics mismatch after recomputing: %+v vs %+v", wp1DetailsRecomputed.Metrics, wp1Details.Metrics)
	}

	// Details for WP2 (leg)
	wp2Details, err := expedition.Details(dbConn, wp2.ID)
	if err != nil {
		t.Fatalf("Details for wp2 failed: %v", err)
	}
	if wp2Details.Kind != "leg" {
		t.Errorf("expected wp2 kind 'leg', got %q", wp2Details.Kind)
	}
	if wp2Details.Number != 2 {
		t.Errorf("expected wp2 number 2, got %d", wp2Details.Number)
	}
	if wp2Details.From != solVis {
		t.Errorf("expected wp2 leg start %q, got %q", solVis, wp2Details.From)
	}
	if wp2Details.To != alphaVis {
		t.Errorf("expected wp2 leg end %q, got %q", alphaVis, wp2Details.To)
	}
	if wp2Details.PrevSystem != solName {
		t.Errorf("expected wp2 prev system %q, got %q", solName, wp2Details.PrevSystem)
	}
	if wp2Details.Elapsed == "" {
		t.Errorf("expected non-empty elapsed duration for wp2 leg")
	}

	// Details for WP3 (future)
	wp3Details, err := expedition.Details(dbConn, wp3.ID)
	if err != nil {
		t.Fatalf("Details for wp3 failed: %v", err)
	}
	if wp3Details.Kind != "future" {
		t.Errorf("expected wp3 kind 'future', got %q", wp3Details.Kind)
	}
	if wp3Details.Number != 3 {
		t.Errorf("expected wp3 number 3, got %d", wp3Details.Number)
	}
	if wp3Details.PrevSystem != alphaName {
		t.Errorf("expected wp3 prev system %q, got %q", alphaName, wp3Details.PrevSystem)
	}
	if wp3Details.CurrentSystem != alphaName {
		t.Errorf("expected wp3 current active system %q, got %q", alphaName, wp3Details.CurrentSystem)
	}
	if wp3Details.EstDistFromPrevLy == nil || *wp3Details.EstDistFromPrevLy != 6.0 {
		t.Errorf("expected wp3 dist from prev ly 6.0, got %v", wp3Details.EstDistFromPrevLy)
	}
	if wp3Details.EstDistFromCurrentLy == nil || *wp3Details.EstDistFromCurrentLy != 6.0 {
		t.Errorf("expected wp3 dist from current ly 6.0, got %v", wp3Details.EstDistFromCurrentLy)
	}
}

func TestInsertRowPositionsAndReevaluateConsolidatedUpdates(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer dbConn.Close()

	expeditionRecord, createError := expedition.Create(dbConn, "Batch Test", "Test position updates")
	if createError != nil {
		t.Fatal(createError)
	}

	rows, listError := expedition.ListRows(dbConn, expeditionRecord.ID)
	if listError != nil || len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	wp1 := rows[0]

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
	rows, _ = expedition.ListRows(dbConn, expeditionRecord.ID)
	if len(rows) != 3 || rows[0].Position != 1 || rows[1].Position != 2 || rows[2].Position != 3 {
		t.Fatalf("unexpected rows after end inserts: %+v", rows)
	}

	// Insert in middle (after wp1, before wp2)
	wpMid := mustInsert(t, dbConn, expeditionRecord.ID, wp1.ID, expedition.RowWaypoint, 0)
	if wpMid.Position != 2 {
		t.Errorf("expected wpMid position 2, got %d", wpMid.Position)
	}

	rows, _ = expedition.ListRows(dbConn, expeditionRecord.ID)
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

	rows, _ = expedition.ListRows(dbConn, expeditionRecord.ID)
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	for i, r := range rows {
		if r.Position != i+1 {
			t.Errorf("row %d position is %d, want %d", i, r.Position, i+1)
		}
	}

	// Test Reevaluate consolidated updates
	// Setup systems
	setSys := func(rowID int64, name string) {
		if _, updateError := expedition.UpdateRow(dbConn, expedition.RowPatch{ID: rowID, SystemName: &name}); updateError != nil {
			t.Fatal(updateError)
		}
	}
	setSys(wpTop.ID, "Alpha")
	setSys(wp1.ID, "Beta")
	setSys(wpMid.ID, "Gamma")
	setSys(wp2.ID, "Delta")
	setSys(wp3.ID, "Epsilon")

	// Start expedition
	_ = db.WithTx(dbConn, func(tx *sql.Tx) error {
		return expedition.Start(tx, expeditionRecord.ID, "2026-02-01T10:00:00Z")
	})

	// Add an arrival that skips Beta and visits Gamma directly, triggering out-of-order reordering,
	// position updates, visited_at, visit_event_uid, leg_mode_id all in one evaluation.
	_, err := dbConn.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at)
		VALUES ('ev-start', '2026-02-01T10:00:00Z', 'FSDJump', 'Alpha', '{}', '2026-02-01T10:00:00Z'),
		       ('ev-gamma', '2026-02-01T12:00:00Z', 'FSDJump', 'Gamma', '{}', '2026-02-01T12:00:00Z')`)
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

	// Verify rows after reevaluation:
	// Alpha (visited, pos 1) -> Gamma (visited, moved to pos 2) -> Beta (unvisited, pos 3) -> Delta (pos 4) -> Epsilon (pos 5)
	rows, _ = expedition.ListRows(dbConn, expeditionRecord.ID)
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	expectedOrder := []string{"Alpha", "Gamma", "Beta", "Delta", "Epsilon"}
	for i, expectedSys := range expectedOrder {
		if rows[i].SystemName != expectedSys {
			t.Errorf("row %d system %s, want %s", i, rows[i].SystemName, expectedSys)
		}
		if rows[i].Position != i+1 {
			t.Errorf("row %d position %d, want %d", i, rows[i].Position, i+1)
		}
	}
	if rows[0].VisitedAt != "2026-02-01T10:00:00Z" {
		t.Errorf("Alpha visited_at mismatch: %q", rows[0].VisitedAt)
	}
	if rows[1].VisitedAt != "2026-02-01T12:00:00Z" || rows[1].VisitEventUID != "ev-gamma" {
		t.Errorf("Gamma visit mismatch: %q / %q", rows[1].VisitedAt, rows[1].VisitEventUID)
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
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer dbConn.Close()

	expedition.InvalidateShipCache()

	expeditionRecord, err := expedition.Create(dbConn, "Route Telemetry Test", "")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := expedition.ListRows(dbConn, expeditionRecord.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	firstWaypoint := rows[0]
	solName := "Sol"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = ?, x = 0, y = 0, z = 0 WHERE id = ?`, solName, firstWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	secondWaypoint := mustInsert(t, dbConn, expeditionRecord.ID, firstWaypoint.ID, expedition.RowWaypoint, 0)
	secondWaypointName := "Alpha Centauri"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = ?, x = 100, y = 0, z = 0 WHERE id = ?`, secondWaypointName, secondWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	thirdWaypoint := mustInsert(t, dbConn, expeditionRecord.ID, secondWaypoint.ID, expedition.RowWaypoint, 0)
	thirdWaypointName := "Barnard's Star"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET system_name = ?, x = 250, y = 0, z = 0 WHERE id = ?`, thirdWaypointName, thirdWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	// In planning status:
	// PlannedLy = 100 + 150 = 250 Ly.
	// RemainingLy = 250 Ly.
	// No row is next waypoint.
	planPlanning, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil)
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
	if txErr := db.WithTx(dbConn, func(tx *sql.Tx) error {
		return expedition.Start(tx, expeditionRecord.ID, "2026-03-01T10:00:00Z")
	}); txErr != nil {
		t.Fatal(txErr)
	}
	visitedAt := "2026-03-01T10:00:00Z"
	if _, err := dbConn.Exec(`UPDATE expedition_rows SET visited_at = ? WHERE id = ?`, visitedAt, firstWaypoint.ID); err != nil {
		t.Fatal(err)
	}

	// Player is initially at Sol (0, 0, 0)
	_, err = dbConn.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, star_x, star_y, star_z, raw_json, ingested_at)
		VALUES ('ev-sol', '2026-03-01T10:00:00Z', 'Location', 'Sol', 0.0, 0.0, 0.0, '{}', '2026-03-01T10:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	planAtOrigin, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if planAtOrigin.NextWaypointRowID != secondWaypoint.ID {
		t.Errorf("expected NextWaypointRowID %d (secondWaypoint), got %d", secondWaypoint.ID, planAtOrigin.NextWaypointRowID)
	}
	// secondWaypoint is next waypoint, distance from player (0,0,0) to secondWaypoint (100,0,0) is 100.
	// RemainingLy = 100 (player to secondWaypoint) + 150 (secondWaypoint to thirdWaypoint) = 250.
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
	if !secondWaypointRow.IsNextWaypoint {
		t.Errorf("expected secondWaypoint to be next waypoint")
	}
	if secondWaypointRow.DistFromCurrentLy == nil || *secondWaypointRow.DistFromCurrentLy != 100.0 {
		t.Errorf("expected secondWaypoint dist from player 100.0, got %v", secondWaypointRow.DistFromCurrentLy)
	}
	if thirdWaypointRow.IsNextWaypoint {
		t.Errorf("expected thirdWaypoint NOT to be next waypoint")
	}
	if thirdWaypointRow.DistFromPrevLy == nil || *thirdWaypointRow.DistFromPrevLy != 150.0 {
		t.Errorf("expected thirdWaypoint dist from prev 150.0, got %v", thirdWaypointRow.DistFromPrevLy)
	}

	// Player jumps closer to secondWaypoint: Waypoint "Interim System" at (60, 0, 0)
	_, err = dbConn.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, star_x, star_y, star_z, raw_json, ingested_at)
		VALUES ('ev-jump1', '2026-03-01T11:00:00Z', 'FSDJump', 'Interim System', 60.0, 0.0, 0.0, '{}', '2026-03-01T11:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}

	planAfterFirstJump, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil)
	if err != nil {
		t.Fatal(err)
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
	planAfterSecondWaypointVisit, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil)
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
	planAfterFinalWaypointVisit, err := expedition.BuildPlan(dbConn, expeditionRecord.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if planAfterFinalWaypointVisit.CoveredLy != 250.0 {
		t.Errorf("expected covered ly 250.0 after thirdWaypoint visit, got %v", planAfterFinalWaypointVisit.CoveredLy)
	}
}
