package stats_test

import (
	"testing"

	"planisphere/internal/replay/stats"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestCarrierTariffRealization(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Dock at a Fleet Carrier (25% tariff), relog on foot in carrier Concourse (25% tariff), then dock at an Orbis station (0% tariff).
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"CarrierSys","SystemAddress":2101,"BodyID":1,"BodyName":"CarrierSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-01T10:03:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"ConcourseSys","SystemAddress":2103,"BodyID":1,"BodyName":"ConcourseSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-01T10:05:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"StationSys","SystemAddress":2102,"BodyID":1,"BodyName":"StationSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-01T11:00:00Z","event":"Docked","StationName":"K9F-8TQ","StationType":"FleetCarrier","StarSystem":"CarrierSys","SystemAddress":2101,"MarketID":3700000001}`,
		`{"timestamp":"2026-04-01T11:02:00Z","event":"MultiSellExplorationData","BaseValue":750000,"Bonus":750,"TotalEarnings":750750,"Discovered":[{"SystemName":"CarrierSys","NumBodies":1}]}`,
		`{"timestamp":"2026-04-01T11:10:00Z","event":"Location","Docked":false,"OnFoot":true,"StarSystem":"CarrierSys","SystemAddress":2101,"Body":"CarrierSys A","BodyType":"Star"}`,
		`{"timestamp":"2026-04-01T11:15:00Z","event":"CarrierJump","Docked":false,"OnFoot":true,"StarSystem":"JumpDestSys","SystemAddress":2104,"Body":"JumpDestSys 1","BodyType":"Planet"}`,
		`{"timestamp":"2026-04-01T11:18:00Z","event":"MultiSellExplorationData","BaseValue":300000,"Bonus":0,"TotalEarnings":300000,"Discovered":[{"SystemName":"ConcourseSys","NumBodies":1}]}`,
		`{"timestamp":"2026-04-01T11:25:00Z","event":"Undocked","StationName":"K9F-8TQ","StationType":"FleetCarrier","MarketID":3700000001}`,
		`{"timestamp":"2026-04-01T11:35:00Z","event":"Docked","StationName":"Galileo","StationType":"Orbis","StarSystem":"StationSys","SystemAddress":2102,"MarketID":3200000001}`,
		`{"timestamp":"2026-04-01T11:36:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":1000,"TotalEarnings":1001000,"Discovered":[{"SystemName":"StationSys","NumBodies":1}]}`,
	)

	breakdown, computeError := engineInstance.ComputeRealization(stats.Range{After: "2026-04-01T09:50:00Z", Until: "2026-04-01T10:30:00Z"}, nil)
	if computeError != nil {
		t.Fatalf("ComputeRealization failed: %v", computeError)
	}
	if breakdown.CarrierTariffLoss != 350250 {
		t.Errorf("expected CarrierTariffLoss 350,250, got %d", breakdown.CarrierTariffLoss)
	}
	if breakdown.TotalRealized != 2051750 {
		t.Errorf("expected TotalRealized 2,051,750, got %d", breakdown.TotalRealized)
	}
}

func TestResurrectConcourseCarrierTariff(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Non-ship death recovery onto a Fleet Carrier Concourse incurs 25% tariff, while penal Station respawn clears carrier state.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-04-03T10:00:00Z","event":"Docked","StationName":"K1V-6QB","StationType":"FleetCarrier","StarSystem":"BaseSys","SystemAddress":2200,"MarketID":3700000099}`,
		`{"timestamp":"2026-04-03T10:05:00Z","event":"Undocked","StationName":"K1V-6QB","StationType":"FleetCarrier","MarketID":3700000099}`,
		`{"timestamp":"2026-04-03T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"CarrierRecoverSys","SystemAddress":2201,"BodyID":1,"BodyName":"CarrierRecoverSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-03T10:15:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"PenalEscapeSys","SystemAddress":2202,"BodyID":1,"BodyName":"PenalEscapeSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2026-04-03T10:30:00Z","event":"Resurrect","Option":"recover","Cost":5000,"Bankrupt":false}`,
		`{"timestamp":"2026-04-03T10:30:08Z","event":"Location","Docked":false,"OnFoot":true,"StarSystem":"BaseSys","SystemAddress":2200,"Body":"BaseSys 1","BodyType":"Planet"}`,
		`{"timestamp":"2026-04-03T10:35:00Z","event":"MultiSellExplorationData","BaseValue":600000,"Bonus":0,"TotalEarnings":600000,"Discovered":[{"SystemName":"CarrierRecoverSys","NumBodies":1}]}`,
		`{"timestamp":"2026-04-03T10:45:00Z","event":"Resurrect","Option":"escape","Cost":0,"Bankrupt":false}`,
		`{"timestamp":"2026-04-03T10:45:08Z","event":"Location","Docked":false,"OnFoot":true,"StarSystem":"PenalSys","SystemAddress":2203,"Body":"Ashcliffe Penitentiary","BodyType":"Station"}`,
		`{"timestamp":"2026-04-03T10:50:00Z","event":"MultiSellExplorationData","BaseValue":400000,"Bonus":0,"TotalEarnings":400000,"Discovered":[{"SystemName":"PenalEscapeSys","NumBodies":1}]}`,
	)

	breakdown, computeError := engineInstance.ComputeRealization(stats.Range{After: "2026-04-03T10:08:00Z", Until: "2026-04-03T10:20:00Z"}, nil)
	if computeError != nil {
		t.Fatalf("ComputeRealization failed: %v", computeError)
	}
	if breakdown.CarrierTariffLoss != 200000 {
		t.Errorf("expected CarrierTariffLoss 200,000, got %d", breakdown.CarrierTariffLoss)
	}
	if breakdown.TotalRealized != 1000000 {
		t.Errorf("expected TotalRealized 1,000,000, got %d", breakdown.TotalRealized)
	}
}

func TestPopulatedAndPre2025OrganicEligibility(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Populated Bubble system suppresses first bonuses and stolen flags; unpopulated untouched pre-2025 body earns 5x estimate and flags stolen when sold with Bonus 0.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2026-04-02T09:00:00Z","event":"FSDJump","StarSystem":"HR 1385","SystemAddress":3101,"JumpDist":15.0,"Population":4500000}`,
		`{"timestamp":"2026-04-02T09:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"HR 1385","SystemAddress":3101,"BodyID":2,"BodyName":"HR 1385 2","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`,
		`{"timestamp":"2026-04-02T09:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Bacterial_Genus_Name;","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","SystemAddress":3101,"Body":2}`,
		`{"timestamp":"2024-06-01T10:00:00Z","event":"FSDJump","StarSystem":"DeepUntouched","SystemAddress":3102,"JumpDist":40.0,"Population":0}`,
		`{"timestamp":"2024-06-01T10:05:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DeepUntouched","SystemAddress":3102,"BodyID":1,"BodyName":"DeepUntouched 1","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2024-06-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Stratum_Genus_Name;","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","SystemAddress":3102,"Body":1}`,
		`{"timestamp":"2024-06-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`,
		`{"timestamp":"2026-04-02T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`,
	)

	populatedWindow := stats.Range{After: "2026-04-02T09:05:00Z", Until: "2026-04-02T10:00:00Z"}
	populatedCarto, popCartoError := engineInstance.ComputeCartographics(populatedWindow, nil)
	if popCartoError != nil {
		t.Fatalf("ComputeCartographics populated window failed: %v", popCartoError)
	}
	populatedExo, popExoError := engineInstance.ComputeExobiology(populatedWindow, nil)
	if popExoError != nil {
		t.Fatalf("ComputeExobiology populated window failed: %v", popExoError)
	}
	if populatedCarto.FirstDiscoveries != 0 || populatedExo.FirstLogged != 0 || populatedExo.OrganicValueBonus != 0 {
		t.Errorf("expected zero first bonuses in preloaded populated system, got carto=%+v exo=%+v", populatedCarto, populatedExo)
	}
	populatedRealization, popRealErr := engineInstance.ComputeRealization(populatedWindow, nil)
	if popRealErr != nil {
		t.Fatalf("ComputeRealization populated window failed: %v", popRealErr)
	}
	if populatedRealization.OrganicStolen != 0 {
		t.Errorf("expected OrganicStolen 0 in populated system, got %d", populatedRealization.OrganicStolen)
	}

	pre2025Window := stats.Range{After: "2024-06-01T09:55:00Z", Until: "2024-06-01T11:00:00Z"}
	pre2025Metrics, preMetricsError := engineInstance.ComputeExobiology(pre2025Window, nil)
	if preMetricsError != nil {
		t.Fatalf("ComputeExobiology pre-2025 window failed: %v", preMetricsError)
	}
	if pre2025Metrics.FirstLogged != 1 || pre2025Metrics.OrganicValueTotal != 95054000 {
		t.Errorf("expected pre-2025 untouched body to earn 5x first-logged estimate, got %+v", pre2025Metrics)
	}
	pre2025Realization, preRealErr := engineInstance.ComputeRealization(pre2025Window, nil)
	if preRealErr != nil {
		t.Fatalf("ComputeRealization pre-2025 window failed: %v", preRealErr)
	}
	if pre2025Realization.OrganicStolen != 76043200 {
		t.Errorf("expected pre-2025 untouched body sold with Bonus 0 to record 76,043,200 OrganicStolen, got %d", pre2025Realization.OrganicStolen)
	}
}

func TestRadicoidaUnicaAggregatedCGAndPostCG(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Parent body scanned prior to cgWindow, followed by three Radicoida Unica scans during the November 2025 CG.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2025-11-24T17:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"HIP 87621","SystemAddress":147882789259,"BodyID":3,"BodyName":"HIP 87621 2 a","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`,
		`{"timestamp":"2025-11-24T18:46:36Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Genus_Localised":"Radicoida","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","SystemAddress":147882789259,"Body":3}`,
		`{"timestamp":"2025-11-24T18:50:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Genus_Localised":"Radicoida","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","SystemAddress":147882789259,"Body":3}`,
		`{"timestamp":"2025-11-24T18:55:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Genus_Localised":"Radicoida","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","SystemAddress":147882789259,"Body":3}`,
		`{"timestamp":"2025-11-24T19:12:26Z","event":"SellOrganicData","MarketID":129038712,"BioData":[{"Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","Value":2856888,"Bonus":0}]}`,
		`{"timestamp":"2025-11-24T20:00:00Z","event":"Resurrect","Option":"free","Cost":0,"Bankrupt":false}`,
	)

	cgWindow := stats.Range{After: "2025-11-24T18:00:00Z", Until: "2025-11-24T21:00:00Z"}
	cgMetrics, cgMetricsError := engineInstance.ComputeExobiology(cgWindow, nil)
	if cgMetricsError != nil {
		t.Fatalf("ComputeExobiology CG window failed: %v", cgMetricsError)
	}
	if cgMetrics.OrganicSamples != 3 || cgMetrics.OrganicValueTotal != 2856888 {
		t.Errorf("expected 3 CG samples totaling 2,856,888 Cr, got %+v", cgMetrics)
	}
	cgRealization, cgRealErr := engineInstance.ComputeRealization(cgWindow, nil)
	if cgRealErr != nil {
		t.Fatalf("ComputeRealization CG window failed: %v", cgRealErr)
	}
	if cgRealization.TotalRealized != 2856888 || cgRealization.OrganicLostDeath != 0 || cgRealization.PendingValue != 0 {
		t.Errorf("expected all 3 aggregated CG samples realized with zero death loss, got %+v", cgRealization)
	}
}

func TestSessionOdysseyAndPreUpdate14Realization(t *testing.T) {
	databaseConnection := dbtest.OpenMemory(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Horizons session before sub-window start, Odyssey session mapping an identical body, and pre-Update-14 organic scan sold with 1x bonus.
	journaltest.InsertParsedLines(
		t,
		databaseConnection,
		`{"timestamp":"2020-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Asp"}`,
		`{"timestamp":"2020-05-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"HorizonsSys","SystemAddress":4101,"BodyID":1,"BodyName":"HorizonsSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`,
		`{"timestamp":"2020-05-01T10:12:00Z","event":"SAAScanComplete","SystemAddress":4101,"BodyID":1,"BodyName":"HorizonsSys 1","ProbesUsed":5,"EfficiencyTarget":6}`,
		`{"timestamp":"2022-09-20T01:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Asp","Odyssey":true}`,
		`{"timestamp":"2022-09-20T01:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"OdysseySys","SystemAddress":4102,"BodyID":1,"BodyName":"OdysseySys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`,
		`{"timestamp":"2022-09-20T01:12:00Z","event":"SAAScanComplete","SystemAddress":4102,"BodyID":1,"BodyName":"OdysseySys 1","ProbesUsed":5,"EfficiencyTarget":6}`,
		`{"timestamp":"2022-09-20T02:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"LegacyBioSys","SystemAddress":4103,"BodyID":2,"BodyName":"LegacyBioSys 2","PlanetClass":"Rocky body","MassEM":0.3,"WasDiscovered":false,"WasMapped":false}`,
		`{"timestamp":"2022-09-20T02:08:22Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Clypeus_Genus_Name;","Species":"$Codex_Ent_Clypeus_03_Name;","Species_Localised":"Clypeus Speculumi","SystemAddress":4103,"Body":2}`,
		`{"timestamp":"2022-10-05T00:11:12Z","event":"SellOrganicData","MarketID":3701316096,"BioData":[{"Species":"$Codex_Ent_Clypeus_03_Name;","Species_Localised":"Clypeus Speculumi","Value":711500,"Bonus":711500}]}`,
	)

	horizonsMetrics, horizonsError := engineInstance.ComputeCartographics(stats.Range{After: "2020-05-01T10:05:00Z", Until: "2020-05-01T10:30:00Z"}, nil)
	if horizonsError != nil {
		t.Fatalf("ComputeCartographics Horizons window failed: %v", horizonsError)
	}
	odysseyMetrics, odysseyError := engineInstance.ComputeCartographics(stats.Range{After: "2022-09-20T01:05:00Z", Until: "2022-09-20T01:30:00Z"}, nil)
	if odysseyError != nil {
		t.Fatalf("ComputeCartographics Odyssey window failed: %v", odysseyError)
	}
	if horizonsMetrics.ScanValueTotal >= odysseyMetrics.ScanValueTotal {
		t.Errorf("expected Horizons mapped value (%d) < Odyssey mapped value (%d)", horizonsMetrics.ScanValueTotal, odysseyMetrics.ScanValueTotal)
	}

	legacyBioWindow := stats.Range{After: "2022-09-20T01:50:00Z", Until: "2022-09-20T02:30:00Z"}
	legacyBioMetrics, bioError := engineInstance.ComputeExobiology(legacyBioWindow, nil)
	if bioError != nil {
		t.Fatalf("ComputeExobiology legacy bio window failed: %v", bioError)
	}
	if legacyBioMetrics.OrganicValueBase != 711500 || legacyBioMetrics.OrganicValueBonus != 711500 || legacyBioMetrics.OrganicValueTotal != 1423000 {
		t.Errorf("expected pre-Update-14 Clypeus Speculumi 711,500 + 711,500 = 1,423,000, got %+v", legacyBioMetrics)
	}
}
