// Package edsm resolves star system coordinates, first from the local
// journal archive via injected callbacks, then from a shared cache, then from the EDSM API.
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
	DB                 *sql.DB
	HTTP               *http.Client
	EDSMBase           string
	Online             func() bool // whether online lookups are allowed
	JournalLookup      func(systemName string) (*Info, error)
	JournalBatchLookup func(systemNames []string) (map[string]*Info, error)
	mu                 sync.Mutex
	lastCall           time.Time
	minSpacing         time.Duration
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

// ResolveLocal looks a system up offline from the injected journal lookup and
// shared system cache, returning (nil, nil) when no cached coordinates exist.
func (resolver *Resolver) ResolveLocal(name string) (*Info, error) {
	normalizedName := strings.Join(strings.Fields(name), " ")
	if normalizedName == "" {
		return nil, nil
	}
	if resolver.JournalLookup != nil {
		journalSystem, journalError := resolver.JournalLookup(normalizedName)
		if journalError != nil {
			return nil, journalError
		}
		if journalSystem != nil {
			cachedSystem, cacheError := resolver.fromCache(normalizedName)
			if cacheError != nil {
				return nil, cacheError
			}
			resolvedCopy := *journalSystem
			if cachedSystem != nil && cachedSystem.EDSMID != 0 {
				resolvedCopy.EDSMID = cachedSystem.EDSMID
			}
			return &resolvedCopy, nil
		}
	}
	cachedSystem, cacheError := resolver.fromCache(normalizedName)
	if cacheError != nil {
		return nil, cacheError
	}
	if cachedSystem != nil && cachedSystem.Pos != nil {
		return cachedSystem, nil
	}
	return nil, nil
}

// ResolveLocalBatch looks up multiple systems offline in chunked batches from
// the injected journal lookup and shared system_cache, keyed by lowercased
// whitespace-normalized system name.
func (resolver *Resolver) ResolveLocalBatch(names []string) (map[string]*Info, error) {
	uniqueNames, uniqueKeys := deduplicateSystemNames(names)
	if len(uniqueNames) == 0 {
		return map[string]*Info{}, nil
	}
	journalByKey, journalError := resolver.lookupJournalBatch(uniqueNames)
	if journalError != nil {
		return nil, journalError
	}
	cachedByKey, cacheError := resolver.fromCacheBatch(uniqueKeys)
	if cacheError != nil {
		return nil, cacheError
	}

	resolvedByKey := make(map[string]*Info, len(uniqueKeys))
	for _, systemKey := range uniqueKeys {
		if journalSystem := journalByKey[systemKey]; journalSystem != nil {
			resolvedCopy := *journalSystem
			if cachedSystem := cachedByKey[systemKey]; cachedSystem != nil && cachedSystem.EDSMID != 0 {
				resolvedCopy.EDSMID = cachedSystem.EDSMID
			}
			resolvedByKey[systemKey] = &resolvedCopy
			continue
		}
		if cachedSystem := cachedByKey[systemKey]; cachedSystem != nil && cachedSystem.Pos != nil {
			resolvedByKey[systemKey] = cachedSystem
		}
	}
	return resolvedByKey, nil
}

func (resolver *Resolver) lookupJournalBatch(uniqueNames []string) (map[string]*Info, error) {
	resolvedByKey := make(map[string]*Info, len(uniqueNames))
	if resolver.JournalBatchLookup != nil {
		for chunkStart := 0; chunkStart < len(uniqueNames); chunkStart += systemCacheChunkSize {
			chunkEnd := min(chunkStart+systemCacheChunkSize, len(uniqueNames))
			chunkMap, lookupError := resolver.JournalBatchLookup(uniqueNames[chunkStart:chunkEnd])
			if lookupError != nil {
				return nil, lookupError
			}
			for systemKey, journalSystem := range chunkMap {
				if journalSystem != nil {
					resolvedByKey[normalizeSystemCacheKey(systemKey)] = journalSystem
				}
			}
		}
		return resolvedByKey, nil
	}
	if resolver.JournalLookup != nil {
		for _, systemName := range uniqueNames {
			journalSystem, lookupError := resolver.JournalLookup(systemName)
			if lookupError != nil {
				return nil, lookupError
			}
			if journalSystem != nil {
				resolvedByKey[normalizeSystemCacheKey(systemName)] = journalSystem
			}
		}
	}
	return resolvedByKey, nil
}

// Resolve looks a system up (journal -> cache -> EDSM).
func (resolver *Resolver) Resolve(ctx context.Context, name string) (*Info, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return nil, ErrorNotFound
	}
	if localInfo, localError := resolver.ResolveLocal(name); localError != nil || localInfo != nil {
		return localInfo, localError
	}
	if resolver.Online != nil && !resolver.Online() {
		return nil, fmt.Errorf("%w (online lookups disabled)", ErrorNotFound)
	}
	edsmInfo, edsmError := resolver.fetchEDSM(ctx, name)
	if edsmError != nil {
		return nil, edsmError
	}
	if storeError := resolver.store(name, edsmInfo); storeError != nil {
		return nil, storeError
	}
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
