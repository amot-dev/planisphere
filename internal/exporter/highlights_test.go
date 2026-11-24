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
	if len(roundTrip.HighlightsFullOrder) != 9 {
		testingContext.Fatalf("expected 9 round-tripped leaderboards, got %d", len(roundTrip.HighlightsFullOrder))
	}
}
