package replay

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// SessionState tracks chronological game-session and system metadata across
// journal events so valuation and realization share identical Odyssey,
// population, and Fleet Carrier docking context.
type SessionState struct {
	defaultOdyssey          bool
	hasSeenLoadGame         bool
	currentOdyssey          bool
	isDockedOnFleetCarrier  bool
	pendingConcourseRespawn bool
	carrierSystemAddress    int64
	populationBySystem      map[int64]int64
}

// NewSessionState initializes a chronological session state tracker for replay.
func NewSessionState(defaultOdyssey bool, estimatedSystemCount int) *SessionState {
	if estimatedSystemCount < 16 {
		estimatedSystemCount = 16
	}
	return &SessionState{
		defaultOdyssey:     defaultOdyssey,
		populationBySystem: make(map[int64]int64, estimatedSystemCount),
	}
}

// OdysseyAt reports whether Odyssey expansion rules apply at timestamp.
func (state *SessionState) OdysseyAt(timestamp string) bool {
	if !state.defaultOdyssey {
		return false
	}
	if state.hasSeenLoadGame {
		return state.currentOdyssey
	}
	if timestamp != "" && timestamp < reference.OdysseyReleaseTimestamp {
		return false
	}
	return state.defaultOdyssey
}

// SystemPopulation returns the latest observed population for systemAddress.
func (state *SessionState) SystemPopulation(systemAddress int64) int64 {
	return state.populationBySystem[systemAddress]
}

// IsDockedOnFleetCarrier reports whether the commander is currently docked at
// or aboard a Fleet Carrier concourse.
func (state *SessionState) IsDockedOnFleetCarrier() bool {
	return state.isDockedOnFleetCarrier
}

// ApplyLoadGame updates Odyssey session state from a LoadGame JSON payload.
func (state *SessionState) ApplyLoadGame(rawJSON []byte) {
	var loadGameEvent journal.LoadGameEvent
	if json.Unmarshal(rawJSON, &loadGameEvent) != nil {
		return
	}
	state.hasSeenLoadGame = true
	state.currentOdyssey = loadGameEvent.Odyssey != nil && *loadGameEvent.Odyssey
	state.pendingConcourseRespawn = false
}

// ApplyLocation updates system population and carrier concourse docking state
// from a Location JSON payload.
func (state *SessionState) ApplyLocation(rawJSON []byte) {
	var locationEvent journal.LocationEvent
	if json.Unmarshal(rawJSON, &locationEvent) != nil || locationEvent.Multicrew {
		return
	}
	wasPendingConcourseRespawn := state.pendingConcourseRespawn
	state.pendingConcourseRespawn = false
	if locationEvent.SystemAddress != 0 {
		state.populationBySystem[locationEvent.SystemAddress] = locationEvent.Population
	}
	if locationEvent.Docked {
		state.setFleetCarrierDocked(
			strings.EqualFold(locationEvent.StationType, reference.FleetCarrierStationType),
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

// ApplyDocked updates carrier docking state from a Docked JSON payload.
func (state *SessionState) ApplyDocked(rawJSON []byte) {
	var dockedEvent journal.DockedEvent
	if json.Unmarshal(rawJSON, &dockedEvent) != nil {
		return
	}
	state.setFleetCarrierDocked(
		strings.EqualFold(dockedEvent.StationType, reference.FleetCarrierStationType),
		dockedEvent.SystemAddress,
	)
}

// ApplyUndocked clears active Fleet Carrier docking state upon departure.
func (state *SessionState) ApplyUndocked() {
	state.setFleetCarrierDocked(false, 0)
}

// ApplyFSDJumpEvent updates system population and clears docking state on jump.
func (state *SessionState) ApplyFSDJumpEvent(jumpEvent *journal.FSDJumpEvent) {
	if jumpEvent == nil || jumpEvent.Multicrew {
		return
	}
	if jumpEvent.SystemAddress != 0 {
		state.populationBySystem[jumpEvent.SystemAddress] = jumpEvent.Population
	}
	state.setFleetCarrierDocked(false, 0)
}

// ApplyCarrierJump updates system population and onboard carrier docking state.
func (state *SessionState) ApplyCarrierJump(rawJSON []byte) {
	var carrierJumpEvent journal.CarrierJumpEvent
	if json.Unmarshal(rawJSON, &carrierJumpEvent) != nil {
		return
	}
	if carrierJumpEvent.SystemAddress != 0 {
		state.populationBySystem[carrierJumpEvent.SystemAddress] = carrierJumpEvent.Population
	}
	if carrierJumpEvent.Docked {
		state.setFleetCarrierDocked(
			strings.EqualFold(carrierJumpEvent.StationType, reference.FleetCarrierStationType),
			carrierJumpEvent.SystemAddress,
		)
	} else if carrierJumpEvent.OnFoot {
		state.setFleetCarrierDocked(true, carrierJumpEvent.SystemAddress)
	}
}

// ApplyResurrect clears active docking state and marks non-ship concourse
// respawns so the ensuing Location event can restore carrier context.
func (state *SessionState) ApplyResurrect(resurrectOption string) {
	state.setFleetCarrierDocked(false, 0)
	normalizedOption := strings.ToLower(strings.TrimSpace(resurrectOption))
	state.pendingConcourseRespawn = normalizedOption == "recover" || normalizedOption == "escape"
}

func (state *SessionState) setFleetCarrierDocked(dockedOnCarrier bool, systemAddress int64) {
	state.pendingConcourseRespawn = false
	state.isDockedOnFleetCarrier = dockedOnCarrier
	if dockedOnCarrier {
		state.carrierSystemAddress = systemAddress
	} else {
		state.carrierSystemAddress = 0
	}
}

// ToBodyScanWithPopulation converts a decoded journal scan into a valuation input.
func ToBodyScanWithPopulation(scanEvent *journal.ScanEvent, systemPopulation int64) reference.BodyScan {
	return reference.BodyScan{
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

// SeedLoadGameBeforeScoped loads the latest LoadGame event matching scope at or
// before afterTimestamp into state when replaying a bounded window.
func SeedLoadGameBeforeScoped(queryer db.Queryer, afterTimestamp string, state *SessionState, scope ProfileScope) error {
	if afterTimestamp == "" {
		return nil
	}
	var rawLoadGame []byte
	queryError := queryer.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'LoadGame' AND timestamp <= ? AND `+scope.SQLPredicate("")+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`, afterTimestamp).Scan(&rawLoadGame)
	if queryError == sql.ErrNoRows {
		return nil
	}
	if queryError != nil {
		return fmt.Errorf("seed LoadGame before %s: %w", afterTimestamp, queryError)
	}
	state.ApplyLoadGame(rawLoadGame)
	return nil
}

// PreloadSystemPopulationsScoped batch-loads the latest known Population up to
// untilTimestamp matching scope when afterTimestamp is non-empty.
func PreloadSystemPopulationsScoped(queryer db.Queryer, afterTimestamp string, loadedRows []Row, untilTimestamp string, state *SessionState, scope ProfileScope) error {
	if afterTimestamp == "" {
		return nil
	}
	return PreloadSystemPopulationsForRowsScoped(queryer, loadedRows, untilTimestamp, state, scope)
}

// PreloadSystemPopulationsForRowsScoped batch-loads the latest known Population
// up to untilTimestamp matching scope for unseen systems in loadedRows.
func PreloadSystemPopulationsForRowsScoped(queryer db.Queryer, loadedRows []Row, untilTimestamp string, state *SessionState, scope ProfileScope) error {
	populations, err := LoadPopulationMapScoped(queryer, collectUnseenSystemAddresses(loadedRows), untilTimestamp, scope)
	if err != nil {
		return err
	}
	for systemAddress, population := range populations {
		state.populationBySystem[systemAddress] = population
	}
	return nil
}

// LoadPopulationMapScoped batch-loads the latest known Population up to
// untilTimestamp matching scope for systemAddresses.
func LoadPopulationMapScoped(queryer db.Queryer, systemAddresses []int64, untilTimestamp string, scope ProfileScope) (map[int64]int64, error) {
	populations := make(map[int64]int64, len(systemAddresses))
	if len(systemAddresses) == 0 {
		return populations, nil
	}
	if untilTimestamp == "" {
		untilTimestamp = "9999-12-31T23:59:59Z"
	}
	const chunkSize = 200
	scopePredicate := scope.SQLPredicate("")
	for startIndex := 0; startIndex < len(systemAddresses); startIndex += chunkSize {
		endIndex := min(startIndex+chunkSize, len(systemAddresses))
		if err := loadPopulationChunk(queryer, systemAddresses[startIndex:endIndex], untilTimestamp, scopePredicate, populations); err != nil {
			return nil, err
		}
	}
	return populations, nil
}

func collectUnseenSystemAddresses(loadedRows []Row) []int64 {
	seenArrivalSystems := make(map[int64]bool, len(loadedRows)/4)
	for _, rowItem := range loadedRows {
		switch rowItem.Type {
		case journal.EventFSDJump, journal.EventLocation, journal.EventCarrierJump:
			if rowItem.SystemAddress.Valid && rowItem.SystemAddress.Int64 != 0 {
				seenArrivalSystems[rowItem.SystemAddress.Int64] = true
			}
		}
	}
	missingSet := make(map[int64]bool)
	var missingAddresses []int64
	for _, rowItem := range loadedRows {
		switch rowItem.Type {
		case journal.EventScan, journal.EventSAAScanComplete, journal.EventScanOrganic:
			if rowItem.SystemAddress.Valid && rowItem.SystemAddress.Int64 != 0 {
				address := rowItem.SystemAddress.Int64
				if !seenArrivalSystems[address] && !missingSet[address] {
					missingSet[address] = true
					missingAddresses = append(missingAddresses, address)
				}
			}
		}
	}
	return missingAddresses
}

func loadPopulationChunk(queryer db.Queryer, addressChunk []int64, untilTimestamp, scopePredicate string, populations map[int64]int64) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(addressChunk)), ",")
	queryArgs := make([]any, 0, len(addressChunk)+1)
	queryArgs = append(queryArgs, untilTimestamp)
	for _, address := range addressChunk {
		queryArgs = append(queryArgs, address)
	}
	// Unary +event forces SQLite to seek idx_journal_events_body (system_address) instead of scanning all arrival rows on idx_journal_events_event_ts.
	query := `SELECT system_address, raw_json FROM (
		SELECT system_address, raw_json,
		       ROW_NUMBER() OVER (PARTITION BY system_address ORDER BY timestamp DESC, id DESC) as row_rank
		FROM journal_events
		WHERE +event IN ('FSDJump', 'Location', 'CarrierJump')
		  AND timestamp <= ? AND ` + scopePredicate + `
		  AND system_address IN (` + placeholders + `)
	) WHERE row_rank = 1`

	databaseRows, queryError := queryer.Query(query, queryArgs...)
	if queryError != nil {
		return fmt.Errorf("preload system populations: %w", queryError)
	}
	defer databaseRows.Close()

	var populationPayload struct {
		Population int64 `json:"Population"`
	}
	for databaseRows.Next() {
		var systemAddress int64
		var rawJSON string
		if scanError := databaseRows.Scan(&systemAddress, &rawJSON); scanError != nil {
			return fmt.Errorf("scan preloaded population row: %w", scanError)
		}
		populationPayload.Population = 0
		if json.Unmarshal([]byte(rawJSON), &populationPayload) == nil {
			populations[systemAddress] = populationPayload.Population
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		return fmt.Errorf("iterate preloaded population rows: %w", iterationError)
	}
	return nil
}
