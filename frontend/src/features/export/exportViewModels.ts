import { expedition } from '../../api/models'
import { formatCredits, formatInt, formatLoss, formatLy } from '../../domain/galacticTime'
import { DEFAULT_LEG_BORDER_COLOR, DEFAULT_MODE_COLOR, ORIGIN_LEG_BORDER_COLOR } from '../../domain/theme'
import { buildTelemetryHeaderViewModel, TelemetryHeaderViewModel } from './exportDetailsViewModel'
import { computeAllHighlightLeaderboards, HighlightLeaderboard, HighlightRankingEntry, selectHighlightCards } from './exportHighlights'
import {
  CareerDiffItem, formatEstimatedCarrierCredits, FormattedNetProfit,
  ResolvedTelemetryContext, resolveFormattedNetProfit, resolveTelemetryContext, sanitizeExportHexColor,
} from './exportMetrics'
import {
  CareerDiffDisplay, ExpeditionExportData, HighlightDepth,
  HighlightsBlockConfig, SummaryBlockConfig, WaypointsBlockConfig, WaypointsColumnId,
} from './exportTypes'

export * from './exportDetailsViewModel'

/** Single label-value item rendered inside the Summary route metadata strip. */
export interface SummaryRouteSegment { key: string; label: string; value: string }

/** Fixed-slot KPI tile view-model inside the Summary top or bottom row. */
export interface SummaryKpiTileViewModel {
  id: string
  label: string
  value: string
  subValue: string
  borderAccent: 'amber' | 'cyan' | 'emerald'
  valueTone: 'main' | 'amber' | 'cyan' | 'emerald'
  careerDiffText: string | null
}

/** Pure Summary block view-model shared by SummaryBlock.tsx and htmlGenerator.ts. */
export interface SummaryViewModel {
  header: TelemetryHeaderViewModel
  routeSegments: SummaryRouteSegment[]
  topRowTiles: SummaryKpiTileViewModel[]
  bottomRowTiles: SummaryKpiTileViewModel[]
  bottomRowCols: 3 | 4
  netProfit: FormattedNetProfit | null
}

/** Optional column header metadata for the Waypoints manifest table. */
export interface WaypointsColumnDescriptor {
  id: Exclude<WaypointsColumnId, 'regionHeaders'>
  headerLabel: string
}

/** Sanitized leg mode badge rendered in the Waypoints Mode column. */
export interface WaypointModeBadgeViewModel { name: string; safeColor: string }

/** Region divider row inside the Waypoints manifest table. */
export interface WaypointRegionRowViewModel { kind: 'region'; rowId: number; regionTitle: string }

/** Waypoint entry row inside the Waypoints manifest table. */
export interface WaypointEntryRowViewModel {
  kind: 'waypoint'
  rowId: number
  number: number
  systemName: string
  poiName: string
  borderColor: string
  timestampText: string
  legDistanceText: string
  durationText: string
  shipsText: string
  modeBadge: WaypointModeBadgeViewModel | null
  fcStatusText: string
  gecText: string
  notesText: string
}

/** Discriminated union of rows inside the Waypoints manifest table. */
export type WaypointTableRowViewModel = WaypointRegionRowViewModel | WaypointEntryRowViewModel

/** Pure Waypoints block view-model shared by WaypointsBlock.tsx and htmlGenerator.ts. */
export interface WaypointsViewModel {
  expeditionName: string
  scopeUpperLabel: 'FULL PLAN' | 'VISITED ONLY'
  scopeTitleLabel: 'Full Plan' | 'Visited Waypoints'
  visitedStatusLabel: string
  coveredDistanceLabel: string
  exportedAtGalactic: string
  selectedColumns: ReadonlySet<WaypointsColumnId>
  columnHeaders: string[]
  columnCount: number
  rows: WaypointTableRowViewModel[]
}

/** Ranked Highlight entry with sanitized CSS hex mode color. */
export interface HighlightEntryViewModel extends HighlightRankingEntry { modeColor?: string }

/** Highlight leaderboard card view-model with sanitized entry mode colors. */
export interface HighlightBoardViewModel extends Omit<HighlightLeaderboard, 'entries'> {
  entries: HighlightEntryViewModel[]
}

/** Pure Highlights block view-model shared by HighlightsBlock.tsx and htmlGenerator.ts. */
export interface HighlightsViewModel {
  eyebrow: string
  expeditionName: string
  trophyCountLabel: string
  effectiveStartGalactic: string
  exportedAtGalactic: string
  depth: HighlightDepth
  isEmpty: boolean
  emptyMessage: string
  gridCols: 1 | 2 | 3
  boards: HighlightBoardViewModel[]
}

const WAYPOINT_OPTIONAL_COLUMNS: ReadonlyArray<WaypointsColumnDescriptor> = [
  { id: 'timestamps', headerLabel: 'Galactic Time' },
  { id: 'legDistance', headerLabel: 'Leg Ly' },
  { id: 'duration', headerLabel: 'Duration' },
  { id: 'shipsFlown', headerLabel: 'Ships' },
  { id: 'flightMode', headerLabel: 'Mode' },
  { id: 'fcStops', headerLabel: 'FC' },
  { id: 'gecBadges', headerLabel: 'GEC' },
  { id: 'notes', headerLabel: 'Notes' },
]

const EMPTY_HIGHLIGHTS_MESSAGE = 'Select at least one highlight card with recorded expedition data'

/** Resolves the pure Summary block view-model for both React PNG cards and HTML reports. */
export function buildSummaryViewModel(
  exportData: ExpeditionExportData,
  config: SummaryBlockConfig,
  nowMs?: number,
): SummaryViewModel {
  const context = resolveTelemetryContext(exportData, config.scope, config.selectedModeId, nowMs)
  const diffById = new Map<CareerDiffItem['id'], CareerDiffItem>()
  if (config.careerDiff !== 'hide' && context.careerDiff) {
    for (const diffItem of context.careerDiff) diffById.set(diffItem.id, diffItem)
  }
  const showCarrierTile =
    context.carrierCost != null && (context.metrics.carrierJumps > 0 || context.carrierCost.totalCost > 0)

  return {
    header: buildTelemetryHeaderViewModel('SUMMARY', 'amber', context, exportData.exportedAtGalactic),
    routeSegments: buildSummaryRouteSegments(exportData.commanders, context),
    topRowTiles: buildSummaryTopRowTiles(context, diffById, config.careerDiff),
    bottomRowTiles: buildSummaryBottomRowTiles(context, diffById, config.careerDiff, showCarrierTile),
    bottomRowCols: showCarrierTile ? 4 : 3,
    netProfit: resolveFormattedNetProfit(context),
  }
}

function summaryTile(
  id: string,
  label: string,
  value: string,
  subValue: string,
  borderAccent: SummaryKpiTileViewModel['borderAccent'],
  valueTone: SummaryKpiTileViewModel['valueTone'],
  careerDiffText: string | null = null,
): SummaryKpiTileViewModel {
  return { id, label, value, subValue, borderAccent, valueTone, careerDiffText }
}

function buildSummaryRouteSegments(commanders: string[], context: ResolvedTelemetryContext): SummaryRouteSegment[] {
  const segments: SummaryRouteSegment[] = []
  if (commanders.length > 0) segments.push({ key: 'cmdr', label: 'CMDR', value: commanders.join(', ') })
  if (context.visitedWaypoints != null && context.plannedWaypoints != null) {
    segments.push({ key: 'wp', label: 'WAYPOINTS', value: `${context.visitedWaypoints} / ${context.plannedWaypoints}` })
  }
  if (context.distinctRegionCount != null) {
    segments.push({ key: 'reg', label: 'REGIONS', value: String(context.distinctRegionCount) })
  }
  if (context.shipsFlown.length > 0) segments.push({ key: 'ships', label: 'SHIPS', value: context.shipsFlown.join(', ') })
  return segments
}

function buildSummaryTopRowTiles(
  context: ResolvedTelemetryContext,
  diffById: ReadonlyMap<CareerDiffItem['id'], CareerDiffItem>,
  careerDiffMode: CareerDiffDisplay,
): SummaryKpiTileViewModel[] {
  const { metrics, realization, derived } = context
  const avgJumpSub = derived.avgJumpRangeLy != null ? `${formatLy(derived.avgJumpRangeLy, 1)} avg` : 'No piloted jumps'
  const realizedSub =
    realization.pendingValue > 0
      ? `Pending ${formatCredits(realization.pendingValue)}`
      : `Losses ${formatLoss(derived.totalLossesCredits)}`

  return [
    summaryTile(
      'distanceLy', 'Distance Travelled', formatLy(metrics.distanceLy, 1),
      `Ship ${formatLy(derived.shipDistanceLy, 1)} · Carrier ${formatLy(metrics.carrierPassengerLy, 1)}`,
      'amber', 'amber', formatInlineCareerDiff(diffById.get('distanceLy'), careerDiffMode, (value) => formatLy(value, 1)),
    ),
    summaryTile(
      'jumps', 'Hyperspace Jumps', formatInt(metrics.jumps), avgJumpSub,
      'amber', 'main', formatInlineCareerDiff(diffById.get('jumps'), careerDiffMode, formatInt),
    ),
    summaryTile(
      'estimatedValue', 'Estimated Exploration', formatCredits(derived.combinedEstimatedCredits),
      `Carto ${formatCredits(metrics.scanValueTotal)} · Exo ${formatCredits(metrics.organicValueTotal)}`,
      'cyan', 'cyan', formatInlineCareerDiff(diffById.get('estimatedValue'), careerDiffMode, formatCredits),
    ),
    summaryTile(
      'realizedValue', 'Realized Net Value', formatCredits(realization.totalRealized), realizedSub,
      'emerald', 'amber', formatInlineCareerDiff(diffById.get('realizedValue'), careerDiffMode, formatCredits),
    ),
  ]
}

function buildSummaryBottomRowTiles(
  context: ResolvedTelemetryContext,
  diffById: ReadonlyMap<CareerDiffItem['id'], CareerDiffItem>,
  careerDiffMode: CareerDiffDisplay,
  showCarrierTile: boolean,
): SummaryKpiTileViewModel[] {
  const { metrics, carrierCost } = context
  const tiles: SummaryKpiTileViewModel[] = [
    summaryTile(
      'bodiesScanned', 'Bodies Scanned', formatInt(metrics.bodiesScanned),
      `${formatInt(metrics.firstDiscoveries)} first discovered`,
      'cyan', 'cyan', formatInlineCareerDiff(diffById.get('bodiesScanned'), careerDiffMode, formatInt),
    ),
    summaryTile(
      'bodiesMapped', 'Bodies Mapped', formatInt(metrics.bodiesMapped),
      `${formatInt(metrics.firstMapped)} first mapped`,
      'cyan', 'cyan', formatInlineCareerDiff(diffById.get('bodiesMapped'), careerDiffMode, formatInt),
    ),
    summaryTile(
      'organicSamples', 'Organic Samples', formatInt(metrics.organicSamples),
      `${formatInt(metrics.firstLogged)} first logged`,
      'emerald', 'emerald', formatInlineCareerDiff(diffById.get('organicSamples'), careerDiffMode, formatInt),
    ),
  ]
  if (showCarrierTile && carrierCost) {
    tiles.push(
      summaryTile(
        'carrier', 'Carrier',
        formatEstimatedCarrierCredits(carrierCost.totalCost, Boolean(carrierCost.totalEstimated)),
        `${formatInt(metrics.carrierJumps)} jumps · ${formatLy(metrics.carrierDistanceLy, 1)} · ${formatInt(metrics.tritiumUsed ?? 0)} t`,
        'amber', 'amber',
      ),
    )
  }
  return tiles
}

function formatInlineCareerDiff(
  diffItem: CareerDiffItem | undefined,
  displayMode: CareerDiffDisplay,
  formatValue: (value: number) => string,
): string | null {
  if (!diffItem || displayMode === 'hide') return null
  const parts: string[] = []
  if (displayMode === 'absolute' || displayMode === 'both') parts.push(`PRE ${formatValue(diffItem.baseline)}`)
  if (displayMode === 'percentage' || displayMode === 'both') {
    const growth = diffItem.isNewBaseline
      ? 'NEW'
      : diffItem.growthPercent != null
        ? `+${diffItem.growthPercent.toFixed(1)}%`
        : '—'
    parts.push(growth)
  }
  return parts.join(' · ')
}

/** Resolves the pure Waypoints table view-model for both React PNG cards and HTML reports. */
export function buildWaypointsViewModel(
  exportData: ExpeditionExportData,
  config: WaypointsBlockConfig,
): WaypointsViewModel {
  const selectedColumns = new Set<WaypointsColumnId>(config.selectedColumns)
  const detailsByRowId = new Map<number, expedition.WaypointDetails>()
  for (const detail of exportData.rowDetails ?? []) detailsByRowId.set(detail.rowId, detail)

  const columnHeaders = [
    '#', 'System',
    ...WAYPOINT_OPTIONAL_COLUMNS.filter((column) => selectedColumns.has(column.id)).map((column) => column.headerLabel),
  ]
  const rows: WaypointTableRowViewModel[] = []
  for (const rowView of exportData.plan.rows ?? []) {
    if (rowView.row.rowType === 'region') {
      if (selectedColumns.has('regionHeaders')) {
        rows.push({ kind: 'region', rowId: rowView.row.id, regionTitle: rowView.regionName || 'Galactic Region' })
      }
      continue
    }
    if (config.waypointsScope === 'visited' && !rowView.row.visitedAt) continue
    rows.push(buildWaypointEntryRow(rowView, detailsByRowId.get(rowView.row.id)))
  }

  return {
    expeditionName: exportData.plan.expedition.name,
    scopeUpperLabel: config.waypointsScope === 'fullPlan' ? 'FULL PLAN' : 'VISITED ONLY',
    scopeTitleLabel: config.waypointsScope === 'fullPlan' ? 'Full Plan' : 'Visited Waypoints',
    visitedStatusLabel: `${exportData.plan.visitedWaypointCount} / ${exportData.plan.waypointCount} VISITED`,
    coveredDistanceLabel: `COVERED ${formatLy(exportData.plan.coveredLy, 1)}`,
    exportedAtGalactic: exportData.exportedAtGalactic,
    selectedColumns,
    columnHeaders,
    columnCount: columnHeaders.length,
    rows,
  }
}

function buildWaypointEntryRow(
  rowView: expedition.RowView,
  detail: expedition.WaypointDetails | undefined,
): WaypointEntryRowViewModel {
  const borderColor =
    rowView.number === 1 ? ORIGIN_LEG_BORDER_COLOR : sanitizeExportHexColor(rowView.legModeColor, DEFAULT_LEG_BORDER_COLOR)
  const modeBadge =
    rowView.number > 1 && rowView.legModeName
      ? { name: rowView.legModeName, safeColor: sanitizeExportHexColor(rowView.legModeColor, DEFAULT_MODE_COLOR) }
      : null

  return {
    kind: 'waypoint',
    rowId: rowView.row.id,
    number: rowView.number,
    systemName: rowView.row.systemName,
    poiName: rowView.row.poiName || '',
    borderColor,
    timestampText: formatWaypointTimestampCell(rowView, detail),
    legDistanceText: rowView.distFromPrevLy != null ? formatLy(rowView.distFromPrevLy, 1) : '—',
    durationText: detail?.elapsed || '—',
    shipsText: (detail?.shipsUsed ?? []).join(', ') || '—',
    modeBadge,
    fcStatusText: rowView.row.fcStop ? (rowView.row.fcArrived ? 'ARRIVED' : 'STOP') : '—',
    gecText: rowView.gec ? rowView.gec.name : '—',
    notesText: rowView.row.notes || '',
  }
}

function formatWaypointTimestampCell(rowView: expedition.RowView, detail: expedition.WaypointDetails | undefined): string {
  if (rowView.number === 1) {
    return rowView.departedAtGalactic ? `DEPARTED ${rowView.departedAtGalactic}` : 'AWAITING DEPARTURE'
  }
  if (rowView.row.visitedAt) {
    return detail?.inSystem ? `${rowView.visitedAtGalactic} (IN SYSTEM)` : rowView.visitedAtGalactic
  }
  return rowView.visitState === 'current' ? 'IN PROGRESS' : ''
}

/** Resolves the pure Highlights trophy grid view-model for both React PNG cards and HTML reports. */
export function buildHighlightsViewModel(
  exportData: ExpeditionExportData,
  config: HighlightsBlockConfig,
  nowMs?: number,
): HighlightsViewModel {
  const allLeaderboards = computeAllHighlightLeaderboards(exportData, {
    lucrativeMetric: config.lucrativeMetric,
    distanceMetric: config.distanceMetric,
    cardIds: config.selectedCards,
    maxDepth: config.depth,
    nowMs,
  })
  const visibleBoards = selectHighlightCards(allLeaderboards, config.selectedCards, config.depth)
  const boards: HighlightBoardViewModel[] = visibleBoards.map((board) => ({
    ...board,
    entries: board.entries.map((entry) => ({
      ...entry,
      modeColor: entry.modeColor ? sanitizeExportHexColor(entry.modeColor) : undefined,
    })),
  }))
  const boardCount = boards.length
  const gridCols: 1 | 2 | 3 = boardCount <= 1 ? 1 : boardCount === 2 || boardCount === 4 ? 2 : 3

  return {
    eyebrow: `EXPEDITION HIGHLIGHTS // TOP ${config.depth}`,
    expeditionName: exportData.plan.expedition.name,
    trophyCountLabel: `${boards.length} TROPHY CARDS`,
    effectiveStartGalactic: exportData.effectiveStartGalactic,
    exportedAtGalactic: exportData.exportedAtGalactic,
    depth: config.depth,
    isEmpty: boards.length === 0,
    emptyMessage: EMPTY_HIGHLIGHTS_MESSAGE,
    gridCols,
    boards,
  }
}
