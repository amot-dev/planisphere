package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/eddiscovery"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/settings"
	"planisphere/internal/version"
)

// Settings is the user-configurable application state.
type Settings struct {
	SetupCompleted              bool               `json:"setupCompleted"`
	JournalDir                  string             `json:"journalDir"`
	DefaultJournalDir           string             `json:"defaultJournalDir"`
	JournalDirExists            bool               `json:"journalDirExists"`
	JournalDebounceMs           int                `json:"journalDebounceMs"`
	DefaultJournalDebounceMs    int                `json:"defaultJournalDebounceMs"`
	TritiumPricePerTonne        int64              `json:"tritiumPricePerTonne"`
	DefaultTritiumPricePerTonne int64              `json:"defaultTritiumPricePerTonne"`
	OnlineLookups               bool               `json:"onlineLookups"`
	ExternalLinkInara           string             `json:"externalLinkInara"`
	ExternalLinkEDSM            string             `json:"externalLinkEdsm"`
	ExternalLinkGEC             string             `json:"externalLinkGec"`
	DBPath                      string             `json:"dbPath"`
	DefaultDBPath               string             `json:"defaultDbPath"`
	ActiveExpeditionID          int64              `json:"activeExpeditionId"`
	GECCount                    int                `json:"gecCount"`
	GECFetchedAt                string             `json:"gecFetchedAt"`
	Commanders                  []replay.Commander `json:"commanders"`
	CareerBaselineScope         string             `json:"careerBaselineScope"`
	EDDiscoveryDBPath           string             `json:"edDiscoveryDbPath"`
	DefaultEDDiscoveryDBPath    string             `json:"defaultEdDiscoveryDbPath"`
	EDDiscoveryDBExists         bool               `json:"edDiscoveryDbExists"`
	LatestVersion               string             `json:"latestVersion"`
	VersionCheckFailed          bool               `json:"versionCheckFailed"`
}

// GetSettings returns the current settings.
func (app *App) GetSettings() (Settings, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return Settings{}, readyError
	}
	dir := settings.JournalDir(app.db, journal.DefaultJournalDir())
	commanders, listError := replay.ListCommanders(app.db)
	if listError != nil {
		return Settings{}, fmt.Errorf("list commanders: %w", listError)
	}
	if commanders == nil {
		commanders = []replay.Commander{}
	}
	defaultDBPath, _ := app.defaultDatabasePath()
	defaultEDDPath := eddiscovery.DefaultDBPath()
	discoveredEDDPath := eddiscovery.DiscoverDBPath()
	var gecCount int
	if app.gec != nil {
		gecCount = app.gec.Count()
	}
	gecFetched := settings.GECFetchedAt(app.db)
	latestVersion, versionCheckFailed := app.getVersionStatus()
	externalLinkInara, externalLinkEDSM, externalLinkGEC := settings.ExternalLinkModes(app.db)
	return Settings{
		SetupCompleted:              settings.SetupCompleted(app.db),
		JournalDir:                  dir,
		DefaultJournalDir:           journal.DefaultJournalDir(),
		JournalDirExists:            dirExists(dir),
		JournalDebounceMs:           settings.JournalDebounceMs(app.db),
		DefaultJournalDebounceMs:    int(reference.DefaultDebounceDelay / time.Millisecond),
		TritiumPricePerTonne:        settings.TritiumPricePerTonne(app.db),
		DefaultTritiumPricePerTonne: reference.DefaultTritiumPricePerTonne,
		OnlineLookups:               settings.OnlineLookups(app.db),
		ExternalLinkInara:           externalLinkInara,
		ExternalLinkEDSM:            externalLinkEDSM,
		ExternalLinkGEC:             externalLinkGEC,
		DBPath:                      app.dbPath,
		DefaultDBPath:               defaultDBPath,
		ActiveExpeditionID:          settings.ActiveExpeditionID(app.db),
		GECCount:                    gecCount,
		GECFetchedAt:                gecFetched,
		Commanders:                  commanders,
		CareerBaselineScope:         settings.CareerBaselineScope(app.db),
		EDDiscoveryDBPath:           discoveredEDDPath,
		DefaultEDDiscoveryDBPath:    defaultEDDPath,
		EDDiscoveryDBExists:         discoveredEDDPath != "" || fileExists(defaultEDDPath),
		LatestVersion:               latestVersion,
		VersionCheckFailed:          versionCheckFailed,
	}, nil
}

// BrowseDBPath opens a save-file dialog so the user can choose where the SQLite database file should be stored.
func (app *App) BrowseDBPath() (string, error) {
	defaultDir := ""
	defaultFilename := app.defaultDatabaseFilename()
	app.dbMu.RLock()
	app.waitStartupLocked()
	currentDBPath := app.dbPath
	dialogContext := app.contextLocked()
	app.dbMu.RUnlock()
	if currentDBPath != "" {
		defaultDir = filepath.Dir(currentDBPath)
		defaultFilename = filepath.Base(currentDBPath)
	}
	return runtime.SaveFileDialog(dialogContext, runtime.SaveDialogOptions{
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
		defaultPath, defaultPathError := app.defaultDatabasePath()
		if defaultPathError != nil {
			return Settings{}, defaultPathError
		}
		dbPath = defaultPath
	} else if dirExists(dbPath) {
		dbPath = filepath.Join(dbPath, app.defaultDatabaseFilename())
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
		if app.isSetupCompleted() {
			app.startConfiguredWatcher()
		}
	}
	if !isDebugMode {
		if saveError := app.saveConfiguredDatabasePath(dbPath); saveError != nil {
			return Settings{}, saveError
		}
	}
	app.publishApp(0, events.AppDatabaseSwitched)
	return app.GetSettings()
}

func (app *App) saveJournalDirSetting(dir string) error {
	return app.withReadyReadLock(func() error {
		dir = strings.TrimSpace(dir)
		if dir != "" && !dirExists(dir) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return settings.SetJournalDir(app.db, dir)
	})
}

func (app *App) scanAndStartWatcher() {
	app.runScan()
	app.startConfiguredWatcher()
}

// SetupConfigureJournals sets the journal directory, performs a synchronous
// initial scan of local .log files, starts the live watcher, and returns the
// resulting archive status (including oldest journal date and discovered commanders).
func (app *App) SetupConfigureJournals(dir string) (IngestStatus, error) {
	if err := app.saveJournalDirSetting(dir); err != nil {
		return IngestStatus{}, err
	}
	setupCompleted := app.isSetupCompleted()
	_, _, affectedTargets := app.executeJournalScan(false)
	app.startConfiguredWatcher()
	if setupCompleted {
		app.emitDataChanged(0, affectedTargets.Union(events.NewUITargetSet(events.UITargetIngest, events.UITargetSettings)))
	}
	return app.GetIngestStatus()
}

// CompleteSetup marks the first-time setup wizard as finished and triggers
// any deferred startup tasks (watcher and GEC catalog sync).
func (app *App) CompleteSetup() (Settings, error) {
	var (
		activeWatcher *journal.Watcher
		watchContext  context.Context
		configuredDir string
		shouldRefresh bool
	)
	if setupError := app.withReadyReadLock(func() error {
		if saveError := settings.SetSetupCompleted(app.db, true); saveError != nil {
			return saveError
		}
		if app.watcher != nil && (app.ctx == nil || app.ctx.Err() == nil) {
			activeWatcher, watchContext, configuredDir = app.watcher, app.contextLocked(), settings.JournalDir(app.db, journal.DefaultJournalDir())
		}
		if settings.OnlineLookups(app.db) && app.gec != nil && app.gec.Count() == 0 {
			shouldRefresh = true
		}
		return nil
	}); setupError != nil {
		return Settings{}, setupError
	}
	if activeWatcher != nil {
		activeWatcher.Start(watchContext, configuredDir)
	}
	if shouldRefresh {
		app.spawnBackground(app.maybeRefreshGEC)
	}
	app.publishApp(0, events.AppSettingsChanged)
	return app.GetSettings()
}

// SetCommanderEnabled enables or disables a commander in statistics and plans.
func (app *App) SetCommanderEnabled(name string, enabled bool) (Settings, error) {
	if toggleError := app.withReadyReadLock(func() error {
		return settings.SetCommanderEnabled(app.db, name, enabled)
	}); toggleError != nil {
		return Settings{}, toggleError
	}
	app.clearExportSnapshot()
	app.publishApp(0, events.AppCommanderFilterChanged)
	return app.GetSettings()
}

// BrowseEDDiscoveryDB opens a file picker to select an EDDUser.sqlite file.
func (app *App) BrowseEDDiscoveryDB() (string, error) {
	return runtime.OpenFileDialog(app.context(), runtime.OpenDialogOptions{
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
	importContext, readyError := app.readyContext()
	if readyError != nil {
		return 0, readyError
	}

	app.ingestMu.RLock()
	defer app.ingestMu.RUnlock()

	app.dbMu.RLock()
	database := app.db
	ingester := app.ingester
	app.dbMu.RUnlock()
	if database == nil || ingester == nil {
		return 0, errorNotReady
	}

	eddPath = strings.TrimSpace(eddPath)
	if eddPath == "" {
		eddPath = eddiscovery.DefaultDBPath()
	}
	defer app.emitEDDProgress(false, 0, 0, 0, "")

	importResult, importError := eddiscovery.Import(importContext, ingester, eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if importError != nil {
		return 0, importError
	}

	var markError error
	app.dbMu.RLock()
	if app.db == database {
		markError = settings.MarkEDDRecovered(app.db, eddPath)
	}
	app.dbMu.RUnlock()

	uiTargets := events.NewUITargetSet(events.UITargetIngest)
	if markError == nil {
		uiTargets = uiTargets.Union(events.NewUITargetSet(events.UITargetSettings))
	}
	affectedTargets := app.publishJournalBatch(importResult.Events, importResult.EarliestTimestamp, importResult.LatestTimestamp)
	app.emitDataChanged(0, affectedTargets.Union(uiTargets))
	if markError != nil {
		return 0, fmt.Errorf("mark EDDiscovery history recovered: %w", markError)
	}
	return importResult.LinesRead, nil
}

// SetJournalDir changes the journal directory, restarts the watcher and
// performs a catch-up scan in the background. Pass "" to restore the default.
func (app *App) SetJournalDir(dir string) (Settings, error) {
	if err := app.saveJournalDirSetting(dir); err != nil {
		return Settings{}, err
	}
	app.spawnBackground(app.scanAndStartWatcher)
	app.publishApp(0, events.AppSettingsChanged)
	return app.GetSettings()
}

// BrowseJournalDir opens a native directory picker and returns the choice ("" if cancelled).
func (app *App) BrowseJournalDir() (string, error) {
	app.dbMu.RLock()
	app.waitStartupLocked()
	defaultDir := journal.DefaultJournalDir()
	if app.db != nil {
		defaultDir = settings.JournalDir(app.db, defaultDir)
	}
	dialogContext := app.contextLocked()
	app.dbMu.RUnlock()
	return runtime.OpenDirectoryDialog(dialogContext, runtime.OpenDialogOptions{
		Title:            "Select Elite Dangerous Journal Directory",
		DefaultDirectory: defaultDir,
	})
}

// SetOnlineLookups toggles online EDSM/Spansh/GEC lookups.
func (app *App) SetOnlineLookups(enabled bool) (Settings, error) {
	if saveError := app.withReadyReadLock(func() error {
		return settings.SetOnlineLookups(app.db, enabled)
	}); saveError != nil {
		return Settings{}, saveError
	}
	if enabled {
		app.spawnBackground(app.maybeRefreshGEC)
	}
	app.publishApp(0, events.AppSettingsChanged)
	return app.GetSettings()
}

// SetJournalDebounceMs updates the live journal file-watcher debounce delay
// in milliseconds, clamped to [MinDebounceDelay, MaxDebounceDelay], and
// applies it immediately to the running watcher.
func (app *App) SetJournalDebounceMs(milliseconds int) (Settings, error) {
	clampedDuration := reference.ClampDebounceDelay(time.Duration(milliseconds) * time.Millisecond)

	var runningWatcher *journal.Watcher
	if saveError := app.withReadyReadLock(func() error {
		runningWatcher = app.watcher
		return settings.SetJournalDebounceMs(app.db, milliseconds)
	}); saveError != nil {
		return Settings{}, fmt.Errorf("persist journal debounce setting: %w", saveError)
	}

	if runningWatcher != nil {
		runningWatcher.SetDebounceDelay(clampedDuration)
	}
	app.publishApp(0, events.AppSettingsChanged)
	return app.GetSettings()
}

// SetTritiumPricePerTonne updates the Tritium cost per tonne in credits used for
// expedition Fleet Carrier cost estimates.
func (app *App) SetTritiumPricePerTonne(pricePerTonne int64) (Settings, error) {
	if saveError := app.withReadyReadLock(func() error {
		return settings.SetTritiumPricePerTonne(app.db, pricePerTonne)
	}); saveError != nil {
		return Settings{}, fmt.Errorf("persist tritium price setting: %w", saveError)
	}
	app.publishApp(0, events.AppSettingsChanged, events.AppAdjustmentsChanged)
	return app.GetSettings()
}

// SetExternalLinks updates the visibility modes for waypoint external star
// system links (Inara: "on"/"off", EDSM: "on"/"if_no_gec"/"off", GEC: "on"/"off").
func (app *App) SetExternalLinks(inaraMode, edsmMode, gecMode string) (Settings, error) {
	if saveError := app.withReadyReadLock(func() error {
		return settings.SetExternalLinkModes(app.db, inaraMode, edsmMode, gecMode)
	}); saveError != nil {
		return Settings{}, fmt.Errorf("persist external link settings: %w", saveError)
	}
	app.publishApp(0, events.AppSettingsChanged)
	return app.GetSettings()
}
