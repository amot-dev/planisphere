package expedition_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/modes"
)

func lookupDefaultModeIDs(testingContext *testing.T, databaseConnection *sql.DB) (explorationID, tourismID, travelID int64) {
	testingContext.Helper()
	modeList, listError := modes.List(databaseConnection)
	if listError != nil {
		testingContext.Fatalf("modes.List: %v", listError)
	}
	for _, modeItem := range modeList {
		switch modeItem.Name {
		case "Exploration":
			explorationID = modeItem.ID
		case "Tourism":
			tourismID = modeItem.ID
		case "Travel":
			travelID = modeItem.ID
		}
	}
	if explorationID == 0 || tourismID == 0 || travelID == 0 {
		testingContext.Fatalf("missing default modes: exp=%d tour=%d travel=%d", explorationID, tourismID, travelID)
	}
	return explorationID, tourismID, travelID
}

func TestPerExpeditionLegModesEngageCascadeAndActiveSync(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	_, expeditionProjections := newExpeditionProjections(databaseConnection)
	explorationID, tourismID, travelID := lookupDefaultModeIDs(testingContext, databaseConnection)

	ingestJournalLines(testingContext, databaseConnection, []string{
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":1002,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-04-01T12:00:00Z","event":"FSDJump","StarSystem":"Beta","SystemAddress":1003,"StarPos":[20,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-04-01T13:00:00Z","event":"FSDJump","StarSystem":"Gamma","SystemAddress":1004,"StarPos":[30,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-04-01T13:15:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Gamma 1","BodyID":1,"StarSystem":"Gamma","SystemAddress":1004,"PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-01T13:30:00Z","event":"FSDJump","StarSystem":"Transit Out","SystemAddress":1005,"StarPos":[35,0,0],"JumpDist":5.0}`,
	})

	expeditionRecord, createError := expedition.CreateWithWaypoints(databaseConnection, "Bulk Engage Test", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Alpha"},
		{SystemName: "Beta"},
		{SystemName: "Gamma"},
		{SystemName: "Delta"},
	})
	if createError != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", createError)
	}
	if expeditionRecord.ActiveModeID != travelID {
		testingContext.Fatalf("expected new expedition default active_mode_id=%d (Travel), got %d", travelID, expeditionRecord.ActiveModeID)
	}

	// Engage with Exploration: all already-visited waypoints #2..#4 and active_mode_id must become Exploration.
	if startError := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-04-01T10:00:00Z", explorationID); startError != nil {
		testingContext.Fatalf("StartTx: %v", startError)
	}
	rows, listError := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listError != nil {
		testingContext.Fatalf("ListRows: %v", listError)
	}
	if rows[0].LegModeID != 0 {
		testingContext.Fatalf("expected origin row LegModeID=0, got %d", rows[0].LegModeID)
	}
	for _, index := range []int{1, 2, 3} {
		if rows[index].LegModeID != explorationID {
			testingContext.Fatalf("expected visited row %d LegModeID=%d, got %d", index, explorationID, rows[index].LegModeID)
		}
	}

	// Editing the unvisited active in-progress leg (Delta) updates expedition.active_mode_id.
	if _, setActiveLegError := expedition.SetLegModeTx(databaseConnection, rows[4].ID, tourismID, false); setActiveLegError != nil {
		testingContext.Fatalf("SetLegModeTx active leg: %v", setActiveLegError)
	}
	planAfterActiveSync, planError := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, expeditionProjections)
	if planError != nil {
		testingContext.Fatalf("BuildPlan: %v", planError)
	}
	if planAfterActiveSync.Expedition.ActiveModeID != tourismID || planAfterActiveSync.Rows[4].LegModeID != tourismID {
		testingContext.Fatalf("expected active leg and expedition activeModeId=%d, got exp=%d row=%d",
			tourismID, planAfterActiveSync.Expedition.ActiveModeID, planAfterActiveSync.Rows[4].LegModeID)
	}

	// Cascading Travel from Beta (index 2) updates Beta and Gamma only, leaving Alpha (Exploration)
	// and active in-progress Delta / active_mode_id (Tourism) untouched.
	if _, cascadeError := expedition.SetLegModeTx(databaseConnection, rows[2].ID, travelID, true); cascadeError != nil {
		testingContext.Fatalf("SetLegModeTx cascade: %v", cascadeError)
	}
	rowsAfterCascade, _ := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	expAfterCascade, _ := expedition.Get(databaseConnection, expeditionRecord.ID)
	if rowsAfterCascade[1].LegModeID != explorationID || rowsAfterCascade[2].LegModeID != travelID || rowsAfterCascade[3].LegModeID != travelID {
		testingContext.Fatalf("unexpected leg modes after cascade: alpha=%d beta=%d gamma=%d",
			rowsAfterCascade[1].LegModeID, rowsAfterCascade[2].LegModeID, rowsAfterCascade[3].LegModeID)
	}
	if expAfterCascade.ActiveModeID != tourismID {
		testingContext.Fatalf("expected cascade on visited legs to preserve active_mode_id=%d, got %d", tourismID, expAfterCascade.ActiveModeID)
	}

	// Completed expeditions reject SetActiveMode but still allow per-leg mode updates.
	if completeError := expedition.CompleteTx(databaseConnection, expeditionRecord.ID); completeError != nil {
		testingContext.Fatalf("CompleteTx: %v", completeError)
	}
	if setModeError := expedition.SetActiveModeTx(databaseConnection, expeditionRecord.ID, explorationID); setModeError == nil {
		testingContext.Fatalf("expected SetActiveModeTx on completed expedition to fail")
	}
	if _, completedLegError := expedition.SetLegModeTx(databaseConnection, rowsAfterCascade[1].ID, tourismID, false); completedLegError != nil {
		testingContext.Fatalf("expected per-leg mode edit on completed expedition to succeed: %v", completedLegError)
	}

	// Cloning resets active_mode_id back to Travel.
	clonedExpedition, cloneError := expedition.Clone(databaseConnection, expeditionRecord.ID)
	if cloneError != nil {
		testingContext.Fatalf("Clone: %v", cloneError)
	}
	if clonedExpedition.ActiveModeID != travelID {
		testingContext.Fatalf("expected cloned expedition active_mode_id=%d, got %d", travelID, clonedExpedition.ActiveModeID)
	}
}

func TestHistoricalLegSplitInheritsSplitLegModeAndOverlappingExpeditionsIsolateStats(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	_, expeditionProjections := newExpeditionProjections(databaseConnection)
	explorationID, tourismID, travelID := lookupDefaultModeIDs(testingContext, databaseConnection)

	ingestJournalLines(testingContext, databaseConnection, []string{
		`{"timestamp":"2026-05-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":3001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-05-01T11:00:00Z","event":"FSDJump","StarSystem":"MidStop","SystemAddress":3002,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-05-01T12:00:00Z","event":"FSDJump","StarSystem":"OuterStop","SystemAddress":3003,"StarPos":[35,0,0],"JumpDist":25.0}`,
	})

	expeditionA, createAError := expedition.CreateWithWaypoints(databaseConnection, "Expedition A", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "OuterStop"},
		{SystemName: "MidStop"},
	})
	if createAError != nil {
		testingContext.Fatalf("CreateWithWaypoints A: %v", createAError)
	}
	if startAError := expedition.StartTx(databaseConnection, expeditionA.ID, "2026-05-01T10:00:00Z", explorationID); startAError != nil {
		testingContext.Fatalf("StartTx A: %v", startAError)
	}
	// Switch expedition A's active mode to Travel before slotting MidStop between Sol and OuterStop (Exploration).
	if setActiveError := expedition.SetActiveModeTx(databaseConnection, expeditionA.ID, travelID); setActiveError != nil {
		testingContext.Fatalf("SetActiveModeTx A: %v", setActiveError)
	}
	rowsA, _ := expedition.ListRows(databaseConnection, expeditionA.ID)
	if moveError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		if _, err := expedition.MoveRow(transaction, rowsA[2].ID, 1); err != nil {
			return err
		}
		_, err := expedition.Reevaluate(transaction, expeditionA.ID)
		return err
	}); moveError != nil {
		testingContext.Fatalf("MoveRow MidStop into historical slot: %v", moveError)
	}
	updatedRowsA, _ := expedition.ListRows(databaseConnection, expeditionA.ID)
	if updatedRowsA[1].SystemName != "MidStop" || updatedRowsA[1].LegModeID != explorationID {
		testingContext.Fatalf("expected split historical MidStop to inherit Exploration (%d), got %+v", explorationID, updatedRowsA[1])
	}

	// Create overlapping Expedition B covering the identical date range but tagged Tourism on MidStop and Travel on OuterStop.
	expeditionB, createBError := expedition.CreateWithWaypoints(databaseConnection, "Expedition B", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "MidStop"},
		{SystemName: "OuterStop"},
	})
	if createBError != nil {
		testingContext.Fatalf("CreateWithWaypoints B: %v", createBError)
	}
	if startBError := expedition.StartTx(databaseConnection, expeditionB.ID, "2026-05-01T10:00:00Z", tourismID); startBError != nil {
		testingContext.Fatalf("StartTx B: %v", startBError)
	}
	rowsB, _ := expedition.ListRows(databaseConnection, expeditionB.ID)
	if _, setOuterError := expedition.SetLegModeTx(databaseConnection, rowsB[2].ID, travelID, false); setOuterError != nil {
		testingContext.Fatalf("SetLegModeTx B outer: %v", setOuterError)
	}

	sharedRange := replaystats.Range{After: "2026-05-01T10:00:00Z", Until: "2026-05-01T12:00:00Z"}
	summaryA, errA := expeditionProjections.TimeRangeStats.Compute(sharedRange, &replaystats.Filter{
		Enabled:      true,
		ExpeditionID: expeditionA.ID,
		Modes:        []replaystats.ModeSelection{{ModeID: explorationID, Travel: true, Discovery: true}},
	})
	if errA != nil {
		testingContext.Fatalf("Compute A: %v", errA)
	}
	summaryB, errB := expeditionProjections.TimeRangeStats.Compute(sharedRange, &replaystats.Filter{
		Enabled:      true,
		ExpeditionID: expeditionB.ID,
		Modes:        []replaystats.ModeSelection{{ModeID: tourismID, Travel: true, Discovery: true}},
	})
	if errB != nil {
		testingContext.Fatalf("Compute B: %v", errB)
	}
	if summaryA.Metrics.DistanceLy != 35.0 || summaryB.Metrics.DistanceLy != 10.0 {
		testingContext.Fatalf("expected isolated overlapping stats A=35.0 Ly B=10.0 Ly, got A=%.1f B=%.1f",
			summaryA.Metrics.DistanceLy, summaryB.Metrics.DistanceLy)
	}
}

func TestCustomModeUsageAndDeleteReassignment(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	_, _, travelID := lookupDefaultModeIDs(testingContext, databaseConnection)

	customMode, createModeError := modes.Create(databaseConnection, "Exobio Sprint", "Fast biology loop", "#00E5A3")
	if createModeError != nil {
		testingContext.Fatalf("modes.Create: %v", createModeError)
	}

	ingestJournalLines(testingContext, databaseConnection, []string{
		`{"timestamp":"2026-07-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":4001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-07-01T11:00:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":4002,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-07-01T12:00:00Z","event":"FSDJump","StarSystem":"Beta","SystemAddress":4003,"StarPos":[20,0,0],"JumpDist":10.0}`,
	})

	activeExp, _ := expedition.CreateWithWaypoints(databaseConnection, "Active Custom", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"}, {SystemName: "Alpha"}, {SystemName: "Beta"},
	})
	if err := expedition.StartTx(databaseConnection, activeExp.ID, "2026-07-01T10:00:00Z", customMode.ID); err != nil {
		testingContext.Fatalf("StartTx activeExp: %v", err)
	}

	completedExp, _ := expedition.CreateWithWaypoints(databaseConnection, "Completed Unused", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"}, {SystemName: "Alpha"},
	})
	if err := expedition.StartTx(databaseConnection, completedExp.ID, "2026-07-01T10:00:00Z", customMode.ID); err != nil {
		testingContext.Fatalf("StartTx completedExp: %v", err)
	}
	completedRows, _ := expedition.ListRows(databaseConnection, completedExp.ID)
	if _, err := expedition.SetLegModeTx(databaseConnection, completedRows[1].ID, travelID, false); err != nil {
		testingContext.Fatalf("SetLegModeTx completedExp: %v", err)
	}
	if err := expedition.CompleteTx(databaseConnection, completedExp.ID); err != nil {
		testingContext.Fatalf("CompleteTx completedExp: %v", err)
	}

	usageList, usageError := modes.UsageByExpedition(databaseConnection, customMode.ID)
	if usageError != nil {
		testingContext.Fatalf("UsageByExpedition: %v", usageError)
	}
	if len(usageList) != 1 || usageList[0].ExpeditionID != activeExp.ID || usageList[0].LegCount != 2 || !usageList[0].IsActiveMode {
		testingContext.Fatalf("unexpected mode usage list (completed inactive active_mode_id should be excluded): %+v", usageList)
	}

	if deleteError := modes.DeleteTx(databaseConnection, customMode.ID); deleteError != nil {
		testingContext.Fatalf("DeleteTx: %v", deleteError)
	}
	reloadedActiveExp, _ := expedition.Get(databaseConnection, activeExp.ID)
	reloadedCompletedExp, _ := expedition.Get(databaseConnection, completedExp.ID)
	reloadedActiveRows, _ := expedition.ListRows(databaseConnection, activeExp.ID)
	if reloadedActiveExp.ActiveModeID != travelID || reloadedCompletedExp.ActiveModeID != travelID {
		testingContext.Fatalf("expected active_mode_id reassigned to Travel (%d), got active=%d completed=%d",
			travelID, reloadedActiveExp.ActiveModeID, reloadedCompletedExp.ActiveModeID)
	}
	if reloadedActiveRows[1].LegModeID != travelID || reloadedActiveRows[2].LegModeID != travelID {
		testingContext.Fatalf("expected visited leg modes reassigned to Travel (%d), got %d / %d",
			travelID, reloadedActiveRows[1].LegModeID, reloadedActiveRows[2].LegModeID)
	}
}

func TestMigrateLegModesV11BackfillFromLegacyModeWindows(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	explorationID, tourismID, travelID := lookupDefaultModeIDs(testingContext, databaseConnection)

	expeditionRecord, _ := expedition.CreateWithWaypoints(databaseConnection, "Legacy Upgrade", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"}, {SystemName: "Alpha"}, {SystemName: "Beta"},
	})
	rows, _ := expedition.ListRows(databaseConnection, expeditionRecord.ID)

	// Simulate a pre-v11 database state with legacy mode_windows and unmapped active_mode_id / leg_mode_id.
	setupStatements := []struct {
		query string
		args  []any
	}{
		{`UPDATE expeditions SET status = 'active', start_timestamp = '2026-08-01T10:00:00Z', active_mode_id = 0 WHERE id = ?`, []any{expeditionRecord.ID}},
		{`UPDATE expedition_rows SET visited_at = '2026-08-01T10:00:00Z', leg_mode_id = 0 WHERE id = ?`, []any{rows[0].ID}},
		{`UPDATE expedition_rows SET visited_at = '2026-08-01T11:00:00Z', leg_mode_id = 0 WHERE id = ?`, []any{rows[1].ID}},
		{`UPDATE expedition_rows SET visited_at = '2026-08-01T12:00:00Z', leg_mode_id = 0 WHERE id = ?`, []any{rows[2].ID}},
		{`INSERT INTO mode_windows(mode_id, start_timestamp, end_timestamp) VALUES (?, '2026-08-01T10:30:00Z', '2026-08-01T11:30:00Z'), (?, '2026-08-01T13:00:00Z', NULL)`, []any{explorationID, tourismID}},
		{`DELETE FROM settings WHERE key = 'leg_modes_v11'`, nil},
	}
	for _, stmt := range setupStatements {
		if _, execError := databaseConnection.Exec(stmt.query, stmt.args...); execError != nil {
			testingContext.Fatalf("seed legacy pre-v11 state (%s): %v", stmt.query, execError)
		}
	}

	if migrateError := db.Migrate(databaseConnection); migrateError != nil {
		testingContext.Fatalf("db.Migrate v11: %v", migrateError)
	}

	migratedExp, _ := expedition.Get(databaseConnection, expeditionRecord.ID)
	migratedRows, _ := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if migratedExp.ActiveModeID != tourismID {
		testingContext.Fatalf("expected migrated active_mode_id=%d from open mode_window, got %d", tourismID, migratedExp.ActiveModeID)
	}
	if migratedRows[0].LegModeID != 0 {
		testingContext.Fatalf("expected origin waypoint to stay LegModeID=0, got %d", migratedRows[0].LegModeID)
	}
	if migratedRows[1].LegModeID != explorationID || migratedRows[2].LegModeID != travelID {
		testingContext.Fatalf("expected Alpha=%d (covered window) and Beta=%d (Travel fallback), got Alpha=%d Beta=%d",
			explorationID, travelID, migratedRows[1].LegModeID, migratedRows[2].LegModeID)
	}
}

func TestLoadModeLookupDepartureSpansAndInSystemFrontier(testingContext *testing.T) {
	databaseConnection := openExpeditionTestDB(testingContext)
	_, expeditionProjections := newExpeditionProjections(databaseConnection)
	explorationID, _, travelID := lookupDefaultModeIDs(testingContext, databaseConnection)

	ingestJournalLines(testingContext, databaseConnection, []string{
		`{"timestamp":"2026-10-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":6001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-10-01T11:00:00Z","event":"FSDJump","StarSystem":"Vela","SystemAddress":6002,"StarPos":[10,0,0],"JumpDist":10.0}`,
		`{"timestamp":"2026-10-01T11:15:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Vela 1","BodyID":1,"StarSystem":"Vela","SystemAddress":6002,"PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	})

	expeditionRecord, _ := expedition.CreateWithWaypoints(databaseConnection, "In-System Mode Test", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Vela"},
		{SystemName: "Colonia"},
	})
	if startErr := expedition.StartTx(databaseConnection, expeditionRecord.ID, "2026-10-01T10:00:00Z", explorationID); startErr != nil {
		testingContext.Fatalf("StartTx: %v", startErr)
	}
	// Pre-select Travel for the upcoming Colonia leg while still parked inside Vela.
	if setErr := expedition.SetActiveModeTx(databaseConnection, expeditionRecord.ID, travelID); setErr != nil {
		testingContext.Fatalf("SetActiveModeTx: %v", setErr)
	}

	// While still inside Vela, Vela's in-system scan (11:15) must belong to Vela's Exploration mode, not active_mode_id Travel.
	lookupWhileInSystem, lookupErr := expedition.LoadModeLookup(databaseConnection, expeditionRecord.ID)
	if lookupErr != nil {
		testingContext.Fatalf("LoadModeLookup in-system: %v", lookupErr)
	}
	if gotMode := lookupWhileInSystem("2026-10-01T11:15:00Z"); gotMode != explorationID {
		testingContext.Fatalf("expected Vela in-system scan to map to Exploration (%d), got %d", explorationID, gotMode)
	}

	// Depart Vela for Colonia: Vela's scan remains Exploration while the departure jump maps to Travel.
	if _, execErr := databaseConnection.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, raw_json, source_file, ingested_at)
		 VALUES ('ev-mode-depart-vela', '2026-10-01T11:30:00Z', 'FSDJump', 'Transit', 6003, 20, 0, 0, '{"timestamp":"2026-10-01T11:30:00Z","event":"FSDJump","StarSystem":"Transit","SystemAddress":6003,"StarPos":[20,0,0],"JumpDist":10.0}', 't.log', '2026-10-01T11:30:00Z')`,
	); execErr != nil {
		testingContext.Fatalf("insert departure jump: %v", execErr)
	}
	if _, reevalErr := expedition.ReevaluateTx(databaseConnection, expeditionRecord.ID); reevalErr != nil {
		testingContext.Fatalf("ReevaluateTx: %v", reevalErr)
	}

	lookupAfterDepart, lookupAfterErr := expedition.LoadModeLookup(databaseConnection, expeditionRecord.ID)
	if lookupAfterErr != nil {
		testingContext.Fatalf("LoadModeLookup after departure: %v", lookupAfterErr)
	}
	if gotScanMode := lookupAfterDepart("2026-10-01T11:15:00Z"); gotScanMode != explorationID {
		testingContext.Fatalf("expected Vela scan after departure to stay Exploration (%d), got %d", explorationID, gotScanMode)
	}
	if gotDepartMode := lookupAfterDepart("2026-10-01T11:30:00Z"); gotDepartMode != travelID {
		testingContext.Fatalf("expected departure jump to map to Travel (%d), got %d", travelID, gotDepartMode)
	}

	planView, _ := expedition.BuildPlan(databaseConnection, expeditionRecord.ID, nil, expeditionProjections)
	if planView.Rows[2].VisitState != expedition.VisitCurrent || planView.Rows[2].LegModeID != travelID {
		testingContext.Fatalf("expected Colonia to advance to VisitCurrent with Travel (%d) after departing Vela, got state=%q mode=%d",
			travelID, planView.Rows[2].VisitState, planView.Rows[2].LegModeID)
	}
}
