package db

import (
	"database/sql"
	"fmt"
	"time"

	"planisphere/internal/version"
)

const (
	// IngestSchemaWatermarkKey stores the latest ingested_journals.updated_at
	// timestamp observed under the current ingestion schema so that any
	// journals ingested while downgraded to an older binary are detected and
	// healed on startup.
	IngestSchemaWatermarkKey = "ingest_schema_watermark"

	// LastAppVersionKey records the application version that last migrated
	// this database.
	LastAppVersionKey = "last_app_version"
)

// AdvanceIngestWatermark records timestamp as the latest ingestion schema
// watermark in settings, preserving any existing watermark that is already
// lexicographically newer.
func AdvanceIngestWatermark(queryer Queryer, timestamp string) error {
	_, execError := queryer.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = CASE WHEN excluded.value > settings.value THEN excluded.value ELSE settings.value END`,
		IngestSchemaWatermarkKey, timestamp)
	return execError
}

// healDowngradedJournalIngest detects and repairs journal files that were
// ingested while the user ran an older binary against this database. Newer
// binaries advance settings.ingest_schema_watermark whenever journals are
// ingested, whereas older binaries do not know about the watermark setting and
// only advance ingested_journals.updated_at. Any row with updated_at > watermark
// was therefore touched by an older parser (potentially skipping newer event
// types or generating legacy UIDs), so its byte_offset is reset for re-ingestion
// and SellOrganicData UIDs are re-normalized. Finally, currentWatermark takes
// max(now, latestJournalUpdatedAt) so that future-dated updated_at timestamps
// from clock skew do not trigger an infinite re-healing loop on subsequent launches.
func healDowngradedJournalIngest(dbConn *sql.DB) error {
	watermark, getWatermarkError := getMetaSetting(dbConn, IngestSchemaWatermarkKey)
	if getWatermarkError != nil {
		return getWatermarkError
	}
	if watermark != "" {
		resetResult, resetError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0 WHERE updated_at > ?`, watermark)
		if resetError != nil {
			return fmt.Errorf("reset downgraded journal offsets: %w", resetError)
		}
		rowsAffected, rowsError := resetResult.RowsAffected()
		if rowsError != nil {
			return fmt.Errorf("read healed journal count: %w", rowsError)
		}
		if rowsAffected > 0 {
			if dedupError := migrateSellOrganicUIDsV8(dbConn); dedupError != nil {
				return dedupError
			}
		}
	}
	if unarmError := migrateUnarmExpeditionsV9(dbConn); unarmError != nil {
		return unarmError
	}

	var latestJournalUpdatedAt string
	if maxQueryError := dbConn.QueryRow(`SELECT COALESCE(MAX(updated_at), '') FROM ingested_journals`).Scan(&latestJournalUpdatedAt); maxQueryError != nil {
		return maxQueryError
	}
	currentWatermark := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if latestJournalUpdatedAt > currentWatermark {
		currentWatermark = latestJournalUpdatedAt
	}
	if setWatermarkError := setMetaSetting(dbConn, IngestSchemaWatermarkKey, currentWatermark); setWatermarkError != nil {
		return setWatermarkError
	}
	return setMetaSetting(dbConn, LastAppVersionKey, version.Version)
}
