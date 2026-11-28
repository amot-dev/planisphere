package app

import (
	"fmt"

	"planisphere/internal/reference"
	"planisphere/internal/replay"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
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
	InGameDuration         string                     `json:"inGameDuration,omitempty"`
	CareerStart            string                     `json:"careerStart"`
	CareerStartGalactic    string                     `json:"careerStartGalactic"`
	CareerDuration         string                     `json:"careerDuration"`
	CareerInGameDuration   string                     `json:"careerInGameDuration,omitempty"`
	FilterActive           bool                       `json:"filterActive"`
	AllTimeLoaded          bool                       `json:"allTimeLoaded"`
	Live                   stats.Metrics              `json:"live"`                   // from journals (mode-filtered)
	Adjustments            stats.Metrics              `json:"adjustments"`            // manual
	Expedition             stats.Metrics              `json:"expedition"`             // live + adjustments
	AllJournals            stats.Metrics              `json:"allJournals"`            // all ingested journal events (unfiltered)
	Realization            stats.RealizationBreakdown `json:"realization"`            // expedition realized earnings and losses (mode-filtered)
	AllJournalsRealization stats.RealizationBreakdown `json:"allJournalsRealization"` // all-time realized earnings and losses across all journals (unfiltered)
	CarrierCost            expedition.CarrierCost     `json:"carrierCost"`
}

// GetSidebarStats computes the sidebar for an expedition with mode filters,
// computing career-wide All Time metrics only when includeAllTime is true.
func (app *App) GetSidebarStats(expeditionID int64, filter stats.Filter, includeAllTime bool) (SidebarStats, error) {
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

	expScope := expeditionRecord.ProfileScope()
	if includeAllTime {
		careerScope := expScope
		if settings.CareerBaselineScope(app.db) == settings.CareerScopeAllEnabled {
			careerScope = replay.ProfileScope{}
		}
		careerScope = replay.NormalizeAllTimeProfileScope(app.db, careerScope)

		// Mode filters apply only to the current expedition, never to All Time.
		// All-time personal carrier data comes directly from Frontier's lifetime Statistics.
		if app.careerStats != nil {
			allTimeSummary, careerError := app.careerStats.GetScoped(careerScope)
			if careerError != nil {
				return sidebar, fmt.Errorf("compute all-time career stats: %w", careerError)
			}
			sidebar.AllJournals = allTimeSummary.Metrics
			sidebar.AllJournalsRealization = allTimeSummary.Realization
		}

		// Career metrics across ingested journals for the configured career scope.
		if careerStart, careerStartGalactic := replay.CareerStartScoped(app.db, careerScope); careerStart != "" {
			sidebar.CareerStart = careerStart
			sidebar.CareerStartGalactic = careerStartGalactic
			sidebar.CareerDuration = reference.Elapsed(careerStart, "")
		}
		if app.playSessions != nil {
			careerPlayDuration, hasCareerPlay, careerPlayError := app.playSessions.CareerDurationScoped(careerScope)
			if careerPlayError != nil {
				return sidebar, fmt.Errorf("compute career play duration: %w", careerPlayError)
			}
			if hasCareerPlay {
				sidebar.CareerInGameDuration = reference.PlayDurationLabel(careerPlayDuration)
			}
		}
		sidebar.AllTimeLoaded = true
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
			if app.playSessions != nil {
				expeditionSessions, sessionError := app.playSessions.SessionsScoped(displayStart, sidebar.End, expScope)
				if sessionError != nil {
					return sidebar, fmt.Errorf("compute expedition play sessions: %w", sessionError)
				}
				if expeditionPlayDuration, hasExpeditionPlay := replay.SumPlayDuration(expeditionSessions, displayStart, sidebar.End); hasExpeditionPlay {
					sidebar.InGameDuration = reference.PlayDurationLabel(expeditionPlayDuration)
				}
			}

			expeditionRange := stats.Range{After: queryAfter, Until: expeditionRecord.EndTimestamp}
			filter.Scope = expScope
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
