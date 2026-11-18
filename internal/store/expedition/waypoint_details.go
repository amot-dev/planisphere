package expedition

import (
	"database/sql"
	"errors"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
)

// WaypointDetails is the content of a waypoint's expandable drawer.
type WaypointDetails struct {
	RowID        int64                            `json:"rowId"`
	Kind         string                           `json:"kind"` // "origin" (waypoint #1), "leg" (visited N>1 or active current leg)
	Number       int                              `json:"number"`
	From         string                           `json:"from"`
	To           string                           `json:"to"`
	FromGalactic string                           `json:"fromGalactic"`
	ToGalactic   string                           `json:"toGalactic"`
	Elapsed      string                           `json:"elapsed"`
	PrevSystem   string                           `json:"prevSystem"`
	Metrics      replaystats.Metrics              `json:"metrics"`
	Realization  replaystats.RealizationBreakdown `json:"realization"`
}

func populateWindowTelemetry(proj *Projections, timeRange replaystats.Range, waypointDetails *WaypointDetails) error {
	summary, computeError := proj.TimeRangeStats.Compute(timeRange, nil)
	if computeError != nil {
		return computeError
	}
	waypointDetails.Metrics = summary.Metrics
	waypointDetails.Realization = summary.Realization
	return nil
}

func populateOriginWaypointDetails(conn *sql.DB, targetRow Row, waypointDetails *WaypointDetails, proj *Projections) error {
	waypointDetails.Kind = "origin"
	waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, reference.ToGalactic(targetRow.VisitedAt)
	firstTimestamp := replay.FirstEventTimestamp(conn)
	waypointDetails.From, waypointDetails.FromGalactic = firstTimestamp, reference.ToGalactic(firstTimestamp)
	return populateWindowTelemetry(proj, replaystats.Range{After: "", Until: targetRow.VisitedAt}, waypointDetails)
}

func populateLegDetails(conn *sql.DB, targetRow Row, rows []Row, waypointDetails *WaypointDetails, proj *Projections) error {
	waypointDetails.Kind = "leg"
	// The leg starts at the visit of the waypoint visited immediately before this one.
	afterTimestamp := ""
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" && (targetRow.VisitedAt == "" || rowItem.VisitedAt < targetRow.VisitedAt) && rowItem.VisitedAt > afterTimestamp {
			afterTimestamp = rowItem.VisitedAt
			waypointDetails.PrevSystem = rowItem.SystemName
		}
	}
	if afterTimestamp == "" {
		var startTimestamp sql.NullString
		_ = conn.QueryRow(`SELECT COALESCE(start_timestamp, '') FROM expeditions WHERE id = ?`, targetRow.ExpeditionID).Scan(&startTimestamp)
		afterTimestamp = startTimestamp.String
	}
	waypointDetails.From, waypointDetails.FromGalactic = afterTimestamp, reference.ToGalactic(afterTimestamp)
	waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, reference.ToGalactic(targetRow.VisitedAt)
	return populateWindowTelemetry(proj, replaystats.Range{After: afterTimestamp, Until: targetRow.VisitedAt}, waypointDetails)
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

// Details computes the drawer content for a visited or active current waypoint row.
func Details(conn *sql.DB, rowID int64, proj *Projections) (WaypointDetails, error) {
	if proj == nil || proj.TimeRangeStats == nil {
		return WaypointDetails{}, errors.New("expedition TimeRangeStats projection is required")
	}
	targetRow, rowError := GetRow(conn, rowID)
	if rowError != nil {
		return WaypointDetails{}, rowError
	}
	if !targetRow.IsWaypoint() {
		return WaypointDetails{}, nil
	}

	rows, listError := ListRows(conn, targetRow.ExpeditionID)
	if listError != nil {
		return WaypointDetails{}, listError
	}

	var (
		waypointNumber     int
		selfNumber         int
		hasVisitedWaypoint bool
		nextWaypointID     int64
	)
	for i := range rows {
		rowItem := &rows[i]
		if !rowItem.IsWaypoint() {
			continue
		}
		waypointNumber++
		if rowItem.ID == rowID {
			selfNumber = waypointNumber
		}
		if rowItem.VisitedAt != "" {
			hasVisitedWaypoint = true
		} else if nextWaypointID == 0 {
			nextWaypointID = rowItem.ID
		}
	}

	if selfNumber == 0 {
		return WaypointDetails{}, nil
	}
	if targetRow.VisitedAt == "" {
		if !hasVisitedWaypoint || nextWaypointID != rowID {
			return WaypointDetails{}, nil
		}
		expeditionRecord, getError := Get(conn, targetRow.ExpeditionID)
		if getError != nil || expeditionRecord.Status != StatusActive {
			return WaypointDetails{}, getError
		}
	}

	waypointDetails := WaypointDetails{
		RowID:  rowID,
		Number: selfNumber,
	}

	if selfNumber == 1 {
		if originError := populateOriginWaypointDetails(conn, targetRow, &waypointDetails, proj); originError != nil {
			return waypointDetails, originError
		}
	} else {
		if legError := populateLegDetails(conn, targetRow, rows, &waypointDetails, proj); legError != nil {
			return waypointDetails, legError
		}
	}

	waypointDetails.Elapsed = formatLegElapsed(waypointDetails.From, waypointDetails.To)
	return waypointDetails, nil
}

