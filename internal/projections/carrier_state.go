package projections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/sources/journal"
)

var (
	carrierStateJournalEvents = journal.NewEventSet(
		journal.EventCarrierStats,
	)
	carrierStateAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// CarrierStatsSnapshot holds the most recent decoded CarrierStats journal event.
type CarrierStatsSnapshot struct {
	Event     journal.CarrierStatsEvent
	Timestamp string
	Found     bool
}

// CarrierDepotInfo contains carrier mass and tritium depot telemetry from journal events.
type CarrierDepotInfo struct {
	UsedCapacity int `json:"usedCapacity"`
	DepotTritium int `json:"depotTritium"`
	Cargo        int `json:"cargo"`
}

// CarrierState caches the latest CarrierStats snapshot and resolves live Fleet
// Carrier capacity, tritium depot fuel level, callsign, and last known carrier
// star system.
type CarrierState struct {
	database    *sql.DB
	latestStats *projection.Cache[CarrierStatsSnapshot]
}

// NewCarrierState creates a CarrierState projection and registers it on bus.
func NewCarrierState(dbConn *sql.DB, bus *projection.Bus) *CarrierState {
	state := &CarrierState{database: dbConn}
	state.latestStats = projection.NewCache(
		bus,
		"carrier_state",
		state.Triggers(),
		state.loadLatestStats,
	)
	return state
}

// Triggers declares the journal and application events that invalidate CarrierState.
func (state *CarrierState) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: carrierStateJournalEvents,
		App:     carrierStateAppEvents,
	}
}

// LatestStats returns the cached latest CarrierStats snapshot.
func (state *CarrierState) LatestStats() (CarrierStatsSnapshot, error) {
	return state.latestStats.Get()
}

// IsValid reports whether the latest CarrierStats snapshot is currently cached.
func (state *CarrierState) IsValid() bool {
	return state.latestStats.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (state *CarrierState) Generation() uint64 {
	return state.latestStats.Generation()
}

func (state *CarrierState) loadLatestStats() (CarrierStatsSnapshot, error) {
	if state.database == nil {
		return CarrierStatsSnapshot{}, fmt.Errorf("carrier state database is nil")
	}
	latestRow, queryError := journal.QueryLatestEvent(state.database, carrierStateJournalEvents)
	if queryError != nil || latestRow == nil {
		return CarrierStatsSnapshot{}, queryError
	}
	var parsedEvent journal.CarrierStatsEvent
	if json.Unmarshal([]byte(latestRow.RawJSON), &parsedEvent) != nil {
		return CarrierStatsSnapshot{}, nil
	}
	return CarrierStatsSnapshot{
		Event:     parsedEvent,
		Timestamp: latestRow.Timestamp,
		Found:     true,
	}, nil
}

// matchesTargetCarrier compares an event's CarrierID/MarketID and StationName
// against the player's latest CarrierStats identity when both identifiers are present.
func matchesTargetCarrier(playerCarrierID int64, playerCallsign string, eventCarrierID int64, eventStationName string) bool {
	if playerCarrierID > 0 && eventCarrierID > 0 {
		return eventCarrierID == playerCarrierID
	}
	trimmedStation := strings.TrimSpace(eventStationName)
	if playerCallsign != "" && trimmedStation != "" {
		return strings.Contains(strings.ToUpper(trimmedStation), strings.ToUpper(playerCallsign))
	}
	return true
}

// LastLocation returns the system name of the most recent known Fleet Carrier location
// across CarrierJump, Docked/Location aboard the carrier, and CarrierJumpRequest events.
func (state *CarrierState) LastLocation() (string, error) {
	if state.database == nil {
		return "", fmt.Errorf("carrier state database is nil")
	}
	var (
		playerCarrierID int64
		playerCallsign  string
	)
	if statsSnapshot, statsError := state.LatestStats(); statsError == nil && statsSnapshot.Found {
		playerCarrierID = statsSnapshot.Event.CarrierID
		playerCallsign = strings.TrimSpace(statsSnapshot.Event.Callsign)
	}

	jumpSystem, jumpTimestamp := state.latestMatchedCarrierJump(playerCarrierID, playerCallsign)
	dockedSystem, dockedTimestamp := state.latestDockedCarrierLocation(playerCarrierID, playerCallsign)
	requestSystem, requestTimestamp := state.latestCarrierJumpRequest(playerCarrierID, playerCallsign)

	bestSystem := jumpSystem
	bestTimestamp := jumpTimestamp
	if dockedTimestamp != "" && (bestTimestamp == "" || dockedTimestamp >= bestTimestamp) {
		bestSystem = dockedSystem
		bestTimestamp = dockedTimestamp
	}
	if requestTimestamp != "" && (bestTimestamp == "" || requestTimestamp >= bestTimestamp) {
		bestSystem = requestSystem
	}
	return strings.TrimSpace(bestSystem), nil
}

func (state *CarrierState) latestMatchedCarrierJump(playerCarrierID int64, playerCallsign string) (string, string) {
	databaseRows, queryError := state.database.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event = 'CarrierJump' AND system_name != '' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 20`)
	if queryError != nil {
		log.Printf("query carrier jump location: %v", queryError)
		return "", ""
	}
	defer databaseRows.Close()

	for databaseRows.Next() {
		var candidateSystem, candidateTimestamp, candidateRawJSON string
		if scanError := databaseRows.Scan(&candidateSystem, &candidateTimestamp, &candidateRawJSON); scanError != nil {
			log.Printf("scan carrier jump location: %v", scanError)
			return "", ""
		}
		var jumpPayload struct {
			MarketID    int64  `json:"MarketID"`
			StationName string `json:"StationName"`
		}
		if json.Unmarshal([]byte(candidateRawJSON), &jumpPayload) != nil {
			continue
		}
		if matchesTargetCarrier(playerCarrierID, playerCallsign, jumpPayload.MarketID, jumpPayload.StationName) {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		log.Printf("iterate carrier jump location: %v", iterationError)
	}
	return "", ""
}

func (state *CarrierState) latestDockedCarrierLocation(playerCarrierID int64, playerCallsign string) (string, string) {
	databaseRows, queryError := state.database.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event IN ('Docked', 'Location') AND system_name != '' AND ` + journal.CmdrFilterSQL + `
		AND raw_json LIKE '%FleetCarrier%'
		ORDER BY timestamp DESC, id DESC LIMIT 20`)
	if queryError != nil {
		log.Printf("query docked carrier location: %v", queryError)
		return "", ""
	}
	defer databaseRows.Close()

	for databaseRows.Next() {
		var candidateSystem, candidateTimestamp, candidateRawJSON string
		if scanError := databaseRows.Scan(&candidateSystem, &candidateTimestamp, &candidateRawJSON); scanError != nil {
			log.Printf("scan docked carrier location: %v", scanError)
			return "", ""
		}
		var dockRecord struct {
			StationType string `json:"StationType"`
			MarketID    int64  `json:"MarketID"`
			StationName string `json:"StationName"`
			Docked      *bool  `json:"Docked"`
		}
		if json.Unmarshal([]byte(candidateRawJSON), &dockRecord) != nil {
			continue
		}
		if !strings.EqualFold(dockRecord.StationType, "FleetCarrier") {
			continue
		}
		if dockRecord.Docked != nil && !*dockRecord.Docked {
			continue
		}
		if matchesTargetCarrier(playerCarrierID, playerCallsign, dockRecord.MarketID, dockRecord.StationName) {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		log.Printf("iterate docked carrier location: %v", iterationError)
	}
	return "", ""
}

func (state *CarrierState) latestCarrierJumpRequest(playerCarrierID int64, playerCallsign string) (string, string) {
	databaseRows, queryError := state.database.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event = 'CarrierJumpRequest' AND system_name != '' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 20`)
	if queryError != nil {
		log.Printf("query carrier jump request location: %v", queryError)
		return "", ""
	}
	defer databaseRows.Close()

	for databaseRows.Next() {
		var candidateSystem, candidateTimestamp, candidateRawJSON string
		if scanError := databaseRows.Scan(&candidateSystem, &candidateTimestamp, &candidateRawJSON); scanError != nil {
			log.Printf("scan carrier jump request location: %v", scanError)
			return "", ""
		}
		var requestPayload struct {
			CarrierID int64 `json:"CarrierID"`
		}
		if json.Unmarshal([]byte(candidateRawJSON), &requestPayload) != nil {
			continue
		}
		if matchesTargetCarrier(playerCarrierID, playerCallsign, requestPayload.CarrierID, "") {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		log.Printf("iterate carrier jump request location: %v", iterationError)
	}
	return "", ""
}

// DepotInfo returns the latest Fleet Carrier capacity, depot tritium level, and cargo storage.
func (state *CarrierState) DepotInfo() (CarrierDepotInfo, error) {
	depotInfo := CarrierDepotInfo{
		UsedCapacity: 5000,
		DepotTritium: 1000,
		Cargo:        0,
	}
	if state.database == nil {
		return depotInfo, fmt.Errorf("carrier state database is nil")
	}

	var (
		playerCarrierID int64
		playerCallsign  string
		statsTimestamp  string
	)
	if statsSnapshot, statsError := state.LatestStats(); statsError == nil && statsSnapshot.Found {
		playerCarrierID = statsSnapshot.Event.CarrierID
		playerCallsign = strings.TrimSpace(statsSnapshot.Event.Callsign)
		statsTimestamp = statsSnapshot.Timestamp
		if usedCapacity := statsSnapshot.Event.UsedCapacity(); usedCapacity > 0 {
			depotInfo.UsedCapacity = usedCapacity
		}
		if statsSnapshot.Event.FuelLevel > 0 {
			depotInfo.DepotTritium = statsSnapshot.Event.FuelLevel
		}
		depotInfo.Cargo = statsSnapshot.Event.SpaceUsage.Cargo
	}

	depositRows, depositQueryError := state.database.Query(`SELECT raw_json, timestamp FROM journal_events
		WHERE event = 'CarrierDepositFuel' AND ` + journal.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 20`)
	if depositQueryError == nil {
		defer depositRows.Close()
		for depositRows.Next() {
			var rawDeposit, depositTimestamp string
			if scanError := depositRows.Scan(&rawDeposit, &depositTimestamp); scanError != nil {
				break
			}
			var depositEvent journal.CarrierDepositFuelEvent
			if json.Unmarshal([]byte(rawDeposit), &depositEvent) != nil || depositEvent.Total <= 0 {
				continue
			}
			if !matchesTargetCarrier(playerCarrierID, playerCallsign, depositEvent.CarrierID, "") {
				continue
			}
			if statsTimestamp == "" || depositTimestamp >= statsTimestamp {
				depotInfo.DepotTritium = depositEvent.Total
			}
			break
		}
	}

	if depotInfo.DepotTritium <= 0 || depotInfo.DepotTritium > 1000 {
		depotInfo.DepotTritium = 1000
	}
	return depotInfo, nil
}

