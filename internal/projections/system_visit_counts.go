package projections

import (
	"database/sql"
	"strings"

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
// requested systemNames across globally enabled profiles, loading only uncached systems from SQLite.
func (counts *SystemVisitCounts) CountsForSystems(systemNames []string) (map[string]int, error) {
	return counts.CountsForSystemsScoped(systemNames, replay.ProfileScope{})
}

// CountsForSystemsScoped returns visit counts keyed by replay.NormalizeSystemKey
// for all requested systemNames matching scope.
func (counts *SystemVisitCounts) CountsForSystemsScoped(systemNames []string, scope replay.ProfileScope) (map[string]int, error) {
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
	if scope.IsAllEnabled() {
		return counts.entries.GetManyOrLoad(normalizedKeys, func(missingNames []string) (map[string]int, error) {
			return counts.loadCountsForScope(missingNames, replay.ProfileScope{})
		})
	}
	return counts.countsForExplicitScope(normalizedKeys, scope)
}

func (counts *SystemVisitCounts) loadCountsForScope(missingNames []string, scope replay.ProfileScope) (map[string]int, error) {
	var recovered []RecoveredJump
	if counts.recoveredJumps != nil {
		var err error
		if recovered, err = counts.recoveredJumps.AllScoped(scope); err != nil {
			return nil, err
		}
	}
	return replay.QuerySystemVisitCountsScoped(counts.database, missingNames, recovered, scope)
}

func (counts *SystemVisitCounts) countsForExplicitScope(normalizedKeys []string, scope replay.ProfileScope) (map[string]int, error) {
	prefix := scope.CacheKeySuffix() + "|"
	scopedKeys := make([]string, len(normalizedKeys))
	for index, key := range normalizedKeys {
		scopedKeys[index] = prefix + key
	}
	cached, err := counts.entries.GetManyOrLoad(scopedKeys, func(missingScoped []string) (map[string]int, error) {
		missingNames := make([]string, len(missingScoped))
		for index, key := range missingScoped {
			missingNames[index] = strings.TrimPrefix(key, prefix)
		}
		byName, queryErr := counts.loadCountsForScope(missingNames, scope)
		if queryErr != nil {
			return nil, queryErr
		}
		byScoped := make(map[string]int, len(byName))
		for key, count := range byName {
			byScoped[prefix+key] = count
		}
		return byScoped, nil
	})
	if err != nil {
		return nil, err
	}
	resolved := make(map[string]int, len(normalizedKeys))
	for _, key := range normalizedKeys {
		resolved[key] = cached[prefix+key]
	}
	return resolved, nil
}
