package modes_test

import (
	"database/sql"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/store/modes"
)

func openModesTestDB(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	dbConn, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatal(openError)
	}
	testingContext.Cleanup(func() {
		_ = dbConn.Close()
	})
	return dbConn
}

func TestModeColorValidation(testingContext *testing.T) {
	dbConn := openModesTestDB(testingContext)

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
