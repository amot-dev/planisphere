package app

import (
	"log"

	"planisphere/internal/core/events"
	"planisphere/internal/sources/releases"
	"planisphere/internal/version"
)

var fetchLatestGitHubVersion = releases.FetchLatestRelease

func (app *App) getVersionStatus() (string, bool) {
	app.versionCheckMu.Lock()
	defer app.versionCheckMu.Unlock()
	return app.latestVersion, app.versionCheckFailed
}

func (app *App) checkLatestVersionOnStartup() {
	app.dbMu.RLock()
	requestContext := app.ctx
	app.dbMu.RUnlock()
	if requestContext == nil {
		return
	}

	app.versionCheckMu.Lock()
	if app.versionChecked {
		app.versionCheckMu.Unlock()
		return
	}
	app.versionChecked = true
	app.versionCheckMu.Unlock()

	latestReleaseVersion, fetchError := fetchLatestGitHubVersion(requestContext)
	if fetchError != nil {
		log.Printf("version check: %v", fetchError)
		app.versionCheckMu.Lock()
		app.versionCheckFailed = true
		app.versionCheckMu.Unlock()
		app.publishApp("settings", 0, events.AppSettingsChanged)
		return
	}

	app.versionCheckMu.Lock()
	app.latestVersion = latestReleaseVersion
	app.versionCheckFailed = false
	app.versionCheckMu.Unlock()

	if latestReleaseVersion != "" && latestReleaseVersion != version.Version {
		app.publishApp("settings", 0, events.AppSettingsChanged)
	}
}
