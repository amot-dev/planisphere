package app

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/replay"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func (app *App) applyCommanderOverride(mutation func() error) (Settings, error) {
	if err := app.runUndoExclusive(func() error {
		if overrideError := app.withExclusiveCommanderDB(mutation); overrideError != nil {
			return overrideError
		}
		app.clearUndoHistory()
		return nil
	}); err != nil {
		return Settings{}, err
	}
	app.clearExportSnapshot()
	app.publishApp(0, events.AppCommanderFilterChanged, events.AppSettingsChanged, events.AppExpeditionPlanChanged)
	return app.GetSettings()
}

// MergeCommanders merges sourceKey into targetKey, refreshes commander save-slot
// resolution, and returns updated Settings.
func (app *App) MergeCommanders(sourceKey, targetKey string) (Settings, error) {
	return app.applyCommanderOverride(func() error {
		return settings.MergeCommanders(app.db, sourceKey, targetKey)
	})
}

// UnmergeCommanderAlias detaches aliasName from profileKey into its own
// commander profile and returns updated Settings.
func (app *App) UnmergeCommanderAlias(aliasName, profileKey string) (Settings, error) {
	return app.applyCommanderOverride(func() error {
		return settings.UnmergeCommanderAlias(app.db, aliasName, profileKey)
	})
}

// SplitCommander splits profileKey's career history at splitTimestamp (ISO-8601
// or YYYY-MM-DD) and returns updated Settings.
func (app *App) SplitCommander(profileKey, splitTimestamp string) (Settings, error) {
	return app.applyCommanderOverride(func() error {
		return settings.SplitCommander(app.db, profileKey, splitTimestamp)
	})
}

// ResetCommanderOverrides clears all manual merge, split, and unmerge overrides
// affecting profileKey and returns updated Settings.
func (app *App) ResetCommanderOverrides(profileKey string) (Settings, error) {
	return app.applyCommanderOverride(func() error {
		return settings.ResetCommanderOverrides(app.db, profileKey)
	})
}

// ListCommanderLoadGamesOnDate returns chronological LoadGame sessions recorded
// for profileKey on the UTC calendar date dateISO ("YYYY-MM-DD").
func (app *App) ListCommanderLoadGamesOnDate(profileKey, dateISO string) ([]replay.CommanderSession, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	sessions, queryError := replay.ListCommanderLoadGamesOnDate(app.db, profileKey, dateISO)
	if sessions == nil && queryError == nil {
		sessions = []replay.CommanderSession{}
	}
	return sessions, queryError
}

// SetCareerBaselineScope updates whether sidebar All Time and Waypoint #1
// OriginBaseline metrics follow the active expedition's commander scope
// ("expedition") or all globally enabled commanders ("all_enabled").
func (app *App) SetCareerBaselineScope(scope string) (Settings, error) {
	if saveError := app.withReadyReadLock(func() error {
		return settings.SetCareerBaselineScope(app.db, scope)
	}); saveError != nil {
		return Settings{}, saveError
	}
	app.clearExportSnapshot()
	app.publishApp(0, events.AppCommanderFilterChanged, events.AppSettingsChanged)
	return app.GetSettings()
}

// UpdateExpeditionScoped updates an expedition's metadata and assigned commander
// save-slot profile atomically, re-evaluating active or completed waypoint progress
// when the assigned commander changes.
func (app *App) UpdateExpeditionScoped(
	id int64,
	name, description string,
	commanderProfileID int64,
) error {
	var profileChanged bool
	metaChanged, updateError := app.recordExpeditionMetaMutation(id, "Edit Expedition", func(targetDB *sql.DB, before expedition.Expedition) (bool, error) {
		var txError error
		if profileChanged, txError = expedition.UpdateMetaAndCommanderTx(targetDB, id, name, description, commanderProfileID); txError != nil {
			return false, txError
		}
		if profileChanged {
			app.clearExpeditionUndoHistoryLocked(targetDB, id)
			return false, nil
		}
		return expeditionMetaChanged(before, name, description), nil
	})
	if updateError != nil {
		return updateError
	}
	app.clearExportSnapshot()
	if profileChanged {
		app.publishApp(id, events.AppExpeditionPlanChanged, events.AppExpeditionBoundsChanged, events.AppExpeditionModesChanged)
		return nil
	}
	if metaChanged {
		app.publishApp(id, events.AppExpeditionPlanChanged)
	}
	return nil
}

// StartExpeditionScoped applies the expedition's commander save-slot profile,
// activates the expedition at timestamp with initialModeID, and replays scoped
// arrival history.
func (app *App) StartExpeditionScoped(
	expeditionID int64,
	timestamp string,
	initialModeID int64,
	commanderProfileID int64,
) error {
	applied, startError := app.recordUndoableMutation(expeditionID, "Engage Expedition", func(targetDB *sql.DB) (bool, error) {
		err := expedition.StartScopedTx(targetDB, expeditionID, timestamp, initialModeID, commanderProfileID)
		return err == nil, err
	})
	if startError != nil {
		return startError
	}
	if applied {
		app.clearExportSnapshot()
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionBoundsChanged, events.AppExpeditionModesChanged)
	}
	return nil
}
