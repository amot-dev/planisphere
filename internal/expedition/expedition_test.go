package expedition_test

import (
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
	scanResult, scanError := ingester.ScanDir(tmpDir)
	if scanError != nil {
		t.Fatal(scanError)
	}
	if len(scanResult.NewEvents) != len(lines) {
		t.Fatalf("ingested %d events, want %d", len(scanResult.NewEvents), len(lines))
	}

	// Re-scanning the same directory is idempotent (0 new events).
	rescanResult, rescanError := ingester.ScanDir(tmpDir)
	if rescanError != nil {
		t.Fatal(rescanError)
	}
	if len(rescanResult.NewEvents) != 0 {
		t.Fatalf("expected 0 new events on idempotent rescan, got %d", len(rescanResult.NewEvents))
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
	// 1. Sol (waypoint)
	// 2. Region 1 (header1)
	// 3. Alpha Centauri (waypoint)
	// All empty waypoints and headers 2 & 3 must be deleted.
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

