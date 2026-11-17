package journal

import "strings"

// EventType identifies a persisted Elite Dangerous journal event type.
type EventType uint8

// Persisted Elite Dangerous journal event types.
const (
	EventCommander EventType = iota
	EventLoadGame
	EventStatistics
	EventLocation
	EventFSDJump
	EventCarrierJump
	EventCarrierJumpRequest
	EventCarrierStats
	EventCarrierDepositFuel
	EventDocked
	EventUndocked
	EventFSDTarget
	EventStartJump
	EventJetConeBoost
	EventSynthesis
	EventScan
	EventSAAScanComplete
	EventFSSDiscoveryScan
	EventScanOrganic
	EventMultiSellExplorationData
	EventSellExplorationData
	EventSellOrganicData
	EventShipyardSwap
	EventDied
	EventResurrect
	EventNpcCrewPaidWage
	eventTypeCount
)

var eventTypeNames = [eventTypeCount]string{
	EventCommander:                "Commander",
	EventLoadGame:                 "LoadGame",
	EventStatistics:               "Statistics",
	EventLocation:                 "Location",
	EventFSDJump:                  "FSDJump",
	EventCarrierJump:              "CarrierJump",
	EventCarrierJumpRequest:       "CarrierJumpRequest",
	EventCarrierStats:             "CarrierStats",
	EventCarrierDepositFuel:       "CarrierDepositFuel",
	EventDocked:                   "Docked",
	EventUndocked:                 "Undocked",
	EventFSDTarget:                "FSDTarget",
	EventStartJump:                "StartJump",
	EventJetConeBoost:             "JetConeBoost",
	EventSynthesis:                "Synthesis",
	EventScan:                     "Scan",
	EventSAAScanComplete:          "SAAScanComplete",
	EventFSSDiscoveryScan:         "FSSDiscoveryScan",
	EventScanOrganic:              "ScanOrganic",
	EventMultiSellExplorationData: "MultiSellExplorationData",
	EventSellExplorationData:      "SellExplorationData",
	EventSellOrganicData:          "SellOrganicData",
	EventShipyardSwap:             "ShipyardSwap",
	EventDied:                     "Died",
	EventResurrect:                "Resurrect",
	EventNpcCrewPaidWage:          "NpcCrewPaidWage",
}

var eventTypeByName map[string]EventType

func init() {
	eventTypeByName = make(map[string]EventType, eventTypeCount)
	RelevantEvents = make(map[string]bool, eventTypeCount)
	for index := EventType(0); index < eventTypeCount; index++ {
		eventName := eventTypeNames[index]
		eventTypeByName[eventName] = index
		RelevantEvents[eventName] = true
	}
}

// String returns Frontier's exact JSON event name for eventType.
func (eventType EventType) String() string {
	if int(eventType) >= len(eventTypeNames) {
		return ""
	}
	return eventTypeNames[eventType]
}

// ParseEventType resolves a raw Frontier journal event name to its typed EventType.
func ParseEventType(rawName string) (EventType, bool) {
	parsedType, found := eventTypeByName[rawName]
	return parsedType, found
}

// EventSet is a zero-allocation bitmask of EventType values.
type EventSet uint64

// AllEvents includes every persisted Frontier journal event type.
const AllEvents EventSet = (1 << eventTypeCount) - 1

// NewEventSet returns an EventSet containing all provided event types.
func NewEventSet(items ...EventType) EventSet {
	var eventMask EventSet
	for _, item := range items {
		eventMask = eventMask.Add(item)
	}
	return eventMask
}

// Add returns a copy of eventSet with item included.
func (eventSet EventSet) Add(item EventType) EventSet {
	if item >= eventTypeCount {
		return eventSet
	}
	return eventSet | (1 << item)
}

// Union returns the combined bitmask of eventSet and otherSet.
func (eventSet EventSet) Union(otherSet EventSet) EventSet {
	return eventSet | otherSet
}

// Has reports whether item is present in eventSet.
func (eventSet EventSet) Has(item EventType) bool {
	if item >= eventTypeCount {
		return false
	}
	return eventSet&(1<<item) != 0
}

// Intersects reports whether eventSet and otherSet share at least one event type.
func (eventSet EventSet) Intersects(otherSet EventSet) bool {
	return eventSet&otherSet != 0
}

// IsEmpty reports whether eventSet contains no event types.
func (eventSet EventSet) IsEmpty() bool {
	return eventSet == 0
}

// Names returns Frontier's event names for all types in eventSet in enum order.
func (eventSet EventSet) Names() []string {
	var resolvedNames []string
	for index := EventType(0); index < eventTypeCount; index++ {
		if eventSet.Has(index) {
			resolvedNames = append(resolvedNames, index.String())
		}
	}
	return resolvedNames
}

// SQLInClause formats eventSet as a SQL IN literal list such as 'FSDJump','Location'.
func (eventSet EventSet) SQLInClause() string {
	var clauseBuilder strings.Builder
	wroteFirst := false
	for index := EventType(0); index < eventTypeCount; index++ {
		if !eventSet.Has(index) {
			continue
		}
		if wroteFirst {
			clauseBuilder.WriteByte(',')
		}
		wroteFirst = true
		clauseBuilder.WriteByte('\'')
		clauseBuilder.WriteString(index.String())
		clauseBuilder.WriteByte('\'')
	}
	return clauseBuilder.String()
}

