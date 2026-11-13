package eddiscovery

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
)

// ProgressFunc receives progress updates during an EDDiscovery archive import.
type ProgressFunc func(current, total, files int, stage string)

const relevantEventFilterSQL = `journalEntry.EventType IN (
	'Commander','LoadGame','Statistics','Location','FSDJump','CarrierJump',
	'CarrierStats','CarrierDepositFuel','CarrierJumpRequest','Docked','Undocked','FSDTarget','Scan','SAAScanComplete','FSSDiscoveryScan',
	'ScanOrganic','MultiSellExplorationData','SellExplorationData','SellOrganicData','ShipyardSwap',
	'Died','Resurrect','NpcCrewPaidWage'
)`

type importStatements struct {
	insertEventStmt       *sql.Stmt
	upsertCommanderStmt   *sql.Stmt
	backfillCommanderStmt *sql.Stmt
}

func (statements *importStatements) close() {
	if statements.insertEventStmt != nil {
		_ = statements.insertEventStmt.Close()
	}
	if statements.upsertCommanderStmt != nil {
		_ = statements.upsertCommanderStmt.Close()
	}
	if statements.backfillCommanderStmt != nil {
		_ = statements.backfillCommanderStmt.Close()
	}
}

func prepareImportStatements(transaction *sql.Tx) (*importStatements, error) {
	statements := &importStatements{}
	var prepError error

	// Upsert so missing commander attribution on earlier live-ingested rows is backfilled on conflict.
	statements.insertEventStmt, prepError = transaction.Prepare(journal.InsertJournalEventPrefixSQL + ` ON CONFLICT(event_uid) DO UPDATE SET
		commander = COALESCE(NULLIF(excluded.commander, ''), journal_events.commander)`)
	if prepError != nil {
		return nil, prepError
	}

	statements.upsertCommanderStmt, prepError = transaction.Prepare(journal.UpsertCommanderSQL)
	if prepError != nil {
		statements.close()
		return nil, prepError
	}

	statements.backfillCommanderStmt, prepError = transaction.Prepare(journal.BackfillFileCommanderSQL)
	if prepError != nil {
		statements.close()
		return nil, prepError
	}

	return statements, nil
}

type commanderSightingTracker struct {
	fid       string
	firstSeen string
	lastSeen  string
}

func recordCommanderSighting(
	trackers map[string]*commanderSightingTracker,
	commanderName string,
	frontierID string,
	eventTimestamp string,
) {
	if commanderName == "" {
		return
	}
	tracker, exists := trackers[commanderName]
	if !exists {
		trackers[commanderName] = &commanderSightingTracker{
			fid:       frontierID,
			firstSeen: eventTimestamp,
			lastSeen:  eventTimestamp,
		}
		return
	}
	if tracker.fid == "" && frontierID != "" {
		tracker.fid = frontierID
	}
	if eventTimestamp != "" {
		if tracker.firstSeen == "" || eventTimestamp < tracker.firstSeen {
			tracker.firstSeen = eventTimestamp
		}
		if tracker.lastSeen == "" || eventTimestamp > tracker.lastSeen {
			tracker.lastSeen = eventTimestamp
		}
	}
}

type archiveImportAccumulator struct {
	statements          *importStatements
	commanderTrackers   map[string]*commanderSightingTracker
	fileEventCounts     map[string]int
	filePaths           map[string]string
	fileFirstTimestamps map[string]string
	fileLastTimestamps  map[string]string
	lastFileName        string
	currentCommander    string
	fileHasUnattributed bool
	ingestedAt          string
}

func newArchiveImportAccumulator(statements *importStatements, ingestedAt string) *archiveImportAccumulator {
	return &archiveImportAccumulator{
		statements:          statements,
		commanderTrackers:   map[string]*commanderSightingTracker{},
		fileEventCounts:     map[string]int{},
		filePaths:           map[string]string{},
		fileFirstTimestamps: map[string]string{},
		fileLastTimestamps:  map[string]string{},
		ingestedAt:          ingestedAt,
	}
}

func (accumulator *archiveImportAccumulator) flushPendingFileCommander() error {
	if accumulator.lastFileName != "" && accumulator.fileHasUnattributed && accumulator.currentCommander != "" {
		if _, execError := accumulator.statements.backfillCommanderStmt.Exec(accumulator.currentCommander, accumulator.lastFileName); execError != nil {
			return execError
		}
	}
	return nil
}

func (accumulator *archiveImportAccumulator) processEntry(
	rawFileName string,
	rawFilePath string,
	rawEventData string,
	result *journal.Result,
) error {
	normalizedFileName := filepath.Base(strings.ReplaceAll(rawFileName, `\`, `/`))
	if normalizedFileName != accumulator.lastFileName {
		if flushError := accumulator.flushPendingFileCommander(); flushError != nil {
			return flushError
		}
		accumulator.lastFileName = normalizedFileName
		accumulator.currentCommander = ""
		accumulator.fileHasUnattributed = false
		result.FilesScanned++
	}
	accumulator.filePaths[normalizedFileName] = rawFilePath
	result.LinesRead++

	parsedEvent, keepEvent, parseError := journal.ParseLine([]byte(rawEventData), normalizedFileName)
	if parseError != nil {
		if len(result.Errors) < 20 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", normalizedFileName, parseError))
		}
		return nil
	}
	if !keepEvent {
		return nil
	}
	if parsedEvent.Commander != "" {
		accumulator.currentCommander = parsedEvent.Commander
	} else if accumulator.currentCommander != "" {
		parsedEvent.Commander = accumulator.currentCommander
	} else {
		accumulator.fileHasUnattributed = true
	}

	recordCommanderSighting(accumulator.commanderTrackers, parsedEvent.Commander, parsedEvent.FID, parsedEvent.Timestamp)

	if _, execError := journal.ExecJournalEvent(accumulator.statements.insertEventStmt, &parsedEvent, normalizedFileName, accumulator.ingestedAt); execError != nil {
		return execError
	}

	accumulator.fileEventCounts[normalizedFileName]++
	if journal.IsRealizationEvent(parsedEvent.Event) {
		result.HasRealizationEvents = true
	}
	if firstSeen, exists := accumulator.fileFirstTimestamps[normalizedFileName]; !exists || parsedEvent.Timestamp < firstSeen {
		accumulator.fileFirstTimestamps[normalizedFileName] = parsedEvent.Timestamp
	}
	if lastSeen, exists := accumulator.fileLastTimestamps[normalizedFileName]; !exists || parsedEvent.Timestamp > lastSeen {
		accumulator.fileLastTimestamps[normalizedFileName] = parsedEvent.Timestamp
	}
	return nil
}

func (accumulator *archiveImportAccumulator) finalizeMetadata(
	transaction *sql.Tx,
	totalEntries int,
	reportProgress ProgressFunc,
	result *journal.Result,
) error {
	if flushError := accumulator.flushPendingFileCommander(); flushError != nil {
		return flushError
	}

	for commanderName, tracker := range accumulator.commanderTrackers {
		if _, execError := accumulator.statements.upsertCommanderStmt.Exec(commanderName, tracker.fid, tracker.firstSeen, tracker.lastSeen); execError != nil {
			return execError
		}
	}

	reportProgress(totalEntries, totalEntries, result.FilesScanned, "Finalizing journal archive metadata…")
	for fileName, eventCount := range accumulator.fileEventCounts {
		if _, execError := transaction.Exec(`INSERT OR IGNORE INTO ingested_journals
			(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
			VALUES (?, ?, 0, ?, 0, ?, ?, ?, ?)`,
			fileName, accumulator.filePaths[fileName], eventCount, eventCount,
			accumulator.fileFirstTimestamps[fileName], accumulator.fileLastTimestamps[fileName], accumulator.ingestedAt); execError != nil {
			return fmt.Errorf("record imported journal %s: %w", fileName, execError)
		}
		result.FilesUpdated++
	}
	return db.AdvanceIngestWatermark(transaction, accumulator.ingestedAt)
}

func importEntriesTx(
	ctx context.Context,
	destConn *sql.DB,
	rows *sql.Rows,
	totalEntries int,
	reportProgress ProgressFunc,
	result *journal.Result,
) error {
	return db.WithTx(destConn, func(transaction *sql.Tx) error {
		statements, prepError := prepareImportStatements(transaction)
		if prepError != nil {
			return prepError
		}
		defer statements.close()

		accumulator := newArchiveImportAccumulator(statements, ed.NowISO())
		entryIndex := 0
		for rows.Next() {
			var rawFileName, rawFilePath, rawEventData string
			if scanError := rows.Scan(&rawFileName, &rawFilePath, &rawEventData); scanError != nil {
				return scanError
			}

			if entryIndex%1500 == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}

			if entryError := accumulator.processEntry(rawFileName, rawFilePath, rawEventData, result); entryError != nil {
				return entryError
			}

			if entryIndex%1500 == 0 {
				reportProgress(entryIndex, totalEntries, result.FilesScanned, "Ingesting historical journal events into SQLite archive…")
			}
			entryIndex++
		}

		if rowsError := rows.Err(); rowsError != nil {
			return rowsError
		}

		return accumulator.finalizeMetadata(transaction, totalEntries, reportProgress, result)
	})
}

// Import performs a one-time recovery of raw journal events stored inside an
// EDDiscovery SQLite database (JournalEntries.EventData) for older .log files
// that may have been deleted from disk. Once imported, all events live
// permanently inside Planisphere's journal_events table.
func Import(ctx context.Context, ingester *journal.Ingester, databasePath string, onProgress ProgressFunc) (journal.Result, error) {
	return ingester.WithExclusiveConn(func(destConn *sql.DB) (journal.Result, error) {
		select {
		case <-ctx.Done():
			return journal.Result{}, ctx.Err()
		default:
		}

		reportProgress := func(current, total, files int, stage string) {
			if onProgress != nil {
				onProgress(current, total, files, stage)
			}
		}

		var result journal.Result
		if fileInfo, statError := os.Stat(databasePath); statError != nil || fileInfo.IsDir() {
			return result, fmt.Errorf("EDDiscovery database not found: %s", databasePath)
		}

		reportProgress(0, 0, 0, "Opening EDDiscovery database and reading journal entries…")
		eddConn, openError := sql.Open("sqlite", "file:"+databasePath+"?mode=ro&_pragma=busy_timeout(5000)")
		if openError != nil {
			return result, fmt.Errorf("open EDDiscovery DB: %w", openError)
		}
		defer eddConn.Close()

		var totalEntries int
		if countError := eddConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM JournalEntries journalEntry WHERE `+relevantEventFilterSQL).Scan(&totalEntries); countError != nil {
			return result, fmt.Errorf("count EDDiscovery JournalEntries: %w", countError)
		}

		rows, queryError := eddConn.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(travelUnit.Name, ''), 'EDD-' || journalEntry.TravelLogId),
			       COALESCE(NULLIF(travelUnit.Path, ''), 'EDD-' || journalEntry.TravelLogId),
			       journalEntry.EventData
			FROM JournalEntries journalEntry
			LEFT JOIN TravelLogUnit travelUnit ON travelUnit.Id = journalEntry.TravelLogId
			WHERE `+relevantEventFilterSQL+`
			ORDER BY journalEntry.TravelLogId ASC, journalEntry.Id ASC`)
		if queryError != nil {
			return result, fmt.Errorf("query EDDiscovery JournalEntries: %w", queryError)
		}
		defer rows.Close()

		reportProgress(0, totalEntries, 0, "Ingesting historical journal events into SQLite archive…")
		txError := importEntriesTx(ctx, destConn, rows, totalEntries, reportProgress, &result)
		if txError == nil {
			journal.InvalidateCommanderCountsCache()
		}
		reportProgress(totalEntries, totalEntries, result.FilesScanned, "Complete")
		return result, txError
	})
}

