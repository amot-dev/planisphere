package journal_test

import (
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
	if len(res.NewEvents) != 3 {
		t.Fatalf("expected 3 new events, got %d", len(res.NewEvents))
	}
	if res.LinesRead != 3 {
		t.Fatalf("expected 3 lines read, got %d", res.LinesRead)
	}

	// Ingesting the same file again should produce 0 new events (idempotent)
	res2, err := ingester.IngestFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.NewEvents) != 0 {
		t.Fatalf("expected 0 new events on re-ingest, got %d", len(res2.NewEvents))
	}
}
