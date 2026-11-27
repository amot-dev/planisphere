package expedition

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/store/modes"
	"planisphere/internal/store/settings"
)

const (
	restoreRowInsertSQL = `INSERT INTO expedition_rows(
		id, expedition_id, position, row_type, region_id, fc_stop, fc_arrived, leg_mode_id,
		system_name, poi_name, notes, system_address, edsm_id, x, y, z, coords_source,
		visited_at, departed_at, visit_event_uid, created_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	restoreRowUpdateSQL = `UPDATE expedition_rows
		SET position = ?, row_type = ?, region_id = ?, fc_stop = ?, fc_arrived = ?, leg_mode_id = ?,
		    system_name = ?, poi_name = ?, notes = ?, system_address = ?, edsm_id = ?,
		    x = ?, y = ?, z = ?, coords_source = ?, visited_at = ?, departed_at = ?,
		    visit_event_uid = ?, updated_at = ?
		WHERE id = ?`
)

// Snapshot holds a deep-copied, immutable point-in-time state of an expedition,
// its planner rows, and its active Fleet Carrier route setting.
type Snapshot struct {
	Expedition             Expedition
	Rows                   []Row
	ActiveCarrierRouteJSON string
}

// RestoreOutcome summarizes the database mutations performed by RestoreSnapshotTx.
type RestoreOutcome struct {
	PreviousSnapshot     Snapshot
	ChangedEvents        events.AppEventSet
	CarrierRouteRestored bool
	NeedsCoordResolution bool
}

// Equal reports whether snapshot and other hold identical persisted expedition,
// row, and active carrier route state (ignoring created_at and updated_at).
func (snapshot Snapshot) Equal(other Snapshot) bool {
	return expeditionsEqual(snapshot.Expedition, other.Expedition) &&
		slices.Equal(snapshot.Rows, other.Rows) &&
		snapshot.ActiveCarrierRouteJSON == other.ActiveCarrierRouteJSON
}

// CaptureSnapshot loads an immutable snapshot of expeditionID, its rows, and
// its active Fleet Carrier route JSON.
func CaptureSnapshot(queryer db.Queryer, expeditionID int64) (Snapshot, error) {
	expeditionRecord, getError := Get(queryer, expeditionID)
	if getError != nil {
		return Snapshot{}, getError
	}
	rows, listError := ListRows(queryer, expeditionID)
	if listError != nil {
		return Snapshot{}, listError
	}
	activeCarrierRouteJSON, routeError := settings.Get(queryer, activeCarrierRouteSettingKey(expeditionID))
	if routeError != nil {
		return Snapshot{}, fmt.Errorf("load active carrier route for expedition %d: %w", expeditionID, routeError)
	}
	return Snapshot{Expedition: expeditionRecord, Rows: rows, ActiveCarrierRouteJSON: activeCarrierRouteJSON}, nil
}

// RestoreSnapshotTx diff-restores target inside a single transaction, carrying
// over newly resolved coordinates and GEC POI names on unchanged system names,
// remapping missing commander profile IDs and deleted custom mode IDs, and
// re-evaluating live arrivals when the restored expedition is active.
func RestoreSnapshotTx(dbConn *sql.DB, target Snapshot) (RestoreOutcome, error) {
	var outcome RestoreOutcome
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		previousSnapshot, captureError := CaptureSnapshot(transaction, target.Expedition.ID)
		if captureError != nil {
			return captureError
		}
		outcome.PreviousSnapshot = previousSnapshot

		targetExpedition := cloneExpedition(target.Expedition)
		targetRows := slices.Clone(target.Rows)
		var resolveProfileError error
		if targetExpedition.CommanderProfileID, resolveProfileError = resolveValidOrLatestCommanderProfileID(transaction, targetExpedition.CommanderProfileID); resolveProfileError != nil {
			return resolveProfileError
		}
		if remapError := remapDeletedModeIDs(transaction, &targetExpedition, targetRows); remapError != nil {
			return remapError
		}
		previousRowsByID := make(map[int64]Row, len(previousSnapshot.Rows))
		for _, rowItem := range previousSnapshot.Rows {
			previousRowsByID[rowItem.ID] = rowItem
		}
		outcome.NeedsCoordResolution = preserveLiveResolvedFields(targetRows, previousRowsByID)

		now := reference.NowISO()
		if err := restoreExpeditionRecord(transaction, previousSnapshot.Expedition, targetExpedition, now); err != nil {
			return err
		}
		rowsMutated, restoreRowsError := restoreExpeditionRows(transaction, targetExpedition.ID, previousSnapshot.Rows, previousRowsByID, targetRows, now)
		if restoreRowsError != nil {
			return restoreRowsError
		}
		if rowsMutated {
			if touchError := touch(transaction, targetExpedition.ID); touchError != nil {
				return touchError
			}
		}
		var restoreRouteError error
		if outcome.CarrierRouteRestored, restoreRouteError = restoreCarrierRouteSetting(transaction, targetExpedition.ID, previousSnapshot.ActiveCarrierRouteJSON, target.ActiveCarrierRouteJSON); restoreRouteError != nil {
			return restoreRouteError
		}

		finalRows := targetRows
		if targetExpedition.Status == StatusActive {
			reevalChanged, reevalError := Reevaluate(transaction, targetExpedition.ID)
			if reevalError != nil {
				return reevalError
			}
			if reevalChanged {
				reloadedRows, reloadError := ListRows(transaction, targetExpedition.ID)
				if reloadError != nil {
					return reloadError
				}
				finalRows = reloadedRows
			}
		}
		outcome.ChangedEvents = computeRestoreChangedEvents(previousSnapshot, targetExpedition, finalRows, target.ActiveCarrierRouteJSON)
		return nil
	})
	if txError != nil {
		return RestoreOutcome{}, txError
	}
	return outcome, nil
}

func cloneExpedition(source Expedition) Expedition {
	cloned := source
	if source.Adjustments.TritiumUsed != nil {
		tritiumCopy := *source.Adjustments.TritiumUsed
		cloned.Adjustments.TritiumUsed = &tritiumCopy
	}
	return cloned
}

func cloneSnapshot(source Snapshot) Snapshot {
	return Snapshot{
		Expedition:             cloneExpedition(source.Expedition),
		Rows:                   slices.Clone(source.Rows),
		ActiveCarrierRouteJSON: source.ActiveCarrierRouteJSON,
	}
}

func preserveLiveResolvedFields(targetRows []Row, previousRowsByID map[int64]Row) bool {
	needsResolution := false
	for index := range targetRows {
		rowItem := &targetRows[index]
		if !rowItem.IsWaypoint() {
			continue
		}
		if !rowItem.HasCoords && rowItem.SystemName != "" {
			if currentRow, exists := previousRowsByID[rowItem.ID]; exists && currentRow.SystemName == rowItem.SystemName && currentRow.HasCoords {
				rowItem.SystemAddress, rowItem.EDSMID = currentRow.SystemAddress, currentRow.EDSMID
				rowItem.HasCoords, rowItem.X, rowItem.Y, rowItem.Z = true, currentRow.X, currentRow.Y, currentRow.Z
				rowItem.CoordsSource = currentRow.CoordsSource
				if rowItem.POIName == "" && currentRow.POIName != "" {
					rowItem.POIName = currentRow.POIName
				}
			}
		}
		if rowItem.SystemName != "" && !rowItem.HasCoords && rowItem.CoordsSource == "" {
			needsResolution = true
		}
	}
	return needsResolution
}

func restoreExpeditionRecord(transaction *sql.Tx, previousExpedition, targetExpedition Expedition, now string) error {
	if expeditionsEqual(previousExpedition, targetExpedition) {
		return nil
	}
	adjustmentsBytes, marshalError := json.Marshal(targetExpedition.Adjustments)
	if marshalError != nil {
		return fmt.Errorf("marshal expedition %d adjustments: %w", targetExpedition.ID, marshalError)
	}
	if _, execError := transaction.Exec(
		`UPDATE expeditions SET name = ?, description = ?, status = ?, start_timestamp = ?, end_timestamp = ?,
		 armed_at = NULL, adjustments_json = ?, carrier_type = ?, carrier_weekly_upkeep = ?,
		 active_mode_id = ?, commander_profile_id = ?, updated_at = ? WHERE id = ?`,
		targetExpedition.Name, targetExpedition.Description, targetExpedition.Status,
		nullableString(targetExpedition.StartTimestamp), nullableString(targetExpedition.EndTimestamp),
		string(adjustmentsBytes), targetExpedition.CarrierType, targetExpedition.CarrierWeeklyUpkeep,
		targetExpedition.ActiveModeID, targetExpedition.CommanderProfileID, now, targetExpedition.ID,
	); execError != nil {
		return fmt.Errorf("restore expedition %d record: %w", targetExpedition.ID, execError)
	}
	return nil
}

func restoreExpeditionRows(
	transaction *sql.Tx,
	expeditionID int64,
	previousRows []Row,
	previousRowsByID map[int64]Row,
	targetRows []Row,
	now string,
) (bool, error) {
	targetRowIDs := make(map[int64]bool, len(targetRows))
	for _, targetRow := range targetRows {
		targetRowIDs[targetRow.ID] = true
	}
	rowsMutated := false
	for _, previousRow := range previousRows {
		if targetRowIDs[previousRow.ID] {
			continue
		}
		if _, deleteError := transaction.Exec(`DELETE FROM expedition_rows WHERE id = ?`, previousRow.ID); deleteError != nil {
			return false, fmt.Errorf("delete extra row %d during restore: %w", previousRow.ID, deleteError)
		}
		rowsMutated = true
	}

	var insertStatement, updateStatement *sql.Stmt
	defer func() {
		if insertStatement != nil {
			_ = insertStatement.Close()
		}
		if updateStatement != nil {
			_ = updateStatement.Close()
		}
	}()

	for _, targetRow := range targetRows {
		previousRow, exists := previousRowsByID[targetRow.ID]
		if !exists {
			if insertStatement == nil {
				var prepareError error
				if insertStatement, prepareError = transaction.Prepare(restoreRowInsertSQL); prepareError != nil {
					return false, fmt.Errorf("prepare restore row insert: %w", prepareError)
				}
			}
			args := append(append([]any{targetRow.ID, expeditionID}, rowColumnValues(targetRow)...), now, now)
			if _, execError := insertStatement.Exec(args...); execError != nil {
				return false, fmt.Errorf("reinsert row %d during restore: %w", targetRow.ID, execError)
			}
			rowsMutated = true
			continue
		}
		if previousRow == targetRow {
			continue
		}
		if updateStatement == nil {
			var prepareError error
			if updateStatement, prepareError = transaction.Prepare(restoreRowUpdateSQL); prepareError != nil {
				return false, fmt.Errorf("prepare restore row update: %w", prepareError)
			}
		}
		args := append(rowColumnValues(targetRow), now, targetRow.ID)
		if _, execError := updateStatement.Exec(args...); execError != nil {
			return false, fmt.Errorf("update row %d during restore: %w", targetRow.ID, execError)
		}
		rowsMutated = true
	}
	return rowsMutated, nil
}

func rowColumnValues(rowItem Row) []any {
	var regionValue, coordX, coordY, coordZ any
	if rowItem.RowType == RowRegion && rowItem.RegionID > 0 {
		regionValue = rowItem.RegionID
	}
	if rowItem.HasCoords {
		coordX, coordY, coordZ = rowItem.X, rowItem.Y, rowItem.Z
	}
	return []any{
		rowItem.Position, rowItem.RowType, regionValue, rowItem.FCStop, rowItem.FCArrived, rowItem.LegModeID,
		rowItem.SystemName, rowItem.POIName, rowItem.Notes, nullableInt64(rowItem.SystemAddress), nullableInt64(rowItem.EDSMID),
		coordX, coordY, coordZ, rowItem.CoordsSource,
		nullableString(rowItem.VisitedAt), nullableString(rowItem.DepartedAt), nullableString(rowItem.VisitEventUID),
	}
}

func remapDeletedModeIDs(transaction *sql.Tx, targetExpedition *Expedition, targetRows []Row) error {
	hasPositiveModeID := targetExpedition.ActiveModeID > 0
	if !hasPositiveModeID {
		for index := range targetRows {
			if targetRows[index].LegModeID > 0 {
				hasPositiveModeID = true
				break
			}
		}
	}
	if !hasPositiveModeID {
		return nil
	}
	modeList, listError := modes.List(transaction)
	if listError != nil {
		return fmt.Errorf("list modes for snapshot restore: %w", listError)
	}
	existingModeIDs := make(map[int64]bool, len(modeList))
	for _, modeItem := range modeList {
		existingModeIDs[modeItem.ID] = true
	}
	fallbackModeID := resolvePlanFallbackModeID(modeList)
	if targetExpedition.ActiveModeID > 0 && !existingModeIDs[targetExpedition.ActiveModeID] {
		targetExpedition.ActiveModeID = fallbackModeID
	}
	for index := range targetRows {
		if targetRows[index].LegModeID > 0 && !existingModeIDs[targetRows[index].LegModeID] {
			targetRows[index].LegModeID = fallbackModeID
		}
	}
	return nil
}

func restoreCarrierRouteSetting(transaction *sql.Tx, expeditionID int64, previousRouteJSON, targetRouteJSON string) (bool, error) {
	if previousRouteJSON == targetRouteJSON {
		return false, nil
	}
	if targetRouteJSON == "" {
		if clearError := ClearActiveCarrierRoute(transaction, expeditionID); clearError != nil {
			return false, fmt.Errorf("clear active carrier route during restore: %w", clearError)
		}
		return true, nil
	}
	if setError := settings.Set(transaction, activeCarrierRouteSettingKey(expeditionID), targetRouteJSON); setError != nil {
		return false, fmt.Errorf("restore active carrier route setting: %w", setError)
	}
	return true, nil
}

type waypointModeState struct {
	id         int64
	legModeID  int64
	visitedAt  string
	departedAt string
}

func collectWaypointModeStates(rows []Row) []waypointModeState {
	modeStates := make([]waypointModeState, 0, len(rows))
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && (rowItem.LegModeID != 0 || rowItem.VisitedAt != "" || rowItem.DepartedAt != "") {
			modeStates = append(modeStates, waypointModeState{
				id: rowItem.ID, legModeID: rowItem.LegModeID, visitedAt: rowItem.VisitedAt, departedAt: rowItem.DepartedAt,
			})
		}
	}
	return modeStates
}

func computeRestoreChangedEvents(
	previousSnapshot Snapshot,
	finalExpedition Expedition,
	finalRows []Row,
	finalCarrierRouteJSON string,
) events.AppEventSet {
	previousExpedition := previousSnapshot.Expedition
	var changedEvents events.AppEventSet
	if !expeditionsEqual(previousExpedition, finalExpedition) || !slices.Equal(previousSnapshot.Rows, finalRows) || previousSnapshot.ActiveCarrierRouteJSON != finalCarrierRouteJSON {
		changedEvents = changedEvents.Add(events.AppExpeditionPlanChanged)
	}
	commanderChanged := previousExpedition.CommanderProfileID != finalExpedition.CommanderProfileID
	if previousExpedition.Status != finalExpedition.Status || previousExpedition.StartTimestamp != finalExpedition.StartTimestamp ||
		previousExpedition.EndTimestamp != finalExpedition.EndTimestamp || commanderChanged {
		changedEvents = changedEvents.Add(events.AppExpeditionBoundsChanged)
	}
	if previousExpedition.ActiveModeID != finalExpedition.ActiveModeID || commanderChanged ||
		!slices.Equal(collectWaypointModeStates(previousSnapshot.Rows), collectWaypointModeStates(finalRows)) {
		changedEvents = changedEvents.Add(events.AppExpeditionModesChanged)
	}
	if !carrierStatsEqual(previousExpedition.Adjustments, finalExpedition.Adjustments) ||
		previousExpedition.CarrierType != finalExpedition.CarrierType ||
		previousExpedition.CarrierWeeklyUpkeep != finalExpedition.CarrierWeeklyUpkeep {
		changedEvents = changedEvents.Add(events.AppAdjustmentsChanged)
	}
	return changedEvents
}

func expeditionsEqual(left, right Expedition) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Description == right.Description &&
		left.Status == right.Status && left.StartTimestamp == right.StartTimestamp && left.EndTimestamp == right.EndTimestamp &&
		carrierStatsEqual(left.Adjustments, right.Adjustments) && left.CarrierType == right.CarrierType &&
		left.CarrierWeeklyUpkeep == right.CarrierWeeklyUpkeep && left.ActiveModeID == right.ActiveModeID &&
		left.CommanderProfileID == right.CommanderProfileID
}

func carrierStatsEqual(left, right CarrierStats) bool {
	if left.CarrierJumps != right.CarrierJumps || left.CarrierDistanceLy != right.CarrierDistanceLy {
		return false
	}
	if left.TritiumUsed == nil || right.TritiumUsed == nil {
		return left.TritiumUsed == right.TritiumUsed
	}
	return *left.TritiumUsed == *right.TritiumUsed
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
