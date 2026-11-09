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

	"planisphere/internal/db"
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

func TestGetCarrierLastLocation(t *testing.T) {
	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open :memory: db: %v", err)
	}
	defer testDB.Close()

	testApp := &App{db: testDB}

	// Initially no events
	loc, err := testApp.GetCarrierLastLocation()
	if err != nil || loc != "" {
		t.Fatalf("expected empty location, got '%s', err: %v", loc, err)
	}

	// Old CarrierJump in System A
	_, err = testDB.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at)
		VALUES ('uid1', '2026-01-01T00:00:00Z', 'CarrierJump', 'System A', '{"event":"CarrierJump","StarSystem":"System A"}', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert uid1: %v", err)
	}

	loc, err = testApp.GetCarrierLastLocation()
	if err != nil || loc != "System A" {
		t.Fatalf("expected System A from CarrierJump, got '%s'", loc)
	}

	// CarrierStats identifying carrier ID 98765
	_, err = testDB.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at)
		VALUES ('uid2', '2026-01-01T01:00:00Z', 'CarrierStats', 'System A', '{"event":"CarrierStats","CarrierID":98765,"Callsign":"ABC-123"}', '2026-01-01T01:00:00Z')`)
	if err != nil {
		t.Fatalf("insert uid2: %v", err)
	}

	// Newer docking event on player's fleet carrier in System B
	_, err = testDB.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at)
		VALUES ('uid3', '2026-01-05T00:00:00Z', 'Docked', 'System B', '{"event":"Docked","StarSystem":"System B","StationType":"FleetCarrier","MarketID":98765,"StationName":"ABC-123"}', '2026-01-05T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert uid3: %v", err)
	}

	loc, err = testApp.GetCarrierLastLocation()
	if err != nil || loc != "System B" {
		t.Fatalf("expected System B from newer Docked event, got '%s'", loc)
	}

	// Even newer remote jump request to System C
	_, err = testDB.Exec(`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at)
		VALUES ('uid4', '2026-01-10T00:00:00Z', 'CarrierJumpRequest', 'System C', '{"event":"CarrierJumpRequest","SystemName":"System C","CarrierID":98765}', '2026-01-10T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert uid4: %v", err)
	}

	loc, err = testApp.GetCarrierLastLocation()
	if err != nil || loc != "System C" {
		t.Fatalf("expected System C from latest CarrierJumpRequest, got '%s'", loc)
	}
}

func TestQuerySpanshCarrierRoute_ContextCancellationAndTimeout(t *testing.T) {
	testCases := []struct {
		name        string
		setupCtx    func(pollStarted chan struct{}) (context.Context, context.CancelFunc)
		expectedErr error
	}{
		{
			name: "ContextCanceledDuringPollDo",
			setupCtx: func(pollStarted chan struct{}) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					<-pollStarted
					cancel()
				}()
				return ctx, cancel
			},
			expectedErr: context.Canceled,
		},
		{
			name: "ContextDeadlineExceeded",
			setupCtx: func(pollStarted chan struct{}) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				return ctx, cancel
			},
			expectedErr: context.DeadlineExceeded,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			originalURL := spanshBaseURL
			originalInterval := spanshPollInterval
			t.Cleanup(func() {
				spanshBaseURL = originalURL
				spanshPollInterval = originalInterval
			})
			spanshPollInterval = 5 * time.Millisecond

			pollStarted := make(chan struct{})
			testServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/api/fleetcarrier/route" {
					rw.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(rw).Encode(map[string]string{"job": "job-test"})
					return
				}
				if strings.HasPrefix(req.URL.Path, "/api/results/") {
					select {
					case <-pollStarted:
					default:
						close(pollStarted)
					}
					<-req.Context().Done()
					return
				}
				http.NotFound(rw, req)
			}))
			defer testServer.Close()
			spanshBaseURL = testServer.URL

			ctx, cancel := tc.setupCtx(pollStarted)
			defer cancel()

			jumps, queryError := querySpanshCarrierRoute(ctx, "Sol", "Colonia", 0, 1000)
			if jumps != nil {
				t.Fatalf("expected nil jumps, got %v", jumps)
			}
			if queryError == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(queryError, tc.expectedErr) {
				t.Errorf("expected %v error, got: %v", tc.expectedErr, queryError)
			}
			if !strings.Contains(queryError.Error(), "Spansh route calculation canceled or timed out") {
				t.Errorf("expected error message to contain cancellation text, got: %v", queryError)
			}
		})
	}
}

func TestQuerySpanshCarrierRoute_ContextCanceledDuringTickerWait(t *testing.T) {
	originalURL := spanshBaseURL
	originalInterval := spanshPollInterval
	t.Cleanup(func() {
		spanshBaseURL = originalURL
		spanshPollInterval = originalInterval
	})
	// Long poll interval so select case <-ctx.Done() fires while waiting for ticker
	spanshPollInterval = 1 * time.Second

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		if httpRequest.URL.Path == "/api/fleetcarrier/route" {
			responseWriter.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(responseWriter).Encode(map[string]string{"job": "job-cancel-wait"})
			return
		}
		http.NotFound(responseWriter, httpRequest)
	}))
	defer testServer.Close()

	spanshBaseURL = testServer.URL

	// Cancel after short delay so initial route creation completes and loop enters ticker wait
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancelFunc()
	}()

	jumps, queryError := querySpanshCarrierRoute(ctx, "Sol", "Colonia", 0, 1000)
	if jumps != nil {
		t.Fatalf("expected nil jumps, got %v", jumps)
	}
	if queryError == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(queryError, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", queryError)
	}
	if !strings.Contains(queryError.Error(), "Spansh route calculation canceled or timed out") {
		t.Errorf("expected error message to contain cancellation text, got: %v", queryError)
	}
}

func TestRecordCarrierJump(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := tempDir + "/test.sqlite"

	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := testApp.initialize(ctx, dbPath); err != nil {
		t.Fatalf("initialize db: %v", err)
	}
	defer testApp.shutdown(ctx)

	var emittedEvents []DataChanged
	var emitMu sync.Mutex
	testApp.emit = func(event string, data ...any) {
		if event == EventDataChanged && len(data) > 0 {
			if dc, ok := data[0].(DataChanged); ok {
				emitMu.Lock()
				emittedEvents = append(emittedEvents, dc)
				emitMu.Unlock()
			}
		}
	}

	exp, createErr := testApp.CreateExpedition("Carrier Test", "Testing carrier jumps")
	if createErr != nil {
		t.Fatalf("failed to create expedition: %v", createErr)
	}

	// Clear events from creation
	emitMu.Lock()
	emittedEvents = nil
	emitMu.Unlock()

	// Record a carrier jump of 498.5 Ly using 95t tritium
	if err := testApp.RecordCarrierJump(exp.ID, 498.5, 95); err != nil {
		t.Fatalf("RecordCarrierJump failed: %v", err)
	}

	// Verify emitted event scope is strictly "adjustments", not "all"
	emitMu.Lock()
	if len(emittedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(emittedEvents))
	}
	if emittedEvents[0].Scope != "adjustments" {
		t.Errorf("expected scope 'adjustments', got '%s'", emittedEvents[0].Scope)
	}
	if emittedEvents[0].ExpeditionID != exp.ID {
		t.Errorf("expected expeditionID %d, got %d", exp.ID, emittedEvents[0].ExpeditionID)
	}
	emitMu.Unlock()

	// Verify adjustments were correctly written
	planView, getErr := testApp.GetPlan(exp.ID)
	if getErr != nil {
		t.Fatalf("GetPlan failed: %v", getErr)
	}
	adj := planView.Expedition.Adjustments
	if adj.CarrierJumps != 1 {
		t.Errorf("expected 1 carrier jump, got %d", adj.CarrierJumps)
	}
	if adj.CarrierDistanceLy != 498.5 {
		t.Errorf("expected 498.5 carrier distance, got %f", adj.CarrierDistanceLy)
	}
	if adj.TritiumUsed == nil || *adj.TritiumUsed != 95 {
		t.Errorf("expected 95 tritium, got %v", adj.TritiumUsed)
	}

	// Record a second carrier jump
	if err := testApp.RecordCarrierJump(exp.ID, 120.0, 25); err != nil {
		t.Fatalf("second RecordCarrierJump failed: %v", err)
	}
	planView2, _ := testApp.GetPlan(exp.ID)
	adj2 := planView2.Expedition.Adjustments
	if adj2.CarrierJumps != 2 {
		t.Errorf("expected 2 carrier jumps, got %d", adj2.CarrierJumps)
	}
	if adj2.CarrierDistanceLy != 618.5 {
		t.Errorf("expected 618.5 carrier distance, got %f", adj2.CarrierDistanceLy)
	}
	if adj2.TritiumUsed == nil || *adj2.TritiumUsed != 120 {
		t.Errorf("expected 120 tritium, got %v", adj2.TritiumUsed)
	}
}

func TestLiveStatsCacheOnCarrierJump(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := tempDir + "/test.sqlite"

	testApp := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := testApp.initialize(ctx, dbPath); err != nil {
		t.Fatalf("initialize db: %v", err)
	}
	defer testApp.shutdown(ctx)

	exp, createErr := testApp.CreateExpedition("Stats Cache Test", "Testing live stats caching")
	if createErr != nil {
		t.Fatalf("failed to create expedition: %v", createErr)
	}

	plan, _ := testApp.GetPlan(exp.ID)
	if len(plan.Rows) > 0 {
		sysName := "Sol"
		_ = testApp.UpdateRow(expedition.RowPatch{ID: plan.Rows[0].Row.ID, SystemName: &sysName})
	}

	// Start expedition so stats computing is active
	if err := testApp.StartExpedition(exp.ID, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("StartExpedition failed: %v", err)
	}

	filter := stats.Filter{Enabled: false}

	// First call to GetSidebarStats should populate liveStatsCache
	sidebar1, err := testApp.GetSidebarStats(exp.ID, filter)
	if err != nil {
		t.Fatalf("GetSidebarStats failed: %v", err)
	}
	if sidebar1.Expedition.CarrierJumps != 0 {
		t.Errorf("expected 0 carrier jumps, got %d", sidebar1.Expedition.CarrierJumps)
	}

	testApp.liveStatsMu.Lock()
	cacheLen := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if cacheLen == 0 {
		t.Fatal("expected liveStatsCache to have at least 1 entry")
	}

	// Record carrier jump
	if err := testApp.RecordCarrierJump(exp.ID, 500.0, 100); err != nil {
		t.Fatalf("RecordCarrierJump failed: %v", err)
	}

	// liveStatsCache should NOT have been cleared because no raw journal events or modes changed
	testApp.liveStatsMu.Lock()
	cacheLenAfter := len(testApp.liveStatsCache)
	testApp.liveStatsMu.Unlock()
	if cacheLenAfter != cacheLen {
		t.Errorf("expected liveStatsCache length %d to be preserved, got %d", cacheLen, cacheLenAfter)
	}

	// Second call to GetSidebarStats uses the cached live metrics and combines with new adjustments
	sidebar2, err := testApp.GetSidebarStats(exp.ID, filter)
	if err != nil {
		t.Fatalf("second GetSidebarStats failed: %v", err)
	}
	if sidebar2.Expedition.CarrierJumps != 1 {
		t.Errorf("expected 1 carrier jump, got %d", sidebar2.Expedition.CarrierJumps)
	}
	if sidebar2.Expedition.CarrierDistanceLy != 500.0 {
		t.Errorf("expected 500.0 carrier distance, got %f", sidebar2.Expedition.CarrierDistanceLy)
	}
	if sidebar2.Adjustments.CarrierJumps != 1 {
		t.Errorf("expected 1 adjustment carrier jump, got %d", sidebar2.Adjustments.CarrierJumps)
	}
}



