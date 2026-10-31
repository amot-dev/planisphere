package db_test

import (
	"testing"

	"planisphere/internal/db"
)

func TestDefaultPath(t *testing.T) {
	p, err := db.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if p == "" {
		t.Fatalf("expected non-empty default path")
	}
}

func TestMigrationV5(t *testing.T) {
	conn, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	var count int
	if err := conn.QueryRow("SELECT count(*) FROM pragma_table_info('gec_pois_cache') WHERE name = 'source'").Scan(&count); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected source column to exist in gec_pois_cache")
	}
}
