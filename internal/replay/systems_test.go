package replay_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/testutil/dbtest"
)

func TestJournalSystemQueriesRespectCmdrFilterLatestOrderAndBatchChunking(testingContext *testing.T) {
	var typedNilDB *sql.DB
	if nilCoords, nilError := replay.QueryJournalSystemCoords(typedNilDB, "Sol"); nilError != nil || nilCoords != nil {
		testingContext.Fatalf("expected nil result for typed-nil *sql.DB in QueryJournalSystemCoords, got %+v err=%v", nilCoords, nilError)
	}

	databaseConnection := dbtest.OpenMemory(testingContext)

	if _, execError := databaseConnection.Exec(`
		INSERT INTO commanders (name, fid, enabled, first_seen, last_seen) VALUES
		('CmdrEnabled', 'FE', 1, '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z'),
		('CmdrDisabled', 'FD', 0, '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z');

		INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, commander, source_file, raw_json, ingested_at) VALUES
		('sys-old', '2026-01-01T10:00:00Z', 'FSDJump', 'alpha centauri', 100, 1.0, 2.0, 3.0, 'CmdrEnabled', 'a.log', '{}', '2026-01-01T10:00:00Z'),
		('sys-new', '2026-01-02T10:00:00Z', 'FSDJump', 'Alpha Centauri', 102, 4.0, 5.0, 6.0, 'CmdrEnabled', 'a.log', '{}', '2026-01-02T10:00:00Z'),
		('sys-nocoords', '2026-01-03T10:00:00Z', 'Scan', 'ALPHA CENTAURI', 102, NULL, NULL, NULL, 'CmdrEnabled', 'a.log', '{}', '2026-01-03T10:00:00Z'),
		('sys-disabled-override', '2026-01-04T10:00:00Z', 'FSDJump', 'Alpha Centauri', 999, 99.0, 99.0, 99.0, 'CmdrDisabled', 'b.log', '{}', '2026-01-04T10:00:00Z'),
		('sys-disabled-only', '2026-01-04T11:00:00Z', 'FSDJump', 'Raxxla', 777, 10.0, 20.0, 30.0, 'CmdrDisabled', 'b.log', '{}', '2026-01-04T11:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed system events: %v", execError)
	}

	singleCoords, singleError := replay.QueryJournalSystemCoords(databaseConnection, "  alpha   centauri ")
	if singleError != nil || singleCoords == nil {
		testingContext.Fatalf("QueryJournalSystemCoords: coords=%+v err=%v", singleCoords, singleError)
	}
	if singleCoords.Name != "Alpha Centauri" || singleCoords.SystemAddress != 102 || singleCoords.Pos.X != 4.0 {
		testingContext.Fatalf("expected latest enabled coordinate row, got %+v", singleCoords)
	}

	disabledCoords, disabledError := replay.QueryJournalSystemCoords(databaseConnection, "Raxxla")
	if disabledError != nil || disabledCoords != nil {
		testingContext.Fatalf("expected disabled commander system Raxxla excluded, got %+v err=%v", disabledCoords, disabledError)
	}

	canonicalAlpha, canonicalError := replay.QueryCanonicalSystemName(databaseConnection, "alpha centauri")
	if canonicalError != nil || canonicalAlpha != "ALPHA CENTAURI" {
		testingContext.Fatalf("expected latest enabled canonical name 'ALPHA CENTAURI', got %q err=%v", canonicalAlpha, canonicalError)
	}
	canonicalDisabled, canonicalDisabledError := replay.QueryCanonicalSystemName(databaseConnection, "raxxla")
	if canonicalDisabledError != nil || canonicalDisabled != "" {
		testingContext.Fatalf("expected empty canonical name for disabled commander system, got %q err=%v", canonicalDisabled, canonicalDisabledError)
	}

	// Seed 520 systems to exercise multi-chunk IN queries (> 500 chunk boundary).
	batchInputs := make([]string, 0, 525)
	batchInputs = append(batchInputs, "  alpha centauri ", "RAXXLA", "   ")
	statement, prepareError := databaseConnection.Prepare(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, commander, source_file, raw_json, ingested_at)
		 VALUES (?, '2026-02-01T00:00:00Z', 'FSDJump', ?, ?, ?, 0, 0, 'CmdrEnabled', 'bulk.log', '{}', '2026-02-01T00:00:00Z')`,
	)
	if prepareError != nil {
		testingContext.Fatalf("prepare bulk insert: %v", prepareError)
	}
	defer statement.Close()

	for systemIndex := 0; systemIndex < 520; systemIndex++ {
		canonicalSystemName := fmt.Sprintf("Sector Batch %03d", systemIndex)
		if _, execError := statement.Exec(
			fmt.Sprintf("bulk-uid-%d", systemIndex),
			canonicalSystemName,
			int64(2000+systemIndex),
			float64(systemIndex),
		); execError != nil {
			testingContext.Fatalf("insert bulk system %d: %v", systemIndex, execError)
		}
		batchInputs = append(batchInputs, strings.ToLower(canonicalSystemName))
	}

	batchResult, batchError := replay.QueryJournalSystemCoordsBatch(databaseConnection, batchInputs)
	if batchError != nil {
		testingContext.Fatalf("QueryJournalSystemCoordsBatch: %v", batchError)
	}
	if len(batchResult) != 521 {
		testingContext.Fatalf("expected 521 enabled systems resolved across chunks, got %d", len(batchResult))
	}
	if _, foundDisabled := batchResult["raxxla"]; foundDisabled {
		testingContext.Fatalf("expected disabled commander system excluded from batch result")
	}
	lastBatchEntry, foundLast := batchResult["sector batch 519"]
	if !foundLast || lastBatchEntry.Name != "Sector Batch 519" || lastBatchEntry.Pos.X != 519 {
		testingContext.Fatalf("unexpected chunked entry 519: %+v (found=%v)", lastBatchEntry, foundLast)
	}
}
