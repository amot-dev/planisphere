package expedition

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
)

// Drop slot preview statuses.
const (
	DropSlotValidVisited   = "valid_visited"
	DropSlotValidUnvisited = "valid_unvisited"
	DropSlotBlockedFC      = "blocked_fc"
	DropSlotInvalid        = "invalid"
)

// DropSlotPreview describes the outcome of moving a waypoint row to TargetIndex.
type DropSlotPreview struct {
	TargetIndex       int    `json:"targetIndex"`
	Status            string `json:"status"`
	VisitedAtGalactic string `json:"visitedAtGalactic,omitempty"`
	ResetsFCArrived   bool   `json:"resetsFcArrived,omitempty"`
}

type indexedTransitions struct {
	items     []Arrival
	byName    map[string][]int
	byAddress map[int64][]int
}

type boundsEvaluationContext struct {
	firstWaypointIndex      int
	startTimestamp          string
	originVisitUID          string
	lockedSpanshTargetRowID int64
	normalizedSystemNames   []string
	originalVisitedAt       map[int64]string
	originalIndexByID       map[int64]int
	transitions             indexedTransitions
	candidateRows           []Row
	candidateSystems        []string
}

func readLockedSpanshTargetRowID(queryer db.Queryer, expeditionID int64) int64 {
	rawSetting, getError := db.GetSetting(queryer, fmt.Sprintf("active_carrier_route_%d", expeditionID))
	if getError != nil || rawSetting == "" {
		return 0
	}
	var persistedRoute struct {
		TargetRowID int64 `json:"targetRowId"`
		ManualMode  bool  `json:"manualMode"`
		Route       struct {
			ManualMode bool              `json:"manualMode"`
			Jumps      []json.RawMessage `json:"jumps"`
		} `json:"route"`
	}
	if json.Unmarshal([]byte(rawSetting), &persistedRoute) != nil {
		return 0
	}
	if persistedRoute.ManualMode || persistedRoute.Route.ManualMode || len(persistedRoute.Route.Jumps) == 0 {
		return 0
	}
	return persistedRoute.TargetRowID
}

func buildIndexedTransitions(transitions []Arrival) indexedTransitions {
	index := indexedTransitions{
		items:     transitions,
		byName:    make(map[string][]int, 64),
		byAddress: make(map[int64][]int, 64),
	}
	for i := range transitions {
		name := transitions[i].SystemName
		if name != "" {
			index.byName[name] = append(index.byName[name], i)
		}
		address := transitions[i].SystemAddress
		if address != 0 {
			index.byAddress[address] = append(index.byAddress[address], i)
		}
	}
	return index
}

func firstIndexAtOrAfter(indices []int, minIndex int) int {
	pos := sort.SearchInts(indices, minIndex)
	if pos >= len(indices) {
		return -1
	}
	return indices[pos]
}

func replayIndexedTransitions(
	rows []Row,
	normalizedSystemNames []string,
	firstWaypointIndex int,
	startTimestamp string,
	originVisitUID string,
	transitions indexedTransitions,
) []Row {
	rows[firstWaypointIndex].VisitedAt = startTimestamp
	rows[firstWaypointIndex].VisitEventUID = originVisitUID
	nextWaypointIndex := nextWaypointAfter(rows, firstWaypointIndex)
	minTransitionIndex := 0

	for nextWaypointIndex >= 0 && minTransitionIndex < len(transitions.items) {
		normalizedSystem := normalizedSystemNames[nextWaypointIndex]
		if normalizedSystem == "" {
			break
		}
		matchedTransitionIndex := firstIndexAtOrAfter(transitions.byName[normalizedSystem], minTransitionIndex)
		if rows[nextWaypointIndex].SystemAddress != 0 {
			addressMatchIndex := firstIndexAtOrAfter(transitions.byAddress[rows[nextWaypointIndex].SystemAddress], minTransitionIndex)
			if addressMatchIndex >= 0 && (matchedTransitionIndex < 0 || addressMatchIndex < matchedTransitionIndex) {
				matchedTransitionIndex = addressMatchIndex
			}
		}
		if matchedTransitionIndex < 0 {
			break
		}
		arrival := &transitions.items[matchedTransitionIndex]
		rows[nextWaypointIndex].VisitedAt, rows[nextWaypointIndex].VisitEventUID = arrival.Timestamp, arrival.UID
		minTransitionIndex = matchedTransitionIndex + 1
		nextWaypointIndex = nextWaypointAfter(rows, nextWaypointIndex)
	}
	return rows
}

func isChronologicallyValidMove(
	candidateRows []Row,
	draggedRowID int64,
	fromIndex int,
	targetIndex int,
	firstWaypointIndex int,
	originRowID int64,
	originalVisitedAt map[int64]string,
	originalIndexByID map[int64]int,
) bool {
	if targetIndex <= firstWaypointIndex {
		return false
	}
	firstIdx := firstWaypoint(candidateRows)
	if firstIdx < 0 || candidateRows[firstIdx].ID != originRowID {
		return false
	}
	evaluatedDragged := candidateRows[targetIndex]
	for i, rowItem := range candidateRows {
		if rowItem.ID == draggedRowID {
			continue
		}
		originalVisit, wasVisited := originalVisitedAt[rowItem.ID]
		if !wasVisited {
			continue
		}
		if rowItem.VisitedAt == "" {
			return false
		}
		originalIndex := originalIndexByID[rowItem.ID]
		canRebindEarlier := fromIndex < targetIndex && fromIndex < originalIndex
		if canRebindEarlier {
			if rowItem.VisitedAt > originalVisit {
				return false
			}
		} else if rowItem.VisitedAt != originalVisit {
			return false
		}
		if i > targetIndex && (evaluatedDragged.VisitedAt == "" || evaluatedDragged.VisitedAt >= rowItem.VisitedAt) {
			return false
		}
	}
	return true
}

func hasLockedOrArrivedCarrierStopAfter(
	candidateRows []Row,
	draggedRowID int64,
	targetIndex int,
	lockedSpanshTargetRowID int64,
) bool {
	for i := targetIndex + 1; i < len(candidateRows); i++ {
		rowItem := candidateRows[i]
		if rowItem.ID != draggedRowID && rowItem.IsWaypoint() && rowItem.FCStop &&
			(rowItem.FCArrived || (lockedSpanshTargetRowID > 0 && rowItem.ID == lockedSpanshTargetRowID)) {
			return true
		}
	}
	return false
}

func moveResetsCarrierArrival(candidateRows []Row, draggedRow Row, targetIndex int) bool {
	if !draggedRow.FCStop || !draggedRow.FCArrived {
		return false
	}
	for i := 0; i < targetIndex; i++ {
		rowItem := candidateRows[i]
		if rowItem.ID != draggedRow.ID && rowItem.IsWaypoint() && rowItem.FCStop && !rowItem.FCArrived {
			return true
		}
	}
	return false
}

func evaluateRowDropTarget(
	evalCtx *boundsEvaluationContext,
	rows []Row,
	fromIndex int,
	targetIndex int,
) DropSlotPreview {
	draggedRow := rows[fromIndex]
	if evalCtx.lockedSpanshTargetRowID > 0 && draggedRow.ID == evalCtx.lockedSpanshTargetRowID {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}
	if fromIndex == targetIndex {
		if draggedRow.VisitedAt != "" {
			return DropSlotPreview{
				TargetIndex:       targetIndex,
				Status:            DropSlotValidVisited,
				VisitedAtGalactic: ed.ToGalactic(draggedRow.VisitedAt),
			}
		}
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotValidUnvisited}
	}
	if targetIndex <= evalCtx.firstWaypointIndex {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}

	copy(evalCtx.candidateRows, rows)
	copy(evalCtx.candidateSystems, evalCtx.normalizedSystemNames)
	for i := range evalCtx.candidateRows {
		evalCtx.candidateRows[i].VisitedAt, evalCtx.candidateRows[i].VisitEventUID = "", ""
	}
	evalCtx.candidateRows = moveTo(evalCtx.candidateRows, fromIndex, targetIndex)
	evalCtx.candidateSystems = moveTo(evalCtx.candidateSystems, fromIndex, targetIndex)
	evalCtx.candidateRows = replayIndexedTransitions(
		evalCtx.candidateRows,
		evalCtx.candidateSystems,
		evalCtx.firstWaypointIndex,
		evalCtx.startTimestamp,
		evalCtx.originVisitUID,
		evalCtx.transitions,
	)

	if !isChronologicallyValidMove(
		evalCtx.candidateRows,
		draggedRow.ID,
		fromIndex,
		targetIndex,
		evalCtx.firstWaypointIndex,
		rows[evalCtx.firstWaypointIndex].ID,
		evalCtx.originalVisitedAt,
		evalCtx.originalIndexByID,
	) {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}
	if draggedRow.FCStop && !draggedRow.FCArrived &&
		hasLockedOrArrivedCarrierStopAfter(evalCtx.candidateRows, draggedRow.ID, targetIndex, evalCtx.lockedSpanshTargetRowID) {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotBlockedFC}
	}

	resetsFCArrived := moveResetsCarrierArrival(evalCtx.candidateRows, draggedRow, targetIndex)
	evaluatedDragged := evalCtx.candidateRows[targetIndex]
	if evaluatedDragged.VisitedAt != "" {
		return DropSlotPreview{
			TargetIndex:       targetIndex,
			Status:            DropSlotValidVisited,
			VisitedAtGalactic: ed.ToGalactic(evaluatedDragged.VisitedAt),
			ResetsFCArrived:   resetsFCArrived,
		}
	}
	return DropSlotPreview{
		TargetIndex:     targetIndex,
		Status:          DropSlotValidUnvisited,
		ResetsFCArrived: resetsFCArrived,
	}
}

func prepareBoundsEvaluation(
	queryer db.Queryer,
	expeditionRecord Expedition,
	rows []Row,
	useCache bool,
) (*boundsEvaluationContext, error) {
	firstWaypointIndex := firstWaypoint(rows)
	if firstWaypointIndex < 0 || expeditionRecord.StartTimestamp == "" {
		return nil, nil
	}
	var arrivals []Arrival
	var arrivalsError error
	if useCache {
		arrivals, arrivalsError = loadCachedExpeditionArrivals(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	} else {
		arrivals, arrivalsError = loadExpeditionArrivals(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	}
	if arrivalsError != nil {
		return nil, arrivalsError
	}

	normalizedSystemNames := make([]string, len(rows))
	originalVisitedAt := make(map[int64]string, len(rows))
	originalIndexByID := make(map[int64]int, len(rows))
	for i := range rows {
		originalIndexByID[rows[i].ID] = i
		if rows[i].RowType == RowWaypoint {
			normalizedSystemNames[i] = strings.ToLower(strings.TrimSpace(rows[i].SystemName))
			if rows[i].VisitedAt != "" {
				originalVisitedAt[rows[i].ID] = rows[i].VisitedAt
			}
		}
	}

	originVisitUID, rawTransitions := prepareNormalizedTransitions(
		&rows[firstWaypointIndex],
		normalizedSystemNames[firstWaypointIndex],
		expeditionRecord.StartTimestamp,
		arrivals,
	)

	return &boundsEvaluationContext{
		firstWaypointIndex:      firstWaypointIndex,
		startTimestamp:          expeditionRecord.StartTimestamp,
		originVisitUID:          originVisitUID,
		lockedSpanshTargetRowID: readLockedSpanshTargetRowID(queryer, expeditionRecord.ID),
		normalizedSystemNames:   normalizedSystemNames,
		originalVisitedAt:       originalVisitedAt,
		originalIndexByID:       originalIndexByID,
		transitions:             buildIndexedTransitions(rawTransitions),
		candidateRows:           make([]Row, len(rows)),
		candidateSystems:        make([]string, len(rows)),
	}, nil
}

// ComputeRowDropPreviews evaluates all candidate target indices for moving rowID
// within an active expedition, returning nil when the expedition is not active
// or rowID cannot be reordered.
func ComputeRowDropPreviews(queryer db.Queryer, rowID int64) ([]DropSlotPreview, error) {
	targetRow, getError := GetRow(queryer, rowID)
	if getError != nil {
		return nil, getError
	}
	if !targetRow.IsWaypoint() {
		return nil, nil
	}
	expeditionRecord, expError := Get(queryer, targetRow.ExpeditionID)
	if expError != nil {
		return nil, expError
	}
	if expeditionRecord.Status != StatusActive || expeditionRecord.StartTimestamp == "" {
		return nil, nil
	}
	rows, listError := ListRows(queryer, expeditionRecord.ID)
	if listError != nil {
		return nil, listError
	}
	fromIndex := indexOf(rows, rowID)
	if fromIndex < 0 {
		return nil, nil
	}
	evalCtx, prepError := prepareBoundsEvaluation(queryer, expeditionRecord, rows, true)
	if prepError != nil || evalCtx == nil || fromIndex == evalCtx.firstWaypointIndex {
		return nil, prepError
	}
	if evalCtx.lockedSpanshTargetRowID > 0 && rowID == evalCtx.lockedSpanshTargetRowID {
		return nil, nil
	}

	rowPreviews := make([]DropSlotPreview, len(rows))
	for targetIndex := range rows {
		rowPreviews[targetIndex] = evaluateRowDropTarget(evalCtx, rows, fromIndex, targetIndex)
	}
	return rowPreviews, nil
}

func validateWaypointMove(queryer db.Queryer, expeditionRecord Expedition, rows []Row, fromIndex, toIndex int) (DropSlotPreview, error) {
	firstWaypointIndex := firstWaypoint(rows)
	if firstWaypointIndex < 0 {
		return DropSlotPreview{}, nil
	}
	if fromIndex == firstWaypointIndex {
		return DropSlotPreview{}, errors.New("waypoint #1 cannot be moved after the expedition has started")
	}
	lockedSpanshTargetRowID := readLockedSpanshTargetRowID(queryer, expeditionRecord.ID)
	if lockedSpanshTargetRowID > 0 && rows[fromIndex].ID == lockedSpanshTargetRowID {
		return DropSlotPreview{}, errors.New("waypoint cannot be moved while fleet carrier route is active")
	}
	evalCtx, prepError := prepareBoundsEvaluation(queryer, expeditionRecord, rows, false)
	if prepError != nil || evalCtx == nil {
		return DropSlotPreview{}, prepError
	}
	preview := evaluateRowDropTarget(evalCtx, rows, fromIndex, toIndex)
	switch preview.Status {
	case DropSlotInvalid:
		return preview, errors.New("waypoint cannot be moved outside its chronological visit bounds")
	case DropSlotBlockedFC:
		return preview, errors.New("pending fleet carrier stop cannot be moved before an arrived or actively routed carrier stop")
	}
	return preview, nil
}

// SetCarrierArrivalOverride manually sets the Fleet Carrier arrival status on rowID
// and cascades the change across preceding (when arrived is true) or subsequent
// (when arrived is false) carrier stops so arrived stops always precede pending stops.
func SetCarrierArrivalOverride(transaction *sql.Tx, rowID int64, arrived bool) (int64, error) {
	targetRow, getError := GetRow(transaction, rowID)
	if getError != nil {
		return 0, getError
	}
	if !targetRow.IsWaypoint() || !targetRow.FCStop {
		return targetRow.ExpeditionID, errors.New("row is not a designated fleet carrier stop")
	}
	lockedSpanshTargetRowID := readLockedSpanshTargetRowID(transaction, targetRow.ExpeditionID)
	now := ed.NowISO()
	if arrived {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 1, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position <= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
		}
		if lockedSpanshTargetRowID > 0 {
			routedRow, routedError := GetRow(transaction, lockedSpanshTargetRowID)
			if routedError == nil && routedRow.FCArrived {
				routeSettingKey := fmt.Sprintf("active_carrier_route_%d", targetRow.ExpeditionID)
				if _, clearRouteError := transaction.Exec(`DELETE FROM settings WHERE key = ?`, routeSettingKey); clearRouteError != nil {
					return targetRow.ExpeditionID, fmt.Errorf("clear active carrier route: %w", clearRouteError)
				}
			}
		}
	} else {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 0, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position >= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
		}
	}
	if touchError := touch(transaction, targetRow.ExpeditionID); touchError != nil {
		return targetRow.ExpeditionID, touchError
	}
	return targetRow.ExpeditionID, nil
}
