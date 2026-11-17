// Package realization builds and caches the exploration scan-to-sale accounting
// ledger connecting scans, mappings, and organic samples with redemptions and losses.
package realization

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

var (
	salesLedgerJournalEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventDocked,
		journal.EventUndocked,
		journal.EventScan,
		journal.EventSAAScanComplete,
		journal.EventScanOrganic,
		journal.EventSellExplorationData,
		journal.EventMultiSellExplorationData,
		journal.EventSellOrganicData,
		journal.EventResurrect,
		journal.EventNpcCrewPaidWage,
	)
	salesLedgerAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppDatabaseSwitched,
	)
)

// Breakdown contains the realized net earnings and informational deduction
// breakdown for a time window (for example, an expedition waypoint leg or
// overall expedition).
type Breakdown struct {
	TotalRealized     int64 `json:"totalRealized"`     // Net credits pocketed across cartographics and exobiology sales
	CartoLostDeath    int64 `json:"cartoLostDeath"`    // Cartographics lost on ship destruction
	OrganicLostDeath  int64 `json:"organicLostDeath"`  // Exobiology lost on any death
	CartoStolen       int64 `json:"cartoStolen"`       // Cartographics discovery bonuses claimed by someone else first
	OrganicStolen     int64 `json:"organicStolen"`     // Exobiology first-logged bonuses claimed by someone else first
	CarrierTariffLoss int64 `json:"carrierTariffLoss"` // Universal Cartographics 25% tariff when selling on a Fleet Carrier
	NpcCrewLoss       int64 `json:"npcCrewLoss"`       // Active or inactive NPC crew wage deductions
	PendingValue      int64 `json:"pendingValue"`      // Estimated value still in transit (unsold and not lost)
}

// Add returns the field-wise sum of base and other.
func (base Breakdown) Add(other Breakdown) Breakdown {
	return Breakdown{
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

// OrganicSaleOutcome records the Vista Genomics sale metadata paired with a
// completed ScanOrganic sample so exobiology estimates stay synchronized.
type OrganicSaleOutcome struct {
	SaleTimestamp string
	SaleMarketID  int64
	BonusRealized bool
}

type bodyKey struct {
	systemAddress int64
	bodyID        int64
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

type unattributedSale struct {
	timestamp         string
	kind              string // "carto" or "organic"
	netRealized       int64
	crewLoss          int64
	carrierTariffLoss int64
}

// Ledger tracks all scans and their final realization outcomes across a commander's history.
type Ledger struct {
	records           []*scanRecord
	organicSales      map[string]OrganicSaleOutcome
	unattributedSales []unattributedSale
}

// OrganicSaleDetails returns realized Vista Genomics sale metadata for a
// ScanOrganic event_uid, including BonusRealized (required because of Frontier
// Issue #81891 and for backwards compatibility with older journal files).
func (ledger *Ledger) OrganicSaleDetails(eventUID string) (OrganicSaleOutcome, bool) {
	if ledger == nil || eventUID == "" {
		return OrganicSaleOutcome{}, false
	}
	outcome, found := ledger.organicSales[eventUID]
	return outcome, found
}

// Breakdown aggregates realized earnings, losses, and pending transit value
// for all records falling inside (afterTimestamp, untilTimestamp].
func (ledger *Ledger) Breakdown(
	afterTimestamp, untilTimestamp string,
	allowDiscovery func(timestamp string) bool,
) Breakdown {
	if ledger == nil {
		return Breakdown{}
	}
	if untilTimestamp == "" {
		untilTimestamp = "9999-12-31T23:59:59Z"
	}
	var breakdown Breakdown
	accumulateRealizedRecords(&breakdown, ledger.records, afterTimestamp, untilTimestamp, allowDiscovery)
	accumulateUnattributedSales(&breakdown, ledger.unattributedSales, afterTimestamp, untilTimestamp, allowDiscovery)
	return breakdown
}

// SalesLedger caches the all-time exploration scan-to-sale accounting ledger
// matching body scans and organic samples against Universal Cartographics and
// Vista Genomics payouts, carrier tariffs, NPC crew cuts, stolen bonuses, and
// death losses.
type SalesLedger struct {
	database *sql.DB
	ledger   *projection.Cache[*Ledger]
}

// NewSalesLedger creates a SalesLedger projection and registers it on bus.
func NewSalesLedger(dbConn *sql.DB, bus *projection.Bus) *SalesLedger {
	salesLedger := &SalesLedger{database: dbConn}
	salesLedger.ledger = projection.NewCache(
		bus,
		"sales_ledger",
		salesLedger.Triggers(),
		func() (*Ledger, error) {
			if salesLedger.database == nil {
				return nil, fmt.Errorf("sales ledger database is nil")
			}
			return BuildLedger(salesLedger.database, true)
		},
	)
	return salesLedger
}

// Triggers declares the journal and application events that invalidate SalesLedger.
func (salesLedger *SalesLedger) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: salesLedgerJournalEvents,
		App:     salesLedgerAppEvents,
	}
}

// Get returns the cached all-time realization ledger.
func (salesLedger *SalesLedger) Get() (*Ledger, error) {
	return salesLedger.ledger.Get()
}

// IsValid reports whether the all-time realization ledger is currently cached.
func (salesLedger *SalesLedger) IsValid() bool {
	return salesLedger.ledger.IsValid()
}

// Generation returns the cache invalidation generation counter.
func (salesLedger *SalesLedger) Generation() uint64 {
	return salesLedger.ledger.Generation()
}

// Breakdown returns the realization breakdown within (afterTimestamp, untilTimestamp]
// filtered by allowDiscovery when non-nil.
func (salesLedger *SalesLedger) Breakdown(
	afterTimestamp, untilTimestamp string,
	allowDiscovery func(timestamp string) bool,
) (Breakdown, error) {
	cachedLedger, loadError := salesLedger.Get()
	if loadError != nil {
		return Breakdown{}, loadError
	}
	return cachedLedger.Breakdown(afterTimestamp, untilTimestamp, allowDiscovery), nil
}

// OrganicSaleDetails looks up realized Vista Genomics sale metadata for one ScanOrganic event UID.
func (salesLedger *SalesLedger) OrganicSaleDetails(eventUID string) (OrganicSaleOutcome, bool, error) {
	cachedLedger, loadError := salesLedger.Get()
	if loadError != nil {
		return OrganicSaleOutcome{}, false, loadError
	}
	outcome, found := cachedLedger.OrganicSaleDetails(eventUID)
	return outcome, found, nil
}

// normalizeSpecies resolves a canonical lowercase species name from Frontier's
// journal fields, where codexID uses token syntax such as
// "$Codex_Ent_Stratum_07_Name;". It checks the exobiology species catalogue
// first, then falls back to the trimmed localised name, and finally strips
// leading and trailing "$" and ";" delimiters from the raw codex token.
func normalizeSpecies(codexID, localised string) string {
	if speciesEntry, ok := reference.LookupSpecies(codexID, localised); ok {
		return strings.ToLower(speciesEntry.Name)
	}
	if trimmedLocalised := strings.TrimSpace(localised); trimmedLocalised != "" {
		return strings.ToLower(trimmedLocalised)
	}
	trimmedCodexID := strings.Trim(strings.TrimSpace(codexID), "$;")
	return strings.ToLower(trimmedCodexID)
}

func normalizeVariant(codexID, localised string) string {
	if trimmedLocalised := strings.TrimSpace(localised); trimmedLocalised != "" {
		return strings.ToLower(trimmedLocalised)
	}
	trimmedCodexID := strings.Trim(strings.TrimSpace(codexID), "$;")
	return strings.ToLower(trimmedCodexID)
}

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

func accumulateRealizedRecords(
	breakdown *Breakdown, records []*scanRecord, after, until string, allowDiscovery func(string) bool,
) {
	firstRecordIndex := sort.Search(len(records), func(recordIndex int) bool {
		return records[recordIndex].timestamp > after
	})
	for recordIndex := firstRecordIndex; recordIndex < len(records); recordIndex++ {
		scanEntry := records[recordIndex]
		if scanEntry.timestamp > until {
			break
		}
		if allowDiscovery != nil && !allowDiscovery(scanEntry.timestamp) {
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
	breakdown *Breakdown, sales []unattributedSale, after, until string, allowDiscovery func(string) bool,
) {
	firstSaleIndex := sort.Search(len(sales), func(saleIndex int) bool {
		return sales[saleIndex].timestamp > after
	})
	for saleIndex := firstSaleIndex; saleIndex < len(sales); saleIndex++ {
		saleEntry := sales[saleIndex]
		if saleEntry.timestamp > until {
			break
		}
		if allowDiscovery != nil && !allowDiscovery(saleEntry.timestamp) {
			continue
		}
		breakdown.TotalRealized += saleEntry.netRealized
		breakdown.CarrierTariffLoss += saleEntry.carrierTariffLoss
		breakdown.NpcCrewLoss += saleEntry.crewLoss
	}
}

func parseUnixSeconds(timestamp string) (int64, bool) {
	parsedTime, parseError := reference.ParseISO(timestamp)
	if parseError != nil {
		return 0, false
	}
	return parsedTime.Unix(), true
}
