package app

import (
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

// CarrierCostBreakdown holds the expedition Fleet Carrier cost breakdown in credits.
type CarrierCostBreakdown struct {
	CarrierType    string `json:"carrierType"`
	WeeklyUpkeep   int64  `json:"weeklyUpkeep"`
	TritiumPrice   int64  `json:"tritiumPrice"`
	TritiumCost    int64  `json:"tritiumCost"`
	JumpCost       int64  `json:"jumpCost"`
	UpkeepCost     int64  `json:"upkeepCost"`
	TotalCost      int64  `json:"totalCost"`
	TotalEstimated bool   `json:"totalEstimated"`
}

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
	Realization            stats.RealizationBreakdown `json:"realization"`            // expedition realized earnings and losses (mode-filtered)
	AllJournalsRealization stats.RealizationBreakdown `json:"allJournalsRealization"` // all-time realized earnings and losses across all journals (unfiltered)
	CarrierCost            CarrierCostBreakdown       `json:"carrierCost"`
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
	manualAdjustments := expeditionRecord.Adjustments.ToMetrics()
	sidebar := SidebarStats{
		ExpeditionID: expeditionRecord.ID,
		Status:       expeditionRecord.Status,
		Started:      expeditionRecord.StartTimestamp != "",
		Start:        expeditionRecord.StartTimestamp,
		End:          expeditionRecord.EndTimestamp,
		FilterActive: filter.Enabled,
		Adjustments:  manualAdjustments,
	}

	// Mode filters apply only to the current expedition, never to All Time.
	// All-time personal carrier data comes directly from Frontier's lifetime Statistics.
	if app.careerStats != nil {
		if allTimeSummary, careerError := app.careerStats.Get(); careerError == nil {
			sidebar.AllJournals = allTimeSummary.Metrics
			sidebar.AllJournalsRealization = allTimeSummary.Realization
		}
	}

	// Career metrics across all ingested journals for active commanders (populated at all times).
	if careerStart, careerStartGalactic := replay.CareerStart(app.db); careerStart != "" {
		sidebar.CareerStart = careerStart
		sidebar.CareerStartGalactic = careerStartGalactic
		sidebar.CareerDuration = reference.Elapsed(careerStart, "")
	}

	if sidebar.Started {
		if sidebar.End == "" {
			sidebar.EndIsNow = true
			sidebar.EndGalactic = reference.TimeToGalactic(timeNow())
		} else {
			sidebar.EndGalactic = reference.ToGalactic(sidebar.End)
		}
		sidebar.StartGalactic = reference.ToGalactic(sidebar.Start)
		sidebar.Duration = reference.Elapsed(sidebar.Start, sidebar.End)

		expeditionRange := stats.Range{After: expeditionRecord.StartTimestamp, Until: expeditionRecord.EndTimestamp}
		expeditionSummary, computeError := app.timeRangeStats.Compute(expeditionRange, &filter)
		if computeError != nil {
			return sidebar, computeError
		}
		// Expedition carrier operations (CarrierJumps, CarrierDistanceLy, and TritiumUsed)
		// come exclusively from the Spansh or manual route logger (expeditionRecord.Adjustments).
		sidebar.Live = expeditionSummary.Metrics
		sidebar.Expedition = expeditionSummary.Metrics.Add(manualAdjustments)
		sidebar.Realization = expeditionSummary.Realization
	} else {
		sidebar.Expedition = manualAdjustments
	}
	sidebar.CarrierCost = app.computeExpeditionCarrierCost(expeditionRecord, sidebar.Expedition, sidebar.Started)
	return sidebar, nil
}

func (app *App) computeExpeditionCarrierCost(
	expeditionRecord expedition.Expedition,
	expeditionMetrics stats.Metrics,
	expeditionStarted bool,
) CarrierCostBreakdown {
	tritiumPrice := app.tritiumPricePerTonne()
	var tritiumTonnes int64
	if expeditionMetrics.TritiumUsed != nil && *expeditionMetrics.TritiumUsed > 0 {
		tritiumTonnes = int64(*expeditionMetrics.TritiumUsed)
	}
	tritiumCost := tritiumTonnes * tritiumPrice
	jumpCost := int64(expeditionMetrics.CarrierJumps) * reference.CarrierJumpMaintenanceCost
	var upkeepCost int64
	if expeditionStarted && expeditionRecord.CarrierWeeklyUpkeep > 0 {
		upkeepCost = reference.CarrierProratedUpkeep(
			expeditionRecord.StartTimestamp,
			expeditionRecord.EndTimestamp,
			expeditionRecord.CarrierWeeklyUpkeep,
			timeNow(),
		)
	}
	totalCost := tritiumCost + jumpCost + upkeepCost
	return CarrierCostBreakdown{
		CarrierType:    expeditionRecord.CarrierType,
		WeeklyUpkeep:   expeditionRecord.CarrierWeeklyUpkeep,
		TritiumPrice:   tritiumPrice,
		TritiumCost:    tritiumCost,
		JumpCost:       jumpCost,
		UpkeepCost:     upkeepCost,
		TotalCost:      totalCost,
		TotalEstimated: tritiumCost > 0 || upkeepCost > 0,
	}
}

