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

type expeditionCommanderAnchor struct {
	expeditionID   int64
	oldProfileKey  string
	startTimestamp string
	anchorEventID  int64
}

type profileStartKey struct {
	profileID      int64
	startTimestamp string
}

func withReconciledCommanderOverrides(dbConn *sql.DB, mutate func(transaction *sql.Tx) error) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		anchors, err := snapshotExpeditionCommanders(transaction)
		if err != nil {
			return err
		}
		if err := mutate(transaction); err != nil {
			return err
		}
		if err := db.ResolveCommanders(transaction); err != nil {
			return err
		}
		return restoreExpeditionCommandersFromAnchors(transaction, anchors)
	})
}

func snapshotExpeditionCommanders(transaction *sql.Tx) ([]expeditionCommanderAnchor, error) {
	rows, err := transaction.Query(`
		SELECT ec.expedition_id, ec.profile_id, cp.profile_key, COALESCE(e.start_timestamp, '')
		FROM expedition_commanders ec
		JOIN commander_profiles cp ON cp.id = ec.profile_id
		JOIN expeditions e ON e.id = ec.expedition_id
		ORDER BY ec.expedition_id ASC, ec.profile_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("snapshot expedition_commanders: %w", err)
	}
	var anchors []expeditionCommanderAnchor
	var profileIDs []int64
	for rows.Next() {
		var item expeditionCommanderAnchor
		var profileID int64
		if scanErr := rows.Scan(&item.expeditionID, &profileID, &item.oldProfileKey, &item.startTimestamp); scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		anchors = append(anchors, item)
		profileIDs = append(profileIDs, profileID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil || len(anchors) == 0 {
		return nil, err
	}

	memoizedAnchors := make(map[profileStartKey]int64, len(anchors))
	for index := range anchors {
		lookupKey := profileStartKey{profileID: profileIDs[index], startTimestamp: anchors[index].startTimestamp}
		eventID, cached := memoizedAnchors[lookupKey]
		if !cached {
			if eventID, err = resolveProfileAnchorEventID(transaction, lookupKey.profileID, lookupKey.startTimestamp); err != nil {
				return nil, err
			}
			memoizedAnchors[lookupKey] = eventID
		}
		anchors[index].anchorEventID = eventID
	}
	return anchors, nil
}

func resolveProfileAnchorEventID(transaction *sql.Tx, profileID int64, startTimestamp string) (int64, error) {
	var anchorEventID int64
	if startTimestamp != "" {
		err := transaction.QueryRow(
			`SELECT id FROM journal_events WHERE +profile_id = ? AND timestamp >= ? ORDER BY timestamp ASC, id ASC LIMIT 1`,
			profileID, startTimestamp,
		).Scan(&anchorEventID)
		if err == nil {
			return anchorEventID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	err := transaction.QueryRow(
		`SELECT id FROM journal_events WHERE +profile_id = ? ORDER BY timestamp DESC, id DESC LIMIT 1`,
		profileID,
	).Scan(&anchorEventID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return anchorEventID, err
}

func restoreExpeditionCommandersFromAnchors(transaction *sql.Tx, anchors []expeditionCommanderAnchor) error {
	if len(anchors) == 0 {
		return nil
	}
	expeditionOrder := make([]int64, 0, len(anchors))
	resolvedByExpedition := make(map[int64][]int64, len(anchors))
	for _, anchor := range anchors {
		if _, seen := resolvedByExpedition[anchor.expeditionID]; !seen {
			expeditionOrder = append(expeditionOrder, anchor.expeditionID)
		}
		newProfileID, err := lookupAnchorTargetProfileID(transaction, anchor)
		if err != nil {
			return err
		}
		if newProfileID > 0 {
			resolvedByExpedition[anchor.expeditionID] = append(resolvedByExpedition[anchor.expeditionID], newProfileID)
		}
	}
	for _, expeditionID := range expeditionOrder {
		profileIDs := resolvedByExpedition[expeditionID]
		if len(profileIDs) == 0 {
			continue
		}
		if _, err := transaction.Exec(`DELETE FROM expedition_commanders WHERE expedition_id = ?`, expeditionID); err != nil {
			return err
		}
		for _, profileID := range profileIDs {
			if _, err := transaction.Exec(`INSERT OR IGNORE INTO expedition_commanders (expedition_id, profile_id) VALUES (?, ?)`, expeditionID, profileID); err != nil {
				return err
			}
		}
	}
	return nil
}

func lookupAnchorTargetProfileID(transaction *sql.Tx, anchor expeditionCommanderAnchor) (int64, error) {
	var newProfileID int64
	if anchor.anchorEventID > 0 {
		err := transaction.QueryRow(`SELECT profile_id FROM journal_events WHERE id = ?`, anchor.anchorEventID).Scan(&newProfileID)
		if err == nil || errors.Is(err, sql.ErrNoRows) {
			return newProfileID, nil
		}
		return 0, err
	}
	err := transaction.QueryRow(`SELECT id FROM commander_profiles WHERE LOWER(profile_key) = LOWER(?)`, anchor.oldProfileKey).Scan(&newProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return newProfileID, err
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
