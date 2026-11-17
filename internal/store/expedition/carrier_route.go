package expedition

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"planisphere/internal/core/db"
	"planisphere/internal/sources/edsm"
	"planisphere/internal/sources/spansh"
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
	ExpeditionID        int64              `json:"expeditionId"`
	TargetRowID         int64              `json:"targetRowId"`
	Route               CarrierRouteResult `json:"route"`
	JumpIndex           int                `json:"jumpIdx"`
	ManualMode          bool               `json:"manualMode"`
	ManualDistanceInput string             `json:"manualDistInput"`
	ManualTritiumInput  string             `json:"manualTritInput"`
	ManualLegJumps      int                `json:"manualLegJumps"`
	ManualLegDistance   float64            `json:"manualLegDist"`
	ManualLegTritium    int                `json:"manualLegTrit"`
}

func activeCarrierRouteSettingKey(expeditionID int64) string {
	return fmt.Sprintf("active_carrier_route_%d", expeditionID)
}

// GetActiveCarrierRoute retrieves the persisted active carrier route for an expedition.
func GetActiveCarrierRoute(queryer db.Queryer, expeditionID int64) (*ActiveCarrierRouteState, error) {
	routeSettingValue, getError := db.GetSetting(queryer, activeCarrierRouteSettingKey(expeditionID))
	if getError != nil || routeSettingValue == "" {
		return nil, nil
	}
	var routeState ActiveCarrierRouteState
	if unmarshalError := json.Unmarshal([]byte(routeSettingValue), &routeState); unmarshalError != nil {
		return nil, nil
	}
	return &routeState, nil
}

// SaveActiveCarrierRoute persists the active carrier route state for an expedition.
func SaveActiveCarrierRoute(queryer db.Queryer, state ActiveCarrierRouteState) error {
	if state.ExpeditionID <= 0 {
		return fmt.Errorf("invalid expedition id: %d", state.ExpeditionID)
	}
	routeJSON, marshalError := json.Marshal(state)
	if marshalError != nil {
		return marshalError
	}
	return db.SetSetting(queryer, activeCarrierRouteSettingKey(state.ExpeditionID), string(routeJSON))
}

// ClearActiveCarrierRoute deletes the active carrier route for an expedition.
func ClearActiveCarrierRoute(queryer db.Queryer, expeditionID int64) error {
	return db.DeleteSetting(queryer, activeCarrierRouteSettingKey(expeditionID))
}

func normalizeCarrierPlotParameters(
	carrierType string,
	capacityUsed int,
	depotTritium int,
	defaultDepotTritium func() int,
) (string, int, int) {
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
	if depotTritium <= 0 && defaultDepotTritium != nil {
		depotTritium = defaultDepotTritium()
	}
	if depotTritium <= 0 || depotTritium > 1000 {
		depotTritium = 1000
	}
	return carrierType, capacityUsed, depotTritium
}

// PlotCarrierRoute queries the Spansh Fleet Carrier router for a path from source to destination,
// recalculating tritium burn with in-house mechanics and falling back cleanly to manual mode on failure.
func PlotCarrierRoute(
	ctx context.Context,
	queryer db.Queryer,
	source string,
	destination string,
	capacityUsed int,
	carrierType string,
	depotTritium int,
	defaultDepotTritium func() int,
) (CarrierRouteResult, error) {
	source = edsm.CanonicalSystemName(queryer, source)
	destination = edsm.CanonicalSystemName(queryer, destination)
	carrierType, capacityUsed, depotTritium = normalizeCarrierPlotParameters(carrierType, capacityUsed, depotTritium, defaultDepotTritium)

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

	parentCtx := ctx
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

