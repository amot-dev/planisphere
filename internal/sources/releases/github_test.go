package releases_test

import (
	"testing"

	"planisphere/internal/sources/releases"
)

func TestLatestReleaseAPIURL(testingContext *testing.T) {
	gotURL := releases.LatestReleaseAPIURL("https://github.com/amot-dev/planisphere")
	wantURL := "https://api.github.com/repos/amot-dev/planisphere/releases/latest"
	if gotURL != wantURL {
		testingContext.Fatalf("LatestReleaseAPIURL() = %q, want %q", gotURL, wantURL)
	}
}

