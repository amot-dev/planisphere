package app

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"planisphere/internal/replay"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func findCommanderByKey(commanders []replay.Commander, profileKey string) (replay.Commander, bool) {
	for _, cmdr := range commanders {
		if cmdr.ProfileKey == profileKey {
			return cmdr, true
		}
	}
	return replay.Commander{}, false
}

func TestCommanderAPIsCareerBaselineScopeAndCanonicalExportNames(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDir := testingContext.TempDir()

	// Seed journal log with:
	// - Alt account (F202, "CMDR EarlyAlt", 2024 career start with 120 Ly pre-expedition jump)
	// - Pre-FID historic log ("LegacyAlias", empty FID, 2025-01 jump of 30 Ly)
	// - Main account (F101, renamed from "Trebor Old" in 2025-03 to "Danielle Current" in 2026-01, flying an expedition Sol -> Colonia)
	journalLines := []string{
		`{"timestamp":"2024-02-01T08:00:00Z","event":"Fileheader","part":1,"language":"English/UK","Odyssey":true,"gameversion":"4.0.0.1800","build":"r301234/r0 "}`,
		`{"timestamp":"2024-02-01T08:01:00Z","event":"LoadGame","Commander":"CMDR EarlyAlt","FID":"F202","Ship":"cobramkiii","Ship_Localised":"Cobra Mk III","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2024-02-01T08:10:00Z","event":"FSDJump","StarSystem":"Altia","SystemAddress":8001,"StarPos":[0,120,0],"JumpDist":120.0}`,

		`{"timestamp":"2025-01-10T09:00:00Z","event":"FSDJump","StarSystem":"OldSector","SystemAddress":7001,"StarPos":[30,0,0],"JumpDist":30.0}`,
	}
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2024-02-01T080000.01.log"), []byte(joinLines(journalLines[:3])), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	// Pre-v3 style file with Commander event carrying empty FID for LegacyAlias.
	preFIDLines := []string{
		`{"timestamp":"2025-01-10T08:59:00Z","event":"Commander","Name":"LegacyAlias"}`,
		journalLines[3],
	}
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2025-01-10T085900.01.log"), []byte(joinLines(preFIDLines)), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	mainLines := []string{
		`{"timestamp":"2025-03-01T10:00:00Z","event":"Fileheader","part":1,"language":"English/UK","Odyssey":true,"gameversion":"4.0.0.1800","build":"r301234/r0 "}`,
		`{"timestamp":"2025-03-01T10:01:00Z","event":"LoadGame","Commander":"Trebor Old","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2025-03-01T10:05:00Z","event":"FSDJump","StarSystem":"Barnards Star","SystemAddress":1002,"StarPos":[-3,1,4],"JumpDist":50.0}`,
		`{"timestamp":"2026-01-15T09:00:00Z","event":"LoadGame","Commander":"Danielle Current","FID":"F101","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-01-15T09:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-15T10:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":6006,"StarPos":[-9530,-910,19808],"JumpDist":200.0}`,
		`{"timestamp":"2026-01-15T18:00:00Z","event":"LoadGame","Commander":"Danielle Current","FID":"F101","Ship":"anaconda","Ship_Localised":"Anaconda","ShipID":2,"Odyssey":true}`,
		`{"timestamp":"2026-01-15T18:01:00Z","event":"Location","StarSystem":"Colonia","SystemAddress":6006,"StarPos":[-9530,-910,19808]}`,
	}
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2025-03-01T100000.01.log"), []byte(joinLines(mainLines)), 0o600); err != nil {
		testingContext.Fatal(err)
	}

	if _, err := testApp.SetupConfigureJournals(journalDir); err != nil {
		testingContext.Fatalf("SetupConfigureJournals: %v", err)
	}
	if _, err := testApp.CompleteSetup(); err != nil {
		testingContext.Fatalf("CompleteSetup: %v", err)
	}

	initialSettings, err := testApp.GetSettings()
	if err != nil {
		testingContext.Fatalf("GetSettings: %v", err)
	}
	if initialSettings.CareerBaselineScope != settings.CareerScopeExpedition {
		testingContext.Fatalf("expected default CareerBaselineScope=%q, got %q", settings.CareerScopeExpedition, initialSettings.CareerBaselineScope)
	}

	// Merge pre-FID LegacyAlias into live:f101:era:0 ("Danielle Current").
	mergedSettings, err := testApp.MergeCommanders("live:name:legacyalias:era:0", "live:f101:era:0")
	if err != nil {
		testingContext.Fatalf("MergeCommanders: %v", err)
	}
	mainProfile, ok := findCommanderByKey(mergedSettings.Commanders, "live:f101:era:0")
	if !ok || mainProfile.Name != "Danielle Current" || len(mainProfile.Aliases) != 3 {
		testingContext.Fatalf("expected Danielle Current with 3 aliases after merge, got %+v", mainProfile)
	}

	// Create and start an expedition scoped to Danielle Current (mainProfile.ID).
	createdExp, err := testApp.CreateExpedition("Scoped Expedition", "")
	if err != nil {
		testingContext.Fatalf("CreateExpedition: %v", err)
	}
	planBefore, err := testApp.GetPlan(createdExp.ID)
	if err != nil {
		testingContext.Fatal(err)
	}
	solName, coloniaName := "Sol", "Colonia"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: planBefore.Rows[0].Row.ID, SystemName: &solName}); err != nil {
		testingContext.Fatal(err)
	}
	coloniaRow, err := testApp.AddRow(createdExp.ID, planBefore.Rows[0].Row.ID, expedition.RowWaypoint, 0)
	if err != nil {
		testingContext.Fatal(err)
	}
	if err := testApp.UpdateRow(expedition.RowPatch{ID: coloniaRow.ID, SystemName: &coloniaName}); err != nil {
		testingContext.Fatal(err)
	}
	if err := testApp.StartExpeditionScoped(createdExp.ID, "2026-01-15T09:05:00Z", 0, expedition.ScopeSelected, []int64{mainProfile.ID}); err != nil {
		testingContext.Fatalf("StartExpeditionScoped: %v", err)
	}

	// Under default CareerBaselineScope("expedition"), AllJournals, CareerStart, and OriginBaseline reflect ONLY Danielle Current (30+50+200=280 Ly career, 80 Ly origin baseline, 2025-01-10 career start).
	sidebarScoped, err := testApp.GetSidebarStats(createdExp.ID, stats.Filter{})
	if err != nil {
		testingContext.Fatalf("GetSidebarStats(expedition scope): %v", err)
	}
	if sidebarScoped.CareerStart != "2025-01-10T08:59:00Z" || math.Abs(sidebarScoped.AllJournals.DistanceLy-280.0) > 0.01 || math.Abs(sidebarScoped.Live.DistanceLy-200.0) > 0.01 {
		testingContext.Fatalf("unexpected sidebar under expedition career scope: start=%q all=%.1f live=%.1f",
			sidebarScoped.CareerStart, sidebarScoped.AllJournals.DistanceLy, sidebarScoped.Live.DistanceLy)
	}

	exportScoped, err := testApp.GetExpeditionExportData(createdExp.ID)
	if err != nil {
		testingContext.Fatalf("GetExpeditionExportData(expedition scope): %v", err)
	}
	// Export.Commanders must contain ONLY canonical latest display_name, NEVER Trebor Old or LegacyAlias!
	if !reflect.DeepEqual(exportScoped.Commanders, []string{"Danielle Current"}) {
		testingContext.Fatalf("expected canonical Commanders=[Danielle Current], got %v", exportScoped.Commanders)
	}
	if exportScoped.CareerStart != "2025-01-10T08:59:00Z" || exportScoped.OriginBaseline == nil || math.Abs(exportScoped.OriginBaseline.Metrics.DistanceLy-80.0) > 0.01 {
		testingContext.Fatalf("unexpected export baseline under expedition career scope: start=%q origin=%+v",
			exportScoped.CareerStart, exportScoped.OriginBaseline)
	}

	// Switch to SetCareerBaselineScope("all_enabled"): AllJournals, CareerStart, and OriginBaseline now include CMDR EarlyAlt (+120 Ly, 2024-02-01), while Live remains 200 Ly!
	var emittedDomains []string
	var emitMu sync.Mutex
	testApp.emit = func(eventName string, payload ...any) {
		if eventName == EventDataChanged && len(payload) > 0 {
			if changed, ok := payload[0].(DataChanged); ok {
				emitMu.Lock()
				emittedDomains = append(emittedDomains, changed.Domains...)
				emitMu.Unlock()
			}
		}
	}
	updatedScopeSettings, err := testApp.SetCareerBaselineScope(settings.CareerScopeAllEnabled)
	if err != nil || updatedScopeSettings.CareerBaselineScope != settings.CareerScopeAllEnabled {
		testingContext.Fatalf("SetCareerBaselineScope(all_enabled): settings=%+v err=%v", updatedScopeSettings, err)
	}
	emitMu.Lock()
	if !slices.Contains(emittedDomains, "stats") || !slices.Contains(emittedDomains, "plan") || !slices.Contains(emittedDomains, "settings") {
		testingContext.Fatalf("expected stats, plan, and settings domains on SetCareerBaselineScope, got %v", emittedDomains)
	}
	emitMu.Unlock()

	sidebarAllEnabled, err := testApp.GetSidebarStats(createdExp.ID, stats.Filter{})
	if err != nil {
		testingContext.Fatalf("GetSidebarStats(all_enabled): %v", err)
	}
	if sidebarAllEnabled.CareerStart != "2024-02-01T08:01:00Z" || math.Abs(sidebarAllEnabled.AllJournals.DistanceLy-400.0) > 0.01 || math.Abs(sidebarAllEnabled.Live.DistanceLy-200.0) > 0.01 {
		testingContext.Fatalf("unexpected sidebar under all_enabled career scope: start=%q all=%.1f live=%.1f",
			sidebarAllEnabled.CareerStart, sidebarAllEnabled.AllJournals.DistanceLy, sidebarAllEnabled.Live.DistanceLy)
	}
	exportAllEnabled, err := testApp.GetExpeditionExportData(createdExp.ID)
	if err != nil {
		testingContext.Fatalf("GetExpeditionExportData(all_enabled): %v", err)
	}
	if exportAllEnabled.CareerStart != "2024-02-01T08:01:00Z" || exportAllEnabled.OriginBaseline == nil || math.Abs(exportAllEnabled.OriginBaseline.Metrics.DistanceLy-200.0) > 0.01 || math.Abs(exportAllEnabled.Live.DistanceLy-200.0) > 0.01 {
		testingContext.Fatalf("unexpected export under all_enabled career scope: start=%q origin=%+v live=%.1f",
			exportAllEnabled.CareerStart, exportAllEnabled.OriginBaseline, exportAllEnabled.Live.DistanceLy)
	}

	// UpdateExpeditionScoped can switch expedition scope to all_enabled and back.
	if err := testApp.UpdateExpeditionScoped(createdExp.ID, "Scoped Renamed", "Updated", expedition.ScopeAllEnabled, nil); err != nil {
		testingContext.Fatalf("UpdateExpeditionScoped(all_enabled): %v", err)
	}
	exportBothCmdrs, err := testApp.GetExpeditionExportData(createdExp.ID)
	if err != nil || !reflect.DeepEqual(exportBothCmdrs.Commanders, []string{"CMDR EarlyAlt", "Danielle Current"}) {
		testingContext.Fatalf("expected [CMDR EarlyAlt, Danielle Current] after switching expedition to all_enabled, got %v err=%v",
			exportBothCmdrs.Commanders, err)
	}

	// Round-trip ListCommanderLoadGamesOnDate, SplitCommander, UnmergeCommanderAlias, and ResetCommanderOverrides.
	sessions, err := testApp.ListCommanderLoadGamesOnDate("live:f101:era:0", "2026-01-15")
	if err != nil || len(sessions) != 2 || sessions[0].Ship != "Mandalay" || sessions[0].SystemName != "Sol" || sessions[1].Ship != "Anaconda" || sessions[1].SystemName != "Colonia" {
		testingContext.Fatalf("unexpected LoadGame sessions on 2026-01-15: %+v err=%v", sessions, err)
	}
	emitMu.Lock()
	emittedDomains = nil
	emitMu.Unlock()
	splitSettings, err := testApp.SplitCommander("live:f101:era:0", sessions[1].Timestamp)
	if err != nil {
		testingContext.Fatalf("SplitCommander: %v", err)
	}
	emitMu.Lock()
	if !slices.Contains(emittedDomains, "expeditions") || !slices.Contains(emittedDomains, "settings") || !slices.Contains(emittedDomains, "stats") {
		testingContext.Fatalf("expected override mutation to emit expeditions, settings, and stats domains, got %v", emittedDomains)
	}
	emitMu.Unlock()
	if _, hasEra1 := findCommanderByKey(splitSettings.Commanders, "live:f101:era:1"); !hasEra1 {
		testingContext.Fatalf("expected live:f101:era:1 after SplitCommander, got %+v", splitSettings.Commanders)
	}
	unmergedSettings, err := testApp.UnmergeCommanderAlias("Trebor Old", "live:f101:era:0")
	if err != nil {
		testingContext.Fatalf("UnmergeCommanderAlias: %v", err)
	}
	if _, hasUnmerged := findCommanderByKey(unmergedSettings.Commanders, "unmerged:live:f101:trebor old:era:0"); !hasUnmerged {
		testingContext.Fatalf("expected unmerged Trebor Old profile, got %+v", unmergedSettings.Commanders)
	}
	resetSettings, err := testApp.ResetCommanderOverrides("live:f101:era:0")
	if err != nil {
		testingContext.Fatalf("ResetCommanderOverrides: %v", err)
	}
	restoredMain, ok := findCommanderByKey(resetSettings.Commanders, "live:f101:era:0")
	if !ok || restoredMain.HasManualOverride || len(resetSettings.Commanders) != 3 {
		testingContext.Fatalf("expected clean 3 natural profiles after ResetCommanderOverrides, got %+v", resetSettings.Commanders)
	}
}

func TestIngestingCareerWipeInvalidatesWarmCareerProjectionsAndExportSnapshot(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDir := testingContext.TempDir()

	preWipeLines := []string{
		`{"timestamp":"2026-02-01T10:00:00Z","event":"Fileheader","part":1,"language":"English/UK","Odyssey":true,"gameversion":"4.0.0.1800","build":"r301234/r0 "}`,
		`{"timestamp":"2026-02-01T10:01:00Z","event":"LoadGame","Commander":"CMDR WipeTest","FID":"F909","Ship":"mandalay","Ship_Localised":"Mandalay","ShipID":1,"Odyssey":true}`,
		`{"timestamp":"2026-02-01T10:05:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-02-01T10:15:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":2002,"StarPos":[40,0,0],"JumpDist":40.0}`,
		`{"timestamp":"2026-02-01T10:20:00Z","event":"Scan","ScanType":"Detailed","BodyName":"Alpha 1","BodyID":1,"StarSystem":"Alpha","SystemAddress":2002,"PlanetClass":"Water world","MassEM":1.0,"TerraformState":"","WasDiscovered":false,"WasMapped":false}`,
	}
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2026-02-01T100000.01.log"), []byte(joinLines(preWipeLines)), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.SetupConfigureJournals(journalDir); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.CompleteSetup(); err != nil {
		testingContext.Fatal(err)
	}

	exp, err := testApp.CreateExpedition("PreWipe Run", "")
	if err != nil {
		testingContext.Fatal(err)
	}
	planBefore, err := testApp.GetPlan(exp.ID)
	if err != nil {
		testingContext.Fatal(err)
	}
	solName, alphaName := "Sol", "Alpha"
	if err := testApp.UpdateRow(expedition.RowPatch{ID: planBefore.Rows[0].Row.ID, SystemName: &solName}); err != nil {
		testingContext.Fatal(err)
	}
	alphaRow, err := testApp.AddRow(exp.ID, planBefore.Rows[0].Row.ID, expedition.RowWaypoint, 0)
	if err != nil {
		testingContext.Fatal(err)
	}
	if err := testApp.UpdateRow(expedition.RowPatch{ID: alphaRow.ID, SystemName: &alphaName}); err != nil {
		testingContext.Fatal(err)
	}
	if err := testApp.StartExpedition(exp.ID, "2026-02-01T10:05:00Z", 0); err != nil {
		testingContext.Fatal(err)
	}

	// Warm CareerCartographics, ClosedWindowRevision, and lastExportSnapshot before the save wipe.
	statsBeforeWipe, err := testApp.GetSidebarStats(exp.ID, stats.Filter{})
	if err != nil || statsBeforeWipe.AllJournals.BodiesScanned != 1 {
		testingContext.Fatalf("expected 1 pre-wipe scanned body in warm AllJournals, got %+v err=%v", statsBeforeWipe.AllJournals, err)
	}
	if _, err := testApp.GetExpeditionExportData(exp.ID); err != nil {
		testingContext.Fatalf("warm export snapshot: %v", err)
	}
	ingestBefore, err := testApp.GetIngestStatus()
	if err != nil {
		testingContext.Fatal(err)
	}

	wipeLines := []string{
		`{"timestamp":"2026-03-01T12:00:00Z","event":"Fileheader","part":1,"language":"English/UK","Odyssey":true,"gameversion":"4.0.0.1800","build":"r301234/r0 "}`,
		`{"timestamp":"2026-03-01T12:00:01Z","event":"ClearSavedGame","Name":"CMDR WipeTest","FID":"F909"}`,
		`{"timestamp":"2026-03-01T12:00:05Z","event":"NewCommander","Name":"CMDR WipeTest","FID":"F909","Package":"Sidewinder"}`,
		`{"timestamp":"2026-03-01T12:01:00Z","event":"LoadGame","Commander":"CMDR WipeTest","FID":"F909","Ship":"SideWinder","Ship_Localised":"Sidewinder","ShipID":1,"Odyssey":true}`,
	}
	if err := os.WriteFile(filepath.Join(journalDir, "Journal.2026-03-01T120000.01.log"), []byte(joinLines(wipeLines)), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	if err := testApp.RescanJournals(); err != nil {
		testingContext.Fatalf("RescanJournals after wipe: %v", err)
	}

	if _, hasCachedExport := testApp.matchingExportSnapshot(exp.ID, ""); hasCachedExport {
		testingContext.Fatal("expected ClearSavedGame/NewCommander ingestion to clear cached export snapshot")
	}
	ingestAfter, err := testApp.GetIngestStatus()
	if err != nil || ingestAfter.ClosedWindowRevision <= ingestBefore.ClosedWindowRevision {
		testingContext.Fatalf("expected ClosedWindowRevision to advance after wipe (%d -> %d, err=%v)",
			ingestBefore.ClosedWindowRevision, ingestAfter.ClosedWindowRevision, err)
	}
	statsAfterWipe, err := testApp.GetSidebarStats(exp.ID, stats.Filter{})
	if err != nil || statsAfterWipe.AllJournals.BodiesScanned != 0 {
		testingContext.Fatalf("expected pre-wipe era:0 CareerCartographics evicted (BodiesScanned=0), got %+v err=%v",
			statsAfterWipe.AllJournals, err)
	}
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}
