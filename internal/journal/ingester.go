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
	base := filepath.Base(name)
	ok, _ := filepath.Match(JournalGlob, base)
	return ok
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
	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return scanResult, ctx.Err()
		default:
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

// IngestFile ingests any new complete lines appended to path since the last pass.
func (ingester *Ingester) IngestFile(path string) (Result, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()

	result := Result{FilesScanned: 1}
	fileInfo, statError := os.Stat(path)
	if statError != nil {
		return result, statError
	}
	name := filepath.Base(path)
	size := fileInfo.Size()

	var state fileState
	queryError := ingester.conn.QueryRow(`SELECT byte_offset, line_count, events_stored, first_timestamp, last_timestamp
		FROM ingested_journals WHERE file_name = ?`, name).Scan(&state.offset, &state.lines, &state.stored, &state.first, &state.last)
	if queryError != nil && !errors.Is(queryError, sql.ErrNoRows) {
		return result, queryError
	}
	if state.offset > size {
		// File was truncated or replaced: re-read it; dedup makes this safe.
		state = fileState{}
	}
	if state.offset == size && queryError == nil {
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
	reader := bufio.NewReader(io.LimitReader(fileHandle, size-state.offset))

	txError := db.WithTx(ingester.conn, func(transaction *sql.Tx) error {
		var currentCmdr string
		var prevCmdr sql.NullString
		_ = transaction.QueryRow(`SELECT commander FROM journal_events WHERE source_file = ? AND commander IS NOT NULL AND commander != '' ORDER BY timestamp DESC LIMIT 1`, name).Scan(&prevCmdr)
		currentCmdr = prevCmdr.String

		// Atomic insert-or-ignore avoids a SELECT check per line during large journal ingestion.
		stmt, prepError := transaction.Prepare(`INSERT INTO journal_events
			(event_uid, timestamp, event, system_name, system_address, body_id, body_name,
			 star_x, star_y, star_z, commander, source_file, raw_json, ingested_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(event_uid) DO NOTHING`)
		if prepError != nil {
			return prepError
		}
		defer stmt.Close()
		cmdrStmt, prepCmdrError := transaction.Prepare(`INSERT INTO commanders(name, fid, enabled, first_seen, last_seen)
			VALUES(?, ?, 1, ?, ?)
			ON CONFLICT(name) DO UPDATE SET
				fid = CASE WHEN excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
				first_seen = CASE WHEN commanders.first_seen IS NULL OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
				last_seen = CASE WHEN commanders.last_seen IS NULL OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`)
		if prepCmdrError != nil {
			return prepCmdrError
		}
		defer cmdrStmt.Close()

		now := ed.NowISO()
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
			newOffset += int64(len(rawLine))
			line := rawLine[:len(rawLine)-1]
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			state.lines++
			result.LinesRead++
			parsedEvent, ok, parseError := ParseLine(line, name)
			if parseError != nil {
				if len(result.Errors) < 20 {
					result.Errors = append(result.Errors, fmt.Sprintf("%s line %d: %v", name, state.lines, parseError))
				}
				continue
			}
			if !ok {
				continue
			}
			if parsedEvent.Commander != "" {
				currentCmdr = parsedEvent.Commander
				_, _ = cmdrStmt.Exec(parsedEvent.Commander, parsedEvent.FID, parsedEvent.Timestamp, parsedEvent.Timestamp)
				// Backfill any earlier events in the same file that arrived before LoadGame/Commander.
				_, _ = transaction.Exec(`UPDATE journal_events SET commander = ? WHERE source_file = ? AND (commander IS NULL OR commander = '')`, currentCmdr, name)
			} else if currentCmdr != "" {
				parsedEvent.Commander = currentCmdr
			}
			var xCoord, yCoord, zCoord any
			if parsedEvent.StarPos != nil {
				xCoord, yCoord, zCoord = parsedEvent.StarPos.X, parsedEvent.StarPos.Y, parsedEvent.StarPos.Z
			}
			execResult, execError := stmt.Exec(parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event, nullStr(parsedEvent.SystemName), parsedEvent.SystemAddress, parsedEvent.BodyID,
				nullStr(parsedEvent.BodyName), xCoord, yCoord, zCoord, nullStr(parsedEvent.Commander), name, parsedEvent.Raw, now)
			if execError != nil {
				return execError
			}
			rowsAffected, _ := execResult.RowsAffected()
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
		if currentCmdr != "" && state.last.Valid {
			_, _ = cmdrStmt.Exec(currentCmdr, "", state.last.String, state.last.String)
		}
		_, execError := transaction.Exec(`INSERT INTO ingested_journals
			(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(file_name) DO UPDATE SET
				file_path = excluded.file_path, byte_offset = excluded.byte_offset, line_count = excluded.line_count,
				file_size = excluded.file_size, events_stored = excluded.events_stored,
				first_timestamp = excluded.first_timestamp, last_timestamp = excluded.last_timestamp,
				updated_at = excluded.updated_at`,
			name, path, newOffset, state.lines, size, state.stored, state.first, state.last, now)
		if execError != nil {
			return execError
		}
		return db.AdvanceIngestWatermark(transaction, now)
	})
	if txError != nil {
		return result, txError
	}
	result.FilesUpdated = 1
	return result, nil
}

func nullStr(text string) any {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return text
}

// Status describes the persisted journal archive.
type Status struct {
	FilesTracked int    `json:"filesTracked"`
	EventsStored int    `json:"eventsStored"`
	FirstEventAt string `json:"firstEventAt"`
	LastEventAt  string `json:"lastEventAt"`
}

// CmdrFilterSQL is a SQL predicate on journal_events that excludes events
// belonging to any commander the user has explicitly disabled in Settings.
const CmdrFilterSQL = `(commander IS NULL OR commander = '' OR NOT EXISTS (SELECT 1 FROM commanders c WHERE c.name = journal_events.commander COLLATE NOCASE AND c.enabled = 0))`

// ArchiveStatus returns counts of tracked files and stored events.
// ArchiveStatus returns counts of tracked files and stored events.
func ArchiveStatus(queryer db.Queryer) (Status, error) {
	var status Status
	var first, last sql.NullString
	if queryError := queryer.QueryRow(`SELECT COUNT(*) FROM ingested_journals`).Scan(&status.FilesTracked); queryError != nil {
		return status, queryError
	}
	if queryError := queryer.QueryRow(`SELECT COUNT(*), MIN(timestamp), MAX(timestamp) FROM journal_events WHERE `+CmdrFilterSQL).Scan(&status.EventsStored, &first, &last); queryError != nil {
		return status, queryError
	}
	status.FirstEventAt, status.LastEventAt = first.String, last.String
	return status, nil
}

// Commander represents a player commander identity discovered in journals.
type Commander struct {
	Name          string `json:"name"`
	FID           string `json:"fid"`
	Enabled       bool   `json:"enabled"`
	FirstSeen     string `json:"firstSeen"`
	LastSeen      string `json:"lastSeen"`
	FirstGalactic string `json:"firstGalactic"`
	LastGalactic  string `json:"lastGalactic"`
	EventCount    int    `json:"eventCount"`
}

// ListCommanders returns all discovered commanders ordered by first_seen.
func ListCommanders(queryer db.Queryer) ([]Commander, error) {
	rows, queryError := queryer.Query(`SELECT c.name, COALESCE(c.fid, ''), c.enabled, COALESCE(c.first_seen, ''), COALESCE(c.last_seen, ''),
		(SELECT COUNT(*) FROM journal_events je WHERE je.commander = c.name COLLATE NOCASE)
		FROM commanders c
		ORDER BY c.first_seen ASC, c.name ASC`)
	if queryError != nil {
		return nil, queryError
	}
	defer rows.Close()
	out := []Commander{}
	for rows.Next() {
		var commander Commander
		var enabledFlag int
		if scanError := rows.Scan(&commander.Name, &commander.FID, &enabledFlag, &commander.FirstSeen, &commander.LastSeen, &commander.EventCount); scanError != nil {
			return nil, scanError
		}
		commander.Enabled = enabledFlag != 0
		commander.FirstGalactic = ed.ToGalactic(commander.FirstSeen)
		commander.LastGalactic = ed.ToGalactic(commander.LastSeen)
		out = append(out, commander)
	}
	return out, rows.Err()
}

// SetCommanderEnabled toggles whether a commander's events are included in stats and plans.
func SetCommanderEnabled(queryer db.Queryer, name string, enabled bool) error {
	enabledFlag := 0
	if enabled {
		enabledFlag = 1
	}
	_, execError := queryer.Exec(`UPDATE commanders SET enabled = ? WHERE name = ? COLLATE NOCASE`, enabledFlag, strings.TrimSpace(name))
	return execError
}

// ImportEDDiscoveryDB performs a one-time recovery of raw journal events stored
// inside an EDDiscovery SQLite database (JournalEntries.EventData) for older
// .log files that may have been deleted from disk. Once imported, all events
// live permanently inside our own journal_events table.
func (ingester *Ingester) ImportEDDiscoveryDB(ctx context.Context, eddPath string, onProgress func(current, total, files int, stage string)) (Result, error) {
	ingester.mu.Lock()
	defer ingester.mu.Unlock()

	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	reportProgress := func(current, total, files int, stage string) {
		if onProgress != nil {
			onProgress(current, total, files, stage)
		}
	}

	var result Result
	if fileInfo, statError := os.Stat(eddPath); statError != nil || fileInfo.IsDir() {
		return result, fmt.Errorf("EDDiscovery database not found: %s", eddPath)
	}

	reportProgress(0, 0, 0, "Opening EDDiscovery database and reading journal entries…")
	eddConn, openError := sql.Open("sqlite", "file:"+eddPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if openError != nil {
		return result, fmt.Errorf("open EDDiscovery DB: %w", openError)
	}
	defer eddConn.Close()

	const eventFilter = `journalEntry.EventType IN (
			'Commander','LoadGame','Statistics','Location','FSDJump','CarrierJump',
			'CarrierStats','CarrierDepositFuel','CarrierJumpRequest','Docked','FSDTarget','Scan','SAAScanComplete','FSSDiscoveryScan',
			'ScanOrganic','MultiSellExplorationData','SellExplorationData','SellOrganicData','ShipyardSwap',
			'Died','Resurrect','NpcCrewPaidWage'
		)`

	var total int
	if countError := eddConn.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM JournalEntries journalEntry
		WHERE ` + eventFilter).Scan(&total); countError != nil {
		return result, fmt.Errorf("count EDDiscovery JournalEntries: %w", countError)
	}

	rows, queryError := eddConn.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(travelUnit.Name, ''), 'EDD-' || journalEntry.TravelLogId),
		       COALESCE(NULLIF(travelUnit.Path, ''), 'EDD-' || journalEntry.TravelLogId),
		       journalEntry.EventData
		FROM JournalEntries journalEntry
		LEFT JOIN TravelLogUnit travelUnit ON travelUnit.Id = journalEntry.TravelLogId
		WHERE ` + eventFilter + `
		ORDER BY journalEntry.TravelLogId ASC, journalEntry.Id ASC`)
	if queryError != nil {
		return result, fmt.Errorf("query EDDiscovery JournalEntries: %w", queryError)
	}
	defer rows.Close()

	reportProgress(0, total, 0, "Ingesting historical journal events into SQLite archive…")

	txError := db.WithTx(ingester.conn, func(transaction *sql.Tx) error {
		// Atomic insert-or-ignore avoids a SELECT check per line during large journal ingestion.
		stmt, prepError := transaction.Prepare(`INSERT INTO journal_events
			(event_uid, timestamp, event, system_name, system_address, body_id, body_name,
			 star_x, star_y, star_z, commander, source_file, raw_json, ingested_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(event_uid) DO UPDATE SET
				commander = COALESCE(NULLIF(excluded.commander, ''), journal_events.commander)`)
		if prepError != nil {
			return prepError
		}
		defer stmt.Close()

		cmdrStmt, prepCmdrError := transaction.Prepare(`INSERT INTO commanders(name, fid, enabled, first_seen, last_seen)
			VALUES(?, ?, 1, ?, ?)
			ON CONFLICT(name) DO UPDATE SET
				fid = CASE WHEN excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
				first_seen = CASE WHEN commanders.first_seen IS NULL OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
				last_seen = CASE WHEN commanders.last_seen IS NULL OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`)
		if prepCmdrError != nil {
			return prepCmdrError
		}
		defer cmdrStmt.Close()

		updateCmdrStmt, prepUpdateError := transaction.Prepare(`UPDATE journal_events SET commander = ? WHERE source_file = ? AND (commander IS NULL OR commander = '')`)
		if prepUpdateError != nil {
			return prepUpdateError
		}
		defer updateCmdrStmt.Close()

		type commanderTracker struct {
			fid       string
			firstSeen string
			lastSeen  string
		}
		cmdrTrackers := map[string]*commanderTracker{}

		flushFileCmdr := func(file string, cmdr string, hasUnattributed bool) error {
			if file != "" && hasUnattributed && cmdr != "" {
				if _, execError := updateCmdrStmt.Exec(cmdr, file); execError != nil {
					return execError
				}
			}
			return nil
		}

		now := ed.NowISO()
		var lastFile string
		var currentCmdr string
		fileHasUnattributed := false
		fileEvents := map[string]int{}
		filePaths := map[string]string{}
		fileFirst := map[string]string{}
		fileLast := map[string]string{}

		entryIndex := 0
		for rows.Next() {
			var rawFileName, rawFilePath, rawData string
			if scanError := rows.Scan(&rawFileName, &rawFilePath, &rawData); scanError != nil {
				return scanError
			}

			name := filepath.Base(strings.ReplaceAll(rawFileName, `\`, `/`))
			if name != lastFile {
				if err := flushFileCmdr(lastFile, currentCmdr, fileHasUnattributed); err != nil {
					return err
				}
				lastFile = name
				currentCmdr = ""
				fileHasUnattributed = false
				result.FilesScanned++
			}
			filePaths[name] = rawFilePath
			result.LinesRead++

			if entryIndex%1500 == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				reportProgress(entryIndex, total, result.FilesScanned, "Ingesting historical journal events into SQLite archive…")
			}
			entryIndex++

			parsedEvent, ok, parseError := ParseLine([]byte(rawData), name)
			if parseError != nil || !ok {
				continue
			}
			if parsedEvent.Commander != "" {
				currentCmdr = parsedEvent.Commander
			} else if currentCmdr != "" {
				parsedEvent.Commander = currentCmdr
			} else {
				fileHasUnattributed = true
			}

			if parsedEvent.Commander != "" {
				tracker, exists := cmdrTrackers[parsedEvent.Commander]
				if !exists {
					cmdrTrackers[parsedEvent.Commander] = &commanderTracker{
						fid:       parsedEvent.FID,
						firstSeen: parsedEvent.Timestamp,
						lastSeen:  parsedEvent.Timestamp,
					}
				} else {
					if tracker.fid == "" && parsedEvent.FID != "" {
						tracker.fid = parsedEvent.FID
					}
					if parsedEvent.Timestamp != "" {
						if tracker.firstSeen == "" || parsedEvent.Timestamp < tracker.firstSeen {
							tracker.firstSeen = parsedEvent.Timestamp
						}
						if tracker.lastSeen == "" || parsedEvent.Timestamp > tracker.lastSeen {
							tracker.lastSeen = parsedEvent.Timestamp
						}
					}
				}
			}

			var xCoord, yCoord, zCoord any
			if parsedEvent.StarPos != nil {
				xCoord, yCoord, zCoord = parsedEvent.StarPos.X, parsedEvent.StarPos.Y, parsedEvent.StarPos.Z
			}
			if _, execError := stmt.Exec(parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event, nullStr(parsedEvent.SystemName), parsedEvent.SystemAddress, parsedEvent.BodyID,
				nullStr(parsedEvent.BodyName), xCoord, yCoord, zCoord, nullStr(parsedEvent.Commander), name, parsedEvent.Raw, now); execError != nil {
				return execError
			}
			fileEvents[name]++
			if IsRealizationEvent(parsedEvent.Event) {
				result.HasRealizationEvents = true
			}
			if firstSeen, ok := fileFirst[name]; !ok || parsedEvent.Timestamp < firstSeen {
				fileFirst[name] = parsedEvent.Timestamp
			}
			if lastSeen, ok := fileLast[name]; !ok || parsedEvent.Timestamp > lastSeen {
				fileLast[name] = parsedEvent.Timestamp
			}
		}

		if rowsError := rows.Err(); rowsError != nil {
			return rowsError
		}

		if err := flushFileCmdr(lastFile, currentCmdr, fileHasUnattributed); err != nil {
			return err
		}

		for cmdrName, tracker := range cmdrTrackers {
			if _, execError := cmdrStmt.Exec(cmdrName, tracker.fid, tracker.firstSeen, tracker.lastSeen); execError != nil {
				return execError
			}
		}

		reportProgress(total, total, result.FilesScanned, "Finalizing journal archive metadata…")
		for name, count := range fileEvents {
			_, _ = transaction.Exec(`INSERT OR IGNORE INTO ingested_journals
				(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
				VALUES (?, ?, 0, ?, 0, ?, ?, ?, ?)`,
				name, filePaths[name], count, count, fileFirst[name], fileLast[name], now)
			result.FilesUpdated++
		}
		return db.AdvanceIngestWatermark(transaction, now)
	})
	reportProgress(total, total, result.FilesScanned, "Complete")
	return result, txError
}
