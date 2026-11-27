package stats_test

import (
	"testing"

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
