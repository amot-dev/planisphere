package db

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

type groupedAliasRow struct {
	profileID                    int64
	rawName, fid, startTS, endTS string
	count                        int
}

type existingProfileState struct {
	enabled    int64
	isArchived bool
}

type segmentQueryClause struct {
	whereSQL string
	args     []any
}

type profileTimeBounds struct {
	startTS string
	endTS   string
}

func asciiLower(input string) string {
	hasUpper := false
	for index := 0; index < len(input); index++ {
		if input[index] >= 'A' && input[index] <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return input
	}
	lowered := []byte(input)
	for index := range lowered {
		if lowered[index] >= 'A' && lowered[index] <= 'Z' {
			lowered[index] += 'a' - 'A'
		}
	}
	return string(lowered)
}

func loadExistingAndLegacyEnabled(transaction *sql.Tx) (map[string]existingProfileState, map[string]int64, bool, error) {
	profileRows, err := transaction.Query(`SELECT profile_key, enabled, is_archived_career FROM commander_profiles`)
	if err != nil {
		return nil, nil, false, fmt.Errorf("query existing commander_profiles: %w", err)
	}
	defer profileRows.Close()
	existing := make(map[string]existingProfileState, 8)
	for profileRows.Next() {
		var key string
		var enabled, archivedInt int64
		if err := profileRows.Scan(&key, &enabled, &archivedInt); err != nil {
			return nil, nil, false, err
		}
		existing[key] = existingProfileState{enabled: enabled, isArchived: archivedInt != 0}
	}
	if err := profileRows.Err(); err != nil {
		return nil, nil, false, err
	}

	legacyRows, err := transaction.Query(`SELECT name, enabled FROM commanders`)
	if err != nil {
		return nil, nil, false, fmt.Errorf("query legacy commanders: %w", err)
	}
	defer legacyRows.Close()
	legacy := make(map[string]int64, 8)
	for legacyRows.Next() {
		var name string
		var enabled int64
		if err := legacyRows.Scan(&name, &enabled); err != nil {
			return nil, nil, false, err
		}
		legacy[asciiLower(strings.TrimSpace(name))] = enabled
	}
	return existing, legacy, len(existing) == 0, legacyRows.Err()
}

func loadCommanderOverrides(transaction *sql.Tx) ([]commanderOverrideRule, error) {
	rows, err := transaction.Query(`SELECT rule_type, source_key, target_key, split_timestamp FROM commander_overrides ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query commander_overrides: %w", err)
	}
	defer rows.Close()
	var rules []commanderOverrideRule
	for rows.Next() {
		var rule commanderOverrideRule
		if err := rows.Scan(&rule.ruleType, &rule.sourceKey, &rule.targetKey, &rule.splitTimestamp); err != nil {
			return nil, err
		}
		rule.sourceKey = asciiLower(strings.TrimSpace(rule.sourceKey))
		rule.targetKey = asciiLower(strings.TrimSpace(rule.targetKey))
		rule.splitTimestamp = strings.TrimSpace(rule.splitTimestamp)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func loadSightingsAndCheckpoints(transaction *sql.Tx) ([]resolverSighting, []resolverCheckpoint, error) {
	sightRows, err := transaction.Query(`
		SELECT LOWER(COALESCE(commander, '')), COALESCE(fid, ''), universe, MIN(timestamp)
		FROM journal_events
		WHERE (commander IS NOT NULL AND commander != '' COLLATE NOCASE)
		   OR fid != ''
		GROUP BY commander COLLATE NOCASE, fid, universe`)
	if err != nil {
		return nil, nil, fmt.Errorf("query commander sightings: %w", err)
	}
	defer sightRows.Close()
	var sightings []resolverSighting
	for sightRows.Next() {
		var nocaseName, fid, universe, firstSeen string
		if err := sightRows.Scan(&nocaseName, &fid, &universe, &firstSeen); err != nil {
			return nil, nil, err
		}
		trimmedName := strings.TrimSpace(nocaseName)
		trimmedFID := strings.TrimSpace(fid)
		if trimmedName == "" {
			nocaseName = ""
		}
		if nocaseName != "" || trimmedFID != "" {
			normalizedUniverse := asciiLower(strings.TrimSpace(universe))
			if normalizedUniverse == "" {
				normalizedUniverse = "live"
			}
			sightings = append(sightings, resolverSighting{
				nocaseName: nocaseName, rawFID: trimmedFID, effectiveFID: trimmedFID,
				universe: normalizedUniverse, firstSeen: firstSeen,
			})
		}
	}
	if err := sightRows.Err(); err != nil {
		return nil, nil, err
	}
	checkpoints, err := loadResolverCheckpoints(transaction)
	return sightings, checkpoints, err
}

func loadResolverCheckpoints(transaction *sql.Tx) ([]resolverCheckpoint, error) {
	rows, err := transaction.Query(`
		SELECT id, timestamp, event, LOWER(COALESCE(commander, '')), fid, universe, COALESCE(source_file, ''),
		       CASE WHEN event = 'Statistics' THEN raw_json ELSE '' END
		FROM journal_events WHERE event IN ('Commander', 'LoadGame', 'ClearSavedGame', 'NewCommander', 'Statistics')`)
	if err != nil {
		return nil, fmt.Errorf("query commander checkpoints: %w", err)
	}
	defer rows.Close()
	var checkpoints []resolverCheckpoint
	for rows.Next() {
		var checkpoint resolverCheckpoint
		if err := rows.Scan(&checkpoint.id, &checkpoint.timestamp, &checkpoint.event, &checkpoint.nocaseName, &checkpoint.rawFID, &checkpoint.universe, &checkpoint.sourceFile, &checkpoint.rawJSON); err != nil {
			return nil, err
		}
		checkpoint.rawFID = strings.TrimSpace(checkpoint.rawFID)
		checkpoint.effectiveFID = checkpoint.rawFID
		checkpoint.universe = asciiLower(strings.TrimSpace(checkpoint.universe))
		if checkpoint.universe == "" {
			checkpoint.universe = "live"
		}
		if strings.TrimSpace(checkpoint.nocaseName) != "" || checkpoint.rawFID != "" {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(checkpoints, func(left, right resolverCheckpoint) int {
		if left.timestamp != right.timestamp {
			return strings.Compare(left.timestamp, right.timestamp)
		}
		return int(left.id - right.id)
	})
	return checkpoints, nil
}

func appendSegmentBounds(baseSQL string, baseArgs []any, segment profileEventSegment) (string, []any) {
	args := make([]any, 0, len(baseArgs)+6)
	args = append(args, baseArgs...)
	if segment.hasStartBound {
		if segment.startBound.exclusiveAfterID > 0 {
			baseSQL += ` AND (timestamp > ? OR (timestamp = ? AND id > ?))`
			args = append(args, segment.startBound.timestamp, segment.startBound.timestamp, segment.startBound.exclusiveAfterID)
		} else {
			baseSQL += ` AND timestamp >= ?`
			args = append(args, segment.startBound.timestamp)
		}
	}
	if segment.hasEndBound {
		if segment.endBound.exclusiveAfterID > 0 {
			baseSQL += ` AND (timestamp < ? OR (timestamp = ? AND id <= ?))`
			args = append(args, segment.endBound.timestamp, segment.endBound.timestamp, segment.endBound.exclusiveAfterID)
		} else {
			baseSQL += ` AND timestamp < ?`
			args = append(args, segment.endBound.timestamp)
		}
	}
	return baseSQL, args
}

func buildSegmentClauses(segment profileEventSegment) []segmentQueryClause {
	clauses := make([]segmentQueryClause, 0, len(segment.tuples)+1)
	for _, tuple := range segment.tuples {
		if tuple.nocaseName != "" {
			whereSQL, args := appendSegmentBounds(`commander = ? COLLATE NOCASE AND fid = ? AND universe = ?`, []any{tuple.nocaseName, tuple.rawFID, tuple.universe}, segment)
			clauses = append(clauses, segmentQueryClause{whereSQL: whereSQL, args: args})
			continue
		}
		nullSQL, nullArgs := appendSegmentBounds(`commander IS NULL AND fid = ? AND universe = ?`, []any{tuple.rawFID, tuple.universe}, segment)
		emptySQL, emptyArgs := appendSegmentBounds(`commander = '' COLLATE NOCASE AND fid = ? AND universe = ?`, []any{tuple.rawFID, tuple.universe}, segment)
		clauses = append(clauses,
			segmentQueryClause{whereSQL: nullSQL, args: nullArgs},
			segmentQueryClause{whereSQL: emptySQL, args: emptyArgs},
		)
	}
	return clauses
}

func querySplitSegmentStats(transaction *sql.Tx, segment profileEventSegment) (int, string, error) {
	totalCount := 0
	earliestTimestamp := ""
	for _, clause := range buildSegmentClauses(segment) {
		var count int
		var minTimestamp string
		if err := transaction.QueryRow(`SELECT COUNT(*), COALESCE(MIN(timestamp), '') FROM journal_events WHERE `+clause.whereSQL, clause.args...).Scan(&count, &minTimestamp); err != nil {
			return 0, "", fmt.Errorf("query split era count: %w", err)
		}
		totalCount += count
		if minTimestamp != "" && (earliestTimestamp == "" || minTimestamp < earliestTimestamp) {
			earliestTimestamp = minTimestamp
		}
	}
	return totalCount, earliestTimestamp, nil
}

func persistProfileShellsAndEventIDs(transaction *sql.Tx, profiles []*resolvedCommanderProfile) error {
	slices.SortFunc(profiles, func(left, right *resolvedCommanderProfile) int {
		if left.firstSeen != right.firstSeen {
			return strings.Compare(left.firstSeen, right.firstSeen)
		}
		return strings.Compare(left.profileKey, right.profileKey)
	})
	placeholders, keepArgs := make([]string, len(profiles)), make([]any, len(profiles))
	for index, profile := range profiles {
		if err := transaction.QueryRow(`
			INSERT INTO commander_profiles (profile_key, display_name, fid, universe, era_index, is_archived_career, has_manual_override, enabled, first_seen, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(profile_key) DO UPDATE SET fid = excluded.fid, universe = excluded.universe, era_index = excluded.era_index,
				is_archived_career = excluded.is_archived_career, has_manual_override = excluded.has_manual_override
			RETURNING id`,
			profile.profileKey, profile.profileKey, profile.fid, profile.universe, profile.eraIndex,
			boolToInt(profile.isArchivedCareer), boolToInt(profile.hasManualOverride), profile.firstSeen, profile.lastSeen,
		).Scan(&profile.id); err != nil {
			return fmt.Errorf("upsert commander_profile %s: %w", profile.profileKey, err)
		}
		placeholders[index], keepArgs[index] = "?", profile.profileKey
	}
	if _, err := transaction.Exec(`DELETE FROM commander_profiles WHERE profile_key NOT IN (`+strings.Join(placeholders, ",")+`)`, keepArgs...); err != nil {
		return fmt.Errorf("prune stale commander_profiles: %w", err)
	}
	return assignSegmentEventProfileIDs(transaction, profiles)
}

func assignSegmentEventProfileIDs(transaction *sql.Tx, profiles []*resolvedCommanderProfile) error {
	for _, profile := range profiles {
		for _, segment := range profile.segments {
			for _, clause := range buildSegmentClauses(segment) {
				args := make([]any, 0, len(clause.args)+2)
				args = append(args, profile.id)
				args = append(args, clause.args...)
				args = append(args, profile.id)
				if _, err := transaction.Exec(`UPDATE journal_events SET profile_id = ? WHERE `+clause.whereSQL+` AND profile_id != ?`, args...); err != nil {
					return fmt.Errorf("update journal_events.profile_id for %s: %w", profile.profileKey, err)
				}
			}
		}
	}
	if _, err := transaction.Exec(`UPDATE journal_events SET profile_id = 0 WHERE commander IS NULL AND (fid IS NULL OR fid = '') AND profile_id != 0`); err != nil {
		return err
	}
	_, err := transaction.Exec(`UPDATE journal_events SET profile_id = 0 WHERE commander = '' COLLATE NOCASE AND (fid IS NULL OR fid = '') AND profile_id != 0`)
	return err
}

func finalizeProfilesAndAliases(transaction *sql.Tx, profiles []*resolvedCommanderProfile, existing map[string]existingProfileState, legacy map[string]int64, profilesWereEmpty bool) error {
	if _, err := transaction.Exec(`DELETE FROM commander_aliases`); err != nil {
		return fmt.Errorf("clear commander_aliases: %w", err)
	}
	rows, err := transaction.Query(`
		SELECT profile_id,
		       CASE WHEN commander IS NOT NULL AND commander != '' COLLATE NOCASE THEN MAX(timestamp || char(0) || commander) ELSE '' END,
		       MAX(CASE WHEN fid != '' THEN fid ELSE '' END), MIN(timestamp), MAX(timestamp), COUNT(*)
		FROM journal_events
		WHERE profile_id > 0 AND ((commander IS NOT NULL AND commander != '' COLLATE NOCASE) OR fid != '')
		GROUP BY profile_id, commander COLLATE NOCASE`)
	if err != nil {
		return fmt.Errorf("query commander_aliases groups: %w", err)
	}
	aliasesByProfile := make(map[int64][]groupedAliasRow, len(profiles))
	boundsByProfile := make(map[int64]profileTimeBounds, len(profiles))
	for rows.Next() {
		var item groupedAliasRow
		var encoded string
		if err := rows.Scan(&item.profileID, &encoded, &item.fid, &item.startTS, &item.endTS, &item.count); err != nil {
			rows.Close()
			return err
		}
		bounds := boundsByProfile[item.profileID]
		if bounds.startTS == "" || item.startTS < bounds.startTS {
			bounds.startTS = item.startTS
		}
		if item.endTS > bounds.endTS {
			bounds.endTS = item.endTS
		}
		boundsByProfile[item.profileID] = bounds
		if item.rawName = decodeTimestampPrefixedString(encoded); item.rawName != "" {
			aliasesByProfile[item.profileID] = append(aliasesByProfile[item.profileID], item)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, profile := range profiles {
		if err := writeFinalizedProfileAndAliases(transaction, profile, aliasesByProfile[profile.id], boundsByProfile[profile.id], existing, legacy, profilesWereEmpty); err != nil {
			return err
		}
	}
	return nil
}

func collectManualMergedNames(profile *resolvedCommanderProfile) map[string]bool {
	native, manual := make(map[string]bool), make(map[string]bool)
	for _, segment := range profile.segments {
		if !segment.fromManualMerge {
			for _, tuple := range segment.tuples {
				native[asciiLower(strings.TrimSpace(tuple.nocaseName))] = true
			}
		}
	}
	for _, segment := range profile.segments {
		if segment.fromManualMerge {
			for _, tuple := range segment.tuples {
				folded := asciiLower(strings.TrimSpace(tuple.nocaseName))
				if !native[folded] {
					manual[folded] = true
				}
			}
		}
	}
	return manual
}

func writeFinalizedProfileAndAliases(transaction *sql.Tx, profile *resolvedCommanderProfile, aliases []groupedAliasRow, bounds profileTimeBounds, existing map[string]existingProfileState, legacy map[string]int64, profilesWereEmpty bool) error {
	manualNames := collectManualMergedNames(profile)
	disabledInLegacy := false
	for _, alias := range aliases {
		if profile.firstSeen == "" || alias.startTS < profile.firstSeen {
			profile.firstSeen = alias.startTS
		}
		if profile.displayName == "" || alias.endTS > profile.lastSeen || (alias.endTS == profile.lastSeen && alias.rawName > profile.displayName) {
			profile.lastSeen, profile.displayName = alias.endTS, alias.rawName
		}
		if profilesWereEmpty {
			if flag, ok := legacy[asciiLower(strings.TrimSpace(alias.rawName))]; ok && flag == 0 {
				disabledInLegacy = true
			}
		}
	}
	if bounds.startTS != "" && (profile.firstSeen == "" || bounds.startTS < profile.firstSeen) {
		profile.firstSeen = bounds.startTS
	}
	if bounds.endTS > profile.lastSeen {
		profile.lastSeen = bounds.endTS
	}
	if existingState, ok := existing[profile.profileKey]; ok && existingState.isArchived == profile.isArchivedCareer {
		profile.enabled = existingState.enabled
	} else if disabledInLegacy || profile.universe == "beta" || profile.isArchivedCareer {
		profile.enabled = 0
	} else {
		profile.enabled = 1
	}
	if _, err := transaction.Exec(`UPDATE commander_profiles SET display_name = ?, enabled = ?, first_seen = ?, last_seen = ? WHERE id = ?`,
		profile.displayName, profile.enabled, profile.firstSeen, profile.lastSeen, profile.id); err != nil {
		return err
	}
	if err := upsertLegacyCommanderFromProfile(transaction, profile, legacy); err != nil {
		return err
	}
	return insertProfileAliasRows(transaction, profile, aliases, manualNames)
}

func upsertLegacyCommanderFromProfile(transaction *sql.Tx, profile *resolvedCommanderProfile, legacy map[string]int64) error {
	if profile.displayName == "" {
		return nil
	}
	_, existedInLegacy := legacy[asciiLower(strings.TrimSpace(profile.displayName))]
	_, err := transaction.Exec(`
		INSERT INTO commanders(name, fid, enabled, first_seen, last_seen) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			fid = CASE WHEN commanders.fid = '' AND excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
			enabled = CASE WHEN ? = 0 THEN MAX(commanders.enabled, excluded.enabled) ELSE commanders.enabled END,
			first_seen = CASE WHEN commanders.first_seen IS NULL OR commanders.first_seen = '' OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
			last_seen = CASE WHEN commanders.last_seen IS NULL OR commanders.last_seen = '' OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`,
		profile.displayName, profile.fid, profile.enabled, profile.firstSeen, profile.lastSeen, boolToInt(existedInLegacy))
	return err
}

func insertProfileAliasRows(transaction *sql.Tx, profile *resolvedCommanderProfile, aliases []groupedAliasRow, manualNames map[string]bool) error {
	lowerDisplay := asciiLower(strings.TrimSpace(profile.displayName))
	for _, alias := range aliases {
		aliasFID, source := alias.fid, "auto"
		if aliasFID == "" {
			aliasFID = profile.fid
		}
		lowerRaw := asciiLower(strings.TrimSpace(alias.rawName))
		if manualNames[lowerRaw] {
			source = "manual"
		}
		if _, err := transaction.Exec(`INSERT INTO commander_aliases (profile_id, raw_name, fid, universe, start_timestamp, end_timestamp, event_count, is_current, source) VALUES (?,?,?,?,?,?,?,?,?)`,
			profile.id, alias.rawName, aliasFID, profile.universe, alias.startTS, alias.endTS, alias.count, boolToInt(lowerRaw == lowerDisplay), source); err != nil {
			return err
		}
	}
	return nil
}

func decodeTimestampPrefixedString(encoded string) string {
	if sep := strings.IndexByte(encoded, 0); sep >= 0 {
		return strings.TrimSpace(encoded[sep+1:])
	}
	return strings.TrimSpace(encoded)
}

func boolToInt(flag bool) int {
	if flag {
		return 1
	}
	return 0
}
