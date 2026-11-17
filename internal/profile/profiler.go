package profile

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
)

type profileProjections struct {
	database        *sql.DB
	bus             *projection.Bus
	commanderRoster *projections.CommanderRoster
	recoveredJumps  *projections.RecoveredJumps
	systemArrivals  *projections.SystemArrivals
	visitCounts     *projections.SystemVisitCounts
	shipHistory     *projections.ShipHistory
	salesLedger     *realization.SalesLedger
	careerStats     *stats.CareerStats
	timeRangeStats  *stats.TimeRangeStats
}

func newProfileProjections(database *sql.DB) *profileProjections {
	bus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(database, bus)
	salesLedger := realization.NewSalesLedger(database, bus)
	return &profileProjections{
		database:        database,
		bus:             bus,
		commanderRoster: projections.NewCommanderRoster(database, bus),
		recoveredJumps:  recoveredJumps,
		systemArrivals:  projections.NewSystemArrivals(database, bus),
		visitCounts:     projections.NewSystemVisitCounts(database, bus, recoveredJumps),
		shipHistory:     projections.NewShipHistory(database, bus, recoveredJumps),
		salesLedger:     salesLedger,
		careerStats:     stats.NewCareerStats(database, bus, salesLedger, recoveredJumps),
		timeRangeStats:  stats.NewTimeRangeStats(database, bus, salesLedger, recoveredJumps),
	}
}

func (proj *profileProjections) statsEngine() *stats.Engine {
	return stats.NewEngineWithProjections(proj.database, proj.salesLedger, proj.recoveredJumps)
}

func (proj *profileProjections) expeditionProjections() *expedition.Projections {
	return &expedition.Projections{
		Arrivals:       proj.systemArrivals,
		VisitCounts:    proj.visitCounts,
		ShipHistory:    proj.shipHistory,
		TimeRangeStats: proj.timeRangeStats,
		SalesLedger:    proj.salesLedger,
		RecoveredJumps: proj.recoveredJumps,
	}
}

func (proj *profileProjections) invalidate() {
	if proj != nil && proj.bus != nil {
		proj.bus.PublishApp(events.AppDatabaseSwitched)
	}
}

// RunSnapshotSuite creates a temporary read-only snapshot of the configured
// database, executes the live-data profiler suite against the snapshot, and
// removes the snapshot before returning.
func RunSnapshotSuite(ctx context.Context, options Options) (Report, error) {
	if options.Iterations <= 0 {
		options.Iterations = 3
	}
	sourcePath, resolveError := resolveSourceDBPath(options.DBPath)
	if resolveError != nil {
		return Report{}, resolveError
	}

	snapshotPath, snapshotError := db.CreateDebugSnapshot(sourcePath)
	if snapshotError != nil {
		return Report{}, fmt.Errorf("create debug snapshot from %s: %w", sourcePath, snapshotError)
	}
	defer db.RemoveDebugSnapshot(snapshotPath)

	database, openError := openTracedDatabase(snapshotPath)
	if openError != nil {
		return Report{}, fmt.Errorf("open traced snapshot %s: %w", snapshotPath, openError)
	}
	defer database.Close()

	dataset, inspectError := inspectDataset(database, sourcePath, snapshotPath)
	if inspectError != nil {
		return Report{}, fmt.Errorf("inspect dataset: %w", inspectError)
	}

	recorder := newTreeRecorder()
	setActiveRecorder(recorder)
	defer setActiveRecorder(nil)

	report := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Dataset:     dataset,
	}

	proj := newProfileProjections(database)

	statsProbes, scalingModels, statsError := runStatsProbes(database, proj, recorder, dataset, options)
	if statsError != nil {
		return report, statsError
	}
	report.Probes = append(report.Probes, statsProbes...)
	report.ScalingModels = append(report.ScalingModels, scalingModels...)

	expProbes, expError := runExpeditionProbes(database, proj, recorder, dataset, options)
	if expError != nil {
		return report, expError
	}
	report.Probes = append(report.Probes, expProbes...)

	archiveProbes, archiveError := runArchiveProbes(database, proj, recorder, dataset, options)
	if archiveError != nil {
		return report, archiveError
	}
	report.Probes = append(report.Probes, archiveProbes...)

	frontendProbes, frontendError := runFrontendProbes(ctx, database, proj, dataset, options)
	if frontendError != nil {
		return report, frontendError
	}
	report.Probes = append(report.Probes, frontendProbes...)

	populateReportDiagnostics(&report)
	return report, nil
}

func resolveSourceDBPath(explicitPath string) (string, error) {
	if trimmed := strings.TrimSpace(explicitPath); trimmed != "" {
		return trimmed, nil
	}
	if envPath := strings.TrimSpace(os.Getenv("ED_PLANNER_DB")); envPath != "" {
		return envPath, nil
	}
	return db.ResolveConfiguredPath()
}

func matchesFilter(probeName, filterPattern string) bool {
	trimmed := strings.TrimSpace(filterPattern)
	if trimmed == "" {
		return true
	}
	return strings.Contains(strings.ToLower(probeName), strings.ToLower(trimmed))
}

func inspectDataset(database *sql.DB, sourcePath, snapshotPath string) (DatasetSummary, error) {
	summary := DatasetSummary{
		SourceDBPath:   sourcePath,
		SnapshotDBPath: snapshotPath,
	}
	if fileInfo, statErr := os.Stat(sourcePath); statErr == nil {
		summary.DatabaseSizeBytes = fileInfo.Size()
	}

	_ = database.QueryRow(`SELECT COUNT(*) FROM ingested_journals`).Scan(&summary.FilesTracked)
	_ = database.QueryRow(`SELECT COUNT(*), COALESCE(SUM(LENGTH(raw_json)), 0) FROM journal_events`).Scan(&summary.TotalEvents, &summary.RawJSONBytesTotal)
	_ = database.QueryRow(`SELECT COUNT(*) FROM journal_events WHERE event IN ('LoadGame','Location','FSDJump','CarrierJump','Scan','SAAScanComplete','ScanOrganic')`).Scan(&summary.MetricEvents)
	_ = database.QueryRow(`SELECT COUNT(*) FROM journal_events WHERE event IN ('LoadGame','Location','FSDJump','CarrierJump','Docked','Undocked','Scan','SAAScanComplete','ScanOrganic','SellExplorationData','MultiSellExplorationData','SellOrganicData','Resurrect','NpcCrewPaidWage')`).Scan(&summary.RealizationEvents)
	_ = database.QueryRow(`SELECT COUNT(*) FROM commanders`).Scan(&summary.CommanderCount)
	_ = database.QueryRow(`SELECT COUNT(*) FROM expeditions`).Scan(&summary.ExpeditionCount)
	_ = database.QueryRow(`SELECT COUNT(*) FROM gec_pois_cache`).Scan(&summary.GECPOICount)

	selectPrimaryExpedition(database, &summary)
	summary.Windows = buildRepresentativeWindows(database, summary)
	return summary, nil
}

func selectPrimaryExpedition(database *sql.DB, summary *DatasetSummary) {
	expeditionsList, listErr := expedition.List(database)
	if listErr != nil || len(expeditionsList) == 0 {
		return
	}
	chosen := expeditionsList[0]
	bestScore := -1
	for _, candidate := range expeditionsList {
		rows, _ := expedition.ListRows(database, candidate.ID)
		visited := 0
		for _, r := range rows {
			if r.IsWaypoint() && r.VisitedAt != "" {
				visited++
			}
		}
		score := visited*1000 + len(rows)
		if candidate.Status == expedition.StatusActive {
			score += 50000
		}
		if score > bestScore {
			bestScore = score
			chosen = candidate
		}
	}
	summary.PrimaryExpeditionID = chosen.ID
	summary.PrimaryExpeditionName = chosen.Name
	summary.PrimaryExpeditionStatus = chosen.Status

	rows, _ := expedition.ListRows(database, chosen.ID)
	summary.PrimaryRowCount = len(rows)
	waypointNumber := 0
	for _, rowItem := range rows {
		if !rowItem.IsWaypoint() {
			continue
		}
		waypointNumber++
		summary.PrimaryWaypointCount++
		if waypointNumber == 1 {
			summary.OriginRowID = rowItem.ID
		}
		if rowItem.VisitedAt != "" {
			summary.PrimaryVisitedCount++
			if waypointNumber > 1 && summary.SampleVisitedLegRowID == 0 {
				summary.SampleVisitedLegRowID = rowItem.ID
				summary.SampleVisitedLegNumber = waypointNumber
			}
		}
	}
}

func buildRepresentativeWindows(database *sql.DB, summary DatasetSummary) []RepresentativeWindow {
	var windows []RepresentativeWindow

	if legWindow, ok := findSingleLegWindow(database, summary); ok {
		windows = append(windows, legWindow)
	}
	if expWindow, ok := findExpeditionWindow(database, summary); ok {
		windows = append(windows, expWindow)
	}
	windows = append(windows, RepresentativeWindow{
		Label:          "AllTime",
		AfterTimestamp: "",
		UntilTimestamp: "9999-12-31T23:59:59Z",
		MetricEvents:   summary.MetricEvents,
		RealEvents:     summary.RealizationEvents,
	})
	return windows
}

func findSingleLegWindow(database *sql.DB, summary DatasetSummary) (RepresentativeWindow, bool) {
	if summary.PrimaryExpeditionID > 0 {
		rows, _ := expedition.ListRows(database, summary.PrimaryExpeditionID)
		var visitedTimestamps []string
		for _, rowItem := range rows {
			if rowItem.IsWaypoint() && rowItem.VisitedAt != "" {
				visitedTimestamps = append(visitedTimestamps, rowItem.VisitedAt)
			}
		}
		slices.Sort(visitedTimestamps)
		for i := 1; i < len(visitedTimestamps); i++ {
			after := visitedTimestamps[i-1]
			until := visitedTimestamps[i]
			metricCount, realCount := countWindowEvents(database, after, until)
			if metricCount >= 5 {
				return RepresentativeWindow{
					Label:          "SingleLeg",
					AfterTimestamp: after,
					UntilTimestamp: until,
					MetricEvents:   metricCount,
					RealEvents:     realCount,
				}, true
			}
		}
	}
	var firstTS, endSliceTS sql.NullString
	_ = database.QueryRow(`SELECT MIN(timestamp) FROM journal_events`).Scan(&firstTS)
	_ = database.QueryRow(`SELECT timestamp FROM journal_events ORDER BY timestamp ASC LIMIT 1 OFFSET 250`).Scan(&endSliceTS)
	if firstTS.Valid && endSliceTS.Valid && endSliceTS.String > firstTS.String {
		metricCount, realCount := countWindowEvents(database, firstTS.String, endSliceTS.String)
		return RepresentativeWindow{
			Label:          "SingleLeg",
			AfterTimestamp: firstTS.String,
			UntilTimestamp: endSliceTS.String,
			MetricEvents:   metricCount,
			RealEvents:     realCount,
		}, true
	}
	return RepresentativeWindow{}, false
}

func findExpeditionWindow(database *sql.DB, summary DatasetSummary) (RepresentativeWindow, bool) {
	if summary.PrimaryExpeditionID > 0 {
		expRecord, err := expedition.Get(database, summary.PrimaryExpeditionID)
		if err == nil && expRecord.StartTimestamp != "" {
			until := expRecord.EndTimestamp
			if until == "" {
				until = "9999-12-31T23:59:59Z"
			}
			metricCount, realCount := countWindowEvents(database, expRecord.StartTimestamp, until)
			if metricCount > 0 {
				return RepresentativeWindow{
					Label:          "ExpeditionWindow",
					AfterTimestamp: expRecord.StartTimestamp,
					UntilTimestamp: until,
					MetricEvents:   metricCount,
					RealEvents:     realCount,
				}, true
			}
		}
	}
	if summary.TotalEvents > 20 {
		offset := max(summary.TotalEvents*3/4, 1)
		var midTS sql.NullString
		_ = database.QueryRow(`SELECT timestamp FROM journal_events ORDER BY timestamp ASC LIMIT 1 OFFSET ?`, offset).Scan(&midTS)
		if midTS.Valid && midTS.String != "" {
			metricCount, realCount := countWindowEvents(database, midTS.String, "9999-12-31T23:59:59Z")
			return RepresentativeWindow{
				Label:          "ExpeditionWindow",
				AfterTimestamp: midTS.String,
				UntilTimestamp: "9999-12-31T23:59:59Z",
				MetricEvents:   metricCount,
				RealEvents:     realCount,
			}, true
		}
	}
	return RepresentativeWindow{}, false
}

func countWindowEvents(database *sql.DB, afterTimestamp, untilTimestamp string) (int, int) {
	var metricCount, realCount int
	_ = database.QueryRow(
		`SELECT COUNT(*) FROM journal_events WHERE event IN ('LoadGame','Location','FSDJump','CarrierJump','Scan','SAAScanComplete','ScanOrganic') AND timestamp > ? AND timestamp <= ? AND `+journal.CmdrFilterSQL,
		afterTimestamp, untilTimestamp,
	).Scan(&metricCount)
	_ = database.QueryRow(
		`SELECT COUNT(*) FROM journal_events WHERE event IN ('LoadGame','Location','FSDJump','CarrierJump','Docked','Undocked','Scan','SAAScanComplete','ScanOrganic','SellExplorationData','MultiSellExplorationData','SellOrganicData','Resurrect','NpcCrewPaidWage') AND timestamp > ? AND timestamp <= ? AND `+journal.CmdrFilterSQL,
		afterTimestamp, untilTimestamp,
	).Scan(&realCount)
	return metricCount, realCount
}

func populateReportDiagnostics(report *Report) {
	var leafSpans []HotspotEntry
	var sqlAnomalies []HotspotEntry
	var warmRemnants []HotspotEntry
	var memoryEntries []HotspotEntry

	for _, probe := range report.Probes {
		if probe.Cold.AllocBytes > 0 {
			memoryEntries = append(memoryEntries, HotspotEntry{
				ProbeName:     probe.Name,
				RunMode:       "Cold",
				SpanName:      probe.Cold.RootSpan.Name,
				Location:      probe.Cold.RootSpan.Location,
				SelfDuration:  probe.Cold.MedianDuration,
				TotalDuration: probe.Cold.MedianDuration,
				AllocBytes:    probe.Cold.AllocBytes,
				Finding:       fmt.Sprintf("%.1f B/%s (%.1f allocs/%s)", probe.Cold.BytesPerItem, probe.ItemUnit, probe.Cold.AllocsPerItem, probe.ItemUnit),
			})
		}
		if probe.Cold.RootSpan != nil {
			collectSpanDiagnostics(probe.Name, "Cold", probe.Cold.RootSpan, probe.Cold.MedianDuration, &leafSpans, &sqlAnomalies, nil)
		}
		if probe.HasWarm && probe.Warm.RootSpan != nil {
			collectSpanDiagnostics(probe.Name, "Warm", probe.Warm.RootSpan, probe.Warm.MedianDuration, nil, &sqlAnomalies, &warmRemnants)
		}
	}

	slices.SortFunc(leafSpans, func(left, right HotspotEntry) int {
		return cmp.Compare(right.SelfDuration, left.SelfDuration)
	})
	if len(leafSpans) > 6 {
		leafSpans = leafSpans[:6]
	}

	slices.SortFunc(memoryEntries, func(left, right HotspotEntry) int {
		if right.AllocBytes > left.AllocBytes {
			return 1
		}
		if right.AllocBytes < left.AllocBytes {
			return -1
		}
		return 0
	})
	if len(memoryEntries) > 5 {
		memoryEntries = memoryEntries[:5]
	}

	report.TopBySelfTime = leafSpans
	report.TopByMemory = memoryEntries
	report.SQLAnomalies = deduplicateHotspots(sqlAnomalies)
	report.WarmRemnants = deduplicateHotspots(warmRemnants)
}

func deduplicateHotspots(entries []HotspotEntry) []HotspotEntry {
	seen := make(map[string]bool, len(entries))
	var unique []HotspotEntry
	for _, entry := range entries {
		key := entry.SpanName + "|" + entry.Location + "|" + entry.Finding
		if !seen[key] {
			seen[key] = true
			unique = append(unique, entry)
		}
	}
	return unique
}

func collectSpanDiagnostics(
	probeName string,
	runMode string,
	node *SpanNode,
	rootDuration time.Duration,
	leafSpans *[]HotspotEntry,
	sqlAnomalies *[]HotspotEntry,
	warmRemnants *[]HotspotEntry,
) {
	isLeaf := len(node.Children) == 0
	if isLeaf && leafSpans != nil && node.SelfDuration > 0 {
		*leafSpans = append(*leafSpans, HotspotEntry{
			ProbeName:     probeName,
			RunMode:       runMode,
			SpanName:      node.Name,
			Location:      node.Location,
			SelfDuration:  node.SelfDuration,
			TotalDuration: node.TotalDuration,
			SQLQueries:    node.SQLQueries,
			RowsScanned:   node.RowsScanned,
		})
	}
	if sqlAnomalies != nil && (node.HasNPlusOne || node.HasFullScan || node.HasTempBTree) {
		var flags []string
		if node.HasNPlusOne {
			flags = append(flags, fmt.Sprintf("N+1 loop (%d SQL queries)", node.SQLQueries))
		}
		if node.HasFullScan {
			flags = append(flags, "Full Table Scan")
		}
		if node.HasTempBTree {
			flags = append(flags, "Temp B-Tree Sort")
		}
		*sqlAnomalies = append(*sqlAnomalies, HotspotEntry{
			ProbeName:     probeName,
			RunMode:       runMode,
			SpanName:      node.Name,
			Location:      node.Location,
			SelfDuration:  node.SelfDuration,
			TotalDuration: node.TotalDuration,
			SQLQueries:    node.SQLQueries,
			RowsScanned:   node.RowsScanned,
			Finding:       strings.Join(flags, ", "),
		})
	}
	if warmRemnants != nil && isLeaf && node.Category == "sql" && node.SelfDuration >= 250*time.Microsecond && rootDuration > 0 {
		pct := float64(node.SelfDuration) / float64(rootDuration) * 100.0
		if pct >= 25.0 {
			*warmRemnants = append(*warmRemnants, HotspotEntry{
				ProbeName:     probeName,
				RunMode:       runMode,
				SpanName:      node.Name,
				Location:      node.Location,
				SelfDuration:  node.SelfDuration,
				TotalDuration: node.TotalDuration,
				SQLQueries:    node.SQLQueries,
				RowsScanned:   node.RowsScanned,
				Finding:       fmt.Sprintf("Uncached SQL on Warm pass (%.1f%% of warm time, %d rows)", pct, node.RowsScanned),
			})
		}
	}
	for _, child := range node.Children {
		collectSpanDiagnostics(probeName, runMode, child, rootDuration, leafSpans, sqlAnomalies, warmRemnants)
	}
}

// SaveReport writes a profiling report as formatted JSON to filePath.
func SaveReport(filePath string, report Report) error {
	encoded, marshalErr := json.MarshalIndent(report, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	return os.WriteFile(filePath, append(encoded, '\n'), 0o644)
}

// LoadReport reads a previously saved JSON profiling report from filePath.
func LoadReport(filePath string) (Report, error) {
	rawBytes, readErr := os.ReadFile(filePath)
	if readErr != nil {
		return Report{}, readErr
	}
	var report Report
	if unmarshalErr := json.Unmarshal(rawBytes, &report); unmarshalErr != nil {
		return Report{}, unmarshalErr
	}
	return report, nil
}

// CompareReports computes per-probe timing, allocation, and result-hash deltas
// between a baseline report and a current report.
func CompareReports(baseline, current Report) DiffReport {
	baselineByName := make(map[string]ProbeResult, len(baseline.Probes))
	for _, probe := range baseline.Probes {
		baselineByName[probe.Name] = probe
	}

	diff := DiffReport{BaselineGeneratedAt: baseline.GeneratedAt}
	for _, curProbe := range current.Probes {
		baseProbe, exists := baselineByName[curProbe.Name]
		if !exists {
			continue
		}
		hashMatched := baseProbe.Cold.ResultHash == curProbe.Cold.ResultHash
		if !hashMatched {
			diff.AnyResultMismatch = true
		}
		diff.Entries = append(diff.Entries, DiffEntry{
			ProbeName:         curProbe.Name,
			ColdBaseline:      baseProbe.Cold.MedianDuration,
			ColdCurrent:       curProbe.Cold.MedianDuration,
			ColdDeltaPct:      percentDelta(float64(baseProbe.Cold.MedianDuration), float64(curProbe.Cold.MedianDuration)),
			WarmBaseline:      baseProbe.Warm.MedianDuration,
			WarmCurrent:       curProbe.Warm.MedianDuration,
			WarmDeltaPct:      percentDelta(float64(baseProbe.Warm.MedianDuration), float64(curProbe.Warm.MedianDuration)),
			AllocBaseline:     baseProbe.Cold.AllocBytes,
			AllocCurrent:      curProbe.Cold.AllocBytes,
			AllocDeltaPct:     percentDelta(float64(baseProbe.Cold.AllocBytes), float64(curProbe.Cold.AllocBytes)),
			ResultHashMatched: hashMatched,
		})
	}
	return diff
}

func percentDelta(baselineValue, currentValue float64) float64 {
	if baselineValue <= 0 {
		return 0
	}
	return ((currentValue - baselineValue) / baselineValue) * 100.0
}
