// Package modes manages tracking modes (Exploration / Tourism / Travel and
// user-defined custom flight modes) referenced by expedition legs.
package modes

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

var hexColorPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// UnassignedID is the pseudo mode ID when no flight mode has been assigned.
const UnassignedID int64 = 0

// Mode is a tracking flight mode.
type Mode struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	IsDefault   bool   `json:"isDefault"`
	SortOrder   int    `json:"sortOrder"`
}

// ExpeditionUsage summarizes how an expedition references a flight mode across
// its visited legs and active mode setting.
type ExpeditionUsage struct {
	ExpeditionID   int64  `json:"expeditionId"`
	ExpeditionName string `json:"expeditionName"`
	Status         string `json:"status"`
	LegCount       int    `json:"legCount"`
	IsActiveMode   bool   `json:"isActiveMode"`
}

// List returns all modes ordered for display.
func List(queryer db.Queryer) ([]Mode, error) {
	rows, queryError := queryer.Query(`SELECT id, name, description, color, is_default, sort_order FROM modes ORDER BY sort_order, id`)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var modesList []Mode
	for rows.Next() {
		var modeItem Mode
		if scanError := rows.Scan(&modeItem.ID, &modeItem.Name, &modeItem.Description, &modeItem.Color, &modeItem.IsDefault, &modeItem.SortOrder); scanError != nil {
			return nil, scanError
		}
		modesList = append(modesList, modeItem)
	}
	return modesList, rows.Err()
}

// Exists reports whether a flight mode with modeID exists in the database.
func Exists(queryer db.Queryer, modeID int64) (bool, error) {
	if modeID <= 0 {
		return false, nil
	}
	var modeCount int
	if countError := queryer.QueryRow(`SELECT COUNT(*) FROM modes WHERE id = ?`, modeID).Scan(&modeCount); countError != nil {
		return false, countError
	}
	return modeCount > 0, nil
}

func validate(name, color string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", errors.New("mode name is required")
	}
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#FF7100"
	}
	if !hexColorPattern.MatchString(color) {
		return "", "", fmt.Errorf("invalid colour %q (expected #RGB or #RRGGBB)", color)
	}
	return name, color, nil
}

// Create adds a custom mode.
func Create(queryer db.Queryer, name, description, color string) (Mode, error) {
	validName, validColor, validateError := validate(name, color)
	if validateError != nil {
		return Mode{}, validateError
	}
	var maxOrder int
	_ = queryer.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) FROM modes`).Scan(&maxOrder)
	insertResult, insertError := queryer.Exec(`INSERT INTO modes(name, description, color, is_default, sort_order, created_at) VALUES(?,?,?,0,?,?)`,
		validName, strings.TrimSpace(description), validColor, maxOrder+1, reference.NowISO())
	if insertError != nil {
		if strings.Contains(insertError.Error(), "UNIQUE") {
			return Mode{}, fmt.Errorf("a mode named %q already exists", validName)
		}
		return Mode{}, insertError
	}
	modeID, _ := insertResult.LastInsertId()
	return Mode{ID: modeID, Name: validName, Description: strings.TrimSpace(description), Color: validColor, SortOrder: maxOrder + 1}, nil
}

// Update edits a mode's name, description and colour.
func Update(queryer db.Queryer, modeID int64, name, description, color string) error {
	validName, validColor, validateError := validate(name, color)
	if validateError != nil {
		return validateError
	}
	updateResult, updateError := queryer.Exec(`UPDATE modes SET name = ?, description = ?, color = ? WHERE id = ?`, validName, strings.TrimSpace(description), validColor, modeID)
	if updateError != nil {
		if strings.Contains(updateError.Error(), "UNIQUE") {
			return fmt.Errorf("a mode named %q already exists", validName)
		}
		return updateError
	}
	if affectedRows, _ := updateResult.RowsAffected(); affectedRows == 0 {
		return fmt.Errorf("mode %d not found", modeID)
	}
	return nil
}

// UsageByExpedition returns every expedition that currently references modeID on
// visited legs or as its active flight mode (excluding inactive completed active_mode_id).
func UsageByExpedition(queryer db.Queryer, modeID int64) ([]ExpeditionUsage, error) {
	rows, queryError := queryer.Query(`
		SELECT
			e.id,
			e.name,
			e.status,
			COALESCE(SUM(CASE WHEN r.row_type = 'waypoint' AND COALESCE(r.visited_at, '') != '' AND r.leg_mode_id = ? THEN 1 ELSE 0 END), 0) AS leg_count,
			CASE WHEN e.status != 'completed' AND e.active_mode_id = ? THEN 1 ELSE 0 END AS is_active_mode
		FROM expeditions e
		LEFT JOIN expedition_rows r ON r.expedition_id = e.id
		GROUP BY e.id, e.name, e.status, e.active_mode_id
		HAVING leg_count > 0 OR is_active_mode = 1
		ORDER BY e.created_at DESC, e.id DESC`,
		modeID, modeID,
	)
	if queryError != nil {
		return nil, fmt.Errorf("query mode %d usage: %w", modeID, queryError)
	}
	defer rows.Close()

	var usageList []ExpeditionUsage
	for rows.Next() {
		var usageItem ExpeditionUsage
		var activeFlag int
		if scanError := rows.Scan(&usageItem.ExpeditionID, &usageItem.ExpeditionName, &usageItem.Status, &usageItem.LegCount, &activeFlag); scanError != nil {
			return nil, fmt.Errorf("scan mode %d usage: %w", modeID, scanError)
		}
		usageItem.IsActiveMode = activeFlag != 0
		usageList = append(usageList, usageItem)
	}
	return usageList, rows.Err()
}

// Delete removes a custom mode and reassigns any expedition legs and expedition
// active_mode_id values referencing it back to the default fallback mode (Travel).
// The three default modes cannot be deleted.
func Delete(transaction *sql.Tx, modeID int64) error {
	var isDefault bool
	if queryError := transaction.QueryRow(`SELECT is_default FROM modes WHERE id = ?`, modeID).Scan(&isDefault); queryError != nil {
		return fmt.Errorf("mode %d not found", modeID)
	}
	if isDefault {
		return errors.New("default modes cannot be deleted (you can recolour them)")
	}
	fallbackModeID := DefaultFallbackModeID(transaction)
	now := reference.NowISO()
	if _, updateRowsError := transaction.Exec(
		`UPDATE expedition_rows SET leg_mode_id = ?, updated_at = ? WHERE leg_mode_id = ?`,
		fallbackModeID, now, modeID,
	); updateRowsError != nil {
		return fmt.Errorf("reassign leg modes for deleted mode %d: %w", modeID, updateRowsError)
	}
	if _, updateExpError := transaction.Exec(
		`UPDATE expeditions SET active_mode_id = ?, updated_at = ? WHERE active_mode_id = ?`,
		fallbackModeID, now, modeID,
	); updateExpError != nil {
		return fmt.Errorf("reassign expedition active mode for deleted mode %d: %w", modeID, updateExpError)
	}
	_, deleteError := transaction.Exec(`DELETE FROM modes WHERE id = ?`, modeID)
	return deleteError
}

// DeleteTx runs Delete inside a database transaction on dbConn.
func DeleteTx(dbConn *sql.DB, modeID int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return Delete(transaction, modeID)
	})
}

// DefaultFallbackModeID returns the mode ID used whenever no explicit flight
// mode is set (prefers the default "Travel" mode).
func DefaultFallbackModeID(queryer db.Queryer) int64 {
	var fallbackModeID int64
	if queryError := queryer.QueryRow(`SELECT id FROM modes WHERE name = 'Travel' COLLATE NOCASE LIMIT 1`).Scan(&fallbackModeID); queryError == nil && fallbackModeID > 0 {
		return fallbackModeID
	}
	if queryError := queryer.QueryRow(`SELECT id FROM modes ORDER BY is_default DESC, sort_order ASC, id ASC LIMIT 1`).Scan(&fallbackModeID); queryError == nil && fallbackModeID > 0 {
		return fallbackModeID
	}
	return UnassignedID
}
