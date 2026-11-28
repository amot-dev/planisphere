package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	"planisphere/internal/replay"
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

// CareerRealization caches the all-time unfiltered Realized Value profit and loss breakdown per profile scope.
type CareerRealization struct {
	database    *sql.DB
	salesLedger *realization.SalesLedger
	cache       *projection.KeyedCache[string, RealizationBreakdown]
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
	careerReal.cache = projection.NewKeyedCache[string, RealizationBreakdown](
		bus,
		"CareerRealization",
		triggers,
	)
	return careerReal
}

// Triggers reports the journal and app events that invalidate CareerRealization.
func (careerReal *CareerRealization) Triggers() projection.Triggers {
	return careerReal.cache.Triggers()
}

// Get returns the cached all-time realization breakdown for globally enabled profiles.
func (careerReal *CareerRealization) Get() (RealizationBreakdown, error) {
	return careerReal.GetScoped(replay.ProfileScope{})
}

// GetScoped returns cached all-time realization breakdown for scope.
func (careerReal *CareerRealization) GetScoped(scope replay.ProfileScope) (RealizationBreakdown, error) {
	return careerReal.cache.GetOrLoad(scope.CacheKeySuffix(), func() (RealizationBreakdown, error) {
		return careerReal.load(scope)
	})
}

// IsValid reports whether CareerRealization currently holds a warm cached value.
func (careerReal *CareerRealization) IsValid() bool {
	return careerReal.cache.Len() > 0
}

// Generation returns the monotonically increasing invalidation generation counter.
func (careerReal *CareerRealization) Generation() uint64 {
	return careerReal.cache.Generation()
}

func (careerReal *CareerRealization) load(scope replay.ProfileScope) (RealizationBreakdown, error) {
	if careerReal.database == nil {
		return RealizationBreakdown{}, nil
	}
	engine := replaystats.NewEngineWithSources(careerReal.database, careerReal.salesLedger, nil, nil)
	engine.Scope = scope
	return engine.ComputeRealization(Range{}, nil)
}
