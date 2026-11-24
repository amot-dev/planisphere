package exporter

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

// Sentinel errors returned when export preconditions are not met.
var (
	ErrNilDatabase        = errors.New("database connection is required for export")
	ErrMissingProjections = errors.New("expedition projections are required for export")
	ErrNotExportable      = errors.New("expedition must depart waypoint #1 or be completed before exporting")
)

// Build assembles the complete canonical ExpeditionExport snapshot for expeditionID.
func Build(databaseConnection *sql.DB, expeditionID int64, dependencies BuilderDeps) (ExpeditionExport, error) {
	if validationError := validateBuilderDeps(databaseConnection, dependencies); validationError != nil {
		return ExpeditionExport{}, validationError
	}
	currentTime := resolveNow(dependencies.Now)
	planView, planError := expedition.BuildPlan(databaseConnection, expeditionID, dependencies.GECLookup, dependencies.Projections)
	if planError != nil {
		return ExpeditionExport{}, fmt.Errorf("build plan for export %d: %w", expeditionID, planError)
	}
	expeditionRecord := planView.Expedition
	queryAfter, displayStart, departed := expedition.EffectiveStartFromRows(expeditionRecord, planView.Rows)
	if expeditionRecord.Status == expedition.StatusPlanning || (!departed && expeditionRecord.Status != expedition.StatusCompleted) {
		return ExpeditionExport{}, ErrNotExportable
	}
	enabledCommanders, commandersError := replay.ListEnabledCommanderNames(databaseConnection)
	if commandersError != nil {
		return ExpeditionExport{}, fmt.Errorf("list enabled commanders for export %d: %w", expeditionID, commandersError)
	}
	exportSnapshot := initializeExportHeader(databaseConnection, expeditionRecord, planView, enabledCommanders, dependencies, currentTime, displayStart, departed)

	if populateError := populateExpeditionTelemetry(databaseConnection, expeditionRecord, queryAfter, displayStart, departed, currentTime, dependencies, &exportSnapshot); populateError != nil {
		return ExpeditionExport{}, populateError
	}
	if detailsError := populateRowDetailsAndBaseline(databaseConnection, planView, dependencies.Projections, &exportSnapshot); detailsError != nil {
		return ExpeditionExport{}, detailsError
	}
	if bucketsError := populateModeBuckets(planView, queryAfter, departed, dependencies.Projections, &exportSnapshot); bucketsError != nil {
		return ExpeditionExport{}, bucketsError
	}
	exportSnapshot.HighlightsFullOrder = ComputeHighlightLeaderboards(planView, exportSnapshot.RowDetails, currentTime)
	return exportSnapshot, nil
}

func resolveNow(nowProvider func() time.Time) time.Time {
	if nowProvider != nil {
		return nowProvider().UTC()
	}
	return time.Now().UTC()
}

func initializeExportHeader(
	databaseConnection *sql.DB,
	expeditionRecord expedition.Expedition,
	planView expedition.PlanView,
	commanders []string,
	dependencies BuilderDeps,
	currentTime time.Time,
	displayStart string,
	departed bool,
) ExpeditionExport {
	effectiveEnd := expeditionRecord.EndTimestamp
	endIsNow := effectiveEnd == ""
	effectiveEndGalactic := reference.ToGalactic(effectiveEnd)
	if endIsNow {
		effectiveEndGalactic = reference.TimeToGalactic(currentTime)
	}
	careerStart, careerStartGalactic := replay.CareerStart(databaseConnection)
	careerDuration := ""
	if careerStart != "" {
		careerDuration = reference.Elapsed(careerStart, "")
	}
	durationLabel := ""
	if departed && displayStart != "" {
		durationLabel = reference.Elapsed(displayStart, effectiveEnd)
	}
	return ExpeditionExport{
		SchemaVersion:          SchemaVersion,
		ExportedAt:             reference.FormatISO(currentTime),
		ExportedAtGalactic:     reference.TimeToGalactic(currentTime),
		AppVersion:             dependencies.AppVersion,
		Commanders:             commanders,
		Departed:               departed,
		EffectiveStart:         displayStart,
		EffectiveStartGalactic: reference.ToGalactic(displayStart),
		EffectiveEnd:           effectiveEnd,
		EffectiveEndGalactic:   effectiveEndGalactic,
		EndIsNow:               endIsNow,
		Duration:               durationLabel,
		CareerStart:            careerStart,
		CareerStartGalactic:    careerStartGalactic,
		CareerDuration:         careerDuration,
		Plan:                   planView,
		Adjustments:            expeditionRecord.Adjustments.ToMetrics(),
		ModeBuckets:            []ModeBucket{},
		RowDetails:             []expedition.WaypointDetails{},
		HighlightsFullOrder:    []HighlightLeaderboard{},
	}
}

func populateExpeditionTelemetry(
	databaseConnection *sql.DB,
	expeditionRecord expedition.Expedition,
	queryAfter, displayStart string,
	departed bool,
	currentTime time.Time,
	dependencies BuilderDeps,
	exportSnapshot *ExpeditionExport,
) error {
	manualAdjustments := expeditionRecord.Adjustments.ToMetrics()
	if departed {
		expeditionRange := replaystats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
		expeditionSummary, computeError := dependencies.Projections.TimeRangeStats.Compute(expeditionRange, nil)
		if computeError != nil {
			return fmt.Errorf("compute expedition telemetry: %w", computeError)
		}
		exportSnapshot.Live = expeditionSummary.Metrics
		exportSnapshot.Expedition = TelemetrySnapshot{
			Metrics:     expeditionSummary.Metrics.Add(manualAdjustments),
			Realization: expeditionSummary.Realization,
		}
	} else {
		exportSnapshot.Expedition = TelemetrySnapshot{Metrics: manualAdjustments}
	}

	exportSnapshot.CarrierCost = expedition.ComputeCarrierCost(databaseConnection, expeditionRecord, exportSnapshot.Expedition.Metrics, displayStart, departed, currentTime)
	return nil
}

func populateRowDetailsAndBaseline(
	databaseConnection *sql.DB,
	planView expedition.PlanView,
	expeditionProjections *expedition.Projections,
	exportSnapshot *ExpeditionExport,
) error {
	originBaseline, rowDetails, detailsError := expedition.DetailsBatch(databaseConnection, planView, expeditionProjections)
	if detailsError != nil {
		return fmt.Errorf("compute waypoint details batch: %w", detailsError)
	}
	exportSnapshot.OriginBaseline = originBaseline
	exportSnapshot.RowDetails = rowDetails
	return nil
}

func populateModeBuckets(
	planView expedition.PlanView,
	queryAfter string,
	departed bool,
	expeditionProjections *expedition.Projections,
	exportSnapshot *ExpeditionExport,
) error {
	if !departed {
		return nil
	}
	candidateModes, singleCoveringMode := expedition.SelectActiveModes(planView)
	if singleCoveringMode {
		if hasTelemetryActivity(exportSnapshot.Live, exportSnapshot.Expedition.Realization) {
			singleMode := candidateModes[0]
			exportSnapshot.ModeBuckets = []ModeBucket{{
				ModeID:      singleMode.ID,
				ModeName:    singleMode.Name,
				ModeColor:   singleMode.Color,
				Metrics:     exportSnapshot.Live,
				Realization: exportSnapshot.Expedition.Realization,
			}}
		}
		return nil
	}
	buckets, computeError := computeFilteredModeBuckets(planView.Expedition, queryAfter, candidateModes, expeditionProjections)
	if computeError != nil {
		return computeError
	}
	exportSnapshot.ModeBuckets = buckets
	return nil
}

func computeFilteredModeBuckets(
	expeditionRecord expedition.Expedition,
	queryAfter string,
	candidateModes []modes.Mode,
	expeditionProjections *expedition.Projections,
) ([]ModeBucket, error) {
	expeditionRange := replaystats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
	buckets := make([]ModeBucket, 0, len(candidateModes))
	for _, modeItem := range candidateModes {
		modeFilter := replaystats.Filter{
			Enabled:      true,
			ExpeditionID: expeditionRecord.ID,
			Modes:        []replaystats.ModeSelection{{ModeID: modeItem.ID, Travel: true, Discovery: true}},
		}
		summary, computeError := expeditionProjections.TimeRangeStats.Compute(expeditionRange, &modeFilter)
		if computeError != nil {
			return nil, fmt.Errorf("compute mode bucket %q: %w", modeItem.Name, computeError)
		}
		if !hasTelemetryActivity(summary.Metrics, summary.Realization) {
			continue
		}
		buckets = append(buckets, ModeBucket{
			ModeID:      modeItem.ID,
			ModeName:    modeItem.Name,
			ModeColor:   modeItem.Color,
			Metrics:     summary.Metrics,
			Realization: summary.Realization,
		})
	}
	return buckets, nil
}

func hasTelemetryActivity(metrics replaystats.Metrics, realization replaystats.RealizationBreakdown) bool {
	return metrics.DistanceLy > 0 ||
		metrics.Jumps > 0 ||
		metrics.BodiesScanned > 0 ||
		metrics.BodiesMapped > 0 ||
		metrics.OrganicSamples > 0 ||
		metrics.ScanValueTotal > 0 ||
		metrics.OrganicValueTotal > 0 ||
		realization.TotalRealized > 0 ||
		realization.PendingValue > 0 ||
		realization.CartoLostDeath > 0 ||
		realization.OrganicLostDeath > 0
}
