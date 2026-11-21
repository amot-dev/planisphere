// Package replay executes stateless chronological replay over persisted journal
// events and serves read-only queries against the SQLite journal archive.
package replay

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

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

// Bounds per-commander EXISTS and COUNT(*) subqueries in the fast-path query.
const maxFastPathCommanders = 16

const sampleDominantCommanderSQL = `WITH bounds AS (
		SELECT
			(SELECT id FROM journal_events ORDER BY id ASC LIMIT 1) AS min_id,
			(SELECT id FROM journal_events ORDER BY id DESC LIMIT 1) AS max_id
	)
	SELECT
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 1 / 8 ORDER BY id ASC LIMIT 1),
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 2 / 8 ORDER BY id ASC LIMIT 1),
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 3 / 8 ORDER BY id ASC LIMIT 1),
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 4 / 8 ORDER BY id ASC LIMIT 1),
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 5 / 8 ORDER BY id ASC LIMIT 1),
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 6 / 8 ORDER BY id ASC LIMIT 1),
		(SELECT commander FROM journal_events WHERE id >= bounds.min_id + (bounds.max_id - bounds.min_id) * 7 / 8 ORDER BY id ASC LIMIT 1)
	FROM bounds
	WHERE bounds.min_id IS NOT NULL`

type sortedCommanderKey struct {
	name      string
	nocaseKey string
}

func sortCommanderKeys(commanders []Commander) ([]sortedCommanderKey, bool) {
	sortedCommanders := make([]sortedCommanderKey, len(commanders))
	for i, commander := range commanders {
		if commander.Name == "" {
			return nil, false
		}
		sortedCommanders[i] = sortedCommanderKey{
			name:      commander.Name,
			nocaseKey: sqliteNocaseKey(commander.Name),
		}
	}
	slices.SortFunc(sortedCommanders, func(left, right sortedCommanderKey) int {
		return cmp.Compare(left.nocaseKey, right.nocaseKey)
	})
	for i := 1; i < len(sortedCommanders); i++ {
		if sortedCommanders[i].nocaseKey == sortedCommanders[i-1].nocaseKey {
			return nil, false
		}
	}
	return sortedCommanders, true
}

// sampleDominantCommanderIndex probes seven evenly spaced rowids across
// journal_events to pick the commander with the most rows so the fast path can
// derive its count by subtraction instead of scanning its index range. Returns
// false as the second result when sampled rows include an unlisted commander or
// unattributed rows outnumber the top commander's sample votes.
func sampleDominantCommanderIndex(queryer db.Queryer, sortedCommanders []sortedCommanderKey) (int, bool, error) {
	var samples [7]sql.NullString
	sampleError := queryer.QueryRow(sampleDominantCommanderSQL).Scan(
		&samples[0], &samples[1], &samples[2], &samples[3], &samples[4], &samples[5], &samples[6],
	)
	if errors.Is(sampleError, sql.ErrNoRows) {
		return 0, true, nil
	}
	if sampleError != nil {
		return 0, false, fmt.Errorf("sample dominant commander: %w", sampleError)
	}

	indexByKey := make(map[string]int, len(sortedCommanders))
	for i, entry := range sortedCommanders {
		indexByKey[entry.nocaseKey] = i
	}
	votesByIndex := make([]int, len(sortedCommanders))
	unattributedVotes := 0
	for _, sample := range samples {
		if !sample.Valid || sample.String == "" {
			unattributedVotes++
			continue
		}
		matchedIndex, found := indexByKey[sqliteNocaseKey(sample.String)]
		if !found {
			return 0, false, nil
		}
		votesByIndex[matchedIndex]++
	}

	dominantIndex := 0
	for i := 1; i < len(votesByIndex); i++ {
		if votesByIndex[i] > votesByIndex[dominantIndex] {
			dominantIndex = i
		}
	}
	if unattributedVotes > votesByIndex[dominantIndex] {
		return 0, false, nil
	}
	return dominantIndex, true, nil
}

// buildFastCommanderCountQuery constructs a single atomic SELECT that reads the
// total event count via OP_Count, counts NULL and empty-string rows, checks
// every inter-commander gap in idx_journal_events_cmdr with O(log N) EXISTS
// seeks, and counts only the non-dominant commanders.
func buildFastCommanderCountQuery(sortedCommanders []sortedCommanderKey, dominantIndex int) (string, []any, []int) {
	commanderCount := len(sortedCommanders)
	queryArgs := make([]any, 0, commanderCount*3)
	countedIndices := make([]int, 0, commanderCount-1)

	var queryBuilder strings.Builder
	queryBuilder.WriteString(`SELECT
		(SELECT COUNT(*) FROM journal_events),
		(SELECT COUNT(*) FROM journal_events WHERE commander IS NULL),
		(SELECT COUNT(*) FROM journal_events WHERE commander = '' COLLATE NOCASE),
		(EXISTS(SELECT 1 FROM journal_events WHERE commander IS NOT NULL AND commander < '' COLLATE NOCASE)`)

	queryBuilder.WriteString(` OR EXISTS(SELECT 1 FROM journal_events WHERE commander > '' COLLATE NOCASE AND commander < ? COLLATE NOCASE)`)
	queryArgs = append(queryArgs, sortedCommanders[0].name)

	for i := 0; i < commanderCount-1; i++ {
		queryBuilder.WriteString(` OR EXISTS(SELECT 1 FROM journal_events WHERE commander > ? COLLATE NOCASE AND commander < ? COLLATE NOCASE)`)
		queryArgs = append(queryArgs, sortedCommanders[i].name, sortedCommanders[i+1].name)
	}

	queryBuilder.WriteString(` OR EXISTS(SELECT 1 FROM journal_events WHERE commander > ? COLLATE NOCASE))`)
	queryArgs = append(queryArgs, sortedCommanders[commanderCount-1].name)

	for i, entry := range sortedCommanders {
		if i == dominantIndex {
			continue
		}
		queryBuilder.WriteString(`, (SELECT COUNT(*) FROM journal_events WHERE commander = ? COLLATE NOCASE)`)
		queryArgs = append(queryArgs, entry.name)
		countedIndices = append(countedIndices, i)
	}
	return queryBuilder.String(), queryArgs, countedIndices
}

// queryCommanderEventCountsFast computes per-commander event counts without a
// full idx_journal_events_cmdr GROUP BY scan whenever all non-empty commander
// values in journal_events belong to known commanders. Returns usedFastPath=false
// when the caller should fall back to queryCommanderEventCounts.
func queryCommanderEventCountsFast(queryer db.Queryer, commanders []Commander) (map[string]int, bool, error) {
	if len(commanders) == 0 || len(commanders) > maxFastPathCommanders {
		return nil, false, nil
	}
	sortedCommanders, keysValid := sortCommanderKeys(commanders)
	if !keysValid {
		return nil, false, nil
	}

	dominantIndex := 0
	if len(sortedCommanders) > 1 {
		sampledIndex, sampleUsable, sampleError := sampleDominantCommanderIndex(queryer, sortedCommanders)
		if sampleError != nil || !sampleUsable {
			return nil, false, sampleError
		}
		dominantIndex = sampledIndex
	}

	querySQL, queryArgs, countedIndices := buildFastCommanderCountQuery(sortedCommanders, dominantIndex)
	var totalEvents, nullEvents, emptyEvents, hasUnlistedCommander int
	otherCommanderCounts := make([]int, len(countedIndices))
	scanTargets := make([]any, 0, 4+len(countedIndices))
	scanTargets = append(scanTargets, &totalEvents, &nullEvents, &emptyEvents, &hasUnlistedCommander)
	for i := range otherCommanderCounts {
		scanTargets = append(scanTargets, &otherCommanderCounts[i])
	}
	if queryError := queryer.QueryRow(querySQL, queryArgs...).Scan(scanTargets...); queryError != nil {
		return nil, false, fmt.Errorf("query fast commander event counts: %w", queryError)
	}
	if hasUnlistedCommander != 0 {
		return nil, false, nil
	}

	countsByCommander := make(map[string]int, len(sortedCommanders))
	otherEventsTotal := 0
	for i, sortedIndex := range countedIndices {
		countsByCommander[sortedCommanders[sortedIndex].nocaseKey] = otherCommanderCounts[i]
		otherEventsTotal += otherCommanderCounts[i]
	}
	countsByCommander[sortedCommanders[dominantIndex].nocaseKey] = totalEvents - nullEvents - emptyEvents - otherEventsTotal
	return countsByCommander, true, nil
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

	countsByCommander, usedFastPath, countsError := queryCommanderEventCountsFast(queryer, discoveredCommanders)
	if countsError != nil {
		return nil, countsError
	}
	if !usedFastPath {
		countsByCommander, countsError = queryCommanderEventCounts(queryer)
		if countsError != nil {
			return nil, countsError
		}
	}
	for index := range discoveredCommanders {
		discoveredCommanders[index].EventCount = countsByCommander[sqliteNocaseKey(discoveredCommanders[index].Name)]
	}
	return discoveredCommanders, nil
}
