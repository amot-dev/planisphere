// Package journal ingests Elite Dangerous Player Journal files into SQLite and
// tails the active journal in real time.
package journal

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Typed views of the journal events this application understands. They are
// decoded from journal_events.raw_json on demand, which means formulas can be
// re-evaluated against historical data at any time.

// FSDJumpEvent is a hyperspace jump.
type FSDJumpEvent struct {
	Timestamp     string    `json:"timestamp"`
	StarSystem    string    `json:"StarSystem"`
	SystemAddress int64     `json:"SystemAddress"`
	StarPos       []float64 `json:"StarPos"`
	JumpDist      float64   `json:"JumpDist"`
	FuelUsed      float64   `json:"FuelUsed"`
	BoostUsed     int       `json:"BoostUsed"` // 1 basic, 2 standard, 3 premium synthesis, 4 neutron/white dwarf jet cone
}

// LocationEvent is written at login / respawn (and mirrors CarrierJump).
type LocationEvent struct {
	Timestamp     string    `json:"timestamp"`
	StarSystem    string    `json:"StarSystem"`
	SystemAddress int64     `json:"SystemAddress"`
	StarPos       []float64 `json:"StarPos"`
	Docked        bool      `json:"Docked"`
	StationType   string    `json:"StationType"`
}

// CarrierJumpEvent is written when the player is aboard a jumping fleet carrier.
type CarrierJumpEvent struct {
	Timestamp     string    `json:"timestamp"`
	StarSystem    string    `json:"StarSystem"`
	SystemAddress int64     `json:"SystemAddress"`
	StarPos       []float64 `json:"StarPos"`
	MarketID      int64     `json:"MarketID"`
	StationName   string    `json:"StationName"`
}

// CarrierStatsEvent is written when opening carrier management.
type CarrierStatsEvent struct {
	Timestamp  string `json:"timestamp"`
	CarrierID  int64  `json:"CarrierID"`
	Callsign   string `json:"Callsign"`
	Name       string `json:"Name"`
	FuelLevel  int    `json:"FuelLevel"`
	SpaceUsage struct {
		TotalCapacity int `json:"TotalCapacity"`
		FreeSpace     int `json:"FreeSpace"`
	} `json:"SpaceUsage"`
}

// UsedCapacity returns occupied carrier capacity in tonnes.
func (statsEvent CarrierStatsEvent) UsedCapacity() int {
	used := statsEvent.SpaceUsage.TotalCapacity - statsEvent.SpaceUsage.FreeSpace
	if used < 0 {
		return 0
	}
	return used
}

// ScanEvent is an FSS / auto / detailed body scan.
type ScanEvent struct {
	Timestamp      string  `json:"timestamp"`
	ScanType       string  `json:"ScanType"`
	BodyName       string  `json:"BodyName"`
	BodyID         int64   `json:"BodyID"`
	StarSystem     string  `json:"StarSystem"`
	SystemAddress  int64   `json:"SystemAddress"`
	StarType       string  `json:"StarType"`
	StellarMass    float64 `json:"StellarMass"`
	PlanetClass    string  `json:"PlanetClass"`
	MassEM         float64 `json:"MassEM"`
	TerraformState string  `json:"TerraformState"`
	WasDiscovered  bool    `json:"WasDiscovered"`
	WasMapped      bool    `json:"WasMapped"`
	WasFootfalled  *bool   `json:"WasFootfalled"`
}

// IsBeltCluster reports whether the scan is of an asteroid belt cluster.
func (scanEvent ScanEvent) IsBeltCluster() bool {
	return strings.Contains(scanEvent.BodyName, "Belt Cluster") || (scanEvent.StarType == "" && scanEvent.PlanetClass == "")
}

// SAAScanCompleteEvent is a completed DSS (surface probe) mapping.
type SAAScanCompleteEvent struct {
	Timestamp        string `json:"timestamp"`
	BodyName         string `json:"BodyName"`
	SystemAddress    int64  `json:"SystemAddress"`
	BodyID           int64  `json:"BodyID"`
	ProbesUsed       int    `json:"ProbesUsed"`
	EfficiencyTarget int    `json:"EfficiencyTarget"`
}

// ScanOrganicEvent is an exobiology Log / Sample / Analyse step.
type ScanOrganicEvent struct {
	Timestamp        string `json:"timestamp"`
	ScanType         string `json:"ScanType"` // Log, Sample, Analyse
	Genus            string `json:"Genus"`
	GenusLocalised   string `json:"Genus_Localised"`
	Species          string `json:"Species"`
	SpeciesLocalised string `json:"Species_Localised"`
	Variant          string `json:"Variant"`
	VariantLocalised string `json:"Variant_Localised"`
	WasLogged        *bool  `json:"WasLogged"`
	SystemAddress    int64  `json:"SystemAddress"`
	Body             int64  `json:"Body"`
}

// LoadGameEvent is written on every game load.
type LoadGameEvent struct {
	Timestamp     string `json:"timestamp"`
	Commander     string `json:"Commander"`
	FID           string `json:"FID"`
	Ship          string `json:"Ship"`
	ShipLocalised string `json:"Ship_Localised"`
	ShipID        int64  `json:"ShipID"`
	ShipName      string `json:"ShipName"`
	ShipIdent     string `json:"ShipIdent"`
	Odyssey       *bool  `json:"Odyssey"`
}

// ShipyardSwapEvent is written when switching to a stored ship.
type ShipyardSwapEvent struct {
	Timestamp         string `json:"timestamp"`
	ShipType          string `json:"ShipType"`
	ShipTypeLocalised string `json:"ShipType_Localised"`
	ShipID            int64  `json:"ShipID"`
	StoreOldShip      string `json:"StoreOldShip"`
	StoreShipID       int64  `json:"StoreShipID"`
}

// StatisticsEvent holds the subset of the lifetime Statistics we use for the
// "before expedition" baseline.
type StatisticsEvent struct {
	Timestamp   string `json:"timestamp"`
	Exploration struct {
		ExplorationProfits     float64 `json:"Exploration_Profits"`
		PlanetsScannedToLevel2 float64 `json:"Planets_Scanned_To_Level_2"`
		PlanetsScannedToLevel3 float64 `json:"Planets_Scanned_To_Level_3"`
		EfficientScans         float64 `json:"Efficient_Scans"`
		TotalHyperspaceDist    float64 `json:"Total_Hyperspace_Distance"`
		TotalHyperspaceJumps   float64 `json:"Total_Hyperspace_Jumps"`
		SystemsVisited         float64 `json:"Systems_Visited"`
	} `json:"Exploration"`
	Exobiology struct {
		OrganicData        float64 `json:"Organic_Data"`
		OrganicDataProfits float64 `json:"Organic_Data_Profits"`
		FirstLogged        float64 `json:"First_Logged"`
		FirstLoggedProfits float64 `json:"First_Logged_Profits"`
	} `json:"Exobiology"`
	FleetCarrier map[string]json.RawMessage `json:"FLEETCARRIER"`
}

// FleetCarrierNumber reads a numeric FLEETCARRIER statistic. Some of these are
// written as strings with units (e.g. "1234 LY"), so parsing is lenient.
func (statsEvent StatisticsEvent) FleetCarrierNumber(key string) float64 {
	raw, ok := statsEvent.FleetCarrier[key]
	if !ok {
		return 0
	}
	var numericValue float64
	if unmarshalError := json.Unmarshal(raw, &numericValue); unmarshalError == nil {
		return numericValue
	}
	var stringValue string
	if unmarshalError := json.Unmarshal(raw, &stringValue); unmarshalError == nil {
		return leadingNumber(stringValue)
	}
	return 0
}

func leadingNumber(rawString string) float64 {
	trimmed := strings.TrimSpace(strings.ReplaceAll(rawString, ",", ""))
	end := 0
	for end < len(trimmed) && (trimmed[end] == '.' || trimmed[end] == '-' || (trimmed[end] >= '0' && trimmed[end] <= '9')) {
		end++
	}
	parsedNumber, _ := strconv.ParseFloat(trimmed[:end], 64)
	return parsedNumber
}

// Vec returns the StarPos as (coordX, coordY, coordZ) and whether it was present.
func Vec(positionCoords []float64) (coordX, coordY, coordZ float64, hasCoords bool) {
	if len(positionCoords) != 3 {
		return 0, 0, 0, false
	}
	return positionCoords[0], positionCoords[1], positionCoords[2], true
}
