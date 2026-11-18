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
	// exobiologyJournalEvents invalidates exobiology caches when new organic
	// samples are analyzed or when Vista Genomics sales settle First Logged
	// bonus realization.
	exobiologyJournalEvents = journal.NewEventSet(
		journal.EventScanOrganic,
		journal.EventSellOrganicData,
	)
	careerExobiologyAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CareerExobiology caches lifetime unfiltered Vista Genomics exobiology metrics.
type CareerExobiology struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	cache       *projection.Cache[ExobiologyMetrics]
}

// NewCareerExobiology creates a CareerExobiology projection and registers it on bus.
func NewCareerExobiology(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
) *CareerExobiology {
	careerExo := &CareerExobiology{
		database:    dbConn,
		salesLedger: salesLedger,
	}
	triggers := projection.Triggers{
		Journal: exobiologyJournalEvents,
		App:     careerExobiologyAppEvents,
	}
	careerExo.cache = projection.NewCache[ExobiologyMetrics](
		bus,
		"CareerExobiology",
		triggers,
		careerExo.load,
	)
	return careerExo
}

// Triggers reports the journal and app events that invalidate CareerExobiology.
func (careerExo *CareerExobiology) Triggers() projection.Triggers {
	return careerExo.cache.Triggers()
}

// Get returns the cached all-time exobiology metrics.
func (careerExo *CareerExobiology) Get() (ExobiologyMetrics, error) {
	return careerExo.cache.Get()
}

// IsValid reports whether CareerExobiology currently holds a warm cached value.
func (careerExo *CareerExobiology) IsValid() bool {
	return careerExo.cache.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerExo *CareerExobiology) Generation() uint64 {
	return careerExo.cache.Generation()
}

func (careerExo *CareerExobiology) load() (ExobiologyMetrics, error) {
	if careerExo.database == nil {
		return ExobiologyMetrics{}, nil
	}
	engine := replaystats.NewEngineWithSources(careerExo.database, careerExo.salesLedger, nil, nil)
	return engine.ComputeExobiology(Range{}, nil)
}
