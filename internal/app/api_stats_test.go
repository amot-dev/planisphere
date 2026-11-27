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

	unstartedSidebarStats, unstartedStatsError := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{Enabled: false})
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

	sidebarStats, statsError := testApp.GetSidebarStats(expeditionRecord.ID, stats.Filter{Enabled: false})
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
