package ed

import (
	"math"
	"strings"
)

// Universal Cartographics value estimation.
//
// The formulas follow the community-derived "Exploration value formulae"
// (MattG, Frontier forums, post-3.3) as implemented by EDDiscovery / EDMC,
// including the Odyssey mapping bonus. They are estimates: in-game payouts can
// differ by a handful of credits due to rounding.

// BodyScan is the subset of a journal `Scan` event needed for valuation.
type BodyScan struct {
	StarType       string  // non-empty for stars
	PlanetClass    string  // non-empty for planets/moons
	MassEM         float64 // planets, earth masses
	StellarMass    float64 // stars, solar masses
	TerraformState string
	WasDiscovered  bool
	WasMapped      bool
}

// MappingInfo describes the matching `SAAScanComplete` (if any).
type MappingInfo struct {
	Mapped           bool
	ProbesUsed       int
	EfficiencyTarget int
}

// Efficient reports whether the DSS mapping met the efficiency target.
func (m MappingInfo) Efficient() bool {
	return m.Mapped && m.EfficiencyTarget > 0 && m.ProbesUsed <= m.EfficiencyTarget
}

// ScanValue is a value split into the base payout and the first
// discovery / first mapped bonuses.
type ScanValue struct {
	Base  int64 `json:"base"`
	Bonus int64 `json:"bonus"`
	Total int64 `json:"total"`
}

// Add returns the component-wise sum.
func (v ScanValue) Add(o ScanValue) ScanValue {
	return ScanValue{Base: v.Base + o.Base, Bonus: v.Bonus + o.Bonus, Total: v.Total + o.Total}
}

// Sub returns the component-wise difference.
func (v ScanValue) Sub(o ScanValue) ScanValue {
	return ScanValue{Base: v.Base - o.Base, Bonus: v.Bonus - o.Bonus, Total: v.Total - o.Total}
}

const (
	firstDiscoveryMultiplier = 2.6
	planetMassQ              = 0.56591828
	minimumBodyValue         = 500.0
)

// IsStar reports whether the scan describes a star (or other stellar object).
func (b BodyScan) IsStar() bool { return b.StarType != "" }

// IsValuable reports whether the scan describes a star or planet. Belt
// clusters and rings produce Scan events with neither StarType nor PlanetClass.
func (b BodyScan) IsValuable() bool { return b.StarType != "" || b.PlanetClass != "" }

// IsTerraformable reports whether the body is (or was) a terraforming candidate.
func (b BodyScan) IsTerraformable() bool {
	switch strings.ToLower(b.TerraformState) {
	case "terraformable", "terraforming", "terraformed":
		return true
	}
	return false
}

func starK(starType string) float64 {
	st := starType
	switch {
	case st == "N" || st == "H":
		return 22628
	case strings.EqualFold(st, "SupermassiveBlackHole"):
		return 33.5678
	case strings.HasPrefix(st, "D") && len(st) <= 3: // D, DA, DAB, DAO, DAZ, DAV, DB, DBZ, DBV, DO, DOV, DQ, DC, DCV, DX
		return 14057
	default:
		return 1200
	}
}

// planetK returns the base k value and the terraformable bonus for a planet class.
func planetK(planetClass string, terraformable bool) float64 {
	pc := strings.ToLower(planetClass)
	k := 300.0
	tb := 93328.0
	switch pc {
	case "metal rich body":
		k, tb = 21790, 0
	case "ammonia world":
		k, tb = 96932, 0
	case "sudarsky class i gas giant":
		k, tb = 1656, 0
	case "sudarsky class ii gas giant", "high metal content body":
		k, tb = 9654, 100677
	case "water world":
		k, tb = 64831, 116295
	case "earthlike body":
		// Earth-like worlds always receive the terraformable bonus.
		return 64831 + 116295
	case "rocky body", "icy body", "rocky ice body":
		k, tb = 300, 93328
	default:
		// Remaining gas giants (class III-V, helium, water giants, life-bearing).
		k, tb = 300, 0
	}
	if terraformable {
		k += tb
	}
	return k
}

func starValue(b BodyScan, firstDiscovery bool) float64 {
	k := starK(b.StarType)
	mass := b.StellarMass
	if mass <= 0 {
		mass = 1
	}
	v := k + (mass * k / 66.25)
	if firstDiscovery {
		v *= firstDiscoveryMultiplier
	}
	return math.Round(v)
}

func planetValue(b BodyScan, firstDiscovery, mapped, firstMapped, efficient, odyssey bool) float64 {
	k := planetK(b.PlanetClass, b.IsTerraformable())
	mass := b.MassEM
	if mass <= 0 {
		mass = 1
	}
	mult := 1.0
	if mapped {
		switch {
		case firstDiscovery && firstMapped:
			mult = 3.699622554
		case firstMapped:
			mult = 8.0956
		default:
			mult = 10.0 / 3.0
		}
	}
	v := (k + k*planetMassQ*math.Pow(mass, 0.2)) * mult
	if mapped {
		if odyssey {
			v += math.Max(v*0.3, 555)
		}
		if efficient {
			v *= 1.25
		}
	}
	v = math.Max(minimumBodyValue, v)
	if firstDiscovery {
		v *= firstDiscoveryMultiplier
	}
	return math.Round(v)
}

// EstimateBodyValue returns the estimated Universal Cartographics payout for a
// body, split into Base (as if someone else had already discovered/mapped it)
// and Bonus (first discovered / first mapped premium).
//
// First discovery is `WasDiscovered == false`; first mapped is
// `WasMapped == false` combined with a completed DSS mapping.
func EstimateBodyValue(b BodyScan, m MappingInfo, odyssey bool) ScanValue {
	if !b.IsValuable() {
		return ScanValue{}
	}
	firstDisc := !b.WasDiscovered
	var base, total float64
	if b.IsStar() {
		base = starValue(b, false)
		total = starValue(b, firstDisc)
	} else {
		firstMapped := m.Mapped && !b.WasMapped
		base = planetValue(b, false, m.Mapped, false, m.Efficient(), odyssey)
		total = planetValue(b, firstDisc, m.Mapped, firstMapped, m.Efficient(), odyssey)
	}
	if total < base {
		total = base
	}
	return ScanValue{Base: int64(base), Bonus: int64(total - base), Total: int64(total)}
}

// EstimateMappingIncrement returns the extra value gained by DSS mapping a
// body whose FSS scan has already been valued, i.e. value(mapped) - value(unmapped).
// This lets scan value and mapping value be attributed to different time ranges
// (legs) while always summing to the correct total.
func EstimateMappingIncrement(b BodyScan, m MappingInfo, odyssey bool) ScanValue {
	if !m.Mapped || b.IsStar() || !b.IsValuable() {
		return ScanValue{}
	}
	return EstimateBodyValue(b, m, odyssey).Sub(EstimateBodyValue(b, MappingInfo{}, odyssey))
}
