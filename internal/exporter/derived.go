package exporter

import (
	"math"
	"time"

	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

const (
	millisecondsPerDay   = 86_400_000.0
	minInGameRateSeconds = int64(3600)
	secondsPerHour       = 3600.0
)

// ComputeElapsedDays returns fractional elapsed days between fromISO and toISO
// (using currentTime when toISO is empty), or nil when timestamps are missing,
// invalid, or non-positive.
func ComputeElapsedDays(fromISO, toISO string, currentTime time.Time) *float64 {
	elapsedMillis, ok := computeElapsedMillis(fromISO, toISO, currentTime)
	if !ok {
		return nil
	}
	return floatPtr(float64(elapsedMillis) / millisecondsPerDay)
}

func computeElapsedMillis(fromISO, toISO string, currentTime time.Time) (int64, bool) {
	if fromISO == "" {
		return 0, false
	}
	startTime, parseStartError := reference.ParseISO(fromISO)
	if parseStartError != nil {
		return 0, false
	}
	endTime := currentTime
	if toISO != "" {
		parsedEnd, parseEndError := reference.ParseISO(toISO)
		if parseEndError != nil {
			return 0, false
		}
		endTime = parsedEnd
	}
	elapsedMillis := endTime.UnixMilli() - startTime.UnixMilli()
	return elapsedMillis, elapsedMillis > 0
}

// ComputeDerivedMetrics computes derived averages, percentages, and net profits
// with zero-division, sub-24-hour calendar, and sub-1-hour in-game rate guards.
func ComputeDerivedMetrics(
	metrics replaystats.Metrics,
	realization replaystats.RealizationBreakdown,
	carrierCost *expedition.CarrierCost,
	elapsedDays *float64,
	inGameSeconds *int64,
) DerivedMetrics {
	shipDistanceLy := computeShipDistanceLy(metrics)
	totalBoosts := metrics.JetConeBoosts + metrics.PremiumBoosts + metrics.StandardBoosts + metrics.BasicBoosts
	combinedEstimatedCredits := metrics.ScanValueTotal + metrics.OrganicValueTotal
	totalLossesCredits := realization.CartoLostDeath + realization.OrganicLostDeath +
		realization.CartoStolen + realization.OrganicStolen +
		realization.CarrierTariffLoss + realization.NpcCrewLoss

	var avgDistancePerDayLy, avgCombinedCreditsPerDay *float64
	if elapsedDays != nil && *elapsedDays >= 1.0 {
		avgDistancePerDayLy = floatPtr(metrics.DistanceLy / *elapsedDays)
		avgCombinedCreditsPerDay = floatPtr(float64(combinedEstimatedCredits) / *elapsedDays)
	}
	var (
		avgDistancePerHourLy      *float64
		avgJumpsPerHour           *float64
		avgCombinedCreditsPerHour *float64
		avgCartoCreditsPerHour    *float64
		avgBodiesScannedPerHour   *float64
		avgExoCreditsPerHour      *float64
		avgOrganicSamplesPerHour  *float64
		avgRealizedCreditsPerHour *float64
	)
	if inGameSeconds != nil && *inGameSeconds >= minInGameRateSeconds {
		inGameHours := float64(*inGameSeconds) / secondsPerHour
		avgDistancePerHourLy = floatPtr(metrics.DistanceLy / inGameHours)
		avgJumpsPerHour = floatPtr(float64(metrics.Jumps) / inGameHours)
		avgCombinedCreditsPerHour = floatPtr(float64(combinedEstimatedCredits) / inGameHours)
		avgCartoCreditsPerHour = floatPtr(float64(metrics.ScanValueTotal) / inGameHours)
		avgBodiesScannedPerHour = floatPtr(float64(metrics.BodiesScanned) / inGameHours)
		avgExoCreditsPerHour = floatPtr(float64(metrics.OrganicValueTotal) / inGameHours)
		avgOrganicSamplesPerHour = floatPtr(float64(metrics.OrganicSamples) / inGameHours)
		avgRealizedCreditsPerHour = floatPtr(float64(realization.TotalRealized) / inGameHours)
	}
	var avgTritiumPerJump *float64
	if metrics.TritiumUsed != nil && metrics.CarrierJumps > 0 {
		avgTritiumPerJump = safeDivide(float64(*metrics.TritiumUsed), float64(metrics.CarrierJumps))
	}
	var retentionPercent *float64
	if settledEstimate := combinedEstimatedCredits - realization.PendingValue; settledEstimate > 0 {
		retentionPercent = floatPtr((float64(realization.TotalRealized) / float64(settledEstimate)) * 100.0)
	}
	var netProfitAfterCarrierCost *int64
	if carrierCost != nil {
		netProfit := realization.TotalRealized - carrierCost.TotalCost
		netProfitAfterCarrierCost = &netProfit
	}

	return DerivedMetrics{
		ShipDistanceLy:              shipDistanceLy,
		TotalBoosts:                 totalBoosts,
		AvgJumpRangeLy:              safeDivide(shipDistanceLy, float64(metrics.Jumps)),
		BoostRatePercent:            safePercent(totalBoosts, metrics.Jumps),
		AvgDistancePerDayLy:         avgDistancePerDayLy,
		AvgDistancePerHourLy:        avgDistancePerHourLy,
		AvgJumpsPerHour:             avgJumpsPerHour,
		AvgCarrierJumpRangeLy:       safeDivide(metrics.CarrierDistanceLy, float64(metrics.CarrierJumps)),
		AvgTritiumPerJump:           avgTritiumPerJump,
		CombinedEstimatedCredits:    combinedEstimatedCredits,
		DSSEfficiencyPercent:        safePercent(metrics.EfficientMappings, metrics.BodiesMapped),
		FirstDiscoveryRatePercent:   safePercent(metrics.FirstDiscoveries, metrics.BodiesScanned),
		FirstLoggedRatePercent:      safePercent(metrics.FirstLogged, metrics.OrganicSamples),
		AvgCreditsPerSample:         safeDivide(float64(metrics.OrganicValueTotal), float64(metrics.OrganicSamples)),
		AvgCombinedCreditsPerJump:   safeDivide(float64(combinedEstimatedCredits), float64(metrics.Jumps)),
		AvgCombinedCreditsPer1000Ly: safePerThousand(float64(combinedEstimatedCredits), metrics.DistanceLy),
		AvgCombinedCreditsPerDay:    avgCombinedCreditsPerDay,
		AvgCombinedCreditsPerHour:   avgCombinedCreditsPerHour,
		AvgCartoCreditsPerJump:      safeDivide(float64(metrics.ScanValueTotal), float64(metrics.Jumps)),
		AvgCartoCreditsPer1000Ly:    safePerThousand(float64(metrics.ScanValueTotal), metrics.DistanceLy),
		AvgCartoCreditsPerHour:      avgCartoCreditsPerHour,
		AvgBodiesScannedPerHour:     avgBodiesScannedPerHour,
		AvgExoCreditsPerJump:        safeDivide(float64(metrics.OrganicValueTotal), float64(metrics.Jumps)),
		AvgExoCreditsPer1000Ly:      safePerThousand(float64(metrics.OrganicValueTotal), metrics.DistanceLy),
		AvgExoCreditsPerHour:        avgExoCreditsPerHour,
		AvgOrganicSamplesPerHour:    avgOrganicSamplesPerHour,
		TotalLossesCredits:          totalLossesCredits,
		RealizationRetentionPercent: retentionPercent,
		NetProfitAfterCarrierCost:   netProfitAfterCarrierCost,
		AvgRealizedCreditsPerJump:   safeDivide(float64(realization.TotalRealized), float64(metrics.Jumps)),
		AvgRealizedCreditsPer1000Ly: safePerThousand(float64(realization.TotalRealized), metrics.DistanceLy),
		AvgRealizedCreditsPerHour:   avgRealizedCreditsPerHour,
	}
}

// BuildCareerDiff builds the seven pre-expedition baseline vs expedition growth items,
// returning nil when originBaseline is nil.
func BuildCareerDiff(
	originBaseline *expedition.WaypointDetails,
	liveMetrics replaystats.Metrics,
	expeditionRealization replaystats.RealizationBreakdown,
) []CareerDiffItem {
	if originBaseline == nil {
		return nil
	}
	base := originBaseline.Metrics
	return []CareerDiffItem{
		makeCareerDiffItem("distanceLy", base.DistanceLy, liveMetrics.DistanceLy),
		makeCareerDiffItem("jumps", float64(base.Jumps), float64(liveMetrics.Jumps)),
		makeCareerDiffItem("estimatedValue", float64(base.ScanValueTotal+base.OrganicValueTotal), float64(liveMetrics.ScanValueTotal+liveMetrics.OrganicValueTotal)),
		makeCareerDiffItem("realizedValue", float64(originBaseline.Realization.TotalRealized), float64(expeditionRealization.TotalRealized)),
		makeCareerDiffItem("bodiesScanned", float64(base.BodiesScanned), float64(liveMetrics.BodiesScanned)),
		makeCareerDiffItem("bodiesMapped", float64(base.BodiesMapped), float64(liveMetrics.BodiesMapped)),
		makeCareerDiffItem("organicSamples", float64(base.OrganicSamples), float64(liveMetrics.OrganicSamples)),
	}
}

func makeCareerDiffItem(identifier string, baselineValue, delta float64) CareerDiffItem {
	var growthPercent *float64
	if baselineValue > 0 {
		growthPercent = floatPtr((delta / baselineValue) * 100.0)
	}
	return CareerDiffItem{ID: identifier, Baseline: baselineValue, GrowthPercent: growthPercent, IsNewBaseline: baselineValue <= 0 && delta > 0}
}

func safeDivide(numerator, denominator float64) *float64 {
	if !math.IsInf(denominator, 0) && !math.IsNaN(denominator) && denominator > 0 {
		return floatPtr(numerator / denominator)
	}
	return nil
}

func safePercent(part, whole int) *float64 {
	if whole > 0 {
		return floatPtr((float64(part) / float64(whole)) * 100.0)
	}
	return nil
}

func safePerThousand(value, distanceLy float64) *float64 {
	return safeDivide(value*1000.0, distanceLy)
}

func floatPtr(value float64) *float64 { return &value }
