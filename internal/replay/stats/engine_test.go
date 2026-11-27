package stats_test

import (
	"testing"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/replay/stats"
	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestCarrierJumpPassengerDistanceRespectsModeFilter(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay","Odyssey":true}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:30:00Z","event":"CarrierJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100]}`,
		`{"timestamp":"2026-01-18T19:50:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1003,"StarPos":[0,0,106],"JumpDist":6.0}`,
		`{"timestamp":"2026-01-18T20:10:00Z","event":"CarrierJump","StarSystem":"System1","SystemAddress":1004,"StarPos":[0,0,206]}`,
		`{"timestamp":"2026-01-18T20:30:00Z","event":"CarrierJump","StarSystem":"System2","SystemAddress":1005,"StarPos":[0,0,406]}`,
	}
	conn := seedStatsDB(t, lines...)

	defaultModes := expeditiontest.LookupDefaultModes(t, conn)
	explorationModeID := defaultModes.Exploration
	tourismModeID := defaultModes.Tourism
	fallbackModeID := defaultModes.Fallback

	engine := newEngineWithModeLookup(conn, func(timestamp string) int64 {
		switch {
		case timestamp >= "2026-01-18T19:20:00Z" && timestamp < "2026-01-18T20:20:00Z":
			return explorationModeID
		case timestamp >= "2026-01-18T20:20:00Z" && timestamp < "2026-01-18T20:40:00Z":
			return tourismModeID
		default:
			return fallbackModeID
		}
	})

	testCases := []struct {
		name              string
		window            stats.Range
		filter            *stats.Filter
		expectedPassenger float64
		expectedShip      float64
		expectedTotal     float64
		expectedJumps     int
	}{
		{
			name:              "unfiltered window resolves pre-window StarPos baseline",
			window:            stats.Range{After: "2026-01-18T19:20:00Z", Until: "2026-01-18T20:00:00Z"},
			filter:            nil,
			expectedPassenger: 100.0,
			expectedShip:      6.0,
			expectedTotal:     106.0,
			expectedJumps:     1,
		},
		{
			name:   "mode filter with Travel false excludes carrier passenger and ship travel",
			window: stats.Range{After: "2026-01-18T19:00:00Z", Until: "2026-01-18T20:00:00Z"},
			filter: &stats.Filter{
				Enabled: true,
				Modes:   []stats.ModeSelection{{ModeID: explorationModeID, Travel: false, Discovery: true}},
			},
			expectedPassenger: 0,
			expectedShip:      0,
			expectedTotal:     0,
			expectedJumps:     0,
		},
		{
			name:   "mode filter with Travel true retains carrier passenger and ship travel",
			window: stats.Range{After: "2026-01-18T19:00:00Z", Until: "2026-01-18T20:00:00Z"},
			filter: &stats.Filter{
				Enabled: true,
				Modes:   []stats.ModeSelection{{ModeID: explorationModeID, Travel: true, Discovery: true}},
			},
			expectedPassenger: 100.0,
			expectedShip:      6.0,
			expectedTotal:     106.0,
			expectedJumps:     1,
		},
		{
			name:   "spanning modes measures included carrier jump from excluded carrier jump StarPos",
			window: stats.Range{After: "2026-01-18T20:00:00Z"},
			filter: &stats.Filter{
				Enabled: true,
				Modes: []stats.ModeSelection{
					{ModeID: explorationModeID, Travel: false, Discovery: true},
					{ModeID: tourismModeID, Travel: true, Discovery: true},
				},
			},
			expectedPassenger: 200.0,
			expectedShip:      0,
			expectedTotal:     200.0,
			expectedJumps:     0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(subTest *testing.T) {
			computed, computeError := engine.ComputeTravel(testCase.window, testCase.filter)
			if computeError != nil {
				subTest.Fatalf("ComputeTravel: %v", computeError)
			}
			if computed.CarrierPassengerLy != testCase.expectedPassenger ||
				computed.ShipDistanceLy != testCase.expectedShip ||
				computed.DistanceLy != testCase.expectedTotal ||
				computed.Jumps != testCase.expectedJumps {
				subTest.Fatalf("got %+v, want passenger=%.1f ship=%.1f total=%.1f jumps=%d",
					computed, testCase.expectedPassenger, testCase.expectedShip, testCase.expectedTotal, testCase.expectedJumps)
			}
		})
	}
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
	databaseConnection := seedStatsDB(testingContext, lines...)

	defaultModes := expeditiontest.LookupDefaultModes(testingContext, databaseConnection)
	explorationModeID := defaultModes.Exploration
	fallbackModeID := defaultModes.Fallback

	engineInstance := newEngineWithModeLookup(databaseConnection, func(timestamp string) int64 {
		if timestamp >= "2026-11-17T01:10:00Z" && timestamp < "2026-11-17T02:00:00Z" {
			return explorationModeID
		}
		return fallbackModeID
	})

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

func TestTravelMulticrewDoesNotPolluteCarrierJumpBaseline(testingContext *testing.T) {
	testingContext.Run("in-window multicrew arrivals leave baseline intact while taxi updates it", func(subTest *testing.T) {
		journalLines := []string{
			`{"timestamp":"2026-08-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
			`{"timestamp":"2026-08-01T10:10:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":1002,"StarPos":[9000,0,0],"Multicrew":true}`,
			`{"timestamp":"2026-08-01T10:20:00Z","event":"CarrierJump","StarSystem":"CarrierLeg1","SystemAddress":1003,"StarPos":[100,0,0]}`,
			`{"timestamp":"2026-08-01T10:30:00Z","event":"FSDJump","StarSystem":"Sagittarius A*","SystemAddress":1004,"StarPos":[0,9000,0],"JumpDist":50.0,"Multicrew":true}`,
			`{"timestamp":"2026-08-01T10:40:00Z","event":"CarrierJump","StarSystem":"CarrierLeg2","SystemAddress":1005,"StarPos":[120,0,0]}`,
			`{"timestamp":"2026-08-01T10:50:00Z","event":"FSDJump","StarSystem":"ShuttleStop","SystemAddress":1006,"StarPos":[150,0,0],"JumpDist":30.0,"Taxi":true}`,
			`{"timestamp":"2026-08-01T11:00:00Z","event":"CarrierJump","StarSystem":"CarrierLeg3","SystemAddress":1007,"StarPos":[200,0,0]}`,
		}
		databaseConnection := seedStatsDB(subTest, journalLines...)
		engineInstance := newEngineWithModes(databaseConnection)

		travelMetrics, computeError := engineInstance.ComputeTravel(stats.Range{After: "2026-08-01T09:50:00Z"}, nil)
		if computeError != nil {
			subTest.Fatalf("ComputeTravel failed: %v", computeError)
		}
		if travelMetrics.Jumps != 0 || travelMetrics.ShipDistanceLy != 0 {
			subTest.Fatalf("expected zero piloted jumps and ship distance, got %+v", travelMetrics)
		}
		if travelMetrics.CarrierPassengerLy != 170.0 || travelMetrics.DistanceLy != 170.0 {
			subTest.Fatalf("expected CarrierPassengerLy=170.0 and DistanceLy=170.0, got %+v", travelMetrics)
		}
	})

	testingContext.Run("pre-window baseline lookup skips multicrew arrivals", func(subTest *testing.T) {
		journalLines := []string{
			`{"timestamp":"2026-08-02T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":2001,"StarPos":[0,0,0]}`,
			`{"timestamp":"2026-08-02T10:10:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[5000,0,0],"Multicrew":true}`,
			`{"timestamp":"2026-08-02T10:20:00Z","event":"FSDJump","StarSystem":"Beagle Point","SystemAddress":2003,"StarPos":[8000,0,0],"JumpDist":25.0,"Multicrew":true}`,
			`{"timestamp":"2026-08-02T10:40:00Z","event":"CarrierJump","StarSystem":"CarrierDest","SystemAddress":2004,"StarPos":[100,0,0]}`,
		}
		databaseConnection := seedStatsDB(subTest, journalLines...)
		engineInstance := newEngineWithModes(databaseConnection)

		// Window starting after both multicrew arrivals tests skipping Multicrew FSDJump first.
		afterBothMetrics, afterBothError := engineInstance.ComputeTravel(stats.Range{After: "2026-08-02T10:30:00Z"}, nil)
		if afterBothError != nil {
			subTest.Fatalf("ComputeTravel after both multicrew arrivals failed: %v", afterBothError)
		}
		if afterBothMetrics.CarrierPassengerLy != 100.0 || afterBothMetrics.DistanceLy != 100.0 {
			subTest.Fatalf("expected CarrierPassengerLy=100.0 and DistanceLy=100.0 after multicrew FSDJump, got %+v", afterBothMetrics)
		}

		// Window starting between multicrew Location and multicrew FSDJump tests skipping Multicrew Location in SQL.
		afterLocationMetrics, afterLocationError := engineInstance.ComputeTravel(stats.Range{After: "2026-08-02T10:15:00Z"}, nil)
		if afterLocationError != nil {
			subTest.Fatalf("ComputeTravel after multicrew Location failed: %v", afterLocationError)
		}
		if afterLocationMetrics.CarrierPassengerLy != 100.0 || afterLocationMetrics.DistanceLy != 100.0 {
			subTest.Fatalf("expected CarrierPassengerLy=100.0 and DistanceLy=100.0 after multicrew Location, got %+v", afterLocationMetrics)
		}
	})
}

func TestComputeRecoversOfflineAndCrashedCarrierPassengerJumps(testingContext *testing.T) {
	lines := []string{
		`{"timestamp":"2026-11-23T22:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX","Odyssey":true}`,
		`{"timestamp":"2026-11-23T22:00:58Z","event":"FSDJump","StarSystem":"StartSys","SystemAddress":9001,"StarPos":[0,0,0],"JumpDist":40.0}`,
		`{"timestamp":"2026-11-23T22:04:45Z","event":"Docked","StarSystem":"StartSys","SystemAddress":9001,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		`{"timestamp":"2026-11-23T22:25:10Z","event":"CarrierJump","StarSystem":"CarrierHop1","SystemAddress":9002,"StarPos":[400,0,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		`{"timestamp":"2026-11-23T22:46:07Z","event":"CarrierJump","StarSystem":"CarrierHop2","SystemAddress":9003,"StarPos":[800,0,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		// Crash mid-jump into StartSys (revisit) and reconnect via Location (+800 Ly return)
		`{"timestamp":"2026-11-24T00:40:49Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX","Odyssey":true}`,
		`{"timestamp":"2026-11-24T00:41:06Z","event":"Location","StarSystem":"StartSys","SystemAddress":9001,"StarPos":[0,0,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
		// Subsequent online CarrierJump from StartSys to FinalHop (+250 Ly) must measure from StartSys with no gap or double-count
		`{"timestamp":"2026-11-24T01:05:00Z","event":"CarrierJump","StarSystem":"FinalHop","SystemAddress":9004,"StarPos":[0,250,0],"Docked":true,"StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3701199360}`,
	}
	databaseConnection := seedStatsDB(testingContext, lines...)

	defaultModes := expeditiontest.LookupDefaultModes(testingContext, databaseConnection)
	explorationModeID := defaultModes.Exploration
	fallbackModeID := defaultModes.Fallback

	engineInstance := newEngineWithModeLookup(databaseConnection, func(timestamp string) int64 {
		if timestamp >= "2026-11-24T00:00:00Z" && timestamp < "2026-11-24T01:00:00Z" {
			return explorationModeID
		}
		return fallbackModeID
	})

	carrierWindow := stats.Range{After: "2026-11-23T22:01:00Z", Until: "2026-11-24T02:00:00Z"}
	unfiltered, computeError := engineInstance.ComputeTravel(carrierWindow, nil)
	if computeError != nil {
		testingContext.Fatalf("ComputeTravel unfiltered: %v", computeError)
	}
	// 400 (online) + 400 (online) + 800 (crash-recovered Location) + 250 (online) = 1850 Ly; zero piloted ship jumps.
	if unfiltered.Jumps != 0 || unfiltered.ShipDistanceLy != 0 || unfiltered.CarrierPassengerLy != 1850.0 || unfiltered.DistanceLy != 1850.0 {
		testingContext.Fatalf("unexpected unfiltered carrier metrics: %+v (want Jumps=0 ShipDistanceLy=0 CarrierPassengerLy=1850.0)", unfiltered)
	}

	// Excluding explorationModeID Travel must exclude only the 800 Ly recovered hop at 00:41:06Z, leaving 1050 Ly.
	filtered, filterError := engineInstance.ComputeTravel(carrierWindow, &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: explorationModeID, Travel: false, Discovery: true},
			{ModeID: fallbackModeID, Travel: true, Discovery: true},
		},
	})
	if filterError != nil {
		testingContext.Fatalf("ComputeTravel filtered: %v", filterError)
	}
	if filtered.CarrierPassengerLy != 1050.0 || filtered.DistanceLy != 1050.0 {
		testingContext.Fatalf("expected filtered CarrierPassengerLy=1050.0, got %+v", filtered)
	}

	// Recovered carrier jumps must count toward system visit counts (StartSys visited via FSDJump + offline carrier return = 2)
	// while remaining excluded from piloted ship jump timestamps used for vessels flown.
	recovered, recoverError := replay.ComputeRecoveredJumpsScoped(databaseConnection, replay.ProfileScope{})
	if recoverError != nil {
		testingContext.Fatalf("ComputeRecoveredJumpsScoped: %v", recoverError)
	}
	visitCounts, visitError := replay.QuerySystemVisitCountsScoped(databaseConnection, []string{"startsys"}, recovered, replay.ProfileScope{})
	if visitError != nil {
		testingContext.Fatalf("QuerySystemVisitCountsScoped: %v", visitError)
	}
	if visitCounts["startsys"] != 2 {
		testingContext.Fatalf("expected StartSys visit count 2 (FSDJump + offline carrier return), got %d", visitCounts["startsys"])
	}
	pilotedTimestamps, jumpQueryError := replay.QueryJumpTimestampsScoped(databaseConnection, "2026-11-23T22:01:00Z", "2026-11-24T02:00:00Z", recovered, replay.ProfileScope{})
	if jumpQueryError != nil {
		testingContext.Fatalf("QueryJumpTimestampsScoped: %v", jumpQueryError)
	}
	if len(pilotedTimestamps) != 0 {
		testingContext.Fatalf("expected recovered carrier jump excluded from piloted jump timestamps, got %v", pilotedTimestamps)
	}
}
