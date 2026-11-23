package app

import (
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

// SidebarStats is the content of the Stats Sidebar.
type SidebarStats struct {
	ExpeditionID           int64                      `json:"expeditionId"`
	Status                 string                     `json:"status"`
	Started                bool                       `json:"started"`
	Departed               bool                       `json:"departed"`
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
	Live                   stats.Metrics              `json:"live"`                   // from journals (mode-filtered)
	Adjustments            stats.Metrics              `json:"adjustments"`            // manual
	Expedition             stats.Metrics              `json:"expedition"`             // live + adjustments
	AllJournals            stats.Metrics              `json:"allJournals"`            // all ingested journal events (unfiltered)
	Realization            stats.RealizationBreakdown `json:"realization"`            // expedition realized earnings and losses (mode-filtered)
	AllJournalsRealization stats.RealizationBreakdown `json:"allJournalsRealization"` // all-time realized earnings and losses across all journals (unfiltered)
	CarrierCost            expedition.CarrierCost     `json:"carrierCost"`
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

	var effectiveDisplayStart string
	if sidebar.Started {
		queryAfter, displayStart, departed, startError := expedition.EffectiveStart(app.db, expeditionRecord)
		if startError != nil {
			return sidebar, startError
		}
		sidebar.Departed = departed
		effectiveDisplayStart = displayStart
		if departed {
			sidebar.Start = displayStart
			sidebar.StartGalactic = reference.ToGalactic(displayStart)
			if sidebar.End == "" {
				sidebar.EndIsNow = true
				sidebar.EndGalactic = reference.TimeToGalactic(timeNow())
			} else {
				sidebar.EndGalactic = reference.ToGalactic(sidebar.End)
			}
			sidebar.Duration = reference.Elapsed(displayStart, sidebar.End)

			expeditionRange := stats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
			if filter.Enabled {
				filter.ExpeditionID = expeditionID
			}
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
	} else {
		sidebar.Expedition = manualAdjustments
	}
	sidebar.CarrierCost = expedition.ComputeCarrierCost(app.db, expeditionRecord, sidebar.Expedition, effectiveDisplayStart, sidebar.Departed, timeNow())
	return sidebar, nil
}
