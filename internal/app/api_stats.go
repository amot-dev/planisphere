package app

import (
	"database/sql"
	"slices"
	"strconv"
	"strings"

	"planisphere/internal/ed"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

// SidebarStats is the content of the Stats Sidebar.
type SidebarStats struct {
	ExpeditionID           int64                      `json:"expeditionId"`
	Status                 string                     `json:"status"`
	Started                bool                       `json:"started"`
	Start                  string                     `json:"start"`
	End                    string                     `json:"end"`
	StartGalactic          string                     `json:"startGalactic"`
	EndGalactic            string                     `json:"endGalactic"`
	EndIsNow               bool                       `json:"endIsNow"`
	Duration               string                     `json:"duration"`
	CareerStart            string                     `json:"careerStart"`
	CareerStartGalactic    string                     `json:"careerStartGalactic"`
	CareerDuration         string                     `json:"careerDuration"`
	FilterActive           bool                       `json:"filterActive"`
	Live                   stats.Metrics              `json:"live"`        // from journals (mode-filtered)
	Adjustments            stats.Metrics              `json:"adjustments"` // manual
	Expedition             stats.Metrics              `json:"expedition"`  // live + adjustments
	AllJournals            stats.Metrics              `json:"allJournals"` // all ingested journal events (unfiltered)
	BaselineEnabled        bool                       `json:"baselineEnabled"`
	BaselineSource         string                     `json:"baselineSource"`
	Baseline               stats.Metrics              `json:"baseline"`
	Lifetime               stats.Metrics              `json:"lifetime"`               // baseline + expedition
	Realization            stats.RealizationBreakdown `json:"realization"`            // expedition realized earnings and losses (mode-filtered)
	AllJournalsRealization stats.RealizationBreakdown `json:"allJournalsRealization"` // all-time realized earnings and losses across all journals (unfiltered)
}

// resolveCareerStart queries or returns the cached earliest timestamp across
// active commanders. Caller must hold app.dbMu.RLock.
func (app *App) resolveCareerStart() (string, string) {
	app.careerStartMu.Lock()
	if app.careerStartValid {
		cachedStart := app.careerStartTimestamp
		cachedGalactic := app.careerStartGalactic
		app.careerStartMu.Unlock()
		return cachedStart, cachedGalactic
	}
	careerStartGeneration := app.careerStartGen
	app.careerStartMu.Unlock()

	var earliestSeenTimestamp sql.NullString
	_ = app.db.QueryRow(`SELECT MIN(commanders.first_seen) FROM commanders WHERE commanders.enabled = 1 AND commanders.first_seen IS NOT NULL AND commanders.first_seen != ''`).Scan(&earliestSeenTimestamp)
	if !earliestSeenTimestamp.Valid || earliestSeenTimestamp.String == "" {
		_ = app.db.QueryRow(`SELECT MIN(timestamp) FROM journal_events WHERE ` + journal.CmdrFilterSQL).Scan(&earliestSeenTimestamp)
	}

	var computedCareerStart string
	var computedCareerStartGalactic string
	if earliestSeenTimestamp.Valid && earliestSeenTimestamp.String != "" {
		computedCareerStart = earliestSeenTimestamp.String
		computedCareerStartGalactic = ed.ToGalactic(earliestSeenTimestamp.String)
	}

	// Guard cache write-back with careerStartGen so an invalidation via invalidateAllTimeCache
	// that ran while careerStartMu was unlocked during the database query cannot be overwritten by stale results.
	app.careerStartMu.Lock()
	if !app.careerStartValid && app.careerStartGen == careerStartGeneration {
		app.careerStartTimestamp = computedCareerStart
		app.careerStartGalactic = computedCareerStartGalactic
		app.careerStartValid = true
	} else if app.careerStartValid {
		computedCareerStart = app.careerStartTimestamp
		computedCareerStartGalactic = app.careerStartGalactic
	}
	app.careerStartMu.Unlock()
	return computedCareerStart, computedCareerStartGalactic
}

func buildLiveStatsCacheKey(expeditionRecord expedition.Expedition, filter stats.Filter) string {
	var keyBuilder strings.Builder
	keyBuilder.Grow(64)
	keyBuilder.WriteString(strconv.FormatInt(expeditionRecord.ID, 10))
	keyBuilder.WriteByte('|')
	keyBuilder.WriteString(expeditionRecord.StartTimestamp)
	keyBuilder.WriteByte('|')
	keyBuilder.WriteString(expeditionRecord.EndTimestamp)
	keyBuilder.WriteByte('|')
	if !filter.Enabled {
		keyBuilder.WriteString("unfiltered")
		return keyBuilder.String()
	}
	keyBuilder.WriteString("filtered:")
	activeSelections := make([]stats.ModeSelection, 0, len(filter.Modes))
	for _, selection := range filter.Modes {
		if selection.Travel || selection.Discovery {
			activeSelections = append(activeSelections, selection)
		}
	}
	slices.SortFunc(activeSelections, func(left, right stats.ModeSelection) int {
		if left.ModeID < right.ModeID {
			return -1
		}
		if left.ModeID > right.ModeID {
			return 1
		}
		return 0
	})
	wroteFirst := false
	for index := 0; index < len(activeSelections); index++ {
		merged := activeSelections[index]
		for index+1 < len(activeSelections) && activeSelections[index+1].ModeID == merged.ModeID {
			index++
			merged.Travel = merged.Travel || activeSelections[index].Travel
			merged.Discovery = merged.Discovery || activeSelections[index].Discovery
		}
		if wroteFirst {
			keyBuilder.WriteByte(',')
		}
		wroteFirst = true
		keyBuilder.WriteString(strconv.FormatInt(merged.ModeID, 10))
		keyBuilder.WriteByte(':')
		if merged.Travel {
			keyBuilder.WriteByte('t')
		} else {
			keyBuilder.WriteByte('-')
		}
		if merged.Discovery {
			keyBuilder.WriteByte('d')
		} else {
			keyBuilder.WriteByte('-')
		}
	}
	return keyBuilder.String()
}

// computeExpeditionLiveStats returns mode-filtered live metrics and realization
// for a started expedition, utilizing liveStatsCache when valid. Caller must
// hold app.dbMu.RLock.
func (app *App) computeExpeditionLiveStats(
	expeditionRecord expedition.Expedition,
	filter stats.Filter,
	statsEngine *stats.Engine,
) (stats.Metrics, stats.RealizationBreakdown, error) {
	cacheKey := buildLiveStatsCacheKey(expeditionRecord, filter)

	app.liveStatsMu.Lock()
	cachedEntry, isCached := app.liveStatsCache[cacheKey]
	liveStatsGeneration := app.liveStatsGen
	app.liveStatsMu.Unlock()

	if isCached {
		return cachedEntry.metrics, cachedEntry.realization, nil
	}

	expeditionRange := stats.Range{After: expeditionRecord.StartTimestamp, Until: expeditionRecord.EndTimestamp}
	liveMetrics, computeError := statsEngine.Compute(expeditionRange, &filter)
	if computeError != nil {
		return stats.Metrics{}, stats.RealizationBreakdown{}, computeError
	}
	// Carrier operations metrics (carrier jumps, carrier distance, and tritium) for the expedition
	// come exclusively from the Spansh or manual route logger (expeditionRecord.Adjustments) to prevent duplication.
	// Non-operation personal navigation metrics (CarrierPassengerLy and total DistanceLy)
	// are preserved directly from journal events when the commander is aboard.
	liveMetrics.CarrierJumps = 0
	liveMetrics.CarrierDistanceLy = 0
	liveMetrics.TritiumUsed = nil

	liveRealization, realizationError := statsEngine.ComputeRealization(expeditionRange, &filter)
	if realizationError != nil {
		return stats.Metrics{}, stats.RealizationBreakdown{}, realizationError
	}

	// Guard cache write-back with liveStatsGen so an invalidation that ran while
	// liveStatsMu was unlocked during computation cannot be overwritten by stale results.
	app.liveStatsMu.Lock()
	if app.liveStatsGen == liveStatsGeneration {
		app.liveStatsCache[cacheKey] = cachedLiveStats{
			metrics:     liveMetrics,
			realization: liveRealization,
		}
	}
	app.liveStatsMu.Unlock()
	return liveMetrics, liveRealization, nil
}

// GetSidebarStats computes the sidebar for an expedition with mode filters.
func (app *App) GetSidebarStats(expeditionID int64, filter stats.Filter) (SidebarStats, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return SidebarStats{}, readyError
	}
	expeditionRecord, getExpeditionError := expedition.Get(app.db, expeditionID)
	if getExpeditionError != nil {
		return SidebarStats{}, getExpeditionError
	}
	sidebar := SidebarStats{
		ExpeditionID:    expeditionRecord.ID,
		Status:          expeditionRecord.Status,
		Started:         expeditionRecord.StartTimestamp != "",
		Start:           expeditionRecord.StartTimestamp,
		End:             expeditionRecord.EndTimestamp,
		FilterActive:    filter.Enabled,
		Adjustments:     expeditionRecord.Adjustments,
		BaselineEnabled: expeditionRecord.BaselineEnabled,
		BaselineSource:  expeditionRecord.BaselineSource,
		Baseline:        expeditionRecord.Baseline,
	}
	statsEngine := stats.NewEngine(app.db)
	// Mode filters apply only to the current expedition, never to All Time.
	// All-time personal carrier data comes directly from Frontier's lifetime Statistics.
	if allTimeMetrics, allTimeMetricsError := app.getAllTimeMetrics(); allTimeMetricsError == nil {
		sidebar.AllJournals = allTimeMetrics
	}
	if allRealization, allRealizationError := app.getAllTimeRealization(); allRealizationError == nil {
		sidebar.AllJournalsRealization = allRealization
	}

	// Career metrics across all ingested journals for active commanders (populated at all times).
	careerStart, careerStartGalactic := app.resolveCareerStart()
	if careerStart != "" {
		sidebar.CareerStart = careerStart
		sidebar.CareerStartGalactic = careerStartGalactic
		sidebar.CareerDuration = expedition.Elapsed(careerStart, "")
	}

	if sidebar.Started {
		if sidebar.End == "" {
			sidebar.EndIsNow = true
			sidebar.EndGalactic = ed.TimeToGalactic(timeNow())
		} else {
			sidebar.EndGalactic = ed.ToGalactic(sidebar.End)
		}
		sidebar.StartGalactic = ed.ToGalactic(sidebar.Start)
		sidebar.Duration = expedition.Elapsed(sidebar.Start, sidebar.End)

		liveMetrics, liveRealization, computeError := app.computeExpeditionLiveStats(expeditionRecord, filter, statsEngine)
		if computeError != nil {
			return sidebar, computeError
		}
		sidebar.Live = liveMetrics
		sidebar.Expedition = liveMetrics.Add(expeditionRecord.Adjustments)
		sidebar.Realization = liveRealization
	} else {
		sidebar.Expedition = expeditionRecord.Adjustments
	}
	return sidebar, nil
}

