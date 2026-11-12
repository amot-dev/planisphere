package journal

import (
	"database/sql"
	"fmt"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
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

// ArchiveStatus returns counts of tracked files and stored events.
func ArchiveStatus(queryer db.Queryer) (Status, error) {
	var status Status
	var firstTimestamp, lastTimestamp sql.NullString
	if queryError := queryer.QueryRow(`SELECT COUNT(*) FROM ingested_journals`).Scan(&status.FilesTracked); queryError != nil {
		return status, fmt.Errorf("count ingested journals: %w", queryError)
	}
	var hasDisabled int
	if queryError := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM commanders WHERE enabled = 0)`).Scan(&hasDisabled); queryError != nil {
		return status, fmt.Errorf("check disabled commanders: %w", queryError)
	}
	var archiveQuery string
	if hasDisabled == 0 {
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

// ListCommanders returns all discovered commanders ordered by first_seen.
func ListCommanders(queryer db.Queryer) ([]Commander, error) {
	rows, queryError := queryer.Query(`SELECT c.name, COALESCE(c.fid, ''), c.enabled, COALESCE(c.first_seen, ''), COALESCE(c.last_seen, ''),
		(SELECT COUNT(*) FROM journal_events je WHERE je.commander = c.name COLLATE NOCASE)
		FROM commanders c
		ORDER BY c.first_seen ASC, c.name ASC`)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	discoveredCommanders := []Commander{}
	for rows.Next() {
		var commander Commander
		var enabledFlag int
		if scanError := rows.Scan(&commander.Name, &commander.FID, &enabledFlag, &commander.FirstSeen, &commander.LastSeen, &commander.EventCount); scanError != nil {
			return nil, scanError
		}
		commander.Enabled = enabledFlag != 0
		commander.FirstGalactic = ed.ToGalactic(commander.FirstSeen)
		commander.LastGalactic = ed.ToGalactic(commander.LastSeen)
		discoveredCommanders = append(discoveredCommanders, commander)
	}
	return discoveredCommanders, rows.Err()
}

// SetCommanderEnabled toggles whether a commander's events are included in stats and plans.
func SetCommanderEnabled(queryer db.Queryer, name string, enabled bool) error {
	enabledFlag := 0
	if enabled {
		enabledFlag = 1
	}
	_, execError := queryer.Exec(`UPDATE commanders SET enabled = ? WHERE name = ? COLLATE NOCASE`, enabledFlag, strings.TrimSpace(name))
	return execError
}

