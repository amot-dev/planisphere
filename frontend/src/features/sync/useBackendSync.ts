import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { SetActiveExpedition } from '../../api/backend'
import { subscribeBackendEvents } from '../../api/events'
import { app, expedition, stats, type EddImportProgress } from '../../api/models'
import { useBusyTasks } from '../../hooks/useBusyTasks'
import {
  flushDeferredBootEvents,
  type DataChangedDispatchContext,
  type SyncBootLifecycleState,
} from './syncEventRouter'
import { reconcileModeFilter, useDomainLoaders } from './useDomainLoaders'
import { useSyncPause } from './useSyncPause'

/**
 * isExpeditionExportEnabled checks whether export is available for the active
 * expedition, ignoring stale plan or sidebar telemetry from a previously
 * selected expedition while replacement RPCs are in flight.
 */
export function isExpeditionExportEnabled(
  activeExpeditionId: number,
  plan: expedition.PlanView | null | undefined,
  sidebarStats: app.SidebarStats | null | undefined,
): boolean {
  const isPlanForActive = Boolean(
    activeExpeditionId > 0 && plan?.expedition?.id === activeExpeditionId,
  )
  const isStatsForActive = Boolean(
    activeExpeditionId > 0 && sidebarStats?.expeditionId === activeExpeditionId,
  )
  return (
    (isStatsForActive && Boolean(sidebarStats?.departed)) ||
    (isPlanForActive && plan?.expedition?.status === 'completed')
  )
}

/**
 * useBackendSync manages terminal bootstrap state, Wails IPC queries for
 * settings, expeditions, plans, and telemetry, and live runtime event subscriptions.
 */
export function useBackendSync() {
  const [booting, setBooting] = useState<boolean>(true)
  const { busyAction, beginBusyTask, setBusyAction } = useBusyTasks('INITIALIZING TERMINAL')
  const [filter, setFilter] = useState<stats.Filter>(() =>
    stats.Filter.createFrom({ enabled: false, modes: [] }),
  )
  const [eddProgress, setEddProgress] = useState<EddImportProgress | null>(null)

  const bootStateRef = useRef<SyncBootLifecycleState>({
    initialBootComplete: false,
    bootCoreLoaded: false,
    deferredPayloads: [],
  })
  const lastEffectPlanExpeditionIdRef = useRef(0)
  const lastEffectStatsSelectionRef = useRef<{
    expeditionId: number
    filter: stats.Filter
  } | null>(null)
  const filterRef = useRef(filter)

  const handleRefreshAllFilterReconciled = useCallback(
    (nextFilter: stats.Filter, chosenId: number, willTransitionEffectReload: boolean) => {
      setFilter((currentFilter) => (currentFilter === nextFilter ? currentFilter : nextFilter))
      if (!willTransitionEffectReload && chosenId > 0) {
        lastEffectStatsSelectionRef.current = {
          expeditionId: chosenId,
          filter: nextFilter,
        }
      }
    },
    [],
  )

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
    onRefreshAllFilterReconciled: handleRefreshAllFilterReconciled,
  })

  const reconciledFilter = useMemo(
    () => reconcileModeFilter(filter, modesState?.modes),
    [filter, modesState?.modes],
  )
  filterRef.current = reconciledFilter

  useEffect(() => {
    if (reconciledFilter !== filter) {
      setFilter(reconciledFilter)
    }
  }, [filter, reconciledFilter])

  useEffect(() => {
    activeExpeditionIdRef.current = activeExpeditionId
  }, [activeExpeditionId, activeExpeditionIdRef])

  // Initial boot sequence: load core state, active expedition plan, and sidebar
  // telemetry before dismissing the boot screen so the main cockpit UI never
  // paints in an unready state.
  useEffect(() => {
    let mounted = true
    bootStateRef.current.bootCoreLoaded = false
    bootStateRef.current.deferredPayloads.length = 0
    const endBootTask = beginBusyTask('INITIALIZING TERMINAL')
    ;(async () => {
      try {
        const { settingsData, modesStateData, chosenId } = await loadCoreState()
        if (!mounted) {
          return
        }
        const bootFilter = reconcileModeFilter(filterRef.current, modesStateData?.modes)
        if (bootFilter !== filterRef.current) {
          filterRef.current = bootFilter
          setFilter(bootFilter)
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
        endBootTask()
        if (mounted) {
          bootStateRef.current.initialBootComplete = true
          setBooting(false)
          flushDeferredBootEvents(getLiveDispatchContextRef.current())
        }
      }
    })()
    return () => {
      mounted = false
      endBootTask()
    }
  }, [beginBusyTask, loadCoreState, loadPlanOnly, loadStatsOnly])

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
      lastEffectStatsSelectionRef.current?.filter === reconciledFilter
    ) {
      return
    }
    lastEffectStatsSelectionRef.current = {
      expeditionId: activeExpeditionId,
      filter: reconciledFilter,
    }
    void loadStatsOnly(activeExpeditionId, reconciledFilter)
  }, [activeExpeditionId, loadStatsOnly, reconciledFilter])

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
  const getLiveDispatchContextRef = useRef(getLiveDispatchContext)
  getLiveDispatchContextRef.current = getLiveDispatchContext

  const {
    syncPauseState,
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
    async (selectedExpeditionId: number, options?: { persist?: boolean }) => {
      if (!selectedExpeditionId) return
      setActiveExpeditionId(selectedExpeditionId)
      activeExpeditionIdRef.current = selectedExpeditionId
      if (options?.persist === false) return
      try {
        await SetActiveExpedition(selectedExpeditionId)
      } catch (setActiveError) {
        console.error('Failed to set active expedition:', setActiveError)
      }
    },
    [activeExpeditionIdRef, setActiveExpeditionId],
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
    filter: reconciledFilter,
    setFilter,
    eddProgress,
    activeStatusMessage,
    syncPauseState,
    hasPendingSyncUpdates,
    isClockSuspended,
    handleToggleSyncPause,
    handleSelectExpedition,
    refreshAll,
    handleRefreshPlan,
    refreshSettings: loadSettingsOnly,
  }
}

