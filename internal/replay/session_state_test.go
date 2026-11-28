package replay_test

import (
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

func TestSessionStateMulticrewIsolation(testingContext *testing.T) {
	testCases := []struct {
		name               string
		firstAddress       int64
		secondAddress      int64
		applyBeforeDock    func(*replay.SessionState)
		applyBeforeRespawn func(*replay.SessionState)
		applyDuringRespawn func(*replay.SessionState)
	}{
		{
			name:          "ApplyLocation with Multicrew true",
			firstAddress:  9001,
			secondAddress: 9002,
			applyBeforeRespawn: func(sessionState *replay.SessionState) {
				sessionState.ApplyLocation([]byte(`{"timestamp":"2026-08-01T10:05:00Z","event":"Location","StarSystem":"HostBubble","SystemAddress":9001,"Population":4500000,"Docked":false,"Multicrew":true}`))
			},
			applyDuringRespawn: func(sessionState *replay.SessionState) {
				sessionState.ApplyLocation([]byte(`{"timestamp":"2026-08-01T10:10:00Z","event":"Location","StarSystem":"HostBubble","SystemAddress":9002,"Population":12345,"Docked":false,"OnFoot":false,"Multicrew":true}`))
			},
		},
		{
			name:          "ApplyFSDJumpEvent with Multicrew true",
			firstAddress:  9003,
			secondAddress: 9003,
			applyBeforeDock: func(sessionState *replay.SessionState) {
				sessionState.ApplyFSDJumpEvent(nil)
			},
			applyBeforeRespawn: func(sessionState *replay.SessionState) {
				sessionState.ApplyFSDJumpEvent(&journal.FSDJumpEvent{
					StarSystem:    "HostSys",
					SystemAddress: 9003,
					Population:    987654,
					Multicrew:     true,
				})
			},
			applyDuringRespawn: func(sessionState *replay.SessionState) {
				sessionState.ApplyFSDJumpEvent(&journal.FSDJumpEvent{
					StarSystem:    "HostSys",
					SystemAddress: 9003,
					Population:    987654,
					Multicrew:     true,
				})
			},
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(subTest *testing.T) {
			sessionState := replay.NewSessionState(true, 16)
			if testCase.applyBeforeDock != nil {
				testCase.applyBeforeDock(sessionState)
			}
			sessionState.ApplyDocked([]byte(`{"timestamp":"2026-08-01T10:00:00Z","event":"Docked","StationName":"K9F-8TQ","StationType":"FleetCarrier","StarSystem":"CarrierSys","SystemAddress":1001}`))
			if !sessionState.IsDockedOnFleetCarrier() {
				subTest.Fatal("expected docked on fleet carrier before multicrew arrival")
			}

			testCase.applyBeforeRespawn(sessionState)
			if !sessionState.IsDockedOnFleetCarrier() {
				subTest.Fatal("expected multicrew arrival not to clear fleet carrier docking state")
			}
			if population := sessionState.SystemPopulation(testCase.firstAddress); population != 0 {
				subTest.Fatalf("expected multicrew arrival not to record population, got %d", population)
			}

			sessionState.ApplyResurrect("recover")
			testCase.applyDuringRespawn(sessionState)
			if sessionState.IsDockedOnFleetCarrier() {
				subTest.Fatal("expected fleet carrier docking state to remain false prior to physical respawn")
			}
			if population := sessionState.SystemPopulation(testCase.secondAddress); population != 0 {
				subTest.Fatalf("expected multicrew arrival during pending respawn not to record population, got %d", population)
			}

			// Physical concourse respawn immediately following must still find pendingConcourseRespawn intact.
			sessionState.ApplyLocation([]byte(`{"timestamp":"2026-08-01T10:12:00Z","event":"Location","StarSystem":"CarrierSys","SystemAddress":1001,"Population":0,"Docked":false,"OnFoot":true,"BodyType":"Planet"}`))
			if !sessionState.IsDockedOnFleetCarrier() {
				subTest.Fatal("expected pending concourse respawn to survive multicrew arrival and restore fleet carrier docking")
			}

			sessionState.ApplyFSDJumpEvent(&journal.FSDJumpEvent{
				StarSystem:    "PhysicalBubbleSys",
				SystemAddress: 1002,
				Population:    5000,
			})
			if sessionState.IsDockedOnFleetCarrier() {
				subTest.Fatal("expected physical FSDJump to clear fleet carrier docking state")
			}
			if population := sessionState.SystemPopulation(1002); population != 5000 {
				subTest.Fatalf("expected physical FSDJump to record population 5000, got %d", population)
			}
		})
	}
}
