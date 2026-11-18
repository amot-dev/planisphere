package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
)

var (
	carrierOpsJournalEvents = journal.NewEventSet(
		journal.EventStatistics,
	)
	carrierOpsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CareerCarrierOps caches lifetime Personal Carrier Operations jump and distance metrics.
type CareerCarrierOps struct {
	database *sql.DB
	cache    *projection.Cache[CarrierOpsMetrics]
}

// NewCareerCarrierOps creates a CareerCarrierOps projection and registers it on bus.
func NewCareerCarrierOps(dbConn *sql.DB, bus *projection.Bus) *CareerCarrierOps {
	carrierOps := &CareerCarrierOps{database: dbConn}
	triggers := projection.Triggers{
		Journal: carrierOpsJournalEvents,
		App:     carrierOpsAppEvents,
	}
	carrierOps.cache = projection.NewCache[CarrierOpsMetrics](
		bus,
		"CareerCarrierOps",
		triggers,
		carrierOps.load,
	)
	return carrierOps
}

// Triggers reports the journal and app events that invalidate CareerCarrierOps.
func (carrierOps *CareerCarrierOps) Triggers() projection.Triggers {
	return carrierOps.cache.Triggers()
}

// Get returns the cached all-time personal carrier jump and distance totals.
func (carrierOps *CareerCarrierOps) Get() (CarrierOpsMetrics, error) {
	return carrierOps.cache.Get()
}

// IsValid reports whether CareerCarrierOps currently holds a warm cached value.
func (carrierOps *CareerCarrierOps) IsValid() bool {
	return carrierOps.cache.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (carrierOps *CareerCarrierOps) Generation() uint64 {
	return carrierOps.cache.Generation()
}

func (carrierOps *CareerCarrierOps) load() (CarrierOpsMetrics, error) {
	if carrierOps.database == nil {
		return CarrierOpsMetrics{}, nil
	}
	return replaystats.QueryCareerCarrierOps(carrierOps.database)
}
