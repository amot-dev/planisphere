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
func (a *App) GetActiveCarrierRoute(expID int64) (*ActiveCarrierRouteState, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("active_carrier_route_%d", expID)
	val, err := db.GetSetting(a.db, key)
	if err != nil || val == "" {
		return nil, nil
	}
	var state ActiveCarrierRouteState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, nil
	}
	return &state, nil
}

// SaveActiveCarrierRoute persists the active carrier route state for an expedition.
func (a *App) SaveActiveCarrierRoute(state ActiveCarrierRouteState) error {
	if err := a.ready(); err != nil {
		return err
	}
	if state.ExpeditionID <= 0 {
		return fmt.Errorf("invalid expedition id: %d", state.ExpeditionID)
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("active_carrier_route_%d", state.ExpeditionID)
	return db.SetSetting(a.db, key, string(b))
}

// ClearActiveCarrierRoute deletes the active carrier route for an expedition.
func (a *App) ClearActiveCarrierRoute(expID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	key := fmt.Sprintf("active_carrier_route_%d", expID)
	_, err := a.db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return err
}

// GetCarrierLastLocation returns the system name of the most recent CarrierJump event.
func (a *App) GetCarrierLastLocation() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	var sys string
	err := a.db.QueryRow(`SELECT system_name FROM journal_events
		WHERE event = 'CarrierJump' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&sys)
	if err != nil {
		return "", nil
	}
	return sys, nil
}

// GetDefaultCarrierCapacity returns the most recently logged Fleet Carrier used capacity in tonnes,
// or 5000 if no CarrierStats event exists in the journal archive.
func (a *App) GetDefaultCarrierCapacity() int {
	if err := a.ready(); err != nil {
		return 5000
	}
	var raw string
	err := a.db.QueryRow(`SELECT raw_json FROM journal_events WHERE event = 'CarrierStats' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&raw)
	if err != nil || raw == "" {
		return 5000
	}
	var cs journal.CarrierStatsEvent
	if json.Unmarshal([]byte(raw), &cs) != nil {
		return 5000
	}
	if used := cs.UsedCapacity(); used > 0 {
		return used
	}
	return 5000
}

// RecordCarrierJump logs a single completed expedition carrier jump (from Spansh hop completion or Manual Mode)
// into the expedition's manual carrier adjustments.
func (a *App) RecordCarrierJump(expID int64, distanceLy float64, tritium int) error {
	if err := a.ready(); err != nil {
		return err
	}
	e, err := expedition.Get(a.db, expID)
	if err != nil {
		return err
	}
	adj := e.Adjustments
	adj.CarrierJumps++
	if distanceLy > 0 {
		adj.CarrierDistanceLy += distanceLy
	}
	if tritium > 0 {
		cur := 0
		if adj.TritiumUsed != nil {
			cur = *adj.TritiumUsed
		}
		next := cur + tritium
		adj.TritiumUsed = &next
	}
	if err := expedition.SaveAdjustments(a.db, expID, adj); err != nil {
		return err
	}
	a.changed("all", expID)
	return nil
}

func (a *App) canonicalSystemName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || a.db == nil {
		return name
	}
	var canonical string
	if err := a.db.QueryRow(`SELECT system_name FROM journal_events WHERE system_name = ? COLLATE NOCASE LIMIT 1`, name).Scan(&canonical); err == nil && canonical != "" {
		return canonical
	}
	nameKey := strings.ToLower(strings.Join(strings.Fields(name), " "))
	if err := a.db.QueryRow(`SELECT system_name FROM system_cache WHERE name_key = ? LIMIT 1`, nameKey).Scan(&canonical); err == nil && canonical != "" {
		return canonical
	}
	return name
}

// PlotCarrierRoute queries the Spansh Fleet Carrier router for a path from source to destination.
// If Spansh cannot plot a route (e.g. unknown system, offline, or timeout), it returns a
// CarrierRouteResult with ManualMode=true so the UI smoothly opens the bottom panel in manual mode.
func (a *App) PlotCarrierRoute(source, destination string, capacityUsed int, carrierType string) (CarrierRouteResult, error) {
	source = a.canonicalSystemName(source)
	destination = a.canonicalSystemName(destination)
	if capacityUsed < 0 {
		capacityUsed = 0
	}
	if capacityUsed > 25000 {
		capacityUsed = 25000
	}
	if carrierType == "" {
		carrierType = "player"
	}
	res := CarrierRouteResult{
		Origin:       source,
		Destination:  destination,
		CarrierType:  carrierType,
		CapacityUsed: capacityUsed,
		Jumps:        []CarrierRouteJump{},
	}
	if source == "" || destination == "" {
		res.ManualMode = true
		res.Error = "Origin or destination system name is empty — switched to Manual Mode."
		return res, nil
	}
	if strings.EqualFold(source, destination) {
		res.ManualMode = true
		res.Error = "Origin and destination are the same system — switched to Manual Mode."
		return res, nil
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	jumps, err := querySpanshCarrierRoute(reqCtx, source, destination, capacityUsed)
	if err != nil || len(jumps) == 0 {
		res.ManualMode = true
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Error = "Spansh returned no jumps for this route — switched to Manual Mode."
		}
		return res, nil
	}

	res.Jumps = jumps
	res.TotalJumps = len(jumps)
	for _, j := range jumps {
		res.TotalDistLy += j.DistanceLy
		res.TotalTritium += j.TritiumUsed
	}
	return res, nil
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://spansh.co.uk/api/fleetcarrier/route", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", version.UserAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Spansh API: %w", err)
	}
	defer resp.Body.Close()

	var job spanshJobResp
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("invalid Spansh response (HTTP %d)", resp.StatusCode)
	}
	if job.Job == "" && job.JobID != "" {
		job.Job = job.JobID
	}
	if job.Error != "" {
		return nil, fmt.Errorf("Spansh: %s", job.Error)
	}
	if job.Job == "" {
		return nil, fmt.Errorf("Spansh did not return a job ID (HTTP %d)", resp.StatusCode)
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
			pReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
			if err != nil {
				return nil, err
			}
			pReq.Header.Set("User-Agent", version.UserAgent)
			pResp, err := client.Do(pReq)
			if err != nil {
				continue
			}
			var poll spanshPollResp
			decErr := json.NewDecoder(pResp.Body).Decode(&poll)
			pResp.Body.Close()
			if decErr != nil {
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
				var out []CarrierRouteJump
				for idx, j := range poll.Result.Jumps {
					// Index 0 in Spansh fleet carrier results is the origin system with distance 0.
					if idx == 0 && j.Distance <= 0 {
						continue
					}
					trit := j.FuelUsed
					if trit <= 0 {
						trit = j.TritiumUsed
					}
					if trit <= 0 && j.Distance > 0 {
						fuel := j.FuelInTank
						if fuel <= 0 {
							fuel = 1000
						}
						trit = ed.CarrierTritiumCost(j.Distance, capacityUsed, fuel)
					}
					out = append(out, CarrierRouteJump{
						SystemName:       j.Name,
						DistanceLy:       j.Distance,
						DistanceToDestLy: j.DistanceToDestination,
						TritiumUsed:      trit,
						FuelInTank:       j.FuelInTank,
						HasIcyRing:       j.HasIcyRing,
					})
				}
				return out, nil
			}
		}
	}
}
