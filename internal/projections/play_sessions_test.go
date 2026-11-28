package projections_test

import (
	"testing"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestPlaySessionsCachingSelectiveInvalidationAndCommanderFilter(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	journaltest.IngestLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-06-01T10:00:00Z","event":"LoadGame","Commander":"CmdrCache","FID":"F701","Ship":"Mandalay"}`,
		`{"timestamp":"2026-06-01T10:00:02Z","event":"Statistics","Exploration":{"Time_Played":7200}}`,
		`{"timestamp":"2026-06-01T11:00:00Z","event":"Shutdown"}`,
		`{"timestamp":"2026-06-02T10:00:00Z","event":"LoadGame","Commander":"CmdrCache","FID":"F701","Ship":"Mandalay"}`,
		`{"timestamp":"2026-06-02T10:20:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":5.0}`,
	)

	eventBus := projection.NewBus()
	playSessions := projections.NewPlaySessions(databaseConnection, eventBus)
	if !playSessions.Triggers().Journal.Has(journal.EventShutdown) || !playSessions.Triggers().App.Has(events.AppCommanderFilterChanged) {
		testingContext.Fatalf("unexpected PlaySessions triggers: %+v", playSessions.Triggers())
	}

	closedSessions, closedErr := playSessions.Sessions("2026-06-01T09:00:00Z", "2026-06-01T12:00:00Z")
	if closedErr != nil || len(closedSessions) != 1 {
		testingContext.Fatalf("initial closed window: len=%d err=%v", len(closedSessions), closedErr)
	}
	openSessions, openErr := playSessions.Sessions("2026-06-02T09:00:00Z", "")
	if openErr != nil || len(openSessions) != 1 || openSessions[0].End.Sub(openSessions[0].Start) != 20*time.Minute {
		testingContext.Fatalf("initial open window: %+v err=%v", openSessions, openErr)
	}

	// Insert a later live event (2026-06-02T10:45:00Z) in the active journal file and remove the historical row in DB:
	// closed window must remain cached while open window refreshes to 45m.
	var activeSourceFile string
	if scanErr := databaseConnection.QueryRow(`SELECT source_file FROM journal_events WHERE timestamp = '2026-06-02T10:00:00Z'`).Scan(&activeSourceFile); scanErr != nil {
		testingContext.Fatal(scanErr)
	}
	if _, execError := databaseConnection.Exec(`DELETE FROM journal_events WHERE timestamp < '2026-06-02T00:00:00Z'`); execError != nil {
		testingContext.Fatal(execError)
	}
	journaltest.InsertParsedLines(
		testingContext,
		databaseConnection,
		`{"timestamp":"2026-06-02T10:45:00Z","event":"Shutdown"}`,
	)
	if _, execError := databaseConnection.Exec(`UPDATE journal_events SET source_file = ? WHERE timestamp = '2026-06-02T10:45:00Z'`, activeSourceFile); execError != nil {
		testingContext.Fatal(execError)
	}
	eventBus.PublishJournalRange(journal.NewEventSet(journal.EventShutdown), "2026-06-02T10:45:00Z", "2026-06-02T10:45:00Z")

	cachedClosed, cachedClosedErr := playSessions.Sessions("2026-06-01T09:00:00Z", "2026-06-01T12:00:00Z")
	if cachedClosedErr != nil || len(cachedClosed) != 1 {
		testingContext.Fatalf("expected closed historical window to stay cached across later live event, got len=%d err=%v", len(cachedClosed), cachedClosedErr)
	}
	refreshedOpen, refreshedOpenErr := playSessions.Sessions("2026-06-02T09:00:00Z", "")
	if refreshedOpenErr != nil || len(refreshedOpen) != 1 || refreshedOpen[0].End.Sub(refreshedOpen[0].Start) != 45*time.Minute {
		testingContext.Fatalf("expected open window to invalidate and refresh to 45m, got %+v err=%v", refreshedOpen, refreshedOpenErr)
	}

	// Warm career cache before disabling commander so AppCommanderFilterChanged verifies career eviction.
	initialCareer, initialCareerFound, initialCareerErr := playSessions.CareerDurationScoped(replay.ProfileScope{})
	if initialCareerErr != nil || !initialCareerFound || initialCareer <= 0 {
		testingContext.Fatalf("expected populated career duration before disabling commander, got %v found=%v err=%v", initialCareer, initialCareerFound, initialCareerErr)
	}

	// Disabling commander and publishing AppCommanderFilterChanged must invalidate both window and career caches.
	if err := settings.SetCommanderEnabled(databaseConnection, "CmdrCache", false); err != nil {
		testingContext.Fatal(err)
	}
	eventBus.PublishApp(events.AppCommanderFilterChanged)

	afterDisable, disableErr := playSessions.Sessions("2026-06-02T09:00:00Z", "")
	if disableErr != nil || len(afterDisable) != 0 {
		testingContext.Fatalf("expected 0 sessions after disabling commander, got %+v err=%v", afterDisable, disableErr)
	}
	careerDur, careerFound, careerErr := playSessions.CareerDurationScoped(replay.ProfileScope{})
	if careerErr != nil || careerFound || careerDur != 0 {
		testingContext.Fatalf("expected empty career duration after disabling commander, got %v found=%v err=%v", careerDur, careerFound, careerErr)
	}
}
