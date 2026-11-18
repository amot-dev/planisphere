package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/store/modes"
)

// Row types.
const (
	RowWaypoint = "waypoint"
	RowRegion   = "region"
)

// Row is a planner row (waypoint or region header).
type Row struct {
	ID            int64   `json:"id"`
	ExpeditionID  int64   `json:"expeditionId"`
	Position      int     `json:"position"`
	RowType       string  `json:"rowType"`
	RegionID      int     `json:"regionId"`
	FCStop        bool    `json:"fcStop"`
	FCArrived     bool    `json:"fcArrived"`
	LegModeID     int64   `json:"legModeId"`
	SystemName    string  `json:"systemName"`
	POIName       string  `json:"poiName"`
	Notes         string  `json:"notes"`
	SystemAddress int64   `json:"systemAddress"`
	EDSMID        int64   `json:"edsmId"`
	HasCoords     bool    `json:"hasCoords"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Z             float64 `json:"z"`
	CoordsSource  string  `json:"coordsSource"`
	VisitedAt     string  `json:"visitedAt"`
	VisitEventUID string  `json:"visitEventUid"`
}

// Coords returns the row's coordinates if known.
func (row Row) Coords() (reference.Vec3, bool) {
	return reference.Vec3{X: row.X, Y: row.Y, Z: row.Z}, row.HasCoords
}

// IsWaypoint reports whether the row is a waypoint.
func (row Row) IsWaypoint() bool { return row.RowType == RowWaypoint }

const rowCols = `id, expedition_id, position, row_type, COALESCE(region_id, 0), fc_stop, COALESCE(fc_arrived, 0), COALESCE(leg_mode_id, 0),
	system_name, poi_name, notes, COALESCE(system_address, 0), COALESCE(edsm_id, 0), x, y, z, coords_source, COALESCE(visited_at, ''), COALESCE(visit_event_uid, '')`

func scanRow(scanner interface{ Scan(...any) error }) (Row, error) {
	var rowItem Row
	var coordX, coordY, coordZ sql.NullFloat64
	scanError := scanner.Scan(&rowItem.ID, &rowItem.ExpeditionID, &rowItem.Position, &rowItem.RowType, &rowItem.RegionID, &rowItem.FCStop, &rowItem.FCArrived, &rowItem.LegModeID, &rowItem.SystemName, &rowItem.POIName,
		&rowItem.Notes, &rowItem.SystemAddress, &rowItem.EDSMID, &coordX, &coordY, &coordZ, &rowItem.CoordsSource, &rowItem.VisitedAt, &rowItem.VisitEventUID)
	if scanError != nil {
		return rowItem, scanError
	}
	if coordX.Valid && coordY.Valid && coordZ.Valid {
		rowItem.HasCoords, rowItem.X, rowItem.Y, rowItem.Z = true, coordX.Float64, coordY.Float64, coordZ.Float64
	}
	return rowItem, nil
}

// ListRows returns an expedition's rows in display order.
func ListRows(queryer db.Queryer, expeditionID int64) ([]Row, error) {
	rows, queryError := queryer.Query(`SELECT `+rowCols+` FROM expedition_rows WHERE expedition_id = ? ORDER BY position, id`, expeditionID)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var rowList []Row
	for rows.Next() {
		rowItem, scanError := scanRow(rows)
		if scanError != nil {
			return nil, scanError
		}
		rowList = append(rowList, rowItem)
	}
	return rowList, rows.Err()
}

// GetRow loads a single row.
func GetRow(queryer db.Queryer, rowID int64) (Row, error) {
	rowItem, scanError := scanRow(queryer.QueryRow(`SELECT `+rowCols+` FROM expedition_rows WHERE id = ?`, rowID))
	if errors.Is(scanError, sql.ErrNoRows) {
		return rowItem, fmt.Errorf("row %d not found", rowID)
	}
	return rowItem, scanError
}

// writePositions persists the order of rows as positions 1..n.
func writePositions(queryer db.Queryer, rows []Row) error {
	var updatePositionStatement *sql.Stmt
	for i := range rows {
		if rows[i].Position == i+1 {
			continue
		}
		if updatePositionStatement == nil {
			var prepError error
			updatePositionStatement, prepError = queryer.Prepare(`UPDATE expedition_rows SET position = ? WHERE id = ?`)
			if prepError != nil {
				return prepError
			}
			defer updatePositionStatement.Close()
		}
		rows[i].Position = i + 1
		if _, execError := updatePositionStatement.Exec(i+1, rows[i].ID); execError != nil {
			return execError
		}
	}
	return nil
}

// InsertPosition values for InsertRow.
const (
	InsertAtEnd = int64(0)
	InsertAtTop = int64(-1)
)

// InsertRow adds a row after afterRowID (InsertAtEnd or InsertAtTop for the ends).
func InsertRow(transaction *sql.Tx, expeditionID, afterRowID int64, rowType string, regionID int) (Row, error) {
	if rowType != RowWaypoint && rowType != RowRegion {
		return Row{}, fmt.Errorf("invalid row type %q", rowType)
	}
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return Row{}, listError
	}
	insertIndex := len(rows)
	switch afterRowID {
	case InsertAtEnd:
	case InsertAtTop:
		insertIndex = 0
	default:
		found := false
		for i, existingRow := range rows {
			if existingRow.ID == afterRowID {
				insertIndex, found = i+1, true
				break
			}
		}
		if !found {
			return Row{}, fmt.Errorf("row %d not found", afterRowID)
		}
	}
	var regionValue any
	if rowType == RowRegion {
		if _, ok := reference.RegionByID(regionID); !ok {
			regionID = reference.InnerOrionSpurID
		}
		regionValue = regionID
	}
	now := reference.NowISO()
	insertResult, insertError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, region_id, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, expeditionID, insertIndex+1, rowType, regionValue, now, now)
	if insertError != nil {
		return Row{}, insertError
	}
	insertedRowID, idError := insertResult.LastInsertId()
	if idError != nil {
		return Row{}, fmt.Errorf("read inserted row id: %w", idError)
	}
	newRow, getError := GetRow(transaction, insertedRowID)
	if getError != nil {
		return Row{}, getError
	}
	if insertIndex < len(rows) {
		orderedRows := append([]Row{}, rows[:insertIndex]...)
		orderedRows = append(orderedRows, newRow)
		orderedRows = append(orderedRows, rows[insertIndex:]...)
		if writeError := writePositions(transaction, orderedRows); writeError != nil {
			return Row{}, writeError
		}
	}
	if touchError := touch(transaction, expeditionID); touchError != nil {
		return Row{}, touchError
	}
	return GetRow(transaction, insertedRowID)
}

// RowPatch is a partial row update; nil fields are left unchanged.
type RowPatch struct {
	ID         int64   `json:"id"`
	FCStop     *bool   `json:"fcStop,omitempty"`
	FCArrived  *bool   `json:"fcArrived,omitempty"`
	LegModeID  *int64  `json:"legModeId,omitempty"`
	SystemName *string `json:"systemName,omitempty"`
	POIName    *string `json:"poiName,omitempty"`
	Notes      *string `json:"notes,omitempty"`
	RegionID   *int    `json:"regionId,omitempty"`
}

func validateFCStopAgainstActiveRoute(rows []Row, currentRowIndex int, activeRoute *ActiveCarrierRouteState) error {
	isManualMode := activeRoute.ManualMode || activeRoute.Route.ManualMode || len(activeRoute.Route.Jumps) == 0
	if !isManualMode {
		targetIndex := -1
		if activeRoute.TargetRowID > 0 {
			targetIndex = indexOf(rows, activeRoute.TargetRowID)
		}
		if targetIndex >= 0 && currentRowIndex <= targetIndex {
			return errors.New("fleet carrier stop cannot be changed at or before an active routed stop")
		}
		return nil
	}
	for i, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.FCStop && !rowItem.FCArrived {
			if currentRowIndex < i {
				return errors.New("fleet carrier stop cannot be changed before the active manual carrier stop")
			}
			break
		}
	}
	return nil
}

func validateActiveSystemNameEdit(queryer db.Queryer, currentRow Row, loadRows func() ([]Row, error)) error {
	if currentRow.VisitedAt != "" {
		return errors.New("star system cannot be changed on a visited waypoint")
	}
	if currentRow.FCStop && currentRow.FCArrived {
		return errors.New("star system cannot be changed while fleet carrier is marked arrived")
	}
	rows, listError := loadRows()
	if listError != nil {
		return listError
	}
	if firstWaypointIndex := firstWaypoint(rows); firstWaypointIndex >= 0 && rows[firstWaypointIndex].ID == currentRow.ID {
		return errors.New("waypoint #1 star system cannot be changed after the expedition has started")
	}
	if lockedTargetID := readLockedSpanshTargetRowID(queryer, currentRow.ExpeditionID); lockedTargetID > 0 && currentRow.ID == lockedTargetID {
		return errors.New("star system cannot be changed while fleet carrier route is active")
	}
	return nil
}

func validateActiveFCStopEdit(queryer db.Queryer, currentRow Row, nextFCStop bool, loadRows func() ([]Row, error)) error {
	if !nextFCStop && currentRow.FCArrived {
		return errors.New("arrived fleet carrier stop cannot be removed")
	}
	rows, listError := loadRows()
	if listError != nil {
		return listError
	}
	currentRowIndex := indexOf(rows, currentRow.ID)
	if currentRowIndex < 0 {
		return fmt.Errorf("row %d not found", currentRow.ID)
	}
	lastFCArrivedIndex := -1
	for i, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.FCStop && rowItem.FCArrived {
			lastFCArrivedIndex = i
		}
	}
	if nextFCStop && currentRowIndex <= lastFCArrivedIndex {
		return errors.New("fleet carrier stop cannot be added at or before an arrived carrier stop")
	}
	activeRoute, routeError := GetActiveCarrierRoute(queryer, currentRow.ExpeditionID)
	if routeError != nil {
		return routeError
	}
	if activeRoute == nil {
		return nil
	}
	return validateFCStopAgainstActiveRoute(rows, currentRowIndex, activeRoute)
}

func validateRowStructuralEdits(queryer db.Queryer, currentRow Row, patch RowPatch, systemChanging bool) error {
	if patch.RegionID != nil && currentRow.RowType == RowRegion {
		if _, ok := reference.RegionByID(*patch.RegionID); !ok {
			return fmt.Errorf("invalid region %d", *patch.RegionID)
		}
	}

	regionChanging := patch.RegionID != nil && currentRow.RowType == RowRegion && *patch.RegionID != currentRow.RegionID
	fcStopChanging := patch.FCStop != nil && *patch.FCStop != currentRow.FCStop
	fcArrivedChanging := patch.FCArrived != nil && *patch.FCArrived != currentRow.FCArrived
	if !regionChanging && !fcStopChanging && !fcArrivedChanging && !systemChanging {
		return nil
	}

	expeditionRecord, expError := Get(queryer, currentRow.ExpeditionID)
	if expError != nil {
		return expError
	}
	if expeditionRecord.Status == StatusCompleted {
		return errors.New("completed expedition structure cannot be modified")
	}
	if expeditionRecord.Status != StatusActive {
		return nil
	}

	var cachedRows []Row
	rowsLoaded := false
	loadRows := func() ([]Row, error) {
		if rowsLoaded {
			return cachedRows, nil
		}
		loadedRows, listError := ListRows(queryer, currentRow.ExpeditionID)
		if listError != nil {
			return nil, listError
		}
		cachedRows = loadedRows
		rowsLoaded = true
		return cachedRows, nil
	}

	if systemChanging {
		if systemError := validateActiveSystemNameEdit(queryer, currentRow, loadRows); systemError != nil {
			return systemError
		}
	}
	if fcStopChanging {
		return validateActiveFCStopEdit(queryer, currentRow, *patch.FCStop, loadRows)
	}
	return nil
}

// UpdateRow applies a patch. systemChanged reports whether the system name
// changed (coordinates are cleared and must be re-resolved).
func UpdateRow(queryer db.Queryer, patch RowPatch) (systemChanged bool, updateError error) {
	currentRow, getError := GetRow(queryer, patch.ID)
	if getError != nil {
		return false, getError
	}
	var normalizedSystemName string
	if patch.SystemName != nil && currentRow.RowType == RowWaypoint {
		normalizedSystemName = strings.Join(strings.Fields(*patch.SystemName), " ")
		systemChanged = normalizedSystemName != currentRow.SystemName
	}
	if validationError := validateRowStructuralEdits(queryer, currentRow, patch, systemChanged); validationError != nil {
		return false, validationError
	}
	now := reference.NowISO()
	if patch.FCStop != nil {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET fc_stop = ?, updated_at = ? WHERE id = ?`, *patch.FCStop, now, patch.ID); execError != nil {
			return false, execError
		}
		if !*patch.FCStop {
			if _, execError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE id = ?`, patch.ID); execError != nil {
				return false, execError
			}
		}
	}
	if patch.FCArrived != nil {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = ?, updated_at = ? WHERE id = ?`, *patch.FCArrived, now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if patch.LegModeID != nil {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET leg_mode_id = ?, updated_at = ? WHERE id = ?`, *patch.LegModeID, now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if patch.POIName != nil {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET poi_name = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(*patch.POIName), now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if patch.Notes != nil {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET notes = ?, updated_at = ? WHERE id = ?`, *patch.Notes, now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if patch.RegionID != nil && currentRow.RowType == RowRegion {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET region_id = ?, updated_at = ? WHERE id = ?`, *patch.RegionID, now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if systemChanged {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET system_name = ?, system_address = NULL, edsm_id = NULL,
			x = NULL, y = NULL, z = NULL, coords_source = '', visited_at = NULL, visit_event_uid = NULL, updated_at = ?
			WHERE id = ?`, normalizedSystemName, now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if touchError := touch(queryer, currentRow.ExpeditionID); touchError != nil {
		return false, touchError
	}
	return systemChanged, nil
}

// SetRowCoords stores resolved coordinates if the row still has systemName.
func SetRowCoords(queryer db.Queryer, rowID int64, systemName string, systemAddress, edsmID int64, coords *reference.Vec3, source string) error {
	var xCoord, yCoord, zCoord, addressValue, edsmIDValue any
	if coords != nil {
		xCoord, yCoord, zCoord = coords.X, coords.Y, coords.Z
	}
	if systemAddress != 0 {
		addressValue = systemAddress
	}
	if edsmID != 0 {
		edsmIDValue = edsmID
	}
	_, execError := queryer.Exec(`UPDATE expedition_rows SET system_address = ?, edsm_id = ?, x = ?, y = ?, z = ?, coords_source = ?
		WHERE id = ? AND system_name = ?`, addressValue, edsmIDValue, xCoord, yCoord, zCoord, source, rowID, systemName)
	return execError
}

// DeleteRow removes a row and re-indexes positions.
func DeleteRow(transaction *sql.Tx, rowID int64) (int64, error) {
	rowItem, getError := GetRow(transaction, rowID)
	if getError != nil {
		return 0, getError
	}
	expeditionRecord, expError := Get(transaction, rowItem.ExpeditionID)
	if expError != nil {
		return 0, expError
	}
	if expeditionRecord.Status == StatusCompleted {
		return 0, errors.New("completed expedition rows cannot be deleted")
	}
	if expeditionRecord.Status != StatusPlanning && rowItem.IsWaypoint() {
		if lockedSpanshTargetRowID := readLockedSpanshTargetRowID(transaction, rowItem.ExpeditionID); lockedSpanshTargetRowID > 0 && lockedSpanshTargetRowID == rowID {
			return 0, errors.New("waypoint cannot be deleted while fleet carrier route is active")
		}
		existingRows, listExistingError := ListRows(transaction, rowItem.ExpeditionID)
		if listExistingError != nil {
			return 0, listExistingError
		}
		if firstIdx := firstWaypoint(existingRows); firstIdx >= 0 && existingRows[firstIdx].ID == rowID {
			return 0, errors.New("waypoint #1 cannot be deleted after the expedition has started")
		}
	}
	if _, deleteError := transaction.Exec(`DELETE FROM expedition_rows WHERE id = ?`, rowID); deleteError != nil {
		return 0, deleteError
	}
	rows, listError := ListRows(transaction, rowItem.ExpeditionID)
	if listError != nil {
		return 0, listError
	}
	if touchError := touch(transaction, rowItem.ExpeditionID); touchError != nil {
		return 0, touchError
	}
	return rowItem.ExpeditionID, writePositions(transaction, rows)
}

// PruneEmptyRows removes all empty waypoints (system_name empty or whitespace)
// and region headers that have only empty or no waypoints after them.
func PruneEmptyRows(transaction *sql.Tx, expeditionID int64) error {
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return listError
	}
	if len(rows) == 0 {
		return nil
	}

	// Determine which waypoints are non-empty.
	isNonEmptyWaypoint := make([]bool, len(rows))
	for i, rowItem := range rows {
		if rowItem.RowType == RowWaypoint && strings.TrimSpace(rowItem.SystemName) != "" {
			isNonEmptyWaypoint[i] = true
		}
	}

	// Identify empty waypoints and headers with zero non-empty waypoints after them.
	var rowIDsToDelete []int64
	var survivingRows []Row

	for i, rowItem := range rows {
		if rowItem.RowType == RowWaypoint {
			if !isNonEmptyWaypoint[i] {
				rowIDsToDelete = append(rowIDsToDelete, rowItem.ID)
			} else {
				survivingRows = append(survivingRows, rowItem)
			}
			continue
		}

		if rowItem.RowType == RowRegion {
			// Count how many non-empty waypoints follow this header until the next header or end.
			nonEmptyCount := 0
			for nextIndex := i + 1; nextIndex < len(rows); nextIndex++ {
				if rows[nextIndex].RowType == RowRegion {
					break
				}
				if isNonEmptyWaypoint[nextIndex] {
					nonEmptyCount++
				}
			}
			if nonEmptyCount == 0 {
				rowIDsToDelete = append(rowIDsToDelete, rowItem.ID)
			} else {
				survivingRows = append(survivingRows, rowItem)
			}
			continue
		}

		survivingRows = append(survivingRows, rowItem)
	}

	if len(rowIDsToDelete) == 0 {
		return nil
	}

	for _, deleteID := range rowIDsToDelete {
		if _, deleteError := transaction.Exec(`DELETE FROM expedition_rows WHERE id = ?`, deleteID); deleteError != nil {
			return deleteError
		}
	}

	if touchError := touch(transaction, expeditionID); touchError != nil {
		return touchError
	}
	return writePositions(transaction, survivingRows)
}

// MoveRow moves a row to the zero-based index toIndex.
func MoveRow(transaction *sql.Tx, rowID int64, toIndex int) (int64, error) {
	rowItem, getError := GetRow(transaction, rowID)
	if getError != nil {
		return 0, getError
	}
	expeditionRecord, expError := Get(transaction, rowItem.ExpeditionID)
	if expError != nil {
		return 0, expError
	}
	if expeditionRecord.Status == StatusCompleted {
		return 0, errors.New("completed expedition rows cannot be reordered")
	}
	rows, listError := ListRows(transaction, rowItem.ExpeditionID)
	if listError != nil {
		return 0, listError
	}
	fromIndex := indexOf(rows, rowID)
	if fromIndex < 0 {
		return 0, fmt.Errorf("row %d not found", rowID)
	}
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex >= len(rows) {
		toIndex = len(rows) - 1
	}
	if fromIndex == toIndex {
		return rowItem.ExpeditionID, nil
	}
	if expeditionRecord.Status != StatusPlanning && rowItem.IsWaypoint() {
		preview, validateError := validateWaypointMove(transaction, expeditionRecord, rows, fromIndex, toIndex)
		if validateError != nil {
			return 0, validateError
		}
		if preview.ResetsFCArrived {
			if _, resetError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE id = ?`, rowID); resetError != nil {
				return 0, resetError
			}
		}
	}
	rows = moveTo(rows, fromIndex, toIndex)
	if touchError := touch(transaction, rowItem.ExpeditionID); touchError != nil {
		return 0, touchError
	}
	return rowItem.ExpeditionID, writePositions(transaction, rows)
}

func indexOf(rows []Row, targetRowID int64) int {
	for i, rowItem := range rows {
		if rowItem.ID == targetRowID {
			return i
		}
	}
	return -1
}

// moveTo moves items[fromIndex] in place so that it ends up at index toIndex.
func moveTo[T any](items []T, fromIndex, toIndex int) []T {
	if fromIndex < 0 || fromIndex >= len(items) {
		return items
	}
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex >= len(items) {
		toIndex = len(items) - 1
	}
	if fromIndex == toIndex {
		return items
	}
	targetItem := items[fromIndex]
	if fromIndex < toIndex {
		copy(items[fromIndex:toIndex], items[fromIndex+1:toIndex+1])
	} else {
		copy(items[toIndex+1:fromIndex+1], items[toIndex:fromIndex])
	}
	items[toIndex] = targetItem
	return items
}

func touch(queryer db.Queryer, expeditionID int64) error {
	_, execError := queryer.Exec(`UPDATE expeditions SET updated_at = ? WHERE id = ?`, reference.NowISO(), expeditionID)
	return execError
}

// InsertRowTx inserts a row inside a database transaction on dbConn.
func InsertRowTx(dbConn *sql.DB, expeditionID, afterRowID int64, rowType string, regionID int) (Row, error) {
	var createdRow Row
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var insertError error
		createdRow, insertError = InsertRow(transaction, expeditionID, afterRowID, rowType, regionID)
		return insertError
	})
	return createdRow, txError
}

// UpdateRowTx applies patch inside a database transaction on dbConn.
func UpdateRowTx(dbConn *sql.DB, patch RowPatch) (bool, error) {
	var systemChanged bool
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var updateError error
		systemChanged, updateError = UpdateRow(transaction, patch)
		return updateError
	})
	return systemChanged, txError
}

// DeleteRowTx removes rowID inside a database transaction on dbConn.
func DeleteRowTx(dbConn *sql.DB, rowID int64) (int64, error) {
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var deleteError error
		expeditionID, deleteError = DeleteRow(transaction, rowID)
		return deleteError
	})
	return expeditionID, txError
}

// MoveRowTx moves rowID to toIndex inside a database transaction on dbConn.
func MoveRowTx(dbConn *sql.DB, rowID int64, toIndex int) (int64, error) {
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var moveError error
		expeditionID, moveError = MoveRow(transaction, rowID, toIndex)
		return moveError
	})
	return expeditionID, txError
}

// addOneSecond shifts an ISO timestamp forward by one second. Leg telemetry
// uses half-open arrival intervals (prevVisit, rowVisit], whereas modes.SetWindow
// stores half-open [StartsAt, EndsAt) intervals at 1-second resolution; shifting
// both bounds maps (prevVisit, rowVisit] onto [prevVisit+1s, rowVisit+1s) and
// (latestVisit, +inf) onto [latestVisit+1s, "") so arrival-second events stay in the arriving leg.
func addOneSecond(isoTimestamp string) string {
	parsedTime, parseError := reference.ParseISO(isoTimestamp)
	if parseError != nil {
		return isoTimestamp
	}
	return parsedTime.Add(time.Second).UTC().Format("2006-01-02T15:04:05Z")
}

// SetLegMode updates the single mode assigned to the leg arriving at rowID.
// When the waypoint has been visited (after a previous visited waypoint), it
// updates the closed time window (prevVisit, rowVisit] in mode_windows; when
// the waypoint is the active in-progress leg, it updates the open-ended window
// (latestVisit, +inf) so filtered expedition and all-time stats immediately
// reflect the change.
func SetLegMode(transaction *sql.Tx, rowID int64, modeID int64) (int64, error) {
	targetRow, rowError := GetRow(transaction, rowID)
	if rowError != nil {
		return 0, rowError
	}
	rows, listError := ListRows(transaction, targetRow.ExpeditionID)
	if listError != nil {
		return targetRow.ExpeditionID, listError
	}
	if targetRow.VisitedAt != "" {
		if _, updateError := UpdateRow(transaction, RowPatch{ID: rowID, LegModeID: &modeID}); updateError != nil {
			return 0, updateError
		}
		previousTimestamp := ""
		for _, candidateRow := range rows {
			if candidateRow.IsWaypoint() && candidateRow.VisitedAt != "" && candidateRow.VisitedAt < targetRow.VisitedAt && candidateRow.VisitedAt > previousTimestamp {
				previousTimestamp = candidateRow.VisitedAt
			}
		}
		if previousTimestamp != "" {
			startISO := addOneSecond(previousTimestamp)
			endISO := addOneSecond(targetRow.VisitedAt)
			if endISO > startISO {
				if windowError := modes.SetWindow(transaction, 0, modeID, startISO, endISO); windowError != nil {
					return targetRow.ExpeditionID, windowError
				}
			}
		}
		return targetRow.ExpeditionID, nil
	}

	latestTimestamp := ""
	for _, candidateRow := range rows {
		if candidateRow.IsWaypoint() && candidateRow.VisitedAt != "" && candidateRow.VisitedAt > latestTimestamp {
			latestTimestamp = candidateRow.VisitedAt
		}
	}
	if latestTimestamp != "" {
		return targetRow.ExpeditionID, modes.SetWindow(transaction, 0, modeID, addOneSecond(latestTimestamp), "")
	}
	return targetRow.ExpeditionID, modes.Switch(transaction, modeID, "")
}

// SetLegModeTx runs SetLegMode inside a database transaction on dbConn.
func SetLegModeTx(dbConn *sql.DB, rowID int64, modeID int64) (int64, error) {
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var setError error
		expeditionID, setError = SetLegMode(transaction, rowID, modeID)
		return setError
	})
	return expeditionID, txError
}

// SetCarrierArrivalOverride manually sets the Fleet Carrier arrival status on rowID
// and cascades the change across preceding (when arrived is true) or subsequent
// (when arrived is false) carrier stops so arrived stops always precede pending stops.
func SetCarrierArrivalOverride(transaction *sql.Tx, rowID int64, arrived bool) (int64, error) {
	targetRow, getError := GetRow(transaction, rowID)
	if getError != nil {
		return 0, getError
	}
	if !targetRow.IsWaypoint() || !targetRow.FCStop {
		return targetRow.ExpeditionID, errors.New("row is not a designated fleet carrier stop")
	}
	lockedSpanshTargetRowID := readLockedSpanshTargetRowID(transaction, targetRow.ExpeditionID)
	now := reference.NowISO()
	if arrived {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 1, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position <= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
		}
		if lockedSpanshTargetRowID > 0 {
			routedRow, routedError := GetRow(transaction, lockedSpanshTargetRowID)
			if routedError == nil && routedRow.FCArrived {
				if clearRouteError := ClearActiveCarrierRoute(transaction, targetRow.ExpeditionID); clearRouteError != nil {
					return targetRow.ExpeditionID, fmt.Errorf("clear active carrier route: %w", clearRouteError)
				}
			}
		}
	} else {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 0, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position >= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
		}
	}
	if touchError := touch(transaction, targetRow.ExpeditionID); touchError != nil {
		return targetRow.ExpeditionID, touchError
	}
	return targetRow.ExpeditionID, nil
}

// SetCarrierArrivalOverrideTx runs SetCarrierArrivalOverride inside a database transaction on dbConn.
func SetCarrierArrivalOverrideTx(dbConn *sql.DB, rowID int64, arrived bool) (int64, error) {
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var overrideError error
		expeditionID, overrideError = SetCarrierArrivalOverride(transaction, rowID, arrived)
		return overrideError
	})
	return expeditionID, txError
}

// MissingPOIRow identifies a waypoint row whose poi_name is still blank.
type MissingPOIRow struct {
	ID            int64
	SystemName    string
	SystemAddress int64
}

// ListMissingPOIRows returns all named waypoint rows without a POI label.
func ListMissingPOIRows(queryer db.Queryer) ([]MissingPOIRow, error) {
	databaseRows, queryError := queryer.Query(
		`SELECT id, system_name, COALESCE(system_address, 0) FROM expedition_rows WHERE row_type = 'waypoint' AND (poi_name IS NULL OR TRIM(poi_name) = '') AND system_name != ''`,
	)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()
	var missingRows []MissingPOIRow
	for databaseRows.Next() {
		var rowItem MissingPOIRow
		if scanError := databaseRows.Scan(&rowItem.ID, &rowItem.SystemName, &rowItem.SystemAddress); scanError != nil {
			return nil, scanError
		}
		missingRows = append(missingRows, rowItem)
	}
	return missingRows, databaseRows.Err()
}

// SetRowPOIIfBlank stores poiName on rowID only when its poi_name column is still empty.
func SetRowPOIIfBlank(queryer db.Queryer, rowID int64, poiName string) error {
	_, execError := queryer.Exec(
		`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`,
		poiName,
		rowID,
	)
	return execError
}
