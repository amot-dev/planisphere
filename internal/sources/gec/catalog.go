package gec

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
)

// POI is a cached GEC/GMP entry.
type POI struct {
	ID            int64
	Name          string
	SystemName    string
	SystemAddress int64
	Type          string
	URL           string
	Source        string
}

// Catalog is an in-memory index over gec_pois_cache.
type Catalog struct {
	dbConn *sql.DB
	mu     sync.RWMutex
	byName map[string][]POI
	byAddr map[int64][]POI
}

// NewCatalog creates a catalog and loads the cache.
func NewCatalog(dbConn *sql.DB) *Catalog {
	catalog := &Catalog{dbConn: dbConn}
	_ = catalog.Reload()
	return catalog
}

// Reload rebuilds the in-memory index from the database.
func (catalog *Catalog) Reload() error {
	rows, queryError := catalog.dbConn.Query(`SELECT id, name, system_name, COALESCE(system_address,0), poi_type, poi_url, COALESCE(source, 'GEC')
		FROM gec_pois_cache
		ORDER BY CASE WHEN UPPER(source) = 'GEC' THEN 0 ELSE 1 END, id`)
	if queryError != nil {
		return queryError
	}
	defer rows.Close()
	byName := map[string][]POI{}
	byAddr := map[int64][]POI{}
	for rows.Next() {
		var poiItem POI
		if scanError := rows.Scan(&poiItem.ID, &poiItem.Name, &poiItem.SystemName, &poiItem.SystemAddress, &poiItem.Type, &poiItem.URL, &poiItem.Source); scanError != nil {
			return scanError
		}
		poiItem.Name = html.UnescapeString(poiItem.Name)
		poiItem.SystemName = html.UnescapeString(poiItem.SystemName)
		poiItem.Type = html.UnescapeString(poiItem.Type)
		nameKey := strings.ToLower(poiItem.SystemName)
		byName[nameKey] = append(byName[nameKey], poiItem)
		if poiItem.SystemAddress != 0 {
			byAddr[poiItem.SystemAddress] = append(byAddr[poiItem.SystemAddress], poiItem)
		}
	}
	if rowsError := rows.Err(); rowsError != nil {
		return rowsError
	}
	catalog.mu.Lock()
	catalog.byName, catalog.byAddr = byName, byAddr
	catalog.mu.Unlock()
	return nil
}

// Count returns the number of cached POIs.
func (catalog *Catalog) Count() int {
	var poiCount int
	_ = catalog.dbConn.QueryRow(`SELECT COUNT(*) FROM gec_pois_cache`).Scan(&poiCount)
	return poiCount
}

// Lookup returns POIs in a system (by id64 first, then by name).
func (catalog *Catalog) Lookup(systemName string, systemAddress int64) []POI {
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	if systemAddress != 0 {
		if poisForAddress := catalog.byAddr[systemAddress]; len(poisForAddress) > 0 {
			return poisForAddress
		}
	}
	return catalog.byName[strings.ToLower(strings.TrimSpace(systemName))]
}

// Import replaces the cache with a combined-catalogue JSON document.
func (catalog *Catalog) Import(body []byte) (int, error) {
	var entries []remotePOI
	if decodeError := json.Unmarshal(body, &entries); decodeError != nil {
		return 0, fmt.Errorf("GEC decode: %w", decodeError)
	}
	now := reference.NowISO()
	count := 0
	txError := db.WithTx(catalog.dbConn, func(transaction *sql.Tx) error {
		if _, deleteError := transaction.Exec(`DELETE FROM gec_pois_cache`); deleteError != nil {
			return deleteError
		}
		stmt, prepError := transaction.Prepare(`INSERT OR REPLACE INTO gec_pois_cache
			(id, name, system_name, system_address, poi_type, region, summary, poi_url, x, y, z, raw_json, source, fetched_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if prepError != nil {
			return prepError
		}
		defer stmt.Close()
		for _, remote := range entries {
			if remote.ID == 0 || strings.TrimSpace(remote.GalMapSearch) == "" {
				continue
			}
			sourceName := strings.ToUpper(strings.TrimSpace(remote.Source))
			if sourceName == "" {
				sourceName = "GEC"
			}
			// Avoid primary key collisions between GEC and GMP datasets
			rowID := remote.ID
			if sourceName != "GEC" {
				rowID = 1_000_000_000 + remote.ID
			}

			// Keep the cache lean: remotePOI only defines metadata fields,
			// automatically ignoring long-form descriptions without allocating them.
			slimJSON, _ := json.Marshal(remote)
			var xCoord, yCoord, zCoord any
			if len(remote.Coordinates) == 3 {
				xCoord, yCoord, zCoord = remote.Coordinates[0], remote.Coordinates[1], remote.Coordinates[2]
			}
			targetURL := remote.PoiURL
			if targetURL == "" && sourceName == "GEC" {
				targetURL = fmt.Sprintf("https://edastro.com/gec/view/%d", remote.ID)
			}
			poiName := html.UnescapeString(strings.TrimSpace(remote.Name))
			systemName := html.UnescapeString(strings.TrimSpace(remote.GalMapSearch))
			poiType := html.UnescapeString(strings.TrimSpace(remote.Type))
			region := html.UnescapeString(strings.TrimSpace(remote.Region))
			summary := html.UnescapeString(strings.TrimSpace(remote.Summary))
			if _, insertError := stmt.Exec(rowID, poiName, systemName, remote.ID64, poiType, region, summary,
				targetURL, xCoord, yCoord, zCoord, string(slimJSON), sourceName, now); insertError != nil {
				return insertError
			}
			count++
		}
		return nil
	})
	return count, txError
}

