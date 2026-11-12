package stats

import (
	"cmp"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"planisphere/internal/ed/exceptions"
	"planisphere/internal/journal"
)

type parsedWageEvent struct {
	unixSeconds int64
	amount      int64
	consumed    bool
}

type crewWageMatcher struct {
	wageEvents []parsedWageEvent
}

// newCrewWageMatcher indexes all NpcCrewPaidWage events in chronological order
// so sales can binary-search and claim concurrent crew wage deductions once.
func newCrewWageMatcher(events []eventRow) *crewWageMatcher {
	wageEvents := make([]parsedWageEvent, 0, 50)
	for _, journalEvent := range events {
		if journalEvent.eventType != "NpcCrewPaidWage" {
			continue
		}
		var wageEvent journal.NpcCrewPaidWageEvent
		if json.Unmarshal([]byte(journalEvent.rawJSON), &wageEvent) != nil {
			continue
		}
		if unixSeconds, parsedOk := parseUnixSeconds(wageEvent.Timestamp); parsedOk {
			wageEvents = append(wageEvents, parsedWageEvent{
				unixSeconds: unixSeconds,
				amount:      wageEvent.Amount,
			})
		}
	}
	return &crewWageMatcher{wageEvents: wageEvents}
}

// claimWage matches and consumes NpcCrewPaidWage events within 5 seconds of
// saleTimestamp. Marking matched events as consumed prevents rapid consecutive
// sale pages from double-counting the same crew wage deduction.
func (matcher *crewWageMatcher) claimWage(saleTimestamp string) int64 {
	if len(matcher.wageEvents) == 0 {
		return 0
	}
	saleUnixSeconds, parsedOk := parseUnixSeconds(saleTimestamp)
	if !parsedOk {
		return 0
	}
	minimumWageUnixSeconds := saleUnixSeconds - 5
	maximumWageUnixSeconds := saleUnixSeconds + 5
	firstWageIndex := sort.Search(len(matcher.wageEvents), func(wageIndex int) bool {
		return matcher.wageEvents[wageIndex].unixSeconds >= minimumWageUnixSeconds
	})
	var totalWage int64
	for wageIndex := firstWageIndex; wageIndex < len(matcher.wageEvents); wageIndex++ {
		if matcher.wageEvents[wageIndex].unixSeconds > maximumWageUnixSeconds {
			break
		}
		if !matcher.wageEvents[wageIndex].consumed {
			matcher.wageEvents[wageIndex].consumed = true
			totalWage += matcher.wageEvents[wageIndex].amount
		}
	}
	return totalWage
}

// computeCarrierTariff calculates Universal Cartographics' 25% Fleet Carrier
// tariff from the journal's post-tariff BaseValue and Bonus fields. When docked
// on a Fleet Carrier (including the commander's own vessel), Frontier logs
// BaseValue + Bonus at exact 75% of pre-tariff scan value, making the 25%
// tariff equal to one third of (BaseValue + Bonus).
func computeCarrierTariff(isDockedOnFleetCarrier bool, baseValue, bonusValue int64) int64 {
	if !isDockedOnFleetCarrier {
		return 0
	}
	postTariffGross := baseValue + bonusValue
	if postTariffGross <= 0 {
		return 0
	}
	return postTariffGross / 3
}

func (builder *ledgerBuilder) recordOrganicSale(rawJSON string) {
	var sellOrganicEvent journal.SellOrganicDataEvent
	if json.Unmarshal([]byte(rawJSON), &sellOrganicEvent) != nil {
		return
	}
	var totalSaleValue, totalSaleBonus int64
	for _, bioItem := range sellOrganicEvent.BioData {
		totalSaleValue += bioItem.Value
		totalSaleBonus += bioItem.Bonus
	}
	saleKey := saleDedupKey{
		kind:       "organic",
		timestamp:  sellOrganicEvent.Timestamp,
		primaryID:  sellOrganicEvent.MarketID,
		itemCount:  len(sellOrganicEvent.BioData),
		totalValue: totalSaleValue,
		totalBonus: totalSaleBonus,
	}
	if builder.seenSales[saleKey] {
		return
	}
	builder.seenSales[saleKey] = true

	totalSaleGross := totalSaleValue + totalSaleBonus
	totalCrewWage := builder.wageMatcher.claimWage(sellOrganicEvent.Timestamp)
	var allocatedEventCrew int64

	for itemIndex, bioItem := range sellOrganicEvent.BioData {
		itemCrewWage := computeOrganicItemCrewShare(
			itemIndex, len(sellOrganicEvent.BioData), bioItem.Value+bioItem.Bonus,
			totalSaleGross, totalCrewWage, &allocatedEventCrew,
		)
		remainingSameSpecies := countRemainingSameSpeciesItems(sellOrganicEvent.BioData, itemIndex)
		builder.applyOrganicSaleItem(sellOrganicEvent, bioItem, itemCrewWage, remainingSameSpecies)
	}
}

func computeOrganicItemCrewShare(
	itemIndex, itemCount int, itemGross, totalSaleGross, totalCrewWage int64, allocatedSoFar *int64,
) int64 {
	if totalCrewWage <= 0 || totalSaleGross <= 0 {
		return 0
	}
	if itemIndex == itemCount-1 {
		return totalCrewWage - *allocatedSoFar
	}
	share := (totalCrewWage * itemGross) / totalSaleGross
	*allocatedSoFar += share
	return share
}

func countRemainingSameSpeciesItems(bioItems []journal.BioDataItem, currentIndex int) int {
	currentSpecies := normalizeSpecies(bioItems[currentIndex].Species, bioItems[currentIndex].SpeciesLocalised)
	if currentSpecies == "" {
		return 0
	}
	remainingCount := 0
	for laterIndex := currentIndex + 1; laterIndex < len(bioItems); laterIndex++ {
		if normalizeSpecies(bioItems[laterIndex].Species, bioItems[laterIndex].SpeciesLocalised) == currentSpecies {
			remainingCount++
		}
	}
	return remainingCount
}

func (builder *ledgerBuilder) applyOrganicSaleItem(
	sellEvent journal.SellOrganicDataEvent, bioItem journal.BioDataItem, itemCrewWage int64, remainingSameSpecies int,
) {
	claimedIndices := findMatchingOrganicBatch(
		builder.unsoldOrganic, bioItem, sellEvent.Timestamp, sellEvent.MarketID, remainingSameSpecies,
	)
	if len(claimedIndices) == 0 {
		itemGross := bioItem.Value + bioItem.Bonus
		builder.ledger.unattributedSales = append(builder.ledger.unattributedSales, unattributedSale{
			timestamp:   sellEvent.Timestamp,
			kind:        "organic",
			netRealized: itemGross - itemCrewWage,
			crewLoss:    itemCrewWage,
		})
		return
	}
	allocateOrganicBatchPayout(builder.ledger, builder.unsoldOrganic, claimedIndices, sellEvent, bioItem, itemCrewWage)
	builder.removeSoldOrganicRecords(claimedIndices)
}

// findMatchingOrganicBatch selects one pending ScanOrganic record for normal
// Vista Genomics sales, or multiple pending records when Frontier aggregates
// repeated scans of the same species into a single BioData entry (such as
// Radicoida Unica Community Goal deliveries).
func findMatchingOrganicBatch(
	unsoldRecords []*scanRecord, bioItem journal.BioDataItem, saleTimestamp string, saleMarketID int64, remainingSameSpecies int,
) []int {
	bestMatchIndex := findMatchingOrganic(unsoldRecords, bioItem)
	if bestMatchIndex < 0 {
		return nil
	}
	bestRecord := unsoldRecords[bestMatchIndex]
	unitBaseValue := exceptions.EvaluateOrganic(exceptions.OrganicContext{
		ScanTimestamp:    bestRecord.timestamp,
		SaleTimestamp:    saleTimestamp,
		SystemAddress:    bestRecord.systemAddress,
		SaleMarketID:     saleMarketID,
		SpeciesCodex:     bestRecord.species,
		SpeciesLocalised: bestRecord.speciesLocalised,
		GenusCodex:       bestRecord.genus,
	}).Base
	if unitBaseValue <= 0 || bioItem.Value < unitBaseValue*2 {
		return []int{bestMatchIndex}
	}

	candidateIndices := rankMatchingOrganicIndices(unsoldRecords, bioItem)
	maxClaimable := len(candidateIndices) - remainingSameSpecies
	if maxClaimable <= 1 {
		return []int{bestMatchIndex}
	}
	estimatedSampleCount := int((bioItem.Value + unitBaseValue/2) / unitBaseValue)
	claimCount := min(max(estimatedSampleCount, 1), maxClaimable)
	return candidateIndices[:claimCount]
}

func rankMatchingOrganicIndices(unsoldRecords []*scanRecord, bioItem journal.BioDataItem) []int {
	soldSpecies := normalizeSpecies(bioItem.Species, bioItem.SpeciesLocalised)
	soldVariant := normalizeVariant(bioItem.Variant, bioItem.VariantLocalised)
	expectedBonus := bioItem.Bonus > 0

	type scoredCandidate struct {
		recordIndex int
		tierScore   int
	}
	candidates := make([]scoredCandidate, 0, len(unsoldRecords))
	for recordIndex, candidateRecord := range unsoldRecords {
		if tierScore := organicCandidateTier(candidateRecord, soldSpecies, soldVariant, expectedBonus); tierScore > 0 {
			candidates = append(candidates, scoredCandidate{recordIndex: recordIndex, tierScore: tierScore})
		}
	}
	slices.SortStableFunc(candidates, func(leftCandidate, rightCandidate scoredCandidate) int {
		return cmp.Or(
			cmp.Compare(rightCandidate.tierScore, leftCandidate.tierScore),
			cmp.Compare(leftCandidate.recordIndex, rightCandidate.recordIndex),
		)
	})
	rankedIndices := make([]int, len(candidates))
	for candidatePosition, candidate := range candidates {
		rankedIndices[candidatePosition] = candidate.recordIndex
	}
	return rankedIndices
}

func allocateOrganicBatchPayout(
	ledger *RealizationLedger, unsoldRecords []*scanRecord, claimedIndices []int,
	sellEvent journal.SellOrganicDataEvent, bioItem journal.BioDataItem, itemCrewWage int64,
) {
	batchSize := int64(len(claimedIndices))
	var allocatedBase, allocatedBonus, allocatedCrew int64
	for batchPosition, recordIndex := range claimedIndices {
		matchedRecord := unsoldRecords[recordIndex]
		shareBase := bioItem.Value / batchSize
		shareBonus := bioItem.Bonus / batchSize
		shareCrew := itemCrewWage / batchSize
		if batchPosition == len(claimedIndices)-1 {
			shareBase = bioItem.Value - allocatedBase
			shareBonus = bioItem.Bonus - allocatedBonus
			shareCrew = itemCrewWage - allocatedCrew
		} else {
			allocatedBase += shareBase
			allocatedBonus += shareBonus
			allocatedCrew += shareCrew
		}
		finalizeSoldOrganicRecord(ledger, matchedRecord, sellEvent, shareBase, shareBonus, shareCrew)
	}
}

func finalizeSoldOrganicRecord(
	ledger *RealizationLedger, matchedRecord *scanRecord, sellEvent journal.SellOrganicDataEvent,
	shareBase, shareBonus, shareCrew int64,
) {
	matchedRecord.status = "sold"
	matchedRecord.soldTimestamp = sellEvent.Timestamp
	matchedRecord.actualBase = shareBase
	matchedRecord.actualBonus = shareBonus
	matchedRecord.crewLoss = shareCrew
	matchedRecord.actualNet = (shareBase + shareBonus) - shareCrew

	if matchedRecord.eventUID != "" {
		ledger.organicSales[matchedRecord.eventUID] = organicSaleOutcome{
			saleTimestamp: sellEvent.Timestamp,
			saleMarketID:  sellEvent.MarketID,
			bonusRealized: shareBonus > 0,
		}
	}

	effectiveFirstLogged := matchedRecord.undiscoveredAtScan || shareBonus > 0
	resolvedValuation := exceptions.EvaluateOrganic(exceptions.OrganicContext{
		ScanTimestamp:    matchedRecord.timestamp,
		SaleTimestamp:    sellEvent.Timestamp,
		SystemAddress:    matchedRecord.systemAddress,
		SaleMarketID:     sellEvent.MarketID,
		SpeciesCodex:     matchedRecord.species,
		SpeciesLocalised: matchedRecord.speciesLocalised,
		GenusCodex:       matchedRecord.genus,
		FirstLogged:      effectiveFirstLogged,
	})
	matchedRecord.theoreticalBase = resolvedValuation.Base
	matchedRecord.theoreticalBonus = resolvedValuation.Bonus
	matchedRecord.theoreticalTotal = resolvedValuation.Total

	if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 && shareBonus == 0 {
		matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
	}
}

func (builder *ledgerBuilder) removeSoldOrganicRecords(claimedIndices []int) {
	if len(claimedIndices) == 1 {
		singleIndex := claimedIndices[0]
		builder.unsoldOrganic = append(builder.unsoldOrganic[:singleIndex], builder.unsoldOrganic[singleIndex+1:]...)
		return
	}
	remaining := builder.unsoldOrganic[:0]
	for _, record := range builder.unsoldOrganic {
		if record.status == "pending" {
			remaining = append(remaining, record)
		}
	}
	builder.unsoldOrganic = remaining
}

func (builder *ledgerBuilder) recordSingleCartoSale(rawJSON string) {
	var sellEvent journal.SellExplorationDataEvent
	if json.Unmarshal([]byte(rawJSON), &sellEvent) != nil {
		return
	}
	saleKey := saleDedupKey{
		kind:       "carto",
		timestamp:  sellEvent.Timestamp,
		primaryID:  sellEvent.TotalEarnings,
		itemCount:  len(sellEvent.Systems),
		totalValue: sellEvent.BaseValue,
		totalBonus: sellEvent.Bonus,
	}
	if builder.seenSales[saleKey] {
		return
	}
	builder.seenSales[saleKey] = true

	totalCrewWage := builder.wageMatcher.claimWage(sellEvent.Timestamp)
	carrierTariff := computeCarrierTariff(builder.session.isDockedOnFleetCarrier, sellEvent.BaseValue, sellEvent.Bonus)
	soldSystemSet := buildNormalizedSet(sellEvent.Systems)
	matchedRecords := builder.claimSoldCartoRecords(soldSystemSet, sellEvent.Timestamp)

	if len(matchedRecords) == 0 {
		builder.ledger.unattributedSales = append(builder.ledger.unattributedSales, unattributedSale{
			timestamp:         sellEvent.Timestamp,
			kind:              "carto",
			netRealized:       sellEvent.TotalEarnings,
			crewLoss:          totalCrewWage,
			carrierTariffLoss: carrierTariff,
		})
		return
	}

	allocateCartoSalePayout(matchedRecords, sellEvent.Timestamp, sellEvent.TotalEarnings, sellEvent.BaseValue, sellEvent.Bonus, totalCrewWage, carrierTariff)
	discoveredSet := buildNormalizedSet(sellEvent.Discovered)
	for _, matchedRecord := range matchedRecords {
		if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 {
			if !discoveredSet[matchedRecord.normalizedBodyName] && !discoveredSet[matchedRecord.normalizedStarSystem] {
				matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
			}
		}
	}
}

// recordMultiCartoSale records a bulk MultiSellExplorationData page. Unlike
// SellExplorationData, Frontier bakes First Discovery and First Mapped bonuses
// directly into BaseValue on bulk pages and uses Bonus exclusively for flat
// system-completion bonuses, so stolen-bonus shortfalls cannot be inferred from
// MultiSellExplorationData.Bonus.
func (builder *ledgerBuilder) recordMultiCartoSale(rawJSON string) {
	var multiSellEvent journal.MultiSellExplorationDataEvent
	if json.Unmarshal([]byte(rawJSON), &multiSellEvent) != nil {
		return
	}
	saleKey := saleDedupKey{
		kind:       "carto_multi",
		timestamp:  multiSellEvent.Timestamp,
		primaryID:  multiSellEvent.TotalEarnings,
		itemCount:  len(multiSellEvent.Discovered),
		totalValue: multiSellEvent.BaseValue,
		totalBonus: multiSellEvent.Bonus,
	}
	if builder.seenSales[saleKey] {
		return
	}
	builder.seenSales[saleKey] = true

	totalCrewWage := builder.wageMatcher.claimWage(multiSellEvent.Timestamp)
	carrierTariff := computeCarrierTariff(builder.session.isDockedOnFleetCarrier, multiSellEvent.BaseValue, multiSellEvent.Bonus)
	soldSystemSet := make(map[string]bool, len(multiSellEvent.Discovered))
	for _, discoveredEntry := range multiSellEvent.Discovered {
		soldSystemSet[strings.ToLower(strings.TrimSpace(discoveredEntry.SystemName))] = true
	}

	matchedRecords := builder.claimSoldCartoRecords(soldSystemSet, multiSellEvent.Timestamp)
	if len(matchedRecords) == 0 {
		builder.ledger.unattributedSales = append(builder.ledger.unattributedSales, unattributedSale{
			timestamp:         multiSellEvent.Timestamp,
			kind:              "carto",
			netRealized:       multiSellEvent.TotalEarnings,
			crewLoss:          totalCrewWage,
			carrierTariffLoss: carrierTariff,
		})
		return
	}
	allocateCartoSalePayout(matchedRecords, multiSellEvent.Timestamp, multiSellEvent.TotalEarnings, multiSellEvent.BaseValue, multiSellEvent.Bonus, totalCrewWage, carrierTariff)
}

func buildNormalizedSet(items []string) map[string]bool {
	normalizedSet := make(map[string]bool, len(items))
	for _, rawItem := range items {
		normalizedSet[strings.ToLower(strings.TrimSpace(rawItem))] = true
	}
	return normalizedSet
}

// allocateCartoSalePayout distributes a cartographic sale's net TotalEarnings,
// base value, completion bonus, NPC crew wage, and Fleet Carrier tariff across
// matched scan records proportionally by theoreticalTotal (or evenly when zero),
// assigning integer remainders to the final record so window sums remain exact.
func allocateCartoSalePayout(
	matchedRecords []*scanRecord, soldTimestamp string,
	totalEarnings, baseValue, bonusValue, totalCrewWage, carrierTariff int64,
) {
	var totalTheoretical int64
	for _, matchedRecord := range matchedRecords {
		totalTheoretical += matchedRecord.theoreticalTotal
	}
	var allocatedNet, allocatedBase, allocatedBonus, allocatedCrew, allocatedTariff int64
	for recordIndex, matchedRecord := range matchedRecords {
		matchedRecord.status = "sold"
		matchedRecord.soldTimestamp = soldTimestamp
		if recordIndex == len(matchedRecords)-1 {
			matchedRecord.actualNet = totalEarnings - allocatedNet
			matchedRecord.actualBase = baseValue - allocatedBase
			matchedRecord.actualBonus = bonusValue - allocatedBonus
			matchedRecord.crewLoss = totalCrewWage - allocatedCrew
			matchedRecord.carrierTariffLoss = carrierTariff - allocatedTariff
			continue
		}
		allocationRatio := 1.0 / float64(len(matchedRecords))
		if totalTheoretical > 0 {
			allocationRatio = float64(matchedRecord.theoreticalTotal) / float64(totalTheoretical)
		}
		matchedRecord.actualNet = int64(float64(totalEarnings) * allocationRatio)
		matchedRecord.actualBase = int64(float64(baseValue) * allocationRatio)
		matchedRecord.actualBonus = int64(float64(bonusValue) * allocationRatio)
		matchedRecord.crewLoss = int64(float64(totalCrewWage) * allocationRatio)
		matchedRecord.carrierTariffLoss = int64(float64(carrierTariff) * allocationRatio)
		allocatedNet += matchedRecord.actualNet
		allocatedBase += matchedRecord.actualBase
		allocatedBonus += matchedRecord.actualBonus
		allocatedCrew += matchedRecord.crewLoss
		allocatedTariff += matchedRecord.carrierTariffLoss
	}
}

