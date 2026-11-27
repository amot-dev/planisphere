package projections_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
)

func TestRecoveredJumpsProjectionCachingInvalidationAndScoping(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	temporaryDirectory := testingContext.TempDir()
	journalPath := filepath.Join(temporaryDirectory, "Journal.2026-11-16T180124.01.log")

	initialLines := []string{
		`{"timestamp":"2026-11-17T01:00:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:00:05Z","event":"Location","StarSystem":"Origin","SystemAddress":100,"StarPos":[0,0,0],"Docked":false}`,
		`{"timestamp":"2026-11-17T01:01:20Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopOne","SystemAddress":101}`,
		`{"timestamp":"2026-11-17T01:02:00Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:02:10Z","event":"Location","StarSystem":"HopOne","SystemAddress":101,"StarPos":[30,40,0],"Docked":false}`,
	}
	if writeError := os.WriteFile(journalPath, []byte(strings.Join(initialLines, "\n")+"\n"), 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	if _, scanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); scanError != nil {
		testingContext.Fatal(scanError)
	}

	eventBus := projection.NewBus()
	recoveredJumps := projections.NewRecoveredJumps(databaseConnection, eventBus)
	if !recoveredJumps.Triggers().Journal.Has(journal.EventLocation) || recoveredJumps.Triggers().Journal.Has(journal.EventFSDJump) {
		testingContext.Fatalf("unexpected RecoveredJumps triggers: %v", recoveredJumps.Triggers())
	}

	allInitial, allErr := recoveredJumps.All()
	if allErr != nil || len(allInitial) != 1 || allInitial[0].SystemName != "HopOne" {
		testingContext.Fatalf("All() = %+v err=%v, want 1 HopOne jump", allInitial, allErr)
	}
	initialRecovered, recoverError := recoveredJumps.InRange("", "")
	if recoverError != nil {
		testingContext.Fatalf("InRange: %v", recoverError)
	}
	if len(initialRecovered) != 1 || initialRecovered[0].SystemName != "HopOne" {
		testingContext.Fatalf("expected 1 initial recovered jump, got %+v", initialRecovered)
	}

	// Appending a second recovered jump and publishing lookback-only events must NOT invalidate RecoveredJumps;
	// only EventLocation completes a candidate and invalidates the projection.
	hopTwoLines := strings.Join([]string{
		`{"timestamp":"2026-11-17T01:10:00Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"HopTwo","SystemAddress":102}`,
		`{"timestamp":"2026-11-17T01:10:30Z","event":"LoadGame","Commander":"CmdrTest","FID":"F1","Ship":"Explorer_NX"}`,
		`{"timestamp":"2026-11-17T01:10:40Z","event":"Location","StarSystem":"HopTwo","SystemAddress":102,"StarPos":[30,40,200],"Docked":false}`,
	}, "\n") + "\n"
	appendHandle, openAppendError := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if openAppendError != nil {
		testingContext.Fatal(openAppendError)
	}
	if _, writeAppendError := appendHandle.WriteString(hopTwoLines); writeAppendError != nil {
		_ = appendHandle.Close()
		testingContext.Fatal(writeAppendError)
	}
	_ = appendHandle.Close()
	if _, rescanError := journal.NewIngester(databaseConnection).ScanDir(context.Background(), temporaryDirectory); rescanError != nil {
		testingContext.Fatal(rescanError)
	}

	eventBus.PublishJournal(journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
		journal.EventResurrect,
	))
	cachedRecovered, cachedError := recoveredJumps.InRange("", "")
	if cachedError != nil {
		testingContext.Fatal(cachedError)
	}
	if len(cachedRecovered) != 1 {
		testingContext.Fatalf("expected lookback events not to invalidate RecoveredJumps (want 1 cached), got %d", len(cachedRecovered))
	}

	eventBus.PublishJournal(journal.NewEventSet(journal.EventLocation))
	refreshedRecovered, refreshError := recoveredJumps.InRange("", "")
	if refreshError != nil {
		testingContext.Fatal(refreshError)
	}
	if len(refreshedRecovered) != 2 || refreshedRecovered[1].SystemName != "HopTwo" {
		testingContext.Fatalf("expected 2 recovered jumps after EventLocation invalidation, got %+v", refreshedRecovered)
	}

	// Disabling the commander and publishing AppCommanderFilterChanged must suppress recovered jumps in AllEnabled while preserving explicit scope.
	commanders, listErr := replay.ListCommanders(databaseConnection)
	if listErr != nil || len(commanders) != 1 {
		testingContext.Fatalf("ListCommanders: %+v err=%v", commanders, listErr)
	}
	if disableError := settings.SetCommanderEnabled(databaseConnection, "CmdrTest", false); disableError != nil {
		testingContext.Fatal(disableError)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)

	disabledRecovered, disabledError := recoveredJumps.InRange("", "")
	if disabledError != nil {
		testingContext.Fatal(disabledError)
	}
	if len(disabledRecovered) != 0 {
		testingContext.Fatalf("expected 0 recovered jumps when commander disabled, got %d", len(disabledRecovered))
	}
	scopedRecovered, scopedErr := recoveredJumps.InRangeScoped("", "", replay.NewProfileScope([]int64{commanders[0].ID}))
	if scopedErr != nil || len(scopedRecovered) != 2 {
		testingContext.Fatalf("expected 2 recovered jumps via explicit scope on disabled profile, got %d err=%v", len(scopedRecovered), scopedErr)
	}
}
