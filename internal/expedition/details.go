package expedition

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"planisphere/internal/ed"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

type originCacheEntry struct {
	metrics     stats.Metrics
	realization stats.RealizationBreakdown
}

var (
	originMetricsMu    sync.Mutex
	originMetricsCache = make(map[string]originCacheEntry)
	originMetricsGen   uint64
)

// InvalidateOriginMetricsCache clears the cached origin waypoint metrics and realization breakdown.
func InvalidateOriginMetricsCache() {
	originMetricsMu.Lock()
	originMetricsCache = make(map[string]originCacheEntry)
	originMetricsGen++
	originMetricsMu.Unlock()
}

// WaypointDetails is the content of a waypoint's expandable drawer.
type WaypointDetails struct {
	RowID        int64                      `json:"rowId"`
	Kind         string                     `json:"kind"` // "origin" (waypoint #1), "leg" (visited N>1), "future"
	Number       int                        `json:"number"`
	From         string                     `json:"from"`
	To           string                     `json:"to"`
	FromGalactic string                     `json:"fromGalactic"`
	ToGalactic   string                     `json:"toGalactic"`
	Elapsed      string                     `json:"elapsed"`
	PrevSystem   string                     `json:"prevSystem"`
	Metrics      stats.Metrics              `json:"metrics"`
	Realization  stats.RealizationBreakdown `json:"realization"`

	EstDistFromPrevLy    *float64 `json:"estDistFromPrevLy,omitempty"`
	EstDistFromCurrentLy *float64 `json:"estDistFromCurrentLy,omitempty"`
	CurrentSystem        string   `json:"currentSystem"`
	MissingCoords        bool     `json:"missingCoords"`
}

func populateFutureWaypointDetails(waypointDetails *WaypointDetails, targetRow Row, previousWaypoint, currentWaypoint *Row) {
	waypointDetails.Kind = "future"
	if previousWaypoint != nil && previousWaypoint.HasCoords && targetRow.HasCoords {
		startPosition, _ := previousWaypoint.Coords()
		endPosition, _ := targetRow.Coords()
		distanceLy := ed.Distance(startPosition, endPosition)
		waypointDetails.EstDistFromPrevLy = &distanceLy
	}
	if currentWaypoint != nil && currentWaypoint.HasCoords && targetRow.HasCoords {
		currentPosition, _ := currentWaypoint.Coords()
		endPosition, _ := targetRow.Coords()
		distanceLy := ed.Distance(currentPosition, endPosition)
		waypointDetails.EstDistFromCurrentLy = &distanceLy
	}
	if currentWaypoint != nil {
		waypointDetails.CurrentSystem = currentWaypoint.SystemName
	}
	waypointDetails.MissingCoords = !targetRow.HasCoords || (previousWaypoint != nil && !previousWaypoint.HasCoords)
}

func populateOriginWaypointDetails(conn *sql.DB, statsEngine *stats.Engine, targetRow Row, waypointDetails *WaypointDetails) error {
	waypointDetails.Kind = "origin"
	waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, ed.ToGalactic(targetRow.VisitedAt)
	var firstTimestamp sql.NullString
	_ = conn.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&firstTimestamp)
	waypointDetails.From, waypointDetails.FromGalactic = firstTimestamp.String, ed.ToGalactic(firstTimestamp.String)

	cacheKey := fmt.Sprintf("%d|%s|%s", targetRow.ExpeditionID, firstTimestamp.String, targetRow.VisitedAt)
	originMetricsMu.Lock()
	cachedOriginEntry, isOriginCached := originMetricsCache[cacheKey]
	originMetricsGeneration := originMetricsGen
	originMetricsMu.Unlock()

	if isOriginCached {
		waypointDetails.Metrics = cachedOriginEntry.metrics
		waypointDetails.Realization = cachedOriginEntry.realization
		return nil
	}

	originRange := stats.Range{After: "", Until: targetRow.VisitedAt}
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

	// Guard cache write-back with originMetricsGen so an invalidation that ran while
	// originMetricsMu was unlocked during computation cannot be overwritten by stale results.
	originMetricsMu.Lock()
	if originMetricsGen == originMetricsGeneration {
		originMetricsCache[cacheKey] = originCacheEntry{
			metrics:     originMetrics,
			realization: originRealization,
		}
	}
	originMetricsMu.Unlock()
	return nil
}

func populateVisitedLegDetails(conn *sql.DB, statsEngine *stats.Engine, targetRow Row, rows []Row, waypointDetails *WaypointDetails) error {
	waypointDetails.Kind = "leg"
	// The leg starts at the visit of the waypoint visited immediately before this one.
	afterTimestamp := ""
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" && rowItem.VisitedAt < targetRow.VisitedAt && rowItem.VisitedAt > afterTimestamp {
			afterTimestamp = rowItem.VisitedAt
			waypointDetails.PrevSystem = rowItem.SystemName
		}
	}
	if afterTimestamp == "" {
		var startTimestamp sql.NullString
		_ = conn.QueryRow(`SELECT COALESCE(start_timestamp, '') FROM expeditions WHERE id = ?`, targetRow.ExpeditionID).Scan(&startTimestamp)
		afterTimestamp = startTimestamp.String
	}
	waypointDetails.From, waypointDetails.FromGalactic = afterTimestamp, ed.ToGalactic(afterTimestamp)
	waypointDetails.To, waypointDetails.ToGalactic = targetRow.VisitedAt, ed.ToGalactic(targetRow.VisitedAt)

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

// Details computes the drawer content for a waypoint row.
func Details(conn *sql.DB, rowID int64) (WaypointDetails, error) {
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
		waypointNumber   int
		selfNumber       int
		previousWaypoint *Row
		currentWaypoint  *Row
	)

	for i := range rows {
		rowItem := &rows[i]
		if !rowItem.IsWaypoint() {
			continue
		}
		waypointNumber++
		if rowItem.ID == rowID {
			selfNumber = waypointNumber
		} else if selfNumber == 0 {
			previousCopy := *rowItem
			previousWaypoint = &previousCopy
		}
		if rowItem.VisitedAt != "" && (currentWaypoint == nil || rowItem.VisitedAt > currentWaypoint.VisitedAt) {
			currentWaypoint = rowItem
		}
	}

	if selfNumber == 0 {
		return WaypointDetails{}, nil
	}

	waypointDetails := WaypointDetails{
		RowID:  rowID,
		Number: selfNumber,
	}
	if previousWaypoint != nil {
		waypointDetails.PrevSystem = previousWaypoint.SystemName
	}

	if targetRow.VisitedAt == "" {
		populateFutureWaypointDetails(&waypointDetails, targetRow, previousWaypoint, currentWaypoint)
		return waypointDetails, nil
	}

	statsEngine := stats.NewEngine(conn)
	if selfNumber == 1 {
		if originError := populateOriginWaypointDetails(conn, statsEngine, targetRow, &waypointDetails); originError != nil {
			return waypointDetails, originError
		}
	} else {
		if legError := populateVisitedLegDetails(conn, statsEngine, targetRow, rows, &waypointDetails); legError != nil {
			return waypointDetails, legError
		}
	}

	if startTime, parseStartError := ed.ParseISO(waypointDetails.From); parseStartError == nil {
		if endTime, parseEndError := ed.ParseISO(waypointDetails.To); parseEndError == nil {
			waypointDetails.Elapsed = ed.DurationLabel(endTime.Sub(startTime))
		}
	}
	return waypointDetails, nil
}

// Elapsed returns a human label between two ISO timestamps (now when toTimestamp is empty).
// Uses rounded days and years without hours or minutes.
func Elapsed(fromTimestamp, toTimestamp string) string {
	startTime, parseError := ed.ParseISO(fromTimestamp)
	if parseError != nil {
		return ""
	}
	endTime := time.Now().UTC()
	if toTimestamp != "" {
		if parsedTime, parseError := ed.ParseISO(toTimestamp); parseError == nil {
			endTime = parsedTime
		}
	}
	return ed.DurationDaysLabel(endTime.Sub(startTime))
}

