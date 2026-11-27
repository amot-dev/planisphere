package realization

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"planisphere/internal/reference"
	"planisphere/internal/sources/journal"
)

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
	builder.noteSaleTimestamp(sellEvent.Timestamp)
	carrierTariff := reference.CarrierCartographicsTariff(builder.session.IsDockedOnFleetCarrier(), sellEvent.BaseValue, sellEvent.Bonus)
	soldSystemSet := buildNormalizedSet(sellEvent.Systems)
	matchedRecords := builder.claimSoldCartoRecords(soldSystemSet, sellEvent.Timestamp)

	if len(matchedRecords) == 0 {
		builder.ledger.unattributedSales = append(builder.ledger.unattributedSales, unattributedSale{
			profileID:         builder.profileID,
			nocaseCommander:   builder.nocaseCommander,
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
	builder.noteSaleTimestamp(multiSellEvent.Timestamp)
	carrierTariff := reference.CarrierCartographicsTariff(builder.session.IsDockedOnFleetCarrier(), multiSellEvent.BaseValue, multiSellEvent.Bonus)
	soldSystemSet := make(map[string]bool, len(multiSellEvent.Discovered))
	for _, discoveredEntry := range multiSellEvent.Discovered {
		soldSystemSet[strings.ToLower(strings.TrimSpace(discoveredEntry.SystemName))] = true
	}

	matchedRecords := builder.claimSoldCartoRecords(soldSystemSet, multiSellEvent.Timestamp)
	if len(matchedRecords) == 0 {
		builder.ledger.unattributedSales = append(builder.ledger.unattributedSales, unattributedSale{
			profileID:         builder.profileID,
			nocaseCommander:   builder.nocaseCommander,
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

func (builder *ledgerBuilder) claimSoldCartoRecords(soldSystemSet map[string]bool, saleTimestamp string) []*scanRecord {
	var matchedRecords []*scanRecord
	if len(soldSystemSet) > 0 {
		matchedRecords = builder.claimSoldSystems(soldSystemSet, saleTimestamp)
	} else {
		matchedRecords = builder.claimAllPendingCarto(saleTimestamp)
	}
	slices.SortFunc(matchedRecords, func(leftRecord, rightRecord *scanRecord) int {
		return cmp.Or(
			cmp.Compare(leftRecord.timestamp, rightRecord.timestamp),
			cmp.Compare(leftRecord.systemAddress, rightRecord.systemAddress),
			cmp.Compare(leftRecord.bodyID, rightRecord.bodyID),
		)
	})
	return matchedRecords
}

func (builder *ledgerBuilder) claimSoldSystems(soldSystemSet map[string]bool, saleTimestamp string) []*scanRecord {
	matchedRecords := make([]*scanRecord, 0, len(soldSystemSet)*4)
	for normalizedSystem := range soldSystemSet {
		systemRecords := builder.unsoldCartoBySystem[normalizedSystem]
		if len(systemRecords) == 0 {
			continue
		}
		remainingRecords := systemRecords[:0]
		for _, candidateRecord := range systemRecords {
			if candidateRecord.status == "pending" && candidateRecord.timestamp <= saleTimestamp {
				builder.markCartoRecordSold(candidateRecord)
				matchedRecords = append(matchedRecords, builder.ledger.ensureMutableRecord(candidateRecord))
			} else if candidateRecord.status == "pending" {
				remainingRecords = append(remainingRecords, candidateRecord)
			}
		}
		if len(remainingRecords) == 0 {
			delete(builder.unsoldCartoBySystem, normalizedSystem)
		} else {
			builder.unsoldCartoBySystem[normalizedSystem] = remainingRecords
		}
	}
	return matchedRecords
}

func (builder *ledgerBuilder) claimAllPendingCarto(saleTimestamp string) []*scanRecord {
	matchedRecords := make([]*scanRecord, 0, len(builder.unsoldCarto))
	for _, candidateRecord := range builder.unsoldCarto {
		if candidateRecord.status == "pending" && candidateRecord.timestamp <= saleTimestamp {
			builder.markCartoRecordSold(candidateRecord)
			matchedRecords = append(matchedRecords, builder.ledger.ensureMutableRecord(candidateRecord))
		}
	}
	if len(builder.unsoldCarto) == 0 {
		clear(builder.unsoldCartoBySystem)
		return matchedRecords
	}
	for normalizedSystem, systemRecords := range builder.unsoldCartoBySystem {
		remainingRecords := systemRecords[:0]
		for _, systemRecord := range systemRecords {
			recordKey := bodyKey{systemAddress: systemRecord.systemAddress, bodyID: systemRecord.bodyID}
			if builder.unsoldCarto[recordKey] == systemRecord {
				remainingRecords = append(remainingRecords, systemRecord)
			}
		}
		if len(remainingRecords) == 0 {
			delete(builder.unsoldCartoBySystem, normalizedSystem)
		} else {
			builder.unsoldCartoBySystem[normalizedSystem] = remainingRecords
		}
	}
	return matchedRecords
}

func (builder *ledgerBuilder) markCartoRecordSold(record *scanRecord) {
	key := bodyKey{systemAddress: record.systemAddress, bodyID: record.bodyID}
	delete(builder.unsoldCarto, key)
	if record.hasScan {
		builder.soldScan[key] = true
	}
	if record.hasMapped {
		builder.soldMapped[key] = true
	}
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
