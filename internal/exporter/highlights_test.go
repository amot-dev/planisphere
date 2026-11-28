package exporter_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"planisphere/internal/exporter"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

func TestComputeHighlightLeaderboardsParityAndTieBreaking(testingContext *testing.T) {
	pinnedNow := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

	planView := expedition.PlanView{
		Rows: []expedition.RowView{
			{
				Row:        expedition.Row{ID: 100, RowType: expedition.RowRegion},
				RegionName: "Inner Orion Spur",
			},
			{
				Row: expedition.Row{
					ID:         1,
					RowType:    expedition.RowWaypoint,
					SystemName: "Sol",
					HasCoords:  true,
					X:          0,
					Y:          0,
					Z:          0,
					VisitedAt:  "2026-04-01T08:00:00Z",
					DepartedAt: "2026-04-01T10:00:00Z",
				},
				Number:         1,
				DetectedRegion: "Inner Orion Spur",
			},
			{
				Row: expedition.Row{
					ID:         2,
					RowType:    expedition.RowWaypoint,
					SystemName: "Alpha Rim",
					HasCoords:  true,
					X:          1000,
					Y:          0,
					Z:          0,
					VisitedAt:  "2026-04-02T10:00:00Z",
					DepartedAt: "2026-04-02T12:00:00Z",
				},
				Number:         2,
				DetectedRegion: "Inner Orion Spur",
				LegModeID:      1,
				LegModeName:    "Exploration",
				LegModeColor:   "#00D2FF",
			},
			{
				Row: expedition.Row{
					ID:         3,
					RowType:    expedition.RowWaypoint,
					SystemName: "Beta Crossing",
					HasCoords:  true,
					X:          3000,
					Y:          0,
					Z:          4000,
					VisitedAt:  "2026-04-04T12:00:00Z",
					DepartedAt: "2026-04-04T14:00:00Z",
				},
				Number:         3,
				DetectedRegion: "",
				LegModeID:      2,
				LegModeName:    "Travel",
				LegModeColor:   "#FF7100",
			},
			{
				Row: expedition.Row{
					ID:         4,
					RowType:    expedition.RowWaypoint,
					SystemName: "Odin Beacon",
					HasCoords:  true,
					X:          1200,
					Y:          0,
					Z:          1600,
					VisitedAt:  "2026-04-05T10:00:00Z",
				},
				Number:         4,
				DetectedRegion: "Odin's Hold",
				LegModeID:      1,
				LegModeName:    "Exploration",
				LegModeColor:   "#00D2FF",
			},
			{
				Row: expedition.Row{
					ID:         5,
					RowType:    expedition.RowWaypoint,
					SystemName: "Unvisited Frontier",
					HasCoords:  true,
					X:          9000,
					Y:          0,
					Z:          0,
				},
				Number:     5,
				VisitState: expedition.VisitCurrent,
			},
		},
	}

	rowDetails := []expedition.WaypointDetails{
		{
			RowID:      2,
			Kind:       "leg",
			Number:     2,
			PrevSystem: "",
			From:       "2026-04-01T10:00:00Z",
			To:         "2026-04-02T12:00:00Z",
			Elapsed:    "1d 02h 00m",
			Metrics: replaystats.Metrics{
				DistanceLy:         1234.56,
				ShipDistanceLy:     0,
				CarrierPassengerLy: 234.56,
				Jumps:              20,
				BodiesScanned:      40,
				BodiesMapped:       10,
				FirstDiscoveries:   30,
				ScanValueTotal:     12_000_000,
				OrganicSamples:     4,
				FirstLogged:        4,
				OrganicValueTotal:  40_000_000,
			},
		},
		{
			RowID:      3,
			Kind:       "leg",
			Number:     3,
			PrevSystem: "Alpha Rim",
			From:       "2026-04-02T12:00:00Z",
			To:         "2026-04-04T14:00:00Z",
			Elapsed:    "2d 02h 00m",
			Metrics: replaystats.Metrics{
				DistanceLy:        3000.0,
				ShipDistanceLy:    3000.0,
				Jumps:             20,
				BodiesScanned:     15,
				BodiesMapped:      4,
				FirstDiscoveries:  5,
				ScanValueTotal:    5_000_000,
				OrganicSamples:    1,
				FirstLogged:       1,
				OrganicValueTotal: 10_000_000,
			},
			Realization: replaystats.RealizationBreakdown{
				CartoLostDeath:   1_000_000,
				OrganicLostDeath: 2_000_000,
			},
		},
		{
			RowID:      4,
			Kind:       "leg",
			Number:     4,
			PrevSystem: "Beta Crossing",
			From:       "2026-04-04T14:00:00Z",
			To:         "",
			InSystem:   true,
			Elapsed:    "",
			Metrics: replaystats.Metrics{
				DistanceLy:        600.0,
				ShipDistanceLy:    600.0,
				Jumps:             10,
				BodiesScanned:     60,
				BodiesMapped:      20,
				FirstDiscoveries:  50,
				ScanValueTotal:    25_000_000,
				OrganicSamples:    8,
				FirstLogged:       7,
				OrganicValueTotal: 80_000_000,
			},
		},
		{
			RowID:      5,
			Kind:       "leg",
			Number:     5,
			PrevSystem: "Odin Beacon",
			From:       "2026-04-05T11:00:00Z",
			Metrics:    replaystats.Metrics{DistanceLy: 50.0, ShipDistanceLy: 50.0, Jumps: 1},
		},
	}

	boards := exporter.ComputeHighlightLeaderboards(planView, rowDetails, pinnedNow)
	expectedOrder := []string{
		"mostLucrative",
		"longestDistance",
		"mostJumps",
		"longestDuration",
		"shortestDistance",
		"mostBodiesScanned",
		"mostOrganicSamples",
		"furthestFromSol",
		"costliestMistake",
		"highestEarningPace",
		"fastestTransitPace",
	}
	if len(boards) != len(expectedOrder) {
		testingContext.Fatalf("expected %d boards, got %d", len(expectedOrder), len(boards))
	}
	boardByID := make(map[string]exporter.HighlightLeaderboard, len(boards))
	for index, board := range boards {
		if board.CardID != expectedOrder[index] {
			testingContext.Fatalf("board[%d] cardId=%q, want %q", index, board.CardID, expectedOrder[index])
		}
		boardByID[board.CardID] = board
	}

	lucrative := boardByID["mostLucrative"]
	if len(lucrative.Entries) != 3 || lucrative.Entries[0].WaypointNumber != 4 {
		testingContext.Fatalf("unexpected mostLucrative entries: %+v", lucrative.Entries)
	}
	if lucrative.Entries[0].FormattedPrimary != "105,000,000 Cr" ||
		lucrative.Entries[0].SecondaryLabel != "Carto 25,000,000 Cr · Exo 80,000,000 Cr" ||
		!lucrative.Entries[0].InSystem {
		testingContext.Fatalf("unexpected mostLucrative winner formatting: %+v", lucrative.Entries[0])
	}
	if lucrative.Entries[1].RouteLabel != "Origin -> Alpha Rim" || lucrative.Entries[1].Label != "Leg #1 -> #2" {
		testingContext.Fatalf("expected fallback Origin route label, got %+v", lucrative.Entries[1])
	}

	// Legs #2 and #3 tie at 20 jumps; stable plan order must keep Waypoint #2 ahead of Waypoint #3,
	// and Leg #2 must derive ship distance as DistanceLy - CarrierPassengerLy (1,000.0 Ly).
	jumps := boardByID["mostJumps"]
	if jumps.Entries[0].WaypointNumber != 2 || jumps.Entries[1].WaypointNumber != 3 {
		testingContext.Fatalf("expected stable tie-break [2, 3] on mostJumps, got [%d, %d]",
			jumps.Entries[0].WaypointNumber, jumps.Entries[1].WaypointNumber)
	}
	if jumps.Entries[0].FormattedPrimary != "20 jumps" || jumps.Entries[0].SecondaryLabel != "1,000.0 Ly · 50.0 Ly avg" {
		testingContext.Fatalf("unexpected mostJumps formatting: %+v", jumps.Entries[0])
	}

	// Open-ended parked Leg #4 uses pinnedNow (22h 00m) when Elapsed is empty, and unvisited Leg #5 is excluded.
	duration := boardByID["longestDuration"]
	if len(duration.Entries) != 3 || duration.Entries[2].WaypointNumber != 4 || duration.Entries[2].FormattedPrimary != "22h 00m" {
		testingContext.Fatalf("unexpected longestDuration entries: %+v", duration.Entries)
	}
	shortest := boardByID["shortestDistance"]
	if shortest.Entries[0].WaypointNumber != 4 || shortest.Entries[0].FormattedPrimary != "600.0 Ly" {
		testingContext.Fatalf("expected WP #4 (600.0 Ly) to win shortestDistance excluding unvisited WP #5, got %+v", shortest.Entries[0])
	}
	longest := boardByID["longestDistance"]
	if longest.Entries[1].FormattedPrimary != "1,234.6 Ly" || longest.Entries[1].SecondaryLabel != "20 jumps · 1d 02h 00m" {
		testingContext.Fatalf("unexpected longestDistance formatting: %+v", longest.Entries[1])
	}

	bodies := boardByID["mostBodiesScanned"]
	if bodies.Entries[0].FormattedPrimary != "60 bodies" || bodies.Entries[0].SecondaryLabel != "20 mapped · 50 first disc." {
		testingContext.Fatalf("unexpected mostBodiesScanned winner: %+v", bodies.Entries[0])
	}
	organics := boardByID["mostOrganicSamples"]
	if organics.Entries[0].FormattedPrimary != "8 samples" || organics.Entries[0].SecondaryLabel != "7 first logged · 80,000,000 Cr" {
		testingContext.Fatalf("unexpected mostOrganicSamples winner: %+v", organics.Entries[0])
	}

	furthest := boardByID["furthestFromSol"]
	if len(furthest.Entries) != 4 || furthest.Entries[0].WaypointNumber != 3 ||
		math.Abs(furthest.Entries[0].PrimaryValue-5000.0) > 0.001 ||
		furthest.Entries[0].FormattedPrimary != "5,000.0 Ly" ||
		furthest.Entries[0].SecondaryLabel != "Unknown Region" ||
		furthest.Entries[3].WaypointNumber != 1 {
		testingContext.Fatalf("unexpected furthestFromSol entries: %+v", furthest.Entries)
	}

	mistake := boardByID["costliestMistake"]
	if len(mistake.Entries) != 1 || mistake.Entries[0].FormattedPrimary != "-3,000,000 Cr" ||
		mistake.Entries[0].SecondaryLabel != "Carto -1,000,000 Cr · Exo -2,000,000 Cr" {
		testingContext.Fatalf("unexpected costliestMistake entries: %+v", mistake.Entries)
	}

	// Zero out death losses and verify empty leaderboards serialize [] rather than null.
	rowDetails[1].Realization = replaystats.RealizationBreakdown{}
	snapshot := exporter.ExpeditionExport{
		SchemaVersion:       exporter.SchemaVersion,
		Plan:                planView,
		RowDetails:          rowDetails,
		HighlightsFullOrder: exporter.ComputeHighlightLeaderboards(planView, rowDetails, pinnedNow),
	}
	canonicalBytes, marshalError := exporter.MarshalCanonicalJSON(snapshot)
	if marshalError != nil {
		testingContext.Fatalf("MarshalCanonicalJSON: %v", marshalError)
	}
	if !strings.Contains(string(canonicalBytes), `"entries": []`) {
		testingContext.Fatalf("expected empty leaderboard entries to marshal as [], got:\n%s", string(canonicalBytes))
	}
	var roundTrip exporter.ExpeditionExport
	if unmarshalError := json.Unmarshal(canonicalBytes, &roundTrip); unmarshalError != nil {
		testingContext.Fatalf("unmarshal canonical json: %v", unmarshalError)
	}
	if len(roundTrip.HighlightsFullOrder) != 11 {
		testingContext.Fatalf("expected 11 round-tripped leaderboards, got %d", len(roundTrip.HighlightsFullOrder))
	}

	// Populate differential realized payouts and pure carrier-passenger travel to verify ComputeHighlightVariants.
	rowDetails[0].Realization = replaystats.RealizationBreakdown{TotalRealized: 48_000_000}
	rowDetails[1].Realization = replaystats.RealizationBreakdown{TotalRealized: 15_000_000}
	rowDetails[2].Realization = replaystats.RealizationBreakdown{PendingValue: 105_000_000}
	rowDetails[1].Metrics.ShipDistanceLy = 0
	rowDetails[1].Metrics.CarrierPassengerLy = 3000.0

	variants := exporter.ComputeHighlightVariants(planView, rowDetails)
	expectedVariants := []string{
		"mostLucrativeRealized",
		"longestShipDistance",
		"shortestShipDistance",
		"longestInGameDuration",
		"highestEarningPaceRealized",
		"fastestTransitPaceShip",
	}
	if len(variants) != len(expectedVariants) {
		testingContext.Fatalf("expected %d variant boards, got %d", len(expectedVariants), len(variants))
	}
	for index, variantBoard := range variants {
		if variantBoard.CardID != expectedVariants[index] {
			testingContext.Fatalf("variant[%d] cardId=%q, want %q", index, variantBoard.CardID, expectedVariants[index])
		}
	}
	// Leg #4 has highest estimated (105M) with 0 realized; Leg #2 (48M realized) wins mostLucrativeRealized while keeping Carto/Exo secondary label.
	if len(variants[0].Entries) != 2 || variants[0].Title != "Most Lucrative Leg" ||
		variants[0].Entries[0].WaypointNumber != 2 ||
		variants[0].Entries[0].FormattedPrimary != "48,000,000 Cr" ||
		variants[0].Entries[0].SecondaryLabel != "Carto 12,000,000 Cr · Exo 40,000,000 Cr" {
		testingContext.Fatalf("unexpected mostLucrativeRealized entries: %+v", variants[0].Entries)
	}
	// Leg #3 is 3,000 Ly pure carrier passenger (0 ship Ly, excluded); Leg #2 (1,000 Ly ship) wins longestShipDistance and Leg #4 (600 Ly) wins shortestShipDistance.
	if len(variants[1].Entries) != 2 || variants[1].Title != "Longest Leg (Distance)" ||
		variants[1].Entries[0].WaypointNumber != 2 || variants[1].Entries[0].FormattedPrimary != "1,000.0 Ly" {
		testingContext.Fatalf("unexpected longestShipDistance entries: %+v", variants[1].Entries)
	}
	if len(variants[2].Entries) != 2 || variants[2].Title != "Shortest Leg (Distance)" ||
		variants[2].Entries[0].WaypointNumber != 4 || variants[2].Entries[0].FormattedPrimary != "600.0 Ly" {
		testingContext.Fatalf("unexpected shortestShipDistance entries: %+v", variants[2].Entries)
	}
}

func TestComputeHighlightInGameDurationAndPaceBoards(testingContext *testing.T) {
	pinnedNow := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	planView := expedition.PlanView{
		Rows: []expedition.RowView{
			{Row: expedition.Row{ID: 1, RowType: expedition.RowWaypoint, SystemName: "Sol", VisitedAt: "2026-04-01T08:00:00Z"}, Number: 1},
			{Row: expedition.Row{ID: 2, RowType: expedition.RowWaypoint, SystemName: "Alpha Rim", VisitedAt: "2026-04-02T10:00:00Z"}, Number: 2},
			{Row: expedition.Row{ID: 3, RowType: expedition.RowWaypoint, SystemName: "Beta Crossing", VisitedAt: "2026-04-03T10:00:00Z"}, Number: 3},
			{Row: expedition.Row{ID: 4, RowType: expedition.RowWaypoint, SystemName: "Gamma Sprint", VisitedAt: "2026-04-04T10:00:00Z"}, Number: 4},
		},
	}
	// Leg #2: 2h in-game (7200s), 1.5h transit (5400s), 1,500 Ly total (1,200 Ly ship), 60M est / 30M realized.
	// Leg #3: 1h in-game (3600s, empty label fallback), 1h transit (3600s), 2,000 Ly pure carrier (0 ship Ly), 40M est / 40M realized.
	// Leg #4: 3599s in-game and 3599s transit (< 1h guard), huge distance and value must be excluded from pace boards but still appear in longestInGameDuration.
	rowDetails := []expedition.WaypointDetails{
		{
			RowID: 2, Kind: "leg", Number: 2, PrevSystem: "Sol",
			InGameSeconds: int64Ptr(7200), InGameDuration: "2h 00m",
			TransitInGameSeconds: int64Ptr(5400), TransitInGameDuration: "1h 30m",
			Metrics:     replaystats.Metrics{DistanceLy: 1500, ShipDistanceLy: 1200, Jumps: 24, ScanValueTotal: 20_000_000, OrganicValueTotal: 40_000_000},
			Realization: replaystats.RealizationBreakdown{TotalRealized: 30_000_000},
		},
		{
			RowID: 3, Kind: "leg", Number: 3, PrevSystem: "Alpha Rim",
			InGameSeconds: int64Ptr(3600), InGameDuration: "",
			TransitInGameSeconds: int64Ptr(3600), TransitInGameDuration: "",
			Metrics:     replaystats.Metrics{DistanceLy: 2000, ShipDistanceLy: 0, CarrierPassengerLy: 2000, Jumps: 0, ScanValueTotal: 40_000_000},
			Realization: replaystats.RealizationBreakdown{TotalRealized: 40_000_000},
		},
		{
			RowID: 4, Kind: "leg", Number: 4, PrevSystem: "Beta Crossing",
			InGameSeconds: int64Ptr(3599), InGameDuration: "59m",
			TransitInGameSeconds: int64Ptr(3599), TransitInGameDuration: "59m",
			Metrics:     replaystats.Metrics{DistanceLy: 5000, ShipDistanceLy: 5000, Jumps: 80, ScanValueTotal: 200_000_000},
			Realization: replaystats.RealizationBreakdown{TotalRealized: 200_000_000},
		},
	}

	boards := exporter.ComputeHighlightLeaderboards(planView, rowDetails, pinnedNow)
	boardByID := make(map[string]exporter.HighlightLeaderboard, len(boards))
	for _, board := range boards {
		boardByID[board.CardID] = board
	}
	variants := exporter.ComputeHighlightVariants(planView, rowDetails)
	variantByID := make(map[string]exporter.HighlightLeaderboard, len(variants))
	for _, variantBoard := range variants {
		variantByID[variantBoard.CardID] = variantBoard
	}

	longestInGame := variantByID["longestInGameDuration"]
	if len(longestInGame.Entries) != 3 || longestInGame.Accent != "cyan" ||
		longestInGame.Entries[0].WaypointNumber != 2 || longestInGame.Entries[0].FormattedPrimary != "2h 00m" ||
		longestInGame.Entries[0].SecondaryLabel != "1,500.0 Ly · 24 jumps" ||
		longestInGame.Entries[1].FormattedPrimary != "1h 00m" {
		testingContext.Fatalf("unexpected longestInGameDuration entries: %+v", longestInGame.Entries)
	}

	earningEst := boardByID["highestEarningPace"]
	if len(earningEst.Entries) != 2 || earningEst.Accent != "amber" ||
		earningEst.Entries[0].WaypointNumber != 3 || earningEst.Entries[0].FormattedPrimary != "40,000,000 Cr/h" ||
		earningEst.Entries[0].SecondaryLabel != "40,000,000 Cr · 1h 00m" ||
		earningEst.Entries[1].WaypointNumber != 2 || earningEst.Entries[1].FormattedPrimary != "30,000,000 Cr/h" {
		testingContext.Fatalf("unexpected highestEarningPace entries: %+v", earningEst.Entries)
	}

	earningRealized := variantByID["highestEarningPaceRealized"]
	if len(earningRealized.Entries) != 2 ||
		earningRealized.Entries[0].WaypointNumber != 3 || earningRealized.Entries[0].FormattedPrimary != "40,000,000 Cr/h" ||
		earningRealized.Entries[1].FormattedPrimary != "15,000,000 Cr/h" ||
		earningRealized.Entries[1].SecondaryLabel != "30,000,000 Cr · 2h 00m" {
		testingContext.Fatalf("unexpected highestEarningPaceRealized entries: %+v", earningRealized.Entries)
	}

	transitTotal := boardByID["fastestTransitPace"]
	if len(transitTotal.Entries) != 2 ||
		transitTotal.Entries[0].WaypointNumber != 3 || transitTotal.Entries[0].FormattedPrimary != "2,000.0 Ly/h" ||
		transitTotal.Entries[0].SecondaryLabel != "2,000.0 Ly · 1h 00m" ||
		transitTotal.Entries[1].FormattedPrimary != "1,000.0 Ly/h" {
		testingContext.Fatalf("unexpected fastestTransitPace entries: %+v", transitTotal.Entries)
	}

	transitShip := variantByID["fastestTransitPaceShip"]
	if len(transitShip.Entries) != 1 ||
		transitShip.Entries[0].WaypointNumber != 2 || transitShip.Entries[0].FormattedPrimary != "800.0 Ly/h" ||
		transitShip.Entries[0].SecondaryLabel != "1,200.0 Ly · 1h 30m" {
		testingContext.Fatalf("unexpected fastestTransitPaceShip entries: %+v", transitShip.Entries)
	}
}
