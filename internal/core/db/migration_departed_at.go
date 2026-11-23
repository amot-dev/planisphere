package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	migrationCmdrFilterSQL   = `(commander IS NULL OR commander = '' OR NOT EXISTS (SELECT 1 FROM commanders c WHERE c.name = journal_events.commander COLLATE NOCASE AND c.enabled = 0))`
	migrationNonMulticrewSQL = `(instr(raw_json, '"Multicrew"') = 0 OR NOT COALESCE(json_extract(raw_json, '$.Multicrew'), 0))`
)

type migrationExpeditionRecord struct {
	id             int64
	status         string
	startTimestamp string
}

type migrationVisitedRow struct {
	id            int64
	systemName    string
	systemAddress int64
	visitedAt     string
	visitEventUID string
	departedAt    string
}

type migrationArrivalRow struct {
	uid           string
	timestamp     string
	systemName    string
	systemAddress int64
	commander     string
}

func migrateDepartedAtV12(dbConn *sql.DB) error {
	expeditions, listError := loadStartedExpeditionsForV12(dbConn)
	if listError != nil {
		return listError
	}
	if len(expeditions) == 0 {
		return nil
	}

	return WithTx(dbConn, func(transaction *sql.Tx) error {
		for _, expeditionItem := range expeditions {
			if backfillError := backfillExpeditionDeparturesV12(transaction, expeditionItem); backfillError != nil {
				return backfillError
			}
		}
		return nil
	})
}

func loadStartedExpeditionsForV12(queryer Queryer) ([]migrationExpeditionRecord, error) {
	rows, queryError := queryer.Query(`
		SELECT id, status, COALESCE(start_timestamp, '')
		FROM expeditions
		WHERE status IN ('active', 'completed') AND COALESCE(start_timestamp, '') != ''
		ORDER BY id ASC`)
	if queryError != nil {
		return nil, fmt.Errorf("query started expeditions (v12): %w", queryError)
	}
	defer rows.Close()

	var expeditions []migrationExpeditionRecord
	for rows.Next() {
		var item migrationExpeditionRecord
		if scanError := rows.Scan(&item.id, &item.status, &item.startTimestamp); scanError != nil {
			return nil, fmt.Errorf("scan started expedition (v12): %w", scanError)
		}
		expeditions = append(expeditions, item)
	}
	return expeditions, rows.Err()
}

func backfillExpeditionDeparturesV12(transaction *sql.Tx, expeditionItem migrationExpeditionRecord) error {
	visitedRows, rowsError := loadVisitedRowsForV12(transaction, expeditionItem.id)
	if rowsError != nil || len(visitedRows) == 0 {
		return rowsError
	}

	arrivals, arrivalsError := loadNonMulticrewArrivalsSinceV12(transaction, expeditionItem.startTimestamp)
	if arrivalsError != nil {
		return arrivalsError
	}

	finalIndex := 0
	var finalSystemAddress int64
	var finalStayCommander string
	for index := range visitedRows {
		resolvedAddress, stayCommander := resolveRowArrivalContextV12(visitedRows[index], arrivals)
		visitedRows[index].departedAt = findFirstDepartureV12(visitedRows[index], stayCommander, arrivals)
		if index == 0 || visitedRows[index].visitedAt > visitedRows[finalIndex].visitedAt {
			finalIndex = index
			finalSystemAddress = resolvedAddress
			finalStayCommander = stayCommander
		}
	}

	if expeditionItem.status == "completed" {
		finalRow := &visitedRows[finalIndex]
		stayUpperBound := subtractOneSecondClampedV12(finalRow.departedAt, finalRow.visitedAt)
		lastActivity, activityError := queryLastInSystemActivityV12(
			transaction,
			finalRow.visitedAt,
			stayUpperBound,
			finalRow.systemName,
			finalSystemAddress,
			finalStayCommander,
		)
		if activityError != nil {
			return activityError
		}
		normalizedEnd := finalRow.visitedAt
		if lastActivity > normalizedEnd {
			normalizedEnd = lastActivity
		}
		if _, updateExpError := transaction.Exec(
			`UPDATE expeditions SET end_timestamp = ? WHERE id = ?`,
			normalizedEnd, expeditionItem.id,
		); updateExpError != nil {
			return fmt.Errorf("update completed expedition %d end_timestamp (v12): %w", expeditionItem.id, updateExpError)
		}
		// Match Complete plus Reevaluate behavior: any departure occurring strictly after
		// the locked completion timestamp falls outside the completed expedition window.
		if finalRow.departedAt != "" && finalRow.departedAt > normalizedEnd {
			finalRow.departedAt = ""
		}
	}

	for _, visitedRow := range visitedRows {
		var departedValue any
		if visitedRow.departedAt != "" {
			departedValue = visitedRow.departedAt
		}
		if _, updateRowError := transaction.Exec(
			`UPDATE expedition_rows SET departed_at = ? WHERE id = ?`,
			departedValue, visitedRow.id,
		); updateRowError != nil {
			return fmt.Errorf("update row %d departed_at (v12): %w", visitedRow.id, updateRowError)
		}
	}
	return nil
}

func loadVisitedRowsForV12(queryer Queryer, expeditionID int64) ([]migrationVisitedRow, error) {
	rows, queryError := queryer.Query(`
		SELECT id, system_name, COALESCE(system_address, 0), COALESCE(visited_at, ''), COALESCE(visit_event_uid, '')
		FROM expedition_rows
		WHERE expedition_id = ? AND row_type = 'waypoint' AND COALESCE(visited_at, '') != ''
		ORDER BY position, id`, expeditionID)
	if queryError != nil {
		return nil, fmt.Errorf("query visited rows for expedition %d (v12): %w", expeditionID, queryError)
	}
	defer rows.Close()

	var visitedRows []migrationVisitedRow
	for rows.Next() {
		var rowItem migrationVisitedRow
		if scanError := rows.Scan(&rowItem.id, &rowItem.systemName, &rowItem.systemAddress, &rowItem.visitedAt, &rowItem.visitEventUID); scanError != nil {
			return nil, fmt.Errorf("scan visited row for expedition %d (v12): %w", expeditionID, scanError)
		}
		visitedRows = append(visitedRows, rowItem)
	}
	return visitedRows, rows.Err()
}

func loadNonMulticrewArrivalsSinceV12(queryer Queryer, startTimestamp string) ([]migrationArrivalRow, error) {
	rows, queryError := queryer.Query(`
		SELECT event_uid, timestamp, COALESCE(system_name, ''), COALESCE(system_address, 0), COALESCE(commander, '')
		FROM journal_events
		WHERE event IN ('FSDJump', 'Location', 'CarrierJump')
		  AND timestamp >= ?
		  AND `+migrationCmdrFilterSQL+`
		  AND `+migrationNonMulticrewSQL+`
		ORDER BY timestamp, id`, startTimestamp)
	if queryError != nil {
		return nil, fmt.Errorf("query arrivals since %s (v12): %w", startTimestamp, queryError)
	}
	defer rows.Close()

	var arrivals []migrationArrivalRow
	for rows.Next() {
		var arrivalItem migrationArrivalRow
		if scanError := rows.Scan(&arrivalItem.uid, &arrivalItem.timestamp, &arrivalItem.systemName, &arrivalItem.systemAddress, &arrivalItem.commander); scanError != nil {
			return nil, fmt.Errorf("scan arrival (v12): %w", scanError)
		}
		arrivals = append(arrivals, arrivalItem)
	}
	return arrivals, rows.Err()
}

func sameSystemForV12(leftName string, leftAddress int64, rightName string, rightAddress int64) bool {
	if leftAddress != 0 && rightAddress != 0 && leftAddress == rightAddress {
		return true
	}
	normalizedLeft := strings.ToLower(strings.TrimSpace(leftName))
	normalizedRight := strings.ToLower(strings.TrimSpace(rightName))
	return normalizedLeft != "" && normalizedLeft == normalizedRight
}

func sameCommanderForV12(leftCommander, rightCommander string) bool {
	if leftCommander == "" || rightCommander == "" {
		return true
	}
	return strings.EqualFold(leftCommander, rightCommander)
}

func resolveRowArrivalContextV12(rowItem migrationVisitedRow, arrivals []migrationArrivalRow) (int64, string) {
	resolvedAddress := rowItem.systemAddress
	for _, arrival := range arrivals {
		if rowItem.visitEventUID != "" && arrival.uid == rowItem.visitEventUID {
			if resolvedAddress == 0 {
				resolvedAddress = arrival.systemAddress
			}
			return resolvedAddress, arrival.commander
		}
	}
	for _, arrival := range arrivals {
		if arrival.timestamp == rowItem.visitedAt && sameSystemForV12(rowItem.systemName, rowItem.systemAddress, arrival.systemName, arrival.systemAddress) {
			if resolvedAddress == 0 {
				resolvedAddress = arrival.systemAddress
			}
			return resolvedAddress, arrival.commander
		}
	}
	return resolvedAddress, ""
}

func findFirstDepartureV12(rowItem migrationVisitedRow, stayCommander string, arrivals []migrationArrivalRow) string {
	for _, arrival := range arrivals {
		if arrival.timestamp <= rowItem.visitedAt {
			continue
		}
		if !sameCommanderForV12(stayCommander, arrival.commander) {
			continue
		}
		if strings.TrimSpace(arrival.systemName) == "" && arrival.systemAddress == 0 {
			continue
		}
		if sameSystemForV12(rowItem.systemName, rowItem.systemAddress, arrival.systemName, arrival.systemAddress) {
			continue
		}
		return arrival.timestamp
	}
	return ""
}

func subtractOneSecondClampedV12(departedAt, visitedAt string) string {
	if departedAt == "" {
		return ""
	}
	parsedTime, parseError := time.Parse("2006-01-02T15:04:05Z", departedAt)
	if parseError != nil {
		return departedAt
	}
	bounded := parsedTime.Add(-time.Second).UTC().Format("2006-01-02T15:04:05Z")
	if visitedAt != "" && bounded < visitedAt {
		return visitedAt
	}
	return bounded
}

func queryLastInSystemActivityV12(queryer Queryer, afterTimestamp, untilTimestamp, systemName string, systemAddress int64, stayCommander string) (string, error) {
	query := `SELECT COALESCE(MAX(timestamp), '')
		FROM journal_events
		WHERE timestamp > ?`
	queryArgs := []any{afterTimestamp}
	if untilTimestamp != "" {
		query += ` AND timestamp <= ?`
		queryArgs = append(queryArgs, untilTimestamp)
	}
	query += `
		  AND (
			(event = 'Scan' AND COALESCE(body_name, '') NOT LIKE '%Belt Cluster%' AND (COALESCE(json_extract(raw_json, '$.StarType'), '') != '' OR COALESCE(json_extract(raw_json, '$.PlanetClass'), '') != ''))
			OR (event = 'SAAScanComplete' AND COALESCE(json_extract(raw_json, '$.EfficiencyTarget'), 0) > 0)
			OR (event = 'ScanOrganic' AND LOWER(COALESCE(json_extract(raw_json, '$.ScanType'), '')) = 'analyse')
		  )`
	if systemAddress != 0 {
		query += ` AND system_address = ?`
		queryArgs = append(queryArgs, systemAddress)
	} else if trimmedSystemName := strings.TrimSpace(systemName); trimmedSystemName != "" {
		query += ` AND system_name = ? COLLATE NOCASE`
		queryArgs = append(queryArgs, trimmedSystemName)
	}
	if trimmedCommander := strings.TrimSpace(stayCommander); trimmedCommander != "" {
		query += ` AND (COALESCE(commander, '') = '' OR commander = ? COLLATE NOCASE)`
		queryArgs = append(queryArgs, trimmedCommander)
	}
	query += `
		  AND ` + migrationCmdrFilterSQL + `
		  AND ` + migrationNonMulticrewSQL

	var lastActivity string
	if scanError := queryer.QueryRow(query, queryArgs...).Scan(&lastActivity); scanError != nil {
		return "", fmt.Errorf("query last in-system activity after %s (v12): %w", afterTimestamp, scanError)
	}
	return lastActivity, nil
}
