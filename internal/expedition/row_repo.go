package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
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
func (row Row) Coords() (ed.Vec3, bool) {
	return ed.Vec3{X: row.X, Y: row.Y, Z: row.Z}, row.HasCoords
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
		if _, ok := ed.RegionByID(regionID); !ok {
			regionID = ed.InnerOrionSpurID
		}
		regionValue = regionID
	}
	now := ed.NowISO()
	insertResult, insertError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, region_id, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, expeditionID, insertIndex+1, rowType, regionValue, now, now)
	if insertError != nil {
		return Row{}, insertError
	}
	insertedRowID, _ := insertResult.LastInsertId()
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
	touch(transaction, expeditionID)
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

// UpdateRow applies a patch. systemChanged reports whether the system name
// changed (coordinates are cleared and must be re-resolved).
func UpdateRow(queryer db.Queryer, patch RowPatch) (systemChanged bool, updateError error) {
	currentRow, getError := GetRow(queryer, patch.ID)
	if getError != nil {
		return false, getError
	}
	now := ed.NowISO()
	if patch.FCStop != nil {
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET fc_stop = ?, updated_at = ? WHERE id = ?`, *patch.FCStop, now, patch.ID); execError != nil {
			return false, execError
		}
		if !*patch.FCStop {
			_, _ = queryer.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE id = ?`, patch.ID)
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
		if _, ok := ed.RegionByID(*patch.RegionID); !ok {
			return false, fmt.Errorf("invalid region %d", *patch.RegionID)
		}
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET region_id = ?, updated_at = ? WHERE id = ?`, *patch.RegionID, now, patch.ID); execError != nil {
			return false, execError
		}
	}
	if patch.SystemName != nil && currentRow.RowType == RowWaypoint {
		normalizedSystemName := strings.Join(strings.Fields(*patch.SystemName), " ")
		if normalizedSystemName != currentRow.SystemName {
			systemChanged = true
			if _, execError := queryer.Exec(`UPDATE expedition_rows SET system_name = ?, system_address = NULL, edsm_id = NULL,
				x = NULL, y = NULL, z = NULL, coords_source = '', visited_at = NULL, visit_event_uid = NULL, updated_at = ?
				WHERE id = ?`, normalizedSystemName, now, patch.ID); execError != nil {
				return false, execError
			}
		}
	}
	touch(queryer, currentRow.ExpeditionID)
	return systemChanged, nil
}

// SetRowCoords stores resolved coordinates if the row still has systemName.
func SetRowCoords(queryer db.Queryer, rowID int64, systemName string, systemAddress, edsmID int64, coords *ed.Vec3, source string) error {
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
	if _, deleteError := transaction.Exec(`DELETE FROM expedition_rows WHERE id = ?`, rowID); deleteError != nil {
		return 0, deleteError
	}
	rows, listError := ListRows(transaction, rowItem.ExpeditionID)
	if listError != nil {
		return 0, listError
	}
	touch(transaction, rowItem.ExpeditionID)
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

	touch(transaction, expeditionID)
	return writePositions(transaction, survivingRows)
}

// MoveRow moves a row to the zero-based index toIndex.
func MoveRow(transaction *sql.Tx, rowID int64, toIndex int) (int64, error) {
	rowItem, getError := GetRow(transaction, rowID)
	if getError != nil {
		return 0, getError
	}
	rows, listError := ListRows(transaction, rowItem.ExpeditionID)
	if listError != nil {
		return 0, listError
	}
	rows = moveTo(rows, indexOf(rows, rowID), toIndex)
	touch(transaction, rowItem.ExpeditionID)
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

// moveTo moves rows[fromIndex] so that it ends up at index toIndex.
func moveTo(rows []Row, fromIndex, toIndex int) []Row {
	if fromIndex < 0 || fromIndex >= len(rows) {
		return rows
	}
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex >= len(rows) {
		toIndex = len(rows) - 1
	}
	if fromIndex == toIndex {
		return rows
	}
	targetItem := rows[fromIndex]
	reordered := make([]Row, 0, len(rows))
	reordered = append(reordered, rows[:fromIndex]...)
	reordered = append(reordered, rows[fromIndex+1:]...)
	reordered = append(reordered[:toIndex], append([]Row{targetItem}, reordered[toIndex:]...)...)
	return reordered
}

func touch(queryer db.Queryer, expeditionID int64) {
	_, _ = queryer.Exec(`UPDATE expeditions SET updated_at = ? WHERE id = ?`, ed.NowISO(), expeditionID)
}

