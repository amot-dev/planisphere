package stats

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/reference"
	"planisphere/internal/reference/exceptions"
	"planisphere/internal/replay"
	"planisphere/internal/replay/realization"
	"planisphere/internal/sources/journal"
)

// prepareComputeScans identifies mapped bodies that have no Scan event in
// loadedEvents and preloads their latest prior Scan at or before afterTimestamp.
// Two passes ensure same-second mappings ordered before their matching Scan in
// loadedEvents are not marked missing.
func (engine *Engine) prepareComputeScans(afterTimestamp string, loadedEvents []replay.Row, scope replay.ProfileScope) (map[bodyKey]*journal.ScanEvent, map[bodyKey]bool, error) {
	scans := make(map[bodyKey]*journal.ScanEvent)
	missingScans := make(map[bodyKey]bool)
	scannedInEvents := make(map[bodyKey]bool, len(loadedEvents)/2)
	for _, rowItem := range loadedEvents {
		if rowItem.Type == journal.EventScan && rowItem.SystemAddress.Valid && rowItem.BodyID.Valid {
			scannedInEvents[bodyKey{rowItem.SystemAddress.Int64, rowItem.BodyID.Int64}] = true
		}
	}
	neededSet := make(map[bodyKey]bool)
	var neededKeys []bodyKey
	for _, rowItem := range loadedEvents {
		if rowItem.Type == journal.EventSAAScanComplete && rowItem.SystemAddress.Valid && rowItem.BodyID.Valid {
			key := bodyKey{rowItem.SystemAddress.Int64, rowItem.BodyID.Int64}
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
	preloaded, preloadError := engine.preloadScans(neededKeys, afterTimestamp, scope)
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
	scope                   replay.ProfileScope
	after                   string
	until                   string
	filterSet               *filterSets
	session                 *replay.SessionState
	previousPosition        reference.Vec3
	hasPreviousPosition     bool
	previousPositionSeeded  bool
	realizationLedger       *realization.Ledger
	realizationLedgerLoaded bool
	scans                   map[bodyKey]*journal.ScanEvent
	chronologicalScans      map[bodyKey][]*journal.ScanEvent
	missingScans            map[bodyKey]bool
	scannedBodies           map[bodyKey]bool
	mappedBodies            map[bodyKey]bool
}

func (accumulator *computeAccumulator) processTravelEvent(rowItem replay.Row, metrics *Metrics) error {
	switch rowItem.Type {
	case journal.EventLocation:
		if len(rowItem.RawJSON) > 0 {
			var locationEvent journal.LocationEvent
			if json.Unmarshal(rowItem.RawJSON, &locationEvent) == nil && locationEvent.Multicrew {
				return nil
			}
		}
		accumulator.recordStarPosition(rowItem.StarPos, rowItem.HasStarPos)
	case journal.EventFSDJump:
		accumulator.processFSDJump(rowItem, metrics)
	case journal.EventCarrierJump:
		return accumulator.processCarrierJump(rowItem, metrics)
	}
	return nil
}

func (accumulator *computeAccumulator) recordStarPosition(starPos reference.Vec3, hasStarPos bool) {
	if !hasStarPos {
		return
	}
	accumulator.previousPosition = starPos
	accumulator.hasPreviousPosition = true
}

func (accumulator *computeAccumulator) ensurePreviousPosition() error {
	if accumulator.hasPreviousPosition || accumulator.previousPositionSeeded {
		return nil
	}
	accumulator.previousPositionSeeded = true
	if accumulator.after == "" {
		return nil
	}
	var starX, starY, starZ sql.NullFloat64
	queryError := accumulator.engine.DB.QueryRow(`SELECT star_x, star_y, star_z FROM journal_events
		WHERE event IN ('FSDJump', 'Location', 'CarrierJump') AND star_x IS NOT NULL
		AND timestamp <= ? AND `+accumulator.scope.SQLPredicate("")+` AND `+replay.NonMulticrewSQLFilter+`
		ORDER BY timestamp DESC, id DESC LIMIT 1`, accumulator.after).Scan(&starX, &starY, &starZ)
	if queryError != nil && !errors.Is(queryError, sql.ErrNoRows) {
		return fmt.Errorf("seed previous star position before %s: %w", accumulator.after, queryError)
	}
	if starX.Valid && starY.Valid && starZ.Valid {
		accumulator.previousPosition = reference.Vec3{X: starX.Float64, Y: starY.Float64, Z: starZ.Float64}
		accumulator.hasPreviousPosition = true
	}
	return nil
}

func creditJump(timestamp string, distanceLy float64, boostUsed int, filterSet *filterSets, metrics *Metrics) {
	if !filterSet.allow(timestamp, false) {
		return
	}
	metrics.Jumps++
	metrics.ShipDistanceLy += distanceLy
	metrics.DistanceLy += distanceLy
	switch boostUsed {
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

func (accumulator *computeAccumulator) processFSDJump(rowItem replay.Row, metrics *Metrics) {
	var jumpEvent journal.FSDJumpEvent
	if json.Unmarshal(rowItem.RawJSON, &jumpEvent) != nil {
		return
	}
	if jumpEvent.Multicrew {
		return
	}
	// Apex Interstellar Taxis physically move the player across systems, whereas
	// Telepresence Multicrew leaves the player's ship behind, so Taxi jumps still
	// update previousPosition while Multicrew jumps must not.
	accumulator.recordStarPosition(rowItem.StarPos, rowItem.HasStarPos)
	if jumpEvent.Taxi {
		return
	}
	creditJump(rowItem.Timestamp, jumpEvent.JumpDist, jumpEvent.BoostUsed, accumulator.filterSet, metrics)
}

func creditCarrierPassengerDistance(timestamp string, distanceLy float64, filterSet *filterSets, metrics *Metrics) {
	if distanceLy <= 0 || !filterSet.allow(timestamp, false) {
		return
	}
	metrics.CarrierPassengerLy += distanceLy
	metrics.DistanceLy += distanceLy
}

func (accumulator *computeAccumulator) processCarrierJump(rowItem replay.Row, metrics *Metrics) error {
	if seedError := accumulator.ensurePreviousPosition(); seedError != nil {
		return seedError
	}
	var legDistanceLy float64
	if accumulator.hasPreviousPosition && rowItem.HasStarPos {
		legDistanceLy = reference.Distance(accumulator.previousPosition, rowItem.StarPos)
	}
	accumulator.recordStarPosition(rowItem.StarPos, rowItem.HasStarPos)
	creditCarrierPassengerDistance(rowItem.Timestamp, legDistanceLy, accumulator.filterSet, metrics)
	return nil
}

func (accumulator *computeAccumulator) processScan(rowItem replay.Row, metrics *Metrics) {
	var scanEvent journal.ScanEvent
	if json.Unmarshal(rowItem.RawJSON, &scanEvent) != nil || scanEvent.IsBeltCluster() {
		return
	}
	key := bodyKey{scanEvent.SystemAddress, scanEvent.BodyID}
	scanCopy := scanEvent
	accumulator.scans[key] = &scanCopy
	if !accumulator.filterSet.allow(rowItem.Timestamp, true) {
		return
	}
	metrics.BodiesScanned++
	if scanEvent.ScanType == "NavBeaconDetail" || accumulator.scannedBodies[key] {
		return
	}
	accumulator.scannedBodies[key] = true
	systemPopulation := accumulator.session.SystemPopulation(scanEvent.SystemAddress)
	if reference.CanExpectFirstDiscovered(systemPopulation, scanEvent.WasDiscovered, scanEvent.WasMapped) {
		metrics.FirstDiscoveries++
	}
	odysseyEnabled := accumulator.session.OdysseyAt(rowItem.Timestamp)
	metrics.addScan(reference.EstimateBodyValue(
		replay.ToBodyScanWithPopulation(&scanEvent, systemPopulation), reference.MappingInfo{}, odysseyEnabled,
	))
}

func (accumulator *computeAccumulator) processMapping(rowItem replay.Row, metrics *Metrics) error {
	var saaEvent journal.SAAScanCompleteEvent
	if json.Unmarshal(rowItem.RawJSON, &saaEvent) != nil {
		return nil
	}
	key := bodyKey{saaEvent.SystemAddress, saaEvent.BodyID}
	// Planetary ring hotspot scans log EfficiencyTarget == 0 and do not map planets.
	if accumulator.mappedBodies[key] || !accumulator.filterSet.allow(rowItem.Timestamp, true) || saaEvent.EfficiencyTarget <= 0 {
		return nil
	}
	accumulator.mappedBodies[key] = true
	metrics.BodiesMapped++
	mappingInfo := reference.MappingInfo{Mapped: true, ProbesUsed: saaEvent.ProbesUsed, EfficiencyTarget: saaEvent.EfficiencyTarget}
	if mappingInfo.Efficient() {
		metrics.EfficientMappings++
	}
	parentScan, lookupError := accumulator.resolveParentScan(key, rowItem.Timestamp)
	if lookupError != nil || parentScan == nil {
		return lookupError
	}
	systemPopulation := accumulator.session.SystemPopulation(saaEvent.SystemAddress)
	bodyScan := replay.ToBodyScanWithPopulation(parentScan, systemPopulation)
	if !bodyScan.IsValuable() || bodyScan.IsStar() {
		return nil
	}
	if reference.CanExpectFirstMapped(systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped) {
		metrics.FirstMapped++
	}
	odysseyEnabled := accumulator.session.OdysseyAt(rowItem.Timestamp)
	metrics.addMapping(reference.EstimateMappingIncrementBreakdown(bodyScan, mappingInfo, odysseyEnabled))
	return nil
}

func (accumulator *computeAccumulator) processOrganic(rowItem replay.Row, metrics *Metrics) error {
	var organicEvent journal.ScanOrganicEvent
	if json.Unmarshal(rowItem.RawJSON, &organicEvent) != nil || !strings.EqualFold(organicEvent.ScanType, "Analyse") {
		return nil
	}
	if !accumulator.filterSet.allow(rowItem.Timestamp, true) {
		return nil
	}
	metrics.OrganicSamples++
	systemPopulation := accumulator.session.SystemPopulation(organicEvent.SystemAddress)
	firstLoggedBonus := accumulator.firstLogged(&organicEvent, systemPopulation, rowItem.Timestamp)

	var saleOutcome realization.OrganicSaleOutcome
	if rowItem.UID != "" {
		if !accumulator.realizationLedgerLoaded {
			var ledgerError error
			accumulator.realizationLedger, ledgerError = accumulator.engine.loadLedger()
			if ledgerError != nil {
				return ledgerError
			}
			accumulator.realizationLedgerLoaded = true
		}
		saleOutcome, _ = accumulator.realizationLedger.OrganicSaleDetails(rowItem.UID)
		if saleOutcome.BonusRealized {
			firstLoggedBonus = true
		}
	}
	if firstLoggedBonus {
		metrics.FirstLogged++
	}
	organicValuation := exceptions.EvaluateOrganic(exceptions.OrganicContext{
		ScanTimestamp:    rowItem.Timestamp,
		SaleTimestamp:    saleOutcome.SaleTimestamp,
		SystemAddress:    organicEvent.SystemAddress,
		SaleMarketID:     saleOutcome.SaleMarketID,
		SpeciesCodex:     organicEvent.Species,
		SpeciesLocalised: organicEvent.SpeciesLocalised,
		GenusCodex:       organicEvent.Genus,
		FirstLogged:      firstLoggedBonus,
	})
	metrics.addOrganic(organicValuation)
	return nil
}

func latestScanAtOrBefore(bodyScans []*journal.ScanEvent, sampleTimestamp string) *journal.ScanEvent {
	var matchedScan *journal.ScanEvent
	for _, candidate := range bodyScans {
		if candidate.Timestamp == "" || candidate.Timestamp <= sampleTimestamp {
			matchedScan = candidate
		}
	}
	return matchedScan
}

func (accumulator *computeAccumulator) resolveParentScan(key bodyKey, timestamp string) (*journal.ScanEvent, error) {
	parentScan := accumulator.scans[key]
	if parentScan != nil || accumulator.missingScans[key] {
		return parentScan, nil
	}
	lookedUpScan, lookupError := accumulator.engine.lookupScan(key, timestamp, accumulator.scope)
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
func (accumulator *computeAccumulator) firstLogged(organicEvent *journal.ScanOrganicEvent, systemPopulation int64, timestamp string) bool {
	key := bodyKey{organicEvent.SystemAddress, organicEvent.Body}
	parentScan := latestScanAtOrBefore(accumulator.chronologicalScans[key], timestamp)
	if parentScan == nil {
		return false
	}
	return reference.CanExpectFirstLogged(systemPopulation, parentScan.WasDiscovered, parentScan.WasMapped, parentScan.WasFootfalled)
}
