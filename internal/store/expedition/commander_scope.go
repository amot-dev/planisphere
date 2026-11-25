package expedition

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
)

// Expedition commander save-slot scope modes.
const (
	ScopeAllEnabled = "all_enabled"
	ScopeSelected   = "selected"
)

// ProfileScope returns the replay ProfileScope configured on expeditionRecord.
func (expeditionRecord Expedition) ProfileScope() replay.ProfileScope {
	if expeditionRecord.CommanderScope != ScopeSelected {
		return replay.ProfileScope{}
	}
	return replay.NewProfileScope(expeditionRecord.CommanderProfileIDs)
}

// LoadExpeditionCommanderIDs batch-loads sorted commander profile IDs for expeditionIDs in one query.
func LoadExpeditionCommanderIDs(queryer db.Queryer, expeditionIDs []int64) (map[int64][]int64, error) {
	if len(expeditionIDs) == 0 {
		return map[int64][]int64{}, nil
	}
	placeholders, args := make([]string, len(expeditionIDs)), make([]any, len(expeditionIDs))
	for index, expeditionID := range expeditionIDs {
		placeholders[index], args[index] = "?", expeditionID
	}
	rows, err := queryer.Query(
		`SELECT expedition_id, profile_id FROM expedition_commanders WHERE expedition_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY expedition_id ASC, profile_id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("load expedition_commanders: %w", err)
	}
	defer rows.Close()

	byExpedition := make(map[int64][]int64, len(expeditionIDs))
	for rows.Next() {
		var expeditionID, profileID int64
		if err := rows.Scan(&expeditionID, &profileID); err != nil {
			return nil, err
		}
		byExpedition[expeditionID] = append(byExpedition[expeditionID], profileID)
	}
	return byExpedition, rows.Err()
}

func attachSelectedCommanderIDs(queryer db.Queryer, expeditions []Expedition) error {
	var selectedIDs []int64
	for index := range expeditions {
		if expeditions[index].CommanderProfileIDs == nil {
			expeditions[index].CommanderProfileIDs = []int64{}
		}
		if expeditions[index].CommanderScope == ScopeSelected {
			selectedIDs = append(selectedIDs, expeditions[index].ID)
		}
	}
	if len(selectedIDs) == 0 {
		return nil
	}
	byExpedition, err := LoadExpeditionCommanderIDs(queryer, selectedIDs)
	if err != nil {
		return err
	}
	for index := range expeditions {
		if ids := byExpedition[expeditions[index].ID]; len(ids) > 0 {
			expeditions[index].CommanderProfileIDs = ids
		}
	}
	return nil
}

func cloneExpeditionCommanders(transaction *sql.Tx, sourceID, clonedID int64) error {
	_, err := transaction.Exec(`INSERT INTO expedition_commanders (expedition_id, profile_id) SELECT ?, profile_id FROM expedition_commanders WHERE expedition_id = ?`, clonedID, sourceID)
	return err
}

// SetCommanderScope validates and persists an expedition's commander scope and
// selected commander_profiles rows. Empty commanderScope preserves existing scope.
func SetCommanderScope(transaction *sql.Tx, expeditionID int64, commanderScope string, profileIDs []int64) (bool, error) {
	if strings.TrimSpace(commanderScope) == "" {
		return false, nil
	}
	existing, err := Get(transaction, expeditionID)
	if err != nil {
		return false, err
	}
	return setCommanderScopeForExisting(transaction, existing, commanderScope, profileIDs)
}

func setCommanderScopeForExisting(transaction *sql.Tx, existing Expedition, commanderScope string, profileIDs []int64) (bool, error) {
	trimmedScope := strings.ToLower(strings.TrimSpace(commanderScope))
	if trimmedScope == "" {
		return false, nil
	}
	if trimmedScope != ScopeAllEnabled && trimmedScope != ScopeSelected {
		return false, fmt.Errorf("invalid commander scope %q", commanderScope)
	}

	var normalizedIDs []int64
	if trimmedScope == ScopeSelected {
		var err error
		if normalizedIDs, err = normalizeAndValidateSelectedProfiles(transaction, profileIDs); err != nil {
			return false, err
		}
	}
	scopeChanged := existing.CommanderScope != trimmedScope || !slices.Equal(existing.CommanderProfileIDs, normalizedIDs)
	if _, err := transaction.Exec(`DELETE FROM expedition_commanders WHERE expedition_id = ?`, existing.ID); err != nil {
		return false, err
	}
	for _, profileID := range normalizedIDs {
		if _, err := transaction.Exec(`INSERT INTO expedition_commanders (expedition_id, profile_id) VALUES (?, ?)`, existing.ID, profileID); err != nil {
			return false, err
		}
	}
	_, err := transaction.Exec(`UPDATE expeditions SET commander_scope = ?, updated_at = ? WHERE id = ?`, trimmedScope, reference.NowISO(), existing.ID)
	return scopeChanged, err
}

func normalizeAndValidateSelectedProfiles(transaction *sql.Tx, profileIDs []int64) ([]int64, error) {
	cleaned := make([]int64, 0, len(profileIDs))
	for _, profileID := range profileIDs {
		if profileID <= 0 {
			return nil, fmt.Errorf("invalid commander profile id %d", profileID)
		}
		cleaned = append(cleaned, profileID)
	}
	if len(cleaned) == 0 {
		return nil, errors.New("at least one commander profile is required when scope is selected")
	}
	slices.Sort(cleaned)
	cleaned = slices.Compact(cleaned)

	placeholders, args := make([]string, len(cleaned)), make([]any, len(cleaned))
	for index, profileID := range cleaned {
		placeholders[index], args[index] = "?", profileID
	}
	var matchedCount int
	if err := transaction.QueryRow(`SELECT COUNT(*) FROM commander_profiles WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...).Scan(&matchedCount); err != nil {
		return nil, fmt.Errorf("validate commander_profiles selection: %w", err)
	}
	if matchedCount != len(cleaned) {
		return nil, errors.New("one or more selected commander profiles do not exist")
	}
	return cleaned, nil
}

// UpdateMetaAndScopeTx updates an expedition's name, description, and commander
// scope inside one transaction, re-evaluating waypoint visits when scope changes
// on an active or completed expedition.
func UpdateMetaAndScopeTx(dbConn *sql.DB, id int64, name, description, commanderScope string, profileIDs []int64) (bool, error) {
	var scopeChanged bool
	txError := db.WithTx(dbConn, func(transaction *sql.Tx) error {
		existing, err := Get(transaction, id)
		if err != nil {
			return err
		}
		if err := UpdateMeta(transaction, id, name, description); err != nil {
			return err
		}
		if scopeChanged, err = setCommanderScopeForExisting(transaction, existing, commanderScope, profileIDs); err != nil {
			return err
		}
		if scopeChanged && (existing.Status == StatusActive || existing.Status == StatusCompleted) {
			_, err = Reevaluate(transaction, id)
			return err
		}
		return nil
	})
	return scopeChanged, txError
}

// StartScopedTx updates the expedition's commander scope (when non-empty) and
// starts the expedition atomically inside one transaction.
func StartScopedTx(dbConn *sql.DB, expeditionID int64, startTimestamp string, initialModeID int64, commanderScope string, profileIDs []int64) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if _, err := SetCommanderScope(transaction, expeditionID, commanderScope, profileIDs); err != nil {
			return err
		}
		return Start(transaction, expeditionID, startTimestamp, initialModeID)
	})
}
