package app

import (
	"testing"
)

func TestReconcileSpanshJumps(t *testing.T) {
	// Raw jumps simulating Spansh API output
	// Index 0: origin system with distance 0
	// Index 1: 494.89 Ly jump where Spansh calculated 89 tonnes assuming 437 depot
	// Index 2: 120.00 Ly jump
	rawJumps := []SpanshRawJump{
		{
			Name:                  "Origin System",
			Distance:              0,
			DistanceToDestination: 614.89,
			FuelUsed:              0,
			TritiumUsed:           0,
			FuelInTank:            437,
		},
		{
			Name:                  "Waypoint 1",
			Distance:              494.89,
			DistanceToDestination: 120.0,
			FuelUsed:              89,
			TritiumUsed:           89,
			FuelInTank:            437,
		},
		{
			Name:                  "Waypoint 2",
			Distance:              120.0,
			DistanceToDestination: 0,
			FuelUsed:              26,
			TritiumUsed:           26,
			FuelInTank:            348,
		},
	}

	usedCapacity := 8548
	depotTritium := 1000 // Actual in-game full depot tank

	reconciled := ReconcileSpanshJumps(rawJumps, usedCapacity, depotTritium)

	// Origin hop with distance 0 should be skipped
	if len(reconciled) != 2 {
		t.Fatalf("expected 2 jumps, got %d", len(reconciled))
	}

	// Jump 1: 494.89 Ly, UsedCapacity=8548, Depot=1000
	// Mass = 25000 + 8548 + 1000 = 34548
	// 5 + 494.89 * 34548 / 200000 = 90.487... -> ceil = 91
	hop1 := reconciled[0]
	if hop1.SystemName != "Waypoint 1" {
		t.Errorf("expected SystemName 'Waypoint 1', got %s", hop1.SystemName)
	}
	if hop1.DistanceLy != 494.89 {
		t.Errorf("expected DistanceLy 494.89, got %f", hop1.DistanceLy)
	}
	if hop1.TritiumUsed != 91 {
		t.Errorf("reconciliation failed: expected TritiumUsed = 91 (overriding Spansh's 89), got %d", hop1.TritiumUsed)
	}
	if hop1.FuelInTank != 1000 {
		t.Errorf("expected FuelInTank = 1000 (overriding Spansh's 437), got %d", hop1.FuelInTank)
	}

	// Jump 2: 120.00 Ly, UsedCapacity=8548, Depot=1000
	// Mass = 34548
	// 5 + 120.0 * 34548 / 200000 = 5 + 20.7288 = 25.7288 -> ceil = 26
	hop2 := reconciled[1]
	if hop2.TritiumUsed != 26 {
		t.Errorf("expected hop2 TritiumUsed = 26, got %d", hop2.TritiumUsed)
	}
}

func TestReconcileSpanshJumpsDepotOverride(t *testing.T) {
	// If the user specifies a non-standard depot value, e.g. 500 tonnes:
	rawJumps := []SpanshRawJump{
		{
			Name:                  "Test Hop",
			Distance:              500.0,
			DistanceToDestination: 0,
			FuelUsed:              999,
		},
	}
	// Empty load (0 capacity), 1000 depot: ceil(5 + 500 * 26000 / 200000) = 70
	jumps1000 := ReconcileSpanshJumps(rawJumps, 0, 1000)
	if jumps1000[0].TritiumUsed != 70 {
		t.Errorf("expected 70 tonnes for full depot, got %d", jumps1000[0].TritiumUsed)
	}

	// Empty load (0 capacity), 500 depot:
	// Mass = 25000 + 0 + 500 = 25500
	// 5 + 500 * 25500 / 200000 = 5 + 63.75 = 68.75 -> ceil = 69
	jumps500 := ReconcileSpanshJumps(rawJumps, 0, 500)
	if jumps500[0].TritiumUsed != 69 {
		t.Errorf("expected 69 tonnes for 500t depot, got %d", jumps500[0].TritiumUsed)
	}
}

func TestReconcileSpanshJumpsSquadronCarrier(t *testing.T) {
	// Squadron Carriers feature up to 60,000 tonnes capacity
	rawJumps := []SpanshRawJump{
		{
			Name:                  "Deep Space Hop",
			Distance:              500.0,
			DistanceToDestination: 0,
			FuelUsed:              999,
		},
	}
	// Fully loaded squadron carrier: 60,000 tonnes used capacity, 1,000 tonnes depot
	// Mass = 25000 + 60000 + 1000 = 86000 tonnes
	// 5 + 500 * 86000 / 200000 = 5 + 215 = 220 tonnes
	jumps := ReconcileSpanshJumps(rawJumps, 60000, 1000)
	if jumps[0].TritiumUsed != 220 {
		t.Errorf("expected 220 tonnes for 60,000t squadron carrier, got %d", jumps[0].TritiumUsed)
	}
}

