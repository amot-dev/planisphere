package app

import (
	"database/sql"
	"html"
	"strconv"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/expedition"
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
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.Expedition{}, readyError
	}
	createdExpedition, createError := expedition.Create(app.db, name, description)
	if createError != nil {
		return createdExpedition, createError
	}
	_ = db.SetSetting(app.db, settingActiveExp, strconv.FormatInt(createdExpedition.ID, 10))
	app.changed("plan", createdExpedition.ID)
	return createdExpedition, nil
}

// CloneExpedition duplicates an expedition (and its waypoints and region headers)
// into a new expedition in the planning state, and makes the clone active.
func (app *App) CloneExpedition(id int64) (expedition.Expedition, error) {
	var clonedExpedition expedition.Expedition
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var cloneError error
		clonedExpedition, cloneError = expedition.Clone(app.db, id)
		if cloneError != nil {
			return cloneError
		}
		return db.SetSetting(app.db, settingActiveExp, strconv.FormatInt(clonedExpedition.ID, 10))
	}(); err != nil {
		return clonedExpedition, err
	}
	app.kickResolution(clonedExpedition.ID, false)
	app.changed("plan", clonedExpedition.ID)
	return clonedExpedition, nil
}

// UpdateExpedition renames an expedition.
func (app *App) UpdateExpedition(id int64, name, description string) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	updateError := expedition.UpdateMeta(app.db, id, name, description)
	if updateError == nil {
		app.changed("plan", id)
	}
	return updateError
}

// DeleteExpedition deletes an expedition plan (journal history is kept).
func (app *App) DeleteExpedition(id int64) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	if deleteError := expedition.Delete(app.db, id); deleteError != nil {
		return deleteError
	}
	if app.activeExpeditionID() == id {
		_ = db.SetSetting(app.db, settingActiveExp, "")
	}
	app.invalidateLiveStatsCache()
	app.changed("plan", id)
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
		return db.SetSetting(app.db, settingActiveExp, strconv.FormatInt(id, 10))
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
	return expedition.BuildPlan(app.db, id, app.gecLookup)
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
	var txError error
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		txError = db.WithTx(app.db, func(transaction *sql.Tx) error {
			var insertError error
			newRow, insertError = expedition.InsertRow(transaction, expeditionID, afterRowID, rowType, regionID)
			return insertError
		})
		return txError
	}(); err != nil {
		return newRow, err
	}
	app.changed("plan", expeditionID)
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
		changed, updateError = expedition.UpdateRow(app.db, patch)
		if updateError != nil {
			return updateError
		}
		var getError error
		rowItem, getError = expedition.GetRow(app.db, patch.ID)
		if getError != nil {
			return getError
		}
		if changed {
			if rowItem.SystemName != "" && strings.TrimSpace(rowItem.POIName) == "" && app.gec != nil {
				if matchedPOIs := app.gec.Lookup(rowItem.SystemName, rowItem.SystemAddress); len(matchedPOIs) > 0 && strings.TrimSpace(matchedPOIs[0].Name) != "" {
					poiName := html.UnescapeString(strings.TrimSpace(matchedPOIs[0].Name))
					if _, execError := app.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, rowItem.ID); execError == nil {
						rowItem.POIName = poiName
					}
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
	}
	app.changed("plan", rowItem.ExpeditionID)
	return nil
}

// SetLegMode assigns a mode to the leg arriving at rowID and updates the
// corresponding time window in mode_windows when the waypoint has been visited.
func (app *App) SetLegMode(rowID, modeID int64) error {
	var expeditionID int64
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(transaction *sql.Tx) error {
			var setModeError error
			expeditionID, setModeError = expedition.SetLegMode(transaction, rowID, modeID)
			return setModeError
		})
	}(); err != nil {
		return err
	}
	app.changed("modes", expeditionID)
	return nil
}

// SetActiveLegMode sets the mode for the current in-progress leg (since the
// latest visited waypoint).
func (app *App) SetActiveLegMode(expeditionID, modeID int64) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(transaction *sql.Tx) error {
			return expedition.SetActiveLegMode(transaction, expeditionID, modeID)
		})
	}(); err != nil {
		return err
	}
	app.changed("modes", expeditionID)
	return nil
}

// RetryResolve re-runs coordinate resolution for a waypoint.
func (app *App) RetryResolve(rowID int64) error {
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
		if rowItem.SystemName == "" {
			return nil
		}
		_, _ = app.db.Exec(`DELETE FROM system_cache WHERE name_key = ? AND source = 'notfound'`, strings.ToLower(rowItem.SystemName))
		return nil
	}(); err != nil {
		return err
	}
	if rowItem.SystemName != "" {
		app.resolveRowAsync(rowItem, true)
	}
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
		return db.WithTx(app.db, func(transaction *sql.Tx) error {
			var deleteRowError error
			expeditionID, deleteRowError = expedition.DeleteRow(transaction, rowID)
			return deleteRowError
		})
	}(); err != nil {
		return err
	}
	app.reevaluate(expeditionID)
	app.invalidateLiveStatsCache()
	app.changed("plan", expeditionID)
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
		return db.WithTx(app.db, func(transaction *sql.Tx) error {
			var moveRowError error
			expeditionID, moveRowError = expedition.MoveRow(transaction, rowID, toIndex)
			return moveRowError
		})
	}(); err != nil {
		return err
	}
	if rowItem.RowType == expedition.RowWaypoint {
		app.reevaluate(expeditionID)
	}
	app.changed("plan", expeditionID)
	return nil
}

// RegionOptions is the prioritised region dropdown content.
type RegionOptions struct {
	AnchorID          int               `json:"anchorId"`
	AnchorName        string            `json:"anchorName"`
	AnchorIsDefault   bool              `json:"anchorIsDefault"`
	SuggestedRegionID int               `json:"suggestedRegionId"` // region of the next waypoint's coordinates
	Options           []ed.RegionOption `json:"options"`
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
		anchor = ed.InnerOrionSpurID
	}
	regionRecord, _ := ed.RegionByID(anchor)
	regionOptionsResult.AnchorID, regionOptionsResult.AnchorName = anchor, regionRecord.Name
	regionOptionsResult.Options = ed.PrioritizedRegions(anchor)
	startIndex := cutoffIndex
	if editing {
		startIndex = cutoffIndex + 1
	}
	for rowIndex := startIndex; rowIndex < len(rows); rowIndex++ {
		if rows[rowIndex].IsWaypoint() && rows[rowIndex].HasCoords {
			if foundRegion, ok := ed.RegionAt(rows[rowIndex].X, rows[rowIndex].Y, rows[rowIndex].Z); ok {
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

func (app *App) lifecycle(expeditionID int64, action func(transaction *sql.Tx) error) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, action)
	}(); err != nil {
		return err
	}
	app.reevaluate(expeditionID)
	app.invalidateLiveStatsCache()
	app.changed("plan", expeditionID)
	return nil
}

// StartExpedition starts the expedition at the given visit timestamp
// (ISO-8601 UTC) and retroactively replays history from that moment.
func (app *App) StartExpedition(expeditionID int64, timestamp string) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Start(transaction, expeditionID, timestamp) })
}

// StartExpeditionOnArrival activates the expedition and waits for the next
// arrival at Waypoint #1 to set the start timestamp.
func (app *App) StartExpeditionOnArrival(expeditionID int64) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Arm(transaction, expeditionID) })
}

// CompleteExpedition ends the expedition now.
func (app *App) CompleteExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Complete(transaction, expeditionID, "") })
}

// ReopenExpedition makes a completed expedition active again.
func (app *App) ReopenExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Reopen(transaction, expeditionID) })
}

// GetWaypointDetails returns the expandable drawer content for a waypoint.
func (app *App) GetWaypointDetails(rowID int64) (expedition.WaypointDetails, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.WaypointDetails{}, readyError
	}
	return expedition.Details(app.db, rowID)
}

