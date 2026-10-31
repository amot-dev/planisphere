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
	"CarrierStats":             true,
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
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	rest = rest[j+1:]
	k := bytes.IndexByte(rest, '"')
	if k < 0 {
		return ""
	}
	return string(rest[:k])
}

func optInt(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}

func optFloat(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// ComputeUID returns SHA256(timestamp + event + key identifiers) as hex. The
// key identifiers are chosen per event type so that the same in-game action is
// always mapped to the same UID, regardless of which file it was read from.
func ComputeUID(timestamp, event string, keys ...string) string {
	h := sha256.New()
	h.Write([]byte(timestamp))
	h.Write([]byte{0})
	h.Write([]byte(event))
	for _, k := range keys {
		h.Write([]byte{0})
		h.Write([]byte(k))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func keyIdentifiers(c *commonFields, raw []byte) []string {
	body := strings.TrimSpace(string(c.Body))
	switch c.Event {
	case "FSDJump", "Location", "CarrierJump", "FSSDiscoveryScan", "FSDTarget":
		return []string{optInt(c.SystemAddress)}
	case "Scan":
		return []string{optInt(c.SystemAddress), optInt(c.BodyID), c.ScanType}
	case "SAAScanComplete":
		return []string{optInt(c.SystemAddress), optInt(c.BodyID)}
	case "ScanOrganic":
		return []string{optInt(c.SystemAddress), body, c.Species, c.ScanType}
	case "Commander":
		return []string{c.FID, c.Commander}
	case "LoadGame":
		return []string{c.FID, optInt(c.ShipID)}
	case "ShipyardSwap":
		return []string{optInt(c.ShipID), optInt(c.StoreShipID)}
	case "CarrierStats":
		return []string{optInt(c.CarrierID), optFloat(c.FuelLevel)}
	case "MultiSellExplorationData", "SellExplorationData":
		return []string{optInt(c.TotalEarnings)}
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
func ParseLine(line []byte, sourceFile string) (ev Event, ok bool, err error) {
	line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("\xef\xbb\xbf")))
	if len(line) == 0 || line[0] != '{' {
		return Event{}, false, nil
	}
	name := PeekEventName(line)
	if !RelevantEvents[name] {
		return Event{}, false, nil
	}
	var c commonFields
	if err := json.Unmarshal(line, &c); err != nil {
		return Event{}, false, fmt.Errorf("decode %s: %w", name, err)
	}
	if !RelevantEvents[c.Event] || c.Timestamp == "" {
		return Event{}, false, nil
	}
	ts, err := ed.NormalizeISO(c.Timestamp)
	if err != nil {
		return Event{}, false, err
	}
	ev = Event{
		Timestamp:     ts,
		Event:         c.Event,
		Commander:     strings.TrimSpace(c.Commander),
		FID:           strings.TrimSpace(c.FID),
		SystemAddress: c.SystemAddress,
		BodyID:        c.BodyID,
		BodyName:      c.BodyName,
		SourceFile:    sourceFile,
		Raw:           string(line),
	}
	if c.Event == "Commander" && ev.Commander == "" {
		ev.Commander = strings.TrimSpace(c.Name)
	}
	switch {
	case c.StarSystem != "":
		ev.SystemName = c.StarSystem
	case c.SystemName != "":
		ev.SystemName = c.SystemName
	case c.Event == "FSDTarget":
		ev.SystemName = c.Name
	}
	if c.Event == "ScanOrganic" && ev.BodyID == nil && len(c.Body) > 0 {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(c.Body)), 10, 64); err == nil {
			ev.BodyID = &n
		}
	}
	if c.Event == "Location" && ev.BodyName == "" && len(c.Body) > 0 {
		var s string
		if json.Unmarshal(c.Body, &s) == nil {
			ev.BodyName = s
		}
	}
	if x, y, z, ok := Vec(c.StarPos); ok {
		ev.StarPos = &ed.Vec3{X: x, Y: y, Z: z}
	}
	ev.UID = ComputeUID(ts, c.Event, keyIdentifiers(&c, line)...)
	return ev, true, nil
}
