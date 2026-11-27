package realization_test

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections/realization"
	"planisphere/internal/replay"
	replayreal "planisphere/internal/replay/realization"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestSalesLedgerCachingAndBusInvalidation(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	rawOrganicScan := `{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		rawOrganicScan,
	)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	if !salesLedger.Triggers().Journal.Has(journal.EventSellOrganicData) || salesLedger.Triggers().Journal.Has(journal.EventFSDJump) || salesLedger.IsValid() {
		testingContext.Fatalf("unexpected cold SalesLedger state: triggers=%v valid=%v", salesLedger.Triggers(), salesLedger.IsValid())
	}

	firstResult, firstError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if firstError != nil || !salesLedger.IsValid() {
		testingContext.Fatalf("first Breakdown failed: valid=%v err=%v", salesLedger.IsValid(), firstError)
	}
	if firstResult.PendingValue <= 0 {
		testingContext.Fatalf("expected positive PendingValue, got %d", firstResult.PendingValue)
	}

	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`,
	)

	// Unrelated and session-context-only events must leave cached SalesLedger untouched.
	initialLedgerGeneration := salesLedger.Generation()
	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventFSDTarget,
		journal.EventFSSDiscoveryScan,
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventDocked,
		journal.EventUndocked,
	))
	if salesLedger.Generation() != initialLedgerGeneration {
		testingContext.Fatalf("expected session-context events not to invalidate SalesLedger, gen %d->%d", initialLedgerGeneration, salesLedger.Generation())
	}
	cachedResult, cachedError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if cachedError != nil {
		testingContext.Fatalf("cached Breakdown failed: %v", cachedError)
	}
	if cachedResult.PendingValue != firstResult.PendingValue {
		testingContext.Fatalf("expected identical cached pending value %d, got %d", firstResult.PendingValue, cachedResult.PendingValue)
	}

	// Publishing EventSellOrganicData invalidates SalesLedger so the sale appears.
	eventBus.PublishJournal(journal.NewEventSet(journal.EventSellOrganicData))
	refreshedResult, refreshedError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if refreshedError != nil {
		testingContext.Fatalf("refreshed Breakdown failed: %v", refreshedError)
	}
	if refreshedResult.PendingValue != 0 || refreshedResult.TotalRealized != 95054000 {
		testingContext.Fatalf("unexpected refreshed breakdown: %+v", refreshedResult)
	}
	var organicScanUID string
	if scanUIDErr := databaseConnection.QueryRow(`SELECT event_uid FROM journal_events WHERE event = 'ScanOrganic' LIMIT 1`).Scan(&organicScanUID); scanUIDErr != nil {
		testingContext.Fatalf("query ScanOrganic event_uid: %v", scanUIDErr)
	}
	saleOutcome, outcomeFound, outcomeErr := salesLedger.OrganicSaleDetails(organicScanUID)
	if outcomeErr != nil || !outcomeFound || !saleOutcome.BonusRealized || saleOutcome.SaleMarketID != 123 || saleOutcome.SaleTimestamp != "2026-03-01T12:00:00Z" {
		testingContext.Fatalf("unexpected OrganicSaleDetails: outcome=%+v found=%v err=%v", saleOutcome, outcomeFound, outcomeErr)
	}
	if _, missingFound, missingErr := salesLedger.OrganicSaleDetails("nonexistent-uid"); missingErr != nil || missingFound {
		testingContext.Fatalf("expected missing OrganicSaleDetails to return false, got found=%v err=%v", missingFound, missingErr)
	}

	// Publishing AppCommanderFilterChanged also invalidates the cached ledger.
	if _, execError := databaseConnection.Exec(`UPDATE journal_events SET commander = 'CmdrAlt'`); execError != nil {
		testingContext.Fatal(execError)
	}
	if _, execError := databaseConnection.Exec(`INSERT INTO commanders(name, enabled) VALUES ('CmdrAlt', 0)`); execError != nil {
		testingContext.Fatal(execError)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)
	filteredResult, filteredError := salesLedger.Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)
	if filteredError != nil {
		testingContext.Fatalf("filtered Breakdown failed: %v", filteredError)
	}
	if filteredResult.TotalRealized != 0 {
		testingContext.Fatalf("expected TotalRealized 0 after disabling commander, got %d", filteredResult.TotalRealized)
	}
}

func insertScopedLedgerEvent(testingContext *testing.T, databaseConnection *sql.DB, profileID int64, commander, timestamp, eventName, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, rawJSON+"|"+commander)
	_, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, profile_id, commander, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, profileID, commander, rawJSON, timestamp,
	)
	if execError != nil {
		testingContext.Fatalf("insert scoped %s event: %v", eventName, execError)
	}
}

func TestSalesLedgerPerProfileIsolationAndDisabledScope(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	if _, err := databaseConnection.Exec(`
		INSERT INTO commander_profiles (id, profile_key, display_name, fid, universe, era_index, is_archived_career, enabled, first_seen, last_seen) VALUES
		(1, 'live:f1:era:0', 'CmdrOne', 'F1', 'live', 0, 0, 1, '2026-04-01T10:00:00Z', '2026-04-01T12:00:00Z'),
		(2, 'live:f2:era:0', 'CmdrTwo', 'F2', 'live', 0, 0, 1, '2026-04-01T10:00:00Z', '2026-04-01T12:00:00Z'),
		(3, 'live:f1:era:0_old', 'CmdrArchived', 'F3', 'live', 0, 1, 0, '2025-01-01T10:00:00Z', '2025-01-01T12:00:00Z')
	`); err != nil {
		testingContext.Fatalf("seed commander_profiles: %v", err)
	}

	// Profile 1 (enabled) scans an Earthlike body and Stratum Tectonicas and leaves both unsold in transit.
	insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrOne", "2026-04-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"AlphaSys","SystemAddress":1001,"BodyID":1,"BodyName":"AlphaSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrOne", "2026-04-01T10:05:00Z", "ScanOrganic",
		`{"timestamp":"2026-04-01T10:05:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1001,"Body":1}`)

	// Profile 2 (enabled) scans the same species and an Earthlike body, suffers ship destruction (rebuy), then scans and sells new data.
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T10:10:00Z", "Scan",
		`{"timestamp":"2026-04-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"AlphaSys","SystemAddress":1001,"BodyID":1,"BodyName":"AlphaSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T10:15:00Z", "ScanOrganic",
		`{"timestamp":"2026-04-01T10:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1001,"Body":1}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T10:30:00Z", "Resurrect",
		`{"timestamp":"2026-04-01T10:30:00Z","event":"Resurrect","Option":"rebuy","Cost":1200000,"Bankrupt":false}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T11:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-04-01T11:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":2002,"Body":2}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T11:30:00Z", "SellOrganicData",
		`{"timestamp":"2026-04-01T11:30:00Z","event":"SellOrganicData","MarketID":500,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 2, "CmdrTwo", "2026-04-01T11:30:01Z", "MultiSellExplorationData",
		`{"timestamp":"2026-04-01T11:30:01Z","event":"MultiSellExplorationData","BaseValue":500000,"Bonus":0,"TotalEarnings":500000,"Discovered":[{"SystemName":"AlphaSys","NumBodies":1}]}`)

	// Profile 3 (disabled archived career) has its own sold organic sample.
	insertScopedLedgerEvent(testingContext, databaseConnection, 3, "CmdrArchived", "2025-01-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2025-01-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":3003,"Body":1}`)
	insertScopedLedgerEvent(testingContext, databaseConnection, 3, "CmdrArchived", "2025-01-01T11:00:00Z", "SellOrganicData",
		`{"timestamp":"2025-01-01T11:00:00Z","event":"SellOrganicData","MarketID":600,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)

	prof1Breakdown, err := salesLedger.BreakdownScoped("", "", replay.NewProfileScope([]int64{1}), nil)
	if err != nil {
		testingContext.Fatalf("BreakdownScoped(prof 1): %v", err)
	}
	if prof1Breakdown.PendingValue <= 95054000 || prof1Breakdown.CartoLostDeath != 0 || prof1Breakdown.OrganicLostDeath != 0 || prof1Breakdown.TotalRealized != 0 {
		testingContext.Fatalf("expected Profile 1 scans 100%% pending and untouched by Profile 2 death/sales, got %+v", prof1Breakdown)
	}

	prof2Breakdown, err := salesLedger.BreakdownScoped("", "", replay.NewProfileScope([]int64{2}), nil)
	if err != nil {
		testingContext.Fatalf("BreakdownScoped(prof 2): %v", err)
	}
	if prof2Breakdown.CartoLostDeath <= 0 || prof2Breakdown.OrganicLostDeath != 95054000 || prof2Breakdown.TotalRealized != 19510800 || prof2Breakdown.PendingValue != 0 {
		testingContext.Fatalf("unexpected Profile 2 breakdown: %+v", prof2Breakdown)
	}

	combinedBreakdown, err := salesLedger.Breakdown("", "", nil)
	if err != nil {
		testingContext.Fatalf("combined Breakdown: %v", err)
	}
	expectedCombined := prof1Breakdown.Add(prof2Breakdown)
	if combinedBreakdown != expectedCombined {
		testingContext.Fatalf("expected combined AllEnabled breakdown %+v, got %+v", expectedCombined, combinedBreakdown)
	}

	genBeforeArchived := salesLedger.Generation()
	prof3Breakdown, err := salesLedger.BreakdownScoped("", "", replay.NewProfileScope([]int64{3}), nil)
	if err != nil {
		testingContext.Fatalf("BreakdownScoped(disabled prof 3): %v", err)
	}
	if prof3Breakdown.TotalRealized != 95054000 || prof3Breakdown.PendingValue != 0 || salesLedger.Generation() != genBeforeArchived {
		testingContext.Fatalf("expected disabled archived Profile 3 breakdown TotalRealized=95054000 from warm ledger, got %+v", prof3Breakdown)
	}
}

func TestSalesLedgerDatabaseSwitchResetAndConcurrentRaceSafety(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	// Seed multiple initial rows so lastEventID > 1 before testing full-state reset on AppDatabaseSwitched and Invalidate.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-06-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"OldSysA","SystemAddress":6001,"BodyID":1,"BodyName":"OldSysA 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-06-01T10:01:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"OldSysB","SystemAddress":6002,"BodyID":1,"BodyName":"OldSysB 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-06-01T10:02:00Z","event":"MultiSellExplorationData","BaseValue":2000000,"Bonus":0,"TotalEarnings":2000000,"Discovered":[{"SystemName":"OldSysA","NumBodies":1},{"SystemName":"OldSysB","NumBodies":1}]}`,
	)

	eventBus := projection.NewBus()
	salesLedger := realization.NewSalesLedger(databaseConnection, eventBus)
	if salesLedger.Name() != "SalesLedger" {
		testingContext.Fatalf("unexpected projection name %q", salesLedger.Name())
	}
	initialBreakdown, initErr := salesLedger.Breakdown("", "", nil)
	if initErr != nil || initialBreakdown.TotalRealized != 2000000 {
		testingContext.Fatalf("initial Breakdown: %+v err=%v", initialBreakdown, initErr)
	}

	// Simulate switching to a smaller archive (primary keys reset to 1) via AppDatabaseSwitched.
	if _, err := databaseConnection.Exec(`DELETE FROM journal_events; DELETE FROM sqlite_sequence WHERE name = 'journal_events'`); err != nil {
		testingContext.Fatalf("reset journal_events sequence: %v", err)
	}
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-06-02T09:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":7001,"Body":1}`,
	)
	eventBus.PublishApp(events.AppDatabaseSwitched)

	switchedBreakdown, switchErr := salesLedger.Breakdown("", "", nil)
	if switchErr != nil || switchedBreakdown.TotalRealized != 0 || switchedBreakdown.PendingValue != 19010800 {
		testingContext.Fatalf("expected cold rebuild after AppDatabaseSwitched, got %+v err=%v", switchedBreakdown, switchErr)
	}

	// Direct Invalidate must also discard retained watermark state.
	if _, err := databaseConnection.Exec(`DELETE FROM journal_events; DELETE FROM sqlite_sequence WHERE name = 'journal_events'`); err != nil {
		testingContext.Fatalf("reset journal_events for direct Invalidate: %v", err)
	}
	salesLedger.Invalidate()
	clearedBreakdown, clearErr := salesLedger.Breakdown("", "", nil)
	if clearErr != nil || clearedBreakdown != (realization.Breakdown{}) {
		testingContext.Fatalf("expected empty ledger after direct Invalidate, got %+v err=%v", clearedBreakdown, clearErr)
	}

	// Concurrent readers (Get, BreakdownScoped, OrganicSaleDetails) racing with live journal appends and invalidations.
	var readerGroup sync.WaitGroup
	stopReaders := make(chan struct{})
	readerErrors := make(chan error, 8)
	scopeProf1 := replay.NewProfileScope([]int64{1})

	for worker := 0; worker < 4; worker++ {
		readerGroup.Add(1)
		go func() {
			defer readerGroup.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				snapshot, getErr := salesLedger.Get()
				if getErr != nil {
					select {
					case readerErrors <- getErr:
					default:
					}
					return
				}
				_ = snapshot.Breakdown("", "", nil)
				if _, scopedErr := salesLedger.BreakdownScoped("", "", scopeProf1, nil); scopedErr != nil {
					select {
					case readerErrors <- scopedErr:
					default:
					}
					return
				}
				if _, _, detailsErr := salesLedger.OrganicSaleDetails("probe-uid"); detailsErr != nil {
					select {
					case readerErrors <- detailsErr:
					default:
					}
					return
				}
			}
		}()
	}

	for step := 1; step <= 25; step++ {
		tsScan := fmt.Sprintf("2026-06-03T10:%02d:00Z", step)
		tsMap := fmt.Sprintf("2026-06-03T10:%02d:10Z", step)
		tsOrg := fmt.Sprintf("2026-06-03T10:%02d:20Z", step)
		tsAction := fmt.Sprintf("2026-06-03T10:%02d:30Z", step)
		sysAddr := int64(8000 + step)

		insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrRace", tsScan, "Scan",
			fmt.Sprintf(`{"timestamp":%q,"event":"Scan","ScanType":"Detailed","StarSystem":"RaceSys %d","SystemAddress":%d,"BodyID":1,"BodyName":"RaceSys %d A","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`, tsScan, step, sysAddr, step))
		eventBus.PublishJournal(journal.NewEventSet(journal.EventScan))

		insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrRace", tsMap, "SAAScanComplete",
			fmt.Sprintf(`{"timestamp":%q,"event":"SAAScanComplete","SystemAddress":%d,"BodyID":1,"BodyName":"RaceSys %d A","ProbesUsed":4,"EfficiencyTarget":6}`, tsMap, sysAddr, step))
		eventBus.PublishJournal(journal.NewEventSet(journal.EventSAAScanComplete))

		insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrRace", tsOrg, "ScanOrganic",
			fmt.Sprintf(`{"timestamp":%q,"event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":%d,"Body":1}`, tsOrg, sysAddr))
		eventBus.PublishJournal(journal.NewEventSet(journal.EventScanOrganic))

		if step%3 == 0 {
			insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrRace", tsAction, "Resurrect",
				fmt.Sprintf(`{"timestamp":%q,"event":"Resurrect","Option":"rebuy","Cost":100000,"Bankrupt":false}`, tsAction))
			eventBus.PublishJournal(journal.NewEventSet(journal.EventResurrect))
		} else {
			insertScopedLedgerEvent(testingContext, databaseConnection, 1, "CmdrRace", tsAction, "SellOrganicData",
				fmt.Sprintf(`{"timestamp":%q,"event":"SellOrganicData","MarketID":555,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`, tsAction))
			eventBus.PublishJournal(journal.NewEventSet(journal.EventSellOrganicData))
		}
	}

	close(stopReaders)
	readerGroup.Wait()
	select {
	case workerErr := <-readerErrors:
		testingContext.Fatalf("concurrent reader failed: %v", workerErr)
	default:
	}

	finalIncremental, finalErr := salesLedger.BreakdownScoped("", "", scopeProf1, nil)
	if finalErr != nil {
		testingContext.Fatalf("final BreakdownScoped: %v", finalErr)
	}
	expectedFullLedger, fullBuildErr := replayreal.BuildLedger(databaseConnection, true)
	if fullBuildErr != nil {
		testingContext.Fatalf("full BuildLedger: %v", fullBuildErr)
	}
	if expectedBreakdown := expectedFullLedger.BreakdownForScope("", "", scopeProf1, nil); finalIncremental != expectedBreakdown {
		testingContext.Fatalf("concurrent incremental parity mismatch: got=%+v want=%+v", finalIncremental, expectedBreakdown)
	}
}
