package app

import (
	"net/http"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"planisphere/internal/reference"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/httptestutil"
	"planisphere/internal/testutil/journaltest"
)

func TestExpeditionResolutionWorkerBatchOfflineAndCoalescedOnlineEmissions(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	if setOnlineErr := settings.SetOnlineLookups(testApp.db, true); setOnlineErr != nil {
		testingContext.Fatalf("enable online lookups: %v", setOnlineErr)
	}
	if setGECFetchErr := settings.SetGECFetchedAt(testApp.db, reference.NowISO()); setGECFetchErr != nil {
		testingContext.Fatalf("set GEC timestamp: %v", setGECFetchErr)
	}

	journaltest.InsertParsedLines(
		testingContext,
		testApp.db,
		`{"timestamp":"2026-01-01T00:00:00Z","event":"FSDJump","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
	)
	if _, execError := testApp.db.Exec(`
		INSERT INTO system_cache (name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at)
		VALUES ('colonia', 'Colonia', 3238296097059, 43805, -9530.5, -910.28125, 19808.125, 'edsm', '2026-01-01T00:00:00Z');
		INSERT INTO gec_pois_cache (id, name, system_name, system_address, poi_type, region, summary, poi_url, raw_json, source, fetched_at) VALUES
		(101, 'Jaques Station', 'Colonia', 3238296097059, 'Historical', 'Colonia', '', 'https://edastro.com/gec/view/101', '{}', 'GEC', '2026-01-01T00:00:00Z'),
		(102, 'Distant View in Blue', 'Beagle Point', 827689533218, 'Vista', 'The Abyss', '', 'https://edastro.com/gec/view/102', '{}', 'GEC', '2026-01-01T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed resolution fixtures: %v", execError)
	}
	if reloadError := testApp.gec.Reload(); reloadError != nil {
		testingContext.Fatalf("reload GEC: %v", reloadError)
	}

	createdExpedition, createError := expedition.CreateWithWaypoints(testApp.db, "Coalesced Worker Route", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Colonia"},
		{SystemName: "Barnards Loop"},
		{SystemName: "Sgr A Star"},
		{SystemName: "Beagle Point"},
	})
	if createError != nil {
		testingContext.Fatalf("create seeded expedition: %v", createError)
	}

	eventsRecorder := captureAppEvents(testingContext, testApp)

	var onlineRequests atomic.Int32
	offlineVerifiedBeforeFirstHTTP := atomic.Bool{}
	testApp.resolver.EDSMBase = "https://edsm.example.test"
	testApp.resolver.HTTP = httptestutil.NewClient(func(request *http.Request) (*http.Response, error) {
		requestNumber := onlineRequests.Add(1)
		if requestNumber == 1 {
			rowsBeforeOnline, listBeforeErr := expedition.ListRows(testApp.db, createdExpedition.ID)
			if listBeforeErr != nil {
				testingContext.Errorf("list rows before online: %v", listBeforeErr)
			}
			emitsSoFar := eventsRecorder.CountDomain("plan")
			if len(rowsBeforeOnline) == 5 && rowsBeforeOnline[0].HasCoords && rowsBeforeOnline[1].HasCoords && rowsBeforeOnline[1].POIName == "Jaques Station" && emitsSoFar == 1 {
				offlineVerifiedBeforeFirstHTTP.Store(true)
			}
		}
		var responsePayload string
		switch request.URL.Query().Get("systemName") {
		case "Barnards Loop":
			responsePayload = `{"name":"Barnards Loop","id":10,"id64":5001,"coords":{"x":100,"y":-200,"z":-1300}}`
		case "Beagle Point":
			responsePayload = `{"name":"Beagle Point","id":20,"id64":827689533218,"coords":{"x":-1111.5,"y":-134.2,"z":65269.75}}`
		default:
			responsePayload = `[]`
		}
		return httptestutil.Response(http.StatusOK, responsePayload), nil
	})

	testApp.runExpeditionResolutionWorker(testApp.db, createdExpedition.ID, false)

	if !offlineVerifiedBeforeFirstHTTP.Load() {
		testingContext.Fatalf("expected offline batch resolution and single emit to finish before first online EDSM request")
	}
	if onlineRequests.Load() != 3 {
		testingContext.Fatalf("expected 3 online EDSM requests, got %d", onlineRequests.Load())
	}
	totalPlanEmits := eventsRecorder.CountDomain("plan")
	eventsRecorder.Reset()
	if totalPlanEmits != 2 {
		testingContext.Fatalf("expected 2 coalesced plan emissions (offline pass + online completion), got %d", totalPlanEmits)
	}

	// When OnlineLookups is disabled, unresolved waypoints commit notfound + GEC POI in one batch with 0 online calls and 1 emit.
	if setOfflineErr := settings.SetOnlineLookups(testApp.db, false); setOfflineErr != nil {
		testingContext.Fatalf("disable online lookups: %v", setOfflineErr)
	}
	offlineExpedition, offlineCreateErr := expedition.CreateWithWaypoints(testApp.db, "Offline Batch Route", "", []expedition.SeedWaypoint{
		{SystemName: "Sol"},
		{SystemName: "Uncached Frontier"},
	})
	if offlineCreateErr != nil {
		testingContext.Fatalf("create offline expedition: %v", offlineCreateErr)
	}
	onlineRequests.Store(0)
	testApp.runExpeditionResolutionWorker(testApp.db, offlineExpedition.ID, false)
	offlineRows, offlineListErr := expedition.ListRows(testApp.db, offlineExpedition.ID)
	if offlineListErr != nil {
		testingContext.Fatalf("list offline expedition rows: %v", offlineListErr)
	}
	offlinePlanEmits := eventsRecorder.CountDomain("plan")
	if onlineRequests.Load() != 0 || offlinePlanEmits != 1 || !offlineRows[0].HasCoords || offlineRows[1].CoordsSource != "notfound" {
		testingContext.Fatalf("expected single transactional offline batch (0 HTTP, 1 emit), got http=%d emits=%d rows=%+v", onlineRequests.Load(), offlinePlanEmits, offlineRows)
	}

	// Verify transactional populateMissingPOIs backfills blank POIs and guards system_name against TOCTOU edits.
	if _, execError := testApp.db.Exec(`
		UPDATE expedition_rows SET poi_name = '' WHERE expedition_id = ? AND system_name = 'Colonia';
		UPDATE expedition_rows SET poi_name = 'Custom Beagle Label' WHERE expedition_id = ? AND system_name = 'Beagle Point'
	`, createdExpedition.ID, createdExpedition.ID); execError != nil {
		testingContext.Fatalf("prepare missing POI rows: %v", execError)
	}
	testApp.populateMissingPOIs()
	updatedRows, updatedListErr := expedition.ListRows(testApp.db, createdExpedition.ID)
	if updatedListErr != nil {
		testingContext.Fatalf("list updated rows: %v", updatedListErr)
	}
	if updatedRows[1].POIName != "Jaques Station" || updatedRows[4].POIName != "Custom Beagle Label" {
		testingContext.Fatalf("unexpected POI backfill: colonia=%q beagle=%q", updatedRows[1].POIName, updatedRows[4].POIName)
	}
	if staleUpdateErr := expedition.SetMissingRowPOIsTx(testApp.db, []expedition.RowPOIUpdate{
		{RowID: updatedRows[3].ID, SystemName: "Stale System Name", POIName: "Should Not Apply"},
	}); staleUpdateErr != nil {
		testingContext.Fatalf("SetMissingRowPOIsTx stale system guard: %v", staleUpdateErr)
	}
	guardedRow, guardedRowErr := expedition.GetRow(testApp.db, updatedRows[3].ID)
	if guardedRowErr != nil {
		testingContext.Fatalf("get guarded row: %v", guardedRowErr)
	}
	if guardedRow.POIName != "" {
		testingContext.Fatalf("expected mismatched system_name POI update to be skipped, got %q", guardedRow.POIName)
	}
}

func TestOnlineResolutionAbortsAcrossDatabaseSwitch(testingContext *testing.T) {
	tempDir := testingContext.TempDir()
	testApp := setupTestApp(testingContext)
	enteredHTTP, releaseHTTP, httpCalls := make(chan struct{}), make(chan struct{}), atomic.Int32{}
	testApp.resolver.EDSMBase = "https://edsm.example.test"
	testApp.resolver.HTTP = httptestutil.NewClient(func(_ *http.Request) (*http.Response, error) {
		if httpCalls.Add(1) == 1 {
			close(enteredHTTP)
			<-releaseHTTP
		}
		return httptestutil.Response(http.StatusOK, `{"name":"Deep One","id":1,"id64":9001,"coords":{"x":1,"y":2,"z":3}}`), nil
	})
	oldExp, oldErr := expedition.CreateWithWaypoints(testApp.db, "Old", "", []expedition.SeedWaypoint{{SystemName: "Deep One"}, {SystemName: "Deep Two"}})
	if onlineErr := settings.SetOnlineLookups(testApp.db, true); onlineErr != nil || oldErr != nil {
		testingContext.Fatalf("prepare old expedition: onlineErr=%v oldErr=%v", onlineErr, oldErr)
	}
	testApp.kickResolution(oldExp.ID, false)
	<-enteredHTTP
	_, switchErr := testApp.SetDatabasePath(filepath.Join(tempDir, "switched.sqlite"))
	newExp, newErr := expedition.CreateWithWaypoints(testApp.db, "New", "", []expedition.SeedWaypoint{{SystemName: "Deep One"}})
	close(releaseHTTP)
	testApp.backgroundWg.Wait()
	newRows, listErr := expedition.ListRows(testApp.db, newExp.ID)
	if switchErr != nil || newErr != nil || listErr != nil || httpCalls.Load() != 1 || len(newRows) != 1 || newRows[0].CoordsSource != "" || newRows[0].HasCoords {
		testingContext.Fatalf("expected switched DB untouched and 1 HTTP call, got switchErr=%v newErr=%v listErr=%v calls=%d rows=%+v", switchErr, newErr, listErr, httpCalls.Load(), newRows)
	}
}

func TestAsyncResolutionVisitedAddressMatchEmitsStatsDomain(testingContext *testing.T) {
	testApp := setupTestApp(testingContext)
	journaltest.InsertParsedLines(
		testingContext,
		testApp.db,
		`{"timestamp":"2026-03-01T10:00:00Z","event":"Location","StarSystem":"Sol","SystemAddress":10477373803,"StarPos":[0,0,0]}`,
		`{"timestamp":"2026-03-01T11:00:00Z","event":"FSDJump","StarSystem":"Colonia","SystemAddress":3238296097059,"StarPos":[-9530.5,-910.28125,19808.125]}`,
	)
	if _, execErr := testApp.db.Exec(`INSERT INTO system_cache (name_key,system_name,system_address,edsm_id,x,y,z,source,fetched_at) VALUES ('eol prou rs-t d3-94','Eol Prou RS-T d3-94',3238296097059,43805,-9530.5,-910.28125,19808.125,'edsm','2026-03-01T10:00:00Z')`); execErr != nil {
		testingContext.Fatalf("seed resolution fixtures: %v", execErr)
	}
	createdExp := mustCreateExpeditionWithWaypoints(testingContext, testApp, "Address Visit", "", "Sol")
	addedRow, addErr := testApp.AddRow(createdExp.ID, 0, expedition.RowWaypoint, 0)
	if addErr != nil || testApp.StartExpedition(createdExp.ID, "2026-03-01T10:00:00Z", 0) != nil {
		testingContext.Fatalf("prepare active expedition: addErr=%v", addErr)
	}
	aliasName := "Eol Prou RS-T d3-94"
	eventsRecorder := captureAppEvents(testingContext, testApp)
	updateErr := testApp.UpdateRow(expedition.RowPatch{ID: addedRow.ID, SystemName: &aliasName})
	testApp.backgroundWg.Wait()
	resolvedRow, rowErr := expedition.GetRow(testApp.db, addedRow.ID)
	sawPlanAndStats := false
	for _, change := range eventsRecorder.DataChanges() {
		if slices.Contains(change.Domains, "plan") && slices.Contains(change.Domains, "stats") {
			sawPlanAndStats = true
		}
	}
	if updateErr != nil || rowErr != nil || resolvedRow.VisitedAt != "2026-03-01T11:00:00Z" || !sawPlanAndStats {
		testingContext.Fatalf("expected visited match and plan+stats emit, got updateErr=%v rowErr=%v row=%+v sawBoth=%v", updateErr, rowErr, resolvedRow, sawPlanAndStats)
	}
}
