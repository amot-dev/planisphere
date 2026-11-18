package projections

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	recoveredJumpsJournalEvents = journal.NewEventSet(
		journal.EventLocation,
	)
	recoveredJumpsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// RecoveredJump represents a hyperspace jump completed server-side during a
// client disconnect, reconstructed from the subsequent LoadGame and Location sequence.
type RecoveredJump = replay.RecoveredJump

// RecoveredJumps caches crash-recovered hyperspace jumps over the active archive.
type RecoveredJumps struct {
	database *sql.DB
	cache    *projection.Cache[[]RecoveredJump]
}

// NewRecoveredJumps creates a RecoveredJumps projection and registers it on bus.
func NewRecoveredJumps(dbConn *sql.DB, bus *projection.Bus) *RecoveredJumps {
	triggers := projection.Triggers{
		Journal: recoveredJumpsJournalEvents,
		App:     recoveredJumpsAppEvents,
	}
	return &RecoveredJumps{
		database: dbConn,
		cache: projection.NewCache[[]RecoveredJump](bus, "RecoveredJumps", triggers, func() ([]RecoveredJump, error) {
			if dbConn == nil {
				return nil, nil
			}
			return replay.ComputeRecoveredJumps(dbConn)
		}),
	}
}

// Triggers reports the journal and app events that invalidate RecoveredJumps.
func (jumps *RecoveredJumps) Triggers() projection.Triggers {
	return jumps.cache.Triggers()
}

// All returns the full cached slice of crash-recovered hyperspace jumps in chronological order.
func (jumps *RecoveredJumps) All() ([]RecoveredJump, error) {
	return jumps.cache.Get()
}

// InRange returns cached crash-recovered jumps falling inside (afterTimestamp, untilTimestamp].
func (jumps *RecoveredJumps) InRange(afterTimestamp, untilTimestamp string) ([]RecoveredJump, error) {
	allRecovered, getError := jumps.All()
	if getError != nil || len(allRecovered) == 0 {
		return nil, getError
	}
	return replay.FilterRecoveredJumpsInRange(allRecovered, afterTimestamp, untilTimestamp), nil
}
