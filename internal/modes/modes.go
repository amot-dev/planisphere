// Package modes manages tracking modes (Exploration / Tourism / Travel and
// custom modes) and the non-overlapping time windows during which each mode
// was active.
package modes

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
)

// UnassignedID is the pseudo mode ID for time not covered by any window.
const UnassignedID int64 = 0

// Mode is a tracking mode.
type Mode struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	IsDefault   bool   `json:"isDefault"`
	SortOrder   int    `json:"sortOrder"`
}

// Window is a period during which a mode was active. End == "" means open
// ended (the currently active mode).
type Window struct {
	ID     int64  `json:"id"`
	ModeID int64  `json:"modeId"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

// Contains reports whether timestamp lies in [Start, End).
func (window Window) Contains(timestamp string) bool {
	return timestamp >= window.Start && (window.End == "" || timestamp < window.End)
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

func validate(name, color string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", errors.New("mode name is required")
	}
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#FF7100"
	}
	if !strings.HasPrefix(color, "#") || (len(color) != 7 && len(color) != 4) {
		return "", "", fmt.Errorf("invalid colour %q (expected #RRGGBB)", color)
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
		validName, strings.TrimSpace(description), validColor, maxOrder+1, ed.NowISO())
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

// Delete removes a custom mode (and, via ON DELETE CASCADE, its windows).
// The three default modes cannot be deleted.
func Delete(queryer db.Queryer, modeID int64) error {
	var isDefault bool
	if queryError := queryer.QueryRow(`SELECT is_default FROM modes WHERE id = ?`, modeID).Scan(&isDefault); queryError != nil {
		return fmt.Errorf("mode %d not found", modeID)
	}
	if isDefault {
		return errors.New("default modes cannot be deleted (you can rename or recolour them)")
	}
	_, deleteError := queryer.Exec(`DELETE FROM modes WHERE id = ?`, modeID)
	return deleteError
}

// ListWindows returns every window ordered by start.
func ListWindows(queryer db.Queryer) ([]Window, error) {
	rows, queryError := queryer.Query(`SELECT id, mode_id, start_timestamp, COALESCE(end_timestamp, '') FROM mode_windows ORDER BY start_timestamp, id`)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var windowsList []Window
	for rows.Next() {
		var windowItem Window
		if scanError := rows.Scan(&windowItem.ID, &windowItem.ModeID, &windowItem.Start, &windowItem.End); scanError != nil {
			return nil, scanError
		}
		windowsList = append(windowsList, windowItem)
	}
	return windowsList, rows.Err()
}

// Active returns the currently active (open-ended) mode window, if any.
func Active(queryer db.Queryer) (*Window, error) {
	var activeWindow Window
	queryError := queryer.QueryRow(`SELECT id, mode_id, start_timestamp FROM mode_windows WHERE end_timestamp IS NULL ORDER BY start_timestamp DESC LIMIT 1`).
		Scan(&activeWindow.ID, &activeWindow.ModeID, &activeWindow.Start)
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, queryError
	}
	return &activeWindow, nil
}

// endAfter compares window ends where "" means +infinity: firstEnd > secondEnd.
func endAfter(firstEnd, secondEnd string) bool {
	if firstEnd == secondEnd {
		return false
	}
	if firstEnd == "" {
		return true
	}
	if secondEnd == "" {
		return false
	}
	return firstEnd > secondEnd
}

// SetWindow writes [start, end) for modeID (end "" = open ended), trimming or
// splitting any overlapping windows so windows never overlap. If replaceID is
// non-zero that window is removed first (used when editing a window).
// modeID == UnassignedID clears the range without inserting a new window.
func SetWindow(transaction *sql.Tx, replaceID, modeID int64, start, end string) error {
	normalizedStart, parseStartError := ed.NormalizeISO(start)
	if parseStartError != nil {
		return parseStartError
	}
	normalizedEnd := ""
	if end != "" {
		var parseEndError error
		if normalizedEnd, parseEndError = ed.NormalizeISO(end); parseEndError != nil {
			return parseEndError
		}
		if normalizedEnd <= normalizedStart {
			return errors.New("window end must be after its start")
		}
	}
	if replaceID != 0 {
		if _, deleteError := transaction.Exec(`DELETE FROM mode_windows WHERE id = ?`, replaceID); deleteError != nil {
			return deleteError
		}
	}
	existingWindows, listError := ListWindows(transaction)
	if listError != nil {
		return listError
	}
	for _, windowItem := range existingWindows {
		overlaps := (normalizedEnd == "" || windowItem.Start < normalizedEnd) && (windowItem.End == "" || normalizedStart < windowItem.End)
		if !overlaps {
			continue
		}
		if _, deleteError := transaction.Exec(`DELETE FROM mode_windows WHERE id = ?`, windowItem.ID); deleteError != nil {
			return deleteError
		}
		if windowItem.Start < normalizedStart { // keep head [windowItem.Start, normalizedStart)
			if insertHeadError := insertWindow(transaction, windowItem.ModeID, windowItem.Start, normalizedStart); insertHeadError != nil {
				return insertHeadError
			}
		}
		if normalizedEnd != "" && endAfter(windowItem.End, normalizedEnd) { // keep tail [normalizedEnd, windowItem.End)
			if insertTailError := insertWindow(transaction, windowItem.ModeID, normalizedEnd, windowItem.End); insertTailError != nil {
				return insertTailError
			}
		}
	}
	if modeID == UnassignedID {
		return nil
	}
	var modeCount int
	if countError := transaction.QueryRow(`SELECT COUNT(*) FROM modes WHERE id = ?`, modeID).Scan(&modeCount); countError != nil || modeCount == 0 {
		return fmt.Errorf("mode %d not found", modeID)
	}
	return insertWindow(transaction, modeID, normalizedStart, normalizedEnd)
}

func insertWindow(transaction *sql.Tx, modeID int64, startTime, endTime string) error {
	var nullableEndTime any
	if endTime != "" {
		nullableEndTime = endTime
	}
	_, execError := transaction.Exec(`INSERT INTO mode_windows(mode_id, start_timestamp, end_timestamp) VALUES(?,?,?)`, modeID, startTime, nullableEndTime)
	return execError
}

// Switch makes modeID the active mode from now on (UnassignedID stops tracking).
func Switch(transaction *sql.Tx, modeID int64, atTimestamp string) error {
	if atTimestamp == "" {
		atTimestamp = ed.NowISO()
	}
	return SetWindow(transaction, 0, modeID, atTimestamp, "")
}

// DeleteWindow removes a window.
func DeleteWindow(queryer db.Queryer, windowID int64) error {
	_, execError := queryer.Exec(`DELETE FROM mode_windows WHERE id = ?`, windowID)
	return execError
}

// DefaultFallbackModeID returns the mode ID used whenever no explicit mode
// window covers a timestamp (prefers the default "Travel" mode).
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

// Timeline answers "which mode was active at time t" for a set of windows.
type Timeline struct {
	windows       []Window
	defaultModeID int64
}

// NewTimeline builds a timeline from windows (any order).
func NewTimeline(windowsList []Window, defaultModeID ...int64) *Timeline {
	copiedWindows := append([]Window(nil), windowsList...)
	sort.Slice(copiedWindows, func(firstIndex, secondIndex int) bool {
		return copiedWindows[firstIndex].Start < copiedWindows[secondIndex].Start
	})
	fallbackMode := UnassignedID
	if len(defaultModeID) > 0 {
		fallbackMode = defaultModeID[0]
	}
	return &Timeline{windows: copiedWindows, defaultModeID: fallbackMode}
}

// LoadTimeline loads all windows into a Timeline with the default fallback mode (Travel).
func LoadTimeline(queryer db.Queryer) (*Timeline, error) {
	windowsList, listError := ListWindows(queryer)
	if listError != nil {
		return nil, listError
	}
	return NewTimeline(windowsList, DefaultFallbackModeID(queryer)), nil
}

// ModeAt returns the mode active at targetTimestamp (falling back to the default mode, e.g. Travel, when none).
func (timeline *Timeline) ModeAt(targetTimestamp string) int64 {
	// Last window starting at or before targetTimestamp. Windows written through SetWindow
	// never overlap, so only that window can contain targetTimestamp.
	candidateIndex := sort.Search(len(timeline.windows), func(i int) bool { return timeline.windows[i].Start > targetTimestamp }) - 1
	if candidateIndex >= 0 && timeline.windows[candidateIndex].Contains(targetTimestamp) {
		return timeline.windows[candidateIndex].ModeID
	}
	return timeline.defaultModeID
}
