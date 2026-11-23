package replay_test

import (
	"testing"

	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

func TestSessionStateMulticrewIsolation(testingContext *testing.T) {
	testingContext.Run("ApplyLocation with Multicrew true preserves carrier dock, population, and respawn state", func(subTest *testing.T) {
		sessionState := replay.NewSessionState(true, 16)
		sessionState.ApplyDocked(`{"timestamp":"2026-08-01T10:00:00Z","event":"Docked","StationName":"K9F-8TQ","StationType":"FleetCarrier","StarSystem":"CarrierSys","SystemAddress":1001}`)
		if !sessionState.IsDockedOnFleetCarrier() {
			subTest.Fatal("expected docked on fleet carrier before multicrew Location")
		}

		sessionState.ApplyLocation(`{"timestamp":"2026-08-01T10:05:00Z","event":"Location","StarSystem":"HostBubble","SystemAddress":9001,"Population":4500000,"Docked":false,"Multicrew":true}`)
		if !sessionState.IsDockedOnFleetCarrier() {
			subTest.Fatal("expected multicrew Location not to clear fleet carrier docking state")
		}
		if population := sessionState.SystemPopulation(9001); population != 0 {
			subTest.Fatalf("expected multicrew Location not to record population, got %d", population)
		}

		sessionState.ApplyResurrect("recover")
		sessionState.ApplyLocation(`{"timestamp":"2026-08-01T10:10:00Z","event":"Location","StarSystem":"HostBubble","SystemAddress":9002,"Population":12345,"Docked":false,"OnFoot":false,"Multicrew":true}`)
		if sessionState.IsDockedOnFleetCarrier() {
			subTest.Fatal("expected fleet carrier docking state to remain false prior to physical respawn")
		}
		if population := sessionState.SystemPopulation(9002); population != 0 {
			subTest.Fatalf("expected multicrew Location during pending respawn not to record population, got %d", population)
		}

		// Real concourse respawn immediately following must still find pendingConcourseRespawn intact.
		sessionState.ApplyLocation(`{"timestamp":"2026-08-01T10:12:00Z","event":"Location","StarSystem":"CarrierSys","SystemAddress":1001,"Population":0,"Docked":false,"OnFoot":true,"BodyType":"Planet"}`)
		if !sessionState.IsDockedOnFleetCarrier() {
			subTest.Fatal("expected pending concourse respawn to survive multicrew Location and restore fleet carrier docking")
		}
	})

	testingContext.Run("ApplyFSDJumpEvent with Multicrew true preserves carrier dock, population, and respawn state", func(subTest *testing.T) {
		sessionState := replay.NewSessionState(true, 16)
		sessionState.ApplyFSDJumpEvent(nil)
		sessionState.ApplyDocked(`{"timestamp":"2026-08-01T11:00:00Z","event":"Docked","StationName":"K9F-8TQ","StationType":"FleetCarrier","StarSystem":"CarrierSys","SystemAddress":1001}`)

		sessionState.ApplyFSDJumpEvent(&journal.FSDJumpEvent{
			StarSystem:    "HostSys",
			SystemAddress: 9003,
			Population:    987654,
			Multicrew:     true,
		})
		if !sessionState.IsDockedOnFleetCarrier() {
			subTest.Fatal("expected multicrew FSDJump not to clear fleet carrier docking state")
		}
		if population := sessionState.SystemPopulation(9003); population != 0 {
			subTest.Fatalf("expected multicrew FSDJump not to record population, got %d", population)
		}

		sessionState.ApplyResurrect("recover")
		sessionState.ApplyFSDJumpEvent(&journal.FSDJumpEvent{
			StarSystem:    "HostSys",
			SystemAddress: 9003,
			Population:    987654,
			Multicrew:     true,
		})
		sessionState.ApplyLocation(`{"timestamp":"2026-08-01T11:05:00Z","event":"Location","StarSystem":"CarrierSys","SystemAddress":1001,"Population":0,"Docked":false,"OnFoot":true,"BodyType":"Planet"}`)
		if !sessionState.IsDockedOnFleetCarrier() {
			subTest.Fatal("expected pending concourse respawn to survive multicrew FSDJump and restore fleet carrier docking")
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
