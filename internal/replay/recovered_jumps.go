package replay

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// RecoveredJump represents a hyperspace jump completed server-side during a
// client disconnect, reconstructed from the subsequent LoadGame and Location sequence.
type RecoveredJump struct {
	LocationEventUID string
	Timestamp        string
	SystemName       string
	SystemAddress    int64
	StarPos          reference.Vec3
	JumpDist         float64
	BoostUsed        int
}

// ComputeRecoveredJumps reconstructs all crash-recovered hyperspace jumps in chronological order.
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
	for chunkStart := 0; chunkStart < len(candidates); chunkStart += candidateChunkSize {
		chunkEnd := min(chunkStart+candidateChunkSize, len(candidates))
		chunk := candidates[chunkStart:chunkEnd]
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
			if jump, ok := resolveCandidateFromSlice(candidate, loadedEvents[cursor:sliceEnd]); ok {
				recovered = append(recovered, jump)
			}
		}
	}
	return recovered, nil
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

func resolveCandidateFromSlice(candidate primedDisconnectCandidate, windowEvents []preDisconnectEvent) (RecoveredJump, bool) {
	var (
		latestStartJump *journal.StartJumpEvent
		boostUsed       int
	)
	for _, item := range windowEvents {
		if candidate.commander != "" && item.commander != "" && !strings.EqualFold(candidate.commander, item.commander) {
			continue
		}
		if !eventOrderedBefore(candidate.previousArrival.timestamp, candidate.previousArrival.id, item.timestamp, item.id) {
			continue
		}
		if eventOrderedBefore(candidate.locationTimestamp, candidate.locationID, item.timestamp, item.id) {
			continue
		}
		if item.eventType == journal.EventResurrect {
			return RecoveredJump{}, false
		}
		if !eventOrderedBefore(candidate.loadGameTimestamp, candidate.loadGameID, item.timestamp, item.id) {
			applyPreDisconnectEvent(item, &boostUsed, &latestStartJump)
		}
	}
	if latestStartJump == nil || latestStartJump.Taxi || latestStartJump.Multicrew {
		return RecoveredJump{}, false
	}
	if !disconnectSystemsMatch(latestStartJump.SystemAddress, latestStartJump.StarSystem, candidate.locationAddress, candidate.locationSystem) {
		return RecoveredJump{}, false
	}
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
	}, true
}
