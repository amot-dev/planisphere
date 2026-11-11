package stats_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/db"
	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

func setupRealizationDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseConnection, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() {
		_ = databaseConnection.Close()
		stats.InvalidateLedgerCache()
	})
	return databaseConnection
}

func insertEvent(t *testing.T, databaseConnection *sql.DB, timestamp, event, rawJSON string) {
	t.Helper()
	eventUID := journal.ComputeUID(timestamp, event, rawJSON)
	_, err := databaseConnection.Exec(`INSERT INTO journal_events(event_uid, timestamp, event, raw_json, ingested_at)
		VALUES (?, ?, ?, ?, ?)`, eventUID, timestamp, event, rawJSON, timestamp)
	if err != nil {
		t.Fatalf("insert %s event: %v", event, err)
	}
}

func TestRealizationNpcCrewCut(t *testing.T) {
	databaseConnection := setupRealizationDB(t)

	// Scan
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":111,"Body":1}`)

	// Sale for 19,010,800 credits
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	// Concurrent NPC crew cut: 1,901,080 (10%)
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "NpcCrewPaidWage",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"NpcCrewPaidWage","NpcCrewId":99,"NpcCrewName":"Piloteer","Amount":1901080}`)

	engine := stats.NewEngine(databaseConnection)
	realizationBreakdown, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	if realizationBreakdown.TotalRealized != 19010800 {
		t.Errorf("expected TotalRealized 19,010,800, got %d", realizationBreakdown.TotalRealized)
	}
	if realizationBreakdown.NpcCrewLoss != 1901080 {
		t.Errorf("expected NpcCrewLoss 1,901,080, got %d", realizationBreakdown.NpcCrewLoss)
	}
}

func TestRealizationCartoLostDeathAndSold(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Ship destruction with insurance rebuy (Option "rebuy", Cost > 0) loses pending cartography.
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Sol","SystemAddress":1234,"BodyID":1,"BodyName":"Sol A 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T11:00:05Z", "Resurrect",
		`{"timestamp":"2026-03-01T11:00:05Z","event":"Resurrect","Option":"rebuy","Cost":2500000,"Bankrupt":false}`)

	// Taking a free Sidewinder on ship destruction ("freewinder" with Cost 0) loses pending cartography.
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Achenar","SystemAddress":2345,"BodyID":1,"BodyName":"Achenar 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T13:00:05Z", "Resurrect",
		`{"timestamp":"2026-03-01T13:00:05Z","event":"Resurrect","Option":"freewinder","Cost":0,"Bankrupt":false}`)

	// Bankruptcy into a free Sidewinder (Option "free", Cost 0, Bankrupt true) also loses pending cartography.
	insertEvent(t, databaseConnection, "2026-03-01T14:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T14:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alioth","SystemAddress":3456,"BodyID":1,"BodyName":"Alioth 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T15:00:05Z", "Resurrect",
		`{"timestamp":"2026-03-01T15:00:05Z","event":"Resurrect","Option":"free","Cost":0,"Bankrupt":true}`)

	for _, scenario := range []struct {
		label  string
		window stats.Range
	}{
		{label: "insurance rebuy", window: stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T10:30:00Z"}},
		{label: "freewinder replacement", window: stats.Range{After: "2026-03-01T11:30:00Z", Until: "2026-03-01T12:30:00Z"}},
		{label: "bankruptcy replacement", window: stats.Range{After: "2026-03-01T13:30:00Z", Until: "2026-03-01T14:30:00Z"}},
	} {
		realizationBreakdown, computeError := engineInstance.ComputeRealization(scenario.window, nil)
		if computeError != nil {
			t.Fatalf("ComputeRealization (%s) failed: %v", scenario.label, computeError)
		}
		if realizationBreakdown.CartoLostDeath <= 0 {
			t.Errorf("%s: expected CartoLostDeath > 0, got %d", scenario.label, realizationBreakdown.CartoLostDeath)
		}
		if realizationBreakdown.TotalRealized != 0 {
			t.Errorf("%s: expected TotalRealized 0, got %d", scenario.label, realizationBreakdown.TotalRealized)
		}
	}
}

func TestRealizationLedgerCaching(t *testing.T) {
	databaseConnection := setupRealizationDB(t)

	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":111,"Body":1}`)

	engineInstance := stats.NewEngine(databaseConnection)

	firstResult, firstComputeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if firstComputeError != nil {
		t.Fatalf("first ComputeRealization failed: %v", firstComputeError)
	}
	if firstResult.PendingValue <= 0 {
		t.Fatalf("expected positive PendingValue, got %d", firstResult.PendingValue)
	}

	// Insert second event directly into database without invalidating cache
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":76043200}]}`)

	// Query with cached ledger should still report pending because cache has not been invalidated
	secondResult, secondComputeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if secondComputeError != nil {
		t.Fatalf("second ComputeRealization failed: %v", secondComputeError)
	}
	if secondResult.PendingValue != firstResult.PendingValue {
		t.Fatalf("expected identical cached pending value %d, got %d", firstResult.PendingValue, secondResult.PendingValue)
	}

	// Invalidate ledger cache
	stats.InvalidateLedgerCache()

	// Query after invalidation should pick up the sale
	thirdResult, thirdComputeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if thirdComputeError != nil {
		t.Fatalf("third ComputeRealization failed: %v", thirdComputeError)
	}
	if thirdResult.PendingValue != 0 {
		t.Fatalf("expected pending value 0 after sale realization and cache invalidation, got %d", thirdResult.PendingValue)
	}
	if thirdResult.TotalRealized != 95054000 {
		t.Fatalf("expected TotalRealized 95054000 after cache invalidation, got %d", thirdResult.TotalRealized)
	}
}

func TestRealizationOnFootDeathPreservesCarto(t *testing.T) {
	databaseConnection := setupRealizationDB(t)

	// Scan body in ship
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Sol","SystemAddress":1234,"BodyID":1,"BodyName":"Sol A 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`)

	// Scan organic on foot
	insertEvent(t, databaseConnection, "2026-03-01T10:30:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:30:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":1234,"Body":1}`)

	// On-foot free suit redeploy and detention-center hand-in (with Cost > 0 fine) both preserve shipboard cartography
	insertEvent(t, databaseConnection, "2026-03-01T11:00:05Z", "Resurrect",
		`{"timestamp":"2026-03-01T11:00:05Z","event":"Resurrect","Option":"free","Cost":0,"Bankrupt":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T11:30:05Z", "Resurrect",
		`{"timestamp":"2026-03-01T11:30:05Z","event":"Resurrect","Option":"handin","Cost":5000,"Bankrupt":false}`)

	// Later sold cartography via MultiSellExplorationData
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "MultiSellExplorationData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":500000,"TotalEarnings":1500000,"Discovered":[{"SystemName":"Sol","NumBodies":1}]}`)

	engineInstance := stats.NewEngine(databaseConnection)
	realizationBreakdown, computeError := engineInstance.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T10:45:00Z"}, nil)
	if computeError != nil {
		t.Fatalf("ComputeRealization failed: %v", computeError)
	}

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
	databaseConnection := setupRealizationDB(t)

	// Scan two systems during the leg: both Alpha and Beta are undiscovered (WasDiscovered: false) and expect a bonus.
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Alpha","SystemAddress":1001,"BodyID":1,"BodyName":"Alpha 1","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:15:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:15:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Beta","SystemAddress":1002,"BodyID":1,"BodyName":"Beta 1","PlanetClass":"High metal content body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	// Page 1 sells only Alpha with Bonus: 0 (someone else turned Alpha in first, while Beta remains unsold).
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "MultiSellExplorationData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":0,"TotalEarnings":1000000,"Discovered":[{"SystemName":"Alpha","NumBodies":1}]}`)

	engineInstance := stats.NewEngine(databaseConnection)
	legWindow := stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T10:30:00Z"}

	afterFirstPageBreakdown, firstPageError := engineInstance.ComputeRealization(legWindow, nil)
	if firstPageError != nil {
		t.Fatalf("ComputeRealization after first page failed: %v", firstPageError)
	}
	if afterFirstPageBreakdown.TotalRealized != 1000000 {
		t.Errorf("expected TotalRealized 1,000,000 after first page, got %d", afterFirstPageBreakdown.TotalRealized)
	}
	if afterFirstPageBreakdown.CartoStolen != 24188 {
		t.Errorf("expected CartoStolen 24,188 (only Alpha bonus stolen), got %d", afterFirstPageBreakdown.CartoStolen)
	}
	if afterFirstPageBreakdown.PendingValue != 39305 {
		t.Errorf("expected PendingValue 39,305 for unsold Beta scan, got %d", afterFirstPageBreakdown.PendingValue)
	}

	// Page 2 is sold a week later and realizes Beta with its full discovery bonus.
	insertEvent(t, databaseConnection, "2026-03-08T12:00:00Z", "MultiSellExplorationData",
		`{"timestamp":"2026-03-08T12:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":500000,"TotalEarnings":1500000,"Discovered":[{"SystemName":"Beta","NumBodies":1}]}`)
	stats.InvalidateLedgerCache()

	afterSecondPageBreakdown, secondPageError := engineInstance.ComputeRealization(legWindow, nil)
	if secondPageError != nil {
		t.Fatalf("ComputeRealization after second page failed: %v", secondPageError)
	}
	if afterSecondPageBreakdown.TotalRealized != 2500000 {
		t.Errorf("expected combined TotalRealized 2,500,000 across pages, got %d", afterSecondPageBreakdown.TotalRealized)
	}
	if afterSecondPageBreakdown.CartoStolen != 24188 {
		t.Errorf("expected CartoStolen to remain 24,188 (only Alpha bonus stolen, Beta not marked stolen), got %d", afterSecondPageBreakdown.CartoStolen)
	}
	if afterSecondPageBreakdown.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 after both pages sold, got %d", afterSecondPageBreakdown.PendingValue)
	}
}

func TestRealizationSameTimestampDistinctSalesPreservedAndCanonicalUID(t *testing.T) {
	// Verify ParseLine generates the exact same UID for raw journal vs EDDiscovery whitespace formatting
	rawLine := []byte(`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`)
	eddLine := []byte(`{ "timestamp":"2026-03-01T12:00:00Z", "event":"SellOrganicData", "MarketID":123, "BioData":[ { "Species":"$Codex_Ent_Bacterial_01_Name;", "Value":1000000, "Bonus":0 } ] }`)
	rawJournalEvent, isRawValid, rawParseError := journal.ParseLine(rawLine, "Journal.log")
	eddJournalEvent, isEDDValid, eddParseError := journal.ParseLine(eddLine, "EDDiscovery")
	if !isRawValid || !isEDDValid || rawParseError != nil || eddParseError != nil {
		t.Fatalf("ParseLine failed: isRawValid=%v rawParseError=%v isEDDValid=%v eddParseError=%v", isRawValid, rawParseError, isEDDValid, eddParseError)
	}
	if rawJournalEvent.UID != eddJournalEvent.UID {
		t.Fatalf("expected identical canonical UID for SellOrganicData across whitespace differences, got %s vs %s", rawJournalEvent.UID, eddJournalEvent.UID)
	}

	// Verify duplicate whitespace-varied sales are ignored while genuinely distinct sales sharing the same 1-second timestamp are both preserved
	databaseConnection := setupRealizationDB(t)
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Bacterial","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","WasLogged":true,"SystemAddress":111,"Body":1}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:05:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:05:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":111,"Body":1}`)

	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`)
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{ "timestamp":"2026-03-01T12:00:00Z", "event":"SellOrganicData", "MarketID":123, "BioData":[ {"Species":"$Codex_Ent_Bacterial_01_Name;", "Value":1000000, "Bonus":0} ] }`)
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	engine := stats.NewEngine(databaseConnection)
	realizationBreakdown, err := engine.ComputeRealization(stats.Range{}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}
	if realizationBreakdown.TotalRealized != 20010800 {
		t.Errorf("expected TotalRealized 20,010,800 for two distinct sales at same timestamp with duplicate ignored, got %d", realizationBreakdown.TotalRealized)
	}
}

func TestRealizationFirstFootfallOnlyDeterminesFirstLoggedAndStolen(t *testing.T) {
	databaseConnection := setupRealizationDB(t)

	// Body 1: Already footfalled (WasFootfalled: true).
	// Even though ScanOrganic reports WasLogged: false (https://issues.frontierstore.net/issue-detail/81891),
	// selling with Bonus: 0 must NOT be flagged as stolen.
	insertEvent(t, databaseConnection, "2026-03-01T09:50:00Z", "Scan",
		`{"timestamp":"2026-03-01T09:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"Pegasi","SystemAddress":5001,"BodyID":1,"BodyName":"Pegasi 1","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Cactoid","Species":"$Codex_Ent_Cactoid_03_Name;","Species_Localised":"Cactoida Vermis","WasLogged":false,"SystemAddress":5001,"Body":1}`)

	// Body 2: First footfall (WasFootfalled: false), even if previously discovered from space (WasDiscovered: true).
	// Selling with Bonus: 0 MUST be flagged as stolen because WasFootfalled was false at time of scan.
	insertEvent(t, databaseConnection, "2026-03-01T10:10:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DeepSpace","SystemAddress":5002,"BodyID":2,"BodyName":"DeepSpace 2","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":false,"WasFootfalled":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:20:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:20:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":5002,"Body":2}`)

	// Sell both with Bonus = 0
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[
			{"Species":"$Codex_Ent_Cactoid_03_Name;","Species_Localised":"Cactoida Vermis","Value":16202800,"Bonus":0},
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":0}
		]}`)

	engine := stats.NewEngine(databaseConnection)
	realizationBreakdown, err := engine.ComputeRealization(stats.Range{After: "2026-03-01T09:00:00Z", Until: "2026-03-01T11:00:00Z"}, nil)
	if err != nil {
		t.Fatalf("ComputeRealization failed: %v", err)
	}

	// Only Stratum Tectonicas (76,043,200 bonus) on un-footfalled Body 2 should be flagged stolen;
	// Cactoida Vermis on already-footfalled Body 1 must NOT be flagged stolen.
	if realizationBreakdown.OrganicStolen != 76043200 {
		t.Errorf("expected OrganicStolen 76,043,200 (only Body 2), got %d", realizationBreakdown.OrganicStolen)
	}
	if realizationBreakdown.OrganicLostDeath != 0 {
		t.Errorf("expected OrganicLostDeath 0, got %d", realizationBreakdown.OrganicLostDeath)
	}
}

func TestFootfalledBodyUpgradesExobiologyBonusWhenSoldWithBonus(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engine := stats.NewEngine(databaseConnection)

	// Body already has WasFootfalled: true (another commander landed previously without logging organics).
	insertEvent(t, databaseConnection, "2026-03-01T09:50:00Z", "Scan",
		`{"timestamp":"2026-03-01T09:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"FootfalledSys","SystemAddress":6001,"BodyID":1,"BodyName":"FootfalledSys 1","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":6001,"Body":1}`)

	legWindow := stats.Range{After: "2026-03-01T09:55:00Z", Until: "2026-03-01T10:30:00Z"}

	// Before sale: estimated at base value only (19,010,800 Cr) because WasFootfalled is true.
	beforeMetrics, err := engine.Compute(legWindow, nil)
	if err != nil {
		t.Fatalf("Compute before sale failed: %v", err)
	}
	if beforeMetrics.FirstLogged != 0 {
		t.Errorf("expected FirstLogged 0 before sale, got %d", beforeMetrics.FirstLogged)
	}
	if beforeMetrics.OrganicValueTotal != 19010800 {
		t.Errorf("expected OrganicValueTotal 19,010,800 before sale, got %d", beforeMetrics.OrganicValueTotal)
	}

	// Sold on a later leg with the 4x first-logged bonus (76,043,200 Cr).
	insertEvent(t, databaseConnection, "2026-03-01T14:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T14:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":76043200}
		]}`)
	stats.InvalidateLedgerCache()

	// After sale: the original leg's Exobiology Value upgrades to include the first-logged bonus (95,054,000 Cr).
	afterMetrics, err := engine.Compute(legWindow, nil)
	if err != nil {
		t.Fatalf("Compute after sale failed: %v", err)
	}
	if afterMetrics.FirstLogged != 1 {
		t.Errorf("expected FirstLogged 1 after sale, got %d", afterMetrics.FirstLogged)
	}
	if afterMetrics.OrganicValueBonus != 76043200 {
		t.Errorf("expected OrganicValueBonus 76,043,200 after sale, got %d", afterMetrics.OrganicValueBonus)
	}
	if afterMetrics.OrganicValueTotal != 95054000 {
		t.Errorf("expected OrganicValueTotal 95,054,000 after sale, got %d", afterMetrics.OrganicValueTotal)
	}
}

func TestOrganicSaleBonusTieBreakerAvoidsFalseUpgradeAndFalseStolen(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// First scan Stratum Tectonicas on an already-footfalled body (WasFootfalled: true),
	// then scan the same species on an un-footfalled body (WasFootfalled: false).
	insertEvent(t, databaseConnection, "2026-03-01T09:50:00Z", "Scan",
		`{"timestamp":"2026-03-01T09:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"TieBreakSys","SystemAddress":7001,"BodyID":1,"BodyName":"TieBreakSys 1","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`)
	insertEvent(t, databaseConnection, "2026-03-01T09:52:00Z", "Scan",
		`{"timestamp":"2026-03-01T09:52:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"TieBreakSys","SystemAddress":7001,"BodyID":2,"BodyName":"TieBreakSys 2","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":false,"WasFootfalled":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":7001,"Body":1}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:10:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":7001,"Body":2}`)

	// Sell both in one SellOrganicData event with the bonus-paying sample listed first.
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "SellOrganicData",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":76043200},
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":0}
		]}`)

	organicWindow := stats.Range{After: "2026-03-01T09:55:00Z", Until: "2026-03-01T11:00:00Z"}
	realizationResult, realizationError := engineInstance.ComputeRealization(organicWindow, nil)
	if realizationError != nil {
		t.Fatalf("ComputeRealization failed: %v", realizationError)
	}
	expectedRealized := int64(19010800 + 76043200 + 19010800)
	if realizationResult.TotalRealized != expectedRealized {
		t.Errorf("expected TotalRealized %d, got %d", expectedRealized, realizationResult.TotalRealized)
	}
	if realizationResult.OrganicStolen != 0 {
		t.Errorf("expected OrganicStolen 0, got %d", realizationResult.OrganicStolen)
	}
	if realizationResult.PendingValue != 0 {
		t.Errorf("expected PendingValue 0, got %d", realizationResult.PendingValue)
	}

	legWindow := stats.Range{After: "2026-03-01T09:45:00Z", Until: "2026-03-01T11:00:00Z"}
	legMetrics, computeError := engineInstance.Compute(legWindow, nil)
	if computeError != nil {
		t.Fatalf("Compute failed: %v", computeError)
	}
	if legMetrics.FirstLogged != 1 {
		t.Errorf("expected FirstLogged 1, got %d", legMetrics.FirstLogged)
	}
}

func TestRealizationCartoDeduplicationSoldStateAndPostRebuyRescan(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Initial AutoScan of DedupeSys 1.
	insertEvent(t, databaseConnection, "2026-03-01T10:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Scan","ScanType":"AutoScan","StarSystem":"DedupeSys","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	firstLegWindow := stats.Range{After: "2026-03-01T09:50:00Z", Until: "2026-03-01T10:30:00Z"}
	afterInitialScan, initialScanError := engineInstance.ComputeRealization(firstLegWindow, nil)
	if initialScanError != nil {
		t.Fatalf("ComputeRealization after initial scan failed: %v", initialScanError)
	}
	bodyScanOnlyValue := afterInitialScan.PendingValue
	if bodyScanOnlyValue <= 0 {
		t.Fatalf("expected positive PendingValue after initial scan, got %d", bodyScanOnlyValue)
	}

	// Map DedupeSys 1 with DSS and emit a duplicate SAAScanComplete plus a follow-up Detailed Scan.
	insertEvent(t, databaseConnection, "2026-03-01T10:05:00Z", "SAAScanComplete",
		`{"timestamp":"2026-03-01T10:05:00Z","event":"SAAScanComplete","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","ProbesUsed":5,"EfficiencyTarget":6}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:06:00Z", "SAAScanComplete",
		`{"timestamp":"2026-03-01T10:06:00Z","event":"SAAScanComplete","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","ProbesUsed":5,"EfficiencyTarget":6}`)
	insertEvent(t, databaseConnection, "2026-03-01T10:10:00Z", "Scan",
		`{"timestamp":"2026-03-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DedupeSys","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	stats.InvalidateLedgerCache()

	afterMappingAndDuplicateScan, duplicateScanError := engineInstance.ComputeRealization(firstLegWindow, nil)
	if duplicateScanError != nil {
		t.Fatalf("ComputeRealization after mapping and duplicate scan failed: %v", duplicateScanError)
	}
	if afterMappingAndDuplicateScan.PendingValue <= bodyScanOnlyValue {
		t.Fatalf("expected PendingValue after DSS mapping (%d) to exceed scan-only value (%d) without being overwritten by follow-up Scan",
			afterMappingAndDuplicateScan.PendingValue, bodyScanOnlyValue)
	}

	// Selling DedupeSys must clear all pending value without leaving an orphaned pending record from the second Scan.
	insertEvent(t, databaseConnection, "2026-03-01T11:00:00Z", "MultiSellExplorationData",
		`{"timestamp":"2026-03-01T11:00:00Z","event":"MultiSellExplorationData","BaseValue":1500000,"Bonus":500000,"TotalEarnings":2000000,"Discovered":[{"SystemName":"DedupeSys","NumBodies":1}]}`)
	stats.InvalidateLedgerCache()

	afterSale, afterSaleError := engineInstance.ComputeRealization(firstLegWindow, nil)
	if afterSaleError != nil {
		t.Fatalf("ComputeRealization after sale failed: %v", afterSaleError)
	}
	if afterSale.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 after selling DedupeSys, got %d", afterSale.PendingValue)
	}
	if afterSale.TotalRealized != 2000000 {
		t.Errorf("expected TotalRealized 2,000,000 after selling DedupeSys, got %d", afterSale.TotalRealized)
	}

	// Re-scanning or re-mapping an already-sold body later in the career must not create a new pending record.
	insertEvent(t, databaseConnection, "2026-03-01T12:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"Scan","ScanType":"AutoScan","StarSystem":"DedupeSys","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`)
	insertEvent(t, databaseConnection, "2026-03-01T12:05:00Z", "SAAScanComplete",
		`{"timestamp":"2026-03-01T12:05:00Z","event":"SAAScanComplete","SystemAddress":8001,"BodyID":1,"BodyName":"DedupeSys 1","ProbesUsed":5,"EfficiencyTarget":6}`)
	stats.InvalidateLedgerCache()

	rescanSoldWindow := stats.Range{After: "2026-03-01T11:30:00Z", Until: "2026-03-01T12:30:00Z"}
	afterSoldRescan, soldRescanError := engineInstance.ComputeRealization(rescanSoldWindow, nil)
	if soldRescanError != nil {
		t.Fatalf("ComputeRealization after re-scanning sold body failed: %v", soldRescanError)
	}
	if afterSoldRescan.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 when re-scanning an already-sold body, got %d", afterSoldRescan.PendingValue)
	}

	// Scanning a body, losing it to ship destruction, and then re-scanning it must create a new payable pending record.
	insertEvent(t, databaseConnection, "2026-03-01T13:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T13:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RebuySys","SystemAddress":8002,"BodyID":1,"BodyName":"RebuySys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T13:30:00Z", "Resurrect",
		`{"timestamp":"2026-03-01T13:30:00Z","event":"Resurrect","Option":"rebuy","Cost":1000000,"Bankrupt":false}`)
	insertEvent(t, databaseConnection, "2026-03-01T14:00:00Z", "Scan",
		`{"timestamp":"2026-03-01T14:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"RebuySys","SystemAddress":8002,"BodyID":1,"BodyName":"RebuySys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	stats.InvalidateLedgerCache()

	postRebuyWindow := stats.Range{After: "2026-03-01T13:45:00Z", Until: "2026-03-01T14:30:00Z"}
	afterPostRebuyRescan, postRebuyRescanError := engineInstance.ComputeRealization(postRebuyWindow, nil)
	if postRebuyRescanError != nil {
		t.Fatalf("ComputeRealization after post-rebuy rescan failed: %v", postRebuyRescanError)
	}
	if afterPostRebuyRescan.PendingValue != bodyScanOnlyValue {
		t.Errorf("expected PendingValue %d after re-scanning lost body post-rebuy, got %d", bodyScanOnlyValue, afterPostRebuyRescan.PendingValue)
	}

	// Selling the post-rebuy re-scan realizes the payout on the new record.
	insertEvent(t, databaseConnection, "2026-03-01T15:00:00Z", "MultiSellExplorationData",
		`{"timestamp":"2026-03-01T15:00:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":500000,"TotalEarnings":1500000,"Discovered":[{"SystemName":"RebuySys","NumBodies":1}]}`)
	stats.InvalidateLedgerCache()

	afterPostRebuySale, postRebuySaleError := engineInstance.ComputeRealization(postRebuyWindow, nil)
	if postRebuySaleError != nil {
		t.Fatalf("ComputeRealization after post-rebuy sale failed: %v", postRebuySaleError)
	}
	if afterPostRebuySale.PendingValue != 0 {
		t.Errorf("expected PendingValue 0 after selling post-rebuy rescan, got %d", afterPostRebuySale.PendingValue)
	}
	if afterPostRebuySale.TotalRealized != 1500000 {
		t.Errorf("expected TotalRealized 1,500,000 after selling post-rebuy rescan, got %d", afterPostRebuySale.TotalRealized)
	}
}

func TestRealizationCrewWageWindowAndBinarySearchBounds(testingContext *testing.T) {
	databaseConnection := setupRealizationDB(testingContext)
	engineInstance := stats.NewEngine(databaseConnection)

	// Record at exact After boundary (excluded), inside window (included), and after Until boundary (excluded).
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:00:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":9001,"Body":1}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:15:00Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":9001,"Body":2}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:30:01Z", "ScanOrganic",
		`{"timestamp":"2026-03-01T10:30:01Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":9001,"Body":3}`)

	// Unattributed cartographic sales at exact After boundary (excluded), exact Until boundary (included), and after Until (excluded).
	// Also test NpcCrewPaidWage 5-second window boundaries around the 10:30:00Z sale:
	// -6s (10:29:54Z, excluded), -5s (10:29:55Z, included: 10,000), +5s (10:30:05Z, included: 20,000), +6s (10:30:06Z, excluded).
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:00:00Z", "SellExplorationData",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"SellExplorationData","Systems":["BeforeSys"],"Discovered":[],"BaseValue":100000,"Bonus":0,"TotalEarnings":100000}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:29:54Z", "NpcCrewPaidWage",
		`{"timestamp":"2026-03-01T10:29:54Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":99999}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:29:55Z", "NpcCrewPaidWage",
		`{"timestamp":"2026-03-01T10:29:55Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":10000}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:30:00Z", "SellExplorationData",
		`{"timestamp":"2026-03-01T10:30:00Z","event":"SellExplorationData","Systems":["InsideSys"],"Discovered":[],"BaseValue":500000,"Bonus":0,"TotalEarnings":500000}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:30:05Z", "NpcCrewPaidWage",
		`{"timestamp":"2026-03-01T10:30:05Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":20000}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:30:06Z", "NpcCrewPaidWage",
		`{"timestamp":"2026-03-01T10:30:06Z","event":"NpcCrewPaidWage","NpcCrewId":1,"NpcCrewName":"CrewA","Amount":88888}`)
	insertEvent(testingContext, databaseConnection, "2026-03-01T10:30:01Z", "SellExplorationData",
		`{"timestamp":"2026-03-01T10:30:01Z","event":"SellExplorationData","Systems":["AfterSys"],"Discovered":[],"BaseValue":700000,"Bonus":0,"TotalEarnings":700000}`)

	window := stats.Range{After: "2026-03-01T10:00:00Z", Until: "2026-03-01T10:30:00Z"}
	breakdown, computeError := engineInstance.ComputeRealization(window, nil)
	if computeError != nil {
		testingContext.Fatalf("ComputeRealization failed: %v", computeError)
	}
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

