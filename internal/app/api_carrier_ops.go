package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/store/expedition"
)

// StartCarrierRoute plots a Spansh Fleet Carrier route (or falls back to manual
// mode on non-cancellation errors) outside the undo lock, then atomically persists
// the resulting active carrier route state under session undo unless cancelled.
func (app *App) StartCarrierRoute(expeditionID, targetRowID int64, source, destination string, capacityUsed int, carrierType string, depotTritium int) (*ActiveCarrierRouteState, error) {
	if expeditionID <= 0 {
		return nil, fmt.Errorf("invalid expedition ID: %d", expeditionID)
	}
	var (
		initialDB       *sql.DB
		canonicalSource string
		canonicalDest   string
		resolvedDepot   int
		cancelCtx       context.Context
		plotSeq         uint64
		cancelPlot      context.CancelFunc
	)
	if preflightError := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		expeditionRecord, expError := expedition.Get(app.db, expeditionID)
		if expError != nil {
			return expError
		}
		if expeditionRecord.Status == expedition.StatusCompleted {
			return errors.New("completed expedition cannot start a carrier route")
		}
		initialDB = app.db
		var parentCtx context.Context
		canonicalSource, canonicalDest, resolvedDepot, parentCtx = app.resolveCarrierPlotInputsLocked(source, destination, depotTritium)
		cancelCtx, plotSeq, cancelPlot = app.beginCarrierRoutePlot(parentCtx)
		return nil
	}(); preflightError != nil {
		return nil, preflightError
	}
	defer app.finishCarrierRoutePlot(plotSeq, cancelPlot)

	routeResult := executeCarrierRoutePlot(cancelCtx, canonicalSource, canonicalDest, capacityUsed, carrierType, resolvedDepot)
	if cancelCtx.Err() != nil || !app.isCurrentCarrierRoutePlot(plotSeq) || !app.isCurrentDB(initialDB) {
		return nil, nil
	}

	var savedState ActiveCarrierRouteState
	wasCancelled := false
	applied, saveError := app.recordUndoableMutation(expeditionID, "Start Carrier Route", func(targetDB *sql.DB) (bool, error) {
		if targetDB != initialDB || !app.isCurrentDB(targetDB) || cancelCtx.Err() != nil || !app.isCurrentCarrierRoutePlot(plotSeq) {
			wasCancelled = true
			return false, nil
		}
		currentExpedition, currentErr := expedition.Get(targetDB, expeditionID)
		if currentErr != nil {
			return false, currentErr
		}
		if currentExpedition.Status == expedition.StatusCompleted {
			return false, errors.New("completed expedition cannot start a carrier route")
		}
		savedState = ActiveCarrierRouteState{
			ExpeditionID: expeditionID,
			TargetRowID:  targetRowID,
			Route:        routeResult,
			JumpIndex:    0,
			ManualMode:   routeResult.ManualMode,
		}
		return expedition.SaveActiveCarrierRouteIfChanged(targetDB, savedState)
	})
	if saveError != nil {
		return nil, saveError
	}
	if wasCancelled {
		return nil, nil
	}
	if applied {
		app.publishAppForDB(initialDB, expeditionID, events.AppExpeditionPlanChanged)
	}
	return &savedState, nil
}

// OpenManualCarrierRoute cancels any in-flight Spansh plot, resolves default
// carrier origin, destination, and depot values, and opens the manual carrier
// router under session undo.
func (app *App) OpenManualCarrierRoute(expeditionID int64) (*ActiveCarrierRouteState, error) {
	app.CancelCarrierRoutePlot()
	var resultState *ActiveCarrierRouteState
	applied, saveError := app.recordUndoableMutation(expeditionID, "Open Manual Carrier Plotter", func(targetDB *sql.DB) (bool, error) {
		expeditionRecord, getError := expedition.Get(targetDB, expeditionID)
		if getError != nil {
			return false, getError
		}
		if expeditionRecord.Status == expedition.StatusCompleted {
			return false, errors.New("completed expedition cannot open manual carrier plotter")
		}
		existingRoute, routeError := expedition.GetActiveCarrierRoute(targetDB, expeditionID)
		if routeError != nil {
			return false, routeError
		}
		if normalizedExisting := expedition.NormalizeActiveCarrierRoute(existingRoute); normalizedExisting != nil {
			resultState = normalizedExisting
			return false, nil
		}
		defaults, defaultsError := app.prepareCarrierRoutePromptLocked(targetDB, expeditionID, 0)
		if defaultsError != nil {
			return false, defaultsError
		}
		savedState := ActiveCarrierRouteState{
			ExpeditionID: expeditionID,
			TargetRowID:  defaults.TargetRowID,
			ManualMode:   true,
			Route: CarrierRouteResult{
				Origin:       defaults.Origin,
				Destination:  defaults.Destination,
				CarrierType:  defaults.CarrierType,
				CapacityUsed: defaults.CapacityUsed,
				DepotTritium: defaults.DepotTritium,
				ManualMode:   true,
				Jumps:        []CarrierRouteJump{},
			},
		}
		resultState = &savedState
		return expedition.SaveActiveCarrierRouteIfChanged(targetDB, savedState)
	})
	if saveError != nil {
		return nil, saveError
	}
	if applied {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return resultState, nil
}

func (app *App) mutateActiveCarrierRoute(
	expeditionID int64,
	undoLabel string,
	transform func(ActiveCarrierRouteState) (ActiveCarrierRouteState, bool),
) (*ActiveCarrierRouteState, error) {
	var resultState *ActiveCarrierRouteState
	applied, saveError := app.recordUndoableMutation(expeditionID, undoLabel, func(targetDB *sql.DB) (bool, error) {
		rawRoute, getError := expedition.GetActiveCarrierRoute(targetDB, expeditionID)
		if getError != nil {
			return false, getError
		}
		normalizedRoute := expedition.NormalizeActiveCarrierRoute(rawRoute)
		if normalizedRoute == nil {
			return false, nil
		}
		updatedState, changed := transform(*normalizedRoute)
		updatedState.ExpeditionID = expeditionID
		resultState = &updatedState
		if !changed {
			return false, nil
		}
		return expedition.SaveActiveCarrierRouteIfChanged(targetDB, updatedState)
	})
	if saveError != nil {
		return nil, saveError
	}
	if applied {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return resultState, nil
}

// RefillCarrierDepot transfers tritium into the active route's carrier fuel
// depot, recalculates remaining jump fuel burn, and records an undo entry.
func (app *App) RefillCarrierDepot(expeditionID int64) (*ActiveCarrierRouteState, error) {
	return app.mutateActiveCarrierRoute(expeditionID, "Refill Carrier Depot", expedition.RefillCarrierDepotState)
}

// SwitchCarrierRouteToManual converts an active Spansh carrier route into manual
// jump-logging mode, seeding draft inputs from the current hop under session undo.
func (app *App) SwitchCarrierRouteToManual(expeditionID int64) (*ActiveCarrierRouteState, error) {
	return app.mutateActiveCarrierRoute(expeditionID, "Switch Carrier Route to Manual Mode", func(state ActiveCarrierRouteState) (ActiveCarrierRouteState, bool) {
		return expedition.SwitchRouteStateToManual(state), true
	})
}

func spanshJumpUndoLabel(beforeSnapshot expedition.Snapshot) string {
	var parsedState ActiveCarrierRouteState
	if beforeSnapshot.ActiveCarrierRouteJSON != "" && json.Unmarshal([]byte(beforeSnapshot.ActiveCarrierRouteJSON), &parsedState) == nil {
		if normalized := expedition.NormalizeActiveCarrierRoute(&parsedState); normalized != nil && normalized.JumpIndex+1 >= len(normalized.Route.Jumps) {
			return "Complete Carrier Arrival"
		}
	}
	return "Log Carrier Jump"
}

// CompleteSpanshCarrierJump advances an active Spansh carrier route by one hop
// (or completes carrier arrival on the final hop) under a single session undo entry.
func (app *App) CompleteSpanshCarrierJump(expeditionID int64) (*ActiveCarrierRouteState, error) {
	var (
		nextState    *ActiveCarrierRouteState
		recordedJump bool
	)
	applied, jumpError := app.runUndoableCapture(
		func(targetDB *sql.DB) (int64, string, expedition.Snapshot, error) {
			beforeSnapshot, captureError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return expeditionID, spanshJumpUndoLabel(beforeSnapshot), beforeSnapshot, captureError
		},
		func(targetDB *sql.DB, beforeSnapshot expedition.Snapshot) (bool, error) {
			var txApplied bool
			var txError error
			nextState, txApplied, recordedJump, txError = expedition.CompleteSpanshJumpTx(targetDB, expeditionID)
			if txError != nil || !txApplied {
				return false, txError
			}
			afterSnapshot, afterError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return !beforeSnapshot.Equal(afterSnapshot), afterError
		},
	)
	if jumpError != nil {
		return nil, jumpError
	}
	if !applied {
		return nextState, nil
	}
	if recordedJump {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppAdjustmentsChanged)
	} else {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return nextState, nil
}

// LogManualCarrierJump records a manual carrier jump and updates active manual
// leg counters atomically under session undo.
func (app *App) LogManualCarrierJump(expeditionID int64, distanceLy float64, tritiumUsed int) (*ActiveCarrierRouteState, error) {
	var nextState *ActiveCarrierRouteState
	applied, jumpError := app.recordUndoableMutation(expeditionID, "Log Carrier Jump", func(targetDB *sql.DB) (bool, error) {
		var didLog bool
		var txError error
		nextState, didLog, txError = expedition.LogManualCarrierJumpTx(targetDB, expeditionID, distanceLy, tritiumUsed)
		return didLog, txError
	})
	if jumpError != nil {
		return nil, jumpError
	}
	if applied {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppAdjustmentsChanged)
	}
	return nextState, nil
}

// SaveCarrierManualDraftInputs persists draft distance and tritium input text on
// an active carrier route without recording an undo entry.
func (app *App) SaveCarrierManualDraftInputs(expeditionID int64, distanceInput, tritiumInput string) error {
	return app.runUndoExclusive(func() error {
		return app.withReadyReadLock(func() error {
			return expedition.SaveCarrierManualDraftInputs(app.db, expeditionID, distanceInput, tritiumInput)
		})
	})
}

// ClearActiveCarrierRouteWithUndo deletes any active carrier route for expeditionID,
// records a session undo entry when a route existed, and refreshes the plan view.
func (app *App) ClearActiveCarrierRouteWithUndo(expeditionID int64) error {
	app.CancelCarrierRoutePlot()
	applied, clearError := app.recordUndoableMutation(expeditionID, "Clear Carrier Route", func(targetDB *sql.DB) (bool, error) {
		return expedition.ClearActiveCarrierRouteIfPresent(targetDB, expeditionID)
	})
	if clearError != nil || !applied {
		return clearError
	}
	app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	return nil
}

// CompleteCarrierRouteArrival atomically logs any final carrier jump, clears the
// active carrier route, and marks targetRowID (or the resolved arrival target row
// when targetRowID is non-positive) arrived under one undo entry.
func (app *App) CompleteCarrierRouteArrival(expeditionID int64, targetRowID int64, distanceLy float64, tritium int) error {
	app.CancelCarrierRoutePlot()
	applied, arrivalError := app.recordUndoableSnapshotDiff(expeditionID, "Complete Carrier Arrival", func(targetDB *sql.DB) error {
		return expedition.CompleteCarrierRouteArrivalTx(targetDB, expeditionID, targetRowID, distanceLy, tritium)
	})
	if arrivalError != nil || !applied {
		return arrivalError
	}
	if distanceLy > 0 || tritium > 0 {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppAdjustmentsChanged)
	} else {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return nil
}
