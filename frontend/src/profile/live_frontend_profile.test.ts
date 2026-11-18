import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { ExpeditionPlanner } from '../components/planner/ExpeditionPlanner'
import { WaypointRowItem } from '../components/planner/WaypointRowItem'
import { WaypointDetailsDrawer } from '../components/planner/WaypointDetailsDrawer'
import { StatsSidebar } from '../components/sidebar/StatsSidebar'
import { app, expedition, modes, stats } from '../../wailsjs/go/models'

declare const process: {
  env: Record<string, string | undefined>
}

interface NodeFSModule {
  readFileSync: (filePath: string, encoding: string) => string
  writeFileSync: (filePath: string, content: string, encoding: string) => void
}

interface LiveFrontendFixture {
  plan: Record<string, unknown>
  sidebarStats: Record<string, unknown>
  modes: Array<Record<string, unknown>>
  visitedDetails: Array<Record<string, unknown>>
}

interface SpanNodeJSON {
  name: string
  location?: string
  category?: string
  calls: number
  itemsProcessed: number
  itemUnit?: string
  totalDuration: number
  selfDuration: number
  sqlDuration: number
  goCpuDuration: number
  sqlQueries: number
  rowsScanned: number
  allocBytes: number
  mallocs: number
  children?: SpanNodeJSON[]
}

interface ProbeRunJSON {
  mode: string
  iterations: number
  medianDuration: number
  minDuration: number
  maxDuration: number
  normalizedNsPerItem: number
  itemsPerSecond: number
  allocBytes: number
  bytesPerItem: number
  mallocs: number
  allocsPerItem: number
  sqlQueries: number
  sqlDuration: number
  goCpuDuration: number
  resultHash: string
  resultSummary: string
  rootSpan: SpanNodeJSON
}

interface ProbeResultJSON {
  name: string
  category: string
  description: string
  itemsProcessed: number
  itemUnit: string
  secondaryCount?: number
  secondaryUnit?: string
  cold: ProbeRunJSON
  warm: ProbeRunJSON
  hasWarm: boolean
  coldWarmMatch: boolean
  warmSpeedupPct: number
}

const fixturePath = process.env.LIVE_PROFILE_FIXTURE ?? ''
const outputPath = process.env.LIVE_PROFILE_OUT ?? ''
const requestedIterations = Number.parseInt(process.env.LIVE_PROFILE_ITER ?? '3', 10)
const iterationsCount = Number.isFinite(requestedIterations) && requestedIterations > 0 ? requestedIterations : 3

function msToNs(milliseconds: number): number {
  return Math.round(milliseconds * 1_000_000)
}

function computeSimpleHash(inputText: string): string {
  let hashValue = 2166136261
  for (let index = 0; index < inputText.length; index++) {
    hashValue ^= inputText.charCodeAt(index)
    hashValue = Math.imul(hashValue, 16777619)
  }
  return (hashValue >>> 0).toString(16).padStart(8, '0')
}

function buildProbeRun(
  mode: string,
  rootName: string,
  location: string,
  itemsProcessed: number,
  itemUnit: string,
  iterations: number,
  executeStep: () => { summary: string; hashInput: string; children: SpanNodeJSON[] },
): ProbeRunJSON {
  const durationsNs: number[] = []
  let lastSummary = ''
  let lastHash = ''
  let lastChildren: SpanNodeJSON[] = []

  for (let iter = 0; iter < iterations; iter++) {
    const startMs = performance.now()
    const stepOutcome = executeStep()
    const elapsedNs = msToNs(performance.now() - startMs)
    durationsNs.push(elapsedNs)
    lastSummary = stepOutcome.summary
    lastHash = computeSimpleHash(stepOutcome.hashInput)
    lastChildren = stepOutcome.children
  }

  durationsNs.sort((left, right) => left - right)
  const medianNs = durationsNs[Math.floor(durationsNs.length / 2)] ?? 0
  const minNs = durationsNs[0] ?? 0
  const maxNs = durationsNs[durationsNs.length - 1] ?? 0

  let childrenSumNs = 0
  for (const child of lastChildren) {
    childrenSumNs += child.totalDuration
  }
  const selfNs = Math.max(0, medianNs - childrenSumNs)
  const safeItems = Math.max(itemsProcessed, 1)
  const nsPerItem = medianNs / safeItems
  const itemsPerSecond = medianNs > 0 ? (safeItems * 1_000_000_000) / medianNs : 0

  return {
    mode,
    iterations,
    medianDuration: medianNs,
    minDuration: minNs,
    maxDuration: maxNs,
    normalizedNsPerItem: nsPerItem,
    itemsPerSecond,
    allocBytes: 0,
    bytesPerItem: 0,
    mallocs: 0,
    allocsPerItem: 0,
    sqlQueries: 0,
    sqlDuration: 0,
    goCpuDuration: medianNs,
    resultHash: lastHash,
    resultSummary: lastSummary,
    rootSpan: {
      name: rootName,
      location,
      category: 'render',
      calls: 1,
      itemsProcessed: safeItems,
      itemUnit,
      totalDuration: medianNs,
      selfDuration: selfNs,
      sqlDuration: 0,
      goCpuDuration: medianNs,
      sqlQueries: 0,
      rowsScanned: 0,
      allocBytes: 0,
      mallocs: 0,
      children: lastChildren,
    },
  }
}

describe.skipIf(!fixturePath || !outputPath)('Live Data Frontend CLI Profiler', () => {
  it('profiles Wails model hydration and React component tree rendering on live archive data', async () => {
    const dynamicImporter = new Function('specifier', 'return import(specifier)') as (
      specifier: string,
    ) => Promise<NodeFSModule>
    const nodeFS = await dynamicImporter('node:fs')
    const rawFixtureJSON = nodeFS.readFileSync(fixturePath, 'utf-8')
    const parsedFixture = JSON.parse(rawFixtureJSON) as LiveFrontendFixture

    const rawVisitedDetails = Array.isArray(parsedFixture.visitedDetails)
      ? parsedFixture.visitedDetails
      : []
    const rawModes = Array.isArray(parsedFixture.modes) ? parsedFixture.modes : []
    const rowCount = Array.isArray(parsedFixture.plan.rows) ? parsedFixture.plan.rows.length : 0
    const detailsCount = rawVisitedDetails.length
    const totalHydrationItems = Math.max(rowCount + detailsCount + 2, 1)

    const runHydrationPass = () => {
      const children: SpanNodeJSON[] = []

      const t0 = performance.now()
      const hydratedPlan = expedition.PlanView.createFrom(parsedFixture.plan)
      const planNs = msToNs(performance.now() - t0)
      children.push({
        name: 'expedition.PlanView.createFrom',
        location: 'frontend/wailsjs/go/models.ts',
        category: 'render',
        calls: 1,
        itemsProcessed: Math.max(hydratedPlan.rows.length, 1),
        itemUnit: 'rows',
        totalDuration: planNs,
        selfDuration: planNs,
        sqlDuration: 0,
        goCpuDuration: planNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      const t1 = performance.now()
      const hydratedSidebar = app.SidebarStats.createFrom(parsedFixture.sidebarStats)
      const sidebarNs = msToNs(performance.now() - t1)
      children.push({
        name: 'app.SidebarStats.createFrom',
        location: 'frontend/wailsjs/go/models.ts',
        category: 'render',
        calls: 1,
        itemsProcessed: 1,
        itemUnit: 'sidebar',
        totalDuration: sidebarNs,
        selfDuration: sidebarNs,
        sqlDuration: 0,
        goCpuDuration: sidebarNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      const t2 = performance.now()
      const hydratedDetails = rawVisitedDetails.map((item) =>
        expedition.WaypointDetails.createFrom(item),
      )
      const detailsNs = msToNs(performance.now() - t2)
      children.push({
        name: 'expedition.WaypointDetails.createFrom[Batch]',
        location: 'frontend/wailsjs/go/models.ts',
        category: 'render',
        calls: hydratedDetails.length,
        itemsProcessed: Math.max(hydratedDetails.length, 1),
        itemUnit: 'drawers',
        totalDuration: detailsNs,
        selfDuration: detailsNs,
        sqlDuration: 0,
        goCpuDuration: detailsNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      return {
        summary: `PlanRows=${hydratedPlan.rows.length} VisitedDrawers=${hydratedDetails.length} ExpeditionCarto=${hydratedSidebar.expedition?.scanValueTotal ?? 0}`,
        hashInput: `${hydratedPlan.rows.length}:${hydratedDetails.length}:${hydratedSidebar.expedition?.scanValueTotal ?? 0}`,
        children,
      }
    }

    const hydrationCold = buildProbeRun(
      'Cold',
      'Frontend/WailsHydration',
      'frontend/wailsjs/go/models.ts',
      totalHydrationItems,
      'models',
      1,
      runHydrationPass,
    )
    const hydrationWarm = buildProbeRun(
      'Warm',
      'Frontend/WailsHydration',
      'frontend/wailsjs/go/models.ts',
      totalHydrationItems,
      'models',
      iterationsCount,
      runHydrationPass,
    )

    const hydratedPlan = expedition.PlanView.createFrom(parsedFixture.plan)
    const hydratedSidebar = app.SidebarStats.createFrom(parsedFixture.sidebarStats)
    const hydratedModes = rawModes.map((item) => modes.Mode.createFrom(item))
    const hydratedDetails = rawVisitedDetails.map((item) =>
      expedition.WaypointDetails.createFrom(item),
    )
    const modesState = app.ModesState.createFrom({
      modes: hydratedModes,
      windows: [],
    })
    const emptyFilter = stats.Filter.createFrom({ enabled: false, modes: [] })

    const runRenderPass = () => {
      const children: SpanNodeJSON[] = []

      const tPlanner = performance.now()
      const plannerHTML = renderToString(
        createElement(ExpeditionPlanner, {
          plan: hydratedPlan,
          archiveRevision: 'live-profile-rev',
          onPlanOptimisticUpdate: () => {},
          onOpenStartModal: () => {},
          onRefreshPlan: () => {},
          onBusyChange: () => {},
        }),
      )
      const plannerNs = msToNs(performance.now() - tPlanner)
      children.push({
        name: 'ExpeditionPlanner (Full Route Table SSR)',
        location: 'frontend/src/components/planner/ExpeditionPlanner.tsx:54',
        category: 'render',
        calls: 1,
        itemsProcessed: Math.max(hydratedPlan.rows.length, 1),
        itemUnit: 'rows',
        totalDuration: plannerNs,
        selfDuration: plannerNs,
        sqlDuration: 0,
        goCpuDuration: plannerNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      const tSidebar = performance.now()
      const sidebarHTML = renderToString(
        createElement(StatsSidebar, {
          sidebarStats: hydratedSidebar,
          statsLoading: false,
          modesState,
          filter: emptyFilter,
          collapsed: false,
          carrierRouteOpen: false,
          onFilterChange: () => {},
          onQuickSwitchMode: () => {},
        }),
      )
      const sidebarNs = msToNs(performance.now() - tSidebar)
      children.push({
        name: 'StatsSidebar (Expedition and All-Time Telemetry SSR)',
        location: 'frontend/src/components/sidebar/StatsSidebar.tsx:29',
        category: 'render',
        calls: 1,
        itemsProcessed: 1,
        itemUnit: 'sidebar',
        totalDuration: sidebarNs,
        selfDuration: sidebarNs,
        sqlDuration: 0,
        goCpuDuration: sidebarNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      const waypointRows = hydratedPlan.rows.filter((rowView) => rowView.row.rowType === 'waypoint')
      const tRows = performance.now()
      let rowsHTMLLength = 0
      for (let idx = 0; idx < waypointRows.length; idx++) {
        const rowView = waypointRows[idx]
        const html = renderToString(
          createElement(
            'table',
            null,
            createElement(
              'tbody',
              null,
              createElement(WaypointRowItem, {
                rowView,
                index: idx,
                isPlanning: hydratedPlan.expedition.status === 'planning',
                isLocked: hydratedPlan.expedition.status === 'completed',
                isBeingDragged: false,
                isExpanded: false,
                isNextFcStop: false,
                hasActiveCarrierRoute: false,
                copiedSystemRowId: null,
                insertMenuOpen: false,
                lastVisitedIndex: hydratedPlan.visitedWaypointCount - 1,
                rowStyle: {},
                dropProps: {},
                onToggleExpand: () => {},
                onFieldBlur: () => {},
                onCopySystem: () => {},
                onToggleFcStop: () => {},
                onOverrideFcArrived: () => {},
                onOpenCarrierRoutePrompt: () => {},
                onToggleInsertMenu: () => {},
                onInsert: () => {},
                onDelete: () => {},
                onDragStart: () => {},
                onDragEnd: () => {},
              }),
            ),
          ),
        )
        rowsHTMLLength += html.length
      }
      const rowsNs = msToNs(performance.now() - tRows)
      children.push({
        name: 'WaypointRowItem[Per-Row Keystroke Re-Render Pass]',
        location: 'frontend/src/components/planner/WaypointRowItem.tsx:126',
        category: 'render',
        calls: waypointRows.length,
        itemsProcessed: Math.max(waypointRows.length, 1),
        itemUnit: 'waypoints',
        totalDuration: rowsNs,
        selfDuration: rowsNs,
        sqlDuration: 0,
        goCpuDuration: rowsNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      const tDrawers = performance.now()
      let drawersHTMLLength = 0
      const fallbackRowView = waypointRows[0] ?? hydratedPlan.rows[0]
      if (fallbackRowView) {
        for (const detail of hydratedDetails) {
          const matchingRow =
            hydratedPlan.rows.find((candidate) => candidate.row.id === detail.rowId) ?? fallbackRowView
          const drawerHTML = renderToString(
            createElement(WaypointDetailsDrawer, {
              details: detail,
              loading: false,
              rowView: matchingRow,
              modesList: hydratedModes,
            }),
          )
          drawersHTMLLength += drawerHTML.length
        }
      }
      const drawersNs = msToNs(performance.now() - tDrawers)
      children.push({
        name: 'WaypointDetailsDrawer[Visited Drawers Batch]',
        location: 'frontend/src/components/planner/WaypointDetailsDrawer.tsx:28',
        category: 'render',
        calls: hydratedDetails.length,
        itemsProcessed: Math.max(hydratedDetails.length, 1),
        itemUnit: 'drawers',
        totalDuration: drawersNs,
        selfDuration: drawersNs,
        sqlDuration: 0,
        goCpuDuration: drawersNs,
        sqlQueries: 0,
        rowsScanned: 0,
        allocBytes: 0,
        mallocs: 0,
      })

      const totalHTMLBytes = plannerHTML.length + sidebarHTML.length + rowsHTMLLength + drawersHTMLLength
      return {
        summary: `Rows=${hydratedPlan.rows.length} Waypoints=${waypointRows.length} Drawers=${hydratedDetails.length} HTMLBytes=${totalHTMLBytes}`,
        hashInput: `${plannerHTML.length}:${sidebarHTML.length}:${rowsHTMLLength}:${drawersHTMLLength}`,
        children,
      }
    }

    const renderCold = buildProbeRun(
      'Cold',
      'Frontend/RenderTree',
      'frontend/src/components/planner/ExpeditionPlanner.tsx:54',
      Math.max(hydratedPlan.rows.length, 1),
      'rows',
      1,
      runRenderPass,
    )
    const renderWarm = buildProbeRun(
      'Warm',
      'Frontend/RenderTree',
      'frontend/src/components/planner/ExpeditionPlanner.tsx:54',
      Math.max(hydratedPlan.rows.length, 1),
      'rows',
      iterationsCount,
      runRenderPass,
    )

    const calcSpeedup = (coldNs: number, warmNs: number): number =>
      coldNs > 0 ? ((coldNs - warmNs) / coldNs) * 100.0 : 0

    const probeResults: ProbeResultJSON[] = [
      {
        name: 'Frontend/WailsHydration',
        category: 'Frontend',
        description: 'Hydrate Wails TypeScript classes (PlanView, SidebarStats, WaypointDetails) from live JSON',
        itemsProcessed: totalHydrationItems,
        itemUnit: 'models',
        secondaryCount: hydratedPlan.rows.length,
        secondaryUnit: 'rows',
        cold: hydrationCold,
        warm: hydrationWarm,
        hasWarm: true,
        coldWarmMatch: hydrationCold.resultHash === hydrationWarm.resultHash,
        warmSpeedupPct: calcSpeedup(hydrationCold.medianDuration, hydrationWarm.medianDuration),
      },
      {
        name: 'Frontend/RenderTree',
        category: 'Frontend',
        description: 'Render ExpeditionPlanner, StatsSidebar, WaypointRowItem table pass, and WaypointDetailsDrawers',
        itemsProcessed: Math.max(hydratedPlan.rows.length, 1),
        itemUnit: 'rows',
        secondaryCount: hydratedDetails.length,
        secondaryUnit: 'drawers',
        cold: renderCold,
        warm: renderWarm,
        hasWarm: true,
        coldWarmMatch: renderCold.resultHash === renderWarm.resultHash,
        warmSpeedupPct: calcSpeedup(renderCold.medianDuration, renderWarm.medianDuration),
      },
    ]

    nodeFS.writeFileSync(outputPath, JSON.stringify(probeResults, null, 2), 'utf-8')
    expect(probeResults).toHaveLength(2)
  })
})
