package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/version"
)

// CarrierRouteJump is one hop in a plotted Spansh Fleet Carrier route.
type CarrierRouteJump struct {
	SystemName       string  `json:"systemName"`
	DistanceLy       float64 `json:"distanceLy"`
	DistanceToDestLy float64 `json:"distanceToDestLy"`
	TritiumUsed      int     `json:"tritiumUsed"`
	FuelInTank       int     `json:"fuelInTank"`
	HasIcyRing       bool    `json:"hasIcyRing"`
}

// CarrierRouteResult is returned to the UI when initiating a carrier route to a waypoint.
type CarrierRouteResult struct {
	Origin       string             `json:"origin"`
	Destination  string             `json:"destination"`
	CarrierType  string             `json:"carrierType"` // "player" | "squadron"
	CapacityUsed int                `json:"capacityUsed"`
	ManualMode   bool               `json:"manualMode"`
	Error        string             `json:"error,omitempty"`
	TotalJumps   int                `json:"totalJumps"`
	TotalDistLy  float64            `json:"totalDistLy"`
	TotalTritium int                `json:"totalTritium"`
	Jumps        []CarrierRouteJump `json:"jumps"`
}

// ActiveCarrierRouteState stores the active fleet carrier navigation state
// for an expedition, persisting across application restarts and sessions.
type ActiveCarrierRouteState struct {
	ExpeditionID    int64              `json:"expeditionId"`
	TargetRowID     int64              `json:"targetRowId"`
	Route           CarrierRouteResult `json:"route"`
	JumpIdx         int                `json:"jumpIdx"`
	ManualMode      bool               `json:"manualMode"`
	ManualDistInput string             `json:"manualDistInput"`
	ManualTritInput string             `json:"manualTritInput"`
	ManualLegJumps  int                `json:"manualLegJumps"`
	ManualLegDist   float64            `json:"manualLegDist"`
	ManualLegTrit   int                `json:"manualLegTrit"`
	Minimized       bool               `json:"minimized"`
}

// GetActiveCarrierRoute retrieves the persisted active carrier route for an expedition.
func (app *App) GetActiveCarrierRoute(expeditionID int64) (*ActiveCarrierRouteState, error) {
	if readyError := app.ready(); readyError != nil {
		return nil, readyError
	}
	key := fmt.Sprintf("active_carrier_route_%d", expeditionID)
	routeSettingValue, getError := db.GetSetting(app.db, key)
	if getError != nil || routeSettingValue == "" {
		return nil, nil
	}
	var state ActiveCarrierRouteState
	if unmarshalError := json.Unmarshal([]byte(routeSettingValue), &state); unmarshalError != nil {
		return nil, nil
	}
	return &state, nil
}

// SaveActiveCarrierRoute persists the active carrier route state for an expedition.
func (app *App) SaveActiveCarrierRoute(state ActiveCarrierRouteState) error {
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	if state.ExpeditionID <= 0 {
		return fmt.Errorf("invalid expedition id: %d", state.ExpeditionID)
	}
	routeJSON, marshalError := json.Marshal(state)
	if marshalError != nil {
		return marshalError
	}
	key := fmt.Sprintf("active_carrier_route_%d", state.ExpeditionID)
	return db.SetSetting(app.db, key, string(routeJSON))
}

// ClearActiveCarrierRoute deletes the active carrier route for an expedition.
func (app *App) ClearActiveCarrierRoute(expeditionID int64) error {
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	key := fmt.Sprintf("active_carrier_route_%d", expeditionID)
	_, deleteError := app.db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return deleteError
}

// GetCarrierLastLocation returns the system name of the most recent CarrierJump event.
func (app *App) GetCarrierLastLocation() (string, error) {
	if readyError := app.ready(); readyError != nil {
		return "", readyError
	}
	var systemName string
	queryError := app.db.QueryRow(`SELECT system_name FROM journal_events
		WHERE event = 'CarrierJump' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&systemName)
	if queryError != nil {
		return "", nil
	}
	return systemName, nil
}

// GetDefaultCarrierCapacity returns the most recently logged Fleet Carrier used capacity in tonnes,
// or 5000 if no CarrierStats event exists in the journal archive.
func (app *App) GetDefaultCarrierCapacity() int {
	if readyError := app.ready(); readyError != nil {
		return 5000
	}
	var rawJSON string
	queryError := app.db.QueryRow(`SELECT raw_json FROM journal_events WHERE event = 'CarrierStats' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawJSON)
	if queryError != nil || rawJSON == "" {
		return 5000
	}
	var carrierStats journal.CarrierStatsEvent
	if json.Unmarshal([]byte(rawJSON), &carrierStats) != nil {
		return 5000
	}
	if usedCapacity := carrierStats.UsedCapacity(); usedCapacity > 0 {
		return usedCapacity
	}
	return 5000
}

// RecordCarrierJump logs a single completed expedition carrier jump (from Spansh hop completion or Manual Mode)
// into the expedition's manual carrier adjustments.
func (app *App) RecordCarrierJump(expeditionID int64, distanceLy float64, tritium int) error {
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	expeditionRecord, getError := expedition.Get(app.db, expeditionID)
	if getError != nil {
		return getError
	}
	adjustments := expeditionRecord.Adjustments
	adjustments.CarrierJumps++
	if distanceLy > 0 {
		adjustments.CarrierDistanceLy += distanceLy
	}
	if tritium > 0 {
		currentTritium := 0
		if adjustments.TritiumUsed != nil {
			currentTritium = *adjustments.TritiumUsed
		}
		nextTritium := currentTritium + tritium
		adjustments.TritiumUsed = &nextTritium
	}
	if saveError := expedition.SaveAdjustments(app.db, expeditionID, adjustments); saveError != nil {
		return saveError
	}
	app.changed("all", expeditionID)
	return nil
}

func (app *App) canonicalSystemName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || app.db == nil {
		return name
	}
	var canonical string
	if queryError := app.db.QueryRow(`SELECT system_name FROM journal_events WHERE system_name = ? COLLATE NOCASE LIMIT 1`, name).Scan(&canonical); queryError == nil && canonical != "" {
		return canonical
	}
	nameKey := strings.ToLower(strings.Join(strings.Fields(name), " "))
	if queryError := app.db.QueryRow(`SELECT system_name FROM system_cache WHERE name_key = ? LIMIT 1`, nameKey).Scan(&canonical); queryError == nil && canonical != "" {
		return canonical
	}
	return name
}

// PlotCarrierRoute queries the Spansh Fleet Carrier router for a path from source to destination.
// If Spansh cannot plot a route (e.g. unknown system, offline, or timeout), it returns a
// CarrierRouteResult with ManualMode=true so the UI smoothly opens the bottom panel in manual mode.
func (app *App) PlotCarrierRoute(source, destination string, capacityUsed int, carrierType string) (CarrierRouteResult, error) {
	source = app.canonicalSystemName(source)
	destination = app.canonicalSystemName(destination)
	if capacityUsed < 0 {
		capacityUsed = 0
	}
	if capacityUsed > 25000 {
		capacityUsed = 25000
	}
	if carrierType == "" {
		carrierType = "player"
	}
	routeResult := CarrierRouteResult{
		Origin:       source,
		Destination:  destination,
		CarrierType:  carrierType,
		CapacityUsed: capacityUsed,
		Jumps:        []CarrierRouteJump{},
	}
	if source == "" || destination == "" {
		routeResult.ManualMode = true
		routeResult.Error = "Origin or destination system name is empty — switched to Manual Mode."
		return routeResult, nil
	}
	if strings.EqualFold(source, destination) {
		routeResult.ManualMode = true
		routeResult.Error = "Origin and destination are the same system — switched to Manual Mode."
		return routeResult, nil
	}

	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	jumps, queryError := querySpanshCarrierRoute(reqCtx, source, destination, capacityUsed)
	if queryError != nil || len(jumps) == 0 {
		routeResult.ManualMode = true
		if queryError != nil {
			routeResult.Error = queryError.Error()
		} else {
			routeResult.Error = "Spansh returned no jumps for this route — switched to Manual Mode."
		}
		return routeResult, nil
	}

	routeResult.Jumps = jumps
	routeResult.TotalJumps = len(jumps)
	for _, jump := range jumps {
		routeResult.TotalDistLy += jump.DistanceLy
		routeResult.TotalTritium += jump.TritiumUsed
	}
	return routeResult, nil
}

type spanshJobResp struct {
	Job    string `json:"job"`
	JobID  string `json:"job_id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type spanshPollResp struct {
	Job    string `json:"job"`
	Status string `json:"status"`
	Error  string `json:"error"`
	Result struct {
		Jumps []struct {
			Name                  string  `json:"name"`
			Distance              float64 `json:"distance"`
			DistanceToDestination float64 `json:"distance_to_destination"`
			FuelUsed              int     `json:"fuel_used"`
			TritiumUsed           int     `json:"tritium_used"`
			FuelInTank            int     `json:"fuel_in_tank"`
			HasIcyRing            bool    `json:"has_icy_ring"`
		} `json:"jumps"`
	} `json:"result"`
}

func querySpanshCarrierRoute(ctx context.Context, source, destination string, capacityUsed int) ([]CarrierRouteJump, error) {
	form := url.Values{}
	form.Set("source", source)
	form.Set("destinations", destination)
	form.Set("capacity_used", strconv.Itoa(capacityUsed))
	form.Set("calculate_tritium", "1")

	httpRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPost, "https://spansh.co.uk/api/fleetcarrier/route", strings.NewReader(form.Encode()))
	if requestError != nil {
		return nil, requestError
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpRequest.Header.Set("User-Agent", version.UserAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	httpResponse, responseError := client.Do(httpRequest)
	if responseError != nil {
		return nil, fmt.Errorf("could not reach Spansh API: %w", responseError)
	}
	defer httpResponse.Body.Close()

	var job spanshJobResp
	if decodeError := json.NewDecoder(httpResponse.Body).Decode(&job); decodeError != nil {
		return nil, fmt.Errorf("invalid Spansh response (HTTP %d)", httpResponse.StatusCode)
	}
	if job.Job == "" && job.JobID != "" {
		job.Job = job.JobID
	}
	if job.Error != "" {
		return nil, fmt.Errorf("Spansh: %s", job.Error)
	}
	if job.Job == "" {
		return nil, fmt.Errorf("Spansh did not return a job ID (HTTP %d)", httpResponse.StatusCode)
	}

	pollURL := "https://spansh.co.uk/api/results/" + url.PathEscape(job.Job)
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Spansh route calculation timed out")
		case <-ticker.C:
			pollRequest, pollRequestError := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
			if pollRequestError != nil {
				return nil, pollRequestError
			}
			pollRequest.Header.Set("User-Agent", version.UserAgent)
			pollResponse, pollResponseError := client.Do(pollRequest)
			if pollResponseError != nil {
				continue
			}
			var poll spanshPollResp
			decodePollError := json.NewDecoder(pollResponse.Body).Decode(&poll)
			pollResponse.Body.Close()
			if decodePollError != nil {
				continue
			}
			if poll.Error != "" {
				// Spansh newly queued jobs can transiently return "Job not found" (or 404)
				// during the first few seconds before Redis/worker distribution. Allow retries.
				if strings.Contains(strings.ToLower(poll.Error), "not found") && time.Since(startTime) < 8*time.Second {
					continue
				}
				return nil, fmt.Errorf("Spansh router: %s", poll.Error)
			}
			if poll.Status == "queued" {
				continue
			}
			if poll.Status == "ok" {
				var carrierRouteJumps []CarrierRouteJump
				for index, jumpItem := range poll.Result.Jumps {
					// Index 0 in Spansh fleet carrier results is the origin system with distance 0.
					if index == 0 && jumpItem.Distance <= 0 {
						continue
					}
					tritiumAmount := jumpItem.FuelUsed
					if tritiumAmount <= 0 {
						tritiumAmount = jumpItem.TritiumUsed
					}
					if tritiumAmount <= 0 && jumpItem.Distance > 0 {
						tankFuel := jumpItem.FuelInTank
						if tankFuel <= 0 {
							tankFuel = 1000
						}
						tritiumAmount = ed.CarrierTritiumCost(jumpItem.Distance, capacityUsed, tankFuel)
					}
					carrierRouteJumps = append(carrierRouteJumps, CarrierRouteJump{
						SystemName:       jumpItem.Name,
						DistanceLy:       jumpItem.Distance,
						DistanceToDestLy: jumpItem.DistanceToDestination,
						TritiumUsed:      tritiumAmount,
						FuelInTank:       jumpItem.FuelInTank,
						HasIcyRing:       jumpItem.HasIcyRing,
					})
				}
				return carrierRouteJumps, nil
			}
		}
	}
}
