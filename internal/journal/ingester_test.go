package journal_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/journal"
)

func TestIngestFile(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "Journal.2026-01-18T190000.01.log")
	lines := []string{
		`{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"mandalay"}`,
		`{"timestamp":"2026-01-18T19:10:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T19:20:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100],"JumpDist":4.3}`,
	}
	if err := os.WriteFile(logFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := journal.NewIngester(conn)
	res, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewEvents != 3 {
		t.Fatalf("expected 3 new events, got %d", res.NewEvents)
	}
	if res.LinesRead != 3 {
		t.Fatalf("expected 3 lines read, got %d", res.LinesRead)
	}

	// Ingesting the same file again should produce 0 new events (idempotent)
	res2, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if res2.NewEvents != 0 {
		t.Fatalf("expected 0 new events on re-ingest, got %d", res2.NewEvents)
	}
}

func TestIngestFilePartialLine(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "Journal.2026-01-18T200000.01.log")

	line1 := `{"timestamp":"2026-01-18T20:01:00Z","event":"LoadGame","Commander":"CmdrPartial","FID":"F2","Ship":"cobra"}` + "\n"
	line2 := `{"timestamp":"2026-01-18T20:02:00Z","event":"Location","StarSystem":"Sol","SystemAddress":2001,"StarPos":[0,0,0]}` + "\n"
	partialLine := `{"timestamp":"2026-01-18T20:03:00Z","event":"FSDJump","Star`

	if err := os.WriteFile(logFile, []byte(line1+line2+partialLine), 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := journal.NewIngester(conn)
	res1, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if res1.LinesRead != 2 {
		t.Fatalf("expected 2 complete lines read, got %d", res1.LinesRead)
	}
	if res1.NewEvents != 2 {
		t.Fatalf("expected 2 new events, got %d", res1.NewEvents)
	}

	var storedOffset int64
	name := filepath.Base(logFile)
	if err := conn.QueryRow("SELECT byte_offset FROM ingested_journals WHERE file_name = ?", name).Scan(&storedOffset); err != nil {
		t.Fatal(err)
	}
	expectedOffset := int64(len(line1) + len(line2))
	if storedOffset != expectedOffset {
		t.Fatalf("expected byte_offset %d, got %d", expectedOffset, storedOffset)
	}

	// Complete the third line
	remainingLine := `System":"Alpha Centauri","SystemAddress":2002,"StarPos":[0,0,100],"JumpDist":4.3}` + "\n"
	fileHandle, err := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileHandle.WriteString(remainingLine); err != nil {
		fileHandle.Close()
		t.Fatal(err)
	}
	fileHandle.Close()

	res2, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if res2.LinesRead != 1 {
		t.Fatalf("expected 1 line read on second pass, got %d", res2.LinesRead)
	}
	if res2.NewEvents != 1 {
		t.Fatalf("expected 1 new event on second pass, got %d", res2.NewEvents)
	}

	if err := conn.QueryRow("SELECT byte_offset FROM ingested_journals WHERE file_name = ?", name).Scan(&storedOffset); err != nil {
		t.Fatal(err)
	}
	expectedFinalOffset := expectedOffset + int64(len(partialLine)+len(remainingLine))
	if storedOffset != expectedFinalOffset {
		t.Fatalf("expected final byte_offset %d, got %d", expectedFinalOffset, storedOffset)
	}
}

func TestImportEDDiscoveryDB(t *testing.T) {
	tmpDir := t.TempDir()
	eddPath := filepath.Join(tmpDir, "EDD.sqlite")

	eddConn, err := sql.Open("sqlite", eddPath)
	if err != nil {
		t.Fatal(err)
	}
	defer eddConn.Close()

	_, err = eddConn.Exec(`
		CREATE TABLE TravelLogUnit (
			Id INTEGER PRIMARY KEY,
			Name TEXT,
			Path TEXT
		);
		CREATE TABLE JournalEntries (
			Id INTEGER PRIMARY KEY,
			TravelLogId INTEGER,
			EventType TEXT,
			EventData TEXT
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = eddConn.Exec(`
		INSERT INTO TravelLogUnit (Id, Name, Path) VALUES
			(1, 'Journal.2026-01-18T190000.01.log', 'C:\ED\Journal.2026-01-18T190000.01.log'),
			(2, 'Journal.2026-01-19T200000.01.log', 'C:\ED\Journal.2026-01-19T200000.01.log');
		INSERT INTO JournalEntries (Id, TravelLogId, EventType, EventData) VALUES
			(1, 1, 'Location', '{"timestamp":"2026-01-18T19:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}'),
			(2, 1, 'LoadGame', '{"timestamp":"2026-01-18T19:05:00Z","event":"LoadGame","Commander":"CmdrOne","FID":"F001","Ship":"mandalay"}'),
			(3, 1, 'FSDJump', '{"timestamp":"2026-01-18T19:20:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[0,0,100],"JumpDist":4.3}'),
			(4, 1, 'Music', '{"timestamp":"2026-01-18T19:25:00Z","event":"Music","MusicTrack":"Exploration"}'),
			(5, 2, 'Commander', '{"timestamp":"2026-01-19T20:00:00Z","event":"Commander","Commander":"CmdrTwo","FID":"F002"}'),
			(6, 2, 'FSDJump', '{"timestamp":"2026-01-19T20:30:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2001,"StarPos":[100,200,300]}');
	`)
	if err != nil {
		t.Fatal(err)
	}

	destConn, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()

	ingester := journal.NewIngester(destConn)

	var lastTotal int
	progressCalled := false
	res, err := ingester.ImportEDDiscoveryDB(eddPath, func(current, total, files int, stage string) {
		progressCalled = true
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("ImportEDDiscoveryDB failed: %v", err)
	}

	if !progressCalled {
		t.Fatal("expected onProgress to be called")
	}
	// Total filtered events should be 5 (excluding 'Music')
	if lastTotal != 5 {
		t.Fatalf("expected total 5, got %d", lastTotal)
	}
	if res.LinesRead != 5 {
		t.Fatalf("expected 5 lines read, got %d", res.LinesRead)
	}
	if res.FilesScanned != 2 {
		t.Fatalf("expected 2 files scanned, got %d", res.FilesScanned)
	}

	// Verify all 5 events in journal_events have the correct commander set,
	// especially event 1 (Location) which arrived before LoadGame in file 1.
	rows, err := destConn.Query(`SELECT event, commander FROM journal_events ORDER BY timestamp ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	expectedCmdrs := []struct {
		event string
		cmdr  string
	}{
		{"Location", "CmdrOne"},
		{"LoadGame", "CmdrOne"},
		{"FSDJump", "CmdrOne"},
		{"Commander", "CmdrTwo"},
		{"FSDJump", "CmdrTwo"},
	}

	idx := 0
	for rows.Next() {
		var ev, cmdr sql.NullString
		if err := rows.Scan(&ev, &cmdr); err != nil {
			t.Fatal(err)
		}
		if idx >= len(expectedCmdrs) {
			t.Fatalf("extra row returned: %s, %s", ev.String, cmdr.String)
		}
		if ev.String != expectedCmdrs[idx].event {
			t.Errorf("row %d: expected event %s, got %s", idx, expectedCmdrs[idx].event, ev.String)
		}
		if cmdr.String != expectedCmdrs[idx].cmdr {
			t.Errorf("row %d (%s): expected commander %s, got %s", idx, ev.String, expectedCmdrs[idx].cmdr, cmdr.String)
		}
		idx++
	}
	if idx != len(expectedCmdrs) {
		t.Fatalf("expected %d rows, got %d", len(expectedCmdrs), idx)
	}

	// Verify commanders table
	cmdrRows, err := destConn.Query(`SELECT name, fid, first_seen, last_seen FROM commanders ORDER BY name ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer cmdrRows.Close()

	type cmdrRecord struct {
		name, fid, firstSeen, lastSeen string
	}
	var cmdrs []cmdrRecord
	for cmdrRows.Next() {
		var c cmdrRecord
		if err := cmdrRows.Scan(&c.name, &c.fid, &c.firstSeen, &c.lastSeen); err != nil {
			t.Fatal(err)
		}
		cmdrs = append(cmdrs, c)
	}
	if len(cmdrs) != 2 {
		t.Fatalf("expected 2 commanders, got %d", len(cmdrs))
	}
	if cmdrs[0].name != "CmdrOne" || cmdrs[0].fid != "F001" || cmdrs[0].firstSeen != "2026-01-18T19:05:00Z" || cmdrs[0].lastSeen != "2026-01-18T19:20:00Z" {
		t.Errorf("unexpected CmdrOne record: %+v", cmdrs[0])
	}
	if cmdrs[1].name != "CmdrTwo" || cmdrs[1].fid != "F002" || cmdrs[1].firstSeen != "2026-01-19T20:00:00Z" || cmdrs[1].lastSeen != "2026-01-19T20:30:00Z" {
		t.Errorf("unexpected CmdrTwo record: %+v", cmdrs[1])
	}
}
