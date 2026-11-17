package profile

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"planisphere/internal/projections/stats"
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
	var computePoints []ScalingPoint

	for _, window := range dataset.Windows {
		probeName := "Stats/Compute/" + window.Label
		if matchesFilter(probeName, options.FilterPattern) {
			computeProbe, err := profileStatsComputeWindow(database, proj, recorder, window, nil, probeName, options.Iterations)
			if err != nil {
				return nil, nil, err
			}
			results = append(results, computeProbe)
			if window.MetricEvents > 0 {
				computePoints = append(computePoints, ScalingPoint{
					WindowLabel: window.Label,
					Items:       window.MetricEvents,
					ItemUnit:    "metric_events",
					ColdTime:    computeProbe.Cold.MedianDuration,
					WarmTime:    computeProbe.Warm.MedianDuration,
					NsPerItem:   computeProbe.Warm.NormalizedNsPerItem,
				})
			}
		}

		realProbeName := "Stats/ComputeRealization/" + window.Label
		if matchesFilter(realProbeName, options.FilterPattern) {
			realProbe, err := profileStatsRealizationWindow(database, proj, recorder, window, realProbeName, options.Iterations)
			if err != nil {
				return nil, nil, err
			}
			results = append(results, realProbe)
		}
	}

	if filteredProbe, ok, err := profileModeFilteredCompute(database, proj, recorder, dataset, options); err != nil {
		return nil, nil, err
	} else if ok {
		results = append(results, filteredProbe)
	}

	var scalingModels []ScalingModel
	if len(computePoints) >= 2 {
		scalingModels = append(scalingModels, computeLinearScalingModel("stats.Engine.Compute (Warm Ledger)", "metric_events", computePoints))
	}
	return results, scalingModels, nil
}

func profileStatsComputeWindow(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	window RepresentativeWindow,
	filter *stats.Filter,
	probeName string,
	iterations int,
) (ProbeResult, error) {
	timeRange := stats.Range{After: window.AfterTimestamp, Until: window.UntilTimestamp}
	description := fmt.Sprintf("Compute metrics over %s window (%s..%s)", window.Label, shortTimestamp(window.AfterTimestamp), shortTimestamp(window.UntilTimestamp))

	return runColdWarmProbe(
		database,
		recorder,
		probeName,
		"Stats",
		description,
		"stats.(*Engine).Compute",
		"metric_events",
		"discoveries",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			statsEngine := proj.statsEngine()
			var filterCopy *stats.Filter
			if filter != nil {
				copied := *filter
				filterCopy = &copied
			}
			metrics, computeError := statsEngine.Compute(timeRange, filterCopy)
			if computeError != nil {
				return singleExecOutcome{}, computeError
			}
			summary := fmt.Sprintf(
				"Jumps=%d Dist=%.1fLy Scans=%d Mapped=%d Carto=%d Orgs=%d Exo=%d",
				metrics.Jumps,
				metrics.DistanceLy,
				metrics.BodiesScanned,
				metrics.BodiesMapped,
				metrics.ScanValueTotal,
				metrics.OrganicSamples,
				metrics.OrganicValueTotal,
			)
			processedCount := max(window.MetricEvents, 1)
			return singleExecOutcome{
				value:     metrics,
				summary:   summary,
				items:     processedCount,
				secondary: metrics.BodiesScanned + metrics.OrganicSamples,
			}, nil
		},
	)
}

func profileStatsRealizationWindow(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	window RepresentativeWindow,
	probeName string,
	iterations int,
) (ProbeResult, error) {
	timeRange := stats.Range{After: window.AfterTimestamp, Until: window.UntilTimestamp}
	description := fmt.Sprintf("Compute realization breakdown over %s window", window.Label)

	return runColdWarmProbe(
		database,
		recorder,
		probeName,
		"Stats",
		description,
		"stats.(*Engine).ComputeRealization",
		"real_events",
		"credits",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			statsEngine := proj.statsEngine()
			breakdown, realError := statsEngine.ComputeRealization(timeRange, nil)
			if realError != nil {
				return singleExecOutcome{}, realError
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
			processedCount := max(window.RealEvents, 1)
			return singleExecOutcome{
				value:     breakdown,
				summary:   summary,
				items:     processedCount,
				secondary: int(breakdown.TotalRealized / 1_000_000),
			}, nil
		},
	)
}

func profileModeFilteredCompute(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) (ProbeResult, bool, error) {
	probeName := "Stats/Compute/ExpeditionModeFiltered"
	if !matchesFilter(probeName, options.FilterPattern) || len(dataset.Windows) == 0 {
		return ProbeResult{}, false, nil
	}
	targetWindow := dataset.Windows[len(dataset.Windows)-1]
	for _, candidateWindow := range dataset.Windows {
		if candidateWindow.Label == "ExpeditionWindow" {
			targetWindow = candidateWindow
			break
		}
	}
	modeList, listError := modes.List(database)
	if listError != nil || len(modeList) == 0 {
		return ProbeResult{}, false, listError
	}
	modeFilter := &stats.Filter{
		Enabled: true,
		Modes: []stats.ModeSelection{
			{ModeID: modeList[0].ID, Travel: true, Discovery: true},
		},
	}
	result, err := profileStatsComputeWindow(database, proj, recorder, targetWindow, modeFilter, probeName, options.Iterations)
	return result, true, err
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
		deltaNs := float64((largest.WarmTime - smallest.WarmTime).Nanoseconds())
		deltaItems := float64(largest.Items - smallest.Items)
		if deltaNs > 0 {
			marginalNsPerItem = deltaNs / deltaItems
		}
		estimatedVariableSmallest := time.Duration(math.Round(marginalNsPerItem * float64(smallest.Items)))
		if smallest.WarmTime > estimatedVariableSmallest {
			fixedSetup = smallest.WarmTime - estimatedVariableSmallest
		}
	} else if largest.Items > 0 {
		marginalNsPerItem = float64(largest.WarmTime.Nanoseconds()) / float64(largest.Items)
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

