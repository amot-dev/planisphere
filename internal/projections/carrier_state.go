package projections

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	carrierStateJournalEvents = journal.NewEventSet(
		journal.EventCarrierStats,
	)
	carrierStateAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CarrierStatsSnapshot holds the most recent decoded CarrierStats journal event.
type CarrierStatsSnapshot = replay.CarrierStatsSnapshot

// CarrierDepotInfo summarizes the player's Fleet Carrier capacity and Tritium fuel depot.
type CarrierDepotInfo = replay.CarrierDepotInfo

// CarrierState caches the latest CarrierStats identity and fuel snapshot.
type CarrierState struct {
	database    *sql.DB
	latestStats *projection.Cache[CarrierStatsSnapshot]
}

// NewCarrierState creates a CarrierState projection and registers it on bus.
func NewCarrierState(dbConn *sql.DB, bus *projection.Bus) *CarrierState {
	triggers := projection.Triggers{
		Journal: carrierStateJournalEvents,
		App:     carrierStateAppEvents,
	}
	return &CarrierState{
		database: dbConn,
		latestStats: projection.NewCache[CarrierStatsSnapshot](bus, "CarrierState", triggers, func() (CarrierStatsSnapshot, error) {
			if dbConn == nil {
				return CarrierStatsSnapshot{}, nil
			}
			return replay.LoadLatestCarrierStats(dbConn)
		}),
	}
}

// Triggers reports the journal and app events that invalidate CarrierState.
func (state *CarrierState) Triggers() projection.Triggers {
	return state.latestStats.Triggers()
}

// LatestStats returns the cached latest CarrierStats snapshot.
func (state *CarrierState) LatestStats() (CarrierStatsSnapshot, error) {
	return state.latestStats.Get()
}

// IsValid reports whether LatestStats currently holds a warm cached value.
func (state *CarrierState) IsValid() bool {
	return state.latestStats.IsValid()
}

// Generation returns the monotonically increasing invalidation generation counter.
func (state *CarrierState) Generation() uint64 {
	return state.latestStats.Generation()
}
