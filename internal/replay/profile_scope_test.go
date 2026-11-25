package replay_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"planisphere/internal/core/projection"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

func TestProfileScopeNormalizationSQLAndWindowCacheKeys(testingContext *testing.T) {
	var defaultScope replay.ProfileScope
	if !defaultScope.IsAllEnabled() || defaultScope.CacheKeySuffix() != "" {
		testingContext.Fatalf("expected zero ProfileScope to be AllEnabled with empty suffix, got %+v suffix=%q", defaultScope, defaultScope.CacheKeySuffix())
	}

	selectedScope := replay.NewProfileScope([]int64{3, 0, 1, 3, -4})
	if selectedScope.IsAllEnabled() {
		testingContext.Fatal("expected NewProfileScope([3,1]) to be explicit Selected")
	}
	if !reflect.DeepEqual(selectedScope.ProfileIDs, []int64{1, 3}) {
		testingContext.Fatalf("expected sorted deduplicated positive IDs [1 3], got %v", selectedScope.ProfileIDs)
	}
	if selectedScope.CacheKeySuffix() != "|prof:1,3" {
		testingContext.Fatalf("unexpected CacheKeySuffix %q", selectedScope.CacheKeySuffix())
	}
	if !selectedScope.ContainsProfileID(1) || !selectedScope.ContainsProfileID(3) || selectedScope.ContainsProfileID(2) {
		testingContext.Fatalf("unexpected ContainsProfileID checks for %+v", selectedScope)
	}
	if gotSQL := selectedScope.SQLPredicate("je"); gotSQL != "+je.profile_id IN (1,3)" {
		testingContext.Fatalf("unexpected aliased SQLPredicate %q", gotSQL)
	}

	emptyExplicit := replay.ProfileScope{Selected: true}
	if emptyExplicit.IsAllEnabled() || emptyExplicit.CacheKeySuffix() != "|prof:none" || emptyExplicit.SQLPredicate("") != "+profile_id IN (-1)" {
		testingContext.Fatalf("unexpected empty explicit ProfileScope behavior: %+v suffix=%q sql=%q",
			emptyExplicit, emptyExplicit.CacheKeySuffix(), emptyExplicit.SQLPredicate(""))
	}

	windowKey := "2026-01-01T00:00:00Z|2026-01-15T12:00:00Z" + selectedScope.CacheKeySuffix()
	if !projection.WindowEndsBefore(windowKey, "2026-01-16T00:00:00Z") {
		testingContext.Fatalf("expected WindowEndsBefore true for scoped closed window %q", windowKey)
	}
	if projection.WindowEndsBefore(windowKey, "2026-01-15T00:00:00Z") {
		testingContext.Fatalf("expected WindowEndsBefore false for earlier event against %q", windowKey)
	}
}

func TestListCommandersProfilesAliasesScopedNamesAndDateSessions(testingContext *testing.T) {
	conn := openReplayTestDB(testingContext)
	tempDir := testingContext.TempDir()

	files := []struct {
		name  string
		lines []string
	}{
		{
			// FID F100 Era 0 before ClearSavedGame, followed by Era 1 with a mid-career rename.
			name: "Journal.2026-05-01T080000.01.log",
			lines: []string{
				`{"timestamp":"2026-05-01T08:00:00Z","event":"LoadGame","Commander":"CmdrWiped","FID":"F100","Ship":"SideWinder","Odyssey":true}`,
				`{"timestamp":"2026-05-01T08:00:05Z","event":"Location","StarSystem":"LHS 3447","SystemAddress":100,"StarPos":[0,0,0]}`,
				`{"timestamp":"2026-05-01T09:00:00Z","event":"ClearSavedGame","Name":"CmdrWiped","FID":"F100"}`,
				`{"timestamp":"2026-05-01T09:01:00Z","event":"NewCommander","Name":"CmdrOldHandle","FID":"F100","Package":"ImperialBountyHunter"}`,
				`{"timestamp":"2026-05-01T09:01:05Z","event":"LoadGame","Commander":"CmdrOldHandle","FID":"F100","Ship":"Empire_Eagle","Odyssey":true}`,
				`{"timestamp":"2026-05-01T09:01:10Z","event":"Location","StarSystem":"Achenar","SystemAddress":101,"StarPos":[10,0,0]}`,
			},
		},
		{
			name: "Journal.2026-05-02T100000.01.log",
			lines: []string{
				`{"timestamp":"2026-05-02T10:00:00Z","event":"LoadGame","Commander":"CmdrNewHandle","FID":"F100","Ship":"Explorer_NX","Odyssey":true}`,
				`{"timestamp":"2026-05-02T10:00:12Z","event":"Location","StarSystem":"Colonia","SystemAddress":102,"StarPos":[100,0,0]}`,
				`{"timestamp":"2026-05-02T18:30:00Z","event":"LoadGame","Commander":"CmdrNewHandle","FID":"F100","Ship":"Mandalay","Odyssey":true}`,
				`{"timestamp":"2026-05-02T18:32:00Z","event":"FSDJump","StarSystem":"Sagittarius A*","SystemAddress":103,"StarPos":[200,0,0],"JumpDist":45.0}`,
			},
		},
		{
			name: "Journal.2026-05-02T200000.01.log",
			lines: []string{
				`{"timestamp":"2026-05-02T20:00:00Z","event":"LoadGame","Commander":"CmdrAltPilot","FID":"F200","Ship":"Krait_Light","Odyssey":true}`,
				`{"timestamp":"2026-05-02T20:00:05Z","event":"Location","StarSystem":"Sol","SystemAddress":104,"StarPos":[0,0,0]}`,
			},
		},
	}
	for _, fileSpec := range files {
		fullPath := filepath.Join(tempDir, fileSpec.name)
		if err := os.WriteFile(fullPath, []byte(strings.Join(fileSpec.lines, "\n")+"\n"), 0o644); err != nil {
			testingContext.Fatal(err)
		}
	}
	if _, err := journal.NewIngester(conn).ScanDir(context.Background(), tempDir); err != nil {
		testingContext.Fatalf("ScanDir: %v", err)
	}

	commanders, listErr := replay.ListCommanders(conn)
	if listErr != nil {
		testingContext.Fatalf("ListCommanders: %v", listErr)
	}
	if len(commanders) != 3 {
		testingContext.Fatalf("expected 3 resolved commander profiles, got %d: %+v", len(commanders), commanders)
	}

	archivedEra := commanders[0]
	if archivedEra.Name != "CmdrWiped" || !archivedEra.IsArchivedCareer || archivedEra.Enabled || archivedEra.EraIndex != 0 || archivedEra.EventCount != 3 {
		testingContext.Fatalf("unexpected archived era profile: %+v", archivedEra)
	}

	activeRenamed := commanders[1]
	if activeRenamed.Name != "CmdrNewHandle" || activeRenamed.IsArchivedCareer || !activeRenamed.Enabled || activeRenamed.EraIndex != 1 || activeRenamed.EventCount != 7 {
		testingContext.Fatalf("unexpected active renamed profile: %+v", activeRenamed)
	}
	if len(activeRenamed.Aliases) != 2 ||
		activeRenamed.Aliases[0].Name != "CmdrNewHandle" || !activeRenamed.Aliases[0].IsCurrent || activeRenamed.Aliases[0].EventCount != 4 ||
		activeRenamed.Aliases[1].Name != "CmdrOldHandle" || activeRenamed.Aliases[1].IsCurrent || activeRenamed.Aliases[1].EventCount != 3 {
		testingContext.Fatalf("unexpected aliases on active renamed profile: %+v", activeRenamed.Aliases)
	}

	// ListEnabledCommanderNames must return ONLY canonical display_name values and never historic alias CmdrOldHandle or disabled CmdrWiped.
	enabledNames, namesErr := replay.ListEnabledCommanderNames(conn)
	if namesErr != nil {
		testingContext.Fatalf("ListEnabledCommanderNames: %v", namesErr)
	}
	if !reflect.DeepEqual(enabledNames, []string{"CmdrNewHandle", "CmdrAltPilot"}) {
		testingContext.Fatalf("expected canonical enabled names [CmdrNewHandle CmdrAltPilot], got %v", enabledNames)
	}

	// Explicit scope on disabled archived profile must still return its canonical display_name and CareerStart.
	archivedScope := replay.NewProfileScope([]int64{archivedEra.ID})
	archivedNames, scopedNamesErr := replay.ListCommanderNamesForScope(conn, archivedScope)
	if scopedNamesErr != nil || !reflect.DeepEqual(archivedNames, []string{"CmdrWiped"}) {
		testingContext.Fatalf("expected [CmdrWiped] for archived scope, got %v err=%v", archivedNames, scopedNamesErr)
	}
	enabledStart, _ := replay.CareerStart(conn)
	archivedStart, _ := replay.CareerStartScoped(conn, archivedScope)
	if enabledStart != "2026-05-01T09:01:00Z" || archivedStart != "2026-05-01T08:00:00Z" {
		testingContext.Fatalf("unexpected career starts: enabled=%q archived=%q", enabledStart, archivedStart)
	}

	// ListCommanderLoadGamesOnDate returns chronological sessions for that profile on 2026-05-02 only.
	sessions, sessErr := replay.ListCommanderLoadGamesOnDate(conn, activeRenamed.ProfileKey, "2026-05-02")
	if sessErr != nil {
		testingContext.Fatalf("ListCommanderLoadGamesOnDate: %v", sessErr)
	}
	if len(sessions) != 2 {
		testingContext.Fatalf("expected 2 LoadGame sessions on 2026-05-02, got %+v", sessions)
	}
	if sessions[0].Timestamp != "2026-05-02T10:00:00Z" || sessions[0].Ship != "Caspian Explorer" || sessions[0].SystemName != "Colonia" {
		testingContext.Fatalf("unexpected first session: %+v", sessions[0])
	}
	if sessions[1].Timestamp != "2026-05-02T18:30:00Z" || sessions[1].Ship != "Mandalay" || sessions[1].SystemName != "Sagittarius A*" {
		testingContext.Fatalf("unexpected second session: %+v", sessions[1])
	}
	if _, badDateErr := replay.ListCommanderLoadGamesOnDate(conn, activeRenamed.ProfileKey, "2026/05/02"); badDateErr == nil {
		testingContext.Fatal("expected error for malformed dateISO")
	}
	if _, badKeyErr := replay.ListCommanderLoadGamesOnDate(conn, "nonexistent:key", "2026-05-02"); badKeyErr == nil {
		testingContext.Fatal("expected error for unknown profileKey")
	}
}

func TestStayActivityMatchesMidStayRenameAndIsolatesSameNameWipe(testingContext *testing.T) {
	conn := openReplayTestDB(testingContext)
	tempDir := testingContext.TempDir()
	journalPath := filepath.Join(tempDir, "Journal.2026-06-01T100000.01.log")
	lines := []string{
		`{"timestamp":"2026-06-01T10:00:00Z","event":"LoadGame","Commander":"CmdrSame","FID":"F500","Ship":"Mandalay"}`,
		`{"timestamp":"2026-06-01T10:01:00Z","event":"Location","StarSystem":"StaySys","SystemAddress":777,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-06-01T10:05:00Z","event":"LoadGame","Commander":"CmdrRenamed","FID":"F500","Ship":"Mandalay"}`,
		`{"timestamp":"2026-06-01T10:10:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"StaySys","SystemAddress":777,"BodyID":1,"BodyName":"StaySys 1","PlanetClass":"Water world","MassEM":1.0}`,
		`{"timestamp":"2026-06-01T11:00:00Z","event":"ClearSavedGame","Name":"CmdrSame","FID":"F500"}`,
		`{"timestamp":"2026-06-01T11:01:00Z","event":"NewCommander","Name":"CmdrSame","FID":"F500","Package":"Sidewinder"}`,
		`{"timestamp":"2026-06-01T11:02:00Z","event":"LoadGame","Commander":"CmdrSame","FID":"F500","Ship":"SideWinder"}`,
		`{"timestamp":"2026-06-01T11:15:00Z","event":"Scan","ScanType":"Detailed","StarSystem":"StaySys","SystemAddress":777,"BodyID":2,"BodyName":"StaySys 2","PlanetClass":"Earthlike body","MassEM":1.0}`,
	}
	if err := os.WriteFile(journalPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		testingContext.Fatal(err)
	}
	if _, err := journal.NewIngester(conn).ScanDir(context.Background(), tempDir); err != nil {
		testingContext.Fatal(err)
	}

	commanders, err := replay.ListCommanders(conn)
	if err != nil || len(commanders) != 2 {
		testingContext.Fatalf("expected 2 career eras, got %d err=%v", len(commanders), err)
	}
	era0Scope := replay.NewProfileScope([]int64{commanders[0].ID})
	stayCtx, ctxErr := replay.ResolveArrivalStayContextScoped(conn, "", "2026-06-01T10:01:00Z", "StaySys", 777, era0Scope)
	if ctxErr != nil || stayCtx.ProfileID != commanders[0].ID {
		testingContext.Fatalf("unexpected stayCtx: %+v err=%v", stayCtx, ctxErr)
	}
	// Era 0 activity must include the 10:10:00Z scan after rename to CmdrRenamed, while ignoring Era 1's 11:15:00Z scan under CmdrSame.
	lastActivity, actErr := replay.QueryLastInSystemActivityTimestampForStay(conn, "2026-06-01T10:01:00Z", "2026-06-01T12:00:00Z", "StaySys", 777, stayCtx, era0Scope)
	if actErr != nil || lastActivity != "2026-06-01T10:10:00Z" {
		testingContext.Fatalf("expected lastActivity 2026-06-01T10:10:00Z (post-rename, pre-wipe), got %q err=%v", lastActivity, actErr)
	}
}
