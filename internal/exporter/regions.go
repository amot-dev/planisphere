package exporter

import (
	"cmp"
	"strings"
	"time"

	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

const (
	defaultAttributedRegionName = "Unknown Region"
	defaultRegionModeName       = "Travel"
	defaultRegionModeColor      = "#FF7100"
)

type playtimeAccumulator struct {
	totalSeconds    int64
	transitSeconds  int64
	inSystemSeconds int64
	hasInGame       bool
}

func (accumulator *playtimeAccumulator) addWaypointDetails(details *expedition.WaypointDetails) {
	if details == nil || details.InGameSeconds == nil {
		return
	}
	accumulator.hasInGame = true
	accumulator.totalSeconds += *details.InGameSeconds
	if details.TransitInGameSeconds != nil {
		accumulator.transitSeconds += *details.TransitInGameSeconds
	}
	if details.InSystemInGameSeconds != nil {
		accumulator.inSystemSeconds += *details.InSystemInGameSeconds
	}
}

func (accumulator playtimeAccumulator) labelsAndPtrs() (
	inGameSeconds *int64,
	inGameDuration string,
	transitInGameSeconds *int64,
	transitInGameDuration string,
	inSystemInGameSeconds *int64,
	inSystemInGameDuration string,
) {
	if !accumulator.hasInGame {
		return nil, "", nil, "", nil, ""
	}
	return int64Ptr(accumulator.totalSeconds), formatPlaySeconds(accumulator.totalSeconds),
		int64Ptr(accumulator.transitSeconds), formatPlaySeconds(accumulator.transitSeconds),
		int64Ptr(accumulator.inSystemSeconds), formatPlaySeconds(accumulator.inSystemSeconds)
}

func int64Ptr(value int64) *int64 {
	return &value
}

func cloneInt64Ptr(source *int64) *int64 {
	if source == nil {
		return nil
	}
	return int64Ptr(*source)
}

func formatPlaySeconds(totalSeconds int64) string {
	return reference.PlayDurationLabel(time.Duration(totalSeconds) * time.Second)
}

type sliceTrackState struct {
	totalElapsedMs int64
	playtime       playtimeAccumulator
	seenShips      map[string]bool
}

type regionTrackState struct {
	combined      sliceTrackState
	modeIndexByID map[int64]int
	modeStates    []sliceTrackState
}

// BuildLegAndRegionTelemetry attributes Galactic Regions across visited non-origin legs,
// computes per-leg telemetry, and aggregates combined and per-mode RegionBuckets in arrival order.
func BuildLegAndRegionTelemetry(
	planView expedition.PlanView,
	rowDetails []expedition.WaypointDetails,
	currentTime time.Time,
) ([]ExportLegTelemetry, []RegionBucket) {
	detailsByRowID := make(map[int64]expedition.WaypointDetails, len(rowDetails))
	for _, detailEntry := range rowDetails {
		detailsByRowID[detailEntry.RowID] = detailEntry
	}

	legTelemetry := make([]ExportLegTelemetry, 0, len(rowDetails))
	regionBuckets := make([]RegionBucket, 0, 8)
	regionStates := make([]regionTrackState, 0, 8)
	regionIndexByName := make(map[string]int, 8)
	activeManualRegion := ""

	for _, rowView := range planView.Rows {
		if rowView.Row.RowType == expedition.RowRegion {
			activeManualRegion = cmp.Or(rowView.RegionName, activeManualRegion)
			continue
		}
		if rowView.Number <= 1 || rowView.Row.VisitedAt == "" {
			continue
		}
		details, hasDetails := detailsByRowID[rowView.Row.ID]
		if !hasDetails {
			continue
		}

		regionName := cmp.Or(rowView.DetectedRegion, activeManualRegion, defaultAttributedRegionName)
		shipsFlown := appendTrimmedUnique(nil, make(map[string]bool, len(details.ShipsUsed)), details.ShipsUsed)
		legElapsedMs, hasElapsed := computeElapsedMillis(details.From, details.To, currentTime)
		var legElapsedDays *float64
		if hasElapsed {
			legElapsedDays = floatPtr(float64(legElapsedMs) / millisecondsPerDay)
		}
		legTelemetry = append(legTelemetry, ExportLegTelemetry{
			RowID:                  rowView.Row.ID,
			RegionName:             regionName,
			ElapsedDays:            legElapsedDays,
			InGameSeconds:          cloneInt64Ptr(details.InGameSeconds),
			InGameDuration:         details.InGameDuration,
			TransitInGameSeconds:   cloneInt64Ptr(details.TransitInGameSeconds),
			TransitInGameDuration:  details.TransitInGameDuration,
			InSystemInGameSeconds:  cloneInt64Ptr(details.InSystemInGameSeconds),
			InSystemInGameDuration: details.InSystemInGameDuration,
			ShipsFlown:             shipsFlown,
			Derived:                ComputeDerivedMetrics(details.Metrics, details.Realization, nil, legElapsedDays, details.InGameSeconds),
		})

		regionIndex, exists := regionIndexByName[regionName]
		if !exists {
			regionIndex = len(regionBuckets)
			regionIndexByName[regionName] = regionIndex
			regionBuckets = append(regionBuckets, RegionBucket{
				RegionName:  regionName,
				LegRowIDs:   make([]int64, 0, 4),
				ShipsFlown:  make([]string, 0, 2),
				ModeBuckets: make([]RegionModeBucket, 0, 2),
			})
			regionStates = append(regionStates, regionTrackState{
				combined:      sliceTrackState{seenShips: make(map[string]bool, 2)},
				modeIndexByID: make(map[int64]int, 2),
			})
		}
		recordVisitedLegInRegion(&regionBuckets[regionIndex], &regionStates[regionIndex], rowView, details, shipsFlown, legElapsedMs, hasElapsed)
	}

	for index := range regionBuckets {
		finalizeRegionBucket(&regionBuckets[index], &regionStates[index])
	}
	return legTelemetry, regionBuckets
}

// CollectShipsForVisitedLegs returns ordered unique trimmed ship hull names across visited
// non-origin legs, optionally restricted to legs matching filterModeID.
func CollectShipsForVisitedLegs(
	planView expedition.PlanView,
	rowDetails []expedition.WaypointDetails,
	filterModeID *int64,
) []string {
	detailsByRowID := make(map[int64]expedition.WaypointDetails, len(rowDetails))
	for _, detailEntry := range rowDetails {
		detailsByRowID[detailEntry.RowID] = detailEntry
	}
	uniqueShips := make([]string, 0, 4)
	seenShips := make(map[string]bool, 4)
	for _, rowView := range planView.Rows {
		if rowView.Row.RowType == expedition.RowRegion || rowView.Number <= 1 || rowView.Row.VisitedAt == "" {
			continue
		}
		if filterModeID != nil && rowView.LegModeID != *filterModeID {
			continue
		}
		if details, hasDetails := detailsByRowID[rowView.Row.ID]; hasDetails {
			uniqueShips = appendTrimmedUnique(uniqueShips, seenShips, details.ShipsUsed)
		}
	}
	return uniqueShips
}

func appendTrimmedUnique(existing []string, seen map[string]bool, rawValues []string) []string {
	if existing == nil {
		existing = make([]string, 0, len(rawValues))
	}
	for _, rawValue := range rawValues {
		if trimmed := strings.TrimSpace(rawValue); trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			existing = append(existing, trimmed)
		}
	}
	return existing
}

func recordVisitedLegInRegion(
	bucket *RegionBucket,
	state *regionTrackState,
	rowView expedition.RowView,
	details expedition.WaypointDetails,
	shipsFlown []string,
	legElapsedMs int64,
	hasElapsed bool,
) {
	bucket.LegRowIDs = append(bucket.LegRowIDs, rowView.Row.ID)
	accumulateLegSlice(&bucket.LegCount, &bucket.FromGalactic, &bucket.ToGalactic, &bucket.InSystem, &bucket.CoveredLy, &bucket.ShipsFlown, &bucket.Metrics, &bucket.Realization, &state.combined, rowView, details, shipsFlown, legElapsedMs, hasElapsed)
	if rowView.LegModeID <= 0 {
		return
	}
	modeIndex, exists := state.modeIndexByID[rowView.LegModeID]
	if !exists {
		modeIndex = len(bucket.ModeBuckets)
		state.modeIndexByID[rowView.LegModeID] = modeIndex
		bucket.ModeBuckets = append(bucket.ModeBuckets, RegionModeBucket{
			ModeID:     rowView.LegModeID,
			ModeName:   cmp.Or(rowView.LegModeName, defaultRegionModeName),
			ModeColor:  cmp.Or(rowView.LegModeColor, defaultRegionModeColor),
			ShipsFlown: make([]string, 0, 2),
		})
		state.modeStates = append(state.modeStates, sliceTrackState{seenShips: make(map[string]bool, 2)})
	}
	modeBucket := &bucket.ModeBuckets[modeIndex]
	accumulateLegSlice(&modeBucket.LegCount, &modeBucket.FromGalactic, &modeBucket.ToGalactic, &modeBucket.InSystem, &modeBucket.CoveredLy, &modeBucket.ShipsFlown, &modeBucket.Metrics, &modeBucket.Realization, &state.modeStates[modeIndex], rowView, details, shipsFlown, legElapsedMs, hasElapsed)
}

func accumulateLegSlice(
	legCount *int,
	fromGalactic, toGalactic *string,
	inSystem *bool,
	coveredLy *float64,
	shipsFlown *[]string,
	metrics *replaystats.Metrics,
	realization *replaystats.RealizationBreakdown,
	state *sliceTrackState,
	rowView expedition.RowView,
	details expedition.WaypointDetails,
	legShips []string,
	legElapsedMs int64,
	hasElapsed bool,
) {
	*legCount++
	*metrics = metrics.Add(details.Metrics)
	*realization = realization.Add(details.Realization)
	if *fromGalactic == "" && details.FromGalactic != "" {
		*fromGalactic = details.FromGalactic
	}
	if details.ToGalactic != "" {
		*toGalactic = details.ToGalactic
	}
	if details.InSystem {
		*inSystem = true
		*toGalactic = "IN SYSTEM"
	}
	if rowView.DistFromPrevLy != nil {
		*coveredLy += *rowView.DistFromPrevLy
	}
	*shipsFlown = appendTrimmedUnique(*shipsFlown, state.seenShips, legShips)
	if hasElapsed {
		state.totalElapsedMs += legElapsedMs
	}
	state.playtime.addWaypointDetails(&details)
}

func finalizeRegionBucket(bucket *RegionBucket, state *regionTrackState) {
	bucket.InGameSeconds, bucket.InGameDuration, bucket.TransitInGameSeconds, bucket.TransitInGameDuration, bucket.InSystemInGameSeconds, bucket.InSystemInGameDuration = state.combined.playtime.labelsAndPtrs()
	bucket.ElapsedDays, bucket.Duration, bucket.Derived = finalizeSliceTimingAndDerived(bucket.Metrics, bucket.Realization, state.combined.totalElapsedMs, bucket.InGameSeconds)
	for index := range bucket.ModeBuckets {
		modeBucket := &bucket.ModeBuckets[index]
		modeBucket.InGameSeconds, modeBucket.InGameDuration, modeBucket.TransitInGameSeconds, modeBucket.TransitInGameDuration, modeBucket.InSystemInGameSeconds, modeBucket.InSystemInGameDuration = state.modeStates[index].playtime.labelsAndPtrs()
		modeBucket.ElapsedDays, modeBucket.Duration, modeBucket.Derived = finalizeSliceTimingAndDerived(modeBucket.Metrics, modeBucket.Realization, state.modeStates[index].totalElapsedMs, modeBucket.InGameSeconds)
	}
}

func finalizeSliceTimingAndDerived(metrics replaystats.Metrics, realization replaystats.RealizationBreakdown, totalElapsedMs int64, inGameSeconds *int64) (*float64, string, DerivedMetrics) {
	var elapsedDays *float64
	duration := ""
	if totalElapsedMs > 0 {
		elapsedDays = floatPtr(float64(totalElapsedMs) / millisecondsPerDay)
		duration = reference.DurationLabel(time.Duration(totalElapsedMs) * time.Millisecond)
	}
	return elapsedDays, duration, ComputeDerivedMetrics(metrics, realization, nil, elapsedDays, inGameSeconds)
}
