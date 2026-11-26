package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

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

// CarrierRoutePromptDefaults holds pre-resolved default form values for
// initiating a Spansh or manual Fleet Carrier route.
type CarrierRoutePromptDefaults = expedition.CarrierRoutePromptDefaults

var querySpanshCarrierRoute = spansh.QueryCarrierRoute

func (app *App) beginCarrierRoutePlot(parentCtx context.Context) (context.Context, uint64, context.CancelFunc) {
	cancelCtx, cancelPlot := context.WithCancel(parentCtx)
	app.carrierPlotMu.Lock()
	previousCancel := app.carrierPlotCancel
	app.carrierPlotSeq++
	plotSeq := app.carrierPlotSeq
	app.carrierPlotCancel = cancelPlot
	app.carrierPlotMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	return cancelCtx, plotSeq, cancelPlot
}

func (app *App) finishCarrierRoutePlot(plotSeq uint64, cancelPlot context.CancelFunc) {
	app.carrierPlotMu.Lock()
	if app.carrierPlotSeq == plotSeq {
		app.carrierPlotCancel = nil
	}
	app.carrierPlotMu.Unlock()
	cancelPlot()
}

func (app *App) isCurrentCarrierRoutePlot(plotSeq uint64) bool {
	app.carrierPlotMu.Lock()
	defer app.carrierPlotMu.Unlock()
	return app.carrierPlotSeq == plotSeq
}

// CancelCarrierRoutePlot aborts any in-flight Spansh Fleet Carrier route plot request.
func (app *App) CancelCarrierRoutePlot() {
	app.carrierPlotMu.Lock()
	previousCancel := app.carrierPlotCancel
	app.carrierPlotCancel = nil
	app.carrierPlotSeq++
	app.carrierPlotMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
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

func (app *App) prepareCarrierRoutePromptLocked(targetDB *sql.DB, expeditionID, targetRowID int64) (CarrierRoutePromptDefaults, error) {
	expeditionRecord, getError := expedition.Get(targetDB, expeditionID)
	if getError != nil {
		return CarrierRoutePromptDefaults{}, getError
	}
	rows, rowsError := expedition.ListRows(targetDB, expeditionID)
	if rowsError != nil {
		return CarrierRoutePromptDefaults{}, rowsError
	}
	selection, selectionError := expedition.ResolveCarrierRoutePromptSelection(expeditionRecord, rows, targetRowID)
	if selectionError != nil {
		return CarrierRoutePromptDefaults{}, selectionError
	}

	carrierType := "player"
	if expeditionRecord.CarrierType == "squadron" {
		carrierType = "squadron"
	}

	capacityUsed := 5000
	depotTritium := reference.CarrierFuelTankTonnes
	var lastCarrierLocation string
	if app.carrierState != nil {
		if snapshot, snapshotError := app.carrierState.LatestStats(); snapshotError == nil {
			if location, locationError := replay.QueryCarrierLastLocation(targetDB, snapshot); locationError == nil {
				lastCarrierLocation = location
			}
			if depotInfo, depotError := replay.QueryCarrierDepotInfo(targetDB, snapshot); depotError == nil {
				if depotInfo.UsedCapacity > 0 {
					capacityUsed = depotInfo.UsedCapacity
				}
				if depotInfo.DepotTritium > 0 {
					depotTritium = depotInfo.DepotTritium
				}
			}
		}
	}

	origin := expedition.ResolveCarrierStartSystem(rows, selection.UpToIndex, lastCarrierLocation, "")
	if origin == "" {
		playerPosition, playerSystem, playerLocationError := replay.QueryLatestPlayerLocationScoped(targetDB, expeditionRecord.ProfileScope())
		if playerLocationError != nil && !errors.Is(playerLocationError, sql.ErrNoRows) {
			return CarrierRoutePromptDefaults{}, playerLocationError
		}
		if playerPosition == nil && selection.FallbackVisitedSystem != "" {
			playerSystem = selection.FallbackVisitedSystem
		}
		origin = strings.TrimSpace(playerSystem)
	}

	return CarrierRoutePromptDefaults{
		TargetRowID:  selection.TargetRowID,
		Origin:       origin,
		Destination:  selection.Destination,
		CarrierType:  carrierType,
		CapacityUsed: capacityUsed,
		DepotTritium: depotTritium,
	}, nil
}

// PrepareCarrierRoutePrompt resolves default origin, destination, carrier type,
// capacity, and depot tritium values for launching a Spansh or manual carrier route.
func (app *App) PrepareCarrierRoutePrompt(expeditionID, targetRowID int64) (CarrierRoutePromptDefaults, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return CarrierRoutePromptDefaults{}, readyError
	}
	return app.prepareCarrierRoutePromptLocked(app.db, expeditionID, targetRowID)
}

func normalizeCarrierPlotParameters(carrierType string, capacityUsed int, depotTritium int) (string, int, int) {
	if carrierType == "" {
		carrierType = "player"
	}
	maxCapacity := reference.PersonalCarrierMaxCapacity
	if carrierType == "squadron" {
		maxCapacity = reference.SquadronCarrierMaxCapacity
	}
	capacityUsed = max(0, min(maxCapacity, capacityUsed))
	if depotTritium <= 0 || depotTritium > reference.CarrierFuelTankTonnes {
		depotTritium = reference.CarrierFuelTankTonnes
	}
	return carrierType, capacityUsed, depotTritium
}

func (app *App) resolveCarrierPlotInputsLocked(source, destination string, depotTritium int) (string, string, int, context.Context) {
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

func executeCarrierRoutePlot(parentCtx context.Context, canonicalSource, canonicalDest string, capacityUsed int, carrierType string, resolvedDepot int) CarrierRouteResult {
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
		return routeResult
	}
	if strings.EqualFold(canonicalSource, canonicalDest) {
		routeResult.ManualMode = true
		routeResult.Error = "Origin and destination are the same system — switched to Manual Mode."
		return routeResult
	}

	requestCtx, cancelTimeout := context.WithTimeout(parentCtx, 60*time.Second)
	defer cancelTimeout()

	spanshJumps, queryError := querySpanshCarrierRoute(requestCtx, canonicalSource, canonicalDest, capacityUsed, resolvedDepot)
	if queryError != nil || len(spanshJumps) == 0 {
		routeResult.ManualMode = true
		if queryError != nil {
			routeResult.Error = queryError.Error()
		} else {
			routeResult.Error = "Spansh returned no jumps for this route — switched to Manual Mode."
		}
		return routeResult
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
	return routeResult
}

// PlotCarrierRoute queries the Spansh Fleet Carrier router for a path from source to destination,
// falling back cleanly to manual mode on unknown systems, offline mode, or API timeouts.
func (app *App) PlotCarrierRoute(source, destination string, capacityUsed int, carrierType string, depotTritium int) (CarrierRouteResult, error) {
	app.dbMu.RLock()
	app.waitStartupLocked()
	canonicalSource, canonicalDest, resolvedDepot, parentCtx := app.resolveCarrierPlotInputsLocked(source, destination, depotTritium)
	app.dbMu.RUnlock()
	return executeCarrierRoutePlot(parentCtx, canonicalSource, canonicalDest, capacityUsed, carrierType, resolvedDepot), nil
}
