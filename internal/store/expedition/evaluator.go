package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/projections"
	"planisphere/internal/reference"
	"planisphere/internal/store/modes"
)

// Arrival is a journal event that placed the player in a system.
type Arrival = projections.Arrival

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
func StartCandidates(queryer db.Queryer, expeditionID int64, proj *Projections) (system string, candidates []Arrival, loadError error) {
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
	if proj != nil && proj.Arrivals != nil {
		arrivals, arrivalsError = proj.Arrivals.CandidatesForSystem(waypoint.SystemName, waypoint.SystemAddress)
	} else {
		arrivals, arrivalsError = projections.QuerySystemArrivalCandidates(queryer, waypoint.SystemName, waypoint.SystemAddress)
	}
	if arrivalsError != nil {
		return waypoint.SystemName, nil, arrivalsError
	}
	return waypoint.SystemName, arrivals, nil
}

// Start activates the expedition with startTimestamp as the arrival at
// Waypoint #1, then replays all journal history from that moment.
func Start(transaction *sql.Tx, expeditionID int64, startTimestamp string) error {
	normalizedStartTimestamp, parseError := reference.NormalizeISO(startTimestamp)
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
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusActive, normalizedStartTimestamp, nil); lifecycleError != nil {
		return lifecycleError
	}
	if _, resetFCError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 0 WHERE expedition_id = ?`, expeditionID); resetFCError != nil {
		return resetFCError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
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
	normalizedEndTimestamp := reference.NowISO()
	if endTimestamp != "" {
		var parseError error
		if normalizedEndTimestamp, parseError = reference.NormalizeISO(endTimestamp); parseError != nil {
			return parseError
		}
	}
	if normalizedEndTimestamp <= expeditionRecord.StartTimestamp {
		return errors.New("end must be after the expedition start")
	}
	if pruneError := PruneEmptyRows(transaction, expeditionID); pruneError != nil {
		return pruneError
	}
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusCompleted, expeditionRecord.StartTimestamp, normalizedEndTimestamp); lifecycleError != nil {
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
	if lifecycleError := setLifecycle(transaction, expeditionID, StatusActive, expeditionRecord.StartTimestamp, nil); lifecycleError != nil {
		return lifecycleError
	}
	_, reevalError := Reevaluate(transaction, expeditionID)
	return reevalError
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

func reconcileEvaluatedRowState(rows []Row, originalRowsByID map[int64]Row, modeTimeline *modes.Timeline) {
	seenPendingFCStop := false
	for i := range rows {
		if !rows[i].IsWaypoint() {
			continue
		}
		originalRow := originalRowsByID[rows[i].ID]
		if rows[i].VisitedAt == "" && originalRow.VisitedAt != "" {
			rows[i].LegModeID = 0
		} else if rows[i].VisitedAt != "" && originalRow.VisitedAt != "" && rows[i].VisitedAt != originalRow.VisitedAt {
			if modeTimeline != nil {
				rows[i].LegModeID = modeTimeline.ModeAt(rows[i].VisitedAt)
			} else {
				rows[i].LegModeID = 0
			}
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
	if expeditionRecord.Status == StatusPlanning || expeditionRecord.StartTimestamp == "" {
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

	arrivals, arrivalsError := projections.QueryArrivalsInWindow(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	if arrivalsError != nil {
		return false, arrivalsError
	}

	rows = replayExpeditionArrivals(rows, firstWaypointIndex, expeditionRecord.StartTimestamp, arrivals, modeTimeline)
	reconcileEvaluatedRowState(rows, originalRowsByID, modeTimeline)
	return persistEvaluatedRowChanges(queryer, rows, originalRowsByID)
}

// ActiveExpeditionIDs returns active expeditions that need re-evaluation on new journal data.
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
