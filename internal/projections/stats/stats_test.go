package stats_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/modes"
)

func seedStatsDBFromLines(testingContext *testing.T, journalLines []string) *sql.DB {
	testingContext.Helper()
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("open in-memory db: %v", openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})

	temporaryDirectory := testingContext.TempDir()
	journalPath := filepath.Join(temporaryDirectory, "Journal.2026-01-18T180000.01.log")
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(journalLines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatalf("write journal file: %v", writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); scanError != nil {
		testingContext.Fatalf("ScanDir: %v", scanError)
	}
	return databaseConnection
}

func ingestTestLine(testingContext *testing.T, databaseConnection *sql.DB, rawJSON string) {
	testingContext.Helper()
	parsedEvent, ok, parseError := journal.ParseLine([]byte(rawJSON), "test.log")
	if parseError != nil || !ok {
		testingContext.Fatalf("ParseLine(%s) failed: ok=%v err=%v", rawJSON, ok, parseError)
	}
	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, system_name, system_address, body_id, body_name, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event, parsedEvent.SystemName,
		parsedEvent.SystemAddress, parsedEvent.BodyID, parsedEvent.BodyName, parsedEvent.Raw, parsedEvent.Timestamp,
	); execError != nil {
		testingContext.Fatalf("insert parsed event %s: %v", parsedEvent.Event, execError)
	}
}

func insertEvent(testingContext *testing.T, databaseConnection *sql.DB, timestamp, eventName, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, rawJSON)
	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, rawJSON, timestamp,
	); execError != nil {
		testingContext.Fatalf("insert %s event: %v", eventName, execError)
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
	fallbackModeID := modes.DefaultFallbackModeID(databaseConnection)
	staticModeLoader := func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
		return func(_ string) int64 { return fallbackModeID }, nil
	}
	timeRangeStats := stats.NewTimeRangeStats(databaseConnection, eventBus, salesLedger, recoveredJumps, staticModeLoader)

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

	// Publishing AppModesChanged and AppExpeditionBoundsChanged preserves TimeRangeStats caches.
	eventBus.PublishApp(events.AppModesChanged, events.AppExpeditionBoundsChanged)
	if timeRangeStats.Generation() != initialRangeGeneration || timeRangeStats.CachedCount() != 1 {
		testingContext.Fatalf("expected AppModesChanged and AppExpeditionBoundsChanged to preserve TimeRangeStats (gen=%d->%d, count=%d)",
			initialRangeGeneration, timeRangeStats.Generation(), timeRangeStats.CachedCount())
	}
	preservedSummary, preservedError := timeRangeStats.Compute(timeRange, nil)
	if preservedError != nil || preservedSummary.Metrics.BodiesScanned != 1 {
		testingContext.Fatalf("expected preserved cached BodiesScanned=1, got %d (err=%v)", preservedSummary.Metrics.BodiesScanned, preservedError)
	}

	// Insert third scan and publish EventScan: only Cartographics and Realization invalidate; Travel, CarrierOps, and Exobiology stay valid.
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":3,"BodyName":"RangeSys 3","PlanetClass":"Ammonia world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	eventBus.PublishJournal(journal.NewEventSet(journal.EventScan))
	if !careerStats.Travel().IsValid() || !careerStats.CarrierOps().IsValid() || !careerStats.Exobiology().IsValid() {
		testingContext.Fatalf("expected EventScan to preserve CareerTravel, CareerCarrierOps, and CareerExobiology")
	}
	if careerStats.Cartographics().IsValid() || careerStats.Realization().IsValid() {
		testingContext.Fatalf("expected EventScan to invalidate CareerCartographics and CareerRealization")
	}
	if timeRangeStats.Travel().CachedCount() != 1 || timeRangeStats.Exobiology().CachedCount() != 1 ||
		timeRangeStats.Cartographics().CachedCount() != 0 || timeRangeStats.Realization().CachedCount() != 0 {
		testingContext.Fatalf("unexpected time-range cache counts after EventScan")
	}
	afterScanInvalidation, _ := timeRangeStats.Compute(timeRange, nil)
	if afterScanInvalidation.Metrics.BodiesScanned != 3 {
		testingContext.Fatalf("expected refreshed BodiesScanned=3 after EventScan, got %d", afterScanInvalidation.Metrics.BodiesScanned)
	}
	_, _ = careerStats.Get()

	// Warm a mode-filtered entry alongside the unfiltered entry, then verify AppExpeditionModesChanged
	// invalidates only the mode-filtered cache while preserving the unfiltered "|all" cache.
	modeFilter := &stats.Filter{
		Enabled:      true,
		ExpeditionID: 1,
		Modes:        []stats.ModeSelection{{ModeID: modes.DefaultFallbackModeID(databaseConnection), Travel: true, Discovery: true}},
	}
	filteredBeforeWindowChange, filteredBeforeError := timeRangeStats.Compute(timeRange, modeFilter)
	if filteredBeforeError != nil || filteredBeforeWindowChange.Metrics.BodiesScanned != 3 {
		testingContext.Fatalf("expected mode-filtered BodiesScanned=3 before invalidation, got %d (err=%v)",
			filteredBeforeWindowChange.Metrics.BodiesScanned, filteredBeforeError)
	}
	if timeRangeStats.CachedCount() != 2 {
		testingContext.Fatalf("expected 2 cached entries (unfiltered and filtered), got %d", timeRangeStats.CachedCount())
	}

	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T19:40:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":4,"BodyName":"RangeSys 4","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	eventBus.PublishApp(events.AppExpeditionModesChanged)

	if timeRangeStats.CachedCount() != 1 {
		testingContext.Fatalf("expected AppExpeditionModesChanged to drop CachedCount from 2 to 1, got %d", timeRangeStats.CachedCount())
	}
	preservedUnfilteredSummary, preservedUnfilteredError := timeRangeStats.Compute(timeRange, nil)
	if preservedUnfilteredError != nil || preservedUnfilteredSummary.Metrics.BodiesScanned != 3 {
		testingContext.Fatalf("expected unfiltered cache to stay at BodiesScanned=3, got %d (err=%v)",
			preservedUnfilteredSummary.Metrics.BodiesScanned, preservedUnfilteredError)
	}
	refreshedFilteredSummary, refreshedFilteredError := timeRangeStats.Compute(timeRange, modeFilter)
	if refreshedFilteredError != nil || refreshedFilteredSummary.Metrics.BodiesScanned != 4 {
		testingContext.Fatalf("expected mode-filtered BodiesScanned=4 after AppExpeditionModesChanged, got %d (err=%v)",
			refreshedFilteredSummary.Metrics.BodiesScanned, refreshedFilteredError)
	}
}

func TestRealizationLedgerCaching(testingContext *testing.T) {
	databaseConnection := seedStatsDBFromLines(testingContext, nil)

	insertEvent(testingContext, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	engineInstance := replaystats.NewEngineWithSources(databaseConnection, salesLedger, nil, nil)

	firstResult, firstComputeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if firstComputeError != nil {
		testingContext.Fatalf("first ComputeRealization failed: %v", firstComputeError)
	}
	if firstResult.PendingValue <= 0 {
		testingContext.Fatalf("expected positive PendingValue, got %d", firstResult.PendingValue)
	}

	// Insert second event directly into database without publishing to the bus
	insertEvent(testingContext, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`)

	// Query with cached ledger should still report pending because bus event has not fired
	secondResult, secondComputeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if secondComputeError != nil {
		testingContext.Fatalf("second ComputeRealization failed: %v", secondComputeError)
	}
	if secondResult.PendingValue != firstResult.PendingValue {
		testingContext.Fatalf("expected identical cached pending value %d, got %d", firstResult.PendingValue, secondResult.PendingValue)
	}

	// Publish matching journal event to invalidate ledger cache
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellOrganicData))

	// Query after invalidation should pick up the sale
	thirdResult, thirdComputeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if thirdComputeError != nil {
		testingContext.Fatalf("third ComputeRealization failed: %v", thirdComputeError)
	}
	if thirdResult.PendingValue != 0 {
		testingContext.Fatalf("expected pending value 0 after sale realization and cache invalidation, got %d", thirdResult.PendingValue)
	}
	if thirdResult.TotalRealized != 95054000 {
		testingContext.Fatalf("expected TotalRealized 95054000 after cache invalidation, got %d", thirdResult.TotalRealized)
	}
}

func TestTimeRangeSelectiveInvalidationPreservesClosedWindowsExceptRetroactiveEvents(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"FSDJump","StarSystem":"ClosedSys","SystemAddress":1001,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"ClosedSys","SystemAddress":1001,"BodyID":1,"BodyName":"ClosedSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1001,"Body":1}`,
	}
	databaseConnection := seedStatsDBFromLines(testingContext, lines)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	fallbackModeID := modes.DefaultFallbackModeID(databaseConnection)
	staticModeLoader := func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
		return func(_ string) int64 { return fallbackModeID }, nil
	}
	timeRangeStats := stats.NewTimeRangeStats(databaseConnection, eventBus, salesLedger, recoveredJumps, staticModeLoader)

	closedRange := stats.Range{After: "2026-01-18T19:00:00Z", Until: "2026-01-18T20:00:00Z"}
	activeRange := stats.Range{After: "2026-01-18T20:00:00Z", Until: ""}
	if _, closedError := timeRangeStats.Compute(closedRange, nil); closedError != nil {
		testingContext.Fatalf("warm closedRange: %v", closedError)
	}
	if _, activeError := timeRangeStats.Compute(activeRange, nil); activeError != nil {
		testingContext.Fatalf("warm activeRange: %v", activeError)
	}

	// Normal live tail events after 20:00 must evict activeRange while keeping closedRange cached in every domain.
	eventBus.PublishJournalSince(
		journal.NewEventSet(journal.EventFSDJump, journal.EventScan, journal.EventScanOrganic),
		"2026-01-18T21:00:00Z",
	)
	if timeRangeStats.Travel().CachedCount() != 1 ||
		timeRangeStats.Cartographics().CachedCount() != 1 ||
		timeRangeStats.Exobiology().CachedCount() != 1 ||
		timeRangeStats.Realization().CachedCount() != 1 {
		testingContext.Fatalf(
			"expected closedRange preserved (count=1) and activeRange evicted across all 4 sections, got travel=%d carto=%d exo=%d real=%d",
			timeRangeStats.Travel().CachedCount(),
			timeRangeStats.Cartographics().CachedCount(),
			timeRangeStats.Exobiology().CachedCount(),
			timeRangeStats.Realization().CachedCount(),
		)
	}

	// Retroactive Vista Genomics sale after 20:00 must evict closedRange on Exobiology and Realization only.
	eventBus.PublishJournalSince(journal.NewEventSet(journal.EventSellOrganicData), "2026-01-18T22:00:00Z")
	if timeRangeStats.Travel().CachedCount() != 1 || timeRangeStats.Cartographics().CachedCount() != 1 {
		testingContext.Fatalf("expected Travel and Cartographics closedRange to remain cached on SellOrganicData")
	}
	if timeRangeStats.Exobiology().CachedCount() != 0 || timeRangeStats.Realization().CachedCount() != 0 {
		testingContext.Fatalf("expected SellOrganicData to evict Exobiology and Realization closedRange, got exo=%d real=%d",
			timeRangeStats.Exobiology().CachedCount(), timeRangeStats.Realization().CachedCount())
	}

	// Re-warm closedRange and verify SellExplorationData evicts only Realization while leaving Travel, Cartographics, and Exobiology cached.
	if _, rewarmError := timeRangeStats.Compute(closedRange, nil); rewarmError != nil {
		testingContext.Fatalf("rewarm closedRange: %v", rewarmError)
	}
	eventBus.PublishJournalSince(journal.NewEventSet(journal.EventSellExplorationData), "2026-01-18T23:00:00Z")
	if timeRangeStats.Travel().CachedCount() != 1 ||
		timeRangeStats.Cartographics().CachedCount() != 1 ||
		timeRangeStats.Exobiology().CachedCount() != 1 {
		testingContext.Fatalf("expected Travel, Cartographics, and Exobiology closedRange to remain cached on SellExplorationData")
	}
	if timeRangeStats.Realization().CachedCount() != 0 {
		testingContext.Fatalf("expected SellExplorationData to evict Realization closedRange, got %d", timeRangeStats.Realization().CachedCount())
	}
}

func TestTimeRangeStatsMemoizesModeLoaderOnColdFilteredCompute(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"FSDJump","StarSystem":"ModeSys","SystemAddress":2002,"StarPos":[25,0,0],"JumpDist":25.0}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"ModeSys","SystemAddress":2002,"BodyID":1,"BodyName":"ModeSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":2002,"Body":1}`,
	}
	databaseConnection := seedStatsDBFromLines(testingContext, lines)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	fallbackModeID := modes.DefaultFallbackModeID(databaseConnection)

	var loaderCalls atomic.Int32
	countingModeLoader := func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
		loaderCalls.Add(1)
		return func(_ string) int64 { return fallbackModeID }, nil
	}
	timeRangeStats := stats.NewTimeRangeStats(databaseConnection, eventBus, salesLedger, recoveredJumps, countingModeLoader)

	timeRange := stats.Range{After: "2026-01-18T19:00:00Z", Until: "2026-01-18T20:00:00Z"}
	modeFilter := &stats.Filter{
		Enabled:      true,
		ExpeditionID: 1,
		Modes:        []stats.ModeSelection{{ModeID: fallbackModeID, Travel: true, Discovery: true}},
	}

	summary, computeError := timeRangeStats.Compute(timeRange, modeFilter)
	if computeError != nil {
		testingContext.Fatalf("cold filtered Compute: %v", computeError)
	}
	if loaderCalls.Load() != 1 {
		testingContext.Fatalf("expected modeLoader to be called once on cold filtered Compute, got %d", loaderCalls.Load())
	}
	if summary.Metrics.Jumps != 1 || summary.Metrics.BodiesScanned != 1 || summary.Metrics.OrganicSamples != 1 || summary.Realization.PendingValue <= 0 {
		testingContext.Fatalf("unexpected cold filtered summary: %+v", summary)
	}

	if _, warmError := timeRangeStats.Compute(timeRange, modeFilter); warmError != nil {
		testingContext.Fatalf("warm filtered Compute: %v", warmError)
	}
	if loaderCalls.Load() != 1 {
		testingContext.Fatalf("expected warm filtered Compute to reuse cached sections, got %d loader calls", loaderCalls.Load())
	}

	eventBus.PublishApp(events.AppExpeditionModesChanged)
	if _, refreshError := timeRangeStats.Compute(timeRange, modeFilter); refreshError != nil {
		testingContext.Fatalf("post-invalidation filtered Compute: %v", refreshError)
	}
	if loaderCalls.Load() != 2 {
		testingContext.Fatalf("expected post-invalidation Compute to reload mode lookup once (total 2), got %d", loaderCalls.Load())
	}
}
