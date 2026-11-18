package app

import (
	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/store/modes"
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
		modeState.ActiveModeID, modeState.ActiveSince, modeState.ActiveSinceGalactic = activeWindow.ModeID, activeWindow.Start, reference.ToGalactic(activeWindow.Start)
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
			StartGalactic: reference.ToGalactic(windowItem.Start), EndGalactic: reference.ToGalactic(windowItem.End),
			Duration: reference.Elapsed(windowItem.Start, windowItem.End),
		})
	}
	return modeState, nil
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
	app.publishApp("modes", 0, events.AppModesChanged)
	return createdMode, nil
}

// UpdateMode edits a mode.
func (app *App) UpdateMode(id int64, name, description, color string) error {
	if updateError := app.withReadyReadLock(func() error {
		return modes.Update(app.db, id, name, description, color)
	}); updateError != nil {
		return updateError
	}
	app.publishApp("modes", 0, events.AppModesChanged)
	return nil
}

// DeleteMode deletes a custom mode and its windows.
func (app *App) DeleteMode(id int64) error {
	if deleteError := app.withReadyReadLock(func() error {
		return modes.Delete(app.db, id)
	}); deleteError != nil {
		return deleteError
	}
	app.publishApp("modes", 0, events.AppModesChanged, events.AppModeWindowsChanged)
	return nil
}

// SwitchMode makes modeID the active mode from now (0 stops tracking a mode).
func (app *App) SwitchMode(modeID int64) (ModesState, error) {
	if switchError := app.withReadyReadLock(func() error {
		return modes.SwitchTx(app.db, modeID, "")
	}); switchError != nil {
		return ModesState{}, switchError
	}
	app.publishApp("modes", 0, events.AppModesChanged, events.AppModeWindowsChanged)
	return app.GetModesState()
}
