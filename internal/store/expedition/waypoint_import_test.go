package expedition_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"planisphere/internal/store/expedition"
)

func TestParseWaypointImportText(testingContext *testing.T) {
	testCases := []struct {
		name     string
		rawInput string
		expected expedition.ParsedWaypointImport
	}{
		{
			name:     "empty string returns non-nil empty systems slice",
			rawInput: "",
			expected: expedition.ParsedWaypointImport{
				Format:  expedition.WaypointImportEmpty,
				Systems: []string{},
			},
		},
		{
			name:     "whitespace and mixed line endings return empty format",
			rawInput: "   \r\n\t\r  \n ",
			expected: expedition.ParsedWaypointImport{
				Format:  expedition.WaypointImportEmpty,
				Systems: []string{},
			},
		},
		{
			name: "plain list preserves digit-leading systems and loop revisits while unwrapping quotes and skipping comments",
			rawInput: strings.Join([]string{
				"# Inner Orion Spur",
				"  Shinrarta Dezhra  ",
				"",
				"1. Omega Sector VE-Q b5-15",
				"02) 3 Geminorum",
				"- 2MASS J02351897+6131236",
				"*   Scheau   Prue SE-Q e5-770 ",
				"\"Sagittarius A*\"",
				"\"Quoted \"\"Alias\"\" Star\"",
				"\"   \"",
				"",
				"Shinrarta Dezhra",
			}, "\r\n"),
			expected: expedition.ParsedWaypointImport{
				Format: expedition.WaypointImportPlain,
				Systems: []string{
					"Shinrarta Dezhra",
					"Omega Sector VE-Q b5-15",
					"3 Geminorum",
					"2MASS J02351897+6131236",
					"Scheau Prue SE-Q e5-770",
					"Sagittarius A*",
					"Quoted \"Alias\" Star",
					"Shinrarta Dezhra",
				},
				CollapsedDuplicateCount: 0,
				SkippedLineCount:        2,
			},
		},
		{
			name: "spansh neutron plotter CSV strips BOM and handles CRLF",
			rawInput: "\uFEFF\"System Name\",\"Distance To Arrival\",\"Distance Remaining\",\"Neutron Star\",\"Jumps\"\r\n" +
				"\"Sol\",\"0\",\"22000.47\",\"No\",\"0\"\r\n" +
				"\"Jackson's Lighthouse\",\"137.45\",\"21863.02\",\"Yes\",\"3\"\r\n" +
				"\"Colonia\",\"22000.47\",\"0\",\"No\",\"1\"\r\n",
			expected: expedition.ParsedWaypointImport{
				Format:                  expedition.WaypointImportSpanshCSV,
				Systems:                 []string{"Sol", "Jackson's Lighthouse", "Colonia"},
				CollapsedDuplicateCount: 0,
				SkippedLineCount:        0,
			},
		},
		{
			name: "spansh multi-body CSV collapses consecutive duplicates, preserves revisits, and handles quoted commas",
			rawInput: strings.Join([]string{
				"\"Body Subtype\",\"System Name\",\"Distance To Arrival\"",
				"\"Earth-like world\",\"Sol\",\"499\"",
				"\"Terraformable, rocky\",\"  sol  \",\"750\"",
				"\"Rocky body\",\"Epsilon Indi\",\"120\"",
				"\"High metal content\",\"Sol\",\"360\"",
			}, "\n"),
			expected: expedition.ParsedWaypointImport{
				Format:                  expedition.WaypointImportSpanshCSV,
				Systems:                 []string{"Sol", "Epsilon Indi", "Sol"},
				CollapsedDuplicateCount: 1,
				SkippedLineCount:        0,
			},
		},
		{
			name: "multi-column System header handles ragged rows, empty system cells, blank lines, and unclosed quotes",
			rawInput: strings.Join([]string{
				"",
				"Index, System ,Notes",
				"1,  Rohini  ,\"Waypoint, alpha\"",
				"2",
				"   ",
				"3,   ,missing system",
				"4,\"Gandharvi,unclosed",
			}, "\n"),
			expected: expedition.ParsedWaypointImport{
				Format:                  expedition.WaypointImportSpanshCSV,
				Systems:                 []string{"Rohini", "Gandharvi,unclosed"},
				CollapsedDuplicateCount: 0,
				SkippedLineCount:        2,
			},
		},
		{
			name:     "single-column System line is treated as plain text rather than CSV header",
			rawInput: "System\nSol",
			expected: expedition.ParsedWaypointImport{
				Format:  expedition.WaypointImportPlain,
				Systems: []string{"System", "Sol"},
			},
		},
		{
			name:     "header-only CSV yields non-nil empty systems slice",
			rawInput: "\"System Name\",\"Jumps\"\n",
			expected: expedition.ParsedWaypointImport{
				Format:  expedition.WaypointImportSpanshCSV,
				Systems: []string{},
			},
		},
		{
			name:     "comment-only input yields plain format with skipped count and non-nil empty systems",
			rawInput: "# only a comment\n# second comment",
			expected: expedition.ParsedWaypointImport{
				Format:           expedition.WaypointImportPlain,
				Systems:          []string{},
				SkippedLineCount: 2,
			},
		},
	}

	for _, testCase := range testCases {
		testingContext.Run(testCase.name, func(subTest *testing.T) {
			parsed := expedition.ParseWaypointImportText(testCase.rawInput)
			if parsed.Systems == nil {
				subTest.Fatalf("expected non-nil Systems slice")
			}
			if !reflect.DeepEqual(parsed, testCase.expected) {
				subTest.Fatalf("got %+v, want %+v", parsed, testCase.expected)
			}
			encodedJSON, marshalError := json.Marshal(parsed)
			if marshalError != nil {
				subTest.Fatalf("marshal ParsedWaypointImport: %v", marshalError)
			}
			if len(parsed.Systems) == 0 && !strings.Contains(string(encodedJSON), `"systems":[]`) {
				subTest.Fatalf("expected empty systems slice to marshal as [], got %s", string(encodedJSON))
			}
		})
	}
}

func TestDeriveExpeditionNameFromFilename(testingContext *testing.T) {
	testCases := []struct {
		fileName string
		expected string
	}{
		{fileName: "Xibalba Expedition.txt", expected: "Xibalba Expedition"},
		{fileName: "spansh-route.csv", expected: "spansh-route"},
		{fileName: "route.v2.csv", expected: "route.v2"},
		{fileName: "no-extension", expected: "no-extension"},
		{fileName: ".hidden", expected: ".hidden"},
		{fileName: ".hidden.txt", expected: ".hidden"},
		{fileName: "  ", expected: ""},
	}

	for _, testCase := range testCases {
		if got := expedition.DeriveExpeditionNameFromFilename(testCase.fileName); got != testCase.expected {
			testingContext.Fatalf("DeriveExpeditionNameFromFilename(%q) = %q, want %q", testCase.fileName, got, testCase.expected)
		}
	}
}
