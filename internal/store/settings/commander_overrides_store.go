package settings

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
)

type storedCommanderProfile struct {
	id, hasManualOverride                          int64
	profileKey, fid, universe, firstSeen, lastSeen string
}

type storedCommanderAlias struct{ rawName, fid, universe, source string }

type storedMergeOverride struct {
	id                   int64
	sourceKey, targetKey string
}

func withReconciledCommanderOverrides(dbConn *sql.DB, mutate func(transaction *sql.Tx) error) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if err := mutate(transaction); err != nil {
			return err
		}
		return db.ResolveCommanders(transaction)
	})
}

func lookupCommanderProfile(transaction *sql.Tx, profileKey string) (storedCommanderProfile, error) {
	trimmed := strings.TrimSpace(profileKey)
	if trimmed == "" {
		return storedCommanderProfile{}, errors.New("commander profile key is required")
	}
	var profile storedCommanderProfile
	err := transaction.QueryRow(
		`SELECT id, profile_key, COALESCE(fid, ''), COALESCE(universe, 'live'), has_manual_override, COALESCE(first_seen, ''), COALESCE(last_seen, '')
		 FROM commander_profiles WHERE LOWER(profile_key) = LOWER(?)`, trimmed,
	).Scan(&profile.id, &profile.profileKey, &profile.fid, &profile.universe, &profile.hasManualOverride, &profile.firstSeen, &profile.lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return storedCommanderProfile{}, fmt.Errorf("commander profile %q not found", trimmed)
	}
	return profile, err
}

func loadProfileAliasesForUnmerge(transaction *sql.Tx, profileID int64, targetAlias string) ([]storedCommanderAlias, storedCommanderAlias, error) {
	rows, err := transaction.Query(`SELECT raw_name, COALESCE(fid, ''), COALESCE(universe, 'live'), COALESCE(source, 'auto') FROM commander_aliases WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, storedCommanderAlias{}, err
	}
	defer rows.Close()
	var aliases []storedCommanderAlias
	var matched storedCommanderAlias
	found := false
	for rows.Next() {
		var item storedCommanderAlias
		if err := rows.Scan(&item.rawName, &item.fid, &item.universe, &item.source); err != nil {
			return nil, storedCommanderAlias{}, err
		}
		aliases = append(aliases, item)
		if strings.EqualFold(strings.TrimSpace(item.rawName), targetAlias) {
			matched, found = item, true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, storedCommanderAlias{}, err
	}
	if !found {
		return nil, storedCommanderAlias{}, fmt.Errorf("alias %q not found on commander profile", targetAlias)
	}
	return aliases, matched, nil
}

func loadAllMergeOverrides(transaction *sql.Tx) ([]storedMergeOverride, error) {
	rows, err := transaction.Query(`SELECT id, source_key, target_key FROM commander_overrides WHERE rule_type = 'merge' ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var merges []storedMergeOverride
	for rows.Next() {
		var item storedMergeOverride
		if err := rows.Scan(&item.id, &item.sourceKey, &item.targetKey); err != nil {
			return nil, err
		}
		item.sourceKey, item.targetKey = strings.ToLower(strings.TrimSpace(item.sourceKey)), strings.ToLower(strings.TrimSpace(item.targetKey))
		merges = append(merges, item)
	}
	return merges, rows.Err()
}

func collectMergeRuleIDsReachingProfile(merges []storedMergeOverride, profileKey string) map[int64]bool {
	lowerKey := strings.ToLower(strings.TrimSpace(profileKey))
	accepted := map[string]bool{lowerKey: true, stripProfileEraSuffix(lowerKey): true}
	matched := make(map[int64]bool)
	for changed := true; changed; {
		changed = false
		for _, rule := range merges {
			if !matched[rule.id] && (accepted[rule.targetKey] || accepted[rule.targetKey+":era:0"]) {
				matched[rule.id] = true
				accepted[rule.sourceKey], accepted[stripProfileEraSuffix(rule.sourceKey)] = true, true
				changed = true
			}
		}
	}
	return matched
}

func insertOverrideIfMissing(transaction *sql.Tx, ruleType, sourceKey, targetKey, splitTimestamp, createdAt string) error {
	normalizedSourceKey := strings.ToLower(strings.TrimSpace(sourceKey))
	normalizedTargetKey := strings.ToLower(strings.TrimSpace(targetKey))
	_, err := transaction.Exec(
		`INSERT INTO commander_overrides (rule_type, source_key, target_key, split_timestamp, created_at)
		 SELECT ?, ?, ?, ?, ? WHERE NOT EXISTS (
			SELECT 1 FROM commander_overrides WHERE rule_type = ? AND LOWER(source_key) = LOWER(?) AND LOWER(target_key) = LOWER(?) AND split_timestamp = ?
		 )`,
		ruleType, normalizedSourceKey, normalizedTargetKey, splitTimestamp, createdAt,
		ruleType, normalizedSourceKey, normalizedTargetKey, splitTimestamp,
	)
	return err
}

func stripProfileEraSuffix(profileKey string) string {
	trimmed := strings.ToLower(strings.TrimSpace(profileKey))
	eraIdx := strings.LastIndex(trimmed, ":era:")
	if eraIdx <= 0 || eraIdx+len(":era:") >= len(trimmed) {
		return trimmed
	}
	for index := eraIdx + len(":era:"); index < len(trimmed); index++ {
		if trimmed[index] < '0' || trimmed[index] > '9' {
			return trimmed
		}
	}
	return trimmed[:eraIdx]
}

func parseUnmergedBaseKey(baseKey string) (string, string, bool) {
	if !strings.HasPrefix(baseKey, "unmerged:") {
		return "", "", false
	}
	unmergeKey := strings.TrimPrefix(baseKey, "unmerged:")
	parts := strings.SplitN(unmergeKey, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return unmergeKey, parts[0] + ":" + parts[1], true
}

func normalizeOverrideUniverse(aliasUniverse, profileUniverse string) string {
	if normalized := strings.ToLower(strings.TrimSpace(aliasUniverse)); normalized != "" {
		return normalized
	}
	if normalized := strings.ToLower(strings.TrimSpace(profileUniverse)); normalized != "" {
		return normalized
	}
	return "live"
}
