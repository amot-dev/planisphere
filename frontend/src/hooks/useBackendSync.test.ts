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
    loadCoreState,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
  }

  return {
    filter,
    dispatchContext,
    refreshExpeditionListOnly,
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
})

