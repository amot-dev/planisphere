// Package app exposes the backend to the Wails frontend. Every exported method
// on *App is bound and gets an auto-generated TypeScript wrapper in
// frontend/wailsjs/go/app/App.{js,d.ts}.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/core/db"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/sources/edsm"
	"planisphere/internal/sources/gec"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/settings"
	"planisphere/internal/version"
)

// Frontend event names.
const (
	EventDataChanged = "data:changed" // payload: DataChanged
	EventIngest      = "ingest:status"
	EventEDDProgress = "edd:progress" // payload: EDDImportProgress
)

// DataChanged tells the frontend which data to refresh.
type DataChanged struct {
	Scope        string   `json:"scope"`             // journal | plan | modes | settings | adjustments
	Domains      []string `json:"domains,omitempty"` // ingest | stats | plan | modes | settings | expeditions
	ExpeditionID int64    `json:"expeditionId"`
}

// EDDImportProgress reports live progress while importing historical events from EDDiscovery.
type EDDImportProgress struct {
	Active  bool   `json:"active"`
	Stage   string `json:"stage"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Percent int    `json:"percent"`
	Files   int    `json:"files"`
}

const gecRefreshInterval = 7 * 24 * time.Hour

var errorNotReady = errors.New("database is not available — check the startup error in Config")

// App is the Wails-bound application service.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	ingestMu        sync.RWMutex
	dbMu            sync.RWMutex
	db              *sql.DB
	dbPath          string
	debugMode       bool
	debugTempDBPath string
	startupDone     chan struct{}
	startupOnce     sync.Once
	ingester        *journal.Ingester
	watcher         *journal.Watcher
	resolver        *edsm.Resolver
	gec             *gec.Catalog
	gecRefreshing   atomic.Bool

	backgroundMu sync.Mutex
	shuttingDown bool
	backgroundWg sync.WaitGroup

	scanning             atomic.Bool
	lastErrorMu          sync.Mutex
	lastError            string
	resolveMu            sync.Mutex
	resolving            map[int64]bool
	resolvingExpeditions map[int64]bool
	resolutionSemaphore  chan struct{}

	bus               *projection.Bus
	recoveredJumps    *projections.RecoveredJumps
	systemArrivals    *projections.SystemArrivals
	systemVisitCounts *projections.SystemVisitCounts
	shipHistory       *projections.ShipHistory
	carrierState      *projections.CarrierState
	salesLedger       *realization.SalesLedger
	careerStats       *stats.CareerStats
	timeRangeStats    *stats.TimeRangeStats

	// Latest GitHub release version check fields protected by versionCheckMu.
	versionCheckMu     sync.Mutex
	latestVersion      string
	versionCheckFailed bool
	versionChecked     bool

	// emit is runtime.EventsEmit, replaceable for tests.
	emit func(event string, data ...any)
}

// New creates the application service.
func New() *App {
	return &App{
		debugMode:            version.Debug,
		resolving:            map[int64]bool{},
		resolvingExpeditions: map[int64]bool{},
		resolutionSemaphore:  make(chan struct{}, 1),
	}
}

var timeNow = func() time.Time { return time.Now().UTC() }

// StartupHook returns the Wails OnStartup callback. Lifecycle hooks are
// package-level functions (not methods) so Wails does not bind them to JS.
func StartupHook(app *App) func(ctx context.Context) {
	app.startupDone = make(chan struct{})
	return app.startup
}

// ShutdownHook returns the Wails OnShutdown callback.
func ShutdownHook(app *App) func(ctx context.Context) { return app.shutdown }

func (app *App) markStartupComplete() {
	app.startupOnce.Do(func() {
		if app.startupDone != nil {
			close(app.startupDone)
		}
	})
}

// spawnBackground launches task in a tracked goroutine unless shutdown has
// already begun, preventing WaitGroup Add calls from racing with Wait.
func (app *App) spawnBackground(task func()) bool {
	app.backgroundMu.Lock()
	if app.shuttingDown {
		app.backgroundMu.Unlock()
		return false
	}
	app.backgroundWg.Add(1)
	app.backgroundMu.Unlock()

	go func() {
		defer app.backgroundWg.Done()
		task()
	}()
	return true
}

// waitStartupLocked waits on app.startupDone for asynchronous Wails OnStartup
// database initialization and initial state preparation to finish. Caller must
// hold app.dbMu.RLock, which is temporarily released while waiting so startup
// can acquire app.dbMu.Lock.
func (app *App) waitStartupLocked() {
	if app.startupDone != nil {
		select {
		case <-app.startupDone:
		default:
			startupDone := app.startupDone
			app.dbMu.RUnlock()
			<-startupDone
			app.dbMu.RLock()
		}
	}
}

// ready checks if the database is available. Caller must hold app.dbMu.RLock.
func (app *App) ready() error {
	app.waitStartupLocked()
	if app.db == nil {
		if startupError := app.getError(); startupError != "" {
			return fmt.Errorf("database is not available: %s", startupError)
		}
		return errorNotReady
	}
	return nil
}

func (app *App) startup(ctx context.Context) {
	if app.emit == nil {
		app.emit = func(event string, data ...any) { runtime.EventsEmit(ctx, event, data...) }
	}
	initError := app.initialize(ctx, os.Getenv("ED_PLANNER_DB"))
	if initError != nil {
		log.Printf("startup: %v", initError)
		app.setError(initError.Error())
		app.markStartupComplete()
		return
	}
	app.prepareInitialState()
	app.markStartupComplete()
	app.spawnBackground(app.backgroundStart)
}

// openTargetDatabase opens dbPath directly in production mode, or creates and
// wires an isolated temporary debug snapshot when isDebugMode is active for a
// non-memory database, cleaning up any superseded debug snapshot afterward.
func (app *App) openTargetDatabase(dbPath string, isDebugMode bool, logSnapshot bool) error {
	targetPath := dbPath
	if isDebugMode && dbPath != ":memory:" {
		snapshotPath, snapshotError := db.CreateDebugSnapshot(dbPath)
		if snapshotError != nil {
			return fmt.Errorf("create debug database snapshot: %w", snapshotError)
		}
		if logSnapshot {
			log.Printf("debug mode: snapshotted %s -> %s", dbPath, snapshotPath)
		}
		targetPath = snapshotPath
	}
	if openError := app.openAndWireDB(targetPath); openError != nil {
		if isDebugMode && targetPath != dbPath {
			db.RemoveDebugSnapshot(targetPath)
		}
		return openError
	}
	if isDebugMode && targetPath != dbPath {
		app.dbMu.Lock()
		previousTempPath := app.debugTempDBPath
		app.debugTempDBPath = targetPath
		app.dbMu.Unlock()
		if previousTempPath != "" && previousTempPath != targetPath {
			db.RemoveDebugSnapshot(previousTempPath)
		}
	}
	return nil
}

// initialize opens the database and wires up services. dbPath "" uses the configured or default path.
func (app *App) initialize(ctx context.Context, dbPath string) error {
	app.dbMu.Lock()
	app.ctx, app.cancel = context.WithCancel(ctx)
	if app.emit == nil {
		app.emit = func(string, ...any) {}
	}
	app.dbMu.Unlock()
	if dbPath == "" {
		configuredPath, resolveError := db.ResolveConfiguredPath()
		if resolveError != nil {
			return fmt.Errorf("resolve configured database path: %w", resolveError)
		}
		dbPath = configuredPath
	}
	return app.openTargetDatabase(dbPath, app.debugMode, true)
}

func (app *App) openAndWireDB(dbPath string) error {
	dbConn, openError := db.Open(dbPath)
	if openError != nil {
		return fmt.Errorf("open database %s: %w", dbPath, openError)
	}

	app.ingestMu.Lock()
	defer app.ingestMu.Unlock()

	// Release dbMu before stopping the watcher and closing the previous database
	// because active watcher callbacks acquire dbMu.RLock.
	app.dbMu.RLock()
	previousWatcher := app.watcher
	app.dbMu.RUnlock()
	if previousWatcher != nil {
		previousWatcher.Stop()
	}

	app.dbMu.Lock()
	remainingWatcher := app.watcher
	previousDB := app.db
	app.db, app.dbPath = dbConn, dbPath
	app.ingester = journal.NewIngester(dbConn)
	app.watcher = journal.NewWatcher(app.ingester, app.onJournalBatch)
	app.watcher.SetDebounceDelay(time.Duration(settings.JournalDebounceMs(app.db)) * time.Millisecond)
	app.resolver = edsm.NewResolver(dbConn, func() bool {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if app.db != dbConn {
			return false
		}
		return settings.OnlineLookups(dbConn)
	})
	app.gec = gec.NewCatalog(dbConn)
	app.wireProjections(dbConn)
	app.dbMu.Unlock()

	if remainingWatcher != nil && remainingWatcher != previousWatcher {
		remainingWatcher.Stop()
	}
	if previousDB != nil {
		_ = previousDB.Close()
	}
	return nil
}

func (app *App) shutdown(ctx context.Context) {
	app.markStartupComplete()

	// Gate new background tasks before waiting so WaitGroup.Add cannot race Wait.
	app.backgroundMu.Lock()
	app.shuttingDown = true
	app.backgroundMu.Unlock()

	// Release dbMu before stopping the watcher and awaiting workers because
	// active ingestion and resolution tasks acquire dbMu.RLock.
	app.dbMu.RLock()
	activeWatcher := app.watcher
	cancelFunc := app.cancel
	app.dbMu.RUnlock()

	if cancelFunc != nil {
		cancelFunc()
	}
	if activeWatcher != nil {
		activeWatcher.Stop()
	}

	app.backgroundWg.Wait()

	app.dbMu.Lock()
	remainingWatcher := app.watcher
	app.watcher = nil
	if app.cancel != nil {
		app.cancel()
	}
	databaseToClose := app.db
	app.db = nil
	tempDBPath := app.debugTempDBPath
	app.debugTempDBPath = ""
	app.dbMu.Unlock()

	if remainingWatcher != nil {
		remainingWatcher.Stop()
	}
	if databaseToClose != nil {
		_ = databaseToClose.Close()
	}
	if tempDBPath != "" {
		db.RemoveDebugSnapshot(tempDBPath)
	}
}

func (app *App) isSetupCompleted() bool {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return false
	}
	return settings.SetupCompleted(app.db)
}

func (app *App) startConfiguredWatcher() {
	watcher, configuredJournalDir := func() (*journal.Watcher, string) {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if app.db == nil || (app.ctx != nil && app.ctx.Err() != nil) {
			return nil, ""
		}
		return app.watcher, settings.JournalDir(app.db, journal.DefaultJournalDir())
	}()
	if watcher != nil {
		watcher.Start(app.ctx, configuredJournalDir)
	}
}

// prepareInitialState runs before markStartupComplete opens startupDone so the
// initial frontend boot wave reads reconciled expedition progress and pre-warmed
// telemetry caches without racing a concurrent background stats computation.
func (app *App) prepareInitialState() {
	if !app.isSetupCompleted() {
		return
	}
	// When runScan ingests new events it already calls reevaluateActive; still
	// re-evaluate on zero-new-event boots to reconcile active expeditions after
	// external database edits or schema upgrades.
	if !app.runScan() {
		app.reevaluateActive()
	}
	app.startConfiguredWatcher()
	app.warmInitialStats()
}

func (app *App) backgroundStart() {
	app.spawnBackground(app.checkLatestVersionOnStartup)
	if !app.isSetupCompleted() {
		return
	}
	app.maybeRefreshGEC()
	app.spawnBackground(app.maybeCatchUpEDD)
}

func (app *App) setError(errorMessage string) {
	app.lastErrorMu.Lock()
	app.lastError = errorMessage
	app.lastErrorMu.Unlock()
}

func (app *App) getError() string {
	app.lastErrorMu.Lock()
	defer app.lastErrorMu.Unlock()
	return app.lastError
}

func (app *App) emitEDDProgress(active bool, current, total, files int, stage string) {
	percentageProgress := 0
	if total > 0 {
		percentageProgress = (current * 100) / total
		if percentageProgress > 100 {
			percentageProgress = 100
		}
	}
	app.emit(EventEDDProgress, EDDImportProgress{
		Active:  active,
		Stage:   stage,
		Current: current,
		Total:   total,
		Percent: percentageProgress,
		Files:   files,
	})
}

// These database path helpers keep internal/core/db imports isolated to app.go.
func (app *App) defaultDatabasePath() (string, error) {
	return db.DefaultPath()
}

func (app *App) defaultDatabaseFilename() string {
	return db.DefaultDBFileName
}

func (app *App) saveConfiguredDatabasePath(dbPath string) error {
	return db.SaveConfiguredPath(dbPath)
}

func dirExists(path string) bool {
	fileInfo, statError := os.Stat(path)
	return statError == nil && fileInfo.IsDir()
}

func fileExists(path string) bool {
	fileInfo, statError := os.Stat(path)
	return statError == nil && !fileInfo.IsDir()
}
