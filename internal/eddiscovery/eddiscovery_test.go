package eddiscovery_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/eddiscovery"
	"planisphere/internal/journal"
)

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
	res, err := eddiscovery.Import(context.Background(), ingester, eddPath, func(current, total, files int, stage string) {
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

func TestImportEDDiscoveryDB_ContextCancellation(t *testing.T) {
	temporaryDatabaseConnection, databaseOpenError := db.Open(":memory:")
	if databaseOpenError != nil {
		t.Fatal(databaseOpenError)
	}
	defer temporaryDatabaseConnection.Close()

	ingesterInstance := journal.NewIngester(temporaryDatabaseConnection)

	cancelledContext, cancelFunction := context.WithCancel(context.Background())
	cancelFunction() // Cancel immediately

	_, importError := eddiscovery.Import(cancelledContext, ingesterInstance, "nonexistent.sqlite", nil)
	if !errors.Is(importError, context.Canceled) {
		t.Fatalf("expected context.Canceled error from ImportEDDiscoveryDB, got %v", importError)
	}
}

