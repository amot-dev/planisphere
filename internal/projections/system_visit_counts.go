package projections

import (
	"database/sql"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
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

// NormalizeSystemKey canonicalizes a star system name for case-insensitive
// and whitespace-normalized cache and map lookups.
func NormalizeSystemKey(systemName string) string {
	return strings.ToLower(strings.Join(strings.Fields(systemName), " "))
}

// SystemVisitCounts caches all-time commander visit counts per star system
// (including crash-recovered jumps) for planner coordinate status indicators.
type SystemVisitCounts struct {
	database       *sql.DB
	recoveredJumps *RecoveredJumps
	countsBySystem *projection.KeyedCache[string, int]
}

// NewSystemVisitCounts creates a SystemVisitCounts projection and registers it on bus.
func NewSystemVisitCounts(dbConn *sql.DB, bus *projection.Bus, recoveredJumps *RecoveredJumps) *SystemVisitCounts {
	counts := &SystemVisitCounts{
		database:       dbConn,
		recoveredJumps: recoveredJumps,
	}
	counts.countsBySystem = projection.NewKeyedCache[string, int](
		bus,
		"system_visit_counts",
		counts.Triggers(),
	)
	return counts
}

// Triggers declares the journal and application events that invalidate SystemVisitCounts.
func (counts *SystemVisitCounts) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: systemVisitCountsJournalEvents,
		App:     systemVisitCountsAppEvents,
	}
}

// CountsForSystems returns visit counts keyed by NormalizeSystemKey for all
// requested systemNames, loading only uncached systems from SQLite.
func (counts *SystemVisitCounts) CountsForSystems(systemNames []string) (map[string]int, error) {
	if counts.database == nil {
		return nil, fmt.Errorf("system visit counts database is nil")
	}
	normalizedKeys := make([]string, 0, len(systemNames))
	seenKeys := make(map[string]bool, len(systemNames))
	for _, rawName := range systemNames {
		normalizedName := NormalizeSystemKey(rawName)
		if normalizedName != "" && !seenKeys[normalizedName] {
			seenKeys[normalizedName] = true
			normalizedKeys = append(normalizedKeys, normalizedName)
		}
	}
	if len(normalizedKeys) == 0 {
		return nil, nil
	}
	return counts.countsBySystem.GetManyOrLoad(normalizedKeys, func(missingNormalizedNames []string) (map[string]int, error) {
		var (
			recovered    []RecoveredJump
			recoverError error
		)
		if counts.recoveredJumps != nil {
			recovered, recoverError = counts.recoveredJumps.All()
		} else {
			recovered, recoverError = ComputeRecoveredJumpsInRange(counts.database, "", "")
		}
		if recoverError != nil {
			return nil, recoverError
		}
		return QuerySystemVisitCounts(counts.database, missingNormalizedNames, recovered)
	})
}

// QuerySystemVisitCounts queries commander visit counts for missingNormalizedNames
// against queryer, adding any matching crash-recovered jumps from recovered.
func QuerySystemVisitCounts(
	queryer db.Queryer,
	missingNormalizedNames []string,
	recovered []RecoveredJump,
) (map[string]int, error) {
	if len(missingNormalizedNames) == 0 {
		return map[string]int{}, nil
	}
	placeholders := make([]string, len(missingNormalizedNames))
	queryArgs := make([]any, len(missingNormalizedNames))
	for i, name := range missingNormalizedNames {
		placeholders[i] = "?"
		queryArgs[i] = name
	}
	query := `SELECT system_name, COUNT(*), COALESCE(SUM(CASE WHEN event IN ('FSDJump', 'CarrierJump') THEN 1 ELSE 0 END), 0)
		FROM journal_events
		WHERE system_name COLLATE NOCASE IN (` + strings.Join(placeholders, ",") + `)
		  AND (event IN ('FSDJump', 'Location', 'CarrierJump') OR star_x IS NOT NULL)
		  AND ` + journal.CmdrFilterSQL + `
		GROUP BY system_name COLLATE NOCASE`
	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()

	totalBySystem := make(map[string]int, len(missingNormalizedNames))
	jumpsBySystem := make(map[string]int, len(missingNormalizedNames))
	for databaseRows.Next() {
		var matchedSystem string
		var totalEvents, jumpEvents int
		if scanError := databaseRows.Scan(&matchedSystem, &totalEvents, &jumpEvents); scanError != nil {
			return nil, scanError
		}
		normalizedName := NormalizeSystemKey(matchedSystem)
		totalBySystem[normalizedName] += totalEvents
		jumpsBySystem[normalizedName] += jumpEvents
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return nil, iterationError
	}

	for _, recoveredJump := range recovered {
		normalizedSystem := NormalizeSystemKey(recoveredJump.SystemName)
		if totalBySystem[normalizedSystem] > 0 {
			jumpsBySystem[normalizedSystem]++
		}
	}

	resolvedCounts := make(map[string]int, len(missingNormalizedNames))
	for _, normalizedName := range missingNormalizedNames {
		if totalBySystem[normalizedName] > 0 {
			visitCount := jumpsBySystem[normalizedName]
			if visitCount == 0 {
				visitCount = 1
			}
			resolvedCounts[normalizedName] = visitCount
		} else {
			resolvedCounts[normalizedName] = 0
		}
	}
	return resolvedCounts, nil
}

