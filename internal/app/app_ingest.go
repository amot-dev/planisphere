package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
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
	hasChanges, didStartScan, affectedTargets := app.executeJournalScan(true)
	if !didStartScan {
		return false
	}
	app.emit(EventIngest, nil)
	if hasChanges {
		app.emitDataChanged(0, affectedTargets)
	}
	return hasChanges
}

func (app *App) executeJournalScan(emitProgress bool) (bool, bool, events.UITargetSet) {
	if !app.scanning.CompareAndSwap(false, true) {
		return false, false, 0
	}
	defer app.scanning.Store(false)
	if emitProgress {
		app.emit(EventIngest, nil)
	}
	app.ingestMu.RLock()
	defer app.ingestMu.RUnlock()
	app.dbMu.RLock()
	ingester := app.ingester
	database := app.db
	activeWatcher := app.watcher
	scanContext := app.contextLocked()
	dir := ""
	if ingester != nil && database != nil {
		dir = settings.JournalDir(database, journal.DefaultJournalDir())
	}
	app.dbMu.RUnlock()
	if ingester == nil || database == nil {
		return false, true, 0
	}
	if activeWatcher != nil {
		activeWatcher.ClearPending()
		if app.isLiveSyncPaused() && !app.hasDeferredTargets() {
			app.emit(EventSyncPending, false)
		}
	}
	scanResult, scanError := ingester.ScanDir(scanContext, dir)
	app.recordJournalScanOutcome(database, scanResult, scanError)
	hasChanges := scanResult.HasChanges()
	var affectedTargets events.UITargetSet
	if hasChanges {
		affectedTargets = app.publishIngestResult(scanResult)
	}
	return hasChanges, true, affectedTargets
}

func (app *App) recordJournalScanOutcome(database *sql.DB, scanResult journal.Result, scanError error) {
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
}

// warmInitialStats pre-populates the all-time and active expedition statistics
// caches during startup so initial frontend queries resolve immediately.
func (app *App) warmInitialStats() {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return
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
	var activeExpedition *expedition.Expedition
	if activeID > 0 {
		loadedExpedition, getError := expedition.Get(app.db, activeID)
		if getError != nil {
			if !errors.Is(getError, sql.ErrNoRows) {
				log.Printf("warm initial stats load expedition %d: %v", activeID, getError)
			}
		} else {
			activeExpedition = &loadedExpedition
		}
	}
	if app.careerStats != nil {
		var careerScope replay.ProfileScope
		if activeExpedition != nil && settings.CareerBaselineScope(app.db) != settings.CareerScopeAllEnabled {
			careerScope = activeExpedition.ProfileScope()
		}
		careerScope = replay.NormalizeAllTimeProfileScope(app.db, careerScope)
		if _, metricsError := app.careerStats.GetScoped(careerScope); metricsError != nil {
			log.Printf("warm initial career stats: %v", metricsError)
		}
	}
	if activeExpedition != nil && app.timeRangeStats != nil && activeExpedition.StartTimestamp != "" {
		queryAfter, _, departed, startError := expedition.EffectiveStart(app.db, *activeExpedition)
		if startError != nil {
			log.Printf("warm initial stats resolve effective start %d: %v", activeID, startError)
		} else if departed {
			expeditionRange := stats.Range{After: queryAfter, Until: activeExpedition.EndTimestamp}
			if _, statsError := app.timeRangeStats.Compute(expeditionRange, &stats.Filter{Scope: activeExpedition.ProfileScope()}); statsError != nil {
				log.Printf("warm initial stats compute expedition %d: %v", activeID, statsError)
			}
		}
	}
}

func (app *App) withExclusiveCommanderDB(mutation func() error) error {
	if readyError := app.withReadyReadLock(nil); readyError != nil {
		return readyError
	}
	app.ingestMu.RLock()
	defer app.ingestMu.RUnlock()

	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil || app.ingester == nil {
		return errorNotReady
	}
	_, execErr := app.ingester.WithExclusiveConn(func(_ *sql.DB) (journal.Result, error) {
		return journal.Result{}, mutation()
	})
	return execErr
}

// onJournalBatch runs after the live tailer stored new events or healed identities.
func (app *App) onJournalBatch(scanResult journal.Result) {
	affectedTargets := app.publishIngestResult(scanResult)
	app.emitBackgroundDataChanged(0, affectedTargets)
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
	anyChanged := false
	for _, expeditionID := range expeditionIDs {
		if app.reevaluate(expeditionID) {
			anyChanged = true
		}
		app.kickResolution(expeditionID, false)
	}
	if anyChanged {
		if bus := app.currentBus(); bus != nil {
			bus.PublishApp(events.AppExpeditionModesChanged)
		}
	}
	return expeditionIDs
}

func (app *App) reevaluate(expeditionID int64) bool {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil {
		return false
	}
	changed, txError := expedition.ReevaluateTx(app.db, expeditionID)
	if txError != nil {
		log.Printf("reevaluate expedition %d: %v", expeditionID, txError)
		return false
	}
	return changed
}

func (app *App) reevaluateForDB(targetDB *sql.DB, expeditionID int64) bool {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db != targetDB || targetDB == nil {
		return false
	}
	changed, txError := expedition.ReevaluateTx(targetDB, expeditionID)
	if txError != nil {
		log.Printf("reevaluate expedition %d: %v", expeditionID, txError)
		return false
	}
	return changed
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
	refreshContext := app.contextLocked()
	app.dbMu.RUnlock()
	if !online || gecCatalog == nil {
		return
	}
	if lastFetched != "" {
		if parsedTime, parseError := reference.ParseISO(lastFetched); parseError == nil && time.Since(parsedTime) < gecRefreshInterval && gecCatalog.Count() > 0 {
			return
		}
	}
	if _, refreshError := gecCatalog.Refresh(refreshContext); refreshError != nil {
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
	app.publishBackgroundApp(0, events.AppExpeditionPlanChanged, events.AppSettingsChanged)
}

func (app *App) maybeCatchUpEDD() {
	app.ingestMu.RLock()
	defer app.ingestMu.RUnlock()
	app.dbMu.RLock()
	database := app.db
	ingester := app.ingester
	importContext := app.contextLocked()
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
	importResult, importError := eddiscovery.Import(importContext, ingester, eddPath, func(current, total, files int, stage string) {
		app.emitEDDProgress(stage != "Complete", current, total, files, stage)
	})
	if importError == nil {
		if settingError := settings.MarkEDDDisconnectsRecovered(database); settingError != nil {
			log.Printf("persist edd catch-up marker: %v", settingError)
		}
		affectedTargets := app.publishIngestResult(importResult)
		if affectedTargets.IsEmpty() {
			affectedTargets = events.NewUITargetSet(events.UITargetIngest)
		}
		app.emitBackgroundDataChanged(0, affectedTargets)
	}
}
