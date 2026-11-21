import { useCallback, useEffect, useRef, useState } from 'react'
import { SetActiveExpedition, SwitchMode } from '../../api/backend'
import { subscribeBackendEvents } from '../../api/events'
import { stats, type EddImportProgress } from '../../api/models'
import {
  routeDataChangedEvent,
  type DataChangedDispatchContext,
  type SyncBootLifecycleState,
} from './syncEventRouter'
import { useDomainLoaders } from './useDomainLoaders'
import { useSyncPause } from './useSyncPause'

export {
  routeDataChangedEvent,
  type DataChangedDispatchContext,
  type SyncBootLifecycleState,
}

/**
 * useBackendSync manages terminal bootstrap state, Wails IPC queries for
 * settings, expeditions, plans, and telemetry, and live runtime event subscriptions.
 */
export function useBackendSync() {
  const [booting, setBooting] = useState<boolean>(true)
  const [busyAction, setBusyAction] = useState<string | null>('INITIALIZING TERMINAL')
  const [filter, setFilter] = useState<stats.Filter>(() =>
    stats.Filter.createFrom({ enabled: false, modes: [] }),
  )
  const [eddProgress, setEddProgress] = useState<EddImportProgress | null>(null)

  const bootStateRef = useRef<SyncBootLifecycleState>({
    initialBootComplete: false,
    bootCoreLoaded: false,
    pendingBootRefresh: false,
  })
  const lastEffectPlanExpeditionIdRef = useRef(0)
  const lastEffectStatsSelectionRef = useRef<{
    expeditionId: number
    filter: stats.Filter
  } | null>(null)
  const filterRef = useRef(filter)

  useEffect(() => {
    filterRef.current = filter
  }, [filter])

  const {
    statsLoading,
    settings,
    ingest,
    modesState,
    setModesState,
    expeditions,
    activeExpeditionId,
    setActiveExpeditionId,
    activeExpeditionIdRef,
    plan,
    setPlan,
    sidebarStats,
    loadIngestStatusOnly,
    loadSettingsOnly,
    loadModesOnly,
    refreshExpeditionListOnly,
    loadCoreState,
    loadPlanOnly,
    loadStatsOnly,
    refreshAll,
  } = useDomainLoaders({
    filterRef,
    bootStateRef,
    lastEffectPlanExpeditionIdRef,
  })

  useEffect(() => {
    activeExpeditionIdRef.current = activeExpeditionId
  }, [activeExpeditionId, activeExpeditionIdRef])

  // Initial boot sequence: load core state, active expedition plan, and sidebar
  // telemetry before dismissing the boot screen so the main cockpit UI never
  // paints in an unready state.
  useEffect(() => {
    let mounted = true
    bootStateRef.current.bootCoreLoaded = false
    setBusyAction('INITIALIZING TERMINAL')
    ;(async () => {
      try {
        const { settingsData, chosenId } = await loadCoreState()
        if (!mounted) {
          return
        }
        bootStateRef.current.bootCoreLoaded = true
        lastEffectPlanExpeditionIdRef.current = chosenId
        lastEffectStatsSelectionRef.current =
          chosenId > 0 ? { expeditionId: chosenId, filter: filterRef.current } : null

        if (settingsData?.setupCompleted && chosenId > 0) {
          await Promise.all([
            loadPlanOnly(chosenId),
            loadStatsOnly(chosenId, filterRef.current),
          ])
        }
      } finally {
        if (mounted) {
          bootStateRef.current.initialBootComplete = true
          setBooting(false)
          setBusyAction(null)
          if (bootStateRef.current.pendingBootRefresh) {
            bootStateRef.current.pendingBootRefresh = false
            refreshAll()
          }
        }
      }
    })()
    return () => {
      mounted = false
    }
  }, [loadCoreState, loadPlanOnly, loadStatsOnly, refreshAll])

  // Load plan immediately when expedition changes after initial boot.
  useEffect(() => {
    if (!bootStateRef.current.initialBootComplete) return
    if (activeExpeditionId <= 0) {
      lastEffectPlanExpeditionIdRef.current = 0
      return
    }
    if (lastEffectPlanExpeditionIdRef.current === activeExpeditionId) {
      return
    }
    lastEffectPlanExpeditionIdRef.current = activeExpeditionId
    void loadPlanOnly(activeExpeditionId)
  }, [activeExpeditionId, loadPlanOnly])

  // Reload sidebar stats when expedition or filter changes after initial boot.
  useEffect(() => {
    if (!bootStateRef.current.initialBootComplete) return
    if (activeExpeditionId <= 0) {
      lastEffectStatsSelectionRef.current = null
      return
    }
    if (
      lastEffectStatsSelectionRef.current?.expeditionId === activeExpeditionId &&
      lastEffectStatsSelectionRef.current?.filter === filter
    ) {
      return
    }
    lastEffectStatsSelectionRef.current = { expeditionId: activeExpeditionId, filter }
    void loadStatsOnly(activeExpeditionId, filter)
  }, [activeExpeditionId, filter, loadStatsOnly])

  const activeStatusMessage =
    busyAction ||
    (eddProgress?.active ? `EDD IMPORT ${eddProgress.percent}%` : null) ||
    (ingest?.scanning ? 'INGESTING JOURNALS' : null) ||
    (statsLoading ? 'COMPUTING TELEMETRY' : null)

  const getLiveDispatchContext = useCallback(
    (): DataChangedDispatchContext => ({
      bootState: bootStateRef.current,
      currentActiveExpeditionId: activeExpeditionIdRef.current,
      currentFilter: filterRef.current,
      refreshExpeditionListOnly,
      loadIngestStatusOnly,
      loadSettingsOnly,
      loadModesOnly,
      loadPlanOnly,
      loadStatsOnly,
      refreshAll,
    }),
    [
      activeExpeditionIdRef,
      loadIngestStatusOnly,
      loadModesOnly,
      loadPlanOnly,
      loadSettingsOnly,
      loadStatsOnly,
      refreshAll,
      refreshExpeditionListOnly,
    ],
  )

  const {
    effectiveSyncPauseState,
    hasPendingSyncUpdates,
    isClockSuspended,
    handleToggleSyncPause,
    eventHandlers,
  } = useSyncPause({
    activeStatusMessage,
    getLiveDispatchContext,
    applyEddProgress: setEddProgress,
  })

  // Subscribe to Wails backend events (live fsnotify journal tailing, coordinate resolution, and mode changes).
  useEffect(() => subscribeBackendEvents(eventHandlers), [eventHandlers])

  const handleSelectExpedition = useCallback(
    async (selectedExpeditionId: number) => {
      if (!selectedExpeditionId) return
      setActiveExpeditionId(selectedExpeditionId)
      activeExpeditionIdRef.current = selectedExpeditionId
      try {
        await SetActiveExpedition(selectedExpeditionId)
      } catch (setActiveError) {
        console.error('Failed to set active expedition:', setActiveError)
      }
    },
    [activeExpeditionIdRef, setActiveExpeditionId],
  )

  const handleQuickSwitchMode = useCallback(
    async (modeId: number) => {
      setBusyAction('SWITCHING FLIGHT MODE')
      try {
        const nextModesState = await SwitchMode(modeId)
        setModesState(nextModesState)
      } finally {
        setBusyAction(null)
      }
    },
    [setModesState],
  )

  const handleRefreshPlan = useCallback(() => {
    if (activeExpeditionIdRef.current > 0) {
      void loadPlanOnly(activeExpeditionIdRef.current)
    }
  }, [activeExpeditionIdRef, loadPlanOnly])

  return {
    booting,
    setBusyAction,
    statsLoading,
    settings,
    ingest,
    modesState,
    setModesState,
    expeditions,
    activeExpeditionId,
    plan,
    setPlan,
    sidebarStats,
    filter,
    setFilter,
    eddProgress,
    activeStatusMessage,
    syncPauseState: effectiveSyncPauseState,
    hasPendingSyncUpdates,
    isClockSuspended,
    handleToggleSyncPause,
    handleSelectExpedition,
    handleQuickSwitchMode,
    refreshAll,
    handleRefreshPlan,
    refreshSettings: loadSettingsOnly,
  }
}

