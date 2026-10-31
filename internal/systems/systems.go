// Package systems resolves star system coordinates, first from the local
// journal archive, then from a shared cache, then from the EDSM API.
package systems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/version"
)

// Info is a resolved system.
type Info struct {
	Name          string   `json:"name"`
	SystemAddress int64    `json:"systemAddress"`
	EDSMID        int64    `json:"edsmId"`
	Pos           *ed.Vec3 `json:"pos"`
	Source        string   `json:"source"` // journal | edsm | notfound
}

// ErrNotFound is returned when no source knows the system.
var ErrNotFound = errors.New("system not found")

// Resolver looks up systems.
type Resolver struct {
	DB         *sql.DB
	HTTP       *http.Client
	EDSMBase   string
	Online     func() bool // whether online lookups are allowed
	mu         sync.Mutex
	lastCall   time.Time
	minSpacing time.Duration
}

// NewResolver creates a resolver using the public EDSM API.
func NewResolver(conn *sql.DB, online func() bool) *Resolver {
	return &Resolver{
		DB:         conn,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		EDSMBase:   "https://www.edsm.net",
		Online:     online,
		minSpacing: 500 * time.Millisecond,
	}
}

func key(name string) string { return strings.ToLower(strings.Join(strings.Fields(name), " ")) }

// FromJournal finds the system's coordinates in the local archive.
func FromJournal(q db.Queryer, name string) (*Info, error) {
	var addr sql.NullInt64
	var x, y, z float64
	var canonical string
	err := q.QueryRow(`SELECT system_name, system_address, star_x, star_y, star_z FROM journal_events
		WHERE system_name = ? COLLATE NOCASE AND star_x IS NOT NULL ORDER BY timestamp DESC LIMIT 1`, name).
		Scan(&canonical, &addr, &x, &y, &z)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Info{Name: canonical, SystemAddress: addr.Int64, Pos: &ed.Vec3{X: x, Y: y, Z: z}, Source: "journal"}, nil
}

func (r *Resolver) fromCache(name string) (*Info, error) {
	var in Info
	var addr, edsm sql.NullInt64
	var x, y, z sql.NullFloat64
	err := r.DB.QueryRow(`SELECT system_name, system_address, edsm_id, x, y, z, source, fetched_at FROM system_cache WHERE name_key = ?`, key(name)).
		Scan(&in.Name, &addr, &edsm, &x, &y, &z, &in.Source, new(string))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	in.SystemAddress, in.EDSMID = addr.Int64, edsm.Int64
	if x.Valid {
		in.Pos = &ed.Vec3{X: x.Float64, Y: y.Float64, Z: z.Float64}
	}
	return &in, nil
}

func (r *Resolver) store(name string, in *Info) {
	var x, y, z, addr, edsm any
	if in.Pos != nil {
		x, y, z = in.Pos.X, in.Pos.Y, in.Pos.Z
	}
	if in.SystemAddress != 0 {
		addr = in.SystemAddress
	}
	if in.EDSMID != 0 {
		edsm = in.EDSMID
	}
	_, _ = r.DB.Exec(`INSERT INTO system_cache(name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(name_key) DO UPDATE SET system_name=excluded.system_name,
		system_address=excluded.system_address, edsm_id=excluded.edsm_id, x=excluded.x, y=excluded.y, z=excluded.z,
		source=excluded.source, fetched_at=excluded.fetched_at`,
		key(name), in.Name, addr, edsm, x, y, z, in.Source, ed.NowISO())
}

// Resolve looks a system up (journal -> cache -> EDSM).
func (r *Resolver) Resolve(ctx context.Context, name string) (*Info, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return nil, ErrNotFound
	}
	if in, err := FromJournal(r.DB, name); err != nil || in != nil {
		if in != nil {
			// Enrich with a cached EDSM id when available.
			if c, _ := r.fromCache(name); c != nil && c.EDSMID != 0 {
				in.EDSMID = c.EDSMID
			}
		}
		return in, err
	}
	if c, err := r.fromCache(name); err != nil {
		return nil, err
	} else if c != nil && c.Pos != nil {
		return c, nil
	}
	if r.Online != nil && !r.Online() {
		return nil, fmt.Errorf("%w (online lookups disabled)", ErrNotFound)
	}
	in, err := r.fetchEDSM(ctx, name)
	if err != nil {
		return nil, err
	}
	r.store(name, in)
	if in.Pos == nil {
		return in, ErrNotFound
	}
	return in, nil
}

type edsmSystem struct {
	Name   string `json:"name"`
	ID     int64  `json:"id"`
	ID64   int64  `json:"id64"`
	Coords *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"coords"`
}

func (r *Resolver) fetchEDSM(ctx context.Context, name string) (*Info, error) {
	// EDSM's public API enforces rate limits (~2 requests/sec). We space consecutive requests
	// by minSpacing (500ms) to prevent HTTP 429 throttling or temporary IP bans.
	r.mu.Lock()
	wait := r.minSpacing - time.Since(r.lastCall)
	if wait < 0 {
		wait = 0
	}
	r.lastCall = time.Now().Add(wait)
	r.mu.Unlock()

	if wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	u := fmt.Sprintf("%s/api-v1/system?systemName=%s&showCoordinates=1&showId=1", r.EDSMBase, url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", version.UserAgent)
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("EDSM request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("EDSM returned HTTP %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("EDSM decode: %w", err)
	}
	// Unknown systems are returned as an empty array "[]".
	if t := strings.TrimSpace(string(raw)); t == "[]" || t == "{}" {
		return &Info{Name: name, Source: "notfound"}, nil
	}
	var s edsmSystem
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("EDSM decode: %w", err)
	}
	in := &Info{Name: html.UnescapeString(s.Name), SystemAddress: s.ID64, EDSMID: s.ID, Source: "edsm"}
	if in.Name == "" {
		in.Name = name
	}
	if s.Coords != nil {
		in.Pos = &ed.Vec3{X: s.Coords.X, Y: s.Coords.Y, Z: s.Coords.Z}
	} else {
		in.Source = "notfound"
	}
	return in, nil
}
