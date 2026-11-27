package projections_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestRecoveredJumpsPositiveAndBoostLifecycle(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
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

	// Disabling the commander and publishing AppCommanderFilterChanged must suppress recovered jumps in AllEnabled while preserving explicit scope.
	commanders, listErr := replay.ListCommanders(databaseConnection)
	if listErr != nil || len(commanders) != 1 {
		testingContext.Fatalf("ListCommanders: %+v err=%v", commanders, listErr)
	}
	if disableError := settings.SetCommanderEnabled(databaseConnection, "CmdrTest", false); disableError != nil {
		testingContext.Fatal(disableError)
	}
	if _, execErr := databaseConnection.Exec(`UPDATE commander_profiles SET enabled = 0 WHERE id = ?`, commanders[0].ID); execErr != nil {
		testingContext.Fatal(execErr)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)

	disabledRecovered, disabledError := recoveredJumps.InRange("", "")
	if disabledError != nil {
		testingContext.Fatal(disabledError)
	}
	if len(disabledRecovered) != 0 {
		testingContext.Fatalf("expected 0 recovered jumps when commander disabled, got %d", len(disabledRecovered))
	}
	scopedRecovered, scopedErr := recoveredJumps.InRangeScoped("", "", replay.NewProfileScope([]int64{commanders[0].ID}))
	if scopedErr != nil || len(scopedRecovered) != 5 {
		testingContext.Fatalf("expected 5 recovered jumps via explicit scope on disabled profile, got %d err=%v", len(scopedRecovered), scopedErr)
	}
}

func TestRecoveredJumpsRejections(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(
		testingContext,
		databaseConnection,
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
	)

	recovered, recoverError := replay.ComputeRecoveredJumpsInRangeScoped(databaseConnection, "", "", replay.ProfileScope{})
	if recoverError != nil {
		testingContext.Fatalf("ComputeRecoveredJumpsInRangeScoped: %v", recoverError)
	}
	if len(recovered) != 0 {
		testingContext.Fatalf("expected all non-jump Location transitions to be rejected, got %+v", recovered)
	}
}

func TestRecoveredCarrierJumpsContinuousStayAndOnFootConcourse(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-11-23T22:00:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-23T22:00:58Z","event":"FSDJump","StarSystem":"OriginSys","SystemAddress":5001,"StarPos":[0,0,0],"JumpDist":50.0}`,
		`{"timestamp":"2026-11-23T22:04:45Z","event":"Docked","StarSystem":"OriginSys","SystemAddress":5001,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		// Hop 1: crashed/offline carrier jump while docked on same carrier (100 Ly)
		`{"timestamp":"2026-11-23T22:30:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-23T22:30:10Z","event":"Location","StarSystem":"CarrierHop1","SystemAddress":5002,"StarPos":[100,0,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		// Same-system relog on carrier concourse must not break stay or double-count
		`{"timestamp":"2026-11-23T22:35:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"ExplorationSuit_Class5"}`,
		`{"timestamp":"2026-11-23T22:35:10Z","event":"Location","StarSystem":"CarrierHop1","SystemAddress":5002,"StarPos":[100,0,0],"Docked":false,"OnFoot":true,"BodyType":"Star"}`,
		// Hop 2: chained offline jump logging in on foot in carrier concourse (150 Ly)
		`{"timestamp":"2026-11-23T23:00:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"ExplorationSuit_Class5"}`,
		`{"timestamp":"2026-11-23T23:00:10Z","event":"Location","StarSystem":"CarrierHop2","SystemAddress":5003,"StarPos":[250,0,0],"Docked":false,"OnFoot":true,"BodyType":"Star"}`,
		// Hop 3: second consecutive on-foot concourse offline jump (200 Ly)
		`{"timestamp":"2026-11-23T23:30:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"ExplorationSuit_Class5"}`,
		`{"timestamp":"2026-11-23T23:30:10Z","event":"Location","StarSystem":"CarrierHop3","SystemAddress":5004,"StarPos":[450,0,0],"Docked":false,"OnFoot":true,"BodyType":"Planet"}`,
		// Hop 4: re-embarked into ship before offline jump on same carrier (50 Ly)
		`{"timestamp":"2026-11-24T00:00:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-24T00:00:10Z","event":"Location","StarSystem":"CarrierHop4","SystemAddress":5005,"StarPos":[500,0,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		// Online on-foot CarrierJump (omits MarketID) followed by Hop 5 offline jump docked on same carrier (75 Ly)
		`{"timestamp":"2026-11-24T00:20:00Z","event":"CarrierJump","StarSystem":"CarrierOnline5","SystemAddress":5006,"StarPos":[500,100,0],"Docked":false,"OnFoot":true,"BodyType":"Star"}`,
		`{"timestamp":"2026-11-24T00:45:00Z","event":"LoadGame","Commander":"CmdrCarrier","FID":"F30","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-24T00:45:10Z","event":"Location","StarSystem":"CarrierHop5","SystemAddress":5007,"StarPos":[500,175,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
	)

	recovered, recoverError := replay.ComputeRecoveredJumpsScoped(databaseConnection, replay.ProfileScope{})
	if recoverError != nil {
		testingContext.Fatalf("ComputeRecoveredJumpsScoped: %v", recoverError)
	}
	expectedHops := []struct {
		systemName string
		distance   float64
	}{
		{"CarrierHop1", 100.0},
		{"CarrierHop2", 150.0},
		{"CarrierHop3", 200.0},
		{"CarrierHop4", 50.0},
		{"CarrierHop5", 75.0},
	}
	if len(recovered) != len(expectedHops) {
		testingContext.Fatalf("expected %d recovered carrier jumps, got %d: %+v", len(expectedHops), len(recovered), recovered)
	}
	for index, expected := range expectedHops {
		got := recovered[index]
		if !got.IsCarrier || got.SystemName != expected.systemName || math.Abs(got.JumpDist-expected.distance) > 1e-6 || got.BoostUsed != 0 {
			testingContext.Errorf("carrier hop %d: got %+v, want IsCarrier=true system=%s dist=%.1f boost=0",
				index, got, expected.systemName, expected.distance)
		}
	}
}

func TestRecoveredCarrierJumpsRejections(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-11-23T18:00:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-23T18:00:05Z","event":"Location","StarSystem":"SysStart","SystemAddress":6001,"StarPos":[0,0,0],"Docked":true,"StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
		// Undocked before logging in on same carrier in another system -> rejected
		`{"timestamp":"2026-11-23T18:05:00Z","event":"Undocked","StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
		`{"timestamp":"2026-11-23T18:09:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-23T18:10:00Z","event":"Location","StarSystem":"SysAfterUndock","SystemAddress":6002,"StarPos":[100,0,0],"Docked":true,"StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
		// Resurrect respawn onto carrier in another system -> rejected
		`{"timestamp":"2026-11-23T18:15:00Z","event":"Resurrect","Option":"rebuy","Cost":1000}`,
		`{"timestamp":"2026-11-23T18:15:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-23T18:16:00Z","event":"Location","StarSystem":"SysAfterResurrect","SystemAddress":6003,"StarPos":[200,0,0],"Docked":true,"StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
		// Mismatched carrier MarketID -> rejected
		`{"timestamp":"2026-11-23T18:19:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-23T18:20:00Z","event":"Location","StarSystem":"SysWrongCarrier","SystemAddress":6004,"StarPos":[300,0,0],"Docked":true,"StationName":"BBB-222","StationType":"FleetCarrier","MarketID":222}`,
		// Docked at a non-carrier station clears carrier stay -> rejected
		`{"timestamp":"2026-11-23T18:22:00Z","event":"Docked","StarSystem":"SysWrongCarrier","SystemAddress":6004,"StationName":"Jameson Memorial","StationType":"Orbis","MarketID":333}`,
		`{"timestamp":"2026-11-23T18:24:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-23T18:25:00Z","event":"Location","StarSystem":"SysAfterStarport","SystemAddress":6005,"StarPos":[400,0,0],"Docked":true,"StationName":"BBB-222","StationType":"FleetCarrier","MarketID":222}`,
		// On-foot Station concourse (e.g. escape pod destination) -> rejected
		`{"timestamp":"2026-11-23T18:29:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"ExplorationSuit_Class1"}`,
		`{"timestamp":"2026-11-23T18:30:00Z","event":"Location","StarSystem":"SysStationConcourse","SystemAddress":6006,"StarPos":[500,0,0],"Docked":false,"OnFoot":true,"BodyType":"Station"}`,
		// Re-board carrier then appear on foot on a planet surface (DistFromStarLS present) -> rejected
		`{"timestamp":"2026-11-23T18:35:00Z","event":"Docked","StarSystem":"SysStationConcourse","SystemAddress":6006,"StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
		`{"timestamp":"2026-11-23T18:39:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"ExplorationSuit_Class1"}`,
		`{"timestamp":"2026-11-23T18:40:00Z","event":"Location","StarSystem":"SysPlanetSurface","SystemAddress":6007,"StarPos":[600,0,0],"Docked":false,"OnFoot":true,"BodyType":"Planet","DistFromStarLS":512.5}`,
		// Missing intermediate jump before Docked (Docked system differs from previousArrival) -> rejected
		`{"timestamp":"2026-11-23T18:45:00Z","event":"Docked","StarSystem":"SysMissingArrival","SystemAddress":6008,"StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
		`{"timestamp":"2026-11-23T18:49:50Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F31","Ship":"Mandalay"}`,
		`{"timestamp":"2026-11-23T18:50:00Z","event":"Location","StarSystem":"SysAfterGap","SystemAddress":6009,"StarPos":[700,0,0],"Docked":true,"StationName":"AAA-111","StationType":"FleetCarrier","MarketID":111}`,
	)

	recovered, recoverError := replay.ComputeRecoveredJumpsScoped(databaseConnection, replay.ProfileScope{})
	if recoverError != nil {
		testingContext.Fatalf("ComputeRecoveredJumpsScoped: %v", recoverError)
	}
	if len(recovered) != 0 {
		testingContext.Fatalf("expected all invalid carrier Location transitions to be rejected, got %+v", recovered)
	}
}

func TestRecoveredJumpsMidJumpRenameAndSameNameWipeIsolation(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		// Mid-jump character rename under same FID F900 -> must recover jump with BoostUsed=4.
		"Journal.2026-11-24T010000.01.log": {
			`{"timestamp":"2026-11-24T01:00:00Z","event":"LoadGame","Commander":"CmdrBeforeRename","FID":"F900","Ship":"Mandalay"}`,
			`{"timestamp":"2026-11-24T01:00:05Z","event":"Location","StarSystem":"RenameOrigin","SystemAddress":9001,"StarPos":[0,0,0],"Docked":false}`,
			`{"timestamp":"2026-11-24T01:01:00Z","event":"JetConeBoost","BoostValue":4.0}`,
			`{"timestamp":"2026-11-24T01:01:30Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"RenameDest","SystemAddress":9002}`,
		},
		// Interleaved second profile disconnecting, reconnecting, and completing a normal boosted jump between F900's StartJump and reconnect.
		"Journal.2026-11-24T010200.01.log": {
			`{"timestamp":"2026-11-24T01:02:00Z","event":"LoadGame","Commander":"CmdrInterleaved","FID":"F902","Ship":"Krait_Light"}`,
			`{"timestamp":"2026-11-24T01:02:05Z","event":"Location","StarSystem":"InterleavedOrigin","SystemAddress":9201,"StarPos":[0,0,0],"Docked":false}`,
			`{"timestamp":"2026-11-24T01:02:20Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"InterleavedDest","SystemAddress":9202}`,
			`{"timestamp":"2026-11-24T01:03:00Z","event":"LoadGame","Commander":"CmdrInterleaved","FID":"F902","Ship":"Krait_Light"}`,
			`{"timestamp":"2026-11-24T01:03:10Z","event":"Location","StarSystem":"InterleavedDest","SystemAddress":9202,"StarPos":[0,0,90],"Docked":false}`,
			`{"timestamp":"2026-11-24T01:03:30Z","event":"Synthesis","Name":"FSD Standard"}`,
			`{"timestamp":"2026-11-24T01:03:45Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"NormalBoostedDest","SystemAddress":9203}`,
			`{"timestamp":"2026-11-24T01:04:00Z","event":"FSDJump","StarSystem":"NormalBoostedDest","SystemAddress":9203,"StarPos":[0,0,150],"JumpDist":60.0,"BoostUsed":2}`,
		},
		"Journal.2026-11-24T010500.01.log": {
			`{"timestamp":"2026-11-24T01:05:00Z","event":"LoadGame","Commander":"CmdrAfterRename","FID":"F900","Ship":"Mandalay"}`,
			`{"timestamp":"2026-11-24T01:05:10Z","event":"Location","StarSystem":"RenameDest","SystemAddress":9002,"StarPos":[0,150,0],"Docked":false}`,
		},
		// Same-name Clear Save under FID F901 -> Era 0 StartJump must NEVER match Era 1 LoadGame/Location.
		"Journal.2026-11-24T020000.01.log": {
			`{"timestamp":"2026-11-24T02:00:00Z","event":"LoadGame","Commander":"CmdrSameWipe","FID":"F901","Ship":"Mandalay"}`,
			`{"timestamp":"2026-11-24T02:00:05Z","event":"Location","StarSystem":"WipeOrigin","SystemAddress":9101,"StarPos":[0,0,0],"Docked":false}`,
			`{"timestamp":"2026-11-24T02:01:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"WipeDest","SystemAddress":9102}`,
			`{"timestamp":"2026-11-24T02:02:00Z","event":"ClearSavedGame","Name":"CmdrSameWipe","FID":"F901"}`,
			`{"timestamp":"2026-11-24T02:02:10Z","event":"NewCommander","Name":"CmdrSameWipe","FID":"F901","Package":"Sidewinder"}`,
			`{"timestamp":"2026-11-24T02:02:20Z","event":"LoadGame","Commander":"CmdrSameWipe","FID":"F901","Ship":"SideWinder"}`,
			`{"timestamp":"2026-11-24T02:02:30Z","event":"Location","StarSystem":"WipeDest","SystemAddress":9102,"StarPos":[0,80,0],"Docked":false}`,
		},
	})

	allRecovered, err := replay.ComputeRecoveredJumpsAll(databaseConnection)
	if err != nil {
		testingContext.Fatalf("ComputeRecoveredJumpsAll: %v", err)
	}
	if len(allRecovered) != 2 {
		testingContext.Fatalf("expected interleaved and renamed-pilot jumps to recover (wipe jump rejected), got %+v", allRecovered)
	}
	interleavedJump, renamedJump := allRecovered[0], allRecovered[1]
	if interleavedJump.SystemName != "InterleavedDest" || math.Abs(interleavedJump.JumpDist-90.0) > 1e-6 || interleavedJump.BoostUsed != 0 || interleavedJump.ProfileID <= 0 {
		testingContext.Fatalf("unexpected interleaved recovered jump: %+v", interleavedJump)
	}
	if renamedJump.SystemName != "RenameDest" || math.Abs(renamedJump.JumpDist-150.0) > 1e-6 || renamedJump.BoostUsed != 4 || renamedJump.ProfileID <= 0 || renamedJump.ProfileID == interleavedJump.ProfileID {
		testingContext.Fatalf("unexpected recovered jump across mid-jump rename after interleaved candidate: %+v", renamedJump)
	}
	f902Scoped, f902ScopedError := replay.ComputeRecoveredJumpsScoped(databaseConnection, replay.NewProfileScope([]int64{interleavedJump.ProfileID}))
	if f902ScopedError != nil || len(f902Scoped) != 1 || f902Scoped[0].SystemName != "InterleavedDest" {
		testingContext.Fatalf("expected F902 normal boosted FSDJump excluded from recovered jumps, got %+v err=%v", f902Scoped, f902ScopedError)
	}
}
