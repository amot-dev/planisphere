package app

import (
	"context"
	"database/sql"
	"html"
	"log"
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/edsm"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
)

func (app *App) newCoordinateResolver(dbConn *sql.DB) *edsm.Resolver {
	coordinateResolver := edsm.NewResolver(dbConn, func() bool {
		app.dbMu.RLock()
		defer app.dbMu.RUnlock()
		if app.db != dbConn {
			return false
		}
		return settings.OnlineLookups(dbConn)
	})
	coordinateResolver.JournalLookup = func(systemName string) (*edsm.Info, error) {
		journalSystem, queryError := replay.QueryJournalSystemCoords(dbConn, systemName)
		if queryError != nil || journalSystem == nil {
			return nil, queryError
		}
		position := journalSystem.Pos
		return &edsm.Info{
			Name:          journalSystem.Name,
			SystemAddress: journalSystem.SystemAddress,
			Pos:           &position,
			Source:        "journal",
		}, nil
	}
	coordinateResolver.JournalBatchLookup = func(systemNames []string) (map[string]*edsm.Info, error) {
		journalBatch, queryError := replay.QueryJournalSystemCoordsBatch(dbConn, systemNames)
		if queryError != nil {
			return nil, queryError
		}
		resolvedByKey := make(map[string]*edsm.Info, len(journalBatch))
		for systemKey, journalSystem := range journalBatch {
			position := journalSystem.Pos
			resolvedByKey[systemKey] = &edsm.Info{
				Name:          journalSystem.Name,
				SystemAddress: journalSystem.SystemAddress,
				Pos:           &position,
				Source:        "journal",
			}
		}
		return resolvedByKey, nil
	}
	return coordinateResolver
}

func classifyPendingResolutionRows(rows []expedition.Row) ([]expedition.Row, []string) {
	candidateRows := make([]expedition.Row, 0, len(rows))
	systemNames := make([]string, 0, len(rows))
	for _, rowItem := range rows {
		if !rowItem.IsWaypoint() || rowItem.SystemName == "" || rowItem.HasCoords {
			continue
		}
		if rowItem.CoordsSource != "" && rowItem.CoordsSource != "notfound" && rowItem.CoordsSource != "error" {
			continue
		}
		candidateRows = append(candidateRows, rowItem)
		systemNames = append(systemNames, rowItem.SystemName)
	}
	return candidateRows, systemNames
}

func isRowOnlineEligible(rowItem expedition.Row, allowRetry bool) bool {
	return rowItem.CoordsSource == "" || ((rowItem.CoordsSource == "notfound" || rowItem.CoordsSource == "error") && allowRetry)
}

func (app *App) isRowResolving(rowID int64) bool {
	app.resolveMu.Lock()
	inFlight := app.resolving[rowID]
	app.resolveMu.Unlock()
	return inFlight
}

type localResolutionBatch struct {
	resolvedRowIDs map[int64]bool
	anyUpdated     bool
	anyCoords      bool
	onlineLookups  bool
}

// buildLocalWaypointUpdate builds a transactional update when localSystem has
// coordinates, or marks online-eligible rows notfound offline when onlineLookups is false.
// Caller must hold app.dbMu.RLock.
func (app *App) buildLocalWaypointUpdate(
	rowItem expedition.Row,
	localSystem *edsm.Info,
	onlineLookups bool,
	allowRetry bool,
) (expedition.ResolvedWaypointUpdate, bool) {
	if localSystem != nil && localSystem.Pos != nil {
		if app.isRowResolving(rowItem.ID) {
			return expedition.ResolvedWaypointUpdate{}, false
		}
		poiName := ""
		if strings.TrimSpace(rowItem.POIName) == "" {
			poiName = app.lookupGECPOIName(rowItem.SystemName, localSystem.SystemAddress)
		}
		return expedition.ResolvedWaypointUpdate{
			RowID:         rowItem.ID,
			SystemName:    rowItem.SystemName,
			SystemAddress: localSystem.SystemAddress,
			EDSMID:        localSystem.EDSMID,
			Pos:           localSystem.Pos,
			Source:        localSystem.Source,
			POIName:       poiName,
		}, true
	}
	if !onlineLookups && isRowOnlineEligible(rowItem, allowRetry) && !app.isRowResolving(rowItem.ID) {
		poiName := ""
		if strings.TrimSpace(rowItem.POIName) == "" {
			poiName = app.lookupGECPOIName(rowItem.SystemName, rowItem.SystemAddress)
		}
		return expedition.ResolvedWaypointUpdate{
			RowID:      rowItem.ID,
			SystemName: rowItem.SystemName,
			Source:     "notfound",
			POIName:    poiName,
		}, true
	}
	return expedition.ResolvedWaypointUpdate{}, false
}

func (app *App) resolvePendingRowsLocally(
	targetDB *sql.DB,
	expeditionID int64,
	candidateRows []expedition.Row,
	systemNames []string,
	allowRetry bool,
) localResolutionBatch {
	result := localResolutionBatch{resolvedRowIDs: make(map[int64]bool)}
	if len(candidateRows) == 0 {
		return result
	}

	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db != targetDB || targetDB == nil || app.resolver == nil {
		return result
	}
	result.onlineLookups = settings.OnlineLookups(targetDB)
	localByKey, resolveError := app.resolver.ResolveLocalBatch(systemNames)
	if resolveError != nil {
		log.Printf("batch resolve expedition %d waypoints locally: %v", expeditionID, resolveError)
		return result
	}

	localUpdates := make([]expedition.ResolvedWaypointUpdate, 0, len(candidateRows))
	for _, rowItem := range candidateRows {
		localSystem := localByKey[replay.NormalizeSystemKey(rowItem.SystemName)]
		if update, shouldApply := app.buildLocalWaypointUpdate(rowItem, localSystem, result.onlineLookups, allowRetry); shouldApply {
			localUpdates = append(localUpdates, update)
		}
	}
	if applyError := expedition.ApplyResolvedWaypointsTx(targetDB, localUpdates); applyError != nil {
		log.Printf("store locally resolved waypoints for expedition %d: %v", expeditionID, applyError)
		return result
	}
	result.anyUpdated = len(localUpdates) > 0
	for _, update := range localUpdates {
		result.resolvedRowIDs[update.RowID] = true
		if update.Pos != nil {
			result.anyCoords = true
		}
	}
	return result
}

func (app *App) listExpeditionRowsForResolution(targetDB *sql.DB, expeditionID int64) ([]expedition.Row, bool) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db != targetDB || targetDB == nil {
		return nil, false
	}
	rows, listError := expedition.ListRows(targetDB, expeditionID)
	return rows, listError == nil
}

func (app *App) resolvePendingRowsOnline(
	workerContext context.Context,
	targetDB *sql.DB,
	candidateRows []expedition.Row,
	resolvedRowIDs map[int64]bool,
	allowRetry bool,
) (anyCoords bool, anyUpdated bool) {
	for _, rowItem := range candidateRows {
		if resolvedRowIDs[rowItem.ID] || !isRowOnlineEligible(rowItem, allowRetry) {
			continue
		}
		if workerContext.Err() != nil || !app.isCurrentDB(targetDB) {
			break
		}
		resolvedCoords, updated := app.resolveRowSyncForDB(targetDB, rowItem, true)
		if updated {
			anyUpdated = true
			if resolvedCoords {
				anyCoords = true
			}
		}
	}
	return anyCoords, anyUpdated
}

func (app *App) runExpeditionResolutionWorker(targetDB *sql.DB, expeditionID int64, allowRetry bool) {
	defer func() {
		app.resolveMu.Lock()
		delete(app.resolvingExpeditions, expeditionID)
		app.resolveMu.Unlock()
	}()

	workerContext := app.context()
	if workerContext.Err() != nil {
		return
	}
	rows, loaded := app.listExpeditionRowsForResolution(targetDB, expeditionID)
	if !loaded {
		return
	}
	candidateRows, systemNames := classifyPendingResolutionRows(rows)
	if len(candidateRows) == 0 {
		return
	}

	localBatch := app.resolvePendingRowsLocally(targetDB, expeditionID, candidateRows, systemNames, allowRetry)
	if localBatch.anyUpdated && !app.isShuttingDown() {
		reevalChanged := localBatch.anyCoords && app.reevaluateForDB(targetDB, expeditionID)
		if reevalChanged {
			app.publishAppForDB(targetDB, expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionModesChanged)
		} else {
			app.publishAppForDB(targetDB, expeditionID, events.AppExpeditionPlanChanged)
		}
	}
	if !localBatch.onlineLookups {
		return
	}

	anyOnlineCoords, anyOnlineUpdated := app.resolvePendingRowsOnline(workerContext, targetDB, candidateRows, localBatch.resolvedRowIDs, allowRetry)
	if anyOnlineUpdated && !app.isShuttingDown() {
		reevalChanged := anyOnlineCoords && app.reevaluateForDB(targetDB, expeditionID)
		if reevalChanged {
			app.publishAppForDB(targetDB, expeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionModesChanged)
		} else {
			app.publishAppForDB(targetDB, expeditionID, events.AppExpeditionPlanChanged)
		}
	}
}

// kickResolution resolves coordinates for waypoints of an expedition that do
// not have them yet. allowRetry also retries rows previously marked not-found
// (journal lookup first, so a visit fills them in offline). Runs in a single
// sequential worker goroutine per expedition to avoid goroutine explosion and
// excessive database re-evaluation spam.
func (app *App) kickResolution(expeditionID int64, allowRetry bool) {
	if expeditionID == 0 {
		return
	}
	targetDB := app.currentDB()
	if targetDB == nil {
		return
	}
	app.resolveMu.Lock()
	if app.resolvingExpeditions[expeditionID] {
		app.resolveMu.Unlock()
		return
	}
	app.resolvingExpeditions[expeditionID] = true
	app.resolveMu.Unlock()

	if !app.spawnBackground(func() {
		app.runExpeditionResolutionWorker(targetDB, expeditionID, allowRetry)
	}) {
		app.resolveMu.Lock()
		delete(app.resolvingExpeditions, expeditionID)
		app.resolveMu.Unlock()
	}
}

func (app *App) lookupRowSystemInfo(targetDB *sql.DB, systemName string, online bool) (*edsm.Info, error, bool) {
	if online {
		lookupContext := app.context()
		select {
		case <-lookupContext.Done():
			return nil, nil, false
		case app.resolutionSemaphore <- struct{}{}:
		}
		defer func() {
			<-app.resolutionSemaphore
		}()
		app.dbMu.RLock()
		var coordinateResolver *edsm.Resolver
		dbMatches := app.db == targetDB && targetDB != nil
		if dbMatches {
			coordinateResolver = app.resolver
		}
		app.dbMu.RUnlock()
		if !dbMatches {
			return nil, nil, false
		}
		if coordinateResolver == nil {
			return nil, nil, true
		}
		resolvedInfo, lookupError := coordinateResolver.Resolve(lookupContext, systemName)
		return resolvedInfo, lookupError, true
	}

	var resolvedInfo *edsm.Info
	var lookupError error
	app.dbMu.RLock()
	dbMatches := app.db == targetDB && targetDB != nil
	if dbMatches && app.resolver != nil {
		resolvedInfo, lookupError = app.resolver.ResolveLocal(systemName)
	}
	app.dbMu.RUnlock()
	if !dbMatches || (resolvedInfo == nil && lookupError == nil) {
		return nil, nil, false
	}
	return resolvedInfo, lookupError, true
}

func (app *App) resolveRowSync(rowItem expedition.Row, online bool) (resolvedCoords bool, updated bool) {
	return app.resolveRowSyncForDB(app.currentDB(), rowItem, online)
}

func (app *App) resolveRowSyncForDB(targetDB *sql.DB, rowItem expedition.Row, online bool) (resolvedCoords bool, updated bool) {
	app.resolveMu.Lock()
	if app.resolving[rowItem.ID] {
		app.resolveMu.Unlock()
		return false, false
	}
	app.resolving[rowItem.ID] = true
	app.resolveMu.Unlock()

	defer func() {
		app.resolveMu.Lock()
		delete(app.resolving, rowItem.ID)
		app.resolveMu.Unlock()
	}()

	resolvedInfo, lookupError, shouldStore := app.lookupRowSystemInfo(targetDB, rowItem.SystemName, online)
	if !shouldStore {
		return false, false
	}

	source := "error"
	var systemAddress, edsmID int64
	var positionCoords *reference.Vec3
	switch {
	case resolvedInfo != nil && resolvedInfo.Pos != nil:
		source, systemAddress, edsmID, positionCoords = resolvedInfo.Source, resolvedInfo.SystemAddress, resolvedInfo.EDSMID, resolvedInfo.Pos
	case resolvedInfo != nil:
		source = "notfound"
	case lookupError != nil && strings.Contains(lookupError.Error(), edsm.ErrorNotFound.Error()):
		source = "notfound"
	}

	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db != targetDB || targetDB == nil {
		return false, false
	}
	if setCoordsError := expedition.SetRowCoords(targetDB, rowItem.ID, rowItem.SystemName, systemAddress, edsmID, positionCoords, source); setCoordsError != nil {
		log.Printf("store coords: %v", setCoordsError)
		return false, false
	}
	// Populate POI name from EDAstro GEC or GMP if the waypoint does not already have one set.
	app.applyMissingPOIFromGEC(rowItem.ID, rowItem.SystemName, systemAddress)
	return positionCoords != nil, true
}

func (app *App) resolveRowAsync(targetDB *sql.DB, rowItem expedition.Row, online bool) {
	app.spawnBackground(func() {
		resolvedCoords, updated := app.resolveRowSyncForDB(targetDB, rowItem, online)
		if updated && !app.isShuttingDown() {
			reevalChanged := resolvedCoords && app.reevaluateForDB(targetDB, rowItem.ExpeditionID)
			if reevalChanged {
				app.publishAppForDB(targetDB, rowItem.ExpeditionID, events.AppExpeditionPlanChanged, events.AppExpeditionModesChanged)
			} else {
				app.publishAppForDB(targetDB, rowItem.ExpeditionID, events.AppExpeditionPlanChanged)
			}
		}
	})
}

// lookupGECPOIName returns the HTML-unescaped name of the primary cached GEC or
// GMP POI matching systemName or systemAddress, or "" when none matches.
// Caller must hold app.dbMu.RLock.
func (app *App) lookupGECPOIName(systemName string, systemAddress int64) string {
	if app.gec == nil || systemName == "" {
		return ""
	}
	matchedPOIs := app.gec.Lookup(systemName, systemAddress)
	if len(matchedPOIs) == 0 || strings.TrimSpace(matchedPOIs[0].Name) == "" {
		return ""
	}
	return html.UnescapeString(strings.TrimSpace(matchedPOIs[0].Name))
}

// applyMissingPOIFromGEC looks up systemName and systemAddress in the GEC catalog,
// HTML-unescapes the matched POI name, stores it on rowID when poi_name is still
// blank, and returns the persisted name (or "" when none matched or persisted).
// Caller must hold app.dbMu.RLock.
func (app *App) applyMissingPOIFromGEC(rowID int64, systemName string, systemAddress int64) string {
	if app.db == nil {
		return ""
	}
	poiName := app.lookupGECPOIName(systemName, systemAddress)
	if poiName == "" {
		return ""
	}
	if execError := expedition.SetRowPOIIfBlank(app.db, rowID, poiName); execError != nil {
		log.Printf("update poi name for row %d: %v", rowID, execError)
		return ""
	}
	return poiName
}

func (app *App) populateMissingPOIs() {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if app.db == nil || app.gec == nil {
		return
	}
	missingRows, queryError := expedition.ListMissingPOIRows(app.db)
	if queryError != nil {
		log.Printf("query missing poi rows: %v", queryError)
		return
	}
	poiUpdates := make([]expedition.RowPOIUpdate, 0, len(missingRows))
	for _, rowItem := range missingRows {
		if poiName := app.lookupGECPOIName(rowItem.SystemName, rowItem.SystemAddress); poiName != "" {
			poiUpdates = append(poiUpdates, expedition.RowPOIUpdate{
				RowID:      rowItem.ID,
				SystemName: rowItem.SystemName,
				POIName:    poiName,
			})
		}
	}
	if updateError := expedition.SetMissingRowPOIsTx(app.db, poiUpdates); updateError != nil {
		log.Printf("populate missing poi names: %v", updateError)
	}
}
