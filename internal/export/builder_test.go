package export_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/core/projection"
	"planisphere/internal/export"
	projstats "planisphere/internal/projections/stats"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

type exportTestFixture struct {
	databaseConnection    *sql.DB
	bus                   *projection.Bus
	expeditionProjections *expedition.Projections
	careerStats           *projstats.CareerStats
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
	expeditionProjections := expedition.NewProjections(databaseConnection, bus)
	careerStats := projstats.NewCareerStats(
		databaseConnection,
		bus,
		expeditionProjections.SalesLedger,
		expeditionProjections.RecoveredJumps,
	)
	return &exportTestFixture{
		databaseConnection:    databaseConnection,
		bus:                   bus,
		expeditionProjections: expeditionProjections,
		careerStats:           careerStats,
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

func (fixture *exportTestFixture) builderDeps(fixedTime time.Time) export.BuilderDeps {
	return export.BuilderDeps{
		Projections: fixture.expeditionProjections,
		CareerStats: fixture.careerStats,
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

	if _, planningError := export.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime)); !errors.Is(planningError, export.ErrNotExportable) {
		testingContext.Fatalf("expected ErrNotExportable for planning expedition, got %v", planningError)
	}

	explorationID := fixture.lookupModeID(testingContext, "Exploration")
	if startError := expedition.StartTx(fixture.databaseConnection, expeditionRecord.ID, "2026-04-01T10:05:00Z", explorationID); startError != nil {
		testingContext.Fatalf("start expedition: %v", startError)
	}
	if _, stagedError := export.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime)); !errors.Is(stagedError, export.ErrNotExportable) {
		testingContext.Fatalf("expected ErrNotExportable for staged awaiting-departure expedition, got %v", stagedError)
	}
}

func TestBuildCompletedExpeditionBoundariesModeBucketsAndExactPostCareer(testingContext *testing.T) {
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

	// Subsequent post-expedition flight that must NOT leak into PostCareer for this completed expedition.
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

	snapshot, buildError := export.Build(fixture.databaseConnection, expeditionRecord.ID, fixture.builderDeps(fixedTime))
	if buildError != nil {
		testingContext.Fatalf("export.Build failed: %v", buildError)
	}

	if snapshot.SchemaVersion != export.SchemaVersion {
		testingContext.Fatalf("expected schemaVersion=%d, got %d", export.SchemaVersion, snapshot.SchemaVersion)
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

	// Verify exact historical accounting: OriginBaseline + Live == PostCareer (excluding May 10 flight).
	expectedPostDistance := snapshot.OriginBaseline.Metrics.DistanceLy + snapshot.Live.DistanceLy
	if math.Abs(snapshot.PostCareer.Metrics.DistanceLy-expectedPostDistance) > 0.001 {
		testingContext.Fatalf("PostCareer distance %.2f != OriginBaseline %.2f + Live %.2f", snapshot.PostCareer.Metrics.DistanceLy, snapshot.OriginBaseline.Metrics.DistanceLy, snapshot.Live.DistanceLy)
	}
	if snapshot.PostCareer.Metrics.Jumps != snapshot.OriginBaseline.Metrics.Jumps+snapshot.Live.Jumps {
		testingContext.Fatalf("PostCareer jumps %d != OriginBaseline %d + Live %d", snapshot.PostCareer.Metrics.Jumps, snapshot.OriginBaseline.Metrics.Jumps, snapshot.Live.Jumps)
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

	// Verify ComputeRange respects EffectiveStart inclusive departure jump and mode filtering.
	fullCustomRange, rangeError := export.ComputeRange(fixture.databaseConnection, fixture.expeditionProjections, expeditionRecord.ID, "2026-04-01T10:00:00Z", "2026-04-02T10:20:00Z", 0)
	if rangeError != nil {
		testingContext.Fatalf("ComputeRange combined failed: %v", rangeError)
	}
	if fullCustomRange.Metrics.Jumps != 3 || math.Abs(fullCustomRange.Metrics.DistanceLy-200.0) > 0.001 {
		testingContext.Fatalf("expected combined custom range 3 jumps / 200 Ly, got %d jumps / %.1f Ly", fullCustomRange.Metrics.Jumps, fullCustomRange.Metrics.DistanceLy)
	}

	travelOnlyRange, travelRangeError := export.ComputeRange(fixture.databaseConnection, fixture.expeditionProjections, expeditionRecord.ID, "", "", travelID)
	if travelRangeError != nil {
		testingContext.Fatalf("ComputeRange travel-only failed: %v", travelRangeError)
	}
	if travelOnlyRange.Metrics.Jumps != 1 || math.Abs(travelOnlyRange.Metrics.DistanceLy-80.0) > 0.001 {
		testingContext.Fatalf("expected travel-only range 1 jump / 80 Ly, got %d jumps / %.1f Ly", travelOnlyRange.Metrics.Jumps, travelOnlyRange.Metrics.DistanceLy)
	}
}
