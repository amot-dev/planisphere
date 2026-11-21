import { describe, expect, it, vi } from 'vitest'
import { stats } from '../api/models'
import {
  type DataChangedDispatchContext,
  type SyncBootLifecycleState,
  routeDataChangedEvent,
} from './syncEventRouter'

function createDispatchHarness(
  bootOverrides: Partial<SyncBootLifecycleState> = {},
  expeditionId = 4,
) {
  const filter = stats.Filter.createFrom({ enabled: false, modes: [] })
  const refreshExpeditionListOnly = vi.fn()
  const loadIngestStatusOnly = vi.fn(async () => null)
  const loadSettingsOnly = vi.fn(async () => null)
  const loadModesOnly = vi.fn(async () => null)
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
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: false,
      pendingBootRefresh: false,
    })

    routeDataChangedEvent({ domains: ['ingest', 'plan', 'stats'], expeditionId: 0 }, dispatchContext)

    expect(dispatchContext.bootState.pendingBootRefresh).toBe(false)
    expect(refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(loadPlanOnly).not.toHaveBeenCalled()
    expect(loadStatsOnly).not.toHaveBeenCalled()
    expect(refreshAll).not.toHaveBeenCalled()
  })

  it('queues a deferred refresh instead of cancelling boot loaders once core state has loaded', () => {
    const {
      dispatchContext,
      refreshExpeditionListOnly,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    } = createDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: true,
      pendingBootRefresh: false,
    })

    routeDataChangedEvent({ domains: ['plan'], expeditionId: 1 }, dispatchContext)

    expect(dispatchContext.bootState.pendingBootRefresh).toBe(true)
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
