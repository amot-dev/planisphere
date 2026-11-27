package exporter_test

import (
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/exporter"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

type exportTestFixture struct {
	databaseConnection    *sql.DB
	bus                   *projection.Bus
	expeditionProjections *expedition.Projections
}

func setupExportFixture(testingContext *testing.T) *exportTestFixture {
	testingContext.Helper()
	databaseConnection := dbtest.OpenMemory(testingContext)
	bus, expeditionProjections := expeditiontest.NewProjections(databaseConnection)
	return &exportTestFixture{
		databaseConnection:    databaseConnection,
		bus:                   bus,
		expeditionProjections: expeditionProjections,
	}
}

func (fixture *exportTestFixture) insertLines(testingContext *testing.T, lines ...string) {
	testingContext.Helper()
	journaltest.PublishParsedLines(testingContext, fixture.databaseConnection, fixture.bus, lines...)
}

func (fixture *exportTestFixture) builderDeps(fixedTime time.Time) exporter.BuilderDeps {
	return exporter.BuilderDeps{
		Projections: fixture.expeditionProjections,
		AppVersion:  "0.6.2-test",
		Now:         func() time.Time { return fixedTime },
	}
}

func TestBuildRejectsPlanningAndStagedExpeditions(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	fixture.insertLines(
		testingContext,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"LoadGame","Commander":"CMDR Tester","Ship":"Mandalay"}`,
		`{"timestamp":"2026-04-01T10:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
	)

	expeditionRecord, createError := expedition.CreateWithWaypoints(
		fixture.databaseConnection,
		"Staged Trip",
		"Still docked",
		[]expedition.SeedWaypoint{{SystemName: "Sol"}, {SystemName: "Colonia"}},
	)
	if createError != nil {
		testingContext.Fatalf("create expedition: %v", createError)
	}

	if _, planningError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime)); !errors.Is(planningError, exporter.ErrNotExportable) {
		testingContext.Fatalf("expected ErrNotExportable for planning expedition, got %v", planningError)
	}

	defaultModes := expeditiontest.LookupDefaultModes(testingContext, fixture.databaseConnection)
	if startError := expedition.StartTx(fixture.databaseConnection, expeditionRecord.ID, "2026-04-01T10:05:00Z", defaultModes.Exploration); startError != nil {
		testingContext.Fatalf("start expedition: %v", startError)
	}
	if _, stagedError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime)); !errors.Is(stagedError, exporter.ErrNotExportable) {
		testingContext.Fatalf("expected ErrNotExportable for staged awaiting-departure expedition, got %v", stagedError)
	}
}

func TestBuildCompletedExpeditionBoundariesAndModeBuckets(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	defaultModes := expeditiontest.LookupDefaultModes(testingContext, fixture.databaseConnection)
	explorationID := defaultModes.Exploration
	travelID := defaultModes.Travel

	// Pre-expedition history including an in-system jump into origin Sol, followed by Leg 1 (Exploration), Leg 2 (Travel), and post-expedition flight.
	fixture.insertLines(
		testingContext,
		`{"timestamp":"2026-03-01T08:00:00Z","event":"LoadGame","Commander":"CMDR Tester","Ship":"Mandalay"}`,
		`{"timestamp":"2026-03-01T09:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"JumpDist":25.0}`,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"FSDJump","StarSystem":"Midpoint One","SystemAddress":1001,"StarPos":[50,0,0],"JumpDist":50.0}`,
		`{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha Waypoint","SystemAddress":2002,"StarPos":[120,0,0],"JumpDist":70.0}`,
		`{"timestamp":"2026-04-01T11:15:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Alpha Waypoint A 1","BodyID":1,"StarSystem":"Alpha Waypoint","SystemAddress":2002,"PlanetClass":"High metal content body","MassEM":1.2,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-02T10:00:00Z","event":"FSDJump","StarSystem":"Beta Waypoint","SystemAddress":3003,"StarPos":[200,0,0],"JumpDist":80.0}`,
		`{"timestamp":"2026-04-02T10:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Beta Waypoint A 1","BodyID":1,"StarSystem":"Beta Waypoint","SystemAddress":3003,"PlanetClass":"Water world","MassEM":1.5,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-05-10T15:00:00Z","event":"FSDJump","StarSystem":"Distant Star","SystemAddress":9009,"StarPos":[400,0,0],"JumpDist":200.0}`,
	)

	expeditionRecord, createError := expedition.CreateWithWaypoints(
		fixture.databaseConnection,
		"Deep Range",
		"Boundary test",
		[]expedition.SeedWaypoint{
			{SystemName: "Sol"},
			{SystemName: "Alpha Waypoint"},
			{SystemName: "Beta Waypoint"},
		},
	)
	if createError != nil {
		testingContext.Fatalf("create expedition: %v", createError)
	}
	if startError := expedition.StartTx(fixture.databaseConnection, expeditionRecord.ID, "2026-03-01T09:00:00Z", explorationID); startError != nil {
		testingContext.Fatalf("start expedition: %v", startError)
	}
	rows, listError := expedition.ListRows(fixture.databaseConnection, expeditionRecord.ID)
	if listError != nil {
		testingContext.Fatalf("list rows: %v", listError)
	}
	if _, modeError := expedition.SetLegModeTx(fixture.databaseConnection, rows[2].ID, travelID, false); modeError != nil {
		testingContext.Fatalf("set leg 2 travel mode: %v", modeError)
	}
	if completeError := expedition.CompleteTx(fixture.databaseConnection, expeditionRecord.ID); completeError != nil {
		testingContext.Fatalf("complete expedition: %v", completeError)
	}

	snapshot, buildError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime))
	if buildError != nil {
		testingContext.Fatalf("exporter.Build failed: %v", buildError)
	}

	if snapshot.SchemaVersion != exporter.SchemaVersion {
		testingContext.Fatalf("expected schemaVersion=%d, got %d", exporter.SchemaVersion, snapshot.SchemaVersion)
	}
	if snapshot.EffectiveStart != "2026-04-01T10:00:00Z" {
		testingContext.Fatalf("expected EffectiveStart at WP1 departure 2026-04-01T10:00:00Z, got %q", snapshot.EffectiveStart)
	}
	if snapshot.EffectiveEnd != "2026-04-02T10:20:00Z" {
		testingContext.Fatalf("expected EffectiveEnd at final in-system scan 2026-04-02T10:20:00Z, got %q", snapshot.EffectiveEnd)
	}
	if snapshot.OriginBaseline == nil {
		testingContext.Fatalf("expected OriginBaseline to be populated")
	}
	if len(snapshot.RowDetails) != 2 {
		testingContext.Fatalf("expected 2 leg drawers, got %d", len(snapshot.RowDetails))
	}

	// Verify completed expedition boundaries exclude the May 10 post-expedition flight while preserving origin baseline.
	if math.Abs(snapshot.Live.DistanceLy-200.0) > 0.001 || snapshot.Live.Jumps != 3 {
		testingContext.Fatalf("expected completed Live distance 200.0 Ly and 3 jumps, got %.2f Ly and %d jumps", snapshot.Live.DistanceLy, snapshot.Live.Jumps)
	}
	if math.Abs(snapshot.OriginBaseline.Metrics.DistanceLy-25.0) > 0.001 || snapshot.OriginBaseline.Metrics.Jumps != 1 {
		testingContext.Fatalf("expected OriginBaseline distance 25.0 Ly and 1 jump, got %.2f Ly and %d jumps", snapshot.OriginBaseline.Metrics.DistanceLy, snapshot.OriginBaseline.Metrics.Jumps)
	}

	// Verify ModeBuckets additivity equals Full Expedition Live totals.
	if len(snapshot.ModeBuckets) != 2 {
		testingContext.Fatalf("expected 2 active mode buckets, got %d", len(snapshot.ModeBuckets))
	}
	var bucketSum replaystats.Metrics
	for _, bucket := range snapshot.ModeBuckets {
		bucketSum = bucketSum.Add(bucket.Metrics)
	}
	if math.Abs(bucketSum.DistanceLy-snapshot.Live.DistanceLy) > 0.001 || bucketSum.Jumps != snapshot.Live.Jumps || bucketSum.BodiesScanned != snapshot.Live.BodiesScanned {
		testingContext.Fatalf("mode buckets sum %+v != live %+v", bucketSum, snapshot.Live)
	}
	if snapshot.Expedition.ElapsedDays == nil || *snapshot.Expedition.ElapsedDays < 1.0 ||
		!reflect.DeepEqual(snapshot.Expedition.ShipsFlown, []string{"Mandalay"}) ||
		snapshot.Expedition.Derived.AvgDistancePerDayLy == nil ||
		snapshot.Expedition.Derived.NetProfitAfterCarrierCost == nil {
		testingContext.Fatalf("unexpected Expedition aggregated telemetry: %+v", snapshot.Expedition)
	}
	if snapshot.OriginBaselineElapsedDays == nil || snapshot.OriginBaselineDerived == nil || len(snapshot.CareerDiff) != 7 {
		testingContext.Fatalf("expected OriginBaseline elapsed/derived and 7 CareerDiff items, got elapsed=%v derived=%v diff=%d",
			snapshot.OriginBaselineElapsedDays, snapshot.OriginBaselineDerived, len(snapshot.CareerDiff))
	}
	if len(snapshot.LegTelemetry) != 2 || len(snapshot.RegionBuckets) != 1 ||
		len(snapshot.RegionBuckets[0].ModeBuckets) != 2 ||
		len(snapshot.HighlightsFullOrder) != 9 || len(snapshot.HighlightsVariants) != 3 {
		testingContext.Fatalf("unexpected leg/region/highlight aggregations: legs=%d regions=%d full=%d variants=%d",
			len(snapshot.LegTelemetry), len(snapshot.RegionBuckets), len(snapshot.HighlightsFullOrder), len(snapshot.HighlightsVariants))
	}
	for _, bucket := range snapshot.ModeBuckets {
		if bucket.ElapsedDays == nil || bucket.Derived.NetProfitAfterCarrierCost != nil || !reflect.DeepEqual(bucket.ShipsFlown, []string{"Mandalay"}) {
			testingContext.Fatalf("unexpected ModeBucket derived/ships fields: %+v", bucket)
		}
	}
}

func TestBuildPropagatesCommanderQueryError(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	fixture.insertLines(
		testingContext,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":2002,"StarPos":[30,0,0],"JumpDist":30.0}`,
	)

	expeditionRecord, createError := expedition.CreateWithWaypoints(
		fixture.databaseConnection,
		"Commander Error Check",
		"",
		[]expedition.SeedWaypoint{{SystemName: "Sol"}, {SystemName: "Alpha"}},
	)
	if createError != nil {
		testingContext.Fatalf("create expedition: %v", createError)
	}
	if startError := expedition.StartTx(fixture.databaseConnection, expeditionRecord.ID, "2026-04-01T10:00:00Z", 0); startError != nil {
		testingContext.Fatalf("start expedition: %v", startError)
	}
	if _, alterError := fixture.databaseConnection.Exec(`ALTER TABLE commanders DROP COLUMN first_seen`); alterError != nil {
		testingContext.Fatalf("drop commanders.first_seen: %v", alterError)
	}

	_, buildError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime))
	if buildError == nil || !strings.Contains(buildError.Error(), "list enabled commanders") {
		testingContext.Fatalf("expected commander query error propagation, got %v", buildError)
	}
}

func TestBuildSingleModeReuseUnusedModeSkippingAndBatchDetailsParity(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	defaultModes := expeditiontest.LookupDefaultModes(testingContext, fixture.databaseConnection)
	explorationID := defaultModes.Exploration
	tourismID := defaultModes.Tourism
	if _, createModeError := modes.Create(fixture.databaseConnection, "Unused Custom", "Never selected", "#123456"); createModeError != nil {
		testingContext.Fatalf("create custom mode: %v", createModeError)
	}
	if _, seedCmdrsError := fixture.databaseConnection.Exec(`
		INSERT INTO commanders (name, fid, enabled, first_seen, last_seen) VALUES
		('CMDR Second', 'F2', 1, '2026-04-02T00:00:00Z', '2026-04-02T00:00:00Z'),
		('CMDR Disabled', 'FD', 0, '2026-03-01T00:00:00Z', '2026-03-01T00:00:00Z'),
		('CMDR First', 'F1', 1, '2026-04-01T00:00:00Z', '2026-04-01T00:00:00Z')
	`); seedCmdrsError != nil {
		testingContext.Fatalf("seed commanders: %v", seedCmdrsError)
	}

	fixture.insertLines(
		testingContext,
		`{"timestamp":"2026-04-01T09:00:00Z","event":"LoadGame","Commander":"CMDR First","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1}`,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Midway","SystemAddress":2001,"StarPos":[40,0,0],"JumpDist":40.0}`,
		`{"timestamp":"2026-04-01T11:10:00Z","event":"ShipyardSwap","ShipType":"anaconda","ShipType_Localised":"Anaconda","ShipID":2}`,
		`{"timestamp":"2026-04-01T11:20:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":2002,"StarPos":[90,0,0],"JumpDist":50.0}`,
		`{"timestamp":"2026-04-01T11:30:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alpha","SystemAddress":2002,"BodyID":1,"BodyName":"Alpha 1","PlanetClass":"Water world","MassEM":1.1,"WasDiscovered":false,"WasMapped":false}`,
	)

	expeditionRecord, createError := expedition.CreateWithWaypoints(
		fixture.databaseConnection,
		"Mode Optimization Trip",
		"",
		[]expedition.SeedWaypoint{{SystemName: "Sol"}, {SystemName: "Alpha"}, {SystemName: "Beta"}},
	)
	if createError != nil {
		testingContext.Fatalf("create expedition: %v", createError)
	}
	if startError := expedition.StartTx(fixture.databaseConnection, expeditionRecord.ID, "2026-04-01T10:00:00Z", explorationID); startError != nil {
		testingContext.Fatalf("start expedition: %v", startError)
	}
	// While still docked in Alpha (DepartedAt == ""), changing ActiveModeID to Tourism must NOT prevent single-mode reuse for Exploration.
	if setModeError := expedition.SetActiveModeTx(fixture.databaseConnection, expeditionRecord.ID, tourismID); setModeError != nil {
		testingContext.Fatalf("set active mode tourism: %v", setModeError)
	}

	singleModeSnapshot, singleBuildError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime))
	if singleBuildError != nil {
		testingContext.Fatalf("single-mode exporter.Build: %v", singleBuildError)
	}
	if !reflect.DeepEqual(singleModeSnapshot.Commanders, []string{"CMDR First", "CMDR Second"}) {
		testingContext.Fatalf("unexpected enabled commanders: %v", singleModeSnapshot.Commanders)
	}
	if len(singleModeSnapshot.ModeBuckets) != 1 || singleModeSnapshot.ModeBuckets[0].ModeID != explorationID ||
		!reflect.DeepEqual(singleModeSnapshot.ModeBuckets[0].Metrics, singleModeSnapshot.Live) ||
		!reflect.DeepEqual(singleModeSnapshot.ModeBuckets[0].Realization, singleModeSnapshot.Expedition.Realization) {
		testingContext.Fatalf("expected single Exploration bucket reusing Live telemetry, got %+v", singleModeSnapshot.ModeBuckets)
	}
	cachedBeforeInvalidation := fixture.expeditionProjections.TimeRangeStats.CachedCount()
	fixture.bus.PublishApp(events.AppExpeditionModesChanged)
	if fixture.expeditionProjections.TimeRangeStats.CachedCount() != cachedBeforeInvalidation {
		testingContext.Fatalf("expected zero mode-filtered TimeRangeStats entries for single-mode expedition")
	}

	// Depart Alpha under Tourism: now Exploration (visited leg) and Tourism (trailing active leg) are active while Travel and Unused Custom are skipped.
	fixture.insertLines(testingContext, `{"timestamp":"2026-04-01T12:00:00Z","event":"FSDJump","StarSystem":"Outbound","SystemAddress":2009,"StarPos":[120,0,0],"JumpDist":30.0}`)
	if _, reevalError := expedition.ReevaluateTx(fixture.databaseConnection, expeditionRecord.ID); reevalError != nil {
		testingContext.Fatalf("reevaluate after departure: %v", reevalError)
	}

	multiModeSnapshot, multiBuildError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime))
	if multiBuildError != nil {
		testingContext.Fatalf("multi-mode exporter.Build: %v", multiBuildError)
	}
	if len(multiModeSnapshot.ModeBuckets) != 2 ||
		multiModeSnapshot.ModeBuckets[0].ModeID != explorationID ||
		multiModeSnapshot.ModeBuckets[1].ModeID != tourismID {
		testingContext.Fatalf("expected ordered [Exploration, Tourism] buckets, got %+v", multiModeSnapshot.ModeBuckets)
	}
	cachedWithTwoModes := fixture.expeditionProjections.TimeRangeStats.CachedCount()
	fixture.bus.PublishApp(events.AppExpeditionModesChanged)
	if cachedWithTwoModes-fixture.expeditionProjections.TimeRangeStats.CachedCount() != 2 {
		testingContext.Fatalf("expected exactly 2 mode-filtered TimeRangeStats entries (skipping unused Travel and Custom), got %d",
			cachedWithTwoModes-fixture.expeditionProjections.TimeRangeStats.CachedCount())
	}

	// Verify OriginBaseline and RowDetails bounds, elapsed labels, and multi-ship / VisitCurrent ship attribution directly.
	if multiModeSnapshot.OriginBaseline == nil || multiModeSnapshot.OriginBaseline.Kind != "origin" || multiModeSnapshot.OriginBaseline.To != "2026-04-01T11:00:00Z" {
		testingContext.Fatalf("unexpected OriginBaseline: %+v", multiModeSnapshot.OriginBaseline)
	}
	if len(multiModeSnapshot.RowDetails) != 2 {
		testingContext.Fatalf("expected 2 RowDetails (Alpha plus VisitCurrent Beta), got %d", len(multiModeSnapshot.RowDetails))
	}
	alphaLegDetail := multiModeSnapshot.RowDetails[0]
	if alphaLegDetail.From != "2026-04-01T11:00:00Z" || alphaLegDetail.To != "2026-04-01T12:00:00Z" ||
		alphaLegDetail.Elapsed == "" || !reflect.DeepEqual(alphaLegDetail.ShipsUsed, []string{"Mandalay", "Anaconda"}) {
		testingContext.Fatalf("unexpected closed Alpha RowDetails[0]: %+v", alphaLegDetail)
	}
	openLegDetail := multiModeSnapshot.RowDetails[1]
	if openLegDetail.From != "2026-04-01T12:00:00Z" || openLegDetail.To != "" ||
		openLegDetail.Elapsed == "" || !reflect.DeepEqual(openLegDetail.ShipsUsed, []string{"Anaconda"}) {
		testingContext.Fatalf("unexpected VisitCurrent RowDetails[1]: %+v", openLegDetail)
	}
}

func TestBuildArchivedPreWipeCareerSelectedExpedition(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	histExp, _ := expeditiontest.SeedArchivedPreWipeExpedition(testingContext, fixture.databaseConnection)
	if scopeError := settings.SetCareerBaselineScope(fixture.databaseConnection, settings.CareerScopeExpedition); scopeError != nil {
		testingContext.Fatal(scopeError)
	}

	exportSnapshot, buildError := exporter.Build(fixture.databaseConnection, histExp.ID, fixture.builderDeps(fixedTime))
	if buildError != nil {
		testingContext.Fatalf("exporter.Build archived expedition: %v", buildError)
	}
	if !reflect.DeepEqual(exportSnapshot.Commanders, []string{"CMDR Pioneer"}) {
		testingContext.Fatalf("expected Commanders=[CMDR Pioneer], got %v", exportSnapshot.Commanders)
	}
	if exportSnapshot.CareerStart != "2024-05-01T10:00:00Z" || exportSnapshot.OriginBaseline == nil || math.Abs(exportSnapshot.OriginBaseline.Metrics.DistanceLy-20.0) > 0.01 {
		testingContext.Fatalf("expected pre-wipe CareerStart and OriginBaseline 20.0 Ly, got start=%q baseline=%+v",
			exportSnapshot.CareerStart, exportSnapshot.OriginBaseline)
	}
	if math.Abs(exportSnapshot.Live.DistanceLy-22000.0) > 0.01 || len(exportSnapshot.RowDetails) != 1 || !reflect.DeepEqual(exportSnapshot.RowDetails[0].ShipsUsed, []string{"Anaconda"}) {
		testingContext.Fatalf("expected 22000 Ly Anaconda leg on archived expedition export, got live=%+v details=%+v",
			exportSnapshot.Live, exportSnapshot.RowDetails)
	}
}
