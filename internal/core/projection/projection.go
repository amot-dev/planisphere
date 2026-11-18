// Package projection provides generation-guarded caches and an event bus that
// coordinates invalidation, side-effect triggers, and UI refresh targets.
package projection

import (
	"planisphere/internal/core/events"
)

// Triggers pairs external Elite Dangerous journal events and internal
// Planisphere application events that invalidate a projection or invoke a hook.
type Triggers struct {
	Journal events.JournalEventSet
	App     events.AppEventSet
}

// Matches reports whether triggers and observed share any journal or app event.
func (triggers Triggers) Matches(observed Triggers) bool {
	return triggers.Journal.Intersects(observed.Journal) ||
		triggers.App.Intersects(observed.App)
}

// Union combines triggers and other into a single Triggers value.
func (triggers Triggers) Union(other Triggers) Triggers {
	return Triggers{
		Journal: triggers.Journal.Union(other.Journal),
		App:     triggers.App.Union(other.App),
	}
}

// WithApp returns a copy of triggers with the provided app events added.
func (triggers Triggers) WithApp(items ...events.AppEvent) Triggers {
	return Triggers{
		Journal: triggers.Journal,
		App:     triggers.App.Union(events.NewAppSet(items...)),
	}
}

// WithJournal returns a copy of triggers with the provided journal events added.
func (triggers Triggers) WithJournal(items ...events.JournalEvent) Triggers {
	return Triggers{
		Journal: triggers.Journal.Union(events.NewJournalSet(items...)),
		App:     triggers.App,
	}
}

// TriggerSource is implemented by projections and domain wrappers that expose
// their invalidation triggers.
type TriggerSource interface {
	Triggers() Triggers
}

// TriggersFrom unions the triggers declared by all provided non-nil sources.
func TriggersFrom(sources ...TriggerSource) Triggers {
	var combined Triggers
	for _, source := range sources {
		if source != nil {
			combined = combined.Union(source.Triggers())
		}
	}
	return combined
}

// Projection is the cache invalidation lifecycle contract registered on a Bus.
type Projection interface {
	TriggerSource
	Name() string
	Invalidate()
}

