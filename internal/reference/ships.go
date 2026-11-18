package reference

import "strings"

// shipNames maps journal internal ship identifiers to display names.
var shipNames = map[string]string{
	"adder":                    "Adder",
	"anaconda":                 "Anaconda",
	"asp":                      "Asp Explorer",
	"asp_scout":                "Asp Scout",
	"belugaliner":              "Beluga Liner",
	"cobramkiii":               "Cobra Mk III",
	"cobramkiv":                "Cobra Mk IV",
	"cobramkv":                 "Cobra Mk V",
	"corsair":                  "Corsair",
	"cutter":                   "Imperial Cutter",
	"diamondback":              "Diamondback Scout",
	"diamondbackxl":            "Diamondback Explorer",
	"dolphin":                  "Dolphin",
	"eagle":                    "Eagle",
	"empire_courier":           "Imperial Courier",
	"empire_eagle":             "Imperial Eagle",
	"empire_trader":            "Imperial Clipper",
	"explorer_nx":              "Caspian Explorer",
	"federation_corvette":      "Federal Corvette",
	"federation_dropship":      "Federal Dropship",
	"federation_dropship_mkii": "Federal Assault Ship",
	"federation_gunship":       "Federal Gunship",
	"ferdelance":               "Fer-de-Lance",
	"hauler":                   "Hauler",
	"independant_trader":       "Keelback",
	"krait_light":              "Krait Phantom",
	"krait_mkii":               "Krait Mk II",
	"lakonminer":               "Type-11 Prospector",
	"mamba":                    "Mamba",
	"mandalay":                 "Mandalay",
	"orca":                     "Orca",
	"panthermkii":              "Panther Clipper Mk II",
	"python":                   "Python",
	"python_nx":                "Python Mk II",
	"sidewinder":               "Sidewinder",
	"type6":                    "Type-6 Transporter",
	"type7":                    "Type-7 Transporter",
	"type8":                    "Type-8 Transporter",
	"type9":                    "Type-9 Heavy",
	"type9_military":           "Type-10 Defender",
	"typex":                    "Alliance Chieftain",
	"typex_2":                  "Alliance Crusader",
	"typex_3":                  "Alliance Challenger",
	"viper":                    "Viper Mk III",
	"viper_mkiv":               "Viper Mk IV",
	"vulture":                  "Vulture",
	"smallcombat01_nx":         "Kestrel Mk II",
	"testbuggy":                "SRV Scarab",
	"combat_multicrew_srv_01":  "SRV Scorpion",
	"lander01":                 "Nomad",
}

// NonPlayerVesselKeywords identifies non-starship vessel identifiers excluded
// when tracking active player starships.
var NonPlayerVesselKeywords = []string{"suit", "buggy", "srv", "fighter", "lander"}

// IsPlayerShip reports whether internalShipName represents a jump-capable player
// vessel rather than an on-foot suit, SRV, fighter, or ship-launched lander.
func IsPlayerShip(internalShipName string) bool {
	trimmedName := strings.TrimSpace(internalShipName)
	if trimmedName == "" {
		return false
	}
	lowercaseName := strings.ToLower(trimmedName)
	for _, keyword := range NonPlayerVesselKeywords {
		if strings.Contains(lowercaseName, keyword) {
			return false
		}
	}
	return true
}

// FSDSynthesisBoostTier maps an FSD injection synthesis recipe name to its
// FSDJump BoostUsed code (1 basic, 2 standard, 3 premium), or 0 for non-FSD recipes.
func FSDSynthesisBoostTier(recipeName string) int {
	switch strings.ToLower(strings.TrimSpace(recipeName)) {
	case "fsd basic":
		return 1
	case "fsd standard":
		return 2
	case "fsd premium":
		return 3
	default:
		return 0
	}
}

// ShipDisplayName returns a human-friendly ship name. A localised name from
// the journal wins when present; otherwise the internal identifier is mapped.
func ShipDisplayName(internal, localised string) string {
	if strings.TrimSpace(localised) != "" {
		return localised
	}
	if displayName, found := shipNames[strings.ToLower(internal)]; found {
		return displayName
	}
	if internal == "" {
		return "Unknown ship"
	}
	return internal
}
