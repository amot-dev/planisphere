package stats

import (
	"encoding/json"
	"strings"
	"sync"

	"planisphere/internal/ed"
	"planisphere/internal/journal"
)

// RealizationBreakdown contains the realized earnings and loss breakdown
// for a time window (e.g. an expedition waypoint leg or overall expedition).
type RealizationBreakdown struct {
	TotalRealized    int64 `json:"totalRealized"`    // Combined actual total sold (cartographics + exobiology gross payout received)
	CartoLostDeath   int64 `json:"cartoLostDeath"`   // Cartographics lost (death)
	OrganicLostDeath int64 `json:"organicLostDeath"` // Exobiology lost (death)
	CartoStolen      int64 `json:"cartoStolen"`      // Cartographics stolen (bonuses submitted by someone else first)
	OrganicStolen    int64 `json:"organicStolen"`    // Exobiology stolen (bonuses submitted by someone else first)
	NpcCrewLoss      int64 `json:"npcCrewLoss"`      // NPC crew payouts (combined carto/exo)
	PendingValue     int64 `json:"pendingValue"`     // Still in-transit (unsold and not lost)
}

// Add returns the field-wise sum of two breakdowns.
func (base RealizationBreakdown) Add(other RealizationBreakdown) RealizationBreakdown {
	return RealizationBreakdown{
		TotalRealized:    base.TotalRealized + other.TotalRealized,
		CartoLostDeath:   base.CartoLostDeath + other.CartoLostDeath,
		OrganicLostDeath: base.OrganicLostDeath + other.OrganicLostDeath,
		CartoStolen:      base.CartoStolen + other.CartoStolen,
		OrganicStolen:    base.OrganicStolen + other.OrganicStolen,
		NpcCrewLoss:      base.NpcCrewLoss + other.NpcCrewLoss,
		PendingValue:     base.PendingValue + other.PendingValue,
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
	undiscoveredAtScan   bool
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
	status        string // "pending", "sold", "lost_death"
	actualBase    int64
	actualBonus   int64
	actualGross   int64
	deathLoss     int64
	stolenLoss    int64
	crewLoss      int64
	soldTimestamp string
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

// RealizationLedger tracks all scans and their final realization outcomes.
type RealizationLedger struct {
	records []*scanRecord
	// realizedOrganicBonus maps ScanOrganic event_uid to true when a sample
	// subsequently sold in SellOrganicData with a positive first-logged bonus.
	realizedOrganicBonus map[string]bool
	// Unattributed sales that couldn't be matched to a preceding scan
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
	return ledger.realizedOrganicBonus[eventUID]
}

type unattributedSale struct {
	timestamp string
	kind      string // "carto" or "organic"
	gross     int64
	crewLoss  int64
}

var realizationEvents = []string{
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

	for _, scanEntry := range ledger.records {
		if scanEntry.timestamp <= after || scanEntry.timestamp > until {
			continue
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
		breakdown.TotalRealized += scanEntry.actualGross
		breakdown.NpcCrewLoss += scanEntry.crewLoss

		if scanEntry.status == "pending" {
			breakdown.PendingValue += scanEntry.theoreticalTotal
		}
	}

	// Include unattributed sales whose sale timestamp falls into the window
	for _, sale := range ledger.unattributedSales {
		if sale.timestamp <= after || sale.timestamp > until {
			continue
		}
		if filterSet != nil && !filterSet.allow(sale.timestamp, true) {
			continue
		}
		breakdown.TotalRealized += sale.gross
		breakdown.NpcCrewLoss += sale.crewLoss
	}

	return breakdown, nil
}

func parseUnixSeconds(timestamp string) (int64, bool) {
	parsedTime, parseError := ed.ParseISO(timestamp)
	if parseError != nil {
		return 0, false
	}
	return parsedTime.Unix(), true
}

var (
	ledgerCacheMu sync.RWMutex
	cachedLedger  *RealizationLedger
	cachedDB      any
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
	if cachedLedger != nil && cachedDB == engine.DB {
		ledger := cachedLedger
		ledgerCacheMu.RUnlock()
		return ledger, nil
	}
	ledgerCacheMu.RUnlock()

	ledgerCacheMu.Lock()
	defer ledgerCacheMu.Unlock()
	if cachedLedger != nil && cachedDB == engine.DB {
		return cachedLedger, nil
	}

	freshLedger, constructError := engine.constructLedger()
	if constructError != nil {
		return nil, constructError
	}
	cachedLedger = freshLedger
	cachedDB = engine.DB
	return freshLedger, nil
}

// constructLedger builds the full chronological lifecycle ledger from journal events.
func (engine *Engine) constructLedger() (*RealizationLedger, error) {
	events, loadError := engine.loadEvents("", "9999-12-31T23:59:59Z", realizationEvents)
	if loadError != nil {
		return nil, loadError
	}

	ledger := &RealizationLedger{
		records:              make([]*scanRecord, 0, len(events)/2),
		realizedOrganicBonus: make(map[string]bool),
	}

	type cartoKey struct {
		systemAddress int64
		bodyID        int64
	}

	type parsedWageEvent struct {
		unixSeconds int64
		amount      int64
	}

	unsoldCarto := make(map[cartoKey]*scanRecord)
	unsoldOrganic := make([]*scanRecord, 0, 100)
	recentWageEvents := make([]parsedWageEvent, 0, 50)

	// First pass: collect wage events so they can be matched with concurrent sales
	for _, journalEvent := range events {
		if journalEvent.eventType == "NpcCrewPaidWage" {
			var wageEvent journal.NpcCrewPaidWageEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &wageEvent) == nil {
				if unixSeconds, ok := parseUnixSeconds(wageEvent.Timestamp); ok {
					recentWageEvents = append(recentWageEvents, parsedWageEvent{
						unixSeconds: unixSeconds,
						amount:      wageEvent.Amount,
					})
				}
			}
		}
	}

	findCrewWage := func(saleTimestamp string) int64 {
		saleUnixSeconds, ok := parseUnixSeconds(saleTimestamp)
		if !ok {
			return 0
		}
		var totalWage int64
		for _, wageEvent := range recentWageEvents {
			diffSeconds := wageEvent.unixSeconds - saleUnixSeconds
			if diffSeconds >= -5 && diffSeconds <= 5 {
				totalWage += wageEvent.amount
			}
		}
		return totalWage
	}

	type saleDedupKey struct {
		kind       string
		timestamp  string
		primaryID  int64
		itemCount  int
		totalValue int64
		totalBonus int64
	}

	scans := make(map[cartoKey]*journal.ScanEvent)
	seenSales := make(map[saleDedupKey]bool)

	for eventIndex := 0; eventIndex < len(events); eventIndex++ {
		journalEvent := events[eventIndex]
		switch journalEvent.eventType {
		case "Scan":
			var scanEv journal.ScanEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &scanEv) != nil || scanEv.IsBeltCluster() {
				continue
			}
			key := cartoKey{scanEv.SystemAddress, scanEv.BodyID}
			scanCopy := scanEv
			scans[key] = &scanCopy

			if scanEv.ScanType == "NavBeaconDetail" {
				continue
			}
			estimatedBodyValue := ed.EstimateBodyValue(toBodyScan(&scanEv), ed.MappingInfo{}, engine.Odyssey)
			scanEntry := &scanRecord{
				timestamp:            journalEvent.timestamp,
				systemAddress:        scanEv.SystemAddress,
				starSystem:           scanEv.StarSystem,
				normalizedStarSystem: strings.ToLower(strings.TrimSpace(scanEv.StarSystem)),
				bodyID:               scanEv.BodyID,
				bodyName:             scanEv.BodyName,
				normalizedBodyName:   strings.ToLower(strings.TrimSpace(scanEv.BodyName)),
				isOrganic:            false,
				undiscoveredAtScan:   !scanEv.WasDiscovered,
				theoreticalBase:      estimatedBodyValue.Base,
				theoreticalBonus:     estimatedBodyValue.Bonus,
				theoreticalTotal:     estimatedBodyValue.Total,
				status:               "pending",
			}
			unsoldCarto[key] = scanEntry
			ledger.records = append(ledger.records, scanEntry)

		case "SAAScanComplete":
			var saaEv journal.SAAScanCompleteEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &saaEv) != nil {
				continue
			}
			key := cartoKey{saaEv.SystemAddress, saaEv.BodyID}
			scan := scans[key]
			if scan == nil {
				continue
			}
			mappingInfo := ed.MappingInfo{Mapped: true, ProbesUsed: saaEv.ProbesUsed, EfficiencyTarget: saaEv.EfficiencyTarget}
			mappingIncrement := ed.EstimateMappingIncrement(toBodyScan(scan), mappingInfo, engine.Odyssey)

			scanEntry := unsoldCarto[key]
			if scanEntry != nil {
				scanEntry.theoreticalBase += mappingIncrement.Base
				scanEntry.theoreticalBonus += mappingIncrement.Bonus
				scanEntry.theoreticalTotal += mappingIncrement.Total
				if !scan.WasMapped {
					scanEntry.undiscoveredAtScan = true
				}
			} else {
				// Mapped without existing pending scan in current batch
				scanEntry = &scanRecord{
					timestamp:            journalEvent.timestamp,
					systemAddress:        saaEv.SystemAddress,
					starSystem:           scan.StarSystem,
					normalizedStarSystem: strings.ToLower(strings.TrimSpace(scan.StarSystem)),
					bodyID:               saaEv.BodyID,
					bodyName:             saaEv.BodyName,
					normalizedBodyName:   strings.ToLower(strings.TrimSpace(saaEv.BodyName)),
					isOrganic:            false,
					undiscoveredAtScan:   !scan.WasDiscovered || !scan.WasMapped,
					theoreticalBase:      mappingIncrement.Base,
					theoreticalBonus:     mappingIncrement.Bonus,
					theoreticalTotal:     mappingIncrement.Total,
					status:               "pending",
				}
				unsoldCarto[key] = scanEntry
				ledger.records = append(ledger.records, scanEntry)
			}

		case "ScanOrganic":
			var organicEv journal.ScanOrganicEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &organicEv) != nil || !strings.EqualFold(organicEv.ScanType, "Analyse") {
				continue
			}
			baseVal := ed.SpeciesBaseValue(organicEv.Species, organicEv.SpeciesLocalised, organicEv.Genus)
			parentKey := cartoKey{organicEv.SystemAddress, organicEv.Body}
			parentScan := scans[parentKey]
			// Naively check the parent body's Scan WasFootfalled == false only.
			// ScanOrganic.WasLogged is intentionally ignored because Frontier journals
			// do not reliably populate it: https://issues.frontierstore.net/issue-detail/81891
			firstLogged := parentScan != nil && parentScan.WasFootfalled != nil && !*parentScan.WasFootfalled
			calculatedOrganicValue := ed.OrganicValue(baseVal, firstLogged)
			scanEntry := &scanRecord{
				eventUID:           journalEvent.eventUID,
				timestamp:          journalEvent.timestamp,
				systemAddress:      organicEv.SystemAddress,
				bodyID:             organicEv.Body,
				isOrganic:          true,
				undiscoveredAtScan: firstLogged,
				species:            organicEv.Species,
				speciesLocalised:   organicEv.SpeciesLocalised,
				variant:            organicEv.Variant,
				variantLocalised:   organicEv.VariantLocalised,
				normalizedSpecies:  normalizeSpecies(organicEv.Species, organicEv.SpeciesLocalised),
				normalizedVariant:  normalizeVariant(organicEv.Variant, organicEv.VariantLocalised),
				theoreticalBase:    calculatedOrganicValue.Base,
				theoreticalBonus:   calculatedOrganicValue.Bonus,
				theoreticalTotal:   calculatedOrganicValue.Total,
				status:             "pending",
			}
			unsoldOrganic = append(unsoldOrganic, scanEntry)
			ledger.records = append(ledger.records, scanEntry)

		case "Resurrect":
			var resurrectEvent journal.ResurrectEvent
			if unmarshalError := json.Unmarshal([]byte(journalEvent.rawJSON), &resurrectEvent); unmarshalError != nil {
				continue
			}

			// Exobiology samples in suit are always lost upon any death and resurrection
			for _, organicRecord := range unsoldOrganic {
				if organicRecord.status == "pending" {
					organicRecord.status = "lost_death"
					organicRecord.deathLoss = organicRecord.theoreticalTotal
				}
			}
			unsoldOrganic = unsoldOrganic[:0]

			// Cartographic data is only lost on ship destruction
			if isShipDestructionResurrect(resurrectEvent) {
				for _, cartoRecord := range unsoldCarto {
					if cartoRecord.status == "pending" {
						cartoRecord.status = "lost_death"
						cartoRecord.deathLoss = cartoRecord.theoreticalTotal
					}
				}
				clear(unsoldCarto)
			}

		case "SellOrganicData":
			var sellEv journal.SellOrganicDataEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &sellEv) != nil {
				continue
			}
			var totalSaleValue, totalSaleBonus int64
			for _, item := range sellEv.BioData {
				totalSaleValue += item.Value
				totalSaleBonus += item.Bonus
			}
			totalSaleGross := totalSaleValue + totalSaleBonus
			saleKey := saleDedupKey{
				kind:       "organic",
				timestamp:  sellEv.Timestamp,
				primaryID:  sellEv.MarketID,
				itemCount:  len(sellEv.BioData),
				totalValue: totalSaleValue,
				totalBonus: totalSaleBonus,
			}
			if seenSales[saleKey] {
				continue
			}
			seenSales[saleKey] = true
			totalCrewWage := findCrewWage(sellEv.Timestamp)

			for _, item := range sellEv.BioData {
				itemGross := item.Value + item.Bonus
				var allocatedCrew int64
				if totalSaleGross > 0 && totalCrewWage > 0 {
					allocatedCrew = (totalCrewWage * itemGross) / totalSaleGross
				}

				// Find matching pending unsold specimen using robust species matching
				matchedIndex := findMatchingOrganic(unsoldOrganic, item)

				if matchedIndex >= 0 {
					matchedRecord := unsoldOrganic[matchedIndex]
					matchedRecord.status = "sold"
					matchedRecord.soldTimestamp = sellEv.Timestamp
					matchedRecord.actualBase = item.Value
					matchedRecord.actualBonus = item.Bonus
					matchedRecord.actualGross = itemGross
					matchedRecord.crewLoss = allocatedCrew

					// If scan-time estimation did not award a first-logged bonus (because the
					// parent body already had WasFootfalled == true), but Vista Genomics paid a
					// positive bonus upon sale, record that this ScanOrganic earned the bonus.
					// Required because of Frontier Issue #81891 and remains required for backwards
					// compatibility with older journal files even if or when
					// https://issues.frontierstore.net/issue-detail/81891 is fixed.
					if item.Bonus > 0 && matchedRecord.eventUID != "" {
						ledger.realizedOrganicBonus[matchedRecord.eventUID] = true
						if matchedRecord.theoreticalBonus == 0 {
							upgradedOrganicValue := ed.OrganicValue(matchedRecord.theoreticalBase, true)
							matchedRecord.theoreticalBonus = upgradedOrganicValue.Bonus
							matchedRecord.theoreticalTotal = upgradedOrganicValue.Total
						}
					}

					// Stolen check: only flag as stolen if the body was undiscovered at the time of scan
					// and the player expected a first-logged bonus, but received none.
					if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 && item.Bonus == 0 {
						matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
					}

					// Remove from unsold queue
					unsoldOrganic = append(unsoldOrganic[:matchedIndex], unsoldOrganic[matchedIndex+1:]...)
				} else {
					// Unattributed organic sale
					ledger.unattributedSales = append(ledger.unattributedSales, unattributedSale{
						timestamp: sellEv.Timestamp,
						kind:      "organic",
						gross:     itemGross,
						crewLoss:  allocatedCrew,
					})
				}
			}

		case "SellExplorationData":
			var sellEv journal.SellExplorationDataEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &sellEv) != nil {
				continue
			}
			saleKey := saleDedupKey{
				kind:       "carto",
				timestamp:  sellEv.Timestamp,
				primaryID:  sellEv.TotalEarnings,
				itemCount:  len(sellEv.Systems),
				totalValue: sellEv.BaseValue,
				totalBonus: sellEv.Bonus,
			}
			if seenSales[saleKey] {
				continue
			}
			seenSales[saleKey] = true
			totalCrewWage := findCrewWage(sellEv.Timestamp)
			discoveredSet := make(map[string]bool, len(sellEv.Discovered))
			for _, discoveredBody := range sellEv.Discovered {
				discoveredSet[strings.ToLower(strings.TrimSpace(discoveredBody))] = true
			}

			soldSystemSet := make(map[string]bool, len(sellEv.Systems))
			for _, soldSystemName := range sellEv.Systems {
				soldSystemSet[strings.ToLower(strings.TrimSpace(soldSystemName))] = true
			}

			// Find matching unsold carto records for sold systems
			matchedRecords := make([]*scanRecord, 0, len(unsoldCarto))
			for key, candidateRecord := range unsoldCarto {
				if candidateRecord.status == "pending" && candidateRecord.timestamp <= sellEv.Timestamp {
					if len(soldSystemSet) == 0 || soldSystemSet[candidateRecord.normalizedStarSystem] {
						matchedRecords = append(matchedRecords, candidateRecord)
						delete(unsoldCarto, key)
					}
				}
			}

			if len(matchedRecords) > 0 {
				allocateCartoSalePayout(matchedRecords, sellEv.Timestamp, sellEv.TotalEarnings, sellEv.BaseValue, sellEv.Bonus, totalCrewWage)
				for _, matchedRecord := range matchedRecords {
					// Stolen check: had bonus expectation on an undiscovered body, but body or system was not awarded first discovery
					if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 {
						if !discoveredSet[matchedRecord.normalizedBodyName] && !discoveredSet[matchedRecord.normalizedStarSystem] {
							matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
						}
					}
				}
			} else {
				ledger.unattributedSales = append(ledger.unattributedSales, unattributedSale{
					timestamp: sellEv.Timestamp,
					kind:      "carto",
					gross:     sellEv.TotalEarnings,
					crewLoss:  totalCrewWage,
				})
			}

		case "MultiSellExplorationData":
			var sellEv journal.MultiSellExplorationDataEvent
			if json.Unmarshal([]byte(journalEvent.rawJSON), &sellEv) != nil {
				continue
			}
			saleKey := saleDedupKey{
				kind:       "carto_multi",
				timestamp:  sellEv.Timestamp,
				primaryID:  sellEv.TotalEarnings,
				itemCount:  len(sellEv.Discovered),
				totalValue: sellEv.BaseValue,
				totalBonus: sellEv.Bonus,
			}
			if seenSales[saleKey] {
				continue
			}
			seenSales[saleKey] = true
			totalCrewWage := findCrewWage(sellEv.Timestamp)

			soldSystemSet := make(map[string]bool, len(sellEv.Discovered))
			for _, discoveredEntry := range sellEv.Discovered {
				soldSystemSet[strings.ToLower(strings.TrimSpace(discoveredEntry.SystemName))] = true
			}

			// Match pending cartographic scans belonging to the systems sold on this page
			matchedRecords := make([]*scanRecord, 0, len(unsoldCarto))
			for key, candidateRecord := range unsoldCarto {
				if candidateRecord.status == "pending" && candidateRecord.timestamp <= sellEv.Timestamp {
					if len(soldSystemSet) == 0 || soldSystemSet[candidateRecord.normalizedStarSystem] {
						matchedRecords = append(matchedRecords, candidateRecord)
						delete(unsoldCarto, key)
					}
				}
			}

			if len(matchedRecords) > 0 {
				allocateCartoSalePayout(matchedRecords, sellEv.Timestamp, sellEv.TotalEarnings, sellEv.BaseValue, sellEv.Bonus, totalCrewWage)
				allocateMultiSellStolenLoss(matchedRecords, sellEv.Bonus)
			} else {
				ledger.unattributedSales = append(ledger.unattributedSales, unattributedSale{
					timestamp: sellEv.Timestamp,
					kind:      "carto",
					gross:     sellEv.TotalEarnings,
					crewLoss:  totalCrewWage,
				})
			}
		}
	}

	return ledger, nil
}

// isShipDestructionResurrect reports whether a Resurrect journal event
// represents respawning from ship destruction (which destroys unsold shipboard
// cartographic data) rather than recovering from an on-foot suit death or SRV
// destruction (which preserves shipboard cartographic data).
//
// Loss accounting triggers on Resurrect rather than Died because no scans or
// sales can occur while the commander is on the rebuy or redeployment screen,
// and Died events for on-foot environmental hazards (heat, cold, or oxygen
// depletion) omit killer identifiers. Non-ship recovery options ("recover",
// "rejoin", "escape", "handin", or "free" with zero cost and Bankrupt false)
// preserve cartographic data, including "handin" where Cost > 0 reflects an
// on-foot detention-center bounty fine rather than ship insurance. Ship
// replacement options ("rebuy", "freewinder", Bankrupt true, or positive
// insurance cost) mark unsold cartography as lost.
//
// Edge case: if a commander who can afford their ship insurance (Bankrupt is
// false) voluntarily declines rebuy to forfeit their ship for a free starter
// Sidewinder and the journal records Option "free" with Cost 0 instead of
// "freewinder", Resurrect alone cannot distinguish that voluntary forfeit from
// a free on-foot redeployment to orbit.
func isShipDestructionResurrect(resurrectEvent journal.ResurrectEvent) bool {
	option := strings.ToLower(strings.TrimSpace(resurrectEvent.Option))
	if option == "handin" || option == "recover" || option == "rejoin" || option == "escape" {
		return false
	}
	return option == "rebuy" || option == "freewinder" || resurrectEvent.Bankrupt || resurrectEvent.Cost > 0
}

// allocateCartoSalePayout distributes a cartographic sale's gross earnings,
// base value, discovery bonus, and concurrent NPC crew wage across all matched
// scan records proportionally by each record's theoreticalTotal (falling back
// to an equal split when totalTheoretical is zero). The final record in
// matchedRecords receives the unallocated remainder so integer truncation
// across individual records never causes the sum of realized payouts or crew
// wages to drift from the journal event totals.
func allocateCartoSalePayout(matchedRecords []*scanRecord, soldTimestamp string, totalEarnings, baseValue, bonusValue, totalCrewWage int64) {
	var totalTheoretical int64
	for _, matchedRecord := range matchedRecords {
		totalTheoretical += matchedRecord.theoreticalTotal
	}
	var allocatedGross int64
	var allocatedBase int64
	var allocatedBonus int64
	var allocatedCrew int64
	for recordIndex, matchedRecord := range matchedRecords {
		matchedRecord.status = "sold"
		matchedRecord.soldTimestamp = soldTimestamp
		allocationRatio := 1.0 / float64(len(matchedRecords))
		if totalTheoretical > 0 {
			allocationRatio = float64(matchedRecord.theoreticalTotal) / float64(totalTheoretical)
		}
		if recordIndex == len(matchedRecords)-1 {
			matchedRecord.actualGross = totalEarnings - allocatedGross
			matchedRecord.actualBase = baseValue - allocatedBase
			matchedRecord.actualBonus = bonusValue - allocatedBonus
			if totalCrewWage > 0 {
				matchedRecord.crewLoss = totalCrewWage - allocatedCrew
			}
			continue
		}
		matchedRecord.actualGross = int64(float64(totalEarnings) * allocationRatio)
		matchedRecord.actualBase = int64(float64(baseValue) * allocationRatio)
		matchedRecord.actualBonus = int64(float64(bonusValue) * allocationRatio)
		allocatedGross += matchedRecord.actualGross
		allocatedBase += matchedRecord.actualBase
		allocatedBonus += matchedRecord.actualBonus
		if totalCrewWage > 0 {
			matchedRecord.crewLoss = int64(float64(totalCrewWage) * allocationRatio)
			allocatedCrew += matchedRecord.crewLoss
		}
	}
}

// allocateMultiSellStolenLoss computes and distributes stolen cartographic
// bonus losses for a MultiSellExplorationData page. Because Frontier's
// MultiSellExplorationData event uses its Discovered array to list the star
// systems sold on the page rather than listing only first-discovered bodies,
// stolen discovery and mapping bonuses are measured by the shortfall between
// totalExpectedBonus across undiscovered scans on that page and actualBonus
// paid by Universal Cartographics. The shortfall is distributed proportionally
// by theoreticalBonus across those scans, with the final bonus-expecting scan
// receiving the remainder to avoid integer truncation drift.
func allocateMultiSellStolenLoss(matchedRecords []*scanRecord, actualBonus int64) {
	var totalExpectedBonus int64
	lastBonusRecordIndex := -1
	for recordIndex, matchedRecord := range matchedRecords {
		if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 {
			totalExpectedBonus += matchedRecord.theoreticalBonus
			lastBonusRecordIndex = recordIndex
		}
	}
	realizedBonus := max(actualBonus, int64(0))
	if totalExpectedBonus <= realizedBonus || lastBonusRecordIndex < 0 {
		return
	}
	stolenShortfall := totalExpectedBonus - realizedBonus
	var allocatedStolenLoss int64
	for recordIndex, matchedRecord := range matchedRecords {
		if !matchedRecord.undiscoveredAtScan || matchedRecord.theoreticalBonus <= 0 {
			continue
		}
		if recordIndex == lastBonusRecordIndex {
			matchedRecord.stolenLoss = stolenShortfall - allocatedStolenLoss
			break
		}
		bonusRatio := float64(matchedRecord.theoreticalBonus) / float64(totalExpectedBonus)
		matchedRecord.stolenLoss = int64(float64(stolenShortfall) * bonusRatio)
		allocatedStolenLoss += matchedRecord.stolenLoss
	}
}
