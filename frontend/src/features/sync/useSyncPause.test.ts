// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToString } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { stats, type EddImportProgress } from '../../api/models'
import { SyncStatusButton } from '../../components/ui/SyncStatusButton'
import type { DataChangedDispatchContext } from './syncEventRouter'
import { useSyncPause, type UseSyncPauseResult } from './useSyncPause'

function createLiveDispatchHarness(expeditionId = 4) {
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
      deferredPayloads: [],
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

function mountSyncPauseHook(
  harness: ReturnType<typeof createLiveDispatchHarness>,
  initialActiveStatusMessage: string | null = null,
) {
  const setLiveSyncPausedMock = vi.fn(async () => {})
  window.go = {
    app: {
      App: {
        SetLiveSyncPaused: setLiveSyncPausedMock,
      },
    },
  }

  let latestResult!: UseSyncPauseResult
  let currentStatusMessage = initialActiveStatusMessage

  function HookProbe({ statusMessage }: { statusMessage: string | null }) {
    latestResult = useSyncPause({
      activeStatusMessage: statusMessage,
      getLiveDispatchContext: () => harness.liveContext,
      applyEddProgress: harness.applyEddProgress,
    })
    return null
  }

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  act(() => {
    root.render(createElement(HookProbe, { statusMessage: currentStatusMessage }))
  })

  return {
    setLiveSyncPausedMock,
    getResult: () => latestResult,
    setStatusMessage: (nextStatusMessage: string | null) => {
      currentStatusMessage = nextStatusMessage
      act(() => {
        root.render(createElement(HookProbe, { statusMessage: currentStatusMessage }))
      })
    },
    unmount: () => {
      act(() => {
        root.unmount()
      })
      container.remove()
    },
  }
}

afterEach(() => {
  delete window.go
  delete document.documentElement.dataset.documentHidden
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
})

describe('useSyncPause', () => {
  it('syncs manual pause to backend while keeping explicit user mutations unblocked', () => {
    const harness = createLiveDispatchHarness()
    const mounted = mountSyncPauseHook(harness)

    expect(mounted.getResult().syncPauseState).toBe('active')
    expect(mounted.getResult().isClockSuspended).toBe(false)
    expect(mounted.setLiveSyncPausedMock).toHaveBeenCalledWith(false)
    mounted.setLiveSyncPausedMock.mockClear()

    act(() => {
      mounted.getResult().handleToggleSyncPause()
    })

    expect(mounted.getResult().syncPauseState).toBe('paused')
    expect(mounted.getResult().isClockSuspended).toBe(true)
    expect(mounted.getResult().hasPendingSyncUpdates).toBe(false)
    expect(mounted.setLiveSyncPausedMock).toHaveBeenCalledWith(true)

    // Explicit user mutations (such as AddRow or SetCommanderEnabled) must still route live while PAUSED.
    act(() => {
      mounted.getResult().eventHandlers.onDataChanged({
        domains: ['plan', 'expeditions'],
        expeditionId: 4,
      })
      mounted.getResult().eventHandlers.onDataChanged({
        domains: ['settings'],
      })
    })
    expect(harness.loadPlanOnly).toHaveBeenCalledWith(4)
    expect(harness.refreshExpeditionListOnly).toHaveBeenCalledTimes(1)
    expect(harness.loadSettingsOnly).toHaveBeenCalledTimes(1)

    // Backend watcher pending notification turns the amber waiting flag on until resumed or cleared.
    act(() => {
      mounted.getResult().eventHandlers.onSyncPending?.(true)
    })
    expect(mounted.getResult().hasPendingSyncUpdates).toBe(true)

    act(() => {
      mounted.getResult().handleToggleSyncPause()
    })
    expect(mounted.getResult().syncPauseState).toBe('active')
    expect(mounted.getResult().isClockSuspended).toBe(false)
    expect(mounted.getResult().hasPendingSyncUpdates).toBe(false)
    expect(mounted.setLiveSyncPausedMock).toHaveBeenLastCalledWith(false)

    mounted.unmount()
  })

  it('drains PAUSING into PAUSED when ingest or EDDiscovery tasks finish and supports cancel', () => {
    const harness = createLiveDispatchHarness()
    const mounted = mountSyncPauseHook(harness, 'INGESTING JOURNALS')
    mounted.setLiveSyncPausedMock.mockClear()

    act(() => {
      mounted.getResult().handleToggleSyncPause()
    })
    expect(mounted.getResult().syncPauseState).toBe('pausing')
    expect(mounted.getResult().isClockSuspended).toBe(false)
    expect(mounted.setLiveSyncPausedMock).toHaveBeenCalledWith(true)

    // Completion events are never blocked during PAUSING so activeStatusMessage clears cleanly.
    const completionProgress: EddImportProgress = {
      active: false,
      stage: '',
      current: 0,
      total: 0,
      percent: 0,
      files: 0,
    }
    act(() => {
      mounted.getResult().eventHandlers.onIngestStatus()
      mounted.getResult().eventHandlers.onEddProgress(completionProgress)
    })
    expect(harness.loadIngestStatusOnly).toHaveBeenCalledTimes(1)
    expect(harness.applyEddProgress).toHaveBeenCalledWith(completionProgress)

    mounted.setStatusMessage(null)
    expect(mounted.getResult().syncPauseState).toBe('paused')
    expect(mounted.getResult().isClockSuspended).toBe(true)

    // Re-enter PAUSING while busy and verify clicking again cancels back to READY.
    act(() => {
      mounted.getResult().handleToggleSyncPause()
    })
    mounted.setStatusMessage('COMPUTING TELEMETRY')
    act(() => {
      mounted.getResult().handleToggleSyncPause()
    })
    expect(mounted.getResult().syncPauseState).toBe('pausing')

    act(() => {
      mounted.getResult().handleToggleSyncPause()
    })
    expect(mounted.getResult().syncPauseState).toBe('active')
    expect(mounted.setLiveSyncPausedMock).toHaveBeenLastCalledWith(false)

    mounted.unmount()
  })

  it('toggles data-document-hidden for CSS animation suspension without deferring sync events', () => {
    const harness = createLiveDispatchHarness()
    const mounted = mountSyncPauseHook(harness)

    expect(document.documentElement.dataset.documentHidden).toBe('false')

    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(document.documentElement.dataset.documentHidden).toBe('true')
    expect(mounted.getResult().syncPauseState).toBe('active')

    act(() => {
      mounted.getResult().eventHandlers.onDataChanged({ domains: ['stats'] })
    })
    expect(harness.loadStatsOnly).toHaveBeenCalledWith(4, harness.filter)

    mounted.unmount()
    expect(document.documentElement.dataset.documentHidden).toBeUndefined()
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
