import { describe, expect, it } from 'vitest'
import { createSyncDispatchHarness } from '../../test/fixtures'
import { flushDeferredBootEvents, routeDataChangedEvent } from './syncEventRouter'

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

  it.each<{
    caseLabel: string
    payload: Parameters<typeof routeDataChangedEvent>[0]
    activeExpeditionId?: number
    expectedLoaders: Array<
      'ingest' | 'settings' | 'modes' | 'expeditions' | 'plan' | 'stats' | 'refreshAll'
    >
  }>([
    {
      caseLabel: 'ingest only',
      payload: { domains: ['ingest'] },
      expectedLoaders: ['ingest'],
    },
    {
      caseLabel: 'scan ingest and stats',
      payload: { domains: ['ingest', 'stats'] },
      expectedLoaders: ['ingest', 'stats'],
    },
    {
      caseLabel: 'arrival ingest, plan, and stats',
      payload: { domains: ['ingest', 'plan', 'stats'] },
      expectedLoaders: ['ingest', 'plan', 'stats'],
    },
    {
      caseLabel: 'modes, plan, and stats',
      payload: { domains: ['stats', 'plan', 'modes'] },
      expectedLoaders: ['modes', 'plan', 'stats'],
    },
    {
      caseLabel: 'expeditions and stats',
      payload: { domains: ['stats', 'expeditions'] },
      expectedLoaders: ['expeditions', 'stats'],
    },
    {
      caseLabel: 'full database switch across all domains',
      payload: {
        domains: ['ingest', 'stats', 'plan', 'modes', 'settings', 'expeditions'],
      },
      expectedLoaders: ['refreshAll'],
    },
    {
      caseLabel: 'duplicate domains with no active expedition',
      payload: { domains: ['ingest', 'stats', 'stats', 'ingest', 'settings'] },
      activeExpeditionId: 0,
      expectedLoaders: ['ingest', 'settings'],
    },
    {
      caseLabel: 'inactive expeditionId preserving global domains',
      payload: { expeditionId: 99, domains: ['settings', 'plan'] },
      expectedLoaders: ['settings', 'expeditions'],
    },
    {
      caseLabel: 'active expedition deletion notification',
      payload: { expeditionId: 0, domains: ['settings', 'expeditions'] },
      expectedLoaders: ['settings', 'expeditions'],
    },
  ])(
    'routes $caseLabel selectively without unrelated reloads',
    ({ payload, activeExpeditionId = 4, expectedLoaders }) => {
      const harness = createDispatchHarness({}, activeExpeditionId)
      const expectedSet = new Set(expectedLoaders)

      routeDataChangedEvent(payload, harness.dispatchContext)

      expect(harness.loadIngestStatusOnly).toHaveBeenCalledTimes(expectedSet.has('ingest') ? 1 : 0)
      expect(harness.loadSettingsOnly).toHaveBeenCalledTimes(expectedSet.has('settings') ? 1 : 0)
      expect(harness.loadModesOnly).toHaveBeenCalledTimes(expectedSet.has('modes') ? 1 : 0)
      expect(harness.refreshExpeditionListOnly).toHaveBeenCalledTimes(
        expectedSet.has('expeditions') ? 1 : 0,
      )
      if (expectedSet.has('plan')) {
        expect(harness.loadPlanOnly).toHaveBeenCalledTimes(1)
        expect(harness.loadPlanOnly).toHaveBeenCalledWith(activeExpeditionId)
      } else {
        expect(harness.loadPlanOnly).not.toHaveBeenCalled()
      }
      if (expectedSet.has('stats')) {
        expect(harness.loadStatsOnly).toHaveBeenCalledTimes(1)
        expect(harness.loadStatsOnly).toHaveBeenCalledWith(activeExpeditionId, harness.filter)
      } else {
        expect(harness.loadStatsOnly).not.toHaveBeenCalled()
      }
      if (expectedSet.has('refreshAll')) {
        expect(harness.refreshAll).toHaveBeenCalledTimes(1)
        expect(harness.refreshAll).toHaveBeenCalledWith({ skipPrefetch: true })
      } else {
        expect(harness.refreshAll).not.toHaveBeenCalled()
      }
    },
  )
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
