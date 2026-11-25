package replay

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// CommanderAlias represents one observed commander name era grouped under a
// resolved commander save-slot profile.
type CommanderAlias struct {
	Name           string `json:"name"`
	FID            string `json:"fid"`
	Universe       string `json:"universe"`
	StartTimestamp string `json:"startTimestamp"`
	EndTimestamp   string `json:"endTimestamp"`
	EventCount     int    `json:"eventCount"`
	IsCurrent      bool   `json:"isCurrent"`
	Source         string `json:"source"`
}

// Commander represents a player commander save-slot career profile discovered in journals.
type Commander struct {
	ID                int64            `json:"id"`
	ProfileKey        string           `json:"profileKey"`
	Name              string           `json:"name"`
	FID               string           `json:"fid"`
	Universe          string           `json:"universe"`
	EraIndex          int              `json:"eraIndex"`
	IsArchivedCareer  bool             `json:"isArchivedCareer"`
	HasManualOverride bool             `json:"hasManualOverride"`
	Enabled           bool             `json:"enabled"`
	FirstSeen         string           `json:"firstSeen"`
	LastSeen          string           `json:"lastSeen"`
	FirstGalactic     string           `json:"firstGalactic"`
	LastGalactic      string           `json:"lastGalactic"`
	EventCount        int              `json:"eventCount"`
	Aliases           []CommanderAlias `json:"aliases"`
}

// CommanderSession describes a single LoadGame session on a UTC calendar date.
type CommanderSession struct {
	Timestamp  string `json:"timestamp"`
	Galactic   string `json:"galactic"`
	Ship       string `json:"ship"`
	SystemName string `json:"systemName"`
}

// CareerStart returns the earliest active commander timestamp in ISO-8601 UTC
// and Galactic Standard Time formats.
func CareerStart(queryer db.Queryer) (string, string) {
	return CareerStartScoped(queryer, ProfileScope{})
}

// CareerStartScoped returns the earliest career start timestamp for scope in
// ISO-8601 UTC and Galactic Standard Time formats.
func CareerStartScoped(queryer db.Queryer, scope ProfileScope) (string, string) {
	var earliestSeenTimestamp sql.NullString
	var scanErr error
	if !scope.IsAllEnabled() {
		ids := scope.normalizedIDs()
		if len(ids) == 0 {
			return "", ""
		}
		scanErr = queryer.QueryRow(
			`SELECT MIN(first_seen) FROM commander_profiles WHERE id IN (` + formatInt64CSV(ids) + `) AND first_seen IS NOT NULL AND first_seen != ''`,
		).Scan(&earliestSeenTimestamp)
	} else if profilesExist, checkErr := hasCommanderProfiles(queryer); checkErr == nil && profilesExist {
		scanErr = queryer.QueryRow(
			`SELECT MIN(first_seen) FROM commander_profiles WHERE enabled != 0 AND first_seen IS NOT NULL AND first_seen != ''`,
		).Scan(&earliestSeenTimestamp)
	} else {
		scanErr = queryer.QueryRow(
			`SELECT MIN(commanders.first_seen) FROM commanders WHERE commanders.enabled = 1 AND commanders.first_seen IS NOT NULL AND commanders.first_seen != ''`,
		).Scan(&earliestSeenTimestamp)
	}
	if scanErr != nil || !earliestSeenTimestamp.Valid || earliestSeenTimestamp.String == "" {
		earliestTimestamp := FirstEventTimestampScoped(queryer, scope)
		if earliestTimestamp == "" {
			return "", ""
		}
		return earliestTimestamp, reference.ToGalactic(earliestTimestamp)
	}
	return earliestSeenTimestamp.String, reference.ToGalactic(earliestSeenTimestamp.String)
}

func hasCommanderProfiles(queryer db.Queryer) (bool, error) {
	var existsFlag int
	if queryError := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM commander_profiles)`).Scan(&existsFlag); queryError != nil {
		return false, queryError
	}
	return existsFlag != 0, nil
}

// ListCommanders returns all resolved commander save-slot profiles ordered by
// first_seen, falling back to legacy commanders when commander_profiles is empty.
func ListCommanders(queryer db.Queryer) ([]Commander, error) {
	profiles, err := queryCommanderProfileRows(queryer)
	if err != nil || len(profiles) == 0 {
		if err != nil {
			return nil, err
		}
		return listLegacyCommanders(queryer)
	}
	if err := attachCommanderProfileAliases(queryer, profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

func queryCommanderProfileRows(queryer db.Queryer) ([]Commander, error) {
	rows, err := queryer.Query(`SELECT id, profile_key, display_name, COALESCE(fid, ''), COALESCE(universe, 'live'),
		era_index, is_archived_career, has_manual_override, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '')
		FROM commander_profiles ORDER BY first_seen ASC, display_name ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query commander_profiles: %w", err)
	}
	defer rows.Close()

	profiles := []Commander{}
	for rows.Next() {
		var item Commander
		var archivedFlag, overrideFlag, enabledFlag int
		if err := rows.Scan(&item.ID, &item.ProfileKey, &item.Name, &item.FID, &item.Universe, &item.EraIndex, &archivedFlag, &overrideFlag, &enabledFlag, &item.FirstSeen, &item.LastSeen); err != nil {
			return nil, fmt.Errorf("scan commander_profile row: %w", err)
		}
		item.IsArchivedCareer, item.HasManualOverride, item.Enabled = archivedFlag != 0, overrideFlag != 0, enabledFlag != 0
		item.FirstGalactic, item.LastGalactic = reference.ToGalactic(item.FirstSeen), reference.ToGalactic(item.LastSeen)
		item.Aliases = []CommanderAlias{}
		profiles = append(profiles, item)
	}
	return profiles, rows.Err()
}

func attachCommanderProfileAliases(queryer db.Queryer, profiles []Commander) error {
	indexByID := make(map[int64]int, len(profiles))
	for index := range profiles {
		indexByID[profiles[index].ID] = index
	}
	rows, err := queryer.Query(`SELECT profile_id, raw_name, COALESCE(fid, ''), COALESCE(universe, 'live'),
		COALESCE(start_timestamp, ''), COALESCE(end_timestamp, ''), event_count, is_current, COALESCE(source, 'auto')
		FROM commander_aliases ORDER BY profile_id ASC, is_current DESC, end_timestamp DESC, raw_name ASC`)
	if err != nil {
		return fmt.Errorf("query commander_aliases: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var profileID int64
		var alias CommanderAlias
		var isCurrentFlag int
		if err := rows.Scan(&profileID, &alias.Name, &alias.FID, &alias.Universe, &alias.StartTimestamp, &alias.EndTimestamp, &alias.EventCount, &isCurrentFlag, &alias.Source); err != nil {
			return fmt.Errorf("scan commander_alias row: %w", err)
		}
		alias.IsCurrent = isCurrentFlag != 0
		if profileIdx, found := indexByID[profileID]; found {
			profiles[profileIdx].Aliases = append(profiles[profileIdx].Aliases, alias)
			profiles[profileIdx].EventCount += alias.EventCount
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range profiles {
		if len(profiles[index].Aliases) == 0 {
			if err := queryer.QueryRow(`SELECT COUNT(*) FROM journal_events WHERE profile_id = ?`, profiles[index].ID).Scan(&profiles[index].EventCount); err != nil {
				return fmt.Errorf("count profile %d events: %w", profiles[index].ID, err)
			}
		}
	}
	return nil
}

// ListEnabledCommanderNames returns canonical display names of globally enabled
// commander profiles ordered by first_seen without historic aliases.
func ListEnabledCommanderNames(queryer db.Queryer) ([]string, error) {
	return ListCommanderNamesForScope(queryer, ProfileScope{})
}

// ListCommanderNamesForScope returns deduplicated canonical display names for
// scope in first-seen order without including historic alias names.
func ListCommanderNamesForScope(queryer db.Queryer, scope ProfileScope) ([]string, error) {
	if !scope.IsAllEnabled() {
		ids := scope.normalizedIDs()
		if len(ids) == 0 {
			return []string{}, nil
		}
		return queryDeduplicatedCommanderNames(queryer, `SELECT display_name FROM commander_profiles WHERE id IN (`+formatInt64CSV(ids)+`) AND TRIM(display_name) != '' ORDER BY first_seen ASC, display_name ASC, id ASC`)
	}
	hasProfiles, err := hasCommanderProfiles(queryer)
	if err != nil {
		return nil, err
	}
	if hasProfiles {
		return queryDeduplicatedCommanderNames(queryer, `SELECT display_name FROM commander_profiles WHERE enabled != 0 AND TRIM(display_name) != '' ORDER BY first_seen ASC, display_name ASC, id ASC`)
	}
	return queryDeduplicatedCommanderNames(queryer, `SELECT name FROM commanders WHERE enabled != 0 AND TRIM(name) != '' ORDER BY first_seen ASC, name ASC`)
}

func queryDeduplicatedCommanderNames(queryer db.Queryer, query string) ([]string, error) {
	rows, err := queryer.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query commander names: %w", err)
	}
	defer rows.Close()

	names, seenNocase := []string{}, make(map[string]bool)
	for rows.Next() {
		var rawName string
		if err := rows.Scan(&rawName); err != nil {
			return nil, err
		}
		if key := sqliteNocaseKey(strings.TrimSpace(rawName)); key != "" && !seenNocase[key] {
			seenNocase[key] = true
			names = append(names, rawName)
		}
	}
	return names, rows.Err()
}

type sessionTimelineRow struct {
	id                                       int64
	timestamp, event, systemName, sourceFile string
	rawJSON                                  string
}

// ListCommanderLoadGamesOnDate returns chronological LoadGame sessions for
// profileKey on the UTC calendar date dateISO ("YYYY-MM-DD").
func ListCommanderLoadGamesOnDate(queryer db.Queryer, profileKey string, dateISO string) ([]CommanderSession, error) {
	trimmedDate, trimmedKey := strings.TrimSpace(dateISO), strings.TrimSpace(profileKey)
	parsedDate, parseDateErr := time.Parse("2006-01-02", trimmedDate)
	if len(trimmedDate) != 10 || parseDateErr != nil {
		return nil, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", dateISO)
	}
	if trimmedKey == "" {
		return nil, errors.New("profile key is required")
	}
	var profileID int64
	if err := queryer.QueryRow(`SELECT id FROM commander_profiles WHERE LOWER(profile_key) = LOWER(?)`, trimmedKey).Scan(&profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("commander profile %q not found", trimmedKey)
		}
		return nil, fmt.Errorf("lookup commander profile %q: %w", trimmedKey, err)
	}
	lookaheadEnd := parsedDate.UTC().Add(24*time.Hour + 5*time.Minute).Format(reference.ISOLayout)
	rows, err := loadDateSessionRows(queryer, profileID, trimmedDate+"T00:00:00Z", lookaheadEnd)
	if err != nil {
		return nil, err
	}
	return buildDateCommanderSessions(rows, trimmedDate+"T23:59:59Z"), nil
}

func loadDateSessionRows(queryer db.Queryer, profileID int64, windowStart, windowEnd string) ([]sessionTimelineRow, error) {
	rows, err := queryer.Query(`SELECT id, timestamp, event, COALESCE(system_name, ''), COALESCE(source_file, ''),
		CASE WHEN event = 'LoadGame' THEN raw_json ELSE '' END FROM journal_events
		WHERE event IN ('LoadGame', 'Location', 'FSDJump', 'CarrierJump') AND timestamp >= ? AND timestamp <= ? AND +profile_id = ?
		ORDER BY timestamp ASC, id ASC`, windowStart, windowEnd, profileID)
	if err != nil {
		return nil, fmt.Errorf("query date session events: %w", err)
	}
	defer rows.Close()
	var loaded []sessionTimelineRow
	for rows.Next() {
		var item sessionTimelineRow
		if err := rows.Scan(&item.id, &item.timestamp, &item.event, &item.systemName, &item.sourceFile, &item.rawJSON); err != nil {
			return nil, err
		}
		loaded = append(loaded, item)
	}
	return loaded, rows.Err()
}

func buildDateCommanderSessions(rows []sessionTimelineRow, dayEnd string) []CommanderSession {
	var loadIndices []int
	for index, item := range rows {
		if item.event == "LoadGame" {
			loadIndices = append(loadIndices, index)
		}
	}
	sessions := []CommanderSession{}
	for pos, rowIdx := range loadIndices {
		loadRow := rows[rowIdx]
		if loadRow.timestamp > dayEnd {
			break
		}
		prevIdx, nextIdx := -1, len(rows)
		if pos > 0 {
			prevIdx = loadIndices[pos-1]
		}
		if pos+1 < len(loadIndices) {
			nextIdx = loadIndices[pos+1]
		}
		var loadEvent journal.LoadGameEvent
		if json.Unmarshal([]byte(loadRow.rawJSON), &loadEvent) != nil {
			loadEvent = journal.LoadGameEvent{}
		}
		sessions = append(sessions, CommanderSession{
			Timestamp:  loadRow.timestamp,
			Galactic:   reference.ToGalactic(loadRow.timestamp),
			Ship:       reference.ShipDisplayName(loadEvent.Ship, loadEvent.ShipLocalised),
			SystemName: resolveLoadGameSystemName(rows, prevIdx, rowIdx, nextIdx),
		})
	}
	return sessions
}

func resolveLoadGameSystemName(rows []sessionTimelineRow, prevLoadIdx, loadIdx, nextLoadIdx int) string {
	loadRow := rows[loadIdx]
	fiveMinCutoff := ""
	if parsedTime, err := reference.ParseISO(loadRow.timestamp); err == nil {
		fiveMinCutoff = reference.FormatISO(parsedTime.Add(5 * time.Minute))
	}
	hasDistinctFile := loadRow.sourceFile != "" && !strings.EqualFold(loadRow.sourceFile, "EDDiscovery")
	for scanIdx := prevLoadIdx + 1; scanIdx < nextLoadIdx; scanIdx++ {
		candidate := rows[scanIdx]
		if candidate.event == "LoadGame" || strings.TrimSpace(candidate.systemName) == "" || candidate.timestamp < loadRow.timestamp {
			continue
		}
		if (hasDistinctFile && candidate.sourceFile == loadRow.sourceFile) || (fiveMinCutoff != "" && candidate.timestamp <= fiveMinCutoff) {
			return strings.TrimSpace(candidate.systemName)
		}
	}
	if hasDistinctFile {
		for scanIdx := loadIdx - 1; scanIdx > prevLoadIdx; scanIdx-- {
			if rows[scanIdx].sourceFile == loadRow.sourceFile && strings.TrimSpace(rows[scanIdx].systemName) != "" {
				return strings.TrimSpace(rows[scanIdx].systemName)
			}
		}
	}
	return ""
}
