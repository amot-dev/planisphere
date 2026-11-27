package stats_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	projstats "planisphere/internal/projections/stats"
	"planisphere/internal/replay"
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

func seedScopedMultiProfileArchive(testingContext *testing.T) (*timeRangeStatsHarness, replay.ProfileScope, replay.ProfileScope, replay.ProfileScope) {
	testingContext.Helper()
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		// FID F10 Era 0 (archived, disabled by default) + Era 1 (active, enabled).
		"Journal.2026-02-01T100000.01.log": {
			`{"timestamp":"2026-02-01T10:00:00Z","event":"LoadGame","Commander":"CmdrMain","FID":"F10","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-02-01T10:05:00Z","event":"Location","StarSystem":"ArchivedStart","SystemAddress":100,"StarPos":[0,0,0]}`,
			`{"timestamp":"2026-02-01T10:10:00Z","event":"FSDJump","StarSystem":"ArchivedHop","SystemAddress":101,"StarPos":[40,0,0],"JumpDist":40.0}`,
			`{"timestamp":"2026-02-01T10:15:00Z","event":"Statistics","FLEETCARRIER":{"FLEETCARRIER_TOTAL_JUMPS":7,"FLEETCARRIER_DISTANCE_TRAVELLED":3500.0}}`,
			`{"timestamp":"2026-02-01T11:00:00Z","event":"ClearSavedGame","Name":"CmdrMain","FID":"F10"}`,
			`{"timestamp":"2026-02-01T11:01:00Z","event":"NewCommander","Name":"CmdrMain","FID":"F10","Package":"Sidewinder"}`,
			`{"timestamp":"2026-02-01T11:02:00Z","event":"LoadGame","Commander":"CmdrMain","FID":"F10","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-02-01T11:10:00Z","event":"FSDJump","StarSystem":"MainHop","SystemAddress":201,"StarPos":[0,60,0],"JumpDist":60.0}`,
			`{"timestamp":"2026-02-01T11:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"MainHop","SystemAddress":201,"BodyID":1,"BodyName":"MainHop 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		},
		// Interleaved second enabled save slot FID F20.
		"Journal.2026-02-01T110500.01.log": {
			`{"timestamp":"2026-02-01T11:05:00Z","event":"LoadGame","Commander":"CmdrAlt","FID":"F20","Ship":"Explorer_NX","Odyssey":true}`,
			`{"timestamp":"2026-02-01T11:15:00Z","event":"FSDJump","StarSystem":"AltHop","SystemAddress":301,"StarPos":[0,0,25],"JumpDist":25.0}`,
			`{"timestamp":"2026-02-01T11:25:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":301,"Body":1}`,
		},
	})

	commanders, err := replay.ListCommanders(databaseConnection)
	if err != nil || len(commanders) != 3 {
		testingContext.Fatalf("expected 3 profiles, got %+v err=%v", commanders, err)
	}
	return wireTimeRangeStatsHarness(databaseConnection, nil),
		replay.NewProfileScope([]int64{commanders[0].ID}),
		replay.NewProfileScope([]int64{commanders[1].ID}),
		replay.NewProfileScope([]int64{commanders[2].ID})
}
