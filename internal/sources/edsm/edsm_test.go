package edsm_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"planisphere/internal/core/db"
	"planisphere/internal/reference"
	"planisphere/internal/replay"
	"planisphere/internal/sources/edsm"
)

func openEDSMTestDB(testingContext *testing.T) *sql.DB {
	testingContext.Helper()
	databaseConnection, openError := db.Open(":memory:")
	if openError != nil {
		testingContext.Fatal(openError)
	}
	testingContext.Cleanup(func() {
		_ = databaseConnection.Close()
	})
	return databaseConnection
}

func wireReplayCallbacks(databaseConnection *sql.DB, resolver *edsm.Resolver) {
	resolver.JournalLookup = func(systemName string) (*edsm.Info, error) {
		journalSystem, queryError := replay.QueryJournalSystemCoords(databaseConnection, systemName)
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
	resolver.JournalBatchLookup = func(systemNames []string) (map[string]*edsm.Info, error) {
		journalBatch, queryError := replay.QueryJournalSystemCoordsBatch(databaseConnection, systemNames)
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
}

func TestResolveLocalBatchEnrichmentFallbackChunkingAndCmdrFilter(testingContext *testing.T) {
	databaseConnection := openEDSMTestDB(testingContext)
	resolver := edsm.NewResolver(databaseConnection, func() bool { return false })
	wireReplayCallbacks(databaseConnection, resolver)

	if _, execError := databaseConnection.Exec(`
		INSERT INTO commanders (name, fid, enabled) VALUES ('CmdrActive', 'F1', 1), ('CmdrMuted', 'F2', 0);
		INSERT INTO journal_events (event_uid, timestamp, event, system_name, system_address, star_x, star_y, star_z, commander, source_file, raw_json, ingested_at) VALUES
		('ev-sol', '2026-01-01T00:00:00Z', 'FSDJump', 'Sol', 10477373803, 0, 0, 0, 'CmdrActive', 'j.log', '{}', '2026-01-01T00:00:00Z'),
		('ev-muted', '2026-01-01T00:01:00Z', 'FSDJump', 'Muted System', 555, 9, 9, 9, 'CmdrMuted', 'j.log', '{}', '2026-01-01T00:01:00Z');
		INSERT INTO system_cache (name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at) VALUES
		('sol', 'Sol', 10477373803, 27, 0, 0, 0, 'edsm', '2026-01-01T00:00:00Z'),
		('colonia', 'Colonia', 3238296097059, 43805, -9530.5, -910.28125, 19808.125, 'edsm', '2026-01-01T00:00:00Z'),
		('ghost sector', 'Ghost Sector', NULL, NULL, NULL, NULL, NULL, 'notfound', '2026-01-01T00:00:00Z')
	`); execError != nil {
		testingContext.Fatalf("seed journal and system_cache: %v", execError)
	}

	cacheStmt, prepError := databaseConnection.Prepare(
		`INSERT INTO system_cache (name_key, system_name, system_address, edsm_id, x, y, z, source, fetched_at) VALUES (?, ?, ?, ?, ?, 1, 2, 'edsm', '2026-01-01T00:00:00Z')`,
	)
	if prepError != nil {
		testingContext.Fatalf("prepare cache insert: %v", prepError)
	}
	defer cacheStmt.Close()

	inputNames := make([]string, 0, 520)
	inputNames = append(inputNames, "  SOL ", "colonia", "Muted System", "ghost sector", "  ")
	for cacheIndex := 0; cacheIndex < 510; cacheIndex++ {
		displayName := fmt.Sprintf("Cached Waypoint %03d", cacheIndex)
		if _, execError := cacheStmt.Exec(strings.ToLower(displayName), displayName, int64(8000+cacheIndex), int64(9000+cacheIndex), float64(cacheIndex)); execError != nil {
			testingContext.Fatalf("insert cache row %d: %v", cacheIndex, execError)
		}
		inputNames = append(inputNames, strings.ToUpper(displayName))
	}

	resolvedByKey, batchError := resolver.ResolveLocalBatch(inputNames)
	if batchError != nil {
		testingContext.Fatalf("ResolveLocalBatch: %v", batchError)
	}
	if len(resolvedByKey) != 512 {
		testingContext.Fatalf("expected 512 resolved systems (Sol + Colonia + 510 cached), got %d", len(resolvedByKey))
	}

	solInfo := resolvedByKey["sol"]
	if solInfo == nil || solInfo.Source != "journal" || solInfo.EDSMID != 27 || solInfo.Pos == nil {
		testingContext.Fatalf("expected Sol from journal enriched with EDSMID 27, got %+v", solInfo)
	}
	coloniaInfo := resolvedByKey["colonia"]
	if coloniaInfo == nil || coloniaInfo.Source != "edsm" || coloniaInfo.EDSMID != 43805 || coloniaInfo.Pos == nil {
		testingContext.Fatalf("expected Colonia fallback from system_cache, got %+v", coloniaInfo)
	}
	if _, foundMuted := resolvedByKey["muted system"]; foundMuted {
		testingContext.Fatalf("expected disabled commander system Muted System excluded")
	}
	if _, foundNotFound := resolvedByKey["ghost sector"]; foundNotFound {
		testingContext.Fatalf("expected notfound cache entry without coords excluded from ResolveLocalBatch")
	}

	// Verify CanonicalSystemName callback delegation and cache/raw fallbacks.
	canonicalLookup := func(name string) (string, error) {
		return replay.QueryCanonicalSystemName(databaseConnection, name)
	}
	if gotSol := edsm.CanonicalSystemName(databaseConnection, "  sol ", canonicalLookup); gotSol != "Sol" {
		testingContext.Fatalf("expected canonical Sol from journal callback, got %q", gotSol)
	}
	if gotMuted := edsm.CanonicalSystemName(databaseConnection, "muted system", canonicalLookup); gotMuted != "muted system" {
		testingContext.Fatalf("expected disabled commander excluded from canonical lookup, got %q", gotMuted)
	}
	if gotColonia := edsm.CanonicalSystemName(databaseConnection, "COLONIA", canonicalLookup); gotColonia != "Colonia" {
		testingContext.Fatalf("expected canonical Colonia from system_cache fallback, got %q", gotColonia)
	}
}

func TestSystemCacheErrorPropagationInResolveLocalBatchAndStore(testingContext *testing.T) {
	databaseConnection := openEDSMTestDB(testingContext)
	mockServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"name":"Beagle Point","id":124406,"id64":827689533218,"coords":{"x":-1111.5625,"y":-134.21875,"z":65269.75}}`))
	}))
	testingContext.Cleanup(mockServer.Close)

	resolver := edsm.NewResolver(databaseConnection, func() bool { return true })
	resolver.EDSMBase = mockServer.URL
	resolver.HTTP = mockServer.Client()
	resolver.JournalLookup = func(systemName string) (*edsm.Info, error) {
		if strings.EqualFold(systemName, "Sol") {
			return &edsm.Info{Name: "Sol", SystemAddress: 10477373803, Pos: &reference.Vec3{X: 0, Y: 0, Z: 0}, Source: "journal"}, nil
		}
		return nil, nil
	}

	// Healthy online Resolve stores to system_cache and returns coordinates.
	beagleInfo, resolveError := resolver.Resolve(context.Background(), "Beagle Point")
	if resolveError != nil || beagleInfo == nil || beagleInfo.EDSMID != 124406 {
		testingContext.Fatalf("expected online Resolve to succeed, got info=%+v err=%v", beagleInfo, resolveError)
	}

	// Add an abort trigger on system_cache writes to verify store error propagation in Resolve.
	if _, triggerError := databaseConnection.Exec(`
		CREATE TRIGGER fail_system_cache_insert BEFORE INSERT ON system_cache
		BEGIN SELECT RAISE(ABORT, 'simulated system_cache write failure'); END;
	`); triggerError != nil {
		testingContext.Fatalf("create abort trigger: %v", triggerError)
	}
	if _, storeFailError := resolver.Resolve(context.Background(), "Ishums Reach"); storeFailError == nil || !strings.Contains(storeFailError.Error(), "store system_cache") {
		testingContext.Fatalf("expected store error from Resolve when system_cache write fails, got %v", storeFailError)
	}

	// Drop system_cache to verify fromCache and fromCacheBatch errors propagate in ResolveLocal and ResolveLocalBatch.
	if _, dropError := databaseConnection.Exec(`DROP TABLE system_cache`); dropError != nil {
		testingContext.Fatalf("drop system_cache: %v", dropError)
	}
	if _, localJournalErr := resolver.ResolveLocal("Sol"); localJournalErr == nil || !strings.Contains(localJournalErr.Error(), "system_cache") {
		testingContext.Fatalf("expected ResolveLocal enrichment cache error when system_cache is missing, got %v", localJournalErr)
	}
	if _, localMissErr := resolver.ResolveLocal("Colonia"); localMissErr == nil || !strings.Contains(localMissErr.Error(), "system_cache") {
		testingContext.Fatalf("expected ResolveLocal cache error when system_cache is missing, got %v", localMissErr)
	}
	if _, batchErr := resolver.ResolveLocalBatch([]string{"Sol", "Colonia"}); batchErr == nil || !strings.Contains(batchErr.Error(), "system_cache") {
		testingContext.Fatalf("expected ResolveLocalBatch cache error when system_cache is missing, got %v", batchErr)
	}

	// Verify online disabled returns wrapped ErrorNotFound when DB is healthy.
	healthyDB := openEDSMTestDB(testingContext)
	offlineResolver := edsm.NewResolver(healthyDB, func() bool { return false })
	if _, offlineErr := offlineResolver.Resolve(context.Background(), "Unknown Sys"); !errors.Is(offlineErr, edsm.ErrorNotFound) {
		testingContext.Fatalf("expected ErrorNotFound when online disabled, got %v", offlineErr)
	}
}
