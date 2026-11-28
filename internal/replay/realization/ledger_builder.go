package realization

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/reference/exceptions"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

type actorKey struct {
	profileID       int64
	nocaseCommander string
}

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
	profileID           int64
	nocaseCommander     string
	session             *replay.SessionState
	wageMatcher         *crewWageMatcher
	hasPriorSale        bool
	lastSaleUnixSeconds int64
	scans               map[bodyKey]*journal.ScanEvent
	unsoldCarto         map[bodyKey]*scanRecord
	unsoldCartoBySystem map[string][]*scanRecord
	soldScan            map[bodyKey]bool
	soldMapped          map[bodyKey]bool
	unsoldOrganic       []*scanRecord
	seenSales           map[saleDedupKey]bool
}

// LedgerState retains chronological per-actor replay state and a primary-key
// watermark so monotonic live journal appends can advance the realization
// ledger incrementally without replaying full archive history.
type LedgerState struct {
	mu                    sync.Mutex
	consumed              bool
	hasUnresolvedProfiles bool
	defaultOdyssey        bool
	lastEventID           int64
	lastTimestamp         string
	actors                map[actorKey]*ledgerBuilder
	ledger                *Ledger
}

// Ledger returns the immutable realization ledger snapshot at the current watermark.
func (state *LedgerState) Ledger() *Ledger {
	if state == nil {
		return nil
	}
	return state.ledger
}

func actorKeyForRow(row replay.Row) actorKey {
	if row.ProfileID > 0 {
		return actorKey{profileID: row.ProfileID}
	}
	return actorKey{nocaseCommander: replay.SqliteNocaseKey(strings.TrimSpace(row.Commander))}
}

// BuildLedger constructs the full chronological lifecycle realization ledger
// across all commander save-slot profiles from queryer.
func BuildLedger(queryer db.Queryer, defaultOdyssey bool) (*Ledger, error) {
	state, buildError := BuildLedgerState(queryer, defaultOdyssey)
	if buildError != nil {
		return nil, buildError
	}
	return state.Ledger(), nil
}

// BuildLedgerState constructs the full chronological realization ledger and
// retains per-actor replay state and primary-key watermark for incremental advances.
func BuildLedgerState(queryer db.Queryer, defaultOdyssey bool) (*LedgerState, error) {
	enabledActors, actorErr := replay.LoadProfileMatcher(queryer, replay.ProfileScope{})
	if actorErr != nil {
		return nil, actorErr
	}
	loadedEvents, loadError := replay.LoadEventsAllProfiles(queryer, "", "9999-12-31T23:59:59Z", SalesLedgerReplayEvents)
	if loadError != nil {
		return nil, loadError
	}

	estimatedPages := (len(loadedEvents)/2 + ledgerPageSize - 1) >> ledgerPageShift
	sharedLedger := &Ledger{
		recordPages:        make([][]*scanRecord, 0, estimatedPages),
		pageCloned:         make([]bool, 0, estimatedPages),
		organicSales:       make(map[string]OrganicSaleOutcome),
		enabledActors:      enabledActors,
		organicSalesCloned: true,
	}
	if len(loadedEvents) == 0 {
		sharedLedger.sealSnapshot()
		return &LedgerState{
			defaultOdyssey: defaultOdyssey,
			actors:         make(map[actorKey]*ledgerBuilder),
			ledger:         sharedLedger,
		}, nil
	}

	wageMatchers, countsByActor := buildActorWageMatchersAndCounts(loadedEvents)
	actors := make(map[actorKey]*ledgerBuilder, len(countsByActor))
	lastEventID, lastTimestamp, hasUnresolved := applyEventsToActors(sharedLedger, actors, loadedEvents, defaultOdyssey, countsByActor, wageMatchers)
	sharedLedger.sealSnapshot()
	return &LedgerState{
		hasUnresolvedProfiles: hasUnresolved,
		defaultOdyssey:        defaultOdyssey,
		lastEventID:           lastEventID,
		lastTimestamp:         lastTimestamp,
		actors:                actors,
		ledger:                sharedLedger,
	}, nil
}

// Advance incrementally applies newly appended journal events after state's
// primary-key watermark, falling back to BuildLedgerState when unresolved
// commander profiles, historical backfills, or split crew-wage flushes require full replay.
func (state *LedgerState) Advance(queryer db.Queryer) (*LedgerState, error) {
	if state == nil {
		return BuildLedgerState(queryer, true)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.consumed || state.hasUnresolvedProfiles {
		return BuildLedgerState(queryer, state.defaultOdyssey)
	}

	newEvents, loadError := replay.LoadEventsAllProfilesAfterID(queryer, state.lastEventID, SalesLedgerReplayEvents)
	if loadError != nil {
		return nil, loadError
	}
	if len(newEvents) == 0 {
		return state, nil
	}
	for _, journalEvent := range newEvents {
		if journalEvent.Timestamp < state.lastTimestamp {
			return BuildLedgerState(queryer, state.defaultOdyssey)
		}
	}
	batchWageMatchers, countsByActor, canIncrement := prepareIncrementalWages(newEvents, state.actors)
	if !canIncrement {
		return BuildLedgerState(queryer, state.defaultOdyssey)
	}
	enabledActors, actorErr := replay.LoadProfileMatcher(queryer, replay.ProfileScope{})
	if actorErr != nil {
		return nil, actorErr
	}

	state.consumed = true
	clonedPages := slices.Clone(state.ledger.recordPages)
	nextLedger := &Ledger{
		recordPages:          clonedPages,
		recordCount:          state.ledger.recordCount,
		organicSales:         state.ledger.organicSales,
		unattributedSales:    state.ledger.unattributedSales,
		enabledActors:        enabledActors,
		publishedRecordCount: state.ledger.recordCount,
		pageCloned:           make([]bool, len(clonedPages)),
	}
	for actor, existingBuilder := range state.actors {
		existingBuilder.ledger = nextLedger
		if extraWages := batchWageMatchers[actor]; extraWages != nil {
			existingBuilder.wageMatcher.wageEvents = append(existingBuilder.wageMatcher.wageEvents, extraWages.wageEvents...)
		}
	}
	maxEventID, maxTimestamp, hasUnresolved := applyEventsToActors(nextLedger, state.actors, newEvents, state.defaultOdyssey, countsByActor, batchWageMatchers)
	nextLedger.sealSnapshot()
	return &LedgerState{
		hasUnresolvedProfiles: hasUnresolved,
		defaultOdyssey:        state.defaultOdyssey,
		lastEventID:           max(state.lastEventID, maxEventID),
		lastTimestamp:         max(state.lastTimestamp, maxTimestamp),
		actors:                state.actors,
		ledger:                nextLedger,
	}, nil
}

func applyEventsToActors(
	sharedLedger *Ledger,
	actors map[actorKey]*ledgerBuilder,
	eventsToApply []replay.Row,
	defaultOdyssey bool,
	countsByActor map[actorKey]int,
	wageMatchers map[actorKey]*crewWageMatcher,
) (int64, string, bool) {
	var cachedKey actorKey
	var cachedBuilder *ledgerBuilder
	var maxEventID int64
	var maxTimestamp string
	var hasUnresolvedProfiles bool

	for _, journalEvent := range eventsToApply {
		if journalEvent.ID > maxEventID {
			maxEventID = journalEvent.ID
		}
		if journalEvent.Timestamp > maxTimestamp {
			maxTimestamp = journalEvent.Timestamp
		}
		if journalEvent.ProfileID == 0 && strings.TrimSpace(journalEvent.Commander) != "" {
			hasUnresolvedProfiles = true
		}
		key := actorKeyForRow(journalEvent)
		if cachedBuilder == nil || key != cachedKey {
			cachedKey = key
			cachedBuilder = actors[key]
			if cachedBuilder == nil {
				var actorWages *crewWageMatcher
				if wageMatchers != nil {
					actorWages = wageMatchers[key]
				}
				cachedBuilder = newActorLedgerBuilder(sharedLedger, key, defaultOdyssey, countsByActor[key], actorWages)
				actors[key] = cachedBuilder
			}
		}
		cachedBuilder.processEvent(journalEvent)
	}
	return maxEventID, maxTimestamp, hasUnresolvedProfiles
}

func newActorLedgerBuilder(
	sharedLedger *Ledger,
	key actorKey,
	defaultOdyssey bool,
	actorEventCount int,
	wageMatcher *crewWageMatcher,
) *ledgerBuilder {
	if wageMatcher == nil {
		wageMatcher = &crewWageMatcher{}
	}
	return &ledgerBuilder{
		ledger:              sharedLedger,
		profileID:           key.profileID,
		nocaseCommander:     key.nocaseCommander,
		session:             replay.NewSessionState(defaultOdyssey, actorEventCount/16),
		wageMatcher:         wageMatcher,
		scans:               make(map[bodyKey]*journal.ScanEvent, max(actorEventCount/2, 8)),
		unsoldCarto:         make(map[bodyKey]*scanRecord, max(actorEventCount/2, 8)),
		unsoldCartoBySystem: make(map[string][]*scanRecord, max(actorEventCount/8, 4)),
		soldScan:            make(map[bodyKey]bool, max(actorEventCount/2, 8)),
		soldMapped:          make(map[bodyKey]bool, max(actorEventCount/8, 4)),
		unsoldOrganic:       make([]*scanRecord, 0, 16),
		seenSales:           make(map[saleDedupKey]bool, max(actorEventCount/16, 4)),
	}
}

func (builder *ledgerBuilder) processEvent(journalEvent replay.Row) {
	switch journalEvent.Type {
	case journal.EventLoadGame:
		builder.session.ApplyLoadGame(journalEvent.RawJSON)
	case journal.EventLocation:
		builder.session.ApplyLocation(journalEvent.RawJSON)
	case journal.EventFSDJump:
		var jumpEvent journal.FSDJumpEvent
		if json.Unmarshal(journalEvent.RawJSON, &jumpEvent) == nil {
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
	if json.Unmarshal(journalEvent.RawJSON, &scanEvent) != nil || scanEvent.IsBeltCluster() {
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
			existingEntry = builder.mutableCartoRecord(key, existingEntry)
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
		profileID:            builder.profileID,
		nocaseCommander:      builder.nocaseCommander,
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
	builder.ledger.appendRecord(scanEntry)
}

func (builder *ledgerBuilder) recordCartoMapping(journalEvent replay.Row) {
	var mappingEvent journal.SAAScanCompleteEvent
	if json.Unmarshal(journalEvent.RawJSON, &mappingEvent) != nil {
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
		existingEntry = builder.mutableCartoRecord(key, existingEntry)
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
		profileID:            builder.profileID,
		nocaseCommander:      builder.nocaseCommander,
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
	builder.ledger.appendRecord(scanEntry)
}

func (builder *ledgerBuilder) mutableCartoRecord(key bodyKey, record *scanRecord) *scanRecord {
	mutableRecord := builder.ledger.ensureMutableRecord(record)
	if mutableRecord == record {
		return record
	}
	builder.unsoldCarto[key] = mutableRecord
	systemRecords := builder.unsoldCartoBySystem[record.normalizedStarSystem]
	for index, candidate := range systemRecords {
		if candidate == record {
			systemRecords[index] = mutableRecord
			break
		}
	}
	return mutableRecord
}

func (builder *ledgerBuilder) noteSaleTimestamp(saleTimestamp string) {
	if unixSeconds, parsedOk := parseUnixSeconds(saleTimestamp); parsedOk {
		if !builder.hasPriorSale || unixSeconds > builder.lastSaleUnixSeconds {
			builder.hasPriorSale = true
			builder.lastSaleUnixSeconds = unixSeconds
		}
	}
}

func (builder *ledgerBuilder) recordOrganicScan(journalEvent replay.Row) {
	var scanOrganicEvent journal.ScanOrganicEvent
	if json.Unmarshal(journalEvent.RawJSON, &scanOrganicEvent) != nil || !strings.EqualFold(scanOrganicEvent.ScanType, "Analyse") {
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
		profileID:          builder.profileID,
		nocaseCommander:    builder.nocaseCommander,
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
	builder.ledger.appendRecord(scanEntry)
}
