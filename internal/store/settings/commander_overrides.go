package settings

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/reference"
)

// MergeCommanders merges sourceProfileKey into targetProfileKey and re-runs
// deterministic commander profile resolution inside a single transaction.
func MergeCommanders(dbConn *sql.DB, sourceProfileKey, targetProfileKey string) error {
	return withReconciledCommanderOverrides(dbConn, func(transaction *sql.Tx) error {
		sourceProfile, err := lookupCommanderProfile(transaction, sourceProfileKey)
		if err != nil {
			return err
		}
		targetProfile, err := lookupCommanderProfile(transaction, targetProfileKey)
		if err != nil {
			return err
		}
		if strings.EqualFold(sourceProfile.profileKey, targetProfile.profileKey) {
			return errors.New("cannot merge a commander profile into itself")
		}
		if _, err := transaction.Exec(
			`DELETE FROM commander_overrides WHERE rule_type = 'merge' AND LOWER(source_key) = LOWER(?) AND LOWER(target_key) = LOWER(?)`,
			targetProfile.profileKey, sourceProfile.profileKey,
		); err != nil {
			return err
		}
		return applyMergeOverrideMutation(transaction, sourceProfile, targetProfile)
	})
}

func applyMergeOverrideMutation(transaction *sql.Tx, sourceProfile, targetProfile storedCommanderProfile) error {
	sourceBase := stripProfileEraSuffix(sourceProfile.profileKey)
	targetBase := stripProfileEraSuffix(targetProfile.profileKey)
	if cleared, err := tryClearMatchingUnmerge(transaction, sourceBase, targetBase, targetProfile.profileKey); err != nil || cleared {
		return err
	}
	if cleared, err := tryClearMatchingUnmerge(transaction, targetBase, sourceBase, sourceProfile.profileKey); err != nil || cleared {
		return err
	}
	now := reference.NowISO()
	if sourceBase == targetBase {
		deletedSplits, err := deleteSeparatingManualSplits(transaction, sourceBase, sourceProfile, targetProfile)
		if err != nil || deletedSplits > 0 {
			return err
		}
		if err := insertOverrideIfMissing(transaction, "merge", sourceBase, sourceBase, "", now); err != nil {
			return err
		}
	}
	if _, err := transaction.Exec(`DELETE FROM commander_overrides WHERE rule_type = 'merge' AND LOWER(source_key) = LOWER(?)`, sourceProfile.profileKey); err != nil {
		return err
	}
	return insertOverrideIfMissing(transaction, "merge", sourceProfile.profileKey, targetProfile.profileKey, "", now)
}

func tryClearMatchingUnmerge(transaction *sql.Tx, candidateBase, groupBase, groupProfileKey string) (bool, error) {
	unmergeKey, unmergeGroup, ok := parseUnmergedBaseKey(candidateBase)
	if !ok || unmergeGroup != groupBase || !strings.EqualFold(groupProfileKey, groupBase+":era:0") {
		return false, nil
	}
	var eraCount int
	if err := transaction.QueryRow(`SELECT COUNT(*) FROM commander_profiles WHERE LOWER(profile_key) LIKE LOWER(?) || ':era:%'`, groupBase).Scan(&eraCount); err != nil {
		return false, err
	}
	if eraCount != 1 {
		return false, nil
	}
	_, err := transaction.Exec(`DELETE FROM commander_overrides WHERE rule_type = 'unmerge' AND LOWER(source_key) = LOWER(?)`, unmergeKey)
	return err == nil, err
}

func deleteSeparatingManualSplits(transaction *sql.Tx, baseKey string, sourceProfile, targetProfile storedCommanderProfile) (int64, error) {
	earlierLast, laterFirst := sourceProfile.lastSeen, targetProfile.firstSeen
	if targetProfile.firstSeen != "" && (sourceProfile.firstSeen == "" || targetProfile.firstSeen < sourceProfile.firstSeen) {
		earlierLast, laterFirst = targetProfile.lastSeen, sourceProfile.firstSeen
	}
	if earlierLast != "" && laterFirst != "" && earlierLast < laterFirst {
		return execRowsAffected(transaction,
			`DELETE FROM commander_overrides WHERE rule_type = 'split' AND (LOWER(source_key) = LOWER(?) OR LOWER(source_key) LIKE LOWER(?) || ':era:%') AND split_timestamp > ? AND split_timestamp <= ?`,
			baseKey, baseKey, earlierLast, laterFirst)
	}
	return execRowsAffected(transaction,
		`DELETE FROM commander_overrides WHERE rule_type = 'split' AND (LOWER(source_key) = LOWER(?) OR LOWER(source_key) LIKE LOWER(?) || ':era:%')`,
		baseKey, baseKey)
}

// UnmergeCommanderAlias detaches aliasName from profileKey and re-runs
// deterministic commander profile resolution.
func UnmergeCommanderAlias(dbConn *sql.DB, aliasName, profileKey string) error {
	trimmedAlias := strings.TrimSpace(aliasName)
	if trimmedAlias == "" {
		return errors.New("commander alias name is required")
	}
	return withReconciledCommanderOverrides(dbConn, func(transaction *sql.Tx) error {
		profile, err := lookupCommanderProfile(transaction, profileKey)
		if err != nil {
			return err
		}
		aliases, matchedAlias, err := loadProfileAliasesForUnmerge(transaction, profile.id, trimmedAlias)
		if err != nil {
			return err
		}
		if len(aliases) <= 1 && profile.hasManualOverride == 0 {
			return fmt.Errorf("commander profile %q has no merged aliases to unmerge", profile.profileKey)
		}
		deletedMerges, err := deleteAliasMergeOverrides(transaction, profile, matchedAlias, len(aliases))
		if err != nil {
			return err
		}
		insertedUnmerge, err := maybeInsertAliasUnmergeRule(transaction, profile, aliases, matchedAlias, deletedMerges)
		if err != nil {
			return err
		}
		if deletedMerges == 0 && !insertedUnmerge {
			return fmt.Errorf("commander profile %q has no merged aliases to unmerge", profile.profileKey)
		}
		return nil
	})
}

func deleteAliasMergeOverrides(transaction *sql.Tx, profile storedCommanderProfile, alias storedCommanderAlias, aliasCount int) (int, error) {
	merges, err := loadAllMergeOverrides(transaction)
	if err != nil {
		return 0, err
	}
	reachableIDs := collectMergeRuleIDsReachingProfile(merges, profile.profileKey)
	nocaseAlias := strings.ToLower(strings.TrimSpace(alias.rawName))
	universe := normalizeOverrideUniverse(alias.universe, profile.universe)
	lowerAliasFID, lowerProfileFID := strings.ToLower(strings.TrimSpace(alias.fid)), strings.ToLower(strings.TrimSpace(profile.fid))
	profileBase := stripProfileEraSuffix(profile.profileKey)

	deleted := 0
	for _, rule := range merges {
		if !reachableIDs[rule.id] {
			continue
		}
		sourceBase := stripProfileEraSuffix(rule.sourceKey)
		if sourceBase == universe+":name:"+nocaseAlias || strings.HasSuffix(sourceBase, ":"+nocaseAlias) ||
			(lowerAliasFID != "" && lowerAliasFID != lowerProfileFID && sourceBase == universe+":"+lowerAliasFID) ||
			(sourceBase == profileBase && (alias.source == "manual" || aliasCount == 1)) {
			if _, err := transaction.Exec(`DELETE FROM commander_overrides WHERE id = ?`, rule.id); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}

func maybeInsertAliasUnmergeRule(
	transaction *sql.Tx,
	profile storedCommanderProfile,
	aliases []storedCommanderAlias,
	alias storedCommanderAlias,
	deletedMerges int,
) (bool, error) {
	if len(aliases) <= 1 {
		return false, nil
	}
	effectiveFID := effectiveAliasFID(alias, profile)
	if effectiveFID == "" || !hasPeerAliasSharingFID(aliases, alias.rawName, effectiveFID, profile) {
		return false, nil
	}
	if deletedMerges > 0 {
		var sharesFID int
		if err := transaction.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM journal_events WHERE commander = ? COLLATE NOCASE AND LOWER(fid) = LOWER(?) AND fid != '')`,
			alias.rawName, effectiveFID,
		).Scan(&sharesFID); err != nil || sharesFID == 0 {
			return false, err
		}
	}
	unmergeSource := normalizeOverrideUniverse(alias.universe, profile.universe) + ":" + effectiveFID + ":" + strings.ToLower(strings.TrimSpace(alias.rawName))
	if err := insertOverrideIfMissing(transaction, "unmerge", unmergeSource, "", "", reference.NowISO()); err != nil {
		return false, err
	}
	return true, nil
}

func effectiveAliasFID(alias storedCommanderAlias, profile storedCommanderProfile) string {
	if lowered := strings.ToLower(strings.TrimSpace(alias.fid)); lowered != "" {
		return lowered
	}
	return strings.ToLower(strings.TrimSpace(profile.fid))
}

func hasPeerAliasSharingFID(aliases []storedCommanderAlias, targetRawName, effectiveFID string, profile storedCommanderProfile) bool {
	for _, other := range aliases {
		if !strings.EqualFold(strings.TrimSpace(other.rawName), strings.TrimSpace(targetRawName)) &&
			other.source == "auto" && effectiveAliasFID(other, profile) == effectiveFID {
			return true
		}
	}
	return false
}

// SplitCommander records a manual career split boundary at splitTimestamp on
// profileKey's base group and re-runs deterministic commander resolution.
func SplitCommander(dbConn *sql.DB, profileKey, splitTimestamp string) error {
	candidateTimestamp := strings.TrimSpace(splitTimestamp)
	if len(candidateTimestamp) == 10 {
		candidateTimestamp += "T00:00:00Z"
	}
	normalizedTimestamp, parseErr := reference.NormalizeISO(candidateTimestamp)
	if parseErr != nil {
		return fmt.Errorf("invalid split timestamp %q: %w", splitTimestamp, parseErr)
	}
	return withReconciledCommanderOverrides(dbConn, func(transaction *sql.Tx) error {
		profile, err := lookupCommanderProfile(transaction, profileKey)
		if err != nil {
			return err
		}
		return insertOverrideIfMissing(transaction, "split", stripProfileEraSuffix(profile.profileKey), "", normalizedTimestamp, reference.NowISO())
	})
}

// ResetCommanderOverrides clears all manual merge, split, and unmerge overrides
// associated with profileKey and re-runs deterministic commander resolution.
func ResetCommanderOverrides(dbConn *sql.DB, profileKey string) error {
	return withReconciledCommanderOverrides(dbConn, func(transaction *sql.Tx) error {
		profile, err := lookupCommanderProfile(transaction, profileKey)
		if err != nil {
			return err
		}
		merges, err := loadAllMergeOverrides(transaction)
		if err != nil {
			return err
		}
		for ruleID := range collectMergeRuleIDsReachingProfile(merges, profile.profileKey) {
			if _, err := transaction.Exec(`DELETE FROM commander_overrides WHERE id = ?`, ruleID); err != nil {
				return err
			}
		}
		baseKey := stripProfileEraSuffix(profile.profileKey)
		eraPattern := baseKey + ":era:%"
		if _, err := transaction.Exec(
			`DELETE FROM commander_overrides WHERE LOWER(source_key) IN (LOWER(?), LOWER(?)) OR LOWER(target_key) IN (LOWER(?), LOWER(?))
			 OR LOWER(source_key) LIKE LOWER(?) OR LOWER(target_key) LIKE LOWER(?) OR (rule_type = 'unmerge' AND LOWER(source_key) LIKE LOWER(?) || ':%')`,
			profile.profileKey, baseKey, profile.profileKey, baseKey, eraPattern, eraPattern, baseKey,
		); err != nil {
			return err
		}
		if unmergeKey, unmergeGroup, ok := parseUnmergedBaseKey(baseKey); ok {
			_, err := transaction.Exec(`DELETE FROM commander_overrides WHERE LOWER(source_key) = LOWER(?) OR LOWER(target_key) = LOWER(?)`, unmergeKey, unmergeGroup)
			return err
		}
		return nil
	})
}
