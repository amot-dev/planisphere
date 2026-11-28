package journal

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// JournalGlob matches Elite Dangerous journal files.
const JournalGlob = "Journal.*.log"

// IsJournalFile reports whether name looks like a player journal.
func IsJournalFile(name string) bool {
	baseName := filepath.Base(name)
	matched, _ := filepath.Match(JournalGlob, baseName)
	return matched
}

// Ingester copies journal events into the database exactly once. It tracks a
// byte offset per journal file in ingested_journals so it can resume tailing,
// and relies on journal_events.event_uid uniqueness for deduplication.
type Ingester struct {
	conn *sql.DB
	mu   sync.Mutex // serialises ingestion
}

// NewIngester creates an ingester bound to the database.
func NewIngester(conn *sql.DB) *Ingester { return &Ingester{conn: conn} }

// WithExclusiveConn executes operation while holding the ingester write lock,
// allowing external archive importers to serialize safely against live tailing.
func (ingester *Ingester) WithExclusiveConn(operation func(conn *sql.DB) (Result, error)) (Result, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()
	return operation(ingester.conn)
}

// Result summarises an ingestion pass.
type Result struct {
	FilesScanned         int
	FilesUpdated         int
	LinesRead            int
	NewEvents            int      // count of events actually inserted (not previously known)
	CommandersBackfilled int      // count of existing journal_events rows whose identity was backfilled
	ProfilesChanged      bool     // whether commander profile topology or expedition bindings changed
	Events               EventSet // set of newly inserted event types
	EarliestTimestamp    string   // earliest timestamp among newly inserted or backfilled events
	LatestTimestamp      string   // latest timestamp among newly inserted or backfilled events
	Errors               []string
}

// HasChanges reports whether the ingestion pass inserted new events,
// backfilled commander metadata on existing rows, or changed commander profiles.
func (result Result) HasChanges() bool {
	return result.NewEvents > 0 || result.CommandersBackfilled > 0 || result.ProfilesChanged
}

// ObserveTimestamp records timestamp when it expands the earliest or latest bounds seen in result.
func (result *Result) ObserveTimestamp(timestamp string) {
	if timestamp == "" {
		return
	}
	if result.EarliestTimestamp == "" || timestamp < result.EarliestTimestamp {
		result.EarliestTimestamp = timestamp
	}
	if result.LatestTimestamp == "" || timestamp > result.LatestTimestamp {
		result.LatestTimestamp = timestamp
	}
}

func (result *Result) merge(other Result) {
	result.FilesScanned += other.FilesScanned
	result.FilesUpdated += other.FilesUpdated
	result.LinesRead += other.LinesRead
	result.NewEvents += other.NewEvents
	result.CommandersBackfilled += other.CommandersBackfilled
	result.ProfilesChanged = result.ProfilesChanged || other.ProfilesChanged
	result.Events = result.Events.Union(other.Events)
	result.ObserveTimestamp(other.EarliestTimestamp)
	result.ObserveTimestamp(other.LatestTimestamp)
	result.Errors = append(result.Errors, other.Errors...)
}

// ListJournalFiles returns all journal files in dir sorted by name (which is
// chronological within each of the two historical naming schemes).
func ListJournalFiles(dir string) ([]string, error) {
	matches, globError := filepath.Glob(filepath.Join(dir, JournalGlob))
	if globError != nil {
		return nil, globError
	}
	sort.Strings(matches)
	return matches, nil
}

func (ingester *Ingester) loadAllTrackedOffsets() (map[string]int64, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()
	rows, queryError := ingester.conn.Query(`SELECT file_name, byte_offset FROM ingested_journals`)
	if queryError != nil {
		return nil, fmt.Errorf("query tracked journal offsets: %w", queryError)
	}
	defer rows.Close()
	trackedOffsets := make(map[string]int64, 256)
	for rows.Next() {
		var fileName string
		var byteOffset int64
		if scanError := rows.Scan(&fileName, &byteOffset); scanError != nil {
			return nil, fmt.Errorf("scan tracked journal offset: %w", scanError)
		}
		trackedOffsets[fileName] = byteOffset
	}
	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterate tracked journal offsets: %w", rowsError)
	}
	return trackedOffsets, nil
}

func (ingester *Ingester) resolveCommandersLocked() (bool, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()
	var profilesChanged bool
	err := db.WithTx(ingester.conn, func(transaction *sql.Tx) error {
		var resolveErr error
		profilesChanged, resolveErr = db.ResolveCommandersChanged(transaction)
		return resolveErr
	})
	return profilesChanged, err
}

// ScanDir ingests every journal file in dir (idempotent startup catch-up).
func (ingester *Ingester) ScanDir(ctx context.Context, dir string) (Result, error) {
	var scanResult Result
	if !dirExists(dir) {
		return scanResult, fmt.Errorf("journal directory not found: %s", dir)
	}
	files, listError := ListJournalFiles(dir)
	if listError != nil {
		return scanResult, listError
	}
	trackedOffsets, offsetError := ingester.loadAllTrackedOffsets()
	if offsetError != nil {
		return scanResult, offsetError
	}
	anyShouldResolve := false
	for _, filePath := range files {
		select {
		case <-ctx.Done():
			if anyShouldResolve {
				if profilesChanged, resolveErr := ingester.resolveCommandersLocked(); resolveErr != nil {
					scanResult.Errors = append(scanResult.Errors, resolveErr.Error())
				} else {
					scanResult.ProfilesChanged = scanResult.ProfilesChanged || profilesChanged
				}
			}
			return scanResult, ctx.Err()
		default:
		}
		if fileInfo, statError := os.Stat(filePath); statError == nil {
			if trackedOffset, isTracked := trackedOffsets[filepath.Base(filePath)]; isTracked && trackedOffset == fileInfo.Size() {
				scanResult.FilesScanned++
				continue
			}
		}
		fileResult, shouldResolve, ingestError := ingester.ingestFile(filePath, false)
		if ingestError != nil {
			scanResult.Errors = append(scanResult.Errors, fmt.Sprintf("%s: %v", filepath.Base(filePath), ingestError))
		}
		anyShouldResolve = anyShouldResolve || shouldResolve
		scanResult.merge(fileResult)
	}
	if anyShouldResolve {
		profilesChanged, resolveErr := ingester.resolveCommandersLocked()
		if resolveErr != nil {
			return scanResult, resolveErr
		}
		scanResult.ProfilesChanged = scanResult.ProfilesChanged || profilesChanged
	}
	return scanResult, nil
}

type fileState struct {
	offset, lines, stored        int64
	first, last                  sql.NullString
	part, continuedPart          int
	gameVersion, build, universe string
	lastCommander, lastFID       string
}

func (ingester *Ingester) loadTrackedFileState(fileName string, fileSize int64) (fileState, bool, error) {
	defaultState := func() fileState {
		state := fileState{part: 1, universe: "live"}
		if _, parsedPart, ok := db.ParseJournalFilenamePart(fileName); ok {
			state.part = parsedPart
		}
		return state
	}
	state := defaultState()
	queryError := ingester.conn.QueryRow(`
		SELECT byte_offset, line_count, events_stored, first_timestamp, last_timestamp,
		       part, game_version, build, universe, continued_part, last_commander, last_fid
		FROM ingested_journals WHERE file_name = ?`, fileName,
	).Scan(&state.offset, &state.lines, &state.stored, &state.first, &state.last,
		&state.part, &state.gameVersion, &state.build, &state.universe, &state.continuedPart, &state.lastCommander, &state.lastFID)
	if queryError != nil && !errors.Is(queryError, sql.ErrNoRows) {
		return state, false, queryError
	}
	if state.offset > fileSize {
		state = defaultState()
	}
	if state.offset == fileSize && queryError == nil {
		return state, true, nil
	}
	return state, false, nil
}

// InsertJournalEventPrefixSQL is the shared INSERT header for journal_events
// before each caller appends its own ON CONFLICT clause.
const InsertJournalEventPrefixSQL = `INSERT INTO journal_events
	(event_uid, timestamp, event, system_name, system_address, body_id, body_name,
	 star_x, star_y, star_z, commander, source_file, raw_json, ingested_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

// UpsertCommanderSQL inserts a discovered commander or expands its observed
// Frontier ID and timestamp range on conflict.
const UpsertCommanderSQL = `INSERT INTO commanders(name, fid, enabled, first_seen, last_seen)
	VALUES(?, ?, 1, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		fid = CASE WHEN excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
		first_seen = CASE WHEN commanders.first_seen IS NULL OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
		last_seen = CASE WHEN commanders.last_seen IS NULL OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`

// BackfillFileCommanderSQL attributes earlier unlabelled events in a journal
// file once the commander identity appears.
const BackfillFileCommanderSQL = `UPDATE journal_events SET commander = ? WHERE source_file = ? AND (commander IS NULL OR commander = '')`

// NullStringOrNil returns nil when text contains only whitespace, otherwise text.
func NullStringOrNil(text string) any {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return text
}

func ingestAppendedLines(
	transaction *sql.Tx,
	reader *bufio.Reader,
	fileName string,
	state *fileState,
	newOffset *int64,
	result *Result,
	appendBatch *db.LiveAppendBatch,
	ingestedAt string,
) (bool, error) {
	tracker, initError := newTailingSessionTracker(transaction, fileName, state)
	if initError != nil {
		return false, initError
	}
	statements, closeStatements, prepErr := prepareLineIngestStatements(transaction, state.offset == 0, ingestedAt)
	if prepErr != nil {
		return false, prepErr
	}
	defer closeStatements()

	shouldResolve := false
	for {
		rawLine, readError := reader.ReadBytes('\n')
		if readError != nil && !errors.Is(readError, io.EOF) {
			return false, readError
		}
		if len(rawLine) == 0 || rawLine[len(rawLine)-1] != '\n' {
			break
		}
		*newOffset += int64(len(rawLine))
		trimmedLine := rawLine[:len(rawLine)-1]
		if len(bytes.TrimSpace(trimmedLine)) == 0 {
			continue
		}
		lineMutated, lineErr := processAppendedJournalLine(&statements, &tracker, trimmedLine, fileName, state, result, appendBatch)
		if lineErr != nil {
			return false, lineErr
		}
		shouldResolve = shouldResolve || lineMutated
	}
	if err := finalizeSessionState(&statements, &tracker, state); err != nil {
		return false, err
	}
	return shouldResolve, nil
}

func processAppendedJournalLine(
	statements *lineIngestStatements,
	tracker *SessionTracker,
	trimmedLine []byte,
	fileName string,
	state *fileState,
	result *Result,
	appendBatch *db.LiveAppendBatch,
) (bool, error) {
	state.lines++
	result.LinesRead++
	if consumed, headerErr := tracker.ObserveHeaderOrContinued(trimmedLine); headerErr != nil || consumed {
		return false, headerErr
	}
	parsedEvent, keepEvent, parseError := ParseLine(trimmedLine, fileName)
	if parseError != nil {
		if len(result.Errors) < 20 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s line %d: %v", fileName, state.lines, parseError))
		}
		return false, nil
	}
	if keepEvent {
		state.advanceLastTimestamp(parsedEvent.Timestamp)
	} else if rawTimestamp := PeekTimestamp(trimmedLine); rawTimestamp != "" {
		if normalizedTimestamp, normalizeError := reference.NormalizeISO(rawTimestamp); normalizeError == nil {
			state.advanceLastTimestamp(normalizedTimestamp)
		}
	}
	if !keepEvent {
		return false, nil
	}
	onSighting := func(commander, fid, timestamp string) error {
		_, err := statements.upsertCmdrStmt.Exec(commander, fid, timestamp, timestamp)
		return err
	}
	backfilledCount, prepEventError := tracker.PrepareEvent(statements.transaction, &parsedEvent, onSighting)
	if prepEventError != nil {
		return false, prepEventError
	}
	if backfilledCount > 0 {
		result.CommandersBackfilled += backfilledCount
		result.ObserveTimestamp(parsedEvent.Timestamp)
		if state.first.Valid {
			result.ObserveTimestamp(state.first.String)
		}
		if state.last.Valid {
			result.ObserveTimestamp(state.last.String)
		}
	}
	insertedID, inserted, updatedRows, storeErr := persistIngestedEvent(statements.insertEventStmt, statements.updateByteZeroStmt, &parsedEvent, fileName, statements.ingestedAt)
	if storeErr != nil {
		return false, storeErr
	}
	if updatedRows > 0 {
		result.CommandersBackfilled += updatedRows
		result.ObserveTimestamp(parsedEvent.Timestamp)
	}
	if inserted {
		state.recordInsertedEvent(&parsedEvent, result)
		appendBatch.ObserveInserted(insertedID, parsedEvent.Event, parsedEvent.Commander, parsedEvent.FID, parsedEvent.Universe, parsedEvent.Timestamp)
	}
	return backfilledCount > 0 || inserted || updatedRows > 0, nil
}

func (state *fileState) advanceLastTimestamp(normalizedTimestamp string) {
	if normalizedTimestamp != "" && (!state.last.Valid || normalizedTimestamp > state.last.String) {
		state.last = sql.NullString{String: normalizedTimestamp, Valid: true}
	}
}

func (state *fileState) recordInsertedEvent(parsedEvent *Event, result *Result) {
	state.stored++
	result.NewEvents++
	result.Events = result.Events.Add(parsedEvent.Type)
	result.ObserveTimestamp(parsedEvent.Timestamp)
	if !state.first.Valid || parsedEvent.Timestamp < state.first.String {
		state.first = sql.NullString{String: parsedEvent.Timestamp, Valid: true}
	}
	state.advanceLastTimestamp(parsedEvent.Timestamp)
}

func finalizeSessionState(statements *lineIngestStatements, tracker *SessionTracker, state *fileState) error {
	if err := tracker.ResolveTrailingRollover(); err != nil {
		return err
	}
	state.part, state.gameVersion, state.build = tracker.Part, tracker.GameVersion, tracker.Build
	state.universe, state.continuedPart = tracker.Universe, tracker.ContinuedPart
	state.lastCommander, state.lastFID = tracker.CurrentCommander, tracker.CurrentFID
	if tracker.CurrentCommander != "" && state.last.Valid {
		firstSeen := state.last.String
		if tracker.InheritedRollover && state.first.Valid {
			firstSeen = state.first.String
		}
		if _, err := statements.upsertCmdrStmt.Exec(tracker.CurrentCommander, tracker.CurrentFID, firstSeen, state.last.String); err != nil {
			return fmt.Errorf("update trailing commander %s: %w", tracker.CurrentCommander, err)
		}
	}
	return nil
}

func saveTrackedFileState(transaction *sql.Tx, fileName, filePath string, newOffset, fileSize int64, state fileState, ingestedAt string) error {
	_, execError := transaction.Exec(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp,
		 part, game_version, build, universe, continued_part, last_commander, last_fid, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(file_name) DO UPDATE SET
			file_path = excluded.file_path, byte_offset = excluded.byte_offset, line_count = excluded.line_count,
			file_size = excluded.file_size, events_stored = excluded.events_stored,
			first_timestamp = excluded.first_timestamp, last_timestamp = excluded.last_timestamp,
			part = excluded.part, game_version = excluded.game_version, build = excluded.build,
			universe = excluded.universe, continued_part = excluded.continued_part,
			last_commander = excluded.last_commander, last_fid = excluded.last_fid,
			updated_at = excluded.updated_at`,
		fileName, filePath, newOffset, state.lines, fileSize, state.stored, state.first, state.last,
		state.part, state.gameVersion, state.build, state.universe, state.continuedPart, state.lastCommander, state.lastFID, ingestedAt)
	if execError != nil {
		return execError
	}
	return db.AdvanceIngestWatermark(transaction, ingestedAt)
}

// IngestFile ingests any new complete lines appended to path since the last pass.
func (ingester *Ingester) IngestFile(path string) (Result, error) {
	result, _, err := ingester.ingestFile(path, true)
	return result, err
}

func (ingester *Ingester) ingestFile(path string, resolveProfiles bool) (Result, bool, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()
	result := Result{FilesScanned: 1}
	fileInfo, statError := os.Stat(path)
	if statError != nil {
		return result, false, statError
	}
	fileName, fileSize := filepath.Base(path), fileInfo.Size()
	state, alreadyUpToDate, loadStateError := ingester.loadTrackedFileState(fileName, fileSize)
	if loadStateError != nil || alreadyUpToDate {
		return result, false, loadStateError
	}
	fileHandle, openError := os.Open(path)
	if openError != nil {
		return result, false, openError
	}
	defer fileHandle.Close()
	if _, seekError := fileHandle.Seek(state.offset, io.SeekStart); seekError != nil {
		return result, false, seekError
	}
	newOffset := state.offset
	reader := bufio.NewReader(io.LimitReader(fileHandle, fileSize-state.offset))
	shouldResolve := false
	txError := db.WithTx(ingester.conn, func(transaction *sql.Tx) error {
		ingestedAt := reference.NowISO()
		var appendBatch db.LiveAppendBatch
		var lineError error
		shouldResolve, lineError = ingestAppendedLines(transaction, reader, fileName, &state, &newOffset, &result, &appendBatch, ingestedAt)
		if lineError != nil {
			return lineError
		}
		if saveError := saveTrackedFileState(transaction, fileName, path, newOffset, fileSize, state, ingestedAt); saveError != nil {
			return saveError
		}
		if resolveProfiles && shouldResolve {
			var resolveErr error
			result.ProfilesChanged, resolveErr = db.ResolveLiveAppendOrFull(transaction, appendBatch, result.CommandersBackfilled)
			return resolveErr
		}
		return nil
	})
	if txError != nil {
		return result, false, txError
	}
	result.FilesUpdated = 1
	return result, shouldResolve, nil
}
