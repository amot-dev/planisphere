package stats

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

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
	eventUID           string
	timestamp          string
	systemAddress      int64
	starSystem         string
	bodyID             int64
	bodyName           string
	isOrganic          bool
	undiscoveredAtScan bool
	species            string
	speciesLocalised   string
	variant            string
	variantLocalised   string
	normalizedSpecies  string
	normalizedVariant  string
	theoreticalBase    int64
	theoreticalBonus   int64
	theoreticalTotal   int64

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
	mu      sync.RWMutex
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
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
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
	"Died",
	"Resurrect",
	"NpcCrewPaidWage",
}

// ComputeRealization computes the realization breakdown for the given time range.
func (engine *Engine) ComputeRealization(timeRange Range, filter *Filter) (RealizationBreakdown, error) {
	ledger, err := engine.buildLedger()
	if err != nil {
		return RealizationBreakdown{}, err
	}

	after, until := timeRange.bounds()
	var filterSet *filterSets
	if filter != nil && filter.Enabled {
		var prepErr error
		filterSet, prepErr = filter.prepare(engine.DB)
		if prepErr != nil {
			return RealizationBreakdown{}, prepErr
		}
	}

	var breakdown RealizationBreakdown
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()

	for _, rec := range ledger.records {
		if rec.timestamp <= after || rec.timestamp > until {
			continue
		}
		if filterSet != nil && !filterSet.allow(rec.timestamp, true) {
			continue
		}

		if rec.isOrganic {
			breakdown.OrganicLostDeath += rec.deathLoss
			breakdown.OrganicStolen += rec.stolenLoss
		} else {
			breakdown.CartoLostDeath += rec.deathLoss
			breakdown.CartoStolen += rec.stolenLoss
		}
		breakdown.TotalRealized += rec.actualGross
		breakdown.NpcCrewLoss += rec.crewLoss

		if rec.status == "pending" {
			breakdown.PendingValue += rec.theoreticalTotal
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

func parseTime(ts string) time.Time {
	t, _ := ed.ParseISO(ts)
	return t
}

func timeDiffSeconds(ts1, ts2 string) float64 {
	t1 := parseTime(ts1)
	t2 := parseTime(ts2)
	return math.Abs(t1.Sub(t2).Seconds())
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
	events, err := engine.loadEvents("", "9999-12-31T23:59:59Z", realizationEvents)
	if err != nil {
		return nil, err
	}

	ledger := &RealizationLedger{
		records:              make([]*scanRecord, 0, len(events)/2),
		realizedOrganicBonus: make(map[string]bool),
	}

	type cartoKey struct {
		systemAddress int64
		bodyID        int64
	}

	unsoldCarto := make(map[cartoKey]*scanRecord)
	unsoldOrganic := make([]*scanRecord, 0, 100)
	recentWageEvents := make([]journal.NpcCrewPaidWageEvent, 0, 50)
	resurrectEvents := make([]journal.ResurrectEvent, 0, 10)

	// First pass: collect wage and resurrect events so they can be matched with concurrent sales and deaths
	for _, ev := range events {
		switch ev.eventType {
		case "NpcCrewPaidWage":
			var wageEv journal.NpcCrewPaidWageEvent
			if json.Unmarshal([]byte(ev.rawJSON), &wageEv) == nil {
				recentWageEvents = append(recentWageEvents, wageEv)
			}
		case "Resurrect":
			var resEv journal.ResurrectEvent
			if json.Unmarshal([]byte(ev.rawJSON), &resEv) == nil {
				resurrectEvents = append(resurrectEvents, resEv)
			}
		}
	}

	findCrewWage := func(saleTimestamp string) int64 {
		var totalWage int64
		for _, wageEvent := range recentWageEvents {
			if timeDiffSeconds(wageEvent.Timestamp, saleTimestamp) <= 5.0 {
				totalWage += wageEvent.Amount
			}
		}
		return totalWage
	}

	findNextResurrect := func(diedTimestamp string) *journal.ResurrectEvent {
		for _, resEv := range resurrectEvents {
			if resEv.Timestamp >= diedTimestamp && timeDiffSeconds(resEv.Timestamp, diedTimestamp) <= 600.0 {
				return &resEv
			}
		}
		return nil
	}

	scans := make(map[cartoKey]*journal.ScanEvent)
	seenSales := make(map[string]bool)

	for evIdx := 0; evIdx < len(events); evIdx++ {
		ev := events[evIdx]
		switch ev.eventType {
		case "Scan":
			var scanEv journal.ScanEvent
			if json.Unmarshal([]byte(ev.rawJSON), &scanEv) != nil || scanEv.IsBeltCluster() {
				continue
			}
			key := cartoKey{scanEv.SystemAddress, scanEv.BodyID}
			scanCopy := scanEv
			scans[key] = &scanCopy

			if scanEv.ScanType == "NavBeaconDetail" {
				continue
			}
			estimatedBodyValue := ed.EstimateBodyValue(toBodyScan(&scanEv), ed.MappingInfo{}, engine.Odyssey)
			rec := &scanRecord{
				timestamp:          ev.timestamp,
				systemAddress:      scanEv.SystemAddress,
				starSystem:         scanEv.StarSystem,
				bodyID:             scanEv.BodyID,
				bodyName:           scanEv.BodyName,
				isOrganic:          false,
				undiscoveredAtScan: !scanEv.WasDiscovered,
				theoreticalBase:    estimatedBodyValue.Base,
				theoreticalBonus:   estimatedBodyValue.Bonus,
				theoreticalTotal:   estimatedBodyValue.Total,
				status:             "pending",
			}
			unsoldCarto[key] = rec
			ledger.records = append(ledger.records, rec)

		case "SAAScanComplete":
			var saaEv journal.SAAScanCompleteEvent
			if json.Unmarshal([]byte(ev.rawJSON), &saaEv) != nil {
				continue
			}
			key := cartoKey{saaEv.SystemAddress, saaEv.BodyID}
			scan := scans[key]
			if scan == nil {
				continue
			}
			mappingInfo := ed.MappingInfo{Mapped: true, ProbesUsed: saaEv.ProbesUsed, EfficiencyTarget: saaEv.EfficiencyTarget}
			mappingIncrement := ed.EstimateMappingIncrement(toBodyScan(scan), mappingInfo, engine.Odyssey)

			rec := unsoldCarto[key]
			if rec != nil {
				rec.theoreticalBase += mappingIncrement.Base
				rec.theoreticalBonus += mappingIncrement.Bonus
				rec.theoreticalTotal += mappingIncrement.Total
				if !scan.WasMapped {
					rec.undiscoveredAtScan = true
				}
			} else {
				// Mapped without existing pending scan in current batch
				rec = &scanRecord{
					timestamp:          ev.timestamp,
					systemAddress:      saaEv.SystemAddress,
					bodyID:             saaEv.BodyID,
					bodyName:           saaEv.BodyName,
					isOrganic:          false,
					undiscoveredAtScan: !scan.WasDiscovered || !scan.WasMapped,
					theoreticalBase:    mappingIncrement.Base,
					theoreticalBonus:   mappingIncrement.Bonus,
					theoreticalTotal:   mappingIncrement.Total,
					status:             "pending",
				}
				unsoldCarto[key] = rec
				ledger.records = append(ledger.records, rec)
			}

		case "ScanOrganic":
			var organicEv journal.ScanOrganicEvent
			if json.Unmarshal([]byte(ev.rawJSON), &organicEv) != nil || !strings.EqualFold(organicEv.ScanType, "Analyse") {
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
			rec := &scanRecord{
				eventUID:           ev.eventUID,
				timestamp:          ev.timestamp,
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
			unsoldOrganic = append(unsoldOrganic, rec)
			ledger.records = append(ledger.records, rec)

		case "Died":
			var diedEv journal.DiedEvent
			_ = json.Unmarshal([]byte(ev.rawJSON), &diedEv)

			// Determine if this was ship destruction or on-foot suit death
			isShipDestruction := false
			if resEv := findNextResurrect(ev.timestamp); resEv != nil {
				if resEv.Cost > 0 || strings.EqualFold(resEv.Option, "rebuy") {
					isShipDestruction = true
				} else {
					isShipDestruction = false
				}
			} else {
				if diedEv.IsOnFoot() {
					isShipDestruction = false
				} else if diedEv.KillerShip != "" {
					isShipDestruction = true
				}
			}

			// Exobiology samples in suit are always lost on death
			for _, orgRec := range unsoldOrganic {
				if orgRec.status == "pending" {
					orgRec.status = "lost_death"
					orgRec.deathLoss = orgRec.theoreticalTotal
				}
			}
			unsoldOrganic = unsoldOrganic[:0]

			// Cartographic data is only lost on ship destruction
			if isShipDestruction {
				for _, cartoRec := range unsoldCarto {
					if cartoRec.status == "pending" {
						cartoRec.status = "lost_death"
						cartoRec.deathLoss = cartoRec.theoreticalTotal
					}
				}
				clear(unsoldCarto)
			}

		case "SellOrganicData":
			var sellEv journal.SellOrganicDataEvent
			if json.Unmarshal([]byte(ev.rawJSON), &sellEv) != nil {
				continue
			}
			var totalSaleValue, totalSaleBonus int64
			for _, item := range sellEv.BioData {
				totalSaleValue += item.Value
				totalSaleBonus += item.Bonus
			}
			totalSaleGross := totalSaleValue + totalSaleBonus
			saleKey := fmt.Sprintf("organic|%s|%d|%d|%d|%d", sellEv.Timestamp, sellEv.MarketID, len(sellEv.BioData), totalSaleValue, totalSaleBonus)
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
				matchedIdx := findMatchingOrganic(unsoldOrganic, item)

				if matchedIdx >= 0 {
					matched := unsoldOrganic[matchedIdx]
					matched.status = "sold"
					matched.soldTimestamp = sellEv.Timestamp
					matched.actualBase = item.Value
					matched.actualBonus = item.Bonus
					matched.actualGross = itemGross
					matched.crewLoss = allocatedCrew

					// If scan-time estimation did not award a first-logged bonus (because the
					// parent body already had WasFootfalled == true), but Vista Genomics paid a
					// positive bonus upon sale, record that this ScanOrganic earned the bonus.
					// Required because of Frontier Issue #81891 and remains required for backwards
					// compatibility with older journal files even if or when
					// https://issues.frontierstore.net/issue-detail/81891 is fixed.
					if item.Bonus > 0 && matched.eventUID != "" {
						ledger.realizedOrganicBonus[matched.eventUID] = true
						if matched.theoreticalBonus == 0 {
							upgradedVal := ed.OrganicValue(matched.theoreticalBase, true)
							matched.theoreticalBonus = upgradedVal.Bonus
							matched.theoreticalTotal = upgradedVal.Total
						}
					}

					// Stolen check: only flag as stolen if the body was undiscovered at the time of scan
					// and the player expected a first-logged bonus, but received none.
					if matched.undiscoveredAtScan && matched.theoreticalBonus > 0 && item.Bonus == 0 {
						matched.stolenLoss = matched.theoreticalBonus
					}

					// Remove from unsold queue
					unsoldOrganic = append(unsoldOrganic[:matchedIdx], unsoldOrganic[matchedIdx+1:]...)
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
			if json.Unmarshal([]byte(ev.rawJSON), &sellEv) != nil {
				continue
			}
			saleKey := fmt.Sprintf("carto|%s|%d|%d|%d", sellEv.Timestamp, sellEv.TotalEarnings, sellEv.BaseValue, len(sellEv.Systems))
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
			var matchedRecs []*scanRecord
			for key, rec := range unsoldCarto {
				if rec.status == "pending" && rec.timestamp <= sellEv.Timestamp {
					if len(soldSystemSet) == 0 || soldSystemSet[strings.ToLower(rec.starSystem)] {
						matchedRecs = append(matchedRecs, rec)
						delete(unsoldCarto, key)
					}
				}
			}

			if len(matchedRecs) > 0 {
				var totalTheoretical int64
				for _, matchedRecord := range matchedRecs {
					totalTheoretical += matchedRecord.theoreticalTotal
				}
				var allocatedGross int64
				var allocatedBase int64
				var allocatedBonus int64
				var allocatedCrew int64
				for recIdx, matchedRecord := range matchedRecs {
					matchedRecord.status = "sold"
					matchedRecord.soldTimestamp = sellEv.Timestamp
					ratio := 1.0
					if totalTheoretical > 0 {
						ratio = float64(matchedRecord.theoreticalTotal) / float64(totalTheoretical)
					} else if len(matchedRecs) > 0 {
						ratio = 1.0 / float64(len(matchedRecs))
					}
					if recIdx == len(matchedRecs)-1 {
						matchedRecord.actualGross = sellEv.TotalEarnings - allocatedGross
						matchedRecord.actualBase = sellEv.BaseValue - allocatedBase
						matchedRecord.actualBonus = sellEv.Bonus - allocatedBonus
						if totalCrewWage > 0 {
							matchedRecord.crewLoss = totalCrewWage - allocatedCrew
						}
					} else {
						matchedRecord.actualGross = int64(float64(sellEv.TotalEarnings) * ratio)
						matchedRecord.actualBase = int64(float64(sellEv.BaseValue) * ratio)
						matchedRecord.actualBonus = int64(float64(sellEv.Bonus) * ratio)
						allocatedGross += matchedRecord.actualGross
						allocatedBase += matchedRecord.actualBase
						allocatedBonus += matchedRecord.actualBonus
						if totalCrewWage > 0 {
							matchedRecord.crewLoss = int64(float64(totalCrewWage) * ratio)
							allocatedCrew += matchedRecord.crewLoss
						}
					}

					// Stolen check: had bonus expectation on an undiscovered body, but body/system was not awarded first discovery
					if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 {
						normBody := strings.ToLower(strings.TrimSpace(matchedRecord.bodyName))
						normSystem := strings.ToLower(strings.TrimSpace(matchedRecord.starSystem))
						if !discoveredSet[normBody] && !discoveredSet[normSystem] {
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
			if json.Unmarshal([]byte(ev.rawJSON), &sellEv) != nil {
				continue
			}
			saleKey := fmt.Sprintf("carto_multi|%s|%d|%d|%d", sellEv.Timestamp, sellEv.TotalEarnings, sellEv.BaseValue, len(sellEv.Discovered))
			if seenSales[saleKey] {
				continue
			}
			seenSales[saleKey] = true

			totalEarnings := sellEv.TotalEarnings
			baseValue := sellEv.BaseValue
			bonus := sellEv.Bonus
			lastTimestamp := sellEv.Timestamp
			discoveredSet := make(map[string]bool, len(sellEv.Discovered))
			for _, discoveredEntry := range sellEv.Discovered {
				discoveredSet[strings.ToLower(strings.TrimSpace(discoveredEntry.SystemName))] = true
			}

			// Coalesce sequential MultiSellExplorationData events within 60s (e.g. multi-page selling)
			for evIdx+1 < len(events) && events[evIdx+1].eventType == "MultiSellExplorationData" {
				var nextSell journal.MultiSellExplorationDataEvent
				if json.Unmarshal([]byte(events[evIdx+1].rawJSON), &nextSell) == nil {
					if timeDiffSeconds(nextSell.Timestamp, lastTimestamp) <= 60.0 {
						nextKey := fmt.Sprintf("carto_multi|%s|%d|%d|%d", nextSell.Timestamp, nextSell.TotalEarnings, nextSell.BaseValue, len(nextSell.Discovered))
						if !seenSales[nextKey] {
							seenSales[nextKey] = true
							totalEarnings += nextSell.TotalEarnings
							baseValue += nextSell.BaseValue
							bonus += nextSell.Bonus
							for _, disc := range nextSell.Discovered {
								discoveredSet[strings.ToLower(strings.TrimSpace(disc.SystemName))] = true
							}
							lastTimestamp = nextSell.Timestamp
						}
						evIdx++
						continue
					}
				}
				break
			}

			totalCrewWage := findCrewWage(lastTimestamp)

			// In MultiSellExplorationData, all pending cartographic scans accumulated up to this sale
			// timestamp are sold.
			var matchedRecs []*scanRecord
			for key, rec := range unsoldCarto {
				if rec.status == "pending" && rec.timestamp <= lastTimestamp {
					matchedRecs = append(matchedRecs, rec)
					delete(unsoldCarto, key)
				}
			}

			if len(matchedRecs) > 0 {
				var totalTheoretical int64
				for _, matchedRecord := range matchedRecs {
					totalTheoretical += matchedRecord.theoreticalTotal
				}
				var allocatedGross int64
				var allocatedBase int64
				var allocatedBonus int64
				var allocatedCrew int64
				for recIdx, matchedRecord := range matchedRecs {
					matchedRecord.status = "sold"
					matchedRecord.soldTimestamp = lastTimestamp
					ratio := 1.0
					if totalTheoretical > 0 {
						ratio = float64(matchedRecord.theoreticalTotal) / float64(totalTheoretical)
					} else if len(matchedRecs) > 0 {
						ratio = 1.0 / float64(len(matchedRecs))
					}
					if recIdx == len(matchedRecs)-1 {
						matchedRecord.actualGross = totalEarnings - allocatedGross
						matchedRecord.actualBase = baseValue - allocatedBase
						matchedRecord.actualBonus = bonus - allocatedBonus
						if totalCrewWage > 0 {
							matchedRecord.crewLoss = totalCrewWage - allocatedCrew
						}
					} else {
						matchedRecord.actualGross = int64(float64(totalEarnings) * ratio)
						matchedRecord.actualBase = int64(float64(baseValue) * ratio)
						matchedRecord.actualBonus = int64(float64(bonus) * ratio)
						allocatedGross += matchedRecord.actualGross
						allocatedBase += matchedRecord.actualBase
						allocatedBonus += matchedRecord.actualBonus
						if totalCrewWage > 0 {
							matchedRecord.crewLoss = int64(float64(totalCrewWage) * ratio)
							allocatedCrew += matchedRecord.crewLoss
						}
					}

					// Stolen check: player expected discovery bonus on an undiscovered body, but system was not in Discovered array
					if matchedRecord.undiscoveredAtScan && matchedRecord.theoreticalBonus > 0 {
						normSystem := strings.ToLower(strings.TrimSpace(matchedRecord.starSystem))
						if !discoveredSet[normSystem] {
							matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
						}
					}
				}
			} else {
				ledger.unattributedSales = append(ledger.unattributedSales, unattributedSale{
					timestamp: lastTimestamp,
					kind:      "carto",
					gross:     totalEarnings,
					crewLoss:  totalCrewWage,
				})
			}
		}
	}

	return ledger, nil
}
