package replay

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// NonMulticrewSQLFilter excludes Telepresence (Multicrew: true) events where the
// commander never physically relocated their ship or body. Short-circuiting with
// instr avoids parsing raw_json on the vast majority of non-multicrew rows.
const NonMulticrewSQLFilter = `(instr(raw_json, '"Multicrew"') = 0 OR NOT COALESCE(json_extract(raw_json, '$.Multicrew'), 0))`

const nonMulticrewSQLFilter = NonMulticrewSQLFilter

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
	ProfileID     int64  `json:"profileId,omitempty"`
	Commander     string `json:"commander,omitempty"`
}

// ArrivalStayContext identifies the resolved star system address and pilot
// identity (save-slot ProfileID and Commander name) for a waypoint stay.
type ArrivalStayContext struct {
	SystemAddress int64
	ProfileID     int64
	Commander     string
}

// QueryArrivalsInWindowScoped queries chronological arrivals within
// [startTimestamp, endTimestamp] matching scope.
func QueryArrivalsInWindowScoped(queryer db.Queryer, startTimestamp, endTimestamp string, scope ProfileScope) ([]Arrival, error) {
	if endTimestamp != "" {
		return loadArrivalsWhereScoped(queryer, scope, `timestamp >= ? AND timestamp <= ?`, startTimestamp, endTimestamp)
	}
	return loadArrivalsWhereScoped(queryer, scope, `timestamp >= ?`, startTimestamp)
}

// QuerySystemArrivalCandidatesScoped queries every recorded visit to systemName
// or systemAddress matching scope in reverse chronological order (newest first).
func QuerySystemArrivalCandidatesScoped(queryer db.Queryer, systemName string, systemAddress int64, scope ProfileScope) ([]Arrival, error) {
	var (
		loadedArrivals []Arrival
		loadError      error
	)
	if systemAddress != 0 {
		loadedArrivals, loadError = loadArrivalsWhereScoped(queryer, scope, `(system_name = ? COLLATE NOCASE OR system_address = ?)`, systemName, systemAddress)
	} else {
		loadedArrivals, loadError = loadArrivalsWhereScoped(queryer, scope, `system_name = ? COLLATE NOCASE`, systemName)
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
	return QueryLatestPlayerLocationScoped(queryer, ProfileScope{})
}

// QueryLatestPlayerLocationScoped queries the most recent coordinates and star
// system matching scope across FSDJump, Location, and CarrierJump events.
func QueryLatestPlayerLocationScoped(queryer db.Queryer, scope ProfileScope) (*reference.Vec3, string, error) {
	var (
		starX, starY, starZ sql.NullFloat64
		systemName          sql.NullString
	)
	scopePredicate := scope.SQLPredicate("")
	query := `SELECT star_x, star_y, star_z, system_name FROM (
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'FSDJump' AND ` + scopePredicate + ` AND ` + nonMulticrewSQLFilter + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'Location' AND ` + scopePredicate + ` AND ` + nonMulticrewSQLFilter + `
			ORDER BY timestamp DESC, id DESC LIMIT 1
		)
		UNION ALL
		SELECT * FROM (
			SELECT star_x, star_y, star_z, system_name, timestamp, id
			FROM journal_events
			WHERE event = 'CarrierJump' AND ` + scopePredicate + ` AND ` + nonMulticrewSQLFilter + `
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

// ResolveArrivalStayContextScoped resolves the star system address, save-slot
// ProfileID, and stay commander for a waypoint arrival within scope.
func ResolveArrivalStayContextScoped(queryer db.Queryer, visitEventUID, visitedAt, systemName string, systemAddress int64, scope ProfileScope) (ArrivalStayContext, error) {
	if visitEventUID != "" {
		uidQuery := `SELECT COALESCE(system_address, 0), COALESCE(profile_id, 0), COALESCE(commander, '') FROM journal_events WHERE event_uid = ? AND ` + NonMulticrewSQLFilter
		if !scope.IsAllEnabled() {
			uidQuery += ` AND ` + scope.SQLPredicate("")
		}
		var arrivalAddress, stayProfileID int64
		var stayCommander string
		lookupError := queryer.QueryRow(uidQuery, visitEventUID).Scan(&arrivalAddress, &stayProfileID, &stayCommander)
		if lookupError == nil {
			if systemAddress != 0 {
				arrivalAddress = systemAddress
			}
			return ArrivalStayContext{SystemAddress: arrivalAddress, ProfileID: stayProfileID, Commander: stayCommander}, nil
		}
		if !errors.Is(lookupError, sql.ErrNoRows) {
			return ArrivalStayContext{}, fmt.Errorf("resolve arrival stay context by uid %q: %w", visitEventUID, lookupError)
		}
	}

	trimmedSystemName := strings.TrimSpace(systemName)
	if visitedAt == "" || (systemAddress == 0 && trimmedSystemName == "") {
		return ArrivalStayContext{SystemAddress: systemAddress}, nil
	}
	systemPredicate, queryArgs := `system_name = ? COLLATE NOCASE`, []any{visitedAt, trimmedSystemName}
	if systemAddress != 0 {
		systemPredicate, queryArgs = `system_address = ?`, []any{visitedAt, systemAddress}
	}
	fallbackQuery := `SELECT COALESCE(system_address, 0), COALESCE(profile_id, 0), COALESCE(commander, '')
		FROM journal_events
		WHERE event IN (` + journal.FormatSQLInClause(ArrivalEvents) + `)
		  AND timestamp = ? AND ` + systemPredicate + ` AND ` + scope.SQLPredicate("") + ` AND ` + NonMulticrewSQLFilter + `
		ORDER BY id LIMIT 1`
	var arrivalAddress, stayProfileID int64
	var stayCommander string
	if fallbackError := queryer.QueryRow(fallbackQuery, queryArgs...).Scan(&arrivalAddress, &stayProfileID, &stayCommander); fallbackError != nil {
		if errors.Is(fallbackError, sql.ErrNoRows) {
			return ArrivalStayContext{SystemAddress: systemAddress}, nil
		}
		return ArrivalStayContext{}, fmt.Errorf("resolve arrival stay context at %s: %w", visitedAt, fallbackError)
	}
	if systemAddress != 0 {
		arrivalAddress = systemAddress
	}
	return ArrivalStayContext{SystemAddress: arrivalAddress, ProfileID: stayProfileID, Commander: stayCommander}, nil
}

// QueryLastInSystemActivityTimestampForStay returns the latest non-multicrew
// exploration activity timestamp (non-belt body Scan, planet SAAScanComplete, or
// ScanOrganic Analyse) for the target system and stay actor within
// (afterTimestamp, untilTimestamp] and scope, or an empty string when none exist.
func QueryLastInSystemActivityTimestampForStay(queryer db.Queryer, afterTimestamp, untilTimestamp, systemName string, systemAddress int64, stay ArrivalStayContext, scope ProfileScope) (string, error) {
	query := `SELECT COALESCE(MAX(timestamp), '')
		FROM journal_events
		WHERE timestamp > ?`
	queryArgs := []any{afterTimestamp}
	if untilTimestamp != "" {
		query += ` AND timestamp <= ?`
		queryArgs = append(queryArgs, untilTimestamp)
	}
	query += `
		  AND (
			(event = 'Scan' AND COALESCE(body_name, '') NOT LIKE '%Belt Cluster%' AND (COALESCE(json_extract(raw_json, '$.StarType'), '') != '' OR COALESCE(json_extract(raw_json, '$.PlanetClass'), '') != ''))
			OR (event = 'SAAScanComplete' AND COALESCE(json_extract(raw_json, '$.EfficiencyTarget'), 0) > 0)
			OR (event = 'ScanOrganic' AND LOWER(COALESCE(json_extract(raw_json, '$.ScanType'), '')) = 'analyse')
		  )`
	if systemAddress != 0 {
		query += ` AND system_address = ?`
		queryArgs = append(queryArgs, systemAddress)
	} else if trimmedSystemName := strings.TrimSpace(systemName); trimmedSystemName != "" {
		query += ` AND system_name = ? COLLATE NOCASE`
		queryArgs = append(queryArgs, trimmedSystemName)
	}
	trimmedCommander := strings.TrimSpace(stay.Commander)
	if stay.ProfileID > 0 && trimmedCommander != "" && scope.IsAllEnabled() {
		query += ` AND (+profile_id = ? OR (COALESCE(profile_id, 0) = 0 AND (COALESCE(commander, '') = '' OR commander = ? COLLATE NOCASE)))`
		queryArgs = append(queryArgs, stay.ProfileID, trimmedCommander)
	} else if stay.ProfileID > 0 {
		query += ` AND +profile_id = ?`
		queryArgs = append(queryArgs, stay.ProfileID)
	} else if trimmedCommander != "" {
		query += ` AND (COALESCE(commander, '') = '' OR commander = ? COLLATE NOCASE)`
		queryArgs = append(queryArgs, trimmedCommander)
	}
	query += commanderFilterPredicate(queryer, scope) + ` AND ` + NonMulticrewSQLFilter

	var lastTimestamp string
	if scanError := queryer.QueryRow(query, queryArgs...).Scan(&lastTimestamp); scanError != nil {
		return "", fmt.Errorf("query last in-system activity after %s: %w", afterTimestamp, scanError)
	}
	return lastTimestamp, nil
}

func loadArrivalsWhereScoped(queryer db.Queryer, scope ProfileScope, whereClause string, queryArgs ...any) ([]Arrival, error) {
	databaseRows, queryError := queryer.Query(
		`SELECT event_uid, timestamp, event, COALESCE(system_name,''), COALESCE(system_address,0), COALESCE(profile_id,0), COALESCE(commander,'')
		FROM journal_events WHERE event IN (`+journal.FormatSQLInClause(ArrivalEvents)+`) AND (`+whereClause+`) AND `+scope.SQLPredicate("")+` AND `+nonMulticrewSQLFilter+` ORDER BY timestamp, id`,
		queryArgs...,
	)
	if queryError != nil {
		return nil, queryError
	}
	defer databaseRows.Close()
	var arrivals []Arrival
	for databaseRows.Next() {
		var arrival Arrival
		if scanError := databaseRows.Scan(&arrival.UID, &arrival.Timestamp, &arrival.Event, &arrival.SystemName, &arrival.SystemAddress, &arrival.ProfileID, &arrival.Commander); scanError != nil {
			return nil, scanError
		}
		arrival.Galactic = reference.ToGalactic(arrival.Timestamp)
		arrivals = append(arrivals, arrival)
	}
	return arrivals, databaseRows.Err()
}
