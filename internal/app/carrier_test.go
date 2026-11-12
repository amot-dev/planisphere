package app

import (
	"sync"
	"testing"

	"planisphere/internal/expedition"
	"planisphere/internal/stats"
)

func TestGetCarrierLastLocation(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	// Initially no events
	location, getLocationError := testApp.GetCarrierLastLocation()
	if getLocationError != nil || location != "" {
		testingContext.Fatalf("expected empty location, got '%s', err: %v", location, getLocationError)
	}

	// Old CarrierJump in System A
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T00:00:00Z", "CarrierJump", "uid1",
		`{"event":"CarrierJump","StarSystem":"System A"}`)

	location, getLocationError = testApp.GetCarrierLastLocation()
	if getLocationError != nil || location != "System A" {
		testingContext.Fatalf("expected System A from CarrierJump, got '%s'", location)
	}

	// CarrierStats identifying carrier ID 98765
	insertTestJournalEvent(testingContext, testApp, "2026-01-01T01:00:00Z", "CarrierStats", "uid2",
		`{"event":"CarrierStats","CarrierID":98765,"Callsign":"ABC-123"}`)

	// Newer docking event on player's fleet carrier in System B
	insertTestJournalEvent(testingContext, testApp, "2026-01-05T00:00:00Z", "Docked", "uid3",
		`{"event":"Docked","StarSystem":"System B","StationType":"FleetCarrier","MarketID":98765,"StationName":"ABC-123"}`)

	location, getLocationError = testApp.GetCarrierLastLocation()
	if getLocationError != nil || location != "System B" {
		testingContext.Fatalf("expected System B from newer Docked event, got '%s'", location)
	}

	// Even newer remote jump request to System C
	insertTestJournalEvent(testingContext, testApp, "2026-01-10T00:00:00Z", "CarrierJumpRequest", "uid4",
		`{"event":"CarrierJumpRequest","SystemName":"System C","CarrierID":98765}`)

	location, getLocationError = testApp.GetCarrierLastLocation()
	if getLocationError != nil || location != "System C" {
		testingContext.Fatalf("expected System C from latest CarrierJumpRequest, got '%s'", location)
	}
}

func TestRecordCarrierJump(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	var emittedEvents []DataChanged
	var emitMu sync.Mutex
	testApp.emit = func(event string, data ...any) {
		if event == EventDataChanged && len(data) > 0 {
			if dataChangedEvent, ok := data[0].(DataChanged); ok {
				emitMu.Lock()
				emittedEvents = append(emittedEvents, dataChangedEvent)
				emitMu.Unlock()
			}
		}
	}

	expeditionRecord, createError := testApp.CreateExpedition("Carrier Test", "Testing carrier jumps")
	if createError != nil {
		testingContext.Fatalf("failed to create expedition: %v", createError)
	}

	// Clear events from creation
	emitMu.Lock()
	emittedEvents = nil
	emitMu.Unlock()

	// Record a carrier jump of 498.5 Ly using 95t tritium
	if err := testApp.RecordCarrierJump(expeditionRecord.ID, 498.5, 95); err != nil {
		testingContext.Fatalf("RecordCarrierJump failed: %v", err)
	}

	// Verify emitted event scope is strictly "adjustments", not "all"
	emitMu.Lock()
	if len(emittedEvents) != 1 {
		testingContext.Fatalf("expected 1 event, got %d", len(emittedEvents))
	}
	if emittedEvents[0].Scope != "adjustments" {
		testingContext.Errorf("expected scope 'adjustments', got '%s'", emittedEvents[0].Scope)
	}
	if emittedEvents[0].ExpeditionID != expeditionRecord.ID {
		testingContext.Errorf("expected expeditionID %d, got %d", expeditionRecord.ID, emittedEvents[0].ExpeditionID)
	}
	emitMu.Unlock()

	// Verify adjustments were correctly written
	planView, getPlanError := testApp.GetPlan(expeditionRecord.ID)
	if getPlanError != nil {
		testingContext.Fatalf("GetPlan failed: %v", getPlanError)
	}
	initialAdjustments := planView.Expedition.Adjustments
	if initialAdjustments.CarrierJumps != 1 {
		testingContext.Errorf("expected 1 carrier jump, got %d", initialAdjustments.CarrierJumps)
	}
	if initialAdjustments.CarrierDistanceLy != 498.5 {
		testingContext.Errorf("expected 498.5 carrier distance, got %f", initialAdjustments.CarrierDistanceLy)
	}
	if initialAdjustments.TritiumUsed == nil || *initialAdjustments.TritiumUsed != 95 {
		testingContext.Errorf("expected 95 tritium, got %v", initialAdjustments.TritiumUsed)
	}

	// Record a second carrier jump
	if err := testApp.RecordCarrierJump(expeditionRecord.ID, 120.0, 25); err != nil {
		testingContext.Fatalf("second RecordCarrierJump failed: %v", err)
	}
	planView2, _ := testApp.GetPlan(expeditionRecord.ID)
	updatedAdjustments := planView2.Expedition.Adjustments
	if updatedAdjustments.CarrierJumps != 2 {
		testingContext.Errorf("expected 2 carrier jumps, got %d", updatedAdjustments.CarrierJumps)
	}
	if updatedAdjustments.CarrierDistanceLy != 618.5 {
		testingContext.Errorf("expected 618.5 carrier distance, got %f", updatedAdjustments.CarrierDistanceLy)
	}
	if updatedAdjustments.TritiumUsed == nil || *updatedAdjustments.TritiumUsed != 120 {
		testingContext.Errorf("expected 120 tritium, got %v", updatedAdjustments.TritiumUsed)
	}
}

func TestLiveStatsCacheOnCarrierJump(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)

	expeditionRecord, createError := testApp.CreateExpedition("Stats Cache Test", "Testing live stats caching")
	if createError != nil {
		testingContext.Fatalf("failed to create expedition: %v", createError)
	}

	plan, _ := testApp.GetPlan(expeditionRecord.ID)
	if len(plan.Rows) > 0 {
		originSystemName := "Sol"
		_ = testApp.UpdateRow(expedition.RowPatch{ID: plan.Rows[0].Row.ID, SystemName: &originSystemName})
	}

	// Start expedition so stats computing is active
	if err := testApp.StartExpedition(expeditionRecord.ID, "2026-01-01T00:00:00Z"); err != nil {
		testingContext.Fatalf("StartExpedition failed: %v", err)
	}

	filter := stats.Filter{Enabled: false}

	// First call to GetSidebarStats should populate liveStatsCache
	sidebar1, err := testApp.GetSidebarStats(expeditionRecord.ID, filter)
	if err != nil {
		testingContext.Fatalf("GetSidebarStats failed: %v", err)
	}
	if sidebar1.Expedition.CarrierJumps != 0 {
		testingContext.Errorf("expected 0 carrier jumps, got %d", sidebar1.Expedition.CarrierJumps)
	}

	testApp.liveStatsMu.Lock()
	initialCacheEntryCount := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if initialCacheEntryCount == 0 {
		testingContext.Fatal("expected liveStatsCache to have at least 1 entry")
	}

	// Record carrier jump
	if err := testApp.RecordCarrierJump(expeditionRecord.ID, 500.0, 100); err != nil {
		testingContext.Fatalf("RecordCarrierJump failed: %v", err)
	}

	// liveStatsCache should NOT have been cleared because no raw journal events or modes changed
	testApp.liveStatsMu.Lock()
	cacheEntryCountAfterJump := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if cacheEntryCountAfterJump != initialCacheEntryCount {
		testingContext.Errorf("expected liveStatsCache length %d to be preserved, got %d", initialCacheEntryCount, cacheEntryCountAfterJump)
	}

	// Second call to GetSidebarStats uses the cached live metrics and combines with new adjustments
	sidebar2, err := testApp.GetSidebarStats(expeditionRecord.ID, filter)
	if err != nil {
		testingContext.Fatalf("second GetSidebarStats failed: %v", err)
	}
	if sidebar2.Expedition.CarrierJumps != 1 {
		testingContext.Errorf("expected 1 carrier jump, got %d", sidebar2.Expedition.CarrierJumps)
	}
	if sidebar2.Expedition.CarrierDistanceLy != 500.0 {
		testingContext.Errorf("expected 500.0 carrier distance, got %f", sidebar2.Expedition.CarrierDistanceLy)
	}
	if sidebar2.Adjustments.CarrierJumps != 1 {
		testingContext.Errorf("expected 1 adjustment carrier jump, got %d", sidebar2.Adjustments.CarrierJumps)
	}
}

