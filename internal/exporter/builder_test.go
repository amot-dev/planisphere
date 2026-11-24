package exporter_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/exporter"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

type exportTestFixture struct {
	databaseConnection    *sql.DB
	bus                   *projection.Bus
	expeditionProjections *expedition.Projections
}

func newTestProjections(databaseConnection *sql.DB, bus *projection.Bus) *expedition.Projections {
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, bus)
	salesLedger := realization.NewSalesLedger(databaseConnection, bus)
	return &expedition.Projections{
		Arrivals:       projections.NewSystemArrivals(databaseConnection, bus),
		VisitCounts:    projections.NewSystemVisitCounts(databaseConnection, bus, recoveredJumps),
		ShipHistory:    projections.NewShipHistory(databaseConnection, bus),
		TimeRangeStats: stats.NewTimeRangeStats(databaseConnection, bus, salesLedger, recoveredJumps, expedition.LoadModeLookup),
		SalesLedger:    salesLedger,
		RecoveredJumps: recoveredJumps,
	}
}

func setupExportFixture(testingContext *testing.T) *exportTestFixture {
	testingContext.Helper()
	databasePath := filepath.Join(testingContext.TempDir(), "export_test.sqlite")
	databaseConnection, openError := db.Open(databasePath)
	if openError != nil {
		testingContext.Fatalf("open test db: %v", openError)
	}
	testingContext.Cleanup(func() {
		if closeError := databaseConnection.Close(); closeError != nil {
			testingContext.Errorf("close test db: %v", closeError)
		}
	})

	bus := projection.NewBus()
	expeditionProjections := newTestProjections(databaseConnection, bus)
	return &exportTestFixture{
		databaseConnection:    databaseConnection,
		bus:                   bus,
		expeditionProjections: expeditionProjections,
	}
}

func (fixture *exportTestFixture) insertEvent(testingContext *testing.T, timestamp, eventName, keyIdentifier, rawJSON string) {
	testingContext.Helper()
	var systemEnvelope struct {
		StarSystem string `json:"StarSystem"`
	}
	if unmarshalError := json.Unmarshal([]byte(rawJSON), &systemEnvelope); unmarshalError != nil {
		testingContext.Fatalf("unmarshal test event %s: %v", eventName, unmarshalError)
	}
	eventUID := journal.ComputeUID(timestamp, eventName, keyIdentifier)
	if _, execError := fixture.databaseConnection.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, systemEnvelope.StarSystem, rawJSON, timestamp,
	); execError != nil {
		testingContext.Fatalf("insert test event %s: %v", eventName, execError)
	}
	if parsedEvent, ok := journal.ParseEventType(eventName); ok {
		fixture.bus.PublishJournal(journal.NewEventSet(parsedEvent))
	}
}

func (fixture *exportTestFixture) lookupModeID(testingContext *testing.T, targetName string) int64 {
	testingContext.Helper()
	modeList, listError := modes.List(fixture.databaseConnection)
	if listError != nil {
		testingContext.Fatalf("list modes: %v", listError)
	}
	for _, modeItem := range modeList {
		if modeItem.Name == targetName {
			return modeItem.ID
		}
	}
	testingContext.Fatalf("mode %q not found", targetName)
	return 0
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

	fixture.insertEvent(testingContext, "2026-04-01T10:00:00Z", "LoadGame", "cmdr", `{"timestamp":"2026-04-01T10:00:00Z","event":"LoadGame","Commander":"CMDR Tester","Ship":"Mandalay"}`)
	fixture.insertEvent(testingContext, "2026-04-01T10:05:00Z", "Location", "sol", `{"timestamp":"2026-04-01T10:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`)

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

	explorationID := fixture.lookupModeID(testingContext, "Exploration")
	if startError := expedition.StartTx(fixture.databaseConnection, expeditionRecord.ID, "2026-04-01T10:05:00Z", explorationID); startError != nil {
		testingContext.Fatalf("start expedition: %v", startError)
	}
	if _, stagedError := exporter.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime)); !errors.Is(stagedError, exporter.ErrNotExportable) {
		testingContext.Fatalf("expected ErrNotExportable for staged awaiting-departure expedition, got %v", stagedError)
	}
}

func TestBuildCompletedExpeditionBoundariesAndModeBuckets(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	explorationID := fixture.lookupModeID(testingContext, "Exploration")
	travelID := fixture.lookupModeID(testingContext, "Travel")

	// Pre-expedition history including an in-system jump into origin Sol.
	fixture.insertEvent(testingContext, "2026-03-01T08:00:00Z", "LoadGame", "cmdr", `{"timestamp":"2026-03-01T08:00:00Z","event":"LoadGame","Commander":"CMDR Tester","Ship":"Mandalay"}`)
	fixture.insertEvent(testingContext, "2026-03-01T09:00:00Z", "FSDJump", "pre", `{"timestamp":"2026-03-01T09:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"JumpDist":25.0}`)

	// Leg 1 (Exploration): depart Sol -> arrive Waypoint Alpha + in-system scan.
	fixture.insertEvent(testingContext, "2026-04-01T10:00:00Z", "FSDJump", "leg1-a", `{"timestamp":"2026-04-01T10:00:00Z","event":"FSDJump","StarSystem":"Midpoint One","SystemAddress":1001,"StarPos":[50,0,0],"JumpDist":50.0}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:00:00Z", "FSDJump", "leg1-b", `{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha Waypoint","SystemAddress":2002,"StarPos":[120,0,0],"JumpDist":70.0}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:15:00Z", "Scan", "alpha-scan", `{"timestamp":"2026-04-01T11:15:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Alpha Waypoint A 1","BodyID":1,"StarSystem":"Alpha Waypoint","SystemAddress":2002,"PlanetClass":"High metal content body","MassEM":1.2,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`)

	// Leg 2 (Travel): depart Alpha -> arrive Beta Waypoint + final in-system scan.
	fixture.insertEvent(testingContext, "2026-04-02T10:00:00Z", "FSDJump", "leg2-a", `{"timestamp":"2026-04-02T10:00:00Z","event":"FSDJump","StarSystem":"Beta Waypoint","SystemAddress":3003,"StarPos":[200,0,0],"JumpDist":80.0}`)
	fixture.insertEvent(testingContext, "2026-04-02T10:20:00Z", "Scan", "beta-scan", `{"timestamp":"2026-04-02T10:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Beta Waypoint A 1","BodyID":1,"StarSystem":"Beta Waypoint","SystemAddress":3003,"PlanetClass":"Water world","MassEM":1.5,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`)

	// Subsequent post-expedition flight that must NOT leak into completed expedition telemetry.
	fixture.insertEvent(testingContext, "2026-05-10T15:00:00Z", "FSDJump", "later-trip", `{"timestamp":"2026-05-10T15:00:00Z","event":"FSDJump","StarSystem":"Distant Star","SystemAddress":9009,"StarPos":[400,0,0],"JumpDist":200.0}`)

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
}

func TestBuildPropagatesCommanderQueryError(testingContext *testing.T) {
	fixture := setupExportFixture(testingContext)
	fixedTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	fixture.insertEvent(testingContext, "2026-04-01T10:00:00Z", "Location", "sol", `{"timestamp":"2026-04-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:00:00Z", "FSDJump", "alpha", `{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":2002,"StarPos":[30,0,0],"JumpDist":30.0}`)

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
	explorationID := fixture.lookupModeID(testingContext, "Exploration")
	tourismID := fixture.lookupModeID(testingContext, "Tourism")
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

	fixture.insertEvent(testingContext, "2026-04-01T09:00:00Z", "LoadGame", "lg", `{"timestamp":"2026-04-01T09:00:00Z","event":"LoadGame","Commander":"CMDR First","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1}`)
	fixture.insertEvent(testingContext, "2026-04-01T10:00:00Z", "Location", "sol", `{"timestamp":"2026-04-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:00:00Z", "FSDJump", "mid", `{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Midway","SystemAddress":2001,"StarPos":[40,0,0],"JumpDist":40.0}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:10:00Z", "ShipyardSwap", "swap", `{"timestamp":"2026-04-01T11:10:00Z","event":"ShipyardSwap","ShipType":"anaconda","ShipType_Localised":"Anaconda","ShipID":2}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:20:00Z", "FSDJump", "alpha", `{"timestamp":"2026-04-01T11:20:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":2002,"StarPos":[90,0,0],"JumpDist":50.0}`)
	fixture.insertEvent(testingContext, "2026-04-01T11:30:00Z", "Scan", "scan", `{"timestamp":"2026-04-01T11:30:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alpha","SystemAddress":2002,"BodyID":1,"BodyName":"Alpha 1","PlanetClass":"Water world","MassEM":1.1,"WasDiscovered":false,"WasMapped":false}`)

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
	fixture.insertEvent(testingContext, "2026-04-01T12:00:00Z", "FSDJump", "outbound", `{"timestamp":"2026-04-01T12:00:00Z","event":"FSDJump","StarSystem":"Outbound","SystemAddress":2009,"StarPos":[120,0,0],"JumpDist":30.0}`)
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

	// Verify OriginBaseline and RowDetails match per-row expedition.Details (including multi-ship leg and VisitCurrent leg).
	expectedOrigin, originError := expedition.Details(fixture.databaseConnection, multiModeSnapshot.Plan.Rows[0].Row.ID, fixture.expeditionProjections)
	if originError != nil || multiModeSnapshot.OriginBaseline == nil || !reflect.DeepEqual(*multiModeSnapshot.OriginBaseline, expectedOrigin) {
		testingContext.Fatalf("OriginBaseline parity mismatch: got %+v want %+v (err=%v)", multiModeSnapshot.OriginBaseline, expectedOrigin, originError)
	}
	if len(multiModeSnapshot.RowDetails) != 2 {
		testingContext.Fatalf("expected 2 RowDetails (Alpha + VisitCurrent Beta), got %d", len(multiModeSnapshot.RowDetails))
	}
	for index, rowDetail := range multiModeSnapshot.RowDetails {
		expectedDetail, detailError := expedition.Details(fixture.databaseConnection, rowDetail.RowID, fixture.expeditionProjections)
		if detailError != nil {
			testingContext.Fatalf("Details(%d): %v", rowDetail.RowID, detailError)
		}
		if rowDetail.To == "" && rowDetail.Elapsed != "" && expectedDetail.Elapsed != "" {
			rowDetail.Elapsed = expectedDetail.Elapsed
		}
		if !reflect.DeepEqual(rowDetail, expectedDetail) {
			testingContext.Fatalf("RowDetails[%d] parity mismatch:\ngot:  %+v\nwant: %+v", index, rowDetail, expectedDetail)
		}
	}
}
