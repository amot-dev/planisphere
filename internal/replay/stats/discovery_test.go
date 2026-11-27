package stats_test

import (
	"testing"

	"planisphere/internal/reference"
	"planisphere/internal/replay/stats"
	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func expectedEfficientELWMapping(probesUsed int) (reference.ScanValue, reference.MappingIncrementBreakdown) {
	earthlikeScan := reference.BodyScan{PlanetClass: "Earthlike body", MassEM: 1.0}
	return reference.EstimateBodyValue(earthlikeScan, reference.MappingInfo{}, true),
		reference.EstimateMappingIncrementBreakdown(earthlikeScan, reference.MappingInfo{Mapped: true, ProbesUsed: probesUsed, EfficiencyTarget: 6}, true)
}

func TestPrepareComputeScansOrderingAndRangeBounds(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	engineInstance := newEngineWithModes(databaseConnection)

	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"SAAScanComplete","BodyName":"SameSec 1","BodyID":3,"SystemAddress":7001,"ProbesUsed":4,"EfficiencyTarget":6}`,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"Scan","ScanType":"Detailed","BodyName":"SameSec 1","BodyID":3,"StarSystem":"SameSec","SystemAddress":7001,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)

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

	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T11:10:00Z","event":"SAAScanComplete","BodyName":"FutureScan 1","BodyID":4,"SystemAddress":7002,"ProbesUsed":4,"EfficiencyTarget":6}`,
		`{"timestamp":"2026-05-01T11:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"FutureScan 1","BodyID":4,"StarSystem":"FutureScan","SystemAddress":7002,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-05-01T12:05:00Z","event":"Scan","ScanType":"Detailed","BodyName":"PreloadSys 1","BodyID":5,"StarSystem":"PreloadSys","SystemAddress":9999,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-05-01T12:30:00Z","event":"SAAScanComplete","BodyName":"PreloadSys 1","BodyID":5,"SystemAddress":9999,"ProbesUsed":4,"EfficiencyTarget":6}`,
	)

	futureScanWindow := stats.Range{After: "2026-05-01T11:00:00Z", Until: "2026-05-01T12:00:00Z"}
	futureScanMetrics, futureScanError := engineInstance.ComputeCartographics(futureScanWindow, nil)
	if futureScanError != nil {
		testingContext.Fatalf("Compute future-scan range failed: %v", futureScanError)
	}
	if futureScanMetrics.FirstMapped != 0 || futureScanMetrics.ScanValueTotal != expectedScanOnly.Total {
		testingContext.Fatalf("expected future scan not to preload for earlier mapping (FirstMapped=0, ScanValueTotal=%d), got FirstMapped=%d ScanValueTotal=%d",
			expectedScanOnly.Total, futureScanMetrics.FirstMapped, futureScanMetrics.ScanValueTotal)
	}

	preloadedMappingWindow := stats.Range{After: "2026-05-01T12:15:00Z", Until: "2026-05-01T12:45:00Z"}
	preloadedMetrics, preloadedError := engineInstance.ComputeCartographics(preloadedMappingWindow, nil)
	if preloadedError != nil {
		testingContext.Fatalf("Compute preloaded mapping range failed: %v", preloadedError)
	}
	if preloadedMetrics.BodiesMapped != 1 || preloadedMetrics.FirstMapped != 1 || preloadedMetrics.ScanValueTotal != expectedMapping.Total {
		testingContext.Fatalf("expected pre-window scan preloaded for mapped body (BodiesMapped=1, FirstMapped=1, ScanValueTotal=%d), got %+v",
			expectedMapping.Total, preloadedMetrics)
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
			databaseConnection := seedStatsDB(subTest, testCase.journalLines...)
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

func TestComputeExcludesPlanetaryRingMappings(testingContext *testing.T) {
	journalLines := []string{
		`{"timestamp":"2026-07-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-07-01T10:01:00Z","event":"Scan","ScanType":"Detailed","BodyName":"RingTest 1","BodyID":1,"StarSystem":"RingTest","SystemAddress":9501,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-07-01T10:02:00Z","event":"Scan","ScanType":"AutoScan","BodyName":"RingTest 1 A Ring","BodyID":2,"StarSystem":"RingTest","SystemAddress":9501,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-07-01T10:03:00Z","event":"SAAScanComplete","BodyName":"RingTest 1 A Ring","BodyID":2,"SystemAddress":9501,"ProbesUsed":1,"EfficiencyTarget":0}`,
		`{"timestamp":"2026-07-01T10:04:00Z","event":"SAAScanComplete","BodyName":"RingTest 1 B Ring","BodyID":3,"SystemAddress":9501,"ProbesUsed":2,"EfficiencyTarget":0}`,
		`{"timestamp":"2026-07-01T10:05:00Z","event":"SAAScanComplete","BodyName":"RingTest 1","BodyID":1,"SystemAddress":9501,"ProbesUsed":5,"EfficiencyTarget":6}`,
	}
	databaseConnection := seedStatsDB(testingContext, journalLines...)
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

func TestFootfalledBodyUpgradesExobiologyBonusWhenSoldWithBonus(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engine := stats.NewEngine(databaseConnection)

	// Body already has WasFootfalled: true (another commander landed previously without logging organics).
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T09:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"FootfalledSys","SystemAddress":6001,"BodyID":1,"BodyName":"FootfalledSys 1","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":6001,"Body":1}`,
	)

	legWindow := stats.Range{After: "2026-03-01T09:55:00Z", Until: "2026-03-01T10:30:00Z"}

	// Before sale: estimated at base value only (19,010,800 Cr) because WasFootfalled is true.
	beforeMetrics, beforeError := engine.ComputeExobiology(legWindow, nil)
	if beforeError != nil {
		t.Fatalf("ComputeExobiology before sale failed: %v", beforeError)
	}
	if beforeMetrics.FirstLogged != 0 {
		t.Errorf("expected FirstLogged 0 before sale, got %d", beforeMetrics.FirstLogged)
	}
	if beforeMetrics.OrganicValueTotal != 19010800 {
		t.Errorf("expected OrganicValueTotal 19,010,800 before sale, got %d", beforeMetrics.OrganicValueTotal)
	}

	// Sold on a later leg with the 4x first-logged bonus (76,043,200 Cr).
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T14:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":76043200}
		]}`,
	)

	// After sale: the original leg's Exobiology Value upgrades to include the first-logged bonus (95,054,000 Cr).
	afterMetrics, afterError := engine.ComputeExobiology(legWindow, nil)
	if afterError != nil {
		t.Fatalf("ComputeExobiology after sale failed: %v", afterError)
	}
	if afterMetrics.FirstLogged != 1 {
		t.Errorf("expected FirstLogged 1 after sale, got %d", afterMetrics.FirstLogged)
	}
	if afterMetrics.OrganicValueBonus != 76043200 {
		t.Errorf("expected OrganicValueBonus 76,043,200 after sale, got %d", afterMetrics.OrganicValueBonus)
	}
	if afterMetrics.OrganicValueTotal != 95054000 {
		t.Errorf("expected OrganicValueTotal 95,054,000 after sale, got %d", afterMetrics.OrganicValueTotal)
	}
}
