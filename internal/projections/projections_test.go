package projections_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/sources/journal"
)

func insertJournalRow(testingContext *testing.T, database *sql.DB, timestamp, eventName, systemName, keyIdentifier, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, keyIdentifier)
	_, execError := database.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, journal.NullStringOrNil(systemName), rawJSON, timestamp,
	)
	if execError != nil {
		testingContext.Fatalf("insert %s row: %v", eventName, execError)
	}
}

func TestCommanderRosterCachingAndFilterInvalidation(testingContext *testing.T) {
	databaseConnection := openProjectionTestDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	journalPath := filepath.Join(temporaryDirectory, "Journal.2026-05-01T000000.01.log")
	lines := []string{
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrFirst","FID":"F1","Ship":"Mandalay"}`,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":5.0}`,
		`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Commander":"CmdrSecond","FID":"F2","Ship":"Anaconda"}`,
	}
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); scanError != nil {
		testingContext.Fatal(scanError)
	}

	eventBus := projection.NewBus()
	roster := projections.NewCommanderRoster(databaseConnection, eventBus)

	commanders, listError := roster.Commanders()
	if listError != nil || len(commanders) != 2 {
		testingContext.Fatalf("Commanders: len=%d err=%v", len(commanders), listError)
	}
	careerStart, _, startError := roster.CareerStart()
	if startError != nil || careerStart != "2026-05-01T10:00:00Z" {
		testingContext.Fatalf("CareerStart = %q err=%v, want 2026-05-01T10:00:00Z", careerStart, startError)
	}

	// Disabling CmdrFirst without publishing keeps cached CareerStart until AppCommanderFilterChanged fires.
	if err := journal.SetCommanderEnabled(databaseConnection, "CmdrFirst", false); err != nil {
		testingContext.Fatal(err)
	}
	cachedStart, _, _ := roster.CareerStart()
	if cachedStart != "2026-05-01T10:00:00Z" {
		testingContext.Fatalf("expected cached CareerStart before invalidation, got %q", cachedStart)
	}

	eventBus.PublishApp(events.AppCommanderFilterChanged)
	updatedStart, _, _ := roster.CareerStart()
	if updatedStart != "2026-05-02T10:00:00Z" {
		testingContext.Fatalf("expected updated CareerStart 2026-05-02T10:00:00Z, got %q", updatedStart)
	}
}

func TestSystemArrivalsAndVisitCountsSelectiveInvalidation(testingContext *testing.T) {
	databaseConnection := openProjectionTestDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	journalPath := filepath.Join(temporaryDirectory, "Journal.2026-05-02T000000.01.log")
	lines := []string{
		`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Commander":"CmdrNav","FID":"F3","Ship":"Mandalay"}`,
		`{"timestamp":"2026-05-02T10:01:00Z","event":"Location","StarSystem":"ParkSys","SystemAddress":3001,"StarPos":[0,0,0],"Docked":false}`,
		`{"timestamp":"2026-05-02T10:05:00Z","event":"FSDJump","StarSystem":"WaySys","SystemAddress":3002,"StarPos":[10,0,0],"JumpDist":10.0}`,
	}
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); scanError != nil {
		testingContext.Fatal(scanError)
	}

	eventBus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	systemArrivals := projections.NewSystemArrivals(databaseConnection, eventBus)
	visitCounts := projections.NewSystemVisitCounts(databaseConnection, eventBus, recoveredJumps)

	arrivals, arrivalsError := systemArrivals.InWindow("2026-05-02T10:00:00Z", "")
	if arrivalsError != nil || len(arrivals) != 2 {
		testingContext.Fatalf("InWindow: len=%d err=%v", len(arrivals), arrivalsError)
	}
	_, currentSystem, locationError := systemArrivals.LatestPlayerLocation()
	if locationError != nil || currentSystem != "WaySys" {
		testingContext.Fatalf("LatestPlayerLocation = %q err=%v", currentSystem, locationError)
	}

	counts, countsError := visitCounts.CountsForSystems([]string{"  parkSys ", "WaySys", "UnknownSys"})
	if countsError != nil {
		testingContext.Fatalf("CountsForSystems: %v", countsError)
	}
	if counts["parksys"] != 1 || counts["waysys"] != 1 || counts["unknownsys"] != 0 {
		testingContext.Fatalf("unexpected visit counts: %+v", counts)
	}

	// Publishing non-arrival and disconnect-lookback events must NOT invalidate arrivals or visit counts.
	insertJournalRow(testingContext, databaseConnection, "2026-05-02T10:10:00Z", "FSDJump", "WaySys", "visit2",
		`{"timestamp":"2026-05-02T10:10:00Z","event":"FSDJump","StarSystem":"WaySys","SystemAddress":3002,"StarPos":[10,0,0],"JumpDist":10.0}`)
	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventScan,
		journal.EventLoadGame,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
		journal.EventResurrect,
	))
	stillCachedArrivals, _ := systemArrivals.InWindow("2026-05-02T10:00:00Z", "")
	if len(stillCachedArrivals) != 2 {
		testingContext.Fatalf("expected non-arrival events not to invalidate SystemArrivals, got len=%d", len(stillCachedArrivals))
	}
	stillCachedCounts, _ := visitCounts.CountsForSystems([]string{"WaySys"})
	if stillCachedCounts["waysys"] != 1 {
		testingContext.Fatalf("expected non-arrival events not to invalidate SystemVisitCounts, got %d", stillCachedCounts["waysys"])
	}

	// Publishing EventFSDJump invalidates both SystemArrivals and SystemVisitCounts.
	eventBus.PublishJournal(journal.NewEventSet(journal.EventFSDJump))
	refreshedArrivals, _ := systemArrivals.InWindow("2026-05-02T10:00:00Z", "")
	if len(refreshedArrivals) != 3 {
		testingContext.Fatalf("expected refreshed SystemArrivals len=3, got %d", len(refreshedArrivals))
	}
	refreshedCounts, _ := visitCounts.CountsForSystems([]string{"WaySys"})
	if refreshedCounts["waysys"] != 2 {
		testingContext.Fatalf("expected WaySys visit count 2 after refresh, got %d", refreshedCounts["waysys"])
	}
}

func TestShipHistorySuitFilteringAndSwapCustomNameLookup(testingContext *testing.T) {
	databaseConnection := openProjectionTestDB(testingContext)
	eventBus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	shipHistory := projections.NewShipHistory(databaseConnection, eventBus, recoveredJumps)

	// Log first in Caspian Explorer ("Siderite Skillet", ShipID 7), then in Mandalay ("Dust Devil", ShipID 9).
	insertJournalRow(testingContext, databaseConnection, "2026-05-01T10:00:00Z", "LoadGame", "", "lg1",
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Ship":"Explorer_NX","ShipID":7,"ShipName":"Siderite Skillet"}`)
	insertJournalRow(testingContext, databaseConnection, "2026-05-02T10:00:00Z", "LoadGame", "", "lg2",
		`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Ship":"Mandalay","ShipID":9,"ShipName":"Dust Devil"}`)
	// Relog on foot in an engineered Artemis suit -> must NOT override active ship in header or timeline.
	insertJournalRow(testingContext, databaseConnection, "2026-05-03T10:00:00Z", "LoadGame", "", "lg-suit",
		`{"timestamp":"2026-05-03T10:00:00Z","event":"LoadGame","Ship":"ExplorationSuit_Class5","Ship_Localised":"$ExplorationSuit_Class1_Name;","ShipID":9}`)
	// Relog inside SRV Scarab -> must also be ignored.
	insertJournalRow(testingContext, databaseConnection, "2026-05-03T10:05:00Z", "LoadGame", "", "lg-srv",
		`{"timestamp":"2026-05-03T10:05:00Z","event":"LoadGame","Ship":"TestBuggy","Ship_Localised":"SRV Scarab","ShipID":9}`)

	currentBeforeSwap, shipError := shipHistory.CurrentShip()
	if shipError != nil {
		testingContext.Fatalf("CurrentShip before swap: %v", shipError)
	}
	if currentBeforeSwap != "Mandalay \u201cDust Devil\u201d" {
		testingContext.Fatalf("CurrentShip = %q, want %q", currentBeforeSwap, "Mandalay \u201cDust Devil\u201d")
	}

	// Swap back to ShipID 7 via ShipyardSwap (which omits ShipName); header must recover "Siderite Skillet".
	insertJournalRow(testingContext, databaseConnection, "2026-05-04T10:00:00Z", "ShipyardSwap", "", "swap1",
		`{"timestamp":"2026-05-04T10:00:00Z","event":"ShipyardSwap","ShipType":"explorer_nx","ShipID":7,"StoreOldShip":"Mandalay","StoreShipID":9}`)

	currentAfterSwap, swapShipError := shipHistory.CurrentShip()
	if swapShipError != nil {
		testingContext.Fatalf("CurrentShip after swap: %v", swapShipError)
	}
	if currentAfterSwap != "Caspian Explorer \u201cSiderite Skillet\u201d" {
		testingContext.Fatalf("CurrentShip after swap = %q, want %q", currentAfterSwap, "Caspian Explorer \u201cSiderite Skillet\u201d")
	}

	// Timeline and leg transitions remain hull-only (preserving baseline display).
	timeline, timelineError := shipHistory.Timeline("2026-05-02T09:00:00Z", "2026-05-05T00:00:00Z")
	if timelineError != nil {
		testingContext.Fatalf("Timeline: %v", timelineError)
	}
	transitions := shipHistory.ShipsUsedOnLeg(timeline, []string{"2026-05-03T12:00:00Z", "2026-05-04T12:00:00Z"}, "2026-05-02T09:00:00Z", "2026-05-05T00:00:00Z")
	if len(transitions) != 2 || transitions[0].To != "Mandalay" || transitions[1].To != "Caspian Explorer" {
		testingContext.Fatalf("unexpected leg transitions (must stay hull-only): %+v", transitions)
	}
}

func TestCarrierStateIdentityFilteringAcrossAllCarrierEvents(testingContext *testing.T) {
	databaseConnection := openProjectionTestDB(testingContext)
	eventBus := projection.NewBus()
	carrierState := projections.NewCarrierState(databaseConnection, eventBus)

	// Without any CarrierStats, unlabelled Location matches Location/CarrierJump consistently.
	insertJournalRow(testingContext, databaseConnection, "2026-05-01T09:00:00Z", "CarrierJump", "OriginSys", "cj0",
		`{"timestamp":"2026-05-01T09:00:00Z","event":"CarrierJump","StarSystem":"OriginSys"}`)
	initialLoc, initialErr := carrierState.LastLocation()
	if initialErr != nil || initialLoc != "OriginSys" {
		testingContext.Fatalf("initial LastLocation = %q err=%v, want OriginSys", initialLoc, initialErr)
	}

	// Record personal carrier identity (CarrierID 3701199360, K1V-6QB) and fuel state.
	insertJournalRow(testingContext, databaseConnection, "2026-05-01T10:00:00Z", "CarrierStats", "", "cs1",
		`{"timestamp":"2026-05-01T10:00:00Z","event":"CarrierStats","CarrierID":3701199360,"Callsign":"K1V-6QB","Name":"VANCOUVER","FuelLevel":750,"SpaceUsage":{"TotalCapacity":25000,"FreeSpace":15000,"Cargo":120}}`)
	insertJournalRow(testingContext, databaseConnection, "2026-05-01T11:00:00Z", "CarrierJump", "OwnedCarrierSys", "cj-own",
		`{"timestamp":"2026-05-01T11:00:00Z","event":"CarrierJump","StarSystem":"OwnedCarrierSys","StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`)
	eventBus.PublishJournal(journal.NewEventSet(journal.EventCarrierStats))

	// A newer passenger CarrierJump, CarrierJumpRequest, and CarrierDepositFuel on a FOREIGN carrier
	// (3709999999 / D9Z-111) must be ignored in both LastLocation and DepotInfo.
	insertJournalRow(testingContext, databaseConnection, "2026-05-02T12:00:00Z", "CarrierJump", "ForeignDestSys", "cj-foreign",
		`{"timestamp":"2026-05-02T12:00:00Z","event":"CarrierJump","StarSystem":"ForeignDestSys","StationName":"D9Z-111","StationType":"FleetCarrier","MarketID":3709999999}`)
	insertJournalRow(testingContext, databaseConnection, "2026-05-02T12:05:00Z", "CarrierJumpRequest", "ForeignReqSys", "cjr-foreign",
		`{"timestamp":"2026-05-02T12:05:00Z","event":"CarrierJumpRequest","SystemName":"ForeignReqSys","CarrierID":3709999999}`)
	insertJournalRow(testingContext, databaseConnection, "2026-05-02T12:10:00Z", "CarrierDepositFuel", "", "cdf-foreign",
		`{"timestamp":"2026-05-02T12:10:00Z","event":"CarrierDepositFuel","CarrierID":3709999999,"Amount":200,"Total":999}`)

	lastLoc, locError := carrierState.LastLocation()
	if locError != nil || lastLoc != "OwnedCarrierSys" {
		testingContext.Fatalf("LastLocation = %q err=%v, want OwnedCarrierSys (foreign carrier ignored)", lastLoc, locError)
	}
	depotInfo, depotError := carrierState.DepotInfo()
	if depotError != nil {
		testingContext.Fatalf("DepotInfo: %v", depotError)
	}
	if depotInfo.UsedCapacity != 10000 || depotInfo.DepotTritium != 750 || depotInfo.Cargo != 120 {
		testingContext.Fatalf("DepotInfo = %+v, want UsedCapacity=10000 DepotTritium=750 Cargo=120", depotInfo)
	}

	// Own carrier deposit overrides DepotTritium when timestamp >= latest CarrierStats.
	insertJournalRow(testingContext, databaseConnection, "2026-05-02T13:00:00Z", "CarrierDepositFuel", "", "cdf-own",
		`{"timestamp":"2026-05-02T13:00:00Z","event":"CarrierDepositFuel","CarrierID":3701199360,"Amount":150,"Total":900}`)
	updatedDepot, updatedDepotErr := carrierState.DepotInfo()
	if updatedDepotErr != nil || updatedDepot.DepotTritium != 900 {
		testingContext.Fatalf("updated DepotInfo = %+v err=%v, want DepotTritium=900", updatedDepot, updatedDepotErr)
	}
}

