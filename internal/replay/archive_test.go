package replay_test

import (
	"strings"
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestLoadEventsAndQueryLatestEvent(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-01-01T00:00:00Z","event":"Commander","Commander":"CmdrEnabled","FID":"F1"}`,
		`{"timestamp":"2026-01-01T00:01:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-01T00:02:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,2,3],"JumpDist":4.3}`,
		`{"timestamp":"2026-01-01T00:03:00Z","event":"Commander","Commander":"CmdrDisabled","FID":"F2"}`,
		`{"timestamp":"2026-01-01T00:04:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":1003,"StarPos":[10,20,30],"JumpDist":50.0}`,
	)
	if _, execError := databaseConnection.Exec(
		`INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrEnabled', 'F1', 1), ('CmdrDisabled', 'F2', 0)`,
	); execError != nil {
		testingContext.Fatalf("insert commanders: %v", execError)
	}

	arrivalSet := journal.NewEventSet(journal.EventLocation, journal.EventFSDJump)
	loadedRows, loadError := replay.LoadEventsScoped(databaseConnection, "2026-01-01T00:01:00Z", "", arrivalSet, replay.ProfileScope{})
	if loadError != nil {
		testingContext.Fatalf("LoadEventsScoped: %v", loadError)
	}
	if len(loadedRows) != 1 || loadedRows[0].Type != journal.EventFSDJump || loadedRows[0].SystemName != "Alpha Centauri" || !loadedRows[0].HasStarPos {
		testingContext.Fatalf("unexpected loadedRows: %+v", loadedRows)
	}

	latestRow, latestError := replay.QueryLatestEvent(databaseConnection, arrivalSet)
	if latestError != nil || latestRow == nil {
		testingContext.Fatalf("QueryLatestEvent: row=%v err=%v", latestRow, latestError)
	}
	if latestRow.Type != journal.EventFSDJump || latestRow.SystemName != "Alpha Centauri" {
		testingContext.Fatalf("expected latest enabled arrival Alpha Centauri, got %+v", latestRow)
	}

	emptyRows, emptyError := replay.LoadEventsScoped(databaseConnection, "", "", journal.NewEventSet(), replay.ProfileScope{})
	if emptyError != nil || len(emptyRows) != 0 {
		testingContext.Fatalf("expected empty result for empty EventSet, got rows=%v err=%v", emptyRows, emptyError)
	}
	missingLatest, missingError := replay.QueryLatestEvent(databaseConnection, journal.NewEventSet(journal.EventCarrierStats))
	if missingError != nil || missingLatest != nil {
		testingContext.Fatalf("expected nil for absent CarrierStats event, got row=%v err=%v", missingLatest, missingError)
	}
}

func TestArchiveStatusPropagatesDisabledCommanderQueryError(testingContext *testing.T) {
	conn := dbtest.OpenMemory(testingContext)

	if _, dropError := conn.Exec(`DROP TABLE commanders`); dropError != nil {
		testingContext.Fatal(dropError)
	}

	_, statusError := replay.ArchiveStatus(conn)
	if statusError == nil {
		testingContext.Fatal("expected ArchiveStatus to fail when commanders table is missing")
	}
	if !strings.Contains(statusError.Error(), "check disabled commanders") {
		testingContext.Fatalf("expected error wrapping check disabled commanders, got %v", statusError)
	}
}

func TestMulticrewIgnoredAcrossArrivalsLocationAndVisitCounts(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-06-01T09:59:00Z","event":"Commander","Commander":"CmdrA","FID":"FA"}`,
		`{"timestamp":"2026-06-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-06-01T10:10:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[4,0,0],"Taxi":true}`,
		`{"timestamp":"2026-06-01T10:20:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":1003,"StarPos":[9000,0,9000],"Multicrew":true}`,
		`{"timestamp":"2026-06-01T10:30:00Z","event":"FSDJump","StarSystem":"Beagle Point","SystemAddress":1004,"StarPos":[0,0,65000],"Multicrew":true}`,
	)

	windowArrivals, windowError := replay.QueryArrivalsInWindowScoped(databaseConnection, "2026-06-01T09:00:00Z", "", replay.ProfileScope{})
	if windowError != nil {
		testingContext.Fatalf("QueryArrivalsInWindowScoped: %v", windowError)
	}
	if len(windowArrivals) != 2 || windowArrivals[0].SystemName != "Sol" || windowArrivals[1].SystemName != "Alpha Centauri" || windowArrivals[0].Commander != "CmdrA" {
		testingContext.Fatalf("expected only physical Sol and Taxi Alpha Centauri arrivals, got %+v", windowArrivals)
	}

	beagleCandidates, candError := replay.QuerySystemArrivalCandidatesScoped(databaseConnection, "Beagle Point", 1004, replay.ProfileScope{})
	if candError != nil || len(beagleCandidates) != 0 {
		testingContext.Fatalf("expected Multicrew Beagle Point excluded from StartCandidates, got %+v err=%v", beagleCandidates, candError)
	}

	latestPos, latestSys, locError := replay.QueryLatestPlayerLocation(databaseConnection)
	if locError != nil || latestPos == nil || latestSys != "Alpha Centauri" || latestPos.X != 4 {
		testingContext.Fatalf("expected latest physical location Alpha Centauri (4,0,0), got sys=%q pos=%+v err=%v", latestSys, latestPos, locError)
	}

	counts, countError := replay.QuerySystemVisitCountsScoped(databaseConnection, []string{"sol", "alpha centauri", "colonia", "beagle point"}, nil, replay.ProfileScope{})
	if countError != nil {
		testingContext.Fatalf("QuerySystemVisitCountsScoped: %v", countError)
	}
	if counts["sol"] != 1 || counts["alpha centauri"] != 1 || counts["colonia"] != 0 || counts["beagle point"] != 0 {
		testingContext.Fatalf("expected multicrew systems to have 0 visits and taxi/sol 1 visit, got %+v", counts)
	}
}
