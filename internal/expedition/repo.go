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
func (r Row) Coords() (ed.Vec3, bool) {
	return ed.Vec3{X: r.X, Y: r.Y, Z: r.Z}, r.HasCoords
}

// IsWaypoint reports whether the row is a waypoint.
func (r Row) IsWaypoint() bool { return r.RowType == RowWaypoint }

const expeditionCols = `id, name, description, status, COALESCE(start_timestamp,''), COALESCE(end_timestamp,''),
	COALESCE(armed_at,''), baseline_enabled, baseline_source, baseline_json, adjustments_json, created_at, updated_at`

func scanExpedition(sc interface{ Scan(...any) error }) (Expedition, error) {
	var e Expedition
	var baseline, adj string
	err := sc.Scan(&e.ID, &e.Name, &e.Description, &e.Status, &e.StartTimestamp, &e.EndTimestamp, &e.ArmedAt,
		&e.BaselineEnabled, &e.BaselineSource, &baseline, &adj, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return e, err
	}
	_ = json.Unmarshal([]byte(baseline), &e.Baseline)
	_ = json.Unmarshal([]byte(adj), &e.Adjustments)
	return e, nil
}

// List returns all expeditions, newest first.
func List(q db.Queryer) ([]Expedition, error) {
	rs, err := q.Query(`SELECT ` + expeditionCols + ` FROM expeditions ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []Expedition
	for rs.Next() {
		e, err := scanExpedition(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rs.Err()
}

// Get loads one expedition.
func Get(q db.Queryer, id int64) (Expedition, error) {
	e, err := scanExpedition(q.QueryRow(`SELECT `+expeditionCols+` FROM expeditions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("expedition %d not found", id)
	}
	return e, err
}

// Create adds an expedition in planning status with one empty waypoint row.
func Create(conn *sql.DB, name, description string) (Expedition, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Expedition{}, errors.New("expedition name is required")
	}
	now := ed.NowISO()
	var id int64
	err := db.WithTx(conn, func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO expeditions(name, description, status, created_at, updated_at) VALUES(?,?,?,?,?)`,
			name, strings.TrimSpace(description), StatusPlanning, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		_, err = tx.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at) VALUES(?,1,?,?,?)`,
			id, RowWaypoint, now, now)
		return err
	})
	if err != nil {
		return Expedition{}, err
	}
	return Get(conn, id)
}

// UpdateMeta renames an expedition / edits its description.
func UpdateMeta(q db.Queryer, id int64, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("expedition name is required")
	}
	_, err := q.Exec(`UPDATE expeditions SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		name, strings.TrimSpace(description), ed.NowISO(), id)
	return err
}

// Delete removes an expedition and its rows. Journal history is untouched.
func Delete(q db.Queryer, id int64) error {
	_, err := q.Exec(`DELETE FROM expeditions WHERE id = ?`, id)
	return err
}

// Clone creates a new expedition in planning status copying the source
// expedition's metadata and all of its rows (with visit and carrier-arrival
// state cleared).
func Clone(conn *sql.DB, sourceID int64) (Expedition, error) {
	src, err := Get(conn, sourceID)
	if err != nil {
		return Expedition{}, err
	}
	srcRows, err := ListRows(conn, sourceID)
	if err != nil {
		return Expedition{}, err
	}
	cloneName := strings.TrimSpace(src.Name) + " (Clone)"
	now := ed.NowISO()
	var newID int64
	err = db.WithTx(conn, func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO expeditions(name, description, status, created_at, updated_at) VALUES(?,?,?,?,?)`,
			cloneName, src.Description, StatusPlanning, now, now)
		if err != nil {
			return err
		}
		newID, _ = res.LastInsertId()
		if len(srcRows) == 0 {
			_, err = tx.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at) VALUES(?,1,?,?,?)`,
				newID, RowWaypoint, now, now)
			return err
		}
		for i, r := range srcRows {
			var reg, sysAddr, edsmID, x, y, z any
			if r.RowType == RowRegion && r.RegionID > 0 {
				reg = r.RegionID
			}
			if r.SystemAddress != 0 {
				sysAddr = r.SystemAddress
			}
			if r.EDSMID != 0 {
				edsmID = r.EDSMID
			}
			if r.HasCoords {
				x, y, z = r.X, r.Y, r.Z
			}
			if _, err := tx.Exec(`INSERT INTO expedition_rows(
				expedition_id, position, row_type, region_id, fc_stop, fc_arrived, leg_mode_id,
				system_name, poi_name, notes, system_address, edsm_id, x, y, z, coords_source,
				created_at, updated_at
			) VALUES(?,?,?,?,?,0,0,?,?,?,?,?,?,?,?,?,?,?)`,
				newID, i+1, r.RowType, reg, r.FCStop,
				r.SystemName, r.POIName, r.Notes, sysAddr, edsmID, x, y, z, r.CoordsSource,
				now, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Expedition{}, err
	}
	return Get(conn, newID)
}

func setLifecycle(q db.Queryer, id int64, status string, start, end, armed any) error {
	_, err := q.Exec(`UPDATE expeditions SET status = ?, start_timestamp = ?, end_timestamp = ?, armed_at = ?, updated_at = ? WHERE id = ?`,
		status, start, end, armed, ed.NowISO(), id)
	return err
}

func clearVisits(q db.Queryer, id int64) error {
	_, err := q.Exec(`UPDATE expedition_rows SET visited_at = NULL, visit_event_uid = NULL, fc_arrived = 0 WHERE expedition_id = ?`, id)
	return err
}

// SaveBaseline stores the "before expedition" baseline.
func SaveBaseline(q db.Queryer, id int64, enabled bool, source string, m stats.Metrics) error {
	b, _ := json.Marshal(m)
	_, err := q.Exec(`UPDATE expeditions SET baseline_enabled = ?, baseline_source = ?, baseline_json = ?, updated_at = ? WHERE id = ?`,
		enabled, source, string(b), ed.NowISO(), id)
	return err
}

// SaveAdjustments stores manual metric adjustments (e.g. remote carrier jumps).
func SaveAdjustments(q db.Queryer, id int64, m stats.Metrics) error {
	b, _ := json.Marshal(m)
	_, err := q.Exec(`UPDATE expeditions SET adjustments_json = ?, updated_at = ? WHERE id = ?`, string(b), ed.NowISO(), id)
	return err
}

// ---- rows -------------------------------------------------------------------

const rowCols = `id, expedition_id, position, row_type, COALESCE(region_id, 0), fc_stop, COALESCE(fc_arrived, 0), COALESCE(leg_mode_id, 0),
	system_name, poi_name, notes, COALESCE(system_address, 0), COALESCE(edsm_id, 0), x, y, z, coords_source, COALESCE(visited_at, ''), COALESCE(visit_event_uid, '')`

func scanRow(sc interface{ Scan(...any) error }) (Row, error) {
	var r Row
	var x, y, z sql.NullFloat64
	err := sc.Scan(&r.ID, &r.ExpeditionID, &r.Position, &r.RowType, &r.RegionID, &r.FCStop, &r.FCArrived, &r.LegModeID, &r.SystemName, &r.POIName,
		&r.Notes, &r.SystemAddress, &r.EDSMID, &x, &y, &z, &r.CoordsSource, &r.VisitedAt, &r.VisitEventUID)
	if err != nil {
		return r, err
	}
	if x.Valid && y.Valid && z.Valid {
		r.HasCoords, r.X, r.Y, r.Z = true, x.Float64, y.Float64, z.Float64
	}
	return r, nil
}

// ListRows returns an expedition's rows in display order.
func ListRows(q db.Queryer, expID int64) ([]Row, error) {
	rs, err := q.Query(`SELECT `+rowCols+` FROM expedition_rows WHERE expedition_id = ? ORDER BY position, id`, expID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []Row
	for rs.Next() {
		r, err := scanRow(rs)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

// GetRow loads a single row.
func GetRow(q db.Queryer, rowID int64) (Row, error) {
	r, err := scanRow(q.QueryRow(`SELECT `+rowCols+` FROM expedition_rows WHERE id = ?`, rowID))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("row %d not found", rowID)
	}
	return r, err
}

// writePositions persists the order of rows as positions 1..n.
func writePositions(q db.Queryer, rows []Row) error {
	for i := range rows {
		if rows[i].Position == i+1 {
			continue
		}
		rows[i].Position = i + 1
		if _, err := q.Exec(`UPDATE expedition_rows SET position = ? WHERE id = ?`, i+1, rows[i].ID); err != nil {
			return err
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
func InsertRow(tx *sql.Tx, expID, afterRowID int64, rowType string, regionID int) (Row, error) {
	if rowType != RowWaypoint && rowType != RowRegion {
		return Row{}, fmt.Errorf("invalid row type %q", rowType)
	}
	rows, err := ListRows(tx, expID)
	if err != nil {
		return Row{}, err
	}
	idx := len(rows)
	switch afterRowID {
	case InsertAtEnd:
	case InsertAtTop:
		idx = 0
	default:
		found := false
		for i, r := range rows {
			if r.ID == afterRowID {
				idx, found = i+1, true
				break
			}
		}
		if !found {
			return Row{}, fmt.Errorf("row %d not found", afterRowID)
		}
	}
	var region any
	if rowType == RowRegion {
		if _, ok := ed.RegionByID(regionID); !ok {
			regionID = ed.InnerOrionSpurID
		}
		region = regionID
	}
	now := ed.NowISO()
	res, err := tx.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, region_id, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, expID, idx+1, rowType, region, now, now)
	if err != nil {
		return Row{}, err
	}
	id, _ := res.LastInsertId()
	nr, err := GetRow(tx, id)
	if err != nil {
		return Row{}, err
	}
	ordered := append([]Row{}, rows[:idx]...)
	ordered = append(ordered, nr)
	ordered = append(ordered, rows[idx:]...)
	for i := range ordered {
		ordered[i].Position = -1 // force rewrite
	}
	if err := writePositions(tx, ordered); err != nil {
		return Row{}, err
	}
	touch(tx, expID)
	return GetRow(tx, id)
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
func UpdateRow(q db.Queryer, p RowPatch) (systemChanged bool, err error) {
	cur, err := GetRow(q, p.ID)
	if err != nil {
		return false, err
	}
	now := ed.NowISO()
	if p.FCStop != nil {
		if _, err := q.Exec(`UPDATE expedition_rows SET fc_stop = ?, updated_at = ? WHERE id = ?`, *p.FCStop, now, p.ID); err != nil {
			return false, err
		}
		if !*p.FCStop {
			_, _ = q.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE id = ?`, p.ID)
		}
	}
	if p.FCArrived != nil {
		if _, err := q.Exec(`UPDATE expedition_rows SET fc_arrived = ?, updated_at = ? WHERE id = ?`, *p.FCArrived, now, p.ID); err != nil {
			return false, err
		}
	}
	if p.LegModeID != nil {
		if _, err := q.Exec(`UPDATE expedition_rows SET leg_mode_id = ?, updated_at = ? WHERE id = ?`, *p.LegModeID, now, p.ID); err != nil {
			return false, err
		}
	}
	if p.POIName != nil {
		if _, err := q.Exec(`UPDATE expedition_rows SET poi_name = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(*p.POIName), now, p.ID); err != nil {
			return false, err
		}
	}
	if p.Notes != nil {
		if _, err := q.Exec(`UPDATE expedition_rows SET notes = ?, updated_at = ? WHERE id = ?`, *p.Notes, now, p.ID); err != nil {
			return false, err
		}
	}
	if p.RegionID != nil && cur.RowType == RowRegion {
		if _, ok := ed.RegionByID(*p.RegionID); !ok {
			return false, fmt.Errorf("invalid region %d", *p.RegionID)
		}
		if _, err := q.Exec(`UPDATE expedition_rows SET region_id = ?, updated_at = ? WHERE id = ?`, *p.RegionID, now, p.ID); err != nil {
			return false, err
		}
	}
	if p.SystemName != nil && cur.RowType == RowWaypoint {
		name := strings.Join(strings.Fields(*p.SystemName), " ")
		if name != cur.SystemName {
			systemChanged = true
			if _, err := q.Exec(`UPDATE expedition_rows SET system_name = ?, system_address = NULL, edsm_id = NULL,
				x = NULL, y = NULL, z = NULL, coords_source = '', visited_at = NULL, visit_event_uid = NULL, updated_at = ?
				WHERE id = ?`, name, now, p.ID); err != nil {
				return false, err
			}
		}
	}
	touch(q, cur.ExpeditionID)
	return systemChanged, nil
}

// SetRowCoords stores resolved coordinates if the row still has systemName.
func SetRowCoords(q db.Queryer, rowID int64, systemName string, addr, edsmID int64, pos *ed.Vec3, source string) error {
	var x, y, z, a, e any
	if pos != nil {
		x, y, z = pos.X, pos.Y, pos.Z
	}
	if addr != 0 {
		a = addr
	}
	if edsmID != 0 {
		e = edsmID
	}
	_, err := q.Exec(`UPDATE expedition_rows SET system_address = ?, edsm_id = ?, x = ?, y = ?, z = ?, coords_source = ?
		WHERE id = ? AND system_name = ?`, a, e, x, y, z, source, rowID, systemName)
	return err
}

// DeleteRow removes a row and re-indexes positions.
func DeleteRow(tx *sql.Tx, rowID int64) (int64, error) {
	r, err := GetRow(tx, rowID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM expedition_rows WHERE id = ?`, rowID); err != nil {
		return 0, err
	}
	rows, err := ListRows(tx, r.ExpeditionID)
	if err != nil {
		return 0, err
	}
	touch(tx, r.ExpeditionID)
	return r.ExpeditionID, writePositions(tx, rows)
}

// PruneEmptyRows removes all empty waypoints (system_name empty/whitespace)
// and region headers that have only empty or no waypoints after them.
func PruneEmptyRows(tx *sql.Tx, expID int64) error {
	rows, err := ListRows(tx, expID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	// 1. Determine which waypoints are non-empty.
	isNonEmptyWaypoint := make([]bool, len(rows))
	for i, r := range rows {
		if r.RowType == RowWaypoint && strings.TrimSpace(r.SystemName) != "" {
			isNonEmptyWaypoint[i] = true
		}
	}

	// 2. Identify empty waypoints and headers with 0 non-empty waypoints after them.
	var toDelete []int64
	var surviving []Row

	for i, r := range rows {
		if r.RowType == RowWaypoint {
			if !isNonEmptyWaypoint[i] {
				toDelete = append(toDelete, r.ID)
			} else {
				surviving = append(surviving, r)
			}
			continue
		}

		if r.RowType == RowRegion {
			// Count how many non-empty waypoints follow this header until the next header (or end).
			nonEmptyCount := 0
			for j := i + 1; j < len(rows); j++ {
				if rows[j].RowType == RowRegion {
					break
				}
				if isNonEmptyWaypoint[j] {
					nonEmptyCount++
				}
			}
			if nonEmptyCount == 0 {
				toDelete = append(toDelete, r.ID)
			} else {
				surviving = append(surviving, r)
			}
			continue
		}

		surviving = append(surviving, r)
	}

	if len(toDelete) == 0 {
		return nil
	}

	for _, id := range toDelete {
		if _, err := tx.Exec(`DELETE FROM expedition_rows WHERE id = ?`, id); err != nil {
			return err
		}
	}

	touch(tx, expID)
	return writePositions(tx, surviving)
}

// MoveRow moves a row to the zero-based index toIndex.
func MoveRow(tx *sql.Tx, rowID int64, toIndex int) (int64, error) {
	r, err := GetRow(tx, rowID)
	if err != nil {
		return 0, err
	}
	rows, err := ListRows(tx, r.ExpeditionID)
	if err != nil {
		return 0, err
	}
	rows = moveTo(rows, indexOf(rows, rowID), toIndex)
	touch(tx, r.ExpeditionID)
	return r.ExpeditionID, writePositions(tx, rows)
}

func indexOf(rows []Row, id int64) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

// moveTo moves rows[from] so that it ends up at index to.
func moveTo(rows []Row, from, to int) []Row {
	if from < 0 || from >= len(rows) {
		return rows
	}
	if to < 0 {
		to = 0
	}
	if to >= len(rows) {
		to = len(rows) - 1
	}
	if from == to {
		return rows
	}
	item := rows[from]
	out := make([]Row, 0, len(rows))
	out = append(out, rows[:from]...)
	out = append(out, rows[from+1:]...)
	out = append(out[:to], append([]Row{item}, out[to:]...)...)
	return out
}

func touch(q db.Queryer, expID int64) {
	_, _ = q.Exec(`UPDATE expeditions SET updated_at = ? WHERE id = ?`, ed.NowISO(), expID)
}

