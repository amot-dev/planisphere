package expedition

import (
	"database/sql"
	"sync"

	"planisphere/internal/core/events"
)

// DefaultMaxUndoEntries is the default per-expedition undo and redo stack capacity.
const DefaultMaxUndoEntries = 50

// UndoEntry pairs a user-visible action label with a pre-mutation expedition snapshot.
type UndoEntry struct {
	Label    string
	Snapshot Snapshot
}

// UndoStepOutcome reports the result of applying one undo or redo step.
type UndoStepOutcome struct {
	Applied              bool
	Label                string
	ChangedEvents        events.AppEventSet
	CarrierRouteRestored bool
	NeedsCoordResolution bool
	CanUndo              bool
	CanRedo              bool
}

type expeditionUndoStacks struct {
	undo []UndoEntry
	redo []UndoEntry
}

// UndoHistory stores bounded, session-scoped per-expedition undo and redo stacks.
//
// Lock ordering across the application is strictly acyclic:
//
//	UndoHistory.opMu -> App.dbMu -> UndoHistory.mu
//
// Callers serialize forward undoable mutations and undo/redo steps via RunExclusive,
// while mu is a leaf lock protecting only byExp and is never held across SQLite I/O.
type UndoHistory struct {
	opMu       sync.Mutex
	mu         sync.Mutex
	maxEntries int
	byExp      map[int64]*expeditionUndoStacks
}

// NewUndoHistory creates an empty UndoHistory bounded to maxEntries per expedition.
func NewUndoHistory(maxEntries int) *UndoHistory {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxUndoEntries
	}
	return &UndoHistory{maxEntries: maxEntries, byExp: make(map[int64]*expeditionUndoStacks)}
}

// RunExclusive executes operation while holding the operation serialization mutex.
func (history *UndoHistory) RunExclusive(operation func() error) error {
	history.opMu.Lock()
	defer history.opMu.Unlock()
	return operation()
}

// Push appends entry onto expeditionID's undo stack, drops the oldest entry when
// capacity is exceeded, and clears expeditionID's redo stack.
func (history *UndoHistory) Push(expeditionID int64, entry UndoEntry) {
	if expeditionID <= 0 {
		return
	}
	storedEntry := UndoEntry{Label: entry.Label, Snapshot: cloneSnapshot(entry.Snapshot)}
	history.mu.Lock()
	defer history.mu.Unlock()
	stacks := history.byExp[expeditionID]
	if stacks == nil {
		stacks = &expeditionUndoStacks{}
		history.byExp[expeditionID] = stacks
	}
	stacks.undo = appendBoundedEntry(stacks.undo, storedEntry, history.maxEntries)
	stacks.redo = nil
}

// CanUndoRedo reports whether expeditionID currently has undo or redo entries.
func (history *UndoHistory) CanUndoRedo(expeditionID int64) (bool, bool) {
	history.mu.Lock()
	defer history.mu.Unlock()
	stacks := history.byExp[expeditionID]
	if stacks == nil {
		return false, false
	}
	return len(stacks.undo) > 0, len(stacks.redo) > 0
}

// Undo serializes under opMu and applies the top undo entry for expeditionID.
func (history *UndoHistory) Undo(dbConn *sql.DB, expeditionID int64) (UndoStepOutcome, error) {
	history.opMu.Lock()
	defer history.opMu.Unlock()
	return history.applyStepLocked(dbConn, expeditionID, false)
}

// Redo serializes under opMu and applies the top redo entry for expeditionID.
func (history *UndoHistory) Redo(dbConn *sql.DB, expeditionID int64) (UndoStepOutcome, error) {
	history.opMu.Lock()
	defer history.opMu.Unlock()
	return history.applyStepLocked(dbConn, expeditionID, true)
}

// UndoLocked applies the top undo entry for expeditionID while the caller holds opMu.
func (history *UndoHistory) UndoLocked(dbConn *sql.DB, expeditionID int64) (UndoStepOutcome, error) {
	return history.applyStepLocked(dbConn, expeditionID, false)
}

// RedoLocked applies the top redo entry for expeditionID while the caller holds opMu.
func (history *UndoHistory) RedoLocked(dbConn *sql.DB, expeditionID int64) (UndoStepOutcome, error) {
	return history.applyStepLocked(dbConn, expeditionID, true)
}

// ClearExpedition removes all undo and redo entries for expeditionID.
func (history *UndoHistory) ClearExpedition(expeditionID int64) {
	history.mu.Lock()
	defer history.mu.Unlock()
	delete(history.byExp, expeditionID)
}

// ClearAll removes all undo and redo entries across every expedition.
func (history *UndoHistory) ClearAll() {
	history.mu.Lock()
	defer history.mu.Unlock()
	history.byExp = make(map[int64]*expeditionUndoStacks)
}

func (history *UndoHistory) applyStepLocked(dbConn *sql.DB, expeditionID int64, isRedo bool) (UndoStepOutcome, error) {
	// Peek the target entry under leaf mu, then release mu before running SQLite I/O.
	history.mu.Lock()
	startStacks := history.byExp[expeditionID]
	if startStacks == nil {
		history.mu.Unlock()
		return UndoStepOutcome{}, nil
	}
	sourceSlice := &startStacks.undo
	if isRedo {
		sourceSlice = &startStacks.redo
	}
	if len(*sourceSlice) == 0 {
		outcome := UndoStepOutcome{CanUndo: len(startStacks.undo) > 0, CanRedo: len(startStacks.redo) > 0}
		history.mu.Unlock()
		return outcome, nil
	}
	startLen := len(*sourceSlice)
	targetEntry := (*sourceSlice)[startLen-1]
	history.mu.Unlock()

	restoreOutcome, restoreError := RestoreSnapshotTx(dbConn, targetEntry.Snapshot)
	if restoreError != nil {
		return UndoStepOutcome{}, restoreError
	}

	// Re-verify stack identity under leaf mu so a concurrent ClearAll or
	// ClearExpedition during database hot-swap cannot corrupt the new stack state.
	history.mu.Lock()
	currentStacks := history.byExp[expeditionID]
	if currentStacks == startStacks {
		currentSource, currentDestination := &currentStacks.undo, &currentStacks.redo
		if isRedo {
			currentSource, currentDestination = currentDestination, currentSource
		}
		if len(*currentSource) == startLen && startLen > 0 {
			(*currentSource)[startLen-1] = UndoEntry{}
			*currentSource = (*currentSource)[:startLen-1]
			reverseEntry := UndoEntry{Label: targetEntry.Label, Snapshot: restoreOutcome.PreviousSnapshot}
			*currentDestination = appendBoundedEntry(*currentDestination, reverseEntry, history.maxEntries)
		}
	}
	canUndo := currentStacks != nil && len(currentStacks.undo) > 0
	canRedo := currentStacks != nil && len(currentStacks.redo) > 0
	history.mu.Unlock()

	return UndoStepOutcome{
		Applied:              true,
		Label:                targetEntry.Label,
		ChangedEvents:        restoreOutcome.ChangedEvents,
		CarrierRouteRestored: restoreOutcome.CarrierRouteRestored,
		NeedsCoordResolution: restoreOutcome.NeedsCoordResolution,
		CanUndo:              canUndo,
		CanRedo:              canRedo,
	}, nil
}

func appendBoundedEntry(entries []UndoEntry, nextEntry UndoEntry, maxEntries int) []UndoEntry {
	entries = append(entries, nextEntry)
	if len(entries) <= maxEntries {
		return entries
	}
	overflow := len(entries) - maxEntries
	copy(entries, entries[overflow:])
	for clearIndex := len(entries) - overflow; clearIndex < len(entries); clearIndex++ {
		entries[clearIndex] = UndoEntry{}
	}
	return entries[:maxEntries]
}
