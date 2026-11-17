package stats

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"planisphere/internal/core/events"
	"planisphere/internal/core/projection"
	"planisphere/internal/projections"
	"planisphere/internal/projections/realization"
	"planisphere/internal/sources/journal"
)

var (
	timeRangeStatsJournalEvents = journal.NewEventSet(
		journal.EventLoadGame,
		journal.EventLocation,
		journal.EventFSDJump,
		journal.EventCarrierJump,
		journal.EventCarrierStats,
		journal.EventDocked,
		journal.EventUndocked,
		journal.EventStartJump,
		journal.EventJetConeBoost,
		journal.EventSynthesis,
		journal.EventShipyardSwap,
		journal.EventScan,
		journal.EventSAAScanComplete,
		journal.EventScanOrganic,
		journal.EventSellExplorationData,
		journal.EventMultiSellExplorationData,
		journal.EventSellOrganicData,
		journal.EventResurrect,
		journal.EventNpcCrewPaidWage,
	)
	timeRangeStatsAppEvents = events.NewAppSet(
		events.AppCommanderFilterChanged,
		events.AppModesChanged,
		events.AppExpeditionBoundsChanged,
		events.AppDatabaseSwitched,
	)
)

// TimeRangeStats caches travel and exploration telemetry for bounded
// (after, until] time ranges and optional Flight Mode filters, serving both
// the active Expedition sidebar and Waypoint #1 pre-expedition baselines.
type TimeRangeStats struct {
	database       *sql.DB
	salesLedger    *realization.SalesLedger
	recoveredJumps *projections.RecoveredJumps
	entries        *projection.KeyedCache[string, TelemetrySummary]
}

// NewTimeRangeStats creates a TimeRangeStats projection and registers it on bus.
func NewTimeRangeStats(
	dbConn *sql.DB,
	bus *projection.Bus,
	salesLedger *realization.SalesLedger,
	recoveredJumps *projections.RecoveredJumps,
) *TimeRangeStats {
	timeRangeStats := &TimeRangeStats{
		database:       dbConn,
		salesLedger:    salesLedger,
		recoveredJumps: recoveredJumps,
	}
	timeRangeStats.entries = projection.NewKeyedCache[string, TelemetrySummary](
		bus,
		"time_range_stats",
		timeRangeStats.Triggers(),
	)
	return timeRangeStats
}

// Triggers declares the journal and application events that invalidate TimeRangeStats.
func (timeRangeStats *TimeRangeStats) Triggers() projection.Triggers {
	return projection.Triggers{
		Journal: timeRangeStatsJournalEvents,
		App:     timeRangeStatsAppEvents,
	}
}

// CachedCount returns the number of bounded range telemetry entries currently cached.
func (timeRangeStats *TimeRangeStats) CachedCount() int {
	return timeRangeStats.entries.Len()
}

// Generation returns the cache invalidation generation counter.
func (timeRangeStats *TimeRangeStats) Generation() uint64 {
	return timeRangeStats.entries.Generation()
}

// Compute returns cached or freshly evaluated metrics and realization breakdown
// for timeRange and optional mode filter. Carrier operations metrics are
// preserved unzeroed so origin baselines can compute pre-expedition deltas.
func (timeRangeStats *TimeRangeStats) Compute(timeRange Range, filter *Filter) (TelemetrySummary, error) {
	if timeRangeStats.database == nil {
		return TelemetrySummary{}, fmt.Errorf("time range stats database is nil")
	}
	cacheKey := buildTimeRangeStatsKey(timeRange, filter)
	return timeRangeStats.entries.GetOrLoad(cacheKey, func() (TelemetrySummary, error) {
		engine := NewEngineWithProjections(timeRangeStats.database, timeRangeStats.salesLedger, timeRangeStats.recoveredJumps)
		var filterCopy *Filter
		if filter != nil {
			copied := *filter
			copied.preparedSets = nil
			copied.preparedDB = nil
			filterCopy = &copied
		}
		computedMetrics, computeError := engine.Compute(timeRange, filterCopy)
		if computeError != nil {
			return TelemetrySummary{}, computeError
		}
		computedRealization, realizationError := engine.ComputeRealization(timeRange, filterCopy)
		if realizationError != nil {
			return TelemetrySummary{}, realizationError
		}
		return TelemetrySummary{
			Metrics:     computedMetrics,
			Realization: computedRealization,
		}, nil
	})
}

func buildTimeRangeStatsKey(timeRange Range, filter *Filter) string {
	var keyBuilder strings.Builder
	keyBuilder.Grow(64)
	keyBuilder.WriteString(timeRange.After)
	keyBuilder.WriteByte('|')
	keyBuilder.WriteString(timeRange.Until)
	keyBuilder.WriteByte('|')
	if filter == nil || !filter.Enabled {
		keyBuilder.WriteString("unfiltered")
		return keyBuilder.String()
	}
	keyBuilder.WriteString("filtered:")
	activeSelections := make([]ModeSelection, 0, len(filter.Modes))
	for _, selection := range filter.Modes {
		if selection.Travel || selection.Discovery {
			activeSelections = append(activeSelections, selection)
		}
	}
	slices.SortFunc(activeSelections, func(left, right ModeSelection) int {
		if left.ModeID < right.ModeID {
			return -1
		}
		if left.ModeID > right.ModeID {
			return 1
		}
		return 0
	})
	wroteFirst := false
	for index := 0; index < len(activeSelections); index++ {
		merged := activeSelections[index]
		for index+1 < len(activeSelections) && activeSelections[index+1].ModeID == merged.ModeID {
			index++
			merged.Travel = merged.Travel || activeSelections[index].Travel
			merged.Discovery = merged.Discovery || activeSelections[index].Discovery
		}
		if wroteFirst {
			keyBuilder.WriteByte(',')
		}
		wroteFirst = true
		keyBuilder.WriteString(strconv.FormatInt(merged.ModeID, 10))
		keyBuilder.WriteByte(':')
		if merged.Travel {
			keyBuilder.WriteByte('t')
		} else {
			keyBuilder.WriteByte('-')
		}
		if merged.Discovery {
			keyBuilder.WriteByte('d')
		} else {
			keyBuilder.WriteByte('-')
		}
	}
	return keyBuilder.String()
}

