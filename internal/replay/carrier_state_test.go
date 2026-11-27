package replay_test

import (
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestCarrierStateStatsLocationAndDepotResolution(testingContext *testing.T) {
	conn := dbtest.OpenMemory(testingContext)

	// Empty archive returns Found=false and default depot capacity/tritium values.
	emptySnapshot, emptyStatsErr := replay.LoadLatestCarrierStats(conn)
	if emptyStatsErr != nil || emptySnapshot.Found {
		testingContext.Fatalf("expected empty CarrierStatsSnapshot, got %+v err=%v", emptySnapshot, emptyStatsErr)
	}
	defaultDepot, defaultDepotErr := replay.QueryCarrierDepotInfo(conn, emptySnapshot)
	if defaultDepotErr != nil || defaultDepot.UsedCapacity != 5000 || defaultDepot.DepotTritium != 1000 || defaultDepot.Cargo != 0 {
		testingContext.Fatalf("unexpected default depot info: %+v err=%v", defaultDepot, defaultDepotErr)
	}
	emptyLoc, emptyLocErr := replay.QueryCarrierLastLocation(conn, emptySnapshot)
	if emptyLocErr != nil || emptyLoc != "" {
		testingContext.Fatalf("expected empty carrier location on empty archive, got %q err=%v", emptyLoc, emptyLocErr)
	}

	// Direct identity helper checks ID match, callsign match when ID is absent, and wildcard fallback.
	if !replay.MatchesTargetCarrier(370100, "K1V-6QB", 370100, "OTHER") {
		testingContext.Fatal("expected exact CarrierID match")
	}
	if replay.MatchesTargetCarrier(370100, "K1V-6QB", 370999, "K1V-6QB") {
		testingContext.Fatal("expected mismatched CarrierID to reject even if callsign matches")
	}
	if !replay.MatchesTargetCarrier(370100, "K1V-6QB", 0, "Carrier k1v-6qb") || replay.MatchesTargetCarrier(370100, "K1V-6QB", 0, "Carrier Z9Z-000") {
		testingContext.Fatal("unexpected callsign fallback matching behavior")
	}

	// Seed CarrierStats plus CarrierJump, undocked Location (ignored), callsign-matched Docked, foreign events, and newer CarrierJumpRequest.
	journaltest.InsertParsedLines(
		testingContext,
		conn,
		`{"timestamp":"2026-05-01T09:00:00Z","event":"CarrierDepositFuel","CarrierID":370100,"Amount":100,"Total":600}`,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"CarrierStats","CarrierID":370100,"Callsign":"K1V-6QB","Name":"HORIZON","FuelLevel":725,"SpaceUsage":{"TotalCapacity":25000,"FreeSpace":16500,"Cargo":340}}`,
		`{"timestamp":"2026-05-01T11:00:00Z","event":"CarrierJump","StarSystem":"JumpSys","StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":370100}`,
		`{"timestamp":"2026-05-01T12:00:00Z","event":"Location","StarSystem":"UndockedCarrierSys","StationName":"K1V-6QB","StationType":"FleetCarrier","Docked":false}`,
		`{"timestamp":"2026-05-01T12:30:00Z","event":"Docked","StarSystem":"DockedCarrierSys","StationName":"HORIZON K1V-6QB","StationType":"FleetCarrier"}`,
		`{"timestamp":"2026-05-01T13:00:00Z","event":"Docked","StarSystem":"ForeignDockedSys","StationName":"Z9Z-000","StationType":"FleetCarrier","MarketID":370999}`,
	)

	loadedSnapshot, statsErr := replay.LoadLatestCarrierStats(conn)
	if statsErr != nil || !loadedSnapshot.Found || loadedSnapshot.Event.CarrierID != 370100 {
		testingContext.Fatalf("LoadLatestCarrierStats = %+v err=%v", loadedSnapshot, statsErr)
	}

	dockedLoc, dockedLocErr := replay.QueryCarrierLastLocation(conn, loadedSnapshot)
	if dockedLocErr != nil || dockedLoc != "DockedCarrierSys" {
		testingContext.Fatalf("expected DockedCarrierSys from callsign-matched Docked event, got %q err=%v", dockedLoc, dockedLocErr)
	}

	// Newer matching CarrierJumpRequest supersedes earlier Docked and CarrierJump timestamps.
	journaltest.InsertParsedLines(
		testingContext,
		conn,
		`{"timestamp":"2026-05-01T14:00:00Z","event":"CarrierJumpRequest","SystemName":"RequestedSys","CarrierID":370100}`,
		`{"timestamp":"2026-05-01T14:05:00Z","event":"CarrierDepositFuel","CarrierID":370100,"Amount":175,"Total":900}`,
	)

	requestLoc, requestLocErr := replay.QueryCarrierLastLocation(conn, loadedSnapshot)
	if requestLocErr != nil || requestLoc != "RequestedSys" {
		testingContext.Fatalf("expected RequestedSys from latest CarrierJumpRequest, got %q err=%v", requestLoc, requestLocErr)
	}

	depotInfo, depotErr := replay.QueryCarrierDepotInfo(conn, loadedSnapshot)
	if depotErr != nil || depotInfo.UsedCapacity != 8500 || depotInfo.DepotTritium != 900 || depotInfo.Cargo != 340 {
		testingContext.Fatalf("unexpected depot info after CarrierDepositFuel: %+v err=%v", depotInfo, depotErr)
	}
}
