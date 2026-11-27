package expedition_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestInterleavedTwoAccountConcurrentSelectedExpeditions(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	_, projectionsBundle := expeditiontest.NewProjections(dbConn)

	// Monday (CMDR Main, F101, Mandalay), Tuesday (CMDR Alt, F202, Cobra Mk III), Wednesday (CMDR Main swaps to Caspian Explorer and reaches MainWP3).
	// Both accounts start at Sol and pass through SharedHub at different times with different scans and sales.
	journaltest.IngestLines(testingContext, dbConn,
		`{"timestamp":"2026-03-02T09:00:00Z","event":"LoadGame","Commander":"CMDR Main","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-03-02T09:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-03-02T10:00:00Z","event":"FSDJump","StarSystem":"SharedHub","SystemAddress":2002,"StarPos":[100,0,0],"JumpDist":100.0}`,
		`{"timestamp":"2026-03-02T10:15:00Z","event":"Scan","ScanType":"Detailed","BodyName":"SharedHub A 1","BodyID":11,"StarSystem":"SharedHub","SystemAddress":2002,"PlanetClass":"Water world","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,

		// Tuesday: CMDR Alt logs in at Sol, jumps to SharedHub and AltDest in Cobra Mk III, scans an Earthlike body, and sells data.
		`{"timestamp":"2026-03-03T09:00:00Z","event":"LoadGame","Commander":"CMDR Alt","FID":"F202","Ship":"cobramkiii","Ship_Localised":"Cobra Mk III","ShipID":2,"Odyssey":true}`,
		`{"timestamp":"2026-03-03T09:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-03-03T10:00:00Z","event":"FSDJump","StarSystem":"SharedHub","SystemAddress":2002,"StarPos":[100,0,0],"JumpDist":100.0}`,
		`{"timestamp":"2026-03-03T11:00:00Z","event":"FSDJump","StarSystem":"AltDest","SystemAddress":3003,"StarPos":[100,80,0],"JumpDist":80.0}`,
		`{"timestamp":"2026-03-03T11:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"AltDest 1","BodyID":21,"StarSystem":"AltDest","SystemAddress":3003,"PlanetClass":"Earthlike body","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-03T11:40:00Z","event":"MultiSellExplorationData","Discovered":[{"SystemName":"AltDest","NumBodies":1}],"BaseValue":600000,"Bonus":200000,"TotalEarnings":800000}`,

		// Wednesday: CMDR Main swaps to Caspian Explorer at SharedHub and jumps to MainDest, then scans an Ammonia world.
		`{"timestamp":"2026-03-04T09:00:00Z","event":"LoadGame","Commander":"CMDR Main","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-03-04T09:10:00Z","event":"ShipyardSwap","ShipType":"explorer_nx","ShipType_Localised":"Caspian Explorer","ShipID":3,"StoreOldShip":"mandalay","StoreShipID":1}`,
		`{"timestamp":"2026-03-04T09:30:00Z","event":"FSDJump","StarSystem":"MainDest","SystemAddress":4004,"StarPos":[250,0,0],"JumpDist":150.0}`,
		`{"timestamp":"2026-03-04T09:45:00Z","event":"Scan","ScanType":"Detailed","BodyName":"MainDest 1","BodyID":31,"StarSystem":"MainDest","SystemAddress":4004,"PlanetClass":"Ammonia world","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
		// Later on Wednesday, CMDR Alt also jumps into MainDest and scans another body; ExpMain CompleteTx must ignore CMDR Alt's scan.
		`{"timestamp":"2026-03-04T12:00:00Z","event":"LoadGame","Commander":"CMDR Alt","FID":"F202","Ship":"cobramkiii","Ship_Localised":"Cobra Mk III","ShipID":2,"Odyssey":true}`,
		`{"timestamp":"2026-03-04T12:15:00Z","event":"FSDJump","StarSystem":"MainDest","SystemAddress":4004,"StarPos":[250,0,0],"JumpDist":170.0}`,
		`{"timestamp":"2026-03-04T12:30:00Z","event":"Scan","ScanType":"Detailed","BodyName":"MainDest 2","BodyID":32,"StarSystem":"MainDest","SystemAddress":4004,"PlanetClass":"Water world","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
	)

	idsByName := expeditiontest.ProfileIDsByName(testingContext, dbConn)
	if len(idsByName) != 2 {
		testingContext.Fatalf("expected 2 resolved commanders, got %+v", idsByName)
	}
	mainID, altID := idsByName["CMDR Main"], idsByName["CMDR Alt"]

	expMain, err := expedition.CreateWithWaypoints(dbConn, "Main Survey", "Main account", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, X: 0, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "SharedHub", SystemAddress: 2002, HasCoords: true, X: 100, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "AltDest", SystemAddress: 3003, HasCoords: true, X: 100, Y: 80, Z: 0, CoordsSource: "journal"},
		{SystemName: "MainDest", SystemAddress: 4004, HasCoords: true, X: 250, Y: 0, Z: 0, CoordsSource: "journal"},
	})
	if err != nil {
		testingContext.Fatalf("create ExpMain: %v", err)
	}
	expAlt, err := expedition.CreateWithWaypoints(dbConn, "Alt Colonia Hop", "Alt account", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, X: 0, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "SharedHub", SystemAddress: 2002, HasCoords: true, X: 100, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "AltDest", SystemAddress: 3003, HasCoords: true, X: 100, Y: 80, Z: 0, CoordsSource: "journal"},
	})
	if err != nil {
		testingContext.Fatalf("create ExpAlt: %v", err)
	}
	// CreateWithWaypoints auto-selects the most recently active commander (CMDR Alt, last_seen 12:30).
	if expMain.CommanderProfileID != altID || expAlt.CommanderProfileID != altID {
		testingContext.Fatalf("expected newly created expeditions to default CommanderProfileID to latest commander %d, got main=%d alt=%d",
			altID, expMain.CommanderProfileID, expAlt.CommanderProfileID)
	}

	// StartCandidates exposes both profileId and commander in JSON across all profiles while planning, and scopes once started.
	_, allSolCandidates, err := expedition.StartCandidates(dbConn, expMain.ID)
	if err != nil || len(allSolCandidates) != 2 {
		testingContext.Fatalf("expected 2 Sol candidates while planning, got %+v err=%v", allSolCandidates, err)
	}
	encodedCandidate, marshalErr := json.Marshal(allSolCandidates[0])
	if marshalErr != nil {
		testingContext.Fatalf("marshal StartCandidate: %v", marshalErr)
	}
	var candidatePayload map[string]any
	if err := json.Unmarshal(encodedCandidate, &candidatePayload); err != nil || candidatePayload["commander"] == nil || candidatePayload["profileId"] == nil {
		testingContext.Fatalf("expected StartCandidates JSON to include profileId and commander, got %s", string(encodedCandidate))
	}

	if err := expedition.StartScopedTx(dbConn, expMain.ID, "2026-03-02T09:05:00Z", 0, mainID); err != nil {
		testingContext.Fatalf("StartScopedTx ExpMain: %v", err)
	}
	if err := expedition.StartScopedTx(dbConn, expAlt.ID, "2026-03-03T09:05:00Z", 0, altID); err != nil {
		testingContext.Fatalf("StartScopedTx ExpAlt: %v", err)
	}

	_, scopedMainCandidates, err := expedition.StartCandidates(dbConn, expMain.ID)
	if err != nil || len(scopedMainCandidates) != 1 || scopedMainCandidates[0].ProfileID != mainID {
		testingContext.Fatalf("expected 1 scoped Sol candidate for CMDR Main, got %+v err=%v", scopedMainCandidates, err)
	}

	// ExpMain must NOT mark AltDest visited (CMDR Alt visited AltDest, not CMDR Main), and SharedHub DepartedAt on ExpMain must be Wednesday 09:30 (not Tuesday 11:00!).
	mainPlanBeforeMove, err := expedition.BuildPlan(dbConn, expMain.ID, nil, projectionsBundle)
	if err != nil {
		testingContext.Fatalf("BuildPlan ExpMain: %v", err)
	}
	if mainPlanBeforeMove.PlayerSystem != "MainDest" {
		testingContext.Fatalf("expected ExpMain PlayerSystem=MainDest, got %q", mainPlanBeforeMove.PlayerSystem)
	}
	if mainPlanBeforeMove.Rows[1].Row.DepartedAt != "2026-03-04T09:30:00Z" {
		testingContext.Fatalf("expected SharedHub DepartedAt=2026-03-04T09:30:00Z on ExpMain, got %q", mainPlanBeforeMove.Rows[1].Row.DepartedAt)
	}
	if mainPlanBeforeMove.Rows[2].Row.VisitedAt != "" || mainPlanBeforeMove.Rows[2].VisitCount != 0 {
		testingContext.Fatalf("expected AltDest unvisited with VisitCount=0 on ExpMain, got visitedAt=%q visitCount=%d",
			mainPlanBeforeMove.Rows[2].Row.VisitedAt, mainPlanBeforeMove.Rows[2].VisitCount)
	}

	// Scoped DropSlotPreview allows dragging MainDest above unvisited AltDest so MainDest becomes visited on Wednesday, while AltDest stays unvisited.
	previews, err := expedition.ComputeRowDropPreviews(dbConn, mainPlanBeforeMove.Rows[3].Row.ID, projectionsBundle)
	if err != nil || previews[2].Status != expedition.DropSlotValidVisited {
		testingContext.Fatalf("expected slot 2 to be valid_visited for MainDest on ExpMain, got %+v err=%v", previews, err)
	}
	if _, err := expedition.MoveRowTx(dbConn, mainPlanBeforeMove.Rows[3].Row.ID, 2); err != nil {
		testingContext.Fatalf("MoveRowTx MainDest: %v", err)
	}
	if _, err := expedition.ReevaluateTx(dbConn, expMain.ID); err != nil {
		testingContext.Fatalf("ReevaluateTx ExpMain: %v", err)
	}

	mainPlan, err := expedition.BuildPlan(dbConn, expMain.ID, nil, projectionsBundle)
	if err != nil {
		testingContext.Fatalf("BuildPlan ExpMain after move: %v", err)
	}
	altPlan, err := expedition.BuildPlan(dbConn, expAlt.ID, nil, projectionsBundle)
	if err != nil {
		testingContext.Fatalf("BuildPlan ExpAlt: %v", err)
	}

	_, mainLegs := assertBatchMatchesIndividualDetails(testingContext, dbConn, mainPlan, projectionsBundle)
	_, altLegs := assertBatchMatchesIndividualDetails(testingContext, dbConn, altPlan, projectionsBundle)
	if len(mainLegs) != 2 || !reflect.DeepEqual(mainLegs[0].ShipsUsed, []string{"Mandalay"}) || !reflect.DeepEqual(mainLegs[1].ShipsUsed, []string{"Caspian Explorer"}) {
		testingContext.Fatalf("unexpected ExpMain leg ShipsUsed: %+v", mainLegs)
	}
	if math.Abs(mainLegs[0].Metrics.DistanceLy-100.0) > 0.01 || math.Abs(mainLegs[1].Metrics.DistanceLy-150.0) > 0.01 || mainLegs[1].Realization.TotalRealized != 0 {
		testingContext.Fatalf("unexpected ExpMain leg metrics or realization contamination: %+v", mainLegs)
	}
	if len(altLegs) != 2 || !reflect.DeepEqual(altLegs[0].ShipsUsed, []string{"Cobra Mk III"}) || !reflect.DeepEqual(altLegs[1].ShipsUsed, []string{"Cobra Mk III"}) {
		testingContext.Fatalf("unexpected ExpAlt leg ShipsUsed: %+v", altLegs)
	}
	if altLegs[1].Realization.TotalRealized == 0 {
		testingContext.Fatalf("expected ExpAlt leg 2 to realize CMDR Alt's AltDest cartography sale, got %+v", altLegs[1].Realization)
	}

	// Delete unvisited AltDest from ExpMain and CompleteTx: EndTimestamp must be CMDR Main's 09:45 scan, NOT CMDR Alt's 12:30 scan in MainDest!
	if _, err := expedition.DeleteRowTx(dbConn, mainPlan.Rows[3].Row.ID); err != nil {
		testingContext.Fatalf("DeleteRowTx AltDest from ExpMain: %v", err)
	}
	if err := expedition.CompleteTx(dbConn, expMain.ID); err != nil {
		testingContext.Fatalf("CompleteTx ExpMain: %v", err)
	}
	completedMain, err := expedition.Get(dbConn, expMain.ID)
	if err != nil || completedMain.EndTimestamp != "2026-03-04T09:45:00Z" {
		testingContext.Fatalf("expected ExpMain EndTimestamp=2026-03-04T09:45:00Z, got %q err=%v", completedMain.EndTimestamp, err)
	}

	// Clone preserves CommanderProfileID, and List returns each expedition's CommanderProfileID.
	clonedMain, err := expedition.Clone(dbConn, expMain.ID)
	if err != nil || clonedMain.CommanderProfileID != mainID {
		testingContext.Fatalf("expected Clone to preserve CommanderProfileID=%d, got %+v err=%v", mainID, clonedMain, err)
	}
	allListed, err := expedition.List(dbConn)
	if err != nil || len(allListed) < 3 {
		testingContext.Fatalf("List expeditions: %+v err=%v", allListed, err)
	}
	for _, item := range allListed {
		if item.CommanderProfileID <= 0 {
			testingContext.Fatalf("expected positive CommanderProfileID on expedition %d", item.ID)
		}
	}
}

func TestArchivedPreWipeCareerSelectedExpeditionComputesPlanAndDetails(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)
	_, projectionsBundle := expeditiontest.NewProjections(dbConn)

	// Era 0 (pre-wipe archived career, enabled=0 by default) and Era 1 (post-wipe active career, enabled=1).
	journaltest.IngestLines(testingContext, dbConn,
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

	commanders, err := replay.ListCommanders(dbConn)
	if err != nil || len(commanders) != 2 || !commanders[0].IsArchivedCareer || commanders[0].Enabled {
		testingContext.Fatalf("expected era:0 archived disabled and era:1 active enabled, got %+v err=%v", commanders, err)
	}
	archivedProfile := commanders[0]

	histExp, err := expedition.CreateWithWaypoints(dbConn, "2024 Pre-Wipe Colonia Run", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", SystemAddress: 1001, HasCoords: true, X: 0, Y: 0, Z: 0, CoordsSource: "journal"},
		{SystemName: "Colonia", SystemAddress: 6006, HasCoords: true, X: -9530, Y: -910, Z: 19808, CoordsSource: "journal"},
	})
	if err != nil {
		testingContext.Fatalf("create historical expedition: %v", err)
	}
	if err := expedition.StartScopedTx(dbConn, histExp.ID, "2024-05-01T10:15:00Z", 0, archivedProfile.ID); err != nil {
		testingContext.Fatalf("StartScopedTx archived profile: %v", err)
	}
	if err := expedition.CompleteTx(dbConn, histExp.ID); err != nil {
		testingContext.Fatalf("CompleteTx archived expedition: %v", err)
	}

	planView, err := expedition.BuildPlan(dbConn, histExp.ID, nil, projectionsBundle)
	if err != nil || planView.VisitedWaypointCount != 2 {
		testingContext.Fatalf("expected 2 visited waypoints for disabled archived profile in selected expedition, got %+v err=%v", planView, err)
	}

	originBaseline, legDetails := assertBatchMatchesIndividualDetails(testingContext, dbConn, planView, projectionsBundle)
	if originBaseline == nil || math.Abs(originBaseline.Metrics.DistanceLy-20.0) > 0.01 {
		testingContext.Fatalf("expected pre-wipe OriginBaseline 20.0 Ly, got %+v", originBaseline)
	}
	if len(legDetails) != 1 || math.Abs(legDetails[0].Metrics.DistanceLy-22000.0) > 0.01 || !reflect.DeepEqual(legDetails[0].ShipsUsed, []string{"Anaconda"}) {
		testingContext.Fatalf("expected 22000 Ly Anaconda leg on archived expedition details, got %+v", legDetails)
	}
}
