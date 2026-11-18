package replay

import (
	"encoding/json"
	"log"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
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

// LoadLatestCarrierStats loads the most recent CarrierStats journal event.
func LoadLatestCarrierStats(queryer db.Queryer) (CarrierStatsSnapshot, error) {
	latestRow, queryError := QueryLatestEvent(queryer, journal.NewEventSet(journal.EventCarrierStats))
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

// MatchesTargetCarrier compares an event's CarrierID/MarketID and StationName
// against the player's latest CarrierStats identity when both identifiers are present.
func MatchesTargetCarrier(playerCarrierID int64, playerCallsign string, eventCarrierID int64, eventStationName string) bool {
	if playerCarrierID > 0 && eventCarrierID > 0 {
		return eventCarrierID == playerCarrierID
	}
	trimmedStation := strings.TrimSpace(eventStationName)
	if playerCallsign != "" && trimmedStation != "" {
		return strings.Contains(strings.ToUpper(trimmedStation), strings.ToUpper(playerCallsign))
	}
	return true
}

// QueryCarrierLastLocation returns the system name of the most recent known Fleet Carrier location
// across CarrierJump, Docked/Location aboard the carrier, and CarrierJumpRequest events.
func QueryCarrierLastLocation(queryer db.Queryer, statsSnapshot CarrierStatsSnapshot) (string, error) {
	var (
		playerCarrierID int64
		playerCallsign  string
	)
	if statsSnapshot.Found {
		playerCarrierID = statsSnapshot.Event.CarrierID
		playerCallsign = strings.TrimSpace(statsSnapshot.Event.Callsign)
	}

	jumpSystem, jumpTimestamp := latestMatchedCarrierJump(queryer, playerCarrierID, playerCallsign)
	dockedSystem, dockedTimestamp := latestDockedCarrierLocation(queryer, playerCarrierID, playerCallsign)
	requestSystem, requestTimestamp := latestCarrierJumpRequest(queryer, playerCarrierID, playerCallsign)

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

func latestMatchedCarrierJump(queryer db.Queryer, playerCarrierID int64, playerCallsign string) (string, string) {
	databaseRows, queryError := queryer.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event = 'CarrierJump' AND system_name != '' AND ` + CmdrFilterSQL + `
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
		if MatchesTargetCarrier(playerCarrierID, playerCallsign, jumpPayload.MarketID, jumpPayload.StationName) {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		log.Printf("iterate carrier jump location: %v", iterationError)
	}
	return "", ""
}

func latestDockedCarrierLocation(queryer db.Queryer, playerCarrierID int64, playerCallsign string) (string, string) {
	databaseRows, queryError := queryer.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event IN ('Docked', 'Location') AND system_name != '' AND ` + CmdrFilterSQL + `
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
		if !strings.EqualFold(dockRecord.StationType, reference.FleetCarrierStationType) {
			continue
		}
		if dockRecord.Docked != nil && !*dockRecord.Docked {
			continue
		}
		if MatchesTargetCarrier(playerCarrierID, playerCallsign, dockRecord.MarketID, dockRecord.StationName) {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		log.Printf("iterate docked carrier location: %v", iterationError)
	}
	return "", ""
}

func latestCarrierJumpRequest(queryer db.Queryer, playerCarrierID int64, playerCallsign string) (string, string) {
	databaseRows, queryError := queryer.Query(`SELECT system_name, timestamp, raw_json FROM journal_events
		WHERE event = 'CarrierJumpRequest' AND system_name != '' AND ` + CmdrFilterSQL + `
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
		if MatchesTargetCarrier(playerCarrierID, playerCallsign, requestPayload.CarrierID, "") {
			return strings.TrimSpace(candidateSystem), candidateTimestamp
		}
	}
	if iterationError := databaseRows.Err(); iterationError != nil {
		log.Printf("iterate carrier jump request location: %v", iterationError)
	}
	return "", ""
}

// QueryCarrierDepotInfo returns the latest Fleet Carrier capacity, depot tritium level, and cargo storage.
func QueryCarrierDepotInfo(queryer db.Queryer, statsSnapshot CarrierStatsSnapshot) (CarrierDepotInfo, error) {
	depotInfo := CarrierDepotInfo{
		UsedCapacity: 5000,
		DepotTritium: reference.CarrierFuelTankTonnes,
		Cargo:        0,
	}

	var (
		playerCarrierID int64
		playerCallsign  string
		statsTimestamp  string
	)
	if statsSnapshot.Found {
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

	depositRows, depositQueryError := queryer.Query(`SELECT raw_json, timestamp FROM journal_events
		WHERE event = 'CarrierDepositFuel' AND ` + CmdrFilterSQL + `
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
			if !MatchesTargetCarrier(playerCarrierID, playerCallsign, depositEvent.CarrierID, "") {
				continue
			}
			if statsTimestamp == "" || depositTimestamp >= statsTimestamp {
				depotInfo.DepotTritium = depositEvent.Total
			}
			break
		}
	}

	if depotInfo.DepotTritium <= 0 || depotInfo.DepotTritium > reference.CarrierFuelTankTonnes {
		depotInfo.DepotTritium = reference.CarrierFuelTankTonnes
	}
	return depotInfo, nil
}

