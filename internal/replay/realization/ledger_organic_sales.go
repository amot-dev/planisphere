package realization

import (
	"cmp"
	"encoding/json"
	"slices"
	"sort"

	"planisphere/internal/reference"
	"planisphere/internal/reference/exceptions"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
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
func newCrewWageMatcher(loadedEvents []replay.Row) *crewWageMatcher {
	wageEvents := make([]parsedWageEvent, 0, 50)
	for _, journalEvent := range loadedEvents {
		if journalEvent.Type != journal.EventNpcCrewPaidWage {
			continue
		}
		var wageEvent journal.NpcCrewPaidWageEvent
		if json.Unmarshal([]byte(journalEvent.RawJSON), &wageEvent) != nil {
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

func parseUnixSeconds(timestamp string) (int64, bool) {
	parsedTime, parseError := reference.ParseISO(timestamp)
	if parseError != nil {
		return 0, false
	}
	return parsedTime.Unix(), true
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
	ledger *Ledger, unsoldRecords []*scanRecord, claimedIndices []int,
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
	ledger *Ledger, matchedRecord *scanRecord, sellEvent journal.SellOrganicDataEvent,
	shareBase, shareBonus, shareCrew int64,
) {
	matchedRecord.status = "sold"
	matchedRecord.soldTimestamp = sellEvent.Timestamp
	matchedRecord.actualBase = shareBase
	matchedRecord.actualBonus = shareBonus
	matchedRecord.crewLoss = shareCrew
	matchedRecord.actualNet = (shareBase + shareBonus) - shareCrew

	if matchedRecord.eventUID != "" {
		ledger.organicSales[matchedRecord.eventUID] = OrganicSaleOutcome{
			SaleTimestamp: sellEvent.Timestamp,
			SaleMarketID:  sellEvent.MarketID,
			BonusRealized: shareBonus > 0,
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
