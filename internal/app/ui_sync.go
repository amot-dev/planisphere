package app

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

func (app *App) wireProjections(dbConn *sql.DB) {
	bus := projection.NewBus()
	app.recoveredJumps = projections.NewRecoveredJumps(dbConn, bus)
	app.systemArrivals = projections.NewSystemArrivals(dbConn, bus)
	app.systemVisitCounts = projections.NewSystemVisitCounts(dbConn, bus, app.recoveredJumps)
	app.shipHistory = projections.NewShipHistory(dbConn, bus)
	app.carrierState = projections.NewCarrierState(dbConn, bus)
	app.salesLedger = realization.NewSalesLedger(dbConn, bus)
	app.careerStats = stats.NewCareerStats(dbConn, bus, app.salesLedger, app.recoveredJumps)
	app.timeRangeStats = stats.NewTimeRangeStats(dbConn, bus, app.salesLedger, app.recoveredJumps, modes.LoadModeLookup)

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
		projection.Triggers{
			Journal: journal.AllEvents,
			App:     events.NewAppSet(events.AppCommanderFilterChanged, events.AppDatabaseSwitched),
		},
	)
	bus.SubscribeUI(
		events.UITargetStats,
		projection.TriggersFrom(app.careerStats, app.timeRangeStats).
			WithJournal(journal.EventCommander, journal.EventLoadGame).
			WithApp(events.AppAdjustmentsChanged, events.AppSettingsChanged),
	)
	bus.SubscribeUI(
		events.UITargetPlan,
		projection.TriggersFrom(app.systemArrivals, app.systemVisitCounts, app.shipHistory).
			WithApp(events.AppExpeditionPlanChanged, events.AppExpeditionBoundsChanged, events.AppModesChanged),
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
			Journal: journal.NewEventSet(journal.EventCommander, journal.EventLoadGame),
			App:     events.NewAppSet(events.AppSettingsChanged, events.AppCommanderFilterChanged, events.AppDatabaseSwitched),
		},
	)
}

func (app *App) onArrivalTrigger(_ projection.Triggers) {
	app.reevaluateActive()
	app.dbMu.RLock()
	var currentActiveID int64
	if app.db != nil {
		currentActiveID = app.activeExpeditionID()
	}
	app.dbMu.RUnlock()
	if currentActiveID > 0 {
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

func (app *App) currentBus() *projection.Bus {
	app.dbMu.RLock()
	bus := app.bus
	app.dbMu.RUnlock()
	return bus
}

func (app *App) publishJournalEvents(journalEvents journal.EventSet) events.UITargetSet {
	bus := app.currentBus()
	if bus == nil {
		return 0
	}
	return bus.PublishJournal(journalEvents)
}

func (app *App) emitDataChanged(scope string, expeditionID int64, targets events.UITargetSet) {
	app.emit(EventDataChanged, DataChanged{
		Scope:        scope,
		Domains:      targets.Names(),
		ExpeditionID: expeditionID,
	})
}

func (app *App) publishApp(scope string, expeditionID int64, appEvents ...events.AppEvent) {
	bus := app.currentBus()
	var targets events.UITargetSet
	if bus != nil {
		targets = bus.PublishApp(appEvents...)
	}
	app.emitDataChanged(scope, expeditionID, targets)
}

