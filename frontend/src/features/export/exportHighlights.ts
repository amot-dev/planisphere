import { exporter } from '../../api/models'
import {
  formatCredits,
  formatElapsedDuration,
  formatInt,
  formatLy,
} from '../../domain/galacticTime'
import { getVisitedLegs, VisitedLegRecord } from './exportDataIndex'
import {
  ExpeditionExportData,
  HIGHLIGHT_CARDS,
  HighlightCardId,
  HighlightDepth,
  PlainWailsDto,
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

/** Options controlling which valuation, distance, card selection, or depth drive Highlight rankings. */
export interface HighlightRankingOptions {
  lucrativeMetric?: 'estimated' | 'realized'
  distanceMetric?: 'total' | 'ship'
  nowMs?: number
  cardIds?: readonly HighlightCardId[]
  maxDepth?: number
}

type RawPrecomputedBoard = PlainWailsDto<exporter.HighlightLeaderboard>
const precomputedBoardsCache = new WeakMap<
  ReadonlyArray<RawPrecomputedBoard>,
  Map<string, RawPrecomputedBoard>
>()

/**
 * Computes ordered leaderboards across requested Highlight categories, reusing Go-populated
 * canonical rankings when metric toggles use defaults and slicing before formatting in TS.
 */
export function computeAllHighlightLeaderboards(
  exportData: ExpeditionExportData,
  options: HighlightRankingOptions = {},
): HighlightLeaderboard[] {
  const requestedSet = options.cardIds ? new Set<HighlightCardId>(options.cardIds) : null
  if (requestedSet && requestedSet.size === 0) return []

  const nowMs = options.nowMs ?? Date.now()
  const lucrativeMetric = options.lucrativeMetric ?? 'estimated'
  const distanceMetric = options.distanceMetric ?? 'total'
  const maxDepth = options.maxDepth
  const precomputedByCardId = indexPrecomputedBoards(exportData.highlightsFullOrder)

  let visitedLegs: VisitedLegRecord[] | null = null
  const resolveVisitedLegs = (): VisitedLegRecord[] => {
    visitedLegs ??= getVisitedLegs(exportData)
    return visitedLegs
  }

  const leaderboards: HighlightLeaderboard[] = []
  for (const cardMeta of HIGHLIGHT_CARDS) {
    if (requestedSet && !requestedSet.has(cardMeta.id)) continue
    const reusableBoard = resolvePrecomputedBoard(
      precomputedByCardId,
      cardMeta,
      lucrativeMetric,
      distanceMetric,
      maxDepth,
    )
    leaderboards.push(
      reusableBoard ??
        computeLeaderboardForCard(
          cardMeta.id,
          exportData,
          resolveVisitedLegs,
          lucrativeMetric,
          distanceMetric,
          nowMs,
          maxDepth,
        ),
    )
  }
  return leaderboards
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
    .map((board) => ({ ...board, entries: board.entries.slice(0, depth) }))
}

/** Serializes the canonical, unfiltered JSON snapshot including full ordered Highlight rankings. */
export function buildCanonicalJsonSnapshot(exportData: ExpeditionExportData): string {
  return JSON.stringify(
    { ...exportData, highlightsFullOrder: computeAllHighlightLeaderboards(exportData) },
    null,
    2,
  )
}

function indexPrecomputedBoards(
  boards: ReadonlyArray<RawPrecomputedBoard> | undefined,
): Map<string, RawPrecomputedBoard> | null {
  if (!boards || boards.length === 0) return null
  const cached = precomputedBoardsCache.get(boards)
  if (cached) return cached
  const byCardId = new Map<string, RawPrecomputedBoard>()
  for (const board of boards) {
    byCardId.set(board.cardId, board)
  }
  precomputedBoardsCache.set(boards, byCardId)
  return byCardId
}

function resolvePrecomputedBoard(
  precomputedByCardId: Map<string, RawPrecomputedBoard> | null,
  cardMeta: (typeof HIGHLIGHT_CARDS)[number],
  lucrativeMetric: 'estimated' | 'realized',
  distanceMetric: 'total' | 'ship',
  maxDepth: number | undefined,
): HighlightLeaderboard | null {
  if (!precomputedByCardId) return null
  if (cardMeta.id === 'mostLucrative' && lucrativeMetric !== 'estimated') return null
  if ((cardMeta.id === 'longestDistance' || cardMeta.id === 'shortestDistance') && distanceMetric !== 'total') {
    return null
  }
  const precomputed = precomputedByCardId.get(cardMeta.id)
  if (!precomputed) return null
  const entries =
    maxDepth != null ? (maxDepth <= 0 ? [] : precomputed.entries.slice(0, maxDepth)) : [...precomputed.entries]
  return { cardId: cardMeta.id, title: precomputed.title || cardMeta.label, accent: cardMeta.accent, entries }
}

function computeLeaderboardForCard(
  cardId: HighlightCardId,
  exportData: ExpeditionExportData,
  resolveVisitedLegs: () => VisitedLegRecord[],
  lucrativeMetric: 'estimated' | 'realized',
  distanceMetric: 'total' | 'ship',
  nowMs: number,
  maxDepth: number | undefined,
): HighlightLeaderboard {
  switch (cardId) {
    case 'mostLucrative':
      return buildMostLucrativeLeaderboard(resolveVisitedLegs(), lucrativeMetric, maxDepth)
    case 'longestDistance':
      return buildDistanceLeaderboard(resolveVisitedLegs(), distanceMetric, false, maxDepth)
    case 'mostJumps':
      return buildMostJumpsLeaderboard(resolveVisitedLegs(), maxDepth)
    case 'longestDuration':
      return buildLongestDurationLeaderboard(resolveVisitedLegs(), nowMs, maxDepth)
    case 'shortestDistance':
      return buildDistanceLeaderboard(resolveVisitedLegs(), distanceMetric, true, maxDepth)
    case 'mostBodiesScanned':
      return buildMostBodiesScannedLeaderboard(resolveVisitedLegs(), maxDepth)
    case 'mostOrganicSamples':
      return buildMostOrganicSamplesLeaderboard(resolveVisitedLegs(), maxDepth)
    case 'furthestFromSol':
      return buildFurthestFromSolLeaderboard(exportData, maxDepth)
    case 'costliestMistake':
      return buildCostliestMistakeLeaderboard(resolveVisitedLegs(), maxDepth)
  }
}

function rankLegLeaderboard(
  cardId: HighlightCardId,
  legs: VisitedLegRecord[],
  ascending: boolean,
  maxDepth: number | undefined,
  scoreFn: (leg: VisitedLegRecord) => number,
  formatFn: (leg: VisitedLegRecord, score: number) => { formattedPrimary: string; secondaryLabel: string },
): HighlightLeaderboard {
  const meta = HIGHLIGHT_CARDS.find((card) => card.id === cardId)!
  if (maxDepth != null && maxDepth <= 0) {
    return { cardId: meta.id, title: meta.label, accent: meta.accent, entries: [] }
  }
  const scored: Array<{ leg: VisitedLegRecord; score: number }> = []
  for (const leg of legs) {
    const score = scoreFn(leg)
    if (score > 0) scored.push({ leg, score })
  }
  scored.sort((left, right) => (ascending ? left.score - right.score : right.score - left.score))
  const capped = maxDepth != null ? scored.slice(0, maxDepth) : scored
  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: capped.map(({ leg, score }, index) => {
      const { formattedPrimary, secondaryLabel } = formatFn(leg, score)
      return {
        rank: index + 1,
        label: `Leg #${leg.details.number - 1} -> #${leg.details.number}`,
        routeLabel: `${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`,
        primaryValue: score,
        formattedPrimary,
        secondaryLabel,
        modeName: leg.rowView.legModeName || undefined,
        modeColor: leg.rowView.legModeColor || undefined,
        inSystem: leg.details.inSystem || undefined,
        rowId: leg.rowView.row.id,
        waypointNumber: leg.details.number,
      }
    }),
  }
}

function buildMostLucrativeLeaderboard(
  legs: VisitedLegRecord[],
  lucrativeMetric: 'estimated' | 'realized',
  maxDepth: number | undefined,
): HighlightLeaderboard {
  return rankLegLeaderboard(
    'mostLucrative',
    legs,
    false,
    maxDepth,
    (leg) =>
      lucrativeMetric === 'realized'
        ? leg.details.realization.totalRealized
        : leg.details.metrics.scanValueTotal + leg.details.metrics.organicValueTotal,
    (leg, value) => ({
      formattedPrimary: formatCredits(value),
      secondaryLabel: `Carto ${formatCredits(leg.details.metrics.scanValueTotal)} · Exo ${formatCredits(leg.details.metrics.organicValueTotal)}`,
    }),
  )
}

function buildDistanceLeaderboard(
  legs: VisitedLegRecord[],
  distanceMetric: 'total' | 'ship',
  shortest: boolean,
  maxDepth: number | undefined,
): HighlightLeaderboard {
  return rankLegLeaderboard(
    shortest ? 'shortestDistance' : 'longestDistance',
    legs,
    shortest,
    maxDepth,
    (leg) =>
      distanceMetric === 'ship'
        ? (leg.details.metrics.shipDistanceLy ?? 0)
        : leg.details.metrics.distanceLy,
    (leg, distanceLy) => ({
      formattedPrimary: formatLy(distanceLy, 1),
      secondaryLabel: `${formatInt(leg.details.metrics.jumps)} jumps${leg.details.elapsed ? ` · ${leg.details.elapsed}` : ''}`,
    }),
  )
}

function buildMostJumpsLeaderboard(legs: VisitedLegRecord[], maxDepth: number | undefined): HighlightLeaderboard {
  return rankLegLeaderboard(
    'mostJumps',
    legs,
    false,
    maxDepth,
    (leg) => leg.details.metrics.jumps,
    (leg, jumps) => {
      const shipLy = leg.details.metrics.shipDistanceLy ?? 0
      return {
        formattedPrimary: `${formatInt(jumps)} jumps`,
        secondaryLabel: `${formatLy(shipLy, 1)} · ${formatLy(shipLy / jumps, 1)} avg`,
      }
    },
  )
}

function buildLongestDurationLeaderboard(
  legs: VisitedLegRecord[],
  nowMs: number,
  maxDepth: number | undefined,
): HighlightLeaderboard {
  return rankLegLeaderboard(
    'longestDuration',
    legs,
    false,
    maxDepth,
    (leg) => {
      const startMs = Date.parse(leg.details.from)
      const endMs = leg.details.to ? Date.parse(leg.details.to) : nowMs
      return !Number.isNaN(startMs) && !Number.isNaN(endMs) && endMs > startMs ? endMs - startMs : 0
    },
    (leg) => ({
      formattedPrimary: leg.details.elapsed || formatElapsedDuration(leg.details.from, leg.details.to, nowMs),
      secondaryLabel: `${formatLy(leg.details.metrics.distanceLy, 1)} · ${formatInt(leg.details.metrics.jumps)} jumps`,
    }),
  )
}

function buildMostBodiesScannedLeaderboard(
  legs: VisitedLegRecord[],
  maxDepth: number | undefined,
): HighlightLeaderboard {
  return rankLegLeaderboard(
    'mostBodiesScanned',
    legs,
    false,
    maxDepth,
    (leg) => leg.details.metrics.bodiesScanned,
    (leg, bodies) => ({
      formattedPrimary: `${formatInt(bodies)} bodies`,
      secondaryLabel: `${formatInt(leg.details.metrics.bodiesMapped)} mapped · ${formatInt(leg.details.metrics.firstDiscoveries)} first disc.`,
    }),
  )
}

function buildMostOrganicSamplesLeaderboard(
  legs: VisitedLegRecord[],
  maxDepth: number | undefined,
): HighlightLeaderboard {
  return rankLegLeaderboard(
    'mostOrganicSamples',
    legs,
    false,
    maxDepth,
    (leg) => leg.details.metrics.organicSamples,
    (leg, samples) => ({
      formattedPrimary: `${formatInt(samples)} samples`,
      secondaryLabel: `${formatInt(leg.details.metrics.firstLogged)} first logged · ${formatCredits(leg.details.metrics.organicValueTotal)}`,
    }),
  )
}

function buildFurthestFromSolLeaderboard(
  exportData: ExpeditionExportData,
  maxDepth: number | undefined,
): HighlightLeaderboard {
  const meta = HIGHLIGHT_CARDS.find((card) => card.id === 'furthestFromSol')!
  if (maxDepth != null && maxDepth <= 0) {
    return { cardId: meta.id, title: meta.label, accent: meta.accent, entries: [] }
  }
  const scored = (exportData.plan.rows ?? [])
    .filter((rowView) => rowView.row.rowType === 'waypoint' && Boolean(rowView.row.visitedAt) && rowView.row.hasCoords)
    .map((rowView) => ({ rowView, distanceFromSolLy: Math.hypot(rowView.row.x, rowView.row.y, rowView.row.z) }))
    .sort((left, right) => right.distanceFromSolLy - left.distanceFromSolLy)
  const capped = maxDepth != null ? scored.slice(0, maxDepth) : scored

  return {
    cardId: meta.id,
    title: meta.label,
    accent: meta.accent,
    entries: capped.map(({ rowView, distanceFromSolLy }, index) => ({
      rank: index + 1,
      label: `Waypoint #${rowView.number}`,
      routeLabel: rowView.row.systemName,
      primaryValue: distanceFromSolLy,
      formattedPrimary: formatLy(distanceFromSolLy, 1),
      secondaryLabel: rowView.detectedRegion || 'Unknown Region',
      modeName: rowView.legModeName || undefined,
      modeColor: rowView.legModeColor || undefined,
      rowId: rowView.row.id,
      waypointNumber: rowView.number,
    })),
  }
}

function buildCostliestMistakeLeaderboard(
  legs: VisitedLegRecord[],
  maxDepth: number | undefined,
): HighlightLeaderboard {
  return rankLegLeaderboard(
    'costliestMistake',
    legs,
    false,
    maxDepth,
    (leg) => leg.details.realization.cartoLostDeath + leg.details.realization.organicLostDeath,
    (leg, deathLoss) => ({
      formattedPrimary: `-${formatCredits(deathLoss)}`,
      secondaryLabel: `Carto -${formatCredits(leg.details.realization.cartoLostDeath)} · Exo -${formatCredits(leg.details.realization.organicLostDeath)}`,
    }),
  )
}
