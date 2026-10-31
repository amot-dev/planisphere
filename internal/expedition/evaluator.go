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

func loadArrivals(queryer db.Queryer, where string, args ...any) ([]Arrival, error) {
	rows, queryError := queryer.Query(`SELECT event_uid, timestamp, event, COALESCE(system_name,''), COALESCE(system_address,0)
		FROM journal_events WHERE event IN ('FSDJump','Location','CarrierJump') AND `+journal.CmdrFilterSQL+` AND `+where+` ORDER BY timestamp, id`, args...)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var arrivals []Arrival
	for rows.Next() {
		var arrival Arrival
		if scanError := rows.Scan(&arrival.UID, &arrival.Timestamp, &arrival.Event, &arrival.SystemName, &arrival.SystemAddress); scanError != nil {
			return nil, scanError
		}
		arrival.Galactic = ed.ToGalactic(arrival.Timestamp)
		arrivals = append(arrivals, arrival)
	}
	return arrivals, rows.Err()
}

// matches reports whether an arrival is in the row's system (by name,
// case-insensitive, or by SystemAddress when both are known).
func matches(rowItem Row, arrival Arrival) bool {
	if rowItem.RowType != RowWaypoint || strings.TrimSpace(rowItem.SystemName) == "" {
		return false
	}
	if rowItem.SystemAddress != 0 && arrival.SystemAddress != 0 && rowItem.SystemAddress == arrival.SystemAddress {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(rowItem.SystemName), strings.TrimSpace(arrival.SystemName))
}

func firstWaypoint(rows []Row) int {
	for i, rowItem := range rows {
		if rowItem.IsWaypoint() {
			return i
		}
	}
	return -1
}

// StartCandidates lists every recorded visit to Waypoint #1's system, newest first.
func StartCandidates(queryer db.Queryer, expeditionID int64) (system string, candidates []Arrival, loadError error) {
	rows, listError := ListRows(queryer, expeditionID)
	if listError != nil {
		return "", nil, listError
	}
	i := firstWaypoint(rows)
	if i < 0 || strings.TrimSpace(rows[i].SystemName) == "" {
		return "", nil, errors.New("waypoint #1 needs a system name before the expedition can start")
	}
	waypoint := rows[i]
	var arrivals []Arrival
	var arrivalsError error
	if waypoint.SystemAddress != 0 {
		arrivals, arrivalsError = loadArrivals(queryer, `(system_name = ? COLLATE NOCASE OR system_address = ?)`, waypoint.SystemName, waypoint.SystemAddress)
	} else {
		arrivals, arrivalsError = loadArrivals(queryer, `system_name = ? COLLATE NOCASE`, waypoint.SystemName)
	}
	if arrivalsError != nil {
		return waypoint.SystemName, nil, arrivalsError
	}
	for left, right := 0, len(arrivals)-1; left < right; left, right = left+1, right-1 {
		arrivals[left], arrivals[right] = arrivals[right], arrivals[left]
	}
	return waypoint.SystemName, arrivals, nil
}

// Start activates the expedition with startTimestamp as the arrival at
// Waypoint #1, then replays all journal history from that moment.
func Start(transaction *sql.Tx, expeditionID int64, startTimestamp string) error {
	normalizedStartTimestamp, parseError := ed.NormalizeISO(startTimestamp)
	if parseError != nil {
		return parseError
	}
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return listError
	}
	if i := firstWaypoint(rows); i < 0 || rows[i].SystemName == "" {
		return errors.New("waypoint #1 needs a system name before the expedition can start")
	}
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusActive, normalizedStartTimestamp, nil, nil); lifecycleError != nil {
		return lifecycleError
	}
	if initError := initCarrierStops(transaction, expeditionID); initError != nil {
		return initError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
}

// Arm activates the expedition in "waiting for first arrival" state: the next
// arrival at Waypoint #1 (from now on) becomes the start timestamp.
func Arm(transaction *sql.Tx, expeditionID int64) error {
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusActive, nil, nil, ed.NowISO()); lifecycleError != nil {
		return lifecycleError
	}
	if clearError := clearVisits(transaction, expeditionID); clearError != nil {
		return clearError
	}
	if initError := initCarrierStops(transaction, expeditionID); initError != nil {
		return initError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
}

// initCarrierStops initializes carrier stops for an active expedition.
// The first carrier stop serves as the carrier's origin/starting location,
// so it starts as arrived; subsequent carrier stops start as pending.
func initCarrierStops(queryer db.Queryer, expeditionID int64) error {
	if _, updateError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE expedition_id = ?`, expeditionID); updateError != nil {
		return updateError
	}
	_, execError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = 1
		WHERE id = (
			SELECT id FROM expedition_rows
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1
			ORDER BY position ASC LIMIT 1
		)`, expeditionID)
	return execError
}

// Complete ends the expedition at endTimestamp (now when empty).
func Complete(transaction *sql.Tx, expeditionID int64, endTimestamp string) error {
	expeditionRecord, getError := Get(transaction, expeditionID)
	if getError != nil {
		return getError
	}
	if expeditionRecord.Status != StatusActive || expeditionRecord.StartTimestamp == "" {
		return errors.New("only a started, active expedition can be completed")
	}
	normalizedEndTimestamp := ed.NowISO()
	if endTimestamp != "" {
		var parseError error
		if normalizedEndTimestamp, parseError = ed.NormalizeISO(endTimestamp); parseError != nil {
			return parseError
		}
	}
	if normalizedEndTimestamp <= expeditionRecord.StartTimestamp {
		return errors.New("end must be after the expedition start")
	}
	if pruneError := PruneEmptyRows(transaction, expeditionID); pruneError != nil {
		return pruneError
	}
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusCompleted, expeditionRecord.StartTimestamp, normalizedEndTimestamp, nil); lifecycleError != nil {
		return lifecycleError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
}

// Reopen turns a completed expedition back into an active one.
func Reopen(transaction *sql.Tx, expeditionID int64) error {
	expeditionRecord, getError := Get(transaction, expeditionID)
	if getError != nil {
		return getError
	}
	if expeditionRecord.Status != StatusCompleted {
		return errors.New("expedition is not completed")
	}
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusActive, expeditionRecord.StartTimestamp, nil, nil); lifecycleError != nil {
		return lifecycleError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
}

// ResetToPlanning clears the start timestamp and all visit state.
func ResetToPlanning(transaction *sql.Tx, expeditionID int64) error {
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusPlanning, nil, nil, nil); lifecycleError != nil {
		return lifecycleError
	}
	return clearVisits(transaction, expeditionID)
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
func Reevaluate(queryer db.Queryer, expeditionID int64) (bool, error) {
	expeditionRecord, getError := Get(queryer, expeditionID)
	if getError != nil {
		return false, getError
	}
	if expeditionRecord.Status == StatusPlanning {
		return false, nil
	}
	rows, listError := ListRows(queryer, expeditionID)
	if listError != nil {
		return false, listError
	}
	firstWaypointIndex := firstWaypoint(rows)
	if firstWaypointIndex < 0 {
		return false, nil
	}

	// "Wait for first arrival": pick the first arrival at W1 after armed_at.
	if expeditionRecord.StartTimestamp == "" {
		if expeditionRecord.ArmedAt == "" {
			return false, nil
		}
		candidateArrivals, loadError := loadArrivals(queryer, `timestamp >= ?`, expeditionRecord.ArmedAt)
		if loadError != nil {
			return false, loadError
		}
		for _, arrival := range candidateArrivals {
			if matches(rows[firstWaypointIndex], arrival) {
				expeditionRecord.StartTimestamp = arrival.Timestamp
				break
			}
		}
		if expeditionRecord.StartTimestamp == "" {
			return false, nil
		}
		if lifecycleError := setLifecycle(queryer, expeditionID, StatusActive, expeditionRecord.StartTimestamp, nil, nil); lifecycleError != nil {
			return false, lifecycleError
		}
		if initError := initCarrierStops(queryer, expeditionID); initError != nil {
			return false, initError
		}
		var listError error
		rows, listError = ListRows(queryer, expeditionID)
		if listError != nil {
			return false, listError
		}
	}

	originalRowsByID := make(map[int64]Row, len(rows))
	for _, rowItem := range rows {
		originalRowsByID[rowItem.ID] = rowItem
	}
	for i := range rows {
		rows[i].VisitedAt, rows[i].VisitEventUID = "", ""
	}
	modeTimeline, _ := modes.LoadTimeline(queryer)

	var arrivals []Arrival
	var arrivalsError error
	if expeditionRecord.EndTimestamp != "" {
		arrivals, arrivalsError = loadArrivals(queryer, `timestamp >= ? AND timestamp <= ?`, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	} else {
		arrivals, arrivalsError = loadArrivals(queryer, `timestamp >= ?`, expeditionRecord.StartTimestamp)
	}
	if arrivalsError != nil {
		return false, arrivalsError
	}

	// Waypoint #1 is visited at the start timestamp.
	rows[firstWaypointIndex].VisitedAt = expeditionRecord.StartTimestamp
	lastSystemName := strings.ToLower(strings.TrimSpace(rows[firstWaypointIndex].SystemName))
	for _, arrival := range arrivals {
		if arrival.Timestamp == expeditionRecord.StartTimestamp && matches(rows[firstWaypointIndex], arrival) {
			rows[firstWaypointIndex].VisitEventUID = arrival.UID
			if arrival.SystemName != "" {
				lastSystemName = strings.ToLower(strings.TrimSpace(arrival.SystemName))
			}
			break
		}
	}
	currentIndex := firstWaypointIndex

	for _, arrival := range arrivals {
		if arrival.Timestamp == expeditionRecord.StartTimestamp && arrival.UID == rows[firstWaypointIndex].VisitEventUID {
			continue
		}
		systemName := strings.ToLower(strings.TrimSpace(arrival.SystemName))
		if systemName == "" || systemName == lastSystemName {
			continue // not an actual change of system
		}
		lastSystemName = systemName
		targetIndex := -1
		for i, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt == "" && matches(rowItem, arrival) {
				targetIndex = i
				break
			}
		}
		if targetIndex < 0 {
			continue
		}
		rows[targetIndex].VisitedAt, rows[targetIndex].VisitEventUID = arrival.Timestamp, arrival.UID
		if rows[targetIndex].LegModeID == 0 && modeTimeline != nil {
			rows[targetIndex].LegModeID = modeTimeline.ModeAt(arrival.Timestamp)
		}
		nextUnvisitedWaypoint := -1
		for i := currentIndex + 1; i < len(rows); i++ {
			if i == targetIndex || (rows[i].IsWaypoint() && rows[i].VisitedAt == "") {
				nextUnvisitedWaypoint = i
				break
			}
		}
		if targetIndex != nextUnvisitedWaypoint {
			destinationIndex := currentIndex + 1
			if targetIndex < currentIndex {
				destinationIndex = currentIndex // removing target shifts current up by one
			}
			rows = moveTo(rows, targetIndex, destinationIndex)
			targetIndex = destinationIndex
		}
		currentIndex = targetIndex
	}

	changed := false
	for i := range rows {
		rowItem := rows[i]
		originalRow := originalRowsByID[rowItem.ID]
		if originalRow.Position != i+1 {
			changed = true
			if _, updateError := queryer.Exec(`UPDATE expedition_rows SET position = ? WHERE id = ?`, i+1, rowItem.ID); updateError != nil {
				return false, updateError
			}
		}
		if originalRow.VisitedAt != rowItem.VisitedAt || originalRow.VisitEventUID != rowItem.VisitEventUID {
			changed = true
			var visitedAtValue, visitUIDValue any
			if rowItem.VisitedAt != "" {
				visitedAtValue = rowItem.VisitedAt
			}
			if rowItem.VisitEventUID != "" {
				visitUIDValue = rowItem.VisitEventUID
			}
			if _, updateError := queryer.Exec(`UPDATE expedition_rows SET visited_at = ?, visit_event_uid = ? WHERE id = ?`, visitedAtValue, visitUIDValue, rowItem.ID); updateError != nil {
				return false, fmt.Errorf("update visit: %w", updateError)
			}
		}
		if originalRow.FCArrived != rowItem.FCArrived {
			changed = true
			if _, updateError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = ? WHERE id = ?`, rowItem.FCArrived, rowItem.ID); updateError != nil {
				return false, fmt.Errorf("update fc_arrived: %w", updateError)
			}
		}
		if originalRow.LegModeID != rowItem.LegModeID {
			changed = true
			if _, updateError := queryer.Exec(`UPDATE expedition_rows SET leg_mode_id = ? WHERE id = ?`, rowItem.LegModeID, rowItem.ID); updateError != nil {
				return false, fmt.Errorf("update leg_mode_id: %w", updateError)
			}
		}
	}
	return changed, nil
}

// ActiveExpeditionIDs returns expeditions that need re-evaluation on new
// journal data (active, including those waiting for their first arrival).
func ActiveExpeditionIDs(queryer db.Queryer) ([]int64, error) {
	rows, queryError := queryer.Query(`SELECT id FROM expeditions WHERE status = ?`, StatusActive)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	var expeditionIDs []int64
	for rows.Next() {
		var id int64
		if scanError := rows.Scan(&id); scanError != nil {
			return nil, scanError
		}
		expeditionIDs = append(expeditionIDs, id)
	}
	return expeditionIDs, rows.Err()
}

