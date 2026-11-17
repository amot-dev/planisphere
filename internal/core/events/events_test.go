package events

import (
	"slices"
	"testing"
)

func TestAppEventEnumAndSetOperations(testingContext *testing.T) {
	seenNames := make(map[string]bool, appEventCount)
	for index := AppEvent(0); index < appEventCount; index++ {
		eventName := index.String()
		if eventName == "" {
			testingContext.Fatalf("AppEvent(%d).String() returned empty string", index)
		}
		if seenNames[eventName] {
			testingContext.Fatalf("duplicate AppEvent name %q", eventName)
		}
		seenNames[eventName] = true
		if !AllAppEvents.Has(index) {
			testingContext.Fatalf("AllAppEvents missing AppEvent %q", eventName)
		}
	}

	if invalidName := AppEvent(250).String(); invalidName != "" {
		testingContext.Fatalf("expected empty string for out-of-range AppEvent, got %q", invalidName)
	}

	firstSet := NewAppSet(AppModesChanged, AppSettingsChanged)
	secondSet := NewAppSet(AppSettingsChanged, AppDatabaseSwitched)

	if firstSet.IsEmpty() || !NewAppSet().IsEmpty() {
		testingContext.Fatalf("unexpected IsEmpty results")
	}
	if !firstSet.Intersects(secondSet) {
		testingContext.Fatalf("expected intersection on AppSettingsChanged")
	}
	if firstSet.Intersects(NewAppSet(AppAdjustmentsChanged)) {
		testingContext.Fatalf("expected disjoint sets not to intersect")
	}

	combinedSet := firstSet.Union(secondSet)
	expectedNames := []string{"ModesChanged", "SettingsChanged", "DatabaseSwitched"}
	if !slices.Equal(combinedSet.Names(), expectedNames) {
		testingContext.Fatalf("combinedSet.Names() = %v, want %v", combinedSet.Names(), expectedNames)
	}
}

func TestUITargetEnumParseAndSetOperations(testingContext *testing.T) {
	seenNames := make(map[string]bool, uiTargetCount)
	for index := UITarget(0); index < uiTargetCount; index++ {
		targetName := index.String()
		if targetName == "" {
			testingContext.Fatalf("UITarget(%d).String() returned empty string", index)
		}
		if seenNames[targetName] {
			testingContext.Fatalf("duplicate UITarget name %q", targetName)
		}
		seenNames[targetName] = true

		parsedTarget, found := ParseUITarget(" " + targetName + " ")
		if !found || parsedTarget != index {
			testingContext.Fatalf("ParseUITarget(%q) = (%v, %v), want (%v, true)", targetName, parsedTarget, found, index)
		}
	}

	if _, found := ParseUITarget("unknown"); found {
		testingContext.Fatalf("expected unknown UI target not to parse")
	}
	if invalidName := UITarget(250).String(); invalidName != "" {
		testingContext.Fatalf("expected empty string for out-of-range UITarget, got %q", invalidName)
	}

	targetSet := NewUITargetSet(UITargetIngest, UITargetPlan).Add(UITargetStats)
	if !targetSet.Has(UITargetPlan) || !targetSet.Intersects(NewUITargetSet(UITargetStats)) {
		testingContext.Fatalf("unexpected targetSet membership: %v", targetSet.Names())
	}
	expectedNames := []string{"ingest", "stats", "plan"}
	if !slices.Equal(targetSet.Names(), expectedNames) {
		testingContext.Fatalf("targetSet.Names() = %v, want %v", targetSet.Names(), expectedNames)
	}
}

