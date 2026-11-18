package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"planisphere/internal/app"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/gec"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

type frontendLiveFixture struct {
	Plan           expedition.PlanView          `json:"plan"`
	SidebarStats   app.SidebarStats             `json:"sidebarStats"`
	Modes          []modes.Mode                 `json:"modes"`
	VisitedDetails []expedition.WaypointDetails `json:"visitedDetails"`
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
		!matchesFilter("Frontend/RenderTree", options.FilterPattern) {
		return nil, nil
	}

	fixture, buildError := buildFrontendFixture(database, proj, dataset)
	if buildError != nil {
		return nil, buildError
	}

	fixtureFile, createFixtureError := os.CreateTemp("", "planisphere-frontend-fixture-*.json")
	if createFixtureError != nil {
		return nil, fmt.Errorf("create frontend fixture file: %w", createFixtureError)
	}
	fixturePath := fixtureFile.Name()
	defer func() { _ = os.Remove(fixturePath) }()

	encodedFixture, marshalError := json.Marshal(fixture)
	_ = fixtureFile.Close()
	if marshalError != nil {
		return nil, fmt.Errorf("marshal frontend fixture: %w", marshalError)
	}
	if writeError := os.WriteFile(fixturePath, encodedFixture, 0o600); writeError != nil {
		return nil, fmt.Errorf("write frontend fixture: %w", writeError)
	}

	outputFile, createOutError := os.CreateTemp("", "planisphere-frontend-profile-out-*.json")
	if createOutError != nil {
		return nil, fmt.Errorf("create frontend profile output file: %w", createOutError)
	}
	outputPath := outputFile.Name()
	_ = outputFile.Close()
	defer func() { _ = os.Remove(outputPath) }()

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

	expProj := proj.expeditionProjections()
	planView, planError := expedition.BuildPlan(database, dataset.PrimaryExpeditionID, gecLookup, expProj)
	if planError != nil {
		return frontendLiveFixture{}, planError
	}

	statsEngine := proj.statsEngine()
	allMetrics, _ := statsEngine.Compute(stats.Range{}, nil)
	allRealization, _ := statsEngine.ComputeRealization(stats.Range{}, nil)
	expRange := stats.Range{After: planView.Expedition.StartTimestamp, Until: planView.Expedition.EndTimestamp}
	liveMetrics, _ := statsEngine.Compute(expRange, nil)
	liveRealization, _ := statsEngine.ComputeRealization(expRange, nil)

	manualAdjustments := planView.Expedition.Adjustments.ToMetrics()
	sidebar := app.SidebarStats{
		ExpeditionID:           planView.Expedition.ID,
		Status:                 planView.Expedition.Status,
		Started:                planView.Expedition.StartTimestamp != "",
		Start:                  planView.Expedition.StartTimestamp,
		End:                    planView.Expedition.EndTimestamp,
		StartGalactic:          planView.StartGalactic,
		EndGalactic:            planView.EndGalactic,
		Live:                   liveMetrics,
		Adjustments:            manualAdjustments,
		Expedition:             liveMetrics.Add(manualAdjustments),
		AllJournals:            allMetrics,
		Realization:            liveRealization,
		AllJournalsRealization: allRealization,
	}

	modeList, _ := modes.List(database)
	if modeList == nil {
		modeList = []modes.Mode{}
	}
	visitedDetails := []expedition.WaypointDetails{}
	for _, rowView := range planView.Rows {
		if rowView.Row.IsWaypoint() && rowView.Row.VisitedAt != "" {
			if detail, err := expedition.Details(database, rowView.Row.ID, expProj); err == nil {
				visitedDetails = append(visitedDetails, detail)
			}
			if len(visitedDetails) >= 6 {
				break
			}
		}
	}

	return frontendLiveFixture{
		Plan:           planView,
		SidebarStats:   sidebar,
		Modes:          modeList,
		VisitedDetails: visitedDetails,
	}, nil
}

func resolveFrontendDirectory() (string, error) {
	_, callerFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to resolve caller path for frontend directory")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(callerFile), "..", ".."))
	frontendDir := filepath.Join(repoRoot, "frontend")
	if info, statErr := os.Stat(frontendDir); statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("frontend directory not found at %s", frontendDir)
	}
	return frontendDir, nil
}
