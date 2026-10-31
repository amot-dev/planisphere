package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
)

// Arrival is a journal event that placed the player in a system.
type Arrival struct {
	UID           string `json:"uid"`
	Timestamp     string `json:"timestamp"`
	Galactic      string `json:"galactic"`
	Event         string `json:"event"`
	SystemName    string `json:"systemName"`
	SystemAddress int64  `json:"systemAddress"`
}

func loadArrivals(q db.Queryer, where string, args ...any) ([]Arrival, error) {
	rs, err := q.Query(`SELECT event_uid, timestamp, event, COALESCE(system_name,''), COALESCE(system_address,0)
		FROM journal_events WHERE event IN ('FSDJump','Location','CarrierJump') AND `+journal.CmdrFilterSQL+` AND `+where+` ORDER BY timestamp, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []Arrival
	for rs.Next() {
		var a Arrival
		if err := rs.Scan(&a.UID, &a.Timestamp, &a.Event, &a.SystemName, &a.SystemAddress); err != nil {
			return nil, err
		}
		a.Galactic = ed.ToGalactic(a.Timestamp)
		out = append(out, a)
	}
	return out, rs.Err()
}

// matches reports whether an arrival is in the row's system (by name,
// case-insensitive, or by SystemAddress when both are known).
func matches(r Row, a Arrival) bool {
	if r.RowType != RowWaypoint || strings.TrimSpace(r.SystemName) == "" {
		return false
	}
	if r.SystemAddress != 0 && a.SystemAddress != 0 && r.SystemAddress == a.SystemAddress {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.SystemName), strings.TrimSpace(a.SystemName))
}

func firstWaypoint(rows []Row) int {
	for i, r := range rows {
		if r.IsWaypoint() {
			return i
		}
	}
	return -1
}

// StartCandidates lists every recorded visit to Waypoint #1's system, newest first.
func StartCandidates(q db.Queryer, expID int64) (system string, out []Arrival, err error) {
	rows, err := ListRows(q, expID)
	if err != nil {
		return "", nil, err
	}
	i := firstWaypoint(rows)
	if i < 0 || strings.TrimSpace(rows[i].SystemName) == "" {
		return "", nil, errors.New("waypoint #1 needs a system name before the expedition can start")
	}
	w := rows[i]
	var arr []Arrival
	if w.SystemAddress != 0 {
		arr, err = loadArrivals(q, `(system_name = ? COLLATE NOCASE OR system_address = ?)`, w.SystemName, w.SystemAddress)
	} else {
		arr, err = loadArrivals(q, `system_name = ? COLLATE NOCASE`, w.SystemName)
	}
	if err != nil {
		return w.SystemName, nil, err
	}
	for l, r := 0, len(arr)-1; l < r; l, r = l+1, r-1 {
		arr[l], arr[r] = arr[r], arr[l]
	}
	return w.SystemName, arr, nil
}

// Start activates the expedition with startTimestamp as the arrival at
// Waypoint #1, then replays all journal history from that moment.
func Start(tx *sql.Tx, expID int64, startTimestamp string) error {
	ts, err := ed.NormalizeISO(startTimestamp)
	if err != nil {
		return err
	}
	rows, err := ListRows(tx, expID)
	if err != nil {
		return err
	}
	if i := firstWaypoint(rows); i < 0 || rows[i].SystemName == "" {
		return errors.New("waypoint #1 needs a system name before the expedition can start")
	}
	if err := setLifecycle(tx, expID, StatusActive, ts, nil, nil); err != nil {
		return err
	}
	if err := initCarrierStops(tx, expID); err != nil {
		return err
	}
	_, err = Reevaluate(tx, expID)
	return err
}

// Arm activates the expedition in "waiting for first arrival" state: the next
// arrival at Waypoint #1 (from now on) becomes the start timestamp.
func Arm(tx *sql.Tx, expID int64) error {
	if err := setLifecycle(tx, expID, StatusActive, nil, nil, ed.NowISO()); err != nil {
		return err
	}
	if err := clearVisits(tx, expID); err != nil {
		return err
	}
	if err := initCarrierStops(tx, expID); err != nil {
		return err
	}
	_, err := Reevaluate(tx, expID)
	return err
}

// initCarrierStops initializes carrier stops for an active expedition.
// The first carrier stop serves as the carrier's origin/starting location,
// so it starts as arrived; subsequent carrier stops start as pending.
func initCarrierStops(q db.Queryer, expID int64) error {
	if _, err := q.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE expedition_id = ?`, expID); err != nil {
		return err
	}
	_, err := q.Exec(`UPDATE expedition_rows SET fc_arrived = 1
		WHERE id = (
			SELECT id FROM expedition_rows
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1
			ORDER BY position ASC LIMIT 1
		)`, expID)
	return err
}

// Complete ends the expedition at endTimestamp (now when empty).
func Complete(tx *sql.Tx, expID int64, endTimestamp string) error {
	e, err := Get(tx, expID)
	if err != nil {
		return err
	}
	if e.Status != StatusActive || e.StartTimestamp == "" {
		return errors.New("only a started, active expedition can be completed")
	}
	end := ed.NowISO()
	if endTimestamp != "" {
		if end, err = ed.NormalizeISO(endTimestamp); err != nil {
			return err
		}
	}
	if end <= e.StartTimestamp {
		return errors.New("end must be after the expedition start")
	}
	if err := PruneEmptyRows(tx, expID); err != nil {
		return err
	}
	if err := setLifecycle(tx, expID, StatusCompleted, e.StartTimestamp, end, nil); err != nil {
		return err
	}
	_, err = Reevaluate(tx, expID)
	return err
}

// Reopen turns a completed expedition back into an active one.
func Reopen(tx *sql.Tx, expID int64) error {
	e, err := Get(tx, expID)
	if err != nil {
		return err
	}
	if e.Status != StatusCompleted {
		return errors.New("expedition is not completed")
	}
	if err := setLifecycle(tx, expID, StatusActive, e.StartTimestamp, nil, nil); err != nil {
		return err
	}
	_, err = Reevaluate(tx, expID)
	return err
}

// ResetToPlanning clears the start timestamp and all visit state.
func ResetToPlanning(tx *sql.Tx, expID int64) error {
	if err := setLifecycle(tx, expID, StatusPlanning, nil, nil, nil); err != nil {
		return err
	}
	return clearVisits(tx, expID)
}

// Reevaluate replays journal arrivals from the expedition start (in
// chronological order) and recomputes which waypoints were visited, applying
// the out-of-order auto-reordering rule:
//
//   - Waypoint #1 is visited at the start timestamp.
//   - Arrivals before the start timestamp are ignored entirely.
//   - An arrival only counts when the system differs from the previous
//     arrival's system (logging in where you already are is not a visit).
//   - The first unvisited waypoint matching the arrival is marked visited; if
//     it is not directly after the current (last visited) waypoint it is moved
//     there, shifting the skipped waypoints down.
//
// The replay is deterministic and idempotent: running it again over the
// reordered plan yields the same result. It returns whether anything changed.
func Reevaluate(tx db.Queryer, expID int64) (bool, error) {
	e, err := Get(tx, expID)
	if err != nil {
		return false, err
	}
	if e.Status == StatusPlanning {
		return false, nil
	}
	rows, err := ListRows(tx, expID)
	if err != nil {
		return false, err
	}
	w1 := firstWaypoint(rows)
	if w1 < 0 {
		return false, nil
	}

	// "Wait for first arrival": pick the first arrival at W1 after armed_at.
	if e.StartTimestamp == "" {
		if e.ArmedAt == "" {
			return false, nil
		}
		arr, err := loadArrivals(tx, `timestamp >= ?`, e.ArmedAt)
		if err != nil {
			return false, err
		}
		for _, a := range arr {
			if matches(rows[w1], a) {
				e.StartTimestamp = a.Timestamp
				break
			}
		}
		if e.StartTimestamp == "" {
			return false, nil
		}
		if err := setLifecycle(tx, expID, StatusActive, e.StartTimestamp, nil, nil); err != nil {
			return false, err
		}
		if err := initCarrierStops(tx, expID); err != nil {
			return false, err
		}
		rows, err = ListRows(tx, expID)
		if err != nil {
			return false, err
		}
	}

	orig := make(map[int64]Row, len(rows))
	for _, r := range rows {
		orig[r.ID] = r
	}
	for i := range rows {
		rows[i].VisitedAt, rows[i].VisitEventUID = "", ""
	}
	modeTL, _ := modes.LoadTimeline(tx)

	var arrivals []Arrival
	if e.EndTimestamp != "" {
		arrivals, err = loadArrivals(tx, `timestamp >= ? AND timestamp <= ?`, e.StartTimestamp, e.EndTimestamp)
	} else {
		arrivals, err = loadArrivals(tx, `timestamp >= ?`, e.StartTimestamp)
	}
	if err != nil {
		return false, err
	}

	// Waypoint #1 is visited at the start timestamp.
	rows[w1].VisitedAt = e.StartTimestamp
	lastSystem := strings.ToLower(strings.TrimSpace(rows[w1].SystemName))
	for _, a := range arrivals {
		if a.Timestamp == e.StartTimestamp && matches(rows[w1], a) {
			rows[w1].VisitEventUID = a.UID
			if a.SystemName != "" {
				lastSystem = strings.ToLower(strings.TrimSpace(a.SystemName))
			}
			break
		}
	}
	currentIdx := w1

	for _, a := range arrivals {
		if a.Timestamp == e.StartTimestamp && a.UID == rows[w1].VisitEventUID {
			continue
		}
		sys := strings.ToLower(strings.TrimSpace(a.SystemName))
		if sys == "" || sys == lastSystem {
			continue // not an actual change of system
		}
		lastSystem = sys
		target := -1
		for i, r := range rows {
			if r.IsWaypoint() && r.VisitedAt == "" && matches(r, a) {
				target = i
				break
			}
		}
		if target < 0 {
			continue
		}
		rows[target].VisitedAt, rows[target].VisitEventUID = a.Timestamp, a.UID
		if rows[target].LegModeID == 0 && modeTL != nil {
			rows[target].LegModeID = modeTL.ModeAt(a.Timestamp)
		}
		nextUnvisitedWP := -1
		for i := currentIdx + 1; i < len(rows); i++ {
			if i == target || (rows[i].IsWaypoint() && rows[i].VisitedAt == "") {
				nextUnvisitedWP = i
				break
			}
		}
		if target != nextUnvisitedWP {
			dest := currentIdx + 1
			if target < currentIdx {
				dest = currentIdx // removing target shifts current up by one
			}
			rows = moveTo(rows, target, dest)
			target = dest
		}
		currentIdx = target
	}

	changed := false
	for i := range rows {
		r := rows[i]
		o := orig[r.ID]
		if o.Position != i+1 {
			changed = true
			if _, err := tx.Exec(`UPDATE expedition_rows SET position = ? WHERE id = ?`, i+1, r.ID); err != nil {
				return false, err
			}
		}
		if o.VisitedAt != r.VisitedAt || o.VisitEventUID != r.VisitEventUID {
			changed = true
			var v, u any
			if r.VisitedAt != "" {
				v = r.VisitedAt
			}
			if r.VisitEventUID != "" {
				u = r.VisitEventUID
			}
			if _, err := tx.Exec(`UPDATE expedition_rows SET visited_at = ?, visit_event_uid = ? WHERE id = ?`, v, u, r.ID); err != nil {
				return false, fmt.Errorf("update visit: %w", err)
			}
		}
		if o.FCArrived != r.FCArrived {
			changed = true
			if _, err := tx.Exec(`UPDATE expedition_rows SET fc_arrived = ? WHERE id = ?`, r.FCArrived, r.ID); err != nil {
				return false, fmt.Errorf("update fc_arrived: %w", err)
			}
		}
		if o.LegModeID != r.LegModeID {
			changed = true
			if _, err := tx.Exec(`UPDATE expedition_rows SET leg_mode_id = ? WHERE id = ?`, r.LegModeID, r.ID); err != nil {
				return false, fmt.Errorf("update leg_mode_id: %w", err)
			}
		}
	}
	return changed, nil
}

// ActiveExpeditionIDs returns expeditions that need re-evaluation on new
// journal data (active, including those waiting for their first arrival).
func ActiveExpeditionIDs(q db.Queryer) ([]int64, error) {
	rs, err := q.Query(`SELECT id FROM expeditions WHERE status = ?`, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []int64
	for rs.Next() {
		var id int64
		if err := rs.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rs.Err()
}

