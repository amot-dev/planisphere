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

func TestMigrateIdempotency(t *testing.T) {
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		t.Fatalf("Open: %v", openError)
	}
	defer dbConn.Close()

	// Calling Migrate a second time must succeed without errors
	if err := db.Migrate(dbConn); err != nil {
		t.Fatalf("second Migrate call failed: %v", err)
	}

	// Verify expected migration keys were recorded in settings
	expectedKeys := []string{
		"default_modes_seeded",
		"default_expedition_seeded",
		"ingest_parser_v2",
		"gec_schema_v2",
		"commander_schema_v3",
		"fc_arrived_v4",
		"gec_schema_v5",
	}

	for _, key := range expectedKeys {
		val, err := db.GetSetting(dbConn, key)
		if err != nil {
			t.Fatalf("GetSetting(%q): %v", key, err)
		}
		if val != "1" {
			t.Errorf("expected setting %q to be '1', got %q", key, val)
		}
	}

	// Verify default expedition was seeded
	var expName string
	if err := dbConn.QueryRow("SELECT name FROM expeditions LIMIT 1").Scan(&expName); err != nil {
		t.Fatalf("query default expedition: %v", err)
	}
	if expName != db.DefaultExpeditionName {
		t.Errorf("expected expedition %q, got %q", db.DefaultExpeditionName, expName)
	}
}
