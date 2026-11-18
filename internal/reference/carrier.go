package reference

import (
	"math"
	"time"
)

// Fleet carrier constants.
const (
	FleetCarrierStationType     = "FleetCarrier"
	PersonalCarrierMaxCapacity  = 25000
	SquadronCarrierMaxCapacity  = 60000
	CarrierMaxJumpLy            = 500.0
	CarrierFuelTankTonnes       = 1000
	DefaultTritiumPricePerTonne = 50000
	CarrierJumpMaintenanceCost  = 100000
	carrierBaseMassTonnes       = 25000.0
	carrierFuelDivisor          = 200000.0
	carrierFixedJumpTonnes      = 5.0
	secondsPerWeek              = 7 * 24 * 3600.0
)

// CarrierCartographicsTariff calculates Universal Cartographics' 25% Fleet Carrier
// tariff from the journal's post-tariff BaseValue and Bonus fields. When docked
// on a Fleet Carrier (including the commander's own vessel), Frontier logs
// BaseValue + Bonus at exact 75% of pre-tariff scan value, making the 25%
// tariff equal to one third of (BaseValue + Bonus).
func CarrierCartographicsTariff(isDockedOnFleetCarrier bool, baseValue, bonusValue int64) int64 {
	if !isDockedOnFleetCarrier {
		return 0
	}
	postTariffGross := baseValue + bonusValue
	if postTariffGross <= 0 {
		return 0
	}
	return postTariffGross / 3
}

// CarrierTritiumCost estimates the tritium consumed by a fleet carrier jump
// using the standard in-game formula:
//
//	round(5 + dist * (usedCapacity + fuelLevel + 25000) / 200000)
//
// Elite Dangerous rounds fuel cost to the nearest whole number (round).
// usedCapacity is the occupied cargo/module/ship-pack capacity in tonnes
// (TotalCapacity - FreeSpace from CarrierStats) and fuelLevel is the tritium
// currently in the carrier's fuel tank (depot).
func CarrierTritiumCost(distLy float64, usedCapacity, fuelLevel int) int {
	if distLy <= 0 {
		return 0
	}
	if usedCapacity < 0 {
		usedCapacity = 0
	}
	if fuelLevel < 0 {
		fuelLevel = 0
	}
	return int(math.Round(carrierFixedJumpTonnes + distLy*(float64(usedCapacity)+float64(fuelLevel)+carrierBaseMassTonnes)/carrierFuelDivisor))
}

// CarrierProratedUpkeep computes continuously pro-rated carrier upkeep in credits
// over the elapsed interval between startTimestamp and endTimestamp (or nowUTC
// when endTimestamp is empty).
func CarrierProratedUpkeep(startTimestamp, endTimestamp string, weeklyUpkeep int64, nowUTC time.Time) int64 {
	if weeklyUpkeep <= 0 || startTimestamp == "" {
		return 0
	}
	startTime, parseStartError := ParseISO(startTimestamp)
	if parseStartError != nil {
		return 0
	}
	endTime := nowUTC.UTC()
	if endTimestamp != "" {
		parsedEndTime, parseEndError := ParseISO(endTimestamp)
		if parseEndError != nil {
			return 0
		}
		endTime = parsedEndTime
	}
	elapsedSeconds := endTime.Sub(startTime).Seconds()
	if elapsedSeconds <= 0 {
		return 0
	}
	return int64(math.Round((elapsedSeconds / secondsPerWeek) * float64(weeklyUpkeep)))
}
