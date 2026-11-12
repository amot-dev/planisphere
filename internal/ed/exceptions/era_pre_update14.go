package exceptions

import (
	"regexp"
	"strings"

	"planisphere/internal/ed"
)

// Update14ReleaseTimestamp is the UTC deployment cutoff for Elite Dangerous
// Update 14 (29 November 2022), which raised Vista Genomics base values and
// increased the First Logged total multiplier from 2x to 5x.
const Update14ReleaseTimestamp = "2022-11-29T08:00:00Z"

// odysseyUpdate7Timestamp marks Odyssey Update 7 (22 September 2021), which
// adjusted Tubus Rosarium from its launch 400,500 Cr tier to 171,900 Cr.
const odysseyUpdate7Timestamp = "2021-09-22T07:00:00Z"

// PreUpdate14FirstLoggedMultiplier is the total payout multiplier awarded for
// first-logged species prior to Update 14 (1x base plus 1x bonus = 2x total).
const PreUpdate14FirstLoggedMultiplier int64 = 2

var preUpdate14SpeciesValues = map[string]int64{
	"aleoids_01":    379300, // Aleoida Arcus
	"aleoids_02":    339100, // Aleoida Coronamus
	"aleoids_03":    208900, // Aleoida Spica
	"aleoids_04":    208900, // Aleoida Laminiae
	"aleoids_05":    596500, // Aleoida Gravis
	"bacterial_01":  50000,  // Bacterium Aurasus
	"bacterial_02":  296300, // Bacterium Nebulus
	"bacterial_03":  280600, // Bacterium Scopulum
	"bacterial_04":  50000,  // Bacterium Acies
	"bacterial_05":  52900,  // Bacterium Vesicula
	"bacterial_06":  119500, // Bacterium Alcyoneum
	"bacterial_07":  135600, // Bacterium Tela
	"bacterial_08":  426200, // Bacterium Informem
	"bacterial_09":  400500, // Bacterium Volu
	"bacterial_10":  89900,  // Bacterium Bullaris
	"bacterial_11":  267400, // Bacterium Omentum
	"bacterial_12":  121300, // Bacterium Cerbrus
	"bacterial_13":  233300, // Bacterium Verrata
	"cactoid_01":    222500, // Cactoida Cortexum
	"cactoid_02":    164000, // Cactoida Lapis
	"cactoid_03":    711500, // Cactoida Vermis
	"cactoid_04":    222500, // Cactoida Pullulanta
	"cactoid_05":    164000, // Cactoida Peperatis
	"clypeus_01":    426200, // Clypeus Lacrimam
	"clypeus_02":    557800, // Clypeus Margaritus
	"clypeus_03":    711500, // Clypeus Speculumi
	"conchas_01":    264300, // Concha Renibus
	"conchas_02":    400500, // Concha Aureolas
	"conchas_03":    157200, // Concha Labiata
	"conchas_04":    806300, // Concha Biconcavis
	"electricae_01": 339100, // Electricae Pluma
	"electricae_02": 339100, // Electricae Radialem
	"fonticulus_01": 806300, // Fonticulua Segmentatus
	"fonticulus_02": 63600,  // Fonticulua Campestris
	"fonticulus_03": 315300, // Fonticulua Upupam
	"fonticulus_04": 195600, // Fonticulua Lapida
	"fonticulus_05": 900000, // Fonticulua Fluctus
	"fonticulus_06": 127700, // Fonticulua Digitos
	"shrubs_01":     127900, // Frutexa Flabellum
	"shrubs_02":     400500, // Frutexa Acus
	"shrubs_03":     118100, // Frutexa Metallicum
	"shrubs_04":     500100, // Frutexa Flammasis
	"shrubs_05":     118100, // Frutexa Fera
	"shrubs_06":     326500, // Frutexa Sponsae
	"shrubs_07":     118500, // Frutexa Collum
	"fumerolas_01":  339100, // Fumerola Carbosis
	"fumerolas_02":  711500, // Fumerola Extremus
	"fumerolas_03":  389400, // Fumerola Nitris
	"fumerolas_04":  339100, // Fumerola Aquatis
	"fungoids_01":   120200, // Fungoida Setisis
	"fungoids_02":   174000, // Fungoida Stabitis
	"fungoids_03":   224100, // Fungoida Bullarum
	"fungoids_04":   206300, // Fungoida Gelata
	"osseus_01":     239400, // Osseus Fractus
	"osseus_02":     596500, // Osseus Discus
	"osseus_03":     159900, // Osseus Spiralis
	"osseus_04":     197800, // Osseus Pumice
	"osseus_05":     109500, // Osseus Cornibus
	"osseus_06":     477700, // Osseus Pellebantus
	"recepta_01":    596500, // Recepta Umbrux
	"recepta_02":    711500, // Recepta Deltahedronix
	"recepta_03":    645700, // Recepta Conditivus
	"stratum_01":    162200, // Stratum Excutitus
	"stratum_02":    102500, // Stratum Paleas
	"stratum_03":    179500, // Stratum Laminamus
	"stratum_04":    162200, // Stratum Araneamus
	"stratum_05":    102500, // Stratum Limaxus
	"stratum_06":    711500, // Stratum Cucumisis
	"stratum_07":    806300, // Stratum Tectonicas
	"stratum_08":    171900, // Stratum Frigus
	"tubus_01":      160400, // Tubus Conifer
	"tubus_02":      315300, // Tubus Sororibus
	"tubus_03":      557800, // Tubus Cavas
	"tubus_04":      171900, // Tubus Rosarium
	"tubus_05":      400500, // Tubus Compagibus
	"tussocks_01":   320700, // Tussock Pennata
	"tussocks_02":   203700, // Tussock Ventusa
	"tussocks_03":   130200, // Tussock Ignis
	"tussocks_04":   125600, // Tussock Cultro
	"tussocks_05":   125600, // Tussock Catena
	"tussocks_06":   59600,  // Tussock Pennatis
	"tussocks_07":   258700, // Tussock Serrati
	"tussocks_08":   202500, // Tussock Albata
	"tussocks_09":   66500,  // Tussock Propagito
	"tussocks_10":   125600, // Tussock Divisa
	"tussocks_11":   213100, // Tussock Caputus
	"tussocks_12":   400500, // Tussock Triticum
	"tussocks_13":   806300, // Tussock Stigmasis
	"tussocks_14":   645700, // Tussock Virgam
	"tussocks_15":   370000, // Tussock Capillum
}

var preUpdate14GenusFallback = map[string]int64{
	"aleoids":           208900,
	"bacterial":         50000,
	"cactoid":           164000,
	"clypeus":           426200,
	"conchas":           157200,
	"electricae":        339100,
	"fonticulus":        63600,
	"shrubs":            118100,
	"fumerolas":         339100,
	"fungoids":          120200,
	"osseus":            109500,
	"recepta":           596500,
	"stratum":           102500,
	"tubus":             160400,
	"tussocks":          59600,
	"vents":             117900,
	"sphere":            110500,
	"sphereabcd":        110500,
	"sphereefgh":        110500,
	"cone":              108900,
	"brancae":           115900,
	"seed":              115900,
	"seedabcd":          115900,
	"seedefgh":          115900,
	"ground_struct_ice": 117900,
	"tube":              111300,
	"tubeabcd":          111300,
	"tubeefgh":          111300,
}

var preUpdate14TrailingDigitsRe = regexp.MustCompile(`^(.*?)_(\d+)$`)

func init() {
	RegisterOrganicRule(OrganicRule{
		ID:       "era_pre_update14",
		Priority: 10,
		Evaluate: evaluatePreUpdate14Organic,
	})
}

func evaluatePreUpdate14Organic(context OrganicContext) (ed.ScanValue, bool) {
	effectiveTimestamp := context.EffectiveTimestamp()
	if effectiveTimestamp == "" || effectiveTimestamp >= Update14ReleaseTimestamp {
		return ed.ScanValue{}, false
	}
	legacyBaseValue := lookupPreUpdate14BaseValue(context, effectiveTimestamp)
	if legacyBaseValue == 0 {
		return ed.ScanValue{}, false
	}
	if context.FirstLogged {
		bonusValue := legacyBaseValue * (PreUpdate14FirstLoggedMultiplier - 1)
		return ed.ScanValue{
			Base:  legacyBaseValue,
			Bonus: bonusValue,
			Total: legacyBaseValue + bonusValue,
		}, true
	}
	return ed.ScanValue{Base: legacyBaseValue, Total: legacyBaseValue}, true
}

func lookupPreUpdate14BaseValue(context OrganicContext, effectiveTimestamp string) int64 {
	speciesKey := context.ResolvedSpeciesKey()
	if speciesKey == "tubus_04" && effectiveTimestamp < odysseyUpdate7Timestamp {
		return 400500
	}
	if legacyValue, found := preUpdate14SpeciesValues[speciesKey]; found {
		return legacyValue
	}
	if speciesKey != "" {
		genusPrefix := speciesKey
		if matches := preUpdate14TrailingDigitsRe.FindStringSubmatch(speciesKey); matches != nil {
			genusPrefix = matches[1]
		}
		if fallbackValue, found := preUpdate14GenusFallback[genusPrefix]; found {
			return fallbackValue
		}
	}
	if genusKey := ed.CodexKey(context.GenusCodex); genusKey != "" {
		genusKey = strings.TrimSuffix(genusKey, "_genus")
		if fallbackValue, found := preUpdate14GenusFallback[genusKey]; found {
			return fallbackValue
		}
	}
	return 0
}

