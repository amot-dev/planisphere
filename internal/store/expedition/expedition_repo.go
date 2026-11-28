// Package expedition implements expedition plans (waypoints and region
// headers), the expedition lifecycle, the sequential waypoint visit evaluator,
// and per-waypoint leg statistics.
package expedition

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/modes"
)

// CarrierStats stores manual Fleet Carrier jump, distance, and tritium totals recorded for an expedition.
type CarrierStats struct {
	CarrierJumps      int     `json:"carrierJumps"`
	CarrierDistanceLy float64 `json:"carrierDistanceLy"`
	TritiumUsed       *int    `json:"tritiumUsed"`
}

// ToMetrics converts CarrierStats into a replay/stats Metrics struct holding only carrier fields.
func (carrierStats CarrierStats) ToMetrics() replaystats.Metrics {
	return replaystats.Metrics{
		CarrierJumps:      carrierStats.CarrierJumps,
		CarrierDistanceLy: carrierStats.CarrierDistanceLy,
		TritiumUsed:       carrierStats.TritiumUsed,
	}
}

// ApplyTo merges CarrierStats onto targetMetrics in place.
func (carrierStats CarrierStats) ApplyTo(targetMetrics *replaystats.Metrics) {
	if targetMetrics == nil {
		return
	}
	*targetMetrics = targetMetrics.Add(carrierStats.ToMetrics())
}

// Projections bundles the Bus-backed read-only projections required by expedition planner and drawer builders.
type Projections struct {
	Arrivals       *projections.SystemArrivals
	VisitCounts    *projections.SystemVisitCounts
	ShipHistory    *projections.ShipHistory
	TimeRangeStats *stats.TimeRangeStats
	SalesLedger    *realization.SalesLedger
	RecoveredJumps *projections.RecoveredJumps
}

// Expedition statuses.
const (
	StatusPlanning  = "planning"
	StatusActive    = "active"
	StatusCompleted = "completed"
)

// Expedition is an expedition plan and its lifecycle state.
type Expedition struct {
	ID                  int64        `json:"id"`
	Name                string       `json:"name"`
	Description         string       `json:"description"`
	Status              string       `json:"status"`
	StartTimestamp      string       `json:"startTimestamp"`
	EndTimestamp        string       `json:"endTimestamp"`
	Adjustments         CarrierStats `json:"adjustments"`
	CarrierType         string       `json:"carrierType"`
	CarrierWeeklyUpkeep int64        `json:"carrierWeeklyUpkeep"`
	ActiveModeID        int64        `json:"activeModeId"`
	CommanderProfileID  int64        `json:"commanderProfileId"`
	CreatedAt           string       `json:"createdAt"`
	UpdatedAt           string       `json:"updatedAt"`
}

const expeditionCols = `id, name, description, status, COALESCE(start_timestamp,''), COALESCE(end_timestamp,''),
	adjustments_json, COALESCE(carrier_type,'player'), COALESCE(carrier_weekly_upkeep,0), COALESCE(active_mode_id,0), COALESCE(commander_profile_id,0), created_at, updated_at`

func scanExpedition(scanner interface{ Scan(...any) error }) (Expedition, error) {
	var expeditionRecord Expedition
	var adjustmentsJSON string
	scanError := scanner.Scan(&expeditionRecord.ID, &expeditionRecord.Name, &expeditionRecord.Description, &expeditionRecord.Status, &expeditionRecord.StartTimestamp, &expeditionRecord.EndTimestamp,
		&adjustmentsJSON, &expeditionRecord.CarrierType, &expeditionRecord.CarrierWeeklyUpkeep, &expeditionRecord.ActiveModeID, &expeditionRecord.CommanderProfileID, &expeditionRecord.CreatedAt, &expeditionRecord.UpdatedAt)
	if scanError != nil {
		return expeditionRecord, scanError
	}
	if expeditionRecord.Status == StatusActive && expeditionRecord.StartTimestamp == "" {
		expeditionRecord.Status = StatusPlanning
	}
	if expeditionRecord.CarrierType == "" {
		expeditionRecord.CarrierType = "player"
	}
	if expeditionRecord.CommanderProfileID < 0 {
		expeditionRecord.CommanderProfileID = 0
	}
	if trimmedAdjustments := strings.TrimSpace(adjustmentsJSON); trimmedAdjustments != "" && trimmedAdjustments != "{}" {
		if unmarshalError := json.Unmarshal([]byte(trimmedAdjustments), &expeditionRecord.Adjustments); unmarshalError != nil {
			return expeditionRecord, fmt.Errorf("decode expedition %d adjustments: %w", expeditionRecord.ID, unmarshalError)
		}
	}
	return expeditionRecord, nil
}

// List returns all expeditions with planning expeditions first (most recently
// created first), followed by active and completed expeditions ordered by
// expedition start timestamp (most recent first).
func List(queryer db.Queryer) ([]Expedition, error) {
	rows, queryError := queryer.Query(`SELECT ` + expeditionCols + ` FROM expeditions
		ORDER BY
			CASE
				WHEN status = 'planning' OR (status = 'active' AND COALESCE(start_timestamp, '') = '') THEN 0
				ELSE 1
			END ASC,
			CASE
				WHEN status = 'planning' OR (status = 'active' AND COALESCE(start_timestamp, '') = '') THEN created_at
				ELSE COALESCE(start_timestamp, '')
			END DESC,
			created_at DESC,
			id DESC`)
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

// EffectiveStart resolves an expedition's telemetry start from Origin Waypoint #1's
// departure timestamp. queryAfter is an exclusive lower bound placed 1 second
// before departed_at (clamped at visitedAt) so half-open (after, until] queries
// include the departure jump, while displayStart is the exact departed_at
// timestamp for duration and upkeep calculations.
func EffectiveStart(queryer db.Queryer, expeditionRecord Expedition) (queryAfter, displayStart string, departed bool, loadError error) {
	if expeditionRecord.StartTimestamp == "" {
		return "", "", false, nil
	}
	var visitedAt, departedAt string
	queryError := queryer.QueryRow(
		`SELECT COALESCE(visited_at, ''), COALESCE(departed_at, '')
		 FROM expedition_rows
		 WHERE expedition_id = ? AND row_type = 'waypoint'
		 ORDER BY position, id LIMIT 1`,
		expeditionRecord.ID,
	).Scan(&visitedAt, &departedAt)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if queryError != nil {
		return "", "", false, fmt.Errorf("load origin departure for expedition %d: %w", expeditionRecord.ID, queryError)
	}
	if departedAt == "" {
		return "", "", false, nil
	}
	return exclusiveBoundaryBefore(departedAt, visitedAt), departedAt, true, nil
}

// SeedWaypoint describes a waypoint row to seed when creating a new expedition,
// optionally carrying pre-resolved local coordinates and GEC POI metadata.
type SeedWaypoint struct {
	SystemName    string
	POIName       string
	SystemAddress int64
	EDSMID        int64
	HasCoords     bool
	X             float64
	Y             float64
	Z             float64
	CoordsSource  string
}

func normalizeSeedWaypoints(waypoints []SeedWaypoint) []SeedWaypoint {
	normalizedWaypoints := make([]SeedWaypoint, 0, len(waypoints))
	for _, waypoint := range waypoints {
		normalizedSystemName := strings.Join(strings.Fields(waypoint.SystemName), " ")
		if normalizedSystemName == "" {
			continue
		}
		waypoint.SystemName = normalizedSystemName
		waypoint.POIName = strings.TrimSpace(waypoint.POIName)
		normalizedWaypoints = append(normalizedWaypoints, waypoint)
	}
	return normalizedWaypoints
}

func insertSeedWaypoints(transaction *sql.Tx, expeditionID int64, waypoints []SeedWaypoint, timestamp string) error {
	if len(waypoints) == 0 {
		_, rowError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at) VALUES(?,1,?,?,?)`,
			expeditionID, RowWaypoint, timestamp, timestamp)
		return rowError
	}
	rowInsertStatement, prepareError := transaction.Prepare(`INSERT INTO expedition_rows(
		expedition_id, position, row_type, system_name, poi_name, system_address, edsm_id,
		x, y, z, coords_source, created_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if prepareError != nil {
		return fmt.Errorf("prepare seed waypoint insert: %w", prepareError)
	}
	defer rowInsertStatement.Close()

	for index, waypoint := range waypoints {
		var systemAddressValue, edsmIDValue, xCoord, yCoord, zCoord any
		if waypoint.SystemAddress != 0 {
			systemAddressValue = waypoint.SystemAddress
		}
		if waypoint.EDSMID != 0 {
			edsmIDValue = waypoint.EDSMID
		}
		if waypoint.HasCoords {
			xCoord, yCoord, zCoord = waypoint.X, waypoint.Y, waypoint.Z
		}
		if _, insertError := rowInsertStatement.Exec(
			expeditionID, index+1, RowWaypoint, waypoint.SystemName, waypoint.POIName,
			systemAddressValue, edsmIDValue, xCoord, yCoord, zCoord, waypoint.CoordsSource,
			timestamp, timestamp,
		); insertError != nil {
			return fmt.Errorf("insert seed waypoint %d: %w", index+1, insertError)
		}
	}
	return nil
}

// CreateWithWaypoints adds an expedition in planning status seeded with waypoints
// (or one empty waypoint row when waypoints has no non-empty entries).
func CreateWithWaypoints(dbConn *sql.DB, name, description string, waypoints []SeedWaypoint) (Expedition, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Expedition{}, errors.New("expedition name is required")
	}
	normalizedWaypoints := normalizeSeedWaypoints(waypoints)
	now := reference.NowISO()
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		defaultModeID := modes.DefaultFallbackModeID(transaction)
		commanderProfileID, profileError := db.LatestCommanderProfileID(transaction)
		if profileError != nil {
			return profileError
		}
		insertResult, insertError := transaction.Exec(`INSERT INTO expeditions(name, description, status, active_mode_id, commander_profile_id, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
			name, strings.TrimSpace(description), StatusPlanning, defaultModeID, commanderProfileID, now, now)
		if insertError != nil {
			return insertError
		}
		var idError error
		expeditionID, idError = insertResult.LastInsertId()
		if idError != nil {
			return fmt.Errorf("read created expedition id: %w", idError)
		}
		return insertSeedWaypoints(transaction, expeditionID, normalizedWaypoints, now)
	})
	if txError != nil {
		return Expedition{}, txError
	}
	return Get(dbConn, expeditionID)
}

// Create adds an expedition in planning status with one empty waypoint row.
func Create(dbConn *sql.DB, name, description string) (Expedition, error) {
	return CreateWithWaypoints(dbConn, name, description, nil)
}

// UpdateMeta renames an expedition or edits its description.
func UpdateMeta(queryer db.Queryer, expeditionID int64, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("expedition name is required")
	}
	_, execError := queryer.Exec(`UPDATE expeditions SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		name, strings.TrimSpace(description), reference.NowISO(), expeditionID)
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
		normalizedCarrierType, weeklyUpkeep, reference.NowISO(), expeditionID)
	return execError
}

// Delete removes an expedition, its rows, and any active carrier route. Journal history is untouched.
func Delete(queryer db.Queryer, expeditionID int64) error {
	deleteExpedition := func(targetQueryer db.Queryer) error {
		if _, execError := targetQueryer.Exec(`DELETE FROM expeditions WHERE id = ?`, expeditionID); execError != nil {
			return execError
		}
		if clearError := ClearActiveCarrierRoute(targetQueryer, expeditionID); clearError != nil {
			return clearError
		}
		return nil
	}
	if dbConn, isDB := queryer.(*sql.DB); isDB {
		return db.WithTx(dbConn, func(transaction *sql.Tx) error {
			return deleteExpedition(transaction)
		})
	}
	return deleteExpedition(queryer)
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
	now := reference.NowISO()
	var newExpeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		defaultModeID := modes.DefaultFallbackModeID(transaction)
		clonedProfileID, profileError := resolveValidOrLatestCommanderProfileID(transaction, sourceExpedition.CommanderProfileID)
		if profileError != nil {
			return profileError
		}
		insertResult, insertError := transaction.Exec(`INSERT INTO expeditions(name, description, status, carrier_type, carrier_weekly_upkeep, active_mode_id, commander_profile_id, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			cloneName, sourceExpedition.Description, StatusPlanning, sourceExpedition.CarrierType, sourceExpedition.CarrierWeeklyUpkeep, defaultModeID, clonedProfileID, now, now)
		if insertError != nil {
			return insertError
		}
		var idError error
		newExpeditionID, idError = insertResult.LastInsertId()
		if idError != nil {
			return fmt.Errorf("read cloned expedition id: %w", idError)
		}
		if len(sourceRows) == 0 {
			_, seedError := transaction.Exec(`INSERT INTO expedition_rows(expedition_id, position, row_type, created_at, updated_at) VALUES(?,1,?,?,?)`,
				newExpeditionID, RowWaypoint, now, now)
			return seedError
		}
		rowInsertStmt, prepareError := transaction.Prepare(`INSERT INTO expedition_rows(
			expedition_id, position, row_type, region_id, fc_stop, fc_arrived, leg_mode_id,
			system_name, poi_name, notes, system_address, edsm_id, x, y, z, coords_source,
			created_at, updated_at
		) VALUES(?,?,?,?,?,0,0,?,?,?,?,?,?,?,?,?,?,?)`)
		if prepareError != nil {
			return prepareError
		}
		defer rowInsertStmt.Close()
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
			if _, rowInsertError := rowInsertStmt.Exec(
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

// SaveAdjustments stores manual Fleet Carrier jump adjustments for expeditionID.
func SaveAdjustments(queryer db.Queryer, expeditionID int64, carrierStats CarrierStats) error {
	adjustmentsJSON, marshalError := json.Marshal(carrierStats)
	if marshalError != nil {
		return fmt.Errorf("marshal expedition %d adjustments: %w", expeditionID, marshalError)
	}
	_, execError := queryer.Exec(`UPDATE expeditions SET adjustments_json = ?, updated_at = ? WHERE id = ?`, string(adjustmentsJSON), reference.NowISO(), expeditionID)
	return execError
}

// RecordCarrierJump increments carrier jump count, cumulative distance, and tritium
// consumed on expeditionID and persists the updated stats.
func RecordCarrierJump(queryer db.Queryer, expeditionID int64, distanceLy float64, tritium int) error {
	expeditionRecord, getError := Get(queryer, expeditionID)
	if getError != nil {
		return getError
	}
	carrierStats := expeditionRecord.Adjustments
	carrierStats.CarrierJumps++
	if distanceLy > 0 {
		carrierStats.CarrierDistanceLy += distanceLy
	}
	if tritium > 0 {
		currentTritium := 0
		if carrierStats.TritiumUsed != nil {
			currentTritium = *carrierStats.TritiumUsed
		}
		nextTritium := currentTritium + tritium
		carrierStats.TritiumUsed = &nextTritium
	}
	return SaveAdjustments(queryer, expeditionID, carrierStats)
}
