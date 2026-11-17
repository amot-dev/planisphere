package app

import (
	"context"

	"planisphere/internal/core/events"
	"planisphere/internal/projections"
	"planisphere/internal/sources/edsm"
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
type CarrierDepotInfo = projections.CarrierDepotInfo

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
	return app.carrierState.LastLocation()
}

// GetCarrierDepotInfo returns the latest Fleet Carrier capacity, depot tritium level, and cargo storage.
func (app *App) GetCarrierDepotInfo() (CarrierDepotInfo, error) {
	defaultInfo := CarrierDepotInfo{
		UsedCapacity: 5000,
		DepotTritium: 1000,
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
	return app.carrierState.DepotInfo()
}

// RecordCarrierJump logs a single completed expedition carrier jump (from Spansh hop completion or Manual Mode)
// into the expedition's manual carrier adjustments.
func (app *App) RecordCarrierJump(expeditionID int64, distanceLy float64, tritium int) error {
	if err := app.withReadyReadLock(func() error {
		expeditionRecord, getError := expedition.Get(app.db, expeditionID)
		if getError != nil {
			return getError
		}
		adjustments := expeditionRecord.Adjustments
		adjustments.CarrierJumps++
		if distanceLy > 0 {
			adjustments.CarrierDistanceLy += distanceLy
		}
		if tritium > 0 {
			currentTritium := 0
			if adjustments.TritiumUsed != nil {
				currentTritium = *adjustments.TritiumUsed
			}
			nextTritium := currentTritium + tritium
			adjustments.TritiumUsed = &nextTritium
		}
		return expedition.SaveAdjustments(app.db, expeditionID, adjustments)
	}); err != nil {
		return err
	}
	app.publishApp("adjustments", expeditionID, events.AppAdjustmentsChanged)
	return nil
}

// PlotCarrierRoute queries the Spansh Fleet Carrier router for a path from source to destination,
// falling back cleanly to manual mode on unknown systems, offline mode, or API timeouts.
func (app *App) PlotCarrierRoute(source, destination string, capacityUsed int, carrierType string, depotTritium int) (CarrierRouteResult, error) {
	var (
		canonicalSource string
		canonicalDest   string
		resolvedDepot   int
		parentCtx       context.Context
	)
	app.dbMu.RLock()
	app.waitStartupLocked()
	canonicalSource = edsm.CanonicalSystemName(app.db, source)
	canonicalDest = edsm.CanonicalSystemName(app.db, destination)
	if depotTritium <= 0 && app.carrierState != nil {
		depotInfo, _ := app.carrierState.DepotInfo()
		resolvedDepot = depotInfo.DepotTritium
	} else {
		resolvedDepot = depotTritium
	}
	parentCtx = app.ctx
	app.dbMu.RUnlock()

	return expedition.PlotCarrierRoute(parentCtx, nil, canonicalSource, canonicalDest, capacityUsed, carrierType, resolvedDepot, nil)
}

