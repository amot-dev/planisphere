package releases_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"planisphere/internal/sources/releases"
	"planisphere/internal/testutil/httptestutil"
	"planisphere/internal/version"
)

func TestLatestReleaseAPIURL(testingContext *testing.T) {
	urlCases := []struct {
		name          string
		repositoryURL string
		wantURL       string
	}{
		{
			name:          "standard https repository URL",
			repositoryURL: "https://github.com/amot-dev/planisphere",
			wantURL:       "https://api.github.com/repos/amot-dev/planisphere/releases/latest",
		},
		{
			name:          "trailing slash and whitespace",
			repositoryURL: "  https://github.com/amot-dev/planisphere/  ",
			wantURL:       "https://api.github.com/repos/amot-dev/planisphere/releases/latest",
		},
		{
			name:          "git suffix over http",
			repositoryURL: "http://github.com/amot-dev/planisphere.git",
			wantURL:       "https://api.github.com/repos/amot-dev/planisphere/releases/latest",
		},
		{
			name:          "schemeless github host with git suffix and slash",
			repositoryURL: "github.com/amot-dev/planisphere.git/",
			wantURL:       "https://api.github.com/repos/amot-dev/planisphere/releases/latest",
		},
	}

	for _, testCase := range urlCases {
		testingContext.Run(testCase.name, func(subContext *testing.T) {
			gotURL := releases.LatestReleaseAPIURL(testCase.repositoryURL)
			if gotURL != testCase.wantURL {
				subContext.Fatalf("LatestReleaseAPIURL(%q) = %q, want %q", testCase.repositoryURL, gotURL, testCase.wantURL)
			}
		})
	}
}

func TestFetchLatestRelease(testingContext *testing.T) {
	expectedEndpoint := releases.LatestReleaseAPIURL(version.Repository)

	releaseCases := []struct {
		name        string
		makeContext func() (context.Context, context.CancelFunc)
		roundTrip   httptestutil.RoundTripFunc
		wantVersion string
		wantErrText string
		wantErrIs   error
	}{
		{
			name: "200 OK strips leading v and sends required headers",
			roundTrip: func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != expectedEndpoint {
					testingContext.Errorf("request URL = %q, want %q", request.URL.String(), expectedEndpoint)
				}
				if gotAgent := request.Header.Get("User-Agent"); gotAgent != version.UserAgent {
					testingContext.Errorf("User-Agent = %q, want %q", gotAgent, version.UserAgent)
				}
				if gotAccept := request.Header.Get("Accept"); gotAccept != "application/vnd.github+json" {
					testingContext.Errorf("Accept = %q, want application/vnd.github+json", gotAccept)
				}
				return httptestutil.Response(http.StatusOK, `{"tag_name":" v1.2.3 "}`), nil
			},
			wantVersion: "1.2.3",
		},
		{
			name: "non-200 HTTP status returns descriptive error",
			roundTrip: func(_ *http.Request) (*http.Response, error) {
				return httptestutil.Response(http.StatusServiceUnavailable, `{"message":"unavailable"}`), nil
			},
			wantErrText: "github latest release status: 503 Service Unavailable",
		},
		{
			name: "invalid JSON payload returns decode error",
			roundTrip: func(_ *http.Request) (*http.Response, error) {
				return httptestutil.Response(http.StatusOK, `{"tag_name":`), nil
			},
			wantErrText: "decode github latest release",
		},
		{
			name: "canceled context aborts transport request",
			makeContext: func() (context.Context, context.CancelFunc) {
				canceledContext, cancel := context.WithCancel(context.Background())
				cancel()
				return canceledContext, cancel
			},
			roundTrip: func(request *http.Request) (*http.Response, error) {
				return nil, request.Context().Err()
			},
			wantErrText: "fetch github latest release",
			wantErrIs:   context.Canceled,
		},
	}

	for _, testCase := range releaseCases {
		testingContext.Run(testCase.name, func(subContext *testing.T) {
			httptestutil.StubDefaultTransport(subContext, testCase.roundTrip)

			requestContext := context.Background()
			if testCase.makeContext != nil {
				var cancel context.CancelFunc
				requestContext, cancel = testCase.makeContext()
				defer cancel()
			}

			gotVersion, fetchError := releases.FetchLatestRelease(requestContext)
			if testCase.wantErrText != "" {
				if fetchError == nil || !strings.Contains(fetchError.Error(), testCase.wantErrText) {
					subContext.Fatalf("expected error containing %q, got version=%q err=%v", testCase.wantErrText, gotVersion, fetchError)
				}
				if testCase.wantErrIs != nil && !errors.Is(fetchError, testCase.wantErrIs) {
					subContext.Fatalf("expected error wrapping %v, got %v", testCase.wantErrIs, fetchError)
				}
				return
			}
			if fetchError != nil || gotVersion != testCase.wantVersion {
				subContext.Fatalf("FetchLatestRelease() = (%q, %v), want (%q, nil)", gotVersion, fetchError, testCase.wantVersion)
			}
		})
	}
}
