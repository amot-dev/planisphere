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

// Expedition is an expedition plan and its lifecycle state.
type Expedition struct {
	ID                  int64         `json:"id"`
	Name                string        `json:"name"`
	Description         string        `json:"description"`
	Status              string        `json:"status"`
	StartTimestamp      string        `json:"startTimestamp"`
	EndTimestamp        string        `json:"endTimestamp"`
	ArmedAt             string        `json:"armedAt"`
	BaselineEnabled     bool          `json:"baselineEnabled"`
	BaselineSource      string        `json:"baselineSource"`
	Baseline            stats.Metrics `json:"baseline"`
	Adjustments         stats.Metrics `json:"adjustments"`
	CarrierType         string        `json:"carrierType"`
	CarrierWeeklyUpkeep int64         `json:"carrierWeeklyUpkeep"`
	CreatedAt           string        `json:"createdAt"`
	UpdatedAt           string        `json:"updatedAt"`
}

const expeditionCols = `id, name, description, status, COALESCE(start_timestamp,''), COALESCE(end_timestamp,''),
	COALESCE(armed_at,''), baseline_enabled, baseline_source, baseline_json, adjustments_json,
	COALESCE(carrier_type,'player'), COALESCE(carrier_weekly_upkeep,0), created_at, updated_at`

func scanExpedition(scanner interface{ Scan(...any) error }) (Expedition, error) {
	var expeditionRecord Expedition
	var baselineJSON, adjustmentsJSON string
	scanError := scanner.Scan(&expeditionRecord.ID, &expeditionRecord.Name, &expeditionRecord.Description, &expeditionRecord.Status, &expeditionRecord.StartTimestamp, &expeditionRecord.EndTimestamp, &expeditionRecord.ArmedAt,
		&expeditionRecord.BaselineEnabled, &expeditionRecord.BaselineSource, &baselineJSON, &adjustmentsJSON, &expeditionRecord.CarrierType, &expeditionRecord.CarrierWeeklyUpkeep, &expeditionRecord.CreatedAt, &expeditionRecord.UpdatedAt)
	if scanError != nil {
		return expeditionRecord, scanError
	}
	if expeditionRecord.CarrierType == "" {
		expeditionRecord.CarrierType = "player"
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

// UpdateMeta renames an expedition or edits its description.
func UpdateMeta(queryer db.Queryer, expeditionID int64, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("expedition name is required")
	}
	_, execError := queryer.Exec(`UPDATE expeditions SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		name, strings.TrimSpace(description), ed.NowISO(), expeditionID)
	return execError
}

// UpdateCarrierConfig updates the expedition's vessel classification ("player" or "squadron")
// and static weekly upkeep cost in credits.
func UpdateCarrierConfig(queryer db.Queryer, expeditionID int64, carrierType string, weeklyUpkeep int64) error {
	normalizedCarrierType := "player"
	if strings.EqualFold(strings.TrimSpace(carrierType), "squadron") {
		normalizedCarrierType = "squadron"
	}
	if weeklyUpkeep < 0 {
		weeklyUpkeep = 0
	}
	_, execError := queryer.Exec(`UPDATE expeditions SET carrier_type = ?, carrier_weekly_upkeep = ?, updated_at = ? WHERE id = ?`,
		normalizedCarrierType, weeklyUpkeep, ed.NowISO(), expeditionID)
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
		insertResult, insertError := transaction.Exec(`INSERT INTO expeditions(name, description, status, carrier_type, carrier_weekly_upkeep, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
			cloneName, sourceExpedition.Description, StatusPlanning, sourceExpedition.CarrierType, sourceExpedition.CarrierWeeklyUpkeep, now, now)
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
			var regionValue, systemAddressValue, edsmIDValue, xCoord, yCoord, zCoord any
			if sourceRow.RowType == RowRegion && sourceRow.RegionID > 0 {
				regionValue = sourceRow.RegionID
			}
			if sourceRow.SystemAddress != 0 {
				systemAddressValue = sourceRow.SystemAddress
			}
			if sourceRow.EDSMID != 0 {
				edsmIDValue = sourceRow.EDSMID
			}
			if sourceRow.HasCoords {
				xCoord, yCoord, zCoord = sourceRow.X, sourceRow.Y, sourceRow.Z
			}
			if _, rowInsertError := transaction.Exec(`INSERT INTO expedition_rows(
				expedition_id, position, row_type, region_id, fc_stop, fc_arrived, leg_mode_id,
				system_name, poi_name, notes, system_address, edsm_id, x, y, z, coords_source,
				created_at, updated_at
			) VALUES(?,?,?,?,?,0,0,?,?,?,?,?,?,?,?,?,?,?)`,
				newExpeditionID, i+1, sourceRow.RowType, regionValue, sourceRow.FCStop,
				sourceRow.SystemName, sourceRow.POIName, sourceRow.Notes, systemAddressValue, edsmIDValue, xCoord, yCoord, zCoord, sourceRow.CoordsSource,
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

