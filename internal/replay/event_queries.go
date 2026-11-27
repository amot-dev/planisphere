package replay

import (
	"database/sql"
	"errors"
	"fmt"
	"math/bits"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

const selectJournalRowColumnsSQL = `SELECT id, event_uid, timestamp, event, COALESCE(system_name, ''), system_address, body_id, star_x, star_y, star_z, COALESCE(profile_id, 0), COALESCE(commander, ''), raw_json`

// Row is a persisted journal_events record loaded for replay.
type Row struct {
	ID            int64
	UID           string
	Timestamp     string
	Type          journal.EventType
	Event         string
	SystemName    string
	SystemAddress sql.NullInt64
	BodyID        sql.NullInt64
	StarPos       *reference.Vec3
	ProfileID     int64
	Commander     string
	RawJSON       string
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJournalRow(scanner rowScanner) (Row, error) {
	var rowItem Row
	var starX, starY, starZ sql.NullFloat64
	scanError := scanner.Scan(
		&rowItem.ID,
		&rowItem.UID,
		&rowItem.Timestamp,
		&rowItem.Event,
		&rowItem.SystemName,
		&rowItem.SystemAddress,
		&rowItem.BodyID,
		&starX,
		&starY,
		&starZ,
		&rowItem.ProfileID,
		&rowItem.Commander,
		&rowItem.RawJSON,
	)
	if scanError != nil {
		return Row{}, scanError
	}
	if parsedType, knownType := journal.ParseEventType(rowItem.Event); knownType {
		rowItem.Type = parsedType
	}
	if starX.Valid && starY.Valid && starZ.Valid {
		rowItem.StarPos = &reference.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}
	}
	return rowItem, nil
}

func commanderFilterPredicate(queryer db.Queryer, scope ProfileScope) string {
	if !scope.IsAllEnabled() {
		return " AND " + scope.SQLPredicate("")
	}
	hasDisabled, checkError := HasDisabledCommanders(queryer)
	if checkError != nil || hasDisabled {
		return " AND " + CmdrFilterSQL
	}
	return ""
}

// LoadEventsScoped queries persisted journal events matching eventSet and scope
// within (afterTimestamp, untilTimestamp] in chronological (timestamp, id) order.
func LoadEventsScoped(queryer db.Queryer, afterTimestamp, untilTimestamp string, eventSet journal.EventSet, scope ProfileScope) ([]Row, error) {
	return loadEventsWithPredicate(queryer, afterTimestamp, untilTimestamp, eventSet, commanderFilterPredicate(queryer, scope))
}

// LoadEventsAllProfiles queries persisted journal events matching eventSet
// across all enabled and disabled profiles in chronological (timestamp, id) order.
func LoadEventsAllProfiles(queryer db.Queryer, afterTimestamp, untilTimestamp string, eventSet journal.EventSet) ([]Row, error) {
	return loadEventsWithPredicate(queryer, afterTimestamp, untilTimestamp, eventSet, "")
}

// LoadEventsAllProfilesAfterID queries persisted journal events matching eventSet
// with primary key id > afterID across all enabled and disabled profiles in
// chronological (timestamp, id) order.
func LoadEventsAllProfilesAfterID(queryer db.Queryer, afterID int64, eventSet journal.EventSet) ([]Row, error) {
	if eventSet.IsEmpty() {
		return nil, nil
	}
	// Unary +event and +timestamp keep SQLite on SEARCH journal_events USING INTEGER PRIMARY KEY (rowid>?)
	// so live tail reads never scan idx_journal_events_event_ts or idx_journal_events_ts across history.
	query := selectJournalRowColumnsSQL + `
		FROM journal_events WHERE id > ? AND +event IN (` + journal.FormatSQLInClause(eventSet) + `)
		ORDER BY +timestamp, id`
	return queryJournalRows(queryer, 16, query, afterID)
}

func loadEventsWithPredicate(
	queryer db.Queryer,
	afterTimestamp, untilTimestamp string,
	eventSet journal.EventSet,
	filterClause string,
) ([]Row, error) {
	if eventSet.IsEmpty() {
		return nil, nil
	}
	if untilTimestamp == "" {
		untilTimestamp = "9999-12-31T23:59:59Z"
	}

	// Unary +event prevents SQLite from union-scanning idx_journal_events_event_ts
	// across broad unbounded event sets and sorting in a temporary B-tree.
	eventColumn := "event"
	if afterTimestamp == "" && bits.OnesCount64(uint64(eventSet)) >= 6 {
		eventColumn = "+event"
	}

	query := selectJournalRowColumnsSQL + `
		FROM journal_events WHERE ` + eventColumn + ` IN (` + journal.FormatSQLInClause(eventSet) + `) AND timestamp > ? AND timestamp <= ?` +
		filterClause + ` ORDER BY timestamp, id`
	return queryJournalRows(queryer, 256, query, afterTimestamp, untilTimestamp)
}

func queryJournalRows(queryer db.Queryer, initialCapacity int, query string, args ...any) ([]Row, error) {
	rows, queryError := queryer.Query(query, args...)
	if queryError != nil {
		return nil, fmt.Errorf("query journal events: %w", queryError)
	}
	defer rows.Close()

	loadedRows := make([]Row, 0, initialCapacity)
	for rows.Next() {
		rowItem, scanError := scanJournalRow(rows)
		if scanError != nil {
			return nil, fmt.Errorf("scan journal event row: %w", scanError)
		}
		loadedRows = append(loadedRows, rowItem)
	}
	if iterationError := rows.Err(); iterationError != nil {
		return nil, fmt.Errorf("iterate journal events: %w", iterationError)
	}
	return loadedRows, nil
}

// QueryLatestEvent returns the most recent persisted journal event matching eventSet,
// or nil when no matching event exists for enabled commanders.
func QueryLatestEvent(queryer db.Queryer, eventSet journal.EventSet) (*Row, error) {
	return QueryLatestEventScoped(queryer, eventSet, ProfileScope{})
}

// QueryLatestEventScoped returns the most recent persisted journal event
// matching eventSet and scope, or nil when none exists.
func QueryLatestEventScoped(queryer db.Queryer, eventSet journal.EventSet, scope ProfileScope) (*Row, error) {
	if eventSet.IsEmpty() {
		return nil, nil
	}
	query := selectJournalRowColumnsSQL + `
		FROM journal_events WHERE event IN (` + journal.FormatSQLInClause(eventSet) + `)` +
		commanderFilterPredicate(queryer, scope) + ` ORDER BY timestamp DESC, id DESC LIMIT 1`

	rowItem, scanError := scanJournalRow(queryer.QueryRow(query))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("query latest journal event: %w", scanError)
	}
	return &rowItem, nil
}
