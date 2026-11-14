package ed

import (
	"math"
	"time"
)

// Vec3 is a galactic coordinate in light years (journal StarPos order x, y, z).
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Distance returns the 3D Euclidean distance between pointA and pointB.
func Distance(pointA, pointB Vec3) float64 {
	deltaX, deltaY, deltaZ := pointB.X-pointA.X, pointB.Y-pointA.Y, pointB.Z-pointA.Z
	return math.Sqrt(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)
}

// Fleet carrier constants.
const (
	CarrierMaxJumpLy            = 500.0
	CarrierFuelTankTonnes       = 1000
	DefaultTritiumPricePerTonne = 50000
	CarrierJumpMaintenanceCost  = 100000
	carrierBaseMassTonnes       = 25000.0
	carrierFuelDivisor          = 200000.0
	carrierFixedJumpTonnes      = 5.0
	secondsPerWeek              = 7 * 24 * 3600.0
)

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
