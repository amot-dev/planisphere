package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
)

var (
	realizationStatsJournalEvents = journal.NewEventSet(
		journal.EventScan,
		journal.EventSAAScanComplete,
		journal.EventScanOrganic,
		journal.EventSellExplorationData,
		journal.EventMultiSellExplorationData,
		journal.EventSellOrganicData,
		journal.EventResurrect,
		journal.EventNpcCrewPaidWage,
	)
	careerRealizationAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CareerRealization caches the all-time unfiltered Realized Value profit and loss breakdown.
type CareerRealization struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	cache       *projection.Cache[RealizationBreakdown]
}

// NewCareerRealization creates a CareerRealization projection and registers it on bus.
func NewCareerRealization(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
) *CareerRealization {
	careerReal := &CareerRealization{
		database:    dbConn,
		salesLedger: salesLedger,
	}
	triggers := projection.Triggers{
		Journal: realizationStatsJournalEvents,
		App:     careerRealizationAppEvents,
	}
	careerReal.cache = projection.NewCache[RealizationBreakdown](
		bus,
		"CareerRealization",
		triggers,
		careerReal.load,
	)
	return careerReal
}

// Triggers reports the journal and app events that invalidate CareerRealization.
func (careerReal *CareerRealization) Triggers() projection.Triggers {
	return careerReal.cache.Triggers()
}

// Get returns the cached all-time realization breakdown.
func (careerReal *CareerRealization) Get() (RealizationBreakdown, error) {
	return careerReal.cache.Get()
}

// IsValid reports whether CareerRealization currently holds a warm cached value.
func (careerReal *CareerRealization) IsValid() bool {
	return careerReal.cache.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerReal *CareerRealization) Generation() uint64 {
	return careerReal.cache.Generation()
}

func (careerReal *CareerRealization) load() (RealizationBreakdown, error) {
	if careerReal.database == nil {
		return RealizationBreakdown{}, nil
	}
	engine := replaystats.NewEngineWithSources(careerReal.database, careerReal.salesLedger, nil, nil)
	return engine.ComputeRealization(Range{}, nil)
}
