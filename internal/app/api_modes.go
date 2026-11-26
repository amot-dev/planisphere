package app

import (
	"planisphere/internal/core/events"
	"planisphere/internal/store/modes"
)

// ModesState holds the configured flight modes list and default fallback mode ID for the UI.
type ModesState struct {
	Modes         []modes.Mode `json:"modes"`
	DefaultModeID int64        `json:"defaultModeId"`
}

// GetModesState returns all configured flight modes in display order.
func (app *App) GetModesState() (ModesState, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return ModesState{}, readyError
	}
	modesList, listError := modes.List(app.db)
	if listError != nil {
		return ModesState{}, listError
	}
	if modesList == nil {
		modesList = []modes.Mode{}
	}
	return ModesState{Modes: modesList, DefaultModeID: modes.DefaultFallbackModeID(app.db)}, nil
}

// GetModeUsage returns every expedition that currently references id on visited
// legs or as its active flight mode.
func (app *App) GetModeUsage(id int64) ([]modes.ExpeditionUsage, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	usageList, usageError := modes.UsageByExpedition(app.db, id)
	if usageError != nil {
		return nil, usageError
	}
	if usageList == nil {
		usageList = []modes.ExpeditionUsage{}
	}
	return usageList, nil
}

// CreateMode adds a custom mode.
func (app *App) CreateMode(name, description, color string) (modes.Mode, error) {
	var createdMode modes.Mode
	if createError := app.withReadyReadLock(func() error {
		var err error
		createdMode, err = modes.Create(app.db, name, description, color)
		return err
	}); createError != nil {
		return modes.Mode{}, createError
	}
	app.publishApp(0, events.AppModesChanged)
	return createdMode, nil
}

// UpdateMode edits a mode.
func (app *App) UpdateMode(id int64, name, description, color string) error {
	if updateError := app.withReadyReadLock(func() error {
		return modes.Update(app.db, id, name, description, color)
	}); updateError != nil {
		return updateError
	}
	app.publishApp(0, events.AppModesChanged)
	return nil
}

// DeleteMode deletes a custom mode and reassigns any legs and expeditions
// referencing it back to Travel.
func (app *App) DeleteMode(id int64) error {
	if deleteError := app.withReadyReadLock(func() error {
		return modes.DeleteTx(app.db, id)
	}); deleteError != nil {
		return deleteError
	}
	app.publishApp(0, events.AppModesChanged, events.AppExpeditionModesChanged)
	return nil
}
