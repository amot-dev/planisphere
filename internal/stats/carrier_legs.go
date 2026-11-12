package stats

import (
	"database/sql"
	"encoding/json"

	"planisphere/internal/ed"
	"planisphere/internal/journal"
)

// CarrierLeg is the derived distance and tritium consumption of a single CarrierJump.
type CarrierLeg struct {
	DistanceLy float64
	Tritium    int
	FromDelta  bool // tritium measured from CarrierStats fuel delta rather than estimated
}

// CarrierLegs simulates the carrier's history up to untilTimestamp and returns the
// distance and tritium of every CarrierJump keyed by event UID. Distance is the
// 3D distance from the player's immediately preceding system position
// (Location, FSDJump, or CarrierJump) to the CarrierJump destination StarPos.
// Tritium is taken from the CarrierStats fuel delta when a jump is bracketed by
// two CarrierStats snapshots with no other jump in between (and the delta is
// plausible); otherwise it is estimated with ed.CarrierTritiumCost using the
// latest known fuel level and used capacity.
func (engine *Engine) CarrierLegs(untilTimestamp string) (map[string]CarrierLeg, error) {
	return engine.CarrierLegsRange("", untilTimestamp)
}

// seedCarrierSimulationState loads the player's last system coordinates and
// most recent CarrierStats snapshot at or before afterTimestamp.
func (engine *Engine) seedCarrierSimulationState(afterTimestamp string) (*ed.Vec3, *journal.CarrierStatsEvent, int, int, error) {
	var previousPosition *ed.Vec3
	carrierFuelLevel, carrierUsedCapacity := 1000, 0 // assumptions until a CarrierStats snapshot is seen
	var latestStatsSnapshot *journal.CarrierStatsEvent

	if afterTimestamp == "" {
		return previousPosition, latestStatsSnapshot, carrierFuelLevel, carrierUsedCapacity, nil
	}

	var starX, starY, starZ sql.NullFloat64
	positionQueryError := engine.DB.QueryRow(`SELECT star_x, star_y, star_z FROM journal_events
		WHERE event IN ('FSDJump', 'Location', 'CarrierJump') AND star_x IS NOT NULL
		AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`, afterTimestamp).Scan(&starX, &starY, &starZ)
	if positionQueryError != nil && positionQueryError != sql.ErrNoRows {
		return nil, nil, 0, 0, positionQueryError
	}
	if starX.Valid && starY.Valid && starZ.Valid {
		previousPosition = &ed.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}
	}

	var rawCarrierStats string
	statsQueryError := engine.DB.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'CarrierStats' AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`, afterTimestamp).Scan(&rawCarrierStats)
	if statsQueryError != nil && statsQueryError != sql.ErrNoRows {
		return nil, nil, 0, 0, statsQueryError
	}
	if rawCarrierStats != "" {
		var carrierStats journal.CarrierStatsEvent
		if json.Unmarshal([]byte(rawCarrierStats), &carrierStats) == nil {
			latestStatsSnapshot = &carrierStats
			carrierFuelLevel, carrierUsedCapacity = carrierStats.FuelLevel, carrierStats.UsedCapacity()
		}
	}

	return previousPosition, latestStatsSnapshot, carrierFuelLevel, carrierUsedCapacity, nil
}

// CarrierLegsRange simulates carrier jumps in (afterTimestamp, untilTimestamp],
// seeding the initial position and CarrierStats snapshot from the most recent
// events at or before afterTimestamp when afterTimestamp is non-empty.
func (engine *Engine) CarrierLegsRange(afterTimestamp, untilTimestamp string) (map[string]CarrierLeg, error) {
	previousPosition, latestStatsSnapshot, carrierFuelLevel, carrierUsedCapacity, seedError := engine.seedCarrierSimulationState(afterTimestamp)
	if seedError != nil {
		return nil, seedError
	}

	events, loadError := engine.loadEvents(afterTimestamp, untilTimestamp, []string{"FSDJump", "Location", "CarrierJump", "CarrierStats"})
	if loadError != nil {
		return nil, loadError
	}

	carrierLegsByEventUID := map[string]CarrierLeg{}
	var pendingJumpEventUID string // jump since latestStatsSnapshot (only if exactly one)
	jumpsSinceLastStats := 0

	for _, currentEventRow := range events {
		switch currentEventRow.eventType {
		case "FSDJump", "Location":
			if currentEventRow.starPosition != nil {
				positionCopy := *currentEventRow.starPosition
				previousPosition = &positionCopy
			}
		case "CarrierStats":
			var carrierStats journal.CarrierStatsEvent
			if json.Unmarshal([]byte(currentEventRow.rawJSON), &carrierStats) != nil {
				continue
			}
			if latestStatsSnapshot != nil && jumpsSinceLastStats == 1 && pendingJumpEventUID != "" && carrierStats.CarrierID == latestStatsSnapshot.CarrierID {
				fuelDelta := latestStatsSnapshot.FuelLevel - carrierStats.FuelLevel
				recordedLeg := carrierLegsByEventUID[pendingJumpEventUID]
				if fuelDelta > 0 && recordedLeg.Tritium > 0 && fuelDelta <= recordedLeg.Tritium*2+10 {
					recordedLeg.Tritium, recordedLeg.FromDelta = fuelDelta, true
					carrierLegsByEventUID[pendingJumpEventUID] = recordedLeg
				}
			}
			carrierStatsCopy := carrierStats
			latestStatsSnapshot = &carrierStatsCopy
			carrierFuelLevel, carrierUsedCapacity = carrierStats.FuelLevel, carrierStats.UsedCapacity()
			jumpsSinceLastStats, pendingJumpEventUID = 0, ""
		case "CarrierJump":
			computedLeg := CarrierLeg{}
			if previousPosition != nil && currentEventRow.starPosition != nil {
				computedLeg.DistanceLy = ed.Distance(*previousPosition, *currentEventRow.starPosition)
				computedLeg.Tritium = ed.CarrierTritiumCost(computedLeg.DistanceLy, carrierUsedCapacity, carrierFuelLevel)
				carrierFuelLevel -= computedLeg.Tritium
				if carrierFuelLevel < 0 {
					carrierFuelLevel = 0
				}
			}
			if currentEventRow.starPosition != nil {
				positionCopy := *currentEventRow.starPosition
				previousPosition = &positionCopy
			}
			carrierLegsByEventUID[currentEventRow.eventUID] = computedLeg
			jumpsSinceLastStats++
			pendingJumpEventUID = currentEventRow.eventUID
		}
	}
	return carrierLegsByEventUID, nil
}

