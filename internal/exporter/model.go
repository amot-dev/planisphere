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

// DerivedMetrics holds derived averages, ratios, and profit metrics computed
// with zero-division, sub-24-hour calendar, and sub-1-hour in-game rate guards.
type DerivedMetrics struct {
	ShipDistanceLy              float64  `json:"shipDistanceLy"`
	TotalBoosts                 int      `json:"totalBoosts"`
	AvgJumpRangeLy              *float64 `json:"avgJumpRangeLy"`
	BoostRatePercent            *float64 `json:"boostRatePercent"`
	AvgDistancePerDayLy         *float64 `json:"avgDistancePerDayLy"`
	AvgDistancePerHourLy        *float64 `json:"avgDistancePerHourLy"`
	AvgJumpsPerHour             *float64 `json:"avgJumpsPerHour"`
	AvgCarrierJumpRangeLy       *float64 `json:"avgCarrierJumpRangeLy"`
	AvgTritiumPerJump           *float64 `json:"avgTritiumPerJump"`
	CombinedEstimatedCredits    int64    `json:"combinedEstimatedCredits"`
	DSSEfficiencyPercent        *float64 `json:"dssEfficiencyPercent"`
	FirstDiscoveryRatePercent   *float64 `json:"firstDiscoveryRatePercent"`
	FirstLoggedRatePercent      *float64 `json:"firstLoggedRatePercent"`
	AvgCreditsPerSample         *float64 `json:"avgCreditsPerSample"`
	AvgCombinedCreditsPerJump   *float64 `json:"avgCombinedCreditsPerJump"`
	AvgCombinedCreditsPer1000Ly *float64 `json:"avgCombinedCreditsPer1000Ly"`
	AvgCombinedCreditsPerDay    *float64 `json:"avgCombinedCreditsPerDay"`
	AvgCombinedCreditsPerHour   *float64 `json:"avgCombinedCreditsPerHour"`
	AvgCartoCreditsPerJump      *float64 `json:"avgCartoCreditsPerJump"`
	AvgCartoCreditsPer1000Ly    *float64 `json:"avgCartoCreditsPer1000Ly"`
	AvgCartoCreditsPerHour      *float64 `json:"avgCartoCreditsPerHour"`
	AvgBodiesScannedPerHour     *float64 `json:"avgBodiesScannedPerHour"`
	AvgExoCreditsPerJump        *float64 `json:"avgExoCreditsPerJump"`
	AvgExoCreditsPer1000Ly      *float64 `json:"avgExoCreditsPer1000Ly"`
	AvgExoCreditsPerHour        *float64 `json:"avgExoCreditsPerHour"`
	AvgOrganicSamplesPerHour    *float64 `json:"avgOrganicSamplesPerHour"`
	TotalLossesCredits          int64    `json:"totalLossesCredits"`
	RealizationRetentionPercent *float64 `json:"realizationRetentionPercent"`
	NetProfitAfterCarrierCost   *int64   `json:"netProfitAfterCarrierCost"`
	AvgRealizedCreditsPerJump   *float64 `json:"avgRealizedCreditsPerJump"`
	AvgRealizedCreditsPer1000Ly *float64 `json:"avgRealizedCreditsPer1000Ly"`
	AvgRealizedCreditsPerHour   *float64 `json:"avgRealizedCreditsPerHour"`
}

// CareerDiffItem compares one expedition metric against its pre-expedition career baseline.
type CareerDiffItem struct {
	ID            string   `json:"id"`
	Baseline      float64  `json:"baseline"`
	GrowthPercent *float64 `json:"growthPercent"`
	IsNewBaseline bool     `json:"isNewBaseline"`
}

// ExportLegTelemetry holds precomputed region attribution, elapsed days, ships flown,
// and derived rates for a visited non-origin waypoint leg.
type ExportLegTelemetry struct {
	RowID                  int64          `json:"rowId"`
	RegionName             string         `json:"regionName"`
	ElapsedDays            *float64       `json:"elapsedDays"`
	InGameSeconds          *int64         `json:"inGameSeconds,omitempty"`
	InGameDuration         string         `json:"inGameDuration,omitempty"`
	TransitInGameSeconds   *int64         `json:"transitInGameSeconds,omitempty"`
	TransitInGameDuration  string         `json:"transitInGameDuration,omitempty"`
	InSystemInGameSeconds  *int64         `json:"inSystemInGameSeconds,omitempty"`
	InSystemInGameDuration string         `json:"inSystemInGameDuration,omitempty"`
	ShipsFlown             []string       `json:"shipsFlown"`
	Derived                DerivedMetrics `json:"derived"`
}

// RegionModeBucket holds telemetry aggregated for a single flight mode within a Galactic Region.
type RegionModeBucket struct {
	ModeID                 int64                            `json:"modeId"`
	ModeName               string                           `json:"modeName"`
	ModeColor              string                           `json:"modeColor"`
	LegCount               int                              `json:"legCount"`
	FromGalactic           string                           `json:"fromGalactic"`
	ToGalactic             string                           `json:"toGalactic"`
	InSystem               bool                             `json:"inSystem"`
	Duration               string                           `json:"duration"`
	ElapsedDays            *float64                         `json:"elapsedDays"`
	InGameSeconds          *int64                           `json:"inGameSeconds,omitempty"`
	InGameDuration         string                           `json:"inGameDuration,omitempty"`
	TransitInGameSeconds   *int64                           `json:"transitInGameSeconds,omitempty"`
	TransitInGameDuration  string                           `json:"transitInGameDuration,omitempty"`
	InSystemInGameSeconds  *int64                           `json:"inSystemInGameSeconds,omitempty"`
	InSystemInGameDuration string                           `json:"inSystemInGameDuration,omitempty"`
	CoveredLy              float64                          `json:"coveredLy"`
	ShipsFlown             []string                         `json:"shipsFlown"`
	Metrics                replaystats.Metrics              `json:"metrics"`
	Realization            replaystats.RealizationBreakdown `json:"realization"`
	Derived                DerivedMetrics                   `json:"derived"`
}

// RegionBucket holds combined and per-mode telemetry aggregated across visited legs in a Galactic Region.
type RegionBucket struct {
	RegionName             string                           `json:"regionName"`
	LegRowIDs              []int64                          `json:"legRowIds"`
	LegCount               int                              `json:"legCount"`
	FromGalactic           string                           `json:"fromGalactic"`
	ToGalactic             string                           `json:"toGalactic"`
	InSystem               bool                             `json:"inSystem"`
	Duration               string                           `json:"duration"`
	ElapsedDays            *float64                         `json:"elapsedDays"`
	InGameSeconds          *int64                           `json:"inGameSeconds,omitempty"`
	InGameDuration         string                           `json:"inGameDuration,omitempty"`
	TransitInGameSeconds   *int64                           `json:"transitInGameSeconds,omitempty"`
	TransitInGameDuration  string                           `json:"transitInGameDuration,omitempty"`
	InSystemInGameSeconds  *int64                           `json:"inSystemInGameSeconds,omitempty"`
	InSystemInGameDuration string                           `json:"inSystemInGameDuration,omitempty"`
	CoveredLy              float64                          `json:"coveredLy"`
	ShipsFlown             []string                         `json:"shipsFlown"`
	Metrics                replaystats.Metrics              `json:"metrics"`
	Realization            replaystats.RealizationBreakdown `json:"realization"`
	Derived                DerivedMetrics                   `json:"derived"`
	ModeBuckets            []RegionModeBucket               `json:"modeBuckets"`
}

// TelemetrySnapshot pairs navigation and discovery metrics with their realized
// payout, deduction breakdown, ships flown, and derived rates for JSON serialization.
type TelemetrySnapshot struct {
	Metrics                replaystats.Metrics              `json:"metrics"`
	Realization            replaystats.RealizationBreakdown `json:"realization"`
	ElapsedDays            *float64                         `json:"elapsedDays"`
	InGameSeconds          *int64                           `json:"inGameSeconds,omitempty"`
	InGameDuration         string                           `json:"inGameDuration,omitempty"`
	TransitInGameSeconds   *int64                           `json:"transitInGameSeconds,omitempty"`
	TransitInGameDuration  string                           `json:"transitInGameDuration,omitempty"`
	InSystemInGameSeconds  *int64                           `json:"inSystemInGameSeconds,omitempty"`
	InSystemInGameDuration string                           `json:"inSystemInGameDuration,omitempty"`
	ShipsFlown             []string                         `json:"shipsFlown"`
	Derived                DerivedMetrics                   `json:"derived"`
}

// ModeBucket holds expedition telemetry aggregated for a single flight mode.
type ModeBucket struct {
	ModeID                 int64                            `json:"modeId"`
	ModeName               string                           `json:"modeName"`
	ModeColor              string                           `json:"modeColor"`
	ElapsedDays            *float64                         `json:"elapsedDays"`
	InGameSeconds          *int64                           `json:"inGameSeconds,omitempty"`
	InGameDuration         string                           `json:"inGameDuration,omitempty"`
	TransitInGameSeconds   *int64                           `json:"transitInGameSeconds,omitempty"`
	TransitInGameDuration  string                           `json:"transitInGameDuration,omitempty"`
	InSystemInGameSeconds  *int64                           `json:"inSystemInGameSeconds,omitempty"`
	InSystemInGameDuration string                           `json:"inSystemInGameDuration,omitempty"`
	ShipsFlown             []string                         `json:"shipsFlown"`
	Metrics                replaystats.Metrics              `json:"metrics"`
	Realization            replaystats.RealizationBreakdown `json:"realization"`
	Derived                DerivedMetrics                   `json:"derived"`
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
	SchemaVersion             int                          `json:"schemaVersion"`
	ExportedAt                string                       `json:"exportedAt"`
	ExportedAtGalactic        string                       `json:"exportedAtGalactic"`
	AppVersion                string                       `json:"appVersion"`
	Commanders                []string                     `json:"commanders"`
	Departed                  bool                         `json:"departed"`
	EffectiveStart            string                       `json:"effectiveStart"`
	EffectiveStartGalactic    string                       `json:"effectiveStartGalactic"`
	EffectiveEnd              string                       `json:"effectiveEnd"`
	EffectiveEndGalactic      string                       `json:"effectiveEndGalactic"`
	EndIsNow                  bool                         `json:"endIsNow"`
	Duration                  string                       `json:"duration"`
	InGameSeconds             *int64                       `json:"inGameSeconds,omitempty"`
	InGameDuration            string                       `json:"inGameDuration,omitempty"`
	TransitInGameSeconds      *int64                       `json:"transitInGameSeconds,omitempty"`
	TransitInGameDuration     string                       `json:"transitInGameDuration,omitempty"`
	InSystemInGameSeconds     *int64                       `json:"inSystemInGameSeconds,omitempty"`
	InSystemInGameDuration    string                       `json:"inSystemInGameDuration,omitempty"`
	CareerStart               string                       `json:"careerStart"`
	CareerStartGalactic       string                       `json:"careerStartGalactic"`
	CareerDuration            string                       `json:"careerDuration"`
	Plan                      expedition.PlanView          `json:"plan"`
	Expedition                TelemetrySnapshot            `json:"expedition"`
	Live                      replaystats.Metrics          `json:"live"`
	Adjustments               replaystats.Metrics          `json:"adjustments"`
	CarrierCost               expedition.CarrierCost       `json:"carrierCost"`
	OriginBaseline            *expedition.WaypointDetails  `json:"originBaseline,omitempty"`
	OriginBaselineElapsedDays *float64                     `json:"originBaselineElapsedDays"`
	OriginBaselineDerived     *DerivedMetrics              `json:"originBaselineDerived,omitempty"`
	CareerDiff                []CareerDiffItem             `json:"careerDiff,omitempty"`
	ModeBuckets               []ModeBucket                 `json:"modeBuckets"`
	RowDetails                []expedition.WaypointDetails `json:"rowDetails"`
	LegTelemetry              []ExportLegTelemetry         `json:"legTelemetry"`
	RegionBuckets             []RegionBucket               `json:"regionBuckets"`
	HighlightsFullOrder       []HighlightLeaderboard       `json:"highlightsFullOrder"`
	HighlightsVariants        []HighlightLeaderboard       `json:"highlightsVariants"`
}

// BundleFile represents one rendered output file ready to write directly or package into a zip archive.
// When CanonicalJSONExpeditionID is positive and text/binary content is empty, WriteExportOutput
// serializes the matching CanonicalJSONExportedAt snapshot (or rebuilds on miss) directly in Go.
type BundleFile struct {
	Name                      string `json:"name"`
	ContentBase64             string `json:"contentBase64,omitempty"`
	ContentText               string `json:"contentText,omitempty"`
	CanonicalJSONExpeditionID int64  `json:"canonicalJsonExpeditionId,omitempty"`
	CanonicalJSONExportedAt   string `json:"canonicalJsonExportedAt,omitempty"`
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
