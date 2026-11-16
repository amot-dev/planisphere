package profile_test

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
	"planisphere/internal/profile"
)

func seedProfileTestDatabase(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_live.db")

	conn, openErr := db.Open(dbPath)
	if openErr != nil {
		t.Fatalf("open test db: %v", openErr)
	}
	defer conn.Close()

	events := []string{
		`{"timestamp":"2026-01-01T10:00:00Z","event":"LoadGame","Commander":"CmdrProfile","FID":"F100","Ship":"Mandalar","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-01-01T10:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"Population":22780000000}`,
		`{"timestamp":"2026-01-01T10:15:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":20001,"StarPos":[3.0,0.0,3.0],"JumpDist":4.24,"Population":0}`,
		`{"timestamp":"2026-01-01T10:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alpha Centauri","SystemAddress":20001,"BodyID":1,"BodyName":"Alpha Centauri A 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-01-01T10:25:00Z","event":"SAAScanComplete","SystemAddress":20001,"BodyID":1,"BodyName":"Alpha Centauri A 1","ProbesUsed":4,"EfficiencyTarget":6}`,
		`{"timestamp":"2026-01-01T10:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Stratum_Genus_Name;","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","SystemAddress":20001,"Body":1}`,
		`{"timestamp":"2026-01-01T11:00:00Z","event":"FSDJump","StarSystem":"Barnards Star","SystemAddress":20002,"StarPos":[-3.0,5.0,1.0],"JumpDist":6.1,"Population":0}`,
		`{"timestamp":"2026-01-01T11:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Barnards Star","SystemAddress":20002,"BodyID":2,"BodyName":"Barnards Star 2","PlanetClass":"Water world","MassEM":0.8,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-01-01T12:00:00Z","event":"SellOrganicData","MarketID":32001,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`,
	}

	_, _ = conn.Exec(`INSERT INTO commanders(name, fid, enabled, first_seen, last_seen) VALUES ('CmdrProfile', 'F100', 1, '2026-01-01T10:00:00Z', '2026-01-01T12:00:00Z')`)
	_, _ = conn.Exec(`INSERT INTO ingested_journals(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at) VALUES ('Journal.2026-01-01.log', '/tmp/Journal.2026-01-01.log', 1024, 9, 1024, 9, '2026-01-01T10:00:00Z', '2026-01-01T12:00:00Z', '2026-01-01T12:00:00Z')`)

	for _, rawLine := range events {
		parsed, keep, parseErr := journal.ParseLine([]byte(rawLine), "Journal.2026-01-01.log")
		if parseErr != nil || !keep {
			t.Fatalf("parse seed event: keep=%v err=%v", keep, parseErr)
		}
		_, execErr := conn.Exec(
			journal.InsertJournalEventPrefixSQL,
			parsed.UID, parsed.Timestamp, parsed.Event,
			parsed.SystemName, parsed.SystemAddress, parsed.BodyID,
			parsed.BodyName, 0.0, 0.0, 0.0,
			"CmdrProfile", "Journal.2026-01-01.log", parsed.Raw, parsed.Timestamp,
		)
		if execErr != nil {
			t.Fatalf("insert seed event: %v", execErr)
		}
	}

	createdExp, createErr := expedition.Create(conn, "Test Survey", "Profiler verification")
	if createErr != nil {
		t.Fatalf("create expedition: %v", createErr)
	}
	rows, _ := expedition.ListRows(conn, createdExp.ID)
	solName := "Sol"
	_, _ = expedition.UpdateRow(conn, expedition.RowPatch{ID: rows[0].ID, SystemName: &solName})
	_ = db.WithTx(conn, func(tx *sql.Tx) error {
		secondRow, err := expedition.InsertRow(tx, createdExp.ID, rows[0].ID, expedition.RowWaypoint, 0)
		if err != nil {
			return err
		}
		alphaName := "Alpha Centauri"
		_, _ = expedition.UpdateRow(tx, expedition.RowPatch{ID: secondRow.ID, SystemName: &alphaName})
		thirdRow, err := expedition.InsertRow(tx, createdExp.ID, secondRow.ID, expedition.RowWaypoint, 0)
		if err != nil {
			return err
		}
		barnardName := "Barnards Star"
		_, _ = expedition.UpdateRow(tx, expedition.RowPatch{ID: thirdRow.ID, SystemName: &barnardName})
		return expedition.Start(tx, createdExp.ID, "2026-01-01T10:05:00Z")
	})

	return dbPath
}

func TestRunSnapshotSuiteAndDiff(t *testing.T) {
	dbPath := seedProfileTestDatabase(t)

	report, runErr := profile.RunSnapshotSuite(context.Background(), profile.Options{
		DBPath:          dbPath,
		Iterations:      1,
		IncludeFrontend: false,
	})
	if runErr != nil {
		t.Fatalf("RunSnapshotSuite failed: %v", runErr)
	}

	if _, statErr := os.Stat(report.Dataset.SnapshotDBPath); !os.IsNotExist(statErr) {
		t.Errorf("expected temporary snapshot %s to be removed after suite completion", report.Dataset.SnapshotDBPath)
	}
	if len(report.Probes) == 0 {
		t.Fatalf("expected probes to run, got 0")
	}
	if len(report.ScalingModels) == 0 {
		t.Errorf("expected at least 1 multi-window scaling model")
	}

	foundExplainPlan := false
	for _, probe := range report.Probes {
		if probe.HasWarm && !probe.ColdWarmMatch {
			t.Errorf("probe %s Cold hash (%s) != Warm hash (%s)", probe.Name, probe.Cold.ResultHash, probe.Warm.ResultHash)
		}
		if probe.Cold.RootSpan == nil || len(probe.Cold.RootSpan.Children) == 0 {
			t.Errorf("probe %s missing hierarchical child spans", probe.Name)
		}
		if hasExplainPlanRecursive(probe.Cold.RootSpan) {
			foundExplainPlan = true
		}
	}
	if !foundExplainPlan {
		t.Errorf("expected at least one SQL span to include an EXPLAIN QUERY PLAN summary")
	}

	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if saveErr := profile.SaveReport(baselinePath, report); saveErr != nil {
		t.Fatalf("SaveReport failed: %v", saveErr)
	}
	loadedBaseline, loadErr := profile.LoadReport(baselinePath)
	if loadErr != nil {
		t.Fatalf("LoadReport failed: %v", loadErr)
	}
	diff := profile.CompareReports(loadedBaseline, report)
	if diff.AnyResultMismatch {
		t.Errorf("expected identical reports to have zero result hash mismatches")
	}

	rendered := profile.FormatReport(report, &diff)
	for _, expectedSnippet := range []string{
		"PLANISPHERE LIVE DATA PROFILER",
		"PROBE SUMMARY TABLE",
		"MULTI-WINDOW SCALING DECOMPOSITION",
		"HIERARCHICAL EXECUTION TREES",
		"EXPLAIN:",
		"DIAGNOSTICS",
		"BASELINE COMPARISON",
	} {
		if !strings.Contains(rendered, expectedSnippet) {
			t.Errorf("expected rendered report to contain %q", expectedSnippet)
		}
	}
}

func hasExplainPlanRecursive(node *profile.SpanNode) bool {
	if node == nil {
		return false
	}
	if len(node.QueryPlans) > 0 {
		return true
	}
	for _, child := range node.Children {
		if hasExplainPlanRecursive(child) {
			return true
		}
	}
	return false
}