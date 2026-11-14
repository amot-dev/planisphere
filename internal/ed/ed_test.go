package ed

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestGalacticTimeConversion(t *testing.T) {
	isoTimestamp := "2026-01-18T19:42:10Z"
	galacticTime := ToGalactic(isoTimestamp)
	expectedGalacticTime := "3312-01-18 19:42 UTC"
	if galacticTime != expectedGalacticTime {
		t.Fatalf("ToGalactic(%q) = %q, want %q", isoTimestamp, galacticTime, expectedGalacticTime)
	}
	convertedBack, conversionError := FromGalactic(galacticTime)
	if conversionError != nil {
		t.Fatalf("FromGalactic(%q) error: %v", galacticTime, conversionError)
	}
	expectedRoundedISO := "2026-01-18T19:42:00Z"
	if convertedBack != expectedRoundedISO {
		t.Fatalf("FromGalactic(%q) = %q, want %q", galacticTime, convertedBack, expectedRoundedISO)
	}

	roundedUpGalactic := ToGalactic("2026-01-18T19:42:45Z")
	if roundedUpGalactic != "3312-01-18 19:43 UTC" {
		t.Fatalf("ToGalactic round up = %q, want 3312-01-18 19:43 UTC", roundedUpGalactic)
	}

	// Leap day preservation: 2028 is a leap year; 2028+1286 = 3314 is not in Gregorian,
	// yet the galactic string must preserve 02-29.
	leapISOTimestamp := "2028-02-29T08:15:00Z"
	leapGalacticTime := ToGalactic(leapISOTimestamp)
	if leapGalacticTime != "3314-02-29 08:15 UTC" {
		t.Fatalf("ToGalactic leap day = %q", leapGalacticTime)
	}
	leapConvertedBack, leapError := FromGalactic(leapGalacticTime)
	if leapError != nil || leapConvertedBack != leapISOTimestamp {
		t.Fatalf("FromGalactic leap day = %q, %v", leapConvertedBack, leapError)
	}
}

func TestCartographicsValuation(t *testing.T) {
	// Earth-like world, 1.0 EM, first discovered and first mapped efficiently in Odyssey.
	elwScan := BodyScan{
		PlanetClass:   "Earthlike body",
		MassEM:        1.0,
		WasDiscovered: false,
		WasMapped:     false,
	}
	unmapped := EstimateBodyValue(elwScan, MappingInfo{}, true)
	if unmapped.Base <= 0 || unmapped.Bonus <= 0 || unmapped.Total != unmapped.Base+unmapped.Bonus {
		t.Fatalf("unexpected unmapped ELW value: %+v", unmapped)
	}

	mapped := EstimateBodyValue(elwScan, MappingInfo{Mapped: true, ProbesUsed: 5, EfficiencyTarget: 6}, true)
	mappingIncrement := EstimateMappingIncrement(elwScan, MappingInfo{Mapped: true, ProbesUsed: 5, EfficiencyTarget: 6}, true)
	if unmapped.Add(mappingIncrement) != mapped {
		t.Fatalf("scan + mapping increment != total mapped value: %+v + %+v != %+v", unmapped, mappingIncrement, mapped)
	}
	if mapped.Total < 3_000_000 {
		t.Fatalf("expected mapped first-discovered ELW to exceed 3M Cr, got %d", mapped.Total)
	}
}

func TestExobiologyValuation(t *testing.T) {
	baseValue := SpeciesBaseValue("$Codex_Ent_Stratum_07_Name;", "Stratum Tectonicas", "$Codex_Ent_Stratum_Genus_Name;")
	if baseValue != 19010800 {
		t.Fatalf("Stratum Tectonicas base = %d, want 19010800", baseValue)
	}
	calculatedValue := OrganicValue(baseValue, true)
	if calculatedValue.Base != 19010800 || calculatedValue.Bonus != 19010800*4 || calculatedValue.Total != 19010800*5 {
		t.Fatalf("OrganicValue with 5x bonus = %+v", calculatedValue)
	}

	radicoidaBase := SpeciesBaseValue("$Codex_Ent_Ingensradices_Unicus_Name;", "Radicoida Unica", "$Codex_Ent_Ingensradices_Genus_Name;")
	if radicoidaBase != 119037 {
		t.Fatalf("Radicoida Unica base = %d, want 119037", radicoidaBase)
	}
}

func TestBonusEligibilityPredicates(t *testing.T) {
	boolTrue := true
	boolFalse := false

	eligibilityCases := []struct {
		name               string
		systemPopulation   int64
		wasDiscovered      bool
		wasMapped          bool
		wasFootfalled      *bool
		wantFirstDisc      bool
		wantFirstMap       bool
		wantFirstLogged    bool
	}{
		{
			name:             "unpopulated untouched modern body",
			systemPopulation: 0,
			wasDiscovered:    false,
			wasMapped:        false,
			wasFootfalled:    &boolFalse,
			wantFirstDisc:    true,
			wantFirstMap:     true,
			wantFirstLogged:  true,
		},
		{
			name:             "unpopulated discovered unmapped unfootfalled body",
			systemPopulation: 0,
			wasDiscovered:    true,
			wasMapped:        false,
			wasFootfalled:    &boolFalse,
			wantFirstDisc:    false,
			wantFirstMap:     true,
			wantFirstLogged:  true,
		},
		{
			name:             "populated system suppresses all first bonuses",
			systemPopulation: 125000,
			wasDiscovered:    false,
			wasMapped:        false,
			wasFootfalled:    &boolFalse,
			wantFirstDisc:    false,
			wantFirstMap:     false,
			wantFirstLogged:  false,
		},
		{
			name:             "tag-locked bubble body suppresses all first bonuses",
			systemPopulation: 0,
			wasDiscovered:    false,
			wasMapped:        true,
			wasFootfalled:    &boolFalse,
			wantFirstDisc:    false,
			wantFirstMap:     false,
			wantFirstLogged:  false,
		},
		{
			name:             "pre-2025 nil footfall on untouched body expects bonus",
			systemPopulation: 0,
			wasDiscovered:    false,
			wasMapped:        false,
			wasFootfalled:    nil,
			wantFirstDisc:    true,
			wantFirstMap:     true,
			wantFirstLogged:  true,
		},
		{
			name:             "pre-2025 nil footfall on already discovered body suppresses organic bonus",
			systemPopulation: 0,
			wasDiscovered:    true,
			wasMapped:        false,
			wasFootfalled:    nil,
			wantFirstDisc:    false,
			wantFirstMap:     true,
			wantFirstLogged:  false,
		},
		{
			name:             "already footfalled body suppresses scan-time organic bonus",
			systemPopulation: 0,
			wasDiscovered:    true,
			wasMapped:        true,
			wasFootfalled:    &boolTrue,
			wantFirstDisc:    false,
			wantFirstMap:     false,
			wantFirstLogged:  false,
		},
	}

	for _, testCase := range eligibilityCases {
		if actualDisc := CanExpectFirstDiscovered(testCase.systemPopulation, testCase.wasDiscovered, testCase.wasMapped); actualDisc != testCase.wantFirstDisc {
			t.Errorf("%s: CanExpectFirstDiscovered = %v, want %v", testCase.name, actualDisc, testCase.wantFirstDisc)
		}
		if actualMap := CanExpectFirstMapped(testCase.systemPopulation, testCase.wasDiscovered, testCase.wasMapped); actualMap != testCase.wantFirstMap {
			t.Errorf("%s: CanExpectFirstMapped = %v, want %v", testCase.name, actualMap, testCase.wantFirstMap)
		}
		if actualLogged := CanExpectFirstLogged(testCase.systemPopulation, testCase.wasDiscovered, testCase.wasMapped, testCase.wasFootfalled); actualLogged != testCase.wantFirstLogged {
			t.Errorf("%s: CanExpectFirstLogged = %v, want %v", testCase.name, actualLogged, testCase.wantFirstLogged)
		}
	}

	populatedScan := BodyScan{
		PlanetClass:      "Earthlike body",
		MassEM:           1.0,
		SystemPopulation: 5000000,
		WasDiscovered:    false,
		WasMapped:        false,
	}
	populatedMappedValue := EstimateBodyValue(populatedScan, MappingInfo{Mapped: true, ProbesUsed: 5, EfficiencyTarget: 6}, true)
	if populatedMappedValue.Bonus != 0 || populatedMappedValue.Total != populatedMappedValue.Base {
		t.Fatalf("expected zero first-discovery bonus in populated system, got %+v", populatedMappedValue)
	}
}

func TestCarrierTritiumCost(t *testing.T) {
	// 500 Ly jump at empty load (used=0, fuel=1000): round(5 + 500*(26000)/200000) = round(5 + 65) = 70
	calculatedCost := CarrierTritiumCost(500, 0, 1000)
	if calculatedCost != 70 {
		t.Fatalf("CarrierTritiumCost(500, 0, 1000) = %d, want 70", calculatedCost)
	}

	// In-game verified case: NGC 7789 Sector MH-W b31-0 -> Slegoae NS-U d2-2 is 495.27ly,
	// with capacity 8,548 tonnes and 818 tonnes depot fuel.
	// Mass = 25000 + 8548 + 818 = 34366
	// 5 + 495.27 * 34366 / 200000 = 90.102... -> round = 90 tonnes (Elite Dangerous in-game cost)
	inGameCost := CarrierTritiumCost(495.27, 8548, 818)
	if inGameCost != 90 {
		t.Fatalf("CarrierTritiumCost(495.27, 8548, 818) = %d, want 90", inGameCost)
	}

	// 494.89 Ly jump with 8,548 tonnes used capacity and 1,000-ton depot:
	// Mass = 25000 + 8548 + 1000 = 34548
	// 5 + 494.89 * 34548 / 200000 = 90.487... -> round = 90 tonnes (overriding Spansh's 89t truncated estimate)
	gameCost := CarrierTritiumCost(494.89, 8548, 1000)
	if gameCost != 90 {
		t.Fatalf("CarrierTritiumCost(494.89, 8548, 1000) = %d, want 90", gameCost)
	}

	if math.Abs(Distance(Vec3{0, 0, 0}, Vec3{3, 4, 12})-13) > 1e-9 {
		t.Fatalf("Distance failed")
	}
}

func TestCarrierProratedUpkeep(t *testing.T) {
	nowUTC := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	if got := CarrierProratedUpkeep("2026-01-01T00:00:00Z", "2026-01-08T00:00:00Z", 0, nowUTC); got != 0 {
		t.Fatalf("expected 0 upkeep when weeklyUpkeep is 0, got %d", got)
	}
	if got := CarrierProratedUpkeep("", "2026-01-08T00:00:00Z", 14_000_000, nowUTC); got != 0 {
		t.Fatalf("expected 0 upkeep when startTimestamp is empty, got %d", got)
	}
	if got := CarrierProratedUpkeep("2026-01-01T00:00:00Z", "2026-01-08T00:00:00Z", 14_000_000, nowUTC); got != 14_000_000 {
		t.Fatalf("expected 14000000 for 7-day completed expedition, got %d", got)
	}
	if got := CarrierProratedUpkeep("2026-01-01T00:00:00Z", "", 14_000_000, nowUTC); got != 28_000_000 {
		t.Fatalf("expected 28000000 for 14-day active expedition at nowUTC, got %d", got)
	}
	if got := CarrierProratedUpkeep("2026-01-01T00:00:00Z", "2026-01-11T00:00:00Z", 7_000_000, nowUTC); got != 10_000_000 {
		t.Fatalf("expected 10000000 for 10-day expedition at 7M/wk, got %d", got)
	}
}

func TestRegionsAndAdjacency(t *testing.T) {
	if len(Regions) != 42 {
		t.Fatalf("len(Regions) = %d, want 42", len(Regions))
	}
	// Sol (0,0,0) is in Region 18: Inner Orion Spur.
	regionItem, ok := RegionAt(0, 0, 0)
	if !ok || regionItem.ID != InnerOrionSpurID || regionItem.Name != "Inner Orion Spur" {
		t.Fatalf("RegionAt(0,0,0) = %+v, %v", regionItem, ok)
	}
	// Sagittarius A* (~25.2, -20.9, 25899.9) is in Region 1: Galactic Centre.
	coreRegion, ok := RegionAt(25.2, -20.9, 25899.9)
	if !ok || coreRegion.ID != 1 {
		t.Fatalf("RegionAt(Sag A*) = %+v, %v", coreRegion, ok)
	}

	// Default prioritization (anchor 0 -> Region 18) puts Region 18's neighbours first.
	regionOptions := PrioritizedRegions(0)
	if len(regionOptions) != 42 {
		t.Fatalf("len(PrioritizedRegions) = %d", len(regionOptions))
	}
	expectedAdjacentIDs := RegionAdjacency[InnerOrionSpurID]
	for i := 0; i < len(expectedAdjacentIDs); i++ {
		if !regionOptions[i].Adjacent {
			t.Fatalf("expected option %d (%+v) to be marked adjacent", i, regionOptions[i])
		}
	}
	if !regionOptions[len(expectedAdjacentIDs)].Anchor || regionOptions[len(expectedAdjacentIDs)].ID != InnerOrionSpurID {
		t.Fatalf("expected anchor after adjacent options, got %+v", regionOptions[len(expectedAdjacentIDs)])
	}
}

func TestAdjacencyMatchesGrid(t *testing.T) {
	var data struct {
		RegionMap [][][2]int `json:"regionmap"`
	}
	if unmarshalError := json.Unmarshal(regionMapJSON, &data); unmarshalError != nil {
		t.Fatal(unmarshalError)
	}
	rows := make([][]int, len(data.RegionMap))
	width := 0
	for gridZ, rowPairs := range data.RegionMap {
		var row []int
		for _, lengthPair := range rowPairs {
			for pixelIndex := 0; pixelIndex < lengthPair[0]; pixelIndex++ {
				row = append(row, lengthPair[1])
			}
		}
		if len(row) > width {
			width = len(row)
		}
		rows[gridZ] = row
	}
	counts := map[[2]int]int{}
	for gridZ := 0; gridZ < len(rows); gridZ++ {
		for gridX := 0; gridX < len(rows[gridZ]); gridX++ {
			currentRegion := rows[gridZ][gridX]
			if currentRegion == 0 {
				continue
			}
			for _, delta := range [][2]int{{0, 1}, {1, 0}} {
				neighborZ, neighborX := gridZ+delta[0], gridX+delta[1]
				if neighborZ < len(rows) && neighborX < len(rows[neighborZ]) {
					neighborRegion := rows[neighborZ][neighborX]
					if neighborRegion != 0 && neighborRegion != currentRegion {
						regionPair := [2]int{currentRegion, neighborRegion}
						if regionPair[0] > regionPair[1] {
							regionPair[0], regionPair[1] = regionPair[1], regionPair[0]
						}
						counts[regionPair]++
					}
				}
			}
		}
	}
	derived := map[int][]int{}
	for regionPair, pairCount := range counts {
		if pairCount < 5 { // ignore single-pixel diagonal corner contact (3,13)
			continue
		}
		derived[regionPair[0]] = append(derived[regionPair[0]], regionPair[1])
		derived[regionPair[1]] = append(derived[regionPair[1]], regionPair[0])
	}
	for id := 1; id <= 42; id++ {
		sort.Ints(derived[id])
		if !reflect.DeepEqual(derived[id], RegionAdjacency[id]) {
			t.Fatalf("region %d adjacency mismatch: got %v, want %v", id, RegionAdjacency[id], derived[id])
		}
	}
}

func TestDurationDaysLabel(t *testing.T) {
	tests := []struct {
		hours float64
		want  string
	}{
		{0, "0d"},
		{6, "0d"},
		{12, "1d"},
		{23, "1d"},
		{36, "2d"},
		{24 * 45, "45d"},
		{24 * 365, "1y"},
		{24 * (365 + 15), "1y 15d"},
		{24 * (365 * 3 + 120), "3y 120d"},
		{24 * (365 * 2), "2y"},
	}
	for _, testCase := range tests {
		testDuration := time.Duration(testCase.hours * float64(time.Hour))
		actualResult := DurationDaysLabel(testDuration)
		if actualResult != testCase.want {
			t.Errorf("DurationDaysLabel(%.1fh) = %q, want %q", testCase.hours, actualResult, testCase.want)
		}
	}
}

func TestShipDisplayName(t *testing.T) {
	shipDisplayCases := []struct {
		internalID    string
		localisedName string
		expectedName  string
	}{
		{internalID: "lander01", localisedName: "", expectedName: "Nomad"},
		{internalID: "Explorer_NX", localisedName: "", expectedName: "Caspian Explorer"},
		{internalID: "lander01", localisedName: "Custom Nomad", expectedName: "Custom Nomad"},
		{internalID: "future_vessel", localisedName: "", expectedName: "future_vessel"},
		{internalID: "", localisedName: "", expectedName: "Unknown ship"},
	}
	for _, testCase := range shipDisplayCases {
		actualName := ShipDisplayName(testCase.internalID, testCase.localisedName)
		if actualName != testCase.expectedName {
			t.Errorf("ShipDisplayName(%q, %q) = %q, want %q", testCase.internalID, testCase.localisedName, actualName, testCase.expectedName)
		}
	}
}

