package journal

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

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

var (
	commanderCountsMu          sync.Mutex
	cachedCommanderCounts      map[string]int
	cachedCommanderCountsDB    *sql.DB
	cachedCommanderCountsMaxID int64
	cachedCommanderCountsValid bool
	commanderCountsGen         uint64
)

// InvalidateCommanderCountsCache clears the cached per-commander journal event
// counts so backfills or imports without an increase in MAX(id) are observed immediately.
func InvalidateCommanderCountsCache() {
	commanderCountsMu.Lock()
	defer commanderCountsMu.Unlock()
	cachedCommanderCounts = nil
	cachedCommanderCountsDB = nil
	cachedCommanderCountsValid = false
	commanderCountsGen++
}

// sqliteNocaseKey folds ASCII uppercase bytes to lowercase to match SQLite's
// built-in COLLATE NOCASE behavior byte-for-byte.
func sqliteNocaseKey(value string) string {
	hasUpperASCII := false
	for i := 0; i < len(value); i++ {
		if value[i] >= 'A' && value[i] <= 'Z' {
			hasUpperASCII = true
			break
		}
	}
	if !hasUpperASCII {
		return value
	}
	folded := []byte(value)
	for i := range folded {
		if folded[i] >= 'A' && folded[i] <= 'Z' {
			folded[i] += 'a' - 'A'
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

func loadCommanderEventCounts(queryer db.Queryer) (map[string]int, error) {
	sqlDB, isSQLDB := queryer.(*sql.DB)
	if !isSQLDB {
		return queryCommanderEventCounts(queryer)
	}
	var maxJournalID int64
	if maxQueryError := sqlDB.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM journal_events`).Scan(&maxJournalID); maxQueryError != nil {
		return queryCommanderEventCounts(queryer)
	}

	commanderCountsMu.Lock()
	if cachedCommanderCountsValid && cachedCommanderCountsDB == sqlDB && cachedCommanderCountsMaxID == maxJournalID {
		cached := cachedCommanderCounts
		commanderCountsMu.Unlock()
		return cached, nil
	}
	capturedGeneration := commanderCountsGen
	commanderCountsMu.Unlock()

	computedCounts, queryError := queryCommanderEventCounts(queryer)
	if queryError != nil {
		return nil, queryError
	}

	commanderCountsMu.Lock()
	if commanderCountsGen == capturedGeneration {
		cachedCommanderCounts = computedCounts
		cachedCommanderCountsDB = sqlDB
		cachedCommanderCountsMaxID = maxJournalID
		cachedCommanderCountsValid = true
	}
	commanderCountsMu.Unlock()
	return computedCounts, nil
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
		commander.FirstGalactic = ed.ToGalactic(commander.FirstSeen)
		commander.LastGalactic = ed.ToGalactic(commander.LastSeen)
		discoveredCommanders = append(discoveredCommanders, commander)
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate commander rows: %w", iterationError)
	}
	if len(discoveredCommanders) == 0 {
		return discoveredCommanders, nil
	}

	countsByCommander, countsError := loadCommanderEventCounts(queryer)
	if countsError != nil {
		return nil, countsError
	}
	for i := range discoveredCommanders {
		discoveredCommanders[i].EventCount = countsByCommander[sqliteNocaseKey(discoveredCommanders[i].Name)]
	}
	return discoveredCommanders, nil
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

