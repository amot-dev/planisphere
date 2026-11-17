// Package journal parses, deduplicates, and ingests Elite Dangerous journal events.
package journal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"sync"

	"planisphere/internal/reference"
)

// RelevantEvents is the set of journal event types persisted to journal_events.
var RelevantEvents map[string]bool

// LocationEvents are the events that place the player in a system.
var LocationEvents = NewEventSet(EventFSDJump, EventLocation, EventCarrierJump).Names()

// Event is a parsed, persistable journal event.
type Event struct {
	UID           string
	Timestamp     string
	Type          EventType
	Event         string
	Commander     string
	FID           string
	SystemName    string
	SystemAddress *int64
	BodyID        *int64
	BodyName      string
	StarPos       *reference.Vec3
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
	JumpType      string          `json:"JumpType"`
	BoostValue    *float64        `json:"BoostValue"`
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
	BioData       []BioDataItem   `json:"BioData"`
	KillerName    string          `json:"KillerName"`
	KillerShip    string          `json:"KillerShip"`
	Option        string          `json:"Option"`
	Cost          *int64          `json:"Cost"`
	NpcCrewID     *int64          `json:"NpcCrewId"`
	Amount        *int64          `json:"Amount"`
}

var eventKey = []byte(`"event"`)

// PeekEventName extracts the "event" value from a journal line without a full
// JSON decode. Returns "" when not found.
func PeekEventName(line []byte) string {
	eventKeyIndex := bytes.Index(line, eventKey)
	if eventKeyIndex < 0 {
		return ""
	}
	rest := line[eventKeyIndex+len(eventKey):]
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

var sha256HasherPool = sync.Pool{
	New: func() any {
		return sha256.New()
	},
}

// ComputeUID returns SHA256(timestamp + event + key identifiers) as hex. The
// key identifiers are chosen per event type so that the same in-game action is
// always mapped to the same UID, regardless of which file it was read from.
func ComputeUID(timestamp, event string, keys ...string) string {
	hasher := sha256HasherPool.Get().(hash.Hash)
	defer func() {
		hasher.Reset()
		sha256HasherPool.Put(hasher)
	}()

	_, _ = io.WriteString(hasher, timestamp)
	_, _ = io.WriteString(hasher, "\x00")
	_, _ = io.WriteString(hasher, event)
	for _, key := range keys {
		_, _ = io.WriteString(hasher, "\x00")
		_, _ = io.WriteString(hasher, key)
	}

	var digestBuffer [sha256.Size]byte
	sum := hasher.Sum(digestBuffer[:0])
	return hex.EncodeToString(sum)
}

func keyIdentifiers(eventType EventType, fields *commonFields, raw []byte) []string {
	body := strings.TrimSpace(string(fields.Body))
	switch eventType {
	case EventFSDJump, EventLocation, EventCarrierJump, EventFSSDiscoveryScan, EventFSDTarget:
		return []string{optInt(fields.SystemAddress)}
	case EventStartJump:
		return []string{strings.ToLower(strings.TrimSpace(fields.JumpType)), optInt(fields.SystemAddress)}
	case EventJetConeBoost:
		return []string{optFloat(fields.BoostValue)}
	case EventSynthesis:
		return []string{strings.ToLower(strings.TrimSpace(fields.Name))}
	case EventScan:
		return []string{optInt(fields.SystemAddress), optInt(fields.BodyID), fields.ScanType}
	case EventSAAScanComplete:
		return []string{optInt(fields.SystemAddress), optInt(fields.BodyID)}
	case EventScanOrganic:
		return []string{optInt(fields.SystemAddress), body, fields.Species, fields.ScanType}
	case EventCommander:
		return []string{fields.FID, fields.Commander}
	case EventLoadGame:
		return []string{fields.FID, optInt(fields.ShipID)}
	case EventShipyardSwap:
		return []string{optInt(fields.ShipID), optInt(fields.StoreShipID)}
	case EventCarrierStats:
		return []string{optInt(fields.CarrierID), optFloat(fields.FuelLevel)}
	case EventCarrierDepositFuel:
		return []string{optInt(fields.CarrierID), optInt(fields.Total)}
	case EventCarrierJumpRequest:
		return []string{optInt(fields.CarrierID), optInt(fields.SystemAddress)}
	case EventDocked, EventUndocked:
		return []string{optInt(fields.SystemAddress), optInt(fields.MarketID)}
	case EventMultiSellExplorationData, EventSellExplorationData:
		return []string{optInt(fields.TotalEarnings)}
	case EventSellOrganicData:
		var totalValue, totalBonus int64
		for _, item := range fields.BioData {
			totalValue += item.Value
			totalBonus += item.Bonus
		}
		return []string{
			optInt(fields.MarketID),
			strconv.Itoa(len(fields.BioData)),
			strconv.FormatInt(totalValue, 10),
			strconv.FormatInt(totalBonus, 10),
		}
	case EventDied:
		return []string{fields.KillerName, fields.KillerShip}
	case EventResurrect:
		return []string{fields.Option, optInt(fields.Cost)}
	case EventNpcCrewPaidWage:
		return []string{optInt(fields.NpcCrewID), optInt(fields.Amount)}
	case EventStatistics:
		return nil
	default:
		// Unknown shape: fall back to the whole canonical line.
		sum := sha256.Sum256(raw)
		return []string{hex.EncodeToString(sum[:])}
	}
}

func shouldPersistRelevantEvent(fields *commonFields) (EventType, bool) {
	eventType, knownEvent := ParseEventType(fields.Event)
	if !knownEvent || fields.Timestamp == "" {
		return 0, false
	}
	switch eventType {
	case EventStartJump:
		return eventType, strings.EqualFold(strings.TrimSpace(fields.JumpType), "Hyperspace")
	case EventSynthesis:
		return eventType, FSDSynthesisBoostTier(fields.Name) > 0
	default:
		return eventType, true
	}
}

func populateParsedEventDetails(parsedEvent *Event, eventType EventType, fields *commonFields) {
	if eventType == EventCommander && parsedEvent.Commander == "" {
		parsedEvent.Commander = strings.TrimSpace(fields.Name)
	}
	switch {
	case fields.StarSystem != "":
		parsedEvent.SystemName = fields.StarSystem
	case fields.SystemName != "":
		parsedEvent.SystemName = fields.SystemName
	case eventType == EventFSDTarget:
		parsedEvent.SystemName = fields.Name
	}
	if eventType == EventScanOrganic && parsedEvent.BodyID == nil && len(fields.Body) > 0 {
		if parsedBodyID, numError := strconv.ParseInt(strings.TrimSpace(string(fields.Body)), 10, 64); numError == nil {
			parsedEvent.BodyID = &parsedBodyID
		}
	}
	if eventType == EventLocation && parsedEvent.BodyName == "" && len(fields.Body) > 0 {
		var bodyString string
		if json.Unmarshal(fields.Body, &bodyString) == nil {
			parsedEvent.BodyName = bodyString
		}
	}
	if coordX, coordY, coordZ, hasCoords := Vec(fields.StarPos); hasCoords {
		parsedEvent.StarPos = &reference.Vec3{X: coordX, Y: coordY, Z: coordZ}
	}
}

// ParseLine parses a journal line. ok is false when the line is not a relevant
// event (or is not valid JSON).
func ParseLine(line []byte, sourceFile string) (parsedEvent Event, ok bool, parseError error) {
	line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("\xef\xbb\xbf")))
	if len(line) == 0 || line[0] != '{' {
		return Event{}, false, nil
	}
	peekedName := PeekEventName(line)
	if _, knownEvent := ParseEventType(peekedName); !knownEvent {
		return Event{}, false, nil
	}
	var fields commonFields
	if unmarshalError := json.Unmarshal(line, &fields); unmarshalError != nil {
		return Event{}, false, fmt.Errorf("decode %s: %w", peekedName, unmarshalError)
	}
	eventType, persistEvent := shouldPersistRelevantEvent(&fields)
	if !persistEvent {
		return Event{}, false, nil
	}
	normalizedTimestamp, normalizeError := reference.NormalizeISO(fields.Timestamp)
	if normalizeError != nil {
		return Event{}, false, normalizeError
	}
	parsedEvent = Event{
		Timestamp:     normalizedTimestamp,
		Type:          eventType,
		Event:         eventType.String(),
		Commander:     strings.TrimSpace(fields.Commander),
		FID:           strings.TrimSpace(fields.FID),
		SystemAddress: fields.SystemAddress,
		BodyID:        fields.BodyID,
		BodyName:      fields.BodyName,
		SourceFile:    sourceFile,
		Raw:           string(line),
	}
	populateParsedEventDetails(&parsedEvent, eventType, &fields)
	parsedEvent.UID = ComputeUID(normalizedTimestamp, eventType.String(), keyIdentifiers(eventType, &fields, line)...)
	return parsedEvent, true, nil
}
