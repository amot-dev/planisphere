// Package ed contains Elite Dangerous domain knowledge: in-universe time,
// Universal Cartographics / Vista Genomics value formulas, fleet carrier
// maths, ship names and the 42 galactic regions.
package ed

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// GalacticYearOffset is the number of years added to the real-world UTC year
// to obtain the in-universe year (2026 -> 3312).
const GalacticYearOffset = 1286

// ISOLayout is the canonical timestamp layout used by Elite Dangerous journals
// and by every timestamp column persisted in our database. Values in this
// layout sort lexicographically in chronological order.
const ISOLayout = "2006-01-02T15:04:05Z"

// GalacticLayoutSuffix is appended to every in-universe timestamp.
const GalacticLayoutSuffix = " UTC"

// NowISO returns the current time as a journal-style ISO-8601 UTC string.
func NowISO() string { return time.Now().UTC().Format(ISOLayout) }

// FormatISO formats sourceTime as a journal-style ISO-8601 UTC string.
func FormatISO(sourceTime time.Time) string { return sourceTime.UTC().Format(ISOLayout) }

// ParseISO parses a journal timestamp. It accepts the canonical journal
// layout as well as RFC3339 (with offsets / fractional seconds).
func ParseISO(rawTimestamp string) (time.Time, error) {
	rawTimestamp = strings.TrimSpace(rawTimestamp)
	if parsedTime, parseError := time.Parse(ISOLayout, rawTimestamp); parseError == nil {
		return parsedTime, nil
	}
	parsedTime, parseError := time.Parse(time.RFC3339Nano, rawTimestamp)
	if parseError != nil {
		return time.Time{}, fmt.Errorf("invalid ISO-8601 timestamp %q: %w", rawTimestamp, parseError)
	}
	return parsedTime.UTC(), nil
}

// NormalizeISO converts any RFC3339 timestamp into the canonical layout.
func NormalizeISO(rawTimestamp string) (string, error) {
	parsedTime, parseError := ParseISO(rawTimestamp)
	if parseError != nil {
		return "", parseError
	}
	return FormatISO(parsedTime), nil
}

// ToGalactic converts a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z")
// into Galactic Standard Time ("3312-01-18 19:42:10 UTC").
//
// The year shift is done on the formatted string rather than via time.AddDate
// so that dates such as 29 February are preserved verbatim (3314 is not a
// leap year in the Gregorian calendar, but the game simply relabels the year).
func ToGalactic(isoTimestamp string) string {
	if isoTimestamp == "" {
		return ""
	}
	parsedTime, parseError := ParseISO(isoTimestamp)
	if parseError != nil {
		return isoTimestamp
	}
	return TimeToGalactic(parsedTime)
}

// TimeToGalactic converts a time.Time into Galactic Standard Time.
func TimeToGalactic(sourceTime time.Time) string {
	sourceTime = sourceTime.UTC()
	return fmt.Sprintf("%04d-%s%s", sourceTime.Year()+GalacticYearOffset, sourceTime.Format("01-02 15:04:05"), GalacticLayoutSuffix)
}

// FromGalactic converts "3312-01-18 19:42:10 UTC" (the " UTC" suffix, the
// seconds and the time part are optional; a "T" separator is accepted) back
// into a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z").
func FromGalactic(galacticTimeString string) (string, error) {
	cleanedString := strings.TrimSpace(galacticTimeString)
	cleanedString = strings.TrimSuffix(cleanedString, "UTC")
	cleanedString = strings.TrimSpace(cleanedString)
	cleanedString = strings.Replace(cleanedString, "T", " ", 1)
	cleanedString = strings.TrimSuffix(cleanedString, "Z")
	if len(cleanedString) < 10 || cleanedString[4] != '-' {
		return "", fmt.Errorf("invalid galactic timestamp %q", galacticTimeString)
	}
	parsedYear, parseYearError := strconv.Atoi(cleanedString[:4])
	if parseYearError != nil {
		return "", fmt.Errorf("invalid galactic year in %q", galacticTimeString)
	}
	realYear := parsedYear - GalacticYearOffset
	if realYear < 1 {
		return "", fmt.Errorf("galactic year %d out of range", parsedYear)
	}
	restString := cleanedString[4:] // "-01-18 19:42:10"
	layoutPatterns := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	candidate := fmt.Sprintf("%04d%s", realYear, restString)
	for _, layoutPattern := range layoutPatterns {
		if parsedTime, parseError := time.Parse(layoutPattern, candidate); parseError == nil {
			return FormatISO(parsedTime), nil
		}
	}
	return "", fmt.Errorf("invalid galactic timestamp %q", galacticTimeString)
}

// DurationLabel renders an elapsed duration with hours and minutes, including years if >= 365 days.
func DurationLabel(durationVal time.Duration) string {
	if durationVal < 0 {
		durationVal = 0
	}
	totalDays := int(durationVal.Hours()) / 24
	hours := int(durationVal.Hours()) % 24
	mins := int(durationVal.Minutes()) % 60
	if totalDays >= 365 {
		years := totalDays / 365
		days := totalDays % 365
		if days > 0 {
			return fmt.Sprintf("%dy %dd %02dh %02dm", years, days, hours, mins)
		}
		return fmt.Sprintf("%dy %02dh %02dm", years, hours, mins)
	}
	if totalDays > 0 {
		return fmt.Sprintf("%dd %02dh %02dm", totalDays, hours, mins)
	}
	return fmt.Sprintf("%dh %02dm", hours, mins)
}

// DurationDaysLabel renders an elapsed duration in rounded days, including years if >= 365 days (no hours/mins).
// Examples: "0d", "1d", "42d", "1y", "1y 15d", "3y 120d", "2y".
func DurationDaysLabel(durationVal time.Duration) string {
	if durationVal < 0 {
		durationVal = 0
	}
	totalDays := int(math.Round(durationVal.Hours() / 24.0))
	if totalDays < 365 {
		return fmt.Sprintf("%dd", totalDays)
	}
	years := totalDays / 365
	days := totalDays % 365
	if days > 0 {
		return fmt.Sprintf("%dy %dd", years, days)
	}
	return fmt.Sprintf("%dy", years)
}
