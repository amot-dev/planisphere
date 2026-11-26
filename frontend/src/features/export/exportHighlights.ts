import { exporter } from '../../api/models'
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
 * Selects ordered leaderboards across requested Highlight categories from Go-populated
 * canonical and variant rankings, preserving canonical cardId values and slicing to maxDepth.
 */
export function computeAllHighlightLeaderboards(
  exportData: ExpeditionExportData,
  options: HighlightRankingOptions = {},
): HighlightLeaderboard[] {
  const requestedSet = options.cardIds ? new Set<HighlightCardId>(options.cardIds) : null
  if (requestedSet && requestedSet.size === 0) return []

  const lucrativeMetric = options.lucrativeMetric ?? 'estimated'
  const distanceMetric = options.distanceMetric ?? 'total'
  const maxDepth = options.maxDepth
  const fullOrderById = indexBoards(exportData.highlightsFullOrder)
  const variantsById = indexBoards(exportData.highlightsVariants)

  const leaderboards: HighlightLeaderboard[] = []
  for (const cardMeta of HIGHLIGHT_CARDS) {
    if (requestedSet && !requestedSet.has(cardMeta.id)) continue
    const sourceCardId = resolveSourceBoardId(cardMeta.id, lucrativeMetric, distanceMetric)
    const sourceBoard =
      sourceCardId === cardMeta.id
        ? fullOrderById?.get(cardMeta.id)
        : variantsById?.get(sourceCardId)
    const rawEntries = sourceBoard?.entries ?? []
    const entries =
      maxDepth != null ? (maxDepth <= 0 ? [] : rawEntries.slice(0, maxDepth)) : [...rawEntries]
    leaderboards.push({
      cardId: cardMeta.id,
      title: sourceBoard?.title || cardMeta.label,
      accent: cardMeta.accent,
      entries,
    })
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

function resolveSourceBoardId(
  cardId: HighlightCardId,
  lucrativeMetric: 'estimated' | 'realized',
  distanceMetric: 'total' | 'ship',
): string {
  if (cardId === 'mostLucrative' && lucrativeMetric === 'realized') return 'mostLucrativeRealized'
  if (cardId === 'longestDistance' && distanceMetric === 'ship') return 'longestShipDistance'
  if (cardId === 'shortestDistance' && distanceMetric === 'ship') return 'shortestShipDistance'
  return cardId
}

function indexBoards(
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
