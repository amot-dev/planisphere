package replay_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

func TestQuerySystemVisitsScopedAndReconcilesWithVisitCounts(testingContext *testing.T) {
	databaseConnection := openReplayTestDB(testingContext)
	journalDir := testingContext.TempDir()

	// Seed two active commanders plus a third commander disabled in settings:
	// - Sol has only Location events (initial starting system fallback).
	// - Colonia has FSDJump, CarrierJump, a crash-recovered jump, and a Multicrew jump.
	// - TieSys has two FSDJumps at the identical timestamp to verify UID DESC tie-breaking.
	journalLines := []string{
		`{"timestamp":"2026-05-01T08:00:00Z","event":"LoadGame","Commander":"CmdrAlpha","FID":"F101","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T08:01:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-05-01T08:30:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-05-01T09:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,0],"JumpDist":100.0}`,
		`{"timestamp":"2026-05-01T09:30:00Z","event":"FSDJump","StarSystem":"WaypointMid","SystemAddress":2003,"StarPos":[150,0,0],"JumpDist":50.0}`,
		`{"timestamp":"2026-05-01T09:40:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"Colonia","SystemAddress":2002}`,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrAlpha","FID":"F101","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:01:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,0]}`,
		`{"timestamp":"2026-05-01T10:30:00Z","event":"CarrierJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,0]}`,
		`{"timestamp":"2026-05-01T10:45:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,0],"JumpDist":20.0,"Multicrew":true}`,

		`{"timestamp":"2026-05-02T08:00:00Z","event":"LoadGame","Commander":"CmdrBeta","FID":"F202","Ship":"CobraMkIII","Odyssey":true}`,
		`{"timestamp":"2026-05-02T08:05:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,0],"JumpDist":80.0}`,

		`{"timestamp":"2026-05-03T08:00:00Z","event":"LoadGame","Commander":"CmdrMuted","FID":"F303","Ship":"SideWinder","Odyssey":true}`,
		`{"timestamp":"2026-05-03T08:05:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[100,0,0],"JumpDist":80.0}`,
	}
	journalPath := filepath.Join(journalDir, "Journal.2026-05-01T080000.01.log")
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(journalLines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), journalDir); scanError != nil {
		testingContext.Fatalf("ScanDir: %v", scanError)
	}

	commanders, listError := replay.ListCommanders(databaseConnection)
	if listError != nil {
		testingContext.Fatalf("ListCommanders: %v", listError)
	}
	profileIDByCommander := make(map[string]int64, len(commanders))
	for _, commander := range commanders {
		profileIDByCommander[commander.Name] = commander.ID
	}
	if _, disableError := databaseConnection.Exec(`UPDATE commander_profiles SET enabled = 0 WHERE id = ?`, profileIDByCommander["CmdrMuted"]); disableError != nil {
		testingContext.Fatalf("disable CmdrMuted: %v", disableError)
	}

	// Insert two same-timestamp jumps at TieSys to verify deterministic UID DESC sorting.
	if _, insertError := databaseConnection.Exec(`
		INSERT INTO journal_events (event_uid, timestamp, event, system_name, profile_id, commander, raw_json, ingested_at) VALUES
		('uid-a', '2026-05-04T12:00:00Z', 'FSDJump', 'TieSys', ?, 'CmdrAlpha', '{}', '2026-05-04T12:00:00Z'),
		('uid-z', '2026-05-04T12:00:00Z', 'FSDJump', 'TieSys', ?, 'CmdrAlpha', '{}', '2026-05-04T12:00:00Z')
	`, profileIDByCommander["CmdrAlpha"], profileIDByCommander["CmdrAlpha"]); insertError != nil {
		testingContext.Fatalf("insert tie events: %v", insertError)
	}

	emptyVisits, emptyError := replay.QuerySystemVisits(databaseConnection, "   ", nil)
	if emptyError != nil || emptyVisits != nil {
		testingContext.Fatalf("expected nil visits for whitespace query, got %+v err=%v", emptyVisits, emptyError)
	}

	recoveredJumps, recoverError := replay.ComputeRecoveredJumps(databaseConnection)
	if recoverError != nil {
		testingContext.Fatalf("ComputeRecoveredJumps: %v", recoverError)
	}
	if len(recoveredJumps) != 1 || recoveredJumps[0].SystemName != "Colonia" {
		testingContext.Fatalf("expected 1 recovered jump to Colonia, got %+v", recoveredJumps)
	}

	// All-enabled query on Colonia: CmdrBeta FSDJump (May 2), CmdrAlpha CarrierJump (10:30),
	// CmdrAlpha recovered jump (10:01), CmdrAlpha FSDJump (09:00); excludes Multicrew and CmdrMuted.
	coloniaAllVisits, allError := replay.QuerySystemVisits(databaseConnection, "  colonia ", recoveredJumps)
	if allError != nil {
		testingContext.Fatalf("QuerySystemVisits(colonia): %v", allError)
	}
	expectedColoniaTimestamps := []string{
		"2026-05-02T08:05:00Z",
		"2026-05-01T10:30:00Z",
		"2026-05-01T10:01:00Z",
		"2026-05-01T09:00:00Z",
	}
	if len(coloniaAllVisits) != len(expectedColoniaTimestamps) {
		testingContext.Fatalf("expected %d Colonia visits, got %d: %+v", len(expectedColoniaTimestamps), len(coloniaAllVisits), coloniaAllVisits)
	}
	for index, expectedTimestamp := range expectedColoniaTimestamps {
		if coloniaAllVisits[index].Timestamp != expectedTimestamp {
			testingContext.Fatalf("Colonia visit %d timestamp = %q, want %q", index, coloniaAllVisits[index].Timestamp, expectedTimestamp)
		}
		if coloniaAllVisits[index].Galactic != reference.ToGalactic(expectedTimestamp) {
			testingContext.Fatalf("Colonia visit %d galactic = %q, want %q", index, coloniaAllVisits[index].Galactic, reference.ToGalactic(expectedTimestamp))
		}
	}

	// Deduplicate if a recovered jump shares a jump UID already returned from SQL.
	duplicatedRecovered := append(append([]replay.RecoveredJump{}, recoveredJumps...), replay.RecoveredJump{
		LocationEventUID: coloniaAllVisits[0].UID,
		Timestamp:        coloniaAllVisits[0].Timestamp,
		SystemName:       "Colonia",
		ProfileID:        coloniaAllVisits[0].ProfileID,
		Commander:        coloniaAllVisits[0].Commander,
	})
	dedupedVisits, dedupError := replay.QuerySystemVisits(databaseConnection, "Colonia", duplicatedRecovered)
	if dedupError != nil || len(dedupedVisits) != len(expectedColoniaTimestamps) {
		testingContext.Fatalf("expected duplicate recovered UID to be skipped, got len=%d err=%v", len(dedupedVisits), dedupError)
	}

	// Explicit ProfileScope filters both SQL rows and recovered jumps to CmdrAlpha.
	alphaScope := replay.NewProfileScope([]int64{profileIDByCommander["CmdrAlpha"]})
	coloniaAlphaVisits, alphaError := replay.QuerySystemVisitsScoped(databaseConnection, "Colonia", recoveredJumps, alphaScope)
	if alphaError != nil {
		testingContext.Fatalf("QuerySystemVisitsScoped(CmdrAlpha): %v", alphaError)
	}
	if len(coloniaAlphaVisits) != 3 {
		testingContext.Fatalf("expected 3 CmdrAlpha visits to Colonia (FSDJump, recovered, CarrierJump), got %+v", coloniaAlphaVisits)
	}

	// Sol has only Location events: must return single earliest Location event.
	solVisits, solError := replay.QuerySystemVisits(databaseConnection, "Sol", recoveredJumps)
	if solError != nil {
		testingContext.Fatalf("QuerySystemVisits(Sol): %v", solError)
	}
	if len(solVisits) != 1 || solVisits[0].Timestamp != "2026-05-01T08:01:00Z" || solVisits[0].Galactic != reference.ToGalactic("2026-05-01T08:01:00Z") {
		testingContext.Fatalf("expected single earliest Sol Location fallback at 08:01:00Z, got %+v", solVisits)
	}

	// Reconcile 1-to-1 with QuerySystemVisitCountsScoped across all systems.
	visitCountsAll, countAllError := replay.QuerySystemVisitCounts(databaseConnection, []string{"colonia", "sol"}, recoveredJumps)
	if countAllError != nil {
		testingContext.Fatalf("QuerySystemVisitCounts: %v", countAllError)
	}
	if visitCountsAll["colonia"] != len(coloniaAllVisits) || visitCountsAll["sol"] != len(solVisits) {
		testingContext.Fatalf("visit count mismatch: counts=%+v, visits colonia=%d sol=%d", visitCountsAll, len(coloniaAllVisits), len(solVisits))
	}
	visitCountsAlpha, countAlphaError := replay.QuerySystemVisitCountsScoped(databaseConnection, []string{"colonia"}, recoveredJumps, alphaScope)
	if countAlphaError != nil {
		testingContext.Fatalf("QuerySystemVisitCountsScoped: %v", countAlphaError)
	}
	if visitCountsAlpha["colonia"] != len(coloniaAlphaVisits) {
		testingContext.Fatalf("scoped visit count mismatch: counts=%+v, visits=%d", visitCountsAlpha, len(coloniaAlphaVisits))
	}

	// Verify deterministic UID DESC tie-breaking for identical timestamps.
	tieVisits, tieError := replay.QuerySystemVisits(databaseConnection, "TieSys", nil)
	if tieError != nil || len(tieVisits) != 2 || tieVisits[0].UID != "uid-z" || tieVisits[1].UID != "uid-a" {
		testingContext.Fatalf("expected UID DESC tie-breaker [uid-z, uid-a], got %+v err=%v", tieVisits, tieError)
	}
}
