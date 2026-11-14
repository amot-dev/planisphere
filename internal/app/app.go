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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/gec"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
	"planisphere/internal/systems"
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
	Scope        string `json:"scope"` // journal | plan | modes | settings | adjustments
	ExpeditionID int64  `json:"expeditionId"`
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

// Setting keys.
const (
	settingJournalDir           = "journal_dir"
	settingJournalDebounceMs    = "journal_debounce_ms"
	settingOnline               = "online_lookups"
	settingActiveExpedition     = "active_expedition_id"
	settingLastScanAt           = "last_scan_at"
	settingSetupCompleted       = "setup_completed"
	settingTritiumPricePerTonne = "tritium_price_per_tonne"
	gecRefreshInterval          = 7 * 24 * time.Hour
)

var errorNotReady = errors.New("database is not available — check the startup error in Settings")

// App is the Wails-bound application service.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	dbMu            sync.RWMutex
	db              *sql.DB
	dbPath          string
	debugMode       bool
	debugTempDBPath string
	startupDone     chan struct{}
	startupOnce     sync.Once
	ingester        *journal.Ingester
	watcher         *journal.Watcher
	resolver        *systems.Resolver
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

	allTimeMu               sync.Mutex
	allTimeValid            bool
	allTimeMetrics          stats.Metrics
	allTimeRealizationValid bool
	allTimeRealization      stats.RealizationBreakdown

	liveStatsMu    sync.Mutex
	liveStatsCache map[string]cachedLiveStats
	liveStatsGen   uint64

	// Career start timestamp caching fields protected by careerStartMu.
	careerStartMu        sync.Mutex
	careerStartTimestamp string
	careerStartGalactic  string
	careerStartValid     bool
	careerStartGen       uint64

	// Latest CarrierStats caching fields protected by carrierStatsMu.
	carrierStatsMu        sync.Mutex
	carrierStatsEvent     journal.CarrierStatsEvent
	carrierStatsTimestamp string
	carrierStatsFound     bool
	carrierStatsValid     bool
	carrierStatsGen       uint64

	// Latest GitHub release version check fields protected by versionCheckMu.
	versionCheckMu     sync.Mutex
	latestVersion      string
	versionCheckFailed bool
	versionChecked     bool

	// emit is runtime.EventsEmit, replaceable for tests.
	emit func(event string, data ...any)
}

type cachedLiveStats struct {
	metrics     stats.Metrics
	realization stats.RealizationBreakdown
}

// New creates the application service.
func New() *App {
	return &App{
		debugMode:            version.Debug,
		resolving:            map[int64]bool{},
		resolvingExpeditions: map[int64]bool{},
		resolutionSemaphore:  make(chan struct{}, 1),
		liveStatsCache:       map[string]cachedLiveStats{},
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

	app.dbMu.Lock()
	defer app.dbMu.Unlock()

	if app.watcher != nil {
		app.watcher.Stop()
	}
	if app.db != nil {
		_ = app.db.Close()
	}
	app.db, app.dbPath = dbConn, dbPath
	app.ingester = journal.NewIngester(dbConn)
	app.watcher = journal.NewWatcher(app.ingester, app.onJournalBatch)
	app.watcher.SetDebounceDelay(time.Duration(app.journalDebounceMs()) * time.Millisecond)
	app.resolver = systems.NewResolver(dbConn, app.onlineEnabled)
	app.gec = gec.NewCatalog(dbConn)
	stats.InvalidateLedgerCache()
	app.invalidateAllTimeCache()
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
	return app.setupCompleted()
}

func (app *App) startConfiguredWatcher() {
	watcher, configuredJournalDir := func() (*journal.Watcher, string) {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if app.ctx != nil && app.ctx.Err() != nil {
			return nil, ""
		}
		return app.watcher, app.journalDir()
	}()
	if watcher != nil {
		watcher.Start(app.ctx, configuredJournalDir)
	}
}

func (app *App) prepareInitialState() {
	if !app.isSetupCompleted() {
		return
	}
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

func (app *App) changed(scope string, expeditionID int64) {
	if scope == "modes" || scope == "journal" {
		app.invalidateLiveStatsCache()
	}
	app.emit(EventDataChanged, DataChanged{Scope: scope, ExpeditionID: expeditionID})
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

// journalDir returns the configured or default journal directory. Caller must hold app.dbMu.RLock.
func (app *App) journalDir() string {
	if app.db == nil {
		return journal.DefaultJournalDir()
	}
	settingValue, _ := db.GetSetting(app.db, settingJournalDir)
	if strings.TrimSpace(settingValue) == "" {
		return journal.DefaultJournalDir()
	}
	return settingValue
}

// onlineEnabled reports whether online lookups are enabled. Caller must hold app.dbMu.RLock.
func (app *App) onlineEnabled() bool {
	if app.db == nil {
		return false
	}
	settingValue, _ := db.GetSetting(app.db, settingOnline)
	return settingValue != "0"
}

// activeExpeditionID returns the currently active expedition ID. Caller must hold app.dbMu.RLock.
func (app *App) activeExpeditionID() int64 {
	if app.db == nil {
		return 0
	}
	settingValue, _ := db.GetSetting(app.db, settingActiveExpedition)
	var expeditionID int64
	fmt.Sscan(settingValue, &expeditionID)
	return expeditionID
}

// setupCompleted reports whether initial onboarding setup has finished. Caller must hold app.dbMu.RLock.
func (app *App) setupCompleted() bool {
	if app.db == nil {
		return false
	}
	settingValue, _ := db.GetSetting(app.db, settingSetupCompleted)
	return settingValue == "1"
}

// journalDebounceMs returns the configured or default live journal file-watcher
// debounce delay in milliseconds, clamped to [MinDebounceDelay, MaxDebounceDelay].
// Caller must hold app.dbMu.RLock or app.dbMu.Lock.
func (app *App) journalDebounceMs() int {
	defaultDebounceMilliseconds := int(journal.DefaultDebounceDelay / time.Millisecond)
	if app.db == nil {
		return defaultDebounceMilliseconds
	}
	settingValue, getError := db.GetSetting(app.db, settingJournalDebounceMs)
	if getError != nil || strings.TrimSpace(settingValue) == "" {
		return defaultDebounceMilliseconds
	}
	parsedMilliseconds, parseError := strconv.Atoi(strings.TrimSpace(settingValue))
	if parseError != nil {
		return defaultDebounceMilliseconds
	}
	clampedDuration := journal.ClampDebounceDelay(time.Duration(parsedMilliseconds) * time.Millisecond)
	return int(clampedDuration / time.Millisecond)
}

// tritiumPricePerTonne returns the configured or default Tritium cost in credits
// per tonne. Caller must hold app.dbMu.RLock or app.dbMu.Lock.
func (app *App) tritiumPricePerTonne() int64 {
	if app.db == nil {
		return ed.DefaultTritiumPricePerTonne
	}
	settingValue, getError := db.GetSetting(app.db, settingTritiumPricePerTonne)
	if getError != nil || strings.TrimSpace(settingValue) == "" {
		return ed.DefaultTritiumPricePerTonne
	}
	parsedPrice, parseError := strconv.ParseInt(strings.TrimSpace(settingValue), 10, 64)
	if parseError != nil || parsedPrice < 0 {
		return ed.DefaultTritiumPricePerTonne
	}
	return parsedPrice
}
