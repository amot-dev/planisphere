// Package projections maintains reactive in-memory caches over replayed Elite Dangerous journal archive queries.
package projections

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	systemArrivalsJournalEvents = journal.NewEventSet(
		journal.EventFSDJump,
		journal.EventLocation,
		journal.EventCarrierJump,
	)
	systemArrivalsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// Arrival is a journal event that placed the commander in a star system.
type Arrival = replay.Arrival

// SystemArrivals caches commander star-system arrival queries for bounded expedition windows.
type SystemArrivals struct {
	database *sql.DB
	windows  *projection.KeyedCache[string, []Arrival]
}

// NewSystemArrivals creates a SystemArrivals projection and registers it on bus.
func NewSystemArrivals(dbConn *sql.DB, bus *projection.Bus) *SystemArrivals {
	triggers := projection.Triggers{
		Journal: systemArrivalsJournalEvents,
		App:     systemArrivalsAppEvents,
	}
	windows := projection.NewKeyedCache[string, []Arrival](bus, "SystemArrivals", triggers)
	windows.SetInvalidationFilter(func(observed projection.Triggers, windowKey string) bool {
		if !observed.App.IsEmpty() {
			return true
		}
		return !projection.WindowEndsBefore(windowKey, observed.EarliestTimestamp)
	})
	return &SystemArrivals{
		database: dbConn,
		windows:  windows,
	}
}

// Triggers reports the journal and app events that invalidate SystemArrivals.
func (arrivals *SystemArrivals) Triggers() projection.Triggers {
	return arrivals.windows.Triggers()
}

// InWindow returns cached chronological arrivals within [startTimestamp, endTimestamp]
// (inclusive lower bound; optional inclusive upper bound when non-empty).
func (arrivals *SystemArrivals) InWindow(startTimestamp, endTimestamp string) ([]Arrival, error) {
	if arrivals.database == nil {
		return nil, nil
	}
	windowKey := startTimestamp + "|" + endTimestamp
	return arrivals.windows.GetOrLoad(windowKey, func() ([]Arrival, error) {
		return replay.QueryArrivalsInWindow(arrivals.database, startTimestamp, endTimestamp)
	})
}
