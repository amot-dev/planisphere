package replay_test

import (
	"testing"
	"time"

	"planisphere/internal/replay"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

func TestQueryPlaySessionsResolutionTiersAndRollover(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		// Clean Shutdown session (1h 30m)
		"Journal.2026-06-01T100000.01.log": {
			`{"timestamp":"2026-06-01T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-01T10:00:00Z","event":"LoadGame","Commander":"CmdrPlay","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-01T10:00:02Z","event":"Statistics","Exploration":{"Time_Played":10000}}`,
			`{"timestamp":"2026-06-01T11:30:00Z","event":"Shutdown"}`,
		},
		// Music:MainMenu closes at 14:00:00Z even though Shutdown arrives 5m later while idle at menu
		"Journal.2026-06-01T130000.01.log": {
			`{"timestamp":"2026-06-01T13:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-01T13:00:00Z","event":"LoadGame","Commander":"CmdrPlay","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-01T13:00:02Z","event":"Statistics","Exploration":{"Time_Played":15400}}`,
			`{"timestamp":"2026-06-01T14:00:00Z","event":"Music","MusicTrack":"MainMenu"}`,
			`{"timestamp":"2026-06-01T14:05:00Z","event":"Shutdown"}`,
		},
		// Crash recovery via next Statistics.Time_Played (+45m = 2700s; last observed event only +15m)
		"Journal.2026-06-01T160000.01.log": {
			`{"timestamp":"2026-06-01T16:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-01T16:00:00Z","event":"LoadGame","Commander":"CmdrPlay","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-01T16:00:02Z","event":"Statistics","Exploration":{"Time_Played":19000}}`,
			`{"timestamp":"2026-06-01T16:15:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":10.0}`,
		},
		// Multi-part rollover continuation (part 1 -> part 2) that remains active and open: caps at Part 2 last_timestamp (20:30:00Z)
		"Journal.2026-06-01T180000.01.log": {
			`{"timestamp":"2026-06-01T18:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-01T18:00:00Z","event":"LoadGame","Commander":"CmdrPlay","FID":"F500","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-01T18:00:02Z","event":"Statistics","Exploration":{"Time_Played":21700}}`,
			`{"timestamp":"2026-06-01T19:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[10,0,0],"JumpDist":20.0}`,
			`{"timestamp":"2026-06-01T19:05:00Z","event":"Continued","Part":2}`,
		},
		"Journal.2026-06-01T180000.02.log": {
			`{"timestamp":"2026-06-01T19:05:01Z","event":"Fileheader","part":2,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-01T20:00:00Z","event":"FSDJump","StarSystem":"Sagittarius A*","SystemAddress":3003,"StarPos":[20,0,0],"JumpDist":20.0}`,
			`{"timestamp":"2026-06-01T20:30:00Z","event":"FuelScoop","Scooped":4.0,"Total":32.0}`,
		},
	})

	sessions, queryError := replay.QueryPlaySessionsScoped(databaseConnection, "", "", replay.ProfileScope{})
	if queryError != nil {
		testingContext.Fatalf("QueryPlaySessionsScoped: %v", queryError)
	}
	if len(sessions) != 4 {
		testingContext.Fatalf("expected 4 sessions (rollover merged), got %d: %+v", len(sessions), sessions)
	}

	expectedDurations := []time.Duration{
		90 * time.Minute,  // Clean Shutdown 10:00 -> 11:30
		60 * time.Minute,  // MainMenu 13:00 -> 14:00 (ignoring 14:05 Shutdown)
		45 * time.Minute,  // Crash recovery 16:00 -> 16:45 from Time_Played delta (21700 - 19000 = 2700s)
		150 * time.Minute, // Rollover open session 18:00 -> 20:30 capped strictly at observedEnd
	}
	for index, expectedDuration := range expectedDurations {
		actualDuration := sessions[index].End.Sub(sessions[index].Start)
		if actualDuration != expectedDuration {
			testingContext.Errorf("session[%d] duration = %v (%s..%s), want %v",
				index, actualDuration, sessions[index].Start.Format(time.RFC3339), sessions[index].End.Format(time.RFC3339), expectedDuration)
		}
	}

	// Verify SumPlayDuration additive clipping (Transit + InSystem == Full) across a straddling window.
	fullDur, fullFound := replay.SumPlayDuration(sessions, "2026-06-01T10:30:00Z", "2026-06-01T19:30:00Z")
	transitDur, transitFound := replay.SumPlayDuration(sessions, "2026-06-01T10:30:00Z", "2026-06-01T16:20:00Z")
	inSystemDur, inSystemFound := replay.SumPlayDuration(sessions, "2026-06-01T16:20:00Z", "2026-06-01T19:30:00Z")
	if !fullFound || !transitFound || !inSystemFound {
		testingContext.Fatalf("expected overlap flags true: full=%v transit=%v inSystem=%v", fullFound, transitFound, inSystemFound)
	}
	if transitDur+inSystemDur != fullDur {
		testingContext.Fatalf("additive clipping mismatch: transit(%v) + inSystem(%v) != full(%v)", transitDur, inSystemDur, fullDur)
	}
	// Expected: [10:30..11:30]=60m + [13:00..14:00]=60m + [16:00..16:45]=45m + [18:00..19:30]=90m = 255m
	if fullDur != 255*time.Minute {
		testingContext.Fatalf("full clipped duration = %v, want 255m", fullDur)
	}

	// Bounded query with preceding seed and lookahead recovers the crashed 16:00 session even when queried only on [16:10, 17:00].
	boundedSessions, boundedError := replay.QueryPlaySessionsScoped(databaseConnection, "2026-06-01T16:10:00Z", "2026-06-01T17:00:00Z", replay.ProfileScope{})
	if boundedError != nil {
		testingContext.Fatalf("bounded QueryPlaySessionsScoped: %v", boundedError)
	}
	boundedDur, boundedFound := replay.SumPlayDuration(boundedSessions, "2026-06-01T16:10:00Z", "2026-06-01T17:00:00Z")
	if !boundedFound || boundedDur != 35*time.Minute {
		testingContext.Fatalf("bounded crashed session duration = %v (found=%v), want 35m", boundedDur, boundedFound)
	}
}

func TestQueryPlaySessionsMultiLoadGameHistoricalRelogSlack(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	// Historical file with two LoadGame entries sharing currentTP=5000 (no MainMenu logged between them):
	// Sub-session 1: 10:00..10:25 observed (25m), gap to 10:35 is 10m.
	// Sub-session 2: 10:35..10:50 observed (15m), gap to 12:00 is 70m.
	// Next session at 12:00 has Time_Played=8600 (delta = 3600s = 60m -> slack = 60m - 40m = 20m).
	// Slack distribution grants 10m to sub-session 1 (-> 10:35) and remaining 10m to sub-session 2 (-> 11:00).
	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		"Journal.2026-06-02T100000.01.log": {
			`{"timestamp":"2026-06-02T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-02T10:00:00Z","event":"LoadGame","Commander":"CmdrRelog","FID":"F501","Ship":"Anaconda","Odyssey":true}`,
			`{"timestamp":"2026-06-02T10:00:02Z","event":"Statistics","Exploration":{"Time_Played":5000}}`,
			`{"timestamp":"2026-06-02T10:25:00Z","event":"FSDJump","StarSystem":"SysA","SystemAddress":101,"StarPos":[0,0,0],"JumpDist":5.0}`,
			`{"timestamp":"2026-06-02T10:35:00Z","event":"LoadGame","Commander":"CmdrRelog","FID":"F501","Ship":"Anaconda","Odyssey":true}`,
			`{"timestamp":"2026-06-02T10:50:00Z","event":"FSDJump","StarSystem":"SysB","SystemAddress":102,"StarPos":[5,0,0],"JumpDist":5.0}`,
		},
		"Journal.2026-06-02T120000.01.log": {
			`{"timestamp":"2026-06-02T12:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-02T12:00:00Z","event":"LoadGame","Commander":"CmdrRelog","FID":"F501","Ship":"Anaconda","Odyssey":true}`,
			`{"timestamp":"2026-06-02T12:00:02Z","event":"Statistics","Exploration":{"Time_Played":8600}}`,
			`{"timestamp":"2026-06-02T12:10:00Z","event":"Shutdown"}`,
		},
	})

	sessions, queryError := replay.QueryPlaySessionsScoped(databaseConnection, "", "", replay.ProfileScope{})
	if queryError != nil {
		testingContext.Fatalf("QueryPlaySessionsScoped: %v", queryError)
	}
	if len(sessions) != 3 {
		testingContext.Fatalf("expected 3 sessions, got %d: %+v", len(sessions), sessions)
	}
	if gotFirst := sessions[0].End.Sub(sessions[0].Start); gotFirst != 35*time.Minute {
		testingContext.Errorf("sub-session 1 duration = %v, want 35m", gotFirst)
	}
	if gotSecond := sessions[1].End.Sub(sessions[1].Start); gotSecond != 25*time.Minute {
		testingContext.Errorf("sub-session 2 duration = %v, want 25m", gotSecond)
	}
	if sessions[0].End.After(sessions[1].Start) {
		testingContext.Errorf("sub-sessions overlapped: %v > %v", sessions[0].End, sessions[1].Start)
	}

	// Bounded query whose endTimestamp falls between the two LoadGame entries of the multi-LoadGame relog file
	// must not stop lookahead on the 12:00 LoadGame before reading its paired Statistics(Time_Played=8600).
	boundedSessions, boundedError := replay.QueryPlaySessionsScoped(
		databaseConnection,
		"2026-06-02T10:00:00Z",
		"2026-06-02T10:30:00Z",
		replay.ProfileScope{},
	)
	if boundedError != nil {
		testingContext.Fatalf("bounded relog QueryPlaySessionsScoped: %v", boundedError)
	}
	boundedDuration, boundedFound := replay.SumPlayDuration(boundedSessions, "2026-06-02T10:00:00Z", "2026-06-02T10:30:00Z")
	unboundedDuration, unboundedFound := replay.SumPlayDuration(sessions, "2026-06-02T10:00:00Z", "2026-06-02T10:30:00Z")
	if !boundedFound || !unboundedFound || boundedDuration != unboundedDuration || boundedDuration != 30*time.Minute {
		testingContext.Fatalf("bounded relog duration = %v (found=%v), unbounded = %v (found=%v), want 30m",
			boundedDuration, boundedFound, unboundedDuration, unboundedFound)
	}
}

func TestPlaySessionsCrossFileMenuGuardMenuOnlyLaunchAndClosedAnchorSlack(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		// Crashed File A: 10:00 LoadGame, Statistics(1000), 10:15 FSDJump, no Shutdown.
		"Journal.2026-06-04T100000.01.log": {
			`{"timestamp":"2026-06-04T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-04T10:00:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F701","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-04T10:00:02Z","event":"Statistics","Exploration":{"Time_Played":1000}}`,
			`{"timestamp":"2026-06-04T10:15:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":5.0}`,
		},
		// Menu-only launch/quit file: Fileheader + pre-LoadGame Commander + Music:MainMenu + Shutdown with no LoadGame or gameplay events.
		"Journal.2026-06-04T110000.01.log": {
			`{"timestamp":"2026-06-04T11:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-04T11:01:00Z","event":"Commander","FID":"F701","Name":"CmdrGuard"}`,
			`{"timestamp":"2026-06-04T11:02:00Z","event":"Music","MusicTrack":"MainMenu"}`,
			`{"timestamp":"2026-06-04T11:05:00Z","event":"Shutdown"}`,
		},
		// File B starts with pre-LoadGame Music:MainMenu, then LoadGame + Statistics(2800 -> +30m for crashed File A),
		// closes cleanly at 12:21 via Music:MainMenu, then relogs in same file at 12:30 without Statistics and crashes at 12:45.
		"Journal.2026-06-04T120000.01.log": {
			`{"timestamp":"2026-06-04T12:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-04T12:00:05Z","event":"Music","MusicTrack":"MainMenu"}`,
			`{"timestamp":"2026-06-04T12:01:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F701","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-04T12:01:02Z","event":"Statistics","Exploration":{"Time_Played":2800}}`,
			`{"timestamp":"2026-06-04T12:21:00Z","event":"Music","MusicTrack":"MainMenu"}`,
			`{"timestamp":"2026-06-04T12:30:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F701","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-04T12:45:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":2002,"StarPos":[5,0,0],"JumpDist":5.0}`,
		},
		// File C provides Time_Played=5200 (+40m over File B's 2800: 20m closed sub-session + 15m observed + 5m crash slack -> 20m).
		"Journal.2026-06-04T140000.01.log": {
			`{"timestamp":"2026-06-04T14:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-04T14:00:00Z","event":"LoadGame","Commander":"CmdrGuard","FID":"F701","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-04T14:00:02Z","event":"Statistics","Exploration":{"Time_Played":5200}}`,
			`{"timestamp":"2026-06-04T14:10:00Z","event":"Shutdown"}`,
		},
	})

	sessions, queryError := replay.QueryPlaySessionsScoped(databaseConnection, "", "", replay.ProfileScope{})
	if queryError != nil {
		testingContext.Fatalf("QueryPlaySessionsScoped: %v", queryError)
	}
	if len(sessions) != 4 {
		testingContext.Fatalf("expected 4 sessions (menu-only file excluded), got %d: %+v", len(sessions), sessions)
	}
	expectedDurations := []time.Duration{
		30 * time.Minute, // Crashed File A recovered via File B's Time_Played delta (not closed by menu-only or File B MainMenu)
		20 * time.Minute, // File B sub-session 1 closed cleanly by MainMenu (12:01 -> 12:21)
		20 * time.Minute, // File B sub-session 2 recovered 5m crash slack anchored by closed sub-session 1 (12:30 -> 12:50)
		10 * time.Minute, // File C clean shutdown (14:00 -> 14:10)
	}
	for index, expectedDuration := range expectedDurations {
		if actualDuration := sessions[index].End.Sub(sessions[index].Start); actualDuration != expectedDuration {
			testingContext.Errorf("session[%d] duration = %v (%s..%s), want %v",
				index, actualDuration, sessions[index].Start.Format(time.RFC3339), sessions[index].End.Format(time.RFC3339), expectedDuration)
		}
	}

	// Bounded query ending between crashed File A and the menu-only/File B launches must still recover File A's 30m duration.
	boundedSessions, boundedError := replay.QueryPlaySessionsScoped(
		databaseConnection,
		"2026-06-04T10:00:00Z",
		"2026-06-04T10:45:00Z",
		replay.ProfileScope{},
	)
	if boundedError != nil {
		testingContext.Fatalf("bounded crashed File A QueryPlaySessionsScoped: %v", boundedError)
	}
	boundedDuration, boundedFound := replay.SumPlayDuration(boundedSessions, "2026-06-04T10:00:00Z", "2026-06-04T10:45:00Z")
	if !boundedFound || boundedDuration != 30*time.Minute {
		testingContext.Fatalf("bounded crashed File A duration = %v (found=%v), want 30m", boundedDuration, boundedFound)
	}

	// Closed bounded query covering File C with a later truncated archive file must stay bounded and exclude future sessions.
	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		"Journal.2026-06-05T100000.01.log": {
			`{"timestamp":"2026-06-05T10:00:00Z","event":"FSDJump","StarSystem":"FutureSys","SystemAddress":9009,"StarPos":[10,0,0],"JumpDist":10.0}`,
			`{"timestamp":"2026-06-05T10:25:00Z","event":"FSDJump","StarSystem":"FutureSys2","SystemAddress":9010,"StarPos":[20,0,0],"JumpDist":10.0}`,
		},
	})
	closedFutureSessions, closedFutureErr := replay.QueryPlaySessionsScoped(
		databaseConnection,
		"2026-06-04T14:00:00Z",
		"2026-06-04T15:00:00Z",
		replay.ProfileScope{},
	)
	if closedFutureErr != nil {
		testingContext.Fatalf("closed bounded QueryPlaySessionsScoped with later truncated file: %v", closedFutureErr)
	}
	if len(closedFutureSessions) != 1 || closedFutureSessions[0].End.Sub(closedFutureSessions[0].Start) != 10*time.Minute {
		testingContext.Fatalf("expected only File C (10m) in closed bounded query, got %+v", closedFutureSessions)
	}
}

func TestPlaySessionsProfileIsolationAndCareerDuration(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	journaltest.IngestFiles(testingContext, databaseConnection, map[string][]string{
		"Journal.2026-06-03T100000.01.log": {
			`{"timestamp":"2026-06-03T10:00:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-03T10:00:00Z","event":"LoadGame","Commander":"CmdrAlpha","FID":"F601","Ship":"Mandalay","Odyssey":true}`,
			`{"timestamp":"2026-06-03T10:00:02Z","event":"Statistics","Exploration":{"Time_Played":360000}}`,
			`{"timestamp":"2026-06-03T10:30:02Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0],"JumpDist":5.0}`,
		},
		"Journal.2026-06-03T101000.01.log": {
			`{"timestamp":"2026-06-03T10:10:00Z","event":"Fileheader","part":1,"gameversion":"4.0.0.1800","build":"r301234/r0 ","Odyssey":true}`,
			`{"timestamp":"2026-06-03T10:10:00Z","event":"LoadGame","Commander":"CmdrBeta","FID":"F602","Ship":"KraitMkII","Odyssey":true}`,
			`{"timestamp":"2026-06-03T11:10:00Z","event":"Shutdown"}`,
		},
	})

	var alphaID, betaID int64
	if err := databaseConnection.QueryRow(`SELECT id FROM commander_profiles WHERE display_name = 'CmdrAlpha'`).Scan(&alphaID); err != nil {
		testingContext.Fatalf("lookup CmdrAlpha: %v", err)
	}
	if err := databaseConnection.QueryRow(`SELECT id FROM commander_profiles WHERE display_name = 'CmdrBeta'`).Scan(&betaID); err != nil {
		testingContext.Fatalf("lookup CmdrBeta: %v", err)
	}

	// CmdrAlpha career = Codex 100h (360000s) + active tail 30m (10:00:02 -> 10:30:02) = 100h 30m.
	alphaCareer, alphaFound, alphaErr := replay.QueryCareerPlayDurationScoped(databaseConnection, replay.NewProfileScope([]int64{alphaID}))
	if alphaErr != nil || !alphaFound || alphaCareer != 100*time.Hour+30*time.Minute {
		testingContext.Fatalf("CmdrAlpha career = %v (found=%v, err=%v), want 100h30m", alphaCareer, alphaFound, alphaErr)
	}

	// CmdrBeta has zero Statistics rows and falls back cleanly to session reconstruction (1h).
	betaCareer, betaFound, betaErr := replay.QueryCareerPlayDurationScoped(databaseConnection, replay.NewProfileScope([]int64{betaID}))
	if betaErr != nil || !betaFound || betaCareer != time.Hour {
		testingContext.Fatalf("CmdrBeta career = %v (found=%v, err=%v), want 1h", betaCareer, betaFound, betaErr)
	}

	// Combined AllEnabled scope sums per-profile without merging overlapping timestamps (100h30m + 1h = 101h30m).
	allCareer, allFound, allErr := replay.QueryCareerPlayDurationScoped(databaseConnection, replay.ProfileScope{})
	if allErr != nil || !allFound || allCareer != 101*time.Hour+30*time.Minute {
		testingContext.Fatalf("AllEnabled career = %v (found=%v, err=%v), want 101h30m", allCareer, allFound, allErr)
	}

	// Overlapping sessions across Alpha (30m02s) and Beta (60m) sum independently to 90m02s.
	allSessions, sessErr := replay.QueryPlaySessionsScoped(databaseConnection, "", "", replay.ProfileScope{})
	if sessErr != nil {
		testingContext.Fatalf("AllEnabled sessions: %v", sessErr)
	}
	sumAll, sumFound := replay.SumPlayDuration(allSessions, "", "")
	if !sumFound || sumAll != 90*time.Minute+2*time.Second {
		testingContext.Fatalf("AllEnabled summed session duration = %v (found=%v), want 1h30m2s", sumAll, sumFound)
	}
}
