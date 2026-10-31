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

// Contains reports whether ts lies in [Start, End).
func (w Window) Contains(ts string) bool {
	return ts >= w.Start && (w.End == "" || ts < w.End)
}

// List returns all modes ordered for display.
func List(q db.Queryer) ([]Mode, error) {
	rows, err := q.Query(`SELECT id, name, description, color, is_default, sort_order FROM modes ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mode
	for rows.Next() {
		var m Mode
		if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.Color, &m.IsDefault, &m.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
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
func Create(q db.Queryer, name, description, color string) (Mode, error) {
	name, color, err := validate(name, color)
	if err != nil {
		return Mode{}, err
	}
	var maxOrder int
	_ = q.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) FROM modes`).Scan(&maxOrder)
	res, err := q.Exec(`INSERT INTO modes(name, description, color, is_default, sort_order, created_at) VALUES(?,?,?,0,?,?)`,
		name, strings.TrimSpace(description), color, maxOrder+1, ed.NowISO())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Mode{}, fmt.Errorf("a mode named %q already exists", name)
		}
		return Mode{}, err
	}
	id, _ := res.LastInsertId()
	return Mode{ID: id, Name: name, Description: strings.TrimSpace(description), Color: color, SortOrder: maxOrder + 1}, nil
}

// Update edits a mode's name, description and colour.
func Update(q db.Queryer, id int64, name, description, color string) error {
	name, color, err := validate(name, color)
	if err != nil {
		return err
	}
	res, err := q.Exec(`UPDATE modes SET name = ?, description = ?, color = ? WHERE id = ?`, name, strings.TrimSpace(description), color, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("a mode named %q already exists", name)
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("mode %d not found", id)
	}
	return nil
}

// Delete removes a custom mode (and, via ON DELETE CASCADE, its windows).
// The three default modes cannot be deleted.
func Delete(q db.Queryer, id int64) error {
	var isDefault bool
	if err := q.QueryRow(`SELECT is_default FROM modes WHERE id = ?`, id).Scan(&isDefault); err != nil {
		return fmt.Errorf("mode %d not found", id)
	}
	if isDefault {
		return errors.New("default modes cannot be deleted (you can rename or recolour them)")
	}
	_, err := q.Exec(`DELETE FROM modes WHERE id = ?`, id)
	return err
}

// ListWindows returns every window ordered by start.
func ListWindows(q db.Queryer) ([]Window, error) {
	rows, err := q.Query(`SELECT id, mode_id, start_timestamp, COALESCE(end_timestamp, '') FROM mode_windows ORDER BY start_timestamp, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Window
	for rows.Next() {
		var w Window
		if err := rows.Scan(&w.ID, &w.ModeID, &w.Start, &w.End); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Active returns the currently active (open-ended) mode window, if any.
func Active(q db.Queryer) (*Window, error) {
	var w Window
	err := q.QueryRow(`SELECT id, mode_id, start_timestamp FROM mode_windows WHERE end_timestamp IS NULL ORDER BY start_timestamp DESC LIMIT 1`).
		Scan(&w.ID, &w.ModeID, &w.Start)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// endAfter compares window ends where "" means +infinity: a > b.
func endAfter(a, b string) bool {
	if a == b {
		return false
	}
	if a == "" {
		return true
	}
	if b == "" {
		return false
	}
	return a > b
}

// SetWindow writes [start, end) for modeID (end "" = open ended), trimming or
// splitting any overlapping windows so windows never overlap. If replaceID is
// non-zero that window is removed first (used when editing a window).
// modeID == UnassignedID clears the range without inserting a new window.
func SetWindow(tx *sql.Tx, replaceID, modeID int64, start, end string) error {
	var err error
	if start, err = ed.NormalizeISO(start); err != nil {
		return err
	}
	if end != "" {
		if end, err = ed.NormalizeISO(end); err != nil {
			return err
		}
		if end <= start {
			return errors.New("window end must be after its start")
		}
	}
	if replaceID != 0 {
		if _, err := tx.Exec(`DELETE FROM mode_windows WHERE id = ?`, replaceID); err != nil {
			return err
		}
	}
	existing, err := ListWindows(tx)
	if err != nil {
		return err
	}
	for _, w := range existing {
		overlaps := (end == "" || w.Start < end) && (w.End == "" || start < w.End)
		if !overlaps {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM mode_windows WHERE id = ?`, w.ID); err != nil {
			return err
		}
		if w.Start < start { // keep head [w.Start, start)
			if err := insertWindow(tx, w.ModeID, w.Start, start); err != nil {
				return err
			}
		}
		if end != "" && endAfter(w.End, end) { // keep tail [end, w.End)
			if err := insertWindow(tx, w.ModeID, end, w.End); err != nil {
				return err
			}
		}
	}
	if modeID == UnassignedID {
		return nil
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM modes WHERE id = ?`, modeID).Scan(&exists); err != nil || exists == 0 {
		return fmt.Errorf("mode %d not found", modeID)
	}
	return insertWindow(tx, modeID, start, end)
}

func insertWindow(tx *sql.Tx, modeID int64, start, end string) error {
	var endV any
	if end != "" {
		endV = end
	}
	_, err := tx.Exec(`INSERT INTO mode_windows(mode_id, start_timestamp, end_timestamp) VALUES(?,?,?)`, modeID, start, endV)
	return err
}

// Switch makes modeID the active mode from now on (UnassignedID stops tracking).
func Switch(tx *sql.Tx, modeID int64, at string) error {
	if at == "" {
		at = ed.NowISO()
	}
	return SetWindow(tx, 0, modeID, at, "")
}

// DeleteWindow removes a window.
func DeleteWindow(q db.Queryer, id int64) error {
	_, err := q.Exec(`DELETE FROM mode_windows WHERE id = ?`, id)
	return err
}

// DefaultFallbackModeID returns the mode ID used whenever no explicit mode
// window covers a timestamp (prefers the default "Travel" mode).
func DefaultFallbackModeID(q db.Queryer) int64 {
	var id int64
	if err := q.QueryRow(`SELECT id FROM modes WHERE name = 'Travel' COLLATE NOCASE LIMIT 1`).Scan(&id); err == nil && id > 0 {
		return id
	}
	if err := q.QueryRow(`SELECT id FROM modes ORDER BY is_default DESC, sort_order ASC, id ASC LIMIT 1`).Scan(&id); err == nil && id > 0 {
		return id
	}
	return UnassignedID
}

// Timeline answers "which mode was active at time t" for a set of windows.
type Timeline struct {
	windows       []Window
	defaultModeID int64
}

// NewTimeline builds a timeline from windows (any order).
func NewTimeline(ws []Window, defaultModeID ...int64) *Timeline {
	cp := append([]Window(nil), ws...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Start < cp[j].Start })
	def := UnassignedID
	if len(defaultModeID) > 0 {
		def = defaultModeID[0]
	}
	return &Timeline{windows: cp, defaultModeID: def}
}

// LoadTimeline loads all windows into a Timeline with the default fallback mode (Travel).
func LoadTimeline(q db.Queryer) (*Timeline, error) {
	ws, err := ListWindows(q)
	if err != nil {
		return nil, err
	}
	return NewTimeline(ws, DefaultFallbackModeID(q)), nil
}

// ModeAt returns the mode active at ts (falling back to the default mode, e.g. Travel, when none).
func (t *Timeline) ModeAt(ts string) int64 {
	// Last window starting at or before ts. Windows written through SetWindow
	// never overlap, so only that window can contain ts.
	i := sort.Search(len(t.windows), func(i int) bool { return t.windows[i].Start > ts }) - 1
	if i >= 0 && t.windows[i].Contains(ts) {
		return t.windows[i].ModeID
	}
	return t.defaultModeID
}
