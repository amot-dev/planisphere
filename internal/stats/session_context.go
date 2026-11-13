package stats

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"planisphere/internal/ed"
	"planisphere/internal/journal"
)

// odysseyReleaseTimestamp is the UTC launch date of Elite Dangerous: Odyssey
// (19 May 2021). When evaluating synthetic or truncated event streams that
// lack an explicit preceding LoadGame event, timestamps prior to Odyssey's
// release never apply the +30% Odyssey mapping bonus.
const odysseyReleaseTimestamp = "2021-05-19T00:00:00Z"

const fleetCarrierStationType = "FleetCarrier"

// sessionState tracks chronological game-session and system metadata across
// journal events so valuation and realization share identical Odyssey,
// population, and Fleet Carrier docking context.
type sessionState struct {
	defaultOdyssey          bool
	hasSeenLoadGame         bool
	currentOdyssey          bool
	isDockedOnFleetCarrier  bool
	pendingConcourseRespawn bool
	carrierSystemAddress    int64
	populationBySystem      map[int64]int64
}

func newSessionState(defaultOdyssey bool, estimatedSystemCount int) *sessionState {
	if estimatedSystemCount < 16 {
		estimatedSystemCount = 16
	}
	return &sessionState{
		defaultOdyssey:     defaultOdyssey,
		populationBySystem: make(map[int64]int64, estimatedSystemCount),
	}
}

func (state *sessionState) odysseyAt(timestamp string) bool {
	if !state.defaultOdyssey {
		return false
	}
	if state.hasSeenLoadGame {
		return state.currentOdyssey
	}
	if timestamp != "" && timestamp < odysseyReleaseTimestamp {
		return false
	}
	return state.defaultOdyssey
}

func (state *sessionState) systemPopulation(systemAddress int64) int64 {
	return state.populationBySystem[systemAddress]
}

func (state *sessionState) applyLoadGame(rawJSON string) {
	var loadGameEvent journal.LoadGameEvent
	if json.Unmarshal([]byte(rawJSON), &loadGameEvent) != nil {
		return
	}
	state.hasSeenLoadGame = true
	state.currentOdyssey = loadGameEvent.Odyssey != nil && *loadGameEvent.Odyssey
	state.pendingConcourseRespawn = false
}

func (state *sessionState) applyLocation(rawJSON string) {
	var locationEvent journal.LocationEvent
	if json.Unmarshal([]byte(rawJSON), &locationEvent) != nil {
		return
	}
	wasPendingConcourseRespawn := state.pendingConcourseRespawn
	state.pendingConcourseRespawn = false
	if locationEvent.SystemAddress != 0 {
		state.populationBySystem[locationEvent.SystemAddress] = locationEvent.Population
	}
	if locationEvent.Docked {
		state.setFleetCarrierDocked(
			strings.EqualFold(locationEvent.StationType, fleetCarrierStationType),
			locationEvent.SystemAddress,
		)
		return
	}
	// On a Fleet Carrier Concourse (both when relogging on foot and when respawning
	// via Resurrect "recover" or "escape"), Frontier logs Docked: false, OnFoot: true
	// with the orbited Star, Planet, or Null BodyType and omits StationType, whereas
	// Starports, Outposts, Megaships, and Surface Settlements log BodyType: "Station".
	if locationEvent.OnFoot && !strings.EqualFold(locationEvent.BodyType, "Station") {
		if wasPendingConcourseRespawn {
			state.setFleetCarrierDocked(true, locationEvent.SystemAddress)
			return
		}
		if state.isDockedOnFleetCarrier &&
			(state.carrierSystemAddress == 0 || locationEvent.SystemAddress == state.carrierSystemAddress) {
			return
		}
	}
	state.setFleetCarrierDocked(false, 0)
}

func (state *sessionState) applyDocked(rawJSON string) {
	var dockedEvent journal.DockedEvent
	if json.Unmarshal([]byte(rawJSON), &dockedEvent) != nil {
		return
	}
	state.setFleetCarrierDocked(
		strings.EqualFold(dockedEvent.StationType, fleetCarrierStationType),
		dockedEvent.SystemAddress,
	)
}

func (state *sessionState) applyUndocked() {
	state.setFleetCarrierDocked(false, 0)
}

func (state *sessionState) applyFSDJumpEvent(jumpEvent *journal.FSDJumpEvent) {
	if jumpEvent.SystemAddress != 0 {
		state.populationBySystem[jumpEvent.SystemAddress] = jumpEvent.Population
	}
	state.setFleetCarrierDocked(false, 0)
}

func (state *sessionState) applyCarrierJump(rawJSON string) {
	var carrierJumpEvent journal.CarrierJumpEvent
	if json.Unmarshal([]byte(rawJSON), &carrierJumpEvent) != nil {
		return
	}
	if carrierJumpEvent.SystemAddress != 0 {
		state.populationBySystem[carrierJumpEvent.SystemAddress] = carrierJumpEvent.Population
	}
	if carrierJumpEvent.Docked {
		state.setFleetCarrierDocked(
			strings.EqualFold(carrierJumpEvent.StationType, fleetCarrierStationType),
			carrierJumpEvent.SystemAddress,
		)
	} else if carrierJumpEvent.OnFoot {
		state.setFleetCarrierDocked(true, carrierJumpEvent.SystemAddress)
	}
}

func (state *sessionState) applyResurrect(resurrectOption string) {
	state.setFleetCarrierDocked(false, 0)
	normalizedOption := strings.ToLower(strings.TrimSpace(resurrectOption))
	state.pendingConcourseRespawn = normalizedOption == "recover" || normalizedOption == "escape"
}

func (state *sessionState) setFleetCarrierDocked(dockedOnCarrier bool, systemAddress int64) {
	state.pendingConcourseRespawn = false
	state.isDockedOnFleetCarrier = dockedOnCarrier
	if dockedOnCarrier {
		state.carrierSystemAddress = systemAddress
	} else {
		state.carrierSystemAddress = 0
	}
}

func toBodyScanWithPopulation(scanEvent *journal.ScanEvent, systemPopulation int64) ed.BodyScan {
	return ed.BodyScan{
		StarType:         scanEvent.StarType,
		PlanetClass:      scanEvent.PlanetClass,
		MassEM:           scanEvent.MassEM,
		StellarMass:      scanEvent.StellarMass,
		TerraformState:   scanEvent.TerraformState,
		SystemPopulation: systemPopulation,
		WasDiscovered:    scanEvent.WasDiscovered,
		WasMapped:        scanEvent.WasMapped,
	}
}

func (engine *Engine) seedLoadGameBefore(afterTimestamp string, state *sessionState) error {
	if afterTimestamp == "" {
		return nil
	}
	var rawLoadGame string
	queryError := engine.DB.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'LoadGame' AND timestamp <= ? AND `+journal.CmdrFilterSQL+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`, afterTimestamp).Scan(&rawLoadGame)
	if queryError == sql.ErrNoRows {
		return nil
	}
	if queryError != nil {
		return fmt.Errorf("seed LoadGame before %s: %w", afterTimestamp, queryError)
	}
	state.applyLoadGame(rawLoadGame)
	return nil
}

// preloadSystemPopulations batch-loads the latest known Population up to
// untilTimestamp for any system scanned, mapped, or sampled in events whose
// arrival event occurred prior to the loaded time window.
func (engine *Engine) preloadSystemPopulations(afterTimestamp string, events []eventRow, untilTimestamp string, state *sessionState) error {
	if afterTimestamp == "" {
		return nil
	}
	missingAddresses := collectUnseenSystemAddresses(events)
	if len(missingAddresses) == 0 {
		return nil
	}
	const chunkSize = 200
	for startIndex := 0; startIndex < len(missingAddresses); startIndex += chunkSize {
		endIndex := min(startIndex+chunkSize, len(missingAddresses))
		if chunkError := engine.loadPopulationChunk(missingAddresses[startIndex:endIndex], untilTimestamp, state); chunkError != nil {
			return chunkError
		}
	}
	return nil
}

func collectUnseenSystemAddresses(events []eventRow) []int64 {
	seenArrivalSystems := make(map[int64]bool, len(events)/4)
	for _, rowItem := range events {
		if (rowItem.eventType == "FSDJump" || rowItem.eventType == "Location" || rowItem.eventType == "CarrierJump") &&
			rowItem.systemAddress.Valid && rowItem.systemAddress.Int64 != 0 {
			seenArrivalSystems[rowItem.systemAddress.Int64] = true
		}
	}
	missingSet := make(map[int64]bool)
	var missingAddresses []int64
	for _, rowItem := range events {
		if (rowItem.eventType == "Scan" || rowItem.eventType == "SAAScanComplete" || rowItem.eventType == "ScanOrganic") &&
			rowItem.systemAddress.Valid && rowItem.systemAddress.Int64 != 0 {
			address := rowItem.systemAddress.Int64
			if !seenArrivalSystems[address] && !missingSet[address] {
				missingSet[address] = true
				missingAddresses = append(missingAddresses, address)
			}
		}
	}
	return missingAddresses
}

func (engine *Engine) loadPopulationChunk(addressChunk []int64, untilTimestamp string, state *sessionState) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(addressChunk)), ",")
	queryArgs := make([]any, 0, len(addressChunk)+1)
	queryArgs = append(queryArgs, untilTimestamp)
	for _, address := range addressChunk {
		queryArgs = append(queryArgs, address)
	}
	query := `SELECT system_address, raw_json FROM (
		SELECT system_address, raw_json,
		       ROW_NUMBER() OVER (PARTITION BY system_address ORDER BY timestamp DESC, id DESC) as rn
		FROM journal_events
		WHERE event IN ('FSDJump', 'Location', 'CarrierJump')
		  AND timestamp <= ? AND ` + journal.CmdrFilterSQL + `
		  AND system_address IN (` + placeholders + `)
	) WHERE rn = 1`

	rows, queryError := engine.DB.Query(query, queryArgs...)
	if queryError != nil {
		return fmt.Errorf("preload system populations: %w", queryError)
	}
	defer rows.Close()

	var populationPayload struct {
		Population int64 `json:"Population"`
	}
	for rows.Next() {
		var systemAddress int64
		var rawJSON string
		if scanError := rows.Scan(&systemAddress, &rawJSON); scanError != nil {
			return fmt.Errorf("scan preloaded population row: %w", scanError)
		}
		populationPayload.Population = 0
		if json.Unmarshal([]byte(rawJSON), &populationPayload) == nil {
			state.populationBySystem[systemAddress] = populationPayload.Population
		}
	}
	if iterationError := rows.Err(); iterationError != nil {
		return fmt.Errorf("iterate preloaded population rows: %w", iterationError)
	}
	return nil
}

