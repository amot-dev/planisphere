package events

// JournalEvent identifies a persisted journal event type in bitmask triggers.
type JournalEvent uint8

// JournalEventSet is a zero-allocation bitmask of JournalEvent values.
type JournalEventSet uint64

// NewJournalSet returns a JournalEventSet containing all provided events.
func NewJournalSet(items ...JournalEvent) JournalEventSet {
	var eventMask JournalEventSet
	for _, item := range items {
		eventMask = eventMask.Add(item)
	}
	return eventMask
}

// Add returns a copy of eventSet with item included.
func (eventSet JournalEventSet) Add(item JournalEvent) JournalEventSet {
	if item >= 64 {
		return eventSet
	}
	return eventSet | (1 << item)
}

// Union returns the combined bitmask of eventSet and otherSet.
func (eventSet JournalEventSet) Union(otherSet JournalEventSet) JournalEventSet {
	return eventSet | otherSet
}

// Has reports whether item is present in eventSet.
func (eventSet JournalEventSet) Has(item JournalEvent) bool {
	if item >= 64 {
		return false
	}
	return eventSet&(1<<item) != 0
}

// Intersects reports whether eventSet and otherSet share at least one event.
func (eventSet JournalEventSet) Intersects(otherSet JournalEventSet) bool {
	return eventSet&otherSet != 0
}

// IsEmpty reports whether eventSet contains no events.
func (eventSet JournalEventSet) IsEmpty() bool {
	return eventSet == 0
}

