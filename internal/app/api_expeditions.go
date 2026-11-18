package app

import (
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

// ListExpeditions returns all expeditions (newest first).
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
		_ = settings.SetActiveExpeditionID(app.db, createdExpedition.ID)
		return nil
	}); err != nil {
		return createdExpedition, err
	}
	app.publishApp("plan", createdExpedition.ID, events.AppExpeditionPlanChanged)
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
	app.publishApp("plan", clonedExpedition.ID, events.AppExpeditionPlanChanged)
	return clonedExpedition, nil
}

// UpdateExpedition renames an expedition.
func (app *App) UpdateExpedition(id int64, name, description string) error {
	if updateError := app.withReadyReadLock(func() error {
		return expedition.UpdateMeta(app.db, id, name, description)
	}); updateError != nil {
		return updateError
	}
	app.publishApp("plan", id, events.AppExpeditionPlanChanged)
	return nil
}

// UpdateExpeditionCarrierConfig updates the expedition's Fleet Carrier vessel
// classification ("player" or "squadron") and weekly upkeep cost in credits.
func (app *App) UpdateExpeditionCarrierConfig(id int64, carrierType string, weeklyUpkeep int64) error {
	if updateError := app.withReadyReadLock(func() error {
		return expedition.UpdateCarrierConfig(app.db, id, carrierType, weeklyUpkeep)
	}); updateError != nil {
		return updateError
	}
	app.publishApp("plan", id, events.AppExpeditionPlanChanged, events.AppAdjustmentsChanged)
	return nil
}

// DeleteExpedition deletes an expedition plan (journal history is kept).
func (app *App) DeleteExpedition(id int64) error {
	wasActive := false
	nextActiveID := int64(0)
	if deleteError := app.withReadyReadLock(func() error {
		wasActive = app.activeExpeditionID() == id
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
		if setError := settings.SetActiveExpeditionID(app.db, nextActiveID); setError != nil {
			return setError
		}
		return nil
	}); deleteError != nil {
		return deleteError
	}
	if !wasActive {
		app.emitDataChanged("expeditions", id, events.NewUITargetSet(events.UITargetExpeditions))
		return nil
	}
	if nextActiveID > 0 {
		app.kickResolution(nextActiveID, false)
	}
	if bus := app.currentBus(); bus != nil {
		bus.PublishApp(events.AppSettingsChanged)
	}
	app.emitDataChanged("expeditions", 0, events.NewUITargetSet(events.UITargetSettings, events.UITargetExpeditions))
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
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var insertError error
		newRow, insertError = expedition.InsertRowTx(app.db, expeditionID, afterRowID, rowType, regionID)
		return insertError
	}(); err != nil {
		return newRow, err
	}
	app.publishApp("plan", expeditionID, events.AppExpeditionPlanChanged)
	return newRow, nil
}

// UpdateRow applies a partial update to a row. Changing the system name
// triggers asynchronous coordinate resolution and visit re-evaluation.
func (app *App) UpdateRow(patch expedition.RowPatch) error {
	var changed bool
	var rowItem expedition.Row
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var updateError error
		changed, updateError = expedition.UpdateRowTx(app.db, patch)
		if updateError != nil {
			return updateError
		}
		var getError error
		rowItem, getError = expedition.GetRow(app.db, patch.ID)
		if getError != nil {
			return getError
		}
		if changed {
			if rowItem.SystemName != "" && strings.TrimSpace(rowItem.POIName) == "" {
				if poiName := app.applyMissingPOIFromGEC(rowItem.ID, rowItem.SystemName, rowItem.SystemAddress); poiName != "" {
					rowItem.POIName = poiName
				}
			}
		}
		return nil
	}(); err != nil {
		return err
	}
	if changed {
		app.reevaluate(rowItem.ExpeditionID)
		if rowItem.SystemName != "" {
			app.resolveRowAsync(rowItem, true)
		}
		app.publishApp("plan", rowItem.ExpeditionID, events.AppExpeditionPlanChanged)
		return nil
	}
	app.publishApp("plan", rowItem.ExpeditionID, events.AppExpeditionPlanChanged)
	return nil
}

// SetLegMode assigns a mode to the leg arriving at rowID and updates the
// corresponding time window in mode_windows.
func (app *App) SetLegMode(rowID, modeID int64) error {
	var expeditionID int64
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var setModeError error
		expeditionID, setModeError = expedition.SetLegModeTx(app.db, rowID, modeID)
		return setModeError
	}(); err != nil {
		return err
	}
	app.publishApp("modes", expeditionID, events.AppModesChanged, events.AppModeWindowsChanged)
	return nil
}

// OverrideCarrierStopArrival manually overrides the Fleet Carrier arrival status
// on rowID and cascades the status across preceding or subsequent carrier stops
// to preserve the arrived-before-pending order invariant.
func (app *App) OverrideCarrierStopArrival(rowID int64, arrived bool) error {
	var expeditionID int64
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var overrideError error
		expeditionID, overrideError = expedition.SetCarrierArrivalOverrideTx(app.db, rowID, arrived)
		return overrideError
	}(); err != nil {
		return err
	}
	app.publishApp("plan", expeditionID, events.AppExpeditionPlanChanged)
	return nil
}

// DeleteRow removes a row.
func (app *App) DeleteRow(rowID int64) error {
	var expeditionID int64
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var deleteRowError error
		expeditionID, deleteRowError = expedition.DeleteRowTx(app.db, rowID)
		return deleteRowError
	}(); err != nil {
		return err
	}
	app.reevaluate(expeditionID)
	app.publishApp("plan", expeditionID, events.AppExpeditionPlanChanged)
	return nil
}

// MoveRow moves a row to the zero-based index toIndex.
func (app *App) MoveRow(rowID int64, toIndex int) error {
	var expeditionID int64
	var rowItem expedition.Row
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var getError error
		rowItem, getError = expedition.GetRow(app.db, rowID)
		if getError != nil {
			return getError
		}
		var moveRowError error
		expeditionID, moveRowError = expedition.MoveRowTx(app.db, rowID, toIndex)
		return moveRowError
	}(); err != nil {
		return err
	}
	if rowItem.RowType == expedition.RowWaypoint {
		app.reevaluate(expeditionID)
		app.publishApp("plan", expeditionID, events.AppExpeditionPlanChanged)
		return nil
	}
	app.publishApp("plan", expeditionID, events.AppExpeditionPlanChanged)
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

func (app *App) lifecycle(expeditionID int64, action func() error) error {
	if err := app.withReadyReadLock(action); err != nil {
		return err
	}
	app.publishApp("plan", expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionBoundsChanged)
	return nil
}

// StartExpedition starts the expedition at the given visit timestamp
// (ISO-8601 UTC) and retroactively replays history from that moment.
func (app *App) StartExpedition(expeditionID int64, timestamp string) error {
	return app.lifecycle(expeditionID, func() error { return expedition.StartTx(app.db, expeditionID, timestamp) })
}

// CompleteExpedition ends the expedition now.
func (app *App) CompleteExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, func() error { return expedition.CompleteTx(app.db, expeditionID, "") })
}

// ReopenExpedition makes a completed expedition active again.
func (app *App) ReopenExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, func() error { return expedition.ReopenTx(app.db, expeditionID) })
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
