import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { BackendEventHandlers } from '../../api/events'
import type { DataChangedPayload, EddImportProgress } from '../../api/models'
import { routeDataChangedEvent, type DataChangedDispatchContext } from './syncEventRouter'

/**
 * Delay after the document becomes visible before auto-resuming so the user
 * can observe the top-bar status button transition from PAUSED to BUSY or READY.
 */
export const AUTO_RESUME_DELAY_MILLISECONDS = 300

/**
 * SyncPauseState represents the user-facing pause lifecycle of frontend
 * backend-event synchronization.
 */
export type SyncPauseState = 'active' | 'pausing' | 'paused'

/**
 * PendingDeferredSyncState coalesces backend refresh notifications received
 * while frontend synchronization is hidden, pausing, or paused.
 */
export interface PendingDeferredSyncState {
  refreshAll: boolean
  expeditionList: boolean
  ingest: boolean
  settings: boolean
  modes: boolean
  plan: boolean
  stats: boolean
  hasPendingEddProgress: boolean
  latestEddProgress: EddImportProgress | null
}

/**
 * UseSyncPauseOptions supplies the live status indicator, dispatch context
 * factory, and EDDiscovery progress setter needed to defer and flush updates.
 */
export interface UseSyncPauseOptions {
  activeStatusMessage: string | null
  getLiveDispatchContext: () => DataChangedDispatchContext
  applyEddProgress: (progress: EddImportProgress | null) => void
}

/**
 * UseSyncPauseResult exposes the derived pause state, idle clock suspension
 * flag, toggle handler, and Wails event handlers for useBackendSync.
 */
export interface UseSyncPauseResult {
  effectiveSyncPauseState: SyncPauseState
  hasPendingSyncUpdates: boolean
  isClockSuspended: boolean
  handleToggleSyncPause: () => void
  eventHandlers: BackendEventHandlers
}

/**
 * createPendingDeferredSyncState initializes an empty deferred sync queue.
 */
export function createPendingDeferredSyncState(): PendingDeferredSyncState {
  return {
    refreshAll: false,
    expeditionList: false,
    ingest: false,
    settings: false,
    modes: false,
    plan: false,
    stats: false,
    hasPendingEddProgress: false,
    latestEddProgress: null,
  }
}

/**
 * hasPendingDeferredSync returns true when at least one domain reload or
 * EDDiscovery progress update has been queued while deferred.
 */
export function hasPendingDeferredSync(pendingState: PendingDeferredSyncState): boolean {
  return (
    pendingState.refreshAll ||
    pendingState.expeditionList ||
    pendingState.ingest ||
    pendingState.settings ||
    pendingState.modes ||
    pendingState.plan ||
    pendingState.stats ||
    pendingState.hasPendingEddProgress
  )
}

/**
 * createDeferredDispatchContext builds a DataChangedDispatchContext whose
 * loader callbacks record dirty flags into pendingState without issuing IPCs.
 */
export function createDeferredDispatchContext(
  pendingState: PendingDeferredSyncState,
  liveContext: DataChangedDispatchContext,
): DataChangedDispatchContext {
  return {
    bootState: liveContext.bootState,
    currentActiveExpeditionId: liveContext.currentActiveExpeditionId,
    currentFilter: liveContext.currentFilter,
    refreshExpeditionListOnly: () => {
      pendingState.expeditionList = true
    },
    loadIngestStatusOnly: async () => {
      pendingState.ingest = true
      return null
    },
    loadSettingsOnly: async () => {
      pendingState.settings = true
      return null
    },
    loadModesOnly: async () => {
      pendingState.modes = true
      return null
    },
    loadPlanOnly: async () => {
      pendingState.plan = true
    },
    loadStatsOnly: async () => {
      pendingState.stats = true
    },
    refreshAll: () => {
      pendingState.refreshAll = true
    },
  }
}

/**
 * flushPendingDeferredSync resets pendingState and dispatches one coalesced
 * refresh across every dirty domain, returning true when work was flushed.
 */
export function flushPendingDeferredSync(
  pendingState: PendingDeferredSyncState,
  liveContext: DataChangedDispatchContext,
  applyEddProgress: (progress: EddImportProgress | null) => void,
): boolean {
  if (!hasPendingDeferredSync(pendingState)) {
    return false
  }

  const snapshot: PendingDeferredSyncState = { ...pendingState }
  Object.assign(pendingState, createPendingDeferredSyncState())

  if (snapshot.hasPendingEddProgress) {
    applyEddProgress(snapshot.latestEddProgress)
  }

  if (snapshot.refreshAll) {
    liveContext.refreshAll()
    return true
  }

  if (snapshot.ingest) {
    void liveContext.loadIngestStatusOnly()
  }
  if (snapshot.settings) {
    void liveContext.loadSettingsOnly()
  }
  if (snapshot.modes) {
    void liveContext.loadModesOnly()
  }
  if (snapshot.expeditionList) {
    liveContext.refreshExpeditionListOnly()
  }
  if (snapshot.plan && liveContext.currentActiveExpeditionId > 0) {
    void liveContext.loadPlanOnly(liveContext.currentActiveExpeditionId)
  }
  if (snapshot.stats && liveContext.currentActiveExpeditionId > 0) {
    void liveContext.loadStatsOnly(
      liveContext.currentActiveExpeditionId,
      liveContext.currentFilter,
    )
  }
  return true
}

function isDocumentHidden(): boolean {
  return typeof document !== 'undefined' && document.hidden
}

/**
 * useSyncPause coordinates manual pause/resume toggling, automatic deferral
 * while the WebView document is hidden, and single-shot coalesced catch-up.
 */
export function useSyncPause({
  activeStatusMessage,
  getLiveDispatchContext,
  applyEddProgress,
}: UseSyncPauseOptions): UseSyncPauseResult {
  const [syncPauseState, setSyncPauseState] = useState<SyncPauseState>('active')
  const [isAutoPaused, setIsAutoPaused] = useState<boolean>(() => isDocumentHidden())
  const [hasPendingSyncUpdates, setHasPendingSyncUpdates] = useState<boolean>(false)

  const syncPauseStateRef = useRef<SyncPauseState>('active')
  const isAutoPausedRef = useRef<boolean>(isAutoPaused)
  const activeStatusMessageRef = useRef<string | null>(activeStatusMessage)
  const autoResumeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pendingDeferredSyncRef = useRef<PendingDeferredSyncState>(
    createPendingDeferredSyncState(),
  )
  const getLiveDispatchContextRef = useRef(getLiveDispatchContext)
  const applyEddProgressRef = useRef(applyEddProgress)

  activeStatusMessageRef.current = activeStatusMessage
  getLiveDispatchContextRef.current = getLiveDispatchContext
  applyEddProgressRef.current = applyEddProgress

  const clearAutoResumeTimer = useCallback(() => {
    if (autoResumeTimerRef.current !== null) {
      clearTimeout(autoResumeTimerRef.current)
      autoResumeTimerRef.current = null
    }
  }, [])

  const isSyncDeferred = useCallback((): boolean => {
    return (
      isDocumentHidden() ||
      isAutoPausedRef.current ||
      syncPauseStateRef.current !== 'active'
    )
  }, [])

  const flushPendingNow = useCallback(() => {
    flushPendingDeferredSync(
      pendingDeferredSyncRef.current,
      getLiveDispatchContextRef.current(),
      applyEddProgressRef.current,
    )
    setHasPendingSyncUpdates(false)
  }, [])

  // Complete a deferred pause request as soon as all in-flight work finishes.
  useEffect(() => {
    if (syncPauseState === 'pausing' && !activeStatusMessage) {
      syncPauseStateRef.current = 'paused'
      setSyncPauseState('paused')
    }
  }, [activeStatusMessage, syncPauseState])

  // Track document visibility to pause CSS animations, auto-pause sync while
  // hidden, and briefly hold PAUSED before flushing on return to visible.
  useEffect(() => {
    if (typeof document === 'undefined') {
      return
    }

    const syncHiddenAttribute = (hidden: boolean) => {
      document.documentElement.dataset.documentHidden = hidden ? 'true' : 'false'
    }

    syncHiddenAttribute(document.hidden)

    const handleVisibilityChange = () => {
      const hidden = document.hidden
      syncHiddenAttribute(hidden)

      if (hidden) {
        clearAutoResumeTimer()
        isAutoPausedRef.current = true
        setIsAutoPaused(true)
        return
      }

      if (syncPauseStateRef.current !== 'active') {
        clearAutoResumeTimer()
        isAutoPausedRef.current = false
        setIsAutoPaused(false)
        return
      }

      if (!isAutoPausedRef.current) {
        return
      }

      clearAutoResumeTimer()
      autoResumeTimerRef.current = setTimeout(() => {
        autoResumeTimerRef.current = null
        if (document.hidden || syncPauseStateRef.current !== 'active') {
          return
        }
        isAutoPausedRef.current = false
        setIsAutoPaused(false)
        flushPendingNow()
      }, AUTO_RESUME_DELAY_MILLISECONDS)
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      clearAutoResumeTimer()
      delete document.documentElement.dataset.documentHidden
    }
  }, [clearAutoResumeTimer, flushPendingNow])

  const handleToggleSyncPause = useCallback(() => {
    // Clicking PAUSED during the brief auto-resume beat resumes immediately.
    if (syncPauseStateRef.current === 'active' && isAutoPausedRef.current) {
      clearAutoResumeTimer()
      isAutoPausedRef.current = false
      setIsAutoPaused(false)
      if (!isDocumentHidden()) {
        flushPendingNow()
      }
      return
    }

    if (syncPauseStateRef.current === 'active') {
      const nextPauseState: SyncPauseState = activeStatusMessageRef.current
        ? 'pausing'
        : 'paused'
      syncPauseStateRef.current = nextPauseState
      setSyncPauseState(nextPauseState)
      return
    }

    syncPauseStateRef.current = 'active'
    setSyncPauseState('active')
    if (!isDocumentHidden()) {
      clearAutoResumeTimer()
      isAutoPausedRef.current = false
      setIsAutoPaused(false)
      flushPendingNow()
    }
  }, [clearAutoResumeTimer, flushPendingNow])

  const onDataChanged = useCallback(
    (payload?: DataChangedPayload) => {
      const liveContext = getLiveDispatchContextRef.current()
      if (!isSyncDeferred()) {
        routeDataChangedEvent(payload, liveContext)
        return
      }
      routeDataChangedEvent(
        payload,
        createDeferredDispatchContext(pendingDeferredSyncRef.current, liveContext),
      )
      if (hasPendingDeferredSync(pendingDeferredSyncRef.current)) {
        setHasPendingSyncUpdates(true)
      }
    },
    [isSyncDeferred],
  )

  const onIngestStatus = useCallback(() => {
    if (!isSyncDeferred()) {
      void getLiveDispatchContextRef.current().loadIngestStatusOnly()
      return
    }
    pendingDeferredSyncRef.current.ingest = true
    setHasPendingSyncUpdates(true)
  }, [isSyncDeferred])

  const onEddProgress = useCallback(
    (progress: EddImportProgress) => {
      if (!isSyncDeferred()) {
        applyEddProgressRef.current(progress)
        return
      }
      pendingDeferredSyncRef.current.hasPendingEddProgress = true
      pendingDeferredSyncRef.current.latestEddProgress = progress
      setHasPendingSyncUpdates(true)
    },
    [isSyncDeferred],
  )

  const eventHandlers = useMemo<BackendEventHandlers>(
    () => ({
      onDataChanged,
      onIngestStatus,
      onEddProgress,
    }),
    [onDataChanged, onIngestStatus, onEddProgress],
  )

  const effectiveSyncPauseState: SyncPauseState =
    syncPauseState === 'active' && isAutoPaused ? 'paused' : syncPauseState

  return {
    effectiveSyncPauseState,
    hasPendingSyncUpdates,
    isClockSuspended: isAutoPaused || syncPauseState === 'paused',
    handleToggleSyncPause,
    eventHandlers,
  }
}

