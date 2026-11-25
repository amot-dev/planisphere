package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const legacyGalaxySplitTimestamp = "2022-11-29T00:00:00Z"

// ClassifyJournalUniverse classifies a journal file into live, legacy, or beta
// using version headers, filename conventions, and LoadGame payload markers.
func ClassifyJournalUniverse(timestamp, gameVersion, build, sourceFile string, loadGamePresent, loadGameHasOdysseyOrVersion bool) string {
	lowerVersion := asciiLower(strings.TrimSpace(gameVersion))
	lowerBuild := asciiLower(strings.TrimSpace(build))
	if strings.Contains(lowerVersion, "beta") || strings.Contains(lowerVersion, "alpha") ||
		strings.Contains(lowerBuild, "beta") || strings.Contains(lowerBuild, "alpha") {
		return "beta"
	}
	if timestamp >= legacyGalaxySplitTimestamp {
		if strings.HasPrefix(lowerVersion, "3.") || strings.HasPrefix(lowerVersion, "2.") ||
			isLegacyTwoDigitYearJournalFilename(sourceFile) ||
			(loadGamePresent && !loadGameHasOdysseyOrVersion && lowerVersion == "") {
			return "legacy"
		}
	}
	return "live"
}

func isLegacyTwoDigitYearJournalFilename(sourceFile string) bool {
	baseName := filepath.Base(strings.ReplaceAll(sourceFile, `\`, `/`))
	if !strings.HasPrefix(baseName, "Journal.") || len(baseName) < len("Journal.")+6 {
		return false
	}
	stamp := baseName[len("Journal."):]
	for index := 0; index < 6; index++ {
		if stamp[index] < '0' || stamp[index] > '9' {
			return false
		}
	}
	return true
}

// InspectLoadGameUniverseSignals extracts version and Odyssey markers from a LoadGame JSON line.
func InspectLoadGameUniverseSignals(rawJSON string) (gameVersion string, build string, hasOdysseyOrVersion bool) {
	var payload struct {
		Odyssey     *json.RawMessage `json:"Odyssey"`
		GameVersion *string          `json:"gameversion"`
		Build       string           `json:"build"`
	}
	if json.Unmarshal([]byte(rawJSON), &payload) != nil {
		return "", "", false
	}
	if payload.GameVersion != nil {
		gameVersion = strings.TrimSpace(*payload.GameVersion)
	}
	return gameVersion, strings.TrimSpace(payload.Build), payload.Odyssey != nil || payload.GameVersion != nil
}

// ParseJournalFilenamePart parses Journal.<stamp>.<part>.log into its stem prefix and 1-based part number.
func ParseJournalFilenamePart(fileName string) (prefix string, part int, ok bool) {
	baseName := filepath.Base(strings.ReplaceAll(fileName, `\`, `/`))
	if !strings.HasPrefix(baseName, "Journal") || !strings.HasSuffix(baseName, ".log") {
		return "", 0, false
	}
	withoutExtension := strings.TrimSuffix(baseName, ".log")
	dotIndex := strings.LastIndexByte(withoutExtension, '.')
	if dotIndex <= 0 || dotIndex+1 >= len(withoutExtension) {
		return "", 0, false
	}
	partToken := withoutExtension[dotIndex+1:]
	for index := 0; index < len(partToken); index++ {
		if partToken[index] < '0' || partToken[index] > '9' {
			return "", 0, false
		}
	}
	parsedPart, parseError := strconv.Atoi(partToken)
	if parseError != nil || parsedPart < 1 {
		return "", 0, false
	}
	return withoutExtension[:dotIndex], parsedPart, true
}

type v13FileMetadata struct {
	fileName, prefix, firstTimestamp, lastTimestamp     string
	gameVersion, build, universe, loadGameTimestamp     string
	singleFID, lastCommander, lastFID                   string
	inheritedCommander, inheritedFID                    string
	part, unattributedEvents                            int
	loadGamePresent, loadGameHasModern, inIngestedTable bool
	sawClearSavedGame                                   bool
	distinctFIDs                                        map[string]bool
}

func migrateCommanderProfilesV13(dbConn *sql.DB) error {
	return WithTx(dbConn, func(transaction *sql.Tx) error {
		if err := prepareCommanderIndexesAndDefaultsV13(transaction); err != nil {
			return err
		}
		orderedFiles, filesByName, err := collectMigrationFilesV13(transaction)
		if err != nil {
			return err
		}
		propagateUniverseAndRolloversV13(orderedFiles, filesByName)
		if err := persistFileMetadataV13(transaction, orderedFiles); err != nil {
			return err
		}
		return ResolveCommanders(transaction)
	})
}

func prepareCommanderIndexesAndDefaultsV13(transaction *sql.Tx) error {
	statements := []string{
		`DROP INDEX IF EXISTS idx_journal_events_cmdr`,
		`CREATE INDEX idx_journal_events_cmdr ON journal_events(commander COLLATE NOCASE, fid, universe, timestamp, profile_id)`,
		`DROP INDEX IF EXISTS idx_journal_events_profile`,
		`CREATE INDEX idx_journal_events_profile ON journal_events(profile_id, commander COLLATE NOCASE, timestamp, fid)`,
		`UPDATE journal_events SET fid = TRIM(COALESCE(json_extract(raw_json, '$.FID'), ''))
		 WHERE event IN ('Commander', 'LoadGame', 'ClearSavedGame', 'NewCommander')
		   AND fid = '' AND TRIM(COALESCE(json_extract(raw_json, '$.FID'), '')) != ''`,
	}
	for _, stmtSQL := range statements {
		if _, err := transaction.Exec(stmtSQL); err != nil {
			return fmt.Errorf("prepare v13 indexes and identity FIDs: %w", err)
		}
	}
	return nil
}

func collectMigrationFilesV13(transaction *sql.Tx) ([]*v13FileMetadata, map[string]*v13FileMetadata, error) {
	filesByName := make(map[string]*v13FileMetadata, 128)
	ensureFile := func(name string) *v13FileMetadata {
		meta := filesByName[name]
		if meta == nil {
			meta = &v13FileMetadata{fileName: name, part: 1, universe: "live", distinctFIDs: make(map[string]bool, 2)}
			filesByName[name] = meta
		}
		return meta
	}
	if err := scanMigrationSourcesV13(transaction, ensureFile); err != nil {
		return nil, nil, err
	}
	if err := scanMigrationIdentityRowsV13(transaction, ensureFile); err != nil {
		return nil, nil, err
	}
	orderedFiles := make([]*v13FileMetadata, 0, len(filesByName))
	for _, meta := range filesByName {
		orderedFiles = append(orderedFiles, meta)
	}
	slices.SortFunc(orderedFiles, func(left, right *v13FileMetadata) int {
		if left.firstTimestamp != right.firstTimestamp {
			return strings.Compare(left.firstTimestamp, right.firstTimestamp)
		}
		return strings.Compare(left.fileName, right.fileName)
	})
	return orderedFiles, filesByName, nil
}

func scanMigrationSourcesV13(transaction *sql.Tx, ensureFile func(string) *v13FileMetadata) error {
	trackedRows, err := transaction.Query(`SELECT file_name, COALESCE(first_timestamp, ''), COALESCE(last_timestamp, '') FROM ingested_journals`)
	if err != nil {
		return fmt.Errorf("query ingested_journals (v13): %w", err)
	}
	defer trackedRows.Close()
	for trackedRows.Next() {
		var name, firstTS, lastTS string
		if scanErr := trackedRows.Scan(&name, &firstTS, &lastTS); scanErr != nil {
			return scanErr
		}
		meta := ensureFile(name)
		meta.inIngestedTable, meta.firstTimestamp, meta.lastTimestamp = true, firstTS, lastTS
	}
	if err := trackedRows.Err(); err != nil {
		return err
	}

	eventRows, err := transaction.Query(`
		SELECT source_file, MIN(timestamp), MAX(timestamp),
		       MAX(CASE WHEN commander IS NOT NULL AND commander != '' THEN timestamp || char(0) || commander ELSE '' END),
		       SUM(CASE WHEN commander IS NULL OR commander = '' THEN 1 ELSE 0 END)
		FROM journal_events WHERE source_file IS NOT NULL AND source_file != '' GROUP BY source_file`)
	if err != nil {
		return fmt.Errorf("query source_file summary (v13): %w", err)
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var name, minTS, maxTS, encodedCmdr string
		var unattributed int
		if scanErr := eventRows.Scan(&name, &minTS, &maxTS, &encodedCmdr, &unattributed); scanErr != nil {
			return scanErr
		}
		meta := ensureFile(name)
		if meta.firstTimestamp == "" || (minTS != "" && minTS < meta.firstTimestamp) {
			meta.firstTimestamp = minTS
		}
		if maxTS > meta.lastTimestamp {
			meta.lastTimestamp = maxTS
		}
		if sep := strings.IndexByte(encodedCmdr, 0); sep >= 0 {
			meta.lastCommander = encodedCmdr[sep+1:]
		}
		meta.unattributedEvents = unattributed
	}
	return eventRows.Err()
}

func scanMigrationIdentityRowsV13(transaction *sql.Tx, ensureFile func(string) *v13FileMetadata) error {
	rows, err := transaction.Query(`
		SELECT id, COALESCE(source_file, ''), timestamp, event, COALESCE(commander, ''), fid,
		       CASE WHEN event = 'LoadGame' THEN raw_json ELSE '' END
		FROM journal_events WHERE event IN ('Commander', 'LoadGame', 'ClearSavedGame', 'NewCommander')`)
	if err != nil {
		return fmt.Errorf("query identity events (v13): %w", err)
	}
	defer rows.Close()
	type identityItem struct {
		id                                           int64
		sourceFile, timestamp, event, commander, fid string
		rawJSON                                      string
	}
	var items []identityItem
	for rows.Next() {
		var item identityItem
		if scanErr := rows.Scan(&item.id, &item.sourceFile, &item.timestamp, &item.event, &item.commander, &item.fid, &item.rawJSON); scanErr != nil {
			return scanErr
		}
		if item.sourceFile != "" {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	slices.SortFunc(items, func(left, right identityItem) int {
		if left.timestamp != right.timestamp {
			return strings.Compare(left.timestamp, right.timestamp)
		}
		return int(left.id - right.id)
	})
	for _, item := range items {
		applyIdentityRowToFileV13(ensureFile(item.sourceFile), item.timestamp, item.event, item.commander, item.fid, item.rawJSON)
	}
	return nil
}

func applyIdentityRowToFileV13(meta *v13FileMetadata, timestamp, eventName, commander, fid, rawJSON string) {
	if fid != "" {
		meta.distinctFIDs[fid], meta.lastFID = true, fid
	}
	if commander != "" {
		meta.lastCommander = commander
	}
	if eventName == "ClearSavedGame" {
		meta.lastCommander, meta.lastFID, meta.sawClearSavedGame = "", "", true
		return
	}
	if commander != "" || fid != "" {
		meta.sawClearSavedGame = false
	}
	if eventName == "LoadGame" {
		meta.loadGamePresent, meta.loadGameTimestamp = true, timestamp
		gameVersion, build, hasModern := InspectLoadGameUniverseSignals(rawJSON)
		if gameVersion != "" {
			meta.gameVersion = gameVersion
		}
		if build != "" {
			meta.build = build
		}
		meta.loadGameHasModern = meta.loadGameHasModern || hasModern
	}
}

func propagateUniverseAndRolloversV13(orderedFiles []*v13FileMetadata, filesByName map[string]*v13FileMetadata) {
	type prefixPart struct {
		prefix string
		part   int
	}
	byPrefixPart := make(map[prefixPart]*v13FileMetadata, len(orderedFiles))
	for _, meta := range orderedFiles {
		if prefix, part, ok := ParseJournalFilenamePart(meta.fileName); ok {
			meta.prefix, meta.part = prefix, part
			byPrefixPart[prefixPart{prefix, part}] = meta
		}
		if len(meta.distinctFIDs) == 1 {
			for soleFID := range meta.distinctFIDs {
				meta.singleFID = soleFID
			}
		}
		classifyTS := meta.loadGameTimestamp
		if classifyTS == "" {
			classifyTS = meta.firstTimestamp
		}
		meta.universe = ClassifyJournalUniverse(classifyTS, meta.gameVersion, meta.build, meta.fileName, meta.loadGamePresent, meta.loadGameHasModern)
	}
	for index, meta := range orderedFiles {
		if meta.part <= 1 {
			continue
		}
		parent := byPrefixPart[prefixPart{meta.prefix, meta.part - 1}]
		if parent == nil {
			parent = filesByName[fmt.Sprintf("%s.%02d.log", meta.prefix, meta.part-1)]
		}
		if parent == nil && index > 0 && orderedFiles[index-1].part == meta.part-1 {
			parent = orderedFiles[index-1]
		}
		if parent == nil {
			continue
		}
		if !meta.loadGamePresent && meta.gameVersion == "" {
			meta.universe = parent.universe
		}
		if !meta.sawClearSavedGame && parent.lastCommander != "" {
			meta.inheritedCommander, meta.inheritedFID = parent.lastCommander, parent.lastFID
			if meta.lastCommander == "" {
				meta.lastCommander, meta.lastFID = parent.lastCommander, parent.lastFID
			}
			if meta.singleFID == "" && len(meta.distinctFIDs) == 0 {
				meta.singleFID = parent.lastFID
			}
		}
	}
}

func persistFileMetadataV13(transaction *sql.Tx, orderedFiles []*v13FileMetadata) error {
	rolloverStmt, err := transaction.Prepare(`UPDATE journal_events SET commander = ?, fid = CASE WHEN fid = '' THEN ? ELSE fid END WHERE source_file = ? AND (commander IS NULL OR commander = '')`)
	if err != nil {
		return err
	}
	defer rolloverStmt.Close()
	fidStmt, err := transaction.Prepare(`UPDATE journal_events SET fid = ? WHERE source_file = ? AND fid = ''`)
	if err != nil {
		return err
	}
	defer fidStmt.Close()
	universeStmt, err := transaction.Prepare(`UPDATE journal_events SET universe = ? WHERE source_file = ? AND universe != ?`)
	if err != nil {
		return err
	}
	defer universeStmt.Close()
	ingestedStmt, err := transaction.Prepare(`UPDATE ingested_journals SET part = ?, game_version = ?, build = ?, universe = ?, last_commander = ?, last_fid = ? WHERE file_name = ?`)
	if err != nil {
		return err
	}
	defer ingestedStmt.Close()

	for _, meta := range orderedFiles {
		if err := applyFileMetadataStatementsV13(meta, rolloverStmt, fidStmt, universeStmt, ingestedStmt); err != nil {
			return err
		}
	}
	return nil
}

func applyFileMetadataStatementsV13(meta *v13FileMetadata, rolloverStmt, fidStmt, universeStmt, ingestedStmt *sql.Stmt) error {
	if meta.inheritedCommander != "" && meta.unattributedEvents > 0 {
		if _, err := rolloverStmt.Exec(meta.inheritedCommander, meta.inheritedFID, meta.fileName); err != nil {
			return fmt.Errorf("backfill rollover commander for %s (v13): %w", meta.fileName, err)
		}
	}
	if meta.singleFID != "" {
		if _, err := fidStmt.Exec(meta.singleFID, meta.fileName); err != nil {
			return fmt.Errorf("backfill file FID for %s (v13): %w", meta.fileName, err)
		}
	}
	if meta.universe != "live" {
		if _, err := universeStmt.Exec(meta.universe, meta.fileName, meta.universe); err != nil {
			return fmt.Errorf("backfill file universe for %s (v13): %w", meta.fileName, err)
		}
	}
	if meta.inIngestedTable {
		if _, err := ingestedStmt.Exec(meta.part, meta.gameVersion, meta.build, meta.universe, meta.lastCommander, meta.lastFID, meta.fileName); err != nil {
			return fmt.Errorf("update ingested_journals metadata for %s (v13): %w", meta.fileName, err)
		}
	}
	return nil
}
