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
	Scope        string `json:"scope"` // journal | plan | modes | settings
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

	db       *sql.DB
	dbPath   string
	ingester *journal.Ingester
	watcher  *journal.Watcher
	resolver *systems.Resolver
	gec      *gec.Catalog

	scanning    atomic.Bool
	lastErrorMu sync.Mutex
	lastError   string
	resolveMu   sync.Mutex
	resolving  map[int64]bool
	resolveSem chan struct{}

	allTimeMu      sync.Mutex
	allTimeValid   bool
	allTimeMetrics stats.Metrics

	// emit is runtime.EventsEmit, replaceable for tests.
	emit func(event string, data ...any)
}

// New creates the application service.
func New() *App {
	return &App{resolving: map[int64]bool{}, resolveSem: make(chan struct{}, 1)}
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
	if !app.setupCompleted() {
		return
	}
	app.runScan()
	app.watcher.Start(app.ctx, app.journalDir())
	app.maybeRefreshGEC()
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

func (app *App) journalDir() string {
	settingValue, _ := db.GetSetting(app.db, settingJournalDir)
	if strings.TrimSpace(settingValue) == "" {
		return journal.DefaultJournalDir()
	}
	return settingValue
}

func (app *App) onlineEnabled() bool {
	settingValue, _ := db.GetSetting(app.db, settingOnline)
	return settingValue != "0"
}

func (app *App) activeExpeditionID() int64 {
	settingValue, _ := db.GetSetting(app.db, settingActiveExp)
	var expeditionID int64
	fmt.Sscan(settingValue, &expeditionID)
	return expeditionID
}

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
	dir := app.journalDir()
	scanResult, scanError := app.ingester.ScanDir(dir)
	if scanError != nil {
		app.setError(scanError.Error())
	} else if len(scanResult.Errors) > 0 {
		app.setError(fmt.Sprintf("%d journal lines could not be parsed (first: %s)", len(scanResult.Errors), scanResult.Errors[0]))
	} else {
		app.setError("")
	}
	_ = db.SetSetting(app.db, settingLastScanAt, ed.NowISO())
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

func (app *App) invalidateAllTimeCache() {
	app.allTimeMu.Lock()
	app.allTimeValid = false
	app.allTimeMu.Unlock()
}

func (app *App) getAllTimeMetrics() (stats.Metrics, error) {
	app.allTimeMu.Lock()
	defer app.allTimeMu.Unlock()
	if app.allTimeValid {
		return app.allTimeMetrics, nil
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
	expeditionIDs, getIDsError := expedition.ActiveExpeditionIDs(app.db)
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
	txError := db.WithTx(app.db, func(transaction *sql.Tx) error {
		_, reevalError := expedition.Reevaluate(transaction, expeditionID)
		return reevalError
	})
	if txError != nil {
		log.Printf("reevaluate expedition %d: %v", expeditionID, txError)
	}
}

func (app *App) maybeRefreshGEC() {
	if !app.onlineEnabled() {
		return
	}
	lastFetched := app.gec.LastFetched()
	if lastFetched != "" {
		if parsedTime, parseError := ed.ParseISO(lastFetched); parseError == nil && time.Since(parsedTime) < gecRefreshInterval && app.gec.Count() > 0 {
			return
		}
	}
	if _, refreshError := app.gec.Refresh(app.ctx); refreshError != nil {
		log.Printf("gec refresh: %v", refreshError)
		return
	}
	app.populateMissingPOIs()
	app.changed("plan", 0)
}

// kickResolution resolves coordinates for waypoints of an expedition that
// don't have them yet. allowRetry also retries rows previously marked
// not-found (journal lookup first, so a visit fills them in offline).
func (app *App) kickResolution(expeditionID int64, allowRetry bool) {
	if expeditionID == 0 {
		return
	}
	rows, listError := expedition.ListRows(app.db, expeditionID)
	if listError != nil {
		return
	}
	for _, rowItem := range rows {
		if !rowItem.IsWaypoint() || rowItem.SystemName == "" || rowItem.HasCoords {
			continue
		}
		switch rowItem.CoordsSource {
		case "":
			app.resolveRowAsync(rowItem, true)
		case "notfound", "error":
			// Only retry against the local journal (cheap) unless asked.
			app.resolveRowAsync(rowItem, allowRetry)
		}
	}
}

func (app *App) resolveRowAsync(rowItem expedition.Row, online bool) {
	app.resolveMu.Lock()
	if app.resolving[rowItem.ID] {
		app.resolveMu.Unlock()
		return
	}
	app.resolving[rowItem.ID] = true
	app.resolveMu.Unlock()

	go func() {
		defer func() {
			app.resolveMu.Lock()
			delete(app.resolving, rowItem.ID)
			app.resolveMu.Unlock()
		}()
		var systemInfo *systems.Info
		var lookupError error
		if online {
			app.resolveSem <- struct{}{}
			systemInfo, lookupError = app.resolver.Resolve(app.ctx, rowItem.SystemName)
			<-app.resolveSem
		} else {
			systemInfo, lookupError = systems.FromJournal(app.db, rowItem.SystemName)
			if systemInfo == nil && lookupError == nil {
				return
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
		if setCoordsError := expedition.SetRowCoords(app.db, rowItem.ID, rowItem.SystemName, systemAddress, edsmID, positionCoords, source); setCoordsError != nil {
			log.Printf("store coords: %v", setCoordsError)
			return
		}
		// Populate POI name from EDAstro GEC/GMP if the waypoint does not already have one set
		var curPOIName string
		_ = app.db.QueryRow(`SELECT poi_name FROM expedition_rows WHERE id = ?`, rowItem.ID).Scan(&curPOIName)
		if strings.TrimSpace(curPOIName) == "" {
			if matchedPOIs := app.gec.Lookup(rowItem.SystemName, systemAddress); len(matchedPOIs) > 0 && strings.TrimSpace(matchedPOIs[0].Name) != "" {
				poiName := html.UnescapeString(strings.TrimSpace(matchedPOIs[0].Name))
				_, _ = app.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, rowItem.ID)
			}
		}
		app.reevaluate(rowItem.ExpeditionID)
		app.changed("plan", rowItem.ExpeditionID)
	}()
}

func (app *App) populateMissingPOIs() {
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
