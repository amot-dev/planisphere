// Package realization builds the exploration scan-to-sale accounting ledger
// connecting scans, mappings, and organic samples with redemptions and losses.
package realization

import (
	"sort"
	"strings"

	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

// SalesLedgerReplayEvents includes session-context events (LoadGame, Location,
// FSDJump, CarrierJump, Docked, Undocked) needed during SQL replay alongside
// the events that create or settle ledger records.
var SalesLedgerReplayEvents = journal.NewEventSet(
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
