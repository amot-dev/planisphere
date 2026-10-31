package db_test

import (
	"testing"

	"planisphere/internal/db"
)

func TestDefaultPath(t *testing.T) {
	defaultPath, pathError := db.DefaultPath()
	if pathError != nil {
		t.Fatalf("DefaultPath: %v", pathError)
	}
	if defaultPath == "" {
		t.Fatalf("expected non-empty default path")
	}
}

func TestMigrationV5(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatalf("Open: %v", openError)
	}
	defer dbConn.Close()

	var count int
	if queryError := dbConn.QueryRow("SELECT count(*) FROM pragma_table_info('gec_pois_cache') WHERE name = 'source'").Scan(&count); queryError != nil {
		t.Fatalf("pragma: %v", queryError)
	}
	if count != 1 {
		t.Fatalf("expected source column to exist in gec_pois_cache")
	}
}
