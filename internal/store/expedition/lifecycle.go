package expedition

import (
	"database/sql"
	"errors"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
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

// StartTx runs Start inside a database transaction on dbConn.
func StartTx(dbConn *sql.DB, expeditionID int64, startTimestamp string) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return Start(transaction, expeditionID, startTimestamp)
	})
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

// CompleteTx runs Complete inside a database transaction on dbConn.
func CompleteTx(dbConn *sql.DB, expeditionID int64, endTimestamp string) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return Complete(transaction, expeditionID, endTimestamp)
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

