package expedition

import (
	"database/sql"
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
	_, saveError := SaveActiveCarrierRouteIfChanged(queryer, state)
	return saveError
}

// ClearActiveCarrierRoute deletes the active carrier route for an expedition.
func ClearActiveCarrierRoute(queryer db.Queryer, expeditionID int64) error {
	return settings.Delete(queryer, activeCarrierRouteSettingKey(expeditionID))
}

// SaveActiveCarrierRouteIfChanged persists state only when its serialized JSON
// differs from the stored route setting, reporting whether a write occurred.
func SaveActiveCarrierRouteIfChanged(queryer db.Queryer, state ActiveCarrierRouteState) (bool, error) {
	if state.ExpeditionID <= 0 {
		return false, fmt.Errorf("invalid expedition ID: %d", state.ExpeditionID)
	}
	routeJSONBytes, marshalError := json.Marshal(state)
	if marshalError != nil {
		return false, fmt.Errorf("marshal active carrier route: %w", marshalError)
	}
	routeJSON := string(routeJSONBytes)
	settingKey := activeCarrierRouteSettingKey(state.ExpeditionID)
	existingJSON, getError := settings.Get(queryer, settingKey)
	if getError != nil {
		return false, fmt.Errorf("read active carrier route: %w", getError)
	}
	if existingJSON == routeJSON {
		return false, nil
	}
	if setError := settings.Set(queryer, settingKey, routeJSON); setError != nil {
		return false, setError
	}
	return true, nil
}

// ClearActiveCarrierRouteIfPresent deletes the active carrier route for
// expeditionID when present, reporting whether a route existed.
func ClearActiveCarrierRouteIfPresent(queryer db.Queryer, expeditionID int64) (bool, error) {
	existingJSON, getError := settings.Get(queryer, activeCarrierRouteSettingKey(expeditionID))
	if getError != nil {
		return false, fmt.Errorf("read active carrier route: %w", getError)
	}
	if existingJSON == "" {
		return false, nil
	}
	if clearError := ClearActiveCarrierRoute(queryer, expeditionID); clearError != nil {
		return false, clearError
	}
	return true, nil
}

// RecordCarrierJumpAndSaveRouteTx atomically records a carrier jump and persists
// the updated active carrier route state inside a single transaction.
func RecordCarrierJumpAndSaveRouteTx(dbConn *sql.DB, expeditionID int64, distanceLy float64, tritium int, state ActiveCarrierRouteState) error {
	if state.ExpeditionID == 0 {
		state.ExpeditionID = expeditionID
	}
	if state.ExpeditionID != expeditionID {
		return fmt.Errorf("carrier route expedition ID %d does not match %d", state.ExpeditionID, expeditionID)
	}
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if jumpError := RecordCarrierJump(transaction, expeditionID, distanceLy, tritium); jumpError != nil {
			return jumpError
		}
		return SaveActiveCarrierRoute(transaction, state)
	})
}

// CompleteCarrierRouteArrivalTx atomically records an optional final carrier jump,
// clears the active carrier route, and marks targetRowID arrived when positive.
func CompleteCarrierRouteArrivalTx(dbConn *sql.DB, expeditionID int64, targetRowID int64, distanceLy float64, tritium int) error {
	return db.WithTx(dbConn, func(transaction *sql.Tx) error {
		if distanceLy > 0 || tritium > 0 {
			if jumpError := RecordCarrierJump(transaction, expeditionID, distanceLy, tritium); jumpError != nil {
				return jumpError
			}
		}
		if clearError := ClearActiveCarrierRoute(transaction, expeditionID); clearError != nil {
			return clearError
		}
		if targetRowID > 0 {
			targetRow, getError := GetRow(transaction, targetRowID)
			if getError != nil {
				return getError
			}
			if targetRow.ExpeditionID != expeditionID {
				return fmt.Errorf("row %d does not belong to expedition %d", targetRowID, expeditionID)
			}
			arrived := true
			if _, updateError := UpdateRow(transaction, RowPatch{ID: targetRowID, FCArrived: &arrived}); updateError != nil {
				return updateError
			}
		}
		return nil
	})
}
