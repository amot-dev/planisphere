import {
  computeShipDistanceLy,
  formatCredits,
  formatElapsedDuration,
  formatInt,
  formatLy,
} from '../../domain/galacticTime'
import { getVisitedLegs, VisitedLegRecord } from './exportMetrics'
import {
  ExpeditionExportData,
  HIGHLIGHT_CARDS,
  HighlightCardId,
  HighlightDepth,
} from './exportTypes'

/** Single ranked row inside a Highlight leaderboard. */
export interface HighlightRankingEntry {
  rank: number
  label: string
  routeLabel: string
  primaryValue: number
  formattedPrimary: string
  secondaryLabel: string
  modeName?: string
  modeColor?: string
  inSystem?: boolean
  rowId: number
  waypointNumber: number
}

/** Complete ordered leaderboard for one Highlight category. */
export interface HighlightLeaderboard {
  cardId: HighlightCardId
  title: string
  accent: 'amber' | 'cyan' | 'emerald' | 'danger'
  entries: HighlightRankingEntry[]
}

/** Options controlling which valuation or distance fields drive Highlight rankings. */
export interface HighlightRankingOptions {
  lucrativeMetric?: 'estimated' | 'realized'
  distanceMetric?: 'total' | 'ship'
  nowMs?: number
}

/**
 * Computes full ordered leaderboards across all nine v1 Highlight categories.
 * Strictly ranks visited legs (WP #2+ with VisitedAt set) and visited waypoints,
 * excluding unvisited in-progress legs.
 */
export function computeAllHighlightLeaderboards(
  exportData: ExpeditionExportData,
  options: HighlightRankingOptions = {},
): HighlightLeaderboard[] {
  const visitedLegs = getVisitedLegs(exportData)
  const nowMs = options.nowMs ?? Date.now()
  const lucrativeMetric = options.lucrativeMetric ?? 'estimated'
  const distanceMetric = options.distanceMetric ?? 'total'

  return [
    buildMostLucrativeLeaderboard(visitedLegs, lucrativeMetric),
    buildDistanceLeaderboard(visitedLegs, distanceMetric, false),
    buildMostJumpsLeaderboard(visitedLegs),
    buildLongestDurationLeaderboard(visitedLegs, nowMs),
    buildDistanceLeaderboard(visitedLegs, distanceMetric, true),
    buildMostBodiesScannedLeaderboard(visitedLegs),
    buildMostOrganicSamplesLeaderboard(visitedLegs),
    buildFurthestFromSolLeaderboard(exportData),
    buildCostliestMistakeLeaderboard(visitedLegs),
  ]
}

/** Filters computed leaderboards to the selected card IDs, drops empty categories, and slices to depth. */
export function selectHighlightCards(
  leaderboards: HighlightLeaderboard[],
  selectedCardIds: HighlightCardId[],
  depth: HighlightDepth,
): HighlightLeaderboard[] {
  const selectedSet = new Set<HighlightCardId>(selectedCardIds)
  return leaderboards
    .filter((board) => selectedSet.has(board.cardId) && board.entries.length > 0)
    .map((board) => ({
      ...board,
      entries: board.entries.slice(0, depth),
    }))
}

/** Serializes the canonical, unfiltered JSON snapshot including full ordered Highlight rankings. */
export function buildCanonicalJsonSnapshot(exportData: ExpeditionExportData): string {
  const fullLeaderboards = computeAllHighlightLeaderboards(exportData)
  return JSON.stringify(
    {
      ...exportData,
      highlightsFullOrder: fullLeaderboards,
    },
    null,
    2,
  )
}

function cardMetadata(cardId: HighlightCardId) {
  return HIGHLIGHT_CARDS.find((card) => card.id === cardId)!
}

function legLabel(leg: VisitedLegRecord): string {
  return `Leg #${leg.details.number - 1} -> #${leg.details.number}`
}

function legRoute(leg: VisitedLegRecord): string {
  return `${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`
}

function rankEntries<T>(
  items: T[],
  compare: (left: T, right: T) => number,
  buildEntry: (item: T, rank: number) => HighlightRankingEntry,
): HighlightRankingEntry[] {
  const sorted = [...items].sort(compare)
  return sorted.map((item, index) => buildEntry(item, index + 1))
}

function buildMostLucrativeLeaderboard(
  legs: VisitedLegRecord[],
  lucrativeMetric: 'estimated' | 'realized',
): HighlightLeaderboard {
  const meta = cardMetadata('mostLucrative')
  const scored = legs
    .map((leg) => {
      const estimated =
        leg.details.metrics.scanValueTotal + leg.details.metrics.organicValueTotal
      const value =
        lucrativeMetric === 'realized' ? leg.details.realization.totalRealized : estimated
      return { leg, value, estimated }
    })
    .filter((item) => item.value > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) => right.value - left.value,
      ({ leg, value }, rank) => ({
        rank,
        label: legLabel(leg),
        routeLabel: legRoute(leg),
        primaryValue: value,
        formattedPrimary: formatCredits(value),
        secondaryLabel: `Carto ${formatCredits(leg.details.metrics.scanValueTotal)} · Exo ${formatCredits(leg.details.metrics.organicValueTotal)}`,
        modeName: leg.rowView.legModeName,
        modeColor: leg.rowView.legModeColor,
        inSystem: leg.details.inSystem,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }),
    ),
  }
}

function buildDistanceLeaderboard(
  legs: VisitedLegRecord[],
  distanceMetric: 'total' | 'ship',
  shortest: boolean,
): HighlightLeaderboard {
  const cardId: HighlightCardId = shortest ? 'shortestDistance' : 'longestDistance'
  const meta = cardMetadata(cardId)
  const scored = legs
    .map((leg) => ({
      leg,
      distanceLy:
        distanceMetric === 'ship'
          ? computeShipDistanceLy(leg.details.metrics)
          : leg.details.metrics.distanceLy,
    }))
    .filter((item) => item.distanceLy > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) =>
        shortest ? left.distanceLy - right.distanceLy : right.distanceLy - left.distanceLy,
      ({ leg, distanceLy }, rank) => ({
        rank,
        label: legLabel(leg),
        routeLabel: legRoute(leg),
        primaryValue: distanceLy,
        formattedPrimary: formatLy(distanceLy, 1),
        secondaryLabel: `${formatInt(leg.details.metrics.jumps)} jumps${leg.details.elapsed ? ` · ${leg.details.elapsed}` : ''}`,
        modeName: leg.rowView.legModeName,
        modeColor: leg.rowView.legModeColor,
        inSystem: leg.details.inSystem,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }),
    ),
  }
}

function buildMostJumpsLeaderboard(legs: VisitedLegRecord[]): HighlightLeaderboard {
  const meta = cardMetadata('mostJumps')
  const scored = legs.filter((leg) => leg.details.metrics.jumps > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) => right.details.metrics.jumps - left.details.metrics.jumps,
      (leg, rank) => {
        const shipLy = computeShipDistanceLy(leg.details.metrics)
        const avgLy = shipLy / leg.details.metrics.jumps
        return {
          rank,
          label: legLabel(leg),
          routeLabel: legRoute(leg),
          primaryValue: leg.details.metrics.jumps,
          formattedPrimary: `${formatInt(leg.details.metrics.jumps)} jumps`,
          secondaryLabel: `${formatLy(shipLy, 1)} · ${formatLy(avgLy, 1)} avg`,
          modeName: leg.rowView.legModeName,
          modeColor: leg.rowView.legModeColor,
          inSystem: leg.details.inSystem,
          rowId: leg.rowView.row.id,
          waypointNumber: leg.details.number,
        }
      },
    ),
  }
}

function buildLongestDurationLeaderboard(
  legs: VisitedLegRecord[],
  nowMs: number,
): HighlightLeaderboard {
  const meta = cardMetadata('longestDuration')
  const scored = legs
    .map((leg) => {
      const startMs = Date.parse(leg.details.from)
      const endMs = leg.details.to ? Date.parse(leg.details.to) : nowMs
      const durationMs =
        !Number.isNaN(startMs) && !Number.isNaN(endMs) && endMs > startMs ? endMs - startMs : 0
      return { leg, durationMs }
    })
    .filter((item) => item.durationMs > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) => right.durationMs - left.durationMs,
      ({ leg, durationMs }, rank) => ({
        rank,
        label: legLabel(leg),
        routeLabel: legRoute(leg),
        primaryValue: durationMs,
        formattedPrimary:
          leg.details.elapsed || formatElapsedDuration(leg.details.from, leg.details.to, nowMs),
        secondaryLabel: `${formatLy(leg.details.metrics.distanceLy, 1)} · ${formatInt(leg.details.metrics.jumps)} jumps`,
        modeName: leg.rowView.legModeName,
        modeColor: leg.rowView.legModeColor,
        inSystem: leg.details.inSystem,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }),
    ),
  }
}

function buildMostBodiesScannedLeaderboard(legs: VisitedLegRecord[]): HighlightLeaderboard {
  const meta = cardMetadata('mostBodiesScanned')
  const scored = legs.filter((leg) => leg.details.metrics.bodiesScanned > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) => right.details.metrics.bodiesScanned - left.details.metrics.bodiesScanned,
      (leg, rank) => ({
        rank,
        label: legLabel(leg),
        routeLabel: legRoute(leg),
        primaryValue: leg.details.metrics.bodiesScanned,
        formattedPrimary: `${formatInt(leg.details.metrics.bodiesScanned)} bodies`,
        secondaryLabel: `${formatInt(leg.details.metrics.bodiesMapped)} mapped · ${formatInt(leg.details.metrics.firstDiscoveries)} first disc.`,
        modeName: leg.rowView.legModeName,
        modeColor: leg.rowView.legModeColor,
        inSystem: leg.details.inSystem,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }),
    ),
  }
}

function buildMostOrganicSamplesLeaderboard(legs: VisitedLegRecord[]): HighlightLeaderboard {
  const meta = cardMetadata('mostOrganicSamples')
  const scored = legs.filter((leg) => leg.details.metrics.organicSamples > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) => right.details.metrics.organicSamples - left.details.metrics.organicSamples,
      (leg, rank) => ({
        rank,
        label: legLabel(leg),
        routeLabel: legRoute(leg),
        primaryValue: leg.details.metrics.organicSamples,
        formattedPrimary: `${formatInt(leg.details.metrics.organicSamples)} samples`,
        secondaryLabel: `${formatInt(leg.details.metrics.firstLogged)} first logged · ${formatCredits(leg.details.metrics.organicValueTotal)}`,
        modeName: leg.rowView.legModeName,
        modeColor: leg.rowView.legModeColor,
        inSystem: leg.details.inSystem,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }),
    ),
  }
}

function buildFurthestFromSolLeaderboard(exportData: ExpeditionExportData): HighlightLeaderboard {
  const meta = cardMetadata('furthestFromSol')
  const visitedWithCoords = (exportData.plan.rows ?? [])
    .filter((rowView) => rowView.row.rowType === 'waypoint' && Boolean(rowView.row.visitedAt) && rowView.row.hasCoords)
    .map((rowView) => ({
      rowView,
      distanceFromSolLy: Math.hypot(rowView.row.x, rowView.row.y, rowView.row.z),
    }))

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      visitedWithCoords,
      (left, right) => right.distanceFromSolLy - left.distanceFromSolLy,
      ({ rowView, distanceFromSolLy }, rank) => ({
        rank,
        label: `Waypoint #${rowView.number}`,
        routeLabel: rowView.row.systemName,
        primaryValue: distanceFromSolLy,
        formattedPrimary: formatLy(distanceFromSolLy, 1),
        secondaryLabel: rowView.detectedRegion || 'Unknown Region',
        modeName: rowView.legModeName || undefined,
        modeColor: rowView.legModeColor || undefined,
        rowId: rowView.row.id,
        waypointNumber: rowView.number,
      }),
    ),
  }
}

function buildCostliestMistakeLeaderboard(legs: VisitedLegRecord[]): HighlightLeaderboard {
  const meta = cardMetadata('costliestMistake')
  const scored = legs
    .map((leg) => ({
      leg,
      deathLoss:
        leg.details.realization.cartoLostDeath + leg.details.realization.organicLostDeath,
    }))
    .filter((item) => item.deathLoss > 0)

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: rankEntries(
      scored,
      (left, right) => right.deathLoss - left.deathLoss,
      ({ leg, deathLoss }, rank) => ({
        rank,
        label: legLabel(leg),
        routeLabel: legRoute(leg),
        primaryValue: deathLoss,
        formattedPrimary: `-${formatCredits(deathLoss)}`,
        secondaryLabel: `Carto -${formatCredits(leg.details.realization.cartoLostDeath)} · Exo -${formatCredits(leg.details.realization.organicLostDeath)}`,
        modeName: leg.rowView.legModeName,
        modeColor: leg.rowView.legModeColor,
        inSystem: leg.details.inSystem,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }),
    ),
  }
}

