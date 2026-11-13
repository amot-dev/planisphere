package spansh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReconcileSpanshJumps(t *testing.T) {
	rawJumps := []RawJump{
		{Name: "Origin System", Distance: 0, DistanceToDestination: 614.89, FuelUsed: 0, TritiumUsed: 0, FuelInTank: 437},
		{Name: "Waypoint 1", Distance: 494.89, DistanceToDestination: 120.0, FuelUsed: 89, TritiumUsed: 89, FuelInTank: 437},
		{Name: "Waypoint 2", Distance: 120.0, DistanceToDestination: 0, FuelUsed: 26, TritiumUsed: 26, FuelInTank: 348},
	}

	reconciled := ReconcileJumps(rawJumps, 8548, 1000)
	if len(reconciled) != 2 {
		t.Fatalf("expected 2 jumps, got %d", len(reconciled))
	}
	expectedTritiumCosts := []int{90, 26}
	for index, expectedCost := range expectedTritiumCosts {
		if reconciled[index].TritiumUsed != expectedCost {
			t.Errorf("hop %d: expected TritiumUsed = %d, got %d", index, expectedCost, reconciled[index].TritiumUsed)
		}
	}
	if reconciled[0].SystemName != "Waypoint 1" || reconciled[0].FuelInTank != 1000 {
		t.Errorf("unexpected hop 1 data: %+v", reconciled[0])
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
			originalBaseURL := baseURL
			originalPollInterval := pollInterval
			testingContext.Cleanup(func() {
				baseURL = originalBaseURL
				pollInterval = originalPollInterval
			})
			if testCase.pollInterval > 0 {
				pollInterval = testCase.pollInterval
			} else {
				pollInterval = 5 * time.Millisecond
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
			baseURL = testServer.URL

			requestContext, cancelFunction := testCase.setupCtx(pollStarted)
			defer cancelFunction()

			jumps, queryError := QueryCarrierRoute(requestContext, "Sol", "Colonia", 0, 1000)
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

