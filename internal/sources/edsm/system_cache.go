package edsm

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

const systemCacheChunkSize = 500

func normalizeSystemCacheKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func deduplicateSystemNames(names []string) ([]string, []string) {
	uniqueNames := make([]string, 0, len(names))
	uniqueKeys := make([]string, 0, len(names))
	seenKeys := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		normalizedName := strings.Join(strings.Fields(rawName), " ")
		if normalizedName == "" {
			continue
		}
		cacheKey := strings.ToLower(normalizedName)
		if _, exists := seenKeys[cacheKey]; !exists {
			seenKeys[cacheKey] = struct{}{}
			uniqueNames = append(uniqueNames, normalizedName)
			uniqueKeys = append(uniqueKeys, cacheKey)
		}
	}
	return uniqueNames, uniqueKeys
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

// CanonicalSystemName resolves the canonical capitalization of name via the
// optional journalLookup callback or system_cache, returning trimmed name when unknown.
func CanonicalSystemName(queryer db.Queryer, name string, journalLookup func(string) (string, error)) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return ""
	}
	if journalLookup != nil {
		if canonicalName, lookupError := journalLookup(trimmedName); lookupError == nil && canonicalName != "" {
			return canonicalName
		}
	}
	if isNilQueryer(queryer) {
		return trimmedName
	}
	var canonicalName string
	if queryError := queryer.QueryRow(
		`SELECT system_name FROM system_cache WHERE name_key = ? LIMIT 1`,
		normalizeSystemCacheKey(trimmedName),
	).Scan(&canonicalName); queryError == nil && canonicalName != "" {
		return canonicalName
	}
	return trimmedName
}

func (resolver *Resolver) fromCache(name string) (*Info, error) {
	if resolver.DB == nil {
		return nil, nil
	}
	var systemInfo Info
	var nullableSystemAddress, nullableEDSMID sql.NullInt64
	var nullableXCoord, nullableYCoord, nullableZCoord sql.NullFloat64
	queryError := resolver.DB.QueryRow(
		`SELECT system_name, system_address, edsm_id, x, y, z, source, fetched_at FROM system_cache WHERE name_key = ?`,
		normalizeSystemCacheKey(name),
	).Scan(&systemInfo.Name, &nullableSystemAddress, &nullableEDSMID, &nullableXCoord, &nullableYCoord, &nullableZCoord, &systemInfo.Source, new(string))
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, fmt.Errorf("query system_cache for %q: %w", name, queryError)
	}
	systemInfo.SystemAddress, systemInfo.EDSMID = nullableSystemAddress.Int64, nullableEDSMID.Int64
	if nullableXCoord.Valid && nullableYCoord.Valid && nullableZCoord.Valid {
		systemInfo.Pos = &reference.Vec3{X: nullableXCoord.Float64, Y: nullableYCoord.Float64, Z: nullableZCoord.Float64}
	}
	return &systemInfo, nil
}

func (resolver *Resolver) fromCacheBatch(nameKeys []string) (map[string]*Info, error) {
	cachedByKey := make(map[string]*Info, len(nameKeys))
	if resolver.DB == nil || len(nameKeys) == 0 {
		return cachedByKey, nil
	}
	for chunkStart := 0; chunkStart < len(nameKeys); chunkStart += systemCacheChunkSize {
		chunkEnd := min(chunkStart+systemCacheChunkSize, len(nameKeys))
		if chunkError := resolver.querySystemCacheChunk(nameKeys[chunkStart:chunkEnd], cachedByKey); chunkError != nil {
			return nil, chunkError
		}
	}
	return cachedByKey, nil
}

func (resolver *Resolver) querySystemCacheChunk(keyChunk []string, cachedByKey map[string]*Info) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keyChunk)), ",")
	queryArgs := make([]any, len(keyChunk))
	for index, cacheKey := range keyChunk {
		queryArgs[index] = cacheKey
	}
	databaseRows, queryError := resolver.DB.Query(
		`SELECT name_key, system_name, system_address, edsm_id, x, y, z, source FROM system_cache WHERE name_key IN (`+placeholders+`)`,
		queryArgs...,
	)
	if queryError != nil {
		return fmt.Errorf("query system_cache batch: %w", queryError)
	}
	defer databaseRows.Close()

	for databaseRows.Next() {
		var cacheKey string
		var systemInfo Info
		var nullableSystemAddress, nullableEDSMID sql.NullInt64
		var nullableXCoord, nullableYCoord, nullableZCoord sql.NullFloat64
		if scanError := databaseRows.Scan(
			&cacheKey,
			&systemInfo.Name,
			&nullableSystemAddress,
			&nullableEDSMID,
			&nullableXCoord,
			&nullableYCoord,
			&nullableZCoord,
			&systemInfo.Source,
		); scanError != nil {
			return fmt.Errorf("scan system_cache batch row: %w", scanError)
		}
		systemInfo.SystemAddress, systemInfo.EDSMID = nullableSystemAddress.Int64, nullableEDSMID.Int64
		if nullableXCoord.Valid && nullableYCoord.Valid && nullableZCoord.Valid {
			systemInfo.Pos = &reference.Vec3{X: nullableXCoord.Float64, Y: nullableYCoord.Float64, Z: nullableZCoord.Float64}
		}
		cachedByKey[cacheKey] = &systemInfo
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return fmt.Errorf("iterate system_cache batch: %w", iterationError)
	}
	return nil
}

func (resolver *Resolver) store(name string, systemInfo *Info) error {
	if resolver.DB == nil || systemInfo == nil {
		return nil
	}
	var xCoord, yCoord, zCoord, nullableSystemAddress, nullableEDSMID any
	if systemInfo.Pos != nil {
		xCoord, yCoord, zCoord = systemInfo.Pos.X, systemInfo.Pos.Y, systemInfo.Pos.Z
	}
	if systemInfo.SystemAddress != 0 {
		nullableSystemAddress = systemInfo.SystemAddress
	}
	if systemInfo.EDSMID != 0 {
		nullableEDSMID = systemInfo.EDSMID
	}
	_, execError := resolver.DB.Exec(`INSERT INTO system_cache(name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(name_key) DO UPDATE SET system_name=excluded.system_name,
		system_address=excluded.system_address, edsm_id=excluded.edsm_id, x=excluded.x, y=excluded.y, z=excluded.z,
		source=excluded.source, fetched_at=excluded.fetched_at`,
		normalizeSystemCacheKey(name), systemInfo.Name, nullableSystemAddress, nullableEDSMID, xCoord, yCoord, zCoord, systemInfo.Source, reference.NowISO())
	if execError != nil {
		return fmt.Errorf("store system_cache entry for %q: %w", name, execError)
	}
	return nil
}
