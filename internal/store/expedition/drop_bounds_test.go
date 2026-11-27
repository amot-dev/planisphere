package expedition_test

import (
	"database/sql"
	"fmt"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/expeditiontest"
	"planisphere/internal/testutil/journaltest"
)

func TestCarrierFrontierDropBoundsAndArrivalOverride(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	_, proj := expeditiontest.NewProjections(databaseConnection)

	journaltest.IngestLines(testingContext, databaseConnection,
		`{"timestamp":"2026-01-18T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-18T11:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1004,"StarPos":[5,0,0],"JumpDist":5.0}`,
		`{"timestamp":"2026-01-18T12:00:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[4,0,0],"JumpDist":4.0}`,
		`{"timestamp":"2026-01-18T13:00:00Z","event":"FSDJump","StarSystem":"Sirius","SystemAddress":1003,"StarPos":[8,0,0],"JumpDist":8.0}`,
		`{"timestamp":"2026-01-18T14:00:00Z","event":"FSDJump","StarSystem":"Barnard's Star","SystemAddress":1004,"StarPos":[5,0,0],"JumpDist":5.0}`,
	)

	expeditionRecord, solWaypoint := createExpeditionWithOriginRow(testingContext, databaseConnection, "Carrier Frontier Test", "")
	setRowSystemName(testingContext, databaseConnection, solWaypoint.ID, "Sol")
	alphaCentauriWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, solWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, alphaCentauriWaypoint.ID, "Alpha Centauri")
	siriusWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, alphaCentauriWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, siriusWaypoint.ID, "Sirius")
	barnardWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, siriusWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, barnardWaypoint.ID, "Barnard's Star")

	fcTrue := true
	for _, stopRowID := range []int64{solWaypoint.ID, siriusWaypoint.ID, barnardWaypoint.ID} {
		if _, updateStopErr := expedition.UpdateRow(databaseConnection, expedition.RowPatch{ID: stopRowID, FCStop: &fcTrue}); updateStopErr != nil {
			testingContext.Fatalf("set FCStop on row %d: %v", stopRowID, updateStopErr)
		}
	}

	if startErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		return expedition.Start(tx, expeditionRecord.ID, "2026-01-18T10:00:00Z", 0)
	}); startErr != nil {
		testingContext.Fatal(startErr)
	}
	startedOriginRow, startOriginErr := expedition.GetRow(databaseConnection, solWaypoint.ID)
	if startOriginErr != nil {
		testingContext.Fatal(startOriginErr)
	}
	if startedOriginRow.FCArrived {
		testingContext.Fatalf("expected FC stops to start as pending (FCArrived=false) on Engage")
	}

	// Mark siriusWaypoint (Sirius) as carrier arrived via SetCarrierArrivalOverride; solWaypoint and siriusWaypoint are now ARRIVED, barnardWaypoint is PENDING.
	if overrideErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		_, err := expedition.SetCarrierArrivalOverride(tx, siriusWaypoint.ID, true)
		return err
	}); overrideErr != nil {
		testingContext.Fatal(overrideErr)
	}

	barnardDropPreviews, barnardPreviewError := expedition.ComputeRowDropPreviews(databaseConnection, barnardWaypoint.ID, proj)
	if barnardPreviewError != nil {
		testingContext.Fatal(barnardPreviewError)
	}
	// barnardWaypoint ("Barnard's Star") was visited at 11:00 (between solWaypoint Sol@10:00 and alphaCentauriWaypoint Alpha Centauri@12:00),
	// so index 1 is chronologically valid, but blocked_fc because barnardWaypoint is a PENDING FC stop and siriusWaypoint (at index 2) is ARRIVED!
	if barnardDropPreviews[1].Status != expedition.DropSlotBlockedFC {
		testingContext.Fatalf("expected barnardWaypoint drop preview at index 1 to be blocked_fc, got %+v", barnardDropPreviews[1])
	}
	siriusDropPreviews, siriusPreviewError := expedition.ComputeRowDropPreviews(databaseConnection, siriusWaypoint.ID, proj)
	if siriusPreviewError != nil {
		testingContext.Fatal(siriusPreviewError)
	}
	// siriusWaypoint ("Sirius", ARRIVED) moved to index 3 (below PENDING barnardWaypoint) sets ResetsFCArrived = true.
	if !siriusDropPreviews[3].ResetsFCArrived {
		testingContext.Fatalf("expected siriusWaypoint drop preview at index 3 to set ResetsFCArrived=true, got %+v", siriusDropPreviews[3])
	}

	// Add an unvisited non-FC waypoint vegaWaypoint ("Vega") before PENDING barnardWaypoint; moving ARRIVED siriusWaypoint into the unvisited
	// slot after vegaWaypoint (but still before PENDING barnardWaypoint) must NOT set ResetsFCArrived.
	vegaWaypoint := mustInsert(testingContext, databaseConnection, expeditionRecord.ID, siriusWaypoint.ID, expedition.RowWaypoint, 0)
	setRowSystemName(testingContext, databaseConnection, vegaWaypoint.ID, "Vega")
	if reevalErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		_, err := expedition.Reevaluate(tx, expeditionRecord.ID)
		return err
	}); reevalErr != nil {
		testingContext.Fatal(reevalErr)
	}
	siriusPreviewsWithUnvisited, siriusUnvisitedPreviewError := expedition.ComputeRowDropPreviews(databaseConnection, siriusWaypoint.ID, proj)
	if siriusUnvisitedPreviewError != nil {
		testingContext.Fatal(siriusUnvisitedPreviewError)
	}
	// Moving siriusWaypoint (index 2, ARRIVED) to index 3 (after unvisited Vega, but before barnardWaypoint at index 4) makes siriusWaypoint unvisited
	// without crossing any PENDING FC stop, so ResetsFCArrived must be false.
	if siriusPreviewsWithUnvisited[3].Status != expedition.DropSlotValidUnvisited || siriusPreviewsWithUnvisited[3].ResetsFCArrived {
		testingContext.Fatalf("expected siriusWaypoint drop preview at unvisited index 3 before pending stop to keep ResetsFCArrived=false, got %+v", siriusPreviewsWithUnvisited[3])
	}
	if moveBeforePendingErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		if _, err := expedition.MoveRow(tx, siriusWaypoint.ID, 3); err != nil {
			return err
		}
		_, err := expedition.Reevaluate(tx, expeditionRecord.ID)
		return err
	}); moveBeforePendingErr != nil {
		testingContext.Fatal(moveBeforePendingErr)
	}
	movedRow3, movedRowErr := expedition.GetRow(databaseConnection, siriusWaypoint.ID)
	if movedRowErr != nil {
		testingContext.Fatalf("GetRow moved siriusWaypoint: %v", movedRowErr)
	}
	if !movedRow3.FCArrived {
		testingContext.Fatalf("expected siriusWaypoint to remain FCArrived=true when moved into unvisited slot before first pending stop")
	}

	// Cascading override: setting solWaypoint to arrived=false cascades to siriusWaypoint as well.
	if cascadeErr := db.WithTx(databaseConnection, func(tx *sql.Tx) error {
		_, err := expedition.SetCarrierArrivalOverride(tx, solWaypoint.ID, false)
		return err
	}); cascadeErr != nil {
		testingContext.Fatal(cascadeErr)
	}
	reloadedAfterCascade, reloadedCascadeErr := expedition.GetRow(databaseConnection, siriusWaypoint.ID)
	if reloadedCascadeErr != nil {
		testingContext.Fatalf("GetRow reloaded siriusWaypoint: %v", reloadedCascadeErr)
	}
	if reloadedAfterCascade.FCArrived {
		testingContext.Fatalf("expected SetCarrierArrivalOverride(solWaypoint, false) to cascade to siriusWaypoint")
	}

	// Active Spansh route to siriusWaypoint (now at index 3) locks siriusWaypoint against moves/deletes, blocks moving PENDING barnardWaypoint above siriusWaypoint,
	// and is automatically cleared if barnardWaypoint is manually overridden to ARRIVED.
	spanshRouteKey := fmt.Sprintf("active_carrier_route_%d", expeditionRecord.ID)
	spanshRouteJSON := fmt.Sprintf(`{"expeditionId":%d,"targetRowId":%d,"manualMode":false,"route":{"manualMode":false,"jumps":[{"systemName":"Sirius"}]}}`, expeditionRecord.ID, siriusWaypoint.ID)
	if setRouteError := settings.Set(databaseConnection, spanshRouteKey, spanshRouteJSON); setRouteError != nil {
		testingContext.Fatal(setRouteError)
	}
	barnardSpanshBlocked, barnardBlockedError := expedition.ComputeRowDropPreviews(databaseConnection, barnardWaypoint.ID, proj)
	if barnardBlockedError != nil {
		testingContext.Fatal(barnardBlockedError)
	}
	if barnardSpanshBlocked[2].Status != expedition.DropSlotBlockedFC {
		testingContext.Fatalf("expected barnardWaypoint drop before active Spansh target siriusWaypoint to be blocked_fc, got %+v", barnardSpanshBlocked[2])
	}
	if overrideClearError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		_, overrideError := expedition.SetCarrierArrivalOverride(transaction, barnardWaypoint.ID, true)
		return overrideError
	}); overrideClearError != nil {
		testingContext.Fatal(overrideClearError)
	}
	persistedRoute, getRouteError := settings.Get(databaseConnection, spanshRouteKey)
	if getRouteError != nil {
		testingContext.Fatal(getRouteError)
	}
	if persistedRoute != "" {
		testingContext.Fatalf("expected active Spansh route to clear when target stop overridden to arrived, got %q", persistedRoute)
	}
}
