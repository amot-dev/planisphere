// Package replay executes stateless chronological replay over persisted journal
// events and serves read-only queries against the SQLite journal archive.
package replay

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// Status describes the persisted journal archive.
type Status struct {
	FilesTracked int    `json:"filesTracked"`
	EventsStored int    `json:"eventsStored"`
	FirstEventAt string `json:"firstEventAt"`
	LastEventAt  string `json:"lastEventAt"`
}

// CmdrFilterSQL is a SQL predicate on journal_events that excludes events
// belonging to any commander the user has explicitly disabled in Settings.
const CmdrFilterSQL = `(commander IS NULL OR commander = '' OR NOT EXISTS (SELECT 1 FROM commanders c WHERE c.name = journal_events.commander COLLATE NOCASE AND c.enabled = 0))`

// HasDisabledCommanders reports whether any discovered commander is currently
// disabled in Settings.
func HasDisabledCommanders(queryer db.Queryer) (bool, error) {
	var hasDisabled int
	if queryError := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM commanders WHERE enabled = 0)`).Scan(&hasDisabled); queryError != nil {
		return false, queryError
	}
	return hasDisabled != 0, nil
}

// ArchiveStatus returns counts of tracked files and stored events.
func ArchiveStatus(queryer db.Queryer) (Status, error) {
	var status Status
	var firstTimestamp, lastTimestamp sql.NullString
	if queryError := queryer.QueryRow(`SELECT COUNT(*) FROM ingested_journals`).Scan(&status.FilesTracked); queryError != nil {
		return status, fmt.Errorf("count ingested journals: %w", queryError)
	}
	hasDisabled, disabledCheckError := HasDisabledCommanders(queryer)
	if disabledCheckError != nil {
		return status, fmt.Errorf("check disabled commanders: %w", disabledCheckError)
	}
	var archiveQuery string
	if !hasDisabled {
		archiveQuery = `SELECT
			(SELECT COUNT(*) FROM journal_events),
			(SELECT timestamp FROM journal_events ORDER BY timestamp ASC LIMIT 1),
			(SELECT timestamp FROM journal_events ORDER BY timestamp DESC LIMIT 1)`
	} else {
		archiveQuery = `SELECT
			(SELECT COUNT(*) FROM journal_events WHERE ` + CmdrFilterSQL + `),
			(SELECT timestamp FROM journal_events WHERE ` + CmdrFilterSQL + ` ORDER BY timestamp ASC LIMIT 1),
			(SELECT timestamp FROM journal_events WHERE ` + CmdrFilterSQL + ` ORDER BY timestamp DESC LIMIT 1)`
	}
	if queryError := queryer.QueryRow(archiveQuery).Scan(&status.EventsStored, &firstTimestamp, &lastTimestamp); queryError != nil {
		return status, fmt.Errorf("query archive event counts: %w", queryError)
	}
	status.FirstEventAt, status.LastEventAt = firstTimestamp.String, lastTimestamp.String
	return status, nil
}

// FirstEventTimestamp returns the earliest ISO-8601 timestamp across enabled commanders.
func FirstEventTimestamp(queryer db.Queryer) string {
	var firstTimestamp sql.NullString
	_ = queryer.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + CmdrFilterSQL).Scan(&firstTimestamp)
	return firstTimestamp.String
}

// CareerStart returns the earliest active commander timestamp in ISO-8601 UTC
// and Galactic Standard Time formats.
func CareerStart(queryer db.Queryer) (string, string) {
	var earliestSeenTimestamp sql.NullString
	_ = queryer.QueryRow(
		`SELECT MIN(commanders.first_seen) FROM commanders WHERE commanders.enabled = 1 AND commanders.first_seen IS NOT NULL AND commanders.first_seen != ''`,
	).Scan(&earliestSeenTimestamp)
	if !earliestSeenTimestamp.Valid || earliestSeenTimestamp.String == "" {
		earliestTimestamp := FirstEventTimestamp(queryer)
		if earliestTimestamp == "" {
			return "", ""
		}
		return earliestTimestamp, reference.ToGalactic(earliestTimestamp)
	}
	return earliestSeenTimestamp.String, reference.ToGalactic(earliestSeenTimestamp.String)
}

// Commander represents a player commander identity discovered in journals.
type Commander struct {
	Name          string `json:"name"`
	FID           string `json:"fid"`
	Enabled       bool   `json:"enabled"`
	FirstSeen     string `json:"firstSeen"`
	LastSeen      string `json:"lastSeen"`
	FirstGalactic string `json:"firstGalactic"`
	LastGalactic  string `json:"lastGalactic"`
	EventCount    int    `json:"eventCount"`
}

// sqliteNocaseKey folds ASCII uppercase bytes to lowercase to match SQLite's
// built-in COLLATE NOCASE behavior byte-for-byte.
func sqliteNocaseKey(value string) string {
	hasUpperASCII := false
	for index := 0; index < len(value); index++ {
		if value[index] >= 'A' && value[index] <= 'Z' {
			hasUpperASCII = true
			break
		}
	}
	if !hasUpperASCII {
		return value
	}
	folded := []byte(value)
	for index := range folded {
		if folded[index] >= 'A' && folded[index] <= 'Z' {
			folded[index] += 'a' - 'A'
		}
	}
	return string(folded)
}

func queryCommanderEventCounts(queryer db.Queryer) (map[string]int, error) {
	rows, queryError := queryer.Query(`SELECT commander COLLATE NOCASE, COUNT(*)
		FROM journal_events
		WHERE commander IS NOT NULL AND commander != ''
		GROUP BY commander COLLATE NOCASE`)
	if queryError != nil {
		return nil, fmt.Errorf("query commander event counts: %w", queryError)
	}
	defer rows.Close()

	countsByCommander := make(map[string]int)
	for rows.Next() {
		var commanderName string
		var eventCount int
		if scanError := rows.Scan(&commanderName, &eventCount); scanError != nil {
			return nil, fmt.Errorf("scan commander event count: %w", scanError)
		}
		countsByCommander[sqliteNocaseKey(commanderName)] += eventCount
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate commander event counts: %w", iterationError)
	}
	return countsByCommander, nil
}

// ListCommanders returns all discovered commanders ordered by first_seen.
func ListCommanders(queryer db.Queryer) ([]Commander, error) {
	rows, queryError := queryer.Query(`SELECT name, COALESCE(fid, ''), enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '')
		FROM commanders
		ORDER BY first_seen ASC, name ASC`)
	if queryError != nil {
		return nil, fmt.Errorf("query commanders: %w", queryError)
	}
	defer rows.Close()

	discoveredCommanders := []Commander{}
	for rows.Next() {
		var commander Commander
		var enabledFlag int
		if scanError := rows.Scan(&commander.Name, &commander.FID, &enabledFlag, &commander.FirstSeen, &commander.LastSeen); scanError != nil {
			return nil, fmt.Errorf("scan commander row: %w", scanError)
		}
		commander.Enabled = enabledFlag != 0
		commander.FirstGalactic = reference.ToGalactic(commander.FirstSeen)
		commander.LastGalactic = reference.ToGalactic(commander.LastSeen)
		discoveredCommanders = append(discoveredCommanders, commander)
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate commander rows: %w", iterationError)
	}
	if len(discoveredCommanders) == 0 {
		return discoveredCommanders, nil
	}

	countsByCommander, countsError := queryCommanderEventCounts(queryer)
	if countsError != nil {
		return nil, countsError
	}
	for index := range discoveredCommanders {
		discoveredCommanders[index].EventCount = countsByCommander[sqliteNocaseKey(discoveredCommanders[index].Name)]
	}
	return discoveredCommanders, nil
}

