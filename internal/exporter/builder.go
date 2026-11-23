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
	expeditionRecord, loadExpeditionError := expedition.Get(databaseConnection, expeditionID)
	if loadExpeditionError != nil {
		return ExpeditionExport{}, fmt.Errorf("load expedition %d for export: %w", expeditionID, loadExpeditionError)
	}
	queryAfter, displayStart, departed, effectiveStartError := expedition.EffectiveStart(databaseConnection, expeditionRecord)
	if effectiveStartError != nil {
		return ExpeditionExport{}, fmt.Errorf("resolve effective start for expedition %d: %w", expeditionID, effectiveStartError)
	}
	if expeditionRecord.Status == expedition.StatusPlanning || (!departed && expeditionRecord.Status != expedition.StatusCompleted) {
		return ExpeditionExport{}, ErrNotExportable
	}

	planView, planError := expedition.BuildPlan(databaseConnection, expeditionID, dependencies.GECLookup, dependencies.Projections)
	if planError != nil {
		return ExpeditionExport{}, fmt.Errorf("build plan for export %d: %w", expeditionID, planError)
	}
	exportSnapshot := initializeExportHeader(databaseConnection, expeditionRecord, planView, dependencies, currentTime, displayStart, departed)

	if populateError := populateExpeditionTelemetry(databaseConnection, expeditionRecord, queryAfter, displayStart, departed, dependencies, &exportSnapshot); populateError != nil {
		return ExpeditionExport{}, populateError
	}
	if detailsError := populateRowDetailsAndBaseline(databaseConnection, planView.Rows, dependencies.Projections, &exportSnapshot); detailsError != nil {
		return ExpeditionExport{}, detailsError
	}
	if bucketsError := populateModeBuckets(expeditionRecord, planView.Modes, queryAfter, departed, dependencies.Projections, &exportSnapshot); bucketsError != nil {
		return ExpeditionExport{}, bucketsError
	}
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
	dependencies BuilderDeps,
	currentTime time.Time,
	displayStart string,
	departed bool,
) ExpeditionExport {
	commanders := loadEnabledCommanderNames(databaseConnection)
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
	}
}

func loadEnabledCommanderNames(databaseConnection *sql.DB) []string {
	commanderRecords, listError := replay.ListCommanders(databaseConnection)
	if listError != nil || len(commanderRecords) == 0 {
		return []string{}
	}
	enabledNames := make([]string, 0, len(commanderRecords))
	for _, commander := range commanderRecords {
		if commander.Enabled && commander.Name != "" {
			enabledNames = append(enabledNames, commander.Name)
		}
	}
	return enabledNames
}

func populateExpeditionTelemetry(
	databaseConnection *sql.DB,
	expeditionRecord expedition.Expedition,
	queryAfter, displayStart string,
	departed bool,
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

	if dependencies.ComputeCarrierCost != nil {
		exportSnapshot.CarrierCost = dependencies.ComputeCarrierCost(expeditionRecord, exportSnapshot.Expedition.Metrics, displayStart, departed)
	}
	return populatePostCareer(expeditionRecord, dependencies, exportSnapshot)
}

func populatePostCareer(
	expeditionRecord expedition.Expedition,
	dependencies BuilderDeps,
	exportSnapshot *ExpeditionExport,
) error {
	// Completed expeditions snapshot career totals strictly at EndTimestamp so
	// OriginBaseline + Full Expedition equals PostCareer even after later trips.
	if expeditionRecord.EndTimestamp != "" {
		postSummary, computeError := dependencies.Projections.TimeRangeStats.Compute(
			replaystats.Range{After: "", Until: expeditionRecord.EndTimestamp},
			nil,
		)
		if computeError != nil {
			return fmt.Errorf("compute post-expedition career telemetry: %w", computeError)
		}
		exportSnapshot.PostCareer = TelemetrySnapshot{
			Metrics:     postSummary.Metrics,
			Realization: postSummary.Realization,
		}
		return nil
	}
	if dependencies.CareerStats != nil {
		careerSummary, careerError := dependencies.CareerStats.Get()
		if careerError != nil {
			return fmt.Errorf("load active career telemetry: %w", careerError)
		}
		exportSnapshot.PostCareer = TelemetrySnapshot{
			Metrics:     careerSummary.Metrics,
			Realization: careerSummary.Realization,
		}
		return nil
	}
	allTimeSummary, computeError := dependencies.Projections.TimeRangeStats.Compute(replaystats.Range{}, nil)
	if computeError != nil {
		return fmt.Errorf("compute fallback career telemetry: %w", computeError)
	}
	exportSnapshot.PostCareer = TelemetrySnapshot{
		Metrics:     allTimeSummary.Metrics,
		Realization: allTimeSummary.Realization,
	}
	return nil
}

func populateRowDetailsAndBaseline(
	databaseConnection *sql.DB,
	rows []expedition.RowView,
	expeditionProjections *expedition.Projections,
	exportSnapshot *ExpeditionExport,
) error {
	for _, rowView := range rows {
		if !rowView.Row.IsWaypoint() {
			continue
		}
		if rowView.Row.VisitedAt == "" && rowView.VisitState != expedition.VisitCurrent {
			continue
		}
		details, detailsError := expedition.Details(databaseConnection, rowView.Row.ID, expeditionProjections)
		if detailsError != nil {
			return fmt.Errorf("compute waypoint %d details: %w", rowView.Number, detailsError)
		}
		if details.RowID == 0 {
			continue
		}
		if details.Kind == "origin" {
			originCopy := details
			exportSnapshot.OriginBaseline = &originCopy
			continue
		}
		exportSnapshot.RowDetails = append(exportSnapshot.RowDetails, details)
	}
	return nil
}

func populateModeBuckets(
	expeditionRecord expedition.Expedition,
	modeList []modes.Mode,
	queryAfter string,
	departed bool,
	expeditionProjections *expedition.Projections,
	exportSnapshot *ExpeditionExport,
) error {
	if !departed {
		return nil
	}
	expeditionRange := replaystats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
	buckets := make([]ModeBucket, 0, len(modeList))
	for _, modeItem := range modeList {
		modeFilter := replaystats.Filter{
			Enabled:      true,
			ExpeditionID: expeditionRecord.ID,
			Modes:        []replaystats.ModeSelection{{ModeID: modeItem.ID, Travel: true, Discovery: true}},
		}
		summary, computeError := expeditionProjections.TimeRangeStats.Compute(expeditionRange, &modeFilter)
		if computeError != nil {
			return fmt.Errorf("compute mode bucket %q: %w", modeItem.Name, computeError)
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
	exportSnapshot.ModeBuckets = buckets
	return nil
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
