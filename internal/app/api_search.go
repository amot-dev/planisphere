package app

import "planisphere/internal/store/expedition"

// SearchWaypoints searches waypoint systems and POI names across all expeditions
// and attaches exact-system non-waypoint journal visits when found.
func (app *App) SearchWaypoints(query string) (expedition.SearchResponse, error) {
	app.dbMu.RLock()
	defer app.dbMu.RUnlock()
	if readyError := app.ready(); readyError != nil {
		return expedition.SearchResponse{Results: []expedition.WaypointSearchResult{}}, readyError
	}
	searchResponse, searchError := expedition.SearchWaypoints(app.db, query, app.expeditionProjections())
	if searchResponse.Results == nil {
		searchResponse.Results = []expedition.WaypointSearchResult{}
	}
	return searchResponse, searchError
}
