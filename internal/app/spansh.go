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
	DepotTritium int                `json:"depotTritium"`
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
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
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
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
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
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return readyError
	}
	key := fmt.Sprintf("active_carrier_route_%d", expeditionID)
	_, deleteError := app.db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return deleteError
}

// GetCarrierLastLocation returns the system name of the most recent known Fleet Carrier location.
// It checks CarrierJump events, docking and location events aboard the fleet carrier,
// and CarrierJumpRequest events, choosing whichever candidate has the most recent timestamp.
func (app *App) GetCarrierLastLocation() (string, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return "", readyError
	}

	// Identify player carrier ID and callsign from the latest CarrierStats
	var playerCarrierID int64
	var playerCallsign string
	var rawStats string
	if err := app.db.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'CarrierStats' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawStats); err == nil && rawStats != "" {
		var stats journal.CarrierStatsEvent
		if json.Unmarshal([]byte(rawStats), &stats) == nil {
			playerCarrierID = stats.CarrierID
			playerCallsign = strings.TrimSpace(stats.Callsign)
		}
	}

	// Candidate from CarrierJump events
	var jumpSystem, jumpTimestamp string
	_ = app.db.QueryRow(`SELECT system_name, timestamp FROM journal_events
		WHERE event = 'CarrierJump' AND system_name != '' AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&jumpSystem, &jumpTimestamp)

	// Candidate from Docked or Location events aboard a fleet carrier
	var dockedSystem, dockedTimestamp string
	rows, err := app.db.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event IN ('Docked', 'Location') AND system_name != '' AND ` + journal.CmdrFilterSQL + `
		AND raw_json LIKE '%FleetCarrier%'
		ORDER BY timestamp DESC, id DESC LIMIT 20`)
	if err == nil {
		for rows.Next() {
			var sys, ts, raw string
			if rows.Scan(&sys, &ts, &raw) == nil {
				var dockInfo struct {
					StationType string `json:"StationType"`
					MarketID    int64  `json:"MarketID"`
					StationName string `json:"StationName"`
					Docked      *bool  `json:"Docked"`
				}
				if json.Unmarshal([]byte(raw), &dockInfo) == nil {
					if strings.EqualFold(dockInfo.StationType, "FleetCarrier") {
						isDocked := dockInfo.Docked == nil || *dockInfo.Docked
						if isDocked {
							matchesCarrier := true
							if playerCarrierID > 0 && dockInfo.MarketID > 0 {
								matchesCarrier = (dockInfo.MarketID == playerCarrierID)
							} else if playerCallsign != "" && dockInfo.StationName != "" {
								matchesCarrier = strings.Contains(strings.ToUpper(dockInfo.StationName), strings.ToUpper(playerCallsign))
							}
							if matchesCarrier {
								dockedSystem = strings.TrimSpace(sys)
								dockedTimestamp = ts
								break
							}
						}
					}
				}
			}
		}
		rows.Close()
	}

	// Candidate from CarrierJumpRequest (remote jump orders)
	var requestSystem, requestTimestamp string
	_ = app.db.QueryRow(`SELECT system_name, timestamp FROM journal_events
		WHERE event = 'CarrierJumpRequest' AND system_name != '' AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&requestSystem, &requestTimestamp)

	// Pick whichever candidate has the most recent timestamp
	bestSystem := jumpSystem
	bestTimestamp := jumpTimestamp

	if dockedTimestamp != "" && (bestTimestamp == "" || dockedTimestamp >= bestTimestamp) {
		bestSystem = dockedSystem
		bestTimestamp = dockedTimestamp
	}
	if requestTimestamp != "" && (bestTimestamp == "" || requestTimestamp >= bestTimestamp) {
		bestSystem = requestSystem
		bestTimestamp = requestTimestamp
	}

	return strings.TrimSpace(bestSystem), nil
}

// CarrierDepotInfo contains carrier mass and tritium depot telemetry from journal events.
type CarrierDepotInfo struct {
	UsedCapacity int `json:"usedCapacity"`
	DepotTritium int `json:"depotTritium"`
	Cargo        int `json:"cargo"`
}

// GetCarrierDepotInfo returns the latest Fleet Carrier capacity, depot tritium level, and cargo storage.
// It retrieves the actual in-game values from CarrierStats or CarrierDepositFuel journal events,
// defaulting DepotTritium to 1000 (full depot tank) and UsedCapacity to 5000 if not recorded.
func (app *App) GetCarrierDepotInfo() (CarrierDepotInfo, error) {
	info := CarrierDepotInfo{
		UsedCapacity: 5000,
		DepotTritium: 1000,
		Cargo:        0,
	}
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return info, readyError
	}

	var rawCarrierStats string
	var statsTimestamp string
	statsQueryError := app.db.QueryRow(`SELECT raw_json, timestamp FROM journal_events
		WHERE event = 'CarrierStats' AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawCarrierStats, &statsTimestamp)
	if statsQueryError == nil && rawCarrierStats != "" {
		var carrierStats journal.CarrierStatsEvent
		if json.Unmarshal([]byte(rawCarrierStats), &carrierStats) == nil {
			if used := carrierStats.UsedCapacity(); used > 0 {
				info.UsedCapacity = used
			}
			if carrierStats.FuelLevel > 0 {
				info.DepotTritium = carrierStats.FuelLevel
			}
			info.Cargo = carrierStats.SpaceUsage.Cargo
		}
	}

	var rawDeposit string
	var depositTimestamp string
	depositQueryError := app.db.QueryRow(`SELECT raw_json, timestamp FROM journal_events
		WHERE event = 'CarrierDepositFuel' AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawDeposit, &depositTimestamp)
	if depositQueryError == nil && rawDeposit != "" {
		var depositEvent journal.CarrierDepositFuelEvent
		if json.Unmarshal([]byte(rawDeposit), &depositEvent) == nil && depositEvent.Total > 0 {
			if statsTimestamp == "" || depositTimestamp >= statsTimestamp {
				info.DepotTritium = depositEvent.Total
			}
		}
	}

	if info.DepotTritium <= 0 {
		info.DepotTritium = 1000
	}
	if info.DepotTritium > 1000 {
		info.DepotTritium = 1000
	}
	return info, nil
}

// GetDefaultCarrierCapacity returns the most recently logged Fleet Carrier used capacity in tonnes,
// or 5000 if no CarrierStats event exists in the journal archive.
func (app *App) GetDefaultCarrierCapacity() int {
	info, err := app.GetCarrierDepotInfo()
	if err != nil || info.UsedCapacity <= 0 {
		return 5000
	}
	return info.UsedCapacity
}

// GetDefaultCarrierDepot returns the most recently logged Fleet Carrier depot tritium in tonnes,
// or 1000 (full tank) if no CarrierStats or CarrierDepositFuel event exists in the journal archive.
func (app *App) GetDefaultCarrierDepot() int {
	info, err := app.GetCarrierDepotInfo()
	if err != nil || info.DepotTritium <= 0 {
		return 1000
	}
	return info.DepotTritium
}

// RecordCarrierJump logs a single completed expedition carrier jump (from Spansh hop completion or Manual Mode)
// into the expedition's manual carrier adjustments.
func (app *App) RecordCarrierJump(expeditionID int64, distanceLy float64, tritium int) error {
	if err := func() error {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
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
		return expedition.SaveAdjustments(app.db, expeditionID, adjustments)
	}(); err != nil {
		return err
	}
	app.changed("all", expeditionID)
	return nil
}

func (app *App) canonicalSystemName(name string) string {
	name = strings.TrimSpace(name)
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
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
// It intercepts Spansh's native routing response, strips out its native fuel consumption estimates,
// and replaces them with in-house calculations reflecting accurate in-game mass mechanics
// (including the user's actual DepotTritium value, defaulting to 1000 tonnes for a full tank).
// If Spansh cannot plot a route (e.g. unknown system, offline, or timeout), it returns a
// CarrierRouteResult with ManualMode=true so the UI smoothly opens the bottom panel in manual mode.
func (app *App) PlotCarrierRoute(source, destination string, capacityUsed int, carrierType string, depotTritium int) (CarrierRouteResult, error) {
	source = app.canonicalSystemName(source)
	destination = app.canonicalSystemName(destination)
	if carrierType == "" {
		carrierType = "player"
	}
	maxCap := 25000
	if carrierType == "squadron" {
		maxCap = 60000
	}
	if capacityUsed < 0 {
		capacityUsed = 0
	}
	if capacityUsed > maxCap {
		capacityUsed = maxCap
	}
	if depotTritium <= 0 {
		depotInfo, _ := app.GetCarrierDepotInfo()
		depotTritium = depotInfo.DepotTritium
	}
	if depotTritium <= 0 {
		depotTritium = 1000
	}
	if depotTritium > 1000 {
		depotTritium = 1000
	}
	routeResult := CarrierRouteResult{
		Origin:       source,
		Destination:  destination,
		CarrierType:  carrierType,
		CapacityUsed: capacityUsed,
		DepotTritium: depotTritium,
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

	jumps, queryError := querySpanshCarrierRoute(reqCtx, source, destination, capacityUsed, depotTritium)
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

// SpanshRawJump represents one hop in the raw Spansh Fleet Carrier route response.
type SpanshRawJump struct {
	Name                  string  `json:"name"`
	Distance              float64 `json:"distance"`
	DistanceToDestination float64 `json:"distance_to_destination"`
	FuelUsed              int     `json:"fuel_used"`
	TritiumUsed           int     `json:"tritium_used"`
	FuelInTank            int     `json:"fuel_in_tank"`
	HasIcyRing            bool    `json:"has_icy_ring"`
}

// ReconcileSpanshJumps parses the incoming Spansh routing data to extract Distance and UsedCapacity,
// discards Spansh's native tritium consumption estimates (which artificially assume minimum depot fuel),
// and overrides each jump with the in-house in-game formula:
//
//	Calculated_Fuel = ceil(5 + (Distance * (UsedCapacity + DepotTritium + 25000)) / 200000)
func ReconcileSpanshJumps(rawJumps []SpanshRawJump, capacityUsed int, depotTritium int) []CarrierRouteJump {
	if depotTritium <= 0 {
		depotTritium = 1000
	}
	if depotTritium > 1000 {
		depotTritium = 1000
	}
	if capacityUsed < 0 {
		capacityUsed = 0
	}
	if capacityUsed > 60000 {
		capacityUsed = 60000
	}

	var carrierRouteJumps []CarrierRouteJump
	for index, jumpItem := range rawJumps {
		// The initial entry in Spansh fleet carrier results is the origin system with zero distance.
		if index == 0 && jumpItem.Distance <= 0 {
			continue
		}

		// Discard Spansh's native fuel estimates because Spansh assumes an artificially low depot mass.
		// Calculate correct in-game fuel consumption using:
		// Calculated_Fuel = ceil(5 + (Distance * (UsedCapacity + DepotTritium + 25000)) / 200000)
		calculatedFuel := ed.CarrierTritiumCost(jumpItem.Distance, capacityUsed, depotTritium)

		carrierRouteJumps = append(carrierRouteJumps, CarrierRouteJump{
			SystemName:       jumpItem.Name,
			DistanceLy:       jumpItem.Distance,
			DistanceToDestLy: jumpItem.DistanceToDestination,
			TritiumUsed:      calculatedFuel,
			FuelInTank:       depotTritium,
			HasIcyRing:       jumpItem.HasIcyRing,
		})
	}
	return carrierRouteJumps
}

type spanshPollResp struct {
	Job    string `json:"job"`
	Status string `json:"status"`
	Error  string `json:"error"`
	Result struct {
		Jumps []SpanshRawJump `json:"jumps"`
	} `json:"result"`
}

func querySpanshCarrierRoute(ctx context.Context, source, destination string, capacityUsed int, depotTritium int) ([]CarrierRouteJump, error) {
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
				return ReconcileSpanshJumps(poll.Result.Jumps, capacityUsed, depotTritium), nil
			}
		}
	}
}
