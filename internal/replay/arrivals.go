package replay

import (
	"database/sql"
	"errors"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// ArrivalEvents is the set of journal events that place the commander in a system.
var ArrivalEvents = journal.NewEventSet(
	journal.EventFSDJump,
	journal.EventLocation,
	journal.EventCarrierJump,
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

// QueryArrivalsInWindow queries chronological arrivals within [startTimestamp, endTimestamp].
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
			  AND ` + CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'Location'
			  AND ` + CmdrFilterSQL + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'CarrierJump'
			  AND ` + CmdrFilterSQL + `
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
		FROM journal_events WHERE event IN (`+journal.FormatSQLInClause(ArrivalEvents)+`) AND `+CmdrFilterSQL+` AND `+whereClause+` ORDER BY timestamp, id`,
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

