package expedition_test

import (
	"database/sql"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/store/expedition"
)

func setRowSystemName(testingContext *testing.T, databaseConnection *sql.DB, rowID int64, systemName string) {
	testingContext.Helper()
	if _, updateError := expedition.UpdateRow(databaseConnection, expedition.RowPatch{ID: rowID, SystemName: &systemName}); updateError != nil {
		testingContext.Fatal(updateError)
	}
}

func createExpeditionWithOriginRow(testingContext *testing.T, databaseConnection *sql.DB, expeditionName, expeditionDescription string) (expedition.Expedition, expedition.Row) {
	testingContext.Helper()
	expeditionRecord, createError := expedition.Create(databaseConnection, expeditionName, expeditionDescription)
	if createError != nil {
		testingContext.Fatal(createError)
	}
	initialRows, listError := expedition.ListRows(databaseConnection, expeditionRecord.ID)
	if listError != nil || len(initialRows) != 1 {
		testingContext.Fatalf("expected 1 initial row, got %d (%v)", len(initialRows), listError)
	}
	return expeditionRecord, initialRows[0]
}

func mustInsert(testingContext *testing.T, databaseConnection *sql.DB, expeditionID, afterID int64, rowType string, regionID int) expedition.Row {
	testingContext.Helper()
	var insertedRow expedition.Row
	if txError := db.WithTx(databaseConnection, func(transaction *sql.Tx) error {
		var insertError error
		insertedRow, insertError = expedition.InsertRow(transaction, expeditionID, afterID, rowType, regionID)
		return insertError
	}); txError != nil {
		testingContext.Fatal(txError)
	}
	return insertedRow
}

func mustGetExpedition(testingContext *testing.T, databaseConnection *sql.DB, expeditionID int64) expedition.Expedition {
	testingContext.Helper()
	record, getError := expedition.Get(databaseConnection, expeditionID)
	if getError != nil {
		testingContext.Fatalf("expedition.Get(%d): %v", expeditionID, getError)
	}
	return record
}

func mustGetRow(testingContext *testing.T, databaseConnection *sql.DB, rowID int64) expedition.Row {
	testingContext.Helper()
	rowItem, getError := expedition.GetRow(databaseConnection, rowID)
	if getError != nil {
		testingContext.Fatalf("GetRow(%d): %v", rowID, getError)
	}
	return rowItem
}

func mustGetActiveCarrierRoute(testingContext *testing.T, databaseConnection *sql.DB, expeditionID int64) *expedition.ActiveCarrierRouteState {
	testingContext.Helper()
	routeState, routeError := expedition.GetActiveCarrierRoute(databaseConnection, expeditionID)
	if routeError != nil {
		testingContext.Fatalf("GetActiveCarrierRoute(%d): %v", expeditionID, routeError)
	}
	return routeState
}

func mustCaptureSnapshot(testingContext *testing.T, databaseConnection *sql.DB, expeditionID int64) expedition.Snapshot {
	testingContext.Helper()
	snapshot, captureError := expedition.CaptureSnapshot(databaseConnection, expeditionID)
	if captureError != nil {
		testingContext.Fatalf("CaptureSnapshot(%d): %v", expeditionID, captureError)
	}
	return snapshot
}

func mustCreateWithWaypoints(
	testingContext *testing.T,
	databaseConnection *sql.DB,
	expeditionName, expeditionDescription string,
	waypoints ...expedition.SeedWaypoint,
) expedition.Expedition {
	testingContext.Helper()
	expeditionRecord, createError := expedition.CreateWithWaypoints(databaseConnection, expeditionName, expeditionDescription, waypoints)
	if createError != nil {
		testingContext.Fatalf("CreateWithWaypoints(%q): %v", expeditionName, createError)
	}
	return expeditionRecord
}
