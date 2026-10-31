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

	scanning   atomic.Bool
	lastErrMu  sync.Mutex
	lastErr    string
	resolveMu  sync.Mutex
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
func StartupHook(a *App) func(ctx context.Context) { return a.startup }

// ShutdownHook returns the Wails OnShutdown callback.
func ShutdownHook(a *App) func(ctx context.Context) { return a.shutdown }

func (a *App) startup(ctx context.Context) {
	a.emit = func(event string, data ...any) { runtime.EventsEmit(ctx, event, data...) }
	if err := a.initialize(ctx, os.Getenv("ED_PLANNER_DB")); err != nil {
		log.Printf("startup: %v", err)
		a.setErr(err.Error())
		return
	}
	go a.backgroundStart()
}

// initialize opens the database and wires up services. dbPath "" uses the configured or default path.
func (a *App) initialize(ctx context.Context, dbPath string) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	if a.emit == nil {
		a.emit = func(string, ...any) {}
	}
	if dbPath == "" {
		p, err := db.ResolveConfiguredPath()
		if err != nil {
			return err
		}
		dbPath = p
	}
	return a.openAndWireDB(dbPath)
}

func (a *App) openAndWireDB(dbPath string) error {
	conn, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database %s: %w", dbPath, err)
	}
	if a.watcher != nil {
		a.watcher.Stop()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
	a.db, a.dbPath = conn, dbPath
	a.ingester = journal.NewIngester(conn)
	a.watcher = journal.NewWatcher(a.ingester, a.onJournalBatch)
	a.resolver = systems.NewResolver(conn, a.onlineEnabled)
	a.gec = gec.NewCatalog(conn)
	a.invalidateAllTimeCache()
	return nil
}

func (a *App) shutdown(ctx context.Context) {
	if a.watcher != nil {
		a.watcher.Stop()
	}
	if a.cancel != nil {
		a.cancel()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
}

func (a *App) backgroundStart() {
	if !a.setupCompleted() {
		return
	}
	a.runScan()
	a.watcher.Start(a.ctx, a.journalDir())
	a.maybeRefreshGEC()
}

func (a *App) setErr(msg string) {
	a.lastErrMu.Lock()
	a.lastErr = msg
	a.lastErrMu.Unlock()
}

func (a *App) getErr() string {
	a.lastErrMu.Lock()
	defer a.lastErrMu.Unlock()
	return a.lastErr
}

func (a *App) changed(scope string, expID int64) {
	a.emit(EventDataChanged, DataChanged{Scope: scope, ExpeditionID: expID})
}

func (a *App) emitEDDProgress(active bool, cur, tot, files int, stage string) {
	pct := 0
	if tot > 0 {
		pct = (cur * 100) / tot
		if pct > 100 {
			pct = 100
		}
	}
	a.emit(EventEDDProgress, EDDImportProgress{
		Active:  active,
		Stage:   stage,
		Current: cur,
		Total:   tot,
		Percent: pct,
		Files:   files,
	})
}

func (a *App) journalDir() string {
	v, _ := db.GetSetting(a.db, settingJournalDir)
	if strings.TrimSpace(v) == "" {
		return journal.DefaultJournalDir()
	}
	return v
}

func (a *App) onlineEnabled() bool {
	v, _ := db.GetSetting(a.db, settingOnline)
	return v != "0"
}

func (a *App) activeExpeditionID() int64 {
	v, _ := db.GetSetting(a.db, settingActiveExp)
	var id int64
	fmt.Sscan(v, &id)
	return id
}

func (a *App) setupCompleted() bool {
	if a.db == nil {
		return false
	}
	v, _ := db.GetSetting(a.db, settingSetupCompleted)
	return v == "1"
}

// runScan performs the idempotent catch-up scan of the journal directory and
// re-evaluates active expeditions.
func (a *App) runScan() {
	if !a.scanning.CompareAndSwap(false, true) {
		return
	}
	defer a.scanning.Store(false)
	a.emit(EventIngest, nil)
	dir := a.journalDir()
	res, err := a.ingester.ScanDir(dir)
	if err != nil {
		a.setErr(err.Error())
	} else if len(res.Errors) > 0 {
		a.setErr(fmt.Sprintf("%d journal lines could not be parsed (first: %s)", len(res.Errors), res.Errors[0]))
	} else {
		a.setErr("")
	}
	_ = db.SetSetting(a.db, settingLastScanAt, ed.NowISO())
	if res.LinesRead > 0 {
		a.invalidateAllTimeCache()
	}
	a.reevaluateActive()
	a.emit(EventIngest, nil)
	if res.LinesRead > 0 {
		a.changed("journal", 0)
	}
}

// onJournalBatch runs after the live tailer stored new events.
func (a *App) onJournalBatch(res journal.Result) {
	a.invalidateAllTimeCache()
	a.reevaluateActive()
	a.changed("journal", 0)
}

func (a *App) invalidateAllTimeCache() {
	a.allTimeMu.Lock()
	a.allTimeValid = false
	a.allTimeMu.Unlock()
}

func (a *App) getAllTimeMetrics() (stats.Metrics, error) {
	a.allTimeMu.Lock()
	defer a.allTimeMu.Unlock()
	if a.allTimeValid {
		return a.allTimeMetrics, nil
	}
	eng := stats.NewEngine(a.db)
	all, err := eng.Compute(stats.Range{}, nil)
	if err != nil {
		return stats.Metrics{}, err
	}
	var totalJumps sql.NullInt64
	var distTravelled sql.NullFloat64
	_ = a.db.QueryRow(`SELECT json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_TOTAL_JUMPS'), json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_DISTANCE_TRAVELLED')
		FROM journal_events WHERE event = 'Statistics' AND json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_TOTAL_JUMPS') IS NOT NULL AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC LIMIT 1`).Scan(&totalJumps, &distTravelled)
	if totalJumps.Valid && totalJumps.Int64 > 0 {
		all.CarrierJumps = int(totalJumps.Int64)
		all.CarrierDistanceLy = distTravelled.Float64
	}
	a.allTimeMetrics = all
	a.allTimeValid = true
	return all, nil
}

func (a *App) reevaluateActive() {
	ids, err := expedition.ActiveExpeditionIDs(a.db)
	if err != nil {
		log.Printf("reevaluate: %v", err)
		return
	}
	for _, id := range ids {
		a.reevaluate(id)
		a.kickResolution(id, false)
	}
}

func (a *App) reevaluate(expID int64) {
	err := db.WithTx(a.db, func(tx *sql.Tx) error {
		_, err := expedition.Reevaluate(tx, expID)
		return err
	})
	if err != nil {
		log.Printf("reevaluate expedition %d: %v", expID, err)
	}
}

func (a *App) maybeRefreshGEC() {
	if !a.onlineEnabled() {
		return
	}
	last := a.gec.LastFetched()
	if last != "" {
		if t, err := ed.ParseISO(last); err == nil && time.Since(t) < gecRefreshInterval && a.gec.Count() > 0 {
			return
		}
	}
	if _, err := a.gec.Refresh(a.ctx); err != nil {
		log.Printf("gec refresh: %v", err)
		return
	}
	a.populateMissingPOIs()
	a.changed("plan", 0)
}

// kickResolution resolves coordinates for waypoints of an expedition that
// don't have them yet. allowRetry also retries rows previously marked
// not-found (journal lookup first, so a visit fills them in offline).
func (a *App) kickResolution(expID int64, allowRetry bool) {
	if expID == 0 {
		return
	}
	rows, err := expedition.ListRows(a.db, expID)
	if err != nil {
		return
	}
	for _, r := range rows {
		if !r.IsWaypoint() || r.SystemName == "" || r.HasCoords {
			continue
		}
		switch r.CoordsSource {
		case "":
			a.resolveRowAsync(r, true)
		case "notfound", "error":
			// Only retry against the local journal (cheap) unless asked.
			a.resolveRowAsync(r, allowRetry)
		}
	}
}

func (a *App) resolveRowAsync(r expedition.Row, online bool) {
	a.resolveMu.Lock()
	if a.resolving[r.ID] {
		a.resolveMu.Unlock()
		return
	}
	a.resolving[r.ID] = true
	a.resolveMu.Unlock()

	go func() {
		defer func() {
			a.resolveMu.Lock()
			delete(a.resolving, r.ID)
			a.resolveMu.Unlock()
		}()
		var info *systems.Info
		var err error
		if online {
			a.resolveSem <- struct{}{}
			info, err = a.resolver.Resolve(a.ctx, r.SystemName)
			<-a.resolveSem
		} else {
			info, err = systems.FromJournal(a.db, r.SystemName)
			if info == nil && err == nil {
				return
			}
		}
		source := "error"
		var addr, edsmID int64
		var pos *ed.Vec3
		switch {
		case info != nil && info.Pos != nil:
			source, addr, edsmID, pos = info.Source, info.SystemAddress, info.EDSMID, info.Pos
		case info != nil:
			source = "notfound"
		case err != nil && strings.Contains(err.Error(), systems.ErrNotFound.Error()):
			source = "notfound"
		}
		if err := expedition.SetRowCoords(a.db, r.ID, r.SystemName, addr, edsmID, pos, source); err != nil {
			log.Printf("store coords: %v", err)
			return
		}
		// Populate POI name from EDAstro GEC/GMP if the waypoint does not already have one set
		var curPOI string
		_ = a.db.QueryRow(`SELECT poi_name FROM expedition_rows WHERE id = ?`, r.ID).Scan(&curPOI)
		if strings.TrimSpace(curPOI) == "" {
			if ps := a.gec.Lookup(r.SystemName, addr); len(ps) > 0 && strings.TrimSpace(ps[0].Name) != "" {
				poiName := html.UnescapeString(strings.TrimSpace(ps[0].Name))
				_, _ = a.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, r.ID)
			}
		}
		a.reevaluate(r.ExpeditionID)
		a.changed("plan", r.ExpeditionID)
	}()
}

func (a *App) populateMissingPOIs() {
	rows, err := a.db.Query(`SELECT id, system_name, COALESCE(system_address, 0) FROM expedition_rows WHERE row_type = 'waypoint' AND (poi_name IS NULL OR TRIM(poi_name) = '') AND system_name != ''`)
	if err != nil {
		return
	}
	defer rows.Close()
	type item struct {
		id   int64
		sys  string
		addr int64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.sys, &it.addr); err == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		if ps := a.gec.Lookup(it.sys, it.addr); len(ps) > 0 && strings.TrimSpace(ps[0].Name) != "" {
			poiName := html.UnescapeString(strings.TrimSpace(ps[0].Name))
			_, _ = a.db.Exec(`UPDATE expedition_rows SET poi_name = ? WHERE id = ? AND (poi_name IS NULL OR TRIM(poi_name) = '')`, poiName, it.id)
		}
	}
}
