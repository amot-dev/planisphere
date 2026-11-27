package expedition_test

import (
	"fmt"
	"testing"
	"time"

	"planisphere/internal/reference"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestSearchWaypointsTiersRankingAndExclusions(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	emptyResponse, emptyError := expedition.SearchWaypoints(databaseConnection, "   ", nil)
	if emptyError != nil {
		testingContext.Fatalf("SearchWaypoints whitespace: %v", emptyError)
	}
	if emptyResponse.Results == nil || len(emptyResponse.Results) != 0 || emptyResponse.TotalMatches != 0 || emptyResponse.NonWaypointVisits != nil {
		testingContext.Fatalf("expected empty non-nil response for blank query, got %+v", emptyResponse)
	}

	// Expedition name contains "Vela" to verify expedition names alone do not match.
	createdExpedition, createError := expedition.CreateWithWaypoints(databaseConnection, "Vela Grand Tour", "Desc", []expedition.SeedWaypoint{
		{SystemName: "Unrelated Sys", POIName: "Gateway to Vela"}, // Tier 5: substring POI
		{SystemName: "Deep Vela Rim", POIName: "Outer Rim"},       // Tier 4: substring system
		{SystemName: "Far Vela Reach", POIName: "Vela Station"},   // Tier 3: prefix POI beats Tier 4 substring system
		{SystemName: "Vela Pulsar", POIName: "Neutron"},           // Tier 2: prefix system
		{SystemName: "Vela", POIName: "Origin"},                   // Tier 1: exact system
		{SystemName: "Quiet Star", POIName: "Beacon"},             // Will hold "Vela" only in notes
	})
	if createError != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", createError)
	}
	rows, listError := expedition.ListRows(databaseConnection, createdExpedition.ID)
	if listError != nil || len(rows) != 6 {
		testingContext.Fatalf("ListRows: len=%d err=%v", len(rows), listError)
	}

	notesWithQuery := "Vela mentioned only inside waypoint notes"
	if _, updateError := expedition.UpdateRow(databaseConnection, expedition.RowPatch{ID: rows[5].ID, Notes: &notesWithQuery}); updateError != nil {
		testingContext.Fatalf("UpdateRow notes: %v", updateError)
	}
	// Insert a region header row whose unused system_name/poi_name columns must never match.
	regionRow := mustInsert(testingContext, databaseConnection, createdExpedition.ID, expedition.InsertAtTop, expedition.RowRegion, 1)
	if _, execError := databaseConnection.Exec(`UPDATE expedition_rows SET system_name = 'Vela', poi_name = 'Vela Region' WHERE id = ?`, regionRow.ID); execError != nil {
		testingContext.Fatalf("seed region columns: %v", execError)
	}

	searchResponse, searchError := expedition.SearchWaypoints(databaseConnection, "vela", nil)
	if searchError != nil {
		testingContext.Fatalf("SearchWaypoints(vela): %v", searchError)
	}
	if searchResponse.TotalMatches != 5 || len(searchResponse.Results) != 5 {
		testingContext.Fatalf("expected 5 matching waypoints (excluding notes and region row), got total=%d results=%+v",
			searchResponse.TotalMatches, searchResponse.Results)
	}

	expectedOrder := []struct {
		systemName string
		tier       int
	}{
		{systemName: "Vela", tier: 1},
		{systemName: "Vela Pulsar", tier: 2},
		{systemName: "Far Vela Reach", tier: 3},
		{systemName: "Deep Vela Rim", tier: 4},
		{systemName: "Unrelated Sys", tier: 5},
	}
	for index, expected := range expectedOrder {
		actual := searchResponse.Results[index]
		if actual.SystemName != expected.systemName || actual.RelevanceTier != expected.tier {
			testingContext.Fatalf("result %d = (%q, tier %d), want (%q, tier %d)",
				index, actual.SystemName, actual.RelevanceTier, expected.systemName, expected.tier)
		}
	}
}

func TestSearchWaypointsChronologicalTieBreakingAndStatusCoercion(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	seedSingleWaypoint := func(name string) expedition.Expedition {
		testingContext.Helper()
		record, err := expedition.CreateWithWaypoints(databaseConnection, name, "", []expedition.SeedWaypoint{
			{SystemName: "Colonia"},
		})
		if err != nil {
			testingContext.Fatalf("create %s: %v", name, err)
		}
		return record
	}

	retroHistorical := seedSingleWaypoint("Retroactive Historical")
	activeRecent := seedSingleWaypoint("Active Recent")
	planningUnstarted := seedSingleWaypoint("Planning Unstarted")
	coercedPlanning := seedSingleWaypoint("Coerced Active Without Start")

	// Retroactive historical expedition has newest created_at (2026-04-01) but oldest start_timestamp (2024-06-01).
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET status = 'completed', start_timestamp = '2024-06-01T00:00:00Z', end_timestamp = '2024-07-01T00:00:00Z', created_at = '2026-04-01T00:00:00Z' WHERE id = ?`, retroHistorical.ID); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '2026-03-10T00:00:00Z', created_at = '2026-01-01T00:00:00Z' WHERE id = ?`, activeRecent.ID); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET status = 'planning', start_timestamp = NULL, created_at = '2026-03-05T00:00:00Z' WHERE id = ?`, planningUnstarted.ID); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '', created_at = '2026-03-08T00:00:00Z' WHERE id = ?`, coercedPlanning.ID); err != nil {
		testingContext.Fatal(err)
	}

	searchResponse, searchError := expedition.SearchWaypoints(databaseConnection, "Colonia", nil)
	if searchError != nil {
		testingContext.Fatalf("SearchWaypoints(Colonia): %v", searchError)
	}
	if len(searchResponse.Results) != 4 {
		testingContext.Fatalf("expected 4 results, got %+v", searchResponse.Results)
	}

	expectedIDs := []int64{activeRecent.ID, coercedPlanning.ID, planningUnstarted.ID, retroHistorical.ID}
	expectedStatuses := []string{expedition.StatusActive, expedition.StatusPlanning, expedition.StatusPlanning, expedition.StatusCompleted}
	for index, expectedID := range expectedIDs {
		actual := searchResponse.Results[index]
		if actual.ExpeditionID != expectedID || actual.ExpeditionStatus != expectedStatuses[index] {
			testingContext.Fatalf("rank %d = (exp %d, status %q), want (exp %d, status %q)",
				index, actual.ExpeditionID, actual.ExpeditionStatus, expectedID, expectedStatuses[index])
		}
	}
}

func TestSearchWaypointsCondensingRegionNumberingAndEmptySystemPOIs(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	firstExpedition, createFirstError := expedition.CreateWithWaypoints(databaseConnection, "Round Trip", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", POIName: "Home Base"},
		{SystemName: "Alpha Centauri", POIName: "Midpoint"},
		{SystemName: "sol", POIName: "Return Stop"},
		{SystemName: "Sol", POIName: "home base"},
	})
	if createFirstError != nil {
		testingContext.Fatalf("create first expedition: %v", createFirstError)
	}
	firstRows, listError := expedition.ListRows(databaseConnection, firstExpedition.ID)
	if listError != nil || len(firstRows) != 4 {
		testingContext.Fatalf("list first rows: len=%d err=%v", len(firstRows), listError)
	}

	// Insert region headers before WP #1 and between WP #2 and WP #3 so row positions diverge from WP numbers.
	mustInsert(testingContext, databaseConnection, firstExpedition.ID, expedition.InsertAtTop, expedition.RowRegion, 1)
	mustInsert(testingContext, databaseConnection, firstExpedition.ID, firstRows[1].ID, expedition.RowRegion, 2)

	// Mark WP #3 visited so VisitedAt and VisitedAtGalactic are verified on stops.
	visitTimestamp := "2026-04-12T15:30:00Z"
	if _, err := databaseConnection.Exec(`UPDATE expedition_rows SET visited_at = ? WHERE id = ?`, visitTimestamp, firstRows[2].ID); err != nil {
		testingContext.Fatal(err)
	}

	// Add two empty-system POI-only waypoints in the same expedition; they must stay ungrouped.
	emptyPOIStopOne := mustInsert(testingContext, databaseConnection, firstExpedition.ID, 0, expedition.RowWaypoint, 0)
	emptyPOIStopTwo := mustInsert(testingContext, databaseConnection, firstExpedition.ID, 0, expedition.RowWaypoint, 0)
	poiLabelOne := "Nebula Camp Alpha"
	poiLabelTwo := "Nebula Camp Beta"
	if _, err := expedition.UpdateRow(databaseConnection, expedition.RowPatch{ID: emptyPOIStopOne.ID, POIName: &poiLabelOne}); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := expedition.UpdateRow(databaseConnection, expedition.RowPatch{ID: emptyPOIStopTwo.ID, POIName: &poiLabelTwo}); err != nil {
		testingContext.Fatal(err)
	}

	secondExpedition, createSecondError := expedition.CreateWithWaypoints(databaseConnection, "Second Trip", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", POIName: "Alt Departure"},
	})
	if createSecondError != nil {
		testingContext.Fatalf("create second expedition: %v", createSecondError)
	}

	// Ensure deterministic timestamp ordering so secondExpedition precedes firstExpedition.
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET created_at = '2026-04-01T00:00:00Z' WHERE id = ?`, firstExpedition.ID); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET created_at = '2026-04-02T00:00:00Z' WHERE id = ?`, secondExpedition.ID); err != nil {
		testingContext.Fatal(err)
	}

	solResponse, solError := expedition.SearchWaypoints(databaseConnection, "Sol", nil)
	if solError != nil {
		testingContext.Fatalf("SearchWaypoints(Sol): %v", solError)
	}
	if solResponse.TotalMatches != 2 || len(solResponse.Results) != 2 {
		testingContext.Fatalf("expected 2 cards (1 per expedition), got total=%d results=%+v", solResponse.TotalMatches, solResponse.Results)
	}
	if solResponse.Results[0].ExpeditionID != secondExpedition.ID || len(solResponse.Results[0].Stops) != 1 {
		testingContext.Fatalf("expected secondExpedition first with 1 stop, got %+v", solResponse.Results[0])
	}

	roundTripCard := solResponse.Results[1]
	if roundTripCard.ExpeditionID != firstExpedition.ID || roundTripCard.POIName != "Home Base / Return Stop" {
		testingContext.Fatalf("unexpected condensed Round Trip card: %+v", roundTripCard)
	}
	if len(roundTripCard.Stops) != 3 ||
		roundTripCard.Stops[0].Number != 1 ||
		roundTripCard.Stops[1].Number != 3 ||
		roundTripCard.Stops[2].Number != 4 {
		testingContext.Fatalf("expected true WP numbers [1, 3, 4] excluding region headers, got %+v", roundTripCard.Stops)
	}
	if roundTripCard.Stops[1].VisitedAt != visitTimestamp || roundTripCard.Stops[1].VisitedAtGalactic != reference.ToGalactic(visitTimestamp) {
		testingContext.Fatalf("unexpected visited stop timestamps: %+v", roundTripCard.Stops[1])
	}

	emptySystemResponse, emptySystemError := expedition.SearchWaypoints(databaseConnection, "Nebula Camp", nil)
	if emptySystemError != nil {
		testingContext.Fatalf("SearchWaypoints(Nebula Camp): %v", emptySystemError)
	}
	if emptySystemResponse.TotalMatches != 2 || len(emptySystemResponse.Results) != 2 {
		testingContext.Fatalf("expected 2 ungrouped cards for empty-system POI rows, got %+v", emptySystemResponse.Results)
	}
	if emptySystemResponse.Results[0].Stops[0].Number != 5 || emptySystemResponse.Results[1].Stops[0].Number != 6 {
		testingContext.Fatalf("expected ungrouped empty-system stops at WP #5 and #6, got %+v", emptySystemResponse.Results)
	}
}

func TestSearchWaypointsLikeWildcardEscaping(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	if _, err := expedition.CreateWithWaypoints(databaseConnection, "Wildcard Test", "", []expedition.SeedWaypoint{
		{SystemName: "Alpha_1"},
		{SystemName: "AlphaX1"},
		{SystemName: "Core 100%"},
		{SystemName: "Core 10099"},
		{SystemName: `Sector\North`},
		{SystemName: "SectorNorth"},
	}); err != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", err)
	}

	testCases := []struct {
		query          string
		expectedSystem string
	}{
		{query: "Alpha_", expectedSystem: "Alpha_1"},
		{query: "100%", expectedSystem: "Core 100%"},
		{query: `Sector\`, expectedSystem: `Sector\North`},
	}
	for _, testCase := range testCases {
		response, err := expedition.SearchWaypoints(databaseConnection, testCase.query, nil)
		if err != nil {
			testingContext.Fatalf("SearchWaypoints(%q): %v", testCase.query, err)
		}
		if len(response.Results) != 1 || response.Results[0].SystemName != testCase.expectedSystem {
			testingContext.Fatalf("SearchWaypoints(%q) = %+v, want single literal match %q",
				testCase.query, response.Results, testCase.expectedSystem)
		}
	}
}

func TestSearchWaypointsNonWaypointVisitsAttributionAndEdgeCases(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, projectionsBundle := expeditiontest.NewProjections(databaseConnection)

	// Ingest journal events covering:
	// - Starter Port: initial Location-only system (multiple Locations, zero jumps).
	// - Hub Prime: waypoint-linked FSDJump, legacy timestamp-linked FSDJump, Multicrew jump,
	//   crash-recovered jump, CMDR Main scoped expedition visit, CMDR Alt visit during CMDR Main's
	//   scoped expedition, and pre-expedition unattributed visit.
	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-03-01T08:00:00Z","event":"LoadGame","Commander":"CMDR Main","FID":"F101","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-03-01T08:05:00Z","event":"Location","StarSystem":"Starter Port","SystemAddress":1000,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-03-01T08:15:00Z","event":"Location","StarSystem":"Starter Port","SystemAddress":1000,"StarPos":[0,0,0]}`,
		// Pre-expedition unattributed visit to Hub Prime.
		`{"timestamp":"2026-03-01T09:00:00Z","event":"FSDJump","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0],"JumpDist":50.0}`,
		// Origin arrival for Global Expedition (all_enabled, 2026-03-02 -> 2026-03-10).
		`{"timestamp":"2026-03-02T09:00:00Z","event":"FSDJump","StarSystem":"Origin One","SystemAddress":2001,"StarPos":[80,0,0],"JumpDist":30.0}`,
		// Waypoint-linked arrival at Hub Prime (will be marked visited by evaluator with visit_event_uid).
		`{"timestamp":"2026-03-02T10:00:00Z","event":"FSDJump","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0],"JumpDist":30.0}`,
		// Legacy waypoint-linked arrival at Hub Prime (visit_event_uid cleared manually on another waypoint).
		`{"timestamp":"2026-03-03T10:00:00Z","event":"FSDJump","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0],"JumpDist":30.0}`,
		// Multicrew jump to Hub Prime must be excluded.
		`{"timestamp":"2026-03-03T11:00:00Z","event":"FSDJump","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0],"JumpDist":30.0,"Multicrew":true}`,
		// Origin arrival for Main Scoped Expedition (selected=CMDR Main, 2026-03-04 -> open).
		`{"timestamp":"2026-03-04T09:00:00Z","event":"FSDJump","StarSystem":"Origin Two","SystemAddress":2002,"StarPos":[120,0,0],"JumpDist":70.0}`,
		// CMDR Main non-waypoint visit to Hub Prime during both Global and Main Scoped -> picks latest start (Main Scoped).
		`{"timestamp":"2026-03-04T10:00:00Z","event":"FSDJump","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0],"JumpDist":70.0}`,
		// Crash-recovered jump by CMDR Main back to Hub Prime.
		`{"timestamp":"2026-03-04T11:00:00Z","event":"FSDJump","StarSystem":"Relay Out","SystemAddress":2003,"StarPos":[140,0,0],"JumpDist":90.0}`,
		`{"timestamp":"2026-03-04T11:05:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"Hub Prime","SystemAddress":2000}`,
		`{"timestamp":"2026-03-04T11:30:00Z","event":"LoadGame","Commander":"CMDR Main","FID":"F101","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-03-04T11:31:00Z","event":"Location","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0]}`,

		// CMDR Alt visits Hub Prime during both Global (all_enabled) and Main Scoped (CMDR Main only):
		// must NOT be attributed to Main Scoped, so it falls through to Global Expedition.
		`{"timestamp":"2026-03-05T08:00:00Z","event":"LoadGame","Commander":"CMDR Alt","FID":"F202","Ship":"CobraMkIII","Odyssey":true}`,
		`{"timestamp":"2026-03-05T09:00:00Z","event":"FSDJump","StarSystem":"Hub Prime","SystemAddress":2000,"StarPos":[50,0,0],"JumpDist":50.0}`,
	)

	idsByName := expeditiontest.ProfileIDsByName(testingContext, databaseConnection)
	if len(idsByName) != 2 {
		testingContext.Fatalf("ProfileIDsByName: got %+v", idsByName)
	}

	globalExpedition, err := expedition.CreateWithWaypoints(databaseConnection, "Global Survey", "", []expedition.SeedWaypoint{
		{SystemName: "Origin One"},
		{SystemName: "Hub Prime"},
	})
	if err != nil {
		testingContext.Fatal(err)
	}
	if err := expedition.StartTx(databaseConnection, globalExpedition.ID, "2026-03-02T09:00:00Z", 0); err != nil {
		testingContext.Fatalf("StartTx global: %v", err)
	}
	if _, err := databaseConnection.Exec(`UPDATE expeditions SET status = 'completed', end_timestamp = '2026-03-10T00:00:00Z' WHERE id = ?`, globalExpedition.ID); err != nil {
		testingContext.Fatal(err)
	}

	// Add a legacy visited waypoint without visit_event_uid at 2026-03-03T10:00:00Z.
	legacyExpedition, err := expedition.CreateWithWaypoints(databaseConnection, "Legacy Plan", "", []expedition.SeedWaypoint{
		{SystemName: "Hub Prime"},
	})
	if err != nil {
		testingContext.Fatal(err)
	}
	legacyRows, err := expedition.ListRows(databaseConnection, legacyExpedition.ID)
	if err != nil || len(legacyRows) != 1 {
		testingContext.Fatalf("list legacy rows: %v", err)
	}
	if _, err := databaseConnection.Exec(`UPDATE expedition_rows SET visited_at = '2026-03-03T10:00:00Z', visit_event_uid = '' WHERE id = ?`, legacyRows[0].ID); err != nil {
		testingContext.Fatal(err)
	}

	scopedExpedition, err := expedition.CreateWithWaypoints(databaseConnection, "Main Only Run", "", []expedition.SeedWaypoint{
		{SystemName: "Origin Two"},
	})
	if err != nil {
		testingContext.Fatal(err)
	}
	if err := expedition.StartScopedTx(databaseConnection, scopedExpedition.ID, "2026-03-04T09:00:00Z", 0, expedition.ScopeSelected, []int64{idsByName["CMDR Main"]}); err != nil {
		testingContext.Fatalf("StartScopedTx scoped: %v", err)
	}

	// Partial query must NOT populate NonWaypointVisits.
	partialResponse, partialError := expedition.SearchWaypoints(databaseConnection, "Hub Pri", projectionsBundle)
	if partialError != nil {
		testingContext.Fatalf("SearchWaypoints(Hub Pri): %v", partialError)
	}
	if partialResponse.NonWaypointVisits != nil {
		testingContext.Fatalf("expected nil NonWaypointVisits on partial query, got %+v", partialResponse.NonWaypointVisits)
	}

	// Exact query "hub prime" returns 4 non-waypoint visits newest-first:
	// 2026-03-05T09:00:00Z (CMDR Alt -> Global Survey, NOT Main Only Run),
	// 2026-03-04T11:31:00Z (CMDR Main recovered jump -> Main Only Run),
	// 2026-03-04T10:00:00Z (CMDR Main FSDJump -> Main Only Run),
	// 2026-03-01T09:00:00Z (CMDR Main pre-expedition -> unattributed).
	exactResponse, exactError := expedition.SearchWaypoints(databaseConnection, "hub prime", projectionsBundle)
	if exactError != nil {
		testingContext.Fatalf("SearchWaypoints(hub prime): %v", exactError)
	}
	summary := exactResponse.NonWaypointVisits
	if summary == nil {
		testingContext.Fatal("expected NonWaypointVisits summary for exact query")
	}
	if summary.SystemName != "Hub Prime" || summary.TotalCount != 4 || summary.EarliestGalactic != reference.ToGalactic("2026-03-01T09:00:00Z") {
		testingContext.Fatalf("unexpected NonWaypointVisitSummary metadata: %+v", summary)
	}

	expectedVisits := []struct {
		timestamp      string
		expeditionID   int64
		expeditionName string
		status         string
	}{
		{timestamp: "2026-03-05T09:00:00Z", expeditionID: globalExpedition.ID, expeditionName: "Global Survey", status: expedition.StatusCompleted},
		{timestamp: "2026-03-04T11:31:00Z", expeditionID: scopedExpedition.ID, expeditionName: "Main Only Run", status: expedition.StatusActive},
		{timestamp: "2026-03-04T10:00:00Z", expeditionID: scopedExpedition.ID, expeditionName: "Main Only Run", status: expedition.StatusActive},
		{timestamp: "2026-03-01T09:00:00Z", expeditionID: 0, expeditionName: "", status: ""},
	}
	if len(summary.Visits) != len(expectedVisits) {
		testingContext.Fatalf("expected %d non-waypoint visits, got %+v", len(expectedVisits), summary.Visits)
	}
	for index, expected := range expectedVisits {
		actual := summary.Visits[index]
		if actual.Timestamp != expected.timestamp ||
			actual.Galactic != reference.ToGalactic(expected.timestamp) ||
			actual.ExpeditionID != expected.expeditionID ||
			actual.ExpeditionName != expected.expeditionName ||
			actual.ExpeditionStatus != expected.status {
			testingContext.Fatalf("visit %d = %+v, want %+v", index, actual, expected)
		}
	}

	// Location-only system returns single earliest Location fallback.
	starterResponse, starterError := expedition.SearchWaypoints(databaseConnection, "starter port", projectionsBundle)
	if starterError != nil {
		testingContext.Fatalf("SearchWaypoints(starter port): %v", starterError)
	}
	if starterResponse.NonWaypointVisits == nil ||
		starterResponse.NonWaypointVisits.TotalCount != 1 ||
		len(starterResponse.NonWaypointVisits.Visits) != 1 ||
		starterResponse.NonWaypointVisits.Visits[0].Timestamp != "2026-03-01T08:05:00Z" {
		testingContext.Fatalf("expected single earliest Location fallback for Starter Port, got %+v", starterResponse.NonWaypointVisits)
	}
}

func TestSearchWaypointsResultAndNonWaypointVisitCaps(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	// Seed 52 matching waypoints to verify TotalMatches=52 and Results capped at 50.
	seedWaypoints := make([]expedition.SeedWaypoint, 52)
	for index := range seedWaypoints {
		seedWaypoints[index] = expedition.SeedWaypoint{SystemName: fmt.Sprintf("CapSystem %02d", index+1)}
	}
	if _, err := expedition.CreateWithWaypoints(databaseConnection, "Large Plan", "", seedWaypoints); err != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", err)
	}

	cappedSearchResponse, searchError := expedition.SearchWaypoints(databaseConnection, "CapSystem", nil)
	if searchError != nil {
		testingContext.Fatalf("SearchWaypoints(CapSystem): %v", searchError)
	}
	if cappedSearchResponse.TotalMatches != 52 || len(cappedSearchResponse.Results) != 50 {
		testingContext.Fatalf("expected TotalMatches=52 and 50 capped results, got total=%d len=%d",
			cappedSearchResponse.TotalMatches, len(cappedSearchResponse.Results))
	}

	// Seed 205 non-waypoint jumps to Busy Hub to verify Visits capped at 200 with full TotalCount and EarliestGalactic.
	startTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	insertStmt, prepError := databaseConnection.Prepare(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at) VALUES (?, ?, 'FSDJump', 'Busy Hub', '{}', ?)`,
	)
	if prepError != nil {
		testingContext.Fatal(prepError)
	}
	defer insertStmt.Close()

	const totalSeededVisits = 205
	for index := 0; index < totalSeededVisits; index++ {
		visitTimestamp := reference.FormatISO(startTime.Add(time.Duration(index) * time.Hour))
		if _, err := insertStmt.Exec(fmt.Sprintf("busy-uid-%03d", index), visitTimestamp, visitTimestamp); err != nil {
			testingContext.Fatal(err)
		}
	}

	busyResponse, busyError := expedition.SearchWaypoints(databaseConnection, "Busy Hub", nil)
	if busyError != nil {
		testingContext.Fatalf("SearchWaypoints(Busy Hub): %v", busyError)
	}
	summary := busyResponse.NonWaypointVisits
	if summary == nil {
		testingContext.Fatal("expected NonWaypointVisits for Busy Hub")
	}
	expectedEarliest := reference.ToGalactic(reference.FormatISO(startTime))
	expectedNewest := reference.FormatISO(startTime.Add(time.Duration(totalSeededVisits-1) * time.Hour))
	if summary.TotalCount != totalSeededVisits ||
		len(summary.Visits) != 200 ||
		summary.EarliestGalactic != expectedEarliest ||
		summary.Visits[0].Timestamp != expectedNewest {
		testingContext.Fatalf("unexpected Busy Hub truncation summary: total=%d len=%d earliest=%q newest=%q",
			summary.TotalCount, len(summary.Visits), summary.EarliestGalactic, summary.Visits[0].Timestamp)
	}
}
