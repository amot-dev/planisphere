package profile

import (
	"fmt"
	"strings"
	"time"
)

// FormatReport renders a human- and AI-readable telemetry report including the
// overview table, hierarchical Cold/Warm span trees, scaling models, hotspot
// rankings, and optional baseline comparison.
func FormatReport(report Report, diff *DiffReport) string {
	var builder strings.Builder
	builder.Grow(16384)

	writeDatasetHeader(&builder, report.Dataset)
	writeOverviewTable(&builder, report.Probes)
	writeScalingModelsSection(&builder, report.ScalingModels)
	writeHierarchicalTreesSection(&builder, report.Probes)
	writeAgentDiagnosticsSection(&builder, report)
	if diff != nil {
		writeDiffSection(&builder, *diff)
	}
	return builder.String()
}

func writeDatasetHeader(builder *strings.Builder, dataset DatasetSummary) {
	builder.WriteString("\n=== PLANISPHERE LIVE DATA PROFILER ===\n")
	fmt.Fprintf(builder, "Source DB:     %s (%.2f MB)\n", dataset.SourceDBPath, float64(dataset.DatabaseSizeBytes)/(1024*1024))
	fmt.Fprintf(builder, "Snapshot DB:   %s\n", dataset.SnapshotDBPath)
	fmt.Fprintf(
		builder,
		"Archive Scale: %d files | %d total events (%d metric, %d realization, %.2f MB raw_json) | %d commanders | %d GEC POIs\n",
		dataset.FilesTracked,
		dataset.TotalEvents,
		dataset.MetricEvents,
		dataset.RealizationEvents,
		float64(dataset.RawJSONBytesTotal)/(1024*1024),
		dataset.CommanderCount,
		dataset.GECPOICount,
	)
	if dataset.PrimaryExpeditionID > 0 {
		fmt.Fprintf(
			builder,
			"Primary Plan:  Expedition #%d %q (status=%s, %d rows, %d waypoints, %d visited)\n",
			dataset.PrimaryExpeditionID,
			dataset.PrimaryExpeditionName,
			dataset.PrimaryExpeditionStatus,
			dataset.PrimaryRowCount,
			dataset.PrimaryWaypointCount,
			dataset.PrimaryVisitedCount,
		)
	}
}

func writeOverviewTable(builder *strings.Builder, probes []ProbeResult) {
	builder.WriteString("\n--- PROBE SUMMARY TABLE ---\n")
	fmt.Fprintf(
		builder,
		"%-38s %-22s %12s %12s %9s %18s %14s %-6s\n",
		"PROBE", "PROCESSED", "COLD (MED)", "WARM (MED)", "SPEEDUP", "WARM RATE", "COLD MEM", "HASH",
	)
	builder.WriteString(strings.Repeat("-", 138) + "\n")

	for _, probe := range probes {
		processedLabel := fmt.Sprintf("%d %s", probe.ItemsProcessed, probe.ItemUnit)
		warmDurationLabel := "n/a"
		speedupLabel := "n/a"
		warmRateLabel := formatRateNs(probe.Cold.NormalizedNsPerItem, probe.ItemUnit)
		hashStatus := "OK"
		if probe.HasWarm {
			warmDurationLabel = formatDuration(probe.Warm.MedianDuration)
			speedupLabel = fmt.Sprintf("%+.1f%%", -probe.WarmSpeedupPct)
			warmRateLabel = formatRateNs(probe.Warm.NormalizedNsPerItem, probe.ItemUnit)
			if !probe.ColdWarmMatch {
				hashStatus = "MISMATCH"
			}
		}
		fmt.Fprintf(
			builder,
			"%-38s %-22s %12s %12s %9s %18s %14s %-6s\n",
			truncateText(probe.Name, 38),
			truncateText(processedLabel, 22),
			formatDuration(probe.Cold.MedianDuration),
			warmDurationLabel,
			speedupLabel,
			warmRateLabel,
			formatBytes(probe.Cold.AllocBytes),
			hashStatus,
		)
	}
}

func writeScalingModelsSection(builder *strings.Builder, models []ScalingModel) {
	if len(models) == 0 {
		return
	}
	builder.WriteString("\n--- MULTI-WINDOW SCALING DECOMPOSITION (SETUP T0 vs. MARGINAL COST m) ---\n")
	for _, model := range models {
		fmt.Fprintf(
			builder,
			"* %s: Fixed Per-Call Setup T0 = %s | Marginal Cost m = %s (%.0f %s/sec)\n",
			model.FunctionName,
			formatDuration(model.FixedSetupOverhead),
			formatRateNs(model.MarginalNsPerItem, model.ItemUnit),
			model.MarginalItemsPerSec,
			model.ItemUnit,
		)
		for _, point := range model.Points {
			fmt.Fprintf(
				builder,
				"    - %-18s: %8d %-14s | Cold: %10s | Warm: %10s | Apparent Rate: %s\n",
				point.WindowLabel,
				point.Items,
				point.ItemUnit,
				formatDuration(point.ColdTime),
				formatDuration(point.WarmTime),
				formatRateNs(point.NsPerItem, point.ItemUnit),
			)
		}
	}
}

func writeHierarchicalTreesSection(builder *strings.Builder, probes []ProbeResult) {
	builder.WriteString("\n--- HIERARCHICAL EXECUTION TREES (COLD vs. WARM) ---\n")
	for idx, probe := range probes {
		fmt.Fprintf(
			builder,
			"\n[%d] %s: %s\n    Processed: %d %s | Result: %s [Hash: %s]\n",
			idx+1,
			probe.Name,
			probe.Description,
			probe.ItemsProcessed,
			probe.ItemUnit,
			probe.Cold.ResultSummary,
			probe.Cold.ResultHash,
		)
		writeRunTree(builder, "COLD", probe.Cold)
		if probe.HasWarm {
			writeRunTree(builder, "WARM", probe.Warm)
		}
	}
}

func writeRunTree(builder *strings.Builder, label string, run ProbeRun) {
	fmt.Fprintf(
		builder,
		"  %s (Total: %s | SQL: %dq, %s | Go CPU: %s | Mem: %s, %d allocs)\n",
		label,
		formatDuration(run.MedianDuration),
		run.SQLQueries,
		formatDuration(run.SQLDuration),
		formatDuration(run.GoCPUDuration),
		formatBytes(run.AllocBytes),
		run.Mallocs,
	)
	if run.RootSpan != nil {
		writeSpanNodeRecursive(builder, run.RootSpan, "  ", true, true, run.RootSpan.TotalDuration)
	}
}

func writeSpanNodeRecursive(
	builder *strings.Builder,
	node *SpanNode,
	indent string,
	isLast bool,
	isRoot bool,
	rootTotal time.Duration,
) {
	connector := "|-- "
	childIndent := indent + "|   "
	if isLast {
		connector = "`-- "
		childIndent = indent + "    "
	}
	if isRoot {
		connector = "`-- "
		childIndent = indent + "    "
	}

	pctOfRoot := 100.0
	if rootTotal > 0 {
		pctOfRoot = (float64(node.TotalDuration) / float64(rootTotal)) * 100.0
	}
	locationTag := ""
	if node.Location != "" {
		locationTag = fmt.Sprintf(" [%s]", node.Location)
	}
	sqlTag := ""
	if node.SQLQueries > 0 {
		sqlTag = fmt.Sprintf(" | SQL: %dq (%d rows, %s)", node.SQLQueries, node.RowsScanned, formatDuration(node.SQLDuration))
		if node.HasNPlusOne {
			sqlTag += " [N+1 WARNING]"
		}
	}
	rateTag := ""
	if node.ItemsProcessed > 0 && node.TotalDuration > 0 {
		nsPerItem := float64(node.TotalDuration.Nanoseconds()) / float64(node.ItemsProcessed)
		rateTag = fmt.Sprintf(" | %d %s (%s)", node.ItemsProcessed, node.ItemUnit, formatRateNs(nsPerItem, node.ItemUnit))
	}

	fmt.Fprintf(
		builder,
		"%s%s%s%s -> Total: %s (%.1f%%) | Self: %s%s%s\n",
		indent,
		connector,
		node.Name,
		locationTag,
		formatDuration(node.TotalDuration),
		pctOfRoot,
		formatDuration(node.SelfDuration),
		rateTag,
		sqlTag,
	)

	for _, planText := range node.QueryPlans {
		fmt.Fprintf(builder, "%s`-- EXPLAIN: %s\n", childIndent, planText)
	}

	for idx, child := range node.Children {
		writeSpanNodeRecursive(builder, child, childIndent, idx == len(node.Children)-1, false, rootTotal)
	}
}

func writeAgentDiagnosticsSection(builder *strings.Builder, report Report) {
	builder.WriteString("\n--- DIAGNOSTICS AND BOTTLENECK SUMMARY ---\n")

	if len(report.TopBySelfTime) > 0 {
		builder.WriteString("Top Leaf Spans by Self Time (Cold):\n")
		for idx, item := range report.TopBySelfTime {
			fmt.Fprintf(
				builder,
				"  %d. %-34s -> %-36s [%s] Self: %s (SQL: %dq, %d rows)\n",
				idx+1,
				item.ProbeName,
				item.SpanName,
				item.Location,
				formatDuration(item.SelfDuration),
				item.SQLQueries,
				item.RowsScanned,
			)
		}
	}

	if len(report.TopByMemory) > 0 {
		builder.WriteString("Top Probes by Heap Allocations (Cold):\n")
		for idx, item := range report.TopByMemory {
			fmt.Fprintf(
				builder,
				"  %d. %-34s -> %10s (%s)\n",
				idx+1,
				item.ProbeName,
				formatBytes(item.AllocBytes),
				item.Finding,
			)
		}
	}

	if len(report.SQLAnomalies) > 0 {
		builder.WriteString("SQL Query Plan and N+1 Findings:\n")
		for _, anomaly := range report.SQLAnomalies {
			fmt.Fprintf(
				builder,
				"  * [%s/%s] %s [%s]: %s (Time: %s)\n",
				anomaly.ProbeName,
				anomaly.RunMode,
				anomaly.SpanName,
				anomaly.Location,
				anomaly.Finding,
				formatDuration(anomaly.TotalDuration),
			)
		}
	}

	if len(report.WarmRemnants) > 0 {
		builder.WriteString("Uncached SQL Work Remaining on Warm Calls:\n")
		for _, remnant := range report.WarmRemnants {
			fmt.Fprintf(
				builder,
				"  * [%s] %s [%s]: %s (Self: %s)\n",
				remnant.ProbeName,
				remnant.SpanName,
				remnant.Location,
				remnant.Finding,
				formatDuration(remnant.SelfDuration),
			)
		}
	}
}

func writeDiffSection(builder *strings.Builder, diff DiffReport) {
	fmt.Fprintf(builder, "\n--- BASELINE COMPARISON (vs. %s) ---\n", diff.BaselineGeneratedAt)
	fmt.Fprintf(
		builder,
		"%-38s %12s %12s %9s %12s %12s %9s %-8s\n",
		"PROBE", "BASE COLD", "CURR COLD", "DELTA", "BASE WARM", "CURR WARM", "DELTA", "RESULT",
	)
	builder.WriteString(strings.Repeat("-", 120) + "\n")
	for _, entry := range diff.Entries {
		matchStatus := "MATCH"
		if !entry.ResultHashMatched {
			matchStatus = "MISMATCH"
		}
		fmt.Fprintf(
			builder,
			"%-38s %12s %12s %+8.1f%% %12s %12s %+8.1f%% %-8s\n",
			truncateText(entry.ProbeName, 38),
			formatDuration(entry.ColdBaseline),
			formatDuration(entry.ColdCurrent),
			entry.ColdDeltaPct,
			formatDuration(entry.WarmBaseline),
			formatDuration(entry.WarmCurrent),
			entry.WarmDeltaPct,
			matchStatus,
		)
	}
}

func formatDuration(duration time.Duration) string {
	if duration < time.Microsecond {
		return fmt.Sprintf("%d ns", duration.Nanoseconds())
	}
	if duration < time.Millisecond {
		return fmt.Sprintf("%.1f us", float64(duration.Nanoseconds())/1000.0)
	}
	return fmt.Sprintf("%.2f ms", float64(duration.Nanoseconds())/1_000_000.0)
}

func formatRateNs(nsPerItem float64, unit string) string {
	if nsPerItem <= 0 {
		return "0 ns/" + unit
	}
	if nsPerItem < 1000.0 {
		return fmt.Sprintf("%.1f ns/%s", nsPerItem, unit)
	}
	if nsPerItem < 1_000_000.0 {
		return fmt.Sprintf("%.2f us/%s", nsPerItem/1000.0, unit)
	}
	return fmt.Sprintf("%.2f ms/%s", nsPerItem/1_000_000.0, unit)
}

func formatBytes(byteCount uint64) string {
	if byteCount < 1024 {
		return fmt.Sprintf("%d B", byteCount)
	}
	if byteCount < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(byteCount)/1024.0)
	}
	return fmt.Sprintf("%.2f MB", float64(byteCount)/(1024.0*1024.0))
}

func truncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	return text[:maxLength-3] + "..."
}