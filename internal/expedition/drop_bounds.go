package expedition

import (
	"database/sql"
	"errors"
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

func isChronologicallyValidMove(
	candidateRows []Row,
	draggedRowID int64,
	targetIndex int,
	firstWaypointIndex int,
	originRowID int64,
	originalVisitedAt map[int64]string,
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
		if i < targetIndex && rowItem.VisitedAt != originalVisit {
			return false
		}
		if i > targetIndex && (evaluatedDragged.VisitedAt == "" || evaluatedDragged.VisitedAt >= originalVisit) {
			return false
		}
	}
	return true
}

func hasArrivedCarrierStopAfter(candidateRows []Row, draggedRowID int64, targetIndex int) bool {
	for i := targetIndex + 1; i < len(candidateRows); i++ {
		rowItem := candidateRows[i]
		if rowItem.ID != draggedRowID && rowItem.IsWaypoint() && rowItem.FCStop && rowItem.FCArrived {
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
	rows []Row,
	normalizedSystemNames []string,
	fromIndex int,
	targetIndex int,
	firstWaypointIndex int,
	startTimestamp string,
	originVisitUID string,
	transitions []Arrival,
	originalVisitedAt map[int64]string,
	candidateRows []Row,
	candidateSystems []string,
) DropSlotPreview {
	draggedRow := rows[fromIndex]
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
	if targetIndex <= firstWaypointIndex {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}

	copy(candidateRows, rows)
	copy(candidateSystems, normalizedSystemNames)
	for i := range candidateRows {
		candidateRows[i].VisitedAt, candidateRows[i].VisitEventUID = "", ""
	}
	candidateRows = moveTo(candidateRows, fromIndex, targetIndex)
	candidateSystems = moveTo(candidateSystems, fromIndex, targetIndex)
	candidateRows = replayNormalizedTransitions(
		candidateRows,
		candidateSystems,
		firstWaypointIndex,
		startTimestamp,
		originVisitUID,
		transitions,
		nil,
	)

	if !isChronologicallyValidMove(candidateRows, draggedRow.ID, targetIndex, firstWaypointIndex, rows[firstWaypointIndex].ID, originalVisitedAt) {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotInvalid}
	}
	if draggedRow.FCStop && !draggedRow.FCArrived && hasArrivedCarrierStopAfter(candidateRows, draggedRow.ID, targetIndex) {
		return DropSlotPreview{TargetIndex: targetIndex, Status: DropSlotBlockedFC}
	}

	resetsFCArrived := moveResetsCarrierArrival(candidateRows, draggedRow, targetIndex)
	evaluatedDragged := candidateRows[targetIndex]
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

func computePlanDropPreviews(rows []Row, startTimestamp string, arrivals []Arrival) [][]DropSlotPreview {
	firstWaypointIndex := firstWaypoint(rows)
	if firstWaypointIndex < 0 || startTimestamp == "" {
		return nil
	}
	normalizedSystemNames := make([]string, len(rows))
	originalVisitedAt := make(map[int64]string, len(rows))
	for i := range rows {
		if rows[i].RowType == RowWaypoint {
			normalizedSystemNames[i] = strings.ToLower(strings.TrimSpace(rows[i].SystemName))
			if rows[i].VisitedAt != "" {
				originalVisitedAt[rows[i].ID] = rows[i].VisitedAt
			}
		}
	}

	originVisitUID, transitions := prepareNormalizedTransitions(
		&rows[firstWaypointIndex],
		normalizedSystemNames[firstWaypointIndex],
		startTimestamp,
		arrivals,
	)

	candidateRows := make([]Row, len(rows))
	candidateSystems := make([]string, len(rows))
	allPreviews := make([][]DropSlotPreview, len(rows))
	for fromIndex := range rows {
		if !rows[fromIndex].IsWaypoint() || fromIndex == firstWaypointIndex {
			continue
		}
		rowPreviews := make([]DropSlotPreview, len(rows))
		for targetIndex := range rows {
			rowPreviews[targetIndex] = evaluateRowDropTarget(
				rows,
				normalizedSystemNames,
				fromIndex,
				targetIndex,
				firstWaypointIndex,
				startTimestamp,
				originVisitUID,
				transitions,
				originalVisitedAt,
				candidateRows,
				candidateSystems,
			)
		}
		allPreviews[fromIndex] = rowPreviews
	}
	return allPreviews
}

func validateWaypointMove(queryer db.Queryer, expeditionRecord Expedition, rows []Row, fromIndex, toIndex int) (DropSlotPreview, error) {
	firstWaypointIndex := firstWaypoint(rows)
	if firstWaypointIndex < 0 {
		return DropSlotPreview{}, nil
	}
	if fromIndex == firstWaypointIndex {
		return DropSlotPreview{}, errors.New("waypoint #1 cannot be moved after the expedition has started")
	}
	arrivals, arrivalsError := loadExpeditionArrivals(queryer, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp)
	if arrivalsError != nil {
		return DropSlotPreview{}, arrivalsError
	}
	normalizedSystemNames := make([]string, len(rows))
	originalVisitedAt := make(map[int64]string, len(rows))
	for i := range rows {
		if rows[i].RowType == RowWaypoint {
			normalizedSystemNames[i] = strings.ToLower(strings.TrimSpace(rows[i].SystemName))
			if rows[i].VisitedAt != "" {
				originalVisitedAt[rows[i].ID] = rows[i].VisitedAt
			}
		}
	}
	originVisitUID, transitions := prepareNormalizedTransitions(
		&rows[firstWaypointIndex],
		normalizedSystemNames[firstWaypointIndex],
		expeditionRecord.StartTimestamp,
		arrivals,
	)
	candidateRows := make([]Row, len(rows))
	candidateSystems := make([]string, len(rows))
	preview := evaluateRowDropTarget(
		rows,
		normalizedSystemNames,
		fromIndex,
		toIndex,
		firstWaypointIndex,
		expeditionRecord.StartTimestamp,
		originVisitUID,
		transitions,
		originalVisitedAt,
		candidateRows,
		candidateSystems,
	)
	switch preview.Status {
	case DropSlotInvalid:
		return preview, errors.New("waypoint cannot be moved outside its chronological visit bounds")
	case DropSlotBlockedFC:
		return preview, errors.New("pending fleet carrier stop cannot be moved before an arrived carrier stop")
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
	now := ed.NowISO()
	if arrived {
		if _, execError := transaction.Exec(`UPDATE expedition_rows SET fc_arrived = 1, updated_at = ?
			WHERE expedition_id = ? AND row_type = 'waypoint' AND fc_stop = 1 AND position <= ?`,
			now, targetRow.ExpeditionID, targetRow.Position); execError != nil {
			return targetRow.ExpeditionID, execError
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
