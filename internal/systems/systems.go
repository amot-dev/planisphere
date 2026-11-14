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

// ErrorNotFound is returned when no source knows the system.
var ErrorNotFound = errors.New("system not found")

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
func NewResolver(dbConn *sql.DB, online func() bool) *Resolver {
	return &Resolver{
		DB:         dbConn,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		EDSMBase:   "https://www.edsm.net",
		Online:     online,
		minSpacing: 500 * time.Millisecond,
	}
}

func key(name string) string { return strings.ToLower(strings.Join(strings.Fields(name), " ")) }

// FromJournal finds the system's coordinates in the local archive.
func FromJournal(queryer db.Queryer, name string) (*Info, error) {
	var addressVal sql.NullInt64
	var starX, starY, starZ float64
	var canonical string
	queryError := queryer.QueryRow(`SELECT system_name, system_address, star_x, star_y, star_z FROM journal_events
		WHERE system_name = ? COLLATE NOCASE AND star_x IS NOT NULL ORDER BY timestamp DESC LIMIT 1`, name).
		Scan(&canonical, &addressVal, &starX, &starY, &starZ)
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, queryError
	}
	return &Info{Name: canonical, SystemAddress: addressVal.Int64, Pos: &ed.Vec3{X: starX, Y: starY, Z: starZ}, Source: "journal"}, nil
}

func (resolver *Resolver) fromCache(name string) (*Info, error) {
	var systemInfo Info
	var addressVal, edsmVal sql.NullInt64
	var xVal, yVal, zVal sql.NullFloat64
	queryError := resolver.DB.QueryRow(`SELECT system_name, system_address, edsm_id, x, y, z, source, fetched_at FROM system_cache WHERE name_key = ?`, key(name)).
		Scan(&systemInfo.Name, &addressVal, &edsmVal, &xVal, &yVal, &zVal, &systemInfo.Source, new(string))
	if errors.Is(queryError, sql.ErrNoRows) {
		return nil, nil
	}
	if queryError != nil {
		return nil, queryError
	}
	systemInfo.SystemAddress, systemInfo.EDSMID = addressVal.Int64, edsmVal.Int64
	if xVal.Valid {
		systemInfo.Pos = &ed.Vec3{X: xVal.Float64, Y: yVal.Float64, Z: zVal.Float64}
	}
	return &systemInfo, nil
}

func (resolver *Resolver) store(name string, systemInfo *Info) {
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
	_, _ = resolver.DB.Exec(`INSERT INTO system_cache(name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(name_key) DO UPDATE SET system_name=excluded.system_name,
		system_address=excluded.system_address, edsm_id=excluded.edsm_id, x=excluded.x, y=excluded.y, z=excluded.z,
		source=excluded.source, fetched_at=excluded.fetched_at`,
		key(name), systemInfo.Name, nullableSystemAddress, nullableEDSMID, xCoord, yCoord, zCoord, systemInfo.Source, ed.NowISO())
}

// Resolve looks a system up (journal -> cache -> EDSM).
func (resolver *Resolver) Resolve(ctx context.Context, name string) (*Info, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return nil, ErrorNotFound
	}
	if journalInfo, journalError := FromJournal(resolver.DB, name); journalError != nil || journalInfo != nil {
		if journalInfo != nil {
			// Enrich with a cached EDSM id when available.
			if cachedInfo, _ := resolver.fromCache(name); cachedInfo != nil && cachedInfo.EDSMID != 0 {
				journalInfo.EDSMID = cachedInfo.EDSMID
			}
		}
		return journalInfo, journalError
	}
	if cachedInfo, cacheError := resolver.fromCache(name); cacheError != nil {
		return nil, cacheError
	} else if cachedInfo != nil && cachedInfo.Pos != nil {
		return cachedInfo, nil
	}
	if resolver.Online != nil && !resolver.Online() {
		return nil, fmt.Errorf("%w (online lookups disabled)", ErrorNotFound)
	}
	edsmInfo, edsmError := resolver.fetchEDSM(ctx, name)
	if edsmError != nil {
		return nil, edsmError
	}
	resolver.store(name, edsmInfo)
	if edsmInfo.Pos == nil {
		return edsmInfo, ErrorNotFound
	}
	return edsmInfo, nil
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

func (resolver *Resolver) fetchEDSM(ctx context.Context, name string) (*Info, error) {
	// EDSM's public API enforces rate limits (~2 requests/sec). We space consecutive requests
	// by minSpacing (500ms) to prevent HTTP 429 throttling or temporary IP bans.
	resolver.mu.Lock()
	wait := resolver.minSpacing - time.Since(resolver.lastCall)
	if wait < 0 {
		wait = 0
	}
	resolver.lastCall = time.Now().Add(wait)
	resolver.mu.Unlock()

	if wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	requestURL := fmt.Sprintf("%s/api-v1/system?systemName=%s&showCoordinates=1&showId=1", resolver.EDSMBase, url.QueryEscape(name))
	httpRequest, requestError := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if requestError != nil {
		return nil, requestError
	}
	httpRequest.Header.Set("User-Agent", version.UserAgent)
	httpResponse, httpError := resolver.HTTP.Do(httpRequest)
	if httpError != nil {
		return nil, fmt.Errorf("EDSM request failed: %w", httpError)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("EDSM returned HTTP %d", httpResponse.StatusCode)
	}
	var rawPayload json.RawMessage
	if decodeError := json.NewDecoder(httpResponse.Body).Decode(&rawPayload); decodeError != nil {
		return nil, fmt.Errorf("EDSM decode: %w", decodeError)
	}
	// Unknown systems are returned as an empty array "[]".
	if trimmedPayload := strings.TrimSpace(string(rawPayload)); trimmedPayload == "[]" || trimmedPayload == "{}" {
		return &Info{Name: name, Source: "notfound"}, nil
	}
	var parsedSystem edsmSystem
	if unmarshalError := json.Unmarshal(rawPayload, &parsedSystem); unmarshalError != nil {
		return nil, fmt.Errorf("EDSM decode: %w", unmarshalError)
	}
	resolvedInfo := &Info{Name: html.UnescapeString(parsedSystem.Name), SystemAddress: parsedSystem.ID64, EDSMID: parsedSystem.ID, Source: "edsm"}
	if resolvedInfo.Name == "" {
		resolvedInfo.Name = name
	}
	if parsedSystem.Coords != nil {
		resolvedInfo.Pos = &ed.Vec3{X: parsedSystem.Coords.X, Y: parsedSystem.Coords.Y, Z: parsedSystem.Coords.Z}
	} else {
		resolvedInfo.Source = "notfound"
	}
	return resolvedInfo, nil
}
