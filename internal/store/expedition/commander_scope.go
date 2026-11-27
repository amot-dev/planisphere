package expedition

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
)

// ProfileScope returns the replay ProfileScope for expeditionRecord's assigned
// commander save-slot profile, or an unscoped ProfileScope before any commander
// profile has been resolved.
func (expeditionRecord Expedition) ProfileScope() replay.ProfileScope {
	if expeditionRecord.CommanderProfileID > 0 {
		return replay.NewProfileScope([]int64{expeditionRecord.CommanderProfileID})
	}
	return replay.ProfileScope{}
}

func commanderProfileExists(queryer db.Queryer, profileID int64) (bool, error) {
	if profileID <= 0 {
		return false, nil
	}
	var existsFlag int
	if err := queryer.QueryRow(`SELECT EXISTS(SELECT 1 FROM commander_profiles WHERE id = ?)`, profileID).Scan(&existsFlag); err != nil {
		return false, fmt.Errorf("validate commander_profile %d: %w", profileID, err)
	}
	return existsFlag != 0, nil
}

func resolveValidOrLatestCommanderProfileID(queryer db.Queryer, candidateProfileID int64) (int64, error) {
	if candidateProfileID > 0 {
		exists, err := commanderProfileExists(queryer, candidateProfileID)
		if err != nil {
			return 0, err
		}
		if exists {
			return candidateProfileID, nil
		}
	}
	return db.LatestCommanderProfileID(queryer)
}

func resolveTargetCommanderProfileID(queryer db.Queryer, existingProfileID, requestedProfileID int64) (int64, error) {
	if requestedProfileID > 0 {
		exists, err := commanderProfileExists(queryer, requestedProfileID)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, fmt.Errorf("commander profile %d does not exist", requestedProfileID)
		}
		return requestedProfileID, nil
	}
	return resolveValidOrLatestCommanderProfileID(queryer, existingProfileID)
}

func setCommanderProfileForExisting(transaction *sql.Tx, existing Expedition, commanderProfileID int64) (bool, error) {
	targetProfileID, err := resolveTargetCommanderProfileID(transaction, existing.CommanderProfileID, commanderProfileID)
	if err != nil {
		return false, err
	}
	if existing.CommanderProfileID == targetProfileID {
		return false, nil
	}
	_, err = transaction.Exec(
		`UPDATE expeditions SET commander_profile_id = ?, updated_at = ? WHERE id = ?`,
		targetProfileID, reference.NowISO(), existing.ID,
	)
	return true, err
}

// SetCommanderProfile validates and persists an expedition's assigned commander
// save-slot profile. Non-positive commanderProfileID preserves the existing
// profile when still valid, or selects LatestCommanderProfileID otherwise.
func SetCommanderProfile(transaction *sql.Tx, expeditionID int64, commanderProfileID int64) (bool, error) {
	existing, err := Get(transaction, expeditionID)
	if err != nil {
		return false, err
	}
	return setCommanderProfileForExisting(transaction, existing, commanderProfileID)
}

// UpdateMetaAndCommanderTx updates an expedition's name, description, and
// commander save-slot profile inside one transaction, re-evaluating waypoint
// visits when the assigned commander changes on an active or completed expedition.
func UpdateMetaAndCommanderTx(dbConn *sql.DB, id int64, name, description string, commanderProfileID int64) (bool, error) {
	var profileChanged bool
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		existing, err := Get(transaction, id)
		if err != nil {
			return err
		}
		if err := UpdateMeta(transaction, id, name, description); err != nil {
			return err
		}
		if profileChanged, err = setCommanderProfileForExisting(transaction, existing, commanderProfileID); err != nil {
			return err
		}
		if profileChanged && (existing.Status == StatusActive || existing.Status == StatusCompleted) {
			_, err = Reevaluate(transaction, id)
			return err
		}
		return nil
	})
	return profileChanged, txError
}

// StartScopedTx updates the expedition's commander save-slot profile and starts
// the expedition atomically inside one transaction.
func StartScopedTx(dbConn *sql.DB, expeditionID int64, startTimestamp string, initialModeID int64, commanderProfileID int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if _, err := SetCommanderProfile(transaction, expeditionID, commanderProfileID); err != nil {
			return err
		}
		return Start(transaction, expeditionID, startTimestamp, initialModeID)
	})
}

func planningStartCandidatesScope(queryer db.Queryer) (replay.ProfileScope, error) {
	rows, err := queryer.Query(`SELECT id FROM commander_profiles ORDER BY id ASC`)
	if err != nil {
		return replay.ProfileScope{}, fmt.Errorf("query commander_profiles for start candidates: %w", err)
	}
	defer rows.Close()
	var profileIDs []int64
	for rows.Next() {
		var profileID int64
		if scanErr := rows.Scan(&profileID); scanErr != nil {
			return replay.ProfileScope{}, scanErr
		}
		profileIDs = append(profileIDs, profileID)
	}
	if err := rows.Err(); err != nil {
		return replay.ProfileScope{}, err
	}
	if len(profileIDs) == 0 {
		return replay.ProfileScope{}, nil
	}
	return replay.NewProfileScope(profileIDs), nil
}
