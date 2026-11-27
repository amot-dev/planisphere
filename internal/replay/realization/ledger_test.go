package realization_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/replay/realization"
	"planisphere/internal/sources/journal"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func insertRawUIDEvent(testingContext *testing.T, databaseConnection *sql.DB, timestamp, eventName, rawJSON string) {
	testingContext.Helper()
	eventUID := journal.ComputeUID(timestamp, eventName, rawJSON)
	if _, execError := databaseConnection.Exec(
		`INSERT INTO journal_events(event_uid, timestamp, event, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, rawJSON, timestamp,
	); execError != nil {
		testingContext.Fatalf("insert raw-UID %s event: %v", eventName, execError)
	}
}

func mustBuildLedger(testingContext *testing.T, databaseConnection *sql.DB) *realization.Ledger {
	testingContext.Helper()
	ledger, buildError := realization.BuildLedger(databaseConnection, true)
	if buildError != nil {
		testingContext.Fatalf("BuildLedger failed: %v", buildError)
	}
	return ledger
}

func TestRealizationNpcCrewCut(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)

	// Scan two samples sold in rapid consecutive transactions within 5 seconds, with a 10% NPC crew cut on the first sale only.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":111,"Body":1}`,
		`{"timestamp":"2026-03-01T10:05:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Bacterial","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","WasLogged":true,"SystemAddress":111,"Body":2}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"NpcCrewPaidWage","NpcCrewId":99,"NpcCrewName":"Piloteer","Amount":1901080}`,
		`{"timestamp":"2026-03-01T12:00:02Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`,
	)

	realizationBreakdown := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)

	// Net Credits Pocketed = (19,010,800 - 1,901,080) + 1,000,000 = 18,109,720.
	expectedNetRealized := int64(19010800 - 1901080 + 1000000)
	if realizationBreakdown.TotalRealized != expectedNetRealized {
		t.Errorf("expected net TotalRealized %d, got %d", expectedNetRealized, realizationBreakdown.TotalRealized)
	}
	if realizationBreakdown.NpcCrewLoss != 1901080 {
		t.Errorf("expected NpcCrewLoss 1,901,080 (consumed once), got %d", realizationBreakdown.NpcCrewLoss)
	}
}

func TestRealizationCartoLostDeathAndSold(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)

	// Ship destruction via insurance rebuy, free Sidewinder, or bankruptcy each loses pending cartography.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Sol","SystemAddress":1234,"BodyID":1,"BodyName":"Sol A 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T11:00:05Z","event":"Resurrect","Option":"rebuy","Cost":2500000,"Bankrupt":false}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Achenar","SystemAddress":2345,"BodyID":1,"BodyName":"Achenar 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T13:00:05Z","event":"Resurrect","Option":"freewinder","Cost":0,"Bankrupt":false}`,
		`{"timestamp":"2026-03-01T14:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alioth","SystemAddress":3456,"BodyID":1,"BodyName":"Alioth 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T15:00:05Z","event":"Resurrect","Option":"free","Cost":0,"Bankrupt":true}`,
	)

	ledger := mustBuildLedger(t, databaseConnection)
	for _, scenario := range []struct {
		label string
		after string
		until string
	}{
		{label: "insurance rebuy", after: "2026-03-01T09:00:00Z", until: "2026-03-01T10:30:00Z"},
		{label: "freewinder replacement", after: "2026-03-01T11:30:00Z", until: "2026-03-01T12:30:00Z"},
		{label: "bankruptcy replacement", after: "2026-03-01T13:30:00Z", until: "2026-03-01T14:30:00Z"},
	} {
		realizationBreakdown := ledger.Breakdown(scenario.after, scenario.until, nil)
		if realizationBreakdown.CartoLostDeath <= 0 {
			t.Errorf("%s: expected CartoLostDeath > 0, got %d", scenario.label, realizationBreakdown.CartoLostDeath)
		}
		if realizationBreakdown.TotalRealized != 0 {
			t.Errorf("%s: expected TotalRealized 0, got %d", scenario.label, realizationBreakdown.TotalRealized)
		}
	}
}

func TestRealizationOnFootDeathPreservesCarto(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)

	// On-foot free suit redeploy and detention-center hand-in both preserve shipboard cartography while losing organic data.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Sol","SystemAddress":1234,"BodyID":1,"BodyName":"Sol A 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-03-01T10:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1234,"Body":1}`,
		`{"timestamp":"2026-03-01T11:00:05Z","event":"Resurrect","Option":"free","Cost":0,"Bankrupt":false}`,
		`{"timestamp":"2026-03-01T11:30:05Z","event":"Resurrect","Option":"handin","Cost":5000,"Bankrupt":false}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":500000,"TotalEarnings":1500000,"Discovered":[{"SystemName":"Sol","NumBodies":1}]}`,
	)

	realizationBreakdown := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:00:00Z", "2026-03-01T10:45:00Z", nil)

	if realizationBreakdown.CartoLostDeath != 0 {
		t.Errorf("expected CartoLostDeath 0 for on-foot death, got %d", realizationBreakdown.CartoLostDeath)
	}
	if realizationBreakdown.OrganicLostDeath != 95054000 {
		t.Errorf("expected OrganicLostDeath 95,054,000, got %d", realizationBreakdown.OrganicLostDeath)
	}
	if realizationBreakdown.TotalRealized != 1500000 {
		t.Errorf("expected TotalRealized 1,500,000, got %d", realizationBreakdown.TotalRealized)
	}
}

func TestRealizationMultiSellMultiPageAndStolen(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)

	// Scan three undiscovered systems: Alpha and Beta sold via MultiSellExplorationData,
	// Gamma sold individually via SellExplorationData where someone else claimed discovery first.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alpha","SystemAddress":1001,"BodyID":1,"BodyName":"Alpha 1","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T10:15:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Beta","SystemAddress":1002,"BodyID":1,"BodyName":"Beta 1","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T10:20:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Gamma","SystemAddress":1003,"BodyID":1,"BodyName":"Gamma 1","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":0,"TotalEarnings":1000000,"Discovered":[{"SystemName":"Alpha","NumBodies":1}]}`,
	)

	afterFirstPageBreakdown := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:00:00Z", "2026-03-01T10:30:00Z", nil)
	if afterFirstPageBreakdown.TotalRealized != 1000000 {
		t.Errorf("expected TotalRealized 1,000,000 after first page, got %d", afterFirstPageBreakdown.TotalRealized)
	}
	if afterFirstPageBreakdown.CartoStolen != 0 {
		t.Errorf("expected CartoStolen 0 on MultiSellExplorationData with Bonus 0, got %d", afterFirstPageBreakdown.CartoStolen)
	}
	if afterFirstPageBreakdown.PendingValue != 39305*2 {
		t.Errorf("expected PendingValue %d for unsold Beta and Gamma scans, got %d", 39305*2, afterFirstPageBreakdown.PendingValue)
	}

	// Bulk Page 2 realizes Beta, and individual SellExplorationData sells Gamma with empty Discovered (stolen bonus).
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-08T12:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":500000,"TotalEarnings":1500000,"Discovered":[{"SystemName":"Beta","NumBodies":1}]}`,
		`{"timestamp":"2026-03-08T12:05:00Z","event":"SellExplorationData","Systems":["Gamma"],"Discovered":[],"BaseValue":15117,"Bonus":0,"TotalEarnings":15117}`,
	)

	afterSecondPageBreakdown := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:00:00Z", "2026-03-01T10:30:00Z", nil)
	if afterSecondPageBreakdown.TotalRealized != 2515117 {
		t.Errorf("expected combined TotalRealized 2,515,117 across sales, got %d", afterSecondPageBreakdown.TotalRealized)
	}
	if afterSecondPageBreakdown.CartoStolen != 24188 {
		t.Errorf("expected CartoStolen 24,188 from Gamma SellExplorationData, got %d", afterSecondPageBreakdown.CartoStolen)
	}
	if afterSecondPageBreakdown.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 after all systems sold, got %d", afterSecondPageBreakdown.PendingValue)
	}
}

func TestRealizationSameTimestampDistinctSalesPreserved(t *testing.T) {
	// Verify duplicate whitespace-varied sales with legacy un-canonicalized UIDs are ignored while genuinely distinct sales sharing the same 1-second timestamp are both preserved
	databaseConnection := dbtest.OpenMemory(t)
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Bacterial","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","WasLogged":true,"SystemAddress":111,"Body":1}`,
		`{"timestamp":"2026-03-01T10:05:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":111,"Body":1}`,
	)

	insertRawUIDEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`)
	insertRawUIDEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{ "timestamp":"2026-03-01T12:00:00Z", "event":"SellOrganicData", "MarketID":123, "BioData":[ {"Species":"$Codex_Ent_Bacterial_01_Name;", "Value":1000000, "Bonus":0} ] }`)
	insertRawUIDEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	realizationBreakdown := mustBuildLedger(t, databaseConnection).Breakdown("", "", nil)
	if realizationBreakdown.TotalRealized != 20010800 {
		t.Errorf("expected TotalRealized 20,010,800 for two distinct sales at same timestamp with duplicate ignored, got %d", realizationBreakdown.TotalRealized)
	}
}

func TestRealizationFirstFootfallOnlyDeterminesFirstLoggedAndStolen(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)

	// Body 1: Already footfalled (WasFootfalled: true).
	// Even though ScanOrganic reports WasLogged: false (https://issues.frontierstore.net/issue-detail/81891),
	// selling with Bonus: 0 must NOT be flagged as stolen.
	// Body 2: First footfall (WasFootfalled: false), even if previously discovered from space (WasDiscovered: true).
	// Selling with Bonus: 0 MUST be flagged as stolen because WasFootfalled was false at time of scan.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T09:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Pegasi","SystemAddress":5001,"BodyID":1,"BodyName":"Pegasi 1","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Cactoid","Species":"$Codex_Ent_Cactoid_03_Name;","Species_Localised":"Cactoida Vermis","WasLogged":false,"SystemAddress":5001,"Body":1}`,
		`{"timestamp":"2026-03-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DeepSpace","SystemAddress":5002,"BodyID":2,"BodyName":"DeepSpace 2","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-03-01T10:20:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":5002,"Body":2}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[
			{"Species":"$Codex_Ent_Cactoid_03_Name;","Species_Localised":"Cactoida Vermis","Value":16202800,"Bonus":0},
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":0}
		]}`,
	)

	realizationBreakdown := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:00:00Z", "2026-03-01T11:00:00Z", nil)

	// Only Stratum Tectonicas (76,043,200 bonus) on un-footfalled Body 2 should be flagged stolen;
	// Cactoida Vermis on already-footfalled Body 1 must NOT be flagged stolen.
	if realizationBreakdown.OrganicStolen != 76043200 {
		t.Errorf("expected OrganicStolen 76,043,200 (only Body 2), got %d", realizationBreakdown.OrganicStolen)
	}
	if realizationBreakdown.OrganicLostDeath != 0 {
		t.Errorf("expected OrganicLostDeath 0, got %d", realizationBreakdown.OrganicLostDeath)
	}
}

func TestRealizationCartoDeduplicationSoldStateAndPostRebuyRescan(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)

	// Initial AutoScan of DedupeSys 1.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"AutoScan","StarSystem":"DedupeSys","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)

	afterInitialScan := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:50:00Z", "2026-03-01T10:30:00Z", nil)
	bodyScanOnlyValue := afterInitialScan.PendingValue
	if bodyScanOnlyValue <= 0 {
		t.Fatalf("expected positive PendingValue after initial scan, got %d", bodyScanOnlyValue)
	}

	// Map DedupeSys 1 with DSS and emit a duplicate SAAScanComplete plus a follow-up Detailed Scan.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:05:00Z","event":"SAAScanComplete","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","ProbesUsed":5,"EfficiencyTarget":6}`,
		`{"timestamp":"2026-03-01T10:06:00Z","event":"SAAScanComplete","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","ProbesUsed":5,"EfficiencyTarget":6}`,
		`{"timestamp":"2026-03-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DedupeSys","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)

	afterMappingAndDuplicateScan := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:50:00Z", "2026-03-01T10:30:00Z", nil)
	if afterMappingAndDuplicateScan.PendingValue <= bodyScanOnlyValue {
		t.Fatalf("expected PendingValue after DSS mapping (%d) to exceed scan-only value (%d) without being overwritten by follow-up Scan",
			afterMappingAndDuplicateScan.PendingValue, bodyScanOnlyValue)
	}

	// Selling DedupeSys must clear all pending value without leaving an orphaned pending record from the second Scan.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T11:00:00Z","event":"MultiSellExplorationData","BaseValue":1500000,"Bonus":500000,"TotalEarnings":2000000,"Discovered":[{"SystemName":"DedupeSys","NumBodies":1}]}`,
	)

	afterSale := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T09:50:00Z", "2026-03-01T10:30:00Z", nil)
	if afterSale.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 after selling DedupeSys, got %d", afterSale.PendingValue)
	}
	if afterSale.TotalRealized != 2000000 {
		t.Errorf("expected TotalRealized 2,000,000 after selling DedupeSys, got %d", afterSale.TotalRealized)
	}

	// Re-scanning or re-mapping an already-sold body later in the career must not create a new pending record.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"Scan","ScanType":"AutoScan","StarSystem":"DedupeSys","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`,
		`{"timestamp":"2026-03-01T12:05:00Z","event":"SAAScanComplete","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","ProbesUsed":5,"EfficiencyTarget":6}`,
	)

	afterSoldRescan := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T11:30:00Z", "2026-03-01T12:30:00Z", nil)
	if afterSoldRescan.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 when re-scanning an already-sold body, got %d", afterSoldRescan.PendingValue)
	}

	// Scanning a body, losing it to ship destruction, and then re-scanning it must create a new payable pending record.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T13:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RebuySys","SystemAddress":8002,"BodyID":1,"BodyName":"RebuySys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-03-01T13:30:00Z","event":"Resurrect","Option":"rebuy","Cost":1000000,"Bankrupt":false}`,
		`{"timestamp":"2026-03-01T14:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RebuySys","SystemAddress":8002,"BodyID":1,"BodyName":"RebuySys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
	)

	afterPostRebuyRescan := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T13:45:00Z", "2026-03-01T14:30:00Z", nil)
	if afterPostRebuyRescan.PendingValue != bodyScanOnlyValue {
		t.Errorf("expected PendingValue %d after re-scanning lost body post-rebuy, got %d", bodyScanOnlyValue, afterPostRebuyRescan.PendingValue)
	}

	// Selling the post-rebuy re-scan realizes the payout on the new record.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T15:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":500000,"TotalEarnings":1500000,"Discovered":[{"SystemName":"RebuySys","NumBodies":1}]}`,
	)

	afterPostRebuySale := mustBuildLedger(t, databaseConnection).Breakdown("2026-03-01T13:45:00Z", "2026-03-01T14:30:00Z", nil)
	if afterPostRebuySale.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 after selling post-rebuy rescan, got %d", afterPostRebuySale.PendingValue)
	}
	if afterPostRebuySale.TotalRealized != 1500000 {
		t.Errorf("expected TotalRealized 1,500,000 after selling post-rebuy rescan, got %d", afterPostRebuySale.TotalRealized)
	}
}

func TestRealizationCrewWageWindowAndBinarySearchBounds(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	// Record at exact After boundary (excluded), inside window (included), and after Until boundary (excluded),
	// plus NpcCrewPaidWage 5-second window boundaries around the 10:30:00Z sale.
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":9001,"Body":1}`,
		`{"timestamp":"2026-03-01T10:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":9001,"Body":2}`,
		`{"timestamp":"2026-03-01T10:30:01Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":9001,"Body":3}`,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"SellExplorationData","Systems":["BeforeSys"],"Discovered":[],"BaseValue":100000,"Bonus":0,"TotalEarnings":100000}`,
		`{"timestamp":"2026-03-01T10:29:54Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":99999}`,
		`{"timestamp":"2026-03-01T10:29:55Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":10000}`,
		`{"timestamp":"2026-03-01T10:30:00Z","event":"SellExplorationData","Systems":["InsideSys"],"Discovered":[],"BaseValue":500000,"Bonus":0,"TotalEarnings":500000}`,
		`{"timestamp":"2026-03-01T10:30:05Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":20000}`,
		`{"timestamp":"2026-03-01T10:30:06Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":88888}`,
		`{"timestamp":"2026-03-01T10:30:01Z","event":"SellExplorationData","Systems":["AfterSys"],"Discovered":[],"BaseValue":700000,"Bonus":0,"TotalEarnings":700000}`,
	)

	breakdown := mustBuildLedger(testingContext, databaseConnection).Breakdown("2026-03-01T10:00:00Z", "2026-03-01T10:30:00Z", nil)
	if breakdown.PendingValue != 19010800 {
		testingContext.Errorf("expected PendingValue 19,010,800 from single in-window organic scan, got %d", breakdown.PendingValue)
	}
	if breakdown.TotalRealized != 500000 {
		testingContext.Errorf("expected TotalRealized 500,000 from single in-window unattributed sale, got %d", breakdown.TotalRealized)
	}
	if breakdown.NpcCrewLoss != 30000 {
		testingContext.Errorf("expected NpcCrewLoss 30,000 from wage events within 5-second window, got %d", breakdown.NpcCrewLoss)
	}
}
