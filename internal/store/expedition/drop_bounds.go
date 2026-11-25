package expedition

import (
	"errors"
	"sort"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
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
	routeState, getError := GetActiveCarrierRoute(queryer, expeditionID)
	if getError != nil || routeState == nil || routeState.ManualMode || routeState.Route.ManualMode || len(routeState.Route.Jumps) == 0 {
		return 0
	}
	return routeState.TargetRowID
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
		// Moving the dragged row later shifts rows originally behind it earlier in
		// the route, uncovering earlier historical arrivals (for example, skipping a
		// loop leg before a revisited hub) up to originalVisit; all other visited
		// rows must stay pinned to their exact original visit timestamp.
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

// hasLockedOrArrivedCarrierStopAfter reports whether placing a pending FC stop at
// targetIndex would position it ahead of an already-arrived FC stop or the active
// Spansh target stop, which would invert the sequential carrier frontier.
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

// moveResetsCarrierArrival reports whether dragging an arrived FC stop below at
// least one pending FC stop crosses the carrier frontier. Unlike moving a pending
// stop up (which is blocked), dragging an arrived stop past a pending stop is
// allowed with confirmation and clears fc_arrived on the moved stop.
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
	evaluationContext *boundsEvaluationContext,
	rows []Row,
	fromIndex int,
	targetIndex int,
) DropSlotPreview {
	draggedRow := rows[fromIndex]
	// Lock the active Spansh route destination in place so navigation progress
	// stays aligned with its target waypoint row.
	if evaluationContext.lockedSpanshTargetRowID > 0 && draggedRow.ID == evaluationContext.lockedSpanshTargetRowID {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}
	if fromIndex == targetIndex {
		if draggedRow.VisitedAt != "" {
			return DropSlotPreview{
				TargetIndex:       targetIndex,
				Status:            DropSlotValidVisited,
				VisitedAtGalactic: reference.ToGalactic(draggedRow.VisitedAt),
			}
		}
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotValidUnvisited}
	}
	if targetIndex <= evaluationContext.firstWaypointIndex {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}

	copy(evaluationContext.candidateRows, rows)
	copy(evaluationContext.candidateSystems, evaluationContext.normalizedSystemNames)
	for i := range evaluationContext.candidateRows {
		evaluationContext.candidateRows[i].VisitedAt, evaluationContext.candidateRows[i].VisitEventUID = "", ""
	}
	evaluationContext.candidateRows = moveTo(evaluationContext.candidateRows, fromIndex, targetIndex)
	evaluationContext.candidateSystems = moveTo(evaluationContext.candidateSystems, fromIndex, targetIndex)
	evaluationContext.candidateRows = replayIndexedTransitions(
		evaluationContext.candidateRows,
		evaluationContext.candidateSystems,
		evaluationContext.firstWaypointIndex,
		evaluationContext.startTimestamp,
		evaluationContext.originVisitUID,
		evaluationContext.transitions,
	)

	if !isChronologicallyValidMove(
		evaluationContext.candidateRows,
		draggedRow.ID,
		fromIndex,
		targetIndex,
		evaluationContext.firstWaypointIndex,
		rows[evaluationContext.firstWaypointIndex].ID,
		evaluationContext.originalVisitedAt,
		evaluationContext.originalIndexByID,
	) {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}
	// Enforce the asymmetric Fleet Carrier frontier: pending stops cannot move
	// above arrived or Spansh-routed stops, whereas arrived stops moving past a
	// pending stop are permitted with reset confirmation.
	if draggedRow.FCStop && !draggedRow.FCArrived &&
		hasLockedOrArrivedCarrierStopAfter(evaluationContext.candidateRows, draggedRow.ID, targetIndex, evaluationContext.lockedSpanshTargetRowID) {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotBlockedFC}
	}

	resetsFCArrived := moveResetsCarrierArrival(evaluationContext.candidateRows, draggedRow, targetIndex)
	evaluatedDragged := evaluationContext.candidateRows[targetIndex]
	if evaluatedDragged.VisitedAt != "" {
		return DropSlotPreview{
			TargetIndex:       targetIndex,
			Status:            DropSlotValidVisited,
			VisitedAtGalactic: reference.ToGalactic(evaluatedDragged.VisitedAt),
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
	arrivals []Arrival,
) (*boundsEvaluationContext, error) {
	firstWaypointIndex := firstWaypoint(rows)
	if firstWaypointIndex < 0 || expeditionRecord.StartTimestamp == "" {
		return nil, nil
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

	originStay, rawTransitions := prepareNormalizedTransitions(
		&rows[firstWaypointIndex],
		normalizedSystemNames[firstWaypointIndex],
		expeditionRecord.StartTimestamp,
		arrivals,
	)

	return &boundsEvaluationContext{
		firstWaypointIndex:      firstWaypointIndex,
		startTimestamp:          expeditionRecord.StartTimestamp,
		originVisitUID:          originStay.uid,
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
func ComputeRowDropPreviews(queryer db.Queryer, rowID int64, expeditionProjections *Projections) ([]DropSlotPreview, error) {
	if expeditionProjections == nil || expeditionProjections.Arrivals == nil {
		return nil, errors.New("expedition Arrivals projection is required")
	}
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
	arrivals, arrivalsError := expeditionProjections.Arrivals.InWindowScoped(expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp, expeditionRecord.ProfileScope())
	if arrivalsError != nil {
		return nil, arrivalsError
	}
	evaluationContext, prepError := prepareBoundsEvaluation(queryer, expeditionRecord, rows, arrivals)
	if prepError != nil || evaluationContext == nil || fromIndex == evaluationContext.firstWaypointIndex {
		return nil, prepError
	}
	if evaluationContext.lockedSpanshTargetRowID > 0 && rowID == evaluationContext.lockedSpanshTargetRowID {
		return nil, nil
	}

	rowPreviews := make([]DropSlotPreview, len(rows))
	for targetIndex := range rows {
		rowPreviews[targetIndex] = evaluateRowDropTarget(evaluationContext, rows, fromIndex, targetIndex)
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
	arrivals, arrivalsError := replay.QueryArrivalsInWindowScoped(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp, expeditionRecord.ProfileScope())
	if arrivalsError != nil {
		return DropSlotPreview{}, arrivalsError
	}
	evaluationContext, prepError := prepareBoundsEvaluation(queryer, expeditionRecord, rows, arrivals)
	if prepError != nil || evaluationContext == nil {
		return DropSlotPreview{}, prepError
	}
	preview := evaluateRowDropTarget(evaluationContext, rows, fromIndex, toIndex)
	switch preview.Status {
	case DropSlotInvalid:
		return preview, errors.New("waypoint cannot be moved outside its chronological visit bounds")
	case DropSlotBlockedFC:
		return preview, errors.New("pending fleet carrier stop cannot be moved before an arrived or actively routed carrier stop")
	}
	return preview, nil
}
