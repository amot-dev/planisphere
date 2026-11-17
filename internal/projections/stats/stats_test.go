package stats_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/modes"
)

func seedStatsDBFromLines(testingContext *testing.T, journalLines []string) *sql.DB {
	testingContext.Helper()
	databaseConnection := setupRealizationDB(testingContext)
	temporaryDirectory := testingContext.TempDir()
	journalFilePath := filepath.Join(temporaryDirectory, "Journal.2026-03-10T100000.01.log")
	if writeError := os.WriteFile(journalFilePath, []byte(strings.Join(journalLines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	scanResult, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory)
	if scanError != nil {
		testingContext.Fatal(scanError)
	}
	if scanResult.NewEvents != len(journalLines) {
		testingContext.Fatalf("ingested %d events, want %d", scanResult.NewEvents, len(journalLines))
	}
	return databaseConnection
}

func TestCarrierJumpPassengerDistanceRespectsModeFilter(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"CarrierJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100]}`,
		`{"timestamp":"2026-01-18T19:50:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1003,"StarPos":[0,0,106],"JumpDist":6.0}`,
	}
	conn := seedStatsDBFromLines(t, lines)

	modeList, listError := modes.List(conn)
	if listError != nil || len(modeList) < 3 {
		t.Fatalf("expected at least 3 default modes, got %v (%v)", modeList, listError)
	}
	var expModeID int64
	for _, modeItem := range modeList {
		if modeItem.Name == "Exploration" {
			expModeID = modeItem.ID
		}
	}
	if expModeID == 0 {
		t.Fatalf("could not find Exploration mode in %v", modeList)
	}

	if txError := db.WithTx(conn, func(tx *sql.Tx) error {
		return modes.SetWindow(tx, 0, expModeID, "2026-01-18T19:20:00Z", "2026-01-18T20:00:00Z")
	}); txError != nil {
		t.Fatal(txError)
	}

	engine := stats.NewEngine(conn)

	// Seeded window starting after Location ("19:20:00Z") must still resolve previous StarPos ([0,0,0]) for CarrierJump.
	unfiltered, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:20:00Z"}, nil)
	if computeError != nil {
		t.Fatal(computeError)
	}
	if unfiltered.CarrierJumps != 0 || unfiltered.CarrierDistanceLy != 0 || unfiltered.TritiumUsed != nil {
		t.Fatalf("expected journal Compute not to populate carrier operation metrics, got jumps=%d dist=%.1f tritium=%v",
			unfiltered.CarrierJumps, unfiltered.CarrierDistanceLy, unfiltered.TritiumUsed)
	}
	if unfiltered.CarrierPassengerLy != 100.0 {
		t.Fatalf("expected 100.0 carrier passenger Ly, got %.1f", unfiltered.CarrierPassengerLy)
	}
	if unfiltered.ShipDistanceLy != 6.0 {
		t.Fatalf("expected 6.0 ship distance Ly, got %.1f", unfiltered.ShipDistanceLy)
	}
	if unfiltered.DistanceLy != 106.0 {
		t.Fatalf("expected 106.0 total distance Ly, got %.1f", unfiltered.DistanceLy)
	}

	filtered, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: false, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}
	if filtered.CarrierPassengerLy != 0 || filtered.ShipDistanceLy != 0 || filtered.DistanceLy != 0 || filtered.Jumps != 0 {
		t.Fatalf("expected travel metrics filtered to 0 when Travel=false: %+v", filtered)
	}

	filteredTravelOn, computeError := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: expModeID, Travel: true, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}
	if filteredTravelOn.CarrierPassengerLy != 100.0 || filteredTravelOn.DistanceLy != 106.0 {
		t.Fatalf("unexpected metrics with Travel=true: %+v", filteredTravelOn)
	}
}

func TestCarrierPassengerDistanceSpanningDifferentModes(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T20:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T20:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T20:10:00Z","event":"CarrierJump","StarSystem":"System1","SystemAddress":1002,"StarPos":[0,0,100]}`,
		`{"timestamp":"2026-01-18T20:30:00Z","event":"CarrierJump","StarSystem":"System2","SystemAddress":1003,"StarPos":[0,0,300]}`,
	}
	conn := seedStatsDBFromLines(t, lines)

	modeList, listError := modes.List(conn)
	if listError != nil || len(modeList) < 2 {
		t.Fatalf("expected at least 2 default modes, got %v (%v)", modeList, listError)
	}
	modeA := modeList[0].ID
	modeB := modeList[1].ID

	if txError := db.WithTx(conn, func(tx *sql.Tx) error {
		if windowError := modes.SetWindow(tx, 0, modeA, "2026-01-18T20:08:00Z", "2026-01-18T20:20:00Z"); windowError != nil {
			return windowError
		}
		return modes.SetWindow(tx, 0, modeB, "2026-01-18T20:20:00Z", "2026-01-18T20:40:00Z")
	}); txError != nil {
		t.Fatal(txError)
	}

	engine := stats.NewEngine(conn)
	computeResult, computeError := engine.Compute(stats.Range{After: "2026-01-18T20:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modeA, Travel: false, Discovery: true},
			{ModeID: modeB, Travel: true, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
	}

	if computeResult.CarrierJumps != 0 || computeResult.CarrierDistanceLy != 0 || computeResult.TritiumUsed != nil {
		t.Fatalf("expected carrier operation metrics to remain zeroed, got %+v", computeResult)
	}
	if computeResult.CarrierPassengerLy != 200.0 {
		t.Fatalf("expected 200.0 carrier passenger Ly, got %.1f", computeResult.CarrierPassengerLy)
	}
	if computeResult.DistanceLy != 200.0 {
		t.Fatalf("expected 200.0 total distance Ly, got %.1f", computeResult.DistanceLy)
	}
}

func TestPreloadScansForMappedBodies(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T18:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T18:05:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Test Planet 1","BodyID":5,"StarSystem":"TestSys","SystemAddress":9999,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"SAAScanComplete","BodyName":"Test Planet 1","BodyID":5,"SystemAddress":9999,"ProbesUsed":4,"EfficiencyTarget":6}`,
	}
	conn := seedStatsDBFromLines(t, lines)

	engine := stats.NewEngine(conn)
	res, err := engine.Compute(stats.Range{After: "2026-01-18T19:00:00Z"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.BodiesMapped != 1 {
		t.Fatalf("expected 1 body mapped, got %d", res.BodiesMapped)
	}
	if res.FirstMapped != 1 {
		t.Fatalf("expected 1 first mapped, got %d", res.FirstMapped)
	}
	if res.ScanValueTotal == 0 {
		t.Fatalf("expected positive scan value total from preloaded scan, got %d", res.ScanValueTotal)
	}
}

type timelineQueryCountingQueryer struct {
	db.Queryer
	timelineQueryCount int
}

func (countingQueryer *timelineQueryCountingQueryer) Query(query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FROM mode_windows") {
		countingQueryer.timelineQueryCount++
	}
	return countingQueryer.Queryer.Query(query, args...)
}

func TestFilterPreparedSetsReuse(testingContext *testing.T) {
	databaseConnection := setupRealizationDB(testingContext)

	modeList, listError := modes.List(databaseConnection)
	if listError != nil || len(modeList) == 0 {
		testingContext.Fatalf("expected default modes, got %v (%v)", modeList, listError)
	}

	countingQueryer := &timelineQueryCountingQueryer{Queryer: databaseConnection}
	engineInstance := stats.NewEngine(countingQueryer)
	sharedFilter := stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modeList[0].ID, Travel: true, Discovery: true},
		},
	}

	timeWindow := stats.Range{After: "2026-01-01T00:00:00Z", Until: "2026-01-02T00:00:00Z"}
	if _, computeError := engineInstance.Compute(timeWindow, &sharedFilter); computeError != nil {
		testingContext.Fatalf("Compute failed: %v", computeError)
	}
	if _, realizationError := engineInstance.ComputeRealization(timeWindow, &sharedFilter); realizationError != nil {
		testingContext.Fatalf("ComputeRealization failed: %v", realizationError)
	}

	if countingQueryer.timelineQueryCount != 1 {
		testingContext.Fatalf("expected mode_windows timeline query to execute once when reusing Filter, got %d", countingQueryer.timelineQueryCount)
	}
}

func TestPrepareComputeScansOrderingAndRangeBounds(testingContext *testing.T) {
	databaseConnection := setupRealizationDB(testingContext)
	engineInstance := stats.NewEngine(databaseConnection)

	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`)
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"SAAScanComplete","BodyName":"SameSec 1","BodyID":3,"SystemAddress":7001,"ProbesUsed":4,"EfficiencyTarget":6}`)
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"Scan","ScanType":"Detailed","BodyName":"SameSec 1","BodyID":3,"StarSystem":"SameSec","SystemAddress":7001,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	earthlikeScan := reference.BodyScan{PlanetClass: "Earthlike body", MassEM: 1.0}
	expectedScanOnly := reference.EstimateBodyValue(earthlikeScan, reference.MappingInfo{}, true)
	expectedMapping := reference.EstimateMappingIncrement(earthlikeScan, reference.MappingInfo{Mapped: true, ProbesUsed: 4, EfficiencyTarget: 6}, true)

	fallbackModeFilter := &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modes.DefaultFallbackModeID(databaseConnection), Travel: true, Discovery: true},
		},
	}
	sameTimestampMetrics, sameTimestampError := engineInstance.Compute(stats.Range{}, fallbackModeFilter)
	if sameTimestampError != nil {
		testingContext.Fatalf("Compute same-timestamp all-time filtered failed: %v", sameTimestampError)
	}
	expectedCombinedTotal := expectedScanOnly.Total + expectedMapping.Total
	if sameTimestampMetrics.FirstMapped != 1 || sameTimestampMetrics.ScanValueTotal != expectedCombinedTotal {
		testingContext.Fatalf("expected same-timestamp mapping to resolve parent scan (FirstMapped=1, ScanValueTotal=%d), got FirstMapped=%d ScanValueTotal=%d",
			expectedCombinedTotal, sameTimestampMetrics.FirstMapped, sameTimestampMetrics.ScanValueTotal)
	}

	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T11:10:00Z","event":"SAAScanComplete","BodyName":"FutureScan 1","BodyID":4,"SystemAddress":7002,"ProbesUsed":4,"EfficiencyTarget":6}`)
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T11:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"FutureScan 1","BodyID":4,"StarSystem":"FutureScan","SystemAddress":7002,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	futureScanWindow := stats.Range{After: "2026-05-01T11:00:00Z", Until: "2026-05-01T12:00:00Z"}
	futureScanMetrics, futureScanError := engineInstance.Compute(futureScanWindow, nil)
	if futureScanError != nil {
		testingContext.Fatalf("Compute future-scan range failed: %v", futureScanError)
	}
	if futureScanMetrics.FirstMapped != 0 || futureScanMetrics.ScanValueTotal != expectedScanOnly.Total {
		testingContext.Fatalf("expected future scan not to preload for earlier mapping (FirstMapped=0, ScanValueTotal=%d), got FirstMapped=%d ScanValueTotal=%d",
			expectedScanOnly.Total, futureScanMetrics.FirstMapped, futureScanMetrics.ScanValueTotal)
	}
}

func TestComputeRecoversDisconnectedJumpsAcrossRangeAndFilter(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-11-17T01:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX","Odyssey":true}`,
		`{"timestamp":"2026-11-17T01:05:00Z","event":"FSDJump","StarSystem":"Hypo Aeb ST-B b54-0","SystemAddress":538078878161,"StarPos":[-9894.84375,-5.5625,-7569.25],"JumpDist":81.096}`,
		`{"timestamp":"2026-11-17T01:40:30Z","event":"JetConeBoost","BoostValue":4.0}`,
		`{"timestamp":"2026-11-17T01:40:45Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321}`,
		`{"timestamp":"2026-11-17T01:41:42Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX","Odyssey":true}`,
		`{"timestamp":"2026-11-17T01:41:53Z","event":"Location","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321,"StarPos":[-9962.1875,-1.3125,-7604.875],"Docked":false}`,
	}
	databaseConnection := seedStatsDBFromLines(testingContext, lines)

	modeList, listError := modes.List(databaseConnection)
	if listError != nil || len(modeList) == 0 {
		testingContext.Fatalf("expected default modes, got %v (%v)", modeList, listError)
	}
	explorationModeID := modeList[0].ID
	if windowError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		return modes.SetWindow(transaction, 0, explorationModeID, "2026-11-17T01:10:00Z", "2026-11-17T02:00:00Z")
	}); windowError != nil {
		testingContext.Fatal(windowError)
	}

	engineInstance := stats.NewEngine(databaseConnection)

	legMetrics, legError := engineInstance.Compute(stats.Range{After: "2026-11-17T01:05:00Z", Until: "2026-11-17T02:00:00Z"}, nil)
	if legError != nil {
		testingContext.Fatal(legError)
	}
	expectedLegDistance := reference.Distance(reference.Vec3{X: -9894.84375, Y: -5.5625, Z: -7569.25}, reference.Vec3{X: -9962.1875, Y: -1.3125, Z: -7604.875})
	if legMetrics.Jumps != 1 || legMetrics.JetConeBoosts != 1 || legMetrics.ShipDistanceLy != expectedLegDistance || legMetrics.DistanceLy != expectedLegDistance {
		testingContext.Fatalf("unexpected bounded-window recovered metrics: %+v (want dist %.6f)", legMetrics, expectedLegDistance)
	}

	travelExcluded, filterError := engineInstance.Compute(stats.Range{After: "2026-11-17T01:05:00Z", Until: "2026-11-17T02:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes:   []stats.ModeSelection{{ModeID: explorationModeID, Travel: false, Discovery: true}},
	})
	if filterError != nil {
		testingContext.Fatal(filterError)
	}
	if travelExcluded.Jumps != 0 || travelExcluded.ShipDistanceLy != 0 || travelExcluded.JetConeBoosts != 0 {
		testingContext.Fatalf("expected Travel=false filter to exclude recovered jump, got %+v", travelExcluded)
	}
}

func TestCareerStatsStrictStatisticsSourceAndStringParsing(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		// Passenger ride aboard another commander's carrier (100 Ly)
		`{"timestamp":"2026-01-18T19:30:00Z","event":"CarrierJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100]}`,
	}
	databaseConnection := seedStatsDBFromLines(testingContext, lines)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	careerStats := stats.NewCareerStats(databaseConnection, eventBus, salesLedger, recoveredJumps)

	// Without any owned carrier Statistics event, passenger rides stay in CarrierPassengerLy and DistanceLy
	// while Personal Carrier Operations (CarrierJumps, CarrierDistanceLy, TritiumUsed) stay 0 / nil.
	unownedSummary, unownedError := careerStats.Get()
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
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-01-19T10:00:00Z","event":"Statistics","FLEETCARRIER":{"FLEETCARRIER_TOTAL_JUMPS":12,"FLEETCARRIER_DISTANCE_TRAVELLED":"1,234.5 LY"}}`)
	eventBus.PublishJournal(journal.NewEventSet(journal.EventStatistics))

	stringStatsSummary, stringStatsError := careerStats.Get()
	if stringStatsError != nil {
		testingContext.Fatalf("CareerStats.Get with string FLEETCARRIER distance: %v", stringStatsError)
	}
	if stringStatsSummary.Metrics.CarrierJumps != 12 || stringStatsSummary.Metrics.CarrierDistanceLy != 1234.5 {
		testingContext.Fatalf("expected CarrierJumps=12 CarrierDistanceLy=1234.5, got jumps=%d dist=%.1f",
			stringStatsSummary.Metrics.CarrierJumps, stringStatsSummary.Metrics.CarrierDistanceLy)
	}

	// Numeric FLEETCARRIER fields overwrite on newer Statistics event.
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-01-20T10:00:00Z","event":"Statistics","FLEETCARRIER":{"FLEETCARRIER_TOTAL_JUMPS":25,"FLEETCARRIER_DISTANCE_TRAVELLED":9876.25}}`)
	eventBus.PublishJournal(journal.NewEventSet(journal.EventStatistics))

	numericStatsSummary, numericStatsError := careerStats.Get()
	if numericStatsError != nil {
		testingContext.Fatalf("CareerStats.Get with numeric FLEETCARRIER distance: %v", numericStatsError)
	}
	if numericStatsSummary.Metrics.CarrierJumps != 25 || numericStatsSummary.Metrics.CarrierDistanceLy != 9876.25 || numericStatsSummary.Metrics.TritiumUsed != nil {
		testingContext.Fatalf("expected CarrierJumps=25 CarrierDistanceLy=9876.25 TritiumUsed=nil, got jumps=%d dist=%.2f trit=%v",
			numericStatsSummary.Metrics.CarrierJumps, numericStatsSummary.Metrics.CarrierDistanceLy, numericStatsSummary.Metrics.TritiumUsed)
	}
}

func TestTimeRangeStatsCachingAndInvalidation(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":1,"BodyName":"RangeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	}
	databaseConnection := seedStatsDBFromLines(testingContext, lines)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	careerStats := stats.NewCareerStats(databaseConnection, eventBus, salesLedger, recoveredJumps)
	timeRangeStats := stats.NewTimeRangeStats(databaseConnection, eventBus, salesLedger, recoveredJumps)

	if _, careerWarmError := careerStats.Get(); careerWarmError != nil {
		testingContext.Fatalf("initial CareerStats.Get: %v", careerWarmError)
	}
	initialCareerGeneration := careerStats.Generation()

	timeRange := stats.Range{After: "2026-01-18T19:00:00Z", Until: "2026-01-18T20:00:00Z"}
	initialSummary, initialError := timeRangeStats.Compute(timeRange, nil)
	if initialError != nil {
		testingContext.Fatalf("initial TimeRangeStats.Compute: %v", initialError)
	}
	if initialSummary.Metrics.BodiesScanned != 1 {
		testingContext.Fatalf("expected 1 body scanned, got %d", initialSummary.Metrics.BodiesScanned)
	}
	initialRangeGeneration := timeRangeStats.Generation()

	// Insert second scan and publish replay-context/carrier-stats/pre-jump/swap events: neither CareerStats nor TimeRangeStats should invalidate.
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":2,"BodyName":"RangeSys 2","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventCarrierStats,
		journal.EventDocked,
		journal.EventUndocked,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
	))
	if careerStats.Generation() != initialCareerGeneration || timeRangeStats.Generation() != initialRangeGeneration {
		testingContext.Fatalf("expected non-metric events not to invalidate stats caches (careerGen=%d->%d, rangeGen=%d->%d)",
			initialCareerGeneration, careerStats.Generation(), initialRangeGeneration, timeRangeStats.Generation())
	}
	cachedSummary, _ := timeRangeStats.Compute(timeRange, nil)
	if cachedSummary.Metrics.BodiesScanned != 1 {
		testingContext.Fatalf("expected cached BodiesScanned=1 before invalidation, got %d", cachedSummary.Metrics.BodiesScanned)
	}

	// Publishing AppModesChanged clears TimeRangeStats even without raw journal changes.
	eventBus.PublishApp(events.AppModesChanged)
	afterModeInvalidation, _ := timeRangeStats.Compute(timeRange, nil)
	if afterModeInvalidation.Metrics.BodiesScanned != 2 {
		testingContext.Fatalf("expected refreshed BodiesScanned=2 after AppModesChanged, got %d", afterModeInvalidation.Metrics.BodiesScanned)
	}

	// Insert third scan and publish EventScan.
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":3,"BodyName":"RangeSys 3","PlanetClass":"Ammonia world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	eventBus.PublishJournal(journal.NewEventSet(journal.EventScan))
	afterScanInvalidation, _ := timeRangeStats.Compute(timeRange, nil)
	if afterScanInvalidation.Metrics.BodiesScanned != 3 {
		testingContext.Fatalf("expected refreshed BodiesScanned=3 after EventScan, got %d", afterScanInvalidation.Metrics.BodiesScanned)
	}
}

