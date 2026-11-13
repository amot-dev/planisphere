// Package spansh queries the Spansh Fleet Carrier router HTTP API and
// reconciles jump fuel estimates with in-game carrier mass mechanics.
package spansh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"planisphere/internal/ed"
	"planisphere/internal/version"
)

// RouteJump is one reconciled hop in a plotted Spansh Fleet Carrier route.
type RouteJump struct {
	SystemName       string  `json:"systemName"`
	DistanceLy       float64 `json:"distanceLy"`
	DistanceToDestLy float64 `json:"distanceToDestLy"`
	TritiumUsed      int     `json:"tritiumUsed"`
	FuelInTank       int     `json:"fuelInTank"`
	HasIcyRing       bool    `json:"hasIcyRing"`
}

// RawJump represents one hop in the raw Spansh Fleet Carrier route response.
type RawJump struct {
	Name                  string  `json:"name"`
	Distance              float64 `json:"distance"`
	DistanceToDestination float64 `json:"distance_to_destination"`
	FuelUsed              int     `json:"fuel_used"`
	TritiumUsed           int     `json:"tritium_used"`
	FuelInTank            int     `json:"fuel_in_tank"`
	HasIcyRing            bool    `json:"has_icy_ring"`
}

type jobSubmissionResponse struct {
	Job    string `json:"job"`
	JobID  string `json:"job_id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type jobPollResponse struct {
	Job    string `json:"job"`
	Status string `json:"status"`
	Error  string `json:"error"`
	Result struct {
		Jumps []RawJump `json:"jumps"`
	} `json:"result"`
}

var (
	baseURL      = "https://spansh.co.uk"
	pollInterval = 750 * time.Millisecond
)

// ReconcileJumps parses the incoming Spansh routing data to extract Distance and
// capacityUsed, discards Spansh's native tritium consumption estimates (which
// artificially assume minimum depot fuel), and overrides each jump with the
// in-house in-game formula:
//
//	Calculated_Fuel = round(5 + (Distance * (UsedCapacity + DepotTritium + 25000)) / 200000)
func ReconcileJumps(rawJumps []RawJump, capacityUsed int, depotTritium int) []RouteJump {
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

	var reconciledJumps []RouteJump
	for jumpIndex, jumpItem := range rawJumps {
		// The initial entry in Spansh fleet carrier results is the origin system with zero distance.
		if jumpIndex == 0 && jumpItem.Distance <= 0 {
			continue
		}

		// Discard Spansh's native fuel estimates because Spansh assumes an artificially low depot mass.
		// Calculate correct in-game fuel consumption using:
		// Calculated_Fuel = round(5 + (Distance * (UsedCapacity + DepotTritium + 25000)) / 200000)
		calculatedFuel := ed.CarrierTritiumCost(jumpItem.Distance, capacityUsed, depotTritium)

		reconciledJumps = append(reconciledJumps, RouteJump{
			SystemName:       jumpItem.Name,
			DistanceLy:       jumpItem.Distance,
			DistanceToDestLy: jumpItem.DistanceToDestination,
			TritiumUsed:      calculatedFuel,
			FuelInTank:       depotTritium,
			HasIcyRing:       jumpItem.HasIcyRing,
		})
	}
	return reconciledJumps
}

func submitRouteJob(ctx context.Context, httpClient *http.Client, sourceSystem, destinationSystem string, capacityUsed int) (string, error) {
	formValues := url.Values{}
	formValues.Set("source", sourceSystem)
	formValues.Set("destinations", destinationSystem)
	formValues.Set("capacity_used", strconv.Itoa(capacityUsed))
	formValues.Set("calculate_tritium", "1")

	httpRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/fleetcarrier/route", strings.NewReader(formValues.Encode()))
	if requestError != nil {
		return "", requestError
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpRequest.Header.Set("User-Agent", version.UserAgent)

	httpResponse, responseError := httpClient.Do(httpRequest)
	if responseError != nil {
		return "", fmt.Errorf("could not reach Spansh API: %w", responseError)
	}
	defer httpResponse.Body.Close()

	var job jobSubmissionResponse
	if decodeError := json.NewDecoder(httpResponse.Body).Decode(&job); decodeError != nil {
		return "", fmt.Errorf("invalid Spansh response (HTTP %d)", httpResponse.StatusCode)
	}
	if job.Job == "" && job.JobID != "" {
		job.Job = job.JobID
	}
	if job.Error != "" {
		return "", fmt.Errorf("Spansh: %s", job.Error)
	}
	if job.Job == "" {
		return "", fmt.Errorf("Spansh did not return a job ID (HTTP %d)", httpResponse.StatusCode)
	}
	return job.Job, nil
}

func pollRouteJob(ctx context.Context, httpClient *http.Client, jobID string, capacityUsed int, depotTritium int) ([]RouteJump, error) {
	pollURL := baseURL + "/api/results/" + url.PathEscape(jobID)
	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()
	startTime := time.Now()

	// Intentionally keep polling until ctx.Done() expires rather than failing fast on
	// transient transport failures, non-JSON gateway responses, or intermediate job states,
	// so Spansh worker queue propagation and brief upstream gateway blips self-recover.
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Spansh route calculation canceled or timed out: %w", ctx.Err())
		case <-pollTicker.C:
			pollRequest, pollRequestError := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
			if pollRequestError != nil {
				return nil, pollRequestError
			}
			pollRequest.Header.Set("User-Agent", version.UserAgent)
			pollResponse, pollResponseError := httpClient.Do(pollRequest)
			if pollResponseError != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("Spansh route calculation canceled or timed out: %w", ctx.Err())
				}
				if errors.Is(pollResponseError, context.Canceled) || errors.Is(pollResponseError, context.DeadlineExceeded) {
					return nil, fmt.Errorf("Spansh route calculation canceled or timed out: %w", pollResponseError)
				}
				continue
			}
			var poll jobPollResponse
			decodePollError := json.NewDecoder(pollResponse.Body).Decode(&poll)
			pollResponse.Body.Close()
			if decodePollError != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("Spansh route calculation canceled or timed out: %w", ctx.Err())
				}
				continue
			}
			if poll.Error != "" {
				// Spansh newly queued jobs can transiently return "Job not found" (or 404)
				// during the first few seconds before Redis or worker distribution. Allow retries.
				if strings.Contains(strings.ToLower(poll.Error), "not found") && time.Since(startTime) < 8*time.Second {
					continue
				}
				return nil, fmt.Errorf("Spansh router: %s", poll.Error)
			}
			if poll.Status == "queued" {
				continue
			}
			if poll.Status == "ok" {
				return ReconcileJumps(poll.Result.Jumps, capacityUsed, depotTritium), nil
			}
		}
	}
}

// QueryCarrierRoute submits a Fleet Carrier route request to Spansh, polls until
// completion or context cancellation, and returns fuel-reconciled hops.
func QueryCarrierRoute(ctx context.Context, sourceSystem, destinationSystem string, capacityUsed int, depotTritium int) ([]RouteJump, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	jobID, submitError := submitRouteJob(ctx, httpClient, sourceSystem, destinationSystem, capacityUsed)
	if submitError != nil {
		return nil, submitError
	}
	return pollRouteJob(ctx, httpClient, jobID, capacityUsed, depotTritium)
}

