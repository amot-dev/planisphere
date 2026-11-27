import { describe, expect, it, vi } from 'vitest'
import { installWailsRuntimeMock, resetWailsMocks } from '../test/wailsMock'
import { subscribeBackendEvents } from './events'
import type { DataChangedPayload, EddImportProgress } from './models'

describe('subscribeBackendEvents', () => {
  it('returns a safe no-op teardown callback when window or Wails runtime is absent', () => {
    resetWailsMocks()
    const unsubscribeWithoutWindow = subscribeBackendEvents({
      onDataChanged: vi.fn(),
      onIngestStatus: vi.fn(),
      onEddProgress: vi.fn(),
    })
    expect(() => unsubscribeWithoutWindow()).not.toThrow()

    const globalRecord = globalThis as { window?: Window & typeof globalThis }
    const hadWindow = Object.prototype.hasOwnProperty.call(globalRecord, 'window')
    const previousWindow = globalRecord.window
    globalRecord.window = {} as Window & typeof globalThis
    try {
      const unsubscribeWithoutRuntime = subscribeBackendEvents({
        onDataChanged: vi.fn(),
        onIngestStatus: vi.fn(),
        onEddProgress: vi.fn(),
      })
      expect(() => unsubscribeWithoutRuntime()).not.toThrow()
    } finally {
      if (hadWindow && previousWindow !== undefined) {
        globalRecord.window = previousWindow
      } else {
        delete globalRecord.window
      }
    }
  })

  it('routes data:changed, ingest:status, edd:progress, and sync:pending events and unsubscribes cleanly on teardown', () => {
    const runtimeMock = installWailsRuntimeMock()
    const onDataChanged = vi.fn()
    const onIngestStatus = vi.fn()
    const onEddProgress = vi.fn()
    const onSyncPending = vi.fn()

    const unsubscribe = subscribeBackendEvents({
      onDataChanged,
      onIngestStatus,
      onEddProgress,
      onSyncPending,
    })

    expect(runtimeMock.getListenerCount()).toBe(4)
    expect(runtimeMock.getListenerCount('data:changed')).toBe(1)
    expect(runtimeMock.getListenerCount('ingest:status')).toBe(1)
    expect(runtimeMock.getListenerCount('edd:progress')).toBe(1)
    expect(runtimeMock.getListenerCount('sync:pending')).toBe(1)

    const changedPayload: DataChangedPayload = { domains: ['plan', 'stats'], expeditionId: 7 }
    const progressPayload: EddImportProgress = {
      active: true,
      stage: 'Importing events',
      current: 400,
      total: 1000,
      percent: 40,
      files: 6,
    }

    runtimeMock.emitDataChanged(changedPayload)
    runtimeMock.emitIngestStatus()
    runtimeMock.emitEddProgress(progressPayload)
    runtimeMock.emitSyncPending(true)
    runtimeMock.emit('sync:pending')

    expect(onDataChanged).toHaveBeenCalledWith(changedPayload)
    expect(onIngestStatus).toHaveBeenCalledTimes(1)
    expect(onEddProgress).toHaveBeenCalledWith(progressPayload)
    expect(onSyncPending).toHaveBeenNthCalledWith(1, true)
    expect(onSyncPending).toHaveBeenNthCalledWith(2, false)

    unsubscribe()
    expect(runtimeMock.getListenerCount()).toBe(0)

    runtimeMock.emitDataChanged(changedPayload)
    runtimeMock.emitIngestStatus()
    runtimeMock.emitEddProgress(progressPayload)
    runtimeMock.emitSyncPending(true)
    expect(onDataChanged).toHaveBeenCalledTimes(1)
    expect(onIngestStatus).toHaveBeenCalledTimes(1)
    expect(onEddProgress).toHaveBeenCalledTimes(1)
    expect(onSyncPending).toHaveBeenCalledTimes(2)
  })
})
