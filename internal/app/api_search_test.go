package app

import (
	"testing"

	"planisphere/internal/store/expedition"
)

func TestAppSearchWaypointsReadyContractAndDelegation(testingContext *testing.T) {
	uninitializedApp := New()
	unreadyResponse, unreadyError := uninitializedApp.SearchWaypoints("Sol")
	if unreadyError == nil {
		testingContext.Fatal("expected error from SearchWaypoints when database is not initialized")
	}
	if unreadyResponse.Results == nil || len(unreadyResponse.Results) != 0 {
		testingContext.Fatalf("expected non-nil empty Results on error, got %+v", unreadyResponse)
	}

	testApp := setupTestApp(testingContext)
	insertTestJournalEvent(
		testingContext,
		testApp,
		"2026-04-01T10:00:00Z",
		"FSDJump",
		"sol-facade-visit",
		`{"timestamp":"2026-04-01T10:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0],"JumpDist":10.0}`,
	)
	if _, createError := expedition.CreateWithWaypoints(testApp.db, "Facade Search", "", []expedition.SeedWaypoint{
		{SystemName: "Sol", POIName: "Earth"},
	}); createError != nil {
		testingContext.Fatalf("CreateWithWaypoints: %v", createError)
	}

	searchResponse, searchError := testApp.SearchWaypoints("Sol")
	if searchError != nil {
		testingContext.Fatalf("SearchWaypoints(Sol): %v", searchError)
	}
	if searchResponse.TotalMatches != 1 || len(searchResponse.Results) != 1 || searchResponse.Results[0].ExpeditionName != "Facade Search" {
		testingContext.Fatalf("unexpected facade search results: %+v", searchResponse)
	}
	if searchResponse.NonWaypointVisits == nil || searchResponse.NonWaypointVisits.TotalCount != 1 {
		testingContext.Fatalf("expected 1 non-waypoint visit through facade, got %+v", searchResponse.NonWaypointVisits)
	}
}
