package profile

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"planisphere/internal/exporter"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/gec"
	"planisphere/internal/store/expedition"
)

func runExpeditionProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, error) {
	if dataset.PrimaryExpeditionID == 0 {
		return nil, nil
	}
	var results []ProbeResult
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

	if matchesFilter("Expedition/BuildPlan", options.FilterPattern) {
		planProbe, err := profileBuildPlan(database, proj, recorder, dataset, gecLookup, options.Iterations)
		if err != nil {
			return nil, err
		}
		results = append(results, planProbe)
	}

	if dataset.OriginRowID > 0 && matchesFilter("Expedition/Details/OriginWaypoint", options.FilterPattern) {
		originProbe, err := profileWaypointDetails(
			database,
			proj,
			recorder,
			"Expedition/Details/OriginWaypoint",
			"Compute Waypoint #1 (origin baseline) drawer telemetry",
			dataset.OriginRowID,
			options.Iterations,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, originProbe)
	}

	if dataset.SampleVisitedLegRowID > 0 && matchesFilter("Expedition/Details/SingleVisitedLeg", options.FilterPattern) {
		legProbe, err := profileWaypointDetails(
			database,
			proj,
			recorder,
			"Expedition/Details/SingleVisitedLeg",
			fmt.Sprintf("Compute visited leg #%d drawer telemetry", dataset.SampleVisitedLegNumber),
			dataset.SampleVisitedLegRowID,
			options.Iterations,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, legProbe)
	}

	if dataset.PrimaryVisitedCount > 1 && matchesFilter("Expedition/Details/VisitedLegsBatch", options.FilterPattern) {
		batchProbe, ok, err := profileVisitedLegsBatch(database, proj, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		if ok {
			results = append(results, batchProbe)
		}
	}

	if matchesFilter("Expedition/Reevaluate", options.FilterPattern) {
		reevalProbe, err := profileExpeditionReevaluate(database, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		results = append(results, reevalProbe)
	}

	if dataset.SampleVisitedLegRowID > 0 && matchesFilter("Expedition/RowDropPreviews", options.FilterPattern) {
		dropProbe, ok, err := profileRowDropPreviews(database, proj, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		if ok {
			results = append(results, dropProbe)
		}
	}

	if matchesFilter("Expedition/StartCandidates", options.FilterPattern) {
		candidatesProbe, ok, err := profileStartCandidates(database, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		if ok {
			results = append(results, candidatesProbe)
		}
	}

	if matchesFilter("Export/Build", options.FilterPattern) {
		exportProbe, ok, exportError := profileExportBuild(database, proj, recorder, dataset, gecLookup, options.Iterations)
		if exportError != nil {
			return nil, exportError
		}
		if ok {
			results = append(results, exportProbe)
		}
	}

	return results, nil
}

func profileBuildPlan(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	gecLookup expedition.GECLookup,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Expedition/BuildPlan",
		"Expedition",
		fmt.Sprintf("Assemble PlanView for expedition #%d (%s)", dataset.PrimaryExpeditionID, dataset.PrimaryExpeditionName),
		"expedition.BuildPlan",
		"rows",
		"visited",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			planView, buildError := expedition.BuildPlan(database, dataset.PrimaryExpeditionID, gecLookup, proj.expeditionProjections())
			if buildError != nil {
				return singleExecOutcome{}, buildError
			}
			summary := fmt.Sprintf(
				"Rows=%d Waypoints=%d Visited=%d Planned=%.1fLy Covered=%.1fLy Remaining=%.1fLy",
				len(planView.Rows),
				planView.WaypointCount,
				planView.VisitedWaypointCount,
				planView.PlannedLy,
				planView.CoveredLy,
				planView.RemainingLy,
			)
			return singleExecOutcome{
				value:     planView,
				summary:   summary,
				items:     max(len(planView.Rows), 1),
				secondary: planView.VisitedWaypointCount,
			}, nil
		},
	)
}

func profileWaypointDetails(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	probeName string,
	description string,
	rowID int64,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		probeName,
		"Expedition",
		description,
		"expedition.Details",
		"jumps",
		"scans",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			details, detailsError := expedition.Details(database, rowID, proj.expeditionProjections())
			if detailsError != nil {
				return singleExecOutcome{}, detailsError
			}
			summary := fmt.Sprintf(
				"Kind=%s #%d Jumps=%d Dist=%.1fLy Scans=%d Carto=%d Realized=%d",
				details.Kind,
				details.Number,
				details.Metrics.Jumps,
				details.Metrics.DistanceLy,
				details.Metrics.BodiesScanned,
				details.Metrics.ScanValueTotal,
				details.Realization.TotalRealized,
			)
			return singleExecOutcome{
				value:     details,
				summary:   summary,
				items:     max(details.Metrics.Jumps+details.Metrics.BodiesScanned, 1),
				secondary: details.Metrics.BodiesScanned,
			}, nil
		},
	)
}

func profileVisitedLegsBatch(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, bool, error) {
	rows, listError := expedition.ListRows(database, dataset.PrimaryExpeditionID)
	if listError != nil {
		return ProbeResult{}, false, listError
	}
	var visitedRowIDs []int64
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() && rowItem.VisitedAt != "" {
			visitedRowIDs = append(visitedRowIDs, rowItem.ID)
			if len(visitedRowIDs) >= 12 {
				break
			}
		}
	}
	if len(visitedRowIDs) == 0 {
		return ProbeResult{}, false, nil
	}

	probe, err := runColdWarmProbe(
		database,
		recorder,
		"Expedition/Details/VisitedLegsBatch",
		"Expedition",
		fmt.Sprintf("Expand %d visited waypoint drawers sequentially", len(visitedRowIDs)),
		"expedition.Details[Batch]",
		"drawers",
		"jumps",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			var totalJumps int
			var totalCarto int64
			var lastDetails expedition.WaypointDetails
			for idx, targetRowID := range visitedRowIDs {
				label := fmt.Sprintf("expedition.Details[Leg #%d]", idx+1)
				var callError error
				if spanError := recorder.withSubSpan(label, "internal/store/expedition/waypoint_details.go:82", 1, "drawer", func() error {
					lastDetails, callError = expedition.Details(database, targetRowID, proj.expeditionProjections())
					return callError
				}); spanError != nil {
					return singleExecOutcome{}, spanError
				}
				totalJumps += lastDetails.Metrics.Jumps
				totalCarto += lastDetails.Metrics.ScanValueTotal
			}
			summary := fmt.Sprintf("Drawers=%d TotalJumps=%d TotalCarto=%d", len(visitedRowIDs), totalJumps, totalCarto)
			return singleExecOutcome{
				value:     totalCarto,
				summary:   summary,
				items:     len(visitedRowIDs),
				secondary: totalJumps,
			}, nil
		},
	)
	return probe, true, err
}

func profileExpeditionReevaluate(
	database *sql.DB,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runSingleModeProbe(
		database,
		recorder,
		"Expedition/Reevaluate",
		"Expedition",
		fmt.Sprintf("Replay journal arrivals for expedition #%d inside rolled-back tx", dataset.PrimaryExpeditionID),
		"expedition.Reevaluate",
		"waypoints",
		"visited",
		iterations,
		nil,
		func() (singleExecOutcome, error) {
			transaction, beginError := database.Begin()
			if beginError != nil {
				return singleExecOutcome{}, beginError
			}
			defer func() {
				if rollbackError := transaction.Rollback(); rollbackError != nil {
					return
				}
			}()

			changed, reevalError := expedition.Reevaluate(transaction, dataset.PrimaryExpeditionID)
			if reevalError != nil {
				return singleExecOutcome{}, reevalError
			}
			summary := fmt.Sprintf("Waypoints=%d Visited=%d Changed=%v", dataset.PrimaryWaypointCount, dataset.PrimaryVisitedCount, changed)
			return singleExecOutcome{
				value:     changed,
				summary:   summary,
				items:     max(dataset.PrimaryWaypointCount, 1),
				secondary: dataset.PrimaryVisitedCount,
			}, nil
		},
	)
}

func profileStartCandidates(
	database *sql.DB,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, bool, error) {
	rows, listError := expedition.ListRows(database, dataset.PrimaryExpeditionID)
	if listError != nil {
		return ProbeResult{}, false, listError
	}
	hasOriginSystem := false
	for _, rowItem := range rows {
		if rowItem.IsWaypoint() {
			hasOriginSystem = rowItem.SystemName != ""
			break
		}
	}
	if !hasOriginSystem {
		return ProbeResult{}, false, nil
	}

	probe, probeError := runSingleModeProbe(
		database,
		recorder,
		"Expedition/StartCandidates",
		"Expedition",
		"Query historical arrivals at Waypoint #1",
		"expedition.StartCandidates",
		"arrivals",
		"candidates",
		iterations,
		nil,
		func() (singleExecOutcome, error) {
			systemName, candidates, candError := expedition.StartCandidates(database, dataset.PrimaryExpeditionID)
			if candError != nil {
				return singleExecOutcome{}, candError
			}
			summary := fmt.Sprintf("Origin=%q Candidates=%d", systemName, len(candidates))
			return singleExecOutcome{
				value:     candidates,
				summary:   summary,
				items:     max(len(candidates), 1),
				secondary: len(candidates),
			}, nil
		},
	)
	return probe, true, probeError
}

func profileRowDropPreviews(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, bool, error) {
	initialPreviews, initialError := expedition.ComputeRowDropPreviews(database, dataset.SampleVisitedLegRowID, proj.expeditionProjections())
	if initialError != nil || len(initialPreviews) == 0 {
		return ProbeResult{}, false, initialError
	}

	probe, err := runColdWarmProbe(
		database,
		recorder,
		"Expedition/RowDropPreviews",
		"Expedition",
		fmt.Sprintf("Compute chronological drop-slot previews for leg #%d", dataset.SampleVisitedLegNumber),
		"expedition.ComputeRowDropPreviews",
		"slots",
		"valid",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			previews, computeError := expedition.ComputeRowDropPreviews(database, dataset.SampleVisitedLegRowID, proj.expeditionProjections())
			if computeError != nil {
				return singleExecOutcome{}, computeError
			}
			validCount := 0
			for _, preview := range previews {
				if preview.Status == expedition.DropSlotValidVisited || preview.Status == expedition.DropSlotValidUnvisited {
					validCount++
				}
			}
			summary := fmt.Sprintf("Slots=%d Valid=%d", len(previews), validCount)
			return singleExecOutcome{
				value:     previews,
				summary:   summary,
				items:     max(len(previews), 1),
				secondary: validCount,
			}, nil
		},
	)
	return probe, true, err
}

type exportBuildProbeValue struct {
	EffectiveStart    string                     `json:"effectiveStart"`
	EffectiveEnd      string                     `json:"effectiveEnd"`
	ExportedAt        string                     `json:"exportedAt"`
	Commanders        []string                   `json:"commanders"`
	Departed          bool                       `json:"departed"`
	EndIsNow          bool                       `json:"endIsNow"`
	RowDetailsCount   int                        `json:"rowDetailsCount"`
	ModeBucketsCount  int                        `json:"modeBucketsCount"`
	HasOriginBaseline bool                       `json:"hasOriginBaseline"`
	Live              replaystats.Metrics        `json:"live"`
	Expedition        exporter.TelemetrySnapshot `json:"expedition"`
	PostCareer        replaystats.Metrics        `json:"postCareer"`
	CarrierCostTotal  int64                      `json:"carrierCostTotal"`
}

func profileExportNow() time.Time {
	return time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
}

func profileExportBuild(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	gecLookup expedition.GECLookup,
	iterations int,
) (ProbeResult, bool, error) {
	probe, probeError := runColdWarmProbe(
		database, recorder, "Export/Build", "Export",
		fmt.Sprintf("Assemble canonical export snapshot for expedition #%d (%s)", dataset.PrimaryExpeditionID, dataset.PrimaryExpeditionName),
		"exporter.Build", "rows", "legs", iterations, proj.invalidate,
		func() (singleExecOutcome, error) {
			snapshot, buildError := exporter.Build(database, dataset.PrimaryExpeditionID, exporter.BuilderDeps{
				Projections: proj.expeditionProjections(),
				CareerStats: proj.careerStats,
				GECLookup:   gecLookup,
				AppVersion:  "profile",
				Now:         profileExportNow,
			})
			if buildError != nil {
				return singleExecOutcome{}, buildError
			}
			return buildExportProbeOutcome(snapshot), nil
		},
	)
	if errors.Is(probeError, exporter.ErrNotExportable) {
		return ProbeResult{}, false, nil
	}
	return probe, probeError == nil, probeError
}

func buildExportProbeOutcome(snapshot exporter.ExpeditionExport) singleExecOutcome {
	digestValue := exportBuildProbeValue{
		EffectiveStart:    snapshot.EffectiveStart,
		EffectiveEnd:      snapshot.EffectiveEnd,
		ExportedAt:        snapshot.ExportedAt,
		Commanders:        snapshot.Commanders,
		Departed:          snapshot.Departed,
		EndIsNow:          snapshot.EndIsNow,
		RowDetailsCount:   len(snapshot.RowDetails),
		ModeBucketsCount:  len(snapshot.ModeBuckets),
		HasOriginBaseline: snapshot.OriginBaseline != nil,
		Live:              snapshot.Live,
		Expedition:        snapshot.Expedition,
		PostCareer:        snapshot.PostCareer.Metrics,
		CarrierCostTotal:  snapshot.CarrierCost.TotalCost,
	}
	summary := fmt.Sprintf("Rows=%d RowDetails=%d ModeBuckets=%d Jumps=%d Realized=%d",
		len(snapshot.Plan.Rows), len(snapshot.RowDetails), len(snapshot.ModeBuckets),
		snapshot.Live.Jumps, snapshot.Expedition.Realization.TotalRealized,
	)
	return singleExecOutcome{value: digestValue, summary: summary, items: max(len(snapshot.Plan.Rows), 1), secondary: len(snapshot.RowDetails)}
}
