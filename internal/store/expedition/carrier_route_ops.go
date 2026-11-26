package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// CarrierRoutePromptDefaults holds pre-resolved default form values for
// initiating a Spansh or manual Fleet Carrier route on an expedition.
type CarrierRoutePromptDefaults struct {
	TargetRowID  int64  `json:"targetRowId"`
	Origin       string `json:"origin"`
	Destination  string `json:"destination"`
	CarrierType  string `json:"carrierType"`
	CapacityUsed int    `json:"capacityUsed"`
	DepotTritium int    `json:"depotTritium"`
}

// CarrierRoutePromptSelection holds the resolved target row, start-system search
// bound, destination system, and coordinate-backed visited-waypoint fallback for
// preparing a Fleet Carrier route prompt.
type CarrierRoutePromptSelection struct {
	TargetRowID           int64
	UpToIndex             int
	Destination           string
	FallbackVisitedSystem string
}

// ResolveCarrierRoutePromptSelection resolves the target row, preceding search
// bound, destination system, and visited-waypoint fallback for targetRowID (or
// for the manual carrier plotter when targetRowID is non-positive).
func ResolveCarrierRoutePromptSelection(expeditionRecord Expedition, rows []Row, targetRowID int64) (CarrierRoutePromptSelection, error) {
	var selection CarrierRoutePromptSelection
	if latestVisited, _ := latestVisitedWaypoint(rows); latestVisited != nil && latestVisited.HasCoords {
		selection.FallbackVisitedSystem = strings.TrimSpace(latestVisited.SystemName)
	}
	if targetRowID > 0 {
		targetIndex := indexOf(rows, targetRowID)
		if targetIndex < 0 {
			return CarrierRoutePromptSelection{}, fmt.Errorf("row %d not found in expedition %d", targetRowID, expeditionRecord.ID)
		}
		selection.TargetRowID = targetRowID
		selection.UpToIndex = targetIndex
		selection.Destination = strings.TrimSpace(rows[targetIndex].SystemName)
		return selection, nil
	}
	if nextStopIndex := resolveNextFCStopIndex(rows, 0); nextStopIndex >= 0 {
		selection.TargetRowID = rows[nextStopIndex].ID
		selection.UpToIndex = nextStopIndex
		selection.Destination = strings.TrimSpace(rows[nextStopIndex].SystemName)
		return selection, nil
	}
	if expeditionRecord.Status != StatusPlanning {
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].IsWaypoint() && rows[i].VisitedAt != "" {
				selection.UpToIndex = i + 1
				return selection, nil
			}
		}
	}
	selection.UpToIndex = len(rows)
	return selection, nil
}

// ResolveCarrierStartSystem determines the default starting star system for a
// Fleet Carrier route using the fallback order: trimmed lastCarrierLocation,
// most recent preceding FCStop waypoint before upToIndex, most recent preceding
// waypoint before upToIndex, and finally trimmed playerSystem.
func ResolveCarrierStartSystem(rows []Row, upToIndex int, lastCarrierLocation, playerSystem string) string {
	if trimmedCarrierLocation := strings.TrimSpace(lastCarrierLocation); trimmedCarrierLocation != "" {
		return trimmedCarrierLocation
	}
	searchFrom := min(upToIndex-1, len(rows)-1)
	for i := searchFrom; i >= 0; i-- {
		candidate := rows[i]
		if candidate.IsWaypoint() && candidate.FCStop {
			if trimmedSystem := strings.TrimSpace(candidate.SystemName); trimmedSystem != "" {
				return trimmedSystem
			}
		}
	}
	for i := searchFrom; i >= 0; i-- {
		candidate := rows[i]
		if candidate.IsWaypoint() {
			if trimmedSystem := strings.TrimSpace(candidate.SystemName); trimmedSystem != "" {
				return trimmedSystem
			}
		}
	}
	return strings.TrimSpace(playerSystem)
}

func resolveNextFCStopIndex(rows []Row, nextFCStopRowID int64) int {
	if nextFCStopRowID > 0 {
		matchedIndex := indexOf(rows, nextFCStopRowID)
		if matchedIndex >= 0 && rows[matchedIndex].IsWaypoint() && rows[matchedIndex].FCStop && !rows[matchedIndex].FCArrived {
			return matchedIndex
		}
	}
	for i, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.FCStop && !rowItem.FCArrived {
			return i
		}
	}
	return -1
}

// ResolveRoutedCarrierStopIndex returns the row slice index locked by an active
// Spansh or manual Fleet Carrier route, or -1 when no carrier route is active.
func ResolveRoutedCarrierStopIndex(rows []Row, nextFCStopRowID int64, activeRoute *ActiveCarrierRouteState) int {
	if activeRoute == nil {
		return -1
	}
	nextStopIndex := resolveNextFCStopIndex(rows, nextFCStopRowID)
	if activeRoute.IsManualMode() {
		return nextStopIndex
	}
	if activeRoute.TargetRowID > 0 {
		if targetIndex := indexOf(rows, activeRoute.TargetRowID); targetIndex >= 0 {
			return targetIndex
		}
	}
	return nextStopIndex
}

// ResolveRoutedCarrierStopRowID returns the row ID locked by an active Spansh
// or manual Fleet Carrier route, or 0 when no routed stop applies.
func ResolveRoutedCarrierStopRowID(rows []Row, nextFCStopRowID int64, activeRoute *ActiveCarrierRouteState) int64 {
	routedIndex := ResolveRoutedCarrierStopIndex(rows, nextFCStopRowID, activeRoute)
	if routedIndex < 0 || routedIndex >= len(rows) {
		return 0
	}
	return rows[routedIndex].ID
}

// ResolveArrivalTargetRowID resolves the waypoint row ID to mark arrived when
// completing a Fleet Carrier route, preferring an active Spansh route's target
// row when still present in rows and falling back to the next unarrived FC stop.
func ResolveArrivalTargetRowID(rows []Row, activeRoute *ActiveCarrierRouteState) int64 {
	nextFCStopIndex := resolveNextFCStopIndex(rows, 0)
	var nextFCStopRowID int64
	if nextFCStopIndex >= 0 {
		nextFCStopRowID = rows[nextFCStopIndex].ID
	}
	if activeRoute == nil || activeRoute.IsManualMode() {
		return nextFCStopRowID
	}
	if activeRoute.TargetRowID > 0 && indexOf(rows, activeRoute.TargetRowID) >= 0 {
		return activeRoute.TargetRowID
	}
	return nextFCStopRowID
}

// RecalculateRouteDepotFuel updates the carrier depot tritium level and optional
// used cargo capacity, recalculating tritium burn for hops at or after fromJumpIndex.
func RecalculateRouteDepotFuel(route CarrierRouteResult, fromJumpIndex, newDepot int, newCapacityUsed *int) CarrierRouteResult {
	clampedDepot := max(0, min(reference.CarrierFuelTankTonnes, newDepot))
	maxCapacity := reference.PersonalCarrierMaxCapacity
	if route.CarrierType == "squadron" {
		maxCapacity = reference.SquadronCarrierMaxCapacity
	}
	updatedCapacity := route.CapacityUsed
	if newCapacityUsed != nil {
		updatedCapacity = *newCapacityUsed
	}
	updatedCapacity = max(0, min(maxCapacity, updatedCapacity))

	recalculatedJumps := make([]CarrierRouteJump, len(route.Jumps))
	totalTritium := 0
	for hopIndex, jump := range route.Jumps {
		if hopIndex >= fromJumpIndex {
			jump.TritiumUsed = reference.CarrierTritiumCost(jump.DistanceLy, updatedCapacity, clampedDepot)
			jump.FuelInTank = clampedDepot
		}
		recalculatedJumps[hopIndex] = jump
		totalTritium += jump.TritiumUsed
	}

	route.DepotTritium = clampedDepot
	route.CapacityUsed = updatedCapacity
	route.Jumps = recalculatedJumps
	route.TotalTritium = totalTritium
	return route
}

// RefillCarrierDepotState transfers tritium from used carrier capacity into the
// 1000-tonne depot tank (or tops up to 1000 tonnes when used capacity is zero)
// and recalculates remaining route hops, reporting whether any fuel was added.
func RefillCarrierDepotState(state ActiveCarrierRouteState) (ActiveCarrierRouteState, bool) {
	clampedDepot := max(0, min(reference.CarrierFuelTankTonnes, state.Route.DepotTritium))
	neededTonnes := reference.CarrierFuelTankTonnes - clampedDepot
	if neededTonnes <= 0 {
		return state, false
	}
	transferTonnes := neededTonnes
	nextCapacity := state.Route.CapacityUsed
	if state.Route.CapacityUsed > 0 {
		transferTonnes = min(neededTonnes, state.Route.CapacityUsed)
		nextCapacity = max(0, state.Route.CapacityUsed-transferTonnes)
	}
	nextDepot := clampedDepot + transferTonnes
	state.Route = RecalculateRouteDepotFuel(state.Route, max(0, state.JumpIndex), nextDepot, &nextCapacity)
	return state, true
}

// SwitchRouteStateToManual transitions an active carrier route into manual jump
// logging mode, seeding draft distance and tritium inputs from the current hop.
func SwitchRouteStateToManual(state ActiveCarrierRouteState) ActiveCarrierRouteState {
	state.ManualMode = true
	state.ManualDistanceInput = ""
	state.ManualTritiumInput = ""
	if state.JumpIndex >= 0 && state.JumpIndex < len(state.Route.Jumps) {
		currentHop := state.Route.Jumps[state.JumpIndex]
		if currentHop.DistanceLy > 0 {
			state.ManualDistanceInput = strconv.FormatFloat(math.Round(currentHop.DistanceLy*100)/100, 'f', -1, 64)
		}
		if currentHop.TritiumUsed > 0 {
			state.ManualTritiumInput = strconv.Itoa(currentHop.TritiumUsed)
		}
	}
	return state
}

// CompleteSpanshJumpTx advances an active Spansh carrier route by one hop (or
// completes waypoint arrival on the final hop), recording jump telemetry and
// recalculating remaining hop tritium costs atomically in a single transaction.
func CompleteSpanshJumpTx(dbConn *sql.DB, expeditionID int64) (nextState *ActiveCarrierRouteState, applied bool, recordedJump bool, err error) {
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		rawState, routeError := GetActiveCarrierRoute(transaction, expeditionID)
		if routeError != nil {
			return routeError
		}
		normalizedState := NormalizeActiveCarrierRoute(rawState)
		if normalizedState == nil || len(normalizedState.Route.Jumps) == 0 {
			return nil
		}
		state := *normalizedState
		if state.ExpeditionID == 0 {
			state.ExpeditionID = expeditionID
		}

		allJumpsRecorded := state.JumpIndex >= len(state.Route.Jumps)
		var currentHop CarrierRouteJump
		if !allJumpsRecorded {
			currentHop = state.Route.Jumps[state.JumpIndex]
			if state.Route.DepotTritium < currentHop.TritiumUsed {
				return errors.New("insufficient depot tritium for jump")
			}
		}

		if allJumpsRecorded || state.JumpIndex+1 >= len(state.Route.Jumps) {
			rows, rowsError := ListRows(transaction, expeditionID)
			if rowsError != nil {
				return rowsError
			}
			targetRowID := ResolveArrivalTargetRowID(rows, &state)
			jumpDistance := 0.0
			jumpTritium := 0
			if !allJumpsRecorded {
				jumpDistance = currentHop.DistanceLy
				jumpTritium = currentHop.TritiumUsed
			}
			if arrivalError := applyCarrierRouteArrival(transaction, expeditionID, targetRowID, jumpDistance, jumpTritium); arrivalError != nil {
				return arrivalError
			}
			applied = true
			recordedJump = !allJumpsRecorded && (currentHop.DistanceLy > 0 || currentHop.TritiumUsed > 0)
			return nil
		}

		nextJumpIndex := state.JumpIndex + 1
		nextDepot := max(0, state.Route.DepotTritium-currentHop.TritiumUsed)
		state.Route = RecalculateRouteDepotFuel(state.Route, nextJumpIndex, nextDepot, nil)
		state.JumpIndex = nextJumpIndex
		if jumpError := RecordCarrierJump(transaction, expeditionID, currentHop.DistanceLy, currentHop.TritiumUsed); jumpError != nil {
			return jumpError
		}
		if saveError := SaveActiveCarrierRoute(transaction, state); saveError != nil {
			return saveError
		}
		nextState = &state
		applied = true
		recordedJump = true
		return nil
	})
	if txError != nil {
		return nil, false, false, txError
	}
	return nextState, applied, recordedJump, nil
}

// LogManualCarrierJumpTx records a manual Fleet Carrier jump on expeditionID and
// increments the active manual route leg totals while clearing draft inputs.
func LogManualCarrierJumpTx(dbConn *sql.DB, expeditionID int64, distanceLy float64, tritiumUsed int) (*ActiveCarrierRouteState, bool, error) {
	if math.IsNaN(distanceLy) || math.IsInf(distanceLy, 0) || distanceLy < 0 {
		distanceLy = 0
	}
	tritiumUsed = max(0, tritiumUsed)
	if distanceLy <= 0 && tritiumUsed <= 0 {
		return nil, false, nil
	}

	var resultState *ActiveCarrierRouteState
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		rawState, routeError := GetActiveCarrierRoute(transaction, expeditionID)
		if routeError != nil {
			return routeError
		}
		normalizedState := NormalizeActiveCarrierRoute(rawState)
		if normalizedState == nil {
			return errors.New("no active carrier route")
		}

		state := *normalizedState
		state.ExpeditionID = expeditionID
		state.ManualLegJumps++
		state.ManualLegDistance += distanceLy
		state.ManualLegTritium += tritiumUsed
		state.ManualDistanceInput = ""
		state.ManualTritiumInput = ""
		if jumpError := RecordCarrierJump(transaction, expeditionID, distanceLy, tritiumUsed); jumpError != nil {
			return jumpError
		}
		if saveError := SaveActiveCarrierRoute(transaction, state); saveError != nil {
			return saveError
		}
		resultState = &state
		return nil
	})
	if txError != nil {
		return nil, false, txError
	}
	return resultState, true, nil
}
