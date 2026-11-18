// Package edsm resolves star system coordinates, first from the local
// journal archive, then from a shared cache, then from the EDSM API.
package edsm

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

	"planisphere/internal/reference"
	"planisphere/internal/version"
)

// Info is a resolved system.
type Info struct {
	Name          string          `json:"name"`
	SystemAddress int64           `json:"systemAddress"`
	EDSMID        int64           `json:"edsmId"`
	Pos           *reference.Vec3 `json:"pos"`
	Source        string          `json:"source"` // journal | edsm | notfound
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
		resolvedInfo.Pos = &reference.Vec3{X: parsedSystem.Coords.X, Y: parsedSystem.Coords.Y, Z: parsedSystem.Coords.Z}
	} else {
		resolvedInfo.Source = "notfound"
	}
	return resolvedInfo, nil
}
