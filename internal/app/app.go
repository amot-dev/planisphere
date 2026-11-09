// Package app exposes the backend to the Wails frontend. Every exported method
// on *App is bound and gets an auto-generated TypeScript wrapper in
// frontend/wailsjs/go/app/App.{js,d.ts}.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/expedition"
	"planisphere/internal/gec"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
	"planisphere/internal/systems"
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
	settingJournalDir     = "journal_dir"
	settingOnline         = "online_lookups"
	settingActiveExp      = "active_expedition_id"
	settingLastScanAt     = "last_scan_at"
	settingSetupCompleted = "setup_completed"
	gecRefreshInterval    = 7 * 24 * time.Hour
)

// App is the Wails-bound application service.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	dbMu     sync.RWMutex
	db       *sql.DB
	dbPath   string
	ingester *journal.Ingester
	watcher  *journal.Watcher
	resolver *systems.Resolver
	gec                  *gec.Catalog
	gecRefreshing        atomic.Bool

	scanning             atomic.Bool
	lastErrorMu          sync.Mutex
	lastError            string
	resolveMu            sync.Mutex
	resolving            map[int64]bool
	resolvingExpeditions map[int64]bool
	resolveSem           chan struct{}

	allTimeMu      sync.Mutex
	allTimeValid   bool
	allTimeMetrics stats.Metrics

	liveStatsMu    sync.Mutex
	liveStatsCache map[string]stats.Metrics

	// Career start timestamp caching fields protected by careerStartMu.
	careerStartMu        sync.Mutex
	careerStartTimestamp string
	careerStartGalactic  string
	careerStartValid     bool

	// emit is runtime.EventsEmit, replaceable for tests.
	emit func(event string, data ...any)
}

// New creates the application service.
func New() *App {
	return &App{
		resolving:            map[int64]bool{},
		resolvingExpeditions: map[int64]bool{},
		resolveSem:           make(chan struct{}, 1),
		liveStatsCache:       map[string]stats.Metrics{},
	}
}

var timeNow = func() time.Time { return time.Now().UTC() }

// StartupHook returns the Wails OnStartup callback. Lifecycle hooks are
// package-level functions (not methods) so Wails does not bind them to JS.
func StartupHook(app *App) func(ctx context.Context) { return app.startup }

// ShutdownHook returns the Wails OnShutdown callback.
func ShutdownHook(app *App) func(ctx context.Context) { return app.shutdown }

func (app *App) startup(ctx context.Context) {
	app.emit = func(event string, data ...any) { runtime.EventsEmit(ctx, event, data...) }
	if initError := app.initialize(ctx, os.Getenv("ED_PLANNER_DB")); initError != nil {
		log.Printf("startup: %v", initError)
		app.setError(initError.Error())
		return
	}
	go app.backgroundStart()
}

// initialize opens the database and wires up services. dbPath "" uses the configured or default path.
func (app *App) initialize(ctx context.Context, dbPath string) error {
	app.ctx, app.cancel = context.WithCancel(ctx)
	if app.emit == nil {
		app.emit = func(string, ...any) {}
	}
	if dbPath == "" {
		configuredPath, resolveError := db.ResolveConfiguredPath()
		if resolveError != nil {
			return resolveError
		}
		dbPath = configuredPath
	}
	return app.openAndWireDB(dbPath)
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
	app.resolver = systems.NewResolver(dbConn, app.onlineEnabled)
	app.gec = gec.NewCatalog(dbConn)
	app.invalidateAllTimeCache()
	return nil
}

func (app *App) shutdown(ctx context.Context) {
	app.dbMu.Lock()
	defer app.dbMu.Unlock()

	if app.watcher != nil {
		app.watcher.Stop()
	}
	if app.cancel != nil {
		app.cancel()
	}
	if app.db != nil {
		_ = app.db.Close()
	}
}

func (app *App) backgroundStart() {
	app.dbMu.RLock()
	setupDone := app.setupCompleted()
	app.dbMu.RUnlock()
	if !setupDone {
		return
	}
	app.runScan()
	app.dbMu.RLock()
	watcher := app.watcher
	jDir := app.journalDir()
	app.dbMu.RUnlock()
	if watcher != nil {
		watcher.Start(app.ctx, jDir)
	}
	app.maybeRefreshGEC()
	go app.maybeCatchUpEDD()
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
	if scope == "modes" || scope == "journal" || scope == "plan" {
		app.invalidateLiveStatsCache()
	}
	app.emit(EventDataChanged, DataChanged{Scope: scope, ExpeditionID: expeditionID})
}

func (app *App) emitEDDProgress(active bool, current, total, files int, stage string) {
	pct := 0
	if total > 0 {
		pct = (current * 100) / total
		if pct > 100 {
			pct = 100
		}
	}
	app.emit(EventEDDProgress, EDDImportProgress{
		Active:  active,
		Stage:   stage,
		Current: current,
		Total:   total,
		Percent: pct,
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
	settingValue, _ := db.GetSetting(app.db, settingActiveExp)
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

// runScan performs the idempotent catch-up scan of the journal directory and
// re-evaluates active expeditions.
func (app *App) runScan() {
	if !app.scanning.CompareAndSwap(false, true) {
		return
	}
	defer app.scanning.Store(false)
	app.emit(EventIngest, nil)
	app.dbMu.RLock()
	dir := app.journalDir()
	ingester := app.ingester
	database := app.db
	app.dbMu.RUnlock()
	if ingester == nil || database == nil {
		return
	}
	scanResult, scanError := ingester.ScanDir(app.ctx, dir)
	if scanError != nil {
		app.setError(scanError.Error())
	} else if len(scanResult.Errors) > 0 {
		app.setError(fmt.Sprintf("%d journal lines could not be parsed (first: %s)", len(scanResult.Errors), scanResult.Errors[0]))
	} else {
		app.setError("")
	}
	app.dbMu.RLock()
	if app.db != nil {
		_ = db.SetSetting(app.db, settingLastScanAt, ed.NowISO())
	}
	app.dbMu.RUnlock()
	if scanResult.LinesRead > 0 {
		app.invalidateAllTimeCache()
	}
	app.reevaluateActive()
	app.emit(EventIngest, nil)
	if scanResult.LinesRead > 0 {
		app.changed("journal", 0)
	}
}

// onJournalBatch runs after the live tailer stored new events.
func (app *App) onJournalBatch(scanResult journal.Result) {
	app.invalidateAllTimeCache()
	app.reevaluateActive()
	app.changed("journal", 0)
}

func (app *App) invalidateLiveStatsCache() {
	app.liveStatsMu.Lock()
	clear(app.liveStatsCache)
	app.liveStatsMu.Unlock()
}

func (app *App) invalidateAllTimeCache() {
	app.allTimeMu.Lock()
	app.allTimeValid = false
	app.allTimeMu.Unlock()
	app.careerStartMu.Lock()
	app.careerStartValid = false
	app.careerStartMu.Unlock()
	app.invalidateLiveStatsCache()
	expedition.InvalidateShipCache()
}

// getAllTimeMetrics computes or retrieves cached lifetime statistics across all journals. Caller must hold app.dbMu.RLock.
func (app *App) getAllTimeMetrics() (stats.Metrics, error) {
	app.allTimeMu.Lock()
	defer app.allTimeMu.Unlock()
	if app.allTimeValid {
		return app.allTimeMetrics, nil
	}
	if app.db == nil {
		return stats.Metrics{}, errorNotReady
	}
	statsEngine := stats.NewEngine(app.db)
	allMetrics, computeError := statsEngine.Compute(stats.Range{}, nil)
	if computeError != nil {
		return stats.Metrics{}, computeError
	}
	var totalJumps sql.NullInt64
	var distTravelled sql.NullFloat64
	_ = app.db.QueryRow(`SELECT json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_TOTAL_JUMPS'), json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_DISTANCE_TRAVELLED')
		FROM journal_events WHERE event = 'Statistics' AND json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_TOTAL_JUMPS') IS NOT NULL AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC LIMIT 1`).Scan(&totalJumps, &distTravelled)
	if totalJumps.Valid && totalJumps.Int64 > 0 {
		allMetrics.CarrierJumps = int(totalJumps.Int64)
		allMetrics.CarrierDistanceLy = distTravelled.Float64
	}
	app.allTimeMetrics = allMetrics
	app.allTimeValid = true
	return allMetrics, nil
}

func (app *App) reevaluateActive() {
	app.dbMu.RLock()
	if app.db == nil {
		app.dbMu.RUnlock()
		return
	}
	expeditionIDs, getIDsError := expedition.ActiveExpeditionIDs(app.db)
	app.dbMu.RUnlock()
	if getIDsError != nil {
		log.Printf("reevaluate: %v", getIDsError)
		return
	}
	for _, id := range expeditionIDs {
		app.reevaluate(id)
		app.kickResolution(id, false)
	}
}

func (app *App) reevaluate(expeditionID int64) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return
	}
	txError := db.WithTx(app.db, func(transaction *sql.Tx) error {
		_, reevalError := expedition.Reevaluate(transaction, expeditionID)
		return reevalError
	})
	if txError != nil {
		log.Printf("reevaluate expedition %d: %v", expeditionID, txError)
	}
}

func (app *App) maybeRefreshGEC() {
	if !app.gecRefreshing.CompareAndSwap(false, true) {
		return
	}
	defer app.gecRefreshing.Store(false)

	app.dbMu.RLock()
	online := app.onlineEnabled()
	gecCat := app.gec
	app.dbMu.RUnlock()
	if !online {
		return
	}
	if gecCat == nil {
		return
	}
	lastFetched := gecCat.LastFetched()
	if lastFetched != "" {
		if parsedTime, parseError := ed.ParseISO(lastFetched); parseError == nil && time.Since(parsedTime) < gecRefreshInterval && gecCat.Count() > 0 {
			return
		}
	}
	if _, refreshError := gecCat.Refresh(app.ctx); refreshError != nil {
		log.Printf("gec refresh: %v", refreshError)
		return
	}
	app.populateMissingPOIs()
	app.changed("plan", 0)
}

func (app *App) maybeCatchUpEDD() {
	app.dbMu.RLock()
	database := app.db
	ingester := app.ingester
	app.dbMu.RUnlock()
	if database == nil || ingester == nil {
		return
	}
	recovered, _ := db.GetSetting(database, "edd_history_recovered_v1")
	if recovered != "1" {
		return
	}
	alreadyDone, _ := db.GetSetting(database, "edd_history_realization_v6")
	if alreadyDone == "1" {
		return
	}
	eddPath, _ := db.GetSetting(database, "edd_db_path")
	if eddPath == "" {
		eddPath = journal.DiscoverEDDiscoveryDBPath()
		if eddPath == "" {
			eddPath = journal.DefaultEDDiscoveryDBPath()
		}
	}
	if !journal.FileExists(eddPath) {
		return
	}
	defer app.emitEDDProgress(false, 0, 0, 0, "")
	_, err := ingester.ImportEDDiscoveryDB(app.ctx, eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if err == nil {
		_ = db.SetSetting(database, "edd_history_realization_v6", "1")
		app.invalidateAllTimeCache()
		app.reevaluateActive()
		app.changed("journal", 0)
	}
}

// kickResolution resolves coordinates for waypoints of an expedition that
// don't have them yet. allowRetry also retries rows previously marked
// not-found (journal lookup first, so a visit fills them in offline).
// Runs in a single sequential worker goroutine per expedition to avoid
// goroutine explosion and excessive database re-evaluation spam.
func (app *App) kickResolution(expeditionID int64, allowRetry bool) {
	if expeditionID == 0 {
		return
	}
	app.resolveMu.Lock()
	if app.resolvingExpeditions[expeditionID] {
		app.resolveMu.Unlock()
		return
	}
	app.resolvingExpeditions[expeditionID] = true
	app.resolveMu.Unlock()

	go func() {
		defer func() {
			app.resolveMu.Lock()
			delete(app.resolvingExpeditions, expeditionID)
			app.resolveMu.Unlock()
		}()

		app.dbMu.RLock()
		if app.db == nil {
			app.dbMu.RUnlock()
			return
		}
		rows, listError := expedition.ListRows(app.db, expeditionID)
		app.dbMu.RUnlock()
		if listError != nil {
			return
		}
		anyCoords := false
		anyUpdated := false
		lastEmit := time.Now()
		for _, rowItem := range rows {
			if app.ctx != nil {
				select {
				case <-app.ctx.Done():
					return
				default:
				}
			}
			if !rowItem.IsWaypoint() || rowItem.SystemName == "" || rowItem.HasCoords {
				continue
			}
			online := false
			switch rowItem.CoordsSource {
			case "":
				online = true
			case "notfound", "error":
				online = allowRetry
			default:
				continue
			}
			resolvedCoords, updated := app.resolveRowSync(rowItem, online)
			if updated {
				anyUpdated = true
				if resolvedCoords {
					anyCoords = true
				}
				if time.Since(lastEmit) > 1*time.Second {
					if anyCoords {
						app.reevaluate(expeditionID)
					}
					app.changed("plan", expeditionID)
					lastEmit = time.Now()
				}
			}
		}
		if anyUpdated {
			if anyCoords {
				app.reevaluate(expeditionID)
			}
			app.changed("plan", expeditionID)
		}
	}()
}

func (app *App) resolveRowSync(rowItem expedition.Row, online bool) (resolvedCoords bool, updated bool) {
	app.resolveMu.Lock()
	if app.resolving[rowItem.ID] {
		app.resolveMu.Unlock()
		return false, false
	}
	app.resolving[rowItem.ID] = true
	app.resolveMu.Unlock()

	defer func() {
		app.resolveMu.Lock()
		delete(app.resolving, rowItem.ID)
		app.resolveMu.Unlock()
	}()

	var systemInfo *systems.Info
	var lookupError error
	if online {
		if app.ctx != nil {
			select {
			case <-app.ctx.Done():
				return false, false
			case app.resolveSem <- struct{}{}:
			}
		} else {
			app.resolveSem <- struct{}{}
		}
		func() {
			defer func() {
				<-app.resolveSem
			}()
			app.dbMu.RLock()
			res := app.resolver
			app.dbMu.RUnlock()
			if res != nil {
				systemInfo, lookupError = res.Resolve(app.ctx, rowItem.SystemName)
			}
		}()
	} else {
		app.dbMu.RLock()
		database := app.db
		if database != nil {
			systemInfo, lookupError = systems.FromJournal(database, rowItem.SystemName)
		}
		app.dbMu.RUnlock()
		if systemInfo == nil && lookupError == nil {
			return false, false
		}
	}
	source := "error"
	var systemAddress, edsmID int64
	var positionCoords *ed.Vec3
	switch {
	case systemInfo != nil && systemInfo.Pos != nil:
		source, systemAddress, edsmID, positionCoords = systemInfo.Source, systemInfo.SystemAddress, systemInfo.EDSMID, systemInfo.Pos
	case systemInfo != nil:
		source = "notfound"
	case lookupError != nil && strings.Contains(lookupError.Error(), systems.ErrorNotFound.Error()):
		source = "notfound"
	}
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return false, false
	}
	if setCoordsError := expedition.SetRowCoords(app.db, rowItem.ID, rowItem.SystemName, systemAddress, edsmID, positionCoords, source); setCoordsError != nil {
		log.Printf("store coords: %v", setCoordsError)
		return false, false
	}
	// Populate POI name from EDAstro GEC/GMP if the waypoint does not already have one set
	var curPOIName string
	_ = app.db.QueryRow(`SELECT poi_name FROM expedition_rows WHERE id = ?`, rowItem.ID).Scan(&curPOIName)
	if strings.TrimSpace(curPOIName) == "" && app.gec != nil {
		if matchedPOIs := app.gec.Lookup(rowItem.SystemName, systemAddress); len(matchedPOIs) > 0 && strings.TrimSpace(matchedPOIs[0].Name) != "" {
			poiName := html.UnescapeString(strings.TrimSpace(matchedPOIs[0].Name))
			_, _ = app.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, rowItem.ID)
		}
	}
	return positionCoords != nil, true
}

func (app *App) resolveRowAsync(rowItem expedition.Row, online bool) {
	go func() {
		resolvedCoords, updated := app.resolveRowSync(rowItem, online)
		if updated {
			if resolvedCoords {
				app.reevaluate(rowItem.ExpeditionID)
			}
			app.changed("plan", rowItem.ExpeditionID)
		}
	}()
}

func (app *App) populateMissingPOIs() {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil || app.gec == nil {
		return
	}
	rows, queryError := app.db.Query(`SELECT id, system_name, COALESCE(system_address, 0) FROM expedition_rows WHERE row_type = 'waypoint' AND (poi_name IS NULL OR TRIM(poi_name) = '') AND system_name != ''`)
	if queryError != nil {
		return
	}
	defer rows.Close()
	type missingPOIItem struct {
		id            int64
		systemName    string
		systemAddress int64
	}
	var items []missingPOIItem
	for rows.Next() {
		var item missingPOIItem
		if scanError := rows.Scan(&item.id, &item.systemName, &item.systemAddress); scanError == nil {
			items = append(items, item)
		}
	}
	rows.Close()
	for _, item := range items {
		if matchedPOIs := app.gec.Lookup(item.systemName, item.systemAddress); len(matchedPOIs) > 0 && strings.TrimSpace(matchedPOIs[0].Name) != "" {
			poiName := html.UnescapeString(strings.TrimSpace(matchedPOIs[0].Name))
			_, _ = app.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, item.id)
		}
	}
}
