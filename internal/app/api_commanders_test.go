package app

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"planisphere/internal/replay"
	stats "planisphere/internal/replay/stats"
	"planisphere/internal/sources/journal"
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

	// Merge pre-FID LegacyAlias into live:f101:era:0 ("Danielle Current").
	mergedSettings, err := testApp.MergeCommanders("live:name:legacyalias:era:0", "live:f101:era:0")
	if err != nil {
		testingContext.Fatalf("MergeCommanders: %v", err)
	}
	mainProfile, ok := findCommanderByKey(mergedSettings.Commanders, "live:f101:era:0")
	if !ok {
		testingContext.Fatalf("expected live:f101:era:0 profile after merge, got %+v", mergedSettings.Commanders)
	}
	altProfile, ok := findCommanderByKey(mergedSettings.Commanders, "live:f202:era:0")
	if !ok {
		testingContext.Fatalf("expected live:f202:era:0 profile, got %+v", mergedSettings.Commanders)
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
	if err := testApp.StartExpeditionScoped(createdExp.ID, "2026-01-15T09:05:00Z", 0, mainProfile.ID); err != nil {
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
	eventsRecorder := captureAppEvents(testingContext, testApp)
	updatedScopeSettings, err := testApp.SetCareerBaselineScope(settings.CareerScopeAllEnabled)
	if err != nil || updatedScopeSettings.CareerBaselineScope != settings.CareerScopeAllEnabled {
		testingContext.Fatalf("SetCareerBaselineScope(all_enabled): settings=%+v err=%v", updatedScopeSettings, err)
	}
	scopeDomains := eventsRecorder.Domains()
	if !slices.Contains(scopeDomains, "stats") || !slices.Contains(scopeDomains, "plan") || !slices.Contains(scopeDomains, "settings") {
		testingContext.Fatalf("expected stats, plan, and settings domains on SetCareerBaselineScope, got %v", scopeDomains)
	}

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

	// UpdateExpeditionScoped can switch the expedition's single commander save-slot profile and back.
	if err := testApp.UpdateExpeditionScoped(createdExp.ID, "Scoped Renamed", "Updated", altProfile.ID); err != nil {
		testingContext.Fatalf("UpdateExpeditionScoped(altProfile): %v", err)
	}
	reloadedAltPlan, err := testApp.GetPlan(createdExp.ID)
	if err != nil || reloadedAltPlan.Expedition.CommanderProfileID != altProfile.ID {
		testingContext.Fatalf("expected CommanderProfileID=%d after switching to alt, got %+v err=%v", altProfile.ID, reloadedAltPlan.Expedition, err)
	}
	if err := testApp.UpdateExpeditionScoped(createdExp.ID, "Scoped Renamed", "Updated", mainProfile.ID); err != nil {
		testingContext.Fatalf("UpdateExpeditionScoped(mainProfile): %v", err)
	}

	// Verify ListCommanderLoadGamesOnDate delegation and DataChanged domain emissions on commander override mutations.
	sessions, err := testApp.ListCommanderLoadGamesOnDate("live:f101:era:0", "2026-01-15")
	if err != nil || len(sessions) != 2 || sessions[0].Ship != "Mandalay" || sessions[0].SystemName != "Sol" || sessions[1].Ship != "Anaconda" || sessions[1].SystemName != "Colonia" {
		testingContext.Fatalf("unexpected LoadGame sessions on 2026-01-15: %+v err=%v", sessions, err)
	}
	eventsRecorder.Reset()
	if _, err := testApp.SplitCommander("live:f101:era:0", sessions[1].Timestamp); err != nil {
		testingContext.Fatalf("SplitCommander: %v", err)
	}
	splitDomains := eventsRecorder.Domains()
	if !slices.Contains(splitDomains, "expeditions") || !slices.Contains(splitDomains, "settings") || !slices.Contains(splitDomains, "stats") {
		testingContext.Fatalf("expected override mutation to emit expeditions, settings, and stats domains, got %v", splitDomains)
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

	// Warm all-enabled CareerCartographics, ClosedWindowRevision, and lastExportSnapshot before the save wipe.
	if _, err := testApp.SetCareerBaselineScope(settings.CareerScopeAllEnabled); err != nil {
		testingContext.Fatalf("SetCareerBaselineScope(all_enabled): %v", err)
	}
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

func TestByteZeroBackfillAndStatisticsCareerWipeBusInvalidation(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journalDir := testingContext.TempDir()
	logFilename := "Journal.2026-04-10T100000.01.log"
	logPath := filepath.Join(journalDir, logFilename)

	initialLines := []string{
		`{"timestamp":"2026-04-10T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-04-10T10:01:00Z","event":"LoadGame","Commander":"CMDR Heal","FID":"F410","Ship":"Mandalay","Odyssey":true}`,
		`{"timestamp":"2026-04-10T10:02:00Z","event":"Statistics","Exploration":{"Systems_Visited":50,"Total_Hyperspace_Jumps":45}}`,
		`{"timestamp":"2026-04-10T10:05:00Z","event":"FSDJump","StarSystem":"Alpha","SystemAddress":2002,"StarPos":[25,0,0],"JumpDist":25.0}`,
	}
	if err := os.WriteFile(logPath, []byte(joinLines(initialLines)), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.SetupConfigureJournals(journalDir); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.CompleteSetup(); err != nil {
		testingContext.Fatal(err)
	}

	// Disable CMDR Heal, then simulate an unattributed pre-header jump already stored in journal_events with byte_offset reset to 0.
	if _, err := testApp.SetCommanderEnabled("live:f410:era:0", false); err != nil {
		testingContext.Fatalf("SetCommanderEnabled(false): %v", err)
	}
	unattribLine := `{"timestamp":"2026-04-10T10:00:30Z","event":"FSDJump","StarSystem":"PreHeaderSys","SystemAddress":3003,"StarPos":[10,0,0],"JumpDist":10.0}`
	rewrittenLines := []string{initialLines[0], unattribLine, initialLines[1], initialLines[2], initialLines[3]}
	if err := os.WriteFile(logPath, []byte(joinLines(rewrittenLines)), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	parsedUnattrib, _, parseErr := journal.ParseLine([]byte(unattribLine), logFilename)
	if parseErr != nil {
		testingContext.Fatal(parseErr)
	}
	if _, err := testApp.db.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, commander, fid, universe, profile_id, source_file, raw_json, ingested_at)
		 VALUES (?, '2026-04-10T10:00:30Z', 'FSDJump', 'PreHeaderSys', 3003, NULL, '', 'live', 0, ?, ?, '2026-04-10T10:05:00Z')`,
		parsedUnattrib.UID, logFilename, unattribLine,
	); err != nil {
		testingContext.Fatalf("seed byte-zero unattributed jump: %v", err)
	}
	if _, err := testApp.db.Exec(
		`UPDATE ingested_journals SET byte_offset = 0, line_count = 0 WHERE file_name = ?`,
		logFilename,
	); err != nil {
		testingContext.Fatalf("reset byte_offset to zero: %v", err)
	}

	// Warm caches while the unattributed jump is visible (profile_id=0 bypasses disabled CMDR Heal).
	exp, err := testApp.CreateExpedition("Heal Check", "")
	if err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.SetCareerBaselineScope(settings.CareerScopeAllEnabled); err != nil {
		testingContext.Fatal(err)
	}
	warmStatsBeforeHeal, err := testApp.GetSidebarStats(exp.ID, stats.Filter{})
	if err != nil || warmStatsBeforeHeal.AllJournals.Jumps != 1 {
		testingContext.Fatalf("expected 1 unattributed jump before byte-zero heal, got %+v err=%v", warmStatsBeforeHeal.AllJournals, err)
	}
	countsBeforeHeal, err := testApp.systemVisitCounts.CountsForSystems([]string{"PreHeaderSys"})
	if err != nil || countsBeforeHeal["preheadersys"] != 1 {
		testingContext.Fatalf("expected PreHeaderSys visit count 1 before byte-zero heal, got %v err=%v", countsBeforeHeal, err)
	}
	ledgerGenBefore := testApp.salesLedger.Generation()
	eventsRecorder := captureAppEvents(testingContext, testApp)

	// RescanJournals from byte 0 inserts 0 new events while backfilling CMDR Heal onto PreHeaderSys.
	if err := testApp.RescanJournals(); err != nil {
		testingContext.Fatalf("RescanJournals byte-zero heal: %v", err)
	}
	healDomains := eventsRecorder.Domains()
	for _, wantDomain := range []string{"settings", "stats", "plan", "ingest", "expeditions"} {
		if !slices.Contains(healDomains, wantDomain) {
			testingContext.Fatalf("expected byte-zero heal to emit %q domain, got %v", wantDomain, healDomains)
		}
	}
	if testApp.salesLedger.Generation() <= ledgerGenBefore {
		testingContext.Fatal("expected byte-zero heal to invalidate SalesLedger")
	}
	statsAfterHeal, err := testApp.GetSidebarStats(exp.ID, stats.Filter{})
	if err != nil || statsAfterHeal.AllJournals.Jumps != 0 {
		testingContext.Fatalf("expected healed jump on disabled commander excluded from AllJournals, got %+v err=%v", statsAfterHeal.AllJournals, err)
	}
	countsAfterHeal, err := testApp.systemVisitCounts.CountsForSystems([]string{"PreHeaderSys"})
	if err != nil || countsAfterHeal["preheadersys"] != 0 {
		testingContext.Fatalf("expected PreHeaderSys visit count 0 after byte-zero heal, got %v err=%v", countsAfterHeal, err)
	}

	// Re-enable CMDR Heal, warm projections, verify routine Statistics does NOT emit settings/plan, then verify Statistics career-wipe split DOES.
	if _, err := testApp.SetCommanderEnabled("live:f410:era:0", true); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := testApp.GetSidebarStats(exp.ID, stats.Filter{}); err != nil {
		testingContext.Fatal(err)
	}
	closedRevBeforeRoutine := testApp.timeRangeStats.ClosedWindowRevision()
	eventsRecorder.Reset()

	routineLine := `{"timestamp":"2026-04-10T10:10:00Z","event":"Statistics","Exploration":{"Systems_Visited":52,"Total_Hyperspace_Jumps":47}}` + "\n"
	if err := os.WriteFile(logPath, []byte(joinLines(rewrittenLines)+routineLine), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	routineRes, err := testApp.ingester.IngestFile(logPath)
	if err != nil || routineRes.ProfilesChanged {
		testingContext.Fatalf("routine Statistics IngestFile = (%+v, %v)", routineRes, err)
	}
	testApp.onJournalBatch(routineRes)
	routineDomains := eventsRecorder.Domains()
	if slices.Contains(routineDomains, "settings") || slices.Contains(routineDomains, "plan") || testApp.timeRangeStats.ClosedWindowRevision() != closedRevBeforeRoutine {
		testingContext.Fatalf("expected routine Statistics not to trigger commander/plan invalidation, got domains=%v rev=%d->%d",
			routineDomains, closedRevBeforeRoutine, testApp.timeRangeStats.ClosedWindowRevision())
	}

	eventsRecorder.Reset()
	wipeLogPath := filepath.Join(journalDir, "Journal.2026-04-11T090000.01.log")
	wipeSession := joinLines([]string{
		`{"timestamp":"2026-04-11T09:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
		`{"timestamp":"2026-04-11T09:00:01Z","event":"Commander","Name":"CMDR Heal","FID":"F410"}`,
		`{"timestamp":"2026-04-11T09:00:02Z","event":"LoadGame","Commander":"CMDR Heal","FID":"F410","Ship":"SideWinder","Odyssey":true}`,
		`{"timestamp":"2026-04-11T09:00:03Z","event":"Statistics","Exploration":{"Systems_Visited":1,"Total_Hyperspace_Jumps":0}}`,
	})
	if err := os.WriteFile(wipeLogPath, []byte(wipeSession), 0o600); err != nil {
		testingContext.Fatal(err)
	}
	wipeRes, err := testApp.ingester.IngestFile(wipeLogPath)
	if err != nil || !wipeRes.ProfilesChanged {
		testingContext.Fatalf("Statistics wipe IngestFile = (%+v, %v), want ProfilesChanged=true", wipeRes, err)
	}
	testApp.onJournalBatch(wipeRes)
	wipeDomains := eventsRecorder.Domains()
	for _, wantDomain := range []string{"settings", "stats", "plan", "ingest", "expeditions"} {
		if !slices.Contains(wipeDomains, wantDomain) {
			testingContext.Fatalf("expected Statistics wipe split to emit %q domain, got %v", wantDomain, wipeDomains)
		}
	}
	if testApp.timeRangeStats.ClosedWindowRevision() <= closedRevBeforeRoutine {
		testingContext.Fatal("expected ClosedWindowRevision to advance on Statistics career-wipe split")
	}
	statsAfterStatsWipe, err := testApp.GetSidebarStats(exp.ID, stats.Filter{})
	if err != nil || statsAfterStatsWipe.AllJournals.Jumps != 0 {
		testingContext.Fatalf("expected pre-wipe jumps archived and disabled after Statistics wipe split, got %+v err=%v",
			statsAfterStatsWipe.AllJournals, err)
	}
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}
