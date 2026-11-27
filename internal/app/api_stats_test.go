package app

import (
	"testing"

	stats "planisphere/internal/replay/stats"
)

func TestGetSidebarStatsRealizationExpeditionVsAllTime(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	// Commander Jameson
	if _, insertCommanderError := testApp.db.Exec(`INSERT INTO commanders (name, enabled, first_seen) VALUES ('Jameson', 1, '2024-01-01T00:00:00Z')`); insertCommanderError != nil {
		testingContext.Fatalf("insert commander: %v", insertCommanderError)
	}

	// Scan 1: Before expedition (2025)
	insertTestJournalEvent(testingContext, testApp, "2025-01-01T10:00:00Z", "ScanOrganic", "1",
		`{"timestamp":"2025-01-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":111,"Body":1}`)
	insertTestJournalEvent(testingContext, testApp, "2025-01-01T12:00:00Z", "SellOrganicData", "1",
		`{"timestamp":"2025-01-01T12:00:00Z","event":"SellOrganicData","BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	// Create expedition (initially unstarted)
	expeditionRecord, createError := testApp.CreateExpedition("2026 Survey", "")
	if createError != nil {
		testingContext.Fatalf("create expedition: %v", createError)
	}

	unstartedSidebarStats, unstartedStatsError := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{Enabled: false}, true)
	if unstartedStatsError != nil {
		testingContext.Fatalf("GetSidebarStats (unstarted) failed: %v", unstartedStatsError)
	}
	if unstartedSidebarStats.Realization.TotalRealized != 0 {
		testingContext.Errorf("expected unstarted expedition Realization.TotalRealized 0, got %d", unstartedSidebarStats.Realization.TotalRealized)
	}
	if unstartedSidebarStats.AllJournalsRealization.TotalRealized != 19010800 {
		testingContext.Errorf("expected unstarted AllJournalsRealization.TotalRealized 19,010,800, got %d", unstartedSidebarStats.AllJournalsRealization.TotalRealized)
	}

	// Start expedition at 2025-12-31T23:55:00Z and depart WP #1 at 2026-01-01T00:00:00Z.
	if _, updateExpeditionError := testApp.db.Exec(`UPDATE expeditions SET status = 'active', start_timestamp = '2025-12-31T23:55:00Z' WHERE id = ?`, expeditionRecord.ID); updateExpeditionError != nil {
		testingContext.Fatalf("update expedition status: %v", updateExpeditionError)
	}
	if _, updateRowError := testApp.db.Exec(`UPDATE expedition_rows SET system_name = 'Sol', visited_at = '2025-12-31T23:55:00Z', departed_at = '2026-01-01T00:00:00Z' WHERE expedition_id = ?`, expeditionRecord.ID); updateRowError != nil {
		testingContext.Fatalf("update expedition origin row: %v", updateRowError)
	}

	// Scan 2: During expedition (2026)
	insertTestJournalEvent(testingContext, testApp, "2026-03-01T10:00:00Z", "ScanOrganic", "2",
		`{"timestamp":"2026-03-01T10:00:00Z","event":"ScanOrganic","ScanType":"Analyse","Species":"$Codex_Ent_Stratum_07_Name;","WasLogged":true,"SystemAddress":222,"Body":1}`)
	insertTestJournalEvent(testingContext, testApp, "2026-03-01T12:00:00Z", "SellOrganicData", "2",
		`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","BioData":[{"Species":"$Codex_Ent_Stratum_07_Name;","Value":19010800,"Bonus":0}]}`)

	sidebarStats, statsError := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{Enabled: false}, true)
	if statsError != nil {
		testingContext.Fatalf("GetSidebarStats failed: %v", statsError)
	}

	// Expedition realization should reflect only Scan 2 (19,010,800)
	if sidebarStats.Realization.TotalRealized != 19010800 {
		testingContext.Errorf("expected expedition Realization.TotalRealized 19,010,800, got %d", sidebarStats.Realization.TotalRealized)
	}

	// All journals realization should reflect Scan 1 + Scan 2 (38,021,600)
	if sidebarStats.AllJournalsRealization.TotalRealized != 38021600 {
		testingContext.Errorf("expected AllJournalsRealization.TotalRealized 38,021,600, got %d", sidebarStats.AllJournalsRealization.TotalRealized)
	}
}

func TestGetSidebarStatsLazyAllTimeLeavesCareerStatsUncomputed(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	insertTestJournalEvent(testingContext, testApp, "2025-05-01T09:00:00Z", "LoadGame", "lazy-load",
		`{"timestamp":"2025-05-01T09:00:00Z","event":"LoadGame","Commander":"CMDR Lazy","FID":"F777","Ship":"Mandalay","Odyssey":true}`)
	insertTestJournalEvent(testingContext, testApp, "2025-05-01T09:05:00Z", "FSDJump", "lazy-pre-jump",
		`{"timestamp":"2025-05-01T09:05:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":15.0}`)

	expeditionID := createAndStartExpedition(testingContext, testApp, "Lazy Sidebar Scope", "Sol")
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:05:00Z", "FSDJump", "lazy-depart",
		`{"timestamp":"2026-01-01T00:05:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,1,1],"JumpDist":4.3}`)

	// Warm careerStats initially via includeAllTime = true.
	initialAllTime, initialErr := testApp.GetSidebarStats(expeditionID, stats.Filter{}, true)
	if initialErr != nil {
		testingContext.Fatalf("initial GetSidebarStats(true) failed: %v", initialErr)
	}
	if !initialAllTime.AllTimeLoaded || initialAllTime.AllJournals.Jumps != 2 || initialAllTime.CareerStart != "2025-05-01T09:00:00Z" {
		testingContext.Fatalf("unexpected initial All Time sidebar: %+v", initialAllTime)
	}
	if !testApp.careerStats.IsValid() {
		testingContext.Fatal("expected careerStats to be warm after GetSidebarStats(true)")
	}

	// Ingest live FSDJump and Scan events, invalidating CareerTravel and CareerCartographics.
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:10:00Z", "FSDJump", "lazy-live-jump",
		`{"timestamp":"2026-01-01T00:10:00Z","event":"FSDJump","StarSystem":"Barnards Star","SystemAddress":1003,"StarPos":[2,2,2],"JumpDist":6.0}`)
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:12:00Z", "Scan", "lazy-live-scan",
		`{"timestamp":"2026-01-01T00:12:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Barnards Star 1","BodyID":1,"StarSystem":"Barnards Star","SystemAddress":1003,"PlanetClass":"Water world","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`)

	if testApp.careerStats.Travel().IsValid() || testApp.careerStats.Cartographics().IsValid() || testApp.careerStats.IsValid() {
		testingContext.Fatal("expected live FSDJump and Scan events to invalidate career travel and cartographics projections")
	}

	// Default Expedition-tab refresh (includeAllTime = false) updates expedition telemetry without recomputing CareerStats.
	expeditionOnly, expOnlyErr := testApp.GetSidebarStats(expeditionID, stats.Filter{}, false)
	if expOnlyErr != nil {
		testingContext.Fatalf("GetSidebarStats(false) failed: %v", expOnlyErr)
	}
	if expeditionOnly.AllTimeLoaded {
		testingContext.Fatal("expected AllTimeLoaded=false when includeAllTime=false")
	}
	if expeditionOnly.AllJournals != (stats.Metrics{}) || expeditionOnly.AllJournalsRealization != (stats.RealizationBreakdown{}) {
		testingContext.Fatalf("expected zero AllJournals and AllJournalsRealization when includeAllTime=false, got metrics=%+v realization=%+v",
			expeditionOnly.AllJournals, expeditionOnly.AllJournalsRealization)
	}
	if expeditionOnly.CareerStart != "" || expeditionOnly.CareerStartGalactic != "" || expeditionOnly.CareerDuration != "" {
		testingContext.Fatalf("expected empty CareerStart fields when includeAllTime=false, got start=%q gal=%q dur=%q",
			expeditionOnly.CareerStart, expeditionOnly.CareerStartGalactic, expeditionOnly.CareerDuration)
	}
	if expeditionOnly.Live.Jumps != 2 || expeditionOnly.Live.BodiesScanned != 1 {
		testingContext.Fatalf("expected expedition Live metrics to update (Jumps=2, BodiesScanned=1), got %+v", expeditionOnly.Live)
	}
	if testApp.careerStats.Travel().IsValid() || testApp.careerStats.Cartographics().IsValid() || testApp.careerStats.IsValid() {
		testingContext.Fatal("expected GetSidebarStats(false) to leave invalidated CareerStats untouched")
	}

	// Lazy All Time fetch (includeAllTime = true) computes CareerStats and CareerStartScoped on demand.
	loadedAllTime, allTimeErr := testApp.GetSidebarStats(expeditionID, stats.Filter{}, true)
	if allTimeErr != nil {
		testingContext.Fatalf("GetSidebarStats(true) failed: %v", allTimeErr)
	}
	if !loadedAllTime.AllTimeLoaded {
		testingContext.Fatal("expected AllTimeLoaded=true when includeAllTime=true")
	}
	if loadedAllTime.AllJournals.Jumps != 3 || loadedAllTime.AllJournals.BodiesScanned != 1 {
		testingContext.Fatalf("expected AllJournals Jumps=3 BodiesScanned=1, got %+v", loadedAllTime.AllJournals)
	}
	if loadedAllTime.CareerStart != "2025-05-01T09:00:00Z" || loadedAllTime.CareerStartGalactic == "" || loadedAllTime.CareerDuration == "" {
		testingContext.Fatalf("expected populated CareerStart metadata when includeAllTime=true, got %+v", loadedAllTime)
	}
	if !testApp.careerStats.IsValid() {
		testingContext.Fatal("expected GetSidebarStats(true) to warm CareerStats")
	}
}
