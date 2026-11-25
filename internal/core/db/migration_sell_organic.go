package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"strconv"
)

// computeSellOrganicCanonicalUID computes the canonical SHA-256 event UID for a
// SellOrganicData journal row. This calculation mirrors journal.ComputeUID and
// keyIdentifiers for SellOrganicData in internal/sources/journal/parser.go (joining the
// timestamp, event name, MarketID, BioData item count, total value, and total
// bonus with null-byte \x00 delimiters) without importing internal/sources/journal to
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

type pendingSellOrganicUIDUpdate struct {
	id           int64
	canonicalUID string
}

func migrateSellOrganicUIDsV8(dbConn *sql.DB) error {
	rows, queryError := dbConn.Query(`SELECT id, event_uid, timestamp, raw_json FROM journal_events WHERE event = 'SellOrganicData' ORDER BY id ASC`)
	if queryError != nil {
		return queryError
	}
	defer rows.Close()

	seenUIDs := make(map[string]bool)
	var duplicateRowIDs []int64
	var pendingUpdates []pendingSellOrganicUIDUpdate
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
			pendingUpdates = append(pendingUpdates, pendingSellOrganicUIDUpdate{id: row.id, canonicalUID: canonicalUID})
		}
	}
	if iterationError := rows.Err(); iterationError != nil {
		return iterationError
	}
	if closeError := rows.Close(); closeError != nil {
		return closeError
	}
	if len(duplicateRowIDs) == 0 && len(pendingUpdates) == 0 {
		return nil
	}
	return applySellOrganicUIDFixes(dbConn, duplicateRowIDs, pendingUpdates)
}

func applySellOrganicUIDFixes(dbConn *sql.DB, duplicateRowIDs []int64, pendingUpdates []pendingSellOrganicUIDUpdate) error {
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
