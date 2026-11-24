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
	resolvedAddress, stayCommander, contextError := replay.ResolveArrivalStayContext(
		transaction,
		finalRow.VisitEventUID,
		finalRow.VisitedAt,
		finalRow.SystemName,
		finalRow.SystemAddress,
	)
	if contextError != nil {
		return contextError
	}
	stayUpperBound := exclusiveBoundaryBefore(finalRow.DepartedAt, finalRow.VisitedAt)
	lastActivity, activityError := replay.QueryLastInSystemActivityTimestamp(
		transaction,
		finalRow.VisitedAt,
		stayUpperBound,
		finalRow.SystemName,
		resolvedAddress,
		stayCommander,
	)
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

// EffectiveStartFromRows resolves an expedition's telemetry start from Origin
// Waypoint #1's departure timestamp using already-loaded PlanView rows without
// querying expedition_rows again.
func EffectiveStartFromRows(expeditionRecord Expedition, rows []RowView) (queryAfter, displayStart string, departed bool) {
	if expeditionRecord.StartTimestamp == "" {
		return "", "", false
	}
	for index := range rows {
		rowItem := rows[index].Row
		if !rowItem.IsWaypoint() {
			continue
		}
		if rowItem.DepartedAt == "" {
			return "", "", false
		}
		return exclusiveBoundaryBefore(rowItem.DepartedAt, rowItem.VisitedAt), rowItem.DepartedAt, true
	}
	return "", "", false
}

func resolvePlanFallbackModeID(modeList []modes.Mode) int64 {
	for _, modeItem := range modeList {
		if strings.EqualFold(modeItem.Name, "Travel") {
			return modeItem.ID
		}
	}
	for _, modeItem := range modeList {
		if modeItem.IsDefault {
			return modeItem.ID
		}
	}
	if len(modeList) > 0 {
		return modeList[0].ID
	}
	return modes.UnassignedID
}

func resolveLegSpanModeID(rowView RowView, fallbackModeID int64) int64 {
	if rowView.Row.LegModeID > 0 {
		return rowView.Row.LegModeID
	}
	if rowView.LegModeID > 0 {
		return rowView.LegModeID
	}
	return fallbackModeID
}

func collectActiveModeIDs(planView PlanView, fallbackModeID int64) map[int64]bool {
	candidateModeIDs := make(map[int64]bool, len(planView.Modes))
	seenOrigin := false
	hasNonOriginSpan := false
	lastSpanUntil := ""
	for _, rowView := range planView.Rows {
		if !rowView.Row.IsWaypoint() || rowView.Row.VisitedAt == "" {
			continue
		}
		if !seenOrigin {
			seenOrigin = true
			continue
		}
		hasNonOriginSpan = true
		candidateModeIDs[resolveLegSpanModeID(rowView, fallbackModeID)] = true
		if rowView.Row.DepartedAt != "" {
			lastSpanUntil = exclusiveBoundaryBefore(rowView.Row.DepartedAt, rowView.Row.VisitedAt)
		} else {
			lastSpanUntil = "9999-12-31T23:59:59Z"
		}
	}
	effectiveUntil := planView.Expedition.EndTimestamp
	if effectiveUntil == "" {
		effectiveUntil = "9999-12-31T23:59:59Z"
	}
	if !hasNonOriginSpan || effectiveUntil > lastSpanUntil {
		trailingModeID := planView.Expedition.ActiveModeID
		if trailingModeID <= 0 {
			trailingModeID = fallbackModeID
		}
		candidateModeIDs[trailingModeID] = true
	}
	return candidateModeIDs
}

// SelectActiveModes returns the subset of planView.Modes (preserving display
// order) that LoadModeLookup can resolve within the departed expedition window,
// along with whether a single mode covers 100% of that window.
func SelectActiveModes(planView PlanView) ([]modes.Mode, bool) {
	fallbackModeID := resolvePlanFallbackModeID(planView.Modes)
	candidateModeIDs := collectActiveModeIDs(planView, fallbackModeID)
	candidateModes := make([]modes.Mode, 0, len(candidateModeIDs))
	for _, modeItem := range planView.Modes {
		if candidateModeIDs[modeItem.ID] {
			candidateModes = append(candidateModes, modeItem)
		}
	}
	return candidateModes, len(candidateModeIDs) == 1 && len(candidateModes) == 1
}
