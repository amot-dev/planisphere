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
	// Raw jumps simulating Spansh API output: origin system with zero distance,
	// followed by a jump where Spansh calculated 89 tonnes assuming a 437 depot,
	// and a subsequent jump.
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

	// First jump: 494.89 Ly, UsedCapacity=8548, Depot=1000
	// Mass = 25000 + 8548 + 1000 = 34548
	// 5 + 494.89 * 34548 / 200000 = 90.487... -> round = 90
	hop1 := reconciled[0]
	if hop1.SystemName != "Waypoint 1" {
		t.Errorf("expected SystemName 'Waypoint 1', got %s", hop1.SystemName)
	}
	if hop1.DistanceLy != 494.89 {
		t.Errorf("expected DistanceLy 494.89, got %f", hop1.DistanceLy)
	}
	if hop1.TritiumUsed != 90 {
		t.Errorf("reconciliation failed: expected TritiumUsed = 90 (overriding Spansh's 89), got %d", hop1.TritiumUsed)
	}
	if hop1.FuelInTank != 1000 {
		t.Errorf("expected FuelInTank = 1000 (overriding Spansh's 437), got %d", hop1.FuelInTank)
	}

	// Second jump: 120.00 Ly, UsedCapacity=8548, Depot=1000
	// Mass = 34548
	// 5 + 120.0 * 34548 / 200000 = 5 + 20.7288 = 25.7288 -> round = 26
	hop2 := reconciled[1]
	if hop2.TritiumUsed != 26 {
		t.Errorf("expected hop2 TritiumUsed = 26, got %d", hop2.TritiumUsed)
	}
}

func TestReconcileSpanshJumpsInGameVerification(t *testing.T) {
	// In-game test case:
	// NGC 7789 Sector MH-W b31-0 -> Slegoae NS-U d2-2 is 495.27ly.
	// Capacity: 8548/25000, Depot: 818/1000.
	// Mass = 25000 + 8548 + 818 = 34366.
	// 5 + 495.27 * 34366 / 200000 = 90.1022... -> round = 90 tonnes.
	rawJumps := []SpanshRawJump{
		{
			Name:                  "NGC 7789 Sector MH-W b31-0",
			Distance:              0,
			DistanceToDestination: 495.27,
		},
		{
			Name:                  "Slegoae NS-U d2-2",
			Distance:              495.27,
			DistanceToDestination: 0,
		},
	}
	reconciled := ReconcileSpanshJumps(rawJumps, 8548, 818)
	if len(reconciled) != 1 {
		t.Fatalf("expected 1 jump, got %d", len(reconciled))
	}
	if reconciled[0].TritiumUsed != 90 {
		t.Errorf("expected TritiumUsed = 90, got %d", reconciled[0].TritiumUsed)
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

func TestQuerySpanshCarrierRoute_ContextCanceledDuringPollDo(t *testing.T) {
	originalURL := spanshBaseURL
	originalInterval := spanshPollInterval
	t.Cleanup(func() {
		spanshBaseURL = originalURL
		spanshPollInterval = originalInterval
	})
	spanshPollInterval = 5 * time.Millisecond

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	pollStartedChannel := make(chan struct{})

	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		if httpRequest.URL.Path == "/api/fleetcarrier/route" {
			responseWriter.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(responseWriter).Encode(map[string]string{"job": "job-cancel-do"})
			return
		}
		if strings.HasPrefix(httpRequest.URL.Path, "/api/results/") {
			close(pollStartedChannel)
			<-httpRequest.Context().Done()
			return
		}
		http.NotFound(responseWriter, httpRequest)
	}))
	defer testServer.Close()

	spanshBaseURL = testServer.URL

	go func() {
		<-pollStartedChannel
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

func TestQuerySpanshCarrierRoute_ContextDeadlineExceeded(t *testing.T) {
	originalURL := spanshBaseURL
	originalInterval := spanshPollInterval
	t.Cleanup(func() {
		spanshBaseURL = originalURL
		spanshPollInterval = originalInterval
	})
	spanshPollInterval = 5 * time.Millisecond

	ctx, cancelFunc := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelFunc()

	testServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		if httpRequest.URL.Path == "/api/fleetcarrier/route" {
			responseWriter.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(responseWriter).Encode(map[string]string{"job": "job-deadline"})
			return
		}
		if strings.HasPrefix(httpRequest.URL.Path, "/api/results/") {
			<-httpRequest.Context().Done()
			return
		}
		http.NotFound(responseWriter, httpRequest)
	}))
	defer testServer.Close()

	spanshBaseURL = testServer.URL

	jumps, queryError := querySpanshCarrierRoute(ctx, "Sol", "Colonia", 0, 1000)
	if jumps != nil {
		t.Fatalf("expected nil jumps, got %v", jumps)
	}
	if queryError == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(queryError, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded error, got: %v", queryError)
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



