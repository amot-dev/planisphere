package projections

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	shipHistoryJournalEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventShipyardSwap,
	)
	shipHistoryAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// ShipSwitch is one player starship switch in chronological timeline order.
type ShipSwitch = replay.ShipSwitch

// ShipTransition represents entering a different starship while traversing an expedition leg.
type ShipTransition = replay.ShipTransition

// ShipHistory caches player starship timelines across bounded expedition windows.
type ShipHistory struct {
	database *sql.DB
	windows  *projection.KeyedCache[string, []ShipSwitch]
}

// NewShipHistory creates a ShipHistory projection and registers it on bus.
func NewShipHistory(dbConn *sql.DB, bus *projection.Bus) *ShipHistory {
	triggers := projection.Triggers{
		Journal: shipHistoryJournalEvents,
		App:     shipHistoryAppEvents,
	}
	return &ShipHistory{
		database: dbConn,
		windows:  projection.NewKeyedCache[string, []ShipSwitch](bus, "ShipHistory", triggers),
	}
}

// Triggers reports the journal and app events that invalidate ShipHistory.
func (history *ShipHistory) Triggers() projection.Triggers {
	return history.windows.Triggers()
}

// Timeline returns cached chronological player ship switches covering
// [startTimestamp, endTimestamp], seeded with the active ship at or before startTimestamp.
func (history *ShipHistory) Timeline(startTimestamp, endTimestamp string) ([]ShipSwitch, error) {
	if history.database == nil {
		return nil, nil
	}
	windowKey := startTimestamp + "|" + endTimestamp
	return history.windows.GetOrLoad(windowKey, func() ([]ShipSwitch, error) {
		return replay.QueryShipTimeline(history.database, startTimestamp, endTimestamp)
	})
}
