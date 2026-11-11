package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"planisphere/internal/expedition"
	"planisphere/internal/stats"
)

func TestReconcileSpanshJumps(t *testing.T) {
	testCases := []struct {
		name                 string
		rawJumps             []SpanshRawJump
		usedCapacity         int
		depotTritium         int
		expectedCount        int
		expectedTritiumCosts []int
		validate             func(t *testing.T, reconciled []CarrierRouteJump)
	}{
		{
			name: "StandardReconciliationAndOriginSkip",
			rawJumps: []SpanshRawJump{
				{Name: "Origin System", Distance: 0, DistanceToDestination: 614.89, FuelUsed: 0, TritiumUsed: 0, FuelInTank: 437},
				{Name: "Waypoint 1", Distance: 494.89, DistanceToDestination: 120.0, FuelUsed: 89, TritiumUsed: 89, FuelInTank: 437},
				{Name: "Waypoint 2", Distance: 120.0, DistanceToDestination: 0, FuelUsed: 26, TritiumUsed: 26, FuelInTank: 348},
			},
			usedCapacity:         8548,
			depotTritium:         1000,
			expectedCount:        2,
			expectedTritiumCosts: []int{90, 26},
			validate: func(t *testing.T, reconciled []CarrierRouteJump) {
				if reconciled[0].SystemName != "Waypoint 1" || reconciled[0].FuelInTank != 1000 {
					t.Errorf("unexpected hop 1 data: %+v", reconciled[0])
				}
			},
		},
		{
			name: "InGameVerifiedHop",
			rawJumps: []SpanshRawJump{
				{Name: "NGC 7789 Sector MH-W b31-0", Distance: 0, DistanceToDestination: 495.27},
				{Name: "Slegoae NS-U d2-2", Distance: 495.27, DistanceToDestination: 0},
			},
			usedCapacity:         8548,
			depotTritium:         818,
			expectedCount:        1,
			expectedTritiumCosts: []int{90},
		},
		{
			name: "DepotOverrideFull",
			rawJumps: []SpanshRawJump{
				{Name: "Test Hop", Distance: 500.0, DistanceToDestination: 0, FuelUsed: 999},
			},
			usedCapacity:         0,
			depotTritium:         1000,
			expectedCount:        1,
			expectedTritiumCosts: []int{70},
		},
		{
			name: "DepotOverridePartial",
			rawJumps: []SpanshRawJump{
				{Name: "Test Hop", Distance: 500.0, DistanceToDestination: 0, FuelUsed: 999},
			},
			usedCapacity:         0,
			depotTritium:         500,
			expectedCount:        1,
			expectedTritiumCosts: []int{69},
		},
		{
			name: "SquadronCarrierHighCapacity",
			rawJumps: []SpanshRawJump{
				{Name: "Deep Space Hop", Distance: 500.0, DistanceToDestination: 0, FuelUsed: 999},
			},
			usedCapacity:         60000,
			depotTritium:         1000,
			expectedCount:        1,
			expectedTritiumCosts: []int{220},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reconciled := ReconcileSpanshJumps(tc.rawJumps, tc.usedCapacity, tc.depotTritium)
			if len(reconciled) != tc.expectedCount {
				t.Fatalf("expected %d jumps, got %d", tc.expectedCount, len(reconciled))
			}
			for i, wantCost := range tc.expectedTritiumCosts {
				if reconciled[i].TritiumUsed != wantCost {
					t.Errorf("hop %d: expected TritiumUsed = %d, got %d", i, wantCost, reconciled[i].TritiumUsed)
				}
			}
			if tc.validate != nil {
				tc.validate(t, reconciled)
			}
		})
	}
}

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

func TestQuerySpanshCarrierRoute_ContextCancellationAndTimeout(testingContext *testing.T) {
	testCases := []struct {
		name         string
		pollInterval time.Duration
		setupCtx     func(pollStarted chan struct{}) (context.Context, context.CancelFunc)
		expectedErr  error
	}{
		{
			name: "ContextCanceledDuringPollDo",
			setupCtx: func(pollStarted chan struct{}) (context.Context, context.CancelFunc) {
				requestContext, cancelFunction := context.WithCancel(context.Background())
				go func() {
					<-pollStarted
					cancelFunction()
				}()
				return requestContext, cancelFunction
			},
			expectedErr: context.Canceled,
		},
		{
			name: "ContextDeadlineExceeded",
			setupCtx: func(pollStarted chan struct{}) (context.Context, context.CancelFunc) {
				requestContext, cancelFunction := context.WithTimeout(context.Background(), 50*time.Millisecond)
				return requestContext, cancelFunction
			},
			expectedErr: context.DeadlineExceeded,
		},
		{
			name:         "ContextCanceledDuringTickerWait",
			pollInterval: 1 * time.Second,
			setupCtx: func(pollStarted chan struct{}) (context.Context, context.CancelFunc) {
				requestContext, cancelFunction := context.WithCancel(context.Background())
				go func() {
					time.Sleep(30 * time.Millisecond)
					cancelFunction()
				}()
				return requestContext, cancelFunction
			},
			expectedErr: context.Canceled,
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(testingContext *testing.T) {
			originalBaseURL := spanshBaseURL
			originalPollInterval := spanshPollInterval
			testingContext.Cleanup(func() {
				spanshBaseURL = originalBaseURL
				spanshPollInterval = originalPollInterval
			})
			if testCase.pollInterval > 0 {
				spanshPollInterval = testCase.pollInterval
			} else {
				spanshPollInterval = 5 * time.Millisecond
			}

			pollStarted := make(chan struct{})
			testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
				if httpRequest.URL.Path == "/api/fleetcarrier/route" {
					responseWriter.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(responseWriter).Encode(map[string]string{"job": "job-test"})
					return
				}
				if strings.HasPrefix(httpRequest.URL.Path, "/api/results/") {
					select {
					case <-pollStarted:
					default:
						close(pollStarted)
					}
					<-httpRequest.Context().Done()
					return
				}
				http.NotFound(responseWriter, httpRequest)
			}))
			defer testServer.Close()
			spanshBaseURL = testServer.URL

			requestContext, cancelFunction := testCase.setupCtx(pollStarted)
			defer cancelFunction()

			jumps, queryError := querySpanshCarrierRoute(requestContext, "Sol", "Colonia", 0, 1000)
			if jumps != nil {
				testingContext.Fatalf("expected nil jumps, got %v", jumps)
			}
			if queryError == nil {
				testingContext.Fatal("expected error, got nil")
			}
			if !errors.Is(queryError, testCase.expectedErr) {
				testingContext.Errorf("expected %v error, got: %v", testCase.expectedErr, queryError)
			}
			if !strings.Contains(queryError.Error(), "Spansh route calculation canceled or timed out") {
				testingContext.Errorf("expected error message to contain cancellation text, got: %v", queryError)
			}
		})
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



