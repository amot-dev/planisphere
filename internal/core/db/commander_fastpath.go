package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// SqliteNocaseKey folds ASCII uppercase bytes to lowercase to match SQLite's
// built-in COLLATE NOCASE comparison byte-for-byte.
func SqliteNocaseKey(value string) string { return asciiLower(value) }

// LiveAppendBatch summarises newly inserted journal_events rows in a single-file
// tailing transaction so eligible tip appends avoid full-archive scans.
type LiveAppendBatch struct {
	InsertedIDs       []int64
	Commander         string
	FID               string
	Universe          string
	EarliestTimestamp string
	LatestTimestamp   string
	Eligible          bool
}

// ObserveInserted records a newly inserted journal_events row and tracks whether
// all inserted rows in the batch remain eligible for O(added) profile attribution.
func (batch *LiveAppendBatch) ObserveInserted(eventID int64, eventName, commander, fid, universe, timestamp string) {
	trimmedCommander := strings.TrimSpace(commander)
	trimmedFID := strings.TrimSpace(fid)
	normalizedUniverse := asciiLower(strings.TrimSpace(universe))
	if normalizedUniverse == "" {
		normalizedUniverse = "live"
	}
	rowAllowed := eventID > 0 && trimmedCommander != "" && timestamp != "" &&
		eventName != "ClearSavedGame" && eventName != "NewCommander" && eventName != "Statistics"
	if len(batch.InsertedIDs) == 0 {
		batch.InsertedIDs = append(batch.InsertedIDs, eventID)
		batch.Commander, batch.FID, batch.Universe = trimmedCommander, trimmedFID, normalizedUniverse
		batch.EarliestTimestamp, batch.LatestTimestamp = timestamp, timestamp
		batch.Eligible = rowAllowed
		return
	}
	batch.InsertedIDs = append(batch.InsertedIDs, eventID)
	if !batch.Eligible || !rowAllowed ||
		SqliteNocaseKey(trimmedCommander) != SqliteNocaseKey(batch.Commander) ||
		trimmedFID != batch.FID || normalizedUniverse != batch.Universe {
		batch.Eligible = false
		return
	}
	if timestamp < batch.EarliestTimestamp {
		batch.EarliestTimestamp = timestamp
	}
	if timestamp >= batch.LatestTimestamp {
		batch.LatestTimestamp = timestamp
		batch.Commander = trimmedCommander
	}
}

// ResolveLiveAppendOrFull attributes batch in O(added) when commandersBackfilled is
// zero and all fast-path preconditions hold, falling back to ResolveCommandersChanged.
func ResolveLiveAppendOrFull(transaction *sql.Tx, batch LiveAppendBatch, commandersBackfilled int) (bool, error) {
	if commandersBackfilled == 0 {
		if matched, err := TryResolveLiveAppend(transaction, batch); err != nil || matched {
			return false, err
		}
	}
	return ResolveCommandersChanged(transaction)
}

type fastPathTargetProfile struct {
	id, enabled         int64
	firstSeen, lastSeen string
}

// TryResolveLiveAppend applies O(added) commander profile attribution when batch
// appends monotonically onto an existing current-era commander profile.
func TryResolveLiveAppend(transaction *sql.Tx, batch LiveAppendBatch) (bool, error) {
	targetProfile, matched, err := lookupFastPathProfile(transaction, batch)
	if err != nil || !matched {
		return false, err
	}
	if err := applyFastPathAppend(transaction, targetProfile, batch); err != nil {
		return false, err
	}
	return true, nil
}

func lookupFastPathProfile(transaction *sql.Tx, batch LiveAppendBatch) (fastPathTargetProfile, bool, error) {
	if !batch.Eligible || len(batch.InsertedIDs) == 0 || batch.Commander == "" || batch.EarliestTimestamp == "" {
		return fastPathTargetProfile{}, false, nil
	}
	nocaseCommander, lowerFID := SqliteNocaseKey(batch.Commander), asciiLower(batch.FID)
	baseKey := batch.Universe + ":name:" + nocaseCommander
	if lowerFID != "" {
		var isUnmerged bool
		unmergeKey := batch.Universe + ":" + lowerFID + ":" + nocaseCommander
		if err := transaction.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM commander_overrides WHERE rule_type = 'unmerge' AND LOWER(TRIM(source_key)) = ?)`,
			unmergeKey,
		).Scan(&isUnmerged); err != nil {
			return fastPathTargetProfile{}, false, fmt.Errorf("check commander unmerge override: %w", err)
		}
		if isUnmerged {
			baseKey = "unmerged:" + unmergeKey
		} else {
			baseKey = batch.Universe + ":" + lowerFID
		}
	}
	eraPrefix := baseKey + ":era:"
	rows, err := transaction.Query(`
		SELECT id, profile_key, display_name, fid, universe, era_index, is_archived_career, enabled, COALESCE(first_seen, ''), COALESCE(last_seen, '')
		FROM commander_profiles WHERE profile_key >= ? AND profile_key < ?`, eraPrefix, baseKey+":era;")
	if err != nil {
		return fastPathTargetProfile{}, false, fmt.Errorf("query fast-path commander_profiles: %w", err)
	}
	var candidate fastPathTargetProfile
	var candidateDisplay, candidateFID, candidateUniverse string
	activeCount, candidateEra, maxEra := 0, -1, -1
	for rows.Next() {
		var rowID, rowEnabled int64
		var rowEra, rowArchived int
		var rowKey, rowDisplay, rowFID, rowUniverse, rowFirstSeen, rowLastSeen string
		if scanErr := rows.Scan(&rowID, &rowKey, &rowDisplay, &rowFID, &rowUniverse, &rowEra, &rowArchived, &rowEnabled, &rowFirstSeen, &rowLastSeen); scanErr != nil {
			_ = rows.Close()
			return fastPathTargetProfile{}, false, scanErr
		}
		if !strings.HasPrefix(rowKey, eraPrefix) {
			continue
		}
		if rowEra > maxEra {
			maxEra = rowEra
		}
		if rowArchived == 0 {
			activeCount++
			candidate = fastPathTargetProfile{id: rowID, enabled: rowEnabled, firstSeen: rowFirstSeen, lastSeen: rowLastSeen}
			candidateDisplay, candidateFID, candidateUniverse, candidateEra = rowDisplay, rowFID, rowUniverse, rowEra
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fastPathTargetProfile{}, false, err
	}
	if err := rows.Close(); err != nil {
		return fastPathTargetProfile{}, false, err
	}
	if activeCount != 1 || candidateEra != maxEra ||
		candidateDisplay != batch.Commander || candidateFID != batch.FID || candidateUniverse != batch.Universe ||
		candidate.firstSeen == "" || candidate.lastSeen == "" || batch.EarliestTimestamp < candidate.lastSeen {
		return fastPathTargetProfile{}, false, nil
	}
	var currentAliasCount int
	var hasMatchingAlias, hasFutureSplit, hasPendingWipe, hasUnboundExpedition bool
	if err := transaction.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM commander_aliases WHERE profile_id = ? AND is_current = 1),
			EXISTS(SELECT 1 FROM commander_aliases WHERE profile_id = ? AND is_current = 1 AND raw_name COLLATE BINARY = ? AND fid = ? AND universe = ? AND start_timestamp != '' AND end_timestamp != '' AND end_timestamp <= ? AND event_count > 0),
			EXISTS(SELECT 1 FROM commander_overrides WHERE rule_type = 'split' AND TRIM(split_timestamp) > ?),
			EXISTS(SELECT 1 FROM journal_events WHERE event = 'ClearSavedGame' AND timestamp >= ? AND profile_id = ?),
			EXISTS(SELECT 1 FROM expeditions e LEFT JOIN commander_profiles cp ON cp.id = e.commander_profile_id WHERE COALESCE(e.commander_profile_id, 0) <= 0 OR cp.id IS NULL)`,
		candidate.id,
		candidate.id, batch.Commander, batch.FID, batch.Universe, batch.EarliestTimestamp,
		candidate.lastSeen,
		candidate.lastSeen, candidate.id,
	).Scan(&currentAliasCount, &hasMatchingAlias, &hasFutureSplit, &hasPendingWipe, &hasUnboundExpedition); err != nil {
		return fastPathTargetProfile{}, false, fmt.Errorf("verify fast-path commander invariants: %w", err)
	}
	if currentAliasCount != 1 || !hasMatchingAlias || hasFutureSplit || hasPendingWipe || hasUnboundExpedition {
		return fastPathTargetProfile{}, false, nil
	}
	return candidate, true, nil
}

func applyFastPathAppend(transaction *sql.Tx, targetProfile fastPathTargetProfile, batch LiveAppendBatch) error {
	for start := 0; start < len(batch.InsertedIDs); start += 500 {
		chunk := batch.InsertedIDs[start:min(start+500, len(batch.InsertedIDs))]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, targetProfile.id)
		for _, eventID := range chunk {
			args = append(args, eventID)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		if _, err := transaction.Exec(`UPDATE journal_events SET profile_id = ? WHERE id IN (`+placeholders+`)`, args...); err != nil {
			return fmt.Errorf("assign fast-path journal_events.profile_id: %w", err)
		}
	}
	if _, err := transaction.Exec(
		`UPDATE commander_profiles SET last_seen = CASE WHEN last_seen IS NULL OR last_seen < ? THEN ? ELSE last_seen END WHERE id = ?`,
		batch.LatestTimestamp, batch.LatestTimestamp, targetProfile.id,
	); err != nil {
		return fmt.Errorf("update fast-path commander_profiles.last_seen: %w", err)
	}
	if _, err := transaction.Exec(`
		UPDATE commander_aliases
		SET end_timestamp = CASE WHEN end_timestamp < ? THEN ? ELSE end_timestamp END,
		    event_count = event_count + ?
		WHERE profile_id = ? AND raw_name = ? COLLATE NOCASE`,
		batch.LatestTimestamp, batch.LatestTimestamp, len(batch.InsertedIDs), targetProfile.id, batch.Commander,
	); err != nil {
		return fmt.Errorf("update fast-path commander_aliases: %w", err)
	}
	if _, err := transaction.Exec(`
		INSERT INTO commanders(name, fid, enabled, first_seen, last_seen) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			fid = CASE WHEN commanders.fid = '' AND excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
			first_seen = CASE WHEN commanders.first_seen IS NULL OR commanders.first_seen = '' OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
			last_seen = CASE WHEN commanders.last_seen IS NULL OR commanders.last_seen = '' OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`,
		batch.Commander, batch.FID, targetProfile.enabled, targetProfile.firstSeen, batch.LatestTimestamp,
	); err != nil {
		return fmt.Errorf("update fast-path legacy commanders: %w", err)
	}
	return nil
}
