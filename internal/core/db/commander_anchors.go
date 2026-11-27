package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// LatestCommanderProfileID returns the ID of the most recently active commander
// save-slot profile (preferring enabled profiles, then latest last_seen, then
// highest ID), or (0, nil) when commander_profiles is empty.
func LatestCommanderProfileID(queryer Queryer) (int64, error) {
	var profileID int64
	err := queryer.QueryRow(
		`SELECT id FROM commander_profiles ORDER BY enabled DESC, COALESCE(last_seen, '') DESC, id DESC LIMIT 1`,
	).Scan(&profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("query latest commander profile id: %w", err)
	}
	return profileID, nil
}

func migrateSingleExpeditionCommanderV14(dbConn *sql.DB) error {
	return WithTx(dbConn, func(transaction *sql.Tx) error {
		type unmappedExpedition struct {
			id             int64
			commanderScope string
		}
		rows, err := transaction.Query(
			`SELECT id, COALESCE(commander_scope, 'all_enabled') FROM expeditions WHERE COALESCE(commander_profile_id, 0) = 0 ORDER BY id ASC`,
		)
		if err != nil {
			return fmt.Errorf("query expeditions for v14 commander backfill: %w", err)
		}
		var pending []unmappedExpedition
		for rows.Next() {
			var item unmappedExpedition
			if scanErr := rows.Scan(&item.id, &item.commanderScope); scanErr != nil {
				_ = rows.Close()
				return fmt.Errorf("scan expedition for v14 commander backfill: %w", scanErr)
			}
			pending = append(pending, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil || len(pending) == 0 {
			return err
		}

		latestProfileID, err := LatestCommanderProfileID(transaction)
		if err != nil {
			return err
		}

		for _, item := range pending {
			targetProfileID := int64(0)
			if strings.EqualFold(strings.TrimSpace(item.commanderScope), "selected") {
				lookupErr := transaction.QueryRow(
					`SELECT cp.id FROM expedition_commanders ec
					 JOIN commander_profiles cp ON cp.id = ec.profile_id
					 WHERE ec.expedition_id = ?
					 ORDER BY cp.enabled DESC, COALESCE(cp.last_seen, '') DESC, cp.id DESC LIMIT 1`,
					item.id,
				).Scan(&targetProfileID)
				if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
					return fmt.Errorf("lookup selected commander for expedition %d (v14): %w", item.id, lookupErr)
				}
			}
			if targetProfileID == 0 {
				targetProfileID = latestProfileID
			}
			if targetProfileID > 0 {
				if _, updateErr := transaction.Exec(
					`UPDATE expeditions SET commander_profile_id = ? WHERE id = ?`,
					targetProfileID, item.id,
				); updateErr != nil {
					return fmt.Errorf("backfill expedition %d commander_profile_id (v14): %w", item.id, updateErr)
				}
			}
		}
		return nil
	})
}

type expeditionCommanderAnchor struct {
	expeditionID     int64
	currentProfileID int64
	oldProfileKey    string
	startTimestamp   string
	anchorEventID    int64
}

type profileStartAnchorKey struct {
	profileID      int64
	startTimestamp string
}

func snapshotExpeditionCommanderAnchors(transaction *sql.Tx) ([]expeditionCommanderAnchor, error) {
	rows, err := transaction.Query(`
		SELECT e.id, COALESCE(e.commander_profile_id, 0), COALESCE(e.start_timestamp, ''), COALESCE(cp.profile_key, '')
		FROM expeditions e
		LEFT JOIN commander_profiles cp ON cp.id = e.commander_profile_id
		ORDER BY e.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("snapshot expedition commander anchors: %w", err)
	}
	var anchors []expeditionCommanderAnchor
	for rows.Next() {
		var anchor expeditionCommanderAnchor
		if scanErr := rows.Scan(&anchor.expeditionID, &anchor.currentProfileID, &anchor.startTimestamp, &anchor.oldProfileKey); scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		anchors = append(anchors, anchor)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil || len(anchors) == 0 {
		return nil, err
	}

	memoizedEventIDs := make(map[profileStartAnchorKey]int64, len(anchors))
	for index := range anchors {
		if anchors[index].currentProfileID <= 0 || anchors[index].oldProfileKey == "" {
			continue
		}
		lookupKey := profileStartAnchorKey{
			profileID:      anchors[index].currentProfileID,
			startTimestamp: anchors[index].startTimestamp,
		}
		eventID, cached := memoizedEventIDs[lookupKey]
		if !cached {
			if eventID, err = resolveProfileAnchorEventID(transaction, lookupKey.profileID, lookupKey.startTimestamp); err != nil {
				return nil, err
			}
			memoizedEventIDs[lookupKey] = eventID
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

func reconcileExpeditionCommandersFromAnchors(transaction *sql.Tx, anchors []expeditionCommanderAnchor) error {
	if len(anchors) == 0 {
		return nil
	}
	var latestProfileID int64
	loadedLatest := false
	for _, anchor := range anchors {
		targetProfileID, err := lookupAnchorTargetProfileID(transaction, anchor)
		if err != nil {
			return err
		}
		if targetProfileID == 0 {
			if !loadedLatest {
				if latestProfileID, err = LatestCommanderProfileID(transaction); err != nil {
					return err
				}
				loadedLatest = true
			}
			targetProfileID = latestProfileID
		}
		if targetProfileID != anchor.currentProfileID {
			if _, updateErr := transaction.Exec(
				`UPDATE expeditions SET commander_profile_id = ? WHERE id = ?`,
				targetProfileID, anchor.expeditionID,
			); updateErr != nil {
				return fmt.Errorf("update expedition %d commander_profile_id: %w", anchor.expeditionID, updateErr)
			}
		}
	}
	return nil
}

func lookupAnchorTargetProfileID(transaction *sql.Tx, anchor expeditionCommanderAnchor) (int64, error) {
	if anchor.currentProfileID <= 0 {
		return 0, nil
	}
	var resolvedProfileID int64
	if anchor.oldProfileKey != "" {
		if anchor.anchorEventID > 0 {
			err := transaction.QueryRow(`SELECT profile_id FROM journal_events WHERE id = ?`, anchor.anchorEventID).Scan(&resolvedProfileID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return 0, err
			}
			if resolvedProfileID > 0 {
				return resolvedProfileID, nil
			}
		}
		err := transaction.QueryRow(`SELECT id FROM commander_profiles WHERE LOWER(profile_key) = LOWER(?)`, anchor.oldProfileKey).Scan(&resolvedProfileID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return resolvedProfileID, err
	}
	err := transaction.QueryRow(`SELECT id FROM commander_profiles WHERE id = ?`, anchor.currentProfileID).Scan(&resolvedProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return resolvedProfileID, err
}
