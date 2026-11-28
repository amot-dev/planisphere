package stats_test

import (
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/journaltest"
)

func TestCareerStatsStrictStatisticsSourceAndStringParsing(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		// Passenger ride aboard another commander's carrier (100 Ly)
		`{"timestamp":"2026-01-18T19:30:00Z","event":"CarrierJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100]}`,
	}
	harness := newTimeRangeStatsHarness(testingContext, lines, nil)

	// Without any owned carrier Statistics event, passenger rides stay in CarrierPassengerLy and DistanceLy
	// while Personal Carrier Operations (CarrierJumps, CarrierDistanceLy, TritiumUsed) stay 0 / nil.
	unownedSummary, unownedError := harness.careerStats.Get()
	if unownedError != nil {
		testingContext.Fatalf("CareerStats.Get without Statistics: %v", unownedError)
	}
	if unownedSummary.Metrics.CarrierPassengerLy != 100.0 || unownedSummary.Metrics.DistanceLy != 100.0 {
		testingContext.Fatalf("expected passenger carrier distance 100.0, got %+v", unownedSummary.Metrics)
	}
	if unownedSummary.Metrics.CarrierJumps != 0 || unownedSummary.Metrics.CarrierDistanceLy != 0 || unownedSummary.Metrics.TritiumUsed != nil {
		testingContext.Fatalf("expected 0 / nil owned carrier ops without Statistics, got jumps=%d dist=%.1f trit=%v",
			unownedSummary.Metrics.CarrierJumps, unownedSummary.Metrics.CarrierDistanceLy, unownedSummary.Metrics.TritiumUsed)
	}

	// Legacy string-formatted FLEETCARRIER_DISTANCE_TRAVELLED must parse cleanly after EventStatistics invalidation.
	journaltest.PublishParsedLines(
		testingContext,
		harness.conn,
		harness.bus,
		`{"timestamp":"2026-01-19T10:00:00Z","event":"Statistics","FLEETCARRIER":{"FLEETCARRIER_TOTAL_JUMPS":12,"FLEETCARRIER_DISTANCE_TRAVELLED":"1,234.5 LY"}}`,
	)

	stringStatsSummary, stringStatsError := harness.careerStats.Get()
	if stringStatsError != nil {
		testingContext.Fatalf("CareerStats.Get with string FLEETCARRIER distance: %v", stringStatsError)
	}
	if stringStatsSummary.Metrics.CarrierJumps != 12 || stringStatsSummary.Metrics.CarrierDistanceLy != 1234.5 {
		testingContext.Fatalf("expected CarrierJumps=12 CarrierDistanceLy=1234.5, got jumps=%d dist=%.1f",
			stringStatsSummary.Metrics.CarrierJumps, stringStatsSummary.Metrics.CarrierDistanceLy)
	}

	// Numeric FLEETCARRIER fields overwrite on newer Statistics event.
	journaltest.PublishParsedLines(
		testingContext,
		harness.conn,
		harness.bus,
		`{"timestamp":"2026-01-20T10:00:00Z","event":"Statistics","FLEETCARRIER":{"FLEETCARRIER_TOTAL_JUMPS":25,"FLEETCARRIER_DISTANCE_TRAVELLED":9876.25}}`,
	)

	numericStatsSummary, numericStatsError := harness.careerStats.Get()
	if numericStatsError != nil {
		testingContext.Fatalf("CareerStats.Get with numeric FLEETCARRIER distance: %v", numericStatsError)
	}
	if numericStatsSummary.Metrics.CarrierJumps != 25 || numericStatsSummary.Metrics.CarrierDistanceLy != 9876.25 || numericStatsSummary.Metrics.TritiumUsed != nil {
		testingContext.Fatalf("expected CarrierJumps=25 CarrierDistanceLy=9876.25 TritiumUsed=nil, got jumps=%d dist=%.2f trit=%v",
			numericStatsSummary.Metrics.CarrierJumps, numericStatsSummary.Metrics.CarrierDistanceLy, numericStatsSummary.Metrics.TritiumUsed)
	}
}

func TestCareerStatsIsolatesProfilesAndSupportsDisabledArchive(testingContext *testing.T) {
	harness, archivedScope, _, _ := seedScopedMultiProfileArchive(testingContext)
	careerStats := harness.careerStats

	allCareer, err := careerStats.Get()
	if err != nil {
		testingContext.Fatalf("CareerStats.Get: %v", err)
	}
	if allCareer.Metrics.Jumps != 2 || allCareer.Metrics.DistanceLy != 85.0 || allCareer.Metrics.CarrierJumps != 0 {
		testingContext.Fatalf("expected AllEnabled CareerStats to combine main+alt and exclude archived era, got %+v", allCareer.Metrics)
	}

	archivedCareer, err := careerStats.GetScoped(archivedScope)
	if err != nil {
		testingContext.Fatalf("CareerStats.GetScoped(archived): %v", err)
	}
	if archivedCareer.Metrics.Jumps != 1 || archivedCareer.Metrics.DistanceLy != 40.0 || archivedCareer.Metrics.CarrierJumps != 7 {
		testingContext.Fatalf("expected explicit disabled archived CareerStats to return full era metrics, got %+v", archivedCareer.Metrics)
	}
}

func TestCareerStatsScopedSectionIsolationAndCommanderFilterInvalidation(testingContext *testing.T) {
	harness, _, mainScope, altScope := seedScopedMultiProfileArchive(testingContext)
	careerStats := harness.careerStats

	initialMain, initialError := careerStats.GetScoped(mainScope)
	if initialError != nil {
		testingContext.Fatalf("initial CareerStats.GetScoped(main): %v", initialError)
	}
	if initialMain.Metrics.Jumps != 1 || initialMain.Metrics.DistanceLy != 60.0 || initialMain.Metrics.BodiesScanned != 1 || initialMain.Metrics.OrganicSamples != 0 {
		testingContext.Fatalf("unexpected initial mainScope metrics: %+v", initialMain.Metrics)
	}
	if altCareer, altError := careerStats.GetScoped(altScope); altError != nil || altCareer.Metrics.Jumps != 1 || altCareer.Metrics.OrganicSamples != 1 {
		testingContext.Fatalf("unexpected altScope metrics: %+v err=%v", altCareer.Metrics, altError)
	}

	travelGen := careerStats.Travel().Generation()
	carrierGen := careerStats.CarrierOps().Generation()
	cartoGen := careerStats.Cartographics().Generation()
	exoGen := careerStats.Exobiology().Generation()
	realGen := careerStats.Realization().Generation()

	// Insert new rows across all five career sections for mainScope, then publish only EventFSDJump.
	journaltest.InsertParsedLines(
		testingContext,
		harness.conn,
		`{"timestamp":"2026-02-01T12:00:00Z","event":"LoadGame","Commander":"CmdrMain","FID":"F10","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-02-01T12:05:00Z","event":"FSDJump","StarSystem":"MainHop2","SystemAddress":202,"StarPos":[0,95,0],"JumpDist":35.0}`,
		`{"timestamp":"2026-02-01T12:06:00Z","event":"Statistics","FLEETCARRIER":{"FLEETCARRIER_TOTAL_JUMPS":4,"FLEETCARRIER_DISTANCE_TRAVELLED":2000.0}}`,
		`{"timestamp":"2026-02-01T12:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"MainHop2","SystemAddress":202,"BodyID":2,"BodyName":"MainHop2 2","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-02-01T12:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":202,"Body":2}`,
		`{"timestamp":"2026-02-01T12:20:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`,
	)
	harness.bus.PublishJournal(journal.NewEventSet(journal.EventFSDJump))

	if careerStats.Travel().IsValid() || careerStats.Travel().Generation() != travelGen+1 {
		testingContext.Fatalf("expected EventFSDJump to invalidate CareerTravel (valid=%v gen=%d->%d)",
			careerStats.Travel().IsValid(), travelGen, careerStats.Travel().Generation())
	}
	if !careerStats.CarrierOps().IsValid() || !careerStats.Cartographics().IsValid() || !careerStats.Exobiology().IsValid() || !careerStats.Realization().IsValid() ||
		careerStats.CarrierOps().Generation() != carrierGen || careerStats.Cartographics().Generation() != cartoGen ||
		careerStats.Exobiology().Generation() != exoGen || careerStats.Realization().Generation() != realGen {
		testingContext.Fatalf("expected non-travel sections to remain warm with unchanged generations on EventFSDJump")
	}

	afterJump, jumpError := careerStats.GetScoped(mainScope)
	if jumpError != nil {
		testingContext.Fatalf("CareerStats.GetScoped(main) after FSDJump: %v", jumpError)
	}
	if afterJump.Metrics.Jumps != 2 || afterJump.Metrics.DistanceLy != 95.0 {
		testingContext.Fatalf("expected CareerTravel to reload Jumps=2 DistanceLy=95.0, got %+v", afterJump.Metrics)
	}
	if afterJump.Metrics.CarrierJumps != 0 || afterJump.Metrics.BodiesScanned != 1 || afterJump.Metrics.OrganicSamples != 0 || afterJump.Realization != initialMain.Realization {
		testingContext.Fatalf("expected CarrierOps, Cartographics, Exobiology, and Realization to stay cached without reloading, got metrics=%+v real=%+v",
			afterJump.Metrics, afterJump.Realization)
	}

	// AppCommanderFilterChanged must invalidate all five sections so subsequent GetScoped reloads every domain.
	harness.bus.PublishApp(events.AppCommanderFilterChanged)
	if careerStats.Travel().IsValid() || careerStats.CarrierOps().IsValid() || careerStats.Cartographics().IsValid() ||
		careerStats.Exobiology().IsValid() || careerStats.Realization().IsValid() {
		testingContext.Fatalf("expected AppCommanderFilterChanged to invalidate all five career sections")
	}

	afterFilterChange, filterError := careerStats.GetScoped(mainScope)
	if filterError != nil {
		testingContext.Fatalf("CareerStats.GetScoped(main) after AppCommanderFilterChanged: %v", filterError)
	}
	if afterFilterChange.Metrics.Jumps != 2 || afterFilterChange.Metrics.CarrierJumps != 4 ||
		afterFilterChange.Metrics.BodiesScanned != 2 || afterFilterChange.Metrics.OrganicSamples != 1 ||
		afterFilterChange.Realization.TotalRealized != 19010800 {
		testingContext.Fatalf("expected all five career sections reloaded after AppCommanderFilterChanged, got metrics=%+v real=%+v",
			afterFilterChange.Metrics, afterFilterChange.Realization)
	}
}
