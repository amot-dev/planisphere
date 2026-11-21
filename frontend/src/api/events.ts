import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { DataChangedPayload, EddImportProgress } from './models'

/**
 * BackendEventHandlers defines callbacks invoked for real-time Wails runtime
 * notifications emitted by the Go backend.
 */
export interface BackendEventHandlers {
  onDataChanged: (payload?: DataChangedPayload) => void
  onIngestStatus: () => void
  onEddProgress: (progress: EddImportProgress) => void
  onSyncPending?: (pending: boolean) => void
}

/**
 * subscribeBackendEvents registers Wails runtime event listeners for data sync,
 * journal ingestion status, EDDiscovery import progress, and paused live-sync
 * pending indicators, returning an unsubscribe cleanup callback.
 */
export function subscribeBackendEvents(handlers: BackendEventHandlers): () => void {
  if (typeof window === 'undefined' || !window.runtime?.EventsOnMultiple) {
    return () => {}
  }

  const unsubscribeChanged = EventsOn('data:changed', (payload?: DataChangedPayload) => {
    handlers.onDataChanged(payload)
  })
  const unsubscribeIngest = EventsOn('ingest:status', () => {
    handlers.onIngestStatus()
  })
  const unsubscribeEdd = EventsOn('edd:progress', (progress: EddImportProgress) => {
    handlers.onEddProgress(progress)
  })
  const unsubscribeSyncPending = EventsOn('sync:pending', (pending?: boolean) => {
    handlers.onSyncPending?.(Boolean(pending))
  })

  return () => {
    unsubscribeChanged()
    unsubscribeIngest()
    unsubscribeEdd()
    unsubscribeSyncPending()
  }
}

