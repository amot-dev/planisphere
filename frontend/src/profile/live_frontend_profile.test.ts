// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it } from 'vitest'
import { stats } from '../api/models'
import { ExpeditionPlanner } from '../features/planner/ExpeditionPlanner'
import { StatsSidebar } from '../features/sidebar/StatsSidebar'
import {
  buildProbeRun,
  hydrateFrontendFixture,
  installWailsProfileMocks,
  makeRenderSpan,
  msToNs,
  runTextareaBatchCoordinatorPass,
  type HydratedFrontendFixture,
  type LiveFrontendFixture,
  type NodeFSModule,
  type ProbeResultJSON,
  type SpanNodeJSON,
} from './profileHelpers'
import { buildSmokeFrontendFixture } from './smokeFixture'
import { executeExportStudioPass } from './exportProbes'
import { executeHydrationPass, executeSSRRenderPass } from './ssrProbes'

declare const process: {
  env: Record<string, string | undefined>
}

const fixturePath = process.env.LIVE_PROFILE_FIXTURE ?? ''
const outputPath = process.env.LIVE_PROFILE_OUT ?? ''
const requestedIterations = Number.parseInt(process.env.LIVE_PROFILE_ITER ?? '3', 10)
const iterationsCount =
  Number.isFinite(requestedIterations) && requestedIterations > 0 ? requestedIterations : 3

const noop = () => {}

function clickMatchingButton(container: HTMLElement, selector: string): void {
  const targetButton = container.querySelector<HTMLButtonElement>(selector)
  if (!targetButton) return
  act(() => {
    targetButton.click()
  })
}

function mountWorkspaceTree(
  root: ReturnType<typeof createRoot>,
  hydrated: HydratedFrontendFixture,
): number {
  const { plan, sidebar, modesState } = hydrated
  const emptyFilter = stats.Filter.createFrom({ enabled: false, modes: [] })
  const startMs = performance.now()
  act(() => {
    root.render(
      createElement(
        'div',
        null,
        createElement(StatsSidebar, {
          sidebarStats: sidebar,
          statsLoading: false,
          modesState,
          filter: emptyFilter,
          collapsed: false,
          carrierRouteOpen: false,
          onFilterChange: noop,
        }),
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
      ),
    )
  })
  return msToNs(performance.now() - startMs)
}

function executeClientReconciliationPass(hydrated: HydratedFrontendFixture): {
  summary: string
  hashInput: string
  children: SpanNodeJSON[]
} {
  const { plan } = hydrated
  const children: SpanNodeJSON[] = []
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  try {
    const mountNs = mountWorkspaceTree(root, hydrated)
    children.push(
      makeRenderSpan(
        'createRoot Mount (ExpeditionPlanner + StatsSidebar)',
        'frontend/src/features/planner/ExpeditionPlanner.tsx:44',
        1,
        plan.rows.length,
        'rows',
        mountNs,
      ),
    )

    const tCopy = performance.now()
    clickMatchingButton(container, 'button[title$="to clipboard"]')
    const copiedButtonsCount = container.querySelectorAll('button[title^="Copied "]').length
    children.push(
      makeRenderSpan(
        'Single-Row Copy State Update (React.memo Row Bailout)',
        'frontend/src/features/planner/table/PlannerRouteTable.tsx:223',
        1,
        plan.rows.length,
        'rows',
        msToNs(performance.now() - tCopy),
      ),
    )

    const tDrawer = performance.now()
    clickMatchingButton(container, 'button[aria-label="Toggle per-waypoint leg telemetry drawer"]')
    const expandedDrawersCount = container.querySelectorAll('td.p-0 > div.border-l-2').length
    children.push(
      makeRenderSpan(
        'Toggle Waypoint Telemetry Drawer Reconciliation',
        'frontend/src/features/planner/drawer/useWaypointDetails.ts:37',
        1,
        expandedDrawersCount,
        'drawer',
        msToNs(performance.now() - tDrawer),
      ),
    )

    const tScope = performance.now()
    clickMatchingButton(
      container,
      'button[title="Show cumulative metrics across all loaded journal history"]',
    )
    children.push(
      makeRenderSpan(
        'StatsSidebar Scope Switch (Expedition -> All Time)',
        'frontend/src/features/sidebar/StatsSidebar.tsx:29',
        1,
        1,
        'sidebar',
        msToNs(performance.now() - tScope),
      ),
    )

    const tTextarea = performance.now()
    const measuredCells = runTextareaBatchCoordinatorPass(plan.rows)
    children.push(
      makeRenderSpan(
        'AutoExpandingTextarea 3-Phase Batch Coordinator',
        'frontend/src/components/ui/AutoExpandingTextarea.tsx:88',
        1,
        measuredCells,
        'textareas',
        msToNs(performance.now() - tTextarea),
      ),
    )

    const domElementCount = container.querySelectorAll('*').length
    return {
      summary: `DOMNodes=${domElementCount} CopiedButtons=${copiedButtonsCount} ExpandedDrawers=${expandedDrawersCount} TextareasMeasured=${measuredCells}`,
      hashInput: `${domElementCount}:${copiedButtonsCount}:${expandedDrawersCount}:${measuredCells}`,
      children,
    }
  } finally {
    act(() => {
      root.unmount()
    })
    container.remove()
  }
}

function runAllFrontendProbes(
  parsedFixture: LiveFrontendFixture,
  warmIterations: number,
): ProbeResultJSON[] {
  const hydrated = hydrateFrontendFixture(parsedFixture)
  installWailsProfileMocks(hydrated.visitedDetails)
  const calcSpeedup = (coldNs: number, warmNs: number): number =>
    coldNs > 0 ? ((coldNs - warmNs) / coldNs) * 100.0 : 0

  const totalHydrationItems = Math.max(
    hydrated.plan.rows.length + hydrated.visitedDetails.length + hydrated.dropPreviews.length + 2,
    1,
  )
  const hydrationCold = buildProbeRun(
    'Cold',
    'Frontend/WailsHydration',
    'frontend/wailsjs/go/models.ts',
    totalHydrationItems,
    'models',
    1,
    () => executeHydrationPass(parsedFixture),
  )
  const hydrationWarm = buildProbeRun(
    'Warm',
    'Frontend/WailsHydration',
    'frontend/wailsjs/go/models.ts',
    totalHydrationItems,
    'models',
    warmIterations,
    () => executeHydrationPass(parsedFixture),
  )

  const rowItemCount = Math.max(hydrated.plan.rows.length, 1)
  const renderCold = buildProbeRun(
    'Cold',
    'Frontend/RenderTree',
    'frontend/src/features/planner/ExpeditionPlanner.tsx:44',
    rowItemCount,
    'rows',
    1,
    () => executeSSRRenderPass(hydrated),
  )
  const renderWarm = buildProbeRun(
    'Warm',
    'Frontend/RenderTree',
    'frontend/src/features/planner/ExpeditionPlanner.tsx:44',
    rowItemCount,
    'rows',
    warmIterations,
    () => executeSSRRenderPass(hydrated),
  )

  const clientCold = buildProbeRun(
    'Cold',
    'Frontend/ClientReconciliation',
    'frontend/src/features/planner/ExpeditionPlanner.tsx:44',
    rowItemCount,
    'rows',
    1,
    () => executeClientReconciliationPass(hydrated),
  )
  const clientWarm = buildProbeRun(
    'Warm',
    'Frontend/ClientReconciliation',
    'frontend/src/features/planner/ExpeditionPlanner.tsx:44',
    rowItemCount,
    'rows',
    warmIterations,
    () => executeClientReconciliationPass(hydrated),
  )

  const exportRowCount = Math.max(hydrated.exportData?.plan.rows.length ?? hydrated.plan.rows.length, 1)
  const exportSecondaryCount = hydrated.exportData?.rowDetails.length ?? hydrated.visitedDetails.length
  const exportCold = buildProbeRun(
    'Cold',
    'Frontend/ExportStudio',
    'frontend/src/features/export/ExportExpeditionModal.tsx:45',
    exportRowCount,
    'rows',
    1,
    () => executeExportStudioPass(hydrated),
  )
  const exportWarm = buildProbeRun(
    'Warm',
    'Frontend/ExportStudio',
    'frontend/src/features/export/ExportExpeditionModal.tsx:45',
    exportRowCount,
    'rows',
    warmIterations,
    () => executeExportStudioPass(hydrated),
  )

  return [
    {
      name: 'Frontend/WailsHydration',
      category: 'Frontend',
      description:
        'Hydrate Wails TypeScript classes (PlanView, SidebarStats, ModesState, WaypointDetails, DropSlotPreview)',
      itemsProcessed: totalHydrationItems,
      itemUnit: 'models',
      secondaryCount: hydrated.plan.rows.length,
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
      description:
        'SSR render ExpeditionPlanner, StatsSidebar (Expedition and All-Time), drag slots, rows, and drawers',
      itemsProcessed: rowItemCount,
      itemUnit: 'rows',
      secondaryCount: hydrated.visitedDetails.length,
      secondaryUnit: 'drawers',
      cold: renderCold,
      warm: renderWarm,
      hasWarm: true,
      coldWarmMatch: renderCold.resultHash === renderWarm.resultHash,
      warmSpeedupPct: calcSpeedup(renderCold.medianDuration, renderWarm.medianDuration),
    },
    {
      name: 'Frontend/ClientReconciliation',
      category: 'Frontend',
      description:
        'happy-dom client mount, memoized row copy update, drawer toggle, scope switch, and textarea batch layout',
      itemsProcessed: rowItemCount,
      itemUnit: 'rows',
      secondaryCount: hydrated.visitedDetails.length,
      secondaryUnit: 'drawers',
      cold: clientCold,
      warm: clientWarm,
      hasWarm: true,
      coldWarmMatch: clientCold.resultHash === clientWarm.resultHash,
      warmSpeedupPct: calcSpeedup(clientCold.medianDuration, clientWarm.medianDuration),
    },
    {
      name: 'Frontend/ExportStudio',
      category: 'Frontend',
      description:
        'Export Studio cart expansion, ExportBlockRenderer SSR, HTML report generation, and canonical JSON snapshot',
      itemsProcessed: exportRowCount,
      itemUnit: 'rows',
      secondaryCount: exportSecondaryCount,
      secondaryUnit: 'legs',
      cold: exportCold,
      warm: exportWarm,
      hasWarm: true,
      coldWarmMatch: exportCold.resultHash === exportWarm.resultHash,
      warmSpeedupPct: calcSpeedup(exportCold.medianDuration, exportWarm.medianDuration),
    },
  ]
}

describe.skipIf(!fixturePath || !outputPath)('Live Data Frontend CLI Profiler', () => {
  it('profiles Wails model hydration, SSR trees, and happy-dom client reconciliation on live archive data', async () => {
    const dynamicImporter = new Function('specifier', 'return import(specifier)') as (
      specifier: string,
    ) => Promise<NodeFSModule>
    const nodeFS = await dynamicImporter('node:fs')
    const rawFixtureJSON = nodeFS.readFileSync(fixturePath, 'utf-8')
    const parsedFixture = JSON.parse(rawFixtureJSON) as LiveFrontendFixture

    const probeResults = runAllFrontendProbes(parsedFixture, iterationsCount)
    nodeFS.writeFileSync(outputPath, JSON.stringify(probeResults, null, 2), 'utf-8')
    expect(probeResults).toHaveLength(4)
  })
})

describe.skipIf(Boolean(fixturePath))('Frontend Profiler Smoke Suite', () => {
  it('validates hydration, SSR, drag-slot preview, and happy-dom client reconciliation probes', () => {
    const results = runAllFrontendProbes(buildSmokeFrontendFixture(), 1)
    expect(results).toHaveLength(4)
    for (const probe of results) {
      expect(probe.coldWarmMatch).toBe(true)
      expect(probe.cold.rootSpan.children?.length).toBeGreaterThan(0)
      if (probe.name === 'Frontend/ClientReconciliation') {
        expect(probe.cold.resultSummary).toContain('ExpandedDrawers=1')
      }
    }
  })
})
