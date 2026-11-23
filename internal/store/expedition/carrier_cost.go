package expedition

import (
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/settings"
)

// CarrierCost holds the expedition Fleet Carrier credit cost breakdown.
type CarrierCost struct {
	CarrierType    string `json:"carrierType"`
	WeeklyUpkeep   int64  `json:"weeklyUpkeep"`
	TritiumPrice   int64  `json:"tritiumPrice"`
	TritiumCost    int64  `json:"tritiumCost"`
	JumpCost       int64  `json:"jumpCost"`
	UpkeepCost     int64  `json:"upkeepCost"`
	TotalCost      int64  `json:"totalCost"`
	TotalEstimated bool   `json:"totalEstimated"`
}

// ComputeCarrierCost computes the expedition Fleet Carrier tritium, jump maintenance, and prorated upkeep breakdown in credits.
func ComputeCarrierCost(
	queryer db.Queryer,
	expeditionRecord Expedition,
	expeditionMetrics replaystats.Metrics,
	effectiveStartTimestamp string,
	expeditionDeparted bool,
	currentTime time.Time,
) CarrierCost {
	if currentTime.IsZero() {
		currentTime = time.Now().UTC()
	}
	tritiumPrice := settings.TritiumPricePerTonne(queryer)
	var tritiumTonnes int64
	if expeditionMetrics.TritiumUsed != nil && *expeditionMetrics.TritiumUsed > 0 {
		tritiumTonnes = int64(*expeditionMetrics.TritiumUsed)
	}
	tritiumCost := tritiumTonnes * tritiumPrice
	jumpCost := int64(expeditionMetrics.CarrierJumps) * reference.CarrierJumpMaintenanceCost
	var upkeepCost int64
	if expeditionDeparted && effectiveStartTimestamp != "" && expeditionRecord.CarrierWeeklyUpkeep > 0 {
		upkeepCost = reference.CarrierProratedUpkeep(
			effectiveStartTimestamp,
			expeditionRecord.EndTimestamp,
			expeditionRecord.CarrierWeeklyUpkeep,
			currentTime,
		)
	}
	totalCost := tritiumCost + jumpCost + upkeepCost
	return CarrierCost{
		CarrierType:    expeditionRecord.CarrierType,
		WeeklyUpkeep:   expeditionRecord.CarrierWeeklyUpkeep,
		TritiumPrice:   tritiumPrice,
		TritiumCost:    tritiumCost,
		JumpCost:       jumpCost,
		UpkeepCost:     upkeepCost,
		TotalCost:      totalCost,
		TotalEstimated: tritiumCost > 0 || upkeepCost > 0,
	}
}
