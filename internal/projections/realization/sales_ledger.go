// Package realization maintains reactive projection caches for settlement,
// exploration profit, and loss ledgers derived from journal archive events.
package realization

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
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
// scan and organic records across a commander's history.
type Ledger = replayreal.Ledger

// Breakdown captures realized payouts, unrealized pending data, losses, and deductions.
type Breakdown = replayreal.Breakdown

// OrganicSaleOutcome records the resolved first-logged status for one ScanOrganic event UID.
type OrganicSaleOutcome = replayreal.OrganicSaleOutcome

// SalesLedger caches the chronological settlement ledger over persisted journal events.
type SalesLedger struct {
	ledger *projection.Cache[*Ledger]
}

// NewSalesLedger creates a SalesLedger projection and registers it on bus.
func NewSalesLedger(dbConn *sql.DB, bus *projection.Bus) *SalesLedger {
	triggers := projection.Triggers{
		Journal: salesLedgerJournalEvents,
		App:     salesLedgerAppEvents,
	}
	return &SalesLedger{
		ledger: projection.NewCache[*Ledger](bus, "SalesLedger", triggers, func() (*Ledger, error) {
			if dbConn == nil {
				return &Ledger{}, nil
			}
			return replayreal.BuildLedger(dbConn, true)
		}),
	}
}

// Triggers reports the journal and app events that invalidate SalesLedger.
func (salesLedger *SalesLedger) Triggers() projection.Triggers {
	return salesLedger.ledger.Triggers()
}

// Get returns the cached all-time realization ledger.
func (salesLedger *SalesLedger) Get() (*Ledger, error) {
	return salesLedger.ledger.Get()
}

// IsValid reports whether the SalesLedger currently holds a warm cached value.
func (salesLedger *SalesLedger) IsValid() bool {
	return salesLedger.ledger.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (salesLedger *SalesLedger) Generation() uint64 {
	return salesLedger.ledger.Generation()
}

// Breakdown computes the aggregated realization breakdown across (afterTimestamp, untilTimestamp],
// optionally applying filterAllowsDiscovery for discovery mode filtering.
func (salesLedger *SalesLedger) Breakdown(
	afterTimestamp, untilTimestamp string,
	filterAllowsDiscovery func(timestamp string) bool,
) (Breakdown, error) {
	ledgerInstance, getError := salesLedger.Get()
	if getError != nil {
		return Breakdown{}, getError
	}
	return ledgerInstance.Breakdown(afterTimestamp, untilTimestamp, filterAllowsDiscovery), nil
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
