package expedition

import (
	"regexp"
	"strings"
)

// Bulk waypoint import format identifiers.
const (
	WaypointImportEmpty     = "empty"
	WaypointImportPlain     = "plain"
	WaypointImportSpanshCSV = "spansh-csv"
)

var (
	lineEndingReplacer     = strings.NewReplacer("\r\n", "\n", "\r", "\n")
	plainListPrefixPattern = regexp.MustCompile(`^(?:\d+[.)]|[-*])[\s\p{Zs}]+`)
)

// ParsedWaypointImport holds extracted star system names and summary counts
// from a plain-text system list or Spansh CSV route export.
type ParsedWaypointImport struct {
	Format                  string   `json:"format"`
	Systems                 []string `json:"systems"`
	CollapsedDuplicateCount int      `json:"collapsedDuplicateCount"`
	SkippedLineCount        int      `json:"skippedLineCount"`
}

// NormalizeImportedSystemName trims surrounding whitespace and collapses
// internal whitespace runs to a single space.
func NormalizeImportedSystemName(rawName string) string {
	return strings.Join(strings.Fields(rawName), " ")
}

func parseCSVLine(line string) []string {
	fields := make([]string, 0, 4)
	var currentField strings.Builder
	insideQuotes := false

	for index := 0; index < len(line); index++ {
		character := line[index]
		if insideQuotes {
			if character == '"' {
				if index+1 < len(line) && line[index+1] == '"' {
					currentField.WriteByte('"')
					index++
				} else {
					insideQuotes = false
				}
			} else {
				currentField.WriteByte(character)
			}
			continue
		}

		switch character {
		case '"':
			insideQuotes = true
		case ',':
			fields = append(fields, currentField.String())
			currentField.Reset()
		default:
			currentField.WriteByte(character)
		}
	}
	fields = append(fields, currentField.String())
	return fields
}

func findSpanshSystemColumnIndex(headerFields []string) int {
	for index, field := range headerFields {
		if strings.EqualFold(strings.TrimSpace(field), "system name") {
			return index
		}
	}
	if len(headerFields) > 1 {
		for index, field := range headerFields {
			if strings.EqualFold(strings.TrimSpace(field), "system") {
				return index
			}
		}
	}
	return -1
}

func parseSpanshCSVLines(lines []string, headerLineIndex int, systemColumnIndex int) ParsedWaypointImport {
	systems := make([]string, 0)
	collapsedDuplicateCount := 0
	skippedLineCount := 0
	previousSystemKey := ""

	for lineIndex := headerLineIndex + 1; lineIndex < len(lines); lineIndex++ {
		if strings.TrimSpace(lines[lineIndex]) == "" {
			continue
		}
		fields := parseCSVLine(lines[lineIndex])
		rawSystem := ""
		if systemColumnIndex < len(fields) {
			rawSystem = fields[systemColumnIndex]
		}
		systemName := NormalizeImportedSystemName(rawSystem)
		if systemName == "" {
			skippedLineCount++
			continue
		}
		systemKey := strings.ToLower(systemName)
		if systemKey == previousSystemKey {
			collapsedDuplicateCount++
			continue
		}
		previousSystemKey = systemKey
		systems = append(systems, systemName)
	}

	return ParsedWaypointImport{
		Format:                  WaypointImportSpanshCSV,
		Systems:                 systems,
		CollapsedDuplicateCount: collapsedDuplicateCount,
		SkippedLineCount:        skippedLineCount,
	}
}

func cleanPlainTextLine(rawLine string) string {
	cleanedLine := strings.TrimSpace(rawLine)
	if len(cleanedLine) >= 2 && strings.HasPrefix(cleanedLine, "\"") && strings.HasSuffix(cleanedLine, "\"") {
		cleanedLine = strings.TrimSpace(strings.ReplaceAll(cleanedLine[1:len(cleanedLine)-1], "\"\"", "\""))
	}
	// Only strip explicit punctuation-delimited list markers so digit-leading
	// star systems such as "3 Geminorum" or "2MASS J02351897+6131236" stay intact.
	cleanedLine = plainListPrefixPattern.ReplaceAllString(cleanedLine, "")
	return NormalizeImportedSystemName(cleanedLine)
}

func parsePlainTextLines(lines []string) ParsedWaypointImport {
	systems := make([]string, 0)
	skippedLineCount := 0

	for _, rawLine := range lines {
		trimmedLine := strings.TrimSpace(rawLine)
		if trimmedLine == "" {
			continue
		}
		if strings.HasPrefix(trimmedLine, "#") {
			skippedLineCount++
			continue
		}
		systemName := cleanPlainTextLine(trimmedLine)
		if systemName == "" {
			skippedLineCount++
			continue
		}
		systems = append(systems, systemName)
	}

	return ParsedWaypointImport{
		Format:                  WaypointImportPlain,
		Systems:                 systems,
		CollapsedDuplicateCount: 0,
		SkippedLineCount:        skippedLineCount,
	}
}

// ParseWaypointImportText parses either a plain newline-separated list of star
// systems or a Spansh CSV export into an ordered slice of normalized system names.
func ParseWaypointImportText(rawText string) ParsedWaypointImport {
	normalizedText := strings.TrimPrefix(rawText, "\uFEFF")
	if strings.IndexByte(normalizedText, '\r') >= 0 {
		normalizedText = lineEndingReplacer.Replace(normalizedText)
	}
	if strings.TrimSpace(normalizedText) == "" {
		return ParsedWaypointImport{
			Format:  WaypointImportEmpty,
			Systems: []string{},
		}
	}

	lines := strings.Split(normalizedText, "\n")
	firstNonEmptyLineIndex := -1
	for index, line := range lines {
		if strings.TrimSpace(line) != "" {
			firstNonEmptyLineIndex = index
			break
		}
	}
	if firstNonEmptyLineIndex < 0 {
		return ParsedWaypointImport{
			Format:  WaypointImportEmpty,
			Systems: []string{},
		}
	}

	headerFields := parseCSVLine(lines[firstNonEmptyLineIndex])
	if systemColumnIndex := findSpanshSystemColumnIndex(headerFields); systemColumnIndex >= 0 {
		return parseSpanshCSVLines(lines, firstNonEmptyLineIndex, systemColumnIndex)
	}
	return parsePlainTextLines(lines)
}

// DeriveExpeditionNameFromFilename strips a trailing file extension from a
// loaded import filename so it can pre-fill an empty Expedition Name input.
func DeriveExpeditionNameFromFilename(fileName string) string {
	trimmedName := strings.TrimSpace(fileName)
	if trimmedName == "" {
		return ""
	}
	lastDotIndex := strings.LastIndexByte(trimmedName, '.')
	if lastDotIndex <= 0 {
		return trimmedName
	}
	return strings.TrimSpace(trimmedName[:lastDotIndex])
}
