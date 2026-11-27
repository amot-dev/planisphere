// Package realization maintains reactive projection caches for settlement,
// exploration profit, and loss ledgers derived from journal archive events.
package realization

import (
	"database/sql"
	"sync"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	replayreal "planisphere/internal/replay/realization"
	"planisphere/internal/sources/journal"
)

var (
	// salesLedgerJournalEvents restricts cache invalidation to events that actually
	// create, settle, or forfeit ledger records; session-context events are already
	// persisted in SQLite before any subsequent scan or sale is evaluated.
	salesLedgerJournalEvents = journal.NewEventSet(
		journal.EventScan,
		journal.EventSAAScanComplete,
		journal.EventScanOrganic,
		journal.EventSellExplorationData,
		journal.EventMultiSellExplorationData,
		journal.EventSellOrganicData,
		journal.EventResurrect,
		journal.EventNpcCrewPaidWage,
	)
	salesLedgerAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// Ledger holds the chronological, lifetime-reconciled settlement state of all
// scan and organic records across all commander profiles in the archive.
type Ledger = replayreal.Ledger

// Breakdown captures realized payouts, unrealized pending data, losses, and deductions.
type Breakdown = replayreal.Breakdown

// OrganicSaleOutcome records the resolved first-logged status for one ScanOrganic event UID.
type OrganicSaleOutcome = replayreal.OrganicSaleOutcome

// SalesLedger caches the chronological settlement ledger over persisted journal
// events, retaining incremental replay state across journal-only invalidations.
type SalesLedger struct {
	dbConn   *sql.DB
	triggers projection.Triggers

	buildMu sync.Mutex

	mu              sync.Mutex
	cached          *Ledger
	state           *replayreal.LedgerState
	valid           bool
	generation      uint64
	resetGeneration uint64
}

var _ projection.SelectiveProjection = (*SalesLedger)(nil)

// NewSalesLedger creates a SalesLedger projection and registers it on bus.
func NewSalesLedger(dbConn *sql.DB, bus *projection.Bus) *SalesLedger {
	salesLedger := &SalesLedger{
		dbConn: dbConn,
		triggers: projection.Triggers{
			Journal: salesLedgerJournalEvents,
			App:     salesLedgerAppEvents,
		},
	}
	if bus != nil {
		bus.Register(salesLedger)
	}
	return salesLedger
}

// Name returns the projection's diagnostic name.
func (salesLedger *SalesLedger) Name() string {
	return "SalesLedger"
}

// Triggers reports the journal and app events that invalidate SalesLedger.
func (salesLedger *SalesLedger) Triggers() projection.Triggers {
	return salesLedger.triggers
}

// Invalidate clears both the cached ledger snapshot and the retained replay
// watermark so the next Get performs a full cold rebuild.
func (salesLedger *SalesLedger) Invalidate() {
	salesLedger.mu.Lock()
	defer salesLedger.mu.Unlock()
	salesLedger.cached = nil
	salesLedger.state = nil
	salesLedger.valid = false
	salesLedger.generation++
	salesLedger.resetGeneration++
}

// InvalidateObserved retains incremental replay state across journal-only
// invalidations while clearing state on application-level filter or database resets.
func (salesLedger *SalesLedger) InvalidateObserved(observed projection.Triggers) {
	salesLedger.mu.Lock()
	defer salesLedger.mu.Unlock()
	salesLedger.cached = nil
	salesLedger.valid = false
	salesLedger.generation++
	if !observed.App.IsEmpty() {
		salesLedger.state = nil
		salesLedger.resetGeneration++
	}
}

// Get returns the cached all-time realization ledger, advancing from the
// retained primary-key watermark after journal-only invalidations.
func (salesLedger *SalesLedger) Get() (*Ledger, error) {
	if cachedLedger, _, _, _, ok := salesLedger.snapshotState(); ok {
		return cachedLedger, nil
	}
	salesLedger.buildMu.Lock()
	defer salesLedger.buildMu.Unlock()

	cachedLedger, priorState, capturedGeneration, capturedResetGeneration, ok := salesLedger.snapshotState()
	if ok {
		return cachedLedger, nil
	}
	if salesLedger.dbConn == nil {
		return salesLedger.commitState(nil, &Ledger{}, capturedGeneration, capturedResetGeneration), nil
	}
	var nextState *replayreal.LedgerState
	var buildError error
	if priorState != nil {
		nextState, buildError = priorState.Advance(salesLedger.dbConn)
	} else {
		nextState, buildError = replayreal.BuildLedgerState(salesLedger.dbConn, true)
	}
	if buildError != nil {
		return nil, buildError
	}
	return salesLedger.commitState(nextState, nextState.Ledger(), capturedGeneration, capturedResetGeneration), nil
}

func (salesLedger *SalesLedger) snapshotState() (*Ledger, *replayreal.LedgerState, uint64, uint64, bool) {
	salesLedger.mu.Lock()
	defer salesLedger.mu.Unlock()
	if salesLedger.valid {
		return salesLedger.cached, nil, 0, 0, true
	}
	return nil, salesLedger.state, salesLedger.generation, salesLedger.resetGeneration, false
}

func (salesLedger *SalesLedger) commitState(
	nextState *replayreal.LedgerState,
	nextLedger *Ledger,
	capturedGeneration, capturedResetGeneration uint64,
) *Ledger {
	salesLedger.mu.Lock()
	defer salesLedger.mu.Unlock()
	if salesLedger.resetGeneration != capturedResetGeneration {
		if salesLedger.valid {
			return salesLedger.cached
		}
		return nextLedger
	}
	if nextState != nil {
		salesLedger.state = nextState
	}
	if !salesLedger.valid && salesLedger.generation == capturedGeneration {
		salesLedger.cached = nextLedger
		salesLedger.valid = true
	} else if salesLedger.valid {
		return salesLedger.cached
	}
	return nextLedger
}

// IsValid reports whether the SalesLedger currently holds a warm cached value.
func (salesLedger *SalesLedger) IsValid() bool {
	salesLedger.mu.Lock()
	defer salesLedger.mu.Unlock()
	return salesLedger.valid
}

// Generation returns the monotonically increasing invalidation generation counter.
func (salesLedger *SalesLedger) Generation() uint64 {
	salesLedger.mu.Lock()
	defer salesLedger.mu.Unlock()
	return salesLedger.generation
}

// Breakdown computes the aggregated realization breakdown across
// (afterTimestamp, untilTimestamp] for globally enabled profiles, optionally
// applying filterAllowsDiscovery for discovery mode filtering.
func (salesLedger *SalesLedger) Breakdown(
	afterTimestamp, untilTimestamp string,
	filterAllowsDiscovery func(timestamp string) bool,
) (Breakdown, error) {
	return salesLedger.BreakdownScoped(afterTimestamp, untilTimestamp, replay.ProfileScope{}, filterAllowsDiscovery)
}

// BreakdownScoped computes the aggregated realization breakdown across
// (afterTimestamp, untilTimestamp] for scope, optionally applying
// filterAllowsDiscovery for discovery mode filtering.
func (salesLedger *SalesLedger) BreakdownScoped(
	afterTimestamp, untilTimestamp string,
	scope replay.ProfileScope,
	filterAllowsDiscovery func(timestamp string) bool,
) (Breakdown, error) {
	ledgerInstance, getError := salesLedger.Get()
	if getError != nil {
		return Breakdown{}, getError
	}
	return ledgerInstance.BreakdownForScope(afterTimestamp, untilTimestamp, scope, filterAllowsDiscovery), nil
}

// OrganicSaleDetails returns the settled bonus details for an organic scan UID.
func (salesLedger *SalesLedger) OrganicSaleDetails(eventUID string) (OrganicSaleOutcome, bool, error) {
	ledgerInstance, getError := salesLedger.Get()
	if getError != nil {
		return OrganicSaleOutcome{}, false, getError
	}
	outcome, found := ledgerInstance.OrganicSaleDetails(eventUID)
	return outcome, found, nil
}
