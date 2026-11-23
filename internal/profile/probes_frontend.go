package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"planisphere/internal/app"
	"planisphere/internal/exporter"
	projstats "planisphere/internal/projections/stats"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/gec"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

type frontendLiveFixture struct {
	Plan           expedition.PlanView          `json:"plan"`
	SidebarStats   app.SidebarStats             `json:"sidebarStats"`
	ModesState     app.ModesState               `json:"modesState"`
	VisitedDetails []expedition.WaypointDetails `json:"visitedDetails"`
	HeldRowIndex   int                          `json:"heldRowIndex"`
	DropPreviews   []expedition.DropSlotPreview `json:"dropPreviews"`
	ExportData     *exporter.ExpeditionExport   `json:"exportData,omitempty"`
}

func runFrontendProbes(
	ctx context.Context,
	database *sql.DB,
	proj *profileProjections,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, error) {
	if !options.IncludeFrontend || dataset.PrimaryExpeditionID == 0 {
		return nil, nil
	}
	if !matchesFilter("Frontend/WailsHydration", options.FilterPattern) &&
		!matchesFilter("Frontend/RenderTree", options.FilterPattern) &&
		!matchesFilter("Frontend/ClientReconciliation", options.FilterPattern) &&
		!matchesFilter("Frontend/ExportStudio", options.FilterPattern) {
		return nil, nil
	}

	fixture, buildError := buildFrontendFixture(database, proj, dataset)
	if buildError != nil {
		return nil, buildError
	}

	fixturePath, cleanupFixture, writeError := writeTemporaryFixtureJSON(fixture)
	if writeError != nil {
		return nil, writeError
	}
	defer cleanupFixture()

	outputPath, cleanupOutput, outError := createTemporaryOutputFile()
	if outError != nil {
		return nil, outError
	}
	defer cleanupOutput()

	frontendDir, dirError := resolveFrontendDirectory()
	if dirError != nil {
		return nil, dirError
	}

	command := exec.CommandContext(ctx, "npx", "vitest", "run", "src/profile/live_frontend_profile.test.ts")
	command.Dir = frontendDir
	command.Env = append(
		os.Environ(),
		"LIVE_PROFILE_FIXTURE="+fixturePath,
		"LIVE_PROFILE_OUT="+outputPath,
		"LIVE_PROFILE_ITER="+strconv.Itoa(max(options.Iterations, 1)),
	)
	commandOutput, execError := command.CombinedOutput()
	if execError != nil {
		return nil, fmt.Errorf("execute frontend profiler via vitest: %w\n%s", execError, string(commandOutput))
	}

	rawResults, readOutError := os.ReadFile(outputPath)
	if readOutError != nil {
		return nil, fmt.Errorf("read frontend profile output: %w", readOutError)
	}
	var frontendResults []ProbeResult
	if unmarshalError := json.Unmarshal(rawResults, &frontendResults); unmarshalError != nil {
		return nil, fmt.Errorf("decode frontend profile output: %w", unmarshalError)
	}

	var filtered []ProbeResult
	for _, probeItem := range frontendResults {
		if matchesFilter(probeItem.Name, options.FilterPattern) {
			filtered = append(filtered, probeItem)
		}
	}
	return filtered, nil
}

func writeTemporaryFixtureJSON(fixture frontendLiveFixture) (string, func(), error) {
	fixtureFile, createFixtureError := os.CreateTemp("", "planisphere-frontend-fixture-*.json")
	if createFixtureError != nil {
		return "", nil, fmt.Errorf("create frontend fixture file: %w", createFixtureError)
	}
	fixturePath := fixtureFile.Name()
	cleanup := func() {
		if removeError := os.Remove(fixturePath); removeError != nil {
			return
		}
	}
	if closeError := fixtureFile.Close(); closeError != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary fixture handle: %w", closeError)
	}

	encodedFixture, marshalError := json.Marshal(fixture)
	if marshalError != nil {
		cleanup()
		return "", nil, fmt.Errorf("marshal frontend fixture: %w", marshalError)
	}
	if writeError := os.WriteFile(fixturePath, encodedFixture, 0o600); writeError != nil {
		cleanup()
		return "", nil, fmt.Errorf("write frontend fixture: %w", writeError)
	}
	return fixturePath, cleanup, nil
}

func createTemporaryOutputFile() (string, func(), error) {
	outputFile, createOutError := os.CreateTemp("", "planisphere-frontend-profile-out-*.json")
	if createOutError != nil {
		return "", nil, fmt.Errorf("create frontend profile output file: %w", createOutError)
	}
	outputPath := outputFile.Name()
	cleanup := func() {
		if removeError := os.Remove(outputPath); removeError != nil {
			return
		}
	}
	if closeError := outputFile.Close(); closeError != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary output handle: %w", closeError)
	}
	return outputPath, cleanup, nil
}

func buildFrontendFixture(database *sql.DB, proj *profileProjections, dataset DatasetSummary) (frontendLiveFixture, error) {
	gecCatalog := gec.NewCatalog(database)
	gecLookup := func(systemName string, systemAddress int64) *expedition.GECRef {
		matched := gecCatalog.Lookup(systemName, systemAddress)
		if len(matched) == 0 {
			return nil
		}
		return &expedition.GECRef{
			ID:    matched[0].ID,
			Name:  matched[0].Name,
			Type:  matched[0].Type,
			URL:   matched[0].URL,
			Count: len(matched),
		}
	}

	expProjections := proj.expeditionProjections()
	planView, planError := expedition.BuildPlan(database, dataset.PrimaryExpeditionID, gecLookup, expProjections)
	if planError != nil {
		return frontendLiveFixture{}, planError
	}

	sidebarStats, sidebarError := buildFixtureSidebarStats(database, proj, planView)
	if sidebarError != nil {
		return frontendLiveFixture{}, sidebarError
	}

	modesState, modesError := buildFixtureModesState(database)
	if modesError != nil {
		return frontendLiveFixture{}, modesError
	}

	visitedDetails := loadFixtureVisitedDetails(database, planView, expProjections)
	heldRowIndex, dropPreviews := loadFixtureDropPreviews(database, planView, dataset.SampleVisitedLegRowID, expProjections)

	var exportData *exporter.ExpeditionExport
	builtExport, exportError := exporter.Build(database, dataset.PrimaryExpeditionID, exporter.BuilderDeps{
		Projections: expProjections,
		CareerStats: proj.careerStats,
		GECLookup:   gecLookup,
		AppVersion:  "profile",
		Now:         profileExportNow,
	})
	if exportError != nil {
		if !errors.Is(exportError, exporter.ErrNotExportable) {
			return frontendLiveFixture{}, exportError
		}
	} else {
		exportData = &builtExport
	}

	return frontendLiveFixture{
		Plan:           planView,
		SidebarStats:   sidebarStats,
		ModesState:     modesState,
		VisitedDetails: visitedDetails,
		HeldRowIndex:   heldRowIndex,
		DropPreviews:   dropPreviews,
		ExportData:     exportData,
	}, nil
}

func buildFixtureSidebarStats(
	database *sql.DB,
	proj *profileProjections,
	planView expedition.PlanView,
) (app.SidebarStats, error) {
	careerSummary, careerError := proj.careerStats.Get()
	if careerError != nil {
		return app.SidebarStats{}, careerError
	}

	expeditionRecord := planView.Expedition
	manualAdjustments := expeditionRecord.Adjustments.ToMetrics()
	sidebar := app.SidebarStats{
		ExpeditionID:           expeditionRecord.ID,
		Status:                 expeditionRecord.Status,
		Started:                expeditionRecord.StartTimestamp != "",
		End:                    expeditionRecord.EndTimestamp,
		StartGalactic:          planView.StartGalactic,
		EndGalactic:            planView.EndGalactic,
		Adjustments:            manualAdjustments,
		Expedition:             manualAdjustments,
		AllJournals:            careerSummary.Metrics,
		AllJournalsRealization: careerSummary.Realization,
	}

	if careerStart, careerStartGalactic := replay.CareerStart(database); careerStart != "" {
		sidebar.CareerStart = careerStart
		sidebar.CareerStartGalactic = careerStartGalactic
		sidebar.CareerDuration = reference.Elapsed(careerStart, "")
	}

	referenceNow := time.Now().UTC()
	var effectiveDisplayStart string
	if sidebar.Started {
		queryAfter, displayStart, departed, startError := expedition.EffectiveStart(database, expeditionRecord)
		if startError != nil {
			return app.SidebarStats{}, startError
		}
		sidebar.Departed = departed
		effectiveDisplayStart = displayStart
		if departed {
			sidebar.Start = displayStart
			sidebar.StartGalactic = reference.ToGalactic(displayStart)
			if sidebar.End == "" {
				sidebar.EndIsNow = true
				sidebar.EndGalactic = reference.TimeToGalactic(referenceNow)
			}
			sidebar.Duration = reference.Elapsed(displayStart, sidebar.End)
			expeditionRange := projstats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
			expeditionSummary, rangeError := proj.timeRangeStats.Compute(expeditionRange, nil)
			if rangeError != nil {
				return app.SidebarStats{}, rangeError
			}
			sidebar.Live = expeditionSummary.Metrics
			sidebar.Expedition = expeditionSummary.Metrics.Add(manualAdjustments)
			sidebar.Realization = expeditionSummary.Realization
		}
	}

	sidebar.CarrierCost = expedition.ComputeCarrierCost(database, expeditionRecord, sidebar.Expedition, effectiveDisplayStart, sidebar.Departed, referenceNow)
	return sidebar, nil
}

func buildFixtureModesState(database *sql.DB) (app.ModesState, error) {
	modesList, listError := modes.List(database)
	if listError != nil {
		return app.ModesState{}, listError
	}
	if modesList == nil {
		modesList = []modes.Mode{}
	}
	return app.ModesState{Modes: modesList}, nil
}

func loadFixtureVisitedDetails(
	database *sql.DB,
	planView expedition.PlanView,
	expProjections *expedition.Projections,
) []expedition.WaypointDetails {
	visitedDetails := make([]expedition.WaypointDetails, 0, 6)
	for _, rowView := range planView.Rows {
		if rowView.Row.IsWaypoint() && rowView.Row.VisitedAt != "" {
			detail, detailError := expedition.Details(database, rowView.Row.ID, expProjections)
			if detailError == nil {
				visitedDetails = append(visitedDetails, detail)
			}
			if len(visitedDetails) >= 6 {
				break
			}
		}
	}
	return visitedDetails
}

func loadFixtureDropPreviews(
	database *sql.DB,
	planView expedition.PlanView,
	sampleRowID int64,
	expProjections *expedition.Projections,
) (int, []expedition.DropSlotPreview) {
	if sampleRowID <= 0 {
		return -1, []expedition.DropSlotPreview{}
	}
	heldIndex := -1
	for rowIndex, rowView := range planView.Rows {
		if rowView.Row.ID == sampleRowID {
			heldIndex = rowIndex
			break
		}
	}
	if heldIndex < 0 {
		return -1, []expedition.DropSlotPreview{}
	}
	previews, previewError := expedition.ComputeRowDropPreviews(database, sampleRowID, expProjections)
	if previewError != nil || len(previews) == 0 {
		return -1, []expedition.DropSlotPreview{}
	}
	return heldIndex, previews
}

func resolveFrontendDirectory() (string, error) {
	_, callerFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to resolve caller path for frontend directory")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(callerFile), "..", ".."))
	frontendDir := filepath.Join(repoRoot, "frontend")
	if fileInfo, statError := os.Stat(frontendDir); statError != nil || !fileInfo.IsDir() {
		return "", fmt.Errorf("frontend directory not found at %s", frontendDir)
	}
	return frontendDir, nil
}
