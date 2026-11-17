package projections

import (
	"database/sql"
	"encoding/json"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

var carrierSimulationEvents = journal.NewEventSet(
	journal.EventFSDJump,
	journal.EventLocation,
	journal.EventCarrierJump,
	journal.EventCarrierStats,
)

// CarrierLeg is the derived distance and tritium consumption of a single CarrierJump.
type CarrierLeg struct {
	DistanceLy float64
	Tritium    int
	FromDelta  bool // tritium measured from CarrierStats fuel delta rather than estimated
}

// CarrierLegs simulates the carrier's history up to untilTimestamp and returns the
// distance and tritium of every CarrierJump keyed by event UID.
func CarrierLegs(queryer db.Queryer, untilTimestamp string) (map[string]CarrierLeg, error) {
	return CarrierLegsRange(queryer, "", untilTimestamp)
}

func seedCarrierSimulationState(queryer db.Queryer, afterTimestamp string) (*reference.Vec3, *journal.CarrierStatsEvent, int, int, error) {
	var previousPosition *reference.Vec3
	carrierFuelLevel, carrierUsedCapacity := 1000, 0 // assumptions until a CarrierStats snapshot is seen
	var latestStatsSnapshot *journal.CarrierStatsEvent

	if afterTimestamp == "" {
		return previousPosition, latestStatsSnapshot, carrierFuelLevel, carrierUsedCapacity, nil
	}

	var starX, starY, starZ sql.NullFloat64
	positionQueryError := queryer.QueryRow(`SELECT star_x, star_y, star_z FROM journal_events
		WHERE event IN ('FSDJump', 'Location', 'CarrierJump') AND star_x IS NOT NULL
		AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`, afterTimestamp).Scan(&starX, &starY, &starZ)
	if positionQueryError != nil && positionQueryError != sql.ErrNoRows {
		return nil, nil, 0, 0, positionQueryError
	}
	if starX.Valid && starY.Valid && starZ.Valid {
		previousPosition = &reference.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}
	}

	var rawCarrierStats string
	statsQueryError := queryer.QueryRow(`SELECT raw_json FROM journal_events
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
// events at or before afterTimestamp when afterTimestamp is non-empty. Distance
// is the 3D distance from the player's immediately preceding system position to
// the CarrierJump destination StarPos. Tritium is taken from the CarrierStats
// fuel delta when a jump is bracketed by two CarrierStats snapshots with no
// other jump in between and the delta is plausible; otherwise it is estimated
// with reference.CarrierTritiumCost using the latest known fuel level and used capacity.
func CarrierLegsRange(queryer db.Queryer, afterTimestamp, untilTimestamp string) (map[string]CarrierLeg, error) {
	previousPosition, latestStatsSnapshot, carrierFuelLevel, carrierUsedCapacity, seedError := seedCarrierSimulationState(queryer, afterTimestamp)
	if seedError != nil {
		return nil, seedError
	}

	loadedRows, loadError := journal.LoadEvents(queryer, afterTimestamp, untilTimestamp, carrierSimulationEvents)
	if loadError != nil {
		return nil, loadError
	}

	carrierLegsByEventUID := map[string]CarrierLeg{}
	var pendingJumpEventUID string // jump since latestStatsSnapshot (only if exactly one)
	jumpsSinceLastStats := 0

	for _, currentEventRow := range loadedRows {
		switch currentEventRow.Type {
		case journal.EventFSDJump, journal.EventLocation:
			if currentEventRow.StarPos != nil {
				positionCopy := *currentEventRow.StarPos
				previousPosition = &positionCopy
			}
		case journal.EventCarrierStats:
			var carrierStats journal.CarrierStatsEvent
			if json.Unmarshal([]byte(currentEventRow.RawJSON), &carrierStats) != nil {
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
		case journal.EventCarrierJump:
			computedLeg := CarrierLeg{}
			if previousPosition != nil && currentEventRow.StarPos != nil {
				computedLeg.DistanceLy = reference.Distance(*previousPosition, *currentEventRow.StarPos)
				computedLeg.Tritium = reference.CarrierTritiumCost(computedLeg.DistanceLy, carrierUsedCapacity, carrierFuelLevel)
				carrierFuelLevel -= computedLeg.Tritium
				if carrierFuelLevel < 0 {
					carrierFuelLevel = 0
				}
			}
			carrierLegsByEventUID[currentEventRow.UID] = computedLeg
			if currentEventRow.StarPos != nil {
				positionCopy := *currentEventRow.StarPos
				previousPosition = &positionCopy
			}
			jumpsSinceLastStats++
			if jumpsSinceLastStats == 1 {
				pendingJumpEventUID = currentEventRow.UID
			} else {
				pendingJumpEventUID = ""
			}
		}
	}
	return carrierLegsByEventUID, nil
}

