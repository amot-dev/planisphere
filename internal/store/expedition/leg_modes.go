package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/modes"
)

type legModeSpan struct {
	until  string
	modeID int64
}

// SetActiveMode updates the per-expedition active flight mode for a planning or
// active expedition. Completed expeditions have no active in-progress leg and
// reject active mode changes.
func SetActiveMode(transaction *sql.Tx, expeditionID, modeID int64) error {
	expeditionRecord, getError := Get(transaction, expeditionID)
	if getError != nil {
		return getError
	}
	if expeditionRecord.Status == StatusCompleted {
		return errors.New("completed expedition flight mode cannot be changed")
	}
	modeExists, existsError := modes.Exists(transaction, modeID)
	if existsError != nil {
		return existsError
	}
	if !modeExists {
		return fmt.Errorf("mode %d not found", modeID)
	}
	_, execError := transaction.Exec(
		`UPDATE expeditions SET active_mode_id = ?, updated_at = ? WHERE id = ?`,
		modeID, reference.NowISO(), expeditionID,
	)
	return execError
}

// SetActiveModeTx runs SetActiveMode inside a database transaction on dbConn.
func SetActiveModeTx(dbConn *sql.DB, expeditionID, modeID int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		return SetActiveMode(transaction, expeditionID, modeID)
	})
}

// SetLegMode assigns modeID to the leg arriving at rowID. For a visited
// non-origin waypoint, cascade=false updates only rowID while cascade=true
// overwrites all visited waypoints from rowID onward without modifying the
// expedition's active in-progress flight mode. For an unvisited active leg,
// it updates the expedition's active_mode_id so the header selector and drawer
// stay synchronized.
func SetLegMode(transaction *sql.Tx, rowID, modeID int64, cascade bool) (int64, error) {
	modeExists, existsError := modes.Exists(transaction, modeID)
	if existsError != nil {
		return 0, existsError
	}
	if !modeExists {
		return 0, fmt.Errorf("mode %d not found", modeID)
	}
	targetRow, rowError := GetRow(transaction, rowID)
	if rowError != nil {
		return 0, rowError
	}
	if !targetRow.IsWaypoint() {
		return targetRow.ExpeditionID, errors.New("flight mode can only be assigned to a waypoint row")
	}
	if targetRow.VisitedAt == "" {
		return targetRow.ExpeditionID, SetActiveMode(transaction, targetRow.ExpeditionID, modeID)
	}

	rows, listError := ListRows(transaction, targetRow.ExpeditionID)
	if listError != nil {
		return targetRow.ExpeditionID, listError
	}
	originIndex := firstWaypoint(rows)
	if originIndex < 0 || rows[originIndex].ID == targetRow.ID {
		return targetRow.ExpeditionID, errors.New("origin waypoint does not have an incoming leg flight mode")
	}

	now := reference.NowISO()
	if cascade {
		if _, execError := transaction.Exec(
			`UPDATE expedition_rows SET leg_mode_id = ?, updated_at = ?
			 WHERE expedition_id = ? AND row_type = 'waypoint' AND COALESCE(visited_at, '') != '' AND position >= ? AND id != ?`,
			modeID, now, targetRow.ExpeditionID, targetRow.Position, rows[originIndex].ID,
		); execError != nil {
			return targetRow.ExpeditionID, execError
		}
	} else {
		if _, execError := transaction.Exec(
			`UPDATE expedition_rows SET leg_mode_id = ?, updated_at = ? WHERE id = ?`,
			modeID, now, rowID,
		); execError != nil {
			return targetRow.ExpeditionID, execError
		}
	}
	if touchError := touch(transaction, targetRow.ExpeditionID); touchError != nil {
		return targetRow.ExpeditionID, touchError
	}
	return targetRow.ExpeditionID, nil
}

// SetLegModeTx runs SetLegMode inside a database transaction on dbConn.
func SetLegModeTx(dbConn *sql.DB, rowID, modeID int64, cascade bool) (int64, error) {
	var expeditionID int64
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		var setError error
		expeditionID, setError = SetLegMode(transaction, rowID, modeID, cascade)
		return setError
	})
	return expeditionID, txError
}

// LoadModeLookup constructs an O(log N) timestamp-to-modeID lookup closure
// from expeditionID's visited waypoint legs. Visited legs end 1 second before
// departed_at (exclusiveBoundaryBefore(departedAt, visitedAt), clamped at
// visitedAt) so outbound departure jumps map to the next leg (or trailing
// active_mode_id once departed from the latest visited waypoint).
func LoadModeLookup(queryer db.Queryer, expeditionID int64) (replaystats.ModeLookup, error) {
	fallbackModeID := modes.DefaultFallbackModeID(queryer)
	if expeditionID <= 0 {
		return func(_ string) int64 { return fallbackModeID }, nil
	}
	expeditionRecord, getError := Get(queryer, expeditionID)
	if getError != nil {
		return nil, getError
	}
	trailingModeID := expeditionRecord.ActiveModeID
	if trailingModeID <= 0 {
		trailingModeID = fallbackModeID
	}

	rows, queryError := queryer.Query(
		`SELECT COALESCE(visited_at, ''), COALESCE(departed_at, ''), COALESCE(leg_mode_id, 0)
		 FROM expedition_rows
		 WHERE expedition_id = ? AND row_type = 'waypoint' AND COALESCE(visited_at, '') != ''
		 ORDER BY position, id`,
		expeditionID,
	)
	if queryError != nil {
		return nil, fmt.Errorf("load leg mode spans for expedition %d: %w", expeditionID, queryError)
	}
	defer rows.Close()

	var spans []legModeSpan
	seenOrigin := false
	for rows.Next() {
		var visitedAt, departedAt string
		var legModeID int64
		if scanError := rows.Scan(&visitedAt, &departedAt, &legModeID); scanError != nil {
			return nil, fmt.Errorf("scan leg mode span for expedition %d: %w", expeditionID, scanError)
		}
		if !seenOrigin {
			seenOrigin = true
			continue
		}
		if legModeID <= 0 {
			legModeID = fallbackModeID
		}
		spanUntil := "9999-12-31T23:59:59Z"
		if departedAt != "" {
			spanUntil = exclusiveBoundaryBefore(departedAt, visitedAt)
		}
		spans = append(spans, legModeSpan{until: spanUntil, modeID: legModeID})
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, iterationError
	}

	return func(timestamp string) int64 {
		matchedIndex := sort.Search(len(spans), func(index int) bool {
			return spans[index].until >= timestamp
		})
		if matchedIndex < len(spans) {
			return spans[matchedIndex].modeID
		}
		return trailingModeID
	}, nil
}
