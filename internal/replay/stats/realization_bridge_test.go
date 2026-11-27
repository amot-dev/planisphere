package stats_test

import (
	"errors"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/replay"
	"planisphere/internal/replay/realization"
	"planisphere/internal/replay/stats"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

type stubLedgerSource struct {
	ledger *realization.Ledger
	err    error
	calls  int
}

func (source *stubLedgerSource) Get() (*realization.Ledger, error) {
	source.calls++
	return source.ledger, source.err
}

func TestOrganicSaleBonusTieBreakerAvoidsFalseUpgradeAndFalseStolen(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// First scan Stratum Tectonicas on an already-footfalled body (WasFootfalled: true),
	// then scan the same species on an un-footfalled body (WasFootfalled: false), and sell both together.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-03-01T09:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"TieBreakSys","SystemAddress":7001,"BodyID":1,"BodyName":"TieBreakSys 1","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`,
		`{"timestamp":"2026-03-01T09:52:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"TieBreakSys","SystemAddress":7001,"BodyID":2,"BodyName":"TieBreakSys 2","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":7001,"Body":1}`,
		`{"timestamp":"2026-03-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":false,"SystemAddress":7001,"Body":2}`,
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":76043200},
			{"Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","Value":19010800,"Bonus":0}
		]}`,
	)

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
	legMetrics, computeError := engineInstance.ComputeExobiology(legWindow, nil)
	if computeError != nil {
		t.Fatalf("ComputeExobiology failed: %v", computeError)
	}
	if legMetrics.FirstLogged != 1 {
		t.Errorf("expected FirstLogged 1, got %d", legMetrics.FirstLogged)
	}
}

func TestComputeRealizationFilterScopeAndLedgerSourceBridge(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		"Journal.2026-04-01T100000.01.log": {
			`{"timestamp":"2026-04-01T10:00:00Z","event":"LoadGame","Commander":"CmdrMain","FID":"F10","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-04-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Stratum","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","WasLogged":true,"SystemAddress":101,"Body":1}`,
			`{"timestamp":"2026-04-01T10:50:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`,
		},
		"Journal.2026-04-01T102000.01.log": {
			`{"timestamp":"2026-04-01T10:20:00Z","event":"LoadGame","Commander":"CmdrAlt","FID":"F20","Ship":"Explorer_NX","Odyssey":true}`,
			`{"timestamp":"2026-04-01T10:25:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"Bacterial","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","WasLogged":true,"SystemAddress":202,"Body":1}`,
		},
	})

	commanders, listError := replay.ListCommanders(databaseConnection)
	if listError != nil || len(commanders) != 2 {
		testingContext.Fatalf("expected 2 commanders, got %+v err=%v", commanders, listError)
	}
	defaultModes := expeditiontest.LookupDefaultModes(testingContext, databaseConnection)
	explorationModeID := defaultModes.Exploration

	builtLedger, buildError := realization.BuildLedger(databaseConnection, true)
	if buildError != nil {
		testingContext.Fatalf("BuildLedger: %v", buildError)
	}
	ledgerSource := &stubLedgerSource{ledger: builtLedger}
	engineInstance := stats.NewEngineWithSources(databaseConnection, ledgerSource, nil, func(_ db.Queryer, _ int64) (stats.ModeLookup, error) {
		return func(_ string) int64 { return explorationModeID }, nil
	})

	window := stats.Range{After: "2026-04-01T10:00:00Z", Until: "2026-04-01T10:40:00Z"}
	mainScoped, mainError := engineInstance.ComputeRealization(window, &stats.Filter{
		Enabled: true,
		Modes:   []stats.ModeSelection{{ModeID: explorationModeID, Travel: true, Discovery: true}},
		Scope:   replay.NewProfileScope([]int64{commanders[0].ID}),
	})
	if mainError != nil {
		testingContext.Fatalf("ComputeRealization mainScoped: %v", mainError)
	}
	if mainScoped.TotalRealized != 19010800 || mainScoped.PendingValue != 0 || ledgerSource.calls != 1 {
		testingContext.Fatalf("unexpected mainScoped realization: %+v (calls=%d)", mainScoped, ledgerSource.calls)
	}

	discoveryExcluded, excludeError := engineInstance.ComputeRealization(window, &stats.Filter{
		Enabled: true,
		Modes:   []stats.ModeSelection{{ModeID: explorationModeID, Travel: true, Discovery: false}},
	})
	if excludeError != nil || discoveryExcluded.TotalRealized != 0 || discoveryExcluded.PendingValue != 0 {
		testingContext.Fatalf("expected Discovery=false filter to exclude realization records, got %+v err=%v", discoveryExcluded, excludeError)
	}

	ledgerSource.err = errors.New("ledger source failure")
	if _, sourceError := engineInstance.ComputeRealization(window, nil); !errors.Is(sourceError, ledgerSource.err) {
		testingContext.Fatalf("expected ComputeRealization to propagate ledger source error, got %v", sourceError)
	}
}
