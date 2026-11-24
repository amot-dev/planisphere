package replay

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// RecoveredJump represents a hyperspace or Fleet Carrier passenger jump completed
// server-side during a client disconnect or offline session, reconstructed from
// the subsequent Location sequence.
type RecoveredJump struct {
	LocationEventUID string
	Timestamp        string
	SystemName       string
	SystemAddress    int64
	StarPos          reference.Vec3
	JumpDist         float64
	BoostUsed        int
	IsCarrier        bool
}

// ComputeRecoveredJumps reconstructs all crash-recovered hyperspace jumps and
// offline or disconnected Fleet Carrier passenger jumps in chronological order.
func ComputeRecoveredJumps(queryer db.Queryer) ([]RecoveredJump, error) {
	hasDisabled, checkError := HasDisabledCommanders(queryer)
	if checkError != nil {
		return nil, fmt.Errorf("check disabled commanders for disconnect recovery: %w", checkError)
	}
	candidates, queryError := queryPrimedCandidates(queryer, hasDisabled)
	if queryError != nil || len(candidates) == 0 {
		return nil, queryError
	}

	const candidateChunkSize = 100
	var recovered []RecoveredJump
	recoveredCarriersByLocID := make(map[int64]carrierStayState)
	for chunkStart := 0; chunkStart < len(candidates); chunkStart += candidateChunkSize {
		chunk := candidates[chunkStart:min(chunkStart+candidateChunkSize, len(candidates))]
		loadedEvents, loadError := loadPreDisconnectChunk(queryer, chunk, hasDisabled)
		if loadError != nil {
			return nil, loadError
		}
		cursor := 0
		for _, candidate := range chunk {
			for cursor < len(loadedEvents) && eventOrderedBefore(loadedEvents[cursor].timestamp, loadedEvents[cursor].id, candidate.previousArrival.timestamp, candidate.previousArrival.id) {
				cursor++
			}
			sliceEnd := cursor
			for sliceEnd < len(loadedEvents) && !eventOrderedBefore(candidate.locationTimestamp, candidate.locationID, loadedEvents[sliceEnd].timestamp, loadedEvents[sliceEnd].id) {
				sliceEnd++
			}
			windowEvents := loadedEvents[cursor:sliceEnd]
			if candidate.isCarrier {
				jump, matched, resolveError := resolveCarrierCandidateFromSlice(queryer, candidate, windowEvents, hasDisabled, recoveredCarriersByLocID)
				if resolveError != nil {
					return nil, resolveError
				}
				if matched {
					recovered = append(recovered, jump)
				}
			} else if jump, matched := resolveCandidateFromSlice(candidate, windowEvents); matched {
				recovered = append(recovered, jump)
			}
		}
	}
	slices.SortFunc(recovered, compareRecoveredJumps)
	return recovered, nil
}

func compareRecoveredJumps(left, right RecoveredJump) int {
	if timestampCmp := strings.Compare(left.Timestamp, right.Timestamp); timestampCmp != 0 {
		return timestampCmp
	}
	return strings.Compare(left.LocationEventUID, right.LocationEventUID)
}

// ComputeRecoveredJumpsInRange computes crash-recovered hyperspace jumps directly
// against queryer for (afterTimestamp, untilTimestamp].
func ComputeRecoveredJumpsInRange(queryer db.Queryer, afterTimestamp, untilTimestamp string) ([]RecoveredJump, error) {
	allRecovered, computeError := ComputeRecoveredJumps(queryer)
	if computeError != nil || len(allRecovered) == 0 {
		return nil, computeError
	}
	return FilterRecoveredJumpsInRange(allRecovered, afterTimestamp, untilTimestamp), nil
}

// FilterRecoveredJumpsInRange filters a chronologically sorted slice of recovered jumps
// to (afterTimestamp, untilTimestamp].
func FilterRecoveredJumpsInRange(allRecovered []RecoveredJump, afterTimestamp, untilTimestamp string) []RecoveredJump {
	startIndex := 0
	if afterTimestamp != "" {
		startIndex = sort.Search(len(allRecovered), func(i int) bool {
			return allRecovered[i].Timestamp > afterTimestamp
		})
	}
	endIndex := len(allRecovered)
	if untilTimestamp != "" {
		endIndex = sort.Search(len(allRecovered), func(i int) bool {
			return allRecovered[i].Timestamp > untilTimestamp
		})
	}
	if startIndex >= endIndex {
		return nil
	}
	return allRecovered[startIndex:endIndex]
}

func applyPreDisconnectEvent(item preDisconnectEvent, boostUsed *int, latestStartJump **journal.StartJumpEvent) {
	switch item.eventType {
	case journal.EventShipyardSwap:
		// Swapping ships discards any jet-cone or FSD synthesis boost armed on the old ship.
		*boostUsed = 0
	case journal.EventJetConeBoost:
		var jetBoost journal.JetConeBoostEvent
		if json.Unmarshal([]byte(item.rawJSON), &jetBoost) == nil && jetBoost.BoostValue > 1.0 {
			*boostUsed = 4
		}
	case journal.EventSynthesis:
		var synthesis journal.SynthesisEvent
		if json.Unmarshal([]byte(item.rawJSON), &synthesis) == nil {
			if tier := reference.FSDSynthesisBoostTier(synthesis.Name); tier > 0 {
				*boostUsed = tier
			}
		}
	case journal.EventStartJump:
		var startJump journal.StartJumpEvent
		if json.Unmarshal([]byte(item.rawJSON), &startJump) == nil && strings.EqualFold(strings.TrimSpace(startJump.JumpType), "Hyperspace") {
			*latestStartJump = &startJump
		}
	}
}

// walkPreDisconnectWindow iterates verification events in (previousArrival, location]
// for candidate's commander, returning false if a Resurrect aborts recovery and
// invoking onPreLoad for every event at or before LoadGame.
func walkPreDisconnectWindow(
	candidate primedDisconnectCandidate,
	windowEvents []preDisconnectEvent,
	onPreLoad func(preDisconnectEvent),
) bool {
	for _, item := range windowEvents {
		if candidate.commander != "" && item.commander != "" && !strings.EqualFold(candidate.commander, item.commander) {
			continue
		}
		if !eventOrderedBefore(candidate.previousArrival.timestamp, candidate.previousArrival.id, item.timestamp, item.id) ||
			eventOrderedBefore(candidate.locationTimestamp, candidate.locationID, item.timestamp, item.id) {
			continue
		}
		// Any Resurrect in (previousArrival, location] marks a respawn/rebuy teleport
		// rather than a completed jump, so abort recovery across the entire window.
		if item.eventType == journal.EventResurrect {
			return false
		}
		// Cut off pre-disconnect state attribution at LoadGame; events after
		// LoadGame belong to the post-reconnect session rather than the prior jump.
		if !eventOrderedBefore(candidate.loadGameTimestamp, candidate.loadGameID, item.timestamp, item.id) {
			onPreLoad(item)
		}
	}
	return true
}

func buildRecoveredJump(candidate primedDisconnectCandidate, boostUsed int) (RecoveredJump, bool) {
	jumpDistance := reference.Distance(candidate.previousArrival.starPosition, candidate.locationPosition)
	if jumpDistance <= 0 {
		return RecoveredJump{}, false
	}
	return RecoveredJump{
		LocationEventUID: candidate.locationUID,
		Timestamp:        candidate.locationTimestamp,
		SystemName:       candidate.locationSystem,
		SystemAddress:    candidate.locationAddress,
		StarPos:          candidate.locationPosition,
		JumpDist:         jumpDistance,
		BoostUsed:        boostUsed,
		IsCarrier:        candidate.isCarrier,
	}, true
}

func resolveCandidateFromSlice(candidate primedDisconnectCandidate, windowEvents []preDisconnectEvent) (RecoveredJump, bool) {
	var (
		latestStartJump *journal.StartJumpEvent
		boostUsed       int
	)
	if !walkPreDisconnectWindow(candidate, windowEvents, func(item preDisconnectEvent) {
		applyPreDisconnectEvent(item, &boostUsed, &latestStartJump)
	}) {
		return RecoveredJump{}, false
	}
	if latestStartJump == nil || latestStartJump.Taxi || latestStartJump.Multicrew {
		return RecoveredJump{}, false
	}
	if !disconnectSystemsMatch(latestStartJump.SystemAddress, latestStartJump.StarSystem, candidate.locationAddress, candidate.locationSystem) {
		return RecoveredJump{}, false
	}
	return buildRecoveredJump(candidate, boostUsed)
}

type carrierStayState struct {
	aboard      bool
	marketID    int64
	stationName string
}

func sameCarrierIdentity(priorMarketID int64, priorStationName string, locationMarketID int64, locationStationName string) bool {
	if priorMarketID > 0 && locationMarketID > 0 {
		return priorMarketID == locationMarketID
	}
	trimmedPrior := strings.TrimSpace(priorStationName)
	trimmedLocation := strings.TrimSpace(locationStationName)
	if trimmedPrior != "" && trimmedLocation != "" {
		return strings.EqualFold(trimmedPrior, trimmedLocation)
	}
	return true
}

func initialCarrierStayState(
	queryer db.Queryer,
	candidate primedDisconnectCandidate,
	hasDisabled bool,
	recoveredCarriersByLocID map[int64]carrierStayState,
) (carrierStayState, error) {
	switch candidate.previousArrival.eventType {
	case journal.EventCarrierJump:
		stay := carrierStayState{aboard: true}
		var carrierJumpEvent journal.CarrierJumpEvent
		if json.Unmarshal([]byte(candidate.previousArrival.rawJSON), &carrierJumpEvent) == nil {
			stay.marketID = carrierJumpEvent.MarketID
			stay.stationName = carrierJumpEvent.StationName
		}
		return stay, nil
	case journal.EventLocation:
		var prevLocation journal.LocationEvent
		if json.Unmarshal([]byte(candidate.previousArrival.rawJSON), &prevLocation) != nil || prevLocation.Multicrew {
			return carrierStayState{}, nil
		}
		if prevLocation.Docked && strings.EqualFold(prevLocation.StationType, reference.FleetCarrierStationType) {
			return carrierStayState{aboard: true, marketID: prevLocation.MarketID, stationName: prevLocation.StationName}, nil
		}
		if isCarrierConcourseLocation(prevLocation) {
			if chainedStay, exists := recoveredCarriersByLocID[candidate.previousArrival.id]; exists {
				return chainedStay, nil
			}
			return queryPriorCarrierBoardingState(queryer, candidate.previousArrival, candidate.commander, hasDisabled)
		}
	}
	return carrierStayState{}, nil
}

func applyPreDisconnectCarrierEvent(item preDisconnectEvent, previousArrival arrivalSnapshot, stay *carrierStayState) {
	switch item.eventType {
	case journal.EventUndocked:
		var undockFlags struct {
			Multicrew bool `json:"Multicrew"`
		}
		if json.Unmarshal([]byte(item.rawJSON), &undockFlags) == nil && !undockFlags.Multicrew {
			*stay = carrierStayState{}
		}
	case journal.EventDocked:
		var dockedEvent journal.DockedEvent
		if json.Unmarshal([]byte(item.rawJSON), &dockedEvent) != nil || dockedEvent.Multicrew {
			return
		}
		if !strings.EqualFold(dockedEvent.StationType, reference.FleetCarrierStationType) {
			*stay = carrierStayState{}
			return
		}
		// Reject docking whose system disagrees with previousArrival (missing intermediate jump).
		dockedSystemName := dockedEvent.StarSystem
		if dockedSystemName == "" {
			dockedSystemName = item.systemName
		}
		dockedSystemAddr := dockedEvent.SystemAddress
		if dockedSystemAddr == 0 {
			dockedSystemAddr = item.systemAddress
		}
		if (dockedSystemAddr != 0 || strings.TrimSpace(dockedSystemName) != "") &&
			!disconnectSystemsMatch(dockedSystemAddr, dockedSystemName, previousArrival.systemAddress, previousArrival.systemName) {
			*stay = carrierStayState{}
			return
		}
		*stay = carrierStayState{aboard: true, marketID: dockedEvent.MarketID, stationName: dockedEvent.StationName}
	}
}

func resolveCarrierCandidateFromSlice(
	queryer db.Queryer,
	candidate primedDisconnectCandidate,
	windowEvents []preDisconnectEvent,
	hasDisabled bool,
	recoveredCarriersByLocID map[int64]carrierStayState,
) (RecoveredJump, bool, error) {
	stay, stateError := initialCarrierStayState(queryer, candidate, hasDisabled, recoveredCarriersByLocID)
	if stateError != nil {
		return RecoveredJump{}, false, stateError
	}
	if !walkPreDisconnectWindow(candidate, windowEvents, func(item preDisconnectEvent) {
		applyPreDisconnectCarrierEvent(item, candidate.previousArrival, &stay)
	}) {
		return RecoveredJump{}, false, nil
	}
	if !stay.aboard {
		return RecoveredJump{}, false, nil
	}
	if candidate.carrierDocked && !sameCarrierIdentity(stay.marketID, stay.stationName, candidate.carrierMarketID, candidate.carrierStationName) {
		return RecoveredJump{}, false, nil
	}
	jump, validDistance := buildRecoveredJump(candidate, 0)
	if !validDistance {
		return RecoveredJump{}, false, nil
	}
	nextStay := stay
	if candidate.carrierDocked {
		nextStay = carrierStayState{aboard: true, marketID: candidate.carrierMarketID, stationName: candidate.carrierStationName}
	}
	recoveredCarriersByLocID[candidate.locationID] = nextStay
	return jump, true, nil
}
