package expedition_test

import (
	"slices"
	"strings"
	"testing"

	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
	"planisphere/internal/testutil/dbtest"
)

func TestRecordCarrierJumpAndApplyTo(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	createdExpedition, createError := expedition.Create(databaseConnection, "Carrier Stats Test", "")
	if createError != nil {
		testingContext.Fatal(createError)
	}

	if err := expedition.RecordCarrierJump(databaseConnection, createdExpedition.ID, 498.5, 95); err != nil {
		testingContext.Fatalf("first RecordCarrierJump: %v", err)
	}
	if err := expedition.RecordCarrierJump(databaseConnection, createdExpedition.ID, 120.0, 25); err != nil {
		testingContext.Fatalf("second RecordCarrierJump: %v", err)
	}

	reloaded, getError := expedition.Get(databaseConnection, createdExpedition.ID)
	if getError != nil {
		testingContext.Fatal(getError)
	}
	if reloaded.Adjustments.CarrierJumps != 2 || reloaded.Adjustments.CarrierDistanceLy != 618.5 ||
		reloaded.Adjustments.TritiumUsed == nil || *reloaded.Adjustments.TritiumUsed != 120 {
		testingContext.Fatalf("unexpected Adjustments: %+v", reloaded.Adjustments)
	}

	var combinedMetrics replaystats.Metrics
	reloaded.Adjustments.ApplyTo(&combinedMetrics)
	if combinedMetrics.CarrierJumps != 2 || combinedMetrics.CarrierDistanceLy != 618.5 ||
		combinedMetrics.TritiumUsed == nil || *combinedMetrics.TritiumUsed != 120 {
		testingContext.Fatalf("unexpected ApplyTo result: %+v", combinedMetrics)
	}
}

func TestCreateWithWaypoints(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)

	// Empty waypoint slice falls back to a single blank waypoint row.
	emptyExpedition, emptyError := expedition.CreateWithWaypoints(databaseConnection, "Blank Seed", "", []expedition.SeedWaypoint{
		{SystemName: "   "},
	})
	if emptyError != nil {
		testingContext.Fatalf("CreateWithWaypoints empty: %v", emptyError)
	}
	emptyRows, listEmptyError := expedition.ListRows(databaseConnection, emptyExpedition.ID)
	if listEmptyError != nil {
		testingContext.Fatalf("ListRows empty: %v", listEmptyError)
	}
	if len(emptyRows) != 1 || emptyRows[0].RowType != expedition.RowWaypoint || emptyRows[0].SystemName != "" {
		testingContext.Fatalf("expected 1 blank waypoint row, got %+v", emptyRows)
	}

	// Non-empty waypoints preserve order, normalize whitespace, skip blanks, and persist pre-resolved metadata.
	seededExpedition, seedError := expedition.CreateWithWaypoints(databaseConnection, "  Xibalba Run  ", "  Imported route  ", []expedition.SeedWaypoint{
		{
			SystemName:    "  Shinrarta   Dezhra ",
			POIName:       "  Founders World  ",
			SystemAddress: 3932277478106,
			EDSMID:        4345,
			HasCoords:     true,
			X:             55.71875,
			Y:             17.59375,
			Z:             27.15625,
			CoordsSource:  "journal",
		},
		{SystemName: "\t  "},
		{SystemName: "3 Geminorum"},
		{SystemName: "Shinrarta Dezhra"},
	})
	if seedError != nil {
		testingContext.Fatalf("CreateWithWaypoints seeded: %v", seedError)
	}
	if seededExpedition.Status != expedition.StatusPlanning || seededExpedition.Name != "Xibalba Run" || seededExpedition.Description != "Imported route" {
		testingContext.Fatalf("unexpected seeded expedition metadata: %+v", seededExpedition)
	}

	seededRows, listSeededError := expedition.ListRows(databaseConnection, seededExpedition.ID)
	if listSeededError != nil {
		testingContext.Fatalf("ListRows seeded: %v", listSeededError)
	}
	if len(seededRows) != 3 {
		testingContext.Fatalf("expected 3 seeded rows, got %d: %+v", len(seededRows), seededRows)
	}
	if seededRows[0].Position != 1 || seededRows[0].SystemName != "Shinrarta Dezhra" || seededRows[0].POIName != "Founders World" ||
		seededRows[0].SystemAddress != 3932277478106 || seededRows[0].EDSMID != 4345 || !seededRows[0].HasCoords ||
		seededRows[0].X != 55.71875 || seededRows[0].CoordsSource != "journal" {
		testingContext.Fatalf("unexpected row 0: %+v", seededRows[0])
	}
	if seededRows[1].Position != 2 || seededRows[1].SystemName != "3 Geminorum" || seededRows[1].HasCoords {
		testingContext.Fatalf("unexpected row 1: %+v", seededRows[1])
	}
	if seededRows[2].Position != 3 || seededRows[2].SystemName != "Shinrarta Dezhra" {
		testingContext.Fatalf("unexpected row 2: %+v", seededRows[2])
	}
}

func TestScanExpeditionAdjustmentsDecodeError(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	createdExpedition, createError := expedition.Create(databaseConnection, "Adjustments Check", "")
	if createError != nil {
		testingContext.Fatalf("Create: %v", createError)
	}

	if _, blankError := databaseConnection.Exec(`UPDATE expeditions SET adjustments_json = '   ' WHERE id = ?`, createdExpedition.ID); blankError != nil {
		testingContext.Fatalf("set blank adjustments_json: %v", blankError)
	}
	if _, getBlankError := expedition.Get(databaseConnection, createdExpedition.ID); getBlankError != nil {
		testingContext.Fatalf("expected blank adjustments_json to succeed, got %v", getBlankError)
	}

	if _, corruptError := databaseConnection.Exec(`UPDATE expeditions SET adjustments_json = '{corrupt' WHERE id = ?`, createdExpedition.ID); corruptError != nil {
		testingContext.Fatalf("set corrupt adjustments_json: %v", corruptError)
	}
	if _, getCorruptError := expedition.Get(databaseConnection, createdExpedition.ID); getCorruptError == nil || !strings.Contains(getCorruptError.Error(), "decode expedition") {
		testingContext.Fatalf("expected decode error for corrupt adjustments_json, got %v", getCorruptError)
	}
}

func TestListOrdering(testingContext *testing.T) {
	databaseConnection := dbtest.OpenMemory(testingContext)
	if _, clearError := databaseConnection.Exec(`DELETE FROM expeditions`); clearError != nil {
		testingContext.Fatalf("clear seeded expeditions: %v", clearError)
	}

	type seedExpedition struct {
		name           string
		status         string
		startTimestamp string
		createdAt      string
	}
	seedEntries := []seedExpedition{
		{
			name:           "Retroactive Completed",
			status:         expedition.StatusCompleted,
			startTimestamp: "2026-01-15T00:00:00Z",
			createdAt:      "2026-03-20T00:00:00Z",
		},
		{
			name:      "Planning Older",
			status:    expedition.StatusPlanning,
			createdAt: "2026-03-01T00:00:00Z",
		},
		{
			name:           "Active Middle Earlier Created",
			status:         expedition.StatusActive,
			startTimestamp: "2026-02-10T00:00:00Z",
			createdAt:      "2026-02-01T00:00:00Z",
		},
		{
			name:           "Completed Latest Start",
			status:         expedition.StatusCompleted,
			startTimestamp: "2026-02-20T00:00:00Z",
			createdAt:      "2026-02-22T00:00:00Z",
		},
		{
			name:      "Planning Newer",
			status:    expedition.StatusPlanning,
			createdAt: "2026-03-10T00:00:00Z",
		},
		{
			name:           "Legacy Active Missing Start",
			status:         expedition.StatusActive,
			startTimestamp: "",
			createdAt:      "2026-03-05T00:00:00Z",
		},
		{
			name:           "Active Middle Newer Created",
			status:         expedition.StatusActive,
			startTimestamp: "2026-02-10T00:00:00Z",
			createdAt:      "2026-02-15T00:00:00Z",
		},
	}

	for _, seedEntry := range seedEntries {
		createdExpedition, createError := expedition.Create(databaseConnection, seedEntry.name, "")
		if createError != nil {
			testingContext.Fatalf("Create(%q): %v", seedEntry.name, createError)
		}
		var startTimestampValue any
		if seedEntry.startTimestamp != "" {
			startTimestampValue = seedEntry.startTimestamp
		}
		if _, updateError := databaseConnection.Exec(
			`UPDATE expeditions SET status = ?, start_timestamp = ?, created_at = ?, updated_at = ? WHERE id = ?`,
			seedEntry.status, startTimestampValue, seedEntry.createdAt, seedEntry.createdAt, createdExpedition.ID,
		); updateError != nil {
			testingContext.Fatalf("update fixture %q: %v", seedEntry.name, updateError)
		}
	}

	listedExpeditions, listError := expedition.List(databaseConnection)
	if listError != nil {
		testingContext.Fatalf("List: %v", listError)
	}
	actualNames := make([]string, 0, len(listedExpeditions))
	for _, expeditionItem := range listedExpeditions {
		actualNames = append(actualNames, expeditionItem.Name)
	}
	expectedNames := []string{
		"Planning Newer",
		"Legacy Active Missing Start",
		"Planning Older",
		"Completed Latest Start",
		"Active Middle Newer Created",
		"Active Middle Earlier Created",
		"Retroactive Completed",
	}
	if !slices.Equal(actualNames, expectedNames) {
		testingContext.Fatalf("unexpected List order:\ngot:  %v\nwant: %v", actualNames, expectedNames)
	}
	if listedExpeditions[1].Status != expedition.StatusPlanning {
		testingContext.Fatalf("expected legacy active expedition without start timestamp to normalize to planning, got %q", listedExpeditions[1].Status)
	}
}
