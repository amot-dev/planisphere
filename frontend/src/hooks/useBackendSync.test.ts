import { describe, expect, it, vi } from 'vitest'
import { stats } from '../../wailsjs/go/models'
import {
  type DataChangedDispatchContext,
  type SyncBootLifecycleState,
  routeDataChangedEvent,
} from './useBackendSync'

function createDispatchHarness(
  bootOverrides: Partial<SyncBootLifecycleState> = {},
  expeditionId = 4,
) {
  const filter = stats.Filter.createFrom({ enabled: false, modes: [] })
  const refreshExpeditionListOnly = vi.fn()
  const loadIngestStatusOnly = vi.fn(async () => null)
  const loadSettingsOnly = vi.fn(async () => null)
  const loadModesOnly = vi.fn(async () => null)
  const loadCoreState = vi.fn(async () => ({ settingsData: null, chosenId: expeditionId }))
  const loadPlanOnly = vi.fn(async () => {})
  const loadStatsOnly = vi.fn(async () => {})
  const refreshAll = vi.fn()

  const dispatchContext: DataChangedDispatchContext = {
    bootState: {
      initialBootComplete: true,
      bootCoreLoaded: true,
      pendingBootRefresh: false,
      ...bootOverrides,
    },
    currentActiveExpeditionId: expeditionId,
    currentFilter: filter,
    refreshExpeditionListOnly,
    loadIngestStatusOnly,
    loadSettingsOnly,
    loadModesOnly,
    loadCoreState,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
  }

  return {
    filter,
    dispatchContext,
    refreshExpeditionListOnly,
    loadIngestStatusOnly,
    loadSettingsOnly,
    loadModesOnly,
    loadCoreState,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
  }
}

describe('routeDataChangedEvent', () => {
  it('ignores events emitted during initial boot before core state loads', () => {
    const {
      dispatchContext,
      refreshExpeditionListOnly,
      loadCoreState,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: false,
      pendingBootRefresh: false,
    })

    routeDataChangedEvent({ scope: 'journal', expeditionId: 0 }, dispatchContext)

    expect(dispatchContext.bootState.pendingBootRefresh).toBe(false)
    expect(refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(loadCoreState).not.toHaveBeenCalled()
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('queues a deferred refresh instead of cancelling boot loaders once core state has loaded', () => {
    const {
      dispatchContext,
      refreshExpeditionListOnly,
      loadCoreState,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: true,
      pendingBootRefresh: false,
    })

    routeDataChangedEvent({ scope: 'plan', expeditionId: 1 }, dispatchContext)

    expect(dispatchContext.bootState.pendingBootRefresh).toBe(true)
    expect(refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(loadCoreState).not.toHaveBeenCalled()
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('dispatches core, plan, and stats loaders in parallel on post-boot journal updates', () => {
    const { filter, dispatchContext, loadCoreState, loadPlanOnly, loadStatsOnly, refreshAll } =
      createDispatchHarness()
    loadCoreState.mockReturnValue(new Promise(() => {}))

    routeDataChangedEvent({ scope: 'journal', expeditionId: 0 }, dispatchContext)

    expect(loadCoreState).toHaveBeenCalledTimes(1)
    expect(loadPlanOnly).toHaveBeenCalledWith(4)
    expect(loadStatsOnly).toHaveBeenCalledWith(4, filter)
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('routes plan, adjustments, inactive expedition, and settings scopes cleanly after boot', () => {
    const planHarness = createDispatchHarness()
    routeDataChangedEvent({ scope: 'plan', expeditionId: 4 }, planHarness.dispatchContext)
    expect(planHarness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(planHarness.loadStatsOnly).not.toHaveBeenCalled()
    expect(planHarness.refreshAll).not.toHaveBeenCalled()

    const adjustmentsHarness = createDispatchHarness()
    routeDataChangedEvent(
      { scope: 'adjustments', expeditionId: 4 },
      adjustmentsHarness.dispatchContext,
    )
    expect(adjustmentsHarness.loadStatsOnly).toHaveBeenCalledWith(4, adjustmentsHarness.filter)
    expect(adjustmentsHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(adjustmentsHarness.refreshAll).not.toHaveBeenCalled()

    const inactiveHarness = createDispatchHarness()
    routeDataChangedEvent({ scope: 'plan', expeditionId: 99 }, inactiveHarness.dispatchContext)
    expect(inactiveHarness.refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(inactiveHarness.loadPlanOnly).not.toHaveBeenCalled()

    const settingsHarness = createDispatchHarness()
    routeDataChangedEvent({ scope: 'settings', expeditionId: 0 }, settingsHarness.dispatchContext)
    expect(settingsHarness.refreshAll).toHaveBeenCalledTimes(1)
  })

  it('routes targeted UI domains without triggering full reloads or unrelated loaders', () => {
    const ingestOnlyHarness = createDispatchHarness()
    routeDataChangedEvent(
      { scope: 'journal', domains: ['ingest'] },
      ingestOnlyHarness.dispatchContext,
    )
    expect(ingestOnlyHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(ingestOnlyHarness.loadSettingsOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadModesOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadCoreState).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.loadStatsOnly).not.toHaveBeenCalled()
    expect(ingestOnlyHarness.refreshAll).not.toHaveBeenCalled()

    const scanHarness = createDispatchHarness()
    routeDataChangedEvent(
      { scope: 'journal', domains: ['ingest', 'stats'] },
      scanHarness.dispatchContext,
    )
    expect(scanHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(scanHarness.loadStatsOnly).toHaveBeenCalledWith(4, scanHarness.filter)
    expect(scanHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(scanHarness.loadSettingsOnly).not.toHaveBeenCalled()

    const arrivalHarness = createDispatchHarness()
    routeDataChangedEvent(
      { scope: 'journal', domains: ['ingest', 'plan', 'stats'] },
      arrivalHarness.dispatchContext,
    )
    expect(arrivalHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(arrivalHarness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(arrivalHarness.loadStatsOnly).toHaveBeenCalledWith(4, arrivalHarness.filter)
    expect(arrivalHarness.loadCoreState).not.toHaveBeenCalled()

    const modesHarness = createDispatchHarness()
    routeDataChangedEvent(
      { scope: 'modes', domains: ['stats', 'plan', 'modes'] },
      modesHarness.dispatchContext,
    )
    expect(modesHarness.loadModesOnly).toHaveBeenCalledTimes(1)
    expect(modesHarness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(modesHarness.loadStatsOnly).toHaveBeenCalledWith(4, modesHarness.filter)
    expect(modesHarness.loadIngestStatusOnly).not.toHaveBeenCalled()

    const expeditionsHarness = createDispatchHarness()
    routeDataChangedEvent(
      { scope: 'expedition', domains: ['stats', 'expeditions'] },
      expeditionsHarness.dispatchContext,
    )
    expect(expeditionsHarness.refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(expeditionsHarness.loadStatsOnly).toHaveBeenCalledWith(4, expeditionsHarness.filter)
    expect(expeditionsHarness.loadPlanOnly).not.toHaveBeenCalled()

    const dbSwitchHarness = createDispatchHarness()
    routeDataChangedEvent(
      {
        scope: 'settings',
        domains: ['ingest', 'stats', 'plan', 'modes', 'settings', 'expeditions'],
      },
      dbSwitchHarness.dispatchContext,
    )
    expect(dbSwitchHarness.refreshAll).toHaveBeenCalledTimes(1)
    expect(dbSwitchHarness.loadIngestStatusOnly).not.toHaveBeenCalled()
    expect(dbSwitchHarness.loadPlanOnly).not.toHaveBeenCalled()

    const dedupHarness = createDispatchHarness({}, 0)
    routeDataChangedEvent(
      { scope: 'journal', domains: ['ingest', 'stats', 'stats', 'ingest', 'settings'] },
      dedupHarness.dispatchContext,
    )
    expect(dedupHarness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(dedupHarness.loadSettingsOnly).toHaveBeenCalledTimes(1)
    expect(dedupHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(dedupHarness.loadStatsOnly).not.toHaveBeenCalled()
  })
})

