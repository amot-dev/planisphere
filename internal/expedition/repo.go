// Package expedition implements expedition plans (waypoints and region
// headers), the expedition lifecycle, the waypoint visit evaluator with
// out-of-order auto-reordering, and per-waypoint leg statistics.
package expedition

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/stats"
)

// Expedition statuses.
const (
	StatusPlanning  = "planning"
	StatusActive    = "active"
	StatusCompleted = "completed"
)

// Row types.
const (
	RowWaypoint = "waypoint"
	RowRegion   = "region"
)

// Expedition is an expedition plan and its lifecycle state.
type Expedition struct {
	ID              int64         `json:"id"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	Status          string        `json:"status"`
	StartTimestamp  string        `json:"startTimestamp"`
	EndTimestamp    string        `json:"endTimestamp"`
	ArmedAt         string        `json:"armedAt"`
	BaselineEnabled bool          `json:"baselineEnabled"`
	BaselineSource  string        `json:"baselineSource"`
	Baseline        stats.Metrics `json:"baseline"`
	Adjustments     stats.Metrics `json:"adjustments"`
	CreatedAt       string        `json:"createdAt"`
	UpdatedAt       string        `json:"updatedAt"`
}

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

const expeditionCols = `id, name, description, status, COALESCE(start_timestamp,''), COALESCE(end_timestamp,''),
	COALESCE(armed_at,''), baseline_enabled, baseline_source, baseline_json, adjustments_json, created_at, updated_at`

func scanExpedition(scanner interface{ Scan(...any) error }) (Expedition, error) {
	var expeditionRecord Expedition
	var baselineJSON, adjustmentsJSON string
	scanError := scanner.Scan(&expeditionRecord.ID, &expeditionRecord.Name, &expeditionRecord.Description, &expeditionRecord.Status, &expeditionRecord.StartTimestamp, &expeditionRecord.EndTimestamp, &expeditionRecord.ArmedAt,
		&expeditionRecord.BaselineEnabled, &expeditionRecord.BaselineSource, &baselineJSON, &adjustmentsJSON, &expeditionRecord.CreatedAt, &expeditionRecord.UpdatedAt)
	if scanError != nil {
		return expeditionRecord, scanError
	}
	_ = json.Unmarshal([]byte(baselineJSON), &expeditionRecord.Baseline)
	_ = json.Unmarshal([]byte(adjustmentsJSON), &expeditionRecord.Adjustments)
	return expeditionRecord, nil
}

// List returns all expeditions, newest first.
func List(queryer db.Queryer) ([]Expedition, error) {
	rows, queryError := queryer.Query(`SELECT ` + expeditionCols + ` FROM expeditions ORDER BY created_at DESC, id DESC`)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var expeditions []Expedition
	for rows.Next() {
		expeditionRecord, scanError := scanExpedition(rows)
		if scanError != nil {
			return nil, scanError
		}
		expeditions = append(expeditions, expeditionRecord)
	}
	return expeditions, rows.Err()
}

// Get loads one expedition.
func Get(queryer db.Queryer, expeditionID int64) (Expedition, error) {
	expeditionRecord, queryError := scanExpedition(queryer.QueryRow(`SELECT `+expeditionCols+` FROM expeditions WHERE id = ?`, expeditionID))
	if errors.Is(queryError, sql.ErrNoRows) {
		return expeditionRecord, fmt.Errorf("expedition %d not found", expeditionID)
	}
	return expeditionRecord, queryError
}

// Create adds an expedition in planning status with one empty waypoint row.
func Create(dbConn *sql.DB, name, description string) (Expedition, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Expedition{}, errors.New("expedition name is required")
	}
	now := ed.NowISO()
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		insertResult, insertError := transaction.Exec(`INSERT INTO expeditions(name, description, status, created_at, updated_at) VALUES(?,?,?,?,?)`,
			name, strings.TrimSpace(description), StatusPlanning, now, now)
		if insertError != nil {
			return insertError
		}
		expeditionID, _ = insertResult.LastInsertId()
		_, rowError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at) VALUES(?,1,?,?,?)`,
			expeditionID, RowWaypoint, now, now)
		return rowError
	})
	if txError != nil {
		return Expedition{}, txError
	}
	return Get(dbConn, expeditionID)
}

// UpdateMeta renames an expedition / edits its description.
func UpdateMeta(queryer db.Queryer, expeditionID int64, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("expedition name is required")
	}
	_, execError := queryer.Exec(`UPDATE expeditions SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		name, strings.TrimSpace(description), ed.NowISO(), expeditionID)
	return execError
}

// Delete removes an expedition and its rows. Journal history is untouched.
func Delete(queryer db.Queryer, expeditionID int64) error {
	_, execError := queryer.Exec(`DELETE FROM expeditions WHERE id = ?`, expeditionID)
	return execError
}

// Clone creates a new expedition in planning status copying the source
// expedition's metadata and all of its rows (with visit and carrier-arrival
// state cleared).
func Clone(dbConn *sql.DB, sourceExpeditionID int64) (Expedition, error) {
	sourceExpedition, getError := Get(dbConn, sourceExpeditionID)
	if getError != nil {
		return Expedition{}, getError
	}
	sourceRows, listError := ListRows(dbConn, sourceExpeditionID)
	if listError != nil {
		return Expedition{}, listError
	}
	cloneName := strings.TrimSpace(sourceExpedition.Name) + " (Clone)"
	now := ed.NowISO()
	var newExpeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		insertResult, insertError := transaction.Exec(`INSERT INTO expeditions(name, description, status, created_at, updated_at) VALUES(?,?,?,?,?)`,
			cloneName, sourceExpedition.Description, StatusPlanning, now, now)
		if insertError != nil {
			return insertError
		}
		newExpeditionID, _ = insertResult.LastInsertId()
		if len(sourceRows) == 0 {
			_, seedError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at) VALUES(?,1,?,?,?)`,
				newExpeditionID, RowWaypoint, now, now)
			return seedError
		}
		for i, sourceRow := range sourceRows {
			var regionVal, sysAddrVal, edsmIDVal, xCoord, yCoord, zCoord any
			if sourceRow.RowType == RowRegion && sourceRow.RegionID > 0 {
				regionVal = sourceRow.RegionID
			}
			if sourceRow.SystemAddress != 0 {
				sysAddrVal = sourceRow.SystemAddress
			}
			if sourceRow.EDSMID != 0 {
				edsmIDVal = sourceRow.EDSMID
			}
			if sourceRow.HasCoords {
				xCoord, yCoord, zCoord = sourceRow.X, sourceRow.Y, sourceRow.Z
			}
			if _, rowInsertError := transaction.Exec(`INSERT INTO expedition_rows(
				expedition_id, position, row_type, region_id, fc_stop, fc_arrived, leg_mode_id,
				system_name, poi_name, notes, system_address, edsm_id, x, y, z, coords_source,
				created_at, updated_at
			) VALUES(?,?,?,?,?,0,0,?,?,?,?,?,?,?,?,?,?,?)`,
				newExpeditionID, i+1, sourceRow.RowType, regionVal, sourceRow.FCStop,
				sourceRow.SystemName, sourceRow.POIName, sourceRow.Notes, sysAddrVal, edsmIDVal, xCoord, yCoord, zCoord, sourceRow.CoordsSource,
				now, now); rowInsertError != nil {
				return rowInsertError
			}
		}
		return nil
	})
	if txError != nil {
		return Expedition{}, txError
	}
	return Get(dbConn, newExpeditionID)
}

func setLifecycle(queryer db.Queryer, expeditionID int64, status string, startTimestamp, endTimestamp, armedTimestamp any) error {
	_, execError := queryer.Exec(`UPDATE expeditions SET status = ?, start_timestamp = ?, end_timestamp = ?, armed_at = ?, updated_at = ? WHERE id = ?`,
		status, startTimestamp, endTimestamp, armedTimestamp, ed.NowISO(), expeditionID)
	return execError
}

func clearVisits(queryer db.Queryer, expeditionID int64) error {
	_, execError := queryer.Exec(`UPDATE expedition_rows SET visited_at = NULL, visit_event_uid = NULL, fc_arrived = 0 WHERE expedition_id = ?`, expeditionID)
	return execError
}

// SaveBaseline stores the "before expedition" baseline.
func SaveBaseline(queryer db.Queryer, expeditionID int64, enabled bool, source string, baselineMetrics stats.Metrics) error {
	baselineJSON, _ := json.Marshal(baselineMetrics)
	_, execError := queryer.Exec(`UPDATE expeditions SET baseline_enabled = ?, baseline_source = ?, baseline_json = ?, updated_at = ? WHERE id = ?`,
		enabled, source, string(baselineJSON), ed.NowISO(), expeditionID)
	return execError
}

// SaveAdjustments stores manual metric adjustments (e.g. remote carrier jumps).
func SaveAdjustments(queryer db.Queryer, expeditionID int64, adjustmentMetrics stats.Metrics) error {
	adjustmentsJSON, _ := json.Marshal(adjustmentMetrics)
	_, execError := queryer.Exec(`UPDATE expeditions SET adjustments_json = ?, updated_at = ? WHERE id = ?`, string(adjustmentsJSON), ed.NowISO(), expeditionID)
	return execError
}

// ---- rows -------------------------------------------------------------------

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
	for i := range rows {
		if rows[i].Position == i+1 {
			continue
		}
		rows[i].Position = i + 1
		if _, execError := queryer.Exec(`UPDATE expedition_rows SET position = ? WHERE id = ?`, i+1, rows[i].ID); execError != nil {
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

// InsertRow adds a row after afterRowID (InsertAtEnd / InsertAtTop for the ends).
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
	var regionVal any
	if rowType == RowRegion {
		if _, ok := ed.RegionByID(regionID); !ok {
			regionID = ed.InnerOrionSpurID
		}
		regionVal = regionID
	}
	now := ed.NowISO()
	insertResult, insertError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, region_id, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, expeditionID, insertIndex+1, rowType, regionVal, now, now)
	if insertError != nil {
		return Row{}, insertError
	}
	id, _ := insertResult.LastInsertId()
	newRow, getError := GetRow(transaction, id)
	if getError != nil {
		return Row{}, getError
	}
	ordered := append([]Row{}, rows[:insertIndex]...)
	ordered = append(ordered, newRow)
	ordered = append(ordered, rows[insertIndex:]...)
	for i := range ordered {
		ordered[i].Position = -1 // force rewrite
	}
	if writeError := writePositions(transaction, ordered); writeError != nil {
		return Row{}, writeError
	}
	touch(transaction, expeditionID)
	return GetRow(transaction, id)
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
		name := strings.Join(strings.Fields(*patch.SystemName), " ")
		if name != currentRow.SystemName {
			systemChanged = true
			if _, execError := queryer.Exec(`UPDATE expedition_rows SET system_name = ?, system_address = NULL, edsm_id = NULL,
				x = NULL, y = NULL, z = NULL, coords_source = '', visited_at = NULL, visit_event_uid = NULL, updated_at = ?
				WHERE id = ?`, name, now, patch.ID); execError != nil {
				return false, execError
			}
		}
	}
	touch(queryer, currentRow.ExpeditionID)
	return systemChanged, nil
}

// SetRowCoords stores resolved coordinates if the row still has systemName.
func SetRowCoords(queryer db.Queryer, rowID int64, systemName string, systemAddress, edsmID int64, coords *ed.Vec3, source string) error {
	var xCoord, yCoord, zCoord, addressVal, edsmIDVal any
	if coords != nil {
		xCoord, yCoord, zCoord = coords.X, coords.Y, coords.Z
	}
	if systemAddress != 0 {
		addressVal = systemAddress
	}
	if edsmID != 0 {
		edsmIDVal = edsmID
	}
	_, execError := queryer.Exec(`UPDATE expedition_rows SET system_address = ?, edsm_id = ?, x = ?, y = ?, z = ?, coords_source = ?
		WHERE id = ? AND system_name = ?`, addressVal, edsmIDVal, xCoord, yCoord, zCoord, source, rowID, systemName)
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

// PruneEmptyRows removes all empty waypoints (system_name empty/whitespace)
// and region headers that have only empty or no waypoints after them.
func PruneEmptyRows(transaction *sql.Tx, expeditionID int64) error {
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return listError
	}
	if len(rows) == 0 {
		return nil
	}

	// 1. Determine which waypoints are non-empty.
	isNonEmptyWaypoint := make([]bool, len(rows))
	for i, rowItem := range rows {
		if rowItem.RowType == RowWaypoint && strings.TrimSpace(rowItem.SystemName) != "" {
			isNonEmptyWaypoint[i] = true
		}
	}

	// 2. Identify empty waypoints and headers with 0 non-empty waypoints after them.
	var toDelete []int64
	var surviving []Row

	for i, rowItem := range rows {
		if rowItem.RowType == RowWaypoint {
			if !isNonEmptyWaypoint[i] {
				toDelete = append(toDelete, rowItem.ID)
			} else {
				surviving = append(surviving, rowItem)
			}
			continue
		}

		if rowItem.RowType == RowRegion {
			// Count how many non-empty waypoints follow this header until the next header (or end).
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
				toDelete = append(toDelete, rowItem.ID)
			} else {
				surviving = append(surviving, rowItem)
			}
			continue
		}

		surviving = append(surviving, rowItem)
	}

	if len(toDelete) == 0 {
		return nil
	}

	for _, deleteID := range toDelete {
		if _, deleteError := transaction.Exec(`DELETE FROM expedition_rows WHERE id = ?`, deleteID); deleteError != nil {
			return deleteError
		}
	}

	touch(transaction, expeditionID)
	return writePositions(transaction, surviving)
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

