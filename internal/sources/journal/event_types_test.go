package journal

import (
	"slices"
	"testing"
)

func TestJournalEventTypeAndSetOperations(testingContext *testing.T) {
	seenNames := make(map[string]bool, eventTypeCount)
	for index := EventType(0); index < eventTypeCount; index++ {
		eventName := EventName(index)
		if eventName == "" {
			testingContext.Fatalf("EventName(%d) returned empty string", index)
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
	if invalidName := EventName(EventType(250)); invalidName != "" {
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
	if !slices.Equal(EventNames(arrivalSet), expectedNames) {
		testingContext.Fatalf("EventNames(arrivalSet) = %v, want %v", EventNames(arrivalSet), expectedNames)
	}
	expectedSQL := "'Location','FSDJump','CarrierJump'"
	if FormatSQLInClause(arrivalSet) != expectedSQL {
		testingContext.Fatalf("FormatSQLInClause(arrivalSet) = %q, want %q", FormatSQLInClause(arrivalSet), expectedSQL)
	}
}
