package app

import (
	"database/sql"
	"slices"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func (app *App) wireProjections(dbConn *sql.DB) {
	var (
		previousClosedRevision    uint64
		previousTelemetryRevision uint64
	)
	hasPreviousTimeRangeStats := app.timeRangeStats != nil
	if hasPreviousTimeRangeStats {
		previousClosedRevision = app.timeRangeStats.ClosedWindowRevision()
		previousTelemetryRevision = app.timeRangeStats.TelemetryRevision()
	}

	bus := projection.NewBus()
	app.recoveredJumps = projections.NewRecoveredJumps(dbConn, bus)
	app.systemArrivals = projections.NewSystemArrivals(dbConn, bus)
	app.systemVisitCounts = projections.NewSystemVisitCounts(dbConn, bus, app.recoveredJumps)
	app.shipHistory = projections.NewShipHistory(dbConn, bus)
	app.carrierState = projections.NewCarrierState(dbConn, bus)
	app.salesLedger = realization.NewSalesLedger(dbConn, bus)
	app.careerStats = stats.NewCareerStats(dbConn, bus, app.salesLedger, app.recoveredJumps)
	app.timeRangeStats = stats.NewTimeRangeStats(dbConn, bus, app.salesLedger, app.recoveredJumps, expedition.LoadModeLookup)
	if hasPreviousTimeRangeStats {
		app.timeRangeStats.EnsureMinClosedWindowRevision(previousClosedRevision + 1)
		app.timeRangeStats.EnsureMinTelemetryRevision(previousTelemetryRevision + 1)
	}

	bus.OnTrigger(
		projection.Triggers{
			Journal: app.systemArrivals.Triggers().Journal,
			App:     events.NewAppSet(events.AppCommanderFilterChanged),
		},
		app.onArrivalTrigger,
	)
	app.registerUISubscriptions(bus)
	app.bus = bus
}

func (app *App) registerUISubscriptions(bus *projection.Bus) {
	bus.SubscribeUI(
		events.UITargetIngest,
		projection.TriggersFrom(app.timeRangeStats, app.shipHistory, app.systemArrivals).
			WithJournal(
				journal.EventCommander,
				journal.EventLoadGame,
				journal.EventClearSavedGame,
				journal.EventNewCommander,
			).
			WithApp(
				events.AppCommanderFilterChanged,
				events.AppDatabaseSwitched,
				events.AppExpeditionModesChanged,
			),
	)
	bus.SubscribeUI(
		events.UITargetStats,
		projection.TriggersFrom(app.careerStats, app.timeRangeStats).
			WithJournal(journal.EventCommander, journal.EventLoadGame, journal.EventClearSavedGame, journal.EventNewCommander).
			WithApp(events.AppAdjustmentsChanged, events.AppExpeditionBoundsChanged, events.AppExpeditionModesChanged),
	)
	bus.SubscribeUI(
		events.UITargetPlan,
		projection.TriggersFrom(app.systemArrivals, app.systemVisitCounts).
			WithApp(
				events.AppCommanderFilterChanged,
				events.AppExpeditionPlanChanged,
				events.AppExpeditionBoundsChanged,
				events.AppModesChanged,
				events.AppExpeditionModesChanged,
			),
	)
	bus.SubscribeUI(
		events.UITargetModes,
		projection.Triggers{
			App: events.NewAppSet(events.AppModesChanged, events.AppDatabaseSwitched),
		},
	)
	bus.SubscribeUI(
		events.UITargetExpeditions,
		projection.Triggers{
			App: events.NewAppSet(
				events.AppExpeditionPlanChanged,
				events.AppExpeditionBoundsChanged,
				events.AppAdjustmentsChanged,
				events.AppDatabaseSwitched,
			),
		},
	)
	bus.SubscribeUI(
		events.UITargetSettings,
		projection.Triggers{
			Journal: journal.NewEventSet(journal.EventCommander, journal.EventLoadGame, journal.EventClearSavedGame, journal.EventNewCommander),
			App:     events.NewAppSet(events.AppSettingsChanged, events.AppCommanderFilterChanged, events.AppDatabaseSwitched),
		},
	)
}

func (app *App) onArrivalTrigger(_ projection.Triggers) {
	reevaluatedIDs := app.reevaluateActive()
	app.dbMu.RLock()
	var currentActiveID int64
	if app.db != nil {
		currentActiveID = settings.ActiveExpeditionID(app.db)
	}
	app.dbMu.RUnlock()
	if currentActiveID > 0 && !slices.Contains(reevaluatedIDs, currentActiveID) {
		app.kickResolution(currentActiveID, false)
	}
}

func (app *App) expeditionProjections() *expedition.Projections {
	return &expedition.Projections{
		Arrivals:       app.systemArrivals,
		VisitCounts:    app.systemVisitCounts,
		ShipHistory:    app.shipHistory,
		TimeRangeStats: app.timeRangeStats,
		SalesLedger:    app.salesLedger,
		RecoveredJumps: app.recoveredJumps,
	}
}

func (app *App) currentDB() *sql.DB {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	return app.db
}

func (app *App) isCurrentDB(targetDB *sql.DB) bool {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	return app.db == targetDB && targetDB != nil
}

func (app *App) currentBus() *projection.Bus {
	app.dbMu.RLock()
	bus := app.bus
	app.dbMu.RUnlock()
	return bus
}

var careerWipeJournalEvents = journal.NewEventSet(
	journal.EventClearSavedGame,
	journal.EventNewCommander,
)

func (app *App) publishJournalBatch(journalEvents journal.EventSet, earliestTimestamp, latestTimestamp string) events.UITargetSet {
	bus := app.currentBus()
	if bus == nil {
		return 0
	}
	targets := bus.PublishJournalRange(journalEvents, earliestTimestamp, latestTimestamp)
	if journalEvents.Intersects(careerWipeJournalEvents) {
		targets = targets.Union(bus.PublishApp(events.AppCommanderFilterChanged))
	}
	return targets
}

func (app *App) publishIngestResult(ingestResult journal.Result) events.UITargetSet {
	bus := app.currentBus()
	if bus == nil {
		return 0
	}
	if ingestResult.CommandersBackfilled == 0 && !ingestResult.ProfilesChanged {
		return app.publishJournalBatch(ingestResult.Events, ingestResult.EarliestTimestamp, ingestResult.LatestTimestamp)
	}
	targets := bus.PublishJournalRange(ingestResult.Events, ingestResult.EarliestTimestamp, ingestResult.LatestTimestamp)
	return targets.Union(bus.PublishApp(
		events.AppCommanderFilterChanged,
		events.AppSettingsChanged,
		events.AppExpeditionPlanChanged,
	))
}

func (app *App) emitDataChanged(expeditionID int64, targets events.UITargetSet) {
	if targets.IsEmpty() {
		return
	}
	app.emit(EventDataChanged, DataChanged{
		Domains:      targets.Names(),
		ExpeditionID: expeditionID,
	})
}

func (app *App) publishApp(expeditionID int64, appEvents ...events.AppEvent) {
	bus := app.currentBus()
	if bus == nil {
		return
	}
	app.emitDataChanged(expeditionID, bus.PublishApp(appEvents...))
}

func (app *App) publishAppSetForDB(targetDB *sql.DB, expeditionID int64, appEvents events.AppEventSet) {
	if appEvents.IsEmpty() {
		return
	}
	app.dbMu.RLock()
	var bus *projection.Bus
	if app.db == targetDB && targetDB != nil {
		bus = app.bus
	}
	app.dbMu.RUnlock()
	if bus == nil {
		return
	}
	targets := bus.Publish(projection.Triggers{App: appEvents})
	if app.isCurrentDB(targetDB) {
		app.emitDataChanged(expeditionID, targets)
	}
}

func (app *App) publishAppForDB(targetDB *sql.DB, expeditionID int64, appEvents ...events.AppEvent) {
	app.publishAppSetForDB(targetDB, expeditionID, events.NewAppSet(appEvents...))
}

func (app *App) isLiveSyncPaused() bool {
	app.syncPauseMu.Lock()
	defer app.syncPauseMu.Unlock()
	return app.liveSyncPaused
}

func (app *App) hasDeferredTargets() bool {
	app.syncPauseMu.Lock()
	defer app.syncPauseMu.Unlock()
	return !app.deferredTargets.IsEmpty()
}

func (app *App) clearDeferredTargets() {
	app.syncPauseMu.Lock()
	defer app.syncPauseMu.Unlock()
	app.deferredTargets = 0
}

func (app *App) setLiveSyncPaused(paused bool) {
	app.dbMu.RLock()
	activeWatcher := app.watcher
	app.dbMu.RUnlock()

	var flushedTargets events.UITargetSet
	app.syncPauseMu.Lock()
	app.liveSyncPaused = paused
	if !paused {
		flushedTargets = app.deferredTargets
		app.deferredTargets = 0
	}
	app.syncPauseMu.Unlock()

	if activeWatcher != nil {
		activeWatcher.SetPaused(paused)
	}
	if !paused {
		app.emit(EventSyncPending, false)
		app.emitDataChanged(0, flushedTargets)
	}
}

func (app *App) emitBackgroundDataChanged(expeditionID int64, targets events.UITargetSet) {
	if targets.IsEmpty() {
		return
	}
	shouldNotifyPending := false
	shouldHold := false
	app.syncPauseMu.Lock()
	if app.liveSyncPaused {
		shouldHold = true
		wasEmpty := app.deferredTargets.IsEmpty()
		app.deferredTargets = app.deferredTargets.Union(targets)
		shouldNotifyPending = wasEmpty
	}
	app.syncPauseMu.Unlock()

	if shouldHold {
		if shouldNotifyPending {
			app.emit(EventSyncPending, true)
		}
		return
	}
	app.emitDataChanged(expeditionID, targets)
}

func (app *App) publishBackgroundApp(expeditionID int64, appEvents ...events.AppEvent) {
	bus := app.currentBus()
	if bus == nil {
		return
	}
	app.emitBackgroundDataChanged(expeditionID, bus.PublishApp(appEvents...))
}
