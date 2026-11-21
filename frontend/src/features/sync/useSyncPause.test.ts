import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { stats, type EddImportProgress } from '../../api/models'
import { SyncStatusButton } from '../../components/ui/SyncStatusButton'
import {
  routeDataChangedEvent,
  type DataChangedDispatchContext,
  type SyncBootLifecycleState,
} from './syncEventRouter'
import {
  createDeferredDispatchContext,
  createPendingDeferredSyncState,
  flushPendingDeferredSync,
  hasPendingDeferredSync,
} from './useSyncPause'

function createLiveDispatchHarness(
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
  const applyEddProgress = vi.fn()

  const liveContext: DataChangedDispatchContext = {
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
    liveContext,
    refreshExpeditionListOnly,
    loadIngestStatusOnly,
    loadSettingsOnly,
    loadModesOnly,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
    applyEddProgress,
  }
}

describe('deferred sync coalescing queue', () => {
  it('coalesces repeated domain notifications into one RPC per dirty domain on flush', () => {
    const harness = createLiveDispatchHarness()
    const pendingState = createPendingDeferredSyncState()
    const deferredContext = createDeferredDispatchContext(pendingState, harness.liveContext)

    routeDataChangedEvent({ domains: ['ingest', 'stats'] }, deferredContext)
    routeDataChangedEvent({ domains: ['ingest', 'stats'] }, deferredContext)
    routeDataChangedEvent({ domains: ['ingest', 'plan', 'stats'] }, deferredContext)
    routeDataChangedEvent({ domains: ['modes'] }, deferredContext)

    expect(hasPendingDeferredSync(pendingState)).toBe(true)
    expect(harness.loadIngestStatusOnly).not.toHaveBeenCalled()
    expect(harness.loadPlanOnly).not.toHaveBeenCalled()
    expect(harness.loadStatsOnly).not.toHaveBeenCalled()
    expect(harness.loadModesOnly).not.toHaveBeenCalled()

    const didFlush = flushPendingDeferredSync(
      pendingState,
      harness.liveContext,
      harness.applyEddProgress,
    )
    expect(didFlush).toBe(true)
    expect(harness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadPlanOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(harness.loadStatsOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadStatsOnly).toHaveBeenCalledWith(4, harness.filter)
    expect(harness.loadModesOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadSettingsOnly).not.toHaveBeenCalled()
    expect(harness.refreshExpeditionListOnly).not.toHaveBeenCalled()
    expect(harness.refreshAll).not.toHaveBeenCalled()

    // Queue resets cleanly so a subsequent flush is a strict no-op.
    expect(hasPendingDeferredSync(pendingState)).toBe(false)
    expect(
      flushPendingDeferredSync(pendingState, harness.liveContext, harness.applyEddProgress),
    ).toBe(false)
  })

  it('subsumes narrower domain flags when refreshAll is queued', () => {
    const harness = createLiveDispatchHarness()
    const pendingState = createPendingDeferredSyncState()
    const deferredContext = createDeferredDispatchContext(pendingState, harness.liveContext)

    routeDataChangedEvent({ domains: ['ingest', 'plan', 'stats'] }, deferredContext)
    routeDataChangedEvent(
      { domains: ['settings', 'expeditions', 'modes'] },
      deferredContext,
    )

    expect(
      flushPendingDeferredSync(pendingState, harness.liveContext, harness.applyEddProgress),
    ).toBe(true)
    expect(harness.refreshAll).toHaveBeenCalledTimes(1)
    expect(harness.loadIngestStatusOnly).not.toHaveBeenCalled()
    expect(harness.loadPlanOnly).not.toHaveBeenCalled()
    expect(harness.loadStatsOnly).not.toHaveBeenCalled()
  })

  it('preserves inactive-expedition filtering and pre-boot lifecycle guards while deferred', () => {
    const preBootHarness = createLiveDispatchHarness({
      initialBootComplete: false,
      bootCoreLoaded: true,
      pendingBootRefresh: false,
    })
    const preBootPending = createPendingDeferredSyncState()
    routeDataChangedEvent(
      { domains: ['plan', 'stats'], expeditionId: 4 },
      createDeferredDispatchContext(preBootPending, preBootHarness.liveContext),
    )
    expect(preBootHarness.liveContext.bootState.pendingBootRefresh).toBe(true)
    expect(hasPendingDeferredSync(preBootPending)).toBe(false)

    const inactiveHarness = createLiveDispatchHarness()
    const inactivePending = createPendingDeferredSyncState()
    routeDataChangedEvent(
      { expeditionId: 99, domains: ['settings', 'plan', 'stats'] },
      createDeferredDispatchContext(inactivePending, inactiveHarness.liveContext),
    )

    expect(
      flushPendingDeferredSync(
        inactivePending,
        inactiveHarness.liveContext,
        inactiveHarness.applyEddProgress,
      ),
    ).toBe(true)
    expect(inactiveHarness.loadSettingsOnly).toHaveBeenCalledTimes(1)
    expect(inactiveHarness.refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(inactiveHarness.loadPlanOnly).not.toHaveBeenCalled()
    expect(inactiveHarness.loadStatsOnly).not.toHaveBeenCalled()
  })

  it('flushes the latest coalesced EDDiscovery progress payload and ignores empty queues', () => {
    const harness = createLiveDispatchHarness()
    const pendingState = createPendingDeferredSyncState()

    expect(
      flushPendingDeferredSync(pendingState, harness.liveContext, harness.applyEddProgress),
    ).toBe(false)

    const latestProgress: EddImportProgress = {
      active: true,
      stage: 'Importing',
      current: 500,
      total: 1000,
      percent: 50,
      files: 12,
    }
    pendingState.hasPendingEddProgress = true
    pendingState.latestEddProgress = latestProgress

    expect(
      flushPendingDeferredSync(pendingState, harness.liveContext, harness.applyEddProgress),
    ).toBe(true)
    expect(harness.applyEddProgress).toHaveBeenCalledTimes(1)
    expect(harness.applyEddProgress).toHaveBeenCalledWith(latestProgress)
  })
})

describe('SyncStatusButton', () => {
  it('renders READY, BUSY, PAUSING (preserving busy task tooltip), and PAUSED states', () => {
    const readyHtml = renderToString(
      createElement(SyncStatusButton, {
        activeStatusMessage: null,
        syncPauseState: 'active',
        hasPendingSyncUpdates: false,
        onToggleSyncPause: () => {},
      }),
    )
    expect(readyHtml).toContain('READY')
    expect(readyHtml).toContain('aria-pressed="false"')
    expect(readyHtml).toContain('Live sync active. Click to pause.')

    const busyHtml = renderToString(
      createElement(SyncStatusButton, {
        activeStatusMessage: 'COMPUTING TELEMETRY',
        syncPauseState: 'active',
        hasPendingSyncUpdates: false,
        onToggleSyncPause: () => {},
      }),
    )
    expect(busyHtml).toContain('BUSY')
    expect(busyHtml).toContain('Background task: COMPUTING TELEMETRY. Click to pause once ready.')

    const pausingHtml = renderToString(
      createElement(SyncStatusButton, {
        activeStatusMessage: 'COMPUTING TELEMETRY',
        syncPauseState: 'pausing',
        hasPendingSyncUpdates: false,
        onToggleSyncPause: () => {},
      }),
    )
    expect(pausingHtml).toContain('PAUSING')
    expect(pausingHtml).toContain('aria-pressed="true"')
    expect(pausingHtml).toContain(
      'Background task: COMPUTING TELEMETRY. Will pause once ready (click to cancel).',
    )

    const pausedPendingHtml = renderToString(
      createElement(SyncStatusButton, {
        activeStatusMessage: null,
        syncPauseState: 'paused',
        hasPendingSyncUpdates: true,
        onToggleSyncPause: () => {},
      }),
    )
    expect(pausedPendingHtml).toContain('PAUSED')
    expect(pausedPendingHtml).toContain('aria-pressed="true"')
    expect(pausedPendingHtml).toContain('bg-ed-amber')
    expect(pausedPendingHtml).toContain(
      'Live sync paused (updates waiting). Click to resume and refresh.',
    )
  })
})

