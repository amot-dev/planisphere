package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/modes"
	"planisphere/internal/stats"
	"planisphere/internal/version"
)

var errorNotReady = errors.New("database is not available — check the startup error in Settings")

// ready checks if the database is available. Caller must hold app.dbMu.RLock.
func (app *App) ready() error {
	if app.db == nil {
		if startupError := app.getError(); startupError != "" {
			return fmt.Errorf("database is not available: %s", startupError)
		}
		return errorNotReady
	}
	return nil
}

// ============================================================================
// Settings and journal ingestion
// ============================================================================

// Settings is the user-configurable application state.
type Settings struct {
	SetupCompleted           bool                `json:"setupCompleted"`
	JournalDir               string              `json:"journalDir"`
	DefaultJournalDir        string              `json:"defaultJournalDir"`
	JournalDirExists         bool                `json:"journalDirExists"`
	OnlineLookups            bool                `json:"onlineLookups"`
	DBPath                   string              `json:"dbPath"`
	DefaultDBPath            string              `json:"defaultDbPath"`
	ActiveExpeditionID       int64               `json:"activeExpeditionId"`
	GECCount                 int                 `json:"gecCount"`
	GECFetchedAt             string              `json:"gecFetchedAt"`
	Commanders               []journal.Commander `json:"commanders"`
	EDDiscoveryDBPath        string              `json:"edDiscoveryDbPath"`
	DefaultEDDiscoveryDBPath string              `json:"defaultEdDiscoveryDbPath"`
	EDDiscoveryDBExists      bool                `json:"edDiscoveryDbExists"`
}

// GetSettings returns the current settings.
func (app *App) GetSettings() (Settings, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return Settings{}, readyError
	}
	dir := app.journalDir()
	commanders, _ := journal.ListCommanders(app.db)
	if commanders == nil {
		commanders = []journal.Commander{}
	}
	defaultDBPath, _ := db.DefaultPath()
	defaultEDDPath := journal.DefaultEDDiscoveryDBPath()
	discoveredEDDPath := journal.DiscoverEDDiscoveryDBPath()
	var gecCount int
	var gecFetched string
	if app.gec != nil {
		gecCount = app.gec.Count()
		gecFetched = app.gec.LastFetched()
	}
	return Settings{
		SetupCompleted:           app.setupCompleted(),
		JournalDir:               dir,
		DefaultJournalDir:        journal.DefaultJournalDir(),
		JournalDirExists:         journal.DirExists(dir),
		OnlineLookups:            app.onlineEnabled(),
		DBPath:                   app.dbPath,
		DefaultDBPath:            defaultDBPath,
		ActiveExpeditionID:       app.activeExpeditionID(),
		GECCount:                 gecCount,
		GECFetchedAt:             gecFetched,
		Commanders:               commanders,
		EDDiscoveryDBPath:        discoveredEDDPath,
		DefaultEDDiscoveryDBPath: defaultEDDPath,
		EDDiscoveryDBExists:      discoveredEDDPath != "" || journal.FileExists(defaultEDDPath),
	}, nil
}

// BrowseDBPath opens a save-file dialog so the user can choose where the SQLite database file should be stored.
func (app *App) BrowseDBPath() (string, error) {
	defaultDir := ""
	defaultFilename := db.DefaultDBFileName
	app.dbMu.RLock()
	currentDBPath := app.dbPath
	app.dbMu.RUnlock()
	if currentDBPath != "" {
		defaultDir = filepath.Dir(currentDBPath)
		defaultFilename = filepath.Base(currentDBPath)
	}
	return runtime.SaveFileDialog(app.ctx, runtime.SaveDialogOptions{
		Title:                fmt.Sprintf("Select %s Database Location", version.ProductName),
		DefaultDirectory:     defaultDir,
		DefaultFilename:      defaultFilename,
		CanCreateDirectories: true,
		Filters: []runtime.FileFilter{
			{DisplayName: "SQLite Database (*.db, *.sqlite)", Pattern: "*.db;*.sqlite"},
		},
	})
}

// SetDatabasePath updates the active SQLite database location, persists it in
// bootstrap.json, and hot-switches the running database connection if changed.
func (app *App) SetDatabasePath(dbPath string) (Settings, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		defaultPath, defaultPathError := db.DefaultPath()
		if defaultPathError != nil {
			return Settings{}, defaultPathError
		}
		dbPath = defaultPath
	} else if journal.DirExists(dbPath) {
		dbPath = filepath.Join(dbPath, db.DefaultDBFileName)
	}
	dbPath = filepath.Clean(dbPath)

	app.dbMu.RLock()
	currentPath := app.dbPath
	app.dbMu.RUnlock()

	if dbPath != currentPath {
		if openError := app.openAndWireDB(dbPath); openError != nil {
			return Settings{}, openError
		}
		app.dbMu.RLock()
		setupDone := app.setupCompleted()
		watcher := app.watcher
		jDir := app.journalDir()
		app.dbMu.RUnlock()
		if setupDone && watcher != nil {
			watcher.Start(app.ctx, jDir)
		}
	}
	if saveError := db.SaveConfiguredPath(dbPath); saveError != nil {
		return Settings{}, saveError
	}
	app.changed("settings", 0)
	return app.GetSettings()
}

// SetupConfigureJournals sets the journal directory, performs a synchronous
// initial scan of local .log files, starts the live watcher, and returns the
// resulting archive status (including oldest journal date and discovered CMDRs).
func (app *App) SetupConfigureJournals(dir string) (IngestStatus, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		dir = strings.TrimSpace(dir)
		if dir != "" && !journal.DirExists(dir) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return db.SetSetting(app.db, settingJournalDir, dir)
	}(); err != nil {
		return IngestStatus{}, err
	}
	app.runScan()
	app.dbMu.RLock()
	watcher := app.watcher
	jDir := app.journalDir()
	app.dbMu.RUnlock()
	if watcher != nil {
		watcher.Start(app.ctx, jDir)
	}
	app.changed("journal", 0)
	return app.GetIngestStatus()
}

// CompleteSetup marks the first-time setup wizard as finished and triggers
// any deferred startup tasks (watcher and GEC catalog sync).
func (app *App) CompleteSetup() (Settings, error) {
	var shouldRefresh bool
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		if saveError := db.SetSetting(app.db, settingSetupCompleted, "1"); saveError != nil {
			return saveError
		}
		if app.watcher != nil {
			app.watcher.Start(app.ctx, app.journalDir())
		}
		if app.onlineEnabled() && app.gec != nil && app.gec.Count() == 0 {
			shouldRefresh = true
		}
		return nil
	}(); err != nil {
		return Settings{}, err
	}
	if shouldRefresh {
		go app.maybeRefreshGEC()
	}
	app.changed("settings", 0)
	return app.GetSettings()
}

// SetCommanderEnabled enables or disables a commander in statistics and plans.
func (app *App) SetCommanderEnabled(name string, enabled bool) (Settings, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return journal.SetCommanderEnabled(app.db, name, enabled)
	}(); err != nil {
		return Settings{}, err
	}
	app.invalidateAllTimeCache()
	app.reevaluateActive()
	app.changed("journal", 0)
	return app.GetSettings()
}

// BrowseEDDiscoveryDB opens a file picker to select an EDDUser.sqlite file.
func (app *App) BrowseEDDiscoveryDB() (string, error) {
	return runtime.OpenFileDialog(app.ctx, runtime.OpenDialogOptions{
		Title: "Select EDDiscovery SQLite Database (EDDUser.sqlite)",
		Filters: []runtime.FileFilter{
			{DisplayName: "SQLite Database (*.sqlite, *.db)", Pattern: "*.sqlite;*.db"},
		},
	})
}

// ImportEDDiscoveryHistory imports raw historical journal events from an
// EDDiscovery SQLite database (EDDUser.sqlite) into our self-contained SQLite
// archive so deleted older .log files are permanently recovered.
func (app *App) ImportEDDiscoveryHistory(eddPath string) (int, error) {
	app.dbMu.RLock()
	if readyError := app.ready(); readyError != nil {
		app.dbMu.RUnlock()
		return 0, readyError
	}
	eddPath = strings.TrimSpace(eddPath)
	if eddPath == "" {
		eddPath = journal.DefaultEDDiscoveryDBPath()
	}
	defer app.emitEDDProgress(false, 0, 0, 0, "")
	ingester := app.ingester
	if ingester == nil {
		app.dbMu.RUnlock()
		return 0, errorNotReady
	}
	importResult, importError := ingester.ImportEDDiscoveryDB(eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if importError != nil {
		app.dbMu.RUnlock()
		return 0, importError
	}
	_ = db.SetSetting(app.db, "edd_history_recovered_v1", "1")
	_ = db.SetSetting(app.db, "edd_db_path", eddPath)
	_ = db.SetSetting(app.db, "edd_history_realization_v6", "1")
	app.dbMu.RUnlock()

	app.invalidateAllTimeCache()
	app.reevaluateActive()
	app.changed("journal", 0)
	return importResult.LinesRead, nil
}

// SetJournalDir changes the journal directory, restarts the watcher and
// performs a catch-up scan in the background. Pass "" to restore the default.
func (app *App) SetJournalDir(dir string) (Settings, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		dir = strings.TrimSpace(dir)
		if dir != "" && !journal.DirExists(dir) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return db.SetSetting(app.db, settingJournalDir, dir)
	}(); err != nil {
		return Settings{}, err
	}
	go func() {
		app.runScan()
		app.dbMu.RLock()
		watcher := app.watcher
		jDir := app.journalDir()
		app.dbMu.RUnlock()
		if watcher != nil {
			watcher.Start(app.ctx, jDir)
		}
	}()
	return app.GetSettings()
}

// BrowseJournalDir opens a native directory picker and returns the choice ("" if cancelled).
func (app *App) BrowseJournalDir() (string, error) {
	app.dbMu.RLock()
	defaultDir := app.journalDir()
	app.dbMu.RUnlock()
	return runtime.OpenDirectoryDialog(app.ctx, runtime.OpenDialogOptions{
		Title:            "Select Elite Dangerous journal folder",
		DefaultDirectory: defaultDir,
	})
}

// SetOnlineLookups enables/disables EDSM and EDAstro network requests.
func (app *App) SetOnlineLookups(enabled bool) (Settings, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		settingVal := "1"
		if !enabled {
			settingVal = "0"
		}
		return db.SetSetting(app.db, settingOnline, settingVal)
	}(); err != nil {
		return Settings{}, err
	}
	if enabled {
		go app.maybeRefreshGEC()
	}
	return app.GetSettings()
}

// IngestStatus reports journal archive and watcher state.
type IngestStatus struct {
	Scanning           bool   `json:"scanning"`
	WatcherActive      bool   `json:"watcherActive"`
	WatcherError       string `json:"watcherError"`
	LastError          string `json:"lastError"`
	LastScanAt         string `json:"lastScanAt"`
	FilesTracked       int    `json:"filesTracked"`
	EventsStored       int    `json:"eventsStored"`
	FirstEventAt       string `json:"firstEventAt"`
	FirstEventGalactic string `json:"firstEventGalactic"`
	LastEventAt        string `json:"lastEventAt"`
	LastEventGalactic  string `json:"lastEventGalactic"`
	CurrentSystem      string `json:"currentSystem"`
	CurrentShip        string `json:"currentShip"`
}

// GetIngestStatus returns the journal ingestion status.
func (app *App) GetIngestStatus() (IngestStatus, error) {
	status := IngestStatus{Scanning: app.scanning.Load(), LastError: app.getError()}
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return status, nil
	}
	if app.watcher != nil {
		status.WatcherActive, status.WatcherError = app.watcher.Active()
	}
	status.LastScanAt, _ = db.GetSetting(app.db, settingLastScanAt)
	archiveStatus, archiveError := journal.ArchiveStatus(app.db)
	if archiveError != nil {
		return status, archiveError
	}
	status.FilesTracked, status.EventsStored = archiveStatus.FilesTracked, archiveStatus.EventsStored
	status.FirstEventAt, status.LastEventAt = archiveStatus.FirstEventAt, archiveStatus.LastEventAt
	status.FirstEventGalactic = ed.ToGalactic(archiveStatus.FirstEventAt)
	status.LastEventGalactic = ed.ToGalactic(archiveStatus.LastEventAt)
	var currentSystemName sql.NullString
	_ = app.db.QueryRow(`SELECT system_name FROM journal_events WHERE event IN ('FSDJump','Location','CarrierJump') AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&currentSystemName)
	status.CurrentSystem = currentSystemName.String
	var rawJSON, eventName string
	if app.db.QueryRow(`SELECT event, raw_json FROM journal_events WHERE event IN ('LoadGame','ShipyardSwap') AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&eventName, &rawJSON) == nil {
		if eventName == "LoadGame" {
			var loadGame journal.LoadGameEvent
			if json.Unmarshal([]byte(rawJSON), &loadGame) == nil {
				status.CurrentShip = ed.ShipDisplayName(loadGame.Ship, loadGame.ShipLocalised)
				if loadGame.ShipName != "" {
					status.CurrentShip += " \u201c" + loadGame.ShipName + "\u201d"
				}
			}
		} else {
			var shipyardSwap journal.ShipyardSwapEvent
			if json.Unmarshal([]byte(rawJSON), &shipyardSwap) == nil {
				status.CurrentShip = ed.ShipDisplayName(shipyardSwap.ShipType, shipyardSwap.ShipTypeLocalised)
			}
		}
	}
	return status, nil
}

// RescanJournals starts a full idempotent catch-up scan in the background.
func (app *App) RescanJournals() error {
	app.dbMu.RLock()
	if readyError := app.ready(); readyError != nil {
		app.dbMu.RUnlock()
		return readyError
	}
	app.dbMu.RUnlock()
	go app.runScan()
	return nil
}

// RefreshGEC re-downloads the EDAstro Galactic Exploration Catalog.
func (app *App) RefreshGEC() (int, error) {
	if !app.gecRefreshing.CompareAndSwap(false, true) {
		return 0, errors.New("GEC refresh is already in progress")
	}
	defer app.gecRefreshing.Store(false)

	app.dbMu.RLock()
	if readyError := app.ready(); readyError != nil {
		app.dbMu.RUnlock()
		return 0, readyError
	}
	gecCat := app.gec
	app.dbMu.RUnlock()
	if gecCat == nil {
		return 0, errors.New("GEC catalog not initialized")
	}
	refreshCount, refreshError := gecCat.Refresh(app.ctx)
	if refreshError != nil {
		return 0, refreshError
	}
	app.populateMissingPOIs()
	app.changed("plan", 0)
	return refreshCount, nil
}

// OpenURL opens an http(s) URL in the user's default browser.
func (app *App) OpenURL(link string) error {
	parsedURL, parseError := url.Parse(link)
	if parseError != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return fmt.Errorf("refusing to open %q", link)
	}
	runtime.BrowserOpenURL(app.ctx, link)
	return nil
}

// ToGalactic converts an ISO-8601 UTC timestamp to "33XX-MM-DD HH:mm:ss UTC".
func (app *App) ToGalactic(iso string) string { return ed.ToGalactic(iso) }

// FromGalactic converts "33XX-MM-DD HH:mm:ss UTC" to an ISO-8601 UTC timestamp.
func (app *App) FromGalactic(gst string) (string, error) { return ed.FromGalactic(gst) }

// ============================================================================
// Modes
// ============================================================================

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
	byID := map[int64]modes.Mode{}
	for _, modeItem := range modesList {
		byID[modeItem.ID] = modeItem
	}
	active, activeError := modes.Active(app.db)
	if activeError != nil {
		return modeState, activeError
	}
	if active != nil {
		modeState.ActiveModeID, modeState.ActiveSince, modeState.ActiveSinceGalactic = active.ModeID, active.Start, ed.ToGalactic(active.Start)
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
		modeItem := byID[windowItem.ModeID]
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
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	updateError := modes.Update(app.db, id, name, description, color)
	if updateError == nil {
		app.changed("modes", 0)
	}
	return updateError
}

// DeleteMode deletes a custom mode and its windows.
func (app *App) DeleteMode(id int64) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	deleteError := modes.Delete(app.db, id)
	if deleteError == nil {
		app.changed("modes", 0)
	}
	return deleteError
}

// SwitchMode makes modeID the active mode from now (0 = stop tracking a mode).
func (app *App) SwitchMode(modeID int64) (ModesState, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(tx *sql.Tx) error { return modes.Switch(tx, modeID, "") })
	}(); err != nil {
		return ModesState{}, err
	}
	app.changed("modes", 0)
	return app.GetModesState()
}

// ModeWindowInput creates (ID 0) or edits a mode window. Start/End are
// ISO-8601 UTC; End "" means open-ended.
type ModeWindowInput struct {
	ID     int64  `json:"id"`
	ModeID int64  `json:"modeId"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

// SaveModeWindow writes a window retroactively; overlapping windows are
// trimmed/split so that windows never overlap.
func (app *App) SaveModeWindow(in ModeWindowInput) (ModesState, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(tx *sql.Tx) error {
			return modes.SetWindow(tx, in.ID, in.ModeID, in.Start, in.End)
		})
	}(); err != nil {
		return ModesState{}, err
	}
	app.changed("modes", 0)
	return app.GetModesState()
}

// DeleteModeWindow removes a window.
func (app *App) DeleteModeWindow(id int64) (ModesState, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return modes.DeleteWindow(app.db, id)
	}(); err != nil {
		return ModesState{}, err
	}
	app.changed("modes", 0)
	return app.GetModesState()
}

// ============================================================================
// Expeditions and planner rows
// ============================================================================

// ListExpeditions returns all expeditions (newest first).
func (app *App) ListExpeditions() ([]expedition.Expedition, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	expeditionsList, listError := expedition.List(app.db)
	if expeditionsList == nil {
		expeditionsList = []expedition.Expedition{}
	}
	return expeditionsList, listError
}

// CreateExpedition adds an expedition and makes it the active one.
func (app *App) CreateExpedition(name, description string) (expedition.Expedition, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.Expedition{}, readyError
	}
	createdExpedition, createError := expedition.Create(app.db, name, description)
	if createError != nil {
		return createdExpedition, createError
	}
	_ = db.SetSetting(app.db, settingActiveExp, strconv.FormatInt(createdExpedition.ID, 10))
	app.changed("plan", createdExpedition.ID)
	return createdExpedition, nil
}

// CloneExpedition duplicates an expedition (and its waypoints/region headers)
// into a new expedition in the planning state, and makes the clone active.
func (app *App) CloneExpedition(id int64) (expedition.Expedition, error) {
	var clonedExpedition expedition.Expedition
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var cloneError error
		clonedExpedition, cloneError = expedition.Clone(app.db, id)
		if cloneError != nil {
			return cloneError
		}
		return db.SetSetting(app.db, settingActiveExp, strconv.FormatInt(clonedExpedition.ID, 10))
	}(); err != nil {
		return clonedExpedition, err
	}
	go app.kickResolution(clonedExpedition.ID, false)
	app.changed("plan", clonedExpedition.ID)
	return clonedExpedition, nil
}

// UpdateExpedition renames an expedition.
func (app *App) UpdateExpedition(id int64, name, description string) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	updateError := expedition.UpdateMeta(app.db, id, name, description)
	if updateError == nil {
		app.changed("plan", id)
	}
	return updateError
}

// DeleteExpedition deletes an expedition plan (journal history is kept).
func (app *App) DeleteExpedition(id int64) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	if deleteError := expedition.Delete(app.db, id); deleteError != nil {
		return deleteError
	}
	if app.activeExpeditionID() == id {
		_ = db.SetSetting(app.db, settingActiveExp, "")
	}
	app.changed("plan", id)
	return nil
}

// SetActiveExpedition selects the expedition shown in the planner.
func (app *App) SetActiveExpedition(id int64) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.SetSetting(app.db, settingActiveExp, strconv.FormatInt(id, 10))
	}(); err != nil {
		return err
	}
	go app.kickResolution(id, false)
	return nil
}

// GetPlan returns the planner sheet for an expedition.
func (app *App) GetPlan(id int64) (expedition.PlanView, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.PlanView{}, readyError
	}
	return expedition.BuildPlan(app.db, id, app.gecLookup)
}

// gecLookup searches the GEC catalog for POIs matching a system. Caller must hold app.dbMu.RLock.
func (app *App) gecLookup(name string, addr int64) *expedition.GECRef {
	if app.gec == nil {
		return nil
	}
	pois := app.gec.Lookup(name, addr)
	if len(pois) == 0 {
		return nil
	}
	return &expedition.GECRef{ID: pois[0].ID, Name: pois[0].Name, Type: pois[0].Type, URL: pois[0].URL, Count: len(pois)}
}

// AddRow inserts a row after afterRowID (0 = append, -1 = top). rowType is
// "waypoint" or "region"; regionID is used for region headers.
func (app *App) AddRow(expeditionID, afterRowID int64, rowType string, regionID int) (expedition.Row, error) {
	var newRow expedition.Row
	var txError error
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		txError = db.WithTx(app.db, func(tx *sql.Tx) error {
			var insertError error
			newRow, insertError = expedition.InsertRow(tx, expeditionID, afterRowID, rowType, regionID)
			return insertError
		})
		return txError
	}(); err != nil {
		return newRow, err
	}
	app.changed("plan", expeditionID)
	return newRow, nil
}

// UpdateRow applies a partial update to a row. Changing the system name
// triggers asynchronous coordinate resolution and visit re-evaluation.
func (app *App) UpdateRow(patch expedition.RowPatch) error {
	var changed bool
	var rowItem expedition.Row
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var updateError error
		changed, updateError = expedition.UpdateRow(app.db, patch)
		if updateError != nil {
			return updateError
		}
		var getError error
		rowItem, getError = expedition.GetRow(app.db, patch.ID)
		if getError != nil {
			return getError
		}
		if changed {
			if rowItem.SystemName != "" && strings.TrimSpace(rowItem.POIName) == "" && app.gec != nil {
				if pois := app.gec.Lookup(rowItem.SystemName, rowItem.SystemAddress); len(pois) > 0 && strings.TrimSpace(pois[0].Name) != "" {
					poiName := html.UnescapeString(strings.TrimSpace(pois[0].Name))
					if _, execError := app.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, rowItem.ID); execError == nil {
						rowItem.POIName = poiName
					}
				}
			}
		}
		return nil
	}(); err != nil {
		return err
	}
	if changed {
		app.reevaluate(rowItem.ExpeditionID)
		if rowItem.SystemName != "" {
			app.resolveRowAsync(rowItem, true)
		}
	}
	app.changed("plan", rowItem.ExpeditionID)
	return nil
}

// SetLegMode assigns a mode to the leg arriving at rowID and updates the
// corresponding time window in mode_windows when the waypoint has been visited.
func (app *App) SetLegMode(rowID, modeID int64) error {
	var expeditionID int64
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(tx *sql.Tx) error {
			var setModeError error
			expeditionID, setModeError = expedition.SetLegMode(tx, rowID, modeID)
			return setModeError
		})
	}(); err != nil {
		return err
	}
	app.invalidateAllTimeCache()
	app.changed("modes", expeditionID)
	return nil
}

// SetActiveLegMode sets the mode for the current in-progress leg (since the
// latest visited waypoint).
func (app *App) SetActiveLegMode(expeditionID, modeID int64) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(tx *sql.Tx) error {
			return expedition.SetActiveLegMode(tx, expeditionID, modeID)
		})
	}(); err != nil {
		return err
	}
	app.invalidateAllTimeCache()
	app.changed("modes", expeditionID)
	return nil
}

// RetryResolve re-runs coordinate resolution for a waypoint.
func (app *App) RetryResolve(rowID int64) error {
	var rowItem expedition.Row
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var getError error
		rowItem, getError = expedition.GetRow(app.db, rowID)
		if getError != nil {
			return getError
		}
		if rowItem.SystemName == "" {
			return nil
		}
		_, _ = app.db.Exec(`DELETE FROM system_cache WHERE name_key = ? AND source = 'notfound'`, strings.ToLower(rowItem.SystemName))
		return nil
	}(); err != nil {
		return err
	}
	if rowItem.SystemName != "" {
		app.resolveRowAsync(rowItem, true)
	}
	return nil
}

// DeleteRow removes a row.
func (app *App) DeleteRow(rowID int64) error {
	var expeditionID int64
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, func(tx *sql.Tx) error {
			var deleteRowError error
			expeditionID, deleteRowError = expedition.DeleteRow(tx, rowID)
			return deleteRowError
		})
	}(); err != nil {
		return err
	}
	app.reevaluate(expeditionID)
	app.changed("plan", expeditionID)
	return nil
}

// MoveRow moves a row to the zero-based index toIndex.
func (app *App) MoveRow(rowID int64, toIndex int) error {
	var expeditionID int64
	var rowItem expedition.Row
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		var getError error
		rowItem, getError = expedition.GetRow(app.db, rowID)
		if getError != nil {
			return getError
		}
		return db.WithTx(app.db, func(tx *sql.Tx) error {
			var moveRowError error
			expeditionID, moveRowError = expedition.MoveRow(tx, rowID, toIndex)
			return moveRowError
		})
	}(); err != nil {
		return err
	}
	if rowItem.RowType == expedition.RowWaypoint {
		app.reevaluate(expeditionID)
	}
	app.changed("plan", expeditionID)
	return nil
}

// RegionOptions is the prioritised region dropdown content.
type RegionOptions struct {
	AnchorID          int               `json:"anchorId"`
	AnchorName        string            `json:"anchorName"`
	AnchorIsDefault   bool              `json:"anchorIsDefault"`
	SuggestedRegionID int               `json:"suggestedRegionId"` // region of the next waypoint's coordinates
	Options           []ed.RegionOption `json:"options"`
}

// GetRegionOptions returns regions ordered for a Region Header dropdown.
// rowID is either the header being edited (editing = true) or the row after
// which a new header will be inserted (0 = append at end, -1 = top).
func (app *App) GetRegionOptions(expeditionID, rowID int64, editing bool) (RegionOptions, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return RegionOptions{}, readyError
	}
	rows, listRowsError := expedition.ListRows(app.db, expeditionID)
	if listRowsError != nil {
		return RegionOptions{}, listRowsError
	}
	// cut = index of the first row *after* the header position.
	cut := len(rows)
	switch {
	case rowID == expedition.InsertAtTop:
		cut = 0
	case rowID > 0:
		for rowIndex, rowItem := range rows {
			if rowItem.ID == rowID {
				if editing {
					cut = rowIndex
				} else {
					cut = rowIndex + 1
				}
				break
			}
		}
	}
	anchor := 0
	for rowIndex := 0; rowIndex < cut && rowIndex < len(rows); rowIndex++ {
		if rows[rowIndex].RowType == expedition.RowRegion && rows[rowIndex].RegionID > 0 {
			anchor = rows[rowIndex].RegionID
		}
	}
	out := RegionOptions{AnchorIsDefault: anchor == 0}
	if anchor == 0 {
		anchor = ed.InnerOrionSpurID
	}
	regionRecord, _ := ed.RegionByID(anchor)
	out.AnchorID, out.AnchorName = anchor, regionRecord.Name
	out.Options = ed.PrioritizedRegions(anchor)
	start := cut
	if editing {
		start = cut + 1
	}
	for rowIndex := start; rowIndex < len(rows); rowIndex++ {
		if rows[rowIndex].IsWaypoint() && rows[rowIndex].HasCoords {
			if foundRegion, ok := ed.RegionAt(rows[rowIndex].X, rows[rowIndex].Y, rows[rowIndex].Z); ok {
				out.SuggestedRegionID = foundRegion.ID
			}
			break
		}
	}
	return out, nil
}

// StartCandidates lists recorded visits to Waypoint #1.
type StartCandidates struct {
	SystemName string               `json:"systemName"`
	Visits     []expedition.Arrival `json:"visits"`
}

// GetStartCandidates returns every recorded arrival at Waypoint #1 (newest first).
func (app *App) GetStartCandidates(expeditionID int64) (StartCandidates, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return StartCandidates{}, readyError
	}
	systemName, visits, startCandidatesError := expedition.StartCandidates(app.db, expeditionID)
	if visits == nil {
		visits = []expedition.Arrival{}
	}
	return StartCandidates{SystemName: systemName, Visits: visits}, startCandidatesError
}

func (app *App) lifecycle(expeditionID int64, action func(transaction *sql.Tx) error) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		return db.WithTx(app.db, action)
	}(); err != nil {
		return err
	}
	app.reevaluate(expeditionID)
	app.changed("plan", expeditionID)
	return nil
}

// StartExpedition starts the expedition at the given visit timestamp
// (ISO-8601 UTC) and retroactively replays history from that moment.
func (app *App) StartExpedition(expeditionID int64, timestamp string) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Start(transaction, expeditionID, timestamp) })
}

// StartExpeditionOnArrival activates the expedition and waits for the next
// arrival at Waypoint #1 to set the start timestamp.
func (app *App) StartExpeditionOnArrival(expeditionID int64) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Arm(transaction, expeditionID) })
}

// CompleteExpedition ends the expedition now.
func (app *App) CompleteExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Complete(transaction, expeditionID, "") })
}

// ReopenExpedition makes a completed expedition active again.
func (app *App) ReopenExpedition(expeditionID int64) error {
	return app.lifecycle(expeditionID, func(transaction *sql.Tx) error { return expedition.Reopen(transaction, expeditionID) })
}

// GetWaypointDetails returns the expandable drawer content for a waypoint.
func (app *App) GetWaypointDetails(rowID int64) (expedition.WaypointDetails, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.WaypointDetails{}, readyError
	}
	return expedition.Details(app.db, rowID)
}

// ============================================================================
// Stats sidebar
// ============================================================================

// SidebarStats is the content of the Stats Sidebar.
type SidebarStats struct {
	ExpeditionID        int64         `json:"expeditionId"`
	Status              string        `json:"status"`
	Started             bool          `json:"started"`
	Start               string        `json:"start"`
	End                 string        `json:"end"`
	StartGalactic       string        `json:"startGalactic"`
	EndGalactic         string        `json:"endGalactic"`
	EndIsNow            bool          `json:"endIsNow"`
	Duration            string        `json:"duration"`
	CareerStart         string        `json:"careerStart"`
	CareerStartGalactic string        `json:"careerStartGalactic"`
	CareerDuration      string        `json:"careerDuration"`
	FilterActive        bool          `json:"filterActive"`
	Live                stats.Metrics `json:"live"`        // from journals (mode-filtered)
	Adjustments         stats.Metrics `json:"adjustments"` // manual
	Expedition          stats.Metrics `json:"expedition"`  // live + adjustments
	AllJournals         stats.Metrics `json:"allJournals"` // all ingested journal events (mode-filtered)
	BaselineEnabled     bool          `json:"baselineEnabled"`
	BaselineSource      string        `json:"baselineSource"`
	Baseline            stats.Metrics `json:"baseline"`
	Lifetime            stats.Metrics `json:"lifetime"` // baseline + expedition
	Realization         stats.RealizationBreakdown `json:"realization"`
}

// GetSidebarStats computes the sidebar for an expedition with mode filters.
func (app *App) GetSidebarStats(expeditionID int64, filter stats.Filter) (SidebarStats, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return SidebarStats{}, readyError
	}
	expeditionRecord, getExpeditionError := expedition.Get(app.db, expeditionID)
	if getExpeditionError != nil {
		return SidebarStats{}, getExpeditionError
	}
	sidebar := SidebarStats{
		ExpeditionID:    expeditionRecord.ID,
		Status:          expeditionRecord.Status,
		Started:         expeditionRecord.StartTimestamp != "",
		Start:           expeditionRecord.StartTimestamp,
		End:             expeditionRecord.EndTimestamp,
		FilterActive:    filter.Enabled,
		Adjustments:     expeditionRecord.Adjustments,
		BaselineEnabled: expeditionRecord.BaselineEnabled,
		BaselineSource:  expeditionRecord.BaselineSource,
		Baseline:        expeditionRecord.Baseline,
	}
	statsEngine := stats.NewEngine(app.db)
	// Mode filters apply only to the current expedition, never to All Time.
	// All-time personal carrier data comes directly from Frontier's lifetime Statistics.
	if allTimeMetrics, allTimeMetricsError := app.getAllTimeMetrics(); allTimeMetricsError == nil {
		sidebar.AllJournals = allTimeMetrics
	}

	// Career metrics across all ingested journals for active commanders (populated at all times).
	var careerStart string
	var careerStartGalactic string

	app.careerStartMu.Lock()
	if app.careerStartValid {
		careerStart = app.careerStartTimestamp
		careerStartGalactic = app.careerStartGalactic
		app.careerStartMu.Unlock()
	} else {
		app.careerStartMu.Unlock()

		var earliestSeenTimestamp sql.NullString
		_ = app.db.QueryRow(`SELECT MIN(commanders.first_seen) FROM commanders WHERE commanders.enabled = 1 AND commanders.first_seen IS NOT NULL AND commanders.first_seen != ''`).Scan(&earliestSeenTimestamp)
		if !earliestSeenTimestamp.Valid || earliestSeenTimestamp.String == "" {
			_ = app.db.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&earliestSeenTimestamp)
		}

		app.careerStartMu.Lock()
		if !app.careerStartValid {
			if earliestSeenTimestamp.Valid && earliestSeenTimestamp.String != "" {
				app.careerStartTimestamp = earliestSeenTimestamp.String
				app.careerStartGalactic = ed.ToGalactic(earliestSeenTimestamp.String)
			} else {
				app.careerStartTimestamp = ""
				app.careerStartGalactic = ""
			}
			app.careerStartValid = true
		}
		careerStart = app.careerStartTimestamp
		careerStartGalactic = app.careerStartGalactic
		app.careerStartMu.Unlock()
	}

	if careerStart != "" {
		sidebar.CareerStart = careerStart
		sidebar.CareerStartGalactic = careerStartGalactic
		sidebar.CareerDuration = expedition.Elapsed(careerStart, "")
	}
	if sidebar.Started {
		if sidebar.End == "" {
			sidebar.EndIsNow = true
			sidebar.EndGalactic = ed.TimeToGalactic(timeNow())
		} else {
			sidebar.EndGalactic = ed.ToGalactic(sidebar.End)
		}
		sidebar.StartGalactic = ed.ToGalactic(sidebar.Start)
		sidebar.Duration = expedition.Elapsed(sidebar.Start, sidebar.End)

		filterJSON, _ := json.Marshal(filter)
		cacheKey := fmt.Sprintf("%d|%s|%s|%s", expeditionID, expeditionRecord.StartTimestamp, expeditionRecord.EndTimestamp, string(filterJSON))

		app.liveStatsMu.Lock()
		cachedMetrics, isCached := app.liveStatsCache[cacheKey]
		app.liveStatsMu.Unlock()

		var liveMetrics stats.Metrics
		if isCached {
			liveMetrics = cachedMetrics
		} else {
			var computeError error
			liveMetrics, computeError = statsEngine.Compute(stats.Range{After: expeditionRecord.StartTimestamp, Until: expeditionRecord.EndTimestamp}, &filter)
			if computeError != nil {
				return sidebar, computeError
			}
			// Carrier operations metrics (carrier jumps, carrier distance, and tritium) for the expedition
			// come exclusively from the Spansh / manual route logger (expeditionRecord.Adjustments) to prevent duplication.
			// Non-operation personal navigation metrics (CarrierPassengerLy and total DistanceLy)
			// are preserved directly from journal events when the commander is aboard.
			liveMetrics.CarrierJumps = 0
			liveMetrics.CarrierDistanceLy = 0
			liveMetrics.TritiumUsed = nil

			app.liveStatsMu.Lock()
			app.liveStatsCache[cacheKey] = liveMetrics
			app.liveStatsMu.Unlock()
		}

		sidebar.Live = liveMetrics
		sidebar.Expedition = liveMetrics.Add(expeditionRecord.Adjustments)
		sidebar.Realization, _ = statsEngine.ComputeRealization(stats.Range{After: expeditionRecord.StartTimestamp, Until: expeditionRecord.EndTimestamp}, &filter)
	} else {
		sidebar.Realization, _ = statsEngine.ComputeRealization(stats.Range{}, &filter)
	}
	return sidebar, nil
}
