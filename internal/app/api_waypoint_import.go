package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/core/events"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

const maxWaypointImportFileBytes = 5 << 20

// WaypointImportFile holds the base filename and text content loaded from a
// user-selected plain-text or CSV waypoint list file.
type WaypointImportFile struct {
	FileName string `json:"fileName"`
	Content  string `json:"content"`
}

// BrowseWaypointImportFile opens a native file picker for a plain-text or CSV
// waypoint list and returns its filename and UTF-8 contents (empty when cancelled).
func (app *App) BrowseWaypointImportFile() (WaypointImportFile, error) {
	selectedPath, dialogError := runtime.OpenFileDialog(app.ctx, runtime.OpenDialogOptions{
		Title: "Select Waypoint List or Spansh CSV",
		Filters: []runtime.FileFilter{
			{DisplayName: "Waypoint Lists (*.txt, *.csv)", Pattern: "*.txt;*.csv"},
			{DisplayName: "All Files (*)", Pattern: "*"},
		},
	})
	if dialogError != nil || strings.TrimSpace(selectedPath) == "" {
		return WaypointImportFile{}, dialogError
	}
	return readWaypointImportFile(selectedPath)
}

func readWaypointImportFile(filePath string) (WaypointImportFile, error) {
	openedFile, openError := os.Open(filePath)
	if openError != nil {
		return WaypointImportFile{}, fmt.Errorf("open waypoint import file: %w", openError)
	}
	defer openedFile.Close()

	rawContent, readError := io.ReadAll(io.LimitReader(openedFile, maxWaypointImportFileBytes+1))
	if readError != nil {
		return WaypointImportFile{}, fmt.Errorf("read waypoint import file: %w", readError)
	}
	if int64(len(rawContent)) > maxWaypointImportFileBytes {
		return WaypointImportFile{}, errors.New("selected file exceeds the 5 MiB import limit")
	}
	if bytes.IndexByte(rawContent, 0) >= 0 {
		return WaypointImportFile{}, errors.New("selected file does not appear to be plain text or CSV")
	}
	return WaypointImportFile{
		FileName: filepath.Base(filePath),
		Content:  string(rawContent),
	}, nil
}

// resolveSeedWaypointLocally populates coordinates from the local journal or
// cache and any matching GEC POI name without performing network requests.
// Caller must hold app.dbMu.RLock.
func (app *App) resolveSeedWaypointLocally(normalizedName string) expedition.SeedWaypoint {
	seedWaypoint := expedition.SeedWaypoint{SystemName: normalizedName}
	if app.resolver != nil {
		if localInfo, resolveError := app.resolver.ResolveLocal(normalizedName); resolveError == nil && localInfo != nil {
			if localInfo.Name != "" {
				seedWaypoint.SystemName = localInfo.Name
			}
			seedWaypoint.SystemAddress = localInfo.SystemAddress
			seedWaypoint.EDSMID = localInfo.EDSMID
			if localInfo.Pos != nil {
				seedWaypoint.HasCoords = true
				seedWaypoint.X = localInfo.Pos.X
				seedWaypoint.Y = localInfo.Pos.Y
				seedWaypoint.Z = localInfo.Pos.Z
				seedWaypoint.CoordsSource = localInfo.Source
			}
		}
	}
	seedWaypoint.POIName = app.lookupGECPOIName(seedWaypoint.SystemName, seedWaypoint.SystemAddress)
	return seedWaypoint
}

// buildSeedWaypoints normalizes systemNames and deduplicates offline lookups so
// revisited systems query SQLite and GEC only once. Caller must hold app.dbMu.RLock.
func (app *App) buildSeedWaypoints(systemNames []string) ([]expedition.SeedWaypoint, bool) {
	seedWaypoints := make([]expedition.SeedWaypoint, 0, len(systemNames))
	resolvedByKey := make(map[string]expedition.SeedWaypoint, len(systemNames))
	hasUnresolvedCoords := false

	for _, rawSystemName := range systemNames {
		normalizedName := strings.Join(strings.Fields(rawSystemName), " ")
		if normalizedName == "" {
			continue
		}
		lookupKey := strings.ToLower(normalizedName)
		seedWaypoint, alreadyResolved := resolvedByKey[lookupKey]
		if !alreadyResolved {
			seedWaypoint = app.resolveSeedWaypointLocally(normalizedName)
			resolvedByKey[lookupKey] = seedWaypoint
		}
		if !seedWaypoint.HasCoords {
			hasUnresolvedCoords = true
		}
		seedWaypoints = append(seedWaypoints, seedWaypoint)
	}
	return seedWaypoints, hasUnresolvedCoords
}

// CreateExpeditionWithWaypoints creates a planning-mode expedition pre-populated
// with systemNames, resolves already-known coordinates and GEC POIs offline, makes
// it active, and queues background EDSM lookups for any remaining unvisited systems.
func (app *App) CreateExpeditionWithWaypoints(name, description string, systemNames []string) (expedition.Expedition, error) {
	var createdExpedition expedition.Expedition
	var hasUnresolvedCoords bool

	if err := app.withReadyReadLock(func() error {
		seedWaypoints, unresolved := app.buildSeedWaypoints(systemNames)
		hasUnresolvedCoords = unresolved

		var createError error
		createdExpedition, createError = expedition.CreateWithWaypoints(app.db, name, description, seedWaypoints)
		if createError != nil {
			return createError
		}
		if setActiveError := settings.SetActiveExpeditionID(app.db, createdExpedition.ID); setActiveError != nil {
			return fmt.Errorf("set active expedition: %w", setActiveError)
		}
		return nil
	}); err != nil {
		return createdExpedition, err
	}

	if hasUnresolvedCoords {
		app.kickResolution(createdExpedition.ID, false)
	}
	app.publishApp(createdExpedition.ID, events.AppExpeditionPlanChanged)
	return createdExpedition, nil
}

