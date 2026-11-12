package stats

import (
	"encoding/json"
	"fmt"
	"strings"

	"planisphere/internal/ed"
	"planisphere/internal/ed/exceptions"
	"planisphere/internal/journal"
)

// prepareComputeScans identifies mapped or organically sampled bodies that have
// no Scan event in events and preloads their latest prior Scan at or before
// afterTimestamp. Two passes ensure same-second mappings ordered before their
// matching Scan in events are not marked missing.
func (engine *Engine) prepareComputeScans(afterTimestamp string, events []eventRow) (map[bodyKey]*journal.ScanEvent, map[bodyKey]bool, error) {
	scans := make(map[bodyKey]*journal.ScanEvent)
	missingScans := make(map[bodyKey]bool)
	scannedInEvents := make(map[bodyKey]bool, len(events)/2)
	for _, rowItem := range events {
		if rowItem.eventType == "Scan" && rowItem.systemAddress.Valid && rowItem.bodyID.Valid {
			scannedInEvents[bodyKey{rowItem.systemAddress.Int64, rowItem.bodyID.Int64}] = true
		}
	}
	neededSet := make(map[bodyKey]bool)
	var neededKeys []bodyKey
	for _, rowItem := range events {
		if (rowItem.eventType == "SAAScanComplete" || rowItem.eventType == "ScanOrganic") &&
			rowItem.systemAddress.Valid && rowItem.bodyID.Valid {
			key := bodyKey{rowItem.systemAddress.Int64, rowItem.bodyID.Int64}
			if !scannedInEvents[key] && !neededSet[key] {
				neededSet[key] = true
				neededKeys = append(neededKeys, key)
			}
		}
	}
	if len(neededKeys) == 0 {
		return scans, missingScans, nil
	}
	if afterTimestamp == "" {
		for _, key := range neededKeys {
			missingScans[key] = true
		}
		return scans, missingScans, nil
	}
	preloaded, preloadError := engine.preloadScans(neededKeys, afterTimestamp)
	if preloadError != nil {
		return nil, nil, fmt.Errorf("preload compute scans before %s: %w", afterTimestamp, preloadError)
	}
	for key, scanEvent := range preloaded {
		scans[key] = scanEvent
	}
	for _, key := range neededKeys {
		if scans[key] == nil {
			missingScans[key] = true
		}
	}
	return scans, missingScans, nil
}

type computeAccumulator struct {
	engine                  *Engine
	after                   string
	until                   string
	filterSet               *filterSets
	session                 *sessionState
	carrierLegs             map[string]CarrierLeg
	realizationLedger       *RealizationLedger
	realizationLedgerLoaded bool
	scans                   map[bodyKey]*journal.ScanEvent
	missingScans            map[bodyKey]bool
	scannedBodies           map[bodyKey]bool
	mappedBodies            map[bodyKey]bool
}

func (accumulator *computeAccumulator) processEvent(rowItem eventRow, metrics *Metrics) error {
	switch rowItem.eventType {
	case "LoadGame":
		accumulator.session.applyLoadGame(rowItem.rawJSON)
	case "Location":
		accumulator.session.applyLocation(rowItem.rawJSON)
	case "FSDJump":
		accumulator.processFSDJump(rowItem, metrics)
	case "CarrierJump":
		return accumulator.processCarrierJump(rowItem, metrics)
	case "Scan":
		accumulator.processScan(rowItem, metrics)
	case "SAAScanComplete":
		return accumulator.processMapping(rowItem, metrics)
	case "ScanOrganic":
		return accumulator.processOrganic(rowItem, metrics)
	}
	return nil
}

func (accumulator *computeAccumulator) processFSDJump(rowItem eventRow, metrics *Metrics) {
	var jumpEvent journal.FSDJumpEvent
	if json.Unmarshal([]byte(rowItem.rawJSON), &jumpEvent) != nil {
		return
	}
	accumulator.session.applyFSDJumpEvent(&jumpEvent)
	if !accumulator.filterSet.allow(rowItem.timestamp, false) {
		return
	}
	metrics.Jumps++
	metrics.ShipDistanceLy += jumpEvent.JumpDist
	metrics.DistanceLy += jumpEvent.JumpDist
	switch jumpEvent.BoostUsed {
	case 4:
		metrics.JetConeBoosts++
	case 3:
		metrics.PremiumBoosts++
	case 2:
		metrics.StandardBoosts++
	case 1:
		metrics.BasicBoosts++
	}
}

func (accumulator *computeAccumulator) processCarrierJump(rowItem eventRow, metrics *Metrics) error {
	accumulator.session.applyCarrierJump(rowItem.rawJSON)
	if accumulator.carrierLegs == nil {
		var legError error
		if accumulator.carrierLegs, legError = accumulator.engine.CarrierLegsRange(accumulator.after, accumulator.until); legError != nil {
			return legError
		}
	}
	leg := accumulator.carrierLegs[rowItem.eventUID]
	if accumulator.filterSet.allow(rowItem.timestamp, false) {
		metrics.CarrierPassengerLy += leg.DistanceLy
		metrics.DistanceLy += leg.DistanceLy
	}
	metrics.CarrierJumps++
	metrics.CarrierDistanceLy += leg.DistanceLy
	if leg.Tritium > 0 {
		currentTritium := 0
		if metrics.TritiumUsed != nil {
			currentTritium = *metrics.TritiumUsed
		}
		nextTritium := currentTritium + leg.Tritium
		metrics.TritiumUsed = &nextTritium
	}
	return nil
}

func (accumulator *computeAccumulator) processScan(rowItem eventRow, metrics *Metrics) {
	var scanEvent journal.ScanEvent
	if json.Unmarshal([]byte(rowItem.rawJSON), &scanEvent) != nil || scanEvent.IsBeltCluster() {
		return
	}
	key := bodyKey{scanEvent.SystemAddress, scanEvent.BodyID}
	scanCopy := scanEvent
	accumulator.scans[key] = &scanCopy
	if !accumulator.filterSet.allow(rowItem.timestamp, true) {
		return
	}
	metrics.BodiesScanned++
	if scanEvent.ScanType == "NavBeaconDetail" || accumulator.scannedBodies[key] {
		return
	}
	accumulator.scannedBodies[key] = true
	systemPopulation := accumulator.session.systemPopulation(scanEvent.SystemAddress)
	if ed.CanExpectFirstDiscovered(systemPopulation, scanEvent.WasDiscovered, scanEvent.WasMapped) {
		metrics.FirstDiscoveries++
	}
	odysseyEnabled := accumulator.session.odysseyAt(rowItem.timestamp)
	metrics.addScan(ed.EstimateBodyValue(toBodyScanWithPopulation(&scanEvent, systemPopulation), ed.MappingInfo{}, odysseyEnabled))
}

func (accumulator *computeAccumulator) processMapping(rowItem eventRow, metrics *Metrics) error {
	var saaEvent journal.SAAScanCompleteEvent
	if json.Unmarshal([]byte(rowItem.rawJSON), &saaEvent) != nil {
		return nil
	}
	key := bodyKey{saaEvent.SystemAddress, saaEvent.BodyID}
	if accumulator.mappedBodies[key] || !accumulator.filterSet.allow(rowItem.timestamp, true) {
		return nil
	}
	accumulator.mappedBodies[key] = true
	metrics.BodiesMapped++
	parentScan, lookupError := accumulator.resolveParentScan(key, rowItem.timestamp)
	if lookupError != nil || parentScan == nil {
		return lookupError
	}
	systemPopulation := accumulator.session.systemPopulation(saaEvent.SystemAddress)
	if ed.CanExpectFirstMapped(systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped) {
		metrics.FirstMapped++
	}
	odysseyEnabled := accumulator.session.odysseyAt(rowItem.timestamp)
	mappingInfo := ed.MappingInfo{Mapped: true, ProbesUsed: saaEvent.ProbesUsed, EfficiencyTarget: saaEvent.EfficiencyTarget}
	metrics.addScan(ed.EstimateMappingIncrement(toBodyScanWithPopulation(parentScan, systemPopulation), mappingInfo, odysseyEnabled))
	return nil
}

func (accumulator *computeAccumulator) processOrganic(rowItem eventRow, metrics *Metrics) error {
	var organicEvent journal.ScanOrganicEvent
	if json.Unmarshal([]byte(rowItem.rawJSON), &organicEvent) != nil || !strings.EqualFold(organicEvent.ScanType, "Analyse") {
		return nil
	}
	if !accumulator.filterSet.allow(rowItem.timestamp, true) {
		return nil
	}
	metrics.OrganicSamples++
	systemPopulation := accumulator.session.systemPopulation(organicEvent.SystemAddress)
	firstLoggedBonus, checkError := accumulator.firstLogged(&organicEvent, systemPopulation, rowItem.timestamp)
	if checkError != nil {
		return checkError
	}

	var saleOutcome organicSaleOutcome
	if rowItem.eventUID != "" {
		if !accumulator.realizationLedgerLoaded {
			var ledgerError error
			accumulator.realizationLedger, ledgerError = accumulator.engine.buildLedger()
			if ledgerError != nil {
				return ledgerError
			}
			accumulator.realizationLedgerLoaded = true
		}
		saleOutcome, _ = accumulator.realizationLedger.organicSaleDetails(rowItem.eventUID)
		if saleOutcome.bonusRealized {
			firstLoggedBonus = true
		}
	}
	if firstLoggedBonus {
		metrics.FirstLogged++
	}
	organicValuation := exceptions.EvaluateOrganic(exceptions.OrganicContext{
		ScanTimestamp:    rowItem.timestamp,
		SaleTimestamp:    saleOutcome.saleTimestamp,
		SystemAddress:    organicEvent.SystemAddress,
		SaleMarketID:     saleOutcome.saleMarketID,
		SpeciesCodex:     organicEvent.Species,
		SpeciesLocalised: organicEvent.SpeciesLocalised,
		GenusCodex:       organicEvent.Genus,
		FirstLogged:      firstLoggedBonus,
	})
	metrics.addOrganic(organicValuation)
	return nil
}

func (accumulator *computeAccumulator) resolveParentScan(key bodyKey, timestamp string) (*journal.ScanEvent, error) {
	parentScan := accumulator.scans[key]
	if parentScan != nil || accumulator.missingScans[key] {
		return parentScan, nil
	}
	lookedUpScan, lookupError := accumulator.engine.lookupScan(key, timestamp)
	if lookupError != nil {
		return nil, lookupError
	}
	if lookedUpScan != nil {
		accumulator.scans[key] = lookedUpScan
	} else {
		accumulator.missingScans[key] = true
	}
	return lookedUpScan, nil
}

// firstLogged decides whether the first-logged bonus applies at scan time using
// the composite eligibility rule (unpopulated system, non-tag-locked body, and
// WasFootfalled == false with pre-2025 fallback). ScanOrganic.WasLogged is
// intentionally ignored because Frontier journals do not reliably populate it:
// https://issues.frontierstore.net/issue-detail/81891
func (accumulator *computeAccumulator) firstLogged(organicEvent *journal.ScanOrganicEvent, systemPopulation int64, timestamp string) (bool, error) {
	key := bodyKey{organicEvent.SystemAddress, organicEvent.Body}
	parentScan, lookupError := accumulator.resolveParentScan(key, timestamp)
	if lookupError != nil || parentScan == nil {
		return false, lookupError
	}
	return ed.CanExpectFirstLogged(systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped, parentScan.WasFootfalled), nil
}

