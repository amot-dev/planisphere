package version

import "testing"

func TestLatestReleaseAPIURL(testingContext *testing.T) {
	gotURL := LatestReleaseAPIURL("https://github.com/amot-dev/planisphere")
	wantURL := "https://api.github.com/repos/amot-dev/planisphere/releases/latest"
	if gotURL != wantURL {
		testingContext.Fatalf("LatestReleaseAPIURL() = %q, want %q", gotURL, wantURL)
	}
}

