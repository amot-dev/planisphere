package profile

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"

	"planisphere/internal/replay"
	"planisphere/internal/store/expedition"
)

const (
	metricEventNamesSQL = `('LoadGame','Location','FSDJump','CarrierJump','Scan','SAAScanComplete','ScanOrganic')`
	realEventNamesSQL   = `('LoadGame','Location','FSDJump','CarrierJump','Docked','Undocked','Scan','SAAScanComplete','ScanOrganic','SellExplorationData','MultiSellExplorationData','SellOrganicData','Resurrect','NpcCrewPaidWage')`
)

func inspectDataset(database *sql.DB, sourcePath, snapshotPath string) (DatasetSummary, error) {
	summary := DatasetSummary{
		SourceDBPath:   sourcePath,
		SnapshotDBPath: snapshotPath,
	}
	if fileInfo, statError := os.Stat(sourcePath); statError == nil {
		summary.DatabaseSizeBytes = fileInfo.Size()
	}

	if scanError := scanArchiveCounts(database, &summary); scanError != nil {
		return DatasetSummary{}, scanError
	}
	if expeditionError := selectPrimaryExpedition(database, &summary); expeditionError != nil {
		return DatasetSummary{}, expeditionError
	}
	windows, windowError := buildRepresentativeWindows(database, summary)
	if windowError != nil {
		return DatasetSummary{}, windowError
	}
	summary.Windows = windows
	return summary, nil
}

func scanArchiveCounts(database *sql.DB, summary *DatasetSummary) error {
	if queryError := database.QueryRow(`SELECT COUNT(*) FROM ingested_journals`).Scan(&summary.FilesTracked); queryError != nil {
		return fmt.Errorf("count ingested journals: %w", queryError)
	}
	if queryError := database.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(LENGTH(raw_json)), 0) FROM journal_events WHERE ` + replay.CmdrFilterSQL,
	).Scan(&summary.TotalEvents, &summary.RawJSONBytesTotal); queryError != nil {
		return fmt.Errorf("count active commander events: %w", queryError)
	}
	if queryError := database.QueryRow(
		`SELECT COUNT(*) FROM journal_events WHERE event IN ` + metricEventNamesSQL + ` AND ` + replay.CmdrFilterSQL,
	).Scan(&summary.MetricEvents); queryError != nil {
		return fmt.Errorf("count metric events: %w", queryError)
	}
	if queryError := database.QueryRow(
		`SELECT COUNT(*) FROM journal_events WHERE event IN ` + realEventNamesSQL + ` AND ` + replay.CmdrFilterSQL,
	).Scan(&summary.RealizationEvents); queryError != nil {
		return fmt.Errorf("count realization events: %w", queryError)
	}
	if queryError := database.QueryRow(`SELECT COUNT(*) FROM commanders`).Scan(&summary.CommanderCount); queryError != nil {
		return fmt.Errorf("count commanders: %w", queryError)
	}
	if queryError := database.QueryRow(`SELECT COUNT(*) FROM expeditions`).Scan(&summary.ExpeditionCount); queryError != nil {
		return fmt.Errorf("count expeditions: %w", queryError)
	}
	if queryError := database.QueryRow(`SELECT COUNT(*) FROM gec_pois_cache`).Scan(&summary.GECPOICount); queryError != nil {
		return fmt.Errorf("count GEC POIs: %w", queryError)
	}
	return nil
}

func selectPrimaryExpedition(database *sql.DB, summary *DatasetSummary) error {
	expeditionsList, listError := expedition.List(database)
	if listError != nil || len(expeditionsList) == 0 {
		return listError
	}
	chosenExpedition := expeditionsList[0]
	bestScore := -1
	for _, candidate := range expeditionsList {
		candidateRows, rowsError := expedition.ListRows(database, candidate.ID)
		if rowsError != nil {
			return fmt.Errorf("list rows for expedition %d: %w", candidate.ID, rowsError)
		}
		visitedCount := 0
		for _, rowItem := range candidateRows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt != "" {
				visitedCount++
			}
		}
		score := visitedCount*1000 + len(candidateRows)
		if candidate.Status == expedition.StatusActive {
			score += 50000
		}
		if score > bestScore {
			bestScore = score
			chosenExpedition = candidate
		}
	}

	summary.PrimaryExpeditionID = chosenExpedition.ID
	summary.PrimaryExpeditionName = chosenExpedition.Name
	summary.PrimaryExpeditionStatus = chosenExpedition.Status
	return populatePrimaryWaypointSummary(database, chosenExpedition.ID, summary)
}

func populatePrimaryWaypointSummary(database *sql.DB, expeditionID int64, summary *DatasetSummary) error {
	rows, rowsError := expedition.ListRows(database, expeditionID)
	if rowsError != nil {
		return fmt.Errorf("list primary expedition rows: %w", rowsError)
	}
	summary.PrimaryRowCount = len(rows)
	waypointNumber := 0
	for _, rowItem := range rows {
		if !rowItem.IsWaypoint() {
			continue
		}
		waypointNumber++
		summary.PrimaryWaypointCount++
		if waypointNumber == 1 {
			summary.OriginRowID = rowItem.ID
		}
		if rowItem.VisitedAt != "" {
			summary.PrimaryVisitedCount++
			if waypointNumber > 1 && summary.SampleVisitedLegRowID == 0 {
				summary.SampleVisitedLegRowID = rowItem.ID
				summary.SampleVisitedLegNumber = waypointNumber
			}
		}
	}
	return nil
}

func buildRepresentativeWindows(database *sql.DB, summary DatasetSummary) ([]RepresentativeWindow, error) {
	var windows []RepresentativeWindow

	legWindow, hasLegWindow, legError := findSingleLegWindow(database, summary)
	if legError != nil {
		return nil, legError
	}
	if hasLegWindow {
		windows = append(windows, legWindow)
	}

	expeditionWindow, hasExpeditionWindow, expError := findExpeditionWindow(database, summary)
	if expError != nil {
		return nil, expError
	}
	if hasExpeditionWindow {
		windows = append(windows, expeditionWindow)
	}

	windows = append(windows, RepresentativeWindow{
		Label:          "AllTime",
		AfterTimestamp: "",
		UntilTimestamp: "9999-12-31T23:59:59Z",
		MetricEvents:   summary.MetricEvents,
		RealEvents:     summary.RealizationEvents,
	})
	return windows, nil
}

func findSingleLegWindow(database *sql.DB, summary DatasetSummary) (RepresentativeWindow, bool, error) {
	if summary.PrimaryExpeditionID > 0 {
		rows, rowsError := expedition.ListRows(database, summary.PrimaryExpeditionID)
		if rowsError != nil {
			return RepresentativeWindow{}, false, rowsError
		}
		var visitedTimestamps []string
		for _, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt != "" {
				visitedTimestamps = append(visitedTimestamps, rowItem.VisitedAt)
			}
		}
		slices.Sort(visitedTimestamps)
		for i := 1; i < len(visitedTimestamps); i++ {
			afterTimestamp := visitedTimestamps[i-1]
			untilTimestamp := visitedTimestamps[i]
			metricCount, realCount, countError := countWindowEvents(database, afterTimestamp, untilTimestamp)
			if countError != nil {
				return RepresentativeWindow{}, false, countError
			}
			if metricCount >= 5 {
				return RepresentativeWindow{
					Label:          "SingleLeg",
					AfterTimestamp: afterTimestamp,
					UntilTimestamp: untilTimestamp,
					MetricEvents:   metricCount,
					RealEvents:     realCount,
				}, true, nil
			}
		}
	}
	return fallbackSingleLegWindow(database)
}

func fallbackSingleLegWindow(database *sql.DB) (RepresentativeWindow, bool, error) {
	var firstTimestamp, endSliceTimestamp sql.NullString
	if minError := database.QueryRow(
		`SELECT MIN(timestamp) FROM journal_events WHERE ` + replay.CmdrFilterSQL,
	).Scan(&firstTimestamp); minError != nil {
		return RepresentativeWindow{}, false, minError
	}
	offsetError := database.QueryRow(
		`SELECT timestamp FROM journal_events WHERE ` + replay.CmdrFilterSQL + ` ORDER BY timestamp ASC LIMIT 1 OFFSET 250`,
	).Scan(&endSliceTimestamp)
	if offsetError != nil && !errors.Is(offsetError, sql.ErrNoRows) {
		return RepresentativeWindow{}, false, offsetError
	}
	if firstTimestamp.Valid && endSliceTimestamp.Valid && endSliceTimestamp.String > firstTimestamp.String {
		metricCount, realCount, countError := countWindowEvents(database, firstTimestamp.String, endSliceTimestamp.String)
		if countError != nil {
			return RepresentativeWindow{}, false, countError
		}
		return RepresentativeWindow{
			Label:          "SingleLeg",
			AfterTimestamp: firstTimestamp.String,
			UntilTimestamp: endSliceTimestamp.String,
			MetricEvents:   metricCount,
			RealEvents:     realCount,
		}, true, nil
	}
	return RepresentativeWindow{}, false, nil
}

func findExpeditionWindow(database *sql.DB, summary DatasetSummary) (RepresentativeWindow, bool, error) {
	if summary.PrimaryExpeditionID > 0 {
		expeditionRecord, getError := expedition.Get(database, summary.PrimaryExpeditionID)
		if getError != nil {
			return RepresentativeWindow{}, false, getError
		}
		if expeditionRecord.StartTimestamp != "" {
			queryAfter, _, departed, startError := expedition.EffectiveStart(database, expeditionRecord)
			if startError != nil {
				return RepresentativeWindow{}, false, startError
			}
			if departed {
				untilTimestamp := expeditionRecord.EndTimestamp
				if untilTimestamp == "" {
					untilTimestamp = "9999-12-31T23:59:59Z"
				}
				metricCount, realCount, countError := countWindowEvents(database, queryAfter, untilTimestamp)
				if countError != nil {
					return RepresentativeWindow{}, false, countError
				}
				if metricCount > 0 {
					return RepresentativeWindow{
						Label:          "ExpeditionWindow",
						AfterTimestamp: queryAfter,
						UntilTimestamp: untilTimestamp,
						MetricEvents:   metricCount,
						RealEvents:     realCount,
					}, true, nil
				}
			}
		}
	}
	if summary.TotalEvents <= 20 {
		return RepresentativeWindow{}, false, nil
	}
	offset := max(summary.TotalEvents*3/4, 1)
	var midTimestamp sql.NullString
	offsetError := database.QueryRow(
		`SELECT timestamp FROM journal_events WHERE `+replay.CmdrFilterSQL+` ORDER BY timestamp ASC LIMIT 1 OFFSET ?`,
		offset,
	).Scan(&midTimestamp)
	if offsetError != nil && !errors.Is(offsetError, sql.ErrNoRows) {
		return RepresentativeWindow{}, false, offsetError
	}
	if !midTimestamp.Valid || midTimestamp.String == "" {
		return RepresentativeWindow{}, false, nil
	}
	metricCount, realCount, countError := countWindowEvents(database, midTimestamp.String, "9999-12-31T23:59:59Z")
	if countError != nil {
		return RepresentativeWindow{}, false, countError
	}
	return RepresentativeWindow{
		Label:          "ExpeditionWindow",
		AfterTimestamp: midTimestamp.String,
		UntilTimestamp: "9999-12-31T23:59:59Z",
		MetricEvents:   metricCount,
		RealEvents:     realCount,
	}, true, nil
}

func countWindowEvents(database *sql.DB, afterTimestamp, untilTimestamp string) (int, int, error) {
	var metricCount, realCount int
	if metricError := database.QueryRow(
		`SELECT COUNT(*) FROM journal_events WHERE event IN `+metricEventNamesSQL+` AND timestamp > ? AND timestamp <= ? AND `+replay.CmdrFilterSQL,
		afterTimestamp, untilTimestamp,
	).Scan(&metricCount); metricError != nil {
		return 0, 0, fmt.Errorf("count window metric events: %w", metricError)
	}
	if realError := database.QueryRow(
		`SELECT COUNT(*) FROM journal_events WHERE event IN `+realEventNamesSQL+` AND timestamp > ? AND timestamp <= ? AND `+replay.CmdrFilterSQL,
		afterTimestamp, untilTimestamp,
	).Scan(&realCount); realError != nil {
		return 0, 0, fmt.Errorf("count window realization events: %w", realError)
	}
	return metricCount, realCount, nil
}

