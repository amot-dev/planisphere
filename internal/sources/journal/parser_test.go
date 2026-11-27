package journal_test

import (
	"sync"
	"testing"

	"planisphere/internal/sources/journal"
)

func TestComputeUIDDeterministicAndConcurrent(t *testing.T) {
	initialUID := journal.ComputeUID("2026-01-18T19:05:00Z", "FSDJump", "1001", "Sol")
	if len(initialUID) != 64 {
		t.Fatalf("expected 64-char sha256 hex string, got %d (%s)", len(initialUID), initialUID)
	}

	var waitGroup sync.WaitGroup
	for workerIndex := 0; workerIndex < 20; workerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for iteration := 0; iteration < 100; iteration++ {
				computedUID := journal.ComputeUID("2026-01-18T19:05:00Z", "FSDJump", "1001", "Sol")
				if computedUID != initialUID {
					t.Errorf("expected deterministic UID %s, got %s", initialUID, computedUID)
				}
			}
		}()
	}
	waitGroup.Wait()
}

func TestParseLinePreJumpFilters(testingContext *testing.T) {
	testCases := []struct {
		name     string
		rawLine  string
		wantKeep bool
	}{
		{
			name:     "hyperspace StartJump kept",
			rawLine:  `{"timestamp":"2026-11-17T01:40:45Z","event":"StartJump","JumpType":"Hyperspace","StarSystem":"Hypo Aeb JS-D b53-0","SystemAddress":537273637321}`,
			wantKeep: true,
		},
		{
			name:     "supercruise StartJump ignored",
			rawLine:  `{"timestamp":"2026-11-17T01:41:00Z","event":"StartJump","JumpType":"Supercruise"}`,
			wantKeep: false,
		},
		{
			name:     "JetConeBoost kept",
			rawLine:  `{"timestamp":"2026-11-17T01:39:00Z","event":"JetConeBoost","BoostValue":4.0}`,
			wantKeep: true,
		},
		{
			name:     "FSD synthesis kept",
			rawLine:  `{"timestamp":"2026-11-17T01:39:10Z","event":"Synthesis","Name":"FSD Premium"}`,
			wantKeep: true,
		},
		{
			name:     "non-FSD synthesis ignored",
			rawLine:  `{"timestamp":"2026-11-17T01:39:20Z","event":"Synthesis","Name":"Limpet Basic"}`,
			wantKeep: false,
		},
	}

	for _, testCase := range testCases {
		_, gotKeep, parseError := journal.ParseLine([]byte(testCase.rawLine), "Journal.test.log")
		if parseError != nil {
			testingContext.Fatalf("%s: unexpected ParseLine error: %v", testCase.name, parseError)
		}
		if gotKeep != testCase.wantKeep {
			testingContext.Errorf("%s: got keep=%v, want %v", testCase.name, gotKeep, testCase.wantKeep)
		}
	}
}

func TestParseLineSellOrganicDataCanonicalUID(testingContext *testing.T) {
	rawLine := []byte(`{"timestamp":"2026-03-01T12:00:00Z","event":"SellOrganicData","MarketID":123,"BioData":[{"Species":"$Codex_Ent_Bacterial_01_Name;","Value":1000000,"Bonus":0}]}`)
	eddLine := []byte(`{ "timestamp":"2026-03-01T12:00:00Z", "event":"SellOrganicData", "MarketID":123, "BioData":[ { "Species":"$Codex_Ent_Bacterial_01_Name;", "Value":1000000, "Bonus":0 } ] }`)
	rawJournalEvent, isRawValid, rawParseError := journal.ParseLine(rawLine, "Journal.log")
	eddJournalEvent, isEDDValid, eddParseError := journal.ParseLine(eddLine, "EDDiscovery")
	if !isRawValid || !isEDDValid || rawParseError != nil || eddParseError != nil {
		testingContext.Fatalf("ParseLine failed: isRawValid=%v rawParseError=%v isEDDValid=%v eddParseError=%v", isRawValid, rawParseError, isEDDValid, eddParseError)
	}
	if rawJournalEvent.UID != eddJournalEvent.UID {
		testingContext.Fatalf("expected identical canonical UID for SellOrganicData across whitespace differences, got %s vs %s", rawJournalEvent.UID, eddJournalEvent.UID)
	}
}
