package journal

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// FileheaderMeta holds journal session header metadata extracted from Fileheader.
type FileheaderMeta struct {
	Timestamp, GameVersion, Build string
	Part                          int
	Present                       bool
}

// RolloverIdentity holds inherited identity metadata from a preceding journal part.
type RolloverIdentity struct {
	Commander, FID, Universe string
}

// InsertJournalEventWithIdentityPrefixSQL inserts a journal_events row including
// its save-slot FID and universe classification.
const InsertJournalEventWithIdentityPrefixSQL = `INSERT INTO journal_events
	(event_uid, timestamp, event, system_name, system_address, body_id, body_name,
	 star_x, star_y, star_z, commander, fid, universe, source_file, raw_json, ingested_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

// BackfillPreIdentityEventsSQL attributes pre-identity lines in a journal file
// up to the timestamp of the first identity event without overwriting post-wipe rows.
const BackfillPreIdentityEventsSQL = `UPDATE journal_events
	SET commander = ?,
	    fid = CASE WHEN fid = '' THEN ? ELSE fid END,
	    universe = COALESCE(NULLIF(?, ''), universe)
	WHERE source_file = ? AND timestamp <= ?
	  AND (commander IS NULL OR commander = '' OR (commander = ? COLLATE NOCASE AND fid = ''))`

const backfillPostWipeEventsSQL = `UPDATE journal_events
	SET commander = ?,
	    fid = CASE WHEN fid = '' THEN ? ELSE fid END,
	    universe = COALESCE(NULLIF(?, ''), universe)
	WHERE source_file = ? AND timestamp >= ? AND timestamp <= ? AND event != 'ClearSavedGame' AND (commander IS NULL OR commander = '')`

const updateByteZeroExistingEventSQL = `UPDATE journal_events
	SET commander = COALESCE(NULLIF(?1, ''), commander),
	    fid = COALESCE(NULLIF(?2, ''), fid),
	    universe = COALESCE(NULLIF(?3, ''), universe)
	WHERE event_uid = ?4
	  AND (COALESCE(NULLIF(?1, ''), commander) IS NOT commander
	       OR COALESCE(NULLIF(?2, ''), fid) IS NOT fid
	       OR COALESCE(NULLIF(?3, ''), universe) IS NOT universe)`

// ExecJournalEventWithIdentity executes InsertJournalEventWithIdentityPrefixSQL.
func ExecJournalEventWithIdentity(statement *sql.Stmt, parsedEvent *Event, sourceFile string, ingestedAt string) (sql.Result, error) {
	var xCoord, yCoord, zCoord any
	if parsedEvent.StarPos != nil {
		xCoord, yCoord, zCoord = parsedEvent.StarPos.X, parsedEvent.StarPos.Y, parsedEvent.StarPos.Z
	}
	universe := parsedEvent.Universe
	if universe == "" {
		universe = "live"
	}
	return statement.Exec(
		parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event,
		NullStringOrNil(parsedEvent.SystemName), parsedEvent.SystemAddress, parsedEvent.BodyID,
		NullStringOrNil(parsedEvent.BodyName), xCoord, yCoord, zCoord,
		NullStringOrNil(parsedEvent.Commander), parsedEvent.FID, universe, sourceFile, parsedEvent.Raw, ingestedAt,
	)
}

// ParseHeaderOrContinuedLine extracts Fileheader or Continued rollover metadata from a raw JSON line.
func ParseHeaderOrContinuedLine(line []byte) (header FileheaderMeta, isHeader bool, continuedPart int, isContinued bool) {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("\xef\xbb\xbf")))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return FileheaderMeta{}, false, 0, false
	}
	peeked := PeekEventName(trimmed)
	switch {
	case strings.EqualFold(peeked, "Fileheader"):
		var payload struct {
			Timestamp   string `json:"timestamp"`
			Part        int    `json:"part"`
			GameVersion string `json:"gameversion"`
			Build       string `json:"build"`
		}
		if json.Unmarshal(trimmed, &payload) != nil {
			return FileheaderMeta{}, false, 0, false
		}
		normalizedTS := strings.TrimSpace(payload.Timestamp)
		if isoTS, err := reference.NormalizeISO(normalizedTS); err == nil {
			normalizedTS = isoTS
		}
		if payload.Part < 1 {
			payload.Part = 1
		}
		return FileheaderMeta{
			Timestamp: normalizedTS, Part: payload.Part,
			GameVersion: strings.TrimSpace(payload.GameVersion), Build: strings.TrimSpace(payload.Build), Present: true,
		}, true, 0, false
	case strings.EqualFold(peeked, "Continued"):
		var payload struct {
			Part int `json:"Part"`
		}
		if json.Unmarshal(trimmed, &payload) != nil || payload.Part < 1 {
			return FileheaderMeta{}, false, 0, false
		}
		return FileheaderMeta{}, false, payload.Part, true
	default:
		return FileheaderMeta{}, false, 0, false
	}
}

// LookupRolloverParent queries ingested_journals for the preceding session part when part > 1.
func LookupRolloverParent(queryer db.Queryer, fileName string, part int, referenceTimestamp string) (RolloverIdentity, error) {
	if part <= 1 {
		return RolloverIdentity{}, nil
	}
	queryRow := func(sqlText string, args ...any) (RolloverIdentity, bool, error) {
		var identity RolloverIdentity
		err := queryer.QueryRow(sqlText, args...).Scan(&identity.Commander, &identity.FID, &identity.Universe)
		if errors.Is(err, sql.ErrNoRows) {
			return RolloverIdentity{}, false, nil
		}
		if err != nil {
			return RolloverIdentity{}, false, fmt.Errorf("lookup rollover journal parent: %w", err)
		}
		return identity, identity.Commander != "" || identity.FID != "", nil
	}
	if prefix, _, hasPrefix := db.ParseJournalFilenamePart(fileName); hasPrefix {
		if identity, found, err := queryRow(`SELECT last_commander, last_fid, universe FROM ingested_journals WHERE file_name = ? LIMIT 1`,
			fmt.Sprintf("%s.%02d.log", prefix, part-1)); err != nil || found {
			return identity, err
		}
	}
	if identity, found, err := queryRow(`
		SELECT last_commander, last_fid, universe FROM ingested_journals
		WHERE continued_part = ? AND last_commander != ''
		  AND (? = '' OR COALESCE(last_timestamp, first_timestamp, '') <= ?)
		ORDER BY COALESCE(last_timestamp, first_timestamp, updated_at) DESC, file_name DESC LIMIT 1`,
		part, referenceTimestamp, referenceTimestamp); err != nil || found {
		return identity, err
	}
	identity, _, err := queryRow(`
		SELECT last_commander, last_fid, universe FROM ingested_journals
		WHERE part = ? AND last_commander != ''
		  AND (? = '' OR COALESCE(last_timestamp, first_timestamp, '') <= ?)
		ORDER BY COALESCE(last_timestamp, first_timestamp, updated_at) DESC, file_name DESC LIMIT 1`,
		part-1, referenceTimestamp, referenceTimestamp)
	return identity, err
}

// SessionTracker tracks header, universe, rollover, and mid-file wipe state for a journal file.
type SessionTracker struct {
	FileName, GameVersion, Build, Universe string
	CurrentCommander, CurrentFID           string
	PostWipeAfterTimestamp                 string
	Part, ContinuedPart                    int
	InheritedRollover, AttemptedRollover   bool
	SeenHeaderOrLoad, SeenIdentity         bool
	HasPreIdentityUnattrib                 bool
	HasPostWipeUnattrib                    bool
	LookupRollover                         func(part int, referenceTimestamp string) (RolloverIdentity, error)
}

// NewSessionTracker creates a session tracker initialized from fileName.
func NewSessionTracker(fileName string, lookupRollover func(part int, referenceTimestamp string) (RolloverIdentity, error)) SessionTracker {
	part := 1
	if _, parsedPart, ok := db.ParseJournalFilenamePart(fileName); ok {
		part = parsedPart
	}
	return SessionTracker{FileName: fileName, Part: part, Universe: "live", LookupRollover: lookupRollover}
}

func newTailingSessionTracker(transaction *sql.Tx, fileName string, state *fileState) (SessionTracker, error) {
	lookup := func(part int, referenceTimestamp string) (RolloverIdentity, error) {
		return LookupRolloverParent(transaction, fileName, part, referenceTimestamp)
	}
	tracker := NewSessionTracker(fileName, lookup)
	if state.offset == 0 {
		return tracker, nil
	}
	tracker.Part, tracker.GameVersion, tracker.Build = state.part, state.gameVersion, state.build
	if state.universe != "" {
		tracker.Universe = state.universe
	}
	tracker.ContinuedPart = state.continuedPart
	tracker.SeenHeaderOrLoad = state.gameVersion != "" || state.build != ""
	if state.lastCommander != "" || state.lastFID != "" {
		tracker.CurrentCommander, tracker.CurrentFID, tracker.SeenIdentity = state.lastCommander, state.lastFID, true
		return tracker, nil
	}
	var latestEvent, latestCmdr, latestFID, latestUniverse, latestTS string
	err := transaction.QueryRow(`SELECT event, COALESCE(commander, ''), fid, universe, timestamp FROM journal_events WHERE source_file = ? ORDER BY id DESC LIMIT 1`,
		fileName).Scan(&latestEvent, &latestCmdr, &latestFID, &latestUniverse, &latestTS)
	if errors.Is(err, sql.ErrNoRows) {
		return tracker, nil
	}
	if err != nil {
		return tracker, fmt.Errorf("load latest event state for %s: %w", fileName, err)
	}
	if latestUniverse != "" {
		tracker.Universe = latestUniverse
	}
	switch {
	case latestEvent == "ClearSavedGame":
		tracker.SeenIdentity, tracker.PostWipeAfterTimestamp = true, latestTS
	case latestCmdr != "":
		tracker.CurrentCommander, tracker.CurrentFID, tracker.SeenIdentity = latestCmdr, latestFID, true
	default:
		tracker.HasPreIdentityUnattrib = true
	}
	return tracker, nil
}

// ApplyRollover inherits identity and universe from a preceding session part.
func (tracker *SessionTracker) ApplyRollover(rollover RolloverIdentity) {
	if rollover.Commander != "" || rollover.FID != "" {
		tracker.CurrentCommander, tracker.CurrentFID, tracker.InheritedRollover = rollover.Commander, rollover.FID, true
	}
	if !tracker.SeenHeaderOrLoad && rollover.Universe != "" {
		tracker.Universe = rollover.Universe
	}
}

// ObserveHeaderOrContinued parses Fileheader and Continued lines and resolves part > 1 rollovers.
func (tracker *SessionTracker) ObserveHeaderOrContinued(line []byte) (bool, error) {
	header, isHeader, continuedPart, isContinued := ParseHeaderOrContinuedLine(line)
	if isHeader {
		tracker.Part, tracker.GameVersion, tracker.Build, tracker.SeenHeaderOrLoad = header.Part, header.GameVersion, header.Build, true
		classifiedUniverse := db.ClassifyJournalUniverse(header.Timestamp, header.GameVersion, header.Build, tracker.FileName, false, false)
		tracker.Universe = classifiedUniverse
		if tracker.Part > 1 && !tracker.SeenIdentity && tracker.LookupRollover != nil {
			tracker.AttemptedRollover = true
			rollover, err := tracker.LookupRollover(tracker.Part, header.Timestamp)
			if err != nil {
				return true, err
			}
			tracker.ApplyRollover(rollover)
			if header.GameVersion == "" && header.Build == "" && classifiedUniverse == "live" && rollover.Universe != "" {
				tracker.Universe = rollover.Universe
			}
		}
		return true, nil
	}
	if isContinued {
		tracker.ContinuedPart = continuedPart
		return true, nil
	}
	return false, nil
}

// ResolveTrailingRollover resolves rollover identity at end of file when part > 1 had zero events or headers.
func (tracker *SessionTracker) ResolveTrailingRollover() error {
	if tracker.Part > 1 && !tracker.SeenIdentity && !tracker.InheritedRollover && !tracker.AttemptedRollover && tracker.LookupRollover != nil {
		tracker.AttemptedRollover = true
		rollover, err := tracker.LookupRollover(tracker.Part, "")
		if err != nil {
			return err
		}
		tracker.ApplyRollover(rollover)
	}
	return nil
}

// PrepareEvent updates session state, stamps commander/FID/universe onto parsedEvent,
// and executes bounded pre-identity backfills without overwriting pre-wipe rows.
func (tracker *SessionTracker) PrepareEvent(transaction *sql.Tx, parsedEvent *Event, onSighting func(commander, fid, timestamp string) error) (bool, error) {
	if tracker.Part > 1 && !tracker.SeenHeaderOrLoad && !tracker.SeenIdentity && !tracker.InheritedRollover && !tracker.AttemptedRollover && tracker.LookupRollover != nil {
		tracker.AttemptedRollover = true
		rollover, err := tracker.LookupRollover(tracker.Part, parsedEvent.Timestamp)
		if err != nil {
			return false, err
		}
		tracker.ApplyRollover(rollover)
	}
	if err := tracker.updateUniverseForEvent(transaction, parsedEvent); err != nil {
		return false, err
	}
	parsedEvent.Universe = tracker.Universe

	if parsedEvent.Type == EventClearSavedGame {
		return tracker.applyClearSavedGameEvent(transaction, parsedEvent, onSighting)
	}
	if parsedEvent.Commander != "" {
		tracker.CurrentCommander = parsedEvent.Commander
		if parsedEvent.FID != "" {
			tracker.CurrentFID = parsedEvent.FID
		} else if tracker.CurrentFID != "" && parsedEvent.Type != EventNewCommander {
			parsedEvent.FID = tracker.CurrentFID
		}
		didBackfill, err := tracker.flushIdentitySighting(transaction, parsedEvent.Commander, parsedEvent.FID, parsedEvent.Timestamp, onSighting)
		tracker.SeenIdentity = true
		return didBackfill, err
	}
	if tracker.CurrentCommander != "" {
		parsedEvent.Commander = tracker.CurrentCommander
		if parsedEvent.FID == "" {
			parsedEvent.FID = tracker.CurrentFID
		}
	} else if !tracker.SeenIdentity {
		tracker.HasPreIdentityUnattrib = true
	} else {
		tracker.HasPostWipeUnattrib = true
	}
	return false, nil
}

func (tracker *SessionTracker) applyClearSavedGameEvent(
	transaction *sql.Tx,
	parsedEvent *Event,
	onSighting func(commander, fid, timestamp string) error,
) (bool, error) {
	if parsedEvent.Commander == "" {
		parsedEvent.Commander = tracker.CurrentCommander
	}
	if parsedEvent.FID == "" {
		parsedEvent.FID = tracker.CurrentFID
	}
	didBackfill, err := tracker.flushIdentitySighting(transaction, parsedEvent.Commander, parsedEvent.FID, parsedEvent.Timestamp, onSighting)
	if err != nil {
		return false, err
	}
	tracker.SeenIdentity = true
	tracker.HasPreIdentityUnattrib = false
	tracker.HasPostWipeUnattrib = false
	tracker.PostWipeAfterTimestamp = parsedEvent.Timestamp
	tracker.CurrentCommander = ""
	tracker.CurrentFID = ""
	return didBackfill, nil
}

func (tracker *SessionTracker) flushIdentitySighting(transaction *sql.Tx, commander, fid, timestamp string, onSighting func(string, string, string) error) (bool, error) {
	if commander == "" {
		return false, nil
	}
	if onSighting != nil {
		if err := onSighting(commander, fid, timestamp); err != nil {
			return false, err
		}
	}
	if !tracker.SeenIdentity && tracker.HasPreIdentityUnattrib {
		if _, err := transaction.Exec(BackfillPreIdentityEventsSQL, commander, fid, tracker.Universe, tracker.FileName, timestamp, commander); err != nil {
			return false, fmt.Errorf("backfill pre-identity commander for %s: %w", tracker.FileName, err)
		}
		tracker.HasPreIdentityUnattrib = false
		return true, nil
	}
	if tracker.HasPostWipeUnattrib {
		if _, err := transaction.Exec(backfillPostWipeEventsSQL, commander, fid, tracker.Universe, tracker.FileName, tracker.PostWipeAfterTimestamp, timestamp); err != nil {
			return false, fmt.Errorf("backfill post-wipe commander for %s: %w", tracker.FileName, err)
		}
		tracker.HasPostWipeUnattrib = false
		return true, nil
	}
	return false, nil
}

func (tracker *SessionTracker) updateUniverseForEvent(transaction *sql.Tx, parsedEvent *Event) error {
	if parsedEvent.Type == EventLoadGame && !tracker.SeenHeaderOrLoad {
		lgVersion, lgBuild, lgModern := db.InspectLoadGameUniverseSignals(parsedEvent.Raw)
		if lgVersion != "" {
			tracker.GameVersion = lgVersion
		}
		if lgBuild != "" {
			tracker.Build = lgBuild
		}
		prevUniverse := tracker.Universe
		tracker.Universe = db.ClassifyJournalUniverse(parsedEvent.Timestamp, tracker.GameVersion, tracker.Build, tracker.FileName, true, lgModern)
		tracker.SeenHeaderOrLoad = true
		if tracker.Universe != prevUniverse {
			if _, err := transaction.Exec(`UPDATE journal_events SET universe = ? WHERE source_file = ? AND timestamp <= ? AND universe != ?`,
				tracker.Universe, tracker.FileName, parsedEvent.Timestamp, tracker.Universe); err != nil {
				return fmt.Errorf("backfill file universe for %s: %w", tracker.FileName, err)
			}
		}
	} else if !tracker.SeenHeaderOrLoad && !tracker.InheritedRollover {
		tracker.Universe = db.ClassifyJournalUniverse(parsedEvent.Timestamp, "", "", tracker.FileName, false, false)
	}
	return nil
}

func persistIngestedEvent(insertStmt, updateByteZeroStmt *sql.Stmt, parsedEvent *Event, fileName, ingestedAt string) (bool, bool, error) {
	execResult, err := ExecJournalEventWithIdentity(insertStmt, parsedEvent, fileName, ingestedAt)
	if err != nil {
		return false, false, err
	}
	rowsAffected, err := execResult.RowsAffected()
	if err != nil {
		return false, false, fmt.Errorf("read rows affected for %s: %w", fileName, err)
	}
	if rowsAffected > 0 {
		return true, false, nil
	}
	if updateByteZeroStmt == nil {
		return false, false, nil
	}
	updateResult, err := updateByteZeroStmt.Exec(NullStringOrNil(parsedEvent.Commander), parsedEvent.FID, parsedEvent.Universe, parsedEvent.UID)
	if err != nil {
		return false, false, err
	}
	updatedRows, err := updateResult.RowsAffected()
	if err != nil {
		return false, false, fmt.Errorf("read updated rows for %s: %w", fileName, err)
	}
	return false, updatedRows > 0, nil
}

type lineIngestStatements struct {
	transaction        *sql.Tx
	insertEventStmt    *sql.Stmt
	updateByteZeroStmt *sql.Stmt
	upsertCmdrStmt     *sql.Stmt
	ingestedAt         string
}

func prepareLineIngestStatements(transaction *sql.Tx, isByteZeroPass bool, ingestedAt string) (lineIngestStatements, func(), error) {
	statements := lineIngestStatements{transaction: transaction, ingestedAt: ingestedAt}
	closeAll := func() {
		if statements.insertEventStmt != nil {
			_ = statements.insertEventStmt.Close()
		}
		if statements.updateByteZeroStmt != nil {
			_ = statements.updateByteZeroStmt.Close()
		}
		if statements.upsertCmdrStmt != nil {
			_ = statements.upsertCmdrStmt.Close()
		}
	}
	var err error
	if statements.insertEventStmt, err = transaction.Prepare(InsertJournalEventWithIdentityPrefixSQL + ` ON CONFLICT(event_uid) DO NOTHING`); err != nil {
		return statements, func() {}, err
	}
	if isByteZeroPass {
		if statements.updateByteZeroStmt, err = transaction.Prepare(updateByteZeroExistingEventSQL); err != nil {
			closeAll()
			return statements, func() {}, err
		}
	}
	if statements.upsertCmdrStmt, err = transaction.Prepare(UpsertCommanderSQL); err != nil {
		closeAll()
		return statements, func() {}, err
	}
	return statements, closeAll, nil
}
