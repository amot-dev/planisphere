package stats_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay/stats"
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

	engine := newEngineWithModes(conn)

	// Seeded window starting after Location ("19:20:00Z") must still resolve previous StarPos ([0,0,0]) for CarrierJump.
	unfiltered, computeError := engine.ComputeTravel(stats.Range{After: "2026-01-18T19:20:00Z"}, nil)
	if computeError != nil {
		t.Fatal(computeError)
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

	filtered, computeError := engine.ComputeTravel(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
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

	filteredTravelOn, computeError := engine.ComputeTravel(stats.Range{After: "2026-01-18T19:00:00Z"}, &stats.Filter{
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

	engine := newEngineWithModes(conn)
	computeResult, computeError := engine.ComputeTravel(stats.Range{After: "2026-01-18T20:00:00Z"}, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modeA, Travel: false, Discovery: true},
			{ModeID: modeB, Travel: true, Discovery: true},
		},
	})
	if computeError != nil {
		t.Fatal(computeError)
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

	engine := newEngineWithModes(conn)
	mappedMetrics, computeError := engine.ComputeCartographics(stats.Range{After: "2026-01-18T19:00:00Z"}, nil)
	if computeError != nil {
		t.Fatal(computeError)
	}
	if mappedMetrics.BodiesMapped != 1 {
		t.Fatalf("expected 1 body mapped, got %d", mappedMetrics.BodiesMapped)
	}
	if mappedMetrics.FirstMapped != 1 {
		t.Fatalf("expected 1 first mapped, got %d", mappedMetrics.FirstMapped)
	}
	if mappedMetrics.ScanValueTotal == 0 {
		t.Fatalf("expected positive scan value total from preloaded scan, got %d", mappedMetrics.ScanValueTotal)
	}
}

func TestPrepareComputeScansOrderingAndRangeBounds(testingContext *testing.T) {
	databaseConnection := setupRealizationDB(testingContext)
	engineInstance := newEngineWithModes(databaseConnection)

	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`)
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"SAAScanComplete","BodyName":"SameSec 1","BodyID":3,"SystemAddress":7001,"ProbesUsed":4,"EfficiencyTarget":6}`)
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"Scan","ScanType":"Detailed","BodyName":"SameSec 1","BodyID":3,"StarSystem":"SameSec","SystemAddress":7001,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	expectedScanOnly, expectedMapping := expectedEfficientELWMapping(4)

	fallbackModeFilter := &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modes.DefaultFallbackModeID(databaseConnection), Travel: true, Discovery: true},
		},
	}
	sameTimestampMetrics, sameTimestampError := engineInstance.ComputeCartographics(stats.Range{}, fallbackModeFilter)
	if sameTimestampError != nil {
		testingContext.Fatalf("ComputeCartographics same-timestamp all-time filtered failed: %v", sameTimestampError)
	}
	expectedCombinedTotal := expectedScanOnly.Total + expectedMapping.Total
	if sameTimestampMetrics.FirstMapped != 1 || sameTimestampMetrics.EfficientMappings != 1 || sameTimestampMetrics.ScanValueTotal != expectedCombinedTotal {
		testingContext.Fatalf("expected same-timestamp mapping to resolve parent scan (FirstMapped=1, EfficientMappings=1, ScanValueTotal=%d), got FirstMapped=%d EfficientMappings=%d ScanValueTotal=%d",
			expectedCombinedTotal, sameTimestampMetrics.FirstMapped, sameTimestampMetrics.EfficientMappings, sameTimestampMetrics.ScanValueTotal)
	}
	if sameTimestampMetrics.ScanValueEfficiencyBonus != expectedMapping.EfficiencyBonus ||
		sameTimestampMetrics.ScanValueBase+sameTimestampMetrics.ScanValueBonus+sameTimestampMetrics.ScanValueEfficiencyBonus != sameTimestampMetrics.ScanValueTotal {
		testingContext.Fatalf("unexpected 3-way scan value split: base=%d bonus=%d efficiency=%d total=%d (want efficiency=%d)",
			sameTimestampMetrics.ScanValueBase, sameTimestampMetrics.ScanValueBonus, sameTimestampMetrics.ScanValueEfficiencyBonus, sameTimestampMetrics.ScanValueTotal, expectedMapping.EfficiencyBonus)
	}

	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T11:10:00Z","event":"SAAScanComplete","BodyName":"FutureScan 1","BodyID":4,"SystemAddress":7002,"ProbesUsed":4,"EfficiencyTarget":6}`)
	ingestTestLine(testingContext, databaseConnection,
		`{"timestamp":"2026-05-01T11:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"FutureScan 1","BodyID":4,"StarSystem":"FutureScan","SystemAddress":7002,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	futureScanWindow := stats.Range{After: "2026-05-01T11:00:00Z", Until: "2026-05-01T12:00:00Z"}
	futureScanMetrics, futureScanError := engineInstance.ComputeCartographics(futureScanWindow, nil)
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
		`{"timestamp":"2026-11-17T01:20:00Z","event":"FSDJump","StarSystem":"ShuttleHop","SystemAddress":900101,"StarPos":[-9890.0,-5.0,-7565.0],"JumpDist":14.5,"Taxi":true,"Population":50000}`,
		`{"timestamp":"2026-11-17T01:25:00Z","event":"FSDJump","StarSystem":"Hypo Aeb ST-B b54-0","SystemAddress":538078878161,"StarPos":[-9894.84375,-5.5625,-7569.25],"JumpDist":14.5,"Multicrew":true}`,
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

	engineInstance := newEngineWithModes(databaseConnection)

	legMetrics, legError := engineInstance.ComputeTravel(stats.Range{After: "2026-11-17T01:05:00Z", Until: "2026-11-17T02:00:00Z"}, nil)
	if legError != nil {
		testingContext.Fatal(legError)
	}
	expectedLegDistance := reference.Distance(reference.Vec3{X: -9894.84375, Y: -5.5625, Z: -7569.25}, reference.Vec3{X: -9962.1875, Y: -1.3125, Z: -7604.875})
	if legMetrics.Jumps != 1 || legMetrics.JetConeBoosts != 1 || legMetrics.ShipDistanceLy != expectedLegDistance || legMetrics.DistanceLy != expectedLegDistance {
		testingContext.Fatalf("unexpected bounded-window recovered metrics: %+v (want dist %.6f)", legMetrics, expectedLegDistance)
	}

	travelExcluded, filterError := engineInstance.ComputeTravel(stats.Range{After: "2026-11-17T01:05:00Z", Until: "2026-11-17T02:00:00Z"}, &stats.Filter{
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

func TestComputeExobiologyChronologicalScanResolution(testingContext *testing.T) {
	testCases := []struct {
		name           string
		journalLines   []string
		wantSamples    int
		wantFirstLog   int
		wantValueTotal int64
	}{
		{
			name: "revisit footfall flips after initial unfootfalled scan",
			journalLines: []string{
				`{"timestamp":"2026-06-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RevisitSys","SystemAddress":8001,"BodyID":1,"BodyName":"RevisitSys 1","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
				`{"timestamp":"2026-06-01T10:05:00Z","event":"ScanOrganic","ScanType":"Log","Genus":"$Codex_Ent_Bacterial_Genus_Name;","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","SystemAddress":8001,"Body":1}`,
				`{"timestamp":"2026-06-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Bacterial_Genus_Name;","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","SystemAddress":8001,"Body":1}`,
				`{"timestamp":"2026-06-05T12:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RevisitSys","SystemAddress":8001,"BodyID":1,"BodyName":"RevisitSys 1","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`,
				`{"timestamp":"2026-06-05T12:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Stratum_Genus_Name;","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","SystemAddress":8001,"Body":1}`,
			},
			wantSamples:    2,
			wantFirstLog:   1,
			wantValueTotal: 5000000 + 19010800,
		},
		{
			name: "delayed autoscan after first organic analysis",
			journalLines: []string{
				`{"timestamp":"2026-06-02T10:05:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Bacterial_Genus_Name;","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","SystemAddress":8002,"Body":2}`,
				`{"timestamp":"2026-06-02T10:10:00Z","event":"Scan","ScanType":"AutoScan","StarSystem":"DelaySys","SystemAddress":8002,"BodyID":2,"BodyName":"DelaySys 2","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
				`{"timestamp":"2026-06-02T10:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Stratum_Genus_Name;","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","SystemAddress":8002,"Body":2}`,
			},
			wantSamples:    2,
			wantFirstLog:   1,
			wantValueTotal: 1000000 + 95054000,
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(subTest *testing.T) {
			databaseConnection := seedStatsDBFromLines(subTest, testCase.journalLines)
			engineInstance := newEngineWithModes(databaseConnection)
			exobiologyMetrics, computeError := engineInstance.ComputeExobiology(stats.Range{}, nil)
			if computeError != nil {
				subTest.Fatalf("ComputeExobiology failed: %v", computeError)
			}
			if exobiologyMetrics.OrganicSamples != testCase.wantSamples || exobiologyMetrics.FirstLogged != testCase.wantFirstLog || exobiologyMetrics.OrganicValueTotal != testCase.wantValueTotal {
				subTest.Fatalf("got %+v, want OrganicSamples=%d FirstLogged=%d OrganicValueTotal=%d",
					exobiologyMetrics, testCase.wantSamples, testCase.wantFirstLog, testCase.wantValueTotal)
			}
		})
	}
}

func newEngineWithModes(queryer db.Queryer) *stats.Engine {
	return stats.NewEngineWithSources(queryer, nil, nil, modes.LoadModeLookup)
}

func expectedEfficientELWMapping(probesUsed int) (reference.ScanValue, reference.MappingIncrementBreakdown) {
	earthlikeScan := reference.BodyScan{PlanetClass: "Earthlike body", MassEM: 1.0}
	return reference.EstimateBodyValue(earthlikeScan, reference.MappingInfo{}, true),
		reference.EstimateMappingIncrementBreakdown(earthlikeScan, reference.MappingInfo{Mapped: true, ProbesUsed: probesUsed, EfficiencyTarget: 6}, true)
}

func TestComputeExcludesPlanetaryRingMappings(testingContext *testing.T) {
	journalLines := []string{
		`{"timestamp":"2026-07-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-07-01T10:01:00Z","event":"Scan","ScanType":"Detailed","BodyName":"RingTest 1","BodyID":1,"StarSystem":"RingTest","SystemAddress":9501,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-07-01T10:02:00Z","event":"Scan","ScanType":"AutoScan","BodyName":"RingTest 1 A Ring","BodyID":2,"StarSystem":"RingTest","SystemAddress":9501,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-07-01T10:03:00Z","event":"SAAScanComplete","BodyName":"RingTest 1 A Ring","BodyID":2,"SystemAddress":9501,"ProbesUsed":1,"EfficiencyTarget":0}`,
		`{"timestamp":"2026-07-01T10:04:00Z","event":"SAAScanComplete","BodyName":"RingTest 1 B Ring","BodyID":3,"SystemAddress":9501,"ProbesUsed":2,"EfficiencyTarget":0}`,
		`{"timestamp":"2026-07-01T10:05:00Z","event":"SAAScanComplete","BodyName":"RingTest 1","BodyID":1,"SystemAddress":9501,"ProbesUsed":5,"EfficiencyTarget":6}`,
	}
	databaseConnection := seedStatsDBFromLines(testingContext, journalLines)
	engineInstance := newEngineWithModes(databaseConnection)

	computedMetrics, computeError := engineInstance.ComputeCartographics(stats.Range{}, nil)
	if computeError != nil {
		testingContext.Fatalf("ComputeCartographics failed: %v", computeError)
	}

	expectedScan, expectedMapping := expectedEfficientELWMapping(5)
	expectedTotal := expectedScan.Total + expectedMapping.Total

	if computedMetrics.BodiesMapped != 1 || computedMetrics.EfficientMappings != 1 || computedMetrics.FirstMapped != 1 || computedMetrics.ScanValueTotal != expectedTotal {
		testingContext.Fatalf("got BodiesMapped=%d EfficientMappings=%d FirstMapped=%d ScanValueTotal=%d, want 1, 1, 1, %d",
			computedMetrics.BodiesMapped, computedMetrics.EfficientMappings, computedMetrics.FirstMapped, computedMetrics.ScanValueTotal, expectedTotal)
	}
}
