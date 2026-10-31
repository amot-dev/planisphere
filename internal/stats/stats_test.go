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
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
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

	// Fetch default modes
	ms, err := modes.List(conn)
	if err != nil || len(ms) < 3 {
		t.Fatalf("expected at least 3 default modes, got %v (%v)", ms, err)
	}
	var expModeID int64
	for _, m := range ms {
		if m.Name == "Exploration" {
			expModeID = m.ID
		}
	}
	if expModeID == 0 {
		t.Fatalf("could not find Exploration mode in %v", ms)
	}

	// Set window for Exploration mode covering the jumps
	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		return modes.SetWindow(tx, 0, expModeID, "2026-01-18T19:20:00Z", "2026-01-18T20:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}

	eng := stats.NewEngine(conn)

	// 1. Unfiltered baseline
	unfiltered, err := eng.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, nil)
	if err != nil {
		t.Fatal(err)
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
	filtered, err := eng.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: false, Discovery: true},
		},
	})
	if err != nil {
		t.Fatal(err)
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
	filteredAllOff, err := eng.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: false, Discovery: false},
		},
	})
	if err != nil {
		t.Fatal(err)
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
	filteredEmpty, err := eng.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes:   []stats.ModeSelection{},
	})
	if err != nil {
		t.Fatal(err)
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
	filteredTravelOn, err := eng.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: true, Discovery: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filteredTravelOn.CarrierJumps != 1 || filteredTravelOn.CarrierDistanceLy != 100.0 || filteredTravelOn.CarrierPassengerLy != 100.0 {
		t.Fatalf("unexpected metrics with Travel=true: %+v", filteredTravelOn)
	}
}

func TestCarrierOperationsSpanningDifferentModes(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
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

	if err := os.WriteFile(journalFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ing := journal.NewIngester(conn)
	if _, err := ing.ScanDir(tmpDir); err != nil {
		t.Fatal(err)
	}

	ms, err := modes.List(conn)
	if err != nil || len(ms) < 3 {
		t.Fatalf("expected at least 3 default modes, got %v (%v)", ms, err)
	}
	modeA := ms[0].ID
	modeB := ms[1].ID

	if err := db.WithTx(conn, func(tx *sql.Tx) error {
		if err := modes.SetWindow(tx, 0, modeA, "2026-01-18T20:08:00Z", "2026-01-18T20:20:00Z"); err != nil {
			return err
		}
		return modes.SetWindow(tx, 0, modeB, "2026-01-18T20:20:00Z", "2026-01-18T20:40:00Z")
	}); err != nil {
		t.Fatal(err)
	}

	eng := stats.NewEngine(conn)
	res, err := eng.Compute(stats.Range{After: "2026-01-18T20:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modeA, Travel: false, Discovery: true},
			{ModeID: modeB, Travel: true, Discovery: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Carrier operations: must include both jumps (100 Ly + 200 Ly = 300 Ly, 2 jumps)
	if res.CarrierJumps != 2 {
		t.Fatalf("expected 2 carrier jumps across modes, got %d", res.CarrierJumps)
	}
	if res.CarrierDistanceLy != 300.0 {
		t.Fatalf("expected 300.0 carrier distance Ly, got %.1f", res.CarrierDistanceLy)
	}

	// Passenger navigation: only Jump 2 was in Travel=true mode (200 Ly)
	if res.CarrierPassengerLy != 200.0 {
		t.Fatalf("expected 200.0 carrier passenger Ly, got %.1f", res.CarrierPassengerLy)
	}
	if res.DistanceLy != 200.0 {
		t.Fatalf("expected 200.0 total distance Ly, got %.1f", res.DistanceLy)
	}
}
