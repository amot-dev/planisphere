package profile

import (
	"database/sql"
	"fmt"

	"planisphere/internal/replay"
	"planisphere/internal/sources/gec"
	"planisphere/internal/store/expedition"
)

func runArchiveProbes(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	options Options,
) ([]ProbeResult, error) {
	var results []ProbeResult

	if matchesFilter("Journal/ArchiveStatus", options.FilterPattern) {
		archiveProbe, err := profileArchiveStatus(database, proj, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		results = append(results, archiveProbe)
	}

	if matchesFilter("Journal/ListCommanders", options.FilterPattern) {
		cmdrProbe, err := profileListCommanders(database, proj, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		results = append(results, cmdrProbe)
	}

	if dataset.GECPOICount > 0 && matchesFilter("GEC/Reload", options.FilterPattern) {
		gecProbe, err := profileGECReload(database, proj, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		results = append(results, gecProbe)
	}

	if dataset.PrimaryExpeditionID > 0 && matchesFilter("Expedition/Clone", options.FilterPattern) {
		cloneProbe, err := profileExpeditionClone(database, proj, recorder, dataset, options.Iterations)
		if err != nil {
			return nil, err
		}
		results = append(results, cloneProbe)
	}

	return results, nil
}

func profileArchiveStatus(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Journal/ArchiveStatus",
		"Archive",
		"Query archive file count, total events, and first/last timestamps",
		"replay.ArchiveStatus",
		"events",
		"files",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			status, statusError := replay.ArchiveStatus(database)
			if statusError != nil {
				return singleExecOutcome{}, statusError
			}
			summary := fmt.Sprintf("Files=%d Events=%d First=%s Last=%s", status.FilesTracked, status.EventsStored, shortTimestamp(status.FirstEventAt), shortTimestamp(status.LastEventAt))
			return singleExecOutcome{
				value:     status,
				summary:   summary,
				items:     max(status.EventsStored, 1),
				secondary: status.FilesTracked,
			}, nil
		},
	)
}

func profileListCommanders(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Journal/ListCommanders",
		"Archive",
		"Load discovered commanders and per-commander event counts (Cold vs. Warm cache)",
		"replay.ListCommanders",
		"events",
		"commanders",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			commanders, listError := replay.ListCommanders(database)
			if listError != nil {
				return singleExecOutcome{}, listError
			}
			var attributedEvents int
			for _, cmdr := range commanders {
				attributedEvents += cmdr.EventCount
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

func profileGECReload(
	database *sql.DB,
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	catalog := gec.NewCatalog(database)
	return runColdWarmProbe(
		database,
		recorder,
		"GEC/Reload",
		"Catalog",
		"Rebuild in-memory GEC/GMP POI lookup tables from gec_pois_cache",
		"gec.(*Catalog).Reload",
		"gec_pois",
		"pois",
		iterations,
		proj.invalidate,
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
	proj *profileProjections,
	recorder *treeRecorder,
	dataset DatasetSummary,
	iterations int,
) (ProbeResult, error) {
	return runColdWarmProbe(
		database,
		recorder,
		"Expedition/Clone",
		"Expedition",
		fmt.Sprintf("Clone expedition #%d (%d rows) and clean up clone", dataset.PrimaryExpeditionID, dataset.PrimaryRowCount),
		"expedition.Clone",
		"rows",
		"waypoints",
		iterations,
		proj.invalidate,
		func() (singleExecOutcome, error) {
			cloned, cloneError := expedition.Clone(database, dataset.PrimaryExpeditionID)
			if cloneError != nil {
				return singleExecOutcome{}, cloneError
			}
			_ = expedition.Delete(database, cloned.ID)
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

