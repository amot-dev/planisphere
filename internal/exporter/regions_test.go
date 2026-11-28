package exporter_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"planisphere/internal/exporter"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

func float64Ptr(value float64) *float64 { return &value }
func int64Ptr(value int64) *int64       { return &value }

func TestBuildLegAndRegionTelemetryAttributionAndModeBuckets(testingContext *testing.T) {
	pinnedNow := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)

	planView := expedition.PlanView{
		Rows: []expedition.RowView{
			{
				Row:            expedition.Row{ID: 1, RowType: expedition.RowWaypoint, SystemName: "Sol", VisitedAt: "2026-04-01T08:00:00Z"},
				Number:         1,
				DetectedRegion: "Inner Orion Spur",
			},
			{
				Row:            expedition.Row{ID: 2, RowType: expedition.RowWaypoint, SystemName: "Chartless One", VisitedAt: "2026-04-01T14:00:00Z"},
				Number:         2,
				DistFromPrevLy: float64Ptr(300),
				LegModeID:      2,
			},
			{
				Row:        expedition.Row{ID: 100, RowType: expedition.RowRegion},
				RegionName: "Temple",
			},
			{
				Row:            expedition.Row{ID: 3, RowType: expedition.RowWaypoint, SystemName: "Manual Sector", VisitedAt: "2026-04-03T14:00:00Z"},
				Number:         3,
				DistFromPrevLy: float64Ptr(1200),
				LegModeID:      1,
				LegModeName:    "Exploration",
				LegModeColor:   "#00D2FF",
			},
			{
				Row:            expedition.Row{ID: 4, RowType: expedition.RowWaypoint, SystemName: "Detected Override", VisitedAt: "2026-04-04T14:00:00Z"},
				Number:         4,
				DetectedRegion: "Odin's Hold",
				DistFromPrevLy: float64Ptr(800),
				LegModeID:      1,
				LegModeName:    "Exploration",
				LegModeColor:   "#00D2FF",
			},
			{
				Row:            expedition.Row{ID: 5, RowType: expedition.RowWaypoint, SystemName: "Temple Return", VisitedAt: "2026-04-05T14:00:00Z"},
				Number:         5,
				DistFromPrevLy: float64Ptr(500),
				LegModeID:      2,
				LegModeName:    "Fast Transit",
				LegModeColor:   "#FF7100",
			},
			{
				Row:    expedition.Row{ID: 6, RowType: expedition.RowWaypoint, SystemName: "Future Stop"},
				Number: 6,
			},
		},
	}

	rowDetails := []expedition.WaypointDetails{
		{
			RowID:                  2,
			Kind:                   "leg",
			Number:                 2,
			From:                   "2026-04-01T08:00:00Z",
			To:                     "2026-04-01T14:00:00Z",
			FromGalactic:           "3312-04-01 08:00 UTC",
			ToGalactic:             "3312-04-01 14:00 UTC",
			InGameSeconds:          int64Ptr(3600),
			InGameDuration:         "1h 00m",
			TransitInGameSeconds:   int64Ptr(2700),
			TransitInGameDuration:  "45m",
			InSystemInGameSeconds:  int64Ptr(900),
			InSystemInGameDuration: "15m",
			ShipsUsed:              []string{"  Mandalay ", "", "Mandalay"},
			Metrics:                replaystats.Metrics{DistanceLy: 300, ShipDistanceLy: 300, Jumps: 6, ScanValueTotal: 3_000_000},
			Realization:            replaystats.RealizationBreakdown{TotalRealized: 3_000_000},
		},
		{
			RowID:                  3,
			Kind:                   "leg",
			Number:                 3,
			From:                   "2026-04-01T14:00:00Z",
			To:                     "2026-04-03T14:00:00Z",
			FromGalactic:           "3312-04-01 14:00 UTC",
			ToGalactic:             "3312-04-03 14:00 UTC",
			InGameSeconds:          int64Ptr(7200),
			InGameDuration:         "2h 00m",
			TransitInGameSeconds:   int64Ptr(5400),
			TransitInGameDuration:  "1h 30m",
			InSystemInGameSeconds:  int64Ptr(1800),
			InSystemInGameDuration: "30m",
			ShipsUsed:              []string{"Mandalay", "Anaconda"},
			Metrics:                replaystats.Metrics{DistanceLy: 1200, ShipDistanceLy: 1200, Jumps: 20, ScanValueTotal: 20_000_000},
			Realization:            replaystats.RealizationBreakdown{TotalRealized: 18_000_000, CarrierTariffLoss: 2_000_000},
		},
		{
			RowID:        4,
			Kind:         "leg",
			Number:       4,
			From:         "2026-04-03T14:00:00Z",
			To:           "2026-04-04T14:00:00Z",
			FromGalactic: "3312-04-03 14:00 UTC",
			ToGalactic:   "3312-04-04 14:00 UTC",
			ShipsUsed:    []string{"Anaconda"},
			Metrics:      replaystats.Metrics{DistanceLy: 800, ShipDistanceLy: 800, Jumps: 16, OrganicSamples: 2, OrganicValueTotal: 30_000_000},
			Realization:  replaystats.RealizationBreakdown{PendingValue: 30_000_000},
		},
		{
			RowID:                  5,
			Kind:                   "leg",
			Number:                 5,
			From:                   "2026-04-04T14:00:00Z",
			To:                     "",
			FromGalactic:           "3312-04-04 14:00 UTC",
			InSystem:               true,
			InGameSeconds:          int64Ptr(2400),
			InGameDuration:         "40m",
			TransitInGameSeconds:   int64Ptr(1800),
			TransitInGameDuration:  "30m",
			InSystemInGameSeconds:  int64Ptr(600),
			InSystemInGameDuration: "10m",
			ShipsUsed:              []string{"Cobra Mk V"},
			Metrics:                replaystats.Metrics{DistanceLy: 500, ShipDistanceLy: 500, Jumps: 10, ScanValueTotal: 10_000_000},
			Realization:            replaystats.RealizationBreakdown{TotalRealized: 10_000_000},
		},
		{
			RowID:   6,
			Kind:    "leg",
			Number:  6,
			Metrics: replaystats.Metrics{DistanceLy: 999, Jumps: 99},
		},
	}

	legs, regions := exporter.BuildLegAndRegionTelemetry(planView, rowDetails, pinnedNow)
	if len(legs) != 4 {
		testingContext.Fatalf("expected 4 visited legs excluding origin and unvisited WP #6, got %d", len(legs))
	}
	if legs[0].RegionName != "Unknown Region" || legs[1].RegionName != "Temple" || legs[2].RegionName != "Odin's Hold" || legs[3].RegionName != "Temple" {
		testingContext.Fatalf("unexpected leg region attributions: %+v", legs)
	}
	if !reflect.DeepEqual(legs[0].ShipsFlown, []string{"Mandalay"}) || legs[0].Derived.AvgDistancePerDayLy != nil {
		testingContext.Fatalf("expected trimmed unique ships and nil sub-24h daily rate on leg 0, got %+v", legs[0])
	}

	if len(regions) != 3 || regions[0].RegionName != "Unknown Region" || regions[1].RegionName != "Temple" || regions[2].RegionName != "Odin's Hold" {
		testingContext.Fatalf("expected regions in arrival order [Unknown Region, Temple, Odin's Hold], got %+v", regions)
	}
	if len(regions[0].ModeBuckets) != 1 || regions[0].ModeBuckets[0].ModeName != "Travel" || regions[0].ModeBuckets[0].ModeColor != "#FF7100" {
		testingContext.Fatalf("expected default Travel/#FF7100 fallback on empty mode metadata, got %+v", regions[0].ModeBuckets)
	}

	temple := regions[1]
	if !reflect.DeepEqual(temple.LegRowIDs, []int64{3, 5}) || temple.LegCount != 2 {
		testingContext.Fatalf("unexpected Temple leg grouping: %+v", temple)
	}
	if temple.FromGalactic != "3312-04-01 14:00 UTC" || temple.ToGalactic != "IN SYSTEM" || !temple.InSystem {
		testingContext.Fatalf("expected Temple ToGalactic IN SYSTEM override, got from=%q to=%q inSystem=%v", temple.FromGalactic, temple.ToGalactic, temple.InSystem)
	}
	if math.Abs(temple.CoveredLy-1700.0) > 1e-6 || temple.Metrics.Jumps != 30 || temple.Realization.TotalRealized != 28_000_000 {
		testingContext.Fatalf("unexpected Temple aggregated totals: %+v", temple)
	}
	if !reflect.DeepEqual(temple.ShipsFlown, []string{"Mandalay", "Anaconda", "Cobra Mk V"}) {
		testingContext.Fatalf("unexpected Temple shipsFlown: %v", temple.ShipsFlown)
	}
	// Leg 3 (2d) + Leg 5 (open until 2026-04-06T12:00:00Z = 1d 22h) = 3d 22h 00m.
	if temple.Duration != "3d 22h 00m" || temple.Derived.NetProfitAfterCarrierCost != nil || temple.Derived.AvgDistancePerDayLy == nil {
		testingContext.Fatalf("unexpected Temple timing/derived: duration=%q derived=%+v", temple.Duration, temple.Derived)
	}
	if len(temple.ModeBuckets) != 2 || temple.ModeBuckets[0].ModeID != 1 || temple.ModeBuckets[1].ModeID != 2 {
		testingContext.Fatalf("expected 2 ordered Temple mode buckets [1, 2], got %+v", temple.ModeBuckets)
	}
	if legs[0].InGameDuration != "1h 00m" || legs[0].TransitInGameDuration != "45m" || legs[0].InSystemInGameDuration != "15m" || legs[2].InGameSeconds != nil {
		testingContext.Fatalf("unexpected per-leg playtime copy: leg0=%+v leg2=%+v", legs[0], legs[2])
	}
	if temple.InGameSeconds == nil || *temple.InGameSeconds != 9600 || temple.InGameDuration != "2h 40m" ||
		temple.TransitInGameDuration != "2h 00m" || temple.InSystemInGameDuration != "40m" ||
		temple.ModeBuckets[0].InGameDuration != "2h 00m" || temple.ModeBuckets[1].InGameDuration != "40m" {
		testingContext.Fatalf("unexpected Temple region/mode playtime aggregation: %+v", temple)
	}
	if regions[2].InGameSeconds != nil || regions[2].InGameDuration != "" {
		testingContext.Fatalf("expected nil playtime for region without session data, got %+v", regions[2])
	}

	allShips := exporter.CollectShipsForVisitedLegs(planView, rowDetails, nil)
	if !reflect.DeepEqual(allShips, []string{"Mandalay", "Anaconda", "Cobra Mk V"}) {
		testingContext.Fatalf("unexpected all visited ships: %v", allShips)
	}
	modeTwo := int64(2)
	modeTwoShips := exporter.CollectShipsForVisitedLegs(planView, rowDetails, &modeTwo)
	if !reflect.DeepEqual(modeTwoShips, []string{"Mandalay", "Cobra Mk V"}) {
		testingContext.Fatalf("unexpected mode 2 ships: %v", modeTwoShips)
	}
}
