package journal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"planisphere/internal/ed"
)

// RelevantEvents is the set of journal event types persisted to journal_events.
var RelevantEvents = map[string]bool{
	"Commander":                true,
	"LoadGame":                 true,
	"Statistics":               true,
	"Location":                 true,
	"FSDJump":                  true,
	"CarrierJump":              true,
	"CarrierJumpRequest":       true,
	"CarrierStats":             true,
	"CarrierDepositFuel":       true,
	"Docked":                   true,
	"FSDTarget":                true,
	"Scan":                     true,
	"SAAScanComplete":          true,
	"FSSDiscoveryScan":         true,
	"ScanOrganic":              true,
	"MultiSellExplorationData": true,
	"SellExplorationData":      true,
	"SellOrganicData":          true,
	"ShipyardSwap":             true,
}

// LocationEvents are the events that place the player in a system.
var LocationEvents = []string{"FSDJump", "Location", "CarrierJump"}

// Event is a parsed, persistable journal event.
type Event struct {
	UID           string
	Timestamp     string
	Event         string
	Commander     string
	FID           string
	SystemName    string
	SystemAddress *int64
	BodyID        *int64
	BodyName      string
	StarPos       *ed.Vec3
	SourceFile    string
	Raw           string
}

// commonFields are the identifying fields extracted from any relevant event.
type commonFields struct {
	Timestamp     string          `json:"timestamp"`
	Event         string          `json:"event"`
	Commander     string          `json:"Commander"`
	StarSystem    string          `json:"StarSystem"`
	SystemName    string          `json:"SystemName"`
	Name          string          `json:"Name"`
	SystemAddress *int64          `json:"SystemAddress"`
	BodyID        *int64          `json:"BodyID"`
	Body          json.RawMessage `json:"Body"`
	BodyName      string          `json:"BodyName"`
	StarPos       []float64       `json:"StarPos"`
	ScanType      string          `json:"ScanType"`
	Species       string          `json:"Species"`
	ShipID        *int64          `json:"ShipID"`
	StoreShipID   *int64          `json:"StoreShipID"`
	FID           string          `json:"FID"`
	CarrierID     *int64          `json:"CarrierID"`
	FuelLevel     *float64        `json:"FuelLevel"`
	Total         *int64          `json:"Total"`
	TotalEarnings *int64          `json:"TotalEarnings"`
	MarketID      *int64          `json:"MarketID"`
}

var eventKey = []byte(`"event"`)

// PeekEventName extracts the "event" value from a journal line without a full
// JSON decode. Returns "" when not found.
func PeekEventName(line []byte) string {
	i := bytes.Index(line, eventKey)
	if i < 0 {
		return ""
	}
	rest := line[i+len(eventKey):]
	quoteStart := bytes.IndexByte(rest, '"')
	if quoteStart < 0 {
		return ""
	}
	rest = rest[quoteStart+1:]
	quoteEnd := bytes.IndexByte(rest, '"')
	if quoteEnd < 0 {
		return ""
	}
	return string(rest[:quoteEnd])
}

func optInt(intValue *int64) string {
	if intValue == nil {
		return ""
	}
	return strconv.FormatInt(*intValue, 10)
}

func optFloat(floatValue *float64) string {
	if floatValue == nil {
		return ""
	}
	return strconv.FormatFloat(*floatValue, 'f', -1, 64)
}

// ComputeUID returns SHA256(timestamp + event + key identifiers) as hex. The
// key identifiers are chosen per event type so that the same in-game action is
// always mapped to the same UID, regardless of which file it was read from.
func ComputeUID(timestamp, event string, keys ...string) string {
	hasher := sha256.New()
	hasher.Write([]byte(timestamp))
	hasher.Write([]byte{0})
	hasher.Write([]byte(event))
	for _, key := range keys {
		hasher.Write([]byte{0})
		hasher.Write([]byte(key))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func keyIdentifiers(fields *commonFields, raw []byte) []string {
	body := strings.TrimSpace(string(fields.Body))
	switch fields.Event {
	case "FSDJump", "Location", "CarrierJump", "FSSDiscoveryScan", "FSDTarget":
		return []string{optInt(fields.SystemAddress)}
	case "Scan":
		return []string{optInt(fields.SystemAddress), optInt(fields.BodyID), fields.ScanType}
	case "SAAScanComplete":
		return []string{optInt(fields.SystemAddress), optInt(fields.BodyID)}
	case "ScanOrganic":
		return []string{optInt(fields.SystemAddress), body, fields.Species, fields.ScanType}
	case "Commander":
		return []string{fields.FID, fields.Commander}
	case "LoadGame":
		return []string{fields.FID, optInt(fields.ShipID)}
	case "ShipyardSwap":
		return []string{optInt(fields.ShipID), optInt(fields.StoreShipID)}
	case "CarrierStats":
		return []string{optInt(fields.CarrierID), optFloat(fields.FuelLevel)}
	case "CarrierDepositFuel":
		return []string{optInt(fields.CarrierID), optInt(fields.Total)}
	case "CarrierJumpRequest":
		return []string{optInt(fields.CarrierID), optInt(fields.SystemAddress)}
	case "Docked":
		return []string{optInt(fields.SystemAddress), optInt(fields.MarketID)}
	case "MultiSellExplorationData", "SellExplorationData":
		return []string{optInt(fields.TotalEarnings)}
	case "Statistics":
		return nil
	default:
		// Unknown shape: fall back to the whole canonical line.
		sum := sha256.Sum256(raw)
		return []string{hex.EncodeToString(sum[:])}
	}
}

// ParseLine parses a journal line. ok is false when the line is not a relevant
// event (or is not valid JSON).
func ParseLine(line []byte, sourceFile string) (parsedEvent Event, ok bool, parseError error) {
	line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("\xef\xbb\xbf")))
	if len(line) == 0 || line[0] != '{' {
		return Event{}, false, nil
	}
	name := PeekEventName(line)
	if !RelevantEvents[name] {
		return Event{}, false, nil
	}
	var fields commonFields
	if unmarshalError := json.Unmarshal(line, &fields); unmarshalError != nil {
		return Event{}, false, fmt.Errorf("decode %s: %w", name, unmarshalError)
	}
	if !RelevantEvents[fields.Event] || fields.Timestamp == "" {
		return Event{}, false, nil
	}
	normalizedTimestamp, normalizeError := ed.NormalizeISO(fields.Timestamp)
	if normalizeError != nil {
		return Event{}, false, normalizeError
	}
	parsedEvent = Event{
		Timestamp:     normalizedTimestamp,
		Event:         fields.Event,
		Commander:     strings.TrimSpace(fields.Commander),
		FID:           strings.TrimSpace(fields.FID),
		SystemAddress: fields.SystemAddress,
		BodyID:        fields.BodyID,
		BodyName:      fields.BodyName,
		SourceFile:    sourceFile,
		Raw:           string(line),
	}
	if fields.Event == "Commander" && parsedEvent.Commander == "" {
		parsedEvent.Commander = strings.TrimSpace(fields.Name)
	}
	switch {
	case fields.StarSystem != "":
		parsedEvent.SystemName = fields.StarSystem
	case fields.SystemName != "":
		parsedEvent.SystemName = fields.SystemName
	case fields.Event == "FSDTarget":
		parsedEvent.SystemName = fields.Name
	}
	if fields.Event == "ScanOrganic" && parsedEvent.BodyID == nil && len(fields.Body) > 0 {
		if parsedBodyID, numError := strconv.ParseInt(strings.TrimSpace(string(fields.Body)), 10, 64); numError == nil {
			parsedEvent.BodyID = &parsedBodyID
		}
	}
	if fields.Event == "Location" && parsedEvent.BodyName == "" && len(fields.Body) > 0 {
		var bodyString string
		if json.Unmarshal(fields.Body, &bodyString) == nil {
			parsedEvent.BodyName = bodyString
		}
	}
	if coordX, coordY, coordZ, hasCoords := Vec(fields.StarPos); hasCoords {
		parsedEvent.StarPos = &ed.Vec3{X: coordX, Y: coordY, Z: coordZ}
	}
	parsedEvent.UID = ComputeUID(normalizedTimestamp, fields.Event, keyIdentifiers(&fields, line)...)
	return parsedEvent, true, nil
}
