package profile

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	projstats "planisphere/internal/projections/stats"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

type scopedCareerTarget struct {
	scope        replay.ProfileScope
	displayName  string
	metricEvents int
}

func runCareerStatsProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, error) {
	var results []ProbeResult

	if matchesFilter("Stats/CareerStats", options.FilterPattern) {
		careerProbe, careerError := profileCareerStats(database, proj, recorder, dataset, options.Iterations)
		if careerError != nil {
			return nil, careerError
		}
		results = append(results, careerProbe)
	}

	wantsScopedColdWarm := matchesFilter("Stats/CareerStatsScoped", options.FilterPattern)
	wantsScopedLiveJump := matchesFilter("Stats/LiveInvalidation/ScopedFSDJump", options.FilterPattern)
	if !wantsScopedColdWarm && !wantsScopedLiveJump {
		return results, nil
	}

	target, hasTarget, selectError := selectScopedCareerTarget(database, dataset)
	if selectError != nil || !hasTarget {
		return results, selectError
	}

	if wantsScopedColdWarm {
		scopedProbe, scopedError := profileScopedCareerStats(database, proj, recorder, target, options.Iterations)
		if scopedError != nil {
			return nil, scopedError
		}
		results = append(results, scopedProbe)
	}
	if wantsScopedLiveJump {
		liveJumpProbe, liveJumpError := profileScopedLiveJumpInvalidation(database, proj, recorder, target, options.Iterations)
		if liveJumpError != nil {
			return nil, liveJumpError
		}
		results = append(results, liveJumpProbe)
	}
	return results, nil
}

func profileCareerStats(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Stats/CareerStats",
		"Stats",
		"Assemble all-time commander telemetry across 5 cached career section projections",
		"stats.(*CareerStats).Get",
		"metric_events",
		"discoveries",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			telemetrySummary, getError := recordCareerStatsSections(recorder, proj.careerStats, dataset)
			if getError != nil {
				return singleExecOutcome{}, getError
			}
			return buildTelemetryExecOutcome(telemetrySummary, max(dataset.MetricEvents, 1)), nil
		},
	)
}

func recordCareerStatsSections(
	recorder *treeRecorder,
	careerStats *projstats.CareerStats,
	dataset DatasetSummary,
) (projstats.TelemetrySummary, error) {
	var (
		travelMetrics        projstats.TravelMetrics
		carrierOpsMetrics    projstats.CarrierOpsMetrics
		cartographicsMetrics projstats.CartographicsMetrics
		exobiologyMetrics    projstats.ExobiologyMetrics
		realizationBreakdown projstats.RealizationBreakdown
	)
	steps := []struct {
		label, location, unit string
		items                 int
		load                  func() error
	}{
		{"CareerTravel", "internal/projections/stats/career_travel.go:61", "events", dataset.MetricEvents, func() (loadError error) {
			travelMetrics, loadError = careerStats.Travel().Get()
			return loadError
		}},
		{"CareerCarrierOps", "internal/projections/stats/career_carrier_ops.go:50", "statistics", 1, func() (loadError error) {
			carrierOpsMetrics, loadError = careerStats.CarrierOps().Get()
			return loadError
		}},
		{"CareerCartographics", "internal/projections/stats/career_cartographics.go:54", "events", dataset.MetricEvents, func() (loadError error) {
			cartographicsMetrics, loadError = careerStats.Cartographics().Get()
			return loadError
		}},
		{"CareerExobiology", "internal/projections/stats/career_exobiology.go:63", "events", dataset.MetricEvents, func() (loadError error) {
			exobiologyMetrics, loadError = careerStats.Exobiology().Get()
			return loadError
		}},
		{"CareerRealization", "internal/projections/stats/career_realization.go:66", "real_events", dataset.RealizationEvents, func() (loadError error) {
			realizationBreakdown, loadError = careerStats.Realization().Get()
			return loadError
		}},
	}
	for _, step := range steps {
		if stepError := runStatsSubSpan(recorder, step.label, step.location, step.items, step.unit, step.load); stepError != nil {
			return projstats.TelemetrySummary{}, stepError
		}
	}

	var combinedMetrics projstats.Metrics
	travelMetrics.ApplyTo(&combinedMetrics)
	carrierOpsMetrics.ApplyTo(&combinedMetrics)
	cartographicsMetrics.ApplyTo(&combinedMetrics)
	exobiologyMetrics.ApplyTo(&combinedMetrics)
	return projstats.TelemetrySummary{Metrics: combinedMetrics, Realization: realizationBreakdown}, nil
}

func selectScopedCareerTarget(database *sql.DB, dataset DatasetSummary) (scopedCareerTarget, bool, error) {
	var preferredProfileID int64
	if dataset.PrimaryExpeditionID > 0 {
		expQueryError := database.QueryRow(`SELECT commander_profile_id FROM expeditions WHERE id = ?`, dataset.PrimaryExpeditionID).Scan(&preferredProfileID)
		if expQueryError != nil && !errors.Is(expQueryError, sql.ErrNoRows) {
			return scopedCareerTarget{}, false, fmt.Errorf("lookup primary expedition commander profile: %w", expQueryError)
		}
	}
	var profileID int64
	var displayName string
	profileError := database.QueryRow(`SELECT cp.id, cp.display_name FROM commander_profiles cp
		WHERE EXISTS (SELECT 1 FROM journal_events je WHERE je.profile_id = cp.id)
		ORDER BY CASE WHEN cp.id = ? THEN 0 ELSE 1 END, cp.enabled DESC, cp.id ASC LIMIT 1`, preferredProfileID).Scan(&profileID, &displayName)
	if errors.Is(profileError, sql.ErrNoRows) {
		return scopedCareerTarget{}, false, nil
	}
	if profileError != nil {
		return scopedCareerTarget{}, false, fmt.Errorf("select scoped commander profile: %w", profileError)
	}
	var metricEvents int
	if countError := database.QueryRow(`SELECT COUNT(*) FROM journal_events WHERE event IN `+metricEventNamesSQL+` AND +profile_id = ?`, profileID).Scan(&metricEvents); countError != nil {
		return scopedCareerTarget{}, false, fmt.Errorf("count scoped commander metric events: %w", countError)
	}
	return scopedCareerTarget{scope: replay.NewProfileScope([]int64{profileID}), displayName: displayName, metricEvents: metricEvents}, true, nil
}

func measureScopedCareerStats(recorder *treeRecorder, careerStats *projstats.CareerStats, target scopedCareerTarget) (singleExecOutcome, error) {
	var scopedSummary projstats.TelemetrySummary
	stepError := runStatsSubSpan(recorder, "CareerStats.GetScoped", "internal/projections/stats/career_stats.go:102", target.metricEvents, "events", func() error {
		var getError error
		scopedSummary, getError = careerStats.GetScoped(target.scope)
		return getError
	})
	if stepError != nil {
		return singleExecOutcome{}, stepError
	}
	return buildTelemetryExecOutcome(scopedSummary, max(target.metricEvents, 1)), nil
}

func profileScopedCareerStats(database *sql.DB, proj *profileProjections, recorder *treeRecorder, target scopedCareerTarget, iterations int) (ProbeResult, error) {
	description := fmt.Sprintf("Assemble profile-scoped commander telemetry (%s) across 5 keyed career section caches (Cold rebuild vs. Warm hit)", target.displayName)
	probeResult, probeError := runColdWarmProbe(
		database, recorder, "Stats/CareerStatsScoped", "Stats", description,
		"stats.(*CareerStats).GetScoped", "metric_events", "discoveries", iterations, proj.invalidate,
		func() (singleExecOutcome, error) { return measureScopedCareerStats(recorder, proj.careerStats, target) },
	)
	if probeError != nil {
		return ProbeResult{}, probeError
	}
	if probeResult.Warm.SQLQueries != 0 {
		return ProbeResult{}, fmt.Errorf("expected 0 SQL queries on warm CareerStats.GetScoped, got %d", probeResult.Warm.SQLQueries)
	}
	return probeResult, nil
}

func profileScopedLiveJumpInvalidation(database *sql.DB, proj *profileProjections, recorder *treeRecorder, target scopedCareerTarget, iterations int) (ProbeResult, error) {
	var prepError error
	var cartoGenBefore, exoGenBefore uint64
	beforeEach := func() {
		if _, warmError := proj.careerStats.GetScoped(target.scope); warmError != nil {
			prepError = fmt.Errorf("warm scoped career stats: %w", warmError)
			return
		}
		cartoGenBefore = proj.careerStats.Cartographics().Generation()
		exoGenBefore = proj.careerStats.Exobiology().Generation()
		proj.bus.PublishJournal(journal.NewEventSet(journal.EventFSDJump))
		if proj.careerStats.Travel().IsValid() || !proj.careerStats.Cartographics().IsValid() || !proj.careerStats.Exobiology().IsValid() {
			prepError = errors.New("expected FSDJump to invalidate CareerTravel while leaving CareerCartographics and CareerExobiology warm")
		}
	}
	description := fmt.Sprintf("Refresh scoped CareerStats (%s) after a single FSDJump invalidation while Discovery sections stay warm", target.displayName)
	probeResult, probeError := runSingleModeProbe(
		database, recorder, "Stats/LiveInvalidation/ScopedFSDJump", "Stats", description,
		"stats.(*CareerStats).GetScoped", "metric_events", "discoveries", iterations, beforeEach,
		func() (singleExecOutcome, error) {
			if prepError != nil {
				return singleExecOutcome{}, prepError
			}
			return measureScopedCareerStats(recorder, proj.careerStats, target)
		},
	)
	if probeError != nil {
		return ProbeResult{}, probeError
	}
	if proj.careerStats.Cartographics().Generation() != cartoGenBefore || proj.careerStats.Exobiology().Generation() != exoGenBefore ||
		!hasSpanNameFragment(probeResult.Cold.RootSpan, "CareerTravel") ||
		hasSpanNameFragment(probeResult.Cold.RootSpan, "CareerCartographics") ||
		hasSpanNameFragment(probeResult.Cold.RootSpan, "CareerExobiology") {
		return ProbeResult{}, errors.New("expected ScopedFSDJump refresh to recompute CareerTravel only without reloading CareerCartographics or CareerExobiology")
	}
	return probeResult, nil
}

func hasSpanNameFragment(node *SpanNode, fragment string) bool {
	if node == nil {
		return false
	}
	if strings.Contains(node.Name, fragment) {
		return true
	}
	for _, childNode := range node.Children {
		if hasSpanNameFragment(childNode, fragment) {
			return true
		}
	}
	return false
}
