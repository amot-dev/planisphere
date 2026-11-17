package expedition

import (
	"database/sql"
	"time"

	"planisphere/internal/projections/stats"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// WaypointDetails is the content of a waypoint's expandable drawer.
type WaypointDetails struct {
	RowID        int64                      `json:"rowId"`
	Kind         string                     `json:"kind"` // "origin" (waypoint #1), "leg" (visited N>1 or active current leg)
	Number       int                        `json:"number"`
	From         string                     `json:"from"`
	To           string                     `json:"to"`
	FromGalactic string                     `json:"fromGalactic"`
	ToGalactic   string                     `json:"toGalactic"`
	Elapsed      string                     `json:"elapsed"`
	PrevSystem   string                     `json:"prevSystem"`
	Metrics      stats.Metrics              `json:"metrics"`
	Realization  stats.RealizationBreakdown `json:"realization"`
}

func newStatsEngine(conn *sql.DB, proj *Projections) *stats.Engine {
	if proj != nil {
		return stats.NewEngineWithProjections(conn, proj.SalesLedger, proj.RecoveredJumps)
	}
	return stats.NewEngine(conn)
}

func populateOriginWaypointDetails(conn *sql.DB, targetRow Row, waypointDetails *WaypointDetails, proj *Projections) error {
	waypointDetails.Kind = "origin"
	waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, reference.ToGalactic(targetRow.VisitedAt)
	var firstTimestamp sql.NullString
	_ = conn.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&firstTimestamp)
	waypointDetails.From, waypointDetails.FromGalactic = firstTimestamp.String, reference.ToGalactic(firstTimestamp.String)

	originRange := stats.Range{After: "", Until: targetRow.VisitedAt}
	if proj != nil && proj.TimeRangeStats != nil {
		originSummary, computeError := proj.TimeRangeStats.Compute(originRange, nil)
		if computeError != nil {
			return computeError
		}
		waypointDetails.Metrics = originSummary.Metrics
		waypointDetails.Realization = originSummary.Realization
		return nil
	}

	statsEngine := newStatsEngine(conn, proj)
	originMetrics, computeError := statsEngine.Compute(originRange, nil)
	if computeError != nil {
		return computeError
	}
	originRealization, realizationError := statsEngine.ComputeRealization(originRange, nil)
	if realizationError != nil {
		return realizationError
	}
	waypointDetails.Metrics = originMetrics
	waypointDetails.Realization = originRealization
	return nil
}

func populateLegDetails(conn *sql.DB, statsEngine *stats.Engine, targetRow Row, rows []Row, waypointDetails *WaypointDetails) error {
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

	legRange := stats.Range{After: afterTimestamp, Until: targetRow.VisitedAt}
	var computeError error
	waypointDetails.Metrics, computeError = statsEngine.Compute(legRange, nil)
	if computeError != nil {
		return computeError
	}
	var realizationError error
	waypointDetails.Realization, realizationError = statsEngine.ComputeRealization(legRange, nil)
	return realizationError
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
		statsEngine := newStatsEngine(conn, proj)
		if legError := populateLegDetails(conn, statsEngine, targetRow, rows, &waypointDetails); legError != nil {
			return waypointDetails, legError
		}
	}

	waypointDetails.Elapsed = formatLegElapsed(waypointDetails.From, waypointDetails.To)
	return waypointDetails, nil
}

// Elapsed returns a human label between two ISO timestamps (now when toTimestamp is empty).
// Uses rounded days and years without hours or minutes.
func Elapsed(fromTimestamp, toTimestamp string) string {
	startTime, parseError := reference.ParseISO(fromTimestamp)
	if parseError != nil {
		return ""
	}
	endTime := time.Now().UTC()
	if toTimestamp != "" {
		if parsedTime, parseError := reference.ParseISO(toTimestamp); parseError == nil {
			endTime = parsedTime
		}
	}
	return reference.DurationDaysLabel(endTime.Sub(startTime))
}

