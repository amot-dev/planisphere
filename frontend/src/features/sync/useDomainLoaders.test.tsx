// @vitest-environment happy-dom
import { act, useRef } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, expedition, modes, stats } from '../../api/models'
import {
  makeMode,
  makePlanView,
  makeSettings,
  makeSidebarStats,
} from '../../test/fixtures'
import { renderHook } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import type { SyncBootLifecycleState } from './syncEventRouter'
import { reconcileModeFilter, useDomainLoaders } from './useDomainLoaders'

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
