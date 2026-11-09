package stats

import (
	"encoding/json"
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
	timestamp        string
	systemAddress    int64
	starSystem       string
	bodyID           int64
	bodyName         string
	isOrganic        bool
	species          string
	variant          string
	theoreticalBase  int64
	theoreticalBonus int64
	theoreticalTotal int64

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

// RealizationLedger tracks all scans and their final realization outcomes.
type RealizationLedger struct {
	mu      sync.RWMutex
	records []*scanRecord
	// Unattributed sales that couldn't be matched to a preceding scan
	unattributedSales []unattributedSale
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
		records: make([]*scanRecord, 0, len(events)/2),
	}

	type cartoKey struct {
		systemAddress int64
		bodyID        int64
	}

	unsoldCarto := make(map[cartoKey]*scanRecord)
	unsoldOrganic := make([]*scanRecord, 0, 100)
	recentWageEvents := make([]journal.NpcCrewPaidWageEvent, 0, 50)

	// First pass: collect wage events so they can be matched with concurrent sales
	for _, ev := range events {
		if ev.eventType == "NpcCrewPaidWage" {
			var wageEv journal.NpcCrewPaidWageEvent
			if json.Unmarshal([]byte(ev.rawJSON), &wageEv) == nil {
				recentWageEvents = append(recentWageEvents, wageEv)
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

	scans := make(map[cartoKey]*journal.ScanEvent)

	for _, ev := range events {
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
				timestamp:        ev.timestamp,
				systemAddress:    scanEv.SystemAddress,
				starSystem:       scanEv.StarSystem,
				bodyID:           scanEv.BodyID,
				bodyName:         scanEv.BodyName,
				isOrganic:        false,
				theoreticalBase:  estimatedBodyValue.Base,
				theoreticalBonus: estimatedBodyValue.Bonus,
				theoreticalTotal: estimatedBodyValue.Total,
				status:           "pending",
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
			} else {
				// Mapped without existing pending scan in current batch
				rec = &scanRecord{
					timestamp:        ev.timestamp,
					systemAddress:    saaEv.SystemAddress,
					bodyID:           saaEv.BodyID,
					bodyName:         saaEv.BodyName,
					isOrganic:        false,
					theoreticalBase:  mappingIncrement.Base,
					theoreticalBonus: mappingIncrement.Bonus,
					theoreticalTotal: mappingIncrement.Total,
					status:           "pending",
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
			firstLogged := false
			if organicEv.WasLogged != nil {
				firstLogged = !*organicEv.WasLogged
			} else {
				parentKey := cartoKey{organicEv.SystemAddress, organicEv.Body}
				if parentScan := scans[parentKey]; parentScan != nil && parentScan.WasFootfalled != nil {
					firstLogged = !*parentScan.WasFootfalled
				}
			}
			calculatedOrganicValue := ed.OrganicValue(baseVal, firstLogged)
			rec := &scanRecord{
				timestamp:        ev.timestamp,
				systemAddress:    organicEv.SystemAddress,
				bodyID:           organicEv.Body,
				isOrganic:        true,
				species:          organicEv.Species,
				variant:          organicEv.Variant,
				theoreticalBase:  calculatedOrganicValue.Base,
				theoreticalBonus: calculatedOrganicValue.Bonus,
				theoreticalTotal: calculatedOrganicValue.Total,
				status:           "pending",
			}
			unsoldOrganic = append(unsoldOrganic, rec)
			ledger.records = append(ledger.records, rec)

		case "Died":
			var diedEv journal.DiedEvent
			_ = json.Unmarshal([]byte(ev.rawJSON), &diedEv)
			isOnFoot := diedEv.IsOnFoot()

			// Exobiology samples in the suit are always lost on death
			for _, orgRec := range unsoldOrganic {
				if orgRec.status == "pending" {
					orgRec.status = "lost_death"
					orgRec.deathLoss = orgRec.theoreticalTotal
				}
			}
			unsoldOrganic = unsoldOrganic[:0]

			// Cartographic data is lost on ship destruction
			if !isOnFoot {
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
			totalCrewWage := findCrewWage(sellEv.Timestamp)
			var totalSaleGross int64
			for _, item := range sellEv.BioData {
				totalSaleGross += item.Value + item.Bonus
			}

			for _, item := range sellEv.BioData {
				itemGross := item.Value + item.Bonus
				var allocatedCrew int64
				if totalSaleGross > 0 && totalCrewWage > 0 {
					allocatedCrew = (totalCrewWage * itemGross) / totalSaleGross
				}

				// Find oldest pending unsold matching specimen
				matchedIdx := -1
				for i, orgRec := range unsoldOrganic {
					if orgRec.status == "pending" && (strings.EqualFold(orgRec.species, item.Species) || strings.EqualFold(orgRec.variant, item.Variant)) {
						matchedIdx = i
						break
					}
				}

				if matchedIdx >= 0 {
					matched := unsoldOrganic[matchedIdx]
					matched.status = "sold"
					matched.soldTimestamp = sellEv.Timestamp
					matched.actualBase = item.Value
					matched.actualBonus = item.Bonus
					matched.actualGross = itemGross
					matched.crewLoss = allocatedCrew

					// Stolen check: player expected first-logged bonus, but received none
					if matched.theoreticalBonus > 0 && item.Bonus == 0 {
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
				if rec.status == "pending" && (soldSystemSet[strings.ToLower(rec.starSystem)] || len(sellEv.Systems) == 0) {
					matchedRecs = append(matchedRecs, rec)
					delete(unsoldCarto, key)
				}
			}

			if len(matchedRecs) > 0 {
				var totalTheoretical int64
				for _, matchedRecord := range matchedRecs {
					totalTheoretical += matchedRecord.theoreticalTotal
				}
				// Distribute lump-sum earnings, bonuses, and crew deductions proportionally across matched scan records
				// according to each body's share of total theoretical scan value.
				for _, matchedRecord := range matchedRecs {
					matchedRecord.status = "sold"
					matchedRecord.soldTimestamp = sellEv.Timestamp
					ratio := 1.0
					if totalTheoretical > 0 {
						ratio = float64(matchedRecord.theoreticalTotal) / float64(totalTheoretical)
					}
					matchedRecord.actualGross = int64(float64(sellEv.TotalEarnings) * ratio)
					matchedRecord.actualBase = int64(float64(sellEv.BaseValue) * ratio)
					matchedRecord.actualBonus = int64(float64(sellEv.Bonus) * ratio)
					if totalCrewWage > 0 {
						matchedRecord.crewLoss = int64(float64(totalCrewWage) * ratio)
					}

					// Stolen check: had bonus expectation, but body name not in Discovered array
					if matchedRecord.theoreticalBonus > 0 && !discoveredSet[strings.ToLower(strings.TrimSpace(matchedRecord.bodyName))] && sellEv.Bonus == 0 {
						matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
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
			totalCrewWage := findCrewWage(sellEv.Timestamp)
			soldSystemSet := make(map[string]bool, len(sellEv.Discovered))
			for _, discoveredEntry := range sellEv.Discovered {
				soldSystemSet[strings.ToLower(strings.TrimSpace(discoveredEntry.SystemName))] = true
			}

			var matchedRecs []*scanRecord
			for key, rec := range unsoldCarto {
				if rec.status == "pending" && (soldSystemSet[strings.ToLower(rec.starSystem)] || len(sellEv.Discovered) == 0) {
					matchedRecs = append(matchedRecs, rec)
					delete(unsoldCarto, key)
				}
			}

			if len(matchedRecs) > 0 {
				var totalTheoretical int64
				for _, matchedRecord := range matchedRecs {
					totalTheoretical += matchedRecord.theoreticalTotal
				}
				// Distribute multi-sell earnings, bonuses, and crew deductions proportionally across matched scan records
				// according to each body's share of total theoretical scan value.
				for _, matchedRecord := range matchedRecs {
					matchedRecord.status = "sold"
					matchedRecord.soldTimestamp = sellEv.Timestamp
					ratio := 1.0
					if totalTheoretical > 0 {
						ratio = float64(matchedRecord.theoreticalTotal) / float64(totalTheoretical)
					}
					matchedRecord.actualGross = int64(float64(sellEv.TotalEarnings) * ratio)
					matchedRecord.actualBase = int64(float64(sellEv.BaseValue) * ratio)
					matchedRecord.actualBonus = int64(float64(sellEv.Bonus) * ratio)
					if totalCrewWage > 0 {
						matchedRecord.crewLoss = int64(float64(totalCrewWage) * ratio)
					}
					if matchedRecord.theoreticalBonus > 0 && sellEv.Bonus == 0 {
						matchedRecord.stolenLoss = matchedRecord.theoreticalBonus
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
		}
	}

	return ledger, nil
}
