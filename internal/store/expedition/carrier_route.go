package expedition

import (
	"encoding/json"
	"fmt"

	"planisphere/internal/core/db"
	"planisphere/internal/store/settings"
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
	routeSettingValue, getError := settings.Get(queryer, activeCarrierRouteSettingKey(expeditionID))
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
		return fmt.Errorf("invalid expedition ID: %d", state.ExpeditionID)
	}
	routeJSON, marshalError := json.Marshal(state)
	if marshalError != nil {
		return fmt.Errorf("marshal active carrier route: %w", marshalError)
	}
	return settings.Set(queryer, activeCarrierRouteSettingKey(state.ExpeditionID), string(routeJSON))
}

// ClearActiveCarrierRoute deletes the active carrier route for an expedition.
func ClearActiveCarrierRoute(queryer db.Queryer, expeditionID int64) error {
	return settings.Delete(queryer, activeCarrierRouteSettingKey(expeditionID))
}
