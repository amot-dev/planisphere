package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/store/modes"
)

func setLifecycle(queryer db.Queryer, expeditionID int64, status string, startTimestamp, endTimestamp any) error {
	_, execError := queryer.Exec(`UPDATE expeditions SET status = ?, start_timestamp = ?, end_timestamp = ?, armed_at = NULL, updated_at = ? WHERE id = ?`,
		status, startTimestamp, endTimestamp, reference.NowISO(), expeditionID)
	return execError
}

// StartCandidates returns recorded arrivals matching Waypoint #1 (newest first).
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
	arrivals, arrivalsError := replay.QuerySystemArrivalCandidates(queryer, waypoint.SystemName, waypoint.SystemAddress)
	if arrivalsError != nil {
		return waypoint.SystemName, nil, arrivalsError
	}
	return waypoint.SystemName, arrivals, nil
}

// Start activates the expedition with startTimestamp as the arrival at
// Waypoint #1, sets the expedition's starting active flight mode when
// initialModeID is positive (falling back to the expedition's saved active
// mode or Travel), and replays all journal history from that moment.
func Start(transaction *sql.Tx, expeditionID int64, startTimestamp string, initialModeID int64) error {
	normalizedStartTimestamp, parseError := reference.NormalizeISO(startTimestamp)
	if parseError != nil {
		return parseError
	}
	expeditionRecord, getError := Get(transaction, expeditionID)
	if getError != nil {
		return getError
	}
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return listError
	}
	if firstIndex := firstWaypoint(rows); firstIndex < 0 || rows[firstIndex].SystemName == "" {
		return errors.New("waypoint #1 needs a system name before the expedition can start")
	}
	resolvedModeID := expeditionRecord.ActiveModeID
	if initialModeID > 0 {
		modeExists, existsError := modes.Exists(transaction, initialModeID)
		if existsError != nil {
			return existsError
		}
		if !modeExists {
			return fmt.Errorf("mode %d not found", initialModeID)
		}
		resolvedModeID = initialModeID
	}
	if resolvedModeID <= 0 {
		resolvedModeID = modes.DefaultFallbackModeID(transaction)
	}
	if resolvedModeID != expeditionRecord.ActiveModeID {
		if _, modeUpdateError := transaction.Exec(
			`UPDATE expeditions SET active_mode_id = ? WHERE id = ?`,
			resolvedModeID, expeditionID,
		); modeUpdateError != nil {
			return modeUpdateError
		}
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

// StartTx runs Start inside a database transaction on dbConn.
func StartTx(dbConn *sql.DB, expeditionID int64, startTimestamp string, initialModeID int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return Start(transaction, expeditionID, startTimestamp, initialModeID)
	})
}

// Complete ends the expedition at the final visited waypoint's last in-system
// exploration activity (or its arrival timestamp when no exploration activity
// occurred before departure).
func Complete(transaction *sql.Tx, expeditionID int64) error {
	expeditionRecord, getError := Get(transaction, expeditionID)
	if getError != nil {
		return getError
	}
	if expeditionRecord.Status != StatusActive || expeditionRecord.StartTimestamp == "" {
		return errors.New("only a started, active expedition can be completed")
	}
	activeCarrierRoute, routeError := GetActiveCarrierRoute(transaction, expeditionID)
	if routeError != nil {
		return routeError
	}
	if activeCarrierRoute != nil {
		return errors.New("close or finish the active fleet carrier route before completing the expedition")
	}
	rows, listError := ListRows(transaction, expeditionID)
	if listError != nil {
		return listError
	}
	finalRow, visitedWaypointCount := latestVisitedWaypoint(rows)
	if visitedWaypointCount < 2 || finalRow == nil {
		return errors.New("at least 2 visited waypoints are required to complete an expedition")
	}
	stayUpperBound := exclusiveBoundaryBefore(finalRow.DepartedAt, finalRow.VisitedAt)
	lastActivity, activityError := replay.QueryLastInSystemActivityTimestamp(transaction, finalRow.VisitedAt, stayUpperBound)
	if activityError != nil {
		return activityError
	}
	finalEndTimestamp := finalRow.VisitedAt
	if lastActivity > finalEndTimestamp {
		finalEndTimestamp = lastActivity
	}
	normalizedEndTimestamp, parseError := reference.NormalizeISO(finalEndTimestamp)
	if parseError != nil {
		return parseError
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

// CompleteTx runs Complete inside a database transaction on dbConn.
func CompleteTx(dbConn *sql.DB, expeditionID int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return Complete(transaction, expeditionID)
	})
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

// ReopenTx runs Reopen inside a database transaction on dbConn.
func ReopenTx(dbConn *sql.DB, expeditionID int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return Reopen(transaction, expeditionID)
	})
}
