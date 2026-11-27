package reference

import "testing"

func TestStarValuation(testingContext *testing.T) {
	mainSequenceUnitMass := EstimateBodyValue(BodyScan{
		StarType:      "G",
		StellarMass:   1.0,
		WasDiscovered: true,
	}, MappingInfo{}, true)

	starCases := []struct {
		name           string
		bodyScan       BodyScan
		wantBase       int64
		wantFirstBonus bool
	}{
		{
			name:     "neutron star already discovered",
			bodyScan: BodyScan{StarType: "N", StellarMass: 1.4, WasDiscovered: true},
			wantBase: 23106,
		},
		{
			name:           "black hole first discovered",
			bodyScan:       BodyScan{StarType: "H", StellarMass: 5.0, WasDiscovered: false},
			wantBase:       24336,
			wantFirstBonus: true,
		},
		{
			name:     "supermassive black hole",
			bodyScan: BodyScan{StarType: "SupermassiveBlackHole", StellarMass: 4_000_000, WasDiscovered: true},
			wantBase: 2026769,
		},
		{
			name:     "white dwarf single letter class D",
			bodyScan: BodyScan{StarType: "D", StellarMass: 0.6, WasDiscovered: true},
			wantBase: 14184,
		},
		{
			name:     "white dwarf two letter class DA",
			bodyScan: BodyScan{StarType: "DA", StellarMass: 0.6, WasDiscovered: true},
			wantBase: 14184,
		},
		{
			name:           "white dwarf three letter class DCV first discovered",
			bodyScan:       BodyScan{StarType: "DCV", StellarMass: 0.6, WasDiscovered: false},
			wantBase:       14184,
			wantFirstBonus: true,
		},
		{
			name:     "non white dwarf prefix longer than three characters uses main sequence k",
			bodyScan: BodyScan{StarType: "Dwarf", StellarMass: 1.0, WasDiscovered: true},
			wantBase: mainSequenceUnitMass.Base,
		},
		{
			name:           "main sequence G star first discovered",
			bodyScan:       BodyScan{StarType: "G", StellarMass: 1.0, WasDiscovered: false},
			wantBase:       1218,
			wantFirstBonus: true,
		},
		{
			name:     "zero stellar mass defaults to one solar mass",
			bodyScan: BodyScan{StarType: "G", StellarMass: 0, WasDiscovered: true},
			wantBase: mainSequenceUnitMass.Base,
		},
		{
			name:     "negative stellar mass defaults to one solar mass",
			bodyScan: BodyScan{StarType: "G", StellarMass: -2.5, WasDiscovered: true},
			wantBase: mainSequenceUnitMass.Base,
		},
	}

	for _, testCase := range starCases {
		testingContext.Run(testCase.name, func(subContext *testing.T) {
			actualValue := EstimateBodyValue(testCase.bodyScan, MappingInfo{}, true)
			if actualValue.Base != testCase.wantBase {
				subContext.Fatalf("Base = %d, want %d (full=%+v)", actualValue.Base, testCase.wantBase, actualValue)
			}
			if testCase.wantFirstBonus {
				if actualValue.Bonus <= 0 || actualValue.Total != actualValue.Base+actualValue.Bonus {
					subContext.Fatalf("expected positive first-discovery bonus, got %+v", actualValue)
				}
			} else if actualValue.Bonus != 0 || actualValue.Total != actualValue.Base {
				subContext.Fatalf("expected zero bonus for discovered star, got %+v", actualValue)
			}
		})
	}
}

func TestPlanetBeltAndTerraformableBranches(testingContext *testing.T) {
	beltCluster := BodyScan{}
	efficientMapping := MappingInfo{Mapped: true, ProbesUsed: 4, EfficiencyTarget: 6}
	if beltValue := EstimateBodyValue(beltCluster, efficientMapping, true); beltValue != (ScanValue{}) {
		testingContext.Fatalf("expected zero value for belt cluster, got %+v", beltValue)
	}
	if beltIncrement := EstimateMappingIncrement(beltCluster, efficientMapping, true); beltIncrement != (ScanValue{}) {
		testingContext.Fatalf("expected zero mapping increment for belt cluster, got %+v", beltIncrement)
	}
	if beltBreakdown := EstimateMappingIncrementBreakdown(beltCluster, efficientMapping, true); beltBreakdown != (MappingIncrementBreakdown{}) {
		testingContext.Fatalf("expected zero mapping breakdown for belt cluster, got %+v", beltBreakdown)
	}

	starScan := BodyScan{StarType: "G", StellarMass: 1.0}
	if starIncrement := EstimateMappingIncrement(starScan, efficientMapping, true); starIncrement != (ScanValue{}) {
		testingContext.Fatalf("expected zero mapping increment for star, got %+v", starIncrement)
	}
	if starBreakdown := EstimateMappingIncrementBreakdown(starScan, efficientMapping, true); starBreakdown != (MappingIncrementBreakdown{}) {
		testingContext.Fatalf("expected zero mapping breakdown for star, got %+v", starBreakdown)
	}

	terraformStates := []struct {
		state string
		want  bool
	}{
		{state: "Terraformable", want: true},
		{state: "Terraforming", want: true},
		{state: "Terraformed", want: true},
		{state: "Not terraformable", want: false},
		{state: "", want: false},
	}
	for _, stateCase := range terraformStates {
		scan := BodyScan{PlanetClass: "Water world", TerraformState: stateCase.state}
		if got := scan.IsTerraformable(); got != stateCase.want {
			testingContext.Errorf("IsTerraformable(%q) = %v, want %v", stateCase.state, got, stateCase.want)
		}
	}

	planetCases := []struct {
		name        string
		bodyScan    BodyScan
		mappingInfo MappingInfo
		odyssey     bool
		check       func(subContext *testing.T, value ScanValue)
	}{
		{
			name:        "metal rich body first mapped on already discovered world in Horizons",
			bodyScan:    BodyScan{PlanetClass: "Metal rich body", MassEM: 1.0, WasDiscovered: true, WasMapped: false},
			mappingInfo: MappingInfo{Mapped: true, ProbesUsed: 6, EfficiencyTarget: 6},
			odyssey:     false,
			check: func(subContext *testing.T, value ScanValue) {
				if value.Base <= 0 || value.Bonus <= 0 || value.Total != value.Base+value.Bonus {
					subContext.Fatalf("expected first-mapped bonus on discovered metal-rich body, got %+v", value)
				}
			},
		},
		{
			name:        "ammonia world already discovered and already mapped",
			bodyScan:    BodyScan{PlanetClass: "Ammonia world", MassEM: 1.0, WasDiscovered: true, WasMapped: true},
			mappingInfo: MappingInfo{Mapped: true, ProbesUsed: 8, EfficiencyTarget: 6},
			odyssey:     true,
			check: func(subContext *testing.T, value ScanValue) {
				if value.Base <= 100_000 || value.Bonus != 0 || value.Total != value.Base {
					subContext.Fatalf("unexpected mapped ammonia world value: %+v", value)
				}
			},
		},
		{
			name:     "sudarsky class i gas giant zero mass fallback",
			bodyScan: BodyScan{PlanetClass: "Sudarsky class I gas giant", MassEM: 0, WasDiscovered: true, WasMapped: true},
			odyssey:  true,
			check: func(subContext *testing.T, value ScanValue) {
				unitMass := EstimateBodyValue(BodyScan{PlanetClass: "Sudarsky class I gas giant", MassEM: 1.0, WasDiscovered: true, WasMapped: true}, MappingInfo{}, true)
				if value != unitMass {
					subContext.Fatalf("zero MassEM = %+v, want unit MassEM %+v", value, unitMass)
				}
			},
		},
		{
			name:     "sudarsky class ii gas giant and terraformed water world",
			bodyScan: BodyScan{PlanetClass: "Water world", MassEM: 1.0, TerraformState: "Terraformed", WasDiscovered: true, WasMapped: true},
			odyssey:  true,
			check: func(subContext *testing.T, value ScanValue) {
				nonTerraformWW := EstimateBodyValue(BodyScan{PlanetClass: "Water world", MassEM: 1.0, WasDiscovered: true, WasMapped: true}, MappingInfo{}, true)
				classIIGiant := EstimateBodyValue(BodyScan{PlanetClass: "Sudarsky class II gas giant", MassEM: 10.0, WasDiscovered: true, WasMapped: true}, MappingInfo{}, true)
				if value.Total <= nonTerraformWW.Total || classIIGiant.Total <= 0 {
					subContext.Fatalf("expected terraformed WW (%d) > plain WW (%d) and positive Class II (%d)", value.Total, nonTerraformWW.Total, classIIGiant.Total)
				}
			},
		},
		{
			name:     "terraforming rocky ice body exceeds plain rocky ice body",
			bodyScan: BodyScan{PlanetClass: "Rocky ice body", MassEM: 0.5, TerraformState: "Terraforming", WasDiscovered: true, WasMapped: true},
			odyssey:  true,
			check: func(subContext *testing.T, value ScanValue) {
				plain := EstimateBodyValue(BodyScan{PlanetClass: "Rocky ice body", MassEM: 0.5, WasDiscovered: true, WasMapped: true}, MappingInfo{}, true)
				if value.Total <= plain.Total {
					subContext.Fatalf("expected terraforming rocky ice (%d) > plain (%d)", value.Total, plain.Total)
				}
			},
		},
		{
			name:     "tiny icy moon and class iii gas giant respect minimum body value floor",
			bodyScan: BodyScan{PlanetClass: "Icy body", MassEM: 0.0001, WasDiscovered: true, WasMapped: true},
			odyssey:  false,
			check: func(subContext *testing.T, value ScanValue) {
				classIIIGiant := EstimateBodyValue(BodyScan{PlanetClass: "Sudarsky class III gas giant", MassEM: 50.0, WasDiscovered: true, WasMapped: true}, MappingInfo{}, true)
				if value.Total != 500 || classIIIGiant.Total <= 500 {
					subContext.Fatalf("expected 500 Cr minimum floor (got %d) and class III > 500 (got %d)", value.Total, classIIIGiant.Total)
				}
			},
		},
	}

	for _, testCase := range planetCases {
		testingContext.Run(testCase.name, func(subContext *testing.T) {
			testCase.check(subContext, EstimateBodyValue(testCase.bodyScan, testCase.mappingInfo, testCase.odyssey))
		})
	}
}
