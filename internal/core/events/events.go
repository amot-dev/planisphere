// Package events defines typed internal application mutation events and
// frontend UI refresh targets represented as zero-allocation bitmasks.
package events

// AppEvent identifies an internal Planisphere state or configuration mutation.
type AppEvent uint8

// Internal application mutation events.
const (
	AppCommanderFilterChanged AppEvent = iota
	AppModesChanged
	AppExpeditionModesChanged
	AppExpeditionPlanChanged
	AppExpeditionBoundsChanged
	AppAdjustmentsChanged
	AppSettingsChanged
	AppDatabaseSwitched
	appEventCount
)

var appEventNames = [appEventCount]string{
	AppCommanderFilterChanged:  "CommanderFilterChanged",
	AppModesChanged:            "ModesChanged",
	AppExpeditionModesChanged:  "ExpeditionModesChanged",
	AppExpeditionPlanChanged:   "ExpeditionPlanChanged",
	AppExpeditionBoundsChanged: "ExpeditionBoundsChanged",
	AppAdjustmentsChanged:      "AdjustmentsChanged",
	AppSettingsChanged:         "SettingsChanged",
	AppDatabaseSwitched:        "DatabaseSwitched",
}

// String returns the canonical name of the application event.
func (appEvent AppEvent) String() string {
	if int(appEvent) >= len(appEventNames) {
		return ""
	}
	return appEventNames[appEvent]
}

// AppEventSet is a bitmask of AppEvent values.
type AppEventSet uint32

// AllAppEvents includes every defined AppEvent.
const AllAppEvents AppEventSet = (1 << appEventCount) - 1

// NewAppSet returns an AppEventSet containing all provided events.
func NewAppSet(items ...AppEvent) AppEventSet {
	var eventMask AppEventSet
	for _, appEvent := range items {
		eventMask = eventMask.Add(appEvent)
	}
	return eventMask
}

// Add returns a copy of eventSet with item included.
func (eventSet AppEventSet) Add(item AppEvent) AppEventSet {
	if item >= appEventCount {
		return eventSet
	}
	return eventSet | (1 << item)
}

// Union returns the combined bitmask of eventSet and otherSet.
func (eventSet AppEventSet) Union(otherSet AppEventSet) AppEventSet {
	return eventSet | otherSet
}

// Has reports whether item is present in eventSet.
func (eventSet AppEventSet) Has(item AppEvent) bool {
	if item >= appEventCount {
		return false
	}
	return eventSet&(1<<item) != 0
}

// Intersects reports whether eventSet and otherSet share at least one event.
func (eventSet AppEventSet) Intersects(otherSet AppEventSet) bool {
	return eventSet&otherSet != 0
}

// IsEmpty reports whether eventSet contains no events.
func (eventSet AppEventSet) IsEmpty() bool {
	return eventSet == 0
}

// Names returns the canonical names of all events in eventSet in enum order.
func (eventSet AppEventSet) Names() []string {
	var resolvedNames []string
	for index := AppEvent(0); index < appEventCount; index++ {
		if eventSet.Has(index) {
			resolvedNames = append(resolvedNames, index.String())
		}
	}
	return resolvedNames
}

// UITarget identifies a frontend data loader or UI section that responds to backend changes.
type UITarget uint8

// Frontend UI refresh targets.
const (
	UITargetIngest UITarget = iota
	UITargetStats
	UITargetPlan
	UITargetModes
	UITargetSettings
	UITargetExpeditions
	uiTargetCount
)

var uiTargetNames = [uiTargetCount]string{
	UITargetIngest:      "ingest",
	UITargetStats:       "stats",
	UITargetPlan:        "plan",
	UITargetModes:       "modes",
	UITargetSettings:    "settings",
	UITargetExpeditions: "expeditions",
}

// String returns the serialized identifier of the UI refresh target.
func (target UITarget) String() string {
	if int(target) >= len(uiTargetNames) {
		return ""
	}
	return uiTargetNames[target]
}

// UITargetSet is a bitmask of UITarget values.
type UITargetSet uint16

// NewUITargetSet returns a UITargetSet containing all provided targets.
func NewUITargetSet(items ...UITarget) UITargetSet {
	var targetMask UITargetSet
	for _, target := range items {
		targetMask = targetMask.Add(target)
	}
	return targetMask
}

// Add returns a copy of targetSet with item included.
func (targetSet UITargetSet) Add(item UITarget) UITargetSet {
	if item >= uiTargetCount {
		return targetSet
	}
	return targetSet | (1 << item)
}

// Union returns the combined bitmask of targetSet and otherSet.
func (targetSet UITargetSet) Union(otherSet UITargetSet) UITargetSet {
	return targetSet | otherSet
}

// Has reports whether item is present in targetSet.
func (targetSet UITargetSet) Has(item UITarget) bool {
	if item >= uiTargetCount {
		return false
	}
	return targetSet&(1<<item) != 0
}

// Intersects reports whether targetSet and otherSet share at least one target.
func (targetSet UITargetSet) Intersects(otherSet UITargetSet) bool {
	return targetSet&otherSet != 0
}

// IsEmpty reports whether targetSet contains no targets.
func (targetSet UITargetSet) IsEmpty() bool {
	return targetSet == 0
}

// Names returns the serialized identifiers of all targets in targetSet in enum order.
func (targetSet UITargetSet) Names() []string {
	var resolvedNames []string
	for index := UITarget(0); index < uiTargetCount; index++ {
		if targetSet.Has(index) {
			resolvedNames = append(resolvedNames, index.String())
		}
	}
	return resolvedNames
}
