package app

import (
	"database/sql"
	"log"

	"planisphere/internal/store/expedition"
)

// UndoActionResult reports the outcome of a session-scoped expedition undo or redo request.
type UndoActionResult struct {
	Applied              bool   `json:"applied"`
	Label                string `json:"label"`
	CarrierRouteRestored bool   `json:"carrierRouteRestored"`
	CanUndo              bool   `json:"canUndo"`
	CanRedo              bool   `json:"canRedo"`
}

func (app *App) ensureUndoHistory() *expedition.UndoHistory {
	app.dbMu.RLock()
	history := app.undoHistory
	app.dbMu.RUnlock()
	if history != nil {
		return history
	}
	app.dbMu.Lock()
	if app.undoHistory == nil {
		app.undoHistory = expedition.NewUndoHistory(expedition.DefaultMaxUndoEntries)
	}
	history = app.undoHistory
	app.dbMu.Unlock()
	return history
}

// clearUndoHistoryLocked initializes or clears session undo history while the
// caller holds app.dbMu.Lock (acquiring only UndoHistory's leaf lock).
func (app *App) clearUndoHistoryLocked() {
	if app.undoHistory == nil {
		app.undoHistory = expedition.NewUndoHistory(expedition.DefaultMaxUndoEntries)
		return
	}
	app.undoHistory.ClearAll()
}

func (app *App) clearUndoHistory() {
	app.dbMu.RLock()
	history := app.undoHistory
	app.dbMu.RUnlock()
	if history != nil {
		history.ClearAll()
	}
}

func (app *App) clearExpeditionUndoHistory(expeditionID int64) {
	app.dbMu.RLock()
	history := app.undoHistory
	app.dbMu.RUnlock()
	if history != nil {
		history.ClearExpedition(expeditionID)
	}
}

// runUndoExclusive serializes non-undoable DB mutations together with their
// undo-history invalidations under UndoHistory.opMu while keeping publishing
// and background notifications outside the lock.
func (app *App) runUndoExclusive(operation func() error) error {
	return app.ensureUndoHistory().RunExclusive(operation)
}

// runUndoableCapture enforces the acyclic opMu -> app.dbMu -> history.mu lock
// hierarchy and pushes a pre-mutation snapshot only when mutate succeeds,
// reports applied, and the active database handle still matches targetDB.
func (app *App) runUndoableCapture(
	prepare func(targetDB *sql.DB) (expeditionID int64, label string, before expedition.Snapshot, err error),
	mutate func(targetDB *sql.DB, before expedition.Snapshot) (bool, error),
) (bool, error) {
	history := app.ensureUndoHistory()
	var didApply bool
	runError := history.RunExclusive(func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		targetDB := app.db
		expeditionID, label, beforeSnapshot, prepareError := prepare(targetDB)
		if prepareError != nil {
			return prepareError
		}
		applied, mutateError := mutate(targetDB, beforeSnapshot)
		if mutateError != nil || !applied {
			return mutateError
		}
		didApply = true
		if app.db == targetDB && targetDB != nil {
			history.Push(expeditionID, expedition.UndoEntry{Label: label, Snapshot: beforeSnapshot})
		}
		return nil
	})
	return didApply, runError
}

func (app *App) recordUndoableMutation(expeditionID int64, label string, mutate func(targetDB *sql.DB) (bool, error)) (bool, error) {
	return app.runUndoableCapture(
		func(targetDB *sql.DB) (int64, string, expedition.Snapshot, error) {
			beforeSnapshot, captureError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return expeditionID, label, beforeSnapshot, captureError
		},
		func(targetDB *sql.DB, _ expedition.Snapshot) (bool, error) { return mutate(targetDB) },
	)
}

func (app *App) recordExpeditionMetaMutation(
	expeditionID int64,
	label string,
	mutate func(targetDB *sql.DB, before expedition.Expedition) (bool, error),
) (bool, error) {
	return app.runUndoableCapture(
		func(targetDB *sql.DB) (int64, string, expedition.Snapshot, error) {
			beforeSnapshot, captureError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return expeditionID, label, beforeSnapshot, captureError
		},
		func(targetDB *sql.DB, beforeSnapshot expedition.Snapshot) (bool, error) {
			return mutate(targetDB, beforeSnapshot.Expedition)
		},
	)
}

func (app *App) recordRowUndoableMutation(
	rowID int64,
	labelForRow func(expedition.Row) string,
	mutate func(targetDB *sql.DB, initialRow expedition.Row) (bool, error),
) (bool, error) {
	var initialRow expedition.Row
	return app.runUndoableCapture(
		func(targetDB *sql.DB) (int64, string, expedition.Snapshot, error) {
			var rowError error
			if initialRow, rowError = expedition.GetRow(targetDB, rowID); rowError != nil {
				return 0, "", expedition.Snapshot{}, rowError
			}
			beforeSnapshot, captureError := expedition.CaptureSnapshot(targetDB, initialRow.ExpeditionID)
			label := ""
			if labelForRow != nil {
				label = labelForRow(initialRow)
			}
			return initialRow.ExpeditionID, label, beforeSnapshot, captureError
		},
		func(targetDB *sql.DB, _ expedition.Snapshot) (bool, error) { return mutate(targetDB, initialRow) },
	)
}

func (app *App) recordUndoableSnapshotDiff(expeditionID int64, label string, mutate func(targetDB *sql.DB) error) (bool, error) {
	return app.runUndoableCapture(
		func(targetDB *sql.DB) (int64, string, expedition.Snapshot, error) {
			beforeSnapshot, captureError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return expeditionID, label, beforeSnapshot, captureError
		},
		func(targetDB *sql.DB, beforeSnapshot expedition.Snapshot) (bool, error) {
			if mutateError := mutate(targetDB); mutateError != nil {
				return false, mutateError
			}
			afterSnapshot, afterError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return !beforeSnapshot.Equal(afterSnapshot), afterError
		},
	)
}

func (app *App) recordRowSnapshotDiff(rowID int64, label string, mutate func(targetDB *sql.DB) (int64, error)) (int64, bool, error) {
	var expeditionID int64
	applied, err := app.runUndoableCapture(
		func(targetDB *sql.DB) (int64, string, expedition.Snapshot, error) {
			initialRow, rowError := expedition.GetRow(targetDB, rowID)
			if rowError != nil {
				return 0, "", expedition.Snapshot{}, rowError
			}
			expeditionID = initialRow.ExpeditionID
			beforeSnapshot, captureError := expedition.CaptureSnapshot(targetDB, initialRow.ExpeditionID)
			return initialRow.ExpeditionID, label, beforeSnapshot, captureError
		},
		func(targetDB *sql.DB, beforeSnapshot expedition.Snapshot) (bool, error) {
			var mutateError error
			if expeditionID, mutateError = mutate(targetDB); mutateError != nil {
				return false, mutateError
			}
			afterSnapshot, afterError := expedition.CaptureSnapshot(targetDB, expeditionID)
			return !beforeSnapshot.Equal(afterSnapshot), afterError
		},
	)
	return expeditionID, applied, err
}

func (app *App) runExpeditionUndoStep(expeditionID int64, isRedo bool) (UndoActionResult, error) {
	history := app.ensureUndoHistory()
	var targetDB *sql.DB
	var stepOutcome expedition.UndoStepOutcome
	stepError := history.RunExclusive(func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		targetDB = app.db
		var applyError error
		if isRedo {
			stepOutcome, applyError = history.RedoLocked(targetDB, expeditionID)
		} else {
			stepOutcome, applyError = history.UndoLocked(targetDB, expeditionID)
		}
		return applyError
	})
	if stepError != nil {
		return UndoActionResult{}, stepError
	}
	actionResult := UndoActionResult{
		Applied:              stepOutcome.Applied,
		Label:                stepOutcome.Label,
		CarrierRouteRestored: stepOutcome.CarrierRouteRestored,
		CanUndo:              stepOutcome.CanUndo,
		CanRedo:              stepOutcome.CanRedo,
	}
	if !stepOutcome.Applied {
		return actionResult, nil
	}
	if app.isCurrentDB(targetDB) {
		app.clearExportSnapshot()
		if stepOutcome.NeedsCoordResolution {
			app.kickResolutionForDB(targetDB, expeditionID, false)
		}
	}
	app.publishAppSetForDB(targetDB, expeditionID, stepOutcome.ChangedEvents)
	return actionResult, nil
}

// UndoExpedition reverts the most recent undoable expedition mutation in the current session.
func (app *App) UndoExpedition(expeditionID int64) (UndoActionResult, error) {
	return app.runExpeditionUndoStep(expeditionID, false)
}

// RedoExpedition reapplies the most recently undone expedition mutation in the current session.
func (app *App) RedoExpedition(expeditionID int64) (UndoActionResult, error) {
	return app.runExpeditionUndoStep(expeditionID, true)
}

func reevaluateTxLogged(targetDB *sql.DB, expeditionID int64) bool {
	changed, reevalError := expedition.ReevaluateTx(targetDB, expeditionID)
	if reevalError != nil {
		log.Printf("reevaluate expedition %d: %v", expeditionID, reevalError)
		return false
	}
	return changed
}

func rowActionLabel(verb, rowType string) string {
	if rowType == expedition.RowRegion {
		return verb + " Region Header"
	}
	return verb + " Waypoint"
}

func undoLabelForRowPatch(patch expedition.RowPatch, initialRow expedition.Row) string {
	switch {
	case patch.SystemName != nil:
		return "Edit System"
	case patch.POIName != nil:
		return "Edit POI"
	case patch.Notes != nil:
		return "Edit Notes"
	case patch.FCStop != nil:
		return "Toggle Carrier Stop"
	case patch.FCArrived != nil:
		return "Toggle Carrier Arrival"
	case patch.RegionID != nil:
		return "Change Region"
	case patch.LegModeID != nil:
		return "Change Leg Mode"
	default:
		return rowActionLabel("Edit", initialRow.RowType)
	}
}
