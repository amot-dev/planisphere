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

func mustInsert(t *testing.T, conn *sql.DB, expID, afterID int64, rowType string, regionID int) expedition.Row {
	t.Helper()
	var r expedition.Row
	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		var err error
		r, err = expedition.InsertRow(tx, expID, afterID, rowType, regionID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestZeroDataLossAndOutOfOrderReordering(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

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
	if err := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ing := journal.NewIngester(conn)
	res, err := ing.ScanDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NewEvents) != len(lines) {
		t.Fatalf("ingested %d events, want %d", len(res.NewEvents), len(lines))
	}

	// Re-scanning the same directory is idempotent (0 new events).
	res2, err := ing.ScanDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.NewEvents) != 0 {
		t.Fatalf("expected 0 new events on idempotent rescan, got %d", len(res2.NewEvents))
	}

	// Delete the journal file from disk: zero data may be lost.
	if err := os.Remove(journalFile); err != nil {
		t.Fatal(err)
	}

	// Create an expedition with 5 waypoints + a region header.
	exp, err := expedition.Create(conn, "Distant Worlds III", "Deep space survey")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := expedition.ListRows(conn, exp.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected 1 seeded row, got %d (%v)", len(rows), err)
	}
	setName := func(rowID int64, sys string) {
		if _, err := expedition.UpdateRow(conn, expedition.RowPatch{ID: rowID, SystemName: &sys}); err != nil {
			t.Fatal(err)
		}
	}
	setName(rows[0].ID, "Sol")

	w2 := mustInsert(t, conn, exp.ID, rows[0].ID, expedition.RowWaypoint, 0)
	setName(w2.ID, "Dryooe Prou FF-Z d696")
	regHeader := mustInsert(t, conn, exp.ID, w2.ID, expedition.RowRegion, 1)
	w3 := mustInsert(t, conn, exp.ID, regHeader.ID, expedition.RowWaypoint, 0)
	setName(w3.ID, "Outotz LS-K d8-3")
	w4 := mustInsert(t, conn, exp.ID, w3.ID, expedition.RowWaypoint, 0)
	setName(w4.ID, "Sagittarius A*")
	w5 := mustInsert(t, conn, exp.ID, w4.ID, expedition.RowWaypoint, 0)
	setName(w5.ID, "Beagle Point")

	// Start expedition at 2026-01-18T19:42:10Z.
	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		return expedition.Start(tx, exp.ID, "2026-01-18T19:42:10Z")
	}); err != nil {
		t.Fatal(err)
	}

	plan, err := expedition.BuildPlan(conn, exp.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Expected waypoint sequence after auto-reordering:
	// #1 Sol (visited) -> #2 Dryooe Prou FF-Z d696 (visited) -> #3 Beagle Point (current, moved up!) -> #4 Outotz LS-K d8-3 (future; pre-start visit ignored) -> #5 Sagittarius A* (future).
	var wpSystems []string
	var wpNumbers []int
	var wpStates []string
	for _, r := range plan.Rows {
		if r.Row.RowType == expedition.RowWaypoint {
			wpSystems = append(wpSystems, r.Row.SystemName)
			wpNumbers = append(wpNumbers, r.Number)
			wpStates = append(wpStates, r.VisitState)
		}
	}
	wantSystems := []string{"Sol", "Dryooe Prou FF-Z d696", "Beagle Point", "Outotz LS-K d8-3", "Sagittarius A*"}
	if strings.Join(wpSystems, "|") != strings.Join(wantSystems, "|") {
		t.Fatalf("waypoint order = %v, want %v", wpSystems, wantSystems)
	}
	for i, n := range wpNumbers {
		if n != i+1 {
			t.Fatalf("waypoint numbering = %v", wpNumbers)
		}
	}
	wantStates := []string{expedition.VisitVisited, expedition.VisitVisited, expedition.VisitCurrent, expedition.VisitFuture, expedition.VisitFuture}
	if strings.Join(wpStates, "|") != strings.Join(wantStates, "|") {
		t.Fatalf("visit states = %v, want %v", wpStates, wantStates)
	}

	// Verify ship switch transition banner between #2 and #3.
	beagleView := plan.Rows[2]
	if len(beagleView.Transitions) != 1 || beagleView.Transitions[0].Kind != "ship" || beagleView.Transitions[0].To != "Caspian Explorer" {
		t.Fatalf("expected ship transition on Beagle Point leg, got %+v", beagleView.Transitions)
	}

	// Verify mode filtering with the Travel scan/exobio override.
	ms, err := modes.List(conn)
	if err != nil || len(ms) < 3 {
		t.Fatalf("expected 3 default modes, got %v (%v)", ms, err)
	}
	var travelModeID int64
	for _, m := range ms {
		if m.Name == "Travel" {
			travelModeID = m.ID
		}
	}
	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		return modes.SetWindow(tx, 0, travelModeID, "2026-01-18T19:40:00Z", "2026-01-18T22:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}

	eng := stats.NewEngine(conn)
	// Hide Travel jumps/Ly, but include Travel scans/exobio!
	filtered, err := eng.Compute(stats.Range{After: "2026-01-18T19:42:10Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: travelModeID, Travel: false, Discovery: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Jumps != 0 || filtered.DistanceLy != 0 {
		t.Fatalf("expected Travel jumps/distance to be hidden, got %d jumps / %.1f Ly", filtered.Jumps, filtered.DistanceLy)
	}
	if filtered.BodiesScanned != 1 || filtered.BodiesMapped != 1 || filtered.OrganicSamples != 1 || filtered.OrganicValueTotal != 95054000 {
		t.Fatalf("expected Travel scans/exobio override to retain discoveries, got %+v", filtered)
	}
}

func TestPruneEmptyRowsOnComplete(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	exp, err := expedition.Create(conn, "Prune Test", "Test pruning empty rows on complete")
	if err != nil {
		t.Fatal(err)
	}
	initialRows, err := expedition.ListRows(conn, exp.ID)
	if err != nil || len(initialRows) != 1 {
		t.Fatalf("expected 1 initial row, got %d", len(initialRows))
	}
	setSys := func(rowID int64, sys string) {
		if _, err := expedition.UpdateRow(conn, expedition.RowPatch{ID: rowID, SystemName: &sys}); err != nil {
			t.Fatal(err)
		}
	}
	setSys(initialRows[0].ID, "Sol")

	// Header 1: Inner Orion Spur (followed by non-empty waypoints)
	h1 := mustInsert(t, conn, exp.ID, initialRows[0].ID, expedition.RowRegion, 1)
	w2 := mustInsert(t, conn, exp.ID, h1.ID, expedition.RowWaypoint, 0)
	setSys(w2.ID, "Alpha Centauri")

	// Header 2: Elysian Shore (followed only by empty waypoints)
	h2 := mustInsert(t, conn, exp.ID, w2.ID, expedition.RowRegion, 2)
	wEmpty1 := mustInsert(t, conn, exp.ID, h2.ID, expedition.RowWaypoint, 0)
	setSys(wEmpty1.ID, "")
	wEmpty2 := mustInsert(t, conn, exp.ID, wEmpty1.ID, expedition.RowWaypoint, 0)
	setSys(wEmpty2.ID, "   ")

	// Header 3: Empty header at end (no waypoints at all)
	_ = mustInsert(t, conn, exp.ID, wEmpty2.ID, expedition.RowRegion, 3)

	// Start expedition
	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		return expedition.Start(tx, exp.ID, "2026-01-18T19:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}

	// Complete expedition
	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		return expedition.Complete(tx, exp.ID, "2026-01-18T20:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}

	surviving, err := expedition.ListRows(conn, exp.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Surviving should be:
	// 1. Sol (waypoint)
	// 2. Region 1 (h1)
	// 3. Alpha Centauri (waypoint)
	// All empty waypoints and headers 2 & 3 must be deleted.
	if len(surviving) != 3 {
		t.Fatalf("expected 3 surviving rows, got %d: %+v", len(surviving), surviving)
	}
	if surviving[0].SystemName != "Sol" || surviving[0].Position != 1 {
		t.Errorf("row 0 mismatch: %+v", surviving[0])
	}
	if surviving[1].RowType != expedition.RowRegion || surviving[1].RegionID != 1 || surviving[1].Position != 2 {
		t.Errorf("row 1 mismatch: %+v", surviving[1])
	}
	if surviving[2].SystemName != "Alpha Centauri" || surviving[2].Position != 3 {
		t.Errorf("row 2 mismatch: %+v", surviving[2])
	}
}

