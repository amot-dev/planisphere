package app

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/gec"
	"planisphere/internal/store/settings"
)

// IngestStatus reports journal archive and watcher state.
type IngestStatus struct {
	Scanning           bool   `json:"scanning"`
	WatcherActive      bool   `json:"watcherActive"`
	WatcherError       string `json:"watcherError"`
	LastError          string `json:"lastError"`
	LastScanAt         string `json:"lastScanAt"`
	FilesTracked       int    `json:"filesTracked"`
	EventsStored       int    `json:"eventsStored"`
	FirstEventAt       string `json:"firstEventAt"`
	FirstEventGalactic string `json:"firstEventGalactic"`
	LastEventAt        string `json:"lastEventAt"`
	LastEventGalactic  string `json:"lastEventGalactic"`
	CurrentSystem      string `json:"currentSystem"`
	CurrentShip        string `json:"currentShip"`
}

// GetIngestStatus returns the journal ingestion status.
func (app *App) GetIngestStatus() (IngestStatus, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	readyError := app.ready()
	status := IngestStatus{Scanning: app.scanning.Load(), LastError: app.getError()}
	if readyError != nil {
		return status, nil
	}
	if app.watcher != nil {
		status.WatcherActive, status.WatcherError = app.watcher.Active()
	}
	status.LastScanAt = settings.LastScanAt(app.db)
	archiveStatus, archiveError := replay.ArchiveStatus(app.db)
	if archiveError != nil {
		return status, archiveError
	}
	status.FilesTracked, status.EventsStored = archiveStatus.FilesTracked, archiveStatus.EventsStored
	status.FirstEventAt, status.LastEventAt = archiveStatus.FirstEventAt, archiveStatus.LastEventAt
	status.FirstEventGalactic = reference.ToGalactic(archiveStatus.FirstEventAt)
	status.LastEventGalactic = reference.ToGalactic(archiveStatus.LastEventAt)
	if _, currentSystemName, locationError := replay.QueryLatestPlayerLocation(app.db); locationError == nil {
		status.CurrentSystem = currentSystemName
	}
	if currentShipName, shipError := replay.QueryCurrentShip(app.db); shipError == nil {
		status.CurrentShip = currentShipName
	}
	status.Scanning = app.scanning.Load()
	status.LastError = app.getError()
	return status, nil
}

// withReadyReadLock acquires app.dbMu.RLock, waits for startup readiness, and runs action if non-nil.
func (app *App) withReadyReadLock(action func() error) error {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	if action == nil {
		return nil
	}
	return action()
}

func (app *App) readyGECCatalog() (*gec.Catalog, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	if app.gec == nil {
		return nil, errors.New("GEC catalog not initialized")
	}
	return app.gec, nil
}

// RescanJournals performs a full idempotent catch-up scan of the configured journal directory.
func (app *App) RescanJournals() error {
	if readyError := app.withReadyReadLock(nil); readyError != nil {
		return readyError
	}
	app.runScan()
	return nil
}

// RefreshGEC re-downloads the EDAstro Galactic Exploration Catalog.
func (app *App) RefreshGEC() (int, error) {
	if !app.gecRefreshing.CompareAndSwap(false, true) {
		return 0, errors.New("GEC refresh is already in progress")
	}
	defer app.gecRefreshing.Store(false)

	gecCatalog, catalogError := app.readyGECCatalog()
	if catalogError != nil {
		return 0, catalogError
	}
	refreshCount, refreshError := gecCatalog.Refresh(app.ctx)
	if refreshError != nil {
		return 0, refreshError
	}
	_ = app.withReadyReadLock(func() error {
		if app.gec == gecCatalog {
			return settings.SetGECFetchedAt(app.db, reference.NowISO())
		}
		return nil
	})
	app.populateMissingPOIs()
	app.publishApp(0, events.AppExpeditionPlanChanged, events.AppSettingsChanged)
	return refreshCount, nil
}

// OpenURL opens an http(s) URL in the user's default browser.
func (app *App) OpenURL(link string) error {
	parsedURL, parseError := url.Parse(link)
	if parseError != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return fmt.Errorf("refusing to open %q", link)
	}
	runtime.BrowserOpenURL(app.ctx, link)
	return nil
}
