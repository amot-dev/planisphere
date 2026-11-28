package app

import (
	"database/sql"
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

// ListExpeditions returns all expeditions with planning expeditions first
// (most recently created first), followed by active and completed expeditions
// ordered by start timestamp (most recent first).
func (app *App) ListExpeditions() ([]expedition.Expedition, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	expeditionsList, listError := expedition.List(app.db)
	if expeditionsList == nil {
		expeditionsList = []expedition.Expedition{}
	}
	return expeditionsList, listError
}

// CreateExpedition adds an expedition and makes it the active one.
func (app *App) CreateExpedition(name, description string) (expedition.Expedition, error) {
	var createdExpedition expedition.Expedition
	if err := app.withReadyReadLock(func() error {
		var createError error
		createdExpedition, createError = expedition.Create(app.db, name, description)
		if createError != nil {
			return createError
		}
		return settings.SetActiveExpeditionID(app.db, createdExpedition.ID)
	}); err != nil {
		return createdExpedition, err
	}
	app.publishApp(createdExpedition.ID, events.AppExpeditionPlanChanged)
	return createdExpedition, nil
}

// CloneExpedition duplicates an expedition (and its waypoints and region headers)
// into a new expedition in the planning state, and makes the clone active.
func (app *App) CloneExpedition(id int64) (expedition.Expedition, error) {
	var clonedExpedition expedition.Expedition
	if err := app.withReadyReadLock(func() error {
		var cloneError error
		clonedExpedition, cloneError = expedition.Clone(app.db, id)
		if cloneError != nil {
			return cloneError
		}
		return settings.SetActiveExpeditionID(app.db, clonedExpedition.ID)
	}); err != nil {
		return clonedExpedition, err
	}
	app.kickResolution(clonedExpedition.ID, false)
	app.publishApp(clonedExpedition.ID, events.AppExpeditionPlanChanged)
	return clonedExpedition, nil
}

// UpdateExpedition renames an expedition.
func (app *App) UpdateExpedition(id int64, name, description string) error {
	if updateError := app.withReadyReadLock(func() error {
		return expedition.UpdateMeta(app.db, id, name, description)
	}); updateError != nil {
		return updateError
	}
	app.publishApp(id, events.AppExpeditionPlanChanged)
	return nil
}

// UpdateExpeditionCarrierConfig updates the expedition's Fleet Carrier vessel
// classification ("player" or "squadron") and weekly upkeep cost in credits.
func (app *App) UpdateExpeditionCarrierConfig(id int64, carrierType string, weeklyUpkeep int64) error {
	applied, updateError := app.recordExpeditionMetaMutation(id, "Update Carrier Config", func(targetDB *sql.DB, before expedition.Expedition) (bool, error) {
		if execError := expedition.UpdateCarrierConfig(targetDB, id, carrierType, weeklyUpkeep); execError != nil {
			return false, execError
		}
		after, getError := expedition.Get(targetDB, id)
		if getError != nil {
			return false, getError
		}
		return before.CarrierType != after.CarrierType || before.CarrierWeeklyUpkeep != after.CarrierWeeklyUpkeep, nil
	})
	if updateError != nil || !applied {
		return updateError
	}
	app.publishApp(id, events.AppExpeditionPlanChanged, events.AppAdjustmentsChanged)
	return nil
}

// DeleteExpedition deletes an expedition plan (journal history is kept).
func (app *App) DeleteExpedition(id int64) error {
	wasActive := false
	nextActiveID := int64(0)
	if deleteError := app.runUndoExclusive(func() error {
		if err := app.withReadyReadLock(func() error {
			wasActive = settings.ActiveExpeditionID(app.db) == id
			if expeditionDeleteError := expedition.Delete(app.db, id); expeditionDeleteError != nil {
				return expeditionDeleteError
			}
			if !wasActive {
				return nil
			}
			remainingExpeditions, listError := expedition.List(app.db)
			if listError != nil {
				return listError
			}
			if len(remainingExpeditions) > 0 {
				nextActiveID = remainingExpeditions[0].ID
			}
			return settings.SetActiveExpeditionID(app.db, nextActiveID)
		}); err != nil {
			return err
		}
		app.clearExpeditionUndoHistory(id)
		return nil
	}); deleteError != nil {
		return deleteError
	}
	if !wasActive {
		app.emitDataChanged(id, events.NewUITargetSet(events.UITargetExpeditions))
		return nil
	}
	if nextActiveID > 0 {
		app.kickResolution(nextActiveID, false)
	}
	if bus := app.currentBus(); bus != nil {
		bus.PublishApp(events.AppSettingsChanged)
	}
	app.emitDataChanged(0, events.NewUITargetSet(events.UITargetSettings, events.UITargetExpeditions))
	return nil
}

// SetActiveExpedition selects the expedition shown in the planner.
func (app *App) SetActiveExpedition(id int64) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return settings.SetActiveExpeditionID(app.db, id)
	}(); err != nil {
		return err
	}
	app.CancelCarrierRoutePlot()
	app.kickResolution(id, false)
	return nil
}

// GetPlan returns the planner sheet for an expedition.
func (app *App) GetPlan(id int64) (expedition.PlanView, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.PlanView{}, readyError
	}
	return expedition.BuildPlan(app.db, id, app.gecLookup, app.expeditionProjections())
}

// GetRowDropPreviews computes valid historical and unvisited drop-slot previews
// for reordering rowID within an active expedition.
func (app *App) GetRowDropPreviews(rowID int64) ([]expedition.DropSlotPreview, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	previews, previewError := expedition.ComputeRowDropPreviews(app.db, rowID, app.expeditionProjections())
	if previews == nil {
		previews = []expedition.DropSlotPreview{}
	}
	return previews, previewError
}

// gecLookup searches the GEC catalog for POIs matching a system. Caller must hold app.dbMu.RLock.
func (app *App) gecLookup(name string, address int64) *expedition.GECRef {
	if app.gec == nil {
		return nil
	}
	matchedPOIs := app.gec.Lookup(name, address)
	if len(matchedPOIs) == 0 {
		return nil
	}
	return &expedition.GECRef{
		ID:    matchedPOIs[0].ID,
		Name:  matchedPOIs[0].Name,
		Type:  matchedPOIs[0].Type,
		URL:   matchedPOIs[0].URL,
		Count: len(matchedPOIs),
	}
}

// AddRow inserts a row after afterRowID (0 appends at end, -1 inserts at top).
// rowType is "waypoint" or "region"; regionID is used for region headers.
func (app *App) AddRow(expeditionID, afterRowID int64, rowType string, regionID int) (expedition.Row, error) {
	var newRow expedition.Row
	applied, err := app.recordUndoableMutation(expeditionID, rowActionLabel("Add", rowType), func(targetDB *sql.DB) (bool, error) {
		var insertError error
		newRow, insertError = expedition.InsertRowTx(targetDB, expeditionID, afterRowID, rowType, regionID)
		return insertError == nil, insertError
	})
	if err != nil || !applied {
		return newRow, err
	}
	app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	return newRow, nil
}

// UpdateRow applies a partial update to a row. Changing the system name
// triggers asynchronous coordinate resolution and visit re-evaluation.
func (app *App) UpdateRow(patch expedition.RowPatch) error {
	var systemChanged, reevalChanged bool
	var rowItem expedition.Row
	var targetDB *sql.DB
	applied, err := app.recordRowUndoableMutation(patch.ID, func(initialRow expedition.Row) string { return undoLabelForRowPatch(patch, initialRow) }, func(activeDB *sql.DB, initialRow expedition.Row) (bool, error) {
		targetDB = activeDB
		var updateError error
		if systemChanged, updateError = expedition.UpdateRowTx(activeDB, patch); updateError != nil {
			return false, updateError
		}
		var getError error
		if rowItem, getError = expedition.GetRow(activeDB, patch.ID); getError != nil {
			return false, getError
		}
		if systemChanged && rowItem.SystemName != "" && strings.TrimSpace(rowItem.POIName) == "" {
			if poiName := app.applyMissingPOIFromGEC(rowItem.ID, rowItem.SystemName, rowItem.SystemAddress); poiName != "" {
				rowItem.POIName = poiName
			}
		}
		if systemChanged {
			reevalChanged = reevaluateTxLogged(activeDB, rowItem.ExpeditionID)
		}
		return rowItem != initialRow || systemChanged || reevalChanged, nil
	})
	if err != nil || !applied {
		return err
	}
	if systemChanged && rowItem.SystemName != "" {
		app.resolveRowAsync(targetDB, rowItem, true)
	}
	if systemChanged && reevalChanged {
		app.publishAppForDB(targetDB, rowItem.ExpeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionModesChanged)
	} else {
		app.publishAppForDB(targetDB, rowItem.ExpeditionID, events.AppExpeditionPlanChanged)
	}
	return nil
}

// SetExpeditionFlightMode updates the per-expedition active flight mode for a
// planning or active expedition.
func (app *App) SetExpeditionFlightMode(expeditionID, modeID int64) error {
	applied, setError := app.recordExpeditionMetaMutation(expeditionID, "Change Flight Mode", func(targetDB *sql.DB, before expedition.Expedition) (bool, error) {
		if execError := expedition.SetActiveModeTx(targetDB, expeditionID, modeID); execError != nil {
			return false, execError
		}
		return before.ActiveModeID != modeID, nil
	})
	if setError != nil || !applied {
		return setError
	}
	app.publishApp(expeditionID, events.AppExpeditionModesChanged)
	return nil
}

// SetLegMode assigns a mode to the leg arriving at rowID. When cascade is true
// on a visited leg, it also overwrites all subsequent visited legs without
// changing the expedition's active flight mode.
func (app *App) SetLegMode(rowID, modeID int64, cascade bool) error {
	expeditionID, applied, err := app.recordRowSnapshotDiff(rowID, "Change Leg Mode", func(targetDB *sql.DB) (int64, error) {
		return expedition.SetLegModeTx(targetDB, rowID, modeID, cascade)
	})
	if err != nil || !applied {
		return err
	}
	app.publishApp(expeditionID, events.AppExpeditionModesChanged)
	return nil
}

// OverrideCarrierStopArrival manually overrides the Fleet Carrier arrival status
// on rowID and cascades the status across preceding or subsequent carrier stops
// to preserve the arrived-before-pending order invariant.
func (app *App) OverrideCarrierStopArrival(rowID int64, arrived bool) error {
	expeditionID, applied, err := app.recordRowSnapshotDiff(rowID, "Override Carrier Arrival", func(targetDB *sql.DB) (int64, error) {
		return expedition.SetCarrierArrivalOverrideTx(targetDB, rowID, arrived)
	})
	if err != nil || !applied {
		return err
	}
	app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	return nil
}

// DeleteRow removes a row.
func (app *App) DeleteRow(rowID int64) error {
	var expeditionID int64
	var deletedVisited, reevalChanged bool
	applied, err := app.recordRowUndoableMutation(rowID, func(initialRow expedition.Row) string { return rowActionLabel("Delete", initialRow.RowType) }, func(targetDB *sql.DB, initialRow expedition.Row) (bool, error) {
		deletedVisited = initialRow.IsWaypoint() && initialRow.VisitedAt != ""
		var deleteRowError error
		if expeditionID, deleteRowError = expedition.DeleteRowTx(targetDB, rowID); deleteRowError != nil {
			return false, deleteRowError
		}
		reevalChanged = reevaluateTxLogged(targetDB, expeditionID)
		return true, nil
	})
	if err != nil || !applied {
		return err
	}
	if deletedVisited || reevalChanged {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionModesChanged)
	} else {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return nil
}

// MoveRow moves a row to the zero-based index toIndex.
func (app *App) MoveRow(rowID int64, toIndex int) error {
	var expeditionID int64
	var isWaypoint, reevalChanged bool
	applied, err := app.recordRowUndoableMutation(rowID, func(initialRow expedition.Row) string { return rowActionLabel("Move", initialRow.RowType) }, func(targetDB *sql.DB, initialRow expedition.Row) (bool, error) {
		isWaypoint = initialRow.RowType == expedition.RowWaypoint
		var moveRowError error
		if expeditionID, moveRowError = expedition.MoveRowTx(targetDB, rowID, toIndex); moveRowError != nil {
			return false, moveRowError
		}
		movedRow, getError := expedition.GetRow(targetDB, rowID)
		if getError != nil {
			return false, getError
		}
		moved := movedRow.Position != initialRow.Position || movedRow.FCArrived != initialRow.FCArrived
		if isWaypoint && moved {
			reevalChanged = reevaluateTxLogged(targetDB, expeditionID)
		}
		return moved, nil
	})
	if err != nil || !applied {
		return err
	}
	if isWaypoint && reevalChanged {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionModesChanged)
	} else {
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
	}
	return nil
}

// RegionOptions is the prioritised region dropdown content.
type RegionOptions struct {
	AnchorID          int                      `json:"anchorId"`
	AnchorName        string                   `json:"anchorName"`
	AnchorIsDefault   bool                     `json:"anchorIsDefault"`
	SuggestedRegionID int                      `json:"suggestedRegionId"` // region of the next waypoint's coordinates
	Options           []reference.RegionOption `json:"options"`
}

// GetRegionOptions returns regions ordered for a Region Header dropdown.
// rowID is either the header being edited (editing = true) or the row after
// which a new header will be inserted (0 appends at end, -1 inserts at top).
func (app *App) GetRegionOptions(expeditionID, rowID int64, editing bool) (RegionOptions, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return RegionOptions{}, readyError
	}
	rows, listRowsError := expedition.ListRows(app.db, expeditionID)
	if listRowsError != nil {
		return RegionOptions{}, listRowsError
	}
	// cutoffIndex is the index of the first row after the header position.
	cutoffIndex := len(rows)
	switch {
	case rowID == expedition.InsertAtTop:
		cutoffIndex = 0
	case rowID > 0:
		for rowIndex, rowItem := range rows {
			if rowItem.ID == rowID {
				if editing {
					cutoffIndex = rowIndex
				} else {
					cutoffIndex = rowIndex + 1
				}
				break
			}
		}
	}
	anchor := 0
	for rowIndex := 0; rowIndex < cutoffIndex && rowIndex < len(rows); rowIndex++ {
		if rows[rowIndex].RowType == expedition.RowRegion && rows[rowIndex].RegionID > 0 {
			anchor = rows[rowIndex].RegionID
		}
	}
	regionOptionsResult := RegionOptions{AnchorIsDefault: anchor == 0}
	if anchor == 0 {
		anchor = reference.InnerOrionSpurID
	}
	regionRecord, _ := reference.RegionByID(anchor)
	regionOptionsResult.AnchorID, regionOptionsResult.AnchorName = anchor, regionRecord.Name
	regionOptionsResult.Options = reference.PrioritizedRegions(anchor)
	// Inserting before cutoffIndex checks the row currently at cutoffIndex as the first
	// downstream waypoint; editing an existing region header at cutoffIndex skips that header.
	startIndex := cutoffIndex
	if editing {
		startIndex = cutoffIndex + 1
	}
	for rowIndex := startIndex; rowIndex < len(rows); rowIndex++ {
		if rows[rowIndex].IsWaypoint() && rows[rowIndex].HasCoords {
			if foundRegion, ok := reference.RegionAt(rows[rowIndex].X, rows[rowIndex].Y, rows[rowIndex].Z); ok {
				regionOptionsResult.SuggestedRegionID = foundRegion.ID
			}
			break
		}
	}
	return regionOptionsResult, nil
}

// StartCandidates lists recorded visits to Waypoint #1.
type StartCandidates struct {
	SystemName string               `json:"systemName"`
	Visits     []expedition.Arrival `json:"visits"`
}

// GetStartCandidates returns every recorded arrival at Waypoint #1 (newest first).
func (app *App) GetStartCandidates(expeditionID int64) (StartCandidates, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return StartCandidates{}, readyError
	}
	systemName, visits, startCandidatesError := expedition.StartCandidates(app.db, expeditionID)
	if visits == nil {
		visits = []expedition.Arrival{}
	}
	return StartCandidates{SystemName: systemName, Visits: visits}, startCandidatesError
}

func (app *App) lifecycle(expeditionID int64, label string, action func(targetDB *sql.DB) error) error {
	applied, err := app.recordUndoableMutation(expeditionID, label, func(targetDB *sql.DB) (bool, error) {
		err := action(targetDB)
		return err == nil, err
	})
	if err != nil || !applied {
		return err
	}
	app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionBoundsChanged, events.AppExpeditionModesChanged)
	return nil
}

// StartExpedition starts the expedition at the given visit timestamp
// (ISO-8601 UTC), applies initialModeID when positive, and retroactively
// replays history from that moment.
func (app *App) StartExpedition(expeditionID int64, timestamp string, initialModeID int64) error {
	return app.lifecycle(expeditionID, "Engage Expedition", func(targetDB *sql.DB) error {
		return expedition.StartTx(targetDB, expeditionID, timestamp, initialModeID)
	})
}

// CompleteExpedition locks end_timestamp to the final visited waypoint's last
// non-multicrew in-system exploration activity (Scan, SAAScanComplete, or
// ScanOrganic Analyse) for the target system and stay commander before
// departure, falling back to the waypoint's arrival timestamp only when no
// in-system exploration occurred.
func (app *App) CompleteExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, "Complete Expedition", func(targetDB *sql.DB) error { return expedition.CompleteTx(targetDB, expeditionID) })
}

// ReopenExpedition makes a completed expedition active again.
func (app *App) ReopenExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, "Reopen Expedition", func(targetDB *sql.DB) error { return expedition.ReopenTx(targetDB, expeditionID) })
}

// GetWaypointDetails returns the expandable drawer content for a waypoint.
func (app *App) GetWaypointDetails(rowID int64) (expedition.WaypointDetails, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.WaypointDetails{}, readyError
	}
	return expedition.Details(app.db, rowID, app.expeditionProjections())
}
