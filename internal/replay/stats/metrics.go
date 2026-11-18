// Package stats computes expedition and lifetime telemetry metrics from persisted
// journal events. Everything is derived from journal_events.raw_json, so metrics
// can be recomputed at any time (with updated formulas) even after original
// journal files are gone.
package stats

import (
	"errors"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay/realization"
)

// RealizationBreakdown re-exports realization.Breakdown for existing callers
// and Wails TypeScript bindings.
type RealizationBreakdown = realization.Breakdown

// Metrics are the tracked expedition statistics.
type Metrics struct {
	DistanceLy         float64 `json:"distanceLy"`
	ShipDistanceLy     float64 `json:"shipDistanceLy"`     // subset: hyperspace FSD jumps
	CarrierPassengerLy float64 `json:"carrierPassengerLy"` // subset: travelled while aboard a carrier
	Jumps              int     `json:"jumps"`
	CarrierJumps       int     `json:"carrierJumps"`
	CarrierDistanceLy  float64 `json:"carrierDistanceLy"`
	TritiumUsed        *int    `json:"tritiumUsed"` // nil when N/A (not recorded or estimated)
	JetConeBoosts      int     `json:"jetConeBoosts"`
	PremiumBoosts      int     `json:"premiumBoosts"`
	StandardBoosts     int     `json:"standardBoosts"`
	BasicBoosts        int     `json:"basicBoosts"`
	BodiesScanned      int     `json:"bodiesScanned"`
	BodiesMapped       int     `json:"bodiesMapped"`
	ScanValueTotal     int64   `json:"scanValueTotal"`
	ScanValueBase      int64   `json:"scanValueBase"`
	ScanValueBonus     int64   `json:"scanValueBonus"`
	FirstDiscoveries   int     `json:"firstDiscoveries"`
	FirstMapped        int     `json:"firstMapped"`
	OrganicSamples     int     `json:"organicSamples"`
	OrganicValueTotal  int64   `json:"organicValueTotal"`
	OrganicValueBase   int64   `json:"organicValueBase"`
	OrganicValueBonus  int64   `json:"organicValueBonus"`
	FirstLogged        int     `json:"firstLogged"`
}

// Add returns the field-wise sum of metrics and otherMetrics.
func (metrics Metrics) Add(otherMetrics Metrics) Metrics {
	var tritiumTotal *int
	if metrics.TritiumUsed != nil || otherMetrics.TritiumUsed != nil {
		tritiumSum := 0
		if metrics.TritiumUsed != nil {
			tritiumSum += *metrics.TritiumUsed
		}
		if otherMetrics.TritiumUsed != nil {
			tritiumSum += *otherMetrics.TritiumUsed
		}
		tritiumTotal = &tritiumSum
	}
	return Metrics{
		DistanceLy:         metrics.DistanceLy + otherMetrics.DistanceLy,
		ShipDistanceLy:     metrics.ShipDistanceLy + otherMetrics.ShipDistanceLy,
		CarrierPassengerLy: metrics.CarrierPassengerLy + otherMetrics.CarrierPassengerLy,
		Jumps:              metrics.Jumps + otherMetrics.Jumps,
		CarrierJumps:       metrics.CarrierJumps + otherMetrics.CarrierJumps,
		CarrierDistanceLy:  metrics.CarrierDistanceLy + otherMetrics.CarrierDistanceLy,
		TritiumUsed:        tritiumTotal,
		JetConeBoosts:      metrics.JetConeBoosts + otherMetrics.JetConeBoosts,
		PremiumBoosts:      metrics.PremiumBoosts + otherMetrics.PremiumBoosts,
		StandardBoosts:     metrics.StandardBoosts + otherMetrics.StandardBoosts,
		BasicBoosts:        metrics.BasicBoosts + otherMetrics.BasicBoosts,
		BodiesScanned:      metrics.BodiesScanned + otherMetrics.BodiesScanned,
		BodiesMapped:       metrics.BodiesMapped + otherMetrics.BodiesMapped,
		ScanValueTotal:     metrics.ScanValueTotal + otherMetrics.ScanValueTotal,
		ScanValueBase:      metrics.ScanValueBase + otherMetrics.ScanValueBase,
		ScanValueBonus:     metrics.ScanValueBonus + otherMetrics.ScanValueBonus,
		FirstDiscoveries:   metrics.FirstDiscoveries + otherMetrics.FirstDiscoveries,
		FirstMapped:        metrics.FirstMapped + otherMetrics.FirstMapped,
		OrganicSamples:     metrics.OrganicSamples + otherMetrics.OrganicSamples,
		OrganicValueTotal:  metrics.OrganicValueTotal + otherMetrics.OrganicValueTotal,
		OrganicValueBase:   metrics.OrganicValueBase + otherMetrics.OrganicValueBase,
		OrganicValueBonus:  metrics.OrganicValueBonus + otherMetrics.OrganicValueBonus,
		FirstLogged:        metrics.FirstLogged + otherMetrics.FirstLogged,
	}
}

func (metrics *Metrics) addScan(scanValuation reference.ScanValue) {
	metrics.ScanValueTotal += scanValuation.Total
	metrics.ScanValueBase += scanValuation.Base
	metrics.ScanValueBonus += scanValuation.Bonus
}

func (metrics *Metrics) addOrganic(organicValuation reference.ScanValue) {
	metrics.OrganicValueTotal += organicValuation.Total
	metrics.OrganicValueBase += organicValuation.Base
	metrics.OrganicValueBonus += organicValuation.Bonus
}

// TravelMetrics holds Flight Operations navigation and jump metrics.
type TravelMetrics struct {
	DistanceLy         float64 `json:"distanceLy"`
	ShipDistanceLy     float64 `json:"shipDistanceLy"`
	CarrierPassengerLy float64 `json:"carrierPassengerLy"`
	Jumps              int     `json:"jumps"`
	JetConeBoosts      int     `json:"jetConeBoosts"`
	PremiumBoosts      int     `json:"premiumBoosts"`
	StandardBoosts     int     `json:"standardBoosts"`
	BasicBoosts        int     `json:"basicBoosts"`
}

// ExtractTravelMetrics copies Flight Operations navigation fields from metrics.
func ExtractTravelMetrics(metrics Metrics) TravelMetrics {
	return TravelMetrics{
		DistanceLy:         metrics.DistanceLy,
		ShipDistanceLy:     metrics.ShipDistanceLy,
		CarrierPassengerLy: metrics.CarrierPassengerLy,
		Jumps:              metrics.Jumps,
		JetConeBoosts:      metrics.JetConeBoosts,
		PremiumBoosts:      metrics.PremiumBoosts,
		StandardBoosts:     metrics.StandardBoosts,
		BasicBoosts:        metrics.BasicBoosts,
	}
}

// ApplyTo writes Flight Operations navigation fields onto targetMetrics.
func (travel TravelMetrics) ApplyTo(targetMetrics *Metrics) {
	targetMetrics.DistanceLy = travel.DistanceLy
	targetMetrics.ShipDistanceLy = travel.ShipDistanceLy
	targetMetrics.CarrierPassengerLy = travel.CarrierPassengerLy
	targetMetrics.Jumps = travel.Jumps
	targetMetrics.JetConeBoosts = travel.JetConeBoosts
	targetMetrics.PremiumBoosts = travel.PremiumBoosts
	targetMetrics.StandardBoosts = travel.StandardBoosts
	targetMetrics.BasicBoosts = travel.BasicBoosts
}

// CarrierOpsMetrics holds lifetime Personal Carrier Operations jump and distance totals.
type CarrierOpsMetrics struct {
	CarrierJumps      int     `json:"carrierJumps"`
	CarrierDistanceLy float64 `json:"carrierDistanceLy"`
}

// ApplyTo writes Personal Carrier Operations fields onto targetMetrics.
func (carrierOps CarrierOpsMetrics) ApplyTo(targetMetrics *Metrics) {
	targetMetrics.CarrierJumps = carrierOps.CarrierJumps
	targetMetrics.CarrierDistanceLy = carrierOps.CarrierDistanceLy
}

// CartographicsMetrics holds Universal Cartographics body scan, DSS mapping,
// and estimated payout metrics.
type CartographicsMetrics struct {
	BodiesScanned    int   `json:"bodiesScanned"`
	BodiesMapped     int   `json:"bodiesMapped"`
	ScanValueTotal   int64 `json:"scanValueTotal"`
	ScanValueBase    int64 `json:"scanValueBase"`
	ScanValueBonus   int64 `json:"scanValueBonus"`
	FirstDiscoveries int   `json:"firstDiscoveries"`
	FirstMapped      int   `json:"firstMapped"`
}

// ExtractCartographicsMetrics copies Universal Cartographics fields from metrics.
func ExtractCartographicsMetrics(metrics Metrics) CartographicsMetrics {
	return CartographicsMetrics{
		BodiesScanned:    metrics.BodiesScanned,
		BodiesMapped:     metrics.BodiesMapped,
		ScanValueTotal:   metrics.ScanValueTotal,
		ScanValueBase:    metrics.ScanValueBase,
		ScanValueBonus:   metrics.ScanValueBonus,
		FirstDiscoveries: metrics.FirstDiscoveries,
		FirstMapped:      metrics.FirstMapped,
	}
}

// ApplyTo writes Universal Cartographics fields onto targetMetrics.
func (cartographics CartographicsMetrics) ApplyTo(targetMetrics *Metrics) {
	targetMetrics.BodiesScanned = cartographics.BodiesScanned
	targetMetrics.BodiesMapped = cartographics.BodiesMapped
	targetMetrics.ScanValueTotal = cartographics.ScanValueTotal
	targetMetrics.ScanValueBase = cartographics.ScanValueBase
	targetMetrics.ScanValueBonus = cartographics.ScanValueBonus
	targetMetrics.FirstDiscoveries = cartographics.FirstDiscoveries
	targetMetrics.FirstMapped = cartographics.FirstMapped
}

// ExobiologyMetrics holds Vista Genomics organic sample and valuation metrics.
type ExobiologyMetrics struct {
	OrganicSamples    int   `json:"organicSamples"`
	OrganicValueTotal int64 `json:"organicValueTotal"`
	OrganicValueBase  int64 `json:"organicValueBase"`
	OrganicValueBonus int64 `json:"organicValueBonus"`
	FirstLogged       int   `json:"firstLogged"`
}

// ExtractExobiologyMetrics copies Vista Genomics exobiology fields from metrics.
func ExtractExobiologyMetrics(metrics Metrics) ExobiologyMetrics {
	return ExobiologyMetrics{
		OrganicSamples:    metrics.OrganicSamples,
		OrganicValueTotal: metrics.OrganicValueTotal,
		OrganicValueBase:  metrics.OrganicValueBase,
		OrganicValueBonus: metrics.OrganicValueBonus,
		FirstLogged:       metrics.FirstLogged,
	}
}

// ApplyTo writes Vista Genomics exobiology fields onto targetMetrics.
func (exobiology ExobiologyMetrics) ApplyTo(targetMetrics *Metrics) {
	targetMetrics.OrganicSamples = exobiology.OrganicSamples
	targetMetrics.OrganicValueTotal = exobiology.OrganicValueTotal
	targetMetrics.OrganicValueBase = exobiology.OrganicValueBase
	targetMetrics.OrganicValueBonus = exobiology.OrganicValueBonus
	targetMetrics.FirstLogged = exobiology.FirstLogged
}

// TelemetrySummary pairs computed navigation/discovery metrics with their
// realized payout and deduction breakdown.
type TelemetrySummary struct {
	Metrics     Metrics
	Realization RealizationBreakdown
}

// Range is the half-open time range (After, Until]. Empty After means "from
// the beginning of time"; empty Until means "up to now".
type Range struct {
	After string `json:"after"`
	Until string `json:"until"`
}

func (timeRange Range) bounds() (string, string) {
	until := timeRange.Until
	if until == "" {
		until = "9999-12-31T23:59:59Z"
	}
	return timeRange.After, until
}

// ModeSelection controls which modes contribute to which metric family.
type ModeSelection struct {
	ModeID    int64 `json:"modeId"`    // Unassigned (0) = time not covered by a window
	Travel    bool  `json:"travel"`    // jumps, distance, boosts, carrier passenger travel
	Discovery bool  `json:"discovery"` // scans, mapping, values, exobiology
}

// ModeLookup resolves the active Flight Mode ID at a given ISO-8601 timestamp.
type ModeLookup = func(timestamp string) int64

// ModeTimelineLoader loads a ModeLookup closure for querying mode filters against queryer.
type ModeTimelineLoader = func(queryer db.Queryer) (ModeLookup, error)

// Filter restricts metrics to events that occurred while selected modes were
// active. A nil Filter (or Enabled == false) includes everything.
type Filter struct {
	Enabled      bool            `json:"enabled"`
	Modes        []ModeSelection `json:"modes"`
	preparedSets *filterSets
	preparedDB   db.Queryer
}

type filterSets struct {
	travel, discovery map[int64]bool
	modeLookup        ModeLookup
}

// CloneUnprepared returns a shallow copy of filter with cached preparation state cleared.
func CloneUnprepared(filter *Filter) *Filter {
	if filter == nil {
		return nil
	}
	copied := *filter
	copied.preparedSets = nil
	copied.preparedDB = nil
	return &copied
}

func (filter *Filter) prepare(queryer db.Queryer, modeLoader ModeTimelineLoader) (*filterSets, error) {
	if filter == nil || !filter.Enabled {
		return nil, nil
	}
	if filter.preparedSets != nil && filter.preparedDB == queryer {
		return filter.preparedSets, nil
	}
	if modeLoader == nil {
		return nil, errors.New("mode timeline loader is required when mode filter is enabled")
	}
	lookup, loadError := modeLoader(queryer)
	if loadError != nil {
		return nil, loadError
	}
	sets := &filterSets{travel: map[int64]bool{}, discovery: map[int64]bool{}, modeLookup: lookup}
	for _, modeSelection := range filter.Modes {
		if modeSelection.Travel {
			sets.travel[modeSelection.ModeID] = true
		}
		if modeSelection.Discovery {
			sets.discovery[modeSelection.ModeID] = true
		}
	}
	filter.preparedSets = sets
	filter.preparedDB = queryer
	return sets, nil
}

func (sets *filterSets) allow(timestamp string, discovery bool) bool {
	if sets == nil || sets.modeLookup == nil {
		return true
	}
	mode := sets.modeLookup(timestamp)
	if discovery {
		return sets.discovery[mode]
	}
	return sets.travel[mode]
}
