package app

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/eddiscovery"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

// runScan performs the idempotent catch-up scan of the journal directory,
// re-evaluates active expeditions when new events are ingested, and returns
// whether any new events were stored.
func (app *App) runScan() bool {
	if !app.scanning.CompareAndSwap(false, true) {
		return false
	}
	defer app.scanning.Store(false)
	app.emit(EventIngest, nil)
	app.dbMu.RLock()
	dir := app.journalDir()
	ingester := app.ingester
	database := app.db
	app.dbMu.RUnlock()
	if ingester == nil || database == nil {
		app.scanning.Store(false)
		app.emit(EventIngest, nil)
		return false
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
	hasNewEvents := scanResult.NewEvents > 0
	if scanResult.HasRealizationEvents {
		stats.InvalidateLedgerCache()
	}
	if hasNewEvents {
		app.invalidateAllTimeCache()
		app.reevaluateActive()
	}
	app.scanning.Store(false)
	app.emit(EventIngest, nil)
	if hasNewEvents {
		app.changed("journal", 0)
	}
	return hasNewEvents
}

// warmInitialStats pre-populates the all-time and active expedition statistics
// caches during startup so initial frontend queries resolve immediately.
func (app *App) warmInitialStats() {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return
	}
	_, _ = app.getAllTimeMetrics()
	_, _ = app.getAllTimeRealization()
	_, _ = app.resolveCareerStart()
	activeID := app.activeExpeditionID()
	if activeID == 0 {
		if expeditionsList, listError := expedition.List(app.db); listError == nil && len(expeditionsList) > 0 {
			activeID = expeditionsList[0].ID
		}
	}
	if activeID > 0 {
		if expeditionRecord, getError := expedition.Get(app.db, activeID); getError == nil && expeditionRecord.StartTimestamp != "" {
			_, _, _ = app.computeExpeditionLiveStats(expeditionRecord, stats.Filter{}, stats.NewEngine(app.db))
		}
	}
}

// onJournalBatch runs after the live tailer stored new events.
func (app *App) onJournalBatch(scanResult journal.Result) {
	if scanResult.HasRealizationEvents {
		stats.InvalidateLedgerCache()
	}
	app.invalidateAllTimeCache()
	app.reevaluateActive()
	app.changed("journal", 0)
}

func (app *App) invalidateLiveStatsCache() {
	app.liveStatsMu.Lock()
	clear(app.liveStatsCache)
	app.liveStatsGen++
	app.liveStatsMu.Unlock()
}

func (app *App) invalidateAllTimeCache() {
	app.allTimeMu.Lock()
	app.allTimeValid = false
	app.allTimeRealizationValid = false
	app.allTimeMu.Unlock()
	app.careerStartMu.Lock()
	app.careerStartTimestamp = ""
	app.careerStartGalactic = ""
	app.careerStartValid = false
	app.careerStartGen++
	app.careerStartMu.Unlock()
	app.carrierStatsMu.Lock()
	app.carrierStatsEvent = journal.CarrierStatsEvent{}
	app.carrierStatsTimestamp = ""
	app.carrierStatsFound = false
	app.carrierStatsValid = false
	app.carrierStatsGen++
	app.carrierStatsMu.Unlock()
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
	var distanceTravelled sql.NullFloat64
	_ = app.db.QueryRow(`SELECT json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_TOTAL_JUMPS'), json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_DISTANCE_TRAVELLED')
		FROM journal_events WHERE event = 'Statistics' AND json_extract(raw_json, '$.FLEETCARRIER.FLEETCARRIER_TOTAL_JUMPS') IS NOT NULL AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC LIMIT 1`).Scan(&totalJumps, &distanceTravelled)
	if totalJumps.Valid && totalJumps.Int64 > 0 {
		allMetrics.CarrierJumps = int(totalJumps.Int64)
		allMetrics.CarrierDistanceLy = distanceTravelled.Float64
	}
	app.allTimeMetrics = allMetrics
	app.allTimeValid = true
	return allMetrics, nil
}

// getAllTimeRealization computes or retrieves cached lifetime realization across all journals. Caller must hold app.dbMu.RLock.
func (app *App) getAllTimeRealization() (stats.RealizationBreakdown, error) {
	app.allTimeMu.Lock()
	defer app.allTimeMu.Unlock()
	if app.allTimeRealizationValid {
		return app.allTimeRealization, nil
	}
	if app.db == nil {
		return stats.RealizationBreakdown{}, errorNotReady
	}
	statsEngine := stats.NewEngine(app.db)
	allRealization, computeError := statsEngine.ComputeRealization(stats.Range{}, nil)
	if computeError != nil {
		return stats.RealizationBreakdown{}, computeError
	}
	app.allTimeRealization = allRealization
	app.allTimeRealizationValid = true
	return allRealization, nil
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
	for _, expeditionID := range expeditionIDs {
		app.reevaluate(expeditionID)
		app.kickResolution(expeditionID, false)
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
	gecCatalog := app.gec
	app.dbMu.RUnlock()
	if !online || gecCatalog == nil {
		return
	}
	lastFetched := gecCatalog.LastFetched()
	if lastFetched != "" {
		if parsedTime, parseError := ed.ParseISO(lastFetched); parseError == nil && time.Since(parsedTime) < gecRefreshInterval && gecCatalog.Count() > 0 {
			return
		}
	}
	if _, refreshError := gecCatalog.Refresh(app.ctx); refreshError != nil {
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
		eddPath = eddiscovery.DiscoverDBPath()
		if eddPath == "" {
			eddPath = eddiscovery.DefaultDBPath()
		}
	}
	if !journal.FileExists(eddPath) {
		return
	}
	defer app.emitEDDProgress(false, 0, 0, 0, "")
	_, importError := eddiscovery.Import(app.ctx, ingester, eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if importError == nil {
		stats.InvalidateLedgerCache()
		_ = db.SetSetting(database, "edd_history_realization_v6", "1")
		app.invalidateAllTimeCache()
		app.reevaluateActive()
		app.changed("journal", 0)
	}
}

