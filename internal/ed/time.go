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

// FormatISO formats t as a journal-style ISO-8601 UTC string.
func FormatISO(t time.Time) string { return t.UTC().Format(ISOLayout) }

// ParseISO parses a journal timestamp. It accepts the canonical journal
// layout as well as RFC3339 (with offsets / fractional seconds).
func ParseISO(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(ISOLayout, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid ISO-8601 timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// NormalizeISO converts any RFC3339 timestamp into the canonical layout.
func NormalizeISO(s string) (string, error) {
	t, err := ParseISO(s)
	if err != nil {
		return "", err
	}
	return FormatISO(t), nil
}

// ToGalactic converts a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z")
// into Galactic Standard Time ("3312-01-18 19:42:10 UTC").
//
// The year shift is done on the formatted string rather than via time.AddDate
// so that dates such as 29 February are preserved verbatim (3314 is not a
// leap year in the Gregorian calendar, but the game simply relabels the year).
func ToGalactic(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := ParseISO(iso)
	if err != nil {
		return iso
	}
	return TimeToGalactic(t)
}

// TimeToGalactic converts a time.Time into Galactic Standard Time.
func TimeToGalactic(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%04d-%s%s", t.Year()+GalacticYearOffset, t.Format("01-02 15:04:05"), GalacticLayoutSuffix)
}

// FromGalactic converts "3312-01-18 19:42:10 UTC" (the " UTC" suffix, the
// seconds and the time part are optional; a "T" separator is accepted) back
// into a real ISO-8601 UTC timestamp ("2026-01-18T19:42:10Z").
func FromGalactic(gst string) (string, error) {
	s := strings.TrimSpace(gst)
	s = strings.TrimSuffix(s, "UTC")
	s = strings.TrimSpace(s)
	s = strings.Replace(s, "T", " ", 1)
	s = strings.TrimSuffix(s, "Z")
	if len(s) < 10 || s[4] != '-' {
		return "", fmt.Errorf("invalid galactic timestamp %q", gst)
	}
	year, err := strconv.Atoi(s[:4])
	if err != nil {
		return "", fmt.Errorf("invalid galactic year in %q", gst)
	}
	realYear := year - GalacticYearOffset
	if realYear < 1 {
		return "", fmt.Errorf("galactic year %d out of range", year)
	}
	rest := s[4:] // "-01-18 19:42:10"
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	candidate := fmt.Sprintf("%04d%s", realYear, rest)
	for _, l := range layouts {
		if t, err := time.Parse(l, candidate); err == nil {
			return FormatISO(t), nil
		}
	}
	return "", fmt.Errorf("invalid galactic timestamp %q", gst)
}

// DurationLabel renders an elapsed duration with hours and minutes, including years if >= 365 days.
func DurationLabel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalDays := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
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
func DurationDaysLabel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalDays := int(math.Round(d.Hours() / 24.0))
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
