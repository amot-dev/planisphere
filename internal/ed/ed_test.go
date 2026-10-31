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
	iso := "2026-01-18T19:42:10Z"
	got := ToGalactic(iso)
	want := "3312-01-18 19:42:10 UTC"
	if got != want {
		t.Fatalf("ToGalactic(%q) = %q, want %q", iso, got, want)
	}
	back, err := FromGalactic(got)
	if err != nil {
		t.Fatalf("FromGalactic(%q) error: %v", got, err)
	}
	if back != iso {
		t.Fatalf("FromGalactic(%q) = %q, want %q", got, back, iso)
	}

	// Leap day preservation: 2028 is a leap year; 2028+1286 = 3314 is not in Gregorian,
	// yet the galactic string must preserve 02-29.
	leapISO := "2028-02-29T08:15:00Z"
	leapGST := ToGalactic(leapISO)
	if leapGST != "3314-02-29 08:15:00 UTC" {
		t.Fatalf("ToGalactic leap day = %q", leapGST)
	}
	leapBack, err := FromGalactic(leapGST)
	if err != nil || leapBack != leapISO {
		t.Fatalf("FromGalactic leap day = %q, %v", leapBack, err)
	}
}

func TestCartographicsValuation(t *testing.T) {
	// Earth-like world, 1.0 EM, first discovered and first mapped efficiently in Odyssey.
	elw := BodyScan{
		PlanetClass:   "Earthlike body",
		MassEM:        1.0,
		WasDiscovered: false,
		WasMapped:     false,
	}
	unmapped := EstimateBodyValue(elw, MappingInfo{}, true)
	if unmapped.Base <= 0 || unmapped.Bonus <= 0 || unmapped.Total != unmapped.Base+unmapped.Bonus {
		t.Fatalf("unexpected unmapped ELW value: %+v", unmapped)
	}

	mapped := EstimateBodyValue(elw, MappingInfo{Mapped: true, ProbesUsed: 5, EfficiencyTarget: 6}, true)
	inc := EstimateMappingIncrement(elw, MappingInfo{Mapped: true, ProbesUsed: 5, EfficiencyTarget: 6}, true)
	if unmapped.Add(inc) != mapped {
		t.Fatalf("scan + mapping increment != total mapped value: %+v + %+v != %+v", unmapped, inc, mapped)
	}
	if mapped.Total < 3_000_000 {
		t.Fatalf("expected mapped first-discovered ELW to exceed 3M Cr, got %d", mapped.Total)
	}
}

func TestExobiologyValuation(t *testing.T) {
	base := SpeciesBaseValue("$Codex_Ent_Stratum_07_Name;", "Stratum Tectonicas", "$Codex_Ent_Stratum_Genus_Name;")
	if base != 19010800 {
		t.Fatalf("Stratum Tectonicas base = %d, want 19010800", base)
	}
	val := OrganicValue(base, true)
	if val.Base != 19010800 || val.Bonus != 19010800*4 || val.Total != 19010800*5 {
		t.Fatalf("OrganicValue with 5x bonus = %+v", val)
	}
}

func TestCarrierTritiumCost(t *testing.T) {
	// 500 Ly jump at empty load (used=0, fuel=1000): round(5 + 500*(26000)/200000) = round(5 + 65) = 70
	got := CarrierTritiumCost(500, 0, 1000)
	if got != 70 {
		t.Fatalf("CarrierTritiumCost(500, 0, 1000) = %d, want 70", got)
	}
	if math.Abs(Distance(Vec3{0, 0, 0}, Vec3{3, 4, 12})-13) > 1e-9 {
		t.Fatalf("Distance failed")
	}
}

func TestRegionsAndAdjacency(t *testing.T) {
	if len(Regions) != 42 {
		t.Fatalf("len(Regions) = %d, want 42", len(Regions))
	}
	// Sol (0,0,0) is in Region 18: Inner Orion Spur.
	reg, ok := RegionAt(0, 0, 0)
	if !ok || reg.ID != InnerOrionSpurID || reg.Name != "Inner Orion Spur" {
		t.Fatalf("RegionAt(0,0,0) = %+v, %v", reg, ok)
	}
	// Sagittarius A* (~25.2, -20.9, 25899.9) is in Region 1: Galactic Centre.
	core, ok := RegionAt(25.2, -20.9, 25899.9)
	if !ok || core.ID != 1 {
		t.Fatalf("RegionAt(Sag A*) = %+v, %v", core, ok)
	}

	// Default prioritization (anchor 0 -> Region 18) puts Region 18's neighbours first.
	opts := PrioritizedRegions(0)
	if len(opts) != 42 {
		t.Fatalf("len(PrioritizedRegions) = %d", len(opts))
	}
	wantAdj := RegionAdjacency[InnerOrionSpurID]
	for i := 0; i < len(wantAdj); i++ {
		if !opts[i].Adjacent {
			t.Fatalf("expected option %d (%+v) to be marked adjacent", i, opts[i])
		}
	}
	if !opts[len(wantAdj)].Anchor || opts[len(wantAdj)].ID != InnerOrionSpurID {
		t.Fatalf("expected anchor after adjacent options, got %+v", opts[len(wantAdj)])
	}
}

func TestAdjacencyMatchesGrid(t *testing.T) {
	var data struct {
		RegionMap [][][2]int `json:"regionmap"`
	}
	if err := json.Unmarshal(regionMapJSON, &data); err != nil {
		t.Fatal(err)
	}
	rows := make([][]int, len(data.RegionMap))
	width := 0
	for z, r := range data.RegionMap {
		var row []int
		for _, pair := range r {
			for k := 0; k < pair[0]; k++ {
				row = append(row, pair[1])
			}
		}
		if len(row) > width {
			width = len(row)
		}
		rows[z] = row
	}
	counts := map[[2]int]int{}
	for z := 0; z < len(rows); z++ {
		for x := 0; x < len(rows[z]); x++ {
			a := rows[z][x]
			if a == 0 {
				continue
			}
			for _, d := range [][2]int{{0, 1}, {1, 0}} {
				zz, xx := z+d[0], x+d[1]
				if zz < len(rows) && xx < len(rows[zz]) {
					b := rows[zz][xx]
					if b != 0 && b != a {
						p := [2]int{a, b}
						if p[0] > p[1] {
							p[0], p[1] = p[1], p[0]
						}
						counts[p]++
					}
				}
			}
		}
	}
	derived := map[int][]int{}
	for pair, c := range counts {
		if c < 5 { // ignore single-pixel diagonal corner contact (3,13)
			continue
		}
		derived[pair[0]] = append(derived[pair[0]], pair[1])
		derived[pair[1]] = append(derived[pair[1]], pair[0])
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
	for _, tc := range tests {
		dur := time.Duration(tc.hours * float64(time.Hour))
		got := DurationDaysLabel(dur)
		if got != tc.want {
			t.Errorf("DurationDaysLabel(%.1fh) = %q, want %q", tc.hours, got, tc.want)
		}
	}
}

