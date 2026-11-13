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

	"planisphere/internal/db"
	"planisphere/internal/ed"
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
	NewEvents            int // count of events actually inserted (not previously known)
	HasRealizationEvents bool
	Errors               []string
}

func (result *Result) merge(other Result) {
	result.FilesScanned += other.FilesScanned
	result.FilesUpdated += other.FilesUpdated
	result.LinesRead += other.LinesRead
	result.NewEvents += other.NewEvents
	result.HasRealizationEvents = result.HasRealizationEvents || other.HasRealizationEvents
	result.Errors = append(result.Errors, other.Errors...)
}

// IsRealizationEvent reports whether eventName affects exploration payout realization or loss accounting.
func IsRealizationEvent(eventName string) bool {
	switch eventName {
	case "Scan", "SAAScanComplete", "ScanOrganic",
		"SellExplorationData", "MultiSellExplorationData", "SellOrganicData",
		"Resurrect", "NpcCrewPaidWage":
		return true
	default:
		return false
	}
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

// ScanDir ingests every journal file in dir (idempotent startup catch-up).
func (ingester *Ingester) ScanDir(ctx context.Context, dir string) (Result, error) {
	var scanResult Result
	if !DirExists(dir) {
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
	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return scanResult, ctx.Err()
		default:
		}
		if fileInfo, statError := os.Stat(filePath); statError == nil {
			if trackedOffset, isTracked := trackedOffsets[filepath.Base(filePath)]; isTracked && trackedOffset == fileInfo.Size() {
				scanResult.FilesScanned++
				continue
			}
		}
		fileResult, ingestError := ingester.IngestFile(filePath)
		if ingestError != nil {
			scanResult.Errors = append(scanResult.Errors, fmt.Sprintf("%s: %v", filepath.Base(filePath), ingestError))
		}
		scanResult.merge(fileResult)
	}
	return scanResult, nil
}

type fileState struct {
	offset int64
	lines  int64
	stored int64
	first  sql.NullString
	last   sql.NullString
}

func (ingester *Ingester) loadTrackedFileState(fileName string, fileSize int64) (fileState, bool, error) {
	var state fileState
	queryError := ingester.conn.QueryRow(`SELECT byte_offset, line_count, events_stored, first_timestamp, last_timestamp
		FROM ingested_journals WHERE file_name = ?`, fileName).Scan(&state.offset, &state.lines, &state.stored, &state.first, &state.last)
	if queryError != nil && !errors.Is(queryError, sql.ErrNoRows) {
		return state, false, queryError
	}
	if state.offset > fileSize {
		// File was truncated or replaced: re-read it; dedup makes this safe.
		state = fileState{}
	}
	if state.offset == fileSize && queryError == nil {
		return state, true, nil
	}
	return state, false, nil
}

func loadLatestFileCommander(transaction *sql.Tx, fileName string) (string, error) {
	var previousCommander sql.NullString
	queryError := transaction.QueryRow(
		`SELECT commander FROM journal_events WHERE source_file = ? AND commander IS NOT NULL AND commander != '' ORDER BY timestamp DESC LIMIT 1`,
		fileName,
	).Scan(&previousCommander)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", nil
	}
	if queryError != nil {
		return "", fmt.Errorf("load latest commander for %s: %w", fileName, queryError)
	}
	return previousCommander.String, nil
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

// ExecJournalEvent executes a prepared journal_events insert statement using a
// parsed event's columns and optional star position coordinates.
func ExecJournalEvent(statement *sql.Stmt, parsedEvent *Event, sourceFile string, ingestedAt string) (sql.Result, error) {
	var xCoord, yCoord, zCoord any
	if parsedEvent.StarPos != nil {
		xCoord, yCoord, zCoord = parsedEvent.StarPos.X, parsedEvent.StarPos.Y, parsedEvent.StarPos.Z
	}
	return statement.Exec(
		parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event,
		NullStringOrNil(parsedEvent.SystemName), parsedEvent.SystemAddress, parsedEvent.BodyID,
		NullStringOrNil(parsedEvent.BodyName), xCoord, yCoord, zCoord,
		NullStringOrNil(parsedEvent.Commander), sourceFile, parsedEvent.Raw, ingestedAt,
	)
}

func ingestAppendedLines(
	transaction *sql.Tx,
	reader *bufio.Reader,
	fileName string,
	state *fileState,
	newOffset *int64,
	result *Result,
	ingestedAt string,
) error {
	currentCommander, cmdError := loadLatestFileCommander(transaction, fileName)
	if cmdError != nil {
		return cmdError
	}
	hasUnattributedEvents := state.offset == 0 || currentCommander == ""

	// Atomic insert-or-ignore avoids a SELECT check per line during large journal ingestion.
	insertEventStmt, prepError := transaction.Prepare(InsertJournalEventPrefixSQL + ` ON CONFLICT(event_uid) DO NOTHING`)
	if prepError != nil {
		return prepError
	}
	defer insertEventStmt.Close()

	upsertCommanderStmt, prepCommanderError := transaction.Prepare(UpsertCommanderSQL)
	if prepCommanderError != nil {
		return prepCommanderError
	}
	defer upsertCommanderStmt.Close()

	for {
		rawLine, readError := reader.ReadBytes('\n')
		if readError != nil && !errors.Is(readError, io.EOF) {
			return readError
		}
		// Only consume complete lines; a partially written trailing line is
		// picked up on the next pass.
		if len(rawLine) == 0 || rawLine[len(rawLine)-1] != '\n' {
			break
		}
		*newOffset += int64(len(rawLine))
		trimmedLine := rawLine[:len(rawLine)-1]
		if len(bytes.TrimSpace(trimmedLine)) == 0 {
			continue
		}
		state.lines++
		result.LinesRead++
		parsedEvent, keepEvent, parseError := ParseLine(trimmedLine, fileName)
		if parseError != nil {
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, fmt.Sprintf("%s line %d: %v", fileName, state.lines, parseError))
			}
			continue
		}
		if !keepEvent {
			continue
		}
		if parsedEvent.Commander != "" {
			currentCommander = parsedEvent.Commander
			if _, execError := upsertCommanderStmt.Exec(parsedEvent.Commander, parsedEvent.FID, parsedEvent.Timestamp, parsedEvent.Timestamp); execError != nil {
				return fmt.Errorf("upsert commander %s: %w", parsedEvent.Commander, execError)
			}
			if hasUnattributedEvents {
				// Backfill any earlier events in the same file that arrived before LoadGame or Commander.
				if _, execError := transaction.Exec(BackfillFileCommanderSQL, currentCommander, fileName); execError != nil {
					return fmt.Errorf("backfill commander for %s: %w", fileName, execError)
				}
				hasUnattributedEvents = false
			}
		} else if currentCommander != "" {
			parsedEvent.Commander = currentCommander
		} else {
			hasUnattributedEvents = true
		}
		execResult, execError := ExecJournalEvent(insertEventStmt, &parsedEvent, fileName, ingestedAt)
		if execError != nil {
			return execError
		}
		rowsAffected, rowsError := execResult.RowsAffected()
		if rowsError != nil {
			return fmt.Errorf("read rows affected for %s: %w", fileName, rowsError)
		}
		if rowsAffected > 0 {
			state.stored++
			result.NewEvents++
			if IsRealizationEvent(parsedEvent.Event) {
				result.HasRealizationEvents = true
			}
			if !state.first.Valid || parsedEvent.Timestamp < state.first.String {
				state.first = sql.NullString{String: parsedEvent.Timestamp, Valid: true}
			}
			if !state.last.Valid || parsedEvent.Timestamp > state.last.String {
				state.last = sql.NullString{String: parsedEvent.Timestamp, Valid: true}
			}
		}
	}
	if currentCommander != "" && state.last.Valid {
		if _, execError := upsertCommanderStmt.Exec(currentCommander, "", state.last.String, state.last.String); execError != nil {
			return fmt.Errorf("update trailing commander %s: %w", currentCommander, execError)
		}
	}
	return nil
}

func saveTrackedFileState(
	transaction *sql.Tx,
	fileName string,
	filePath string,
	newOffset int64,
	fileSize int64,
	state fileState,
	ingestedAt string,
) error {
	_, execError := transaction.Exec(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(file_name) DO UPDATE SET
			file_path = excluded.file_path, byte_offset = excluded.byte_offset, line_count = excluded.line_count,
			file_size = excluded.file_size, events_stored = excluded.events_stored,
			first_timestamp = excluded.first_timestamp, last_timestamp = excluded.last_timestamp,
			updated_at = excluded.updated_at`,
		fileName, filePath, newOffset, state.lines, fileSize, state.stored, state.first, state.last, ingestedAt)
	if execError != nil {
		return execError
	}
	return db.AdvanceIngestWatermark(transaction, ingestedAt)
}

// IngestFile ingests any new complete lines appended to path since the last pass.
func (ingester *Ingester) IngestFile(path string) (Result, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()

	result := Result{FilesScanned: 1}
	fileInfo, statError := os.Stat(path)
	if statError != nil {
		return result, statError
	}
	fileName := filepath.Base(path)
	fileSize := fileInfo.Size()

	state, alreadyUpToDate, loadStateError := ingester.loadTrackedFileState(fileName, fileSize)
	if loadStateError != nil {
		return result, loadStateError
	}
	if alreadyUpToDate {
		return result, nil
	}

	fileHandle, openError := os.Open(path)
	if openError != nil {
		return result, openError
	}
	defer fileHandle.Close()
	if _, seekError := fileHandle.Seek(state.offset, io.SeekStart); seekError != nil {
		return result, seekError
	}
	newOffset := state.offset
	reader := bufio.NewReader(io.LimitReader(fileHandle, fileSize-state.offset))

	txError := db.WithTx(ingester.conn, func(transaction *sql.Tx) error {
		ingestedAt := ed.NowISO()
		if lineError := ingestAppendedLines(transaction, reader, fileName, &state, &newOffset, &result, ingestedAt); lineError != nil {
			return lineError
		}
		return saveTrackedFileState(transaction, fileName, path, newOffset, fileSize, state, ingestedAt)
	})
	if txError != nil {
		return result, txError
	}
	InvalidateCommanderCountsCache()
	result.FilesUpdated = 1
	return result, nil
}
