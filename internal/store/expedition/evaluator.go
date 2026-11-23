package expedition

import (
	"database/sql"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/replay"
	"planisphere/internal/store/modes"
)

// Arrival is a journal event that placed the player in a system.
type Arrival = replay.Arrival

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
		nextWaypointIndex = nextWaypointAfter(rows, nextWaypointIndex)
	}
	return rows
}

func replayExpeditionArrivals(rows []Row, firstWaypointIndex int, startTimestamp string, arrivals []Arrival) []Row {
	normalizedSystemNames := make([]string, len(rows))
	for i := range rows {
		if rows[i].RowType == RowWaypoint {
			normalizedSystemNames[i] = strings.ToLower(strings.TrimSpace(rows[i].SystemName))
		}
	}
	originVisitUID, transitions := prepareNormalizedTransitions(&rows[firstWaypointIndex], normalizedSystemNames[firstWaypointIndex], startTimestamp, arrivals)
	return replayNormalizedTransitions(rows, normalizedSystemNames, firstWaypointIndex, startTimestamp, originVisitUID, transitions)
}

// reconcileEvaluatedRowState synchronizes derived row fields after chronological
// re-evaluation. A reverse pass assigns LegModeID so waypoints splitting an
// existing visited leg inherit that leg's mode, frontier visits inherit
// activeModeID, unvisited rows clear LegModeID, and already-visited rows preserve
// their existing mode even if rebound to a different arrival timestamp. A forward
// pass clears FCArrived flags that sit after a pending FC stop so Fleet Carrier
// progress remains strictly sequential.
func reconcileEvaluatedRowState(rows []Row, firstWaypointIndex int, originalRowsByID map[int64]Row, activeModeID int64) {
	var nextPreExistingLegModeID int64
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].IsWaypoint() {
			continue
		}
		if i == firstWaypointIndex {
			rows[i].LegModeID = 0
			continue
		}
		originalRow := originalRowsByID[rows[i].ID]
		if rows[i].VisitedAt == "" {
			if originalRow.VisitedAt != "" {
				rows[i].LegModeID = 0
			}
			continue
		}
		if originalRow.VisitedAt != "" {
			if rows[i].LegModeID == 0 {
				rows[i].LegModeID = activeModeID
			}
			nextPreExistingLegModeID = rows[i].LegModeID
			continue
		}
		if rows[i].LegModeID == 0 {
			if nextPreExistingLegModeID > 0 {
				rows[i].LegModeID = nextPreExistingLegModeID
			} else {
				rows[i].LegModeID = activeModeID
			}
		}
	}

	seenPendingFCStop := false
	for i := range rows {
		if !rows[i].IsWaypoint() || !rows[i].FCStop {
			continue
		}
		if seenPendingFCStop && rows[i].FCArrived {
			rows[i].FCArrived = false
		}
		if !rows[i].FCArrived {
			seenPendingFCStop = true
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
	activeModeID := expeditionRecord.ActiveModeID
	if activeModeID <= 0 {
		activeModeID = modes.DefaultFallbackModeID(queryer)
	}

	arrivals, arrivalsError := replay.QueryArrivalsInWindow(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	if arrivalsError != nil {
		return false, arrivalsError
	}

	rows = replayExpeditionArrivals(rows, firstWaypointIndex, expeditionRecord.StartTimestamp, arrivals)
	reconcileEvaluatedRowState(rows, firstWaypointIndex, originalRowsByID, activeModeID)
	return persistEvaluatedRowChanges(queryer, rows, originalRowsByID)
}

// ReevaluateTx runs Reevaluate inside a database transaction on dbConn.
func ReevaluateTx(dbConn *sql.DB, expeditionID int64) (bool, error) {
	var changed bool
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var reevalError error
		changed, reevalError = Reevaluate(transaction, expeditionID)
		return reevalError
	})
	return changed, txError
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
