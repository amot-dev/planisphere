package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LatestReleaseAPIURL derives the GitHub REST API latest-release endpoint from a repository URL.
func LatestReleaseAPIURL(repositoryURL string) string {
	cleaned := strings.TrimSpace(repositoryURL)
	cleaned = strings.TrimSuffix(cleaned, "/")
	cleaned = strings.TrimSuffix(cleaned, ".git")
	cleaned = strings.TrimPrefix(cleaned, "https://github.com/")
	cleaned = strings.TrimPrefix(cleaned, "http://github.com/")
	cleaned = strings.TrimPrefix(cleaned, "github.com/")
	return "https://api.github.com/repos/" + cleaned + "/releases/latest"
}

// FetchLatestRelease queries GitHub Releases for the latest published tag of Repository
// and returns the normalized version string with any leading "v" stripped.
func FetchLatestRelease(ctx context.Context) (string, error) {
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	endpointURL := LatestReleaseAPIURL(Repository)
	request, requestError := http.NewRequestWithContext(requestContext, http.MethodGet, endpointURL, nil)
	if requestError != nil {
		return "", fmt.Errorf("build github release request: %w", requestError)
	}
	request.Header.Set("User-Agent", UserAgent)
	request.Header.Set("Accept", "application/vnd.github+json")

	response, responseError := http.DefaultClient.Do(request)
	if responseError != nil {
		return "", fmt.Errorf("fetch github latest release: %w", responseError)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github latest release status: %s", response.Status)
	}

	var releasePayload struct {
		TagName string `json:"tag_name"`
	}
	if decodeError := json.NewDecoder(response.Body).Decode(&releasePayload); decodeError != nil {
		return "", fmt.Errorf("decode github latest release: %w", decodeError)
	}
	normalizedVersion := strings.TrimSpace(releasePayload.TagName)
	normalizedVersion = strings.TrimPrefix(strings.TrimPrefix(normalizedVersion, "v"), "V")
	return normalizedVersion, nil
}

