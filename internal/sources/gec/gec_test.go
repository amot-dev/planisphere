package gec_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"planisphere/internal/sources/gec"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/version"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestGECandGMPCombinedImport(t *testing.T) {
	dbConn := dbtest.OpenMemory(t)

	catalogInstance := gec.NewCatalog(dbConn)

	// Sample JSON with 1 GEC entry and 1 GMP entry having the SAME ID 729
	rawJSON := `[
		{
			"id": 729,
			"source": "GEC",
			"type": "Tourist Destination",
			"name": "Breathing Water",
			"galMapSearch": "Bleae Thua ZY-Z d13-0",
			"poiUrl": "https://edastro.com/gec/view/729",
			"coordinates": [10.0, 20.0, 30.0],
			"descriptionMardown": "Long description markdown to be dropped",
			"descriptionHtml": "<p>Long description html to be dropped</p>"
		},
		{
			"id": 729,
			"source": "GMP",
			"type": "planetaryNebula",
			"name": "Cupid&#39;s Arrow",
			"galMapSearch": "S171 34",
			"galMapUrl": "https://www.edsm.net/en/system/id/332599/name/S171+34",
			"coordinates": [-2985.96875, 601.75, -1723.96875]
		}
	]`

	count, importError := catalogInstance.Import([]byte(rawJSON))
	if importError != nil {
		t.Fatalf("import failed: %v", importError)
	}
	if count != 2 {
		t.Fatalf("expected 2 imported entries, got %d", count)
	}
	if reloadError := catalogInstance.Reload(); reloadError != nil {
		t.Fatalf("reload failed: %v", reloadError)
	}

	// Verify deprecated raw_json is stored as an empty string
	var cachedRawJSON string
	if queryError := dbConn.QueryRow(`SELECT raw_json FROM gec_pois_cache WHERE id = 729`).Scan(&cachedRawJSON); queryError != nil {
		t.Fatalf("query cached raw_json: %v", queryError)
	}
	if cachedRawJSON != "" {
		t.Errorf("expected cachedRawJSON == \"\", got %q", cachedRawJSON)
	}

	// Lookup GEC
	gecMatch := catalogInstance.Lookup("Bleae Thua ZY-Z d13-0", 0)
	if len(gecMatch) == 0 {
		t.Fatalf("expected GEC match for Bleae Thua ZY-Z d13-0")
	}
	if gecMatch[0].Name != "Breathing Water" || gecMatch[0].Source != "GEC" {
		t.Errorf("unexpected GEC entry: %+v", gecMatch[0])
	}

	// Lookup GMP (e.g. S171 34 -> Cupid's Arrow)
	gmpMatch := catalogInstance.Lookup("S171 34", 0)
	if len(gmpMatch) == 0 {
		t.Fatalf("expected GMP match for S171 34")
	}
	if gmpMatch[0].Name != "Cupid's Arrow" || gmpMatch[0].Source != "GMP" {
		t.Errorf("unexpected GMP entry: %+v", gmpMatch[0])
	}
}

func TestCatalogRefresh(testingContext *testing.T) {
	refreshCases := []struct {
		name        string
		roundTrip   roundTripFunc
		wantCount   int
		wantErrText string
	}{
		{
			name: "200 OK imports and reloads in-memory lookup",
			roundTrip: func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != gec.CombinedURL {
					testingContext.Errorf("request URL = %q, want %q", request.URL.String(), gec.CombinedURL)
				}
				if gotAgent := request.Header.Get("User-Agent"); gotAgent != version.UserAgent {
					testingContext.Errorf("User-Agent = %q, want %q", gotAgent, version.UserAgent)
				}
				payload := `[{"id":101,"source":"GEC","type":"Nebula","name":"Veil West","galMapSearch":"Veil West Sector DL-Y d68","id64":2345678901,"coordinates":[-1300,-150,-260]}]`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(payload)),
				}, nil
			},
			wantCount: 1,
		},
		{
			name: "non-200 HTTP response returns status error",
			roundTrip: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("gateway failure")),
				}, nil
			},
			wantErrText: "GEC download: HTTP 502",
		},
		{
			name: "transport failure wraps download error",
			roundTrip: func(_ *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
			wantErrText: "GEC download failed",
		},
	}

	for _, testCase := range refreshCases {
		testingContext.Run(testCase.name, func(subContext *testing.T) {
			originalTransport := http.DefaultTransport
			subContext.Cleanup(func() {
				http.DefaultTransport = originalTransport
			})
			http.DefaultTransport = testCase.roundTrip

			dbConn := dbtest.OpenMemory(subContext)
			catalogInstance := gec.NewCatalog(dbConn)

			importedCount, refreshError := catalogInstance.Refresh(context.Background())
			if testCase.wantErrText != "" {
				if refreshError == nil || !strings.Contains(refreshError.Error(), testCase.wantErrText) {
					subContext.Fatalf("expected error containing %q, got count=%d err=%v", testCase.wantErrText, importedCount, refreshError)
				}
				return
			}
			if refreshError != nil || importedCount != testCase.wantCount || catalogInstance.Count() != testCase.wantCount {
				subContext.Fatalf("Refresh() = (%d, %v), count=%d, want %d", importedCount, refreshError, catalogInstance.Count(), testCase.wantCount)
			}
			byAddr := catalogInstance.Lookup("Unmatched Name", 2345678901)
			if len(byAddr) != 1 || byAddr[0].Name != "Veil West" || byAddr[0].URL != "https://edastro.com/gec/view/101" {
				subContext.Fatalf("unexpected reloaded address lookup after Refresh: %+v", byAddr)
			}
		})
	}
}
