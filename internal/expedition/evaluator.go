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
func matches(rowItem *Row, arrival Arrival) bool {
	if rowItem.RowType != RowWaypoint || strings.TrimSpace(rowItem.SystemName) == "" {
		return false
	}
	if rowItem.SystemAddress != 0 && arrival.SystemAddress != 0 && rowItem.SystemAddress == arrival.SystemAddress {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(rowItem.SystemName), strings.TrimSpace(arrival.SystemName))
}

func firstWaypoint(rows []Row) int {
	for i := range rows {
		if rows[i].IsWaypoint() {
			return i
		}
	}
	return -1
}

func nextWaypointAfter(rows []Row, afterIndex int) int {
	for i := afterIndex + 1; i < len(rows); i++ {
		if rows[i].IsWaypoint() {
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
	firstIndex := firstWaypoint(rows)
	if firstIndex < 0 || strings.TrimSpace(rows[firstIndex].SystemName) == "" {
		return "", nil, errors.New("waypoint #1 needs a system name before the expedition can start")
	}
	waypoint := rows[firstIndex]
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
	if firstIndex := firstWaypoint(rows); firstIndex < 0 || rows[firstIndex].SystemName == "" {
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
	if initError := initCarrierStops(transaction, expeditionID); initError != nil {
		return initError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
}

// initCarrierStops initializes carrier stops for an active expedition.
// The first carrier stop serves as the carrier's origin or starting location,
// so it starts as arrived; subsequent carrier stops start as pending.
func initCarrierStops(queryer db.Queryer, expeditionID int64) error {
	if _, updateError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE expedition_id = ?`, expeditionID); updateError != nil {
		return updateError
	}
	_, execError := queryer.Exec(`UPDATE expedition_rows SET fc_arrived = 1
		WHERE id = (
			SELECT id FROM expedition_rows
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND TRIM(system_name) != ''
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

func resolveArmedExpeditionStart(queryer db.Queryer, expeditionRecord *Expedition, rows *[]Row, firstWaypointIndex int) (bool, error) {
	if expeditionRecord.ArmedAt == "" {
		return false, nil
	}
	candidateArrivals, loadError := loadArrivals(queryer, `timestamp >= ?`, expeditionRecord.ArmedAt)
	if loadError != nil {
		return false, loadError
	}
	for _, arrival := range candidateArrivals {
		if matches(&(*rows)[firstWaypointIndex], arrival) {
			expeditionRecord.StartTimestamp = arrival.Timestamp
			break
		}
	}
	if expeditionRecord.StartTimestamp == "" {
		return false, nil
	}
	if lifecycleError := setLifecycle(queryer, expeditionRecord.ID, StatusActive, expeditionRecord.StartTimestamp, nil, nil); lifecycleError != nil {
		return false, lifecycleError
	}
	expeditionRecord.Status = StatusActive
	expeditionRecord.ArmedAt = ""
	if initError := initCarrierStops(queryer, expeditionRecord.ID); initError != nil {
		return false, initError
	}
	reloadedRows, listError := ListRows(queryer, expeditionRecord.ID)
	if listError != nil {
		return false, listError
	}
	*rows = reloadedRows
	return true, nil
}

func matchesNormalizedWaypoint(rowItem *Row, normalizedRowSystem string, arrivalSystemAddress int64, normalizedArrivalSystem string) bool {
	if rowItem.RowType != RowWaypoint || normalizedRowSystem == "" {
		return false
	}
	if rowItem.SystemAddress != 0 && arrivalSystemAddress != 0 && rowItem.SystemAddress == arrivalSystemAddress {
		return true
	}
	return normalizedRowSystem == normalizedArrivalSystem
}

func loadExpeditionArrivals(queryer db.Queryer, startTimestamp, endTimestamp string) ([]Arrival, error) {
	if endTimestamp != "" {
		return loadArrivals(queryer, `timestamp >= ? AND timestamp <= ?`, startTimestamp, endTimestamp)
	}
	return loadArrivals(queryer, `timestamp >= ?`, startTimestamp)
}

func loadCachedExpeditionArrivals(queryer db.Queryer, startTimestamp, endTimestamp string) ([]Arrival, error) {
	cacheKey := fmt.Sprintf("%s|%s", startTimestamp, endTimestamp)
	shipCacheMu.Lock()
	if lastMaxJournalID >= 0 {
		if cachedArrivals, ok := arrivalsCacheMap[cacheKey]; ok {
			shipCacheMu.Unlock()
			return cachedArrivals, nil
		}
	}
	capturedMaxID := lastMaxJournalID
	shipCacheMu.Unlock()

	arrivals, loadError := loadExpeditionArrivals(queryer, startTimestamp, endTimestamp)
	if loadError != nil {
		return nil, loadError
	}

	shipCacheMu.Lock()
	if capturedMaxID >= 0 && lastMaxJournalID == capturedMaxID {
		arrivalsCacheMap[cacheKey] = arrivals
	}
	shipCacheMu.Unlock()
	return arrivals, nil
}

// prepareNormalizedTransitions extracts the origin visit event UID and filters
// arrivals into chronological system-change transitions with normalized names.
func prepareNormalizedTransitions(originRow *Row, normalizedOriginSystem, startTimestamp string, arrivals []Arrival) (string, []Arrival) {
	originVisitUID := ""
	lastSystemName := normalizedOriginSystem
	for _, arrival := range arrivals {
		normalizedArrivalSystem := strings.ToLower(strings.TrimSpace(arrival.SystemName))
		if arrival.Timestamp == startTimestamp && matchesNormalizedWaypoint(originRow, normalizedOriginSystem, arrival.SystemAddress, normalizedArrivalSystem) {
			originVisitUID = arrival.UID
			if normalizedArrivalSystem != "" {
				lastSystemName = normalizedArrivalSystem
			}
			break
		}
	}

	transitions := make([]Arrival, 0, len(arrivals))
	for _, arrival := range arrivals {
		if arrival.Timestamp == startTimestamp && arrival.UID == originVisitUID {
			continue
		}
		if arrival.Timestamp <= startTimestamp {
			continue
		}
		normalizedArrivalSystem := strings.ToLower(strings.TrimSpace(arrival.SystemName))
		if normalizedArrivalSystem == "" || normalizedArrivalSystem == lastSystemName {
			continue // not an actual change of system
		}
		lastSystemName = normalizedArrivalSystem
		transitionItem := arrival
		transitionItem.SystemName = normalizedArrivalSystem
		transitions = append(transitions, transitionItem)
	}
	return originVisitUID, transitions
}

func replayNormalizedTransitions(
	rows []Row,
	normalizedSystemNames []string,
	firstWaypointIndex int,
	startTimestamp string,
	originVisitUID string,
	transitions []Arrival,
	modeTimeline *modes.Timeline,
) []Row {
	rows[firstWaypointIndex].VisitedAt = startTimestamp
	rows[firstWaypointIndex].VisitEventUID = originVisitUID
	nextWaypointIndex := nextWaypointAfter(rows, firstWaypointIndex)

	for i := range transitions {
		if nextWaypointIndex < 0 {
			break
		}
		arrival := &transitions[i]
		if !matchesNormalizedWaypoint(&rows[nextWaypointIndex], normalizedSystemNames[nextWaypointIndex], arrival.SystemAddress, arrival.SystemName) {
			continue
		}
		rows[nextWaypointIndex].VisitedAt, rows[nextWaypointIndex].VisitEventUID = arrival.Timestamp, arrival.UID
		if rows[nextWaypointIndex].LegModeID == 0 && modeTimeline != nil {
			rows[nextWaypointIndex].LegModeID = modeTimeline.ModeAt(arrival.Timestamp)
		}
		nextWaypointIndex = nextWaypointAfter(rows, nextWaypointIndex)
	}
	return rows
}

func replayExpeditionArrivals(rows []Row, firstWaypointIndex int, startTimestamp string, arrivals []Arrival, modeTimeline *modes.Timeline) []Row {
	normalizedSystemNames := make([]string, len(rows))
	for i := range rows {
		if rows[i].RowType == RowWaypoint {
			normalizedSystemNames[i] = strings.ToLower(strings.TrimSpace(rows[i].SystemName))
		}
	}
	originVisitUID, transitions := prepareNormalizedTransitions(&rows[firstWaypointIndex], normalizedSystemNames[firstWaypointIndex], startTimestamp, arrivals)
	return replayNormalizedTransitions(rows, normalizedSystemNames, firstWaypointIndex, startTimestamp, originVisitUID, transitions, modeTimeline)
}

func reconcileEvaluatedRowState(rows []Row, originalRowsByID map[int64]Row) {
	seenPendingFCStop := false
	for i := range rows {
		if !rows[i].IsWaypoint() {
			continue
		}
		originalRow := originalRowsByID[rows[i].ID]
		if rows[i].VisitedAt == "" && originalRow.VisitedAt != "" {
			rows[i].LegModeID = 0
		}
		if rows[i].FCStop {
			if seenPendingFCStop && rows[i].FCArrived {
				rows[i].FCArrived = false
			}
			if !rows[i].FCArrived {
				seenPendingFCStop = true
			}
		}
	}
}

func persistEvaluatedRowChanges(queryer db.Queryer, rows []Row, originalRowsByID map[int64]Row) (bool, error) {
	var updateStatement *sql.Stmt
	changed := false
	for i := range rows {
		rowItem := rows[i]
		originalRow := originalRowsByID[rowItem.ID]
		newPosition := i + 1
		if originalRow.Position == newPosition &&
			originalRow.VisitedAt == rowItem.VisitedAt &&
			originalRow.VisitEventUID == rowItem.VisitEventUID &&
			originalRow.FCArrived == rowItem.FCArrived &&
			originalRow.LegModeID == rowItem.LegModeID {
			continue
		}

		changed = true
		if updateStatement == nil {
			var prepError error
			updateStatement, prepError = queryer.Prepare(`UPDATE expedition_rows SET position = ?, visited_at = ?, visit_event_uid = ?, fc_arrived = ?, leg_mode_id = ? WHERE id = ?`)
			if prepError != nil {
				return false, fmt.Errorf("prepare update row: %w", prepError)
			}
			defer updateStatement.Close()
		}

		var visitedAtValue, visitUIDValue any
		if rowItem.VisitedAt != "" {
			visitedAtValue = rowItem.VisitedAt
		}
		if rowItem.VisitEventUID != "" {
			visitUIDValue = rowItem.VisitEventUID
		}
		if _, updateError := updateStatement.Exec(newPosition, visitedAtValue, visitUIDValue, rowItem.FCArrived, rowItem.LegModeID, rowItem.ID); updateError != nil {
			return false, fmt.Errorf("update row %d: %w", rowItem.ID, updateError)
		}
	}
	return changed, nil
}

// Reevaluate replays journal arrivals from the expedition start in
// chronological order and recomputes which waypoints were visited sequentially:
//
//   - Waypoint #1 is visited at the start timestamp.
//   - Arrivals at or before the start timestamp are ignored.
//   - An arrival only counts when the system differs from the previous
//     arrival's system (logging in where you already are is not a visit).
//   - Only the immediate next unvisited waypoint can be completed by an arrival;
//     subsequent waypoints wait until all preceding waypoints have been visited.
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

	// Wait for first arrival: pick the first arrival at Waypoint #1 after armed_at.
	if expeditionRecord.StartTimestamp == "" {
		started, armError := resolveArmedExpeditionStart(queryer, &expeditionRecord, &rows, firstWaypointIndex)
		if armError != nil || !started {
			return false, armError
		}
	}

	originalRowsByID := make(map[int64]Row, len(rows))
	for _, rowItem := range rows {
		originalRowsByID[rowItem.ID] = rowItem
	}
	for i := range rows {
		rows[i].VisitedAt, rows[i].VisitEventUID = "", ""
	}
	modeTimeline, timelineError := modes.LoadTimeline(queryer)
	if timelineError != nil {
		return false, fmt.Errorf("load mode timeline: %w", timelineError)
	}

	arrivals, arrivalsError := loadExpeditionArrivals(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	if arrivalsError != nil {
		return false, arrivalsError
	}

	rows = replayExpeditionArrivals(rows, firstWaypointIndex, expeditionRecord.StartTimestamp, arrivals, modeTimeline)
	reconcileEvaluatedRowState(rows, originalRowsByID)
	changed, persistError := persistEvaluatedRowChanges(queryer, rows, originalRowsByID)
	if changed {
		InvalidateVisitCache()
	}
	return changed, persistError
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
