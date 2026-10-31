package ed

import (
	"regexp"
	"strings"
)

// Vista Genomics exobiology values (Odyssey Update 14+ values, in credits).

// FirstLoggedMultiplier is the total payout multiplier when the player is the
// first to log a species on a body (4x bonus on top of the base = 5x total).
const FirstLoggedMultiplier = 5

// Species describes an Odyssey organic species.
type Species struct {
	CodexID string `json:"codexId"` // e.g. "$Codex_Ent_Stratum_07_Name;"
	Name    string `json:"name"`    // English name, e.g. "Stratum Tectonicas"
	Genus   string `json:"genus"`
	Value   int64  `json:"value"`
}

// speciesTable maps the codex entry key (without "$Codex_Ent_" / "_Name;") to
// the species. Keys are lower-cased.
var speciesTable = map[string]Species{}

// genusFallback holds base values for genera whose every species shares one
// value (the "single sample" Horizons-era organics) or is used when a species
// is not known to the table.
var genusFallback = map[string]int64{
	"aleoids":           3385200,
	"bacterial":         1000000,
	"cactoid":           2483600,
	"clypeus":           8418000,
	"conchas":           2352400,
	"electricae":        6284600,
	"fonticulus":        1000000,
	"shrubs":            1632500,
	"fumerolas":         6284600,
	"fungoids":          1670100,
	"osseus":            1483000,
	"recepta":           12934900,
	"stratum":           1362000,
	"tubus":             2415500,
	"tussocks":          1000000,
	"vents":             1628800, // Amphora Plant
	"sphere":            1499900, // Anemones
	"sphereabcd":        1499900,
	"sphereefgh":        1499900,
	"cone":              1471900, // Bark Mounds
	"brancae":           1593700, // Brain Trees
	"seed":              1593700,
	"seedabcd":          1593700,
	"seedefgh":          1593700,
	"ground_struct_ice": 1628800, // Crystalline Shards
	"tube":              1514500, // Sinuous Tubers
	"tubeabcd":          1514500,
	"tubeefgh":          1514500,
}

func init() {
	add := func(prefix, genus string, entries ...any) {
		for i := 0; i+2 < len(entries); i += 3 {
			variantNumber := entries[i].(string)
			name := entries[i+1].(string)
			rewardValue := int64(entries[i+2].(int))
			key := strings.ToLower(prefix + "_" + variantNumber)
			speciesTable[key] = Species{
				CodexID: "$Codex_Ent_" + prefix + "_" + variantNumber + "_Name;",
				Name:    name, Genus: genus, Value: rewardValue,
			}
		}
	}
	add("Aleoids", "Aleoida",
		"01", "Aleoida Arcus", 7252500,
		"02", "Aleoida Coronamus", 6284600,
		"03", "Aleoida Spica", 3385200,
		"04", "Aleoida Laminiae", 3385200,
		"05", "Aleoida Gravis", 12934900)
	add("Bacterial", "Bacterium",
		"01", "Bacterium Aurasus", 1000000,
		"02", "Bacterium Nebulus", 5289900,
		"03", "Bacterium Scopulum", 4934500,
		"04", "Bacterium Acies", 1000000,
		"05", "Bacterium Vesicula", 1000000,
		"06", "Bacterium Alcyoneum", 1658500,
		"07", "Bacterium Tela", 1949000,
		"08", "Bacterium Informem", 8418000,
		"09", "Bacterium Volu", 7774700,
		"10", "Bacterium Bullaris", 1152500,
		"11", "Bacterium Omentum", 4638900,
		"12", "Bacterium Cerbrus", 1689800,
		"13", "Bacterium Verrata", 3897000)
	add("Cactoid", "Cactoida",
		"01", "Cactoida Cortexum", 3667600,
		"02", "Cactoida Lapis", 2483600,
		"03", "Cactoida Vermis", 16202800,
		"04", "Cactoida Pullulanta", 3667600,
		"05", "Cactoida Peperatis", 2483600)
	add("Clypeus", "Clypeus",
		"01", "Clypeus Lacrimam", 8418000,
		"02", "Clypeus Margaritus", 11873200,
		"03", "Clypeus Speculumi", 16202800)
	add("Conchas", "Concha",
		"01", "Concha Renibus", 4572400,
		"02", "Concha Aureolas", 7774700,
		"03", "Concha Labiata", 2352400,
		"04", "Concha Biconcavis", 19010800)
	add("Electricae", "Electricae",
		"01", "Electricae Pluma", 6284600,
		"02", "Electricae Radialem", 6284600)
	add("Fonticulus", "Fonticulua",
		"01", "Fonticulua Segmentatus", 19010800,
		"02", "Fonticulua Campestris", 1000000,
		"03", "Fonticulua Upupam", 5727600,
		"04", "Fonticulua Lapida", 3111000,
		"05", "Fonticulua Fluctus", 20000000,
		"06", "Fonticulua Digitos", 1804100)
	add("Shrubs", "Frutexa",
		"01", "Frutexa Flabellum", 1808900,
		"02", "Frutexa Acus", 7774700,
		"03", "Frutexa Metallicum", 1632500,
		"04", "Frutexa Flammasis", 10326000,
		"05", "Frutexa Fera", 1632500,
		"06", "Frutexa Sponsae", 5988000,
		"07", "Frutexa Collum", 1639800)
	add("Fumerolas", "Fumerola",
		"01", "Fumerola Carbosis", 6284600,
		"02", "Fumerola Extremus", 16202800,
		"03", "Fumerola Nitris", 7500900,
		"04", "Fumerola Aquatis", 6284600)
	add("Fungoids", "Fungoida",
		"01", "Fungoida Setisis", 1670100,
		"02", "Fungoida Stabitis", 2680300,
		"03", "Fungoida Bullarum", 3703200,
		"04", "Fungoida Gelata", 3330300)
	add("Osseus", "Osseus",
		"01", "Osseus Fractus", 4027800,
		"02", "Osseus Discus", 12934900,
		"03", "Osseus Spiralis", 2404700,
		"04", "Osseus Pumice", 3156300,
		"05", "Osseus Cornibus", 1483000,
		"06", "Osseus Pellebantus", 9739000)
	add("Recepta", "Recepta",
		"01", "Recepta Umbrux", 12934900,
		"02", "Recepta Deltahedronix", 16202800,
		"03", "Recepta Conditivus", 14313700)
	add("Stratum", "Stratum",
		"01", "Stratum Excutitus", 2448900,
		"02", "Stratum Paleas", 1362000,
		"03", "Stratum Laminamus", 2788300,
		"04", "Stratum Araneamus", 2448900,
		"05", "Stratum Limaxus", 1362000,
		"06", "Stratum Cucumisis", 16202800,
		"07", "Stratum Tectonicas", 19010800,
		"08", "Stratum Frigus", 2637500)
	add("Tubus", "Tubus",
		"01", "Tubus Conifer", 2415500,
		"02", "Tubus Sororibus", 5727600,
		"03", "Tubus Cavas", 11873200,
		"04", "Tubus Rosarium", 2637500,
		"05", "Tubus Compagibus", 7774700)
	add("Tussocks", "Tussock",
		"01", "Tussock Pennata", 5853800,
		"02", "Tussock Ventusa", 3277700,
		"03", "Tussock Ignis", 1849000,
		"04", "Tussock Cultro", 1766600,
		"05", "Tussock Catena", 1766600,
		"06", "Tussock Pennatis", 1000000,
		"07", "Tussock Serrati", 4447100,
		"08", "Tussock Albata", 3252500,
		"09", "Tussock Propagito", 1000000,
		"10", "Tussock Divisa", 1766600,
		"11", "Tussock Caputus", 3472400,
		"12", "Tussock Triticum", 7774700,
		"13", "Tussock Stigmasis", 19010800,
		"14", "Tussock Virgam", 14313700,
		"15", "Tussock Capillum", 7025800)

	for _, speciesItem := range speciesTable {
		speciesByName[strings.ToLower(speciesItem.Name)] = speciesItem
	}
}

var speciesByName = map[string]Species{}

var codexRe = regexp.MustCompile(`(?i)^\$codex_ent_(.+?)(?:_name)?;?$`)
var codexNumRe = regexp.MustCompile(`^(.*?)_(\d+)$`)

// codexKey turns "$Codex_Ent_Stratum_07_Name;" into "stratum_07".
func codexKey(codexID string) string {
	matches := codexRe.FindStringSubmatch(strings.TrimSpace(codexID))
	if matches == nil {
		return ""
	}
	return strings.ToLower(matches[1])
}

// LookupSpecies resolves a species from a journal `Species` codex key and/or
// the `Species_Localised` English name. ok is false when unknown.
func LookupSpecies(codexID, localisedName string) (Species, bool) {
	if keyString := codexKey(codexID); keyString != "" {
		if foundSpecies, found := speciesTable[keyString]; found {
			return foundSpecies, true
		}
	}
	if foundSpecies, found := speciesByName[strings.ToLower(strings.TrimSpace(localisedName))]; found {
		return foundSpecies, true
	}
	return Species{}, false
}

// SpeciesBaseValue returns the Vista Genomics base value for a species,
// falling back to genus-level values for single-value genera (anemones,
// brain trees, tubers, ...). Returns 0 when unknown.
func SpeciesBaseValue(speciesCodex, speciesLocalised, genusCodex string) int64 {
	if foundSpecies, found := LookupSpecies(speciesCodex, speciesLocalised); found {
		return foundSpecies.Value
	}
	if speciesKey := codexKey(speciesCodex); speciesKey != "" {
		base := speciesKey
		if matches := codexNumRe.FindStringSubmatch(speciesKey); matches != nil {
			base = matches[1]
		}
		if fallbackValue, found := genusFallback[base]; found {
			return fallbackValue
		}
	}
	if genusKey := codexKey(genusCodex); genusKey != "" {
		genusKey = strings.TrimSuffix(genusKey, "_genus")
		if fallbackValue, found := genusFallback[genusKey]; found {
			return fallbackValue
		}
	}
	return 0
}

// OrganicValue computes the exobiology payout split into base and first
// logged bonus (4x base, i.e. 5x total).
func OrganicValue(baseValue int64, firstLogged bool) ScanValue {
	if firstLogged {
		return ScanValue{Base: baseValue, Bonus: baseValue * (FirstLoggedMultiplier - 1), Total: baseValue * FirstLoggedMultiplier}
	}
	return ScanValue{Base: baseValue, Total: baseValue}
}
