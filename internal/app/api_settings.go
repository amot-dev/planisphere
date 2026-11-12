package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/eddiscovery"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
	"planisphere/internal/version"
)

// Settings is the user-configurable application state.
type Settings struct {
	SetupCompleted           bool                `json:"setupCompleted"`
	JournalDir               string              `json:"journalDir"`
	DefaultJournalDir        string              `json:"defaultJournalDir"`
	JournalDirExists         bool                `json:"journalDirExists"`
	JournalDebounceMs        int                 `json:"journalDebounceMs"`
	DefaultJournalDebounceMs int                 `json:"defaultJournalDebounceMs"`
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
	defaultEDDPath := eddiscovery.DefaultDBPath()
	discoveredEDDPath := eddiscovery.DiscoverDBPath()
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
		JournalDebounceMs:        app.journalDebounceMs(),
		DefaultJournalDebounceMs: int(journal.DefaultDebounceDelay / time.Millisecond),
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
	app.waitStartupLocked()
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
// In debug mode, it creates and switches to an isolated temporary snapshot of
// the target database without modifying bootstrap.json.
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
	app.waitStartupLocked()
	currentPath := app.dbPath
	isDebugMode := app.debugMode
	app.dbMu.RUnlock()

	if dbPath != currentPath {
		if openError := app.openTargetDatabase(dbPath, isDebugMode, false); openError != nil {
			return Settings{}, openError
		}
		app.dbMu.RLock()
		setupDone := app.setupCompleted()
		watcher := app.watcher
		configuredJournalDir := app.journalDir()
		app.dbMu.RUnlock()
		if setupDone && watcher != nil {
			watcher.Start(app.ctx, configuredJournalDir)
		}
	}
	if !isDebugMode {
		if saveError := db.SaveConfiguredPath(dbPath); saveError != nil {
			return Settings{}, saveError
		}
	}
	app.changed("settings", 0)
	return app.GetSettings()
}

// SetupConfigureJournals sets the journal directory, performs a synchronous
// initial scan of local .log files, starts the live watcher, and returns the
// resulting archive status (including oldest journal date and discovered commanders).
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
	configuredJournalDir := app.journalDir()
	app.dbMu.RUnlock()
	if watcher != nil {
		watcher.Start(app.ctx, configuredJournalDir)
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
	stats.InvalidateLedgerCache()
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
	readyError := app.ready()
	ingester := app.ingester
	app.dbMu.RUnlock()

	if readyError != nil {
		return 0, readyError
	}
	if ingester == nil {
		return 0, errorNotReady
	}

	eddPath = strings.TrimSpace(eddPath)
	if eddPath == "" {
		eddPath = eddiscovery.DefaultDBPath()
	}
	defer app.emitEDDProgress(false, 0, 0, 0, "")

	importResult, importError := eddiscovery.Import(app.ctx, ingester, eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if importError != nil {
		return 0, importError
	}

	app.dbMu.RLock()
	if app.db != nil {
		_ = db.SetSetting(app.db, "edd_history_recovered_v1", "1")
		_ = db.SetSetting(app.db, "edd_db_path", eddPath)
		_ = db.SetSetting(app.db, "edd_history_realization_v6", "1")
	}
	app.dbMu.RUnlock()

	stats.InvalidateLedgerCache()
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
		configuredJournalDir := app.journalDir()
		app.dbMu.RUnlock()
		if watcher != nil {
			watcher.Start(app.ctx, configuredJournalDir)
		}
	}()
	return app.GetSettings()
}

// BrowseJournalDir opens a native directory picker and returns the choice ("" if cancelled).
func (app *App) BrowseJournalDir() (string, error) {
	app.dbMu.RLock()
	app.waitStartupLocked()
	defaultDir := app.journalDir()
	app.dbMu.RUnlock()
	return runtime.OpenDirectoryDialog(app.ctx, runtime.OpenDialogOptions{
		Title:            "Select Elite Dangerous journal folder",
		DefaultDirectory: defaultDir,
	})
}

// SetOnlineLookups enables or disables EDSM and EDAstro network requests.
func (app *App) SetOnlineLookups(enabled bool) (Settings, error) {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		settingValue := "1"
		if !enabled {
			settingValue = "0"
		}
		return db.SetSetting(app.db, settingOnline, settingValue)
	}(); err != nil {
		return Settings{}, err
	}
	if enabled {
		go app.maybeRefreshGEC()
	}
	return app.GetSettings()
}

// SetJournalDebounceMs updates the live journal file-watcher debounce delay
// in milliseconds, clamped to [MinDebounceDelay, MaxDebounceDelay], and
// applies it immediately to the running watcher.
func (app *App) SetJournalDebounceMs(milliseconds int) (Settings, error) {
	clampedDuration := journal.ClampDebounceDelay(time.Duration(milliseconds) * time.Millisecond)
	clampedMilliseconds := strconv.FormatInt(clampedDuration.Milliseconds(), 10)

	var runningWatcher *journal.Watcher
	if saveError := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if readyError := app.ready(); readyError != nil {
			return readyError
		}
		runningWatcher = app.watcher
		return db.SetSetting(app.db, settingJournalDebounceMs, clampedMilliseconds)
	}(); saveError != nil {
		return Settings{}, fmt.Errorf("persist journal debounce setting: %w", saveError)
	}

	if runningWatcher != nil {
		runningWatcher.SetDebounceDelay(clampedDuration)
	}
	app.changed("settings", 0)
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
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	readyError := app.ready()
	status := IngestStatus{Scanning: app.scanning.Load(), LastError: app.getError()}
	if readyError != nil {
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
	status.Scanning = app.scanning.Load()
	status.LastError = app.getError()
	return status, nil
}

// RescanJournals performs a full idempotent catch-up scan of the configured journal directory.
func (app *App) RescanJournals() error {
	app.dbMu.RLock()
	if readyError := app.ready(); readyError != nil {
		app.dbMu.RUnlock()
		return readyError
	}
	app.dbMu.RUnlock()
	app.runScan()
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
	gecCatalog := app.gec
	app.dbMu.RUnlock()
	if gecCatalog == nil {
		return 0, errors.New("GEC catalog not initialized")
	}
	refreshCount, refreshError := gecCatalog.Refresh(app.ctx)
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

