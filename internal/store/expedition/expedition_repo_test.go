package expedition_test

import (
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
