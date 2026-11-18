package projections

import (
	"database/sql"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	systemVisitCountsJournalEvents = journal.NewEventSet(
		journal.EventFSDJump,
		journal.EventLocation,
		journal.EventCarrierJump,
	)
	systemVisitCountsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// SystemVisitCounts caches case-insensitive commander visit counts per star system.
type SystemVisitCounts struct {
	database       *sql.DB
	recoveredJumps *RecoveredJumps
	entries        *projection.KeyedCache[string, int]
}

// NewSystemVisitCounts creates a SystemVisitCounts projection and registers it on bus.
func NewSystemVisitCounts(dbConn *sql.DB, bus *projection.Bus, recoveredJumps *RecoveredJumps) *SystemVisitCounts {
	triggers := projection.Triggers{
		Journal: systemVisitCountsJournalEvents,
		App:     systemVisitCountsAppEvents,
	}
	return &SystemVisitCounts{
		database:       dbConn,
		recoveredJumps: recoveredJumps,
		entries:        projection.NewKeyedCache[string, int](bus, "SystemVisitCounts", triggers),
	}
}

// Triggers reports the journal and app events that invalidate SystemVisitCounts.
func (counts *SystemVisitCounts) Triggers() projection.Triggers {
	return counts.entries.Triggers()
}

// CountsForSystems returns visit counts keyed by replay.NormalizeSystemKey for all
// requested systemNames, loading only uncached systems from SQLite.
func (counts *SystemVisitCounts) CountsForSystems(systemNames []string) (map[string]int, error) {
	if counts.database == nil || len(systemNames) == 0 {
		return map[string]int{}, nil
	}

	normalizedKeys := make([]string, 0, len(systemNames))
	for _, systemName := range systemNames {
		if normalized := replay.NormalizeSystemKey(systemName); normalized != "" {
			normalizedKeys = append(normalizedKeys, normalized)
		}
	}
	if len(normalizedKeys) == 0 {
		return map[string]int{}, nil
	}

	return counts.entries.GetManyOrLoad(normalizedKeys, func(missingKeys []string) (map[string]int, error) {
		var recovered []RecoveredJump
		if counts.recoveredJumps != nil {
			var recoveredError error
			recovered, recoveredError = counts.recoveredJumps.All()
			if recoveredError != nil {
				return nil, recoveredError
			}
		}
		return replay.QuerySystemVisitCounts(counts.database, missingKeys, recovered)
	})
}
