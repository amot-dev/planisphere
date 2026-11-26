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
	"planisphere/internal/store/settings"
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
	enabledCommanders, commandersError := replay.ListCommanderNamesForScope(databaseConnection, expeditionRecord.ProfileScope())
	if commandersError != nil {
		return ExpeditionExport{}, fmt.Errorf("list enabled commanders for export %d: %w", expeditionID, commandersError)
	}
	exportSnapshot := initializeExportHeader(databaseConnection, expeditionRecord, planView, enabledCommanders, dependencies, currentTime, displayStart, departed)

	if populateError := populateExpeditionTelemetry(databaseConnection, expeditionRecord, queryAfter, displayStart, departed, currentTime, dependencies, &exportSnapshot); populateError != nil {
		return ExpeditionExport{}, populateError
	}
	if detailsError := populateRowDetailsAndBaseline(databaseConnection, planView, currentTime, dependencies.Projections, &exportSnapshot); detailsError != nil {
		return ExpeditionExport{}, detailsError
	}
	if bucketsError := populateModeBuckets(planView, queryAfter, departed, dependencies.Projections, &exportSnapshot); bucketsError != nil {
		return ExpeditionExport{}, bucketsError
	}
	exportSnapshot.LegTelemetry, exportSnapshot.RegionBuckets = BuildLegAndRegionTelemetry(planView, exportSnapshot.RowDetails, currentTime)
	exportSnapshot.HighlightsFullOrder = ComputeHighlightLeaderboards(planView, exportSnapshot.RowDetails, currentTime)
	exportSnapshot.HighlightsVariants = ComputeHighlightVariants(planView, exportSnapshot.RowDetails)
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
	baselineScope := expeditionRecord.ProfileScope()
	if settings.CareerBaselineScope(databaseConnection) == settings.CareerScopeAllEnabled {
		baselineScope = replay.ProfileScope{}
	}
	careerStart, careerStartGalactic := replay.CareerStartScoped(databaseConnection, baselineScope)
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
		LegTelemetry:           []ExportLegTelemetry{},
		RegionBuckets:          []RegionBucket{},
		HighlightsFullOrder:    []HighlightLeaderboard{},
		HighlightsVariants:     []HighlightLeaderboard{},
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
		expeditionFilter := replaystats.Filter{Scope: expeditionRecord.ProfileScope()}
		expeditionSummary, computeError := dependencies.Projections.TimeRangeStats.Compute(expeditionRange, &expeditionFilter)
		if computeError != nil {
			return fmt.Errorf("compute expedition telemetry: %w", computeError)
		}
		exportSnapshot.Live = expeditionSummary.Metrics
		exportSnapshot.Expedition = TelemetrySnapshot{
			Metrics:     expeditionSummary.Metrics.Add(manualAdjustments),
			Realization: expeditionSummary.Realization,
			ElapsedDays: ComputeElapsedDays(displayStart, expeditionRecord.EndTimestamp, currentTime),
			ShipsFlown:  []string{},
		}
	} else {
		exportSnapshot.Expedition = TelemetrySnapshot{
			Metrics:    manualAdjustments,
			ShipsFlown: []string{},
		}
	}

	exportSnapshot.CarrierCost = expedition.ComputeCarrierCost(databaseConnection, expeditionRecord, exportSnapshot.Expedition.Metrics, displayStart, departed, currentTime)
	return nil
}

func populateRowDetailsAndBaseline(
	databaseConnection *sql.DB,
	planView expedition.PlanView,
	currentTime time.Time,
	expeditionProjections *expedition.Projections,
	exportSnapshot *ExpeditionExport,
) error {
	originBaseline, rowDetails, detailsError := expedition.DetailsBatch(databaseConnection, planView, expeditionProjections)
	if detailsError != nil {
		return fmt.Errorf("compute waypoint details batch: %w", detailsError)
	}
	exportSnapshot.OriginBaseline = originBaseline
	exportSnapshot.RowDetails = rowDetails
	exportSnapshot.Expedition.ShipsFlown = CollectShipsForVisitedLegs(planView, rowDetails, nil)
	exportSnapshot.Expedition.Derived = ComputeDerivedMetrics(
		exportSnapshot.Expedition.Metrics,
		exportSnapshot.Expedition.Realization,
		&exportSnapshot.CarrierCost,
		exportSnapshot.Expedition.ElapsedDays,
	)
	if originBaseline != nil {
		baselineElapsedDays := ComputeElapsedDays(originBaseline.From, originBaseline.To, currentTime)
		baselineDerived := ComputeDerivedMetrics(originBaseline.Metrics, originBaseline.Realization, nil, baselineElapsedDays)
		exportSnapshot.OriginBaselineElapsedDays = baselineElapsedDays
		exportSnapshot.OriginBaselineDerived = &baselineDerived
		exportSnapshot.CareerDiff = BuildCareerDiff(originBaseline, exportSnapshot.Live, exportSnapshot.Expedition.Realization)
	}
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
			modeID := singleMode.ID
			exportSnapshot.ModeBuckets = []ModeBucket{{
				ModeID:      modeID,
				ModeName:    singleMode.Name,
				ModeColor:   singleMode.Color,
				ElapsedDays: exportSnapshot.Expedition.ElapsedDays,
				ShipsFlown:  CollectShipsForVisitedLegs(planView, exportSnapshot.RowDetails, &modeID),
				Metrics:     exportSnapshot.Live,
				Realization: exportSnapshot.Expedition.Realization,
				Derived:     ComputeDerivedMetrics(exportSnapshot.Live, exportSnapshot.Expedition.Realization, nil, exportSnapshot.Expedition.ElapsedDays),
			}}
		}
		return nil
	}
	buckets, computeError := computeFilteredModeBuckets(planView, queryAfter, candidateModes, expeditionProjections, exportSnapshot.RowDetails, exportSnapshot.Expedition.ElapsedDays)
	if computeError != nil {
		return computeError
	}
	exportSnapshot.ModeBuckets = buckets
	return nil
}

func computeFilteredModeBuckets(
	planView expedition.PlanView,
	queryAfter string,
	candidateModes []modes.Mode,
	expeditionProjections *expedition.Projections,
	rowDetails []expedition.WaypointDetails,
	expeditionElapsedDays *float64,
) ([]ModeBucket, error) {
	expeditionRecord := planView.Expedition
	expScope := expeditionRecord.ProfileScope()
	expeditionRange := replaystats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
	buckets := make([]ModeBucket, 0, len(candidateModes))
	for _, modeItem := range candidateModes {
		modeID := modeItem.ID
		modeFilter := replaystats.Filter{
			Enabled:      true,
			ExpeditionID: expeditionRecord.ID,
			Modes:        []replaystats.ModeSelection{{ModeID: modeID, Travel: true, Discovery: true}},
			Scope:        expScope,
		}
		summary, computeError := expeditionProjections.TimeRangeStats.Compute(expeditionRange, &modeFilter)
		if computeError != nil {
			return nil, fmt.Errorf("compute mode bucket %q: %w", modeItem.Name, computeError)
		}
		if !hasTelemetryActivity(summary.Metrics, summary.Realization) {
			continue
		}
		buckets = append(buckets, ModeBucket{
			ModeID:      modeID,
			ModeName:    modeItem.Name,
			ModeColor:   modeItem.Color,
			ElapsedDays: expeditionElapsedDays,
			ShipsFlown:  CollectShipsForVisitedLegs(planView, rowDetails, &modeID),
			Metrics:     summary.Metrics,
			Realization: summary.Realization,
			Derived:     ComputeDerivedMetrics(summary.Metrics, summary.Realization, nil, expeditionElapsedDays),
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
