package profile

import (
	"database/sql"
	"errors"
	"fmt"

	"planisphere/internal/replay"
	"planisphere/internal/sources/gec"
	"planisphere/internal/store/expedition"
)

type ingestStatusSnapshot struct {
	Archive       replay.Status `json:"archive"`
	CurrentSystem string        `json:"currentSystem"`
	CurrentShip   string        `json:"currentShip"`
}

type carrierStateProbeValue struct {
	LastLocation string                  `json:"lastLocation"`
	Depot        replay.CarrierDepotInfo `json:"depot"`
}

func runArchiveProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, error) {
	var results []ProbeResult

	if matchesFilter("Journal/IngestStatus", options.FilterPattern) {
		ingestProbe, ingestError := profileIngestStatus(database, recorder, options.Iterations)
		if ingestError != nil {
			return nil, ingestError
		}
		results = append(results, ingestProbe)
	}

	if matchesFilter("Journal/ListCommanders", options.FilterPattern) {
		commanderProbe, commanderError := profileListCommanders(database, recorder, dataset, options.Iterations)
		if commanderError != nil {
			return nil, commanderError
		}
		results = append(results, commanderProbe)
	}

	if matchesFilter("Carrier/StateAndDepot", options.FilterPattern) {
		carrierProbe, carrierError := profileCarrierStateAndDepot(database, proj, recorder, options.Iterations)
		if carrierError != nil {
			return nil, carrierError
		}
		results = append(results, carrierProbe)
	}

	if dataset.GECPOICount > 0 && matchesFilter("GEC/Reload", options.FilterPattern) {
		gecProbe, gecError := profileGECReload(database, recorder, dataset, options.Iterations)
		if gecError != nil {
			return nil, gecError
		}
		results = append(results, gecProbe)
	}

	if dataset.PrimaryExpeditionID > 0 && matchesFilter("Expedition/Clone", options.FilterPattern) {
		cloneProbe, cloneError := profileExpeditionClone(database, recorder, dataset, options.Iterations)
		if cloneError != nil {
			return nil, cloneError
		}
		results = append(results, cloneProbe)
	}

	return results, nil
}

func profileIngestStatus(
	database *sql.DB,
	recorder *treeRecorder,
	iterations int,
) (ProbeResult, error) {
	return runSingleModeProbe(
		database,
		recorder,
		"Journal/IngestStatus",
		"Archive",
		"Query archive file and event counts, latest player system, and active ship",
		"app.(*App).GetIngestStatus",
		"events",
		"files",
		iterations,
		nil,
		func() (singleExecOutcome, error) {
			var status replay.Status
			if stepError := recorder.withSubSpan("replay.ArchiveStatus", "internal/replay/archive.go:40", 1, "archive", func() error {
				var statusError error
				status, statusError = replay.ArchiveStatus(database)
				return statusError
			}); stepError != nil {
				return singleExecOutcome{}, stepError
			}

			var currentSystem string
			if stepError := recorder.withSubSpan("replay.QueryLatestPlayerLocation", "internal/replay/arrivals.go:108", 1, "location", func() error {
				_, systemName, locationError := replay.QueryLatestPlayerLocation(database)
				if locationError != nil && !errors.Is(locationError, sql.ErrNoRows) {
					return locationError
				}
				currentSystem = systemName
				return nil
			}); stepError != nil {
				return singleExecOutcome{}, stepError
			}

			var currentShip string
			if stepError := recorder.withSubSpan("replay.QueryCurrentShip", "internal/replay/ship_history.go:18", 1, "ship", func() error {
				shipName, shipError := replay.QueryCurrentShip(database)
				if shipError != nil && !errors.Is(shipError, sql.ErrNoRows) {
					return shipError
				}
				currentShip = shipName
				return nil
			}); stepError != nil {
				return singleExecOutcome{}, stepError
			}

			summary := fmt.Sprintf(
				"Files=%d Events=%d System=%q Ship=%q",
				status.FilesTracked,
				status.EventsStored,
				currentSystem,
				currentShip,
			)
			return singleExecOutcome{
				value: ingestStatusSnapshot{
					Archive:       status,
					CurrentSystem: currentSystem,
					CurrentShip:   currentShip,
				},
				summary:   summary,
				items:     max(status.EventsStored, 1),
				secondary: status.FilesTracked,
			}, nil
		},
	)
}

func profileListCommanders(
	database *sql.DB,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runSingleModeProbe(
		database,
		recorder,
		"Journal/ListCommanders",
		"Archive",
		"Load discovered commanders and per-commander event counts via stateless index-seek query",
		"replay.ListCommanders",
		"events",
		"commanders",
		iterations,
		nil,
		func() (singleExecOutcome, error) {
			commanders, listError := replay.ListCommanders(database)
			if listError != nil {
				return singleExecOutcome{}, listError
			}
			var attributedEvents int
			for _, commander := range commanders {
				attributedEvents += commander.EventCount
			}
			summary := fmt.Sprintf("Commanders=%d AttributedEvents=%d", len(commanders), attributedEvents)
			return singleExecOutcome{
				value:     commanders,
				summary:   summary,
				items:     max(dataset.TotalEvents, 1),
				secondary: len(commanders),
			}, nil
		},
	)
}

func profileCarrierStateAndDepot(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Carrier/StateAndDepot",
		"Archive",
		"Resolve CarrierState projection, latest Fleet Carrier location, and Tritium depot state",
		"projections.(*CarrierState).LatestStats",
		"queries",
		"tritium",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			snapshot, snapshotError := proj.carrierState.LatestStats()
			if snapshotError != nil {
				return singleExecOutcome{}, snapshotError
			}
			depotInfo, depotError := replay.QueryCarrierDepotInfo(database, snapshot)
			if depotError != nil {
				return singleExecOutcome{}, depotError
			}
			lastLocation, locationError := replay.QueryCarrierLastLocation(database, snapshot)
			if locationError != nil {
				return singleExecOutcome{}, locationError
			}
			summary := fmt.Sprintf(
				"Location=%q DepotTritium=%dt CapacityUsed=%dt",
				lastLocation,
				depotInfo.DepotTritium,
				depotInfo.UsedCapacity,
			)
			return singleExecOutcome{
				value: carrierStateProbeValue{
					LastLocation: lastLocation,
					Depot:        depotInfo,
				},
				summary:   summary,
				items:     3,
				secondary: depotInfo.DepotTritium,
			}, nil
		},
	)
}

func profileGECReload(
	database *sql.DB,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	catalog := gec.NewCatalog(database)
	return runSingleModeProbe(
		database,
		recorder,
		"GEC/Reload",
		"Catalog",
		"Rebuild in-memory GEC and GMP POI lookup tables from gec_pois_cache",
		"gec.(*Catalog).Reload",
		"gec_pois",
		"pois",
		iterations,
		nil,
		func() (singleExecOutcome, error) {
			if reloadError := catalog.Reload(); reloadError != nil {
				return singleExecOutcome{}, reloadError
			}
			summary := fmt.Sprintf("POIsIndexed=%d", dataset.GECPOICount)
			return singleExecOutcome{
				value:     dataset.GECPOICount,
				summary:   summary,
				items:     max(dataset.GECPOICount, 1),
				secondary: dataset.GECPOICount,
			}, nil
		},
	)
}

func profileExpeditionClone(
	database *sql.DB,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runSingleModeProbe(
		database,
		recorder,
		"Expedition/Clone",
		"Expedition",
		fmt.Sprintf("Clone expedition #%d (%d rows) and clean up clone", dataset.PrimaryExpeditionID, dataset.PrimaryRowCount),
		"expedition.Clone",
		"rows",
		"waypoints",
		iterations,
		nil,
		func() (singleExecOutcome, error) {
			cloned, cloneError := expedition.Clone(database, dataset.PrimaryExpeditionID)
			if cloneError != nil {
				return singleExecOutcome{}, cloneError
			}
			if deleteError := expedition.Delete(database, cloned.ID); deleteError != nil {
				return singleExecOutcome{}, fmt.Errorf("delete temporary expedition clone %d: %w", cloned.ID, deleteError)
			}
			summary := fmt.Sprintf("ClonedRows=%d Name=%q", dataset.PrimaryRowCount, cloned.Name)
			return singleExecOutcome{
				value:     dataset.PrimaryRowCount,
				summary:   summary,
				items:     max(dataset.PrimaryRowCount, 1),
				secondary: dataset.PrimaryWaypointCount,
			}, nil
		},
	)
}
