// Package gec caches the EDAstro Galactic Exploration Catalog (GEC) so
// waypoints can link to curated POI pages offline.
package gec

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"planisphere/internal/version"
)

// CombinedURL is the full GEC catalogue.
const CombinedURL = "https://edastro.com/gec/json/combined"

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

// Refresh downloads the combined catalogue and replaces the cache.
func (catalog *Catalog) Refresh(ctx context.Context) (int, error) {
	httpRequest, requestError := http.NewRequestWithContext(ctx, http.MethodGet, CombinedURL, nil)
	if requestError != nil {
		return 0, requestError
	}
	httpRequest.Header.Set("User-Agent", version.UserAgent)
	httpClient := &http.Client{Timeout: 90 * time.Second}
	httpResponse, httpError := httpClient.Do(httpRequest)
	if httpError != nil {
		return 0, fmt.Errorf("GEC download failed: %w", httpError)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GEC download: HTTP %d", httpResponse.StatusCode)
	}
	responseBody, readError := io.ReadAll(io.LimitReader(httpResponse.Body, 128<<20))
	if readError != nil {
		return 0, readError
	}
	importedCount, importError := catalog.Import(responseBody)
	if importError != nil {
		return 0, importError
	}
	return importedCount, catalog.Reload()
}
