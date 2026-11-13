package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"planisphere/internal/db"
	"planisphere/internal/expedition"
	"planisphere/internal/journal"
	"planisphere/internal/spansh"
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

// latestCarrierStats returns the most recent parsed CarrierStats event and its
// timestamp from journal_events, caching the result until journal or commander
// state changes. Caller must hold app.dbMu.RLock.
func (app *App) latestCarrierStats() (journal.CarrierStatsEvent, string, bool) {
	app.carrierStatsMu.Lock()
	if app.carrierStatsValid {
		cachedEvent := app.carrierStatsEvent
		cachedTimestamp := app.carrierStatsTimestamp
		cachedFound := app.carrierStatsFound
		app.carrierStatsMu.Unlock()
		return cachedEvent, cachedTimestamp, cachedFound
	}
	carrierStatsGeneration := app.carrierStatsGen
	app.carrierStatsMu.Unlock()

	var parsedEvent journal.CarrierStatsEvent
	var statsTimestamp string
	var foundStats bool

	var rawCarrierStats string
	queryError := app.db.QueryRow(`SELECT raw_json, timestamp FROM journal_events
		WHERE event = 'CarrierStats' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawCarrierStats, &statsTimestamp)
	if queryError == nil && rawCarrierStats != "" {
		if json.Unmarshal([]byte(rawCarrierStats), &parsedEvent) == nil {
			foundStats = true
		}
	}
	if !foundStats {
		statsTimestamp = ""
	}

	app.carrierStatsMu.Lock()
	if !app.carrierStatsValid && app.carrierStatsGen == carrierStatsGeneration {
		app.carrierStatsEvent = parsedEvent
		app.carrierStatsTimestamp = statsTimestamp
		app.carrierStatsFound = foundStats
		app.carrierStatsValid = true
	} else if app.carrierStatsValid {
		parsedEvent = app.carrierStatsEvent
		statsTimestamp = app.carrierStatsTimestamp
		foundStats = app.carrierStatsFound
	}
	app.carrierStatsMu.Unlock()

	return parsedEvent, statsTimestamp, foundStats
}

func (app *App) latestDockedCarrierLocation(playerCarrierID int64, playerCallsign string) (string, string) {
	rows, queryError := app.db.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event IN ('Docked', 'Location') AND system_name != '' AND ` + journal.CmdrFilterSQL + `
		AND raw_json LIKE '%FleetCarrier%'
		ORDER BY timestamp DESC, id DESC LIMIT 20`)
	if queryError != nil {
		log.Printf("query docked carrier location: %v", queryError)
		return "", ""
	}
	defer rows.Close()

	for rows.Next() {
		var candidateSystem, candidateTimestamp, candidateRawJSON string
		if scanError := rows.Scan(&candidateSystem, &candidateTimestamp, &candidateRawJSON); scanError != nil {
			log.Printf("scan docked carrier location: %v", scanError)
			return "", ""
		}
		var dockRecord struct {
			StationType string `json:"StationType"`
			MarketID    int64  `json:"MarketID"`
			StationName string `json:"StationName"`
			Docked      *bool  `json:"Docked"`
		}
		if json.Unmarshal([]byte(candidateRawJSON), &dockRecord) != nil {
			continue
		}
		if !strings.EqualFold(dockRecord.StationType, "FleetCarrier") {
			continue
		}
		if dockRecord.Docked != nil && !*dockRecord.Docked {
			continue
		}
		matchesCarrier := true
		if playerCarrierID > 0 && dockRecord.MarketID > 0 {
			matchesCarrier = dockRecord.MarketID == playerCarrierID
		} else if playerCallsign != "" && dockRecord.StationName != "" {
			matchesCarrier = strings.Contains(strings.ToUpper(dockRecord.StationName), strings.ToUpper(playerCallsign))
		}
		if matchesCarrier {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := rows.Err(); iterationError != nil {
		log.Printf("iterate docked carrier location: %v", iterationError)
	}
	return "", ""
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
	if carrierStats, _, foundStats := app.latestCarrierStats(); foundStats {
		playerCarrierID = carrierStats.CarrierID
		playerCallsign = strings.TrimSpace(carrierStats.Callsign)
	}

	// Candidate from CarrierJump events
	var jumpSystem, jumpTimestamp string
	_ = app.db.QueryRow(`SELECT system_name, timestamp FROM journal_events
		WHERE event = 'CarrierJump' AND system_name != '' AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&jumpSystem, &jumpTimestamp)

	// Candidate from Docked or Location events aboard a fleet carrier
	dockedSystem, dockedTimestamp := app.latestDockedCarrierLocation(playerCarrierID, playerCallsign)

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
	depotInfo := CarrierDepotInfo{
		UsedCapacity: 5000,
		DepotTritium: 1000,
		Cargo:        0,
	}
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return depotInfo, readyError
	}

	carrierStats, statsTimestamp, foundStats := app.latestCarrierStats()
	if foundStats {
		if usedCapacity := carrierStats.UsedCapacity(); usedCapacity > 0 {
			depotInfo.UsedCapacity = usedCapacity
		}
		if carrierStats.FuelLevel > 0 {
			depotInfo.DepotTritium = carrierStats.FuelLevel
		}
		depotInfo.Cargo = carrierStats.SpaceUsage.Cargo
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
				depotInfo.DepotTritium = depositEvent.Total
			}
		}
	}

	if depotInfo.DepotTritium <= 0 || depotInfo.DepotTritium > 1000 {
		depotInfo.DepotTritium = 1000
	}
	return depotInfo, nil
}

// GetDefaultCarrierCapacity returns the most recently logged Fleet Carrier used capacity in tonnes,
// or 5000 if no CarrierStats event exists in the journal archive.
func (app *App) GetDefaultCarrierCapacity() int {
	depotInfo, getError := app.GetCarrierDepotInfo()
	if getError != nil || depotInfo.UsedCapacity <= 0 {
		return 5000
	}
	return depotInfo.UsedCapacity
}

// GetDefaultCarrierDepot returns the most recently logged Fleet Carrier depot tritium in tonnes,
// or 1000 (full tank) if no CarrierStats or CarrierDepositFuel event exists in the journal archive.
func (app *App) GetDefaultCarrierDepot() int {
	depotInfo, getError := app.GetCarrierDepotInfo()
	if getError != nil || depotInfo.DepotTritium <= 0 {
		return 1000
	}
	return depotInfo.DepotTritium
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
	app.changed("adjustments", expeditionID)
	return nil
}

func (app *App) canonicalSystemName(name string) string {
	name = strings.TrimSpace(name)
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	app.waitStartupLocked()
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

func normalizeCarrierPlotParameters(app *App, carrierType string, capacityUsed int, depotTritium int) (string, int, int) {
	if carrierType == "" {
		carrierType = "player"
	}
	maxCapacity := 25000
	if carrierType == "squadron" {
		maxCapacity = 60000
	}
	if capacityUsed < 0 {
		capacityUsed = 0
	}
	if capacityUsed > maxCapacity {
		capacityUsed = maxCapacity
	}
	if depotTritium <= 0 {
		depotInfo, _ := app.GetCarrierDepotInfo()
		depotTritium = depotInfo.DepotTritium
	}
	if depotTritium <= 0 || depotTritium > 1000 {
		depotTritium = 1000
	}
	return carrierType, capacityUsed, depotTritium
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
	carrierType, capacityUsed, depotTritium = normalizeCarrierPlotParameters(app, carrierType, capacityUsed, depotTritium)

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

	parentCtx := app.ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(parentCtx, 60*time.Second)
	defer cancel()

	spanshJumps, queryError := spansh.QueryCarrierRoute(requestCtx, source, destination, capacityUsed, depotTritium)
	if queryError != nil || len(spanshJumps) == 0 {
		routeResult.ManualMode = true
		if queryError != nil {
			routeResult.Error = queryError.Error()
		} else {
			routeResult.Error = "Spansh returned no jumps for this route — switched to Manual Mode."
		}
		return routeResult, nil
	}

	mappedJumps := make([]CarrierRouteJump, 0, len(spanshJumps))
	for _, jump := range spanshJumps {
		mappedJumps = append(mappedJumps, CarrierRouteJump{
			SystemName:       jump.SystemName,
			DistanceLy:       jump.DistanceLy,
			DistanceToDestLy: jump.DistanceToDestLy,
			TritiumUsed:      jump.TritiumUsed,
			FuelInTank:       jump.FuelInTank,
			HasIcyRing:       jump.HasIcyRing,
		})
		routeResult.TotalDistLy += jump.DistanceLy
		routeResult.TotalTritium += jump.TritiumUsed
	}
	routeResult.Jumps = mappedJumps
	routeResult.TotalJumps = len(mappedJumps)
	return routeResult, nil
}

