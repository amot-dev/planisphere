package projection

import (
	"slices"
	"sync/atomic"
	"testing"

	"planisphere/internal/core/events"
)

const (
	testEventScan         events.JournalEvent = 0
	testEventFSDJump      events.JournalEvent = 1
	testEventCarrierStats events.JournalEvent = 2
	testEventDocked       events.JournalEvent = 3
	testEventFSDTarget    events.JournalEvent = 4
	testAllJournalEvents  events.JournalEventSet = (1 << 5) - 1
)

func TestTriggersCompositionAndMatching(testingContext *testing.T) {
	scanTriggers := Triggers{
		Journal: events.NewJournalSet(testEventScan),
		App:     events.NewAppSet(events.AppCommanderFilterChanged),
	}
	arrivalTriggers := Triggers{
		Journal: events.NewJournalSet(testEventFSDJump),
		App:     events.NewAppSet(events.AppModesChanged),
	}

	if !scanTriggers.Matches(Triggers{Journal: events.NewJournalSet(testEventScan, testEventDocked)}) {
		testingContext.Fatalf("expected journal trigger match on testEventScan")
	}
	if scanTriggers.Matches(Triggers{Journal: events.NewJournalSet(testEventDocked)}) {
		testingContext.Fatalf("expected disjoint journal event not to match")
	}

	bus := NewBus()
	firstCache := NewCache(bus, "scans", scanTriggers, func() (int, error) { return 1, nil })
	secondCache := NewCache(bus, "arrivals", arrivalTriggers, func() (int, error) { return 2, nil })
	combined := TriggersFrom(firstCache, nil, secondCache).
		WithApp(events.AppSettingsChanged).
		WithJournal(testEventCarrierStats)

	if !combined.Journal.Has(testEventScan) || !combined.Journal.Has(testEventFSDJump) || !combined.Journal.Has(testEventCarrierStats) {
		testingContext.Fatalf("unexpected combined journal triggers: %v", combined.Journal)
	}
	if !combined.App.Has(events.AppCommanderFilterChanged) || !combined.App.Has(events.AppModesChanged) || !combined.App.Has(events.AppSettingsChanged) {
		testingContext.Fatalf("unexpected combined app triggers: %v", combined.App.Names())
	}
}

func TestCacheABAGenerationSafetyOnInvalidate(testingContext *testing.T) {
	loadStarted := make(chan struct{})
	releaseLoad := make(chan struct{})
	var loadCount atomic.Int32

	cache := NewCache(nil, "aba_value", Triggers{}, func() (string, error) {
		attempt := loadCount.Add(1)
		if attempt == 1 {
			close(loadStarted)
			<-releaseLoad
			return "stale-value", nil
		}
		return "fresh-value", nil
	})

	type loadOutcome struct {
		value string
		err   error
	}
	firstResultCh := make(chan loadOutcome, 1)
	go func() {
		loadedValue, loadError := cache.Get()
		firstResultCh <- loadOutcome{value: loadedValue, err: loadError}
	}()

	<-loadStarted
	cache.Invalidate()
	close(releaseLoad)

	firstOutcome := <-firstResultCh
	if firstOutcome.err != nil || firstOutcome.value != "stale-value" {
		testingContext.Fatalf("unexpected in-flight result: %+v", firstOutcome)
	}

	freshValue, freshError := cache.Get()
	if freshError != nil || freshValue != "fresh-value" {
		testingContext.Fatalf("expected fresh-value after concurrent Invalidate, got %q (err=%v)", freshValue, freshError)
	}
	if loadCount.Load() != 2 {
		testingContext.Fatalf("expected stale write-back to be rejected and loader run twice, got %d", loadCount.Load())
	}
}

func TestKeyedCacheGetOrLoadGetManyOrLoadAndABASafety(testingContext *testing.T) {
	keyed := NewKeyedCache[string, int](nil, "keyed", Triggers{})

	firstVal, firstErr := keyed.GetOrLoad("alpha", func() (int, error) { return 10, nil })
	if firstErr != nil || firstVal != 10 {
		testingContext.Fatalf("GetOrLoad alpha = (%d, %v)", firstVal, firstErr)
	}

	var batchCalls atomic.Int32
	var lastMissing []string
	batchResult, batchErr := keyed.GetManyOrLoad([]string{"alpha", "beta", "beta", "gamma"}, func(missing []string) (map[string]int, error) {
		batchCalls.Add(1)
		lastMissing = slices.Clone(missing)
		loaded := make(map[string]int, len(missing))
		for _, key := range missing {
			loaded[key] = len(key)
		}
		return loaded, nil
	})
	if batchErr != nil {
		testingContext.Fatalf("GetManyOrLoad failed: %v", batchErr)
	}
	if batchCalls.Load() != 1 || !slices.Equal(lastMissing, []string{"beta", "gamma"}) {
		testingContext.Fatalf("expected 1 batch call for [beta gamma], got calls=%d missing=%v", batchCalls.Load(), lastMissing)
	}
	if batchResult["alpha"] != 10 || batchResult["beta"] != 4 || batchResult["gamma"] != 5 {
		testingContext.Fatalf("unexpected batchResult: %+v", batchResult)
	}

	// Verify concurrent Invalidate prevents stale write-back for GetManyOrLoad.
	loadStarted := make(chan struct{})
	releaseLoad := make(chan struct{})
	var attempt atomic.Int32
	goErrCh := make(chan error, 1)
	go func() {
		_, loadErr := keyed.GetManyOrLoad([]string{"delta"}, func(missing []string) (map[string]int, error) {
			if attempt.Add(1) == 1 {
				close(loadStarted)
				<-releaseLoad
			}
			return map[string]int{"delta": 99}, nil
		})
		goErrCh <- loadErr
	}()

	<-loadStarted
	keyed.Invalidate()
	close(releaseLoad)
	if asyncErr := <-goErrCh; asyncErr != nil {
		testingContext.Fatalf("async GetManyOrLoad error: %v", asyncErr)
	}

	refreshed, refreshErr := keyed.GetOrLoad("delta", func() (int, error) { return 123, nil })
	if refreshErr != nil || refreshed != 123 {
		testingContext.Fatalf("expected fresh value 123 after Invalidate, got %d (err=%v)", refreshed, refreshErr)
	}
}

func TestBusInvalidatesProjectionsBeforeHandlersAndResolvesUITargets(testingContext *testing.T) {
	bus := NewBus()

	var arrivalLoads atomic.Int32
	arrivalCache := NewCache(
		bus,
		"arrivals",
		Triggers{
			Journal: events.NewJournalSet(testEventFSDJump),
			App:     events.NewAppSet(events.AppCommanderFilterChanged),
		},
		func() (int32, error) {
			return arrivalLoads.Add(1), nil
		},
	)

	var carrierLoads atomic.Int32
	carrierCache := NewCache(
		bus,
		"carrier",
		Triggers{Journal: events.NewJournalSet(testEventCarrierStats)},
		func() (int32, error) {
			return carrierLoads.Add(1), nil
		},
	)

	// Warm both caches.
	_, _ = arrivalCache.Get()
	_, _ = carrierCache.Get()

	var eventsObserved []string
	var arrivalValueDuringHandler int32
	bus.OnTrigger(
		Triggers{Journal: events.NewJournalSet(testEventFSDJump)},
		func(observed Triggers) {
			eventsObserved = append(eventsObserved, "arrival_handler")
			loadedValue, _ := arrivalCache.Get()
			arrivalValueDuringHandler = loadedValue
		},
	)

	bus.SubscribeUI(events.UITargetPlan, TriggersFrom(arrivalCache))
	bus.SubscribeUI(events.UITargetStats, TriggersFrom(arrivalCache, carrierCache).WithApp(events.AppAdjustmentsChanged))
	bus.SubscribeUI(events.UITargetIngest, Triggers{Journal: testAllJournalEvents})

	// FSDTarget has no projection/UI target besides UITargetIngest.
	uiOnTarget := bus.PublishJournal(events.NewJournalSet(testEventFSDTarget))
	if !slices.Equal(uiOnTarget.Names(), []string{"ingest"}) {
		testingContext.Fatalf("expected only ingest UI target for FSDTarget, got %v", uiOnTarget.Names())
	}
	if _, _ = arrivalCache.Get(); arrivalLoads.Load() != 1 {
		testingContext.Fatalf("expected arrivalCache to remain warm after FSDTarget")
	}

	// FSDJump must invalidate arrivalCache BEFORE running arrival_handler, leave carrierCache warm, and return ingest/stats/plan.
	uiOnJump := bus.PublishJournal(events.NewJournalSet(testEventFSDJump))
	if len(eventsObserved) != 1 || arrivalValueDuringHandler != 2 {
		testingContext.Fatalf("expected arrivalCache invalidated before handler (value=%d, observed=%v)", arrivalValueDuringHandler, eventsObserved)
	}
	if _, _ = carrierCache.Get(); carrierLoads.Load() != 1 {
		testingContext.Fatalf("expected carrierCache to remain warm on FSDJump, got %d loads", carrierLoads.Load())
	}
	if !slices.Equal(uiOnJump.Names(), []string{"ingest", "stats", "plan"}) {
		testingContext.Fatalf("expected [ingest stats plan] UI targets for FSDJump, got %v", uiOnJump.Names())
	}

	// AppAdjustmentsChanged should only trigger UITargetStats.
	uiOnAdjustment := bus.PublishApp(events.AppAdjustmentsChanged)
	if !slices.Equal(uiOnAdjustment.Names(), []string{"stats"}) {
		testingContext.Fatalf("expected [stats] UI target for AppAdjustmentsChanged, got %v", uiOnAdjustment.Names())
	}
}

