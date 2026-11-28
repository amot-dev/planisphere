package stats

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
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

// CareerCarrierOps caches lifetime Personal Carrier Operations jump and distance metrics per profile scope.
type CareerCarrierOps struct {
	database *sql.DB
	cache    *projection.KeyedCache[string, CarrierOpsMetrics]
}

// NewCareerCarrierOps creates a CareerCarrierOps projection and registers it on bus.
func NewCareerCarrierOps(dbConn *sql.DB, bus *projection.Bus) *CareerCarrierOps {
	carrierOps := &CareerCarrierOps{database: dbConn}
	triggers := projection.Triggers{
		Journal: carrierOpsJournalEvents,
		App:     carrierOpsAppEvents,
	}
	carrierOps.cache = projection.NewKeyedCache[string, CarrierOpsMetrics](
		bus,
		"CareerCarrierOps",
		triggers,
	)
	return carrierOps
}

// Triggers reports the journal and app events that invalidate CareerCarrierOps.
func (carrierOps *CareerCarrierOps) Triggers() projection.Triggers {
	return carrierOps.cache.Triggers()
}

// Get returns the cached all-time personal carrier jump and distance totals for globally enabled profiles.
func (carrierOps *CareerCarrierOps) Get() (CarrierOpsMetrics, error) {
	return carrierOps.GetScoped(replay.ProfileScope{})
}

// GetScoped returns cached all-time personal carrier jump and distance totals for scope.
func (carrierOps *CareerCarrierOps) GetScoped(scope replay.ProfileScope) (CarrierOpsMetrics, error) {
	return carrierOps.cache.GetOrLoad(scope.CacheKeySuffix(), func() (CarrierOpsMetrics, error) {
		return carrierOps.load(scope)
	})
}

// IsValid reports whether CareerCarrierOps currently holds a warm cached value.
func (carrierOps *CareerCarrierOps) IsValid() bool {
	return carrierOps.cache.Len() > 0
}

// Generation returns the monotonically increasing invalidation generation counter.
func (carrierOps *CareerCarrierOps) Generation() uint64 {
	return carrierOps.cache.Generation()
}

func (carrierOps *CareerCarrierOps) load(scope replay.ProfileScope) (CarrierOpsMetrics, error) {
	if carrierOps.database == nil {
		return CarrierOpsMetrics{}, nil
	}
	return replaystats.QueryCareerCarrierOpsScoped(carrierOps.database, scope)
}
