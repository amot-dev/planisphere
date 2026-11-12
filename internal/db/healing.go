package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"strconv"
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
	watermark, getWatermarkError := GetSetting(dbConn, IngestSchemaWatermarkKey)
	if getWatermarkError != nil {
		return getWatermarkError
	}
	if watermark != "" {
		var downgradedJournalCount int
		if countError := dbConn.QueryRow(`SELECT COUNT(*) FROM ingested_journals WHERE updated_at > ?`, watermark).Scan(&downgradedJournalCount); countError != nil {
			return countError
		}
		if downgradedJournalCount > 0 {
			if _, resetError := dbConn.Exec(`UPDATE ingested_journals SET byte_offset = 0, line_count = 0 WHERE updated_at > ?`, watermark); resetError != nil {
				return resetError
			}
			if dedupError := migrateSellOrganicUIDsV8(dbConn); dedupError != nil {
				return dedupError
			}
		}
	}

	var latestJournalUpdatedAt string
	if maxQueryError := dbConn.QueryRow(`SELECT COALESCE(MAX(updated_at), '') FROM ingested_journals`).Scan(&latestJournalUpdatedAt); maxQueryError != nil {
		return maxQueryError
	}
	currentWatermark := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if latestJournalUpdatedAt > currentWatermark {
		currentWatermark = latestJournalUpdatedAt
	}
	if setWatermarkError := SetSetting(dbConn, IngestSchemaWatermarkKey, currentWatermark); setWatermarkError != nil {
		return setWatermarkError
	}
	return SetSetting(dbConn, LastAppVersionKey, version.Version)
}

// computeSellOrganicCanonicalUID computes the canonical SHA-256 event UID for a
// SellOrganicData journal row. This calculation mirrors journal.ComputeUID and
// keyIdentifiers for SellOrganicData in internal/journal/events.go (joining the
// timestamp, event name, MarketID, BioData item count, total value, and total
// bonus with null-byte \x00 delimiters) without importing internal/journal to
// avoid a package import cycle.
func computeSellOrganicCanonicalUID(hasher hash.Hash, timestamp string, rawJSON string) (string, bool) {
	var payload struct {
		MarketID *int64 `json:"MarketID"`
		BioData  []struct {
			Value int64 `json:"Value"`
			Bonus int64 `json:"Bonus"`
		} `json:"BioData"`
	}
	if unmarshalError := json.Unmarshal([]byte(rawJSON), &payload); unmarshalError != nil {
		return "", false
	}
	var totalValue, totalBonus int64
	for _, bioItem := range payload.BioData {
		totalValue += bioItem.Value
		totalBonus += bioItem.Bonus
	}
	marketIDString := ""
	if payload.MarketID != nil {
		marketIDString = strconv.FormatInt(*payload.MarketID, 10)
	}
	hasher.Reset()
	_, _ = io.WriteString(hasher, timestamp)
	_, _ = io.WriteString(hasher, "\x00SellOrganicData")
	for _, keyPart := range []string{
		marketIDString,
		strconv.Itoa(len(payload.BioData)),
		strconv.FormatInt(totalValue, 10),
		strconv.FormatInt(totalBonus, 10),
	} {
		_, _ = io.WriteString(hasher, "\x00")
		_, _ = io.WriteString(hasher, keyPart)
	}
	var digestBuffer [sha256.Size]byte
	return hex.EncodeToString(hasher.Sum(digestBuffer[:0])), true
}

func migrateSellOrganicUIDsV8(dbConn *sql.DB) error {
	rows, queryError := dbConn.Query(`SELECT id, event_uid, timestamp, raw_json FROM journal_events WHERE event = 'SellOrganicData' ORDER BY id ASC`)
	if queryError != nil {
		return queryError
	}
	defer rows.Close()

	type pendingUIDUpdate struct {
		id           int64
		canonicalUID string
	}
	seenUIDs := make(map[string]bool)
	var duplicateRowIDs []int64
	var pendingUpdates []pendingUIDUpdate
	hasher := sha256.New()

	for rows.Next() {
		var row struct {
			id        int64
			eventUID  string
			timestamp string
			rawJSON   string
		}
		if scanError := rows.Scan(&row.id, &row.eventUID, &row.timestamp, &row.rawJSON); scanError != nil {
			return scanError
		}
		canonicalUID, validPayload := computeSellOrganicCanonicalUID(hasher, row.timestamp, row.rawJSON)
		if !validPayload {
			continue
		}
		if seenUIDs[canonicalUID] {
			duplicateRowIDs = append(duplicateRowIDs, row.id)
			continue
		}
		seenUIDs[canonicalUID] = true
		if row.eventUID != canonicalUID {
			pendingUpdates = append(pendingUpdates, pendingUIDUpdate{id: row.id, canonicalUID: canonicalUID})
		}
	}
	if iterationError := rows.Err(); iterationError != nil {
		return iterationError
	}
	if closeError := rows.Close(); closeError != nil {
		return closeError
	}

	return WithTx(dbConn, func(transaction *sql.Tx) error {
		// Delete duplicate rows first so any duplicate that already holds the
		// canonical UID is removed before updating the surviving older row.
		for _, duplicateID := range duplicateRowIDs {
			if _, deleteError := transaction.Exec(`DELETE FROM journal_events WHERE id = ?`, duplicateID); deleteError != nil {
				return deleteError
			}
		}
		for _, pendingUpdate := range pendingUpdates {
			if _, updateError := transaction.Exec(`UPDATE journal_events SET event_uid = ? WHERE id = ?`, pendingUpdate.canonicalUID, pendingUpdate.id); updateError != nil {
				return updateError
			}
		}
		return nil
	})
}

