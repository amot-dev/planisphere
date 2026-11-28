package replay

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

const selectJournalRowColumnsSQL = `SELECT id, event_uid, timestamp, event, COALESCE(system_name, ''), system_address, body_id, star_x, star_y, star_z, COALESCE(profile_id, 0), COALESCE(commander, ''), raw_json`

// Byte slab sizing bounds for queryJournalRows.
const (
	minTailSlabBytes    = 4 << 10
	minWindowSlabBytes  = 64 << 10
	maxRawJSONSlabBytes = 1 << 20
)

// Row is a persisted journal_events record loaded for replay.
type Row struct {
	ID            int64
	UID           string
	Timestamp     string
	Type          journal.EventType
	HasStarPos    bool
	Event         string
	SystemName    string
	SystemAddress sql.NullInt64
	BodyID        sql.NullInt64
	StarPos       reference.Vec3
	ProfileID     int64
	Commander     string
	RawJSON       json.RawMessage
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rawJSONSlabAllocator struct {
	activeSlab    []byte
	nextSlabBytes int
}

func newRawJSONSlabAllocator(initialRowCapacity int) rawJSONSlabAllocator {
	initialSlabBytes := minWindowSlabBytes
	if initialRowCapacity <= 16 {
		initialSlabBytes = minTailSlabBytes
	}
	return rawJSONSlabAllocator{nextSlabBytes: initialSlabBytes}
}

// copyBytes copies transient SQLite row bytes into contiguous pointer-free byte
// slabs with three-index slice bounds so rows never alias sql.RawBytes or
// clobber adjacent payloads on append.
func (allocator *rawJSONSlabAllocator) copyBytes(source []byte) json.RawMessage {
	sourceLength := len(source)
	if sourceLength == 0 {
		return nil
	}
	if sourceLength > maxRawJSONSlabBytes {
		oversizedBuffer := make([]byte, sourceLength)
		copy(oversizedBuffer, source)
		return oversizedBuffer
	}
	if cap(allocator.activeSlab)-len(allocator.activeSlab) < sourceLength {
		slabSize := max(allocator.nextSlabBytes, sourceLength)
		allocator.activeSlab = make([]byte, 0, slabSize)
		if allocator.nextSlabBytes < maxRawJSONSlabBytes {
			allocator.nextSlabBytes = min(allocator.nextSlabBytes*2, maxRawJSONSlabBytes)
		}
	}
	startOffset := len(allocator.activeSlab)
	allocator.activeSlab = append(allocator.activeSlab, source...)
	endOffset := len(allocator.activeSlab)
	return allocator.activeSlab[startOffset:endOffset:endOffset]
}

func growRowSlice(existingRows []Row, initialCapacity int) []Row {
	if existingRows == nil {
		return make([]Row, 0, max(initialCapacity, 16))
	}
	existingCapacity := cap(existingRows)
	nextCapacity := existingCapacity * 2
	if existingCapacity < 16384 {
		nextCapacity = existingCapacity * 4
	}
	grownRows := make([]Row, len(existingRows), nextCapacity)
	copy(grownRows, existingRows)
	return grownRows
}

type journalRowScanner struct {
	rowItem      Row
	starX        sql.NullFloat64
	starY        sql.NullFloat64
	starZ        sql.NullFloat64
	destinations [13]any
}

func newJournalRowScanner(rawJSONDestination any) *journalRowScanner {
	state := &journalRowScanner{}
	state.destinations = [13]any{
		&state.rowItem.ID,
		&state.rowItem.UID,
		&state.rowItem.Timestamp,
		&state.rowItem.Event,
		&state.rowItem.SystemName,
		&state.rowItem.SystemAddress,
		&state.rowItem.BodyID,
		&state.starX,
		&state.starY,
		&state.starZ,
		&state.rowItem.ProfileID,
		&state.rowItem.Commander,
		rawJSONDestination,
	}
	return state
}

func (state *journalRowScanner) scan(scanner rowScanner) (Row, error) {
	state.rowItem = Row{}
	state.starX = sql.NullFloat64{}
	state.starY = sql.NullFloat64{}
	state.starZ = sql.NullFloat64{}
	if scanError := scanner.Scan(state.destinations[:]...); scanError != nil {
		return Row{}, scanError
	}
	if parsedType, knownType := journal.ParseEventType(state.rowItem.Event); knownType {
		state.rowItem.Type = parsedType
	}
	if state.starX.Valid && state.starY.Valid && state.starZ.Valid {
		state.rowItem.StarPos = reference.Vec3{X: state.starX.Float64, Y: state.starY.Float64, Z: state.starZ.Float64}
		state.rowItem.HasStarPos = true
	}
	return state.rowItem, nil
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

	initialCapacity := 256
	// Unary +event prevents SQLite from union-scanning idx_journal_events_event_ts
	// across broad unbounded event sets and sorting in a temporary B-tree.
	eventColumn := "event"
	if afterTimestamp == "" {
		initialCapacity = 1024
		if bits.OnesCount64(uint64(eventSet)) >= 6 {
			eventColumn = "+event"
		}
	}

	query := selectJournalRowColumnsSQL + `
		FROM journal_events WHERE ` + eventColumn + ` IN (` + journal.FormatSQLInClause(eventSet) + `) AND timestamp > ? AND timestamp <= ?` +
		filterClause + ` ORDER BY timestamp, id`
	return queryJournalRows(queryer, initialCapacity, query, afterTimestamp, untilTimestamp)
}

func queryJournalRows(queryer db.Queryer, initialCapacity int, query string, args ...any) ([]Row, error) {
	rows, queryError := queryer.Query(query, args...)
	if queryError != nil {
		return nil, fmt.Errorf("query journal events: %w", queryError)
	}
	defer rows.Close()

	var loadedRows []Row
	var rawScratch *sql.RawBytes
	var rowScanState *journalRowScanner
	slabAllocator := newRawJSONSlabAllocator(initialCapacity)
	for rows.Next() {
		if rowScanState == nil {
			rawScratch = new(sql.RawBytes)
			rowScanState = newJournalRowScanner(rawScratch)
		}
		rowItem, scanError := rowScanState.scan(rows)
		if scanError != nil {
			return nil, fmt.Errorf("scan journal event row: %w", scanError)
		}
		rowItem.RawJSON = slabAllocator.copyBytes(*rawScratch)
		if len(loadedRows) == cap(loadedRows) {
			loadedRows = growRowSlice(loadedRows, initialCapacity)
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

	var rawBytes []byte
	rowScanState := newJournalRowScanner(&rawBytes)
	rowItem, scanError := rowScanState.scan(queryer.QueryRow(query))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("query latest journal event: %w", scanError)
	}
	rowItem.RawJSON = rawBytes
	return &rowItem, nil
}
