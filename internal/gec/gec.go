// Package gec caches the EDAstro Galactic Exploration Catalog (GEC) so
// waypoints can link to curated POI pages offline.
package gec

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/version"
)

// CombinedURL is the full GEC catalogue.
const CombinedURL = "https://edastro.com/gec/json/combined"

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

type remotePOI struct {
	ID           int64     `json:"id"`
	Source       string    `json:"source"`
	Type         string    `json:"type"`
	Name         string    `json:"name"`
	Region       string    `json:"region"`
	GalMapSearch string    `json:"galMapSearch"`
	GalMapURL    string    `json:"galMapUrl"`
	Coordinates  []float64 `json:"coordinates"`
	Summary      string    `json:"summary"`
	ID64         *int64    `json:"id64"`
	PoiURL       string    `json:"poiUrl"`
}

// Catalog is an in-memory index over gec_pois_cache.
type Catalog struct {
	conn   *sql.DB
	mu     sync.RWMutex
	byName map[string][]POI
	byAddr map[int64][]POI
}

// NewCatalog creates a catalog and loads the cache.
func NewCatalog(conn *sql.DB) *Catalog {
	c := &Catalog{conn: conn}
	_ = c.Reload()
	return c
}

// Reload rebuilds the in-memory index from the database.
func (c *Catalog) Reload() error {
	rs, err := c.conn.Query(`SELECT id, name, system_name, COALESCE(system_address,0), poi_type, poi_url, COALESCE(source, 'GEC')
		FROM gec_pois_cache
		ORDER BY CASE WHEN UPPER(source) = 'GEC' THEN 0 ELSE 1 END, id`)
	if err != nil {
		return err
	}
	defer rs.Close()
	byName := map[string][]POI{}
	byAddr := map[int64][]POI{}
	for rs.Next() {
		var p POI
		if err := rs.Scan(&p.ID, &p.Name, &p.SystemName, &p.SystemAddress, &p.Type, &p.URL, &p.Source); err != nil {
			return err
		}
		p.Name = html.UnescapeString(p.Name)
		p.SystemName = html.UnescapeString(p.SystemName)
		p.Type = html.UnescapeString(p.Type)
		key := strings.ToLower(p.SystemName)
		byName[key] = append(byName[key], p)
		if p.SystemAddress != 0 {
			byAddr[p.SystemAddress] = append(byAddr[p.SystemAddress], p)
		}
	}
	c.mu.Lock()
	c.byName, c.byAddr = byName, byAddr
	c.mu.Unlock()
	return rs.Err()
}

// Count returns the number of cached POIs.
func (c *Catalog) Count() int {
	var n int
	_ = c.conn.QueryRow(`SELECT COUNT(*) FROM gec_pois_cache`).Scan(&n)
	return n
}

// LastFetched returns when the cache was last refreshed ("" if never).
func (c *Catalog) LastFetched() string {
	v, _ := db.GetSetting(c.conn, "gec_fetched_at")
	return v
}

// Lookup returns POIs in a system (by id64 first, then by name).
func (c *Catalog) Lookup(systemName string, systemAddress int64) []POI {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if systemAddress != 0 {
		if ps := c.byAddr[systemAddress]; len(ps) > 0 {
			return ps
		}
	}
	return c.byName[strings.ToLower(strings.TrimSpace(systemName))]
}

// Refresh downloads the combined catalogue and replaces the cache.
func (c *Catalog) Refresh(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CombinedURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", version.UserAgent)
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GEC download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GEC download: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return 0, err
	}
	n, err := c.Import(body)
	if err != nil {
		return 0, err
	}
	return n, c.Reload()
}

// Import replaces the cache with a combined-catalogue JSON document.
func (c *Catalog) Import(body []byte) (int, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return 0, fmt.Errorf("GEC decode: %w", err)
	}
	now := ed.NowISO()
	count := 0
	err := db.WithTx(c.conn, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM gec_pois_cache`); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`INSERT OR REPLACE INTO gec_pois_cache
			(id, name, system_name, system_address, poi_type, region, summary, poi_url, x, y, z, raw_json, source, fetched_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, raw := range entries {
			var p remotePOI
			if json.Unmarshal(raw, &p) != nil || p.ID == 0 || strings.TrimSpace(p.GalMapSearch) == "" {
				continue
			}
			src := strings.ToUpper(strings.TrimSpace(p.Source))
			if src == "" {
				src = "GEC"
			}
			// Avoid primary key collisions between GEC and GMP datasets
			rowID := p.ID
			if src != "GEC" {
				rowID = 1_000_000_000 + p.ID
			}

			// Keep the cache lean: drop the long-form descriptions.
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			delete(m, "descriptionMardown")
			delete(m, "descriptionHtml")
			slim, _ := json.Marshal(m)
			var x, y, z any
			if len(p.Coordinates) == 3 {
				x, y, z = p.Coordinates[0], p.Coordinates[1], p.Coordinates[2]
			}
			url := p.PoiURL
			if url == "" && src == "GEC" {
				url = fmt.Sprintf("https://edastro.com/gec/view/%d", p.ID)
			}
			name := html.UnescapeString(strings.TrimSpace(p.Name))
			sysName := html.UnescapeString(strings.TrimSpace(p.GalMapSearch))
			pType := html.UnescapeString(strings.TrimSpace(p.Type))
			region := html.UnescapeString(strings.TrimSpace(p.Region))
			summary := html.UnescapeString(strings.TrimSpace(p.Summary))
			if _, err := stmt.Exec(rowID, name, sysName, p.ID64, pType, region, summary,
				url, x, y, z, string(slim), src, now); err != nil {
				return err
			}
			count++
		}
		return db.SetSetting(tx, "gec_fetched_at", now)
	})
	return count, err
}

