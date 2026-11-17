package stats

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/sources/journal"
)

var (
	// cartographicsReplayEvents includes session-context events (LoadGame,
	// Location, FSDJump, CarrierJump) needed for Odyssey and system population
	// eligibility during SQL replay alongside Scan and SAAScanComplete.
	cartographicsReplayEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventScan,
		journal.EventSAAScanComplete,
	)
	// cartographicsJournalEvents restricts cache invalidation to body scan and
	// surface mapping events; session-context events are already persisted in
	// SQLite before any subsequent scan or mapping occurs in that system.
	cartographicsJournalEvents = journal.NewEventSet(
		journal.EventScan,
		journal.EventSAAScanComplete,
	)
	careerCartographicsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CartographicsMetrics holds Universal Cartographics body scan, DSS mapping,
// and estimated payout metrics.
type CartographicsMetrics struct {
	BodiesScanned    int   `json:"bodiesScanned"`
	BodiesMapped     int   `json:"bodiesMapped"`
	ScanValueTotal   int64 `json:"scanValueTotal"`
	ScanValueBase    int64 `json:"scanValueBase"`
	ScanValueBonus   int64 `json:"scanValueBonus"`
	FirstDiscoveries int   `json:"firstDiscoveries"`
	FirstMapped      int   `json:"firstMapped"`
}

func extractCartographicsMetrics(metrics Metrics) CartographicsMetrics {
	return CartographicsMetrics{
		BodiesScanned:    metrics.BodiesScanned,
		BodiesMapped:     metrics.BodiesMapped,
		ScanValueTotal:   metrics.ScanValueTotal,
		ScanValueBase:    metrics.ScanValueBase,
		ScanValueBonus:   metrics.ScanValueBonus,
		FirstDiscoveries: metrics.FirstDiscoveries,
		FirstMapped:      metrics.FirstMapped,
	}
}

func (cartographics CartographicsMetrics) applyTo(metrics *Metrics) {
	metrics.BodiesScanned = cartographics.BodiesScanned
	metrics.BodiesMapped = cartographics.BodiesMapped
	metrics.ScanValueTotal = cartographics.ScanValueTotal
	metrics.ScanValueBase = cartographics.ScanValueBase
	metrics.ScanValueBonus = cartographics.ScanValueBonus
	metrics.FirstDiscoveries = cartographics.FirstDiscoveries
	metrics.FirstMapped = cartographics.FirstMapped
}

// CareerCartographics caches all-time unfiltered Universal Cartographics metrics.
type CareerCartographics struct {
	database *sql.DB
	cache    *projection.Cache[CartographicsMetrics]
}

// NewCareerCartographics creates a CareerCartographics projection and registers it on bus.
func NewCareerCartographics(dbConn *sql.DB, bus *projection.Bus) *CareerCartographics {
	careerCarto := &CareerCartographics{database: dbConn}
	careerCarto.cache = projection.NewCache(
		bus,
		"career_cartographics",
		careerCarto.Triggers(),
		careerCarto.load,
	)
	return careerCarto
}

// Triggers declares the journal and application events that invalidate CareerCartographics.
func (careerCarto *CareerCartographics) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: cartographicsJournalEvents,
		App:     careerCartographicsAppEvents,
	}
}

// Get returns the cached all-time cartographics metrics.
func (careerCarto *CareerCartographics) Get() (CartographicsMetrics, error) {
	return careerCarto.cache.Get()
}

// IsValid reports whether all-time cartographics metrics are currently cached.
func (careerCarto *CareerCartographics) IsValid() bool {
	return careerCarto.cache.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (careerCarto *CareerCartographics) Generation() uint64 {
	return careerCarto.cache.Generation()
}

func (careerCarto *CareerCartographics) load() (CartographicsMetrics, error) {
	if careerCarto.database == nil {
		return CartographicsMetrics{}, fmt.Errorf("career cartographics database is nil")
	}
	engine := NewEngine(careerCarto.database)
	return engine.ComputeCartographics(Range{}, nil)
}

// ComputeCartographics returns Universal Cartographics scan and mapping metrics
// for events in timeRange, optionally filtered by Discovery mode selections.
func (engine *Engine) ComputeCartographics(timeRange Range, filter *Filter) (CartographicsMetrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB)
	if prepareError != nil {
		return CartographicsMetrics{}, fmt.Errorf("prepare cartographics stats filter: %w", prepareError)
	}
	loadedEvents, loadError := journal.LoadEvents(engine.DB, after, until, cartographicsReplayEvents)
	if loadError != nil {
		return CartographicsMetrics{}, fmt.Errorf("load cartographics stats events: %w", loadError)
	}

	session := projections.NewSessionState(engine.Odyssey, len(loadedEvents)/8)
	if seedError := projections.SeedLoadGameBefore(engine.DB, after, session); seedError != nil {
		return CartographicsMetrics{}, fmt.Errorf("seed initial loadgame state: %w", seedError)
	}
	if preloadPopError := projections.PreloadSystemPopulations(engine.DB, after, loadedEvents, until, session); preloadPopError != nil {
		return CartographicsMetrics{}, fmt.Errorf("preload system populations: %w", preloadPopError)
	}
	scans, missingScans, preloadScanError := engine.prepareComputeScans(after, loadedEvents)
	if preloadScanError != nil {
		return CartographicsMetrics{}, fmt.Errorf("prepare cartographics scans: %w", preloadScanError)
	}

	accumulator := &computeAccumulator{
		engine:        engine,
		after:         after,
		until:         until,
		filterSet:     filterSet,
		session:       session,
		scans:         scans,
		missingScans:  missingScans,
		scannedBodies: make(map[bodyKey]bool, len(loadedEvents)/2),
		mappedBodies:  make(map[bodyKey]bool, len(loadedEvents)/8),
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processCartographicsEvent(rowItem, &metrics); stepError != nil {
			return CartographicsMetrics{}, stepError
		}
	}
	return extractCartographicsMetrics(metrics), nil
}

func (accumulator *computeAccumulator) processCartographicsEvent(rowItem journal.Row, metrics *Metrics) error {
	switch rowItem.Type {
	case journal.EventLoadGame:
		accumulator.session.ApplyLoadGame(rowItem.RawJSON)
	case journal.EventLocation:
		accumulator.session.ApplyLocation(rowItem.RawJSON)
	case journal.EventFSDJump:
		var jumpEvent journal.FSDJumpEvent
		if json.Unmarshal([]byte(rowItem.RawJSON), &jumpEvent) == nil {
			accumulator.session.ApplyFSDJumpEvent(&jumpEvent)
		}
	case journal.EventCarrierJump:
		accumulator.session.ApplyCarrierJump(rowItem.RawJSON)
	case journal.EventScan:
		accumulator.processScan(rowItem, metrics)
	case journal.EventSAAScanComplete:
		return accumulator.processMapping(rowItem, metrics)
	}
	return nil
}

