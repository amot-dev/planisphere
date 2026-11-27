package projections_test

import (
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

func TestCareerStartAndCommanderFilter(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrFirst","FID":"F1","Ship":"Mandalay"}`,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":5.0}`,
		`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Commander":"CmdrSecond","FID":"F2","Ship":"Anaconda"}`,
	)

	var firstProfileID int64
	if scanErr := databaseConnection.QueryRow(`SELECT id FROM commander_profiles WHERE display_name = 'CmdrFirst'`).Scan(&firstProfileID); scanErr != nil {
		testingContext.Fatalf("lookup CmdrFirst profile ID: %v", scanErr)
	}

	eventBus := projection.NewBus()
	systemArrivals := projections.NewSystemArrivals(databaseConnection, eventBus)
	shipHistory := projections.NewShipHistory(databaseConnection, eventBus)
	visitCounts := projections.NewSystemVisitCounts(databaseConnection, eventBus, nil)
	if !systemArrivals.Triggers().Journal.Has(journal.EventFSDJump) || !shipHistory.Triggers().Journal.Has(journal.EventShipyardSwap) || !visitCounts.Triggers().Journal.Has(journal.EventLocation) {
		testingContext.Fatalf("unexpected projection triggers: arrivals=%v ship=%v visits=%v",
			systemArrivals.Triggers(), shipHistory.Triggers(), visitCounts.Triggers())
	}
	if initialArrivals, initErr := systemArrivals.InWindow("2026-05-01T00:00:00Z", "2026-05-03T00:00:00Z"); initErr != nil || len(initialArrivals) != 1 {
		testingContext.Fatalf("expected 1 warm arrival before commander filter change, got %+v err=%v", initialArrivals, initErr)
	}

	if err := settings.SetCommanderEnabled(databaseConnection, "CmdrFirst", false); err != nil {
		testingContext.Fatal(err)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)

	filteredArrivals, filteredErr := systemArrivals.InWindow("2026-05-01T00:00:00Z", "2026-05-03T00:00:00Z")
	if filteredErr != nil || len(filteredArrivals) != 0 {
		testingContext.Fatalf("expected 0 default arrivals after disabling CmdrFirst, got %+v err=%v", filteredArrivals, filteredErr)
	}

	// Explicit ProfileScope on disabled CmdrFirst still resolves its arrivals, visit counts, and ship timeline.
	firstScope := replay.NewProfileScope([]int64{firstProfileID})
	scopedCounts, scopedCountsErr := visitCounts.CountsForSystemsScoped([]string{"Sol", "Missing"}, firstScope)
	if scopedCountsErr != nil || scopedCounts["sol"] != 1 || scopedCounts["missing"] != 0 {
		testingContext.Fatalf("unexpected scoped visit counts for disabled CmdrFirst: %+v err=%v", scopedCounts, scopedCountsErr)
	}
	scopedArrivals, scopedArrErr := systemArrivals.InWindowScoped("2026-05-01T00:00:00Z", "2026-05-03T00:00:00Z", firstScope)
	if scopedArrErr != nil || len(scopedArrivals) != 1 || scopedArrivals[0].SystemName != "Sol" || scopedArrivals[0].ProfileID != firstProfileID {
		testingContext.Fatalf("unexpected scoped arrivals for disabled CmdrFirst: %+v err=%v", scopedArrivals, scopedArrErr)
	}
	scopedTimeline, scopedShipErr := shipHistory.TimelineScoped("2026-05-01T00:00:00Z", "2026-05-03T00:00:00Z", firstScope)
	if scopedShipErr != nil || len(scopedTimeline) != 1 || scopedTimeline[0].HullName != "Mandalay" {
		testingContext.Fatalf("unexpected scoped ship timeline for disabled CmdrFirst: %+v err=%v", scopedTimeline, scopedShipErr)
	}
}

func TestSystemArrivalsAndVisitCountsSelectiveInvalidation(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Commander":"CmdrNav","FID":"F3","Ship":"Mandalay"}`,
		`{"timestamp":"2026-05-02T10:01:00Z","event":"Location","StarSystem":"ParkSys","SystemAddress":3001,"StarPos":[0,0,0],"Docked":false}`,
		`{"timestamp":"2026-05-02T10:05:00Z","event":"FSDJump","StarSystem":"WaySys","SystemAddress":3002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	)

	eventBus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	systemArrivals := projections.NewSystemArrivals(databaseConnection, eventBus)
	visitCounts := projections.NewSystemVisitCounts(databaseConnection, eventBus, recoveredJumps)

	arrivals, arrivalsError := systemArrivals.InWindow("2026-05-02T10:00:00Z", "")
	if arrivalsError != nil || len(arrivals) != 2 {
		testingContext.Fatalf("InWindow: len=%d err=%v", len(arrivals), arrivalsError)
	}
	_, currentSystem, locationError := replay.QueryLatestPlayerLocation(databaseConnection)
	if locationError != nil || currentSystem != "WaySys" {
		testingContext.Fatalf("QueryLatestPlayerLocation = %q err=%v", currentSystem, locationError)
	}

	counts, countsError := visitCounts.CountsForSystems([]string{"  parkSys ", "WaySys", "UnknownSys"})
	if countsError != nil {
		testingContext.Fatalf("CountsForSystems: %v", countsError)
	}
	if counts["parksys"] != 1 || counts["waysys"] != 1 || counts["unknownsys"] != 0 {
		testingContext.Fatalf("unexpected visit counts: %+v", counts)
	}

	// Publishing non-arrival and disconnect-lookback events must NOT invalidate arrivals or visit counts.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T10:10:00Z","event":"FSDJump","StarSystem":"WaySys","SystemAddress":3002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	)
	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventScan,
		journal.EventLoadGame,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
		journal.EventResurrect,
	))
	stillCachedArrivals, cachedArrErr := systemArrivals.InWindow("2026-05-02T10:00:00Z", "")
	if cachedArrErr != nil || len(stillCachedArrivals) != 2 {
		testingContext.Fatalf("expected non-arrival events not to invalidate SystemArrivals, got len=%d err=%v", len(stillCachedArrivals), cachedArrErr)
	}
	stillCachedCounts, cachedCountsErr := visitCounts.CountsForSystems([]string{"WaySys"})
	if cachedCountsErr != nil || stillCachedCounts["waysys"] != 1 {
		testingContext.Fatalf("expected non-arrival events not to invalidate SystemVisitCounts, got %d err=%v", stillCachedCounts["waysys"], cachedCountsErr)
	}

	// Publishing EventFSDJump invalidates both SystemArrivals and SystemVisitCounts.
	eventBus.PublishJournal(journal.NewEventSet(journal.EventFSDJump))
	refreshedArrivals, refreshedArrErr := systemArrivals.InWindow("2026-05-02T10:00:00Z", "")
	if refreshedArrErr != nil || len(refreshedArrivals) != 3 {
		testingContext.Fatalf("expected refreshed SystemArrivals len=3, got %d err=%v", len(refreshedArrivals), refreshedArrErr)
	}
	refreshedCounts, refreshedCountsErr := visitCounts.CountsForSystems([]string{"WaySys"})
	if refreshedCountsErr != nil || refreshedCounts["waysys"] != 2 {
		testingContext.Fatalf("expected WaySys visit count 2 after refresh, got %d err=%v", refreshedCounts["waysys"], refreshedCountsErr)
	}
}

func TestShipHistorySuitFilteringAndSwapCustomNameLookup(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	eventBus := projection.NewBus()
	shipHistory := projections.NewShipHistory(databaseConnection, eventBus)

	// Log in Caspian Explorer ("Siderite Skillet", ShipID 7), then Mandalay ("Dust Devil", ShipID 9), followed by suit and SRV relogs that must be ignored.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Ship":"Explorer_NX","ShipID":7,"ShipName":"Siderite Skillet"}`,
		`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Ship":"Mandalay","ShipID":9,"ShipName":"Dust Devil"}`,
		`{"timestamp":"2026-05-03T10:00:00Z","event":"LoadGame","Ship":"ExplorationSuit_Class5","Ship_Localised":"$ExplorationSuit_Class1_Name;","ShipID":9}`,
		`{"timestamp":"2026-05-03T10:05:00Z","event":"LoadGame","Ship":"TestBuggy","Ship_Localised":"SRV Scarab","ShipID":9}`,
	)

	currentBeforeSwap, shipError := replay.QueryCurrentShip(databaseConnection)
	if shipError != nil {
		testingContext.Fatalf("QueryCurrentShip before swap: %v", shipError)
	}
	if currentBeforeSwap != "Mandalay \u201cDust Devil\u201d" {
		testingContext.Fatalf("CurrentShip = %q, want %q", currentBeforeSwap, "Mandalay \u201cDust Devil\u201d")
	}

	// Swap back to ShipID 7 via ShipyardSwap (which omits ShipName); header must recover "Siderite Skillet".
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-04T10:00:00Z","event":"ShipyardSwap","ShipType":"explorer_nx","ShipID":7,"StoreOldShip":"Mandalay","StoreShipID":9}`,
	)

	currentAfterSwap, swapShipError := replay.QueryCurrentShip(databaseConnection)
	if swapShipError != nil {
		testingContext.Fatalf("QueryCurrentShip after swap: %v", swapShipError)
	}
	if currentAfterSwap != "Caspian Explorer \u201cSiderite Skillet\u201d" {
		testingContext.Fatalf("CurrentShip after swap = %q, want %q", currentAfterSwap, "Caspian Explorer \u201cSiderite Skillet\u201d")
	}

	// Timeline and leg transitions remain hull-only (preserving baseline display),
	// while QueryJumpTimestampsScoped excludes Taxi and Multicrew FSDJump events.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-03T11:00:00Z","event":"FSDJump","StarSystem":"TaxiSys","JumpDist":12.0,"Taxi":true}`,
		`{"timestamp":"2026-05-03T11:30:00Z","event":"FSDJump","StarSystem":"CrewSys","JumpDist":15.0,"Multicrew":true}`,
		`{"timestamp":"2026-05-03T12:00:00Z","event":"FSDJump","StarSystem":"PilotedOne","JumpDist":40.0}`,
		`{"timestamp":"2026-05-04T12:00:00Z","event":"FSDJump","StarSystem":"PilotedTwo","JumpDist":75.0}`,
	)

	timeline, timelineError := shipHistory.Timeline("2026-05-02T09:00:00Z", "2026-05-05T00:00:00Z")
	if timelineError != nil {
		testingContext.Fatalf("Timeline: %v", timelineError)
	}
	jumpTimestamps, jumpQueryError := replay.QueryJumpTimestampsScoped(databaseConnection, "2026-05-02T09:00:00Z", "2026-05-05T00:00:00Z", nil, replay.ProfileScope{})
	if jumpQueryError != nil {
		testingContext.Fatalf("QueryJumpTimestampsScoped: %v", jumpQueryError)
	}
	if shuttleOnly := replay.ExtractShipsUsedOnLeg(timeline, jumpTimestamps, "2026-05-03T10:30:00Z", "2026-05-03T11:45:00Z"); len(shuttleOnly) != 0 {
		testingContext.Fatalf("expected no vessels flown on shuttle/multicrew-only leg, got %+v", shuttleOnly)
	}
	transitions := replay.ExtractShipsUsedOnLeg(timeline, jumpTimestamps, "2026-05-02T09:00:00Z", "2026-05-05T00:00:00Z")
	if len(transitions) != 2 || transitions[0].To != "Mandalay" || transitions[1].To != "Caspian Explorer" {
		testingContext.Fatalf("unexpected leg transitions (must stay hull-only): %+v", transitions)
	}
}

func TestCarrierStateIdentityFilteringAcrossAllCarrierEvents(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	eventBus := projection.NewBus()
	carrierState := projections.NewCarrierState(databaseConnection, eventBus)
	if !carrierState.Triggers().Journal.Has(journal.EventCarrierStats) || carrierState.IsValid() || carrierState.Generation() != 0 {
		testingContext.Fatalf("unexpected cold CarrierState contract: triggers=%v valid=%v gen=%d",
			carrierState.Triggers(), carrierState.IsValid(), carrierState.Generation())
	}

	// Without any CarrierStats, unlabelled Location matches Location/CarrierJump consistently.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T09:00:00Z","event":"CarrierJump","StarSystem":"OriginSys"}`,
	)
	initialSnapshot, snapshotError := carrierState.LatestStats()
	if snapshotError != nil || !carrierState.IsValid() {
		testingContext.Fatalf("initial LatestStats: valid=%v err=%v", carrierState.IsValid(), snapshotError)
	}
	genBeforeStats := carrierState.Generation()
	initialLoc, initialErr := replay.QueryCarrierLastLocation(databaseConnection, initialSnapshot)
	if initialErr != nil || initialLoc != "OriginSys" {
		testingContext.Fatalf("initial LastLocation = %q err=%v, want OriginSys", initialLoc, initialErr)
	}

	// Record personal carrier identity (CarrierID 3701199360, K1V-6QB) and fuel state, plus foreign carrier events that must be ignored.
	journaltest.PublishParsedLines(
		testingContext,
		databaseConnection,
		eventBus,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"CarrierStats","CarrierID":3701199360,"Callsign":"K1V-6QB","Name":"VANCOUVER","FuelLevel":750,"SpaceUsage":{"TotalCapacity":25000,"FreeSpace":15000,"Cargo":120}}`,
		`{"timestamp":"2026-05-01T11:00:00Z","event":"CarrierJump","StarSystem":"OwnedCarrierSys","StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		`{"timestamp":"2026-05-02T12:00:00Z","event":"CarrierJump","StarSystem":"ForeignDestSys","StationName":"D9Z-111","StationType":"FleetCarrier","MarketID":3709999999}`,
		`{"timestamp":"2026-05-02T12:05:00Z","event":"CarrierJumpRequest","SystemName":"ForeignReqSys","CarrierID":3709999999}`,
		`{"timestamp":"2026-05-02T12:10:00Z","event":"CarrierDepositFuel","CarrierID":3709999999,"Amount":200,"Total":999}`,
	)

	ownedSnapshot, ownedSnapshotError := carrierState.LatestStats()
	if ownedSnapshotError != nil || !ownedSnapshot.Found || carrierState.Generation() <= genBeforeStats {
		testingContext.Fatalf("LatestStats after CarrierStats: snapshot=%+v gen=%d err=%v", ownedSnapshot, carrierState.Generation(), ownedSnapshotError)
	}
	lastLoc, locError := replay.QueryCarrierLastLocation(databaseConnection, ownedSnapshot)
	if locError != nil || lastLoc != "OwnedCarrierSys" {
		testingContext.Fatalf("LastLocation = %q err=%v, want OwnedCarrierSys (foreign carrier ignored)", lastLoc, locError)
	}
	depotInfo, depotError := replay.QueryCarrierDepotInfo(databaseConnection, ownedSnapshot)
	if depotError != nil {
		testingContext.Fatalf("DepotInfo: %v", depotError)
	}
	if depotInfo.UsedCapacity != 10000 || depotInfo.DepotTritium != 750 || depotInfo.Cargo != 120 {
		testingContext.Fatalf("DepotInfo = %+v, want UsedCapacity=10000 DepotTritium=750 Cargo=120", depotInfo)
	}

	// Own carrier deposit overrides DepotTritium when timestamp >= latest CarrierStats.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T13:00:00Z","event":"CarrierDepositFuel","CarrierID":3701199360,"Amount":150,"Total":900}`,
	)
	updatedDepot, updatedDepotErr := replay.QueryCarrierDepotInfo(databaseConnection, ownedSnapshot)
	if updatedDepotErr != nil || updatedDepot.DepotTritium != 900 {
		testingContext.Fatalf("updated DepotInfo = %+v err=%v, want DepotTritium=900", updatedDepot, updatedDepotErr)
	}
}
