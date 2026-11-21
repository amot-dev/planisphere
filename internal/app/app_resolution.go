package app

import (
	"html"
	"log"
	"strings"
	"time"

	"planisphere/internal/core/events"
	"planisphere/internal/reference"
	"planisphere/internal/sources/edsm"
	"planisphere/internal/store/expedition"
)

func (app *App) runExpeditionResolutionWorker(expeditionID int64, allowRetry bool) {
	defer func() {
		app.resolveMu.Lock()
		delete(app.resolvingExpeditions, expeditionID)
		app.resolveMu.Unlock()
	}()

	app.dbMu.RLock()
	if app.db == nil {
		app.dbMu.RUnlock()
		return
	}
	rows, listError := expedition.ListRows(app.db, expeditionID)
	app.dbMu.RUnlock()
	if listError != nil {
		return
	}

	anyCoords := false
	anyUpdated := false
	lastEmit := time.Now()
	for _, rowItem := range rows {
		if app.ctx != nil {
			select {
			case <-app.ctx.Done():
				return
			default:
			}
		}
		if !rowItem.IsWaypoint() || rowItem.SystemName == "" || rowItem.HasCoords {
			continue
		}
		online := false
		switch rowItem.CoordsSource {
		case "":
			online = true
		case "notfound", "error":
			online = allowRetry
		default:
			continue
		}
		resolvedCoords, updated := app.resolveRowSync(rowItem, online)
		if updated {
			anyUpdated = true
			if resolvedCoords {
				anyCoords = true
			}
			if time.Since(lastEmit) > 1*time.Second {
				if anyCoords {
					app.reevaluate(expeditionID)
				}
				app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
				lastEmit = time.Now()
			}
		}
	}
	if anyUpdated {
		if anyCoords {
			app.reevaluate(expeditionID)
		}
		app.publishApp(expeditionID, events.AppExpeditionPlanChanged)
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
	app.resolveMu.Lock()
	if app.resolvingExpeditions[expeditionID] {
		app.resolveMu.Unlock()
		return
	}
	app.resolvingExpeditions[expeditionID] = true
	app.resolveMu.Unlock()

	if !app.spawnBackground(func() {
		app.runExpeditionResolutionWorker(expeditionID, allowRetry)
	}) {
		app.resolveMu.Lock()
		delete(app.resolvingExpeditions, expeditionID)
		app.resolveMu.Unlock()
	}
}

func (app *App) lookupRowSystemInfo(systemName string, online bool) (*edsm.Info, error, bool) {
	if online {
		if app.ctx != nil {
			select {
			case <-app.ctx.Done():
				return nil, nil, false
			case app.resolutionSemaphore <- struct{}{}:
			}
		} else {
			app.resolutionSemaphore <- struct{}{}
		}
		defer func() {
			<-app.resolutionSemaphore
		}()
		app.dbMu.RLock()
		coordinateResolver := app.resolver
		app.dbMu.RUnlock()
		if coordinateResolver == nil {
			return nil, nil, true
		}
		resolvedInfo, lookupError := coordinateResolver.Resolve(app.ctx, systemName)
		return resolvedInfo, lookupError, true
	}

	var resolvedInfo *edsm.Info
	var lookupError error
	app.dbMu.RLock()
	database := app.db
	if database != nil {
		resolvedInfo, lookupError = edsm.FromJournal(database, systemName)
	}
	app.dbMu.RUnlock()
	if resolvedInfo == nil && lookupError == nil {
		return nil, nil, false
	}
	return resolvedInfo, lookupError, true
}

func (app *App) resolveRowSync(rowItem expedition.Row, online bool) (resolvedCoords bool, updated bool) {
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

	resolvedInfo, lookupError, shouldStore := app.lookupRowSystemInfo(rowItem.SystemName, online)
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
	if app.db == nil {
		return false, false
	}
	if setCoordsError := expedition.SetRowCoords(app.db, rowItem.ID, rowItem.SystemName, systemAddress, edsmID, positionCoords, source); setCoordsError != nil {
		log.Printf("store coords: %v", setCoordsError)
		return false, false
	}
	// Populate POI name from EDAstro GEC or GMP if the waypoint does not already have one set.
	app.applyMissingPOIFromGEC(rowItem.ID, rowItem.SystemName, systemAddress)
	return positionCoords != nil, true
}

func (app *App) resolveRowAsync(rowItem expedition.Row, online bool) {
	app.spawnBackground(func() {
		resolvedCoords, updated := app.resolveRowSync(rowItem, online)
		if updated {
			if resolvedCoords {
				app.reevaluate(rowItem.ExpeditionID)
			}
			app.publishApp(rowItem.ExpeditionID, events.AppExpeditionPlanChanged)
		}
	})
}

// applyMissingPOIFromGEC looks up systemName and systemAddress in the GEC catalog,
// HTML-unescapes the matched POI name, stores it on rowID when poi_name is still
// blank, and returns the persisted name (or "" when none matched or persisted).
// Caller must hold app.dbMu.RLock.
func (app *App) applyMissingPOIFromGEC(rowID int64, systemName string, systemAddress int64) string {
	if app.db == nil || app.gec == nil || systemName == "" {
		return ""
	}
	matchedPOIs := app.gec.Lookup(systemName, systemAddress)
	if len(matchedPOIs) == 0 || strings.TrimSpace(matchedPOIs[0].Name) == "" {
		return ""
	}
	poiName := html.UnescapeString(strings.TrimSpace(matchedPOIs[0].Name))
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
	for _, rowItem := range missingRows {
		app.applyMissingPOIFromGEC(rowItem.ID, rowItem.SystemName, rowItem.SystemAddress)
	}
}
