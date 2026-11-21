// Package projection provides generation-guarded caches and an event bus that
// coordinates invalidation, side-effect triggers, and UI refresh targets.
package projection

import (
	"strings"
	"time"

	"planisphere/internal/core/events"
)

// Triggers pairs external Elite Dangerous journal events and internal
// Planisphere application events that invalidate a projection or invoke a hook.
type Triggers struct {
	Journal           events.JournalEventSet
	App               events.AppEventSet
	EarliestTimestamp string
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

// SelectiveProjection is an optional Projection extension that can preserve
// unaffected cache keys based on the observed trigger payload.
type SelectiveProjection interface {
	Projection
	InvalidateObserved(observed Triggers)
}

// WindowEndsBefore reports whether a pipe-delimited "after|until..." cache key
// represents a closed time window whose upper bound strictly precedes earliestTimestamp.
// Returns false for open-ended windows, malformed keys, or empty earliestTimestamp
// so callers defensively evict affected or unknown ranges.
func WindowEndsBefore(cacheKey, earliestTimestamp string) bool {
	if earliestTimestamp == "" {
		return false
	}
	firstSeparator := strings.IndexByte(cacheKey, '|')
	if firstSeparator < 0 {
		return false
	}
	remainder := cacheKey[firstSeparator+1:]
	untilTimestamp := remainder
	if secondSeparator := strings.IndexByte(remainder, '|'); secondSeparator >= 0 {
		untilTimestamp = remainder[:secondSeparator]
	}
	if untilTimestamp == "" {
		return false
	}
	untilTime, untilParseError := time.Parse(time.RFC3339Nano, untilTimestamp)
	earliestTime, earliestParseError := time.Parse(time.RFC3339Nano, earliestTimestamp)
	if untilParseError == nil && earliestParseError == nil {
		return untilTime.Before(earliestTime)
	}
	return untilTimestamp < earliestTimestamp
}
