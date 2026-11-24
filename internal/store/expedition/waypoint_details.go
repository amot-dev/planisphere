package expedition

import (
	"database/sql"
	"errors"
	"sort"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
)

// WaypointDetails is the content of a waypoint's expandable drawer.
type WaypointDetails struct {
	RowID              int64                            `json:"rowId"`
	Kind               string                           `json:"kind"` // "origin" (waypoint #1), "leg" (visited N>1 or active current leg)
	Number             int                              `json:"number"`
	From               string                           `json:"from"`
	To                 string                           `json:"to"`
	FromGalactic       string                           `json:"fromGalactic"`
	ToGalactic         string                           `json:"toGalactic"`
	ArrivedAt   string                           `json:"arrivedAt,omitempty"`
	DepartedAt  string                           `json:"departedAt,omitempty"`
	InSystem    bool                             `json:"inSystem,omitempty"`
	Elapsed            string                           `json:"elapsed"`
	PrevSystem         string                           `json:"prevSystem"`
	ShipsUsed          []string                         `json:"shipsUsed"`
	Metrics            replaystats.Metrics              `json:"metrics"`
	Realization        replaystats.RealizationBreakdown `json:"realization"`
}

func populateWindowTelemetry(expeditionProjections *Projections, timeRange replaystats.Range, waypointDetails *WaypointDetails) error {
	summary, computeError := expeditionProjections.TimeRangeStats.Compute(timeRange, nil)
	if computeError != nil {
		return computeError
	}
	waypointDetails.Metrics = summary.Metrics
	waypointDetails.Realization = summary.Realization
	return nil
}

func populateOriginWaypointDetails(dbConn *sql.DB, targetRow Row, waypointDetails *WaypointDetails, expeditionProjections *Projections) error {
	waypointDetails.Kind = "origin"
	waypointDetails.ArrivedAt = targetRow.VisitedAt
	firstTimestamp := replay.FirstEventTimestamp(dbConn)
	waypointDetails.From, waypointDetails.FromGalactic = firstTimestamp, reference.ToGalactic(firstTimestamp)
	untilQuery := ""
	if targetRow.DepartedAt != "" {
		untilQuery = exclusiveBoundaryBefore(targetRow.DepartedAt, targetRow.VisitedAt)
		waypointDetails.To = targetRow.DepartedAt
		waypointDetails.ToGalactic = reference.ToGalactic(targetRow.DepartedAt)
		waypointDetails.DepartedAt = targetRow.DepartedAt
	} else {
		waypointDetails.InSystem = true
	}
	return populateWindowTelemetry(expeditionProjections, replaystats.Range{After: "", Until: untilQuery}, waypointDetails)
}

func distinctTimelineHulls(shipTimeline []replay.ShipSwitch) ([]string, bool) {
	distinctHulls := make([]string, 0, len(shipTimeline))
	seenHulls := make(map[string]bool, len(shipTimeline))
	for _, shipSwitch := range shipTimeline {
		if shipSwitch.HullName != "" && !seenHulls[shipSwitch.HullName] {
			seenHulls[shipSwitch.HullName] = true
			distinctHulls = append(distinctHulls, shipSwitch.HullName)
		}
	}
	return distinctHulls, len(distinctHulls) == 1 && shipTimeline[0].HullName == distinctHulls[0]
}

func extractTransitionHullNames(shipTimeline []replay.ShipSwitch, jumpTimestamps []string, afterTimestamp, untilTimestamp string) []string {
	transitions := replay.ExtractShipsUsedOnLeg(shipTimeline, jumpTimestamps, afterTimestamp, untilTimestamp)
	shipsUsed := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		if transition.To != "" {
			shipsUsed = append(shipsUsed, transition.To)
		}
	}
	return shipsUsed
}

func resolveLegShipsUsed(
	dbConn *sql.DB,
	expeditionProjections *Projections,
	afterTimestamp, untilTimestamp string,
	pilotedJumpCount int,
) ([]string, error) {
	if pilotedJumpCount <= 0 {
		return []string{}, nil
	}
	shipTimeline, timelineError := expeditionProjections.ShipHistory.Timeline(afterTimestamp, untilTimestamp)
	if timelineError != nil {
		return nil, timelineError
	}
	distinctHulls, singleLeadingHull := distinctTimelineHulls(shipTimeline)
	if len(distinctHulls) == 0 {
		return []string{}, nil
	}
	// When a single hull was already active at leg start and piloted jumps occurred,
	// every piloted jump on the leg belongs to that hull without querying jump timestamps.
	if singleLeadingHull && shipTimeline[0].Timestamp <= afterTimestamp {
		return distinctHulls, nil
	}
	recoveredJumps, recoverError := expeditionProjections.RecoveredJumps.InRange(afterTimestamp, untilTimestamp)
	if recoverError != nil {
		return nil, recoverError
	}
	jumpTimestamps, jumpError := replay.QueryJumpTimestamps(dbConn, afterTimestamp, untilTimestamp, recoveredJumps)
	if jumpError != nil {
		return nil, jumpError
	}
	return extractTransitionHullNames(shipTimeline, jumpTimestamps, afterTimestamp, untilTimestamp), nil
}

func sliceShipTimelineForLeg(fullTimeline []replay.ShipSwitch, afterTimestamp, untilTimestamp string) []replay.ShipSwitch {
	if len(fullTimeline) == 0 {
		return nil
	}
	startIndex := 0
	firstStrictlyAfter := 0
	if afterTimestamp != "" {
		firstStrictlyAfter = sort.Search(len(fullTimeline), func(index int) bool {
			return fullTimeline[index].Timestamp > afterTimestamp
		})
		if firstStrictlyAfter > 0 {
			startIndex = firstStrictlyAfter - 1
		}
	}
	endIndex := len(fullTimeline)
	if untilTimestamp != "" {
		endIndex = sort.Search(len(fullTimeline), func(index int) bool {
			return fullTimeline[index].Timestamp > untilTimestamp
		})
	}
	if afterTimestamp != "" && firstStrictlyAfter > 0 && endIndex < firstStrictlyAfter {
		endIndex = firstStrictlyAfter
	}
	if startIndex >= endIndex {
		return nil
	}
	return fullTimeline[startIndex:endIndex]
}

func findPrecedingVisitedWaypoint(rows []Row, targetRow Row) *Row {
	var precedingRow *Row
	for index := range rows {
		candidate := &rows[index]
		if !candidate.IsWaypoint() || candidate.VisitedAt == "" {
			continue
		}
		if targetRow.VisitedAt != "" && candidate.VisitedAt >= targetRow.VisitedAt {
			continue
		}
		if precedingRow == nil || candidate.VisitedAt > precedingRow.VisitedAt {
			precedingRow = candidate
		}
	}
	return precedingRow
}

func indexPrecedingVisitedWaypoints(rows []Row) map[int64]*Row {
	visitedPointers := make([]*Row, 0, len(rows))
	for index := range rows {
		if rows[index].IsWaypoint() && rows[index].VisitedAt != "" {
			visitedPointers = append(visitedPointers, &rows[index])
		}
	}
	sort.SliceStable(visitedPointers, func(leftIndex, rightIndex int) bool {
		return visitedPointers[leftIndex].VisitedAt < visitedPointers[rightIndex].VisitedAt
	})
	precedingByRowID := make(map[int64]*Row, len(visitedPointers))
	var previousGroupFirst, currentGroupFirst *Row
	for _, visitedRow := range visitedPointers {
		if currentGroupFirst == nil || visitedRow.VisitedAt != currentGroupFirst.VisitedAt {
			previousGroupFirst = currentGroupFirst
			currentGroupFirst = visitedRow
		}
		if previousGroupFirst != nil {
			precedingByRowID[visitedRow.ID] = previousGroupFirst
		}
	}
	return precedingByRowID
}

func resolveLegUpperBound(expeditionRecord Expedition, targetRow Row, waypointDetails *WaypointDetails) string {
	if targetRow.VisitedAt == "" {
		return ""
	}
	waypointDetails.ArrivedAt = targetRow.VisitedAt
	if targetRow.DepartedAt != "" {
		waypointDetails.To = targetRow.DepartedAt
		waypointDetails.ToGalactic = reference.ToGalactic(targetRow.DepartedAt)
		waypointDetails.DepartedAt = targetRow.DepartedAt
		return exclusiveBoundaryBefore(targetRow.DepartedAt, targetRow.VisitedAt)
	}
	if expeditionRecord.Status == StatusCompleted && expeditionRecord.EndTimestamp != "" {
		waypointDetails.To = expeditionRecord.EndTimestamp
		waypointDetails.ToGalactic = reference.ToGalactic(expeditionRecord.EndTimestamp)
		return expeditionRecord.EndTimestamp
	}
	waypointDetails.InSystem = true
	return ""
}

func populateLegWindowBounds(expeditionRecord Expedition, targetRow Row, precedingRow *Row, waypointDetails *WaypointDetails) replaystats.Range {
	waypointDetails.Kind = "leg"
	afterQuery := expeditionRecord.StartTimestamp
	displayFrom := expeditionRecord.StartTimestamp
	if precedingRow != nil {
		waypointDetails.PrevSystem = precedingRow.SystemName
		if precedingRow.DepartedAt != "" {
			afterQuery = exclusiveBoundaryBefore(precedingRow.DepartedAt, precedingRow.VisitedAt)
			displayFrom = precedingRow.DepartedAt
		} else {
			afterQuery = precedingRow.VisitedAt
			displayFrom = precedingRow.VisitedAt
		}
	}
	waypointDetails.From = displayFrom
	waypointDetails.FromGalactic = reference.ToGalactic(displayFrom)
	untilQuery := resolveLegUpperBound(expeditionRecord, targetRow, waypointDetails)
	return replaystats.Range{After: afterQuery, Until: untilQuery}
}

func populateLegDetails(dbConn *sql.DB, expeditionRecord Expedition, targetRow Row, rows []Row, waypointDetails *WaypointDetails, expeditionProjections *Projections) error {
	precedingRow := findPrecedingVisitedWaypoint(rows, targetRow)
	legRange := populateLegWindowBounds(expeditionRecord, targetRow, precedingRow, waypointDetails)
	if telemetryError := populateWindowTelemetry(expeditionProjections, legRange, waypointDetails); telemetryError != nil {
		return telemetryError
	}
	shipsUsed, shipsError := resolveLegShipsUsed(dbConn, expeditionProjections, legRange.After, legRange.Until, waypointDetails.Metrics.Jumps)
	if shipsError != nil {
		return shipsError
	}
	waypointDetails.ShipsUsed = shipsUsed
	return nil
}

func formatLegElapsed(fromTimestamp, toTimestamp string) string {
	startTime, parseStartError := reference.ParseISO(fromTimestamp)
	if parseStartError != nil {
		return ""
	}
	if toTimestamp == "" {
		return reference.DurationLabel(time.Now().UTC().Sub(startTime))
	}
	endTime, parseEndError := reference.ParseISO(toTimestamp)
	if parseEndError != nil {
		return ""
	}
	return reference.DurationLabel(endTime.Sub(startTime))
}

func validateDetailsProjections(expeditionProjections *Projections) error {
	if expeditionProjections == nil || expeditionProjections.TimeRangeStats == nil || expeditionProjections.ShipHistory == nil || expeditionProjections.RecoveredJumps == nil {
		return errors.New("expedition TimeRangeStats, ShipHistory, and RecoveredJumps projections are required")
	}
	return nil
}

func locateWaypointNumberAndNext(rows []Row, rowID int64) (int, int64) {
	waypointNumber := 0
	selfNumber := 0
	var nextWaypointID int64
	for index := range rows {
		if !rows[index].IsWaypoint() {
			continue
		}
		waypointNumber++
		if rows[index].ID == rowID {
			selfNumber = waypointNumber
		}
		if rows[index].VisitedAt == "" && nextWaypointID == 0 {
			nextWaypointID = rows[index].ID
		}
	}
	return selfNumber, nextWaypointID
}

func isWaypointDetailsEligible(expeditionRecord Expedition, targetRow Row, latestVisitedRow *Row, nextWaypointID int64) bool {
	if targetRow.VisitedAt != "" {
		return true
	}
	return latestVisitedRow != nil &&
		latestVisitedRow.DepartedAt != "" &&
		nextWaypointID == targetRow.ID &&
		expeditionRecord.Status == StatusActive
}

// Details computes the drawer content for a visited or active current waypoint row.
func Details(dbConn *sql.DB, rowID int64, expeditionProjections *Projections) (WaypointDetails, error) {
	if validationError := validateDetailsProjections(expeditionProjections); validationError != nil {
		return WaypointDetails{}, validationError
	}
	targetRow, rowError := GetRow(dbConn, rowID)
	if rowError != nil {
		return WaypointDetails{}, rowError
	}
	if !targetRow.IsWaypoint() {
		return WaypointDetails{}, nil
	}
	expeditionRecord, expError := Get(dbConn, targetRow.ExpeditionID)
	if expError != nil {
		return WaypointDetails{}, expError
	}
	rows, listError := ListRows(dbConn, targetRow.ExpeditionID)
	if listError != nil {
		return WaypointDetails{}, listError
	}
	selfNumber, nextWaypointID := locateWaypointNumberAndNext(rows, rowID)
	latestVisitedRow, _ := latestVisitedWaypoint(rows)
	if selfNumber == 0 || !isWaypointDetailsEligible(expeditionRecord, targetRow, latestVisitedRow, nextWaypointID) {
		return WaypointDetails{}, nil
	}

	waypointDetails := WaypointDetails{RowID: rowID, Number: selfNumber, ShipsUsed: []string{}}
	if selfNumber == 1 {
		if originError := populateOriginWaypointDetails(dbConn, targetRow, &waypointDetails, expeditionProjections); originError != nil {
			return waypointDetails, originError
		}
	} else if legError := populateLegDetails(dbConn, expeditionRecord, targetRow, rows, &waypointDetails, expeditionProjections); legError != nil {
		return waypointDetails, legError
	}
	waypointDetails.Elapsed = formatLegElapsed(waypointDetails.From, waypointDetails.To)
	return waypointDetails, nil
}

func extractPlanWaypointState(planRows []RowView) ([]Row, *Row, int64) {
	rows := make([]Row, len(planRows))
	var nextWaypointID int64
	for index := range planRows {
		rows[index] = planRows[index].Row
		if rows[index].IsWaypoint() && rows[index].VisitedAt == "" && nextWaypointID == 0 {
			nextWaypointID = rows[index].ID
		}
	}
	latestVisitedRow, _ := latestVisitedWaypoint(rows)
	return rows, latestVisitedRow, nextWaypointID
}

func computeJumpedLegSpan(rowDetails []WaypointDetails, legRanges []replaystats.Range) (string, string, bool) {
	hasJumpedLeg := false
	hasOpenEndedLeg := false
	earliestLegAfter := ""
	latestLegUntil := ""
	for index := range rowDetails {
		if rowDetails[index].Metrics.Jumps <= 0 {
			continue
		}
		legRange := legRanges[index]
		if !hasJumpedLeg || legRange.After < earliestLegAfter {
			earliestLegAfter = legRange.After
		}
		if legRange.Until == "" {
			hasOpenEndedLeg = true
		} else if !hasOpenEndedLeg && (!hasJumpedLeg || legRange.Until > latestLegUntil) {
			latestLegUntil = legRange.Until
		}
		hasJumpedLeg = true
	}
	if hasOpenEndedLeg {
		latestLegUntil = ""
	}
	return earliestLegAfter, latestLegUntil, hasJumpedLeg
}

func partitionBatchLegTimelines(fullTimeline []replay.ShipSwitch, rowDetails []WaypointDetails, legRanges []replaystats.Range) ([][]replay.ShipSwitch, []bool, bool) {
	legTimelines := make([][]replay.ShipSwitch, len(rowDetails))
	needsJumpTimestamps := make([]bool, len(rowDetails))
	anyNeedsJumpTimestamps := false
	for index := range rowDetails {
		if rowDetails[index].Metrics.Jumps <= 0 {
			continue
		}
		legRange := legRanges[index]
		legTimeline := sliceShipTimelineForLeg(fullTimeline, legRange.After, legRange.Until)
		distinctHulls, singleLeadingHull := distinctTimelineHulls(legTimeline)
		if len(distinctHulls) == 0 {
			continue
		}
		if singleLeadingHull && legTimeline[0].Timestamp <= legRange.After {
			rowDetails[index].ShipsUsed = distinctHulls
			continue
		}
		legTimelines[index] = legTimeline
		needsJumpTimestamps[index] = true
		anyNeedsJumpTimestamps = true
	}
	return legTimelines, needsJumpTimestamps, anyNeedsJumpTimestamps
}

func populateBatchLegShipsUsed(dbConn *sql.DB, expeditionProjections *Projections, rowDetails []WaypointDetails, legRanges []replaystats.Range) error {
	earliestLegAfter, latestLegUntil, hasJumpedLeg := computeJumpedLegSpan(rowDetails, legRanges)
	if !hasJumpedLeg {
		return nil
	}
	fullTimeline, timelineError := expeditionProjections.ShipHistory.Timeline(earliestLegAfter, latestLegUntil)
	if timelineError != nil {
		return timelineError
	}
	legTimelines, needsJumpTimestamps, anyNeedsJumpTimestamps := partitionBatchLegTimelines(fullTimeline, rowDetails, legRanges)
	if !anyNeedsJumpTimestamps {
		return nil
	}
	recoveredJumps, recoverError := expeditionProjections.RecoveredJumps.InRange(earliestLegAfter, latestLegUntil)
	if recoverError != nil {
		return recoverError
	}
	sharedJumpTimestamps, jumpError := replay.QueryJumpTimestamps(dbConn, earliestLegAfter, latestLegUntil, recoveredJumps)
	if jumpError != nil {
		return jumpError
	}
	for index := range rowDetails {
		if needsJumpTimestamps[index] {
			rowDetails[index].ShipsUsed = extractTransitionHullNames(legTimelines[index], sharedJumpTimestamps, legRanges[index].After, legRanges[index].Until)
		}
	}
	return nil
}

func buildOriginBaselineDetails(dbConn *sql.DB, targetRow Row, expeditionProjections *Projections) (*WaypointDetails, error) {
	originDetails := WaypointDetails{RowID: targetRow.ID, Number: 1, ShipsUsed: []string{}}
	if originError := populateOriginWaypointDetails(dbConn, targetRow, &originDetails, expeditionProjections); originError != nil {
		return nil, originError
	}
	originDetails.Elapsed = formatLegElapsed(originDetails.From, originDetails.To)
	return &originDetails, nil
}

// DetailsBatch computes the origin baseline (when Waypoint #1 is visited) and
// ordered leg details (visited non-origin waypoints plus any eligible active
// current leg) directly from planView without re-querying expedition or row
// tables, batching ship timeline and jump timestamp lookups across all legs.
func DetailsBatch(dbConn *sql.DB, planView PlanView, expeditionProjections *Projections) (*WaypointDetails, []WaypointDetails, error) {
	if validationError := validateDetailsProjections(expeditionProjections); validationError != nil {
		return nil, nil, validationError
	}
	rows, latestVisitedRow, nextWaypointID := extractPlanWaypointState(planView.Rows)
	precedingByRowID := indexPrecedingVisitedWaypoints(rows)
	legCapacity := max(len(rows)-1, 0)
	rowDetails := make([]WaypointDetails, 0, legCapacity)
	legRanges := make([]replaystats.Range, 0, legCapacity)
	var originBaseline *WaypointDetails

	waypointNumber := 0
	for index := range rows {
		targetRow := rows[index]
		if !targetRow.IsWaypoint() {
			continue
		}
		waypointNumber++
		if !isWaypointDetailsEligible(planView.Expedition, targetRow, latestVisitedRow, nextWaypointID) {
			continue
		}
		if waypointNumber == 1 {
			var originError error
			if originBaseline, originError = buildOriginBaselineDetails(dbConn, targetRow, expeditionProjections); originError != nil {
				return nil, nil, originError
			}
			continue
		}
		precedingRow := latestVisitedRow
		if targetRow.VisitedAt != "" {
			precedingRow = precedingByRowID[targetRow.ID]
		}
		waypointDetails := WaypointDetails{RowID: targetRow.ID, Number: waypointNumber, ShipsUsed: []string{}}
		legRange := populateLegWindowBounds(planView.Expedition, targetRow, precedingRow, &waypointDetails)
		if telemetryError := populateWindowTelemetry(expeditionProjections, legRange, &waypointDetails); telemetryError != nil {
			return nil, nil, telemetryError
		}
		rowDetails = append(rowDetails, waypointDetails)
		legRanges = append(legRanges, legRange)
	}
	if shipsError := populateBatchLegShipsUsed(dbConn, expeditionProjections, rowDetails, legRanges); shipsError != nil {
		return nil, nil, shipsError
	}
	for index := range rowDetails {
		rowDetails[index].Elapsed = formatLegElapsed(rowDetails[index].From, rowDetails[index].To)
	}
	return originBaseline, rowDetails, nil
}
