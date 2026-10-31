package journal

import (
	"bytes"
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
	FilesScanned int
	FilesUpdated int
	LinesRead    int
	NewEvents    []Event // events actually inserted (not previously known)
	Errors       []string
}

func (r *Result) merge(o Result) {
	r.FilesScanned += o.FilesScanned
	r.FilesUpdated += o.FilesUpdated
	r.LinesRead += o.LinesRead
	r.NewEvents = append(r.NewEvents, o.NewEvents...)
	r.Errors = append(r.Errors, o.Errors...)
}

// ListJournalFiles returns all journal files in dir sorted by name (which is
// chronological within each of the two historical naming schemes).
func ListJournalFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, JournalGlob))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// ScanDir ingests every journal file in dir (idempotent startup catch-up).
func (in *Ingester) ScanDir(dir string) (Result, error) {
	var res Result
	if !DirExists(dir) {
		return res, fmt.Errorf("journal directory not found: %s", dir)
	}
	files, err := ListJournalFiles(dir)
	if err != nil {
		return res, err
	}
	for _, f := range files {
		r, err := in.IngestFile(f)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", filepath.Base(f), err))
		}
		res.merge(r)
	}
	return res, nil
}

type fileState struct {
	offset int64
	lines  int64
	stored int64
	first  sql.NullString
	last   sql.NullString
}

// IngestFile ingests any new complete lines appended to path since the last pass.
func (in *Ingester) IngestFile(path string) (Result, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	res := Result{FilesScanned: 1}
	st, err := os.Stat(path)
	if err != nil {
		return res, err
	}
	name := filepath.Base(path)
	size := st.Size()

	var fs fileState
	err = in.conn.QueryRow(`SELECT byte_offset, line_count, events_stored, first_timestamp, last_timestamp
		FROM ingested_journals WHERE file_name = ?`, name).Scan(&fs.offset, &fs.lines, &fs.stored, &fs.first, &fs.last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return res, err
	}
	if fs.offset > size {
		// File was truncated or replaced: re-read it; dedup makes this safe.
		fs = fileState{}
	}
	if fs.offset == size && err == nil {
		return res, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer f.Close()
	if _, err := f.Seek(fs.offset, io.SeekStart); err != nil {
		return res, err
	}
	data, err := io.ReadAll(io.LimitReader(f, size-fs.offset))
	if err != nil {
		return res, err
	}
	// Only consume complete lines; a partially written trailing line is
	// picked up on the next pass.
	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL < 0 {
		data = nil
	} else {
		data = data[:lastNL+1]
	}
	newOffset := fs.offset + int64(len(data))

	err = db.WithTx(in.conn, func(tx *sql.Tx) error {
		var currentCmdr string
		var prevCmdr sql.NullString
		_ = tx.QueryRow(`SELECT commander FROM journal_events WHERE source_file = ? AND commander IS NOT NULL AND commander != '' ORDER BY timestamp DESC LIMIT 1`, name).Scan(&prevCmdr)
		currentCmdr = prevCmdr.String

		stmt, err := tx.Prepare(`INSERT INTO journal_events
			(event_uid, timestamp, event, system_name, system_address, body_id, body_name,
			 star_x, star_y, star_z, commander, source_file, raw_json, ingested_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(event_uid) DO UPDATE SET
				commander = COALESCE(NULLIF(excluded.commander, ''), journal_events.commander)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		existStmt, err := tx.Prepare(`SELECT 1 FROM journal_events WHERE event_uid = ?`)
		if err != nil {
			return err
		}
		defer existStmt.Close()
		cmdrStmt, err := tx.Prepare(`INSERT INTO commanders(name, fid, enabled, first_seen, last_seen)
			VALUES(?, ?, 1, ?, ?)
			ON CONFLICT(name) DO UPDATE SET
				fid = CASE WHEN excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
				first_seen = CASE WHEN commanders.first_seen IS NULL OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
				last_seen = CASE WHEN commanders.last_seen IS NULL OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`)
		if err != nil {
			return err
		}
		defer cmdrStmt.Close()

		now := ed.NowISO()
		for len(data) > 0 {
			i := bytes.IndexByte(data, '\n')
			line := data[:i]
			data = data[i+1:]
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			fs.lines++
			res.LinesRead++
			ev, ok, perr := ParseLine(line, name)
			if perr != nil {
				if len(res.Errors) < 20 {
					res.Errors = append(res.Errors, fmt.Sprintf("%s line %d: %v", name, fs.lines, perr))
				}
				continue
			}
			if !ok {
				continue
			}
			if ev.Commander != "" {
				currentCmdr = ev.Commander
				_, _ = cmdrStmt.Exec(ev.Commander, ev.FID, ev.Timestamp, ev.Timestamp)
				// Backfill any earlier events in the same file that arrived before LoadGame/Commander.
				_, _ = tx.Exec(`UPDATE journal_events SET commander = ? WHERE source_file = ? AND (commander IS NULL OR commander = '')`, currentCmdr, name)
			} else if currentCmdr != "" {
				ev.Commander = currentCmdr
			}
			var x, y, z any
			if ev.StarPos != nil {
				x, y, z = ev.StarPos.X, ev.StarPos.Y, ev.StarPos.Z
			}
			var existed int
			_ = existStmt.QueryRow(ev.UID).Scan(&existed)
			_, err := stmt.Exec(ev.UID, ev.Timestamp, ev.Event, nullStr(ev.SystemName), ev.SystemAddress, ev.BodyID,
				nullStr(ev.BodyName), x, y, z, nullStr(ev.Commander), name, ev.Raw, now)
			if err != nil {
				return err
			}
			if existed == 0 {
				fs.stored++
				res.NewEvents = append(res.NewEvents, ev)
				if !fs.first.Valid || ev.Timestamp < fs.first.String {
					fs.first = sql.NullString{String: ev.Timestamp, Valid: true}
				}
				if !fs.last.Valid || ev.Timestamp > fs.last.String {
					fs.last = sql.NullString{String: ev.Timestamp, Valid: true}
				}
			}
		}
		if currentCmdr != "" && fs.last.Valid {
			_, _ = cmdrStmt.Exec(currentCmdr, "", fs.last.String, fs.last.String)
		}
		_, err = tx.Exec(`INSERT INTO ingested_journals
			(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(file_name) DO UPDATE SET
				file_path = excluded.file_path, byte_offset = excluded.byte_offset, line_count = excluded.line_count,
				file_size = excluded.file_size, events_stored = excluded.events_stored,
				first_timestamp = excluded.first_timestamp, last_timestamp = excluded.last_timestamp,
				updated_at = excluded.updated_at`,
			name, path, newOffset, fs.lines, size, fs.stored, fs.first, fs.last, now)
		return err
	})
	if err != nil {
		return res, err
	}
	res.FilesUpdated = 1
	return res, nil
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
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
func ArchiveStatus(q db.Queryer) (Status, error) {
	var s Status
	var first, last sql.NullString
	if err := q.QueryRow(`SELECT COUNT(*) FROM ingested_journals`).Scan(&s.FilesTracked); err != nil {
		return s, err
	}
	if err := q.QueryRow(`SELECT COUNT(*), MIN(timestamp), MAX(timestamp) FROM journal_events WHERE `+CmdrFilterSQL).Scan(&s.EventsStored, &first, &last); err != nil {
		return s, err
	}
	s.FirstEventAt, s.LastEventAt = first.String, last.String
	return s, nil
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
func ListCommanders(q db.Queryer) ([]Commander, error) {
	rs, err := q.Query(`SELECT c.name, COALESCE(c.fid, ''), c.enabled, COALESCE(c.first_seen, ''), COALESCE(c.last_seen, ''),
		(SELECT COUNT(*) FROM journal_events je WHERE je.commander = c.name COLLATE NOCASE)
		FROM commanders c
		ORDER BY c.first_seen ASC, c.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := []Commander{}
	for rs.Next() {
		var c Commander
		var en int
		if err := rs.Scan(&c.Name, &c.FID, &en, &c.FirstSeen, &c.LastSeen, &c.EventCount); err != nil {
			return nil, err
		}
		c.Enabled = en != 0
		c.FirstGalactic = ed.ToGalactic(c.FirstSeen)
		c.LastGalactic = ed.ToGalactic(c.LastSeen)
		out = append(out, c)
	}
	return out, rs.Err()
}

// SetCommanderEnabled toggles whether a commander's events are included in stats and plans.
func SetCommanderEnabled(q db.Queryer, name string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := q.Exec(`UPDATE commanders SET enabled = ? WHERE name = ? COLLATE NOCASE`, en, strings.TrimSpace(name))
	return err
}

// ImportEDDiscoveryDB performs a one-time recovery of raw journal events stored
// inside an EDDiscovery SQLite database (JournalEntries.EventData) for older
// .log files that may have been deleted from disk. Once imported, all events
// live permanently inside our own journal_events table.
func (in *Ingester) ImportEDDiscoveryDB(eddPath string, onProgress func(current, total, files int, stage string)) (Result, error) {
	in.mu.Lock()
	defer in.mu.Unlock()

	report := func(cur, tot, files int, stage string) {
		if onProgress != nil {
			onProgress(cur, tot, files, stage)
		}
	}

	var res Result
	if st, err := os.Stat(eddPath); err != nil || st.IsDir() {
		return res, fmt.Errorf("EDDiscovery database not found: %s", eddPath)
	}

	report(0, 0, 0, "Opening EDDiscovery database & reading journal entries…")
	eddConn, err := sql.Open("sqlite", "file:"+eddPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return res, fmt.Errorf("open EDDiscovery DB: %w", err)
	}
	defer eddConn.Close()

	rs, err := eddConn.Query(`
		SELECT COALESCE(NULLIF(t.Name, ''), 'EDD-' || j.TravelLogId),
		       COALESCE(NULLIF(t.Path, ''), 'EDD-' || j.TravelLogId),
		       j.EventData
		FROM JournalEntries j
		LEFT JOIN TravelLogUnit t ON t.Id = j.TravelLogId
		WHERE j.EventType IN (
			'Commander','LoadGame','Statistics','Location','FSDJump','CarrierJump',
			'CarrierStats','FSDTarget','Scan','SAAScanComplete','FSSDiscoveryScan',
			'ScanOrganic','MultiSellExplorationData','SellOrganicData','ShipyardSwap'
		)
		ORDER BY j.TravelLogId ASC, j.Id ASC`)
	if err != nil {
		return res, fmt.Errorf("query EDDiscovery JournalEntries: %w", err)
	}
	defer rs.Close()

	type entryRow struct {
		fileName string
		filePath string
		raw      string
	}
	var entries []entryRow
	for rs.Next() {
		var er entryRow
		if err := rs.Scan(&er.fileName, &er.filePath, &er.raw); err != nil {
			return res, err
		}
		entries = append(entries, er)
	}
	if err := rs.Err(); err != nil {
		return res, err
	}

	total := len(entries)
	report(0, total, 0, "Ingesting historical journal events into SQLite archive…")

	err = db.WithTx(in.conn, func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`INSERT INTO journal_events
			(event_uid, timestamp, event, system_name, system_address, body_id, body_name,
			 star_x, star_y, star_z, commander, source_file, raw_json, ingested_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(event_uid) DO UPDATE SET
				commander = COALESCE(NULLIF(excluded.commander, ''), journal_events.commander)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		cmdrStmt, err := tx.Prepare(`INSERT INTO commanders(name, fid, enabled, first_seen, last_seen)
			VALUES(?, ?, 1, ?, ?)
			ON CONFLICT(name) DO UPDATE SET
				fid = CASE WHEN excluded.fid != '' THEN excluded.fid ELSE commanders.fid END,
				first_seen = CASE WHEN commanders.first_seen IS NULL OR excluded.first_seen < commanders.first_seen THEN excluded.first_seen ELSE commanders.first_seen END,
				last_seen = CASE WHEN commanders.last_seen IS NULL OR excluded.last_seen > commanders.last_seen THEN excluded.last_seen ELSE commanders.last_seen END`)
		if err != nil {
			return err
		}
		defer cmdrStmt.Close()

		now := ed.NowISO()
		var lastFile string
		var currentCmdr string
		fileEvents := map[string]int{}
		filePaths := map[string]string{}
		fileFirst := map[string]string{}
		fileLast := map[string]string{}

		for idx, er := range entries {
			name := filepath.Base(strings.ReplaceAll(er.fileName, `\`, `/`))
			if name != lastFile {
				lastFile = name
				currentCmdr = ""
				res.FilesScanned++
			}
			filePaths[name] = er.filePath
			res.LinesRead++

			if idx%1500 == 0 {
				report(idx, total, res.FilesScanned, "Ingesting historical journal events into SQLite archive…")
			}

			ev, ok, perr := ParseLine([]byte(er.raw), name)
			if perr != nil || !ok {
				continue
			}
			if ev.Commander != "" {
				currentCmdr = ev.Commander
				_, _ = cmdrStmt.Exec(ev.Commander, ev.FID, ev.Timestamp, ev.Timestamp)
				_, _ = tx.Exec(`UPDATE journal_events SET commander = ? WHERE source_file = ? AND (commander IS NULL OR commander = '')`, currentCmdr, name)
			} else if currentCmdr != "" {
				ev.Commander = currentCmdr
				_, _ = cmdrStmt.Exec(currentCmdr, "", ev.Timestamp, ev.Timestamp)
			}

			var x, y, z any
			if ev.StarPos != nil {
				x, y, z = ev.StarPos.X, ev.StarPos.Y, ev.StarPos.Z
			}
			if _, err := stmt.Exec(ev.UID, ev.Timestamp, ev.Event, nullStr(ev.SystemName), ev.SystemAddress, ev.BodyID,
				nullStr(ev.BodyName), x, y, z, nullStr(ev.Commander), name, ev.Raw, now); err != nil {
				return err
			}
			fileEvents[name]++
			if f, ok := fileFirst[name]; !ok || ev.Timestamp < f {
				fileFirst[name] = ev.Timestamp
			}
			if l, ok := fileLast[name]; !ok || ev.Timestamp > l {
				fileLast[name] = ev.Timestamp
			}
		}

		report(total, total, res.FilesScanned, "Finalizing journal archive metadata…")
		for name, count := range fileEvents {
			_, _ = tx.Exec(`INSERT OR IGNORE INTO ingested_journals
				(file_name, file_path, byte_offset, line_count, file_size, events_stored, first_timestamp, last_timestamp, updated_at)
				VALUES (?, ?, 0, ?, 0, ?, ?, ?, ?)`,
				name, filePaths[name], count, count, fileFirst[name], fileLast[name], now)
			res.FilesUpdated++
		}
		return nil
	})
	report(total, total, res.FilesScanned, "Complete")
	return res, err
}
