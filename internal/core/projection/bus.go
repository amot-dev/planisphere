package projection

import (
	"sync"

	"planisphere/internal/core/events"
)

type triggerSubscription struct {
	triggers Triggers
	handler  func(observed Triggers)
}

type uiSubscription struct {
	target   events.UITarget
	triggers Triggers
}

// Bus routes observed journal and application events to registered projections,
// trigger handlers, and UI refresh subscriptions.
type Bus struct {
	mu              sync.RWMutex
	projections     []Projection
	triggerHandlers []triggerSubscription
	uiSubscriptions []uiSubscription
}

// NewBus creates an empty projection event bus.
func NewBus() *Bus {
	return &Bus{}
}

// Register adds proj to the bus so matching published events invalidate its cache.
func (bus *Bus) Register(proj Projection) {
	if proj == nil {
		return
	}
	bus.mu.Lock()
	bus.projections = append(bus.projections, proj)
	bus.mu.Unlock()
}

// OnTrigger registers a callback invoked after matching projections are invalidated.
func (bus *Bus) OnTrigger(triggers Triggers, handler func(observed Triggers)) {
	if handler == nil {
		return
	}
	bus.mu.Lock()
	bus.triggerHandlers = append(bus.triggerHandlers, triggerSubscription{
		triggers: triggers,
		handler:  handler,
	})
	bus.mu.Unlock()
}

// SubscribeUI associates target with triggers so matching published events report
// target in the returned UITargetSet.
func (bus *Bus) SubscribeUI(target events.UITarget, triggers Triggers) {
	bus.mu.Lock()
	bus.uiSubscriptions = append(bus.uiSubscriptions, uiSubscription{
		target:   target,
		triggers: triggers,
	})
	bus.mu.Unlock()
}

// Publish invalidates matching projections first, executes matching trigger
// handlers in registration order, and returns the union of matching UI targets.
func (bus *Bus) Publish(observed Triggers) events.UITargetSet {
	bus.mu.RLock()
	var matchingProjections []Projection
	for _, proj := range bus.projections {
		if proj.Triggers().Matches(observed) {
			matchingProjections = append(matchingProjections, proj)
		}
	}
	var matchingHandlers []func(observed Triggers)
	for _, subscription := range bus.triggerHandlers {
		if subscription.triggers.Matches(observed) {
			matchingHandlers = append(matchingHandlers, subscription.handler)
		}
	}
	var affectedTargets events.UITargetSet
	for _, subscription := range bus.uiSubscriptions {
		if subscription.triggers.Matches(observed) {
			affectedTargets = affectedTargets.Add(subscription.target)
		}
	}
	bus.mu.RUnlock()

	for _, proj := range matchingProjections {
		if selective, ok := proj.(SelectiveProjection); ok {
			selective.InvalidateObserved(observed)
		} else {
			proj.Invalidate()
		}
	}
	for _, handler := range matchingHandlers {
		handler(observed)
	}
	return affectedTargets
}

// PublishJournal dispatches a set of newly ingested journal events across the bus.
func (bus *Bus) PublishJournal(journalEvents events.JournalEventSet) events.UITargetSet {
	return bus.PublishJournalSince(journalEvents, "")
}

// PublishJournalSince dispatches newly ingested journal events across the bus
// along with the earliest timestamp among inserted events so windowed caches
// can preserve closed historical ranges ending strictly before earliestTimestamp.
func (bus *Bus) PublishJournalSince(journalEvents events.JournalEventSet, earliestTimestamp string) events.UITargetSet {
	if journalEvents.IsEmpty() {
		return 0
	}
	return bus.Publish(Triggers{
		Journal:           journalEvents,
		EarliestTimestamp: earliestTimestamp,
	})
}

// PublishApp dispatches internal application mutation events across the bus.
func (bus *Bus) PublishApp(appEvents ...events.AppEvent) events.UITargetSet {
	appSet := events.NewAppSet(appEvents...)
	if appSet.IsEmpty() {
		return 0
	}
	return bus.Publish(Triggers{App: appSet})
}
