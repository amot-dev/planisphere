package realization

import (
	"encoding/json"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/reference/exceptions"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

type saleDedupKey struct {
	kind       string
	timestamp  string
	primaryID  int64
	itemCount  int
	totalValue int64
	totalBonus int64
}

type ledgerBuilder struct {
	ledger              *Ledger
	session             *replay.SessionState
	wageMatcher         *crewWageMatcher
	scans               map[bodyKey]*journal.ScanEvent
	unsoldCarto         map[bodyKey]*scanRecord
	unsoldCartoBySystem map[string][]*scanRecord
	soldScan            map[bodyKey]bool
	soldMapped          map[bodyKey]bool
	unsoldOrganic       []*scanRecord
	seenSales           map[saleDedupKey]bool
}

// BuildLedger constructs the full chronological lifecycle realization ledger from queryer.
func BuildLedger(queryer db.Queryer, defaultOdyssey bool) (*Ledger, error) {
	loadedEvents, loadError := replay.LoadEvents(queryer, "", "9999-12-31T23:59:59Z", SalesLedgerReplayEvents)
	if loadError != nil {
		return nil, loadError
	}

	builder := &ledgerBuilder{
		ledger: &Ledger{
			records:      make([]*scanRecord, 0, len(loadedEvents)/2),
			organicSales: make(map[string]OrganicSaleOutcome),
		},
		session:             replay.NewSessionState(defaultOdyssey, len(loadedEvents)/16),
		wageMatcher:         newCrewWageMatcher(loadedEvents),
		scans:               make(map[bodyKey]*journal.ScanEvent, len(loadedEvents)/2),
		unsoldCarto:         make(map[bodyKey]*scanRecord, len(loadedEvents)/2),
		unsoldCartoBySystem: make(map[string][]*scanRecord, len(loadedEvents)/8),
		soldScan:            make(map[bodyKey]bool, len(loadedEvents)/2),
		soldMapped:          make(map[bodyKey]bool, len(loadedEvents)/8),
		unsoldOrganic:       make([]*scanRecord, 0, 100),
		seenSales:           make(map[saleDedupKey]bool, len(loadedEvents)/16),
	}

	for _, journalEvent := range loadedEvents {
		builder.processEvent(journalEvent)
	}
	return builder.ledger, nil
}

func (builder *ledgerBuilder) processEvent(journalEvent replay.Row) {
	switch journalEvent.Type {
	case journal.EventLoadGame:
		builder.session.ApplyLoadGame(journalEvent.RawJSON)
	case journal.EventLocation:
		builder.session.ApplyLocation(journalEvent.RawJSON)
	case journal.EventFSDJump:
		var jumpEvent journal.FSDJumpEvent
		if json.Unmarshal([]byte(journalEvent.RawJSON), &jumpEvent) == nil {
			builder.session.ApplyFSDJumpEvent(&jumpEvent)
		}
	case journal.EventCarrierJump:
		builder.session.ApplyCarrierJump(journalEvent.RawJSON)
	case journal.EventDocked:
		builder.session.ApplyDocked(journalEvent.RawJSON)
	case journal.EventUndocked:
		builder.session.ApplyUndocked()
	case journal.EventScan:
		builder.recordCartoScan(journalEvent)
	case journal.EventSAAScanComplete:
		builder.recordCartoMapping(journalEvent)
	case journal.EventScanOrganic:
		builder.recordOrganicScan(journalEvent)
	case journal.EventResurrect:
		builder.recordResurrect(journalEvent.RawJSON)
	case journal.EventSellOrganicData:
		builder.recordOrganicSale(journalEvent.RawJSON)
	case journal.EventSellExplorationData:
		builder.recordSingleCartoSale(journalEvent.RawJSON)
	case journal.EventMultiSellExplorationData:
		builder.recordMultiCartoSale(journalEvent.RawJSON)
	}
}

func (builder *ledgerBuilder) recordCartoScan(journalEvent replay.Row) {
	var scanEvent journal.ScanEvent
	if json.Unmarshal([]byte(journalEvent.RawJSON), &scanEvent) != nil || scanEvent.IsBeltCluster() {
		return
	}
	key := bodyKey{systemAddress: scanEvent.SystemAddress, bodyID: scanEvent.BodyID}
	scanCopy := scanEvent
	builder.scans[key] = &scanCopy

	if scanEvent.ScanType == "NavBeaconDetail" || builder.soldScan[key] {
		return
	}
	systemPopulation := builder.session.SystemPopulation(scanEvent.SystemAddress)
	odysseyEnabled := builder.session.OdysseyAt(journalEvent.Timestamp)
	estimatedBodyValue := reference.EstimateBodyValue(
		replay.ToBodyScanWithPopulation(&scanEvent, systemPopulation), reference.MappingInfo{}, odysseyEnabled,
	)
	expectedFirstDiscovery := reference.CanExpectFirstDiscovered(systemPopulation, scanEvent.WasDiscovered, scanEvent.WasMapped)

	if existingEntry := builder.unsoldCarto[key]; existingEntry != nil && existingEntry.status == "pending" {
		if !existingEntry.hasScan {
			existingEntry.theoreticalBase += estimatedBodyValue.Base
			existingEntry.theoreticalBonus += estimatedBodyValue.Bonus
			existingEntry.theoreticalTotal += estimatedBodyValue.Total
			existingEntry.hasScan = true
			if expectedFirstDiscovery {
				existingEntry.undiscoveredAtScan = true
			}
		}
		return
	}

	normalizedSystemName := strings.ToLower(strings.TrimSpace(scanEvent.StarSystem))
	scanEntry := &scanRecord{
		timestamp:            journalEvent.Timestamp,
		systemAddress:        scanEvent.SystemAddress,
		starSystem:           scanEvent.StarSystem,
		normalizedStarSystem: normalizedSystemName,
		bodyID:               scanEvent.BodyID,
		bodyName:             scanEvent.BodyName,
		normalizedBodyName:   strings.ToLower(strings.TrimSpace(scanEvent.BodyName)),
		hasScan:              true,
		undiscoveredAtScan:   expectedFirstDiscovery,
		theoreticalBase:      estimatedBodyValue.Base,
		theoreticalBonus:     estimatedBodyValue.Bonus,
		theoreticalTotal:     estimatedBodyValue.Total,
		status:               "pending",
	}
	builder.unsoldCarto[key] = scanEntry
	builder.unsoldCartoBySystem[normalizedSystemName] = append(builder.unsoldCartoBySystem[normalizedSystemName], scanEntry)
	builder.ledger.records = append(builder.ledger.records, scanEntry)
}

func (builder *ledgerBuilder) recordCartoMapping(journalEvent replay.Row) {
	var mappingEvent journal.SAAScanCompleteEvent
	if json.Unmarshal([]byte(journalEvent.RawJSON), &mappingEvent) != nil {
		return
	}
	key := bodyKey{systemAddress: mappingEvent.SystemAddress, bodyID: mappingEvent.BodyID}
	parentScan := builder.scans[key]
	if parentScan == nil || builder.soldMapped[key] {
		return
	}
	existingEntry := builder.unsoldCarto[key]
	if existingEntry != nil && existingEntry.status == "pending" && existingEntry.hasMapped {
		return
	}

	systemPopulation := builder.session.SystemPopulation(mappingEvent.SystemAddress)
	odysseyEnabled := builder.session.OdysseyAt(journalEvent.Timestamp)
	mappingInfo := reference.MappingInfo{
		Mapped:           true,
		ProbesUsed:       mappingEvent.ProbesUsed,
		EfficiencyTarget: mappingEvent.EfficiencyTarget,
	}
	mappingIncrement := reference.EstimateMappingIncrement(
		replay.ToBodyScanWithPopulation(parentScan, systemPopulation), mappingInfo, odysseyEnabled,
	)
	expectedFirstMapped := reference.CanExpectFirstMapped(systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped)

	if existingEntry != nil && existingEntry.status == "pending" {
		existingEntry.theoreticalBase += mappingIncrement.Base
		existingEntry.theoreticalBonus += mappingIncrement.Bonus
		existingEntry.theoreticalTotal += mappingIncrement.Total
		existingEntry.hasMapped = true
		if expectedFirstMapped {
			existingEntry.undiscoveredAtScan = true
		}
		return
	}

	normalizedSystemName := strings.ToLower(strings.TrimSpace(parentScan.StarSystem))
	// Fall back to CanExpectFirstDiscovered when creating a mapping-only record whose initial
	// Scan was sold in an earlier session so undiscovered status is preserved.
	scanEntry := &scanRecord{
		timestamp:            journalEvent.Timestamp,
		systemAddress:        mappingEvent.SystemAddress,
		starSystem:           parentScan.StarSystem,
		normalizedStarSystem: normalizedSystemName,
		bodyID:               mappingEvent.BodyID,
		bodyName:             mappingEvent.BodyName,
		normalizedBodyName:   strings.ToLower(strings.TrimSpace(mappingEvent.BodyName)),
		hasMapped:            true,
		undiscoveredAtScan:   expectedFirstMapped || reference.CanExpectFirstDiscovered(systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped),
		theoreticalBase:      mappingIncrement.Base,
		theoreticalBonus:     mappingIncrement.Bonus,
		theoreticalTotal:     mappingIncrement.Total,
		status:               "pending",
	}
	builder.unsoldCarto[key] = scanEntry
	builder.unsoldCartoBySystem[normalizedSystemName] = append(builder.unsoldCartoBySystem[normalizedSystemName], scanEntry)
	builder.ledger.records = append(builder.ledger.records, scanEntry)
}

func (builder *ledgerBuilder) recordOrganicScan(journalEvent replay.Row) {
	var scanOrganicEvent journal.ScanOrganicEvent
	if json.Unmarshal([]byte(journalEvent.RawJSON), &scanOrganicEvent) != nil || !strings.EqualFold(scanOrganicEvent.ScanType, "Analyse") {
		return
	}
	parentKey := bodyKey{systemAddress: scanOrganicEvent.SystemAddress, bodyID: scanOrganicEvent.Body}
	parentScan := builder.scans[parentKey]
	systemPopulation := builder.session.SystemPopulation(scanOrganicEvent.SystemAddress)
	firstLogged := parentScan != nil && reference.CanExpectFirstLogged(
		systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped, parentScan.WasFootfalled,
	)
	calculatedOrganicValue := exceptions.EvaluateOrganic(exceptions.OrganicContext{
		ScanTimestamp:    journalEvent.Timestamp,
		SystemAddress:    scanOrganicEvent.SystemAddress,
		SpeciesCodex:     scanOrganicEvent.Species,
		SpeciesLocalised: scanOrganicEvent.SpeciesLocalised,
		GenusCodex:       scanOrganicEvent.Genus,
		FirstLogged:      firstLogged,
	})
	scanEntry := &scanRecord{
		eventUID:           journalEvent.UID,
		timestamp:          journalEvent.Timestamp,
		systemAddress:      scanOrganicEvent.SystemAddress,
		bodyID:             scanOrganicEvent.Body,
		isOrganic:          true,
		undiscoveredAtScan: firstLogged,
		genus:              scanOrganicEvent.Genus,
		species:            scanOrganicEvent.Species,
		speciesLocalised:   scanOrganicEvent.SpeciesLocalised,
		variant:            scanOrganicEvent.Variant,
		variantLocalised:   scanOrganicEvent.VariantLocalised,
		normalizedSpecies:  normalizeSpecies(scanOrganicEvent.Species, scanOrganicEvent.SpeciesLocalised),
		normalizedVariant:  normalizeVariant(scanOrganicEvent.Variant, scanOrganicEvent.VariantLocalised),
		theoreticalBase:    calculatedOrganicValue.Base,
		theoreticalBonus:   calculatedOrganicValue.Bonus,
		theoreticalTotal:   calculatedOrganicValue.Total,
		status:             "pending",
	}
	builder.unsoldOrganic = append(builder.unsoldOrganic, scanEntry)
	builder.ledger.records = append(builder.ledger.records, scanEntry)
}
