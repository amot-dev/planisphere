package realization_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"planisphere/internal/replay"
	"planisphere/internal/replay/realization"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

type recordingQueryer struct {
	inner            *sql.DB
	fullEventQueries int
	tailEventQueries int
}

func (recorder *recordingQueryer) Exec(query string, args ...any) (sql.Result, error) {
	return recorder.inner.Exec(query, args...)
}

func (recorder *recordingQueryer) Query(query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "FROM journal_events") {
		if strings.Contains(query, "WHERE id > ?") {
			recorder.tailEventQueries++
		} else if strings.Contains(query, "timestamp > ?") {
			recorder.fullEventQueries++
		}
	}
	return recorder.inner.Query(query, args...)
}

func (recorder *recordingQueryer) QueryRow(query string, args ...any) *sql.Row {
	return recorder.inner.QueryRow(query, args...)
}

func (recorder *recordingQueryer) Prepare(query string) (*sql.Stmt, error) {
	return recorder.inner.Prepare(query)
}

func assertLedgerParity(testingContext *testing.T, databaseConnection *sql.DB, incrementalLedger *realization.Ledger, stepLabel string) {
	testingContext.Helper()
	expectedLedger := mustBuildLedger(testingContext, databaseConnection)
	for _, window := range [][2]string{
		{"", ""},
		{"2026-05-01T09:59:00Z", "2026-05-01T10:30:00Z"},
		{"2026-05-01T10:30:00Z", "2026-05-01T12:00:00Z"},
	} {
		gotBreakdown := incrementalLedger.Breakdown(window[0], window[1], nil)
		wantBreakdown := expectedLedger.Breakdown(window[0], window[1], nil)
		if gotBreakdown != wantBreakdown {
			testingContext.Fatalf("%s window (%s..%s): incremental=%+v want=%+v", stepLabel, window[0], window[1], gotBreakdown, wantBreakdown)
		}
	}
}

func seedCommanderProfile(testingContext *testing.T, databaseConnection *sql.DB, profileID int64, displayName, frontierID string) {
	testingContext.Helper()
	profileKey := fmt.Sprintf("live:%s:era:0", strings.ToLower(frontierID))
	if _, execError := databaseConnection.Exec(
		`INSERT INTO commander_profiles (id, profile_key, display_name, fid, universe, era_index, is_archived_career, enabled) VALUES (?, ?, ?, ?, 'live', 0, 0, 1)`,
		profileID, profileKey, displayName, frontierID,
	); execError != nil {
		testingContext.Fatalf("seed commander_profiles %s: %v", displayName, execError)
	}
}

func TestLedgerStateIncrementalLiveLifecycleAndSnapshotImmutability(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	trackedQueryer := &recordingQueryer{inner: databaseConnection}
	seedCommanderProfile(testingContext, databaseConnection, 1, "CmdrLive", "F100")

	// Seed baseline archive event before live play starts.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T09:00:00Z","event":"LoadGame","Commander":"CmdrLive","FID":"F100","Odyssey":true}`,
		`{"timestamp":"2026-05-01T09:05:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"SeedSys","SystemAddress":4100,"BodyID":1,"BodyName":"SeedSys 1","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":true,"WasMapped":true}`,
	)

	ledgerState, buildErr := realization.BuildLedgerState(trackedQueryer, true)
	if buildErr != nil {
		testingContext.Fatalf("BuildLedgerState: %v", buildErr)
	}
	if trackedQueryer.fullEventQueries != 1 {
		testingContext.Fatalf("expected 1 initial full query, got %d", trackedQueryer.fullEventQueries)
	}
	snapshotSeed := ledgerState.Ledger()
	seedBreakdown := snapshotSeed.Breakdown("", "", nil)

	advanceAndVerify := func(stepLabel string) *realization.Ledger {
		testingContext.Helper()
		var advanceErr error
		ledgerState, advanceErr = ledgerState.Advance(trackedQueryer)
		if advanceErr != nil {
			testingContext.Fatalf("%s Advance failed: %v", stepLabel, advanceErr)
		}
		if trackedQueryer.fullEventQueries != 1 {
			testingContext.Fatalf("%s unexpectedly fell back to full rebuild (fullEventQueries=%d)", stepLabel, trackedQueryer.fullEventQueries)
		}
		currentSnapshot := ledgerState.Ledger()
		assertLedgerParity(testingContext, databaseConnection, currentSnapshot, stepLabel)
		return currentSnapshot
	}

	// Live step: FSDJump (session context) followed by undiscovered Earthlike Scan.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:00:00Z","event":"FSDJump","StarSystem":"LiveAlpha","SystemAddress":4201,"StarPos":[100,0,0],"Population":0}`,
		`{"timestamp":"2026-05-01T10:02:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"LiveAlpha","SystemAddress":4201,"BodyID":1,"BodyName":"LiveAlpha 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
	)
	snapshotAfterScan := advanceAndVerify("FSDJump+Scan")
	afterScanBreakdown := snapshotAfterScan.Breakdown("", "", nil)

	// Live step: DSS mapping mutating the already-published LiveAlpha 1 scanRecord.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:05:00Z","event":"SAAScanComplete","SystemAddress":4201,"BodyID":1,"BodyName":"LiveAlpha 1","ProbesUsed":5,"EfficiencyTarget":6}`,
	)
	snapshotAfterMapping := advanceAndVerify("SAAScanComplete")
	afterMappingBreakdown := snapshotAfterMapping.Breakdown("", "", nil)
	if afterMappingBreakdown.PendingValue <= afterScanBreakdown.PendingValue {
		testingContext.Fatalf("expected mapping to increase PendingValue (%d -> %d)", afterScanBreakdown.PendingValue, afterMappingBreakdown.PendingValue)
	}

	// Live step: ScanOrganic on un-footfalled LiveAlpha 1.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":4201,"Body":1}`,
	)
	snapshotAfterOrganicScan := advanceAndVerify("ScanOrganic")
	afterOrganicScanBreakdown := snapshotAfterOrganicScan.Breakdown("", "", nil)

	var firstOrganicUID string
	if err := databaseConnection.QueryRow(`SELECT event_uid FROM journal_events WHERE event = 'ScanOrganic' ORDER BY id LIMIT 1`).Scan(&firstOrganicUID); err != nil {
		testingContext.Fatalf("query ScanOrganic UID: %v", err)
	}

	// Live step: Dock on Fleet Carrier and sell cartographics with NPC crew wage in same batch.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:20:00Z","event":"Docked","StationName":"Q1W-2E3","StationType":"FleetCarrier","SystemAddress":4201}`,
		`{"timestamp":"2026-05-01T10:21:00Z","event":"SellExplorationData","Systems":["SeedSys","LiveAlpha"],"Discovered":["LiveAlpha 1"],"BaseValue":2000000,"Bonus":1000000,"TotalEarnings":2050000}`,
		`{"timestamp":"2026-05-01T10:21:01Z","event":"NpcCrewPaidWage","NpcCrewId":7,"NpcCrewName":"CrewMate","Amount":200000}`,
	)
	snapshotAfterCartoSale := advanceAndVerify("DockedCarrier+SellExplorationData+NpcCrewPaidWage")
	afterCartoSaleBreakdown := snapshotAfterCartoSale.Breakdown("", "", nil)
	if afterCartoSaleBreakdown.CarrierTariffLoss <= 0 || afterCartoSaleBreakdown.NpcCrewLoss != 200000 {
		testingContext.Fatalf("expected positive carrier tariff and 200000 crew loss, got %+v", afterCartoSaleBreakdown)
	}

	// Live step: SellOrganicData settling firstOrganicUID.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:25:00Z","event":"SellOrganicData","MarketID":9001,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":76043200}]}`,
	)
	snapshotAfterOrganicSale := advanceAndVerify("SellOrganicData")
	saleOutcome, outcomeFound := snapshotAfterOrganicSale.OrganicSaleDetails(firstOrganicUID)
	if !outcomeFound || !saleOutcome.BonusRealized || saleOutcome.SaleMarketID != 9001 {
		testingContext.Fatalf("expected settled OrganicSaleDetails on new snapshot, got outcome=%+v found=%v", saleOutcome, outcomeFound)
	}

	// Live step: Undock, scan new carto + organic data, then lose both on ship-destruction Resurrect.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-01T10:40:00Z","event":"Undocked","StationName":"Q1W-2E3","StationType":"FleetCarrier"}`,
		`{"timestamp":"2026-05-01T10:45:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DoomSys","SystemAddress":4202,"BodyID":1,"BodyName":"DoomSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-05-01T10:50:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Bacterial","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","WasLogged":false,"SystemAddress":4202,"Body":1}`,
		`{"timestamp":"2026-05-01T11:00:00Z","event":"Resurrect","Option":"rebuy","Cost":1500000,"Bankrupt":false}`,
	)
	snapshotAfterResurrect := advanceAndVerify("Undocked+Scans+Resurrect")
	finalBreakdown := snapshotAfterResurrect.Breakdown("", "", nil)
	if finalBreakdown.CartoLostDeath <= 0 || finalBreakdown.OrganicLostDeath <= 0 || finalBreakdown.PendingValue != 0 {
		testingContext.Fatalf("expected death losses and zero pending after rebuy, got %+v", finalBreakdown)
	}

	// Verify earlier published snapshots remained completely immutable across all subsequent COW mutations.
	if snapshotSeed.Breakdown("", "", nil) != seedBreakdown {
		testingContext.Fatalf("seed snapshot mutated after subsequent advances")
	}
	if snapshotAfterScan.Breakdown("", "", nil) != afterScanBreakdown {
		testingContext.Fatalf("after-scan snapshot mutated by SAAScanComplete or SellExplorationData")
	}
	if snapshotAfterMapping.Breakdown("", "", nil) != afterMappingBreakdown {
		testingContext.Fatalf("after-mapping snapshot mutated by SellExplorationData")
	}
	if snapshotAfterOrganicScan.Breakdown("", "", nil) != afterOrganicScanBreakdown {
		testingContext.Fatalf("after-organic-scan snapshot mutated by sales")
	}
	if _, foundOnPrior := snapshotAfterCartoSale.OrganicSaleDetails(firstOrganicUID); foundOnPrior {
		testingContext.Fatalf("prior snapshot organicSales map was mutated by later SellOrganicData")
	}
}

func TestLedgerStateFallbacksOnBackfillAndSplitCrewWage(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	trackedQueryer := &recordingQueryer{inner: databaseConnection}

	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T12:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"WageSys","SystemAddress":5100,"BodyID":1,"BodyName":"WageSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`,
		`{"timestamp":"2026-05-02T12:10:00Z","event":"SellExplorationData","Systems":["WageSys"],"Discovered":[],"BaseValue":500000,"Bonus":0,"TotalEarnings":450000}`,
	)

	ledgerState, buildErr := realization.BuildLedgerState(trackedQueryer, true)
	if buildErr != nil {
		testingContext.Fatalf("BuildLedgerState: %v", buildErr)
	}

	// Split-flush NpcCrewPaidWage arriving in the next watcher batch without a sale in that batch must rebuild.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T12:10:01Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"PilotA","Amount":50000}`,
	)
	fullBeforeSplitWage := trackedQueryer.fullEventQueries
	var wageAdvanceErr error
	ledgerState, wageAdvanceErr = ledgerState.Advance(trackedQueryer)
	if wageAdvanceErr != nil {
		testingContext.Fatalf("Advance on split NpcCrewPaidWage: %v", wageAdvanceErr)
	}
	if trackedQueryer.fullEventQueries != fullBeforeSplitWage+1 {
		testingContext.Fatalf("expected full rebuild on split NpcCrewPaidWage, fullEventQueries %d -> %d", fullBeforeSplitWage, trackedQueryer.fullEventQueries)
	}
	if got, want := ledgerState.Ledger().Breakdown("", "", nil), mustBuildLedger(testingContext, databaseConnection).Breakdown("", "", nil); got != want || got.NpcCrewLoss != 50000 {
		testingContext.Fatalf("split NpcCrewPaidWage parity mismatch: got=%+v want=%+v", got, want)
	}

	// Split-flush NpcCrewPaidWage arriving alongside a second sale within 5 seconds of a prior batch's sale must also rebuild.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T13:00:00Z","event":"SellExplorationData","Systems":["UnattribSys"],"Discovered":[],"BaseValue":300000,"Bonus":0,"TotalEarnings":270000}`,
	)
	ledgerState, wageAdvanceErr = ledgerState.Advance(trackedQueryer)
	if wageAdvanceErr != nil {
		testingContext.Fatalf("Advance prior to paired split wage: %v", wageAdvanceErr)
	}
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-05-02T13:00:00Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"PilotA","Amount":30000}`,
		`{"timestamp":"2026-05-02T13:00:03Z","event":"SellExplorationData","Systems":["SecondSys"],"Discovered":[],"BaseValue":100000,"Bonus":0,"TotalEarnings":100000}`,
	)
	fullBeforeNearPriorSale := trackedQueryer.fullEventQueries
	ledgerState, wageAdvanceErr = ledgerState.Advance(trackedQueryer)
	if wageAdvanceErr != nil {
		testingContext.Fatalf("Advance on near-prior-sale NpcCrewPaidWage: %v", wageAdvanceErr)
	}
	if trackedQueryer.fullEventQueries != fullBeforeNearPriorSale+1 {
		testingContext.Fatalf("expected full rebuild when batch wage falls within 5s of prior sale, fullEventQueries %d -> %d", fullBeforeNearPriorSale, trackedQueryer.fullEventQueries)
	}
	if got, want := ledgerState.Ledger().Breakdown("2026-05-02T12:59:00Z", "2026-05-02T13:00:01Z", nil),
		mustBuildLedger(testingContext, databaseConnection).Breakdown("2026-05-02T12:59:00Z", "2026-05-02T13:00:01Z", nil); got != want || got.NpcCrewLoss != 30000 {
		testingContext.Fatalf("near-prior-sale wage attribution mismatch: got=%+v want=%+v", got, want)
	}

	// Out-of-order historical backfill (timestamp < lastTimestamp) must fall back to full rebuild so binary search stays sorted.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2025-01-15T08:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"BackfillSys","SystemAddress":5200,"BodyID":1,"BodyName":"BackfillSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)
	fullBeforeBackfill := trackedQueryer.fullEventQueries
	var backfillAdvanceErr error
	ledgerState, backfillAdvanceErr = ledgerState.Advance(trackedQueryer)
	if backfillAdvanceErr != nil {
		testingContext.Fatalf("Advance on historical backfill: %v", backfillAdvanceErr)
	}
	if trackedQueryer.fullEventQueries != fullBeforeBackfill+1 {
		testingContext.Fatalf("expected full rebuild on historical backfill, fullEventQueries %d -> %d", fullBeforeBackfill, trackedQueryer.fullEventQueries)
	}
	if got, want := ledgerState.Ledger().Breakdown("2025-01-01T00:00:00Z", "2025-02-01T00:00:00Z", nil),
		mustBuildLedger(testingContext, databaseConnection).Breakdown("2025-01-01T00:00:00Z", "2025-02-01T00:00:00Z", nil); got != want || got.PendingValue <= 0 {
		testingContext.Fatalf("historical backfill window mismatch: got=%+v want=%+v", got, want)
	}
}

func TestLedgerStateMultiPageCopyOnWriteIsolation(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	trackedQueryer := &recordingQueryer{inner: databaseConnection}
	seedCommanderProfile(testingContext, databaseConnection, 1, "CmdrPaged", "F200")

	baseTime := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	tsAt := func(offsetSeconds int) string {
		return baseTime.Add(time.Duration(offsetSeconds) * time.Second).Format(time.RFC3339)
	}
	seedLines := make([]string, 0, 1028)
	seedLines = append(seedLines,
		`{"timestamp":"2026-06-01T00:00:00Z","event":"LoadGame","Commander":"CmdrPaged","FID":"F200","Odyssey":true}`,
		`{"timestamp":"2026-06-01T00:00:01Z","event":"FSDJump","StarSystem":"PageZeroSys","SystemAddress":9100,"StarPos":[10,0,0],"Population":0}`,
	)
	for bodyNumber := 1; bodyNumber <= 1024; bodyNumber++ {
		seedLines = append(seedLines, fmt.Sprintf(`{"timestamp":%q,"event":"Scan","ScanType":"Detailed","StarSystem":"PageZeroSys","SystemAddress":9100,"BodyID":%d,"BodyName":"PageZeroSys %d","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false}`, tsAt(bodyNumber+1), bodyNumber, bodyNumber))
	}
	pageOneStartTimestamp := tsAt(1100)
	seedLines = append(seedLines,
		fmt.Sprintf(`{"timestamp":%q,"event":"FSDJump","StarSystem":"PageOneSys","SystemAddress":9101,"StarPos":[20,0,0],"Population":0}`, pageOneStartTimestamp),
		fmt.Sprintf(`{"timestamp":%q,"event":"Scan","ScanType":"Detailed","StarSystem":"PageOneSys","SystemAddress":9101,"BodyID":1,"BodyName":"PageOneSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`, tsAt(1101)),
	)
	journaltest.InsertParsedLines(testingContext, databaseConnection, seedLines...)

	ledgerState, buildError := realization.BuildLedgerState(trackedQueryer, true)
	if buildError != nil {
		testingContext.Fatalf("BuildLedgerState: %v", buildError)
	}
	snapshotInitial := ledgerState.Ledger()
	initialTotal := snapshotInitial.Breakdown("", "", nil)
	initialPageOne := snapshotInitial.Breakdown(pageOneStartTimestamp, "", nil)

	advanceIncremental := func(stepLabel, rawLine string) (*realization.Ledger, realization.Breakdown) {
		testingContext.Helper()
		journaltest.InsertParsedLines(testingContext, databaseConnection, rawLine)
		var advanceError error
		ledgerState, advanceError = ledgerState.Advance(trackedQueryer)
		if advanceError != nil || trackedQueryer.fullEventQueries != 1 {
			testingContext.Fatalf("%s Advance: err=%v fullEventQueries=%d", stepLabel, advanceError, trackedQueryer.fullEventQueries)
		}
		currentSnapshot := ledgerState.Ledger()
		expectedLedger := mustBuildLedger(testingContext, databaseConnection)
		for _, afterBound := range []string{"", pageOneStartTimestamp} {
			if got, want := currentSnapshot.Breakdown(afterBound, "", nil), expectedLedger.Breakdown(afterBound, "", nil); got != want {
				testingContext.Fatalf("%s after=%q: got=%+v want=%+v", stepLabel, afterBound, got, want)
			}
		}
		return currentSnapshot, currentSnapshot.Breakdown("", "", nil)
	}

	// Consecutive single-event live Scan appends onto partially filled page 1, pre-watermark page-0 mapping, and cross-page sale.
	snapshotScanOne, scanOneTotal := advanceIncremental("live-scan-1", fmt.Sprintf(`{"timestamp":%q,"event":"Scan","ScanType":"Detailed","StarSystem":"PageOneSys","SystemAddress":9101,"BodyID":2,"BodyName":"PageOneSys 2","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`, tsAt(1105)))
	snapshotScanTwo, scanTwoTotal := advanceIncremental("live-scan-2", fmt.Sprintf(`{"timestamp":%q,"event":"Scan","ScanType":"Detailed","StarSystem":"PageOneSys","SystemAddress":9101,"BodyID":3,"BodyName":"PageOneSys 3","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`, tsAt(1110)))
	snapshotAfterMap, afterMapTotal := advanceIncremental("pre-watermark-map", fmt.Sprintf(`{"timestamp":%q,"event":"SAAScanComplete","SystemAddress":9100,"BodyID":1,"BodyName":"PageZeroSys 1","ProbesUsed":3,"EfficiencyTarget":4}`, tsAt(1120)))
	_, afterSaleTotal := advanceIncremental("cross-page-sale", fmt.Sprintf(`{"timestamp":%q,"event":"SellExplorationData","Systems":["PageZeroSys","PageOneSys"],"Discovered":["PageZeroSys 1","PageOneSys 1","PageOneSys 2","PageOneSys 3"],"BaseValue":5000000,"Bonus":2000000,"TotalEarnings":7000000}`, tsAt(1200)))
	if afterSaleTotal.PendingValue != 0 || afterSaleTotal.TotalRealized != 7000000 {
		testingContext.Fatalf("expected all cross-page records settled for 7000000, got %+v", afterSaleTotal)
	}

	if snapshotInitial.Breakdown("", "", nil) != initialTotal || snapshotInitial.Breakdown(pageOneStartTimestamp, "", nil) != initialPageOne {
		testingContext.Fatalf("initial multi-page snapshot mutated by subsequent advances")
	}
	if snapshotScanOne.Breakdown("", "", nil) != scanOneTotal || snapshotScanTwo.Breakdown("", "", nil) != scanTwoTotal || snapshotAfterMap.Breakdown("", "", nil) != afterMapTotal {
		testingContext.Fatalf("prior incremental snapshots mutated across page copy-on-write updates")
	}
}

func TestLedgerStateHealsMidScanDirUnresolvedProfileIDs(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	trackedQueryer := &recordingQueryer{inner: databaseConnection}
	seedCommanderProfile(testingContext, databaseConnection, 1, "CmdrScanDir", "F900")
	scopeProfileOne := replay.NewProfileScope([]int64{1})

	// Seed an initial resolved scan, then append a mid-ScanDir row with profile_id = 0 before ResolveCommanders runs.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-07-01T10:00:00Z","event":"LoadGame","Commander":"CmdrScanDir","FID":"F900","Odyssey":true}`,
		`{"timestamp":"2026-07-01T10:01:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"ResolvedSys","SystemAddress":9500,"BodyID":1,"BodyName":"ResolvedSys 1","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":true,"WasMapped":true}`,
	)
	ledgerState, buildError := realization.BuildLedgerState(trackedQueryer, true)
	if buildError != nil {
		testingContext.Fatalf("BuildLedgerState: %v", buildError)
	}
	baselinePending := ledgerState.Ledger().BreakdownForScope("", "", scopeProfileOne, nil).PendingValue
	if baselinePending <= 0 {
		testingContext.Fatalf("expected positive baseline PendingValue for profile 1, got %d", baselinePending)
	}

	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, commander, fid, universe, profile_id, raw_json, ingested_at) VALUES (?, ?, 'Scan', 'CmdrScanDir', 'F900', 'live', 0, ?, ?)`,
		"mid-scandir-scan-1",
		"2026-07-01T10:05:00Z",
		`{"timestamp":"2026-07-01T10:05:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"MidScanSys","SystemAddress":9501,"BodyID":1,"BodyName":"MidScanSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		"2026-07-01T10:05:00Z",
	); execError != nil {
		testingContext.Fatalf("insert unresolved mid-ScanDir Scan: %v", execError)
	}

	// Both an incremental Advance and a cold BuildLedgerState mid-ScanDir record hasUnresolvedProfiles.
	var midScanError error
	ledgerState, midScanError = ledgerState.Advance(trackedQueryer)
	if midScanError != nil {
		testingContext.Fatalf("mid-ScanDir Advance: %v", midScanError)
	}
	coldMidScanState, coldBuildError := realization.BuildLedgerState(databaseConnection, true)
	if coldBuildError != nil {
		testingContext.Fatalf("cold mid-ScanDir BuildLedgerState: %v", coldBuildError)
	}
	if gotScoped := ledgerState.Ledger().BreakdownForScope("", "", scopeProfileOne, nil).PendingValue; gotScoped != baselinePending {
		testingContext.Fatalf("expected unresolved profile_id=0 row excluded from explicit scope before stamp, got %d want %d", gotScoped, baselinePending)
	}

	// Simulate ScanDir completion stamping profile_id = 1 without inserting new rows after lastEventID.
	if _, execError := databaseConnection.Exec(`UPDATE journal_events SET profile_id = 1 WHERE event_uid = 'mid-scandir-scan-1'`); execError != nil {
		testingContext.Fatalf("stamp resolved profile_id: %v", execError)
	}
	fullBeforeHeal := trackedQueryer.fullEventQueries
	var healError error
	ledgerState, healError = ledgerState.Advance(trackedQueryer)
	if healError != nil || trackedQueryer.fullEventQueries != fullBeforeHeal+1 {
		testingContext.Fatalf("expected Advance to heal unresolved profile_id via BuildLedgerState, fullEventQueries %d -> %d err=%v", fullBeforeHeal, trackedQueryer.fullEventQueries, healError)
	}
	healedColdState, coldHealError := coldMidScanState.Advance(databaseConnection)
	if coldHealError != nil {
		testingContext.Fatalf("cold mid-ScanDir healing Advance: %v", coldHealError)
	}
	wantScoped := mustBuildLedger(testingContext, databaseConnection).BreakdownForScope("", "", scopeProfileOne, nil)
	if gotIncremental, gotCold := ledgerState.Ledger().BreakdownForScope("", "", scopeProfileOne, nil), healedColdState.Ledger().BreakdownForScope("", "", scopeProfileOne, nil); gotIncremental != wantScoped || gotCold != wantScoped || wantScoped.PendingValue <= baselinePending {
		testingContext.Fatalf("expected healed scoped breakdowns %+v > baseline %d, got inc=%+v cold=%+v", wantScoped, baselinePending, gotIncremental, gotCold)
	}

	// Once healed, subsequent resolved live appends resume incremental tail advances without rebuilding.
	journaltest.InsertParsedLines(testingContext, databaseConnection, `{"timestamp":"2026-07-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"PostHealSys","SystemAddress":9502,"BodyID":1,"BodyName":"PostHealSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	fullAfterHeal := trackedQueryer.fullEventQueries
	ledgerState, healError = ledgerState.Advance(trackedQueryer)
	if healError != nil || trackedQueryer.fullEventQueries != fullAfterHeal {
		testingContext.Fatalf("expected post-heal live append to stay incremental, fullEventQueries %d -> %d err=%v", fullAfterHeal, trackedQueryer.fullEventQueries, healError)
	}
}
