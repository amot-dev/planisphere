// Package projections implements self-contained read-only projections backed
// by generation-guarded caches that subscribe to typed journal and application events.
package projections

import (
	"database/sql"
	"errors"
	"fmt"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/reference"
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
type Arrival struct {
	UID           string `json:"uid"`
	Timestamp     string `json:"timestamp"`
	Galactic      string `json:"galactic"`
	Event         string `json:"event"`
	SystemName    string `json:"systemName"`
	SystemAddress int64  `json:"systemAddress"`
}

// SystemArrivals caches chronological star-system entry sequences (FSDJump,
// Location, CarrierJump) for [start, end] time windows, used to advance
// expedition waypoints and validate drag-and-drop reorder slots.
type SystemArrivals struct {
	database *sql.DB
	windows  *projection.KeyedCache[string, []Arrival]
}

// NewSystemArrivals creates a SystemArrivals projection and registers it on bus.
func NewSystemArrivals(dbConn *sql.DB, bus *projection.Bus) *SystemArrivals {
	arrivals := &SystemArrivals{database: dbConn}
	arrivals.windows = projection.NewKeyedCache[string, []Arrival](
		bus,
		"system_arrivals",
		arrivals.Triggers(),
	)
	return arrivals
}

// Triggers declares the journal and application events that invalidate SystemArrivals.
func (arrivals *SystemArrivals) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: systemArrivalsJournalEvents,
		App:     systemArrivalsAppEvents,
	}
}

// InWindow returns cached chronological arrivals within [startTimestamp, endTimestamp]
// (inclusive lower bound; optional inclusive upper bound when non-empty).
func (arrivals *SystemArrivals) InWindow(startTimestamp, endTimestamp string) ([]Arrival, error) {
	if arrivals.database == nil {
		return nil, fmt.Errorf("system arrivals database is nil")
	}
	cacheKey := fmt.Sprintf("%s|%s", startTimestamp, endTimestamp)
	return arrivals.windows.GetOrLoad(cacheKey, func() ([]Arrival, error) {
		return QueryArrivalsInWindow(arrivals.database, startTimestamp, endTimestamp)
	})
}

// CandidatesForSystem returns every recorded visit to the given star system, newest first.
func (arrivals *SystemArrivals) CandidatesForSystem(systemName string, systemAddress int64) ([]Arrival, error) {
	if arrivals.database == nil {
		return nil, fmt.Errorf("system arrivals database is nil")
	}
	return QuerySystemArrivalCandidates(arrivals.database, systemName, systemAddress)
}

// LatestPlayerLocation returns the most recent coordinates and star system for
// the active commanders.
func (arrivals *SystemArrivals) LatestPlayerLocation() (*reference.Vec3, string, error) {
	if arrivals.database == nil {
		return nil, "", fmt.Errorf("system arrivals database is nil")
	}
	return QueryLatestPlayerLocation(arrivals.database)
}

// QueryArrivalsInWindow queries chronological arrivals within [startTimestamp, endTimestamp]
// directly against queryer without using the cache.
func QueryArrivalsInWindow(queryer db.Queryer, startTimestamp, endTimestamp string) ([]Arrival, error) {
	if endTimestamp != "" {
		return loadArrivalsWhere(queryer, `timestamp >= ? AND timestamp <= ?`, startTimestamp, endTimestamp)
	}
	return loadArrivalsWhere(queryer, `timestamp >= ?`, startTimestamp)
}

// QuerySystemArrivalCandidates queries every recorded visit to systemName or
// systemAddress in reverse chronological order (newest first).
func QuerySystemArrivalCandidates(queryer db.Queryer, systemName string, systemAddress int64) ([]Arrival, error) {
	var (
		loadedArrivals []Arrival
		loadError      error
	)
	if systemAddress != 0 {
		loadedArrivals, loadError = loadArrivalsWhere(queryer, `(system_name = ? COLLATE NOCASE OR system_address = ?)`, systemName, systemAddress)
	} else {
		loadedArrivals, loadError = loadArrivalsWhere(queryer, `system_name = ? COLLATE NOCASE`, systemName)
	}
	if loadError != nil {
		return nil, loadError
	}
	for left, right := 0, len(loadedArrivals)-1; left < right; left, right = left+1, right-1 {
		loadedArrivals[left], loadedArrivals[right] = loadedArrivals[right], loadedArrivals[left]
	}
	return loadedArrivals, nil
}

// QueryLatestPlayerLocation queries the most recent coordinates and star system
// for the active commanders across FSDJump, Location, and CarrierJump events.
func QueryLatestPlayerLocation(queryer db.Queryer) (*reference.Vec3, string, error) {
	var (
		starX, starY, starZ sql.NullFloat64
		systemName          sql.NullString
	)
	query := `SELECT star_x, star_y, star_z, system_name FROM (
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'FSDJump'
			  AND ` + journal.CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'Location'
			  AND ` + journal.CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'CarrierJump'
			  AND ` + journal.CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
	) ORDER BY timestamp DESC, id DESC LIMIT 1`
	queryError := queryer.QueryRow(query).Scan(&starX, &starY, &starZ, &systemName)
	if queryError != nil {
		if errors.Is(queryError, sql.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", queryError
	}
	if !starX.Valid || !starY.Valid || !starZ.Valid {
		return nil, systemName.String, nil
	}
	return &reference.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}, systemName.String, nil
}

func loadArrivalsWhere(queryer db.Queryer, whereClause string, queryArgs ...any) ([]Arrival, error) {
	databaseRows, queryError := queryer.Query(
		`SELECT event_uid, timestamp, event, COALESCE(system_name,''), COALESCE(system_address,0)
		FROM journal_events WHERE event IN (`+systemArrivalsJournalEvents.SQLInClause()+`) AND `+journal.CmdrFilterSQL+` AND `+whereClause+` ORDER BY timestamp, id`,
		queryArgs...,
	)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()
	var arrivals []Arrival
	for databaseRows.Next() {
		var arrival Arrival
		if scanError := databaseRows.Scan(&arrival.UID, &arrival.Timestamp, &arrival.Event, &arrival.SystemName, &arrival.SystemAddress); scanError != nil {
			return nil, scanError
		}
		arrival.Galactic = reference.ToGalactic(arrival.Timestamp)
		arrivals = append(arrivals, arrival)
	}
	return arrivals, databaseRows.Err()
}

