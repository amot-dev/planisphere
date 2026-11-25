package expedition

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
)

const (
	maxWaypointSearchResults     = 50
	maxReturnedNonWaypointVisits = 200
)

var likeWildcardEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// WaypointStopMatch identifies one matching waypoint stop within a condensed
// cross-expedition search result card.
type WaypointStopMatch struct {
	RowID             int64  `json:"rowId"`
	Number            int    `json:"number"`
	POIName           string `json:"poiName"`
	VisitedAt         string `json:"visitedAt"`
	VisitedAtGalactic string `json:"visitedAtGalactic"`
}

// WaypointSearchResult groups matching waypoints sharing a system inside one
// expedition, ranked by relevance tier and expedition recency.
type WaypointSearchResult struct {
	ExpeditionID     int64               `json:"expeditionId"`
	ExpeditionName   string              `json:"expeditionName"`
	ExpeditionStatus string              `json:"expeditionStatus"`
	SystemName       string              `json:"systemName"`
	POIName          string              `json:"poiName"`
	RelevanceTier    int                 `json:"relevanceTier"`
	Stops            []WaypointStopMatch `json:"stops"`
}

// NonWaypointVisit describes a journal visit to an exact-matched system that is
// not linked to any recorded waypoint arrival, optionally attributed to an
// overlapping expedition.
type NonWaypointVisit struct {
	Timestamp        string `json:"timestamp"`
	Galactic         string `json:"galactic"`
	ExpeditionID     int64  `json:"expeditionId,omitempty"`
	ExpeditionName   string `json:"expeditionName,omitempty"`
	ExpeditionStatus string `json:"expeditionStatus,omitempty"`
}

// NonWaypointVisitSummary aggregates exact-system non-waypoint visits alongside
// total count and earliest sighting metadata.
type NonWaypointVisitSummary struct {
	SystemName       string             `json:"systemName"`
	TotalCount       int                `json:"totalCount"`
	EarliestGalactic string             `json:"earliestGalactic"`
	Visits           []NonWaypointVisit `json:"visits"`
}

// SearchResponse carries ranked cross-expedition waypoint search matches and
// optional exact-system non-waypoint journal visits.
type SearchResponse struct {
	Results           []WaypointSearchResult   `json:"results"`
	TotalMatches      int                      `json:"totalMatches"`
	NonWaypointVisits *NonWaypointVisitSummary `json:"nonWaypointVisits,omitempty"`
}

type waypointSearchRow struct {
	rowID, expeditionID                                                        int64
	position, waypointNumber                                                   int
	systemName, poiName, visitedAt                                             string
	expeditionName, expeditionStatus, startTimestamp, createdAt                string
}

type waypointGroupKey struct {
	expeditionID int64
	systemKey    string
	rowID        int64
}

type condensedWaypointGroup struct {
	result             WaypointSearchResult
	effectiveTimestamp string
	firstStopPosition  int
	firstStopRowID     int64
	distinctPOIs       []string
	seenPOIs           map[string]bool
}

type attributableExpedition struct {
	record       Expedition
	profileScope replay.ProfileScope
}

// SearchWaypoints searches waypoint system_name and poi_name values across all
// expeditions, condensing repeated stops at the same system within an
// expedition, and attaches exact-system non-waypoint journal visits when found.
func SearchWaypoints(queryer db.Queryer, rawQuery string, expeditionProjections *Projections) (SearchResponse, error) {
	trimmedQuery := strings.TrimSpace(rawQuery)
	if trimmedQuery == "" {
		return SearchResponse{Results: []WaypointSearchResult{}}, nil
	}
	matchedRows, queryError := queryMatchingWaypointRows(queryer, trimmedQuery)
	if queryError != nil {
		return SearchResponse{Results: []WaypointSearchResult{}}, queryError
	}
	groupedResults := condenseAndRankWaypointMatches(matchedRows, trimmedQuery)
	totalMatches := len(groupedResults)
	if len(groupedResults) > maxWaypointSearchResults {
		groupedResults = groupedResults[:maxWaypointSearchResults]
	}
	nonWaypointSummary, visitsError := loadNonWaypointVisitSummary(queryer, trimmedQuery, expeditionProjections)
	if visitsError != nil {
		return SearchResponse{Results: []WaypointSearchResult{}}, visitsError
	}
	return SearchResponse{Results: groupedResults, TotalMatches: totalMatches, NonWaypointVisits: nonWaypointSummary}, nil
}

func queryMatchingWaypointRows(queryer db.Queryer, trimmedQuery string) ([]waypointSearchRow, error) {
	likePattern := "%" + likeWildcardEscaper.Replace(trimmedQuery) + "%"
	rows, queryError := queryer.Query(`WITH numbered_waypoints AS (
		SELECT r.id, r.expedition_id, r.position, r.system_name, r.poi_name, COALESCE(r.visited_at, '') AS visited_at,
			e.name AS expedition_name, e.status AS expedition_status,
			COALESCE(e.start_timestamp, '') AS start_timestamp, COALESCE(e.created_at, '') AS created_at,
			ROW_NUMBER() OVER (PARTITION BY r.expedition_id ORDER BY r.position ASC, r.id ASC) AS wp_number
		FROM expedition_rows r
		JOIN expeditions e ON e.id = r.expedition_id
		WHERE r.row_type = 'waypoint'
	)
	SELECT id, expedition_id, position, wp_number, system_name, poi_name, visited_at,
		expedition_name, expedition_status, start_timestamp, created_at
	FROM numbered_waypoints
	WHERE system_name LIKE ? ESCAPE '\' COLLATE NOCASE
	   OR poi_name LIKE ? ESCAPE '\' COLLATE NOCASE
	ORDER BY expedition_id ASC, position ASC, id ASC`, likePattern, likePattern)
	if queryError != nil {
		return nil, fmt.Errorf("search expedition waypoints for %q: %w", trimmedQuery, queryError)
	}
	defer rows.Close()

	var matchedRows []waypointSearchRow
	for rows.Next() {
		var matchedRow waypointSearchRow
		if scanError := rows.Scan(
			&matchedRow.rowID, &matchedRow.expeditionID, &matchedRow.position, &matchedRow.waypointNumber,
			&matchedRow.systemName, &matchedRow.poiName, &matchedRow.visitedAt,
			&matchedRow.expeditionName, &matchedRow.expeditionStatus, &matchedRow.startTimestamp, &matchedRow.createdAt,
		); scanError != nil {
			return nil, fmt.Errorf("scan waypoint search row: %w", scanError)
		}
		if matchedRow.expeditionStatus == StatusActive && matchedRow.startTimestamp == "" {
			matchedRow.expeditionStatus = StatusPlanning
		}
		matchedRows = append(matchedRows, matchedRow)
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate waypoint search rows: %w", iterationError)
	}
	return matchedRows, nil
}

func scoreWaypointMatch(systemName, poiName, lowerQuery, normalizedQueryKey string) int {
	trimmedSystem := strings.TrimSpace(systemName)
	lowerSystem := strings.ToLower(trimmedSystem)
	if lowerSystem != "" && (lowerSystem == lowerQuery || (normalizedQueryKey != "" && replay.NormalizeSystemKey(trimmedSystem) == normalizedQueryKey)) {
		return 1
	}
	if lowerSystem != "" && strings.HasPrefix(lowerSystem, lowerQuery) {
		return 2
	}
	if lowerPOI := strings.ToLower(strings.TrimSpace(poiName)); lowerPOI != "" && strings.HasPrefix(lowerPOI, lowerQuery) {
		return 3
	}
	if lowerSystem != "" && strings.Contains(lowerSystem, lowerQuery) {
		return 4
	}
	return 5
}

func effectiveExpeditionTimestamp(status, startTimestamp, createdAt string) string {
	if (status == StatusActive || status == StatusCompleted) && startTimestamp != "" {
		return startTimestamp
	}
	return createdAt
}

func condenseAndRankWaypointMatches(matchedRows []waypointSearchRow, trimmedQuery string) []WaypointSearchResult {
	if len(matchedRows) == 0 {
		return []WaypointSearchResult{}
	}
	lowerQuery, normalizedQueryKey := strings.ToLower(trimmedQuery), replay.NormalizeSystemKey(trimmedQuery)
	groupIndexByKey := make(map[waypointGroupKey]int, len(matchedRows))
	groups := make([]condensedWaypointGroup, 0, len(matchedRows))

	for _, matchedRow := range matchedRows {
		rowTier := scoreWaypointMatch(matchedRow.systemName, matchedRow.poiName, lowerQuery, normalizedQueryKey)
		systemKey := replay.NormalizeSystemKey(matchedRow.systemName)
		groupKey := waypointGroupKey{expeditionID: matchedRow.expeditionID, systemKey: systemKey}
		if systemKey == "" {
			groupKey.rowID = matchedRow.rowID
		}
		groupIndex, exists := groupIndexByKey[groupKey]
		if !exists {
			groupIndex = len(groups)
			groupIndexByKey[groupKey] = groupIndex
			groups = append(groups, condensedWaypointGroup{
				result: WaypointSearchResult{
					ExpeditionID:     matchedRow.expeditionID,
					ExpeditionName:   matchedRow.expeditionName,
					ExpeditionStatus: matchedRow.expeditionStatus,
					SystemName:       matchedRow.systemName,
					RelevanceTier:    rowTier,
				},
				effectiveTimestamp: effectiveExpeditionTimestamp(matchedRow.expeditionStatus, matchedRow.startTimestamp, matchedRow.createdAt),
				firstStopPosition:  matchedRow.position,
				firstStopRowID:     matchedRow.rowID,
				seenPOIs:           make(map[string]bool),
			})
		}

		group := &groups[groupIndex]
		if rowTier < group.result.RelevanceTier {
			group.result.RelevanceTier = rowTier
		}
		group.result.Stops = append(group.result.Stops, WaypointStopMatch{
			RowID:             matchedRow.rowID,
			Number:            matchedRow.waypointNumber,
			POIName:           matchedRow.poiName,
			VisitedAt:         matchedRow.visitedAt,
			VisitedAtGalactic: reference.ToGalactic(matchedRow.visitedAt),
		})
		if trimmedPOI := strings.TrimSpace(matchedRow.poiName); trimmedPOI != "" {
			if poiKey := strings.ToLower(trimmedPOI); !group.seenPOIs[poiKey] {
				group.seenPOIs[poiKey] = true
				group.distinctPOIs = append(group.distinctPOIs, trimmedPOI)
			}
		}
	}

	for index := range groups {
		if len(groups[index].result.Stops) > 1 {
			slices.SortFunc(groups[index].result.Stops, func(left, right WaypointStopMatch) int {
				return cmp.Compare(left.Number, right.Number)
			})
		}
		groups[index].result.POIName = strings.Join(groups[index].distinctPOIs, " / ")
	}
	slices.SortFunc(groups, func(left, right condensedWaypointGroup) int {
		return cmp.Or(
			cmp.Compare(left.result.RelevanceTier, right.result.RelevanceTier),
			cmp.Compare(right.effectiveTimestamp, left.effectiveTimestamp),
			cmp.Compare(right.result.ExpeditionID, left.result.ExpeditionID),
			cmp.Compare(left.firstStopPosition, right.firstStopPosition),
			cmp.Compare(left.firstStopRowID, right.firstStopRowID),
		)
	})

	rankedResults := make([]WaypointSearchResult, len(groups))
	for index := range groups {
		rankedResults[index] = groups[index].result
	}
	return rankedResults
}

func loadNonWaypointVisitSummary(queryer db.Queryer, trimmedQuery string, expeditionProjections *Projections) (*NonWaypointVisitSummary, error) {
	var recoveredJumps []replay.RecoveredJump
	if expeditionProjections != nil && expeditionProjections.RecoveredJumps != nil {
		var recoveredError error
		if recoveredJumps, recoveredError = expeditionProjections.RecoveredJumps.All(); recoveredError != nil {
			return nil, fmt.Errorf("load recovered jumps for waypoint search: %w", recoveredError)
		}
	}
	systemVisits, visitsError := replay.QuerySystemVisits(queryer, trimmedQuery, recoveredJumps)
	if visitsError != nil || len(systemVisits) == 0 {
		return nil, visitsError
	}
	linkedUIDs, linkedLegacyTimestamps, linkedError := queryVisitedWaypointKeys(queryer, trimmedQuery)
	if linkedError != nil {
		return nil, linkedError
	}

	remainingVisits := make([]replay.SystemVisit, 0, len(systemVisits))
	for _, visit := range systemVisits {
		if (visit.UID != "" && linkedUIDs[visit.UID]) || linkedLegacyTimestamps[visit.Timestamp] {
			continue
		}
		remainingVisits = append(remainingVisits, visit)
	}
	if len(remainingVisits) == 0 {
		return nil, nil
	}

	allExpeditions, listError := List(queryer)
	if listError != nil {
		return nil, fmt.Errorf("list expeditions for non-waypoint visit attribution: %w", listError)
	}
	attributableExpeditions := buildAttributableExpeditions(allExpeditions)

	returnedCount := min(len(remainingVisits), maxReturnedNonWaypointVisits)
	attributedVisits := make([]NonWaypointVisit, returnedCount)
	for index := 0; index < returnedCount; index++ {
		visit := remainingVisits[index]
		attributedVisit := NonWaypointVisit{Timestamp: visit.Timestamp, Galactic: visit.Galactic}
		if matchedExpedition, found := matchExpeditionForVisit(attributableExpeditions, visit); found {
			attributedVisit.ExpeditionID = matchedExpedition.ID
			attributedVisit.ExpeditionName = matchedExpedition.Name
			attributedVisit.ExpeditionStatus = matchedExpedition.Status
		}
		attributedVisits[index] = attributedVisit
	}

	canonicalSystemName := remainingVisits[0].SystemName
	if canonicalSystemName == "" {
		canonicalSystemName = trimmedQuery
	}
	return &NonWaypointVisitSummary{
		SystemName:       canonicalSystemName,
		TotalCount:       len(remainingVisits),
		EarliestGalactic: remainingVisits[len(remainingVisits)-1].Galactic,
		Visits:           attributedVisits,
	}, nil
}

func queryVisitedWaypointKeys(queryer db.Queryer, exactSystemName string) (map[string]bool, map[string]bool, error) {
	rows, queryError := queryer.Query(`SELECT COALESCE(visit_event_uid, ''), COALESCE(visited_at, '')
		FROM expedition_rows
		WHERE row_type = 'waypoint' AND system_name = ? COLLATE NOCASE AND visited_at IS NOT NULL AND visited_at != ''`,
		exactSystemName)
	if queryError != nil {
		return nil, nil, fmt.Errorf("query visited waypoints for %q: %w", exactSystemName, queryError)
	}
	defer rows.Close()

	linkedUIDs, linkedLegacyTimestamps := make(map[string]bool), make(map[string]bool)
	for rows.Next() {
		var visitEventUID, visitedAt string
		if scanError := rows.Scan(&visitEventUID, &visitedAt); scanError != nil {
			return nil, nil, fmt.Errorf("scan visited waypoint key: %w", scanError)
		}
		if visitEventUID != "" {
			linkedUIDs[visitEventUID] = true
		} else if visitedAt != "" {
			linkedLegacyTimestamps[visitedAt] = true
		}
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, nil, fmt.Errorf("iterate visited waypoint keys: %w", iterationError)
	}
	return linkedUIDs, linkedLegacyTimestamps, nil
}

func buildAttributableExpeditions(expeditions []Expedition) []attributableExpedition {
	candidates := make([]attributableExpedition, 0, len(expeditions))
	for _, expeditionRecord := range expeditions {
		if (expeditionRecord.Status != StatusActive && expeditionRecord.Status != StatusCompleted) || expeditionRecord.StartTimestamp == "" {
			continue
		}
		candidates = append(candidates, attributableExpedition{record: expeditionRecord, profileScope: expeditionRecord.ProfileScope()})
	}
	slices.SortFunc(candidates, func(left, right attributableExpedition) int {
		return cmp.Or(cmp.Compare(right.record.StartTimestamp, left.record.StartTimestamp), cmp.Compare(right.record.ID, left.record.ID))
	})
	return candidates
}

func matchExpeditionForVisit(candidates []attributableExpedition, visit replay.SystemVisit) (Expedition, bool) {
	for _, candidate := range candidates {
		if visit.Timestamp < candidate.record.StartTimestamp {
			continue
		}
		if candidate.record.EndTimestamp != "" && visit.Timestamp > candidate.record.EndTimestamp {
			continue
		}
		if candidate.record.CommanderScope == ScopeSelected && !candidate.profileScope.ContainsProfileID(visit.ProfileID) {
			continue
		}
		return candidate.record, true
	}
	return Expedition{}, false
}
