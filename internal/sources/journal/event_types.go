package journal

import (
	"strings"

	"planisphere/internal/core/events"
)

// EventType identifies a persisted Elite Dangerous journal event type.
type EventType = events.JournalEvent

// EventSet is a zero-allocation bitmask of EventType values.
type EventSet = events.JournalEventSet

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

// AllEvents includes every persisted Frontier journal event type.
const AllEvents EventSet = (1 << eventTypeCount) - 1

// NewEventSet returns an EventSet containing all provided event types.
func NewEventSet(items ...EventType) EventSet {
	return events.NewJournalSet(items...)
}

// EventName returns Frontier's exact JSON event name for eventType.
func EventName(eventType EventType) string {
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

// EventNames returns Frontier's event names for all types in eventSet in enum order.
func EventNames(eventSet EventSet) []string {
	var resolvedNames []string
	for index := EventType(0); index < eventTypeCount; index++ {
		if eventSet.Has(index) {
			resolvedNames = append(resolvedNames, EventName(index))
		}
	}
	return resolvedNames
}

// FormatSQLInClause formats eventSet as a SQL IN literal list such as 'FSDJump','Location'.
func FormatSQLInClause(eventSet EventSet) string {
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
		clauseBuilder.WriteString(EventName(index))
		clauseBuilder.WriteByte('\'')
	}
	return clauseBuilder.String()
}
