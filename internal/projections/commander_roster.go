// Package projections implements self-contained read-only projections backed
// by generation-guarded caches that subscribe to typed journal and application events.
package projections

import (
	"database/sql"
	"fmt"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

var (
	commanderRosterJournalEvents = journal.AllEvents
	commanderRosterAppEvents     = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CommanderRosterSnapshot holds the full commander filter roster alongside the
// earliest career timestamp among active commanders.
type CommanderRosterSnapshot struct {
	Commanders          []journal.Commander
	CareerStart         string
	CareerStartGalactic string
}

// CommanderRoster caches all discovered commanders across the journal archive,
// their enabled filter state, per-commander event counts, and the earliest
// active career start timestamp.
type CommanderRoster struct {
	database *sql.DB
	snapshot *projection.Cache[CommanderRosterSnapshot]
}

// NewCommanderRoster creates a CommanderRoster projection and registers it on bus.
func NewCommanderRoster(dbConn *sql.DB, bus *projection.Bus) *CommanderRoster {
	roster := &CommanderRoster{database: dbConn}
	roster.snapshot = projection.NewCache(
		bus,
		"commander_roster",
		roster.Triggers(),
		roster.loadSnapshot,
	)
	return roster
}

// Triggers declares the journal and application events that invalidate CommanderRoster.
func (roster *CommanderRoster) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: commanderRosterJournalEvents,
		App:     commanderRosterAppEvents,
	}
}

// Snapshot returns the cached commander roster and active career start timestamps.
func (roster *CommanderRoster) Snapshot() (CommanderRosterSnapshot, error) {
	return roster.snapshot.Get()
}

// IsValid reports whether the commander roster snapshot is currently cached.
func (roster *CommanderRoster) IsValid() bool {
	return roster.snapshot.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (roster *CommanderRoster) Generation() uint64 {
	return roster.snapshot.Generation()
}

// Commanders returns a copy of all discovered commanders ordered by first seen.
func (roster *CommanderRoster) Commanders() ([]journal.Commander, error) {
	rosterSnapshot, loadError := roster.Snapshot()
	if loadError != nil {
		return nil, loadError
	}
	commandersCopy := make([]journal.Commander, len(rosterSnapshot.Commanders))
	copy(commandersCopy, rosterSnapshot.Commanders)
	return commandersCopy, nil
}

// CareerStart returns the earliest active commander timestamp in ISO-8601 UTC
// and Galactic Standard Time formats.
func (roster *CommanderRoster) CareerStart() (string, string, error) {
	rosterSnapshot, loadError := roster.Snapshot()
	if loadError != nil {
		return "", "", loadError
	}
	return rosterSnapshot.CareerStart, rosterSnapshot.CareerStartGalactic, nil
}

func (roster *CommanderRoster) loadSnapshot() (CommanderRosterSnapshot, error) {
	if roster.database == nil {
		return CommanderRosterSnapshot{}, fmt.Errorf("commander roster database is nil")
	}
	commandersList, listError := journal.ListCommanders(roster.database)
	if listError != nil {
		return CommanderRosterSnapshot{}, listError
	}
	careerStartTimestamp, careerStartGalactic := queryCareerStartTimestamp(roster.database)
	return CommanderRosterSnapshot{
		Commanders:          commandersList,
		CareerStart:         careerStartTimestamp,
		CareerStartGalactic: careerStartGalactic,
	}, nil
}

func queryCareerStartTimestamp(queryer db.Queryer) (string, string) {
	var earliestSeenTimestamp sql.NullString
	_ = queryer.QueryRow(
		`SELECT MIN(commanders.first_seen) FROM commanders WHERE commanders.enabled = 1 AND commanders.first_seen IS NOT NULL AND commanders.first_seen != ''`,
	).Scan(&earliestSeenTimestamp)
	if !earliestSeenTimestamp.Valid || earliestSeenTimestamp.String == "" {
		_ = queryer.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&earliestSeenTimestamp)
	}
	if !earliestSeenTimestamp.Valid || earliestSeenTimestamp.String == "" {
		return "", ""
	}
	return earliestSeenTimestamp.String, reference.ToGalactic(earliestSeenTimestamp.String)
}

