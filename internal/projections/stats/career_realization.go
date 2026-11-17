package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
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
	careerReal.cache = projection.NewCache(
		bus,
		"career_realization",
		careerReal.Triggers(),
		careerReal.load,
	)
	return careerReal
}

// Triggers declares the journal and application events that invalidate CareerRealization.
func (careerReal *CareerRealization) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: realizationStatsJournalEvents,
		App:     careerRealizationAppEvents,
	}
}

// Get returns the cached all-time realization breakdown.
func (careerReal *CareerRealization) Get() (RealizationBreakdown, error) {
	return careerReal.cache.Get()
}

// IsValid reports whether the all-time realization breakdown is currently cached.
func (careerReal *CareerRealization) IsValid() bool {
	return careerReal.cache.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (careerReal *CareerRealization) Generation() uint64 {
	return careerReal.cache.Generation()
}

func (careerReal *CareerRealization) load() (RealizationBreakdown, error) {
	if careerReal.database == nil {
		return RealizationBreakdown{}, fmt.Errorf("career realization database is nil")
	}
	engine := NewEngineWithProjections(careerReal.database, careerReal.salesLedger, nil)
	return engine.ComputeRealization(Range{}, nil)
}

