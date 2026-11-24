package replay

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

const systemQueryChunkSize = 500

// JournalSystemInfo holds canonical star-system naming, address, and galactic
// coordinates resolved from persisted journal_events for enabled commanders.
type JournalSystemInfo struct {
	Name          string
	SystemAddress int64
	Pos           reference.Vec3
}

type rankedJournalSystem struct {
	system    JournalSystemInfo
	timestamp string
	rowID     int64
}

func isNilQueryer(queryer db.Queryer) bool {
	switch concreteQueryer := queryer.(type) {
	case nil:
		return true
	case *sql.DB:
		return concreteQueryer == nil
	case *sql.Tx:
		return concreteQueryer == nil
	default:
		return false
	}
}

// QueryJournalSystemCoords returns the most recent persisted coordinates and
// canonical capitalization for systemName across enabled commanders, or nil when
// no matching coordinate-bearing event exists.
func QueryJournalSystemCoords(queryer db.Queryer, systemName string) (*JournalSystemInfo, error) {
	normalizedName := strings.Join(strings.Fields(systemName), " ")
	if normalizedName == "" || isNilQueryer(queryer) {
		return nil, nil
	}
	var resolvedSystem JournalSystemInfo
	queryError := queryer.QueryRow(
		`SELECT system_name, COALESCE(system_address, 0), star_x, star_y, star_z
		 FROM journal_events
		 WHERE system_name = ? COLLATE NOCASE
		   AND star_x IS NOT NULL AND star_y IS NOT NULL AND star_z IS NOT NULL
		   AND `+CmdrFilterSQL+`
		 ORDER BY timestamp DESC, id DESC LIMIT 1`,
		normalizedName,
	).Scan(&resolvedSystem.Name, &resolvedSystem.SystemAddress, &resolvedSystem.Pos.X, &resolvedSystem.Pos.Y, &resolvedSystem.Pos.Z)
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, fmt.Errorf("query journal system coordinates: %w", queryError)
	}
	return &resolvedSystem, nil
}

// QueryJournalSystemCoordsBatch resolves the latest persisted coordinates and
// canonical capitalization for systemNames across enabled commanders, keyed by
// lowercased whitespace-normalized system name.
func QueryJournalSystemCoordsBatch(queryer db.Queryer, systemNames []string) (map[string]JournalSystemInfo, error) {
	if isNilQueryer(queryer) || len(systemNames) == 0 {
		return map[string]JournalSystemInfo{}, nil
	}
	uniqueNames := make([]string, 0, len(systemNames))
	seenKeys := make(map[string]struct{}, len(systemNames))
	for _, rawSystemName := range systemNames {
		normalizedName := strings.Join(strings.Fields(rawSystemName), " ")
		if normalizedName == "" {
			continue
		}
		systemKey := strings.ToLower(normalizedName)
		if _, exists := seenKeys[systemKey]; !exists {
			seenKeys[systemKey] = struct{}{}
			uniqueNames = append(uniqueNames, normalizedName)
		}
	}
	if len(uniqueNames) == 0 {
		return map[string]JournalSystemInfo{}, nil
	}

	rankedByKey := make(map[string]rankedJournalSystem, len(uniqueNames))
	for chunkStart := 0; chunkStart < len(uniqueNames); chunkStart += systemQueryChunkSize {
		chunkEnd := min(chunkStart+systemQueryChunkSize, len(uniqueNames))
		if chunkError := queryJournalSystemCoordsChunk(queryer, uniqueNames[chunkStart:chunkEnd], rankedByKey); chunkError != nil {
			return nil, chunkError
		}
	}

	resolvedByKey := make(map[string]JournalSystemInfo, len(rankedByKey))
	for systemKey, rankedEntry := range rankedByKey {
		resolvedByKey[systemKey] = rankedEntry.system
	}
	return resolvedByKey, nil
}

func queryJournalSystemCoordsChunk(queryer db.Queryer, nameChunk []string, rankedByKey map[string]rankedJournalSystem) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(nameChunk)), ",")
	queryArgs := make([]any, len(nameChunk))
	for index, systemName := range nameChunk {
		queryArgs[index] = systemName
	}
	query := `SELECT system_name, COALESCE(system_address, 0), star_x, star_y, star_z, timestamp, id
		FROM journal_events
		WHERE system_name COLLATE NOCASE IN (` + placeholders + `)
		  AND star_x IS NOT NULL AND star_y IS NOT NULL AND star_z IS NOT NULL
		  AND ` + CmdrFilterSQL
	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return fmt.Errorf("query journal system coordinates batch: %w", queryError)
	}
	defer databaseRows.Close()

	for databaseRows.Next() {
		var candidate rankedJournalSystem
		if scanError := databaseRows.Scan(
			&candidate.system.Name, &candidate.system.SystemAddress,
			&candidate.system.Pos.X, &candidate.system.Pos.Y, &candidate.system.Pos.Z,
			&candidate.timestamp, &candidate.rowID,
		); scanError != nil {
			return fmt.Errorf("scan journal system coordinates row: %w", scanError)
		}
		systemKey := NormalizeSystemKey(candidate.system.Name)
		existing, found := rankedByKey[systemKey]
		if !found || candidate.timestamp > existing.timestamp || (candidate.timestamp == existing.timestamp && candidate.rowID > existing.rowID) {
			rankedByKey[systemKey] = candidate
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return fmt.Errorf("iterate journal system coordinates batch: %w", iterationError)
	}
	return nil
}

// QueryCanonicalSystemName returns the latest persisted capitalization of
// systemName in journal_events across enabled commanders, or "" when unknown.
func QueryCanonicalSystemName(queryer db.Queryer, systemName string) (string, error) {
	normalizedName := strings.Join(strings.Fields(systemName), " ")
	if normalizedName == "" || isNilQueryer(queryer) {
		return "", nil
	}
	var canonicalName string
	queryError := queryer.QueryRow(
		`SELECT system_name
		 FROM journal_events
		 WHERE system_name = ? COLLATE NOCASE AND system_name != '' AND `+CmdrFilterSQL+`
		 ORDER BY timestamp DESC, id DESC LIMIT 1`,
		normalizedName,
	).Scan(&canonicalName)
	if errors.Is(queryError, sql.ErrNoRows) {
		return "", nil
	}
	if queryError != nil {
		return "", fmt.Errorf("query canonical journal system name: %w", queryError)
	}
	return canonicalName, nil
}
