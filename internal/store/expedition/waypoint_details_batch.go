package expedition

import (
	"database/sql"

	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
)

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

func populateBatchLegShipsUsed(
	dbConn *sql.DB,
	expeditionProjections *Projections,
	rowDetails []WaypointDetails,
	legRanges []replaystats.Range,
	scope replay.ProfileScope,
) error {
	earliestLegAfter, latestLegUntil, hasJumpedLeg := computeJumpedLegSpan(rowDetails, legRanges)
	if !hasJumpedLeg {
		return nil
	}
	fullTimeline, timelineError := expeditionProjections.ShipHistory.TimelineScoped(earliestLegAfter, latestLegUntil, scope)
	if timelineError != nil {
		return timelineError
	}
	legTimelines, needsJumpTimestamps, anyNeedsJumpTimestamps := partitionBatchLegTimelines(fullTimeline, rowDetails, legRanges)
	if !anyNeedsJumpTimestamps {
		return nil
	}
	recoveredJumps, recoverError := expeditionProjections.RecoveredJumps.InRangeScoped(earliestLegAfter, latestLegUntil, scope)
	if recoverError != nil {
		return recoverError
	}
	sharedJumpTimestamps, jumpError := replay.QueryJumpTimestampsScoped(dbConn, earliestLegAfter, latestLegUntil, recoveredJumps, scope)
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

func buildOriginBaselineDetails(
	dbConn *sql.DB,
	expeditionRecord Expedition,
	targetRow Row,
	expeditionProjections *Projections,
) (*WaypointDetails, error) {
	originDetails := WaypointDetails{RowID: targetRow.ID, Number: 1, ShipsUsed: []string{}}
	if originError := populateOriginWaypointDetails(dbConn, expeditionRecord, targetRow, &originDetails, expeditionProjections); originError != nil {
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
	expScope := planView.Expedition.ProfileScope()
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
			if originBaseline, originError = buildOriginBaselineDetails(dbConn, planView.Expedition, targetRow, expeditionProjections); originError != nil {
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
		if telemetryError := populateWindowTelemetry(expeditionProjections, legRange, expScope, &waypointDetails); telemetryError != nil {
			return nil, nil, telemetryError
		}
		rowDetails = append(rowDetails, waypointDetails)
		legRanges = append(legRanges, legRange)
	}
	if shipsError := populateBatchLegShipsUsed(dbConn, expeditionProjections, rowDetails, legRanges, expScope); shipsError != nil {
		return nil, nil, shipsError
	}
	if playError := populateBatchLegPlayDurations(expeditionProjections, rowDetails, expScope); playError != nil {
		return nil, nil, playError
	}
	for index := range rowDetails {
		rowDetails[index].Elapsed = formatLegElapsed(rowDetails[index].From, rowDetails[index].To)
		applyLegTransitElapsed(&rowDetails[index])
	}
	return originBaseline, rowDetails, nil
}

func computeBatchPlaySpan(rowDetails []WaypointDetails) (string, string) {
	earliestFrom := ""
	latestTo := ""
	hasOpenEnd := false
	hasUnboundedStart := false
	for index := range rowDetails {
		legFrom := rowDetails[index].From
		if legFrom == "" {
			hasUnboundedStart = true
		} else if !hasUnboundedStart && (earliestFrom == "" || legFrom < earliestFrom) {
			earliestFrom = legFrom
		}
		legTo := rowDetails[index].To
		if legTo == "" {
			hasOpenEnd = true
		} else if !hasOpenEnd && (latestTo == "" || legTo > latestTo) {
			latestTo = legTo
		}
	}
	if hasUnboundedStart {
		earliestFrom = ""
	}
	if hasOpenEnd {
		latestTo = ""
	}
	return earliestFrom, latestTo
}

func populateBatchLegPlayDurations(
	expeditionProjections *Projections,
	rowDetails []WaypointDetails,
	scope replay.ProfileScope,
) error {
	if expeditionProjections.PlaySessions == nil || len(rowDetails) == 0 {
		return nil
	}
	earliestFrom, latestTo := computeBatchPlaySpan(rowDetails)
	sharedSessions, sessionError := expeditionProjections.PlaySessions.SessionsScoped(earliestFrom, latestTo, scope)
	if sessionError != nil {
		return sessionError
	}
	for index := range rowDetails {
		applyLegPlayDurations(&rowDetails[index], sharedSessions)
	}
	return nil
}
