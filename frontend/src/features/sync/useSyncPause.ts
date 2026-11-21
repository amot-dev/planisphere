import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { setLiveSyncPaused } from '../../api/backend'
import type { BackendEventHandlers } from '../../api/events'
import type { DataChangedPayload, EddImportProgress } from '../../api/models'
import { routeDataChangedEvent, type DataChangedDispatchContext } from './syncEventRouter'

/**
 * SyncPauseState represents the user-facing pause lifecycle of live backend
 * synchronization.
 */
export type SyncPauseState = 'active' | 'pausing' | 'paused'

/**
 * UseSyncPauseOptions supplies the live status indicator, dispatch context
 * factory, and EDDiscovery progress setter for backend event routing.
 */
export interface UseSyncPauseOptions {
  activeStatusMessage: string | null
  getLiveDispatchContext: () => DataChangedDispatchContext
  applyEddProgress: (progress: EddImportProgress | null) => void
}

/**
 * UseSyncPauseResult exposes the current pause state, waiting-updates flag,
 * idle clock suspension flag, toggle handler, and Wails event handlers.
 */
export interface UseSyncPauseResult {
  syncPauseState: SyncPauseState
  hasPendingSyncUpdates: boolean
  isClockSuspended: boolean
  handleToggleSyncPause: () => void
  eventHandlers: BackendEventHandlers
}

/**
 * useSyncPause coordinates manual backend live-sync pausing to reduce idle
 * system load, suspends CSS animations while the document is hidden, and
 * routes incoming backend events directly to live domain loaders.
 */
export function useSyncPause({
  activeStatusMessage,
  getLiveDispatchContext,
  applyEddProgress,
}: UseSyncPauseOptions): UseSyncPauseResult {
  const [syncPauseState, setSyncPauseState] = useState<SyncPauseState>('active')
  const [hasPendingSyncUpdates, setHasPendingSyncUpdates] = useState<boolean>(false)

  const syncPauseStateRef = useRef<SyncPauseState>('active')
  const activeStatusMessageRef = useRef<string | null>(activeStatusMessage)
  const getLiveDispatchContextRef = useRef(getLiveDispatchContext)
  const applyEddProgressRef = useRef(applyEddProgress)

  activeStatusMessageRef.current = activeStatusMessage
  getLiveDispatchContextRef.current = getLiveDispatchContext
  applyEddProgressRef.current = applyEddProgress

  // Ensure the backend starts unpaused after initial mount or WebView reload,
  // and restore unpaused state if the hook unmounts.
  useEffect(() => {
    setLiveSyncPaused(false)
    return () => {
      setLiveSyncPaused(false)
    }
  }, [])

  // Complete a deferred pause request as soon as all in-flight work finishes.
  useEffect(() => {
    if (syncPauseState === 'pausing' && !activeStatusMessage) {
      syncPauseStateRef.current = 'paused'
      setSyncPauseState('paused')
    }
  }, [activeStatusMessage, syncPauseState])

  // Track document visibility strictly to pause infinite CSS animations while hidden.
  useEffect(() => {
    if (typeof document === 'undefined') {
      return
    }

    const syncHiddenAttribute = () => {
      document.documentElement.dataset.documentHidden = document.hidden ? 'true' : 'false'
    }

    syncHiddenAttribute()
    document.addEventListener('visibilitychange', syncHiddenAttribute)
    return () => {
      document.removeEventListener('visibilitychange', syncHiddenAttribute)
      delete document.documentElement.dataset.documentHidden
    }
  }, [])

  const handleToggleSyncPause = useCallback(() => {
    if (syncPauseStateRef.current === 'active') {
      const nextPauseState: SyncPauseState = activeStatusMessageRef.current
        ? 'pausing'
        : 'paused'
      syncPauseStateRef.current = nextPauseState
      setSyncPauseState(nextPauseState)
      setLiveSyncPaused(true)
      return
    }

    syncPauseStateRef.current = 'active'
    setSyncPauseState('active')
    setHasPendingSyncUpdates(false)
    setLiveSyncPaused(false)
  }, [])

  const onDataChanged = useCallback((payload?: DataChangedPayload) => {
    routeDataChangedEvent(payload, getLiveDispatchContextRef.current())
  }, [])

  const onIngestStatus = useCallback(() => {
    void getLiveDispatchContextRef.current().loadIngestStatusOnly()
  }, [])

  const onEddProgress = useCallback((progress: EddImportProgress) => {
    applyEddProgressRef.current(progress)
  }, [])

  const onSyncPending = useCallback((pending: boolean) => {
    if (!pending) {
      setHasPendingSyncUpdates(false)
      return
    }
    if (syncPauseStateRef.current !== 'active') {
      setHasPendingSyncUpdates(true)
    }
  }, [])

  const eventHandlers = useMemo<BackendEventHandlers>(
    () => ({
      onDataChanged,
      onIngestStatus,
      onEddProgress,
      onSyncPending,
    }),
    [onDataChanged, onEddProgress, onIngestStatus, onSyncPending],
  )

  return {
    syncPauseState,
    hasPendingSyncUpdates,
    isClockSuspended: syncPauseState === 'paused',
    handleToggleSyncPause,
    eventHandlers,
  }
}
