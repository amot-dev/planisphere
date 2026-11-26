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

func assertOptionalFloat(testingContext *testing.T, fieldName string, got *float64, want float64) {
	testingContext.Helper()
	if got == nil {
		testingContext.Fatalf("%s = nil, want %.6f", fieldName, want)
	}
	if math.Abs(*got-want) > 1e-6 {
		testingContext.Fatalf("%s = %.6f, want %.6f", fieldName, *got, want)
	}
}

func TestComputeElapsedDaysGuardsAndOpenWindow(testingContext *testing.T) {
	pinnedNow := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

	if exporter.ComputeElapsedDays("", "2026-04-05T12:00:00Z", pinnedNow) != nil {
		testingContext.Fatalf("expected nil for empty fromISO")
	}
	if exporter.ComputeElapsedDays("not-an-iso", "2026-04-05T12:00:00Z", pinnedNow) != nil {
		testingContext.Fatalf("expected nil for invalid fromISO")
	}
	if exporter.ComputeElapsedDays("2026-04-01T10:00:00Z", "bad-end", pinnedNow) != nil {
		testingContext.Fatalf("expected nil for invalid toISO")
	}
	if exporter.ComputeElapsedDays("2026-04-05T12:00:00Z", "2026-04-05T12:00:00Z", pinnedNow) != nil {
		testingContext.Fatalf("expected nil when end equals start")
	}
	if exporter.ComputeElapsedDays("2026-04-05T13:00:00Z", "2026-04-05T12:00:00Z", pinnedNow) != nil {
		testingContext.Fatalf("expected nil when end precedes start")
	}

	assertOptionalFloat(
		testingContext,
		"closedWindowDays",
		exporter.ComputeElapsedDays("2026-04-01T10:00:00Z", "2026-04-02T22:00:00Z", pinnedNow),
		1.5,
	)
	assertOptionalFloat(
		testingContext,
		"openWindowDays",
		exporter.ComputeElapsedDays("2026-04-04T12:00:00Z", "", pinnedNow),
		1.0,
	)
}

func TestComputeDerivedMetricsGuardsAndFormulas(testingContext *testing.T) {
	subDay := 0.75
	zeroDerived := exporter.ComputeDerivedMetrics(
		replaystats.Metrics{},
		replaystats.RealizationBreakdown{TotalRealized: 5_000_000, PendingValue: 5_000_000},
		nil,
		&subDay,
	)
	if zeroDerived.AvgJumpRangeLy != nil ||
		zeroDerived.BoostRatePercent != nil ||
		zeroDerived.AvgDistancePerDayLy != nil ||
		zeroDerived.AvgCarrierJumpRangeLy != nil ||
		zeroDerived.AvgTritiumPerJump != nil ||
		zeroDerived.DSSEfficiencyPercent != nil ||
		zeroDerived.FirstDiscoveryRatePercent != nil ||
		zeroDerived.FirstLoggedRatePercent != nil ||
		zeroDerived.AvgCreditsPerSample != nil ||
		zeroDerived.AvgCombinedCreditsPerJump != nil ||
		zeroDerived.AvgCombinedCreditsPer1000Ly != nil ||
		zeroDerived.AvgCombinedCreditsPerDay != nil ||
		zeroDerived.RealizationRetentionPercent != nil ||
		zeroDerived.NetProfitAfterCarrierCost != nil ||
		zeroDerived.AvgRealizedCreditsPerJump != nil ||
		zeroDerived.AvgRealizedCreditsPer1000Ly != nil {
		testingContext.Fatalf("expected nil pointer fields on zero-denominator/sub-24h metrics, got %+v", zeroDerived)
	}

	encodedZero, marshalError := json.Marshal(zeroDerived)
	if marshalError != nil {
		testingContext.Fatalf("marshal zero DerivedMetrics: %v", marshalError)
	}
	if !strings.Contains(string(encodedZero), `"avgJumpRangeLy":null`) ||
		!strings.Contains(string(encodedZero), `"netProfitAfterCarrierCost":null`) {
		testingContext.Fatalf("expected pointer fields to marshal explicit null without omitempty, got %s", string(encodedZero))
	}

	tritiumTonnes := 400
	elapsedDays := 4.0
	metrics := replaystats.Metrics{
		DistanceLy:         4800,
		ShipDistanceLy:     0,
		CarrierPassengerLy: 200,
		Jumps:              80,
		CarrierJumps:       4,
		CarrierDistanceLy:  1800,
		TritiumUsed:        &tritiumTonnes,
		JetConeBoosts:      10,
		PremiumBoosts:      2,
		StandardBoosts:     3,
		BasicBoosts:        1,
		BodiesScanned:      100,
		BodiesMapped:       40,
		EfficientMappings:  30,
		ScanValueTotal:     42_000_000,
		FirstDiscoveries:   80,
		OrganicSamples:     10,
		OrganicValueTotal:  130_000_000,
		FirstLogged:        8,
	}
	realization := replaystats.RealizationBreakdown{
		TotalRealized:     140_000_000,
		CartoLostDeath:    1_000_000,
		OrganicLostDeath:  2_000_000,
		CartoStolen:       300_000,
		OrganicStolen:     700_000,
		CarrierTariffLoss: 2_000_000,
		NpcCrewLoss:       500_000,
		PendingValue:      12_000_000,
	}
	carrierCost := expedition.CarrierCost{TotalCost: 160_000_000}

	derived := exporter.ComputeDerivedMetrics(metrics, realization, &carrierCost, &elapsedDays)
	if math.Abs(derived.ShipDistanceLy-4600.0) > 1e-6 || derived.TotalBoosts != 16 {
		testingContext.Fatalf("unexpected shipDistanceLy=%.2f totalBoosts=%d", derived.ShipDistanceLy, derived.TotalBoosts)
	}
	if derived.CombinedEstimatedCredits != 172_000_000 || derived.TotalLossesCredits != 6_500_000 {
		testingContext.Fatalf("unexpected combined=%d losses=%d", derived.CombinedEstimatedCredits, derived.TotalLossesCredits)
	}
	assertOptionalFloat(testingContext, "AvgJumpRangeLy", derived.AvgJumpRangeLy, 4600.0/80.0)
	assertOptionalFloat(testingContext, "BoostRatePercent", derived.BoostRatePercent, 20.0)
	assertOptionalFloat(testingContext, "AvgDistancePerDayLy", derived.AvgDistancePerDayLy, 1200.0)
	assertOptionalFloat(testingContext, "AvgCarrierJumpRangeLy", derived.AvgCarrierJumpRangeLy, 450.0)
	assertOptionalFloat(testingContext, "AvgTritiumPerJump", derived.AvgTritiumPerJump, 100.0)
	assertOptionalFloat(testingContext, "DSSEfficiencyPercent", derived.DSSEfficiencyPercent, 75.0)
	assertOptionalFloat(testingContext, "FirstDiscoveryRatePercent", derived.FirstDiscoveryRatePercent, 80.0)
	assertOptionalFloat(testingContext, "FirstLoggedRatePercent", derived.FirstLoggedRatePercent, 80.0)
	assertOptionalFloat(testingContext, "AvgCreditsPerSample", derived.AvgCreditsPerSample, 13_000_000.0)
	assertOptionalFloat(testingContext, "AvgCombinedCreditsPerDay", derived.AvgCombinedCreditsPerDay, 43_000_000.0)
	assertOptionalFloat(testingContext, "AvgCartoCreditsPer1000Ly", derived.AvgCartoCreditsPer1000Ly, 8_750_000.0)
	assertOptionalFloat(testingContext, "AvgExoCreditsPerJump", derived.AvgExoCreditsPerJump, 1_625_000.0)
	assertOptionalFloat(testingContext, "RealizationRetentionPercent", derived.RealizationRetentionPercent, 87.5)
	assertOptionalFloat(testingContext, "AvgRealizedCreditsPer1000Ly", derived.AvgRealizedCreditsPer1000Ly, 140_000_000.0/4.8)
	if derived.NetProfitAfterCarrierCost == nil || *derived.NetProfitAfterCarrierCost != -20_000_000 {
		testingContext.Fatalf("expected negative netProfitAfterCarrierCost -20,000,000, got %v", derived.NetProfitAfterCarrierCost)
	}
}

func TestBuildCareerDiffBaselineAndNewGrowth(testingContext *testing.T) {
	if exporter.BuildCareerDiff(nil, replaystats.Metrics{Jumps: 5}, replaystats.RealizationBreakdown{}) != nil {
		testingContext.Fatalf("expected nil CareerDiff when originBaseline is nil")
	}

	baseline := &expedition.WaypointDetails{
		Metrics: replaystats.Metrics{
			DistanceLy:     10_000,
			Jumps:          200,
			ScanValueTotal: 50_000_000,
			BodiesScanned:  100,
			BodiesMapped:   0,
			OrganicSamples: 0,
		},
		Realization: replaystats.RealizationBreakdown{TotalRealized: 40_000_000},
	}
	live := replaystats.Metrics{
		DistanceLy:        2500,
		Jumps:             50,
		ScanValueTotal:    15_000_000,
		OrganicValueTotal: 10_000_000,
		BodiesScanned:     25,
		BodiesMapped:      8,
		OrganicSamples:    0,
	}
	realization := replaystats.RealizationBreakdown{TotalRealized: 20_000_000}

	diff := exporter.BuildCareerDiff(baseline, live, realization)
	if len(diff) != 7 {
		testingContext.Fatalf("expected 7 CareerDiff items, got %d", len(diff))
	}
	assertOptionalFloat(testingContext, "distanceLy.growth", diff[0].GrowthPercent, 25.0)
	assertOptionalFloat(testingContext, "estimatedValue.growth", diff[2].GrowthPercent, 50.0)
	assertOptionalFloat(testingContext, "realizedValue.growth", diff[3].GrowthPercent, 50.0)
	if diff[5].ID != "bodiesMapped" || diff[5].GrowthPercent != nil || !diff[5].IsNewBaseline {
		testingContext.Fatalf("expected bodiesMapped to report IsNewBaseline=true with nil growth, got %+v", diff[5])
	}
	if diff[6].ID != "organicSamples" || diff[6].GrowthPercent != nil || diff[6].IsNewBaseline {
		testingContext.Fatalf("expected zero-baseline zero-delta organicSamples to report IsNewBaseline=false, got %+v", diff[6])
	}
}
