package stats_test

import (
	"sync/atomic"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/journaltest"
)

func TestTimeRangeStatsCachingAndInvalidation(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":1,"BodyName":"RangeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	}
	harness := newTimeRangeStatsHarness(testingContext, lines, nil)
	databaseConnection := harness.conn
	eventBus := harness.bus
	careerStats := harness.careerStats
	timeRangeStats := harness.timeRangeStats

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
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":2,"BodyName":"RangeSys 2","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)
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
	journaltest.PublishParsedLines(
		testingContext,
		databaseConnection,
		eventBus,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":3,"BodyName":"RangeSys 3","PlanetClass":"Ammonia world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)
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
		Modes:        []stats.ModeSelection{{ModeID: harness.fallbackModeID, Travel: true, Discovery: true}},
	}
	filteredBeforeWindowChange, filteredBeforeError := timeRangeStats.Compute(timeRange, modeFilter)
	if filteredBeforeError != nil || filteredBeforeWindowChange.Metrics.BodiesScanned != 3 {
		testingContext.Fatalf("expected mode-filtered BodiesScanned=3 before invalidation, got %d (err=%v)",
			filteredBeforeWindowChange.Metrics.BodiesScanned, filteredBeforeError)
	}
	if timeRangeStats.CachedCount() != 2 {
		testingContext.Fatalf("expected 2 cached entries (unfiltered and filtered), got %d", timeRangeStats.CachedCount())
	}

	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-01-18T19:40:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RangeSys","SystemAddress":5555,"BodyID":4,"BodyName":"RangeSys 4","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)
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

func TestTimeRangeSelectiveInvalidationPreservesClosedWindowsExceptRetroactiveEvents(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"FSDJump","StarSystem":"ClosedSys","SystemAddress":1001,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"ClosedSys","SystemAddress":1001,"BodyID":1,"BodyName":"ClosedSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1001,"Body":1}`,
	}
	harness := newTimeRangeStatsHarness(testingContext, lines, nil)
	eventBus := harness.bus
	timeRangeStats := harness.timeRangeStats

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
	var loaderCalls atomic.Int32
	var fallbackModeID int64
	countingModeLoader := func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
		loaderCalls.Add(1)
		return func(_ string) int64 { return fallbackModeID }, nil
	}
	harness := newTimeRangeStatsHarness(testingContext, lines, countingModeLoader)
	fallbackModeID = harness.fallbackModeID
	eventBus := harness.bus
	timeRangeStats := harness.timeRangeStats

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

func TestTimeRangeStatsClosedWindowRevisionWatermark(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"FSDJump","StarSystem":"SeedSys","SystemAddress":1001,"StarPos":[10,0,0],"JumpDist":10.0}`,
	}
	harness := newTimeRangeStatsHarness(testingContext, lines, nil)
	eventBus := harness.bus
	timeRangeStats := harness.timeRangeStats

	if gotRevision := timeRangeStats.ClosedWindowRevision(); gotRevision != 1 {
		testingContext.Fatalf("expected initial ClosedWindowRevision 1, got %d", gotRevision)
	}

	// Chronological forward non-retroactive gameplay batches advance the high-water mark without incrementing ClosedWindowRevision.
	forwardBatches := []struct {
		events    journal.EventSet
		timestamp string
	}{
		{events: journal.NewEventSet(journal.EventFSDJump), timestamp: "2026-01-18T20:00:00Z"},
		{events: journal.NewEventSet(journal.EventScan, journal.EventSAAScanComplete), timestamp: "2026-01-18T20:10:00Z"},
		{events: journal.NewEventSet(journal.EventScanOrganic), timestamp: "2026-01-18T20:20:00Z"},
	}
	for _, batch := range forwardBatches {
		eventBus.PublishJournalSince(batch.events, batch.timestamp)
		if gotRevision := timeRangeStats.ClosedWindowRevision(); gotRevision != 1 {
			testingContext.Fatalf("expected forward batch at %s to keep ClosedWindowRevision 1, got %d", batch.timestamp, gotRevision)
		}
	}
	// Multi-event forward range [T1, T3] advances highWaterTimestamp to T3 without incrementing ClosedWindowRevision.
	eventBus.PublishJournalRange(
		journal.NewEventSet(journal.EventFSDJump, journal.EventShipyardSwap),
		"2026-01-18T20:22:00Z",
		"2026-01-18T20:28:00Z",
	)
	if gotRevision := timeRangeStats.ClosedWindowRevision(); gotRevision != 1 {
		testingContext.Fatalf("expected forward [T1, T3] range to preserve ClosedWindowRevision 1, got %d", gotRevision)
	}

	expectedRevision := uint64(1)
	assertIncrement := func(label string, trigger func()) {
		testingContext.Helper()
		trigger()
		expectedRevision++
		if gotRevision := timeRangeStats.ClosedWindowRevision(); gotRevision != expectedRevision {
			testingContext.Fatalf("%s: expected ClosedWindowRevision %d, got %d", label, expectedRevision, gotRevision)
		}
	}

	assertIncrement("out-of-order T2 within [T1, T3] range", func() {
		eventBus.PublishJournalRange(journal.NewEventSet(journal.EventScan), "2026-01-18T20:25:00Z", "2026-01-18T20:25:00Z")
	})
	assertIncrement("backfilled ShipyardSwap without metric triggers", func() {
		eventBus.PublishJournalRange(journal.NewEventSet(journal.EventShipyardSwap), "2026-01-18T20:26:00Z", "2026-01-18T20:26:00Z")
	})
	assertIncrement("SellOrganicData", func() {
		eventBus.PublishJournalSince(journal.NewEventSet(journal.EventSellOrganicData), "2026-01-18T20:30:00Z")
	})
	assertIncrement("MultiSellExplorationData", func() {
		eventBus.PublishJournalSince(journal.NewEventSet(journal.EventMultiSellExplorationData), "2026-01-18T20:40:00Z")
	})
	assertIncrement("Resurrect", func() {
		eventBus.PublishJournalSince(journal.NewEventSet(journal.EventResurrect), "2026-01-18T20:50:00Z")
	})
	assertIncrement("NpcCrewPaidWage", func() {
		eventBus.PublishJournalSince(journal.NewEventSet(journal.EventNpcCrewPaidWage), "2026-01-18T21:00:00Z")
	})
	assertIncrement("AppCommanderFilterChanged", func() {
		eventBus.PublishApp(events.AppCommanderFilterChanged)
	})
	assertIncrement("empty EarliestTimestamp", func() {
		eventBus.PublishJournal(journal.NewEventSet(journal.EventScan))
	})
	assertIncrement("historical backfill equal to highWaterTimestamp", func() {
		eventBus.PublishJournalSince(journal.NewEventSet(journal.EventFSDJump), "2026-01-18T21:00:00Z")
	})
	assertIncrement("historical backfill strictly before highWaterTimestamp", func() {
		eventBus.PublishJournalSince(journal.NewEventSet(journal.EventScan), "2026-01-18T18:00:00Z")
	})

	// Subsequent forward live gameplay above the preserved 21:00:00Z high-water mark stays non-invalidating.
	eventBus.PublishJournalSince(journal.NewEventSet(journal.EventFSDJump), "2026-01-18T22:00:00Z")
	if gotRevision := timeRangeStats.ClosedWindowRevision(); gotRevision != expectedRevision {
		testingContext.Fatalf("expected forward batch after backfill to preserve ClosedWindowRevision %d, got %d", expectedRevision, gotRevision)
	}

	monotonicFloor := expectedRevision + 5
	timeRangeStats.EnsureMinClosedWindowRevision(monotonicFloor)
	timeRangeStats.EnsureMinClosedWindowRevision(expectedRevision + 2)
	if gotRevision := timeRangeStats.ClosedWindowRevision(); gotRevision != monotonicFloor {
		testingContext.Fatalf("expected EnsureMinClosedWindowRevision monotonic floor %d, got %d", monotonicFloor, gotRevision)
	}
}

func TestTimeRangeStatsIsolatesScopedProfilesAndPreservesUnfilteredScopeKeys(testingContext *testing.T) {
	harness, _, mainScope, altScope := seedScopedMultiProfileArchive(testingContext)
	eventBus := harness.bus
	fallbackModeID := harness.fallbackModeID
	timeRangeStats := harness.timeRangeStats

	window := stats.Range{After: "2026-02-01T11:00:00Z", Until: "2026-02-01T12:00:00Z"}
	mainRange, err := timeRangeStats.Compute(window, &stats.Filter{Scope: mainScope})
	if err != nil {
		testingContext.Fatalf("TimeRangeStats.Compute(main): %v", err)
	}
	altRange, err := timeRangeStats.Compute(window, &stats.Filter{Scope: altScope})
	if err != nil {
		testingContext.Fatalf("TimeRangeStats.Compute(alt): %v", err)
	}
	if mainRange.Metrics.Jumps != 1 || mainRange.Metrics.DistanceLy != 60.0 || mainRange.Metrics.BodiesScanned != 1 || mainRange.Metrics.OrganicSamples != 0 {
		testingContext.Fatalf("unexpected main scoped window metrics: %+v", mainRange.Metrics)
	}
	if altRange.Metrics.Jumps != 1 || altRange.Metrics.DistanceLy != 25.0 || altRange.Metrics.BodiesScanned != 0 || altRange.Metrics.OrganicSamples != 1 {
		testingContext.Fatalf("unexpected alt scoped window metrics: %+v", altRange.Metrics)
	}

	// Mode-filtered scoped entry must be evicted by AppExpeditionModesChanged while mode-unfiltered scoped entries ("|all|prof:...") stay cached!
	if _, err := timeRangeStats.Compute(window, &stats.Filter{
		Enabled:      true,
		ExpeditionID: 1,
		Modes:        []stats.ModeSelection{{ModeID: fallbackModeID, Travel: true, Discovery: true}},
		Scope:        mainScope,
	}); err != nil {
		testingContext.Fatalf("TimeRangeStats.Compute(mode+scope): %v", err)
	}
	if timeRangeStats.CachedCount() != 3 {
		testingContext.Fatalf("expected 3 cached window entries (main, alt, main+mode), got %d", timeRangeStats.CachedCount())
	}
	eventBus.PublishApp(events.AppExpeditionModesChanged)
	if timeRangeStats.CachedCount() != 2 {
		testingContext.Fatalf("expected AppExpeditionModesChanged to preserve both unfiltered scoped '|all|prof:...' keys (want 2), got %d", timeRangeStats.CachedCount())
	}
}

func TestTimeRangeSectionDirectComputeMatchesCompositeSummary(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"FSDJump","StarSystem":"SubSys","SystemAddress":3003,"StarPos":[40,0,0],"JumpDist":40.0}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"SubSys","SystemAddress":3003,"BodyID":1,"BodyName":"SubSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":3003,"Body":1}`,
	}
	harness := newTimeRangeStatsHarness(testingContext, lines, nil)
	timeRangeStats := harness.timeRangeStats
	window := stats.Range{After: "2026-01-18T19:00:00Z", Until: "2026-01-18T20:00:00Z"}

	travelMetrics, travelErr := timeRangeStats.Travel().Compute(window, nil)
	cartoMetrics, cartoErr := timeRangeStats.Cartographics().Compute(window, nil)
	exoMetrics, exoErr := timeRangeStats.Exobiology().Compute(window, nil)
	realizationMetrics, realErr := timeRangeStats.Realization().Compute(window, nil)
	if travelErr != nil || cartoErr != nil || exoErr != nil || realErr != nil {
		testingContext.Fatalf("direct section Compute errors: travel=%v carto=%v exo=%v real=%v", travelErr, cartoErr, exoErr, realErr)
	}
	composite, compositeErr := timeRangeStats.Compute(window, nil)
	if compositeErr != nil {
		testingContext.Fatalf("composite Compute: %v", compositeErr)
	}
	if travelMetrics.Jumps != 1 || travelMetrics.Jumps != composite.Metrics.Jumps ||
		cartoMetrics.BodiesScanned != 1 || cartoMetrics.ScanValueTotal != composite.Metrics.ScanValueTotal ||
		exoMetrics.OrganicSamples != 1 || exoMetrics.OrganicValueTotal != composite.Metrics.OrganicValueTotal ||
		realizationMetrics != composite.Realization {
		testingContext.Fatalf("section Compute mismatch against composite: travel=%+v carto=%+v exo=%+v real=%+v composite=%+v",
			travelMetrics, cartoMetrics, exoMetrics, realizationMetrics, composite)
	}
}
