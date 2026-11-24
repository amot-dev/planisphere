package app

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/exporter"
	"planisphere/internal/version"
)

// GetExpeditionExportData assembles the complete canonical export snapshot for expeditionID
// and caches it so WriteExportOutput can reuse the exact modal snapshot without drift.
func (app *App) GetExpeditionExportData(expeditionID int64) (exporter.ExpeditionExport, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return exporter.ExpeditionExport{}, readyError
	}
	exportSnapshot, buildError := exporter.Build(app.db, expeditionID, exporter.BuilderDeps{
		Projections: app.expeditionProjections(),
		GECLookup:   app.gecLookup,
		AppVersion:  version.Version,
		Now:         timeNow,
	})
	if buildError != nil {
		return exporter.ExpeditionExport{}, buildError
	}
	app.storeExportSnapshot(exportSnapshot)
	return exportSnapshot, nil
}

func (app *App) storeExportSnapshot(exportSnapshot exporter.ExpeditionExport) {
	snapshotCopy := exportSnapshot
	app.exportSnapshotMu.Lock()
	defer app.exportSnapshotMu.Unlock()
	app.lastExportSnapshot = &snapshotCopy
}

func (app *App) clearExportSnapshot() {
	app.exportSnapshotMu.Lock()
	defer app.exportSnapshotMu.Unlock()
	app.lastExportSnapshot = nil
}

func (app *App) matchingExportSnapshot(expeditionID int64, exportedAt string) (exporter.ExpeditionExport, bool) {
	app.exportSnapshotMu.Lock()
	defer app.exportSnapshotMu.Unlock()
	cached := app.lastExportSnapshot
	if cached != nil && cached.Plan.Expedition.ID == expeditionID && (exportedAt == "" || cached.ExportedAt == exportedAt) {
		return *cached, true
	}
	return exporter.ExpeditionExport{}, false
}

// PickExportSavePath opens a native save-file dialog tailored to fileExt (png, html, json, or zip).
func (app *App) PickExportSavePath(defaultFilename, fileExt string) (string, error) {
	cleanedExtension := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileExt)), ".")
	dialogFilters := exportDialogFilters(cleanedExtension)
	return runtime.SaveFileDialog(app.context(), runtime.SaveDialogOptions{
		Title:                fmt.Sprintf("Export %s Expedition", version.ProductName),
		DefaultFilename:      strings.TrimSpace(defaultFilename),
		CanCreateDirectories: true,
		Filters:              dialogFilters,
	})
}

// WriteExportOutput writes one file directly or packages multiple files into a zip archive at targetPath,
// populating any CanonicalJSONExpeditionID entries from the matching cached modal snapshot (or rebuilding
// on miss) directly in Go without round-tripping snapshot JSON over IPC.
func (app *App) WriteExportOutput(targetPath string, files []exporter.BundleFile) (string, error) {
	resolvedFiles := make([]exporter.BundleFile, len(files))
	copy(resolvedFiles, files)
	for index, bundleFile := range resolvedFiles {
		if bundleFile.CanonicalJSONExpeditionID > 0 && bundleFile.ContentText == "" && bundleFile.ContentBase64 == "" {
			snapshot, matched := app.matchingExportSnapshot(bundleFile.CanonicalJSONExpeditionID, bundleFile.CanonicalJSONExportedAt)
			if !matched {
				var exportError error
				snapshot, exportError = app.GetExpeditionExportData(bundleFile.CanonicalJSONExpeditionID)
				if exportError != nil {
					return "", fmt.Errorf("build canonical json for expedition %d: %w", bundleFile.CanonicalJSONExpeditionID, exportError)
				}
			}
			encodedJSON, marshalError := exporter.MarshalCanonicalJSON(snapshot)
			if marshalError != nil {
				return "", fmt.Errorf("marshal canonical json for expedition %d: %w", bundleFile.CanonicalJSONExpeditionID, marshalError)
			}
			resolvedFiles[index].ContentText = string(encodedJSON)
		}
	}
	return exporter.WriteExport(targetPath, resolvedFiles)
}

func exportDialogFilters(extension string) []runtime.FileFilter {
	switch extension {
	case "png":
		return []runtime.FileFilter{{DisplayName: "PNG Image (*.png)", Pattern: "*.png"}}
	case "html":
		return []runtime.FileFilter{{DisplayName: "HTML Report (*.html)", Pattern: "*.html"}}
	case "json":
		return []runtime.FileFilter{{DisplayName: "JSON Data (*.json)", Pattern: "*.json"}}
	case "zip":
		return []runtime.FileFilter{{DisplayName: "ZIP Archive (*.zip)", Pattern: "*.zip"}}
	default:
		return []runtime.FileFilter{{DisplayName: "All Files (*.*)", Pattern: "*.*"}}
	}
}
