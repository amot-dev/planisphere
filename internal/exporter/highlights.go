package exporter

import (
	"cmp"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"planisphere/internal/reference"
	replaystats "planisphere/internal/replay/stats"
	"planisphere/internal/store/expedition"
)

type visitedLeg struct {
	rowView expedition.RowView
	details expedition.WaypointDetails
}

// ComputeHighlightLeaderboards computes all nine canonical Highlight leaderboards
// across visited non-origin legs and visited coordinate waypoints in stable plan order.
func ComputeHighlightLeaderboards(
	planView expedition.PlanView,
	rowDetails []expedition.WaypointDetails,
	currentTime time.Time,
) []HighlightLeaderboard {
	visitedLegs := collectVisitedLegs(planView, rowDetails)
	return []HighlightLeaderboard{
		buildMostLucrativeBoard(visitedLegs),
		buildDistanceBoard(visitedLegs, false),
		buildMostJumpsBoard(visitedLegs),
		buildLongestDurationBoard(visitedLegs, currentTime),
		buildDistanceBoard(visitedLegs, true),
		buildMostBodiesScannedBoard(visitedLegs),
		buildMostOrganicSamplesBoard(visitedLegs),
		buildFurthestFromSolBoard(planView),
		buildCostliestMistakeBoard(visitedLegs),
	}
}

func collectVisitedLegs(planView expedition.PlanView, rowDetails []expedition.WaypointDetails) []visitedLeg {
	detailsByRowID := make(map[int64]expedition.WaypointDetails, len(rowDetails))
	for _, detailEntry := range rowDetails {
		detailsByRowID[detailEntry.RowID] = detailEntry
	}
	visitedLegs := make([]visitedLeg, 0, len(rowDetails))
	for _, rowView := range planView.Rows {
		if rowView.Row.RowType == expedition.RowRegion || rowView.Number <= 1 || rowView.Row.VisitedAt == "" {
			continue
		}
		if details, hasDetails := detailsByRowID[rowView.Row.ID]; hasDetails {
			visitedLegs = append(visitedLegs, visitedLeg{rowView: rowView, details: details})
		}
	}
	return visitedLegs
}

func rankLegBoard[Score cmp.Ordered](
	cardID, title, accent string,
	legs []visitedLeg,
	ascending bool,
	scoreFn func(visitedLeg) (Score, float64, bool),
	formatFn func(visitedLeg, Score) (string, string),
) HighlightLeaderboard {
	type scoredItem struct {
		leg          visitedLeg
		score        Score
		primaryValue float64
	}
	scored := make([]scoredItem, 0, len(legs))
	for _, leg := range legs {
		if score, primaryValue, ok := scoreFn(leg); ok {
			scored = append(scored, scoredItem{leg: leg, score: score, primaryValue: primaryValue})
		}
	}
	sort.SliceStable(scored, func(leftIndex, rightIndex int) bool {
		if ascending {
			return scored[leftIndex].score < scored[rightIndex].score
		}
		return scored[leftIndex].score > scored[rightIndex].score
	})
	entries := make([]HighlightRankingEntry, len(scored))
	for index, item := range scored {
		formattedPrimary, secondaryLabel := formatFn(item.leg, item.score)
		entries[index] = makeLegEntry(item.leg, index+1, item.primaryValue, formattedPrimary, secondaryLabel)
	}
	return HighlightLeaderboard{CardID: cardID, Title: title, Accent: accent, Entries: entries}
}

func makeLegEntry(
	leg visitedLeg,
	rank int,
	primaryValue float64,
	formattedPrimary, secondaryLabel string,
) HighlightRankingEntry {
	previousSystem := leg.details.PrevSystem
	if previousSystem == "" {
		previousSystem = "Origin"
	}
	return HighlightRankingEntry{
		Rank:             rank,
		Label:            fmt.Sprintf("Leg #%d -> #%d", leg.details.Number-1, leg.details.Number),
		RouteLabel:       fmt.Sprintf("%s -> %s", previousSystem, leg.rowView.Row.SystemName),
		PrimaryValue:     primaryValue,
		FormattedPrimary: formattedPrimary,
		SecondaryLabel:   secondaryLabel,
		ModeName:         leg.rowView.LegModeName,
		ModeColor:        leg.rowView.LegModeColor,
		InSystem:         leg.details.InSystem,
		RowID:            leg.rowView.Row.ID,
		WaypointNumber:   leg.details.Number,
	}
}

func buildMostLucrativeBoard(legs []visitedLeg) HighlightLeaderboard {
	return rankLegBoard(
		"mostLucrative", "Most Lucrative Leg", "amber", legs, false,
		func(leg visitedLeg) (int64, float64, bool) {
			estimated := leg.details.Metrics.ScanValueTotal + leg.details.Metrics.OrganicValueTotal
			return estimated, float64(estimated), estimated > 0
		},
		func(leg visitedLeg, estimated int64) (string, string) {
			secondary := fmt.Sprintf("Carto %s · Exo %s",
				formatCredits(leg.details.Metrics.ScanValueTotal),
				formatCredits(leg.details.Metrics.OrganicValueTotal))
			return formatCredits(estimated), secondary
		},
	)
}

func buildDistanceBoard(legs []visitedLeg, shortest bool) HighlightLeaderboard {
	cardID, title := "longestDistance", "Longest Leg (Distance)"
	if shortest {
		cardID, title = "shortestDistance", "Shortest Leg (Distance)"
	}
	return rankLegBoard(
		cardID, title, "amber", legs, shortest,
		func(leg visitedLeg) (float64, float64, bool) {
			distanceLy := leg.details.Metrics.DistanceLy
			return distanceLy, distanceLy, distanceLy > 0
		},
		func(leg visitedLeg, distanceLy float64) (string, string) {
			secondary := fmt.Sprintf("%s jumps", formatInt64WithCommas(int64(leg.details.Metrics.Jumps)))
			if leg.details.Elapsed != "" {
				secondary = fmt.Sprintf("%s · %s", secondary, leg.details.Elapsed)
			}
			return formatLightYearsOneDecimal(distanceLy), secondary
		},
	)
}

func buildMostJumpsBoard(legs []visitedLeg) HighlightLeaderboard {
	return rankLegBoard(
		"mostJumps", "Most Jumps on a Leg", "amber", legs, false,
		func(leg visitedLeg) (int, float64, bool) {
			jumps := leg.details.Metrics.Jumps
			return jumps, float64(jumps), jumps > 0
		},
		func(leg visitedLeg, jumps int) (string, string) {
			shipDistanceLy := computeShipDistanceLy(leg.details.Metrics)
			averageJumpLy := shipDistanceLy / float64(jumps)
			secondary := fmt.Sprintf("%s · %s avg", formatLightYearsOneDecimal(shipDistanceLy), formatLightYearsOneDecimal(averageJumpLy))
			return fmt.Sprintf("%s jumps", formatInt64WithCommas(int64(jumps))), secondary
		},
	)
}

func buildLongestDurationBoard(legs []visitedLeg, currentTime time.Time) HighlightLeaderboard {
	return rankLegBoard(
		"longestDuration", "Longest Leg (Duration)", "cyan", legs, false,
		func(leg visitedLeg) (int64, float64, bool) {
			durationMs, _ := resolveLegDuration(leg.details, currentTime)
			return durationMs, float64(durationMs), durationMs > 0
		},
		func(leg visitedLeg, _ int64) (string, string) {
			_, elapsed := resolveLegDuration(leg.details, currentTime)
			secondary := fmt.Sprintf("%s · %s jumps",
				formatLightYearsOneDecimal(leg.details.Metrics.DistanceLy),
				formatInt64WithCommas(int64(leg.details.Metrics.Jumps)))
			return elapsed, secondary
		},
	)
}

func resolveLegDuration(details expedition.WaypointDetails, currentTime time.Time) (int64, string) {
	startTime, parseStartError := reference.ParseISO(details.From)
	if parseStartError != nil {
		return 0, ""
	}
	endTime := currentTime
	if details.To != "" {
		parsedEnd, parseEndError := reference.ParseISO(details.To)
		if parseEndError != nil {
			return 0, ""
		}
		endTime = parsedEnd
	}
	durationMs := endTime.UnixMilli() - startTime.UnixMilli()
	if durationMs <= 0 {
		return 0, ""
	}
	if details.Elapsed != "" {
		return durationMs, details.Elapsed
	}
	return durationMs, reference.DurationLabel(endTime.Sub(startTime))
}

func buildMostBodiesScannedBoard(legs []visitedLeg) HighlightLeaderboard {
	return rankLegBoard(
		"mostBodiesScanned", "Most Prolific Survey", "cyan", legs, false,
		func(leg visitedLeg) (int, float64, bool) {
			bodies := leg.details.Metrics.BodiesScanned
			return bodies, float64(bodies), bodies > 0
		},
		func(leg visitedLeg, bodies int) (string, string) {
			metrics := leg.details.Metrics
			secondary := fmt.Sprintf("%s mapped · %s first disc.",
				formatInt64WithCommas(int64(metrics.BodiesMapped)),
				formatInt64WithCommas(int64(metrics.FirstDiscoveries)))
			return fmt.Sprintf("%s bodies", formatInt64WithCommas(int64(bodies))), secondary
		},
	)
}

func buildMostOrganicSamplesBoard(legs []visitedLeg) HighlightLeaderboard {
	return rankLegBoard(
		"mostOrganicSamples", "Most Prolific Exobiology", "emerald", legs, false,
		func(leg visitedLeg) (int, float64, bool) {
			samples := leg.details.Metrics.OrganicSamples
			return samples, float64(samples), samples > 0
		},
		func(leg visitedLeg, samples int) (string, string) {
			metrics := leg.details.Metrics
			secondary := fmt.Sprintf("%s first logged · %s",
				formatInt64WithCommas(int64(metrics.FirstLogged)),
				formatCredits(metrics.OrganicValueTotal))
			return fmt.Sprintf("%s samples", formatInt64WithCommas(int64(samples))), secondary
		},
	)
}

func buildFurthestFromSolBoard(planView expedition.PlanView) HighlightLeaderboard {
	type scoredWaypoint struct {
		rowView           expedition.RowView
		distanceFromSolLy float64
	}
	scored := make([]scoredWaypoint, 0, len(planView.Rows))
	for _, rowView := range planView.Rows {
		row := rowView.Row
		if row.RowType == expedition.RowWaypoint && row.VisitedAt != "" && row.HasCoords {
			distanceFromSolLy := math.Sqrt(row.X*row.X + row.Y*row.Y + row.Z*row.Z)
			scored = append(scored, scoredWaypoint{rowView: rowView, distanceFromSolLy: distanceFromSolLy})
		}
	}
	sort.SliceStable(scored, func(leftIndex, rightIndex int) bool {
		return scored[leftIndex].distanceFromSolLy > scored[rightIndex].distanceFromSolLy
	})
	entries := make([]HighlightRankingEntry, len(scored))
	for index, item := range scored {
		regionLabel := item.rowView.DetectedRegion
		if regionLabel == "" {
			regionLabel = "Unknown Region"
		}
		entries[index] = HighlightRankingEntry{
			Rank:             index + 1,
			Label:            fmt.Sprintf("Waypoint #%d", item.rowView.Number),
			RouteLabel:       item.rowView.Row.SystemName,
			PrimaryValue:     item.distanceFromSolLy,
			FormattedPrimary: formatLightYearsOneDecimal(item.distanceFromSolLy),
			SecondaryLabel:   regionLabel,
			ModeName:         item.rowView.LegModeName,
			ModeColor:        item.rowView.LegModeColor,
			RowID:            item.rowView.Row.ID,
			WaypointNumber:   item.rowView.Number,
		}
	}
	return HighlightLeaderboard{CardID: "furthestFromSol", Title: "Furthest From Sol", Accent: "cyan", Entries: entries}
}

func buildCostliestMistakeBoard(legs []visitedLeg) HighlightLeaderboard {
	return rankLegBoard(
		"costliestMistake", "Costliest Mistake", "danger", legs, false,
		func(leg visitedLeg) (int64, float64, bool) {
			deathLoss := leg.details.Realization.CartoLostDeath + leg.details.Realization.OrganicLostDeath
			return deathLoss, float64(deathLoss), deathLoss > 0
		},
		func(leg visitedLeg, deathLoss int64) (string, string) {
			realization := leg.details.Realization
			secondary := fmt.Sprintf("Carto -%s · Exo -%s",
				formatCredits(realization.CartoLostDeath),
				formatCredits(realization.OrganicLostDeath))
			return fmt.Sprintf("-%s", formatCredits(deathLoss)), secondary
		},
	)
}

func computeShipDistanceLy(metrics replaystats.Metrics) float64 {
	if metrics.ShipDistanceLy != 0 {
		return metrics.ShipDistanceLy
	}
	return math.Max(0, metrics.DistanceLy-metrics.CarrierPassengerLy)
}

func formatCredits(credits int64) string {
	return formatInt64WithCommas(credits) + " Cr"
}

func formatLightYearsOneDecimal(lightYears float64) string {
	roundedTenths := int64(math.Round(lightYears * 10))
	if roundedTenths < 0 {
		magnitudeTenths := -roundedTenths
		return fmt.Sprintf("-%s.%d Ly", formatInt64WithCommas(magnitudeTenths/10), magnitudeTenths%10)
	}
	return fmt.Sprintf("%s.%d Ly", formatInt64WithCommas(roundedTenths/10), roundedTenths%10)
}

func formatInt64WithCommas(value int64) string {
	negative := value < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(^value) + 1
	} else {
		magnitude = uint64(value)
	}
	digits := strconv.FormatUint(magnitude, 10)
	if len(digits) <= 3 {
		if negative {
			return "-" + digits
		}
		return digits
	}
	var builder strings.Builder
	builder.Grow(len(digits) + (len(digits)-1)/3 + 1)
	if negative {
		builder.WriteByte('-')
	}
	leadingCount := len(digits) % 3
	if leadingCount == 0 {
		leadingCount = 3
	}
	builder.WriteString(digits[:leadingCount])
	for offset := leadingCount; offset < len(digits); offset += 3 {
		builder.WriteByte(',')
		builder.WriteString(digits[offset : offset+3])
	}
	return builder.String()
}
