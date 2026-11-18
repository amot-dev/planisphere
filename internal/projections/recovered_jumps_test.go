package projections_test

import (
	"context"
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/settings"
)

func openProjectionTestDB(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("open in-memory db: %v", openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})
	return databaseConnection
}

func TestRecoveredJumpsPositiveAndBoostLifecycle(testingContext *testing.T) {
	databaseConnection := openProjectionTestDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	journalPath := filepath.Join(temporaryDirectory, "Journal.2026-11-16T180124.01.log")

	lines := []string{
		`{"timestamp":"2026-11-17T01:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:00:05Z","event":"Location","StarSystem":"Origin","SystemAddress":100,"StarPos":[0,0,0],"Docked":false}`,
		// Hop 1: unboosted mid-jump disconnect after cancelled target
		`{"timestamp":"2026-11-17T01:01:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"CancelledSys","SystemAddress":999}`,
		`{"timestamp":"2026-11-17T01:01:20Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopOne","SystemAddress":101}`,
		`{"timestamp":"2026-11-17T01:02:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:02:10Z","event":"Location","StarSystem":"HopOne","SystemAddress":101,"StarPos":[30,40,0],"Docked":false}`,
		// Hop 2: JetConeBoost and consecutive failed LoadGame retries before Location spawns
		`{"timestamp":"2026-11-17T01:03:00Z","event":"JetConeBoost","BoostValue":4.0}`,
		`{"timestamp":"2026-11-17T01:03:30Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopTwo","SystemAddress":102}`,
		`{"timestamp":"2026-11-17T01:04:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:04:30Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:04:45Z","event":"Location","StarSystem":"HopTwo","SystemAddress":102,"StarPos":[30,40,200],"Docked":false}`,
		// Hop 3: Basic synthesis overwritten by Premium synthesis before disconnect
		`{"timestamp":"2026-11-17T01:05:00Z","event":"Synthesis","Name":"FSD Basic"}`,
		`{"timestamp":"2026-11-17T01:05:10Z","event":"Synthesis","Name":"FSD Premium"}`,
		`{"timestamp":"2026-11-17T01:05:20Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopThree","SystemAddress":103}`,
		`{"timestamp":"2026-11-17T01:06:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:06:10Z","event":"Location","StarSystem":"HopThree","SystemAddress":103,"StarPos":[30,40,320],"Docked":false}`,
		// Hop 4: Boost wiped by same-system relog before jumping, plus ShipyardSwap reset check
		`{"timestamp":"2026-11-17T01:07:00Z","event":"JetConeBoost","BoostValue":4.0}`,
		`{"timestamp":"2026-11-17T01:07:30Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:07:40Z","event":"Location","StarSystem":"HopThree","SystemAddress":103,"StarPos":[30,40,320],"Docked":false}`,
		`{"timestamp":"2026-11-17T01:08:00Z","event":"Synthesis","Name":"FSD Standard"}`,
		`{"timestamp":"2026-11-17T01:08:10Z","event":"ShipyardSwap","ShipType":"Mandalay","ShipID":12,"StoreOldShip":"Explorer_NX","StoreShipID":1}`,
		`{"timestamp":"2026-11-17T01:08:30Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopFour","SystemAddress":104}`,
		`{"timestamp":"2026-11-17T01:09:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T01:09:10Z","event":"Location","StarSystem":"HopFour","SystemAddress":104,"StarPos":[30,40,380],"Docked":false}`,
	}
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); scanError != nil {
		testingContext.Fatal(scanError)
	}

	eventBus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)

	recovered, recoverError := recoveredJumps.InRange("", "")
	if recoverError != nil {
		testingContext.Fatalf("InRange: %v", recoverError)
	}
	if len(recovered) != 4 {
		testingContext.Fatalf("expected 4 recovered jumps, got %d: %+v", len(recovered), recovered)
	}

	expectedHops := []struct {
		systemName string
		distance   float64
		boostUsed  int
	}{
		{"HopOne", 50.0, 0},
		{"HopTwo", 200.0, 4},
		{"HopThree", 120.0, 3},
		{"HopFour", 60.0, 0},
	}
	for index, expected := range expectedHops {
		got := recovered[index]
		if got.SystemName != expected.systemName || math.Abs(got.JumpDist-expected.distance) > 1e-6 || got.BoostUsed != expected.boostUsed {
			testingContext.Errorf("hop %d: got (%s, %.3f Ly, boost=%d), want (%s, %.3f Ly, boost=%d)",
				index, got.SystemName, got.JumpDist, got.BoostUsed, expected.systemName, expected.distance, expected.boostUsed)
		}
	}

	// Appending a fifth recovered jump and publishing lookback-only events must NOT invalidate RecoveredJumps;
	// only EventLocation completes a candidate and invalidates the projection.
	hopFiveLines := strings.Join([]string{
		`{"timestamp":"2026-11-17T01:10:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopFive","SystemAddress":105}`,
		`{"timestamp":"2026-11-17T01:10:30Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T01:10:40Z","event":"Location","StarSystem":"HopFive","SystemAddress":105,"StarPos":[30,40,420],"Docked":false}`,
	}, "\n") + "\n"
	appendHandle, openAppendError := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if openAppendError != nil {
		testingContext.Fatal(openAppendError)
	}
	if _, writeAppendError := appendHandle.WriteString(hopFiveLines); writeAppendError != nil {
		_ = appendHandle.Close()
		testingContext.Fatal(writeAppendError)
	}
	_ = appendHandle.Close()
	if _, rescanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); rescanError != nil {
		testingContext.Fatal(rescanError)
	}

	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
		journal.EventResurrect,
	))
	cachedRecovered, cachedError := recoveredJumps.InRange("", "")
	if cachedError != nil {
		testingContext.Fatal(cachedError)
	}
	if len(cachedRecovered) != 4 {
		testingContext.Fatalf("expected lookback events not to invalidate RecoveredJumps (want 4 cached), got %d", len(cachedRecovered))
	}

	eventBus.PublishJournal(journal.NewEventSet(journal.EventLocation))
	refreshedRecovered, refreshError := recoveredJumps.InRange("", "")
	if refreshError != nil {
		testingContext.Fatal(refreshError)
	}
	if len(refreshedRecovered) != 5 || refreshedRecovered[4].SystemName != "HopFive" {
		testingContext.Fatalf("expected 5 recovered jumps after EventLocation invalidation, got %+v", refreshedRecovered)
	}

	// Disabling the commander and publishing AppCommanderFilterChanged must suppress recovered jumps.
	if disableError := settings.SetCommanderEnabled(databaseConnection, "CmdrTest", false); disableError != nil {
		testingContext.Fatal(disableError)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)

	disabledRecovered, disabledError := recoveredJumps.InRange("", "")
	if disabledError != nil {
		testingContext.Fatal(disabledError)
	}
	if len(disabledRecovered) != 0 {
		testingContext.Fatalf("expected 0 recovered jumps when commander disabled, got %d", len(disabledRecovered))
	}
}

func TestRecoveredJumpsRejections(testingContext *testing.T) {
	databaseConnection := openProjectionTestDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	journalPath := filepath.Join(temporaryDirectory, "Journal.2026-11-16T190000.01.log")

	lines := []string{
		`{"timestamp":"2026-11-17T02:00:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T02:00:05Z","event":"Location","StarSystem":"StartSys","SystemAddress":200,"StarPos":[0,0,0],"Docked":false}`,
		// Missing StartJump (e.g. multi-machine gap without synced journals) -> rejected
		`{"timestamp":"2026-11-17T02:05:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T02:05:10Z","event":"Location","StarSystem":"OtherPC","SystemAddress":201,"StarPos":[50,0,0],"Docked":false}`,
		// Mismatched StartJump target -> rejected
		`{"timestamp":"2026-11-17T02:06:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"WrongTarget","SystemAddress":999}`,
		`{"timestamp":"2026-11-17T02:07:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T02:07:10Z","event":"Location","StarSystem":"DifferentSys","SystemAddress":202,"StarPos":[100,0,0],"Docked":false}`,
		// Intervening FSDJump after LoadGame before Location disarms prime -> rejected
		`{"timestamp":"2026-11-17T02:08:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"TelepresenceSys","SystemAddress":204}`,
		`{"timestamp":"2026-11-17T02:08:30Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T02:09:00Z","event":"FSDJump","StarSystem":"MidHop","SystemAddress":203,"StarPos":[120,0,0],"JumpDist":20.0}`,
		`{"timestamp":"2026-11-17T02:09:30Z","event":"Location","StarSystem":"TelepresenceSys","SystemAddress":204,"StarPos":[140,0,0],"Docked":false}`,
		// Resurrect respawn -> rejected
		`{"timestamp":"2026-11-17T02:10:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"DetentionSys","SystemAddress":205}`,
		`{"timestamp":"2026-11-17T02:10:15Z","event":"Resurrect","Option":"rebuy","Cost":1000}`,
		`{"timestamp":"2026-11-17T02:10:30Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T02:10:40Z","event":"Location","StarSystem":"DetentionSys","SystemAddress":205,"StarPos":[200,0,0],"Docked":false}`,
		// Offline Fleet Carrier jump (Docked: true) -> rejected
		`{"timestamp":"2026-11-17T02:11:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"CarrierDest","SystemAddress":206}`,
		`{"timestamp":"2026-11-17T02:11:30Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-17T02:11:40Z","event":"Location","StarSystem":"CarrierDest","SystemAddress":206,"StarPos":[500,0,0],"Docked":true,"StationType":"FleetCarrier"}`,
		// Taxi / OnFoot / Suit LoadGame -> rejected
		`{"timestamp":"2026-11-17T02:12:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"ShuttleDest","SystemAddress":207,"Taxi":true}`,
		`{"timestamp":"2026-11-17T02:12:30Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F2","Ship":"ExplorationSuit_Class1"}`,
		`{"timestamp":"2026-11-17T02:12:40Z","event":"Location","StarSystem":"ShuttleDest","SystemAddress":207,"StarPos":[520,0,0],"Docked":false,"OnFoot":true,"Taxi":true}`,
	}
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); scanError != nil {
		testingContext.Fatal(scanError)
	}

	recovered, recoverError := replay.ComputeRecoveredJumpsInRange(databaseConnection, "", "")
	if recoverError != nil {
		testingContext.Fatalf("ComputeRecoveredJumpsInRange: %v", recoverError)
	}
	if len(recovered) != 0 {
		testingContext.Fatalf("expected all non-jump Location transitions to be rejected, got %+v", recovered)
	}
}

