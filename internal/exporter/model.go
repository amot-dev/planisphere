// Package exporter assembles canonical expedition export snapshots from cached
// backend projections and writes single-file or zipped export bundles to disk.
package exporter

import (
	"database/sql"
	"encoding/json"
	"time"

	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

// SchemaVersion identifies the canonical JSON snapshot structure for forward compatibility.
const SchemaVersion = 1

// TelemetrySnapshot pairs navigation and discovery metrics with their realized
// payout and deduction breakdown for JSON serialization.
type TelemetrySnapshot struct {
	Metrics     replaystats.Metrics              `json:"metrics"`
	Realization replaystats.RealizationBreakdown `json:"realization"`
}

// ModeBucket holds expedition telemetry aggregated for a single flight mode.
type ModeBucket struct {
	ModeID      int64                            `json:"modeId"`
	ModeName    string                           `json:"modeName"`
	ModeColor   string                           `json:"modeColor"`
	Metrics     replaystats.Metrics              `json:"metrics"`
	Realization replaystats.RealizationBreakdown `json:"realization"`
}

// HighlightRankingEntry holds one ranked leg or waypoint row inside a Highlight leaderboard.
type HighlightRankingEntry struct {
	Rank             int     `json:"rank"`
	Label            string  `json:"label"`
	RouteLabel       string  `json:"routeLabel"`
	PrimaryValue     float64 `json:"primaryValue"`
	FormattedPrimary string  `json:"formattedPrimary"`
	SecondaryLabel   string  `json:"secondaryLabel"`
	ModeName         string  `json:"modeName,omitempty"`
	ModeColor        string  `json:"modeColor,omitempty"`
	InSystem         bool    `json:"inSystem,omitempty"`
	RowID            int64   `json:"rowId"`
	WaypointNumber   int     `json:"waypointNumber"`
}

// HighlightLeaderboard holds the complete ordered ranking for one Highlight category.
type HighlightLeaderboard struct {
	CardID  string                  `json:"cardId"`
	Title   string                  `json:"title"`
	Accent  string                  `json:"accent"`
	Entries []HighlightRankingEntry `json:"entries"`
}

// ExpeditionExport is the canonical, unfiltered export payload for an expedition.
type ExpeditionExport struct {
	SchemaVersion          int                          `json:"schemaVersion"`
	ExportedAt             string                       `json:"exportedAt"`
	ExportedAtGalactic     string                       `json:"exportedAtGalactic"`
	AppVersion             string                       `json:"appVersion"`
	Commanders             []string                     `json:"commanders"`
	Departed               bool                         `json:"departed"`
	EffectiveStart         string                       `json:"effectiveStart"`
	EffectiveStartGalactic string                       `json:"effectiveStartGalactic"`
	EffectiveEnd           string                       `json:"effectiveEnd"`
	EffectiveEndGalactic   string                       `json:"effectiveEndGalactic"`
	EndIsNow               bool                         `json:"endIsNow"`
	Duration               string                       `json:"duration"`
	CareerStart            string                       `json:"careerStart"`
	CareerStartGalactic    string                       `json:"careerStartGalactic"`
	CareerDuration         string                       `json:"careerDuration"`
	Plan                   expedition.PlanView          `json:"plan"`
	Expedition             TelemetrySnapshot            `json:"expedition"`
	Live                   replaystats.Metrics          `json:"live"`
	Adjustments            replaystats.Metrics          `json:"adjustments"`
	CarrierCost            expedition.CarrierCost       `json:"carrierCost"`
	OriginBaseline         *expedition.WaypointDetails  `json:"originBaseline,omitempty"`
	ModeBuckets            []ModeBucket                 `json:"modeBuckets"`
	RowDetails             []expedition.WaypointDetails `json:"rowDetails"`
	HighlightsFullOrder    []HighlightLeaderboard       `json:"highlightsFullOrder"`
}

// BundleFile represents one rendered output file ready to write directly or package into a zip archive.
type BundleFile struct {
	Name                      string `json:"name"`
	ContentBase64             string `json:"contentBase64,omitempty"`
	ContentText               string `json:"contentText,omitempty"`
	CanonicalJSONExpeditionID int64  `json:"canonicalJsonExpeditionId,omitempty"`
}

// MarshalCanonicalJSON serializes an ExpeditionExport snapshot as indented canonical JSON.
func MarshalCanonicalJSON(snapshot ExpeditionExport) ([]byte, error) {
	return json.MarshalIndent(snapshot, "", "  ")
}

// BuilderDeps injects the read-only projections and helpers required to assemble an export snapshot.
type BuilderDeps struct {
	Projections *expedition.Projections
	GECLookup   expedition.GECLookup
	AppVersion  string
	Now         func() time.Time
}

func validateBuilderDeps(databaseConnection *sql.DB, dependencies BuilderDeps) error {
	if databaseConnection == nil {
		return ErrNilDatabase
	}
	if dependencies.Projections == nil || dependencies.Projections.TimeRangeStats == nil {
		return ErrMissingProjections
	}
	return nil
}

