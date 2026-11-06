package stats_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
	"planisphere/internal/stats"
)

func TestCarrierOperationsIgnoreModeFilter(t *testing.T) {
	conn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	journalFile := filepath.Join(tmpDir, "Journal.2026-01-18T190000.01.log")

	// Setup a timeline of events:
	// 19:05 - LoadGame
	// 19:10 - Location in Sol (0, 0, 0)
	// 19:15 - CarrierStats: fuel 1000, free 20000 (used = 5000)
	// 19:20 - Mode "Exploration" active (Travel: false, Discovery: true)
	// 19:30 - CarrierJump to Proxima (0, 0, 100) -> 100 Ly
	// 19:40 - CarrierStats: fuel 960 (delta = 40 Plausible fuel delta)
	// 19:50 - FSDJump from Proxima to Barnard's Star (0, 0, 106) -> 6 Ly
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:15:00Z","event":"CarrierStats","CarrierID":9001,"FuelLevel":1000,"SpaceUsage":{"FreeSpace":20000,"TotalCapacity":25000}}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"CarrierJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100]}`,
		`{"timestamp":"2026-01-18T19:40:00Z","event":"CarrierStats","CarrierID":9001,"FuelLevel":960,"SpaceUsage":{"FreeSpace":20000,"TotalCapacity":25000}}`,
		`{"timestamp":"2026-01-18T19:50:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1003,"StarPos":[0,0,106],"JumpDist":6.0}`,
	}

	if writeError := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		t.Fatal(writeError)
	}

	ingester := journal.NewIngester(conn)
	scanResult, scanError := ingester.ScanDir(tmpDir)
	if scanError != nil {
		t.Fatal(scanError)
	}
	if scanResult.NewEvents != len(lines) {
		t.Fatalf("ingested %d events, want %d", scanResult.NewEvents, len(lines))
	}

	// Fetch default modes
	modeList, listError := modes.List(conn)
	if listError != nil || len(modeList) < 3 {
		t.Fatalf("expected at least 3 default modes, got %v (%v)", modeList, listError)
	}
	var expModeID int64
	for _, modeItem := range modeList {
		if modeItem.Name == "Exploration" {
			expModeID = modeItem.ID
		}
	}
	if expModeID == 0 {
		t.Fatalf("could not find Exploration mode in %v", modeList)
	}

	// Set window for Exploration mode covering the jumps
	if txError := db.WithTx(conn, func(tx *sql.Tx) error {
		return modes.SetWindow(tx, 0, expModeID, "2026-01-18T19:20:00Z", "2026-01-18T20:00:00Z")
	}); txError != nil {
		t.Fatal(txError)
	}

	engine := stats.NewEngine(conn)

	// 1. Unfiltered baseline
	unfiltered, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, nil)
	if computeError != nil {
		t.Fatal(computeError)
	}
	if unfiltered.CarrierJumps != 1 {
		t.Fatalf("expected 1 carrier jump, got %d", unfiltered.CarrierJumps)
	}
	if unfiltered.CarrierDistanceLy != 100.0 {
		t.Fatalf("expected 100.0 carrier distance Ly, got %.1f", unfiltered.CarrierDistanceLy)
	}
	if unfiltered.CarrierPassengerLy != 100.0 {
		t.Fatalf("expected 100.0 carrier passenger Ly, got %.1f", unfiltered.CarrierPassengerLy)
	}
	if unfiltered.ShipDistanceLy != 6.0 {
		t.Fatalf("expected 6.0 ship distance Ly, got %.1f", unfiltered.ShipDistanceLy)
	}
	if unfiltered.DistanceLy != 106.0 {
		t.Fatalf("expected 106.0 total distance Ly, got %.1f", unfiltered.DistanceLy)
	}
	if unfiltered.TritiumUsed == nil || *unfiltered.TritiumUsed != 40 {
		t.Fatalf("expected 40 tritium used, got %v", unfiltered.TritiumUsed)
	}

	// 2. Filter with Travel = false for Exploration mode
	// Carrier Operations metrics (CarrierJumps, CarrierDistanceLy, TritiumUsed) MUST completely ignore mode!
	// Navigation metrics (CarrierPassengerLy, ShipDistanceLy, DistanceLy) MUST be filtered out!
	filtered, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: false, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}

	if filtered.CarrierJumps != 1 {
		t.Fatalf("expected CarrierJumps to ignore mode filter (want 1), got %d", filtered.CarrierJumps)
	}
	if filtered.CarrierDistanceLy != 100.0 {
		t.Fatalf("expected CarrierDistanceLy to ignore mode filter (want 100.0), got %.1f", filtered.CarrierDistanceLy)
	}
	if filtered.TritiumUsed == nil || *filtered.TritiumUsed != 40 {
		t.Fatalf("expected TritiumUsed to ignore mode filter (want 40), got %v", filtered.TritiumUsed)
	}

	// Non-operation travel metrics must be filtered
	if filtered.CarrierPassengerLy != 0 {
		t.Fatalf("expected CarrierPassengerLy to be filtered to 0, got %.1f", filtered.CarrierPassengerLy)
	}
	if filtered.ShipDistanceLy != 0 {
		t.Fatalf("expected ShipDistanceLy to be filtered to 0, got %.1f", filtered.ShipDistanceLy)
	}
	if filtered.DistanceLy != 0 {
		t.Fatalf("expected DistanceLy to be filtered to 0, got %.1f", filtered.DistanceLy)
	}
	if filtered.Jumps != 0 {
		t.Fatalf("expected Jumps to be filtered to 0, got %d", filtered.Jumps)
	}

	// 2b. Filter with Travel = false AND Discovery = false (mode completely disabled)
	// Carrier Operations metrics MUST still completely ignore mode!
	filteredAllOff, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: false, Discovery: false},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}
	if filteredAllOff.CarrierJumps != 1 || filteredAllOff.CarrierDistanceLy != 100.0 {
		t.Fatalf("expected carrier operations to ignore mode even when all categories off: %+v", filteredAllOff)
	}
	if filteredAllOff.TritiumUsed == nil || *filteredAllOff.TritiumUsed != 40 {
		t.Fatalf("expected tritium used 40 when all categories off, got %v", filteredAllOff.TritiumUsed)
	}
	if filteredAllOff.CarrierPassengerLy != 0 || filteredAllOff.DistanceLy != 0 {
		t.Fatalf("expected passenger distance 0 when all categories off: %+v", filteredAllOff)
	}

	// 2c. Filter enabled with empty modes list (everything excluded by default)
	filteredEmpty, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes:   []stats.ModeSelection{},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}
	if filteredEmpty.CarrierJumps != 1 || filteredEmpty.CarrierDistanceLy != 100.0 {
		t.Fatalf("expected carrier operations to ignore empty mode filter: %+v", filteredEmpty)
	}
	if filteredEmpty.TritiumUsed == nil || *filteredEmpty.TritiumUsed != 40 {
		t.Fatalf("expected tritium used 40 with empty filter, got %v", filteredEmpty.TritiumUsed)
	}
	if filteredEmpty.CarrierPassengerLy != 0 || filteredEmpty.DistanceLy != 0 {
		t.Fatalf("expected passenger distance 0 with empty filter: %+v", filteredEmpty)
	}

	// 3. Filter with Travel = true for Exploration mode
	// All metrics included
	filteredTravelOn, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: true, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}
	if filteredTravelOn.CarrierJumps != 1 || filteredTravelOn.CarrierDistanceLy != 100.0 || filteredTravelOn.CarrierPassengerLy != 100.0 {
		t.Fatalf("unexpected metrics with Travel=true: %+v", filteredTravelOn)
	}
}

func TestCarrierOperationsSpanningDifferentModes(t *testing.T) {
	conn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	journalFile := filepath.Join(tmpDir, "Journal.2026-01-18T200000.01.log")

	// Jump 1 at 20:10 during Mode A (Travel=false) -> 100 Ly
	// Jump 2 at 20:30 during Mode B (Travel=true) -> 200 Ly
	lines := []string{
		`{"timestamp":"2026-01-18T20:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T20:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T20:10:00Z","event":"CarrierJump","StarSystem":"System1","SystemAddress":1002,"StarPos":[0,0,100]}`,
		`{"timestamp":"2026-01-18T20:30:00Z","event":"CarrierJump","StarSystem":"System2","SystemAddress":1003,"StarPos":[0,0,300]}`,
	}

	if writeError := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		t.Fatal(writeError)
	}

	ingester := journal.NewIngester(conn)
	if _, scanError := ingester.ScanDir(tmpDir); scanError != nil {
		t.Fatal(scanError)
	}

	modeList, listError := modes.List(conn)
	if listError != nil || len(modeList) < 2 {
		t.Fatalf("expected at least 2 default modes, got %v (%v)", modeList, listError)
	}
	modeA := modeList[0].ID
	modeB := modeList[1].ID

	if txError := db.WithTx(conn, func(tx *sql.Tx) error {
		if windowError := modes.SetWindow(tx, 0, modeA, "2026-01-18T20:08:00Z", "2026-01-18T20:20:00Z"); windowError != nil {
			return windowError
		}
		return modes.SetWindow(tx, 0, modeB, "2026-01-18T20:20:00Z", "2026-01-18T20:40:00Z")
	}); txError != nil {
		t.Fatal(txError)
	}

	engine := stats.NewEngine(conn)
	computeResult, computeError := engine.Compute(stats.Range{After: "2026-01-18T20:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modeA, Travel: false, Discovery: true},
			{ModeID: modeB, Travel: true, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}

	// Carrier operations: must include both jumps (100 Ly + 200 Ly = 300 Ly, 2 jumps)
	if computeResult.CarrierJumps != 2 {
		t.Fatalf("expected 2 carrier jumps across modes, got %d", computeResult.CarrierJumps)
	}
	if computeResult.CarrierDistanceLy != 300.0 {
		t.Fatalf("expected 300.0 carrier distance Ly, got %.1f", computeResult.CarrierDistanceLy)
	}

	// Passenger navigation: only Jump 2 was in Travel=true mode (200 Ly)
	if computeResult.CarrierPassengerLy != 200.0 {
		t.Fatalf("expected 200.0 carrier passenger Ly, got %.1f", computeResult.CarrierPassengerLy)
	}
	if computeResult.DistanceLy != 200.0 {
		t.Fatalf("expected 200.0 total distance Ly, got %.1f", computeResult.DistanceLy)
	}
}

func TestPreloadScansForMappedBodies(t *testing.T) {
	conn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatal(openError)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	journalFile := filepath.Join(tmpDir, "Journal.2026-01-18T180000.01.log")

	// Scan at 18:00 (before expedition)
	// Expedition range is 19:00 -> 20:00
	// SAAScanComplete at 19:30 (during expedition)
	lines := []string{
		`{"timestamp":"2026-01-18T18:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T18:05:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Test Planet 1","BodyID":5,"StarSystem":"TestSys","SystemAddress":9999,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"SAAScanComplete","BodyName":"Test Planet 1","BodyID":5,"SystemAddress":9999,"ProbesUsed":4,"EfficiencyTarget":6}`,
	}
	if writeError := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		t.Fatal(writeError)
	}

	ingester := journal.NewIngester(conn)
	if _, err := ingester.ScanDir(tmpDir); err != nil {
		t.Fatal(err)
	}

	engine := stats.NewEngine(conn)
	// Compute strictly from 19:00 onward (Scan at 18:00 is outside timeRange)
	res, err := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.BodiesMapped != 1 {
		t.Fatalf("expected 1 body mapped, got %d", res.BodiesMapped)
	}
	if res.FirstMapped != 1 {
		t.Fatalf("expected 1 first mapped, got %d", res.FirstMapped)
	}
	if res.ScanValueTotal == 0 {
		t.Fatalf("expected positive scan value total from preloaded scan, got %d", res.ScanValueTotal)
	}
}
