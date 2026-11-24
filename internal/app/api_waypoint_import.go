package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/core/events"
	"planisphere/internal/sources/edsm"
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
	selectedPath, dialogError := runtime.OpenFileDialog(app.context(), runtime.OpenDialogOptions{
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

func (app *App) buildUniqueSeedWaypoint(uniqueName string, localSystem *edsm.Info) expedition.SeedWaypoint {
	seedWaypoint := expedition.SeedWaypoint{SystemName: uniqueName}
	if localSystem != nil {
		if localSystem.Name != "" {
			seedWaypoint.SystemName = localSystem.Name
		}
		seedWaypoint.SystemAddress = localSystem.SystemAddress
		seedWaypoint.EDSMID = localSystem.EDSMID
		if localSystem.Pos != nil {
			seedWaypoint.HasCoords = true
			seedWaypoint.X, seedWaypoint.Y, seedWaypoint.Z = localSystem.Pos.X, localSystem.Pos.Y, localSystem.Pos.Z
			seedWaypoint.CoordsSource = localSystem.Source
		}
	}
	seedWaypoint.POIName = app.lookupGECPOIName(seedWaypoint.SystemName, seedWaypoint.SystemAddress)
	return seedWaypoint
}

// buildSeedWaypoints normalizes systemNames, resolves unique systems in a single
// offline batch against the journal archive and system_cache, and populates cached
// GEC POI names without per-system SQL queries. Caller must hold app.dbMu.RLock.
func (app *App) buildSeedWaypoints(systemNames []string) ([]expedition.SeedWaypoint, bool) {
	lookupKeys := make([]string, 0, len(systemNames))
	uniqueNames := make([]string, 0, len(systemNames))
	uniqueKeys := make([]string, 0, len(systemNames))
	seenKeys := make(map[string]struct{}, len(systemNames))
	for _, rawSystemName := range systemNames {
		normalizedName := strings.Join(strings.Fields(rawSystemName), " ")
		if normalizedName == "" {
			continue
		}
		lookupKey := strings.ToLower(normalizedName)
		lookupKeys = append(lookupKeys, lookupKey)
		if _, exists := seenKeys[lookupKey]; !exists {
			seenKeys[lookupKey] = struct{}{}
			uniqueNames = append(uniqueNames, normalizedName)
			uniqueKeys = append(uniqueKeys, lookupKey)
		}
	}
	if len(lookupKeys) == 0 {
		return []expedition.SeedWaypoint{}, false
	}
	var localByKey map[string]*edsm.Info
	if app.resolver != nil {
		resolvedBatch, resolveError := app.resolver.ResolveLocalBatch(uniqueNames)
		if resolveError != nil {
			log.Printf("batch resolve seed waypoints locally: %v", resolveError)
		} else {
			localByKey = resolvedBatch
		}
	}
	seedByKey := make(map[string]expedition.SeedWaypoint, len(uniqueNames))
	for index, uniqueName := range uniqueNames {
		lookupKey := uniqueKeys[index]
		seedByKey[lookupKey] = app.buildUniqueSeedWaypoint(uniqueName, localByKey[lookupKey])
	}
	seedWaypoints := make([]expedition.SeedWaypoint, 0, len(lookupKeys))
	hasUnresolvedCoords := false
	for _, lookupKey := range lookupKeys {
		seedWaypoint := seedByKey[lookupKey]
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
