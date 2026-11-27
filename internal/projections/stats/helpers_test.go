package stats_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	projstats "planisphere/internal/projections/stats"
	"planisphere/internal/replay/stats"
	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

type timeRangeStatsHarness struct {
	conn           *sql.DB
	bus            *projection.Bus
	salesLedger    *realization.SalesLedger
	recoveredJumps *projections.RecoveredJumps
	careerStats    *projstats.CareerStats
	timeRangeStats *projstats.TimeRangeStats
	fallbackModeID int64
}

func wireTimeRangeStatsHarness(conn *sql.DB, customModeLoader stats.ModeLookupLoader) *timeRangeStatsHarness {
	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(conn, eventBus)
	recoveredJumps := projections.NewRecoveredJumps(conn, eventBus)
	careerStats := projstats.NewCareerStats(conn, eventBus, salesLedger, recoveredJumps)
	fallbackModeID := modes.DefaultFallbackModeID(conn)
	modeLoader := customModeLoader
	if modeLoader == nil {
		modeLoader = func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
			return func(_ string) int64 { return fallbackModeID }, nil
		}
	}
	return &timeRangeStatsHarness{
		conn:           conn,
		bus:            eventBus,
		salesLedger:    salesLedger,
		recoveredJumps: recoveredJumps,
		careerStats:    careerStats,
		timeRangeStats: projstats.NewTimeRangeStats(conn, eventBus, salesLedger, recoveredJumps, modeLoader),
		fallbackModeID: fallbackModeID,
	}
}

func newTimeRangeStatsHarness(testingContext *testing.T, journalLines []string, customModeLoader stats.ModeLookupLoader) *timeRangeStatsHarness {
	testingContext.Helper()
	databaseConnection := dbtest.OpenMemory(testingContext)
	if len(journalLines) > 0 {
		journaltest.IngestLines(testingContext, databaseConnection, journalLines...)
	}
	return wireTimeRangeStatsHarness(databaseConnection, customModeLoader)
}
