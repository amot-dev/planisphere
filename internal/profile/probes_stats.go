package profile

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	projstats "planisphere/internal/projections/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/modes"
)

func runStatsProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, []ScalingModel, error) {
	var results []ProbeResult

	if matchesFilter("Stats/SalesLedger", options.FilterPattern) {
		ledgerProbe, ledgerError := profileSalesLedger(database, proj, recorder, dataset, options.Iterations)
		if ledgerError != nil {
			return nil, nil, ledgerError
		}
		results = append(results, ledgerProbe)
	}

	if matchesFilter("Stats/CareerStats", options.FilterPattern) {
		careerProbe, careerError := profileCareerStats(database, proj, recorder, dataset, options.Iterations)
		if careerError != nil {
			return nil, nil, careerError
		}
		results = append(results, careerProbe)
	}

	windowProbes, computePoints, windowError := runTimeRangeWindowProbes(database, proj, recorder, dataset, options)
	if windowError != nil {
		return nil, nil, windowError
	}
	results = append(results, windowProbes...)

	liveProbes, liveError := runLiveInvalidationProbes(database, proj, recorder, dataset, options)
	if liveError != nil {
		return nil, nil, liveError
	}
	results = append(results, liveProbes...)

	var scalingModels []ScalingModel
	if len(computePoints) >= 2 {
		scalingModels = append(scalingModels, computeLinearScalingModel(
			"stats.(*TimeRangeStats).Compute (Uncached Window, Warm Ledger)",
			"metric_events",
			computePoints,
		))
	}
	return results, scalingModels, nil
}

func profileSalesLedger(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Stats/SalesLedger",
		"Stats",
		"Build lifetime chronological realization settlement ledger (Cold rebuild vs. Warm cache)",
		"realization.(*SalesLedger).Breakdown",
		"real_events",
		"credits",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			breakdown, breakdownError := proj.salesLedger.Breakdown("", "9999-12-31T23:59:59Z", nil)
			if breakdownError != nil {
				return singleExecOutcome{}, breakdownError
			}
			summary := fmt.Sprintf(
				"Realized=%d Pending=%d Tariff=%d Crew=%d CartoDeath=%d OrgDeath=%d",
				breakdown.TotalRealized,
				breakdown.PendingValue,
				breakdown.CarrierTariffLoss,
				breakdown.NpcCrewLoss,
				breakdown.CartoLostDeath,
				breakdown.OrganicLostDeath,
			)
			return singleExecOutcome{
				value:     breakdown,
				summary:   summary,
				items:     max(dataset.RealizationEvents, 1),
				secondary: int(breakdown.TotalRealized / 1_000_000),
			}, nil
		},
	)
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

func runStatsSubSpan(
	recorder *treeRecorder,
	label string,
	location string,
	items int,
	unit string,
	operation func() error,
) error {
	return recorder.withSubSpan(label, location, max(items, 1), unit, operation)
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

func runTimeRangeWindowProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, []ScalingPoint, error) {
	var results []ProbeResult
	var computePoints []ScalingPoint

	for _, window := range dataset.Windows {
		probeName := "Stats/TimeRange/" + window.Label
		if matchesFilter(probeName, options.FilterPattern) {
			rangeProbe, rangeError := profileTimeRangeWindow(database, proj, recorder, window, nil, probeName, options.Iterations)
			if rangeError != nil {
				return nil, nil, rangeError
			}
			results = append(results, rangeProbe)
			if window.MetricEvents > 0 {
				computePoints = append(computePoints, ScalingPoint{
					WindowLabel: window.Label,
					Items:       window.MetricEvents,
					ItemUnit:    "metric_events",
					ColdTime:    rangeProbe.Cold.MedianDuration,
					WarmTime:    rangeProbe.Warm.MedianDuration,
					NsPerItem:   rangeProbe.Cold.NormalizedNsPerItem,
				})
			}
		}
	}

	filteredProbe, hasFiltered, filteredError := profileModeFilteredTimeRange(database, proj, recorder, dataset, options)
	if filteredError != nil {
		return nil, nil, filteredError
	}
	if hasFiltered {
		results = append(results, filteredProbe)
	}
	return results, computePoints, nil
}

func profileTimeRangeWindow(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	window RepresentativeWindow,
	filter *projstats.Filter,
	probeName string,
	iterations int,
) (ProbeResult, error) {
	timeRange := projstats.Range{After: window.AfterTimestamp, Until: window.UntilTimestamp}
	description := fmt.Sprintf(
		"Compute windowed section projections over %s (%s..%s, Cold miss vs. Warm KeyedCache)",
		window.Label,
		shortTimestamp(window.AfterTimestamp),
		shortTimestamp(window.UntilTimestamp),
	)
	var warmSetupError error
	prepareCold := func() {
		proj.invalidate()
		warmSetupError = proj.warmBaseProjections()
	}

	return runColdWarmProbe(
		database,
		recorder,
		probeName,
		"Stats",
		description,
		"stats.(*TimeRangeStats).Compute",
		"metric_events",
		"discoveries",
		iterations,
		prepareCold,
		func() (singleExecOutcome, error) {
			if warmSetupError != nil {
				return singleExecOutcome{}, warmSetupError
			}
			telemetrySummary, computeError := recordTimeRangeSections(recorder, proj.timeRangeStats, timeRange, filter, window)
			if computeError != nil {
				return singleExecOutcome{}, computeError
			}
			return buildTelemetryExecOutcome(telemetrySummary, max(window.MetricEvents, 1)), nil
		},
	)
}

func recordTimeRangeSections(
	recorder *treeRecorder,
	timeRangeStats *projstats.TimeRangeStats,
	timeRange projstats.Range,
	filter *projstats.Filter,
	window RepresentativeWindow,
) (projstats.TelemetrySummary, error) {
	var (
		travelMetrics        projstats.TravelMetrics
		cartographicsMetrics projstats.CartographicsMetrics
		exobiologyMetrics    projstats.ExobiologyMetrics
		realizationBreakdown projstats.RealizationBreakdown
	)
	steps := []struct {
		label, location, unit string
		items                 int
		load                  func() error
	}{
		{"TimeRangeTravel", "internal/projections/stats/time_range_travel.go:61", "events", window.MetricEvents, func() (loadError error) {
			travelMetrics, loadError = timeRangeStats.Travel().Compute(timeRange, filter)
			return loadError
		}},
		{"TimeRangeCartographics", "internal/projections/stats/time_range_cartographics.go:57", "events", window.MetricEvents, func() (loadError error) {
			cartographicsMetrics, loadError = timeRangeStats.Cartographics().Compute(timeRange, filter)
			return loadError
		}},
		{"TimeRangeExobiology", "internal/projections/stats/time_range_exobiology.go:61", "events", window.MetricEvents, func() (loadError error) {
			exobiologyMetrics, loadError = timeRangeStats.Exobiology().Compute(timeRange, filter)
			return loadError
		}},
		{"TimeRangeRealization", "internal/projections/stats/time_range_realization.go:61", "real_events", window.RealEvents, func() (loadError error) {
			realizationBreakdown, loadError = timeRangeStats.Realization().Compute(timeRange, filter)
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
	cartographicsMetrics.ApplyTo(&combinedMetrics)
	exobiologyMetrics.ApplyTo(&combinedMetrics)
	return projstats.TelemetrySummary{Metrics: combinedMetrics, Realization: realizationBreakdown}, nil
}

func profileModeFilteredTimeRange(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) (ProbeResult, bool, error) {
	probeName := "Stats/TimeRange/ExpeditionModeFiltered"
	if !matchesFilter(probeName, options.FilterPattern) || len(dataset.Windows) == 0 {
		return ProbeResult{}, false, nil
	}
	targetWindow := selectExpeditionOrFallbackWindow(dataset)
	modeList, listError := modes.List(database)
	if listError != nil || len(modeList) == 0 {
		return ProbeResult{}, false, listError
	}
	modeFilter := &projstats.Filter{
		Enabled:      true,
		ExpeditionID: dataset.PrimaryExpeditionID,
		Modes: []projstats.ModeSelection{
			{ModeID: modeList[0].ID, Travel: true, Discovery: true},
		},
	}
	probeResult, probeError := profileTimeRangeWindow(database, proj, recorder, targetWindow, modeFilter, probeName, options.Iterations)
	return probeResult, true, probeError
}

func runLiveInvalidationProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, error) {
	var results []ProbeResult
	targetWindow := selectExpeditionOrFallbackWindow(dataset)
	expeditionRange := projstats.Range{After: targetWindow.AfterTimestamp, Until: targetWindow.UntilTimestamp}
	hasWindow := len(dataset.Windows) > 0

	if matchesFilter("Stats/LiveInvalidation/FSDJump", options.FilterPattern) {
		jumpProbe, jumpError := profileLiveJournalInvalidation(
			database,
			proj,
			recorder,
			dataset,
			expeditionRange,
			hasWindow,
			"Stats/LiveInvalidation/FSDJump",
			"Refresh CareerStats and active TimeRangeStats after a single FSDJump invalidation",
			journal.EventFSDJump,
			options.Iterations,
		)
		if jumpError != nil {
			return nil, jumpError
		}
		results = append(results, jumpProbe)
	}

	if matchesFilter("Stats/LiveInvalidation/Scan", options.FilterPattern) {
		scanProbe, scanError := profileLiveJournalInvalidation(
			database,
			proj,
			recorder,
			dataset,
			expeditionRange,
			hasWindow,
			"Stats/LiveInvalidation/Scan",
			"Refresh CareerStats and active TimeRangeStats after a single body Scan invalidation",
			journal.EventScan,
			options.Iterations,
		)
		if scanError != nil {
			return nil, scanError
		}
		results = append(results, scanProbe)
	}
	return results, nil
}

func profileLiveJournalInvalidation(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	expeditionRange projstats.Range,
	hasExpeditionRange bool,
	probeName string,
	description string,
	triggerEvent journal.EventType,
	iterations int,
) (ProbeResult, error) {
	var prepError error
	beforeEach := func() {
		prepError = proj.prepareJournalInvalidation(expeditionRange, hasExpeditionRange, triggerEvent)
	}
	return runSingleModeProbe(
		database,
		recorder,
		probeName,
		"Stats",
		description,
		"stats.RefreshAfterJournalEvent",
		"metric_events",
		"discoveries",
		iterations,
		beforeEach,
		func() (singleExecOutcome, error) {
			if prepError != nil {
				return singleExecOutcome{}, prepError
			}
			var careerSummary projstats.TelemetrySummary
			if stepError := recorder.withSubSpan("CareerStats.Get", "internal/projections/stats/career_stats.go:94", max(dataset.MetricEvents, 1), "events", func() error {
				var getError error
				careerSummary, getError = proj.careerStats.Get()
				return getError
			}); stepError != nil {
				return singleExecOutcome{}, stepError
			}
			if hasExpeditionRange {
				if stepError := recorder.withSubSpan("TimeRangeStats.Compute", "internal/projections/stats/time_range_stats.go:95", 1, "window", func() error {
					_, computeError := proj.timeRangeStats.Compute(expeditionRange, nil)
					return computeError
				}); stepError != nil {
					return singleExecOutcome{}, stepError
				}
			}
			return buildTelemetryExecOutcome(careerSummary, max(dataset.MetricEvents, 1)), nil
		},
	)
}

func buildTelemetryExecOutcome(telemetrySummary projstats.TelemetrySummary, processedItems int) singleExecOutcome {
	metrics := telemetrySummary.Metrics
	realizationBreakdown := telemetrySummary.Realization
	summary := fmt.Sprintf(
		"Jumps=%d Dist=%.1fLy Scans=%d Mapped=%d Carto=%d Orgs=%d Exo=%d Realized=%d",
		metrics.Jumps,
		metrics.DistanceLy,
		metrics.BodiesScanned,
		metrics.BodiesMapped,
		metrics.ScanValueTotal,
		metrics.OrganicSamples,
		metrics.OrganicValueTotal,
		realizationBreakdown.TotalRealized,
	)
	return singleExecOutcome{
		value:     telemetrySummary,
		summary:   summary,
		items:     processedItems,
		secondary: metrics.BodiesScanned + metrics.OrganicSamples,
	}
}

func selectExpeditionOrFallbackWindow(dataset DatasetSummary) RepresentativeWindow {
	if len(dataset.Windows) == 0 {
		return RepresentativeWindow{Label: "AllTime", UntilTimestamp: "9999-12-31T23:59:59Z"}
	}
	targetWindow := dataset.Windows[len(dataset.Windows)-1]
	for _, candidateWindow := range dataset.Windows {
		if candidateWindow.Label == "ExpeditionWindow" {
			return candidateWindow
		}
	}
	return targetWindow
}

func computeLinearScalingModel(functionName, itemUnit string, points []ScalingPoint) ScalingModel {
	smallest := points[0]
	largest := points[len(points)-1]
	for _, point := range points {
		if point.Items < smallest.Items {
			smallest = point
		}
		if point.Items > largest.Items {
			largest = point
		}
	}
	var marginalNsPerItem float64
	var fixedSetup time.Duration
	if largest.Items > smallest.Items {
		deltaNs := float64((largest.ColdTime - smallest.ColdTime).Nanoseconds())
		deltaItems := float64(largest.Items - smallest.Items)
		if deltaNs > 0 {
			marginalNsPerItem = deltaNs / deltaItems
		}
		estimatedVariableSmallest := time.Duration(math.Round(marginalNsPerItem * float64(smallest.Items)))
		if smallest.ColdTime > estimatedVariableSmallest {
			fixedSetup = smallest.ColdTime - estimatedVariableSmallest
		}
	} else if largest.Items > 0 {
		marginalNsPerItem = float64(largest.ColdTime.Nanoseconds()) / float64(largest.Items)
	}
	var marginalItemsPerSec float64
	if marginalNsPerItem > 0 {
		marginalItemsPerSec = 1e9 / marginalNsPerItem
	}
	return ScalingModel{
		FunctionName:        functionName,
		ItemUnit:            itemUnit,
		FixedSetupOverhead:  fixedSetup,
		MarginalNsPerItem:   marginalNsPerItem,
		MarginalItemsPerSec: marginalItemsPerSec,
		Points:              points,
	}
}

func shortTimestamp(isoTimestamp string) string {
	if isoTimestamp == "" {
		return "start"
	}
	if strings.HasPrefix(isoTimestamp, "9999-") {
		return "now"
	}
	if len(isoTimestamp) >= 10 {
		return isoTimestamp[:10]
	}
	return isoTimestamp
}
