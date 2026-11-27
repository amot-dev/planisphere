// @vitest-environment happy-dom
import { act, useRef } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, expedition, modes, stats } from '../../api/models'
import {
  createSyncDispatchHarness,
  makeMode,
  makePlanView,
  makeSettings,
  makeSidebarStats,
} from '../../test/fixtures'
import { renderHook } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import {
  type SyncBootLifecycleState,
  flushDeferredBootEvents,
  routeDataChangedEvent,
} from './syncEventRouter'
import { isExpeditionExportEnabled } from './useBackendSync'
import { reconcileModeFilter, useDomainLoaders } from './useDomainLoaders'

const createDispatchHarness = createSyncDispatchHarness

describe('routeDataChangedEvent', () => {
  it('ignores events emitted during initial boot before core state loads', () => {
    const {
      dispatchContext,
      refreshExpeditionListOnly,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: false,
      deferredPayloads: [],
    })

    routeDataChangedEvent({ domains: ['ingest', 'plan', 'stats'], expeditionId: 0 }, dispatchContext)

    expect(dispatchContext.bootState.deferredPayloads).toEqual([])
    expect(refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('queues deferred payloads instead of cancelling boot loaders once core state has loaded', () => {
    const {
      dispatchContext,
      refreshExpeditionListOnly,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: true,
      deferredPayloads: [],
    })

    routeDataChangedEvent({ domains: ['plan'], expeditionId: 1 }, dispatchContext)
    routeDataChangedEvent({ domains: [] }, dispatchContext)

    expect(dispatchContext.bootState.deferredPayloads).toEqual([
      { domains: ['plan'], expeditionId: 1 },
    ])
    expect(refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('treats missing or empty domains payloads as a strict no-op after boot', () => {
    for (const payload of [undefined, {}, { domains: [] }]) {
      const {
        dispatchContext,
        refreshExpeditionListOnly,
        loadIngestStatusOnly,
        loadSettingsOnly,
        loadModesOnly,
        loadPlanOnly,
        loadStatsOnly,
        refreshAll,
      } = createDispatchHarness()

      routeDataChangedEvent(payload, dispatchContext)

      expect(refreshExpeditionListOnly).not.toHaveBeenCalled()
      expect(loadIngestStatusOnly).not.toHaveBeenCalled()
      expect(loadSettingsOnly).not.toHaveBeenCalled()
      expect(loadModesOnly).not.toHaveBeenCalled()
      expect(loadPlanOnly).not.toHaveBeenCalled()
      expect(loadStatsOnly).not.toHaveBeenCalled()
      expect(refreshAll).not.toHaveBeenCalled()
    }
  })

  it('routes targeted UI domains without triggering full reloads or unrelated loaders', () => {
    const ingestOnlyHarness = createDispatchHarness()
    routeDataChangedEvent(
      { domains: ['ingest'] },
      ingestOnlyHarness.dispatchContext,
    )
    expect(ingestOnlyHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(ingestOnlyHarness.loadSettingsOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadModesOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadStatsOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.refreshAll).not.toHaveBeenCalled()

    const scanHarness = createDispatchHarness()
    routeDataChangedEvent(
      { domains: ['ingest', 'stats'] },
      scanHarness.dispatchContext,
    )
    expect(scanHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(scanHarness.loadStatsOnly).toHaveBeenCalledWith(4, scanHarness.filter)
    expect(scanHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(scanHarness.loadSettingsOnly).not.toHaveBeenCalled()

    const arrivalHarness = createDispatchHarness()
    routeDataChangedEvent(
      { domains: ['ingest', 'plan', 'stats'] },
      arrivalHarness.dispatchContext,
    )
    expect(arrivalHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(arrivalHarness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(arrivalHarness.loadStatsOnly).toHaveBeenCalledWith(4, arrivalHarness.filter)

    const modesHarness = createDispatchHarness()
    routeDataChangedEvent(
      { domains: ['stats', 'plan', 'modes'] },
      modesHarness.dispatchContext,
    )
    expect(modesHarness.loadModesOnly).toHaveBeenCalledTimes(1)
    expect(modesHarness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(modesHarness.loadStatsOnly).toHaveBeenCalledWith(4, modesHarness.filter)
    expect(modesHarness.loadIngestStatusOnly).not.toHaveBeenCalled()

    const expeditionsHarness = createDispatchHarness()
    routeDataChangedEvent(
      { domains: ['stats', 'expeditions'] },
      expeditionsHarness.dispatchContext,
    )
    expect(expeditionsHarness.refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(expeditionsHarness.loadStatsOnly).toHaveBeenCalledWith(4, expeditionsHarness.filter)
    expect(expeditionsHarness.loadPlanOnly).not.toHaveBeenCalled()

    const dbSwitchHarness = createDispatchHarness()
    routeDataChangedEvent(
      {
        domains: ['ingest', 'stats', 'plan', 'modes', 'settings', 'expeditions'],
      },
      dbSwitchHarness.dispatchContext,
    )
    expect(dbSwitchHarness.refreshAll).toHaveBeenCalledTimes(1)
    expect(dbSwitchHarness.refreshAll).toHaveBeenCalledWith({ skipPrefetch: true })
    expect(dbSwitchHarness.loadIngestStatusOnly).not.toHaveBeenCalled()
    expect(dbSwitchHarness.loadPlanOnly).not.toHaveBeenCalled()

    const dedupHarness = createDispatchHarness({}, 0)
    routeDataChangedEvent(
      { domains: ['ingest', 'stats', 'stats', 'ingest', 'settings'] },
      dedupHarness.dispatchContext,
    )
    expect(dedupHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(dedupHarness.loadSettingsOnly).toHaveBeenCalledTimes(1)
    expect(dedupHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(dedupHarness.loadStatsOnly).not.toHaveBeenCalled()
  })

  it('routes domain updates with an inactive expeditionId without dropping global domains', () => {
    const {
      dispatchContext,
      loadSettingsOnly,
      refreshExpeditionListOnly,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness()

    routeDataChangedEvent(
      { expeditionId: 99, domains: ['settings', 'plan'] },
      dispatchContext,
    )

    expect(loadSettingsOnly).toHaveBeenCalledTimes(1)
    expect(refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('routes active expedition deletion notifications without reloading stale plan or stats', () => {
    const {
      dispatchContext,
      loadSettingsOnly,
      refreshExpeditionListOnly,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness()

    routeDataChangedEvent(
      { expeditionId: 0, domains: ['settings', 'expeditions'] },
      dispatchContext,
    )

    expect(loadSettingsOnly).toHaveBeenCalledTimes(1)
    expect(refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })
})

describe('flushDeferredBootEvents', () => {
  it('replays deferred mid-boot domains selectively and deduplicates repeated loads', () => {
    const harness = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: true,
      deferredPayloads: [],
    })

    routeDataChangedEvent({ domains: ['ingest'] }, harness.dispatchContext)
    routeDataChangedEvent({ domains: ['ingest'] }, harness.dispatchContext)
    routeDataChangedEvent({ domains: ['ingest'] }, harness.dispatchContext)
    routeDataChangedEvent({ domains: ['settings', 'plan'], expeditionId: 99 }, harness.dispatchContext)

    harness.dispatchContext.bootState.initialBootComplete = true
    flushDeferredBootEvents(harness.dispatchContext)

    expect(harness.dispatchContext.bootState.deferredPayloads).toEqual([])
    expect(harness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadSettingsOnly).toHaveBeenCalledTimes(1)
    expect(harness.refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadModesOnly).not.toHaveBeenCalled()
    expect(harness.loadPlanOnly).not.toHaveBeenCalled()
    expect(harness.loadStatsOnly).not.toHaveBeenCalled()
    expect(harness.refreshAll).not.toHaveBeenCalled()
  })

  it('triggers skipPrefetch refreshAll when a database-switch payload was deferred mid-boot', () => {
    const harness = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: true,
      deferredPayloads: [],
    })

    routeDataChangedEvent({ domains: ['ingest'] }, harness.dispatchContext)
    routeDataChangedEvent(
      { domains: ['settings', 'expeditions', 'modes'] },
      harness.dispatchContext,
    )

    harness.dispatchContext.bootState.initialBootComplete = true
    flushDeferredBootEvents(harness.dispatchContext)

    expect(harness.dispatchContext.bootState.deferredPayloads).toEqual([])
    expect(harness.refreshAll).toHaveBeenCalledTimes(1)
    expect(harness.refreshAll).toHaveBeenCalledWith({ skipPrefetch: true })
    expect(harness.loadIngestStatusOnly).not.toHaveBeenCalled()
  })
})

describe('useDomainLoaders refreshAll', () => {
  it('skips prefetching old expedition plan and stats on skipPrefetch and reconciles filter against new database modes', async () => {
    let activeDbExpeditionId = 4
    let activeDbModes: modes.Mode[] = [
      makeMode({ id: 1, name: 'Travel', color: '#ff8800' }),
      makeMode({ id: 3, name: 'Old Custom', color: '#44ff88' }),
    ]
    const onRefreshAllFilterReconciled = vi.fn()

    const { spies } = installWailsAppMock({
      GetSettings: async () =>
        makeSettings({
          setupCompleted: true,
          activeExpeditionId: activeDbExpeditionId,
        }),
      GetIngestStatus: async () => app.IngestStatus.createFrom({ scanning: false }),
      GetModesState: async () => app.ModesState.createFrom({ modes: activeDbModes }),
      ListExpeditions: async () => [
        expedition.Expedition.createFrom({
          id: activeDbExpeditionId,
          name: `Expedition ${activeDbExpeditionId}`,
          status: 'active',
        }),
      ],
      GetPlan: async (expeditionId: number) =>
        makePlanView({
          expedition: { id: expeditionId, name: `Expedition ${expeditionId}`, status: 'active' },
        }),
      GetSidebarStats: async (expeditionId: number, _requestedFilter: stats.Filter) =>
        makeSidebarStats({ expeditionId }),
    })

    let sharedFilterRef!: { current: stats.Filter }
    const mountedHook = renderHook(() => {
      const filterRef = useRef(
        stats.Filter.createFrom({
          enabled: true,
          modes: [
            { modeId: 1, travel: true, discovery: true },
            { modeId: 3, travel: false, discovery: false },
          ],
        }),
      )
      sharedFilterRef = filterRef
      const bootStateRef = useRef<SyncBootLifecycleState>({
        initialBootComplete: true,
        bootCoreLoaded: true,
        deferredPayloads: [],
      })
      const lastEffectPlanExpeditionIdRef = useRef(9)
      return useDomainLoaders({
        filterRef,
        bootStateRef,
        lastEffectPlanExpeditionIdRef,
        onRefreshAllFilterReconciled,
      })
    })
    const loaders = mountedHook.getResult()

    await act(async () => {
      await loaders.loadCoreState()
    })
    expect(loaders.activeExpeditionIdRef.current).toBe(4)

    // Default refreshAll() prefetches active expedition 4 immediately.
    await act(async () => {
      loaders.refreshAll()
      await Promise.resolve()
    })
    expect(spies.GetPlan).toHaveBeenCalledWith(4)
    expect(spies.GetSidebarStats).toHaveBeenCalledWith(4, expect.anything())

    // Database switch to active expedition 9 (matching lastEffectPlanExpeditionIdRef.current = 9)
    // with different flight modes: skipPrefetch never queries stale expedition 4 and passes a
    // filter reconciled against the new database's modes (dropping deleted mode 3, adding mode 2).
    spies.GetPlan.mockClear()
    spies.GetSidebarStats.mockClear()
    onRefreshAllFilterReconciled.mockClear()
    activeDbExpeditionId = 9
    activeDbModes = [
      makeMode({ id: 1, name: 'Travel', color: '#ff8800' }),
      makeMode({ id: 2, name: 'Survey', color: '#00ccff' }),
    ]

    await act(async () => {
      loaders.refreshAll({ skipPrefetch: true })
      expect(loaders.activeExpeditionIdRef.current).toBe(0)
      await Promise.resolve()
    })

    expect(spies.GetPlan).not.toHaveBeenCalledWith(4)
    expect(spies.GetSidebarStats).not.toHaveBeenCalledWith(4, expect.anything())
    expect(spies.GetPlan).toHaveBeenCalledTimes(1)
    expect(spies.GetPlan).toHaveBeenCalledWith(9)
    expect(spies.GetSidebarStats).toHaveBeenCalledTimes(1)
    const [calledExpeditionId, calledFilter] = spies.GetSidebarStats.mock.calls[0]
    expect(calledExpeditionId).toBe(9)
    expect(calledFilter.enabled).toBe(false)
    expect(calledFilter.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: true, discovery: true }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
    ])
    expect(sharedFilterRef.current).toBe(calledFilter)
    expect(onRefreshAllFilterReconciled).toHaveBeenCalledWith(calledFilter, 9, false)
  })
})

describe('reconcileModeFilter', () => {
  const modeTravel = modes.Mode.createFrom({ id: 1, name: 'Travel', color: '#ff8800' })
  const modeSurvey = modes.Mode.createFrom({ id: 2, name: 'Survey', color: '#00ccff' })
  const modeExobio = modes.Mode.createFrom({ id: 3, name: 'Exobiology', color: '#44ff88' })

  it('defaults newly created custom modes to full inclusion when a filter is active', () => {
    const activeFilter = stats.Filter.createFrom({
      enabled: true,
      modes: [
        { modeId: 1, travel: false, discovery: true },
        { modeId: 2, travel: true, discovery: true },
      ],
    })

    const reconciled = reconcileModeFilter(activeFilter, [modeTravel, modeSurvey, modeExobio])

    expect(reconciled.enabled).toBe(true)
    expect(reconciled.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: false, discovery: true }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
      expect.objectContaining({ modeId: 3, travel: true, discovery: true }),
    ])
  })

  it('drops deleted custom modes and resets enabled to false when no exclusions remain', () => {
    const customFiltered = stats.Filter.createFrom({
      enabled: true,
      modes: [
        { modeId: 1, travel: true, discovery: true },
        { modeId: 3, travel: false, discovery: false },
      ],
    })

    const reconciled = reconcileModeFilter(customFiltered, [modeTravel, modeSurvey])

    expect(reconciled.enabled).toBe(false)
    expect(reconciled.modes).toEqual([
      expect.objectContaining({ modeId: 1, travel: true, discovery: true }),
      expect.objectContaining({ modeId: 2, travel: true, discovery: true }),
    ])
  })

  it('preserves exact Filter reference identity when availableModes is unchanged or absent', () => {
    const alignedFilter = stats.Filter.createFrom({
      enabled: true,
      modes: [
        { modeId: 1, travel: false, discovery: true },
        { modeId: 2, travel: true, discovery: true },
      ],
    })

    expect(reconcileModeFilter(alignedFilter, [modeTravel, modeSurvey])).toBe(alignedFilter)
    expect(reconcileModeFilter(alignedFilter, null)).toBe(alignedFilter)
    expect(reconcileModeFilter(alignedFilter, undefined)).toBe(alignedFilter)
  })
})

describe('isExpeditionExportEnabled', () => {
  it('requires activeExpeditionId equality on both plan and sidebarStats branches', () => {
    const completedPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, name: 'Completed Exp', status: 'completed' },
      rows: [],
      modes: [],
    })
    const planningPlan = expedition.PlanView.createFrom({
      expedition: { id: 2, name: 'Planning Exp', status: 'planning' },
      rows: [],
      modes: [],
    })
    const activePlan = expedition.PlanView.createFrom({
      expedition: { id: 2, name: 'Active Exp', status: 'active' },
      rows: [],
      modes: [],
    })
    const departedStats = app.SidebarStats.createFrom({ expeditionId: 1, departed: true })
    const undepartedStats = app.SidebarStats.createFrom({ expeditionId: 2, departed: false })
    const activeDepartedStats = app.SidebarStats.createFrom({ expeditionId: 2, departed: true })

    // Stale completed plan or stale departed stats from expedition 1 must not enable export on expedition 2.
    expect(isExpeditionExportEnabled(2, completedPlan, undepartedStats)).toBe(false)
    expect(isExpeditionExportEnabled(2, planningPlan, departedStats)).toBe(false)
    expect(isExpeditionExportEnabled(0, completedPlan, departedStats)).toBe(false)

    // Matching activeExpeditionId enables export when departed or completed.
    expect(isExpeditionExportEnabled(1, completedPlan, undepartedStats)).toBe(true)
    expect(isExpeditionExportEnabled(2, activePlan, activeDepartedStats)).toBe(true)
  })
})

