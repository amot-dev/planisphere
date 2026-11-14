package ed

import (
	_ "embed"
	"encoding/json"
	"sort"
	"sync"
)

// Galactic regions.
//
// The region names and the coordinate grid (regiondata/RegionMapData.json) come
// from klightspeed/EliteDangerousRegionMap (MIT licence, see
// regiondata/LICENSE). The adjacency table below was derived from that grid by
// recording every pair of regions that share a border of at least a few grid
// cells (single-pixel corner contacts are ignored); TestAdjacencyMatchesGrid
// verifies it against the embedded map.

// InnerOrionSpurID is Region 18 (The Bubble), the default anchor region.
const InnerOrionSpurID = 18

// Region is one of the 42 official galactic regions.
type Region struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Regions lists all 42 regions indexed by ID-1.
var Regions = []Region{
	{1, "Galactic Centre"}, {2, "Empyrean Straits"}, {3, "Ryker's Hope"},
	{4, "Odin's Hold"}, {5, "Norma Arm"}, {6, "Arcadian Stream"},
	{7, "Izanami"}, {8, "Inner Orion-Perseus Conflux"}, {9, "Inner Scutum-Centaurus Arm"},
	{10, "Norma Expanse"}, {11, "Trojan Belt"}, {12, "The Veils"},
	{13, "Newton's Vault"}, {14, "The Conduit"}, {15, "Outer Orion-Perseus Conflux"},
	{16, "Orion-Cygnus Arm"}, {17, "Temple"}, {18, "Inner Orion Spur"},
	{19, "Hawking's Gap"}, {20, "Dryman's Point"}, {21, "Sagittarius-Carina Arm"},
	{22, "Mare Somnia"}, {23, "Acheron"}, {24, "Formorian Frontier"},
	{25, "Hieronymus Delta"}, {26, "Outer Scutum-Centaurus Arm"}, {27, "Outer Arm"},
	{28, "Aquila's Halo"}, {29, "Errant Marches"}, {30, "Perseus Arm"},
	{31, "Formidine Rift"}, {32, "Vulcan Gate"}, {33, "Elysian Shore"},
	{34, "Sanguineous Rim"}, {35, "Outer Orion Spur"}, {36, "Achilles's Altar"},
	{37, "Xibalba"}, {38, "Lyra's Song"}, {39, "Tenebrae"},
	{40, "The Abyss"}, {41, "Kepler's Crest"}, {42, "The Void"},
}

// RegionAdjacency maps a region ID to the IDs of the regions bordering it.
var RegionAdjacency = map[int][]int{
	1:  {2, 3, 4},
	2:  {1, 3, 4, 5, 6},
	3:  {1, 2, 4, 6, 7},
	4:  {1, 2, 3, 5, 7, 8, 9},
	5:  {2, 4, 6, 9, 10},
	6:  {2, 3, 5, 10, 11, 12, 13},
	7:  {3, 4, 8, 13, 15},
	8:  {4, 7, 9, 15, 16},
	9:  {4, 5, 8, 10, 16, 17, 18},
	10: {5, 6, 9, 11, 18, 19, 20},
	11: {6, 10, 12, 20, 21},
	12: {6, 11, 13, 21, 22, 23, 24},
	13: {6, 7, 12, 14, 15, 24, 25},
	14: {13, 15, 25, 26, 27, 30},
	15: {7, 8, 13, 14, 16, 30},
	16: {8, 9, 15, 17, 30, 32},
	17: {9, 16, 18, 32, 33},
	18: {9, 10, 17, 19, 33, 34, 35},
	19: {10, 18, 20, 35, 36, 38},
	20: {10, 11, 19, 21, 38, 39},
	21: {11, 12, 20, 22, 39},
	22: {12, 21, 23, 39},
	23: {12, 22, 24, 40},
	24: {12, 13, 23, 25, 40},
	25: {13, 14, 24, 26, 40},
	26: {14, 25, 27, 40, 42},
	27: {14, 26, 28, 29, 30, 32, 42},
	28: {27, 29, 42},
	29: {27, 28, 31, 32, 33},
	30: {14, 15, 16, 27, 32},
	31: {29, 33, 41},
	32: {16, 17, 27, 29, 30, 33},
	33: {17, 18, 29, 31, 32, 34, 41},
	34: {18, 33, 35, 36, 41},
	35: {18, 19, 34, 36},
	36: {19, 34, 35, 37, 38, 41},
	37: {36, 38, 39, 41},
	38: {19, 20, 36, 37, 39},
	39: {20, 21, 22, 37, 38},
	40: {23, 24, 25, 26},
	41: {31, 33, 34, 36, 37},
	42: {26, 27, 28},
}

// RegionByID returns the region with the given ID.
func RegionByID(id int) (Region, bool) {
	if id < 1 || id > len(Regions) {
		return Region{}, false
	}
	return Regions[id-1], true
}

// RegionOption is a region entry in the prioritised dropdown.
type RegionOption struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Adjacent bool   `json:"adjacent"` // borders the anchor region
	Anchor   bool   `json:"anchor"`   // is the anchor region itself
}

// PrioritizedRegions orders the 42 regions for the Region Header dropdown:
// regions adjacent to the anchor (the previous region header, or Inner Orion
// Spur when there is none) come first, then the anchor itself, then the rest
// in ID order. Pass anchorID <= 0 to use the default anchor.
func PrioritizedRegions(anchorID int) []RegionOption {
	if _, ok := RegionByID(anchorID); !ok {
		anchorID = InnerOrionSpurID
	}
	isAdjacent := map[int]bool{}
	for _, id := range RegionAdjacency[anchorID] {
		isAdjacent[id] = true
	}
	regionOptions := make([]RegionOption, 0, len(Regions))
	for _, regionItem := range Regions {
		regionOptions = append(regionOptions, RegionOption{ID: regionItem.ID, Name: regionItem.Name, Adjacent: isAdjacent[regionItem.ID], Anchor: regionItem.ID == anchorID})
	}
	rank := func(option RegionOption) int {
		switch {
		case option.Adjacent:
			return 0
		case option.Anchor:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(regionOptions, func(firstIndex, secondIndex int) bool {
		return rank(regionOptions[firstIndex]) < rank(regionOptions[secondIndex])
	})
	return regionOptions
}

// ---- coordinate -> region lookup -------------------------------------------

//go:embed regiondata/RegionMapData.json
var regionMapJSON []byte

const (
	regionX0 = -49985.0
	regionZ0 = -24105.0
)

var (
	regionGridOnce sync.Once
	regionGrid     [][][2]int // per row: run-length pairs (length, regionID)
)

func loadRegionGrid() {
	var decodedRegionMap struct {
		RegionMap [][][2]int `json:"regionmap"`
	}
	if unmarshalError := json.Unmarshal(regionMapJSON, &decodedRegionMap); unmarshalError == nil {
		regionGrid = decodedRegionMap.RegionMap
	}
}

const (
	galaxyGridScaleNumerator   = 83
	galaxyGridScaleDenominator = 4096
)

// RegionAt returns the galactic region containing the given coordinates.
func RegionAt(coordX, coordY, coordZ float64) (Region, bool) {
	regionGridOnce.Do(loadRegionGrid)
	_ = coordY
	// The raster grid uses a scale factor of 83 / 4096 pixels per light year (approximately 49.35 light years per pixel),
	// matching Frontier and EDDiscovery galactic coordinate projections.
	pixelX := int((coordX - regionX0) * galaxyGridScaleNumerator / galaxyGridScaleDenominator)
	pixelZ := int((coordZ - regionZ0) * galaxyGridScaleNumerator / galaxyGridScaleDenominator)
	if pixelX < 0 || pixelZ < 0 || pixelZ >= len(regionGrid) {
		return Region{}, false
	}
	runX := 0
	for _, run := range regionGrid[pixelZ] {
		if pixelX < runX+run[0] {
			return RegionByID(run[1])
		}
		runX += run[0]
	}
	return Region{}, false
}
