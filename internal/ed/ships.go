package ed

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
}

// ShipDisplayName returns a human-friendly ship name. A localised name from
// the journal wins when present; otherwise the internal identifier is mapped.
func ShipDisplayName(internal, localised string) string {
	if strings.TrimSpace(localised) != "" {
		return localised
	}
	if n, ok := shipNames[strings.ToLower(internal)]; ok {
		return n
	}
	if internal == "" {
		return "Unknown ship"
	}
	return internal
}
