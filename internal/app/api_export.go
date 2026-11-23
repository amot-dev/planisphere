package app

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/exporter"
	"planisphere/internal/version"
)

// GetExpeditionExportData assembles the complete canonical export snapshot for expeditionID.
func (app *App) GetExpeditionExportData(expeditionID int64) (exporter.ExpeditionExport, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return exporter.ExpeditionExport{}, readyError
	}
	return exporter.Build(app.db, expeditionID, exporter.BuilderDeps{
		Projections: app.expeditionProjections(),
		GECLookup:   app.gecLookup,
		AppVersion:  version.Version,
		Now:         timeNow,
	})
}

// PickExportSavePath opens a native save-file dialog tailored to fileExt (png, html, json, or zip).
func (app *App) PickExportSavePath(defaultFilename, fileExt string) (string, error) {
	cleanedExtension := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileExt)), ".")
	dialogFilters := exportDialogFilters(cleanedExtension)
	return runtime.SaveFileDialog(app.ctx, runtime.SaveDialogOptions{
		Title:                fmt.Sprintf("Export %s Expedition", version.ProductName),
		DefaultFilename:      strings.TrimSpace(defaultFilename),
		CanCreateDirectories: true,
		Filters:              dialogFilters,
	})
}

// WriteExportOutput writes one file directly or packages multiple files into a zip archive at targetPath.
func (app *App) WriteExportOutput(targetPath string, files []exporter.BundleFile) (string, error) {
	return exporter.WriteExport(targetPath, files)
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

