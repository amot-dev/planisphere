package eddiscovery

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// ProgressFunc receives progress updates during an EDDiscovery archive import.
type ProgressFunc func(current, total, files int, stage string)

var relevantEventFilterSQL = "(journalEntry.EventType IN (" +
	journal.FormatSQLInClause(journal.AllEvents&^journal.NewEventSet(journal.EventMusic)) +
	",'Fileheader','FileHeader','Continued') OR (journalEntry.EventType = 'Music' AND journalEntry.EventData LIKE '%MainMenu%'))"

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
	statements.insertEventStmt, prepError = transaction.Prepare(journal.InsertJournalEventWithIdentityPrefixSQL + ` ON CONFLICT(event_uid) DO UPDATE SET
		commander = COALESCE(NULLIF(excluded.commander, ''), journal_events.commander),
		fid = CASE WHEN excluded.fid != '' THEN excluded.fid ELSE journal_events.fid END,
		universe = CASE WHEN excluded.universe != '' THEN excluded.universe ELSE journal_events.universe END
	WHERE COALESCE(NULLIF(excluded.commander, ''), journal_events.commander) IS NOT journal_events.commander
	   OR (excluded.fid != '' AND excluded.fid != journal_events.fid)
	   OR (excluded.universe != '' AND excluded.universe != journal_events.universe)`)
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
	fid, firstSeen, lastSeen string
}

func recordCommanderSighting(trackers map[string]*commanderSightingTracker, commanderName, frontierID, eventTimestamp string) {
	if commanderName == "" {
		return
	}
	tracker, exists := trackers[commanderName]
	if !exists {
		trackers[commanderName] = &commanderSightingTracker{fid: frontierID, firstSeen: eventTimestamp, lastSeen: eventTimestamp}
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

type importedFileMeta struct {
	fileName, filePath, firstTimestamp, lastTimestamp string
	gameVersion, build, universe, lastCommander       string
	lastFID                                           string
	eventCount, part, continuedPart                   int
}

type archiveImportAccumulator struct {
	transaction          *sql.Tx
	statements           *importStatements
	commanderTrackers    map[string]*commanderSightingTracker
	filesByName          map[string]*importedFileMeta
	orderedFiles         []*importedFileMeta
	currentFile          *importedFileMeta
	tracker              journal.SessionTracker
	fallbackCommander    string
	ingestedAt           string
	commandersBackfilled int
}

func newArchiveImportAccumulator(transaction *sql.Tx, statements *importStatements, ingestedAt string) *archiveImportAccumulator {
	return &archiveImportAccumulator{
		transaction:       transaction,
		statements:        statements,
		commanderTrackers: make(map[string]*commanderSightingTracker),
		filesByName:       make(map[string]*importedFileMeta),
		ingestedAt:        ingestedAt,
	}
}

func sanitizeFallbackCommander(rawName string) string {
	trimmed := strings.TrimSpace(rawName)
	if trimmed == "" || strings.EqualFold(trimmed, "Hidden Log") {
		return ""
	}
	return trimmed
}

func (accumulator *archiveImportAccumulator) finishCurrentFile() error {
	meta := accumulator.currentFile
	if meta == nil {
		return nil
	}
	if err := accumulator.tracker.ResolveTrailingRollover(); err != nil {
		return err
	}
	meta.part, meta.gameVersion, meta.build = accumulator.tracker.Part, accumulator.tracker.GameVersion, accumulator.tracker.Build
	meta.universe, meta.continuedPart = accumulator.tracker.Universe, accumulator.tracker.ContinuedPart
	meta.lastCommander, meta.lastFID = accumulator.tracker.CurrentCommander, accumulator.tracker.CurrentFID

	if !accumulator.tracker.SeenIdentity && !accumulator.tracker.InheritedRollover &&
		accumulator.fallbackCommander != "" && meta.eventCount > 0 {
		backfillRes, err := accumulator.statements.backfillCommanderStmt.Exec(accumulator.fallbackCommander, meta.fileName)
		if err != nil {
			return err
		}
		if affectedRows, rowsErr := backfillRes.RowsAffected(); rowsErr == nil {
			accumulator.commandersBackfilled += int(affectedRows)
		}
		meta.lastCommander = accumulator.fallbackCommander
		recordCommanderSighting(accumulator.commanderTrackers, accumulator.fallbackCommander, "", meta.firstTimestamp)
		recordCommanderSighting(accumulator.commanderTrackers, accumulator.fallbackCommander, "", meta.lastTimestamp)
	}
	return nil
}

func (accumulator *archiveImportAccumulator) lookupRolloverParent(fileName string, part int, referenceTimestamp string) (journal.RolloverIdentity, error) {
	if part <= 1 {
		return journal.RolloverIdentity{}, nil
	}
	if prefix, _, ok := db.ParseJournalFilenamePart(fileName); ok {
		if parent := accumulator.filesByName[fmt.Sprintf("%s.%02d.log", prefix, part-1)]; parent != nil && parent.lastCommander != "" {
			return journal.RolloverIdentity{Commander: parent.lastCommander, FID: parent.lastFID, Universe: parent.universe}, nil
		}
	}
	for index := len(accumulator.orderedFiles) - 1; index >= 0; index-- {
		candidate := accumulator.orderedFiles[index]
		candTS := candidate.lastTimestamp
		if candTS == "" {
			candTS = candidate.firstTimestamp
		}
		if candidate.fileName != fileName &&
			(candidate.continuedPart == part || candidate.part == part-1) &&
			candidate.lastCommander != "" &&
			(referenceTimestamp == "" || candTS == "" || candTS <= referenceTimestamp) {
			return journal.RolloverIdentity{Commander: candidate.lastCommander, FID: candidate.lastFID, Universe: candidate.universe}, nil
		}
	}
	return journal.LookupRolloverParent(accumulator.transaction, fileName, part, referenceTimestamp)
}

func (accumulator *archiveImportAccumulator) switchFileIfNeeded(fileName, filePath, rawFallback string, result *journal.Result) error {
	if accumulator.currentFile != nil && accumulator.currentFile.fileName == fileName {
		if accumulator.fallbackCommander == "" {
			accumulator.fallbackCommander = sanitizeFallbackCommander(rawFallback)
		}
		return nil
	}
	if err := accumulator.finishCurrentFile(); err != nil {
		return err
	}
	meta := accumulator.filesByName[fileName]
	if meta == nil {
		meta = &importedFileMeta{fileName: fileName, filePath: filePath, part: 1, universe: "live"}
		accumulator.filesByName[fileName] = meta
		accumulator.orderedFiles = append(accumulator.orderedFiles, meta)
	}
	accumulator.currentFile = meta
	accumulator.fallbackCommander = sanitizeFallbackCommander(rawFallback)
	accumulator.tracker = journal.NewSessionTracker(fileName, func(part int, referenceTimestamp string) (journal.RolloverIdentity, error) {
		return accumulator.lookupRolloverParent(fileName, part, referenceTimestamp)
	})
	result.FilesScanned++
	return nil
}

func (accumulator *archiveImportAccumulator) processEntry(rawFileName, rawFilePath, rawEventData, rawFallback string, result *journal.Result) error {
	normalizedFileName := filepath.Base(strings.ReplaceAll(rawFileName, `\`, `/`))
	if err := accumulator.switchFileIfNeeded(normalizedFileName, rawFilePath, rawFallback, result); err != nil {
		return err
	}
	accumulator.currentFile.filePath = rawFilePath
	result.LinesRead++

	consumed, headerErr := accumulator.tracker.ObserveHeaderOrContinued([]byte(rawEventData))
	if headerErr != nil || consumed {
		return headerErr
	}
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
	backfilledCount, prepErr := accumulator.tracker.PrepareEvent(accumulator.transaction, &parsedEvent, nil)
	if prepErr != nil {
		return prepErr
	}
	accumulator.commandersBackfilled += backfilledCount
	recordCommanderSighting(accumulator.commanderTrackers, parsedEvent.Commander, parsedEvent.FID, parsedEvent.Timestamp)
	if _, execError := journal.ExecJournalEventWithIdentity(accumulator.statements.insertEventStmt, &parsedEvent, normalizedFileName, accumulator.ingestedAt); execError != nil {
		return execError
	}
	accumulator.currentFile.eventCount++
	result.Events = result.Events.Add(parsedEvent.Type)
	result.ObserveTimestamp(parsedEvent.Timestamp)
	if accumulator.currentFile.firstTimestamp == "" || parsedEvent.Timestamp < accumulator.currentFile.firstTimestamp {
		accumulator.currentFile.firstTimestamp = parsedEvent.Timestamp
	}
	if parsedEvent.Timestamp > accumulator.currentFile.lastTimestamp {
		accumulator.currentFile.lastTimestamp = parsedEvent.Timestamp
	}
	return nil
}

func (accumulator *archiveImportAccumulator) finalizeMetadata(transaction *sql.Tx, totalEntries int, reportProgress ProgressFunc, result *journal.Result) error {
	if finishError := accumulator.finishCurrentFile(); finishError != nil {
		return finishError
	}
	for commanderName, tracker := range accumulator.commanderTrackers {
		if _, execError := accumulator.statements.upsertCommanderStmt.Exec(commanderName, tracker.fid, tracker.firstSeen, tracker.lastSeen); execError != nil {
			return execError
		}
	}
	reportProgress(totalEntries, totalEntries, result.FilesScanned, "Finalizing journal archive metadata...")
	upsertJournalStmt, prepErr := transaction.Prepare(`INSERT INTO ingested_journals
		(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp,
		 part, game_version, build, universe, continued_part, last_commander, last_fid, updated_at)
		VALUES (?, ?, 0, ?, 0, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_name) DO UPDATE SET
			part = CASE WHEN ingested_journals.part <= 1 AND excluded.part > 1 THEN excluded.part ELSE ingested_journals.part END,
			game_version = COALESCE(NULLIF(ingested_journals.game_version, ''), excluded.game_version),
			build = COALESCE(NULLIF(ingested_journals.build, ''), excluded.build),
			universe = COALESCE(NULLIF(excluded.universe, ''), ingested_journals.universe),
			continued_part = CASE WHEN ingested_journals.continued_part = 0 THEN excluded.continued_part ELSE ingested_journals.continued_part END,
			last_commander = COALESCE(NULLIF(ingested_journals.last_commander, ''), excluded.last_commander),
			last_fid = COALESCE(NULLIF(ingested_journals.last_fid, ''), excluded.last_fid)`)
	if prepErr != nil {
		return prepErr
	}
	defer upsertJournalStmt.Close()

	for _, meta := range accumulator.orderedFiles {
		if meta.eventCount == 0 && meta.continuedPart == 0 && meta.lastCommander == "" {
			continue
		}
		if _, execError := upsertJournalStmt.Exec(
			meta.fileName, meta.filePath, meta.eventCount, meta.eventCount, meta.firstTimestamp, meta.lastTimestamp,
			meta.part, meta.gameVersion, meta.build, meta.universe, meta.continuedPart, meta.lastCommander, meta.lastFID, accumulator.ingestedAt); execError != nil {
			return fmt.Errorf("record imported journal %s: %w", meta.fileName, execError)
		}
		if meta.eventCount > 0 {
			result.FilesUpdated++
		}
	}
	result.CommandersBackfilled += accumulator.commandersBackfilled
	profilesChanged, resolveError := db.ResolveCommandersChanged(transaction)
	if resolveError != nil {
		return fmt.Errorf("resolve commanders after EDDiscovery import: %w", resolveError)
	}
	result.ProfilesChanged = result.ProfilesChanged || profilesChanged
	return db.AdvanceIngestWatermark(transaction, accumulator.ingestedAt)
}

func detectEDDCommanderJoinSQL(ctx context.Context, eddConn *sql.DB) (selectExpr string, joinClause string) {
	hasColumn := func(tableName, targetColumn string) bool {
		rows, err := eddConn.QueryContext(ctx, "PRAGMA table_info("+tableName+")")
		if err != nil {
			return false
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var cid, notNull, pk int
			var colName, colType string
			var defaultVal any
			if rows.Scan(&cid, &colName, &colType, &notNull, &defaultVal, &pk) != nil {
				return false
			}
			if strings.EqualFold(colName, targetColumn) {
				found = true
			}
		}
		return found && rows.Err() == nil
	}
	if !hasColumn("TravelLogUnit", "CommanderId") || !hasColumn("Commanders", "Id") || !hasColumn("Commanders", "Name") {
		return "''", ""
	}
	joinCondition := "LEFT JOIN Commanders eddCommander ON eddCommander.Id = travelUnit.CommanderId"
	if hasColumn("Commanders", "Deleted") {
		joinCondition += " AND COALESCE(eddCommander.Deleted, 0) = 0"
	}
	return "COALESCE(eddCommander.Name, '')", joinCondition
}

func importEntriesTx(ctx context.Context, destConn *sql.DB, rows *sql.Rows, totalEntries int, reportProgress ProgressFunc, result *journal.Result) error {
	return db.WithTx(destConn, func(transaction *sql.Tx) error {
		statements, prepError := prepareImportStatements(transaction)
		if prepError != nil {
			return prepError
		}
		defer statements.close()

		accumulator := newArchiveImportAccumulator(transaction, statements, reference.NowISO())
		entryIndex := 0
		for rows.Next() {
			var rawFileName, rawFilePath, rawEventData, rawFallbackCommander string
			if scanError := rows.Scan(&rawFileName, &rawFilePath, &rawEventData, &rawFallbackCommander); scanError != nil {
				return scanError
			}
			if entryIndex%1500 == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
			if entryError := accumulator.processEntry(rawFileName, rawFilePath, rawEventData, rawFallbackCommander, result); entryError != nil {
				return entryError
			}
			if entryIndex%1500 == 0 {
				reportProgress(entryIndex, totalEntries, result.FilesScanned, "Ingesting historical journal events into SQLite archive...")
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
		reportProgress := func(processedEntries, total, files int, stage string) {
			if onProgress != nil {
				onProgress(processedEntries, total, files, stage)
			}
		}
		var result journal.Result
		if fileInfo, statError := os.Stat(databasePath); statError != nil || fileInfo.IsDir() {
			return result, fmt.Errorf("EDDiscovery database not found: %s", databasePath)
		}
		reportProgress(0, 0, 0, "Opening EDDiscovery database and reading journal entries...")
		eddConn, openError := sql.Open("sqlite", "file:"+databasePath+"?mode=ro&_pragma=busy_timeout(5000)")
		if openError != nil {
			return result, fmt.Errorf("open EDDiscovery DB: %w", openError)
		}
		defer eddConn.Close()

		var totalEntries int
		if countError := eddConn.QueryRowContext(ctx, `SELECT COUNT(*) FROM JournalEntries journalEntry WHERE `+relevantEventFilterSQL).Scan(&totalEntries); countError != nil {
			return result, fmt.Errorf("count EDDiscovery JournalEntries: %w", countError)
		}
		fallbackSelectSQL, fallbackJoinSQL := detectEDDCommanderJoinSQL(ctx, eddConn)
		rows, queryError := eddConn.QueryContext(ctx, `
			SELECT COALESCE(NULLIF(travelUnit.Name, ''), 'EDD-' || journalEntry.TravelLogId),
			       COALESCE(NULLIF(travelUnit.Path, ''), 'EDD-' || journalEntry.TravelLogId),
			       journalEntry.EventData, `+fallbackSelectSQL+`
			FROM JournalEntries journalEntry
			LEFT JOIN TravelLogUnit travelUnit ON travelUnit.Id = journalEntry.TravelLogId `+fallbackJoinSQL+`
			WHERE `+relevantEventFilterSQL+`
			ORDER BY journalEntry.TravelLogId ASC, journalEntry.Id ASC`)
		if queryError != nil {
			return result, fmt.Errorf("query EDDiscovery JournalEntries: %w", queryError)
		}
		defer rows.Close()

		reportProgress(0, totalEntries, 0, "Ingesting historical journal events into SQLite archive...")
		txError := importEntriesTx(ctx, destConn, rows, totalEntries, reportProgress, &result)
		if txError == nil {
			reportProgress(totalEntries, totalEntries, result.FilesScanned, "Complete")
		}
		return result, txError
	})
}
