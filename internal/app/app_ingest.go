package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/eddiscovery"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

// runScan performs the idempotent catch-up scan of the journal directory,
// re-evaluates active expeditions when new events are ingested, and returns
// whether any new events were stored.
func (app *App) runScan() bool {
	hasNewEvents, didStartScan, affectedTargets := app.executeJournalScan()
	if !didStartScan {
		return false
	}
	app.emit(EventIngest, nil)
	if hasNewEvents {
		app.emitDataChanged("journal", 0, affectedTargets)
	}
	return hasNewEvents
}

func (app *App) executeJournalScan() (bool, bool, events.UITargetSet) {
	if !app.scanning.CompareAndSwap(false, true) {
		return false, false, 0
	}
	defer app.scanning.Store(false)
	app.emit(EventIngest, nil)
	app.ingestMu.RLock()
	defer app.ingestMu.RUnlock()
	app.dbMu.RLock()
	ingester := app.ingester
	database := app.db
	dir := ""
	if ingester != nil && database != nil {
		dir = settings.JournalDir(database, journal.DefaultJournalDir())
	}
	app.dbMu.RUnlock()
	if ingester == nil || database == nil {
		return false, true, 0
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
	if app.db == database {
		if settingError := settings.SetLastScanAt(database, reference.NowISO()); settingError != nil {
			log.Printf("persist last scan timestamp: %v", settingError)
		}
	}
	app.dbMu.RUnlock()
	hasNewEvents := scanResult.NewEvents > 0
	var affectedTargets events.UITargetSet
	if hasNewEvents {
		affectedTargets = app.publishJournalBatch(scanResult.Events, scanResult.EarliestTimestamp)
	}
	return hasNewEvents, true, affectedTargets
}

// warmInitialStats pre-populates the all-time and active expedition statistics
// caches during startup so initial frontend queries resolve immediately.
func (app *App) warmInitialStats() {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return
	}
	if app.careerStats != nil {
		if _, metricsError := app.careerStats.Get(); metricsError != nil {
			log.Printf("warm initial career stats: %v", metricsError)
		}
	}
	activeID := settings.ActiveExpeditionID(app.db)
	if activeID == 0 {
		expeditionsList, listError := expedition.List(app.db)
		if listError != nil {
			log.Printf("warm initial stats list expeditions: %v", listError)
		} else if len(expeditionsList) > 0 {
			activeID = expeditionsList[0].ID
		}
	}
	if activeID > 0 && app.timeRangeStats != nil {
		expeditionRecord, getError := expedition.Get(app.db, activeID)
		if getError != nil {
			if !errors.Is(getError, sql.ErrNoRows) {
				log.Printf("warm initial stats load expedition %d: %v", activeID, getError)
			}
		} else if expeditionRecord.StartTimestamp != "" {
			expeditionRange := stats.Range{After: expeditionRecord.StartTimestamp, Until: expeditionRecord.EndTimestamp}
			if _, statsError := app.timeRangeStats.Compute(expeditionRange, &stats.Filter{}); statsError != nil {
				log.Printf("warm initial stats compute expedition %d: %v", activeID, statsError)
			}
		}
	}
}

// onJournalBatch runs after the live tailer stored new events.
func (app *App) onJournalBatch(scanResult journal.Result) {
	affectedTargets := app.publishJournalBatch(scanResult.Events, scanResult.EarliestTimestamp)
	app.emitDataChanged("journal", 0, affectedTargets)
}

func (app *App) reevaluateActive() []int64 {
	app.dbMu.RLock()
	if app.db == nil {
		app.dbMu.RUnlock()
		return nil
	}
	expeditionIDs, getIDsError := expedition.ActiveExpeditionIDs(app.db)
	app.dbMu.RUnlock()
	if getIDsError != nil {
		log.Printf("reevaluate: %v", getIDsError)
		return nil
	}
	for _, expeditionID := range expeditionIDs {
		app.reevaluate(expeditionID)
		app.kickResolution(expeditionID, false)
	}
	return expeditionIDs
}

func (app *App) reevaluate(expeditionID int64) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return
	}
	if _, txError := expedition.ReevaluateTx(app.db, expeditionID); txError != nil {
		log.Printf("reevaluate expedition %d: %v", expeditionID, txError)
	}
}

func (app *App) maybeRefreshGEC() {
	if !app.gecRefreshing.CompareAndSwap(false, true) {
		return
	}
	defer app.gecRefreshing.Store(false)

	app.dbMu.RLock()
	var (
		online      bool
		lastFetched string
	)
	if app.db != nil {
		online = settings.OnlineLookups(app.db)
		lastFetched = settings.GECFetchedAt(app.db)
	}
	gecCatalog := app.gec
	app.dbMu.RUnlock()
	if !online || gecCatalog == nil {
		return
	}
	if lastFetched != "" {
		if parsedTime, parseError := reference.ParseISO(lastFetched); parseError == nil && time.Since(parsedTime) < gecRefreshInterval && gecCatalog.Count() > 0 {
			return
		}
	}
	if _, refreshError := gecCatalog.Refresh(app.ctx); refreshError != nil {
		log.Printf("gec refresh: %v", refreshError)
		return
	}
	app.dbMu.RLock()
	if app.gec == gecCatalog && app.db != nil {
		if setError := settings.SetGECFetchedAt(app.db, reference.NowISO()); setError != nil {
			log.Printf("persist gec refresh timestamp: %v", setError)
		}
	}
	app.dbMu.RUnlock()
	app.populateMissingPOIs()
	app.publishApp("plan", 0, events.AppExpeditionPlanChanged, events.AppSettingsChanged)
}

func (app *App) maybeCatchUpEDD() {
	app.ingestMu.RLock()
	defer app.ingestMu.RUnlock()
	app.dbMu.RLock()
	database := app.db
	ingester := app.ingester
	app.dbMu.RUnlock()
	if database == nil || ingester == nil {
		return
	}
	recovered, alreadyDone, eddPath := settings.EDDRecoveryState(database)
	if !recovered || alreadyDone {
		return
	}
	if eddPath == "" {
		eddPath = eddiscovery.DiscoverDBPath()
		if eddPath == "" {
			eddPath = eddiscovery.DefaultDBPath()
		}
	}
	if !fileExists(eddPath) {
		return
	}
	defer app.emitEDDProgress(false, 0, 0, 0, "")
	importResult, importError := eddiscovery.Import(app.ctx, ingester, eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if importError == nil {
		if settingError := settings.MarkEDDDisconnectsRecovered(database); settingError != nil {
			log.Printf("persist edd catch-up marker: %v", settingError)
		}
		affectedTargets := app.publishJournalBatch(importResult.Events, importResult.EarliestTimestamp)
		if affectedTargets.IsEmpty() {
			affectedTargets = events.NewUITargetSet(events.UITargetIngest)
		}
		app.emitDataChanged("journal", 0, affectedTargets)
	}
}
