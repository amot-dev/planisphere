import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { app, expedition, realization, stats } from '../api/models'
import {
  buildPlannerTableItems,
  computePlannerTableDimensions,
  gapToTargetIndex,
} from '../domain/planTableItems'
import { WaypointDetailsDrawer } from '../features/planner/drawer/WaypointDetailsDrawer'
import { ExpeditionPlanner } from '../features/planner/ExpeditionPlanner'
import { PlannerRouteTable } from '../features/planner/table/PlannerRouteTable'
import { WaypointRowItem, type WaypointRowHandlers } from '../features/planner/table/WaypointRowItem'
import { ExplorationValuationSection } from '../features/sidebar/ExplorationValuationSection'
import { NavigationTelemetrySection } from '../features/sidebar/NavigationTelemetrySection'
import { StatsSidebar } from '../features/sidebar/StatsSidebar'
import {
  makeRenderSpan,
  msToNs,
  type HydratedFrontendFixture,
  type LiveFrontendFixture,
  type SpanNodeJSON,
} from './profileHelpers'

const noop = () => {}
const emptyDropSlotSet: ReadonlySet<number> = new Set<number>()

/**
 * executeHydrationPass benchmarks Wails TypeScript model hydration from raw JSON.
 */
export function executeHydrationPass(parsedFixture: LiveFrontendFixture): {
  summary: string
  hashInput: string
  children: SpanNodeJSON[]
} {
  const children: SpanNodeJSON[] = []

  const t0 = performance.now()
  const hydratedPlan = expedition.PlanView.createFrom(parsedFixture.plan)
  children.push(
    makeRenderSpan(
      'expedition.PlanView.createFrom',
      'frontend/wailsjs/go/models.ts',
      1,
      hydratedPlan.rows.length,
      'rows',
      msToNs(performance.now() - t0),
    ),
  )

  const t1 = performance.now()
  const hydratedSidebar = app.SidebarStats.createFrom(parsedFixture.sidebarStats)
  const hydratedModesState = parsedFixture.modesState
    ? app.ModesState.createFrom(parsedFixture.modesState)
    : app.ModesState.createFrom({ modes: parsedFixture.modes ?? [] })
  children.push(
    makeRenderSpan(
      'app.SidebarStats + ModesState.createFrom',
      'frontend/wailsjs/go/models.ts',
      2,
      1 + (hydratedModesState.modes?.length ?? 0),
      'models',
      msToNs(performance.now() - t1),
    ),
  )

  const rawDetails = Array.isArray(parsedFixture.visitedDetails) ? parsedFixture.visitedDetails : []
  const rawPreviews = Array.isArray(parsedFixture.dropPreviews) ? parsedFixture.dropPreviews : []
  const t2 = performance.now()
  const hydratedDetails = rawDetails.map((item) => expedition.WaypointDetails.createFrom(item))
  const hydratedPreviews = rawPreviews.map((item) => expedition.DropSlotPreview.createFrom(item))
  children.push(
    makeRenderSpan(
      'WaypointDetails + DropSlotPreview.createFrom[Batch]',
      'frontend/wailsjs/go/models.ts',
      hydratedDetails.length + hydratedPreviews.length,
      hydratedDetails.length + hydratedPreviews.length,
      'models',
      msToNs(performance.now() - t2),
    ),
  )

  return {
    summary: `PlanRows=${hydratedPlan.rows.length} Drawers=${hydratedDetails.length} DropSlots=${hydratedPreviews.length} ExpeditionCarto=${hydratedSidebar.expedition?.scanValueTotal ?? 0}`,
    hashInput: `${hydratedPlan.rows.length}:${hydratedDetails.length}:${hydratedPreviews.length}:${hydratedSidebar.expedition?.scanValueTotal ?? 0}:${hydratedSidebar.allJournals?.scanValueTotal ?? 0}`,
    children,
  }
}

function renderDragPreviewTableHTML(hydrated: HydratedFrontendFixture): string {
  const { plan, heldRowIndex, dropPreviews, lastVisitedIndex } = hydrated
  if (heldRowIndex < 0 || dropPreviews.length === 0) {
    return ''
  }
  const isPlanning = plan.expedition.status === 'planning'
  const isCompleted = plan.expedition.status === 'completed'
  const { visibleColumnCount, tableMinWidthPixels } = computePlannerTableDimensions(
    isPlanning,
    true,
    true,
  )
  const dragTableItems = buildPlannerTableItems(
    plan,
    {},
    isPlanning,
    heldRowIndex,
    lastVisitedIndex,
    dropPreviews,
  )
  const slotGapSet = new Set<number>()
  for (let gapIndex = 1; gapIndex <= plan.rows.length; gapIndex++) {
    const targetIndex = gapToTargetIndex(heldRowIndex, gapIndex)
    const preview = targetIndex !== null ? dropPreviews[targetIndex] : undefined
    if (preview && (preview.status === 'valid_visited' || preview.status === 'blocked_fc')) {
      slotGapSet.add(gapIndex)
    }
  }

  return renderToString(
    createElement(PlannerRouteTable, {
      plan,
      isPlanning,
      isCompleted,
      columns: {
        visibleColumnCount,
        tableMinWidthPixels,
        showNotes: true,
        showCarrierStop: true,
        onToggleShowNotes: noop,
        onToggleShowCarrierStop: noop,
      },
      externalLinks: {},
      drawerState: {
        expandedRowIds: {},
        detailsById: {},
        loadingDetails: {},
        toggleExpand: noop,
      },
      dragState: {
        tableItems: dragTableItems,
        dropSlotGaps: slotGapSet.size > 0 ? slotGapSet : emptyDropSlotSet,
        heldOrDraggedIndex: heldRowIndex,
        draggedIndex: heldRowIndex,
        dragOverGapIndex: heldRowIndex + 1,
        getRowDropProps: () => ({}),
        getSlotDropProps: () => ({ onDragOver: noop, onDrop: noop }),
        onClearDragOverGap: noop,
        onRowHoldStart: noop,
        onRowHoldEnd: noop,
        onRowDragStart: noop,
        onRowDragEnd: noop,
      },
      rowActions: {
        copiedSystemRowId: null,
        insertMenuAfterId: null,
        onSetInsertMenuAfterId: noop,
        onChangeRegion: noop,
        onInsert: noop,
        onDeleteRow: noop,
        onLegModeChange: noop,
        onFieldBlur: noop,
        onCopySystem: noop,
        onToggleFcStop: noop,
        onOverrideFcArrived: noop,
        onOpenCarrierRoutePrompt: noop,
      },
    }),
  )
}

function renderSidebarBothScopesHTML(hydrated: HydratedFrontendFixture): string {
  const { sidebar, modesState } = hydrated
  const emptyFilter = stats.Filter.createFrom({ enabled: false, modes: [] })
  const expeditionHTML = renderToString(
    createElement(StatsSidebar, {
      sidebarStats: sidebar,
      modesState,
      filter: emptyFilter,
      collapsed: false,
      carrierRouteOpen: false,
      onFilterChange: noop,
    }),
  )
  const allTimeHTML = renderToString(
    createElement(
      'div',
      null,
      createElement(NavigationTelemetrySection, {
        viewScope: 'allTime',
        sidebarStats: sidebar,
        displayedMetrics: sidebar.allJournals ?? new stats.Metrics(),
        carrierRouteOpen: false,
        flightOpsExpanded: true,
        carrierOpsExpanded: true,
        carrierCostExpanded: true,
        onToggleFlightOpsExpanded: noop,
        onToggleCarrierOpsExpanded: noop,
        onToggleCarrierCostExpanded: noop,
      }),
      createElement(ExplorationValuationSection, {
        displayedMetrics: sidebar.allJournals ?? new stats.Metrics(),
        displayedRealization: sidebar.allJournalsRealization ?? new realization.Breakdown(),
        scienceOpsExpanded: true,
        scanExpanded: true,
        organicExpanded: true,
        realizedExpanded: true,
        onToggleScienceOpsExpanded: noop,
        onToggleScanExpanded: noop,
        onToggleOrganicExpanded: noop,
        onToggleRealizedExpanded: noop,
      }),
    ),
  )
  return expeditionHTML + allTimeHTML
}

function renderWaypointRowsBatchHTML(hydrated: HydratedFrontendFixture): {
  htmlLength: number
  waypointCount: number
} {
  const { plan, lastVisitedIndex } = hydrated
  const isPlanning = plan.expedition.status === 'planning'
  const isCompleted = plan.expedition.status === 'completed'
  const probeExternalLinks = {
    inara: 'always',
    edsm: 'if_no_gec',
    gec: 'always',
  }
  const probeHandlers: WaypointRowHandlers = {
    onToggleExpand: noop,
    onFieldBlur: noop,
    onCopySystem: noop,
    onToggleFcStop: noop,
    onOverrideFcArrived: noop,
    onOpenCarrierRoutePrompt: noop,
    onToggleInsertMenu: noop,
    onInsert: noop,
    onDelete: noop,
    onDragStart: noop,
    onDragEnd: noop,
  }
  let htmlLength = 0
  let waypointCount = 0

  plan.rows.forEach((rowView, rowIndex) => {
    if (rowView.row.rowType !== 'waypoint') return
    waypointCount++
    const html = renderToString(
      createElement(
        'table',
        null,
        createElement(
          'tbody',
          null,
          createElement(WaypointRowItem, {
            rowView,
            index: rowIndex,
            isPlanning,
            isLocked: isCompleted,
            isBeingDragged: false,
            isExpanded: false,
            isNextWaypoint: Boolean(
              rowView.isNextWaypoint ||
                (plan.nextWaypointRowId && rowView.row.id === plan.nextWaypointRowId),
            ),
            playerSystem: plan.playerSystem,
            isSystemCopied: false,
            insertMenuOpen: false,
            lastVisitedIndex,
            rowStyle: {},
            dropProps: {},
            externalLinks: probeExternalLinks,
            handlers: probeHandlers,
          }),
        ),
      ),
    )
    htmlLength += html.length
  })
  return { htmlLength, waypointCount }
}

function renderVisitedDrawersBatchHTML(hydrated: HydratedFrontendFixture): number {
  const { plan, modesList, visitedDetails } = hydrated
  let drawersHTMLLength = 0
  const fallbackRowView =
    plan.rows.find((candidate) => candidate.row.rowType === 'waypoint') ?? plan.rows[0]
  if (!fallbackRowView) {
    return 0
  }
  for (const detail of visitedDetails) {
    const matchingRow =
      plan.rows.find((candidate) => candidate.row.id === detail.rowId) ?? fallbackRowView
    drawersHTMLLength += renderToString(
      createElement(WaypointDetailsDrawer, {
        details: detail,
        loading: false,
        rowView: matchingRow,
        modeId: matchingRow.legModeId,
        modeColor: matchingRow.legModeColor,
        modesList,
        onChangeLegMode: noop,
      }),
    ).length
  }
  return drawersHTMLLength
}

/**
 * executeSSRRenderPass benchmarks server-rendered HTML generation across the
 * planner table, expedition and all-time sidebars, drag slots, rows, and drawers.
 */
export function executeSSRRenderPass(hydrated: HydratedFrontendFixture): {
  summary: string
  hashInput: string
  children: SpanNodeJSON[]
} {
  const { plan, visitedDetails } = hydrated
  const children: SpanNodeJSON[] = []

  const tPlanner = performance.now()
  const plannerHTML = renderToString(
    createElement(ExpeditionPlanner, {
      plan,
      archiveRevision: 'live-profile-rev',
      externalLinkInara: 'always',
      externalLinkEdsm: 'if_no_gec',
      externalLinkGec: 'always',
      onPlanOptimisticUpdate: noop,
      onOpenStartModal: noop,
      onRefreshPlan: noop,
      onBusyChange: noop,
    }),
  )
  children.push(
    makeRenderSpan(
      'ExpeditionPlanner (Full Route Table SSR)',
      'frontend/src/features/planner/ExpeditionPlanner.tsx:44',
      1,
      plan.rows.length,
      'rows',
      msToNs(performance.now() - tPlanner),
    ),
  )

  const tSidebar = performance.now()
  const sidebarCombinedHTML = renderSidebarBothScopesHTML(hydrated)
  children.push(
    makeRenderSpan(
      'StatsSidebar (Expedition and All-Time Scopes SSR)',
      'frontend/src/features/sidebar/StatsSidebar.tsx:29',
      2,
      2,
      'scopes',
      msToNs(performance.now() - tSidebar),
    ),
  )

  const tDrag = performance.now()
  const dragHTML = renderDragPreviewTableHTML(hydrated)
  if (dragHTML.length > 0) {
    children.push(
      makeRenderSpan(
        'PlannerRouteTable[Drag Drop-Slot Preview SSR]',
        'frontend/src/features/planner/table/PlannerRouteTable.tsx:120',
        1,
        hydrated.dropPreviews.length,
        'slots',
        msToNs(performance.now() - tDrag),
      ),
    )
  }

  const tRows = performance.now()
  const { htmlLength: rowsHTMLLength, waypointCount } = renderWaypointRowsBatchHTML(hydrated)
  children.push(
    makeRenderSpan(
      'WaypointRowItem[Per-Row Render Pass]',
      'frontend/src/features/planner/table/WaypointRowItem.tsx:76',
      waypointCount,
      waypointCount,
      'waypoints',
      msToNs(performance.now() - tRows),
    ),
  )

  const tDrawers = performance.now()
  const drawersHTMLLength = renderVisitedDrawersBatchHTML(hydrated)
  children.push(
    makeRenderSpan(
      'WaypointDetailsDrawer[Visited Drawers Batch]',
      'frontend/src/features/planner/drawer/WaypointDetailsDrawer.tsx:31',
      visitedDetails.length,
      visitedDetails.length,
      'drawers',
      msToNs(performance.now() - tDrawers),
    ),
  )

  const totalHTMLBytes =
    plannerHTML.length + sidebarCombinedHTML.length + dragHTML.length + rowsHTMLLength + drawersHTMLLength
  return {
    summary: `Rows=${plan.rows.length} Waypoints=${waypointCount} Drawers=${visitedDetails.length} HTMLBytes=${totalHTMLBytes}`,
    hashInput: `${plannerHTML.length}:${sidebarCombinedHTML.length}:${dragHTML.length}:${rowsHTMLLength}:${drawersHTMLLength}`,
    children,
  }
}

