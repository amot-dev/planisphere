package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/edsm"
	"planisphere/internal/sources/spansh"
	"planisphere/internal/store/expedition"
)

// CarrierRouteJump is one hop in a plotted Spansh Fleet Carrier route.
type CarrierRouteJump = expedition.CarrierRouteJump

// CarrierRouteResult is returned to the UI when initiating a carrier route to a waypoint.
type CarrierRouteResult = expedition.CarrierRouteResult

// ActiveCarrierRouteState stores the active fleet carrier navigation state
// for an expedition, persisting across application restarts and sessions.
type ActiveCarrierRouteState = expedition.ActiveCarrierRouteState

// CarrierDepotInfo contains carrier mass and tritium depot telemetry from journal events.
type CarrierDepotInfo = replay.CarrierDepotInfo

// GetActiveCarrierRoute retrieves the persisted active carrier route for an expedition.
func (app *App) GetActiveCarrierRoute(expeditionID int64) (*ActiveCarrierRouteState, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	return expedition.GetActiveCarrierRoute(app.db, expeditionID)
}

// SaveActiveCarrierRoute persists the active carrier route state for an expedition.
func (app *App) SaveActiveCarrierRoute(state ActiveCarrierRouteState) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	return expedition.SaveActiveCarrierRoute(app.db, state)
}

// ClearActiveCarrierRoute deletes the active carrier route for an expedition.
func (app *App) ClearActiveCarrierRoute(expeditionID int64) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	return expedition.ClearActiveCarrierRoute(app.db, expeditionID)
}

// SaveActiveCarrierRouteWithUndo persists state for the expedition and records
// a session undo entry when the serialized route JSON changes.
func (app *App) SaveActiveCarrierRouteWithUndo(state ActiveCarrierRouteState, label string) error {
	if state.ExpeditionID <= 0 {
		return fmt.Errorf("invalid expedition ID: %d", state.ExpeditionID)
	}
	trimmedLabel := strings.TrimSpace(label)
	if trimmedLabel == "" {
		trimmedLabel = "Update Carrier Route"
	}
	_, saveError := app.recordUndoableMutation(state.ExpeditionID, trimmedLabel, func(targetDB *sql.DB) (bool, error) {
		return expedition.SaveActiveCarrierRouteIfChanged(targetDB, state)
	})
	return saveError
}

// ClearActiveCarrierRouteWithUndo deletes any active carrier route for expeditionID
// and records a session undo entry when a route existed.
func (app *App) ClearActiveCarrierRouteWithUndo(expeditionID int64) error {
	_, clearError := app.recordUndoableMutation(expeditionID, "Clear Carrier Route", func(targetDB *sql.DB) (bool, error) {
		return expedition.ClearActiveCarrierRouteIfPresent(targetDB, expeditionID)
	})
	return clearError
}

// GetCarrierLastLocation returns the system name of the most recent known Fleet Carrier location.
func (app *App) GetCarrierLastLocation() (string, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return "", readyError
	}
	if app.carrierState == nil {
		return "", nil
	}
	snapshot, snapshotError := app.carrierState.LatestStats()
	if snapshotError != nil {
		return "", snapshotError
	}
	return replay.QueryCarrierLastLocation(app.db, snapshot)
}

// GetCarrierDepotInfo returns the latest Fleet Carrier capacity, depot tritium level, and cargo storage.
// It retrieves the actual in-game values from CarrierStats or CarrierDepositFuel journal events,
// defaulting DepotTritium to 1000 (full depot tank) and UsedCapacity to 5000 if not recorded.
func (app *App) GetCarrierDepotInfo() (CarrierDepotInfo, error) {
	defaultInfo := CarrierDepotInfo{
		UsedCapacity: 5000,
		DepotTritium: reference.CarrierFuelTankTonnes,
		Cargo:        0,
	}
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return defaultInfo, readyError
	}
	if app.carrierState == nil {
		return defaultInfo, nil
	}
	snapshot, snapshotError := app.carrierState.LatestStats()
	if snapshotError != nil {
		return defaultInfo, snapshotError
	}
	return replay.QueryCarrierDepotInfo(app.db, snapshot)
}

// RecordCarrierJump logs a single completed expedition carrier jump (from Spansh hop completion or Manual Mode)
// into the expedition's manual carrier adjustments.
func (app *App) RecordCarrierJump(expeditionID int64, distanceLy float64, tritium int) error {
	applied, err := app.recordUndoableMutation(expeditionID, "Log Carrier Jump", func(targetDB *sql.DB) (bool, error) {
		err := expedition.RecordCarrierJump(targetDB, expeditionID, distanceLy, tritium)
		return err == nil, err
	})
	if err != nil {
		return err
	}
	if applied {
		app.publishApp(expeditionID, events.AppAdjustmentsChanged)
	}
	return nil
}

// RecordCarrierJumpAndSaveRoute atomically records a carrier jump and persists
// the updated active carrier route state under a single session undo entry.
func (app *App) RecordCarrierJumpAndSaveRoute(expeditionID int64, distanceLy float64, tritium int, state ActiveCarrierRouteState) error {
	applied, err := app.recordUndoableMutation(expeditionID, "Log Carrier Jump", func(targetDB *sql.DB) (bool, error) {
		err := expedition.RecordCarrierJumpAndSaveRouteTx(targetDB, expeditionID, distanceLy, tritium, state)
		return err == nil, err
	})
	if err != nil {
		return err
	}
	if applied {
		app.publishApp(expeditionID, events.AppAdjustmentsChanged)
	}
	return nil
}

// CompleteCarrierRouteArrival atomically logs any final carrier jump, clears the
// active carrier route, and marks targetRowID arrived when positive under one undo entry.
func (app *App) CompleteCarrierRouteArrival(expeditionID int64, targetRowID int64, distanceLy float64, tritium int) error {
	applied, err := app.recordUndoableSnapshotDiff(expeditionID, "Complete Carrier Arrival", func(targetDB *sql.DB) error {
		return expedition.CompleteCarrierRouteArrivalTx(targetDB, expeditionID, targetRowID, distanceLy, tritium)
	})
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	if distanceLy > 0 || tritium > 0 {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppAdjustmentsChanged)
	} else {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return nil
}

func normalizeCarrierPlotParameters(carrierType string, capacityUsed int, depotTritium int) (string, int, int) {
	if carrierType == "" {
		carrierType = "player"
	}
	maxCapacity := reference.PersonalCarrierMaxCapacity
	if carrierType == "squadron" {
		maxCapacity = reference.SquadronCarrierMaxCapacity
	}
	if capacityUsed < 0 {
		capacityUsed = 0
	}
	if capacityUsed > maxCapacity {
		capacityUsed = maxCapacity
	}
	if depotTritium <= 0 || depotTritium > reference.CarrierFuelTankTonnes {
		depotTritium = reference.CarrierFuelTankTonnes
	}
	return carrierType, capacityUsed, depotTritium
}

func (app *App) resolveCarrierPlotInputs(source, destination string, depotTritium int) (string, string, int, context.Context) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	app.waitStartupLocked()
	resolvedDepot := depotTritium
	if app.db == nil {
		return strings.TrimSpace(source), strings.TrimSpace(destination), resolvedDepot, app.contextLocked()
	}
	canonicalLookup := func(systemName string) (string, error) {
		return replay.QueryCanonicalSystemName(app.db, systemName)
	}
	canonicalSource := edsm.CanonicalSystemName(app.db, source, canonicalLookup)
	canonicalDest := edsm.CanonicalSystemName(app.db, destination, canonicalLookup)
	if depotTritium <= 0 && app.carrierState != nil {
		if snapshot, snapshotError := app.carrierState.LatestStats(); snapshotError == nil {
			if depotInfo, depotError := replay.QueryCarrierDepotInfo(app.db, snapshot); depotError == nil {
				resolvedDepot = depotInfo.DepotTritium
			}
		}
	}
	return canonicalSource, canonicalDest, resolvedDepot, app.contextLocked()
}

// PlotCarrierRoute queries the Spansh Fleet Carrier router for a path from source to destination,
// falling back cleanly to manual mode on unknown systems, offline mode, or API timeouts.
func (app *App) PlotCarrierRoute(source, destination string, capacityUsed int, carrierType string, depotTritium int) (CarrierRouteResult, error) {
	canonicalSource, canonicalDest, resolvedDepot, parentCtx := app.resolveCarrierPlotInputs(source, destination, depotTritium)
	carrierType, capacityUsed, resolvedDepot = normalizeCarrierPlotParameters(carrierType, capacityUsed, resolvedDepot)
	routeResult := CarrierRouteResult{
		Origin:       canonicalSource,
		Destination:  canonicalDest,
		CarrierType:  carrierType,
		CapacityUsed: capacityUsed,
		DepotTritium: resolvedDepot,
		Jumps:        []CarrierRouteJump{},
	}
	if canonicalSource == "" || canonicalDest == "" {
		routeResult.ManualMode = true
		routeResult.Error = "Origin or destination system name is empty — switched to Manual Mode."
		return routeResult, nil
	}
	if strings.EqualFold(canonicalSource, canonicalDest) {
		routeResult.ManualMode = true
		routeResult.Error = "Origin and destination are the same system — switched to Manual Mode."
		return routeResult, nil
	}

	requestCtx, cancel := context.WithTimeout(parentCtx, 60*time.Second)
	defer cancel()

	spanshJumps, queryError := spansh.QueryCarrierRoute(requestCtx, canonicalSource, canonicalDest, capacityUsed, resolvedDepot)
	if queryError != nil || len(spanshJumps) == 0 {
		routeResult.ManualMode = true
		if queryError != nil {
			routeResult.Error = queryError.Error()
		} else {
			routeResult.Error = "Spansh returned no jumps for this route — switched to Manual Mode."
		}
		return routeResult, nil
	}

	mappedJumps := make([]CarrierRouteJump, 0, len(spanshJumps))
	for _, jump := range spanshJumps {
		mappedJumps = append(mappedJumps, CarrierRouteJump{
			SystemName:       jump.SystemName,
			DistanceLy:       jump.DistanceLy,
			DistanceToDestLy: jump.DistanceToDestLy,
			TritiumUsed:      jump.TritiumUsed,
			FuelInTank:       jump.FuelInTank,
			HasIcyRing:       jump.HasIcyRing,
		})
		routeResult.TotalDistLy += jump.DistanceLy
		routeResult.TotalTritium += jump.TritiumUsed
	}
	routeResult.Jumps = mappedJumps
	routeResult.TotalJumps = len(mappedJumps)
	return routeResult, nil
}
