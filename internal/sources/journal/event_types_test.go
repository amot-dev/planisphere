package journal

import (
	"slices"
	"testing"

	"planisphere/internal/core/db"
)

func TestJournalEventTypeAndSetOperations(testingContext *testing.T) {
	seenNames := make(map[string]bool, eventTypeCount)
	for index := EventType(0); index < eventTypeCount; index++ {
		eventName := index.String()
		if eventName == "" {
			testingContext.Fatalf("EventType(%d).String() returned empty string", index)
		}
		if seenNames[eventName] {
			testingContext.Fatalf("duplicate journal EventType name %q", eventName)
		}
		seenNames[eventName] = true
		if !RelevantEvents[eventName] {
			testingContext.Fatalf("RelevantEvents missing %q", eventName)
		}
		if !AllEvents.Has(index) {
			testingContext.Fatalf("AllEvents missing %q", eventName)
		}
		parsedType, found := ParseEventType(eventName)
		if !found || parsedType != index {
			testingContext.Fatalf("ParseEventType(%q) = (%v, %v), want (%v, true)", eventName, parsedType, found, index)
		}
	}

	if len(RelevantEvents) != int(eventTypeCount) {
		testingContext.Fatalf("len(RelevantEvents) = %d, want %d", len(RelevantEvents), eventTypeCount)
	}
	if _, found := ParseEventType("Music"); found {
		testingContext.Fatalf("expected non-relevant event Music not to parse")
	}
	if invalidName := EventType(250).String(); invalidName != "" {
		testingContext.Fatalf("expected empty string for out-of-range EventType, got %q", invalidName)
	}

	arrivalSet := NewEventSet(EventLocation, EventFSDJump, EventCarrierJump)
	if arrivalSet.IsEmpty() || !NewEventSet().IsEmpty() {
		testingContext.Fatalf("unexpected IsEmpty results")
	}
	if !arrivalSet.Intersects(NewEventSet(EventFSDJump, EventScan)) {
		testingContext.Fatalf("expected intersection on EventFSDJump")
	}
	if arrivalSet.Intersects(NewEventSet(EventScan, EventDocked)) {
		testingContext.Fatalf("expected disjoint sets not to intersect")
	}

	expectedNames := []string{"Location", "FSDJump", "CarrierJump"}
	if !slices.Equal(arrivalSet.Names(), expectedNames) {
		testingContext.Fatalf("arrivalSet.Names() = %v, want %v", arrivalSet.Names(), expectedNames)
	}
	expectedSQL := "'Location','FSDJump','CarrierJump'"
	if arrivalSet.SQLInClause() != expectedSQL {
		testingContext.Fatalf("arrivalSet.SQLInClause() = %q, want %q", arrivalSet.SQLInClause(), expectedSQL)
	}
}

func TestLoadEventsAndQueryLatestEvent(testingContext *testing.T) {
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatalf("open test database: %v", openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})

	seedLines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","event":"Commander","Commander":"CmdrEnabled","FID":"F1"}`,
		`{"timestamp":"2026-01-01T00:01:00Z","event":"Location","StarSystem":"Sol","SystemAddress":1001,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-01-01T00:02:00Z","event":"FSDJump","StarSystem":"Alpha Centauri","SystemAddress":1002,"StarPos":[1,2,3],"JumpDist":4.3}`,
		`{"timestamp":"2026-01-01T00:03:00Z","event":"Commander","Commander":"CmdrDisabled","FID":"F2"}`,
		`{"timestamp":"2026-01-01T00:04:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":1003,"StarPos":[10,20,30],"JumpDist":50.0}`,
	}
	statement, prepError := databaseConnection.Prepare(InsertJournalEventPrefixSQL)
	if prepError != nil {
		testingContext.Fatalf("prepare insert: %v", prepError)
	}
	defer statement.Close()

	activeCommander := ""
	for _, rawLine := range seedLines {
		parsedEvent, ok, parseError := ParseLine([]byte(rawLine), "Journal.test.log")
		if parseError != nil || !ok {
			testingContext.Fatalf("parse line %s: ok=%v err=%v", rawLine, ok, parseError)
		}
		if parsedEvent.Commander != "" {
			activeCommander = parsedEvent.Commander
		} else {
			parsedEvent.Commander = activeCommander
		}
		if _, execError := ExecJournalEvent(statement, &parsedEvent, "Journal.test.log", "2026-01-01T00:05:00Z"); execError != nil {
			testingContext.Fatalf("insert event: %v", execError)
		}
	}
	if _, execError := databaseConnection.Exec(
		`INSERT INTO commanders(name, fid, enabled) VALUES ('CmdrEnabled', 'F1', 1), ('CmdrDisabled', 'F2', 0)`,
	); execError != nil {
		testingContext.Fatalf("insert commanders: %v", execError)
	}

	arrivalSet := NewEventSet(EventLocation, EventFSDJump)
	loadedRows, loadError := LoadEvents(databaseConnection, "2026-01-01T00:01:00Z", "", arrivalSet)
	if loadError != nil {
		testingContext.Fatalf("LoadEvents: %v", loadError)
	}
	if len(loadedRows) != 1 || loadedRows[0].Type != EventFSDJump || loadedRows[0].SystemName != "Alpha Centauri" || loadedRows[0].StarPos == nil {
		testingContext.Fatalf("unexpected loadedRows: %+v", loadedRows)
	}

	latestRow, latestError := QueryLatestEvent(databaseConnection, arrivalSet)
	if latestError != nil || latestRow == nil {
		testingContext.Fatalf("QueryLatestEvent: row=%v err=%v", latestRow, latestError)
	}
	if latestRow.Type != EventFSDJump || latestRow.SystemName != "Alpha Centauri" {
		testingContext.Fatalf("expected latest enabled arrival Alpha Centauri, got %+v", latestRow)
	}

	emptyRows, emptyError := LoadEvents(databaseConnection, "", "", NewEventSet())
	if emptyError != nil || len(emptyRows) != 0 {
		testingContext.Fatalf("expected empty result for empty EventSet, got rows=%v err=%v", emptyRows, emptyError)
	}
	missingLatest, missingError := QueryLatestEvent(databaseConnection, NewEventSet(EventCarrierStats))
	if missingError != nil || missingLatest != nil {
		testingContext.Fatalf("expected nil for absent CarrierStats event, got row=%v err=%v", missingLatest, missingError)
	}
}
