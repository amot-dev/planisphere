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
	StarType         string  // non-empty for stars
	PlanetClass      string  // non-empty for planets/moons
	MassEM           float64 // planets, earth masses
	StellarMass      float64 // stars, solar masses
	TerraformState   string
	SystemPopulation int64 // population of parent system; positive values suppress first bonuses
	WasDiscovered    bool
	WasMapped        bool
}

// MappingInfo describes the matching `SAAScanComplete` (if any).
type MappingInfo struct {
	Mapped           bool
	ProbesUsed       int
	EfficiencyTarget int
}

// Efficient reports whether the DSS mapping met the efficiency target.
func (mapping MappingInfo) Efficient() bool {
	return mapping.Mapped && mapping.EfficiencyTarget > 0 && mapping.ProbesUsed <= mapping.EfficiencyTarget
}

// ScanValue is a value split into the base payout and the first
// discovery / first mapped bonuses.
type ScanValue struct {
	Base  int64 `json:"base"`
	Bonus int64 `json:"bonus"`
	Total int64 `json:"total"`
}

// Add returns the component-wise sum.
func (value ScanValue) Add(other ScanValue) ScanValue {
	return ScanValue{Base: value.Base + other.Base, Bonus: value.Bonus + other.Bonus, Total: value.Total + other.Total}
}

// Sub returns the component-wise difference.
func (value ScanValue) Sub(other ScanValue) ScanValue {
	return ScanValue{Base: value.Base - other.Base, Bonus: value.Bonus - other.Bonus, Total: value.Total - other.Total}
}

const (
	firstDiscoveryMultiplier = 2.6
	planetMassQ              = 0.56591828
	minimumBodyValue         = 500.0
)

// IsStar reports whether the scan describes a star (or other stellar object).
func (bodyScan BodyScan) IsStar() bool { return bodyScan.StarType != "" }

// IsValuable reports whether the scan describes a star or planet. Belt
// clusters and rings produce Scan events with neither StarType nor PlanetClass.
func (bodyScan BodyScan) IsValuable() bool { return bodyScan.StarType != "" || bodyScan.PlanetClass != "" }

// IsTerraformable reports whether the body is (or was) a terraforming candidate.
func (bodyScan BodyScan) IsTerraformable() bool {
	switch strings.ToLower(bodyScan.TerraformState) {
	case "terraformable", "terraforming", "terraformed":
		return true
	}
	return false
}

func starK(starType string) float64 {
	switch {
	case starType == "N" || starType == "H":
		return 22628
	case strings.EqualFold(starType, "SupermassiveBlackHole"):
		return 33.5678
	case strings.HasPrefix(starType, "D") && len(starType) <= 3: // D, DA, DAB, DAO, DAZ, DAV, DB, DBZ, DBV, DO, DOV, DQ, DC, DCV, DX
		return 14057
	default:
		return 1200
	}
}

// planetK returns the base k value and the terraformable bonus for a planet class.
func planetK(planetClass string, terraformable bool) float64 {
	classLower := strings.ToLower(planetClass)
	baseK := 300.0
	terraformBonus := 93328.0
	switch classLower {
	case "metal rich body":
		baseK, terraformBonus = 21790, 0
	case "ammonia world":
		baseK, terraformBonus = 96932, 0
	case "sudarsky class i gas giant":
		baseK, terraformBonus = 1656, 0
	case "sudarsky class ii gas giant", "high metal content body":
		baseK, terraformBonus = 9654, 100677
	case "water world":
		baseK, terraformBonus = 64831, 116295
	case "earthlike body":
		// Earth-like worlds always receive the terraformable bonus.
		return 64831 + 116295
	case "rocky body", "icy body", "rocky ice body":
		baseK, terraformBonus = 300, 93328
	default:
		// Remaining gas giants (class III-V, helium, water giants, life-bearing).
		baseK, terraformBonus = 300, 0
	}
	if terraformable {
		baseK += terraformBonus
	}
	return baseK
}

func starValue(bodyScan BodyScan, firstDiscovery bool) float64 {
	starKVal := starK(bodyScan.StarType)
	mass := bodyScan.StellarMass
	if mass <= 0 {
		mass = 1
	}
	calculatedValue := starKVal + (mass * starKVal / 66.25)
	if firstDiscovery {
		calculatedValue *= firstDiscoveryMultiplier
	}
	return math.Round(calculatedValue)
}

func planetValue(bodyScan BodyScan, firstDiscovery, mapped, firstMapped, efficient, odyssey bool) float64 {
	planetKVal := planetK(bodyScan.PlanetClass, bodyScan.IsTerraformable())
	mass := bodyScan.MassEM
	if mass <= 0 {
		mass = 1
	}
	multiplier := 1.0
	if mapped {
		switch {
		case firstDiscovery && firstMapped:
			multiplier = 3.699622554
		case firstMapped:
			multiplier = 8.0956
		default:
			multiplier = 10.0 / 3.0
		}
	}
	calculatedValue := (planetKVal + planetKVal*planetMassQ*math.Pow(mass, 0.2)) * multiplier
	if mapped {
		if odyssey {
			calculatedValue += math.Max(calculatedValue*0.3, 555)
		}
		if efficient {
			calculatedValue *= 1.25
		}
	}
	calculatedValue = math.Max(minimumBodyValue, calculatedValue)
	if firstDiscovery {
		calculatedValue *= firstDiscoveryMultiplier
	}
	return math.Round(calculatedValue)
}

// EstimateBodyValue returns the estimated Universal Cartographics payout for a
// body, split into Base (as if someone else had already discovered/mapped it)
// and Bonus (first discovered / first mapped premium).
//
// First discovery and first mapped eligibility require an unpopulated parent
// system and a body that is not tag-locked, combined with `WasDiscovered == false`
// or `WasMapped == false` with a completed DSS mapping.
func EstimateBodyValue(bodyScan BodyScan, mappingInfo MappingInfo, odyssey bool) ScanValue {
	if !bodyScan.IsValuable() {
		return ScanValue{}
	}
	isFirstDiscovery := CanExpectFirstDiscovered(bodyScan.SystemPopulation, bodyScan.WasDiscovered, bodyScan.WasMapped)
	var base, total float64
	if bodyScan.IsStar() {
		base = starValue(bodyScan, false)
		total = starValue(bodyScan, isFirstDiscovery)
	} else {
		isFirstMapped := mappingInfo.Mapped && CanExpectFirstMapped(bodyScan.SystemPopulation, bodyScan.WasDiscovered, bodyScan.WasMapped)
		base = planetValue(bodyScan, false, mappingInfo.Mapped, false, mappingInfo.Efficient(), odyssey)
		total = planetValue(bodyScan, isFirstDiscovery, mappingInfo.Mapped, isFirstMapped, mappingInfo.Efficient(), odyssey)
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
func EstimateMappingIncrement(bodyScan BodyScan, mappingInfo MappingInfo, odyssey bool) ScanValue {
	if !mappingInfo.Mapped || bodyScan.IsStar() || !bodyScan.IsValuable() {
		return ScanValue{}
	}
	return EstimateBodyValue(bodyScan, mappingInfo, odyssey).Sub(EstimateBodyValue(bodyScan, MappingInfo{}, odyssey))
}
