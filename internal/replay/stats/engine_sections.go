package stats

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"planisphere/internal/core/db"
	"planisphere/internal/replay"
	"planisphere/internal/sources/journal"
)

var (
	// TravelReplayEvents selects journal events required to replay Flight Operations metrics.
	TravelReplayEvents = journal.NewEventSet(
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
	)
	// CartographicsReplayEvents includes session-context events needed for Odyssey
	// and system population eligibility alongside Scan and SAAScanComplete.
	CartographicsReplayEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventScan,
		journal.EventSAAScanComplete,
	)
	// ExobiologyReplayEvents selects organic analysis events required for exobiology metrics.
	ExobiologyReplayEvents = journal.NewEventSet(
		journal.EventScanOrganic,
	)
)

// QueryCareerCarrierOps loads lifetime Personal Carrier Operations jump and distance totals
// from Frontier's latest Statistics event.
func QueryCareerCarrierOps(queryer db.Queryer) (CarrierOpsMetrics, error) {
	var rawStatistics string
	queryError := queryer.QueryRow(`SELECT raw_json FROM journal_events
		WHERE event = 'Statistics' AND json_extract(raw_json, '$.FLEETCARRIER') IS NOT NULL AND ` + replay.CmdrFilterSQL + `
		ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&rawStatistics)
	if errors.Is(queryError, sql.ErrNoRows) {
		return CarrierOpsMetrics{}, nil
	}
	if queryError != nil {
		return CarrierOpsMetrics{}, fmt.Errorf("query career carrier statistics: %w", queryError)
	}
	if rawStatistics == "" {
		return CarrierOpsMetrics{}, nil
	}
	var statisticsEvent journal.StatisticsEvent
	if json.Unmarshal([]byte(rawStatistics), &statisticsEvent) != nil {
		return CarrierOpsMetrics{}, nil
	}
	return CarrierOpsMetrics{
		CarrierJumps:      int(statisticsEvent.FleetCarrierNumber("FLEETCARRIER_TOTAL_JUMPS")),
		CarrierDistanceLy: statisticsEvent.FleetCarrierNumber("FLEETCARRIER_DISTANCE_TRAVELLED"),
	}, nil
}

// ComputeTravel returns Flight Operations navigation metrics for events in timeRange,
// loading only arrival events (Location, FSDJump, CarrierJump) and recovered jumps.
func (engine *Engine) ComputeTravel(timeRange Range, filter *Filter) (TravelMetrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB, engine.ModeLoader)
	if prepareError != nil {
		return TravelMetrics{}, fmt.Errorf("prepare travel stats filter: %w", prepareError)
	}
	loadedEvents, loadError := replay.LoadEvents(engine.DB, after, until, TravelReplayEvents)
	if loadError != nil {
		return TravelMetrics{}, fmt.Errorf("load travel stats events: %w", loadError)
	}
	accumulator := &computeAccumulator{
		engine:    engine,
		after:     after,
		until:     until,
		filterSet: filterSet,
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processEvent(rowItem, &metrics); stepError != nil {
			return TravelMetrics{}, stepError
		}
	}
	recoveredJumps, recoverError := engine.loadRecoveredJumps(after, until)
	if recoverError != nil {
		return TravelMetrics{}, fmt.Errorf("recover disconnected jumps: %w", recoverError)
	}
	for _, recoveredJump := range recoveredJumps {
		creditJump(recoveredJump.Timestamp, recoveredJump.JumpDist, recoveredJump.BoostUsed, filterSet, &metrics)
	}
	return ExtractTravelMetrics(metrics), nil
}

// ComputeCartographics returns Universal Cartographics scan and mapping metrics
// for events in timeRange, optionally filtered by Discovery mode selections.
func (engine *Engine) ComputeCartographics(timeRange Range, filter *Filter) (CartographicsMetrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB, engine.ModeLoader)
	if prepareError != nil {
		return CartographicsMetrics{}, fmt.Errorf("prepare cartographics stats filter: %w", prepareError)
	}
	loadedEvents, loadError := replay.LoadEvents(engine.DB, after, until, CartographicsReplayEvents)
	if loadError != nil {
		return CartographicsMetrics{}, fmt.Errorf("load cartographics stats events: %w", loadError)
	}

	session := replay.NewSessionState(engine.Odyssey, len(loadedEvents)/8)
	if seedError := replay.SeedLoadGameBefore(engine.DB, after, session); seedError != nil {
		return CartographicsMetrics{}, fmt.Errorf("seed initial loadgame state: %w", seedError)
	}
	if preloadPopError := replay.PreloadSystemPopulations(engine.DB, after, loadedEvents, until, session); preloadPopError != nil {
		return CartographicsMetrics{}, fmt.Errorf("preload system populations: %w", preloadPopError)
	}
	scans, missingScans, preloadScanError := engine.prepareComputeScans(after, loadedEvents)
	if preloadScanError != nil {
		return CartographicsMetrics{}, fmt.Errorf("prepare cartographics scans: %w", preloadScanError)
	}

	accumulator := &computeAccumulator{
		engine:        engine,
		after:         after,
		until:         until,
		filterSet:     filterSet,
		session:       session,
		scans:         scans,
		missingScans:  missingScans,
		scannedBodies: make(map[bodyKey]bool, len(loadedEvents)/2),
		mappedBodies:  make(map[bodyKey]bool, len(loadedEvents)/8),
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processCartographicsEvent(rowItem, &metrics); stepError != nil {
			return CartographicsMetrics{}, stepError
		}
	}
	return ExtractCartographicsMetrics(metrics), nil
}

func (accumulator *computeAccumulator) processCartographicsEvent(rowItem replay.Row, metrics *Metrics) error {
	switch rowItem.Type {
	case journal.EventLoadGame:
		accumulator.session.ApplyLoadGame(rowItem.RawJSON)
	case journal.EventLocation:
		accumulator.session.ApplyLocation(rowItem.RawJSON)
	case journal.EventFSDJump:
		var jumpEvent journal.FSDJumpEvent
		if json.Unmarshal([]byte(rowItem.RawJSON), &jumpEvent) == nil {
			accumulator.session.ApplyFSDJumpEvent(&jumpEvent)
		}
	case journal.EventCarrierJump:
		accumulator.session.ApplyCarrierJump(rowItem.RawJSON)
	case journal.EventScan:
		accumulator.processScan(rowItem, metrics)
	case journal.EventSAAScanComplete:
		return accumulator.processMapping(rowItem, metrics)
	}
	return nil
}

// ComputeExobiology returns Vista Genomics exobiology metrics for events in
// timeRange, loading only ScanOrganic events and batch-preloading their parent
// body scans and system populations.
func (engine *Engine) ComputeExobiology(timeRange Range, filter *Filter) (ExobiologyMetrics, error) {
	var metrics Metrics
	after, until := timeRange.bounds()
	filterSet, prepareError := filter.prepare(engine.DB, engine.ModeLoader)
	if prepareError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("prepare exobiology stats filter: %w", prepareError)
	}
	loadedEvents, loadError := replay.LoadEvents(engine.DB, after, until, ExobiologyReplayEvents)
	if loadError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("load exobiology stats events: %w", loadError)
	}
	if len(loadedEvents) == 0 {
		return ExobiologyMetrics{}, nil
	}

	session := replay.NewSessionState(engine.Odyssey, len(loadedEvents)/4)
	if preloadPopError := replay.PreloadSystemPopulationsForRows(engine.DB, loadedEvents, until, session); preloadPopError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("preload exobiology system populations: %w", preloadPopError)
	}
	chronologicalScans, preloadScanError := engine.prepareExobiologyScans(until, loadedEvents)
	if preloadScanError != nil {
		return ExobiologyMetrics{}, fmt.Errorf("prepare exobiology scans: %w", preloadScanError)
	}

	accumulator := &computeAccumulator{
		engine:             engine,
		after:              after,
		until:              until,
		filterSet:          filterSet,
		session:            session,
		chronologicalScans: chronologicalScans,
	}
	for _, rowItem := range loadedEvents {
		if stepError := accumulator.processOrganic(rowItem, &metrics); stepError != nil {
			return ExobiologyMetrics{}, stepError
		}
	}
	return ExtractExobiologyMetrics(metrics), nil
}

func (engine *Engine) prepareExobiologyScans(untilTimestamp string, loadedEvents []replay.Row) (map[bodyKey][]*journal.ScanEvent, error) {
	seenBodies := make(map[bodyKey]bool)
	var neededKeys []bodyKey
	for _, rowItem := range loadedEvents {
		if rowItem.Type != journal.EventScanOrganic {
			continue
		}
		var organicEvent journal.ScanOrganicEvent
		if json.Unmarshal([]byte(rowItem.RawJSON), &organicEvent) != nil || !strings.EqualFold(organicEvent.ScanType, "Analyse") {
			continue
		}
		key := bodyKey{organicEvent.SystemAddress, organicEvent.Body}
		if !seenBodies[key] {
			seenBodies[key] = true
			neededKeys = append(neededKeys, key)
		}
	}
	if len(neededKeys) == 0 {
		return map[bodyKey][]*journal.ScanEvent{}, nil
	}
	chronologicalScans, preloadError := engine.preloadChronologicalScans(neededKeys, untilTimestamp)
	if preloadError != nil {
		return nil, fmt.Errorf("preload exobiology parent scans before %s: %w", untilTimestamp, preloadError)
	}
	return chronologicalScans, nil
}
