package app

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"planisphere/internal/export"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/version"
)

// GetExpeditionExportData assembles the complete canonical export snapshot for expeditionID.
func (app *App) GetExpeditionExportData(expeditionID int64) (export.ExpeditionExport, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return export.ExpeditionExport{}, readyError
	}
	return export.Build(app.db, expeditionID, export.BuilderDeps{
		Projections: app.expeditionProjections(),
		CareerStats: app.careerStats,
		GECLookup:   app.gecLookup,
		ComputeCarrierCost: func(expeditionRecord expedition.Expedition, expeditionMetrics replaystats.Metrics, effectiveStart string, departed bool) export.CarrierCost {
			cost := app.computeExpeditionCarrierCost(expeditionRecord, expeditionMetrics, effectiveStart, departed)
			return export.CarrierCost{
				CarrierType:    cost.CarrierType,
				WeeklyUpkeep:   cost.WeeklyUpkeep,
				TritiumPrice:   cost.TritiumPrice,
				TritiumCost:    cost.TritiumCost,
				JumpCost:       cost.JumpCost,
				UpkeepCost:     cost.UpkeepCost,
				TotalCost:      cost.TotalCost,
				TotalEstimated: cost.TotalEstimated,
			}
		},
		AppVersion: version.Version,
		Now:        timeNow,
	})
}

// ComputeExportRangeStats evaluates telemetry for a custom timeframe within
// expeditionID, optionally restricted to a single flight mode when modeID > 0.
func (app *App) ComputeExportRangeStats(
	expeditionID int64,
	afterTimestamp, untilTimestamp string,
	modeID int64,
) (export.TelemetrySnapshot, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return export.TelemetrySnapshot{}, readyError
	}
	return export.ComputeRange(
		app.db,
		app.expeditionProjections(),
		expeditionID,
		afterTimestamp,
		untilTimestamp,
		modeID,
	)
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
func (app *App) WriteExportOutput(targetPath string, files []export.BundleFile) (string, error) {
	return export.WriteExport(targetPath, files)
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

