package app

import (
	"database/sql"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/expedition"
	"planisphere/internal/modes"
)

// ModeWindowView is a mode window for display.
type ModeWindowView struct {
	ID            int64  `json:"id"`
	ModeID        int64  `json:"modeId"`
	ModeName      string `json:"modeName"`
	Color         string `json:"color"`
	Start         string `json:"start"`
	End           string `json:"end"`
	StartGalactic string `json:"startGalactic"`
	EndGalactic   string `json:"endGalactic"`
	Duration      string `json:"duration"`
}

// ModesState is everything the UI needs about modes.
type ModesState struct {
	Modes               []modes.Mode     `json:"modes"`
	ActiveModeID        int64            `json:"activeModeId"`
	ActiveSince         string           `json:"activeSince"`
	ActiveSinceGalactic string           `json:"activeSinceGalactic"`
	Windows             []ModeWindowView `json:"windows"`
}

// GetModesState returns modes, the active mode and all windows (newest first).
func (app *App) GetModesState() (ModesState, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return ModesState{}, readyError
	}
	var modeState ModesState
	modesList, listError := modes.List(app.db)
	if listError != nil {
		return modeState, listError
	}
	modeState.Modes = modesList
	if modeState.Modes == nil {
		modeState.Modes = []modes.Mode{}
	}
	modesByID := map[int64]modes.Mode{}
	for _, modeItem := range modesList {
		modesByID[modeItem.ID] = modeItem
	}
	activeWindow, activeError := modes.Active(app.db)
	if activeError != nil {
		return modeState, activeError
	}
	if activeWindow != nil {
		modeState.ActiveModeID, modeState.ActiveSince, modeState.ActiveSinceGalactic = activeWindow.ModeID, activeWindow.Start, ed.ToGalactic(activeWindow.Start)
	} else {
		modeState.ActiveModeID = modes.DefaultFallbackModeID(app.db)
	}
	windowsList, windowsError := modes.ListWindows(app.db)
	if windowsError != nil {
		return modeState, windowsError
	}
	modeState.Windows = []ModeWindowView{}
	for windowIndex := len(windowsList) - 1; windowIndex >= 0; windowIndex-- {
		windowItem := windowsList[windowIndex]
		modeItem := modesByID[windowItem.ModeID]
		modeState.Windows = append(modeState.Windows, ModeWindowView{
			ID: windowItem.ID, ModeID: windowItem.ModeID, ModeName: modeItem.Name, Color: modeItem.Color, Start: windowItem.Start, End: windowItem.End,
			StartGalactic: ed.ToGalactic(windowItem.Start), EndGalactic: ed.ToGalactic(windowItem.End),
			Duration: expedition.Elapsed(windowItem.Start, windowItem.End),
		})
	}
	return modeState, nil
}

// CreateMode adds a custom mode.
func (app *App) CreateMode(name, description, color string) (modes.Mode, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return modes.Mode{}, readyError
	}
	createdMode, createError := modes.Create(app.db, name, description, color)
	if createError == nil {
		app.changed("modes", 0)
	}
	return createdMode, createError
}

// UpdateMode edits a mode.
func (app *App) UpdateMode(id int64, name, description, color string) error {
	if updateError := app.withReadyReadLock(func() error {
		return modes.Update(app.db, id, name, description, color)
	}); updateError != nil {
		return updateError
	}
	app.changed("modes", 0)
	return nil
}

// DeleteMode deletes a custom mode and its windows.
func (app *App) DeleteMode(id int64) error {
	if deleteError := app.withReadyReadLock(func() error {
		return modes.Delete(app.db, id)
	}); deleteError != nil {
		return deleteError
	}
	app.changed("modes", 0)
	return nil
}

// SwitchMode makes modeID the active mode from now (0 stops tracking a mode).
func (app *App) SwitchMode(modeID int64) (ModesState, error) {
	if switchError := app.withReadyReadLock(func() error {
		return db.WithTx(app.db, func(transaction *sql.Tx) error { return modes.Switch(transaction, modeID, "") })
	}); switchError != nil {
		return ModesState{}, switchError
	}
	app.changed("modes", 0)
	return app.GetModesState()
}

// ModeWindowInput creates (ID 0) or edits a mode window. Start and End are
// ISO-8601 UTC; End "" means open-ended.
type ModeWindowInput struct {
	ID     int64  `json:"id"`
	ModeID int64  `json:"modeId"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

// SaveModeWindow writes a window retroactively; overlapping windows are
// trimmed or split so that windows never overlap.
func (app *App) SaveModeWindow(input ModeWindowInput) (ModesState, error) {
	if saveError := app.withReadyReadLock(func() error {
		return db.WithTx(app.db, func(transaction *sql.Tx) error {
			return modes.SetWindow(transaction, input.ID, input.ModeID, input.Start, input.End)
		})
	}); saveError != nil {
		return ModesState{}, saveError
	}
	app.changed("modes", 0)
	return app.GetModesState()
}

// DeleteModeWindow removes a window.
func (app *App) DeleteModeWindow(id int64) (ModesState, error) {
	if deleteError := app.withReadyReadLock(func() error {
		return modes.DeleteWindow(app.db, id)
	}); deleteError != nil {
		return ModesState{}, deleteError
	}
	app.changed("modes", 0)
	return app.GetModesState()
}

