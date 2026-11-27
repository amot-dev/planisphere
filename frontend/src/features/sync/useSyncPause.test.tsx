// @vitest-environment happy-dom
import { act } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import type { EddImportProgress } from '../../api/models'
import { createSyncDispatchHarness } from '../../test/fixtures'
import { renderHook } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { useSyncPause } from './useSyncPause'

function mountSyncPauseHook(
  harness: ReturnType<typeof createSyncDispatchHarness>,
  initialActiveStatusMessage: string | null = null,
) {
  const { spies } = installWailsAppMock({
    SetLiveSyncPaused: async () => {},
  })

  const mountedHook = renderHook(
    ({ statusMessage }: { statusMessage: string | null }) =>
      useSyncPause({
        activeStatusMessage: statusMessage,
        getLiveDispatchContext: () => harness.dispatchContext,
        applyEddProgress: harness.applyEddProgress,
      }),
    { statusMessage: initialActiveStatusMessage },
  )

  return {
    setLiveSyncPausedMock: spies.SetLiveSyncPaused,
    getResult: mountedHook.getResult,
    setStatusMessage: (nextStatusMessage: string | null) => {
      mountedHook.rerender({ statusMessage: nextStatusMessage })
    },
    unmount: mountedHook.unmount,
  }
}

afterEach(() => {
  delete document.documentElement.dataset.documentHidden
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
})

describe('useSyncPause', () => {
  it('syncs manual pause to backend while keeping explicit user mutations unblocked', () => {
    const harness = createSyncDispatchHarness()
    const mounted = mountSyncPauseHook(harness)

    try {
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
    } finally {
      mounted.unmount()
    }
  })

  it('drains PAUSING into PAUSED when ingest or EDDiscovery tasks finish and supports cancel', () => {
    const harness = createSyncDispatchHarness()
    const mounted = mountSyncPauseHook(harness, 'INGESTING JOURNALS')

    try {
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
    } finally {
      mounted.unmount()
    }
  })

  it('toggles data-document-hidden for CSS animation suspension without deferring sync events', () => {
    const harness = createSyncDispatchHarness()
    const mounted = mountSyncPauseHook(harness)

    try {
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
    } finally {
      mounted.unmount()
    }
  })
})
