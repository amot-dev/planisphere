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
	"planisphere/internal/testutil/journaltest"
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

// SeedArchivedPreWipeExpedition ingests a two-era ClearSavedGame journal archive for CMDR Pioneer and returns a completed expedition scoped to the disabled pre-wipe career profile.
func SeedArchivedPreWipeExpedition(testingContext *testing.T, conn *sql.DB) (expedition.Expedition, replay.Commander) {
	testingContext.Helper()
	journaltest.IngestLines(testingContext, conn,
		`{"timestamp":"2024-05-01T10:00:00Z","event":"LoadGame","Commander":"CMDR Pioneer","FID":"F777","Ship":"anaconda","Ship_Localised":"Anaconda","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2024-05-01T10:05:00Z","event":"FSDJump","StarSystem":"Achenar","SystemAddress":501,"StarPos":[67,-119,24],"JumpDist":20.0}`,
		`{"timestamp":"2024-05-01T10:15:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2024-05-01T11:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":6006,"StarPos":[-9530,-910,19808],"JumpDist":22000.0}`,
		`{"timestamp":"2024-05-01T11:10:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Colonia 1","BodyID":1,"StarSystem":"Colonia","SystemAddress":6006,"PlanetClass":"Earthlike body","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2025-01-01T00:00:00Z","event":"ClearSavedGame","Name":"CMDR Pioneer","FID":"F777"}`,
		`{"timestamp":"2025-01-01T00:00:05Z","event":"NewCommander","Name":"CMDR Pioneer","FID":"F777","Package":"Sidewinder"}`,
		`{"timestamp":"2025-01-01T00:05:00Z","event":"LoadGame","Commander":"CMDR Pioneer","FID":"F777","Ship":"SideWinder","Ship_Localised":"Sidewinder","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2025-01-01T00:10:00Z","event":"Location","StarSystem":"Matet","SystemAddress":9001,"StarPos":[10,20,30]}`,
	)

	commanders, listError := replay.ListCommanders(conn)
	if listError != nil || len(commanders) != 2 || !commanders[0].IsArchivedCareer || commanders[0].Enabled {
		testingContext.Fatalf("expected era:0 archived disabled and era:1 active enabled, got %+v err=%v", commanders, listError)
	}
	archivedProfile := commanders[0]

	historicalExpedition, createError := expedition.CreateWithWaypoints(conn, "2024 Pre-Wipe Colonia Run", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, X: 0, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "Colonia", SystemAddress: 6006, HasCoords: true, X: -9530, Y: -910, Z: 19808, CoordsSource: "journal"},
	})
	if createError != nil {
		testingContext.Fatalf("create historical expedition: %v", createError)
	}
	if startError := expedition.StartScopedTx(conn, historicalExpedition.ID, "2024-05-01T10:15:00Z", 0, archivedProfile.ID); startError != nil {
		testingContext.Fatalf("StartScopedTx archived profile: %v", startError)
	}
	if completeError := expedition.CompleteTx(conn, historicalExpedition.ID); completeError != nil {
		testingContext.Fatalf("CompleteTx archived expedition: %v", completeError)
	}
	return historicalExpedition, archivedProfile
}
