package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"planisphere/internal/sources/journal"
	"planisphere/internal/store/expedition"
	"planisphere/internal/store/settings"
	"planisphere/internal/testutil/dbtest"
	"planisphere/internal/testutil/journaltest"
)

type testEmitDispatcher struct {
	mutex   sync.RWMutex
	handler func(event string, payload ...any)
}

func (dispatcher *testEmitDispatcher) currentHandler() func(event string, payload ...any) {
	dispatcher.mutex.RLock()
	defer dispatcher.mutex.RUnlock()
	return dispatcher.handler
}

func (dispatcher *testEmitDispatcher) emit(event string, payload ...any) {
	if activeHandler := dispatcher.currentHandler(); activeHandler != nil {
		activeHandler(event, payload...)
	}
}

func (dispatcher *testEmitDispatcher) swapHandler(nextHandler func(event string, payload ...any)) func(event string, payload ...any) {
	dispatcher.mutex.Lock()
	defer dispatcher.mutex.Unlock()
	previousHandler := dispatcher.handler
	dispatcher.handler = nextHandler
	return previousHandler
}

var (
	testEmitDispatchersMu sync.Mutex
	testEmitDispatchers   = map[*App]*testEmitDispatcher{}
)

func registerTestEmitDispatcher(testingContext *testing.T, targetApp *App) {
	testingContext.Helper()
	dispatcher := &testEmitDispatcher{}
	targetApp.emit = dispatcher.emit

	testEmitDispatchersMu.Lock()
	testEmitDispatchers[targetApp] = dispatcher
	testEmitDispatchersMu.Unlock()

	testingContext.Cleanup(func() {
		testEmitDispatchersMu.Lock()
		defer testEmitDispatchersMu.Unlock()
		delete(testEmitDispatchers, targetApp)
	})
}

func setTestEmitHook(testingContext *testing.T, targetApp *App, nextHandler func(event string, payload ...any)) func(event string, payload ...any) {
	testingContext.Helper()
	testEmitDispatchersMu.Lock()
	dispatcher, exists := testEmitDispatchers[targetApp]
	testEmitDispatchersMu.Unlock()
	if !exists {
		testingContext.Fatalf("test emit dispatcher was not registered before initialize on *App")
	}
	return dispatcher.swapHandler(nextHandler)
}

type capturedAppEvents struct {
	mutex           sync.Mutex
	eventNames      []string
	dataChanges     []DataChanged
	eddProgress     []EDDImportProgress
	pendingStates   []bool
	pendingTrueChan chan struct{}
}

func captureAppEvents(testingContext *testing.T, targetApp *App) *capturedAppEvents {
	testingContext.Helper()
	recorder := &capturedAppEvents{
		pendingTrueChan: make(chan struct{}, 8),
	}
	setTestEmitHook(testingContext, targetApp, func(eventName string, payload ...any) {
		recorder.mutex.Lock()
		defer recorder.mutex.Unlock()
		recorder.eventNames = append(recorder.eventNames, eventName)
		switch eventName {
		case EventDataChanged:
			if len(payload) > 0 {
				if dataChanged, ok := payload[0].(DataChanged); ok {
					recorder.dataChanges = append(recorder.dataChanges, dataChanged)
				}
			}
		case EventEDDProgress:
			if len(payload) > 0 {
				if progress, ok := payload[0].(EDDImportProgress); ok {
					recorder.eddProgress = append(recorder.eddProgress, progress)
				}
			}
		case EventSyncPending:
			if len(payload) > 0 {
				if pendingState, ok := payload[0].(bool); ok {
					recorder.pendingStates = append(recorder.pendingStates, pendingState)
					if pendingState {
						select {
						case recorder.pendingTrueChan <- struct{}{}:
						default:
						}
					}
				}
			}
		}
	})
	return recorder
}

func (recorder *capturedAppEvents) Reset() {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.eventNames = nil
	recorder.dataChanges = nil
	recorder.eddProgress = nil
	recorder.pendingStates = nil
}

func (recorder *capturedAppEvents) EventNames() []string {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]string(nil), recorder.eventNames...)
}

func (recorder *capturedAppEvents) DataChanges() []DataChanged {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]DataChanged(nil), recorder.dataChanges...)
}

func (recorder *capturedAppEvents) EDDProgress() []EDDImportProgress {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]EDDImportProgress(nil), recorder.eddProgress...)
}

func (recorder *capturedAppEvents) Domains() []string {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	var domains []string
	for _, change := range recorder.dataChanges {
		domains = append(domains, change.Domains...)
	}
	return domains
}

func (recorder *capturedAppEvents) CountDomain(domain string) int {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	count := 0
	for _, change := range recorder.dataChanges {
		if slices.Contains(change.Domains, domain) {
			count++
		}
	}
	return count
}

func (recorder *capturedAppEvents) TakeDataChanges() []DataChanged {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	changes := append([]DataChanged(nil), recorder.dataChanges...)
	recorder.eventNames = nil
	recorder.dataChanges = nil
	recorder.eddProgress = nil
	recorder.pendingStates = nil
	return changes
}

func (recorder *capturedAppEvents) TakeAll() ([]DataChanged, []bool) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	changes := append([]DataChanged(nil), recorder.dataChanges...)
	pending := append([]bool(nil), recorder.pendingStates...)
	recorder.eventNames = nil
	recorder.dataChanges = nil
	recorder.eddProgress = nil
	recorder.pendingStates = nil
	return changes, pending
}

func (recorder *capturedAppEvents) PendingTrue() <-chan struct{} {
	return recorder.pendingTrueChan
}

func setupTestApp(testingContext *testing.T) *App {
	testingContext.Helper()
	temporaryDirectory := dbtest.IsolateUserDirs(testingContext)
	databasePath := filepath.Join(temporaryDirectory, "test.sqlite")

	testApp := New()
	registerTestEmitDispatcher(testingContext, testApp)
	applicationContext, cancelFunction := context.WithCancel(context.Background())
	if initializeError := testApp.initialize(applicationContext, databasePath); initializeError != nil {
		cancelFunction()
		testingContext.Fatalf("initialize db: %v", initializeError)
	}
	if err := settings.SetOnlineLookups(testApp.db, false); err != nil {
		cancelFunction()
		testApp.shutdown(applicationContext)
		testingContext.Fatalf("disable online lookups: %v", err)
	}
	testingContext.Cleanup(func() {
		cancelFunction()
		testApp.shutdown(applicationContext)
	})
	return testApp
}

func insertTestJournalEvent(testingContext *testing.T, testApp *App, timestamp, eventName, keyIdentifier, rawJSON string) {
	testingContext.Helper()
	var systemPayload struct {
		StarSystem string `json:"StarSystem"`
		SystemName string `json:"SystemName"`
	}
	if unmarshalError := json.Unmarshal([]byte(rawJSON), &systemPayload); unmarshalError != nil {
		testingContext.Fatalf("unmarshal %s journal event JSON: %v", eventName, unmarshalError)
	}
	systemName := systemPayload.StarSystem
	if systemName == "" {
		systemName = systemPayload.SystemName
	}

	eventUID := journal.ComputeUID(timestamp, eventName, keyIdentifier)
	_, insertError := testApp.db.Exec(
		`INSERT INTO journal_events (event_uid, timestamp, event, system_name, raw_json, ingested_at) VALUES (?, ?, ?, ?, ?, ?)`,
		eventUID, timestamp, eventName, systemName, rawJSON, timestamp,
	)
	if insertError != nil {
		testingContext.Fatalf("insert %s journal event: %v", eventName, insertError)
	}
	if parsedEvent, ok := journal.ParseEventType(eventName); ok {
		testApp.publishJournalBatch(journal.NewEventSet(parsedEvent), "", "")
	}
}

func seedDatabaseWithExpedition(testingContext *testing.T, databasePath, expeditionName, expeditionDescription string, settingPairs ...[2]string) {
	testingContext.Helper()
	seedApp := New()
	seedApp.debugMode = false
	registerTestEmitDispatcher(testingContext, seedApp)
	seedContext, cancelSeed := context.WithCancel(context.Background())
	if initError := seedApp.initialize(seedContext, databasePath); initError != nil {
		cancelSeed()
		testingContext.Fatalf("seed initialize %s: %v", databasePath, initError)
	}
	if err := settings.SetOnlineLookups(seedApp.db, false); err != nil {
		cancelSeed()
		seedApp.shutdown(seedContext)
		testingContext.Fatalf("disable online lookups: %v", err)
	}
	for _, settingPair := range settingPairs {
		if _, setError := seedApp.db.Exec(
			`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			settingPair[0], settingPair[1],
		); setError != nil {
			cancelSeed()
			seedApp.shutdown(seedContext)
			testingContext.Fatalf("seed setting %s: %v", settingPair[0], setError)
		}
	}
	if _, createError := seedApp.CreateExpedition(expeditionName, expeditionDescription); createError != nil {
		cancelSeed()
		seedApp.shutdown(seedContext)
		testingContext.Fatalf("seed CreateExpedition %q: %v", expeditionName, createError)
	}
	cancelSeed()
	seedApp.shutdown(seedContext)
}

func startExpeditionAtOrigin(testingContext *testing.T, targetApp *App, expeditionID int64, originSystemName string) int64 {
	testingContext.Helper()
	planView := mustGetPlan(testingContext, targetApp, expeditionID)
	if len(planView.Rows) == 0 {
		testingContext.Fatalf("GetPlan(%d): expected at least 1 row", expeditionID)
	}
	originRowID := planView.Rows[0].Row.ID
	if updateError := targetApp.UpdateRow(expedition.RowPatch{ID: originRowID, SystemName: &originSystemName}); updateError != nil {
		testingContext.Fatalf("UpdateRow SystemName: %v", updateError)
	}
	if startError := targetApp.StartExpedition(expeditionID, "2026-01-01T00:00:00Z", 0); startError != nil {
		testingContext.Fatalf("StartExpedition: %v", startError)
	}
	return originRowID
}

func createAndStartExpedition(testingContext *testing.T, targetApp *App, expeditionName, originSystemName string) int64 {
	testingContext.Helper()
	createdExpedition, createError := targetApp.CreateExpedition(expeditionName, "")
	if createError != nil {
		testingContext.Fatalf("CreateExpedition %q: %v", expeditionName, createError)
	}
	startExpeditionAtOrigin(testingContext, targetApp, createdExpedition.ID, originSystemName)
	return createdExpedition.ID
}

func mustGetPlan(testingContext *testing.T, testApp *App, expeditionID int64) expedition.PlanView {
	testingContext.Helper()
	planView, planError := testApp.GetPlan(expeditionID)
	if planError != nil {
		testingContext.Fatalf("GetPlan(%d): %v", expeditionID, planError)
	}
	return planView
}

func mustGetActiveCarrierRoute(testingContext *testing.T, testApp *App, expeditionID int64) *ActiveCarrierRouteState {
	testingContext.Helper()
	return mustGetPlan(testingContext, testApp, expeditionID).ActiveCarrierRoute
}

func mustCreateExpeditionWithWaypoints(
	testingContext *testing.T,
	targetApp *App,
	expeditionName, expeditionDescription string,
	systemNames ...string,
) expedition.Expedition {
	testingContext.Helper()
	createdExpedition, createError := targetApp.createExpeditionWithWaypoints(expeditionName, expeditionDescription, systemNames)
	if createError != nil {
		testingContext.Fatalf("createExpeditionWithWaypoints(%q): %v", expeditionName, createError)
	}
	targetApp.backgroundWg.Wait()
	return createdExpedition
}

func setTestRowSystemName(testingContext *testing.T, targetApp *App, rowID int64, systemName string) {
	testingContext.Helper()
	if updateError := targetApp.UpdateRow(expedition.RowPatch{ID: rowID, SystemName: &systemName}); updateError != nil {
		testingContext.Fatalf("UpdateRow(%d, %q): %v", rowID, systemName, updateError)
	}
}

func addTestWaypoint(testingContext *testing.T, targetApp *App, expeditionID, afterRowID int64, systemName string) expedition.Row {
	testingContext.Helper()
	insertedRow, addError := targetApp.AddRow(expeditionID, afterRowID, expedition.RowWaypoint, 0)
	if addError != nil {
		testingContext.Fatalf("AddRow(%d): %v", expeditionID, addError)
	}
	setTestRowSystemName(testingContext, targetApp, insertedRow.ID, systemName)
	return insertedRow
}

func publishTestJournalLines(testingContext *testing.T, targetApp *App, earliestTimestamp, latestTimestamp string, lines ...string) {
	testingContext.Helper()
	insertedTypes := journaltest.InsertParsedLines(testingContext, targetApp.db, lines...)
	if len(insertedTypes) > 0 {
		targetApp.publishJournalBatch(journal.NewEventSet(insertedTypes...), earliestTimestamp, latestTimestamp)
	}
}
