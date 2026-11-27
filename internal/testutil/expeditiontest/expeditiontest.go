// Package expeditiontest provides shared projection bundles and mode/profile lookup helpers for backend tests.
package expeditiontest

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/projections/stats"
	"planisphere/internal/replay"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

// DefaultModes holds the database IDs of the seeded flight modes.
type DefaultModes struct {
	Exploration int64
	Tourism     int64
	Travel      int64
	Fallback    int64
}

// NewProjections wires a fresh projection.Bus and expedition.Projections bundle against conn.
func NewProjections(conn *sql.DB) (*projection.Bus, *expedition.Projections) {
	bus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(conn, bus)
	salesLedger := realization.NewSalesLedger(conn, bus)
	return bus, &expedition.Projections{
		Arrivals:       projections.NewSystemArrivals(conn, bus),
		VisitCounts:    projections.NewSystemVisitCounts(conn, bus, recoveredJumps),
		ShipHistory:    projections.NewShipHistory(conn, bus),
		TimeRangeStats: stats.NewTimeRangeStats(conn, bus, salesLedger, recoveredJumps, expedition.LoadModeLookup),
		SalesLedger:    salesLedger,
		RecoveredJumps: recoveredJumps,
	}
}

// LookupDefaultModes resolves the seeded Exploration, Tourism, Travel, and default fallback mode IDs.
func LookupDefaultModes(testingContext *testing.T, conn *sql.DB) DefaultModes {
	testingContext.Helper()
	modeList, listError := modes.List(conn)
	if listError != nil {
		testingContext.Fatalf("modes.List: %v", listError)
	}
	resolvedModes := DefaultModes{
		Fallback: modes.DefaultFallbackModeID(conn),
	}
	for _, modeItem := range modeList {
		switch modeItem.Name {
		case "Exploration":
			resolvedModes.Exploration = modeItem.ID
		case "Tourism":
			resolvedModes.Tourism = modeItem.ID
		case "Travel":
			resolvedModes.Travel = modeItem.ID
		}
	}
	if resolvedModes.Exploration == 0 || resolvedModes.Tourism == 0 || resolvedModes.Travel == 0 || resolvedModes.Fallback == 0 {
		testingContext.Fatalf("missing default modes: %+v", resolvedModes)
	}
	return resolvedModes
}

// ProfileIDsByName returns a map from canonical commander display name to profile ID.
func ProfileIDsByName(testingContext *testing.T, conn *sql.DB) map[string]int64 {
	testingContext.Helper()
	commanders, listError := replay.ListCommanders(conn)
	if listError != nil {
		testingContext.Fatalf("replay.ListCommanders: %v", listError)
	}
	idsByName := make(map[string]int64, len(commanders))
	for _, commander := range commanders {
		idsByName[commander.Name] = commander.ID
	}
	return idsByName
}
