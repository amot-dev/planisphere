package modes_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"planisphere/internal/store/modes"
	"planisphere/internal/testutil/dbtest"
)

func TestModeColorValidation(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	createCases := []struct {
		name          string
		modeName      string
		rawColor      string
		expectedColor string
		expectError   bool
	}{
		{name: "valid three digit hex", modeName: "Scout", rawColor: "#abc", expectedColor: "#abc"},
		{name: "valid six digit hex", modeName: "Survey", rawColor: "#00D2FF", expectedColor: "#00D2FF"},
		{name: "blank defaults to amber", modeName: "Fallback", rawColor: "  ", expectedColor: "#FF7100"},
		{name: "non hex characters rejected", modeName: "BadHex", rawColor: "#zzzzzz", expectError: true},
		{name: "css injection payload rejected", modeName: "Injection", rawColor: "#12;x:1", expectError: true},
		{name: "missing hash rejected", modeName: "NoHash", rawColor: "FF7100", expectError: true},
		{name: "wrong length rejected", modeName: "BadLen", rawColor: "#12345", expectError: true},
	}

	var createdModeID int64
	for _, testCase := range createCases {
		testingContext.Run("create_"+testCase.name, func(subContext *testing.T) {
			createdMode, createError := modes.Create(dbConn, testCase.modeName, "desc", testCase.rawColor)
			if testCase.expectError {
				if createError == nil {
					subContext.Fatalf("expected error for colour %q, got mode %+v", testCase.rawColor, createdMode)
				}
				if !strings.Contains(createError.Error(), "expected #RGB or #RRGGBB") {
					subContext.Fatalf("unexpected error message: %v", createError)
				}
				return
			}
			if createError != nil {
				subContext.Fatalf("unexpected Create error for colour %q: %v", testCase.rawColor, createError)
			}
			if createdMode.Color != testCase.expectedColor {
				subContext.Fatalf("expected colour %q, got %q", testCase.expectedColor, createdMode.Color)
			}
			if createdModeID == 0 {
				createdModeID = createdMode.ID
			}
		})
	}

	updateCases := []struct {
		name        string
		rawColor    string
		expectError bool
	}{
		{name: "valid three digit hex update", rawColor: "#f0A"},
		{name: "non hex update rejected", rawColor: "#zzzzzz", expectError: true},
		{name: "css injection update rejected", rawColor: "#12;x:1", expectError: true},
	}

	for _, testCase := range updateCases {
		testingContext.Run("update_"+testCase.name, func(subContext *testing.T) {
			updateError := modes.Update(dbConn, createdModeID, "Scout", "updated", testCase.rawColor)
			if testCase.expectError {
				if updateError == nil {
					subContext.Fatalf("expected Update error for colour %q, got nil", testCase.rawColor)
				}
				if !strings.Contains(updateError.Error(), "expected #RGB or #RRGGBB") {
					subContext.Fatalf("unexpected Update error message: %v", updateError)
				}
				return
			}
			if updateError != nil {
				subContext.Fatalf("unexpected Update error for colour %q: %v", testCase.rawColor, updateError)
			}
		})
	}
}

func TestDeleteModeDistinguishesNotFoundFromSQLError(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	missingError := modes.DeleteTx(dbConn, 999999)
	if missingError == nil || !strings.Contains(missingError.Error(), "mode 999999 not found") {
		testingContext.Fatalf("expected not found error for missing mode, got %v", missingError)
	}

	rolledBackTx, beginError := dbConn.Begin()
	if beginError != nil {
		testingContext.Fatal(beginError)
	}
	if rollbackError := rolledBackTx.Rollback(); rollbackError != nil {
		testingContext.Fatal(rollbackError)
	}

	txDeleteError := modes.Delete(rolledBackTx, 1)
	if txDeleteError == nil {
		testingContext.Fatal("expected Delete on rolled-back transaction to fail")
	}
	if strings.Contains(txDeleteError.Error(), "not found") {
		testingContext.Fatalf("expected underlying SQL error rather than not found, got %v", txDeleteError)
	}
	if !errors.Is(txDeleteError, sql.ErrTxDone) {
		testingContext.Fatalf("expected wrapped sql.ErrTxDone, got %v", txDeleteError)
	}
}

func TestModesCRUDUsageAndFallback(testingContext *testing.T) {
	dbConn := dbtest.OpenMemory(testingContext)

	seededModes, listError := modes.List(dbConn)
	if listError != nil || len(seededModes) != 3 {
		testingContext.Fatalf("expected 3 seeded default modes, got %+v err=%v", seededModes, listError)
	}
	fallbackID := modes.DefaultFallbackModeID(dbConn)
	if fallbackID <= 0 {
		testingContext.Fatalf("expected positive DefaultFallbackModeID, got %d", fallbackID)
	}
	if existsZero, err := modes.Exists(dbConn, 0); err != nil || existsZero {
		testingContext.Fatalf("Exists(0) = (%v, %v), want (false, nil)", existsZero, err)
	}
	if existsFallback, err := modes.Exists(dbConn, fallbackID); err != nil || !existsFallback {
		testingContext.Fatalf("Exists(fallback) = (%v, %v), want (true, nil)", existsFallback, err)
	}
	if existsMissing, err := modes.Exists(dbConn, 999999); err != nil || existsMissing {
		testingContext.Fatalf("Exists(999999) = (%v, %v), want (false, nil)", existsMissing, err)
	}

	if _, emptyErr := modes.Create(dbConn, "   ", "desc", "#112233"); emptyErr == nil {
		testingContext.Fatal("expected error creating mode with blank name")
	}
	customMode, createError := modes.Create(dbConn, "Exobiology Survey", "Surface sampling", "#22AA88")
	if createError != nil {
		testingContext.Fatalf("Create custom mode: %v", createError)
	}
	if _, dupCreateErr := modes.Create(dbConn, "Exobiology Survey", "dup", "#22AA88"); dupCreateErr == nil || !strings.Contains(dupCreateErr.Error(), "already exists") {
		testingContext.Fatalf("expected duplicate Create error, got %v", dupCreateErr)
	}
	if dupUpdateErr := modes.Update(dbConn, customMode.ID, seededModes[0].Name, "dup", "#22AA88"); dupUpdateErr == nil || !strings.Contains(dupUpdateErr.Error(), "already exists") {
		testingContext.Fatalf("expected duplicate Update error, got %v", dupUpdateErr)
	}
	if missingUpdateErr := modes.Update(dbConn, 999999, "Ghost", "missing", "#22AA88"); missingUpdateErr == nil || !strings.Contains(missingUpdateErr.Error(), "not found") {
		testingContext.Fatalf("expected missing Update error, got %v", missingUpdateErr)
	}

	// Seed expeditions and rows directly via SQLite without importing store/expedition.
	if _, seedExpErr := dbConn.Exec(`
		INSERT INTO expeditions (id, name, status, active_mode_id, created_at, updated_at) VALUES
			(10, 'Active Survey', 'active', ?, '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z'),
			(11, 'Completed Inactive Only', 'completed', ?, '2026-01-01T12:00:00Z', '2026-01-01T12:00:00Z'),
			(12, 'Completed With Leg', 'completed', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		customMode.ID, customMode.ID, fallbackID,
	); seedExpErr != nil {
		testingContext.Fatalf("seed expeditions for mode usage: %v", seedExpErr)
	}
	if _, seedRowsErr := dbConn.Exec(`
		INSERT INTO expedition_rows (expedition_id, position, row_type, system_name, visited_at, leg_mode_id, created_at, updated_at) VALUES
			(10, 0, 'waypoint', 'Sol', '2026-01-02T01:00:00Z', ?, '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z'),
			(10, 1, 'waypoint', 'Achenar', '', ?, '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z'),
			(12, 0, 'waypoint', 'Colonia', '2026-01-01T05:00:00Z', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		customMode.ID, customMode.ID, customMode.ID,
	); seedRowsErr != nil {
		testingContext.Fatalf("seed expedition_rows for mode usage: %v", seedRowsErr)
	}

	usageList, usageError := modes.UsageByExpedition(dbConn, customMode.ID)
	if usageError != nil || len(usageList) != 2 {
		testingContext.Fatalf("expected 2 referencing expeditions (active + completed with leg), got %+v err=%v", usageList, usageError)
	}
	if usageList[0].ExpeditionID != 10 || usageList[0].LegCount != 1 || !usageList[0].IsActiveMode {
		testingContext.Fatalf("unexpected active expedition usage: %+v", usageList[0])
	}
	if usageList[1].ExpeditionID != 12 || usageList[1].LegCount != 1 || usageList[1].IsActiveMode {
		testingContext.Fatalf("unexpected completed expedition usage: %+v", usageList[1])
	}

	// Default modes reject deletion, while deleting customMode reassigns rows and expeditions to fallbackID.
	if defaultDelErr := modes.DeleteTx(dbConn, fallbackID); defaultDelErr == nil || !strings.Contains(defaultDelErr.Error(), "default modes cannot be deleted") {
		testingContext.Fatalf("expected default mode deletion rejection, got %v", defaultDelErr)
	}
	if deleteErr := modes.DeleteTx(dbConn, customMode.ID); deleteErr != nil {
		testingContext.Fatalf("DeleteTx customMode: %v", deleteErr)
	}
	var reassignedCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM expedition_rows WHERE leg_mode_id = ?`, fallbackID).Scan(&reassignedCount); err != nil || reassignedCount != 3 {
		testingContext.Fatalf("expected all 3 rows reassigned to fallback %d, got %d err=%v", fallbackID, reassignedCount, err)
	}

	// Verify DefaultFallbackModeID secondary fallbacks when Travel is renamed or modes is empty.
	if renameErr := modes.Update(dbConn, fallbackID, "Transit", "renamed", "#FF7100"); renameErr != nil {
		testingContext.Fatalf("rename Travel mode: %v", renameErr)
	}
	if secondaryFallback := modes.DefaultFallbackModeID(dbConn); secondaryFallback <= 0 {
		testingContext.Fatalf("expected secondary default fallback mode > 0, got %d", secondaryFallback)
	}
	if _, clearErr := dbConn.Exec(`DELETE FROM modes`); clearErr != nil {
		testingContext.Fatalf("clear modes table: %v", clearErr)
	}
	if emptyFallback := modes.DefaultFallbackModeID(dbConn); emptyFallback != modes.UnassignedID {
		testingContext.Fatalf("expected UnassignedID (0) when modes table is empty, got %d", emptyFallback)
	}
}
