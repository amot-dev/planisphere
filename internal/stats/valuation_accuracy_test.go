package stats_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/journal"
	"planisphere/internal/stats"
)

func ingestTestLine(t *testing.T, databaseConnection *sql.DB, rawJSON string) {
	t.Helper()
	parsedEvent, ok, parseError := journal.ParseLine([]byte(rawJSON), "test.log")
	if parseError != nil || !ok {
		t.Fatalf("ParseLine(%s) failed: ok=%v err=%v", rawJSON, ok, parseError)
	}
	_, execError := databaseConnection.Exec(`INSERT INTO journal_events(
		event_uid, timestamp, event, system_name, system_address, body_id, body_name, raw_json, ingested_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		parsedEvent.UID, parsedEvent.Timestamp, parsedEvent.Event, parsedEvent.SystemName,
		parsedEvent.SystemAddress, parsedEvent.BodyID, parsedEvent.BodyName, parsedEvent.Raw, parsedEvent.Timestamp,
	)
	if execError != nil {
		t.Fatalf("insert parsed event %s: %v", parsedEvent.Event, execError)
	}
}

func TestCarrierTariffRealization(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T10:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"CarrierSys","SystemAddress":2101,"BodyID":1,"BodyName":"CarrierSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T10:03:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"ConcourseSys","SystemAddress":2103,"BodyID":1,"BodyName":"ConcourseSys 1","PlanetClass":"Water world","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T10:05:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"StationSys","SystemAddress":2102,"BodyID":1,"BodyName":"StationSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":false,"WasMapped":false}`)

	// Dock at a Fleet Carrier: BaseValue + Bonus (750,000 + 750 = 750,750) is 75% of pre-tariff 1,001,000 Cr,
	// so CarrierTariffLoss = 750,750 / 3 = 250,250 Cr.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:00:00Z","event":"Docked","StationName":"K9F-8TQ","StationType":"FleetCarrier","StarSystem":"CarrierSys","SystemAddress":2101,"MarketID":3700000001}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:02:00Z","event":"MultiSellExplorationData","BaseValue":750000,"Bonus":750,"TotalEarnings":750750,"Discovered":[{"SystemName":"CarrierSys","NumBodies":1}]}`)

	// Relog while standing on foot in the carrier Concourse (Frontier omits StationType and logs Docked:false, OnFoot:true),
	// ride an on-foot CarrierJump, and sell at the Concourse Universal Cartographics desk: 300,000 / 3 = 100,000 Cr tariff.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:10:00Z","event":"Location","Docked":false,"OnFoot":true,"StarSystem":"CarrierSys","SystemAddress":2101,"Body":"CarrierSys A","BodyType":"Star"}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:15:00Z","event":"CarrierJump","Docked":false,"OnFoot":true,"StarSystem":"JumpDestSys","SystemAddress":2104,"Body":"JumpDestSys 1","BodyType":"Planet"}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:18:00Z","event":"MultiSellExplorationData","BaseValue":300000,"Bonus":0,"TotalEarnings":300000,"Discovered":[{"SystemName":"ConcourseSys","NumBodies":1}]}`)

	// Undock from the carrier, then dock at an Orbis station: zero carrier tariff.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:25:00Z","event":"Undocked","StationName":"K9F-8TQ","StationType":"FleetCarrier","MarketID":3700000001}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:35:00Z","event":"Docked","StationName":"Galileo","StationType":"Orbis","StarSystem":"StationSys","SystemAddress":2102,"MarketID":3200000001}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-01T11:36:00Z","event":"MultiSellExplorationData","BaseValue":1000000,"Bonus":1000,"TotalEarnings":1001000,"Discovered":[{"SystemName":"StationSys","NumBodies":1}]}`)

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

func TestPopulatedAndPre2025OrganicEligibility(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Populated Bubble system entered prior to the query window: WasDiscovered and WasFootfalled are false,
	// yet Population > 0 must suppress first-discovery and first-logged bonuses and prevent false OrganicStolen.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-02T09:00:00Z","event":"FSDJump","StarSystem":"HR 1385","SystemAddress":3101,"JumpDist":15.0,"Population":4500000}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-02T09:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"HR 1385","SystemAddress":3101,"BodyID":2,"BodyName":"HR 1385 2","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false,"WasFootfalled":false}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-02T09:15:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Bacterial_Genus_Name;","Species":"$Codex_Ent_Bacterial_01_Name;","Species_Localised":"Bacterium Aurasus","SystemAddress":3101,"Body":2}`)

	// Unpopulated pre-2025 body (WasFootfalled omitted) that is completely untouched (!WasDiscovered and !WasMapped).
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2024-06-01T10:00:00Z","event":"FSDJump","StarSystem":"DeepUntouched","SystemAddress":3102,"JumpDist":40.0,"Population":0}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2024-06-01T10:05:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"DeepUntouched","SystemAddress":3102,"BodyID":1,"BodyName":"DeepUntouched 1","PlanetClass":"Rocky body","MassEM":0.2,"WasDiscovered":false,"WasMapped":false}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2024-06-01T10:10:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Stratum_Genus_Name;","Species":"$Codex_Ent_Stratum_07_Name;","Species_Localised":"Stratum Tectonicas","SystemAddress":3102,"Body":1}`)

	// Sell DeepUntouched Stratum with Bonus 0 (stolen) and populated Bacterium Aurasus with Bonus 0 (not stolen).
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2024-06-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-04-02T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`)

	populatedWindow := stats.Range{After: "2026-04-02T09:05:00Z", Until: "2026-04-02T10:00:00Z"}
	populatedMetrics, popMetricsErr := engineInstance.Compute(populatedWindow, nil)
	if popMetricsErr != nil {
		t.Fatalf("Compute populated window failed: %v", popMetricsErr)
	}
	if populatedMetrics.FirstDiscoveries != 0 || populatedMetrics.FirstLogged != 0 || populatedMetrics.OrganicValueBonus != 0 {
		t.Errorf("expected zero first bonuses in preloaded populated system, got %+v", populatedMetrics)
	}
	populatedRealization, popRealErr := engineInstance.ComputeRealization(populatedWindow, nil)
	if popRealErr != nil {
		t.Fatalf("ComputeRealization populated window failed: %v", popRealErr)
	}
	if populatedRealization.OrganicStolen != 0 {
		t.Errorf("expected OrganicStolen 0 in populated system, got %d", populatedRealization.OrganicStolen)
	}

	pre2025Window := stats.Range{After: "2024-06-01T09:55:00Z", Until: "2024-06-01T11:00:00Z"}
	pre2025Metrics, preMetricsErr := engineInstance.Compute(pre2025Window, nil)
	if preMetricsErr != nil {
		t.Fatalf("Compute pre-2025 window failed: %v", preMetricsErr)
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
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Parent body scanned prior to cgWindow, followed by three Radicoida Unica scans during the November 2025 CG.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2025-11-24T17:50:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"HIP 87621","SystemAddress":147882789259,"BodyID":3,"BodyName":"HIP 87621 2 a","PlanetClass":"Rocky body","MassEM":0.1,"WasDiscovered":true,"WasMapped":true,"WasFootfalled":true}`)
	for _, scanTimestamp := range []string{"2025-11-24T18:46:36Z", "2025-11-24T18:50:00Z", "2025-11-24T18:55:00Z"} {
		ingestTestLine(t, databaseConnection,
			`{"timestamp":"`+scanTimestamp+`","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Genus_Localised":"Radicoida","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","SystemAddress":147882789259,"Body":3}`)
	}

	// Frontier aggregates all 3 samples into one BioData entry (3 * 952,296 = 2,856,888 Cr) at Exogene Sciences.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2025-11-24T19:12:26Z","event":"SellOrganicData","MarketID":129038712,"BioData":[{"Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","Value":2856888,"Bonus":0}]}`)

	// Subsequent death must not treat the 2 extra sold CG samples as unsold death losses.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2025-11-24T20:00:00Z","event":"Resurrect","Option":"free","Cost":0,"Bankrupt":false}`)

	// Post-CG scan in 2026 uses permanent 119,037 Cr base value.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2026-02-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Ingensradices_Genus_Name;","Species":"$Codex_Ent_Ingensradices_Unicus_Name;","Species_Localised":"Radicoida Unica","SystemAddress":147882789259,"Body":3}`)

	cgWindow := stats.Range{After: "2025-11-24T18:00:00Z", Until: "2025-11-24T21:00:00Z"}
	cgMetrics, cgMetricsErr := engineInstance.Compute(cgWindow, nil)
	if cgMetricsErr != nil {
		t.Fatalf("Compute CG window failed: %v", cgMetricsErr)
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

	postCGMetrics, postErr := engineInstance.Compute(stats.Range{After: "2026-02-01T09:00:00Z", Until: "2026-02-01T11:00:00Z"}, nil)
	if postErr != nil {
		t.Fatalf("Compute post-CG window failed: %v", postErr)
	}
	if postCGMetrics.OrganicValueTotal != 119037 {
		t.Errorf("expected post-CG Radicoida Unica value 119,037 Cr, got %d", postCGMetrics.OrganicValueTotal)
	}
}

func TestSessionOdysseyAndPreUpdate14Realization(t *testing.T) {
	databaseConnection := setupRealizationDB(t)
	engineInstance := stats.NewEngine(databaseConnection)

	// Horizons session (Odyssey omitted / false) before sub-window start.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2020-05-01T10:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Asp"}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2020-05-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"HorizonsSys","SystemAddress":4101,"BodyID":1,"BodyName":"HorizonsSys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2020-05-01T10:12:00Z","event":"SAAScanComplete","SystemAddress":4101,"BodyID":1,"BodyName":"HorizonsSys 1","ProbesUsed":5,"EfficiencyTarget":6}`)

	// Odyssey session mapping an identical body.
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2022-09-20T01:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Asp","Odyssey":true}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2022-09-20T01:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"OdysseySys","SystemAddress":4102,"BodyID":1,"BodyName":"OdysseySys 1","PlanetClass":"Earthlike body","MassEM":1.0,"WasDiscovered":true,"WasMapped":true}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2022-09-20T01:12:00Z","event":"SAAScanComplete","SystemAddress":4102,"BodyID":1,"BodyName":"OdysseySys 1","ProbesUsed":5,"EfficiencyTarget":6}`)

	// Pre-Update-14 organic scan on untouched body sold prior to Update 14 with 1x bonus (2x total).
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2022-09-20T02:00:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"LegacyBioSys","SystemAddress":4103,"BodyID":2,"BodyName":"LegacyBioSys 2","PlanetClass":"Rocky body","MassEM":0.3,"WasDiscovered":false,"WasMapped":false}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2022-09-20T02:08:22Z","event":"ScanOrganic","ScanType":"Analyse","Genus":"$Codex_Ent_Clypeus_Genus_Name;","Species":"$Codex_Ent_Clypeus_03_Name;","Species_Localised":"Clypeus Speculumi","SystemAddress":4103,"Body":2}`)
	ingestTestLine(t, databaseConnection,
		`{"timestamp":"2022-10-05T00:11:12Z","event":"SellOrganicData","MarketID":3701316096,"BioData":[{"Species":"$Codex_Ent_Clypeus_03_Name;","Species_Localised":"Clypeus Speculumi","Value":711500,"Bonus":711500}]}`)

	horizonsMetrics, horErr := engineInstance.Compute(stats.Range{After: "2020-05-01T10:05:00Z", Until: "2020-05-01T10:30:00Z"}, nil)
	if horErr != nil {
		t.Fatalf("Compute Horizons window failed: %v", horErr)
	}
	odysseyMetrics, odyErr := engineInstance.Compute(stats.Range{After: "2022-09-20T01:05:00Z", Until: "2022-09-20T01:30:00Z"}, nil)
	if odyErr != nil {
		t.Fatalf("Compute Odyssey window failed: %v", odyErr)
	}
	if horizonsMetrics.ScanValueTotal >= odysseyMetrics.ScanValueTotal {
		t.Errorf("expected Horizons mapped value (%d) < Odyssey mapped value (%d)", horizonsMetrics.ScanValueTotal, odysseyMetrics.ScanValueTotal)
	}

	legacyBioWindow := stats.Range{After: "2022-09-20T01:50:00Z", Until: "2022-09-20T02:30:00Z"}
	legacyBioMetrics, bioErr := engineInstance.Compute(legacyBioWindow, nil)
	if bioErr != nil {
		t.Fatalf("Compute legacy bio window failed: %v", bioErr)
	}
	if legacyBioMetrics.OrganicValueBase != 711500 || legacyBioMetrics.OrganicValueBonus != 711500 || legacyBioMetrics.OrganicValueTotal != 1423000 {
		t.Errorf("expected pre-Update-14 Clypeus Speculumi 711,500 + 711,500 = 1,423,000, got %+v", legacyBioMetrics)
	}
}
