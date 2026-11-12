package stats

import (
	"sort"
	"strings"
	"sync"

	"planisphere/internal/db"
	"planisphere/internal/ed"
	"planisphere/internal/journal"
)

// RealizationBreakdown contains the realized net earnings and informational
// deduction breakdown for a time window (for example, an expedition waypoint
// leg or overall expedition).
type RealizationBreakdown struct {
	TotalRealized     int64 `json:"totalRealized"`     // Net credits pocketed across cartographics and exobiology sales
	CartoLostDeath    int64 `json:"cartoLostDeath"`    // Cartographics lost on ship destruction
	OrganicLostDeath  int64 `json:"organicLostDeath"`  // Exobiology lost on any death
	CartoStolen       int64 `json:"cartoStolen"`       // Cartographics discovery bonuses claimed by someone else first
	OrganicStolen     int64 `json:"organicStolen"`     // Exobiology first-logged bonuses claimed by someone else first
	CarrierTariffLoss int64 `json:"carrierTariffLoss"` // Universal Cartographics 25% tariff when selling on a Fleet Carrier
	NpcCrewLoss       int64 `json:"npcCrewLoss"`       // Active or inactive NPC crew wage deductions
	PendingValue      int64 `json:"pendingValue"`      // Estimated value still in transit (unsold and not lost)
}

// Add returns the field-wise sum of two breakdowns.
func (base RealizationBreakdown) Add(other RealizationBreakdown) RealizationBreakdown {
	return RealizationBreakdown{
		TotalRealized:     base.TotalRealized + other.TotalRealized,
		CartoLostDeath:    base.CartoLostDeath + other.CartoLostDeath,
		OrganicLostDeath:  base.OrganicLostDeath + other.OrganicLostDeath,
		CartoStolen:       base.CartoStolen + other.CartoStolen,
		OrganicStolen:     base.OrganicStolen + other.OrganicStolen,
		CarrierTariffLoss: base.CarrierTariffLoss + other.CarrierTariffLoss,
		NpcCrewLoss:       base.NpcCrewLoss + other.NpcCrewLoss,
		PendingValue:      base.PendingValue + other.PendingValue,
	}
}

type scanRecord struct {
	eventUID             string
	timestamp            string
	systemAddress        int64
	starSystem           string
	normalizedStarSystem string
	bodyID               int64
	bodyName             string
	normalizedBodyName   string
	isOrganic            bool
	hasScan              bool
	hasMapped            bool
	undiscoveredAtScan   bool
	genus                string
	species              string
	speciesLocalised     string
	variant              string
	variantLocalised     string
	normalizedSpecies    string
	normalizedVariant    string
	theoreticalBase      int64
	theoreticalBonus     int64
	theoreticalTotal     int64

	// Realized outcome
	status            string // "pending", "sold", "lost_death"
	actualBase        int64
	actualBonus       int64
	actualNet         int64
	deathLoss         int64
	stolenLoss        int64
	crewLoss          int64
	carrierTariffLoss int64
	soldTimestamp     string
}

// normalizeSpecies resolves a canonical lowercase species name from Frontier's
// journal fields, where codexID uses token syntax such as
// "$Codex_Ent_Stratum_07_Name;". It checks the exobiology species catalogue
// first, then falls back to the trimmed localised name, and finally strips
// leading and trailing "$" and ";" delimiters from the raw codex token.
func normalizeSpecies(codexID, localised string) string {
	if speciesEntry, ok := ed.LookupSpecies(codexID, localised); ok {
		return strings.ToLower(speciesEntry.Name)
	}
	if trimmedLocalised := strings.TrimSpace(localised); trimmedLocalised != "" {
		return strings.ToLower(trimmedLocalised)
	}
	trimmedCodexID := strings.Trim(strings.TrimSpace(codexID), "$;")
	return strings.ToLower(trimmedCodexID)
}

// normalizeVariant resolves a canonical lowercase variant name from Frontier's
// journal fields, where codexID uses token syntax such as
// "$Codex_Ent_Stratum_07_A_Name;". It prefers the trimmed localised variant
// string and falls back to stripping leading and trailing "$" and ";"
// delimiters from the raw codex token.
func normalizeVariant(codexID, localised string) string {
	if trimmedLocalised := strings.TrimSpace(localised); trimmedLocalised != "" {
		return strings.ToLower(trimmedLocalised)
	}
	trimmedCodexID := strings.Trim(strings.TrimSpace(codexID), "$;")
	return strings.ToLower(trimmedCodexID)
}

// organicCandidateTier scores how well a pending ScanOrganic record matches a
// sold BioData item across three variant tiers (exact non-empty variant match,
// compatible empty-variant match, and conflicting-variant species fallback),
// adding a tie-breaker point within each tier when the scan's first-logged
// bonus expectation matches whether Vista Genomics paid a bonus.
func organicCandidateTier(candidateRecord *scanRecord, soldSpecies, soldVariant string, expectedBonus bool) int {
	if candidateRecord.status != "pending" || candidateRecord.normalizedSpecies != soldSpecies {
		return 0
	}
	tierBaseScore := 3
	if soldVariant != "" && candidateRecord.normalizedVariant != "" {
		if candidateRecord.normalizedVariant == soldVariant {
			tierBaseScore = 5
		} else {
			tierBaseScore = 1
		}
	}
	if candidateRecord.undiscoveredAtScan == expectedBonus {
		return tierBaseScore + 1
	}
	return tierBaseScore
}

// findMatchingOrganic returns the index of the best-matching pending ScanOrganic
// record in unsoldOrganicRecords for the sold bioDataItem, or -1 if none matches.
//
// Frontier's SellOrganicData journal event (BioData) does not include
// SystemAddress, StarSystem, BodyID, or BodyName, so sold samples cannot be
// joined to ScanOrganic records by body. When a commander scans the same
// species on both an already-footfalled body and an un-footfalled body before
// selling at Vista Genomics, tie-breaking within each variant tier by
// undiscoveredAtScan == (bioDataItem.Bonus > 0) prevents a bonus-paying sample
// from matching the earlier footfalled-body scan and falsely triggering both a
// retroactive bonus upgrade and a stolen-bonus loss on the un-footfalled scan.
func findMatchingOrganic(unsoldOrganicRecords []*scanRecord, bioDataItem journal.BioDataItem) int {
	soldSpecies := normalizeSpecies(bioDataItem.Species, bioDataItem.SpeciesLocalised)
	if soldSpecies == "" {
		return -1
	}
	soldVariant := normalizeVariant(bioDataItem.Variant, bioDataItem.VariantLocalised)
	expectedBonus := bioDataItem.Bonus > 0

	bestMatchIndex := -1
	bestMatchTier := 0
	for recordIndex, candidateRecord := range unsoldOrganicRecords {
		candidateTier := organicCandidateTier(candidateRecord, soldSpecies, soldVariant, expectedBonus)
		if candidateTier > bestMatchTier {
			bestMatchTier = candidateTier
			bestMatchIndex = recordIndex
		}
	}
	return bestMatchIndex
}

type organicSaleOutcome struct {
	saleTimestamp string
	saleMarketID  int64
	bonusRealized bool
}

// RealizationLedger tracks all scans and their final realization outcomes.
type RealizationLedger struct {
	records []*scanRecord
	// organicSales maps ScanOrganic event_uid to its realized Vista Genomics
	// redemption metadata so scan valuation and realization remain synchronized.
	organicSales map[string]organicSaleOutcome
	// Unattributed sales that could not be matched to a preceding scan
	unattributedSales []unattributedSale
}

// HasRealizedOrganicBonus reports whether the ScanOrganic event with eventUID
// was subsequently sold in SellOrganicData with a positive first-logged bonus.
// Required because of Frontier Issue #81891 and remains required for backwards
// compatibility with older journal files even if or when
// https://issues.frontierstore.net/issue-detail/81891 is fixed.
func (ledger *RealizationLedger) HasRealizedOrganicBonus(eventUID string) bool {
	if ledger == nil || eventUID == "" {
		return false
	}
	return ledger.organicSales[eventUID].bonusRealized
}

func (ledger *RealizationLedger) organicSaleDetails(eventUID string) (organicSaleOutcome, bool) {
	if ledger == nil || eventUID == "" {
		return organicSaleOutcome{}, false
	}
	outcome, found := ledger.organicSales[eventUID]
	return outcome, found
}

type unattributedSale struct {
	timestamp         string
	kind              string // "carto" or "organic"
	netRealized       int64
	crewLoss          int64
	carrierTariffLoss int64
}

var realizationEvents = []string{
	"LoadGame",
	"Location",
	"FSDJump",
	"CarrierJump",
	"Docked",
	"Undocked",
	"Scan",
	"SAAScanComplete",
	"ScanOrganic",
	"SellExplorationData",
	"MultiSellExplorationData",
	"SellOrganicData",
	"Resurrect",
	"NpcCrewPaidWage",
}

// ComputeRealization computes the realization breakdown for the given time range.
func (engine *Engine) ComputeRealization(timeRange Range, filter *Filter) (RealizationBreakdown, error) {
	ledger, buildError := engine.buildLedger()
	if buildError != nil {
		return RealizationBreakdown{}, buildError
	}

	after, until := timeRange.bounds()
	var filterSet *filterSets
	if filter != nil && filter.Enabled {
		var prepareError error
		filterSet, prepareError = filter.prepare(engine.DB)
		if prepareError != nil {
			return RealizationBreakdown{}, prepareError
		}
	}

	var breakdown RealizationBreakdown
	accumulateRealizedRecords(&breakdown, ledger.records, after, until, filterSet)
	accumulateUnattributedSales(&breakdown, ledger.unattributedSales, after, until, filterSet)
	return breakdown, nil
}

func accumulateRealizedRecords(
	breakdown *RealizationBreakdown, records []*scanRecord, after, until string, filterSet *filterSets,
) {
	firstRecordIndex := sort.Search(len(records), func(recordIndex int) bool {
		return records[recordIndex].timestamp > after
	})
	for recordIndex := firstRecordIndex; recordIndex < len(records); recordIndex++ {
		scanEntry := records[recordIndex]
		if scanEntry.timestamp > until {
			break
		}
		if filterSet != nil && !filterSet.allow(scanEntry.timestamp, true) {
			continue
		}
		if scanEntry.isOrganic {
			breakdown.OrganicLostDeath += scanEntry.deathLoss
			breakdown.OrganicStolen += scanEntry.stolenLoss
		} else {
			breakdown.CartoLostDeath += scanEntry.deathLoss
			breakdown.CartoStolen += scanEntry.stolenLoss
		}
		breakdown.TotalRealized += scanEntry.actualNet
		breakdown.CarrierTariffLoss += scanEntry.carrierTariffLoss
		breakdown.NpcCrewLoss += scanEntry.crewLoss
		if scanEntry.status == "pending" {
			breakdown.PendingValue += scanEntry.theoreticalTotal
		}
	}
}

func accumulateUnattributedSales(
	breakdown *RealizationBreakdown, sales []unattributedSale, after, until string, filterSet *filterSets,
) {
	firstSaleIndex := sort.Search(len(sales), func(saleIndex int) bool {
		return sales[saleIndex].timestamp > after
	})
	for saleIndex := firstSaleIndex; saleIndex < len(sales); saleIndex++ {
		saleEntry := sales[saleIndex]
		if saleEntry.timestamp > until {
			break
		}
		if filterSet != nil && !filterSet.allow(saleEntry.timestamp, true) {
			continue
		}
		breakdown.TotalRealized += saleEntry.netRealized
		breakdown.CarrierTariffLoss += saleEntry.carrierTariffLoss
		breakdown.NpcCrewLoss += saleEntry.crewLoss
	}
}

func parseUnixSeconds(timestamp string) (int64, bool) {
	parsedTime, parseError := ed.ParseISO(timestamp)
	if parseError != nil {
		return 0, false
	}
	return parsedTime.Unix(), true
}

var (
	ledgerCacheMu      sync.RWMutex
	cachedLedger       *RealizationLedger
	cachedDB           db.Queryer
	cachedOdysseyState bool
)

// InvalidateLedgerCache clears the in-memory cached realization ledger.
func InvalidateLedgerCache() {
	ledgerCacheMu.Lock()
	cachedLedger = nil
	cachedDB = nil
	ledgerCacheMu.Unlock()
}

// buildLedger returns the cached lifecycle ledger or constructs a new one.
func (engine *Engine) buildLedger() (*RealizationLedger, error) {
	ledgerCacheMu.RLock()
	if cachedLedger != nil && cachedDB == engine.DB && cachedOdysseyState == engine.Odyssey {
		ledger := cachedLedger
		ledgerCacheMu.RUnlock()
		return ledger, nil
	}
	ledgerCacheMu.RUnlock()

	ledgerCacheMu.Lock()
	defer ledgerCacheMu.Unlock()
	if cachedLedger != nil && cachedDB == engine.DB && cachedOdysseyState == engine.Odyssey {
		return cachedLedger, nil
	}

	freshLedger, constructError := engine.constructLedger()
	if constructError != nil {
		return nil, constructError
	}
	cachedLedger = freshLedger
	cachedDB = engine.DB
	cachedOdysseyState = engine.Odyssey
	return freshLedger, nil
}
