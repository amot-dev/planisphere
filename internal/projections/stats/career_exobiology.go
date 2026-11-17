package stats

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/sources/journal"
)

var (
	exobiologyReplayEvents = journal.NewEventSet(
		journal.EventScanOrganic,
	)
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

// ExobiologyMetrics holds Vista Genomics organic sample and valuation metrics.
type ExobiologyMetrics struct {
	OrganicSamples    int   `json:"organicSamples"`
	OrganicValueTotal int64 `json:"organicValueTotal"`
	OrganicValueBase  int64 `json:"organicValueBase"`
	OrganicValueBonus int64 `json:"organicValueBonus"`
	FirstLogged       int   `json:"firstLogged"`
}

func extractExobiologyMetrics(metrics Metrics) ExobiologyMetrics {
	return ExobiologyMetrics{
		OrganicSamples:    metrics.OrganicSamples,
		OrganicValueTotal: metrics.OrganicValueTotal,
		OrganicValueBase:  metrics.OrganicValueBase,
		OrganicValueBonus: metrics.OrganicValueBonus,
		FirstLogged:       metrics.FirstLogged,
	}
}

func (exobiology ExobiologyMetrics) applyTo(metrics *Metrics) {
	metrics.OrganicSamples = exobiology.OrganicSamples
	metrics.OrganicValueTotal = exobiology.OrganicValueTotal
	metrics.OrganicValueBase = exobiology.OrganicValueBase
	metrics.OrganicValueBonus = exobiology.OrganicValueBonus
	metrics.FirstLogged = exobiology.FirstLogged
}

// CareerExobiology caches all-time unfiltered Vista Genomics exobiology metrics.
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
	careerExo.cache = projection.NewCache(
		bus,
		"career_exobiology",
		careerExo.Triggers(),
		careerExo.load,
	)
	return careerExo
}

// Triggers declares the journal and application events that invalidate CareerExobiology.
func (careerExo *CareerExobiology) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: exobiologyJournalEvents,
		App:     careerExobiologyAppEvents,
	}
}

// Get returns the cached all-time exobiology metrics.
func (careerExo *CareerExobiology) Get() (ExobiologyMetrics, error) {
	return careerExo.cache.Get()
}

// IsValid reports whether all-time exobiology metrics are currently cached.
func (careerExo *CareerExobiology) IsValid() bool {
	return careerExo.cache.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (careerExo *CareerExobiology) Generation() uint64 {
	return careerExo.cache.Generation()
}

func (careerExo *CareerExobiology) load() (ExobiologyMetrics, error) {
	if careerExo.database == nil {
		return ExobiologyMetrics{}, fmt.Errorf("career exobiology database is nil")
	}
	engine := NewEngineWithProjections(careerExo.database, careerExo.salesLedger, nil)
	return engine.ComputeExobiology(Range{}, nil)
}

// ComputeExobiology returns Vista Genomics exobiology metrics for events in
// timeRange, loading only ScanOrganic events and batch-preloading their parent
// body scans and system populations.
func (engine *Engine) ComputeExobiology(timeRange Range, filter *Filter) (ExobiologyMetrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB)
	if prepareError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("prepare exobiology stats filter: %w", prepareError)
	}
	loadedEvents, loadError := journal.LoadEvents(engine.DB, after, until, exobiologyReplayEvents)
	if loadError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("load exobiology stats events: %w", loadError)
	}
	if len(loadedEvents) == 0 {
		return ExobiologyMetrics{}, nil
	}

	session := projections.NewSessionState(engine.Odyssey, len(loadedEvents)/4)
	if preloadPopError := projections.PreloadSystemPopulationsForRows(engine.DB, loadedEvents, until, session); preloadPopError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("preload exobiology system populations: %w", preloadPopError)
	}
	scans, missingScans, preloadScanError := engine.prepareExobiologyScans(until, loadedEvents)
	if preloadScanError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("prepare exobiology scans: %w", preloadScanError)
	}

	accumulator := &computeAccumulator{
		engine:       engine,
		after:        after,
		until:        until,
		filterSet:    filterSet,
		session:      session,
		scans:        scans,
		missingScans: missingScans,
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processOrganic(rowItem, &metrics); stepError != nil {
			return ExobiologyMetrics{}, stepError
		}
	}
	return extractExobiologyMetrics(metrics), nil
}

func (engine *Engine) prepareExobiologyScans(
	untilTimestamp string,
	loadedEvents []journal.Row,
) (map[bodyKey]*journal.ScanEvent, map[bodyKey]bool, error) {
	scans := make(map[bodyKey]*journal.ScanEvent)
	missingScans := make(map[bodyKey]bool)
	earliestTimestampByBody := make(map[bodyKey]string)
	var neededKeys []bodyKey
	for _, rowItem := range loadedEvents {
		if rowItem.Type != journal.EventScanOrganic || !rowItem.SystemAddress.Valid || !rowItem.BodyID.Valid {
			continue
		}
		key := bodyKey{rowItem.SystemAddress.Int64, rowItem.BodyID.Int64}
		if _, seen := earliestTimestampByBody[key]; !seen {
			earliestTimestampByBody[key] = rowItem.Timestamp
			neededKeys = append(neededKeys, key)
		}
	}
	if len(neededKeys) == 0 {
		return scans, missingScans, nil
	}
	preloaded, preloadError := engine.preloadScans(neededKeys, untilTimestamp)
	if preloadError != nil {
		return nil, nil, fmt.Errorf("preload exobiology parent scans before %s: %w", untilTimestamp, preloadError)
	}
	for _, key := range neededKeys {
		scanEvent := preloaded[key]
		if scanEvent == nil {
			missingScans[key] = true
			continue
		}
		if scanEvent.Timestamp == "" || scanEvent.Timestamp <= earliestTimestampByBody[key] {
			scans[key] = scanEvent
		}
	}
	return scans, missingScans, nil
}

