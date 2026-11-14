import { useCallback, useEffect, useRef, useState } from 'react'
import {
  GetIngestStatus,
  GetModesState,
  GetPlan,
  GetSettings,
  GetSidebarStats,
  ListExpeditions,
  SetActiveExpedition,
  SwitchMode,
} from '../../wailsjs/go/app/App'
import { app, expedition, stats } from '../../wailsjs/go/models'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { EddImportProgress } from '../components/settings/EDDiscoveryImportSection'

/**
 * SyncBootLifecycleState tracks the two-stage terminal boot lifecycle so early
 * Wails events do not cancel in-flight bootstrap RPCs before main paint.
 */
export interface SyncBootLifecycleState {
  initialBootComplete: boolean
  bootCoreLoaded: boolean
  pendingBootRefresh: boolean
}

/**
 * DataChangedDispatchContext supplies current synchronization state and loader
 * callbacks when routing a backend data:changed event.
 */
export interface DataChangedDispatchContext {
  bootState: SyncBootLifecycleState
  currentActiveExpeditionId: number
  currentFilter: stats.Filter
  refreshExpeditionListOnly: () => void
  loadCoreState: () => Promise<{
    settingsData: app.Settings | null
    chosenId: number
  }>
  loadPlanOnly: (expeditionId: number) => Promise<void>
  loadStatsOnly: (expeditionId: number, currentFilter: stats.Filter) => Promise<void>
  refreshAll: () => void
}

/**
 * DataChangedPayload matches the Go app.DataChanged event payload emitted on
 * Wails 'data:changed' runtime notifications.
 */
export interface DataChangedPayload {
  scope?: string
  expeditionId?: number
}

/**
 * routeDataChangedEvent dispatches a Wails data:changed notification without
 * interrupting an in-progress initial boot or waterfalling live journal updates.
 */
export function routeDataChangedEvent(
  payload: DataChangedPayload | undefined,
  dispatchContext: DataChangedDispatchContext,
): void {
  if (!dispatchContext.bootState.initialBootComplete) {
    if (dispatchContext.bootState.bootCoreLoaded) {
      dispatchContext.bootState.pendingBootRefresh = true
    }
    return
  }

  const scope = payload?.scope ?? ''
  const currentActive = dispatchContext.currentActiveExpeditionId
  const eventExpeditionId = payload?.expeditionId

  if (eventExpeditionId && currentActive > 0 && eventExpeditionId !== currentActive) {
    dispatchContext.refreshExpeditionListOnly()
    return
  }

  if (scope === 'plan') {
    if (currentActive > 0) {
      void dispatchContext.loadPlanOnly(currentActive)
    }
    return
  }

  if (scope === 'adjustments') {
    if (currentActive > 0) {
      void dispatchContext.loadStatsOnly(currentActive, dispatchContext.currentFilter)
    }
    return
  }

  if (scope === 'journal') {
    void dispatchContext.loadCoreState()
    if (currentActive > 0) {
      void dispatchContext.loadPlanOnly(currentActive)
      void dispatchContext.loadStatsOnly(currentActive, dispatchContext.currentFilter)
    }
    return
  }

  dispatchContext.refreshAll()
}

/**
 * useBackendSync manages terminal bootstrap state, Wails IPC queries for
 * settings/expeditions/plans/telemetry, and live runtime event subscriptions.
 */
export function useBackendSync() {
  const [booting, setBooting] = useState<boolean>(true)
  const [busyAction, setBusyAction] = useState<string | null>('INITIALIZING TERMINAL')
  const [statsLoading, setStatsLoading] = useState<boolean>(false)

  const [settings, setSettings] = useState<app.Settings | null>(null)
  const [ingest, setIngest] = useState<app.IngestStatus | null>(null)
  const [modesState, setModesState] = useState<app.ModesState | null>(null)
  const [expeditions, setExpeditions] = useState<expedition.Expedition[]>([])
  const [activeExpeditionId, setActiveExpeditionId] = useState<number>(0)
  const [plan, setPlan] = useState<expedition.PlanView | null>(null)
  const [sidebarStats, setSidebarStats] = useState<app.SidebarStats | null>(null)
  const [filter, setFilter] = useState<stats.Filter>(() =>
    stats.Filter.createFrom({ enabled: false, modes: [] }),
  )
  const [eddProgress, setEddProgress] = useState<EddImportProgress | null>(null)

  const activeExpeditionIdRef = useRef(0)
  const setupCompletedRef = useRef(false)
  const bootStateRef = useRef<SyncBootLifecycleState>({
    initialBootComplete: false,
    bootCoreLoaded: false,
    pendingBootRefresh: false,
  })
  const coreStateRequestSequenceRef = useRef(0)
  const lastEffectPlanExpeditionIdRef = useRef(0)
  const lastEffectStatsSelectionRef = useRef<{
    expeditionId: number
    filter: stats.Filter
  } | null>(null)
  const ingestRequestSequenceRef = useRef(0)
  const planRequestSequenceRef = useRef(0)
  const statsRequestSequenceRef = useRef(0)
  const filterRef = useRef(filter)

  useEffect(() => {
    activeExpeditionIdRef.current = activeExpeditionId
  }, [activeExpeditionId])

  useEffect(() => {
    filterRef.current = filter
  }, [filter])

  const loadIngestStatusOnly = useCallback(async () => {
    const requestSequence = ++ingestRequestSequenceRef.current
    try {
      const ingestStatus = await GetIngestStatus()
      if (requestSequence === ingestRequestSequenceRef.current) {
        setIngest(ingestStatus)
      }
      return ingestStatus
    } catch (ingestError) {
      console.error('Failed to load ingest status:', ingestError)
      return null
    }
  }, [])

  const loadCoreState = useCallback(async (): Promise<{
    settingsData: app.Settings | null
    chosenId: number
  }> => {
    const coreRequestSequence = ++coreStateRequestSequenceRef.current
    const ingestRequestSequence = ++ingestRequestSequenceRef.current
    try {
      const [settingsData, ingestStatus, modesStateData, expeditionList] = await Promise.all([
        GetSettings(),
        GetIngestStatus(),
        GetModesState(),
        ListExpeditions(),
      ])
      if (coreRequestSequence !== coreStateRequestSequenceRef.current) {
        return { settingsData: null, chosenId: 0 }
      }
      setupCompletedRef.current = Boolean(settingsData.setupCompleted)
      setSettings(settingsData)
      if (ingestRequestSequence === ingestRequestSequenceRef.current) {
        setIngest(ingestStatus)
      }
      setModesState(modesStateData)

      const loadedExpeditions = expeditionList || []
      setExpeditions(loadedExpeditions)

      let chosenId = 0
      const currentExpeditionId = activeExpeditionIdRef.current
      if (currentExpeditionId && loadedExpeditions.some((expeditionItem) => expeditionItem.id === currentExpeditionId)) {
        chosenId = currentExpeditionId
      } else if (
        settingsData.activeExpeditionId &&
        loadedExpeditions.some((expeditionItem) => expeditionItem.id === settingsData.activeExpeditionId)
      ) {
        chosenId = settingsData.activeExpeditionId
      } else if (loadedExpeditions.length > 0) {
        chosenId = loadedExpeditions[0].id
      }

      if (chosenId > 0) {
        setActiveExpeditionId(chosenId)
        activeExpeditionIdRef.current = chosenId
      } else {
        setActiveExpeditionId(0)
        activeExpeditionIdRef.current = 0
        setPlan(null)
        setSidebarStats(null)
        setStatsLoading(false)
      }
      return { settingsData, chosenId }
    } catch (coreStateError) {
      console.error('Failed to load core state:', coreStateError)
      return { settingsData: null, chosenId: 0 }
    }
  }, [])

  // Fast plan loader (< 5ms) — never blocked by sidebar telemetry computation
  const loadPlanOnly = useCallback(async (expeditionId: number) => {
    if (!expeditionId) return
    const requestSequence = ++planRequestSequenceRef.current
    try {
      const fetchedPlan = await GetPlan(expeditionId)
      if (
        requestSequence === planRequestSequenceRef.current &&
        fetchedPlan?.expedition?.id === activeExpeditionIdRef.current
      ) {
        setPlan(fetchedPlan)
      }
    } catch (loadPlanError) {
      console.error('Failed to load expedition plan:', loadPlanError)
    }
  }, [])

  // Asynchronous sidebar telemetry loader
  const loadStatsOnly = useCallback(async (expeditionId: number, currentFilter: stats.Filter) => {
    if (!expeditionId) return
    const requestSequence = ++statsRequestSequenceRef.current
    setStatsLoading(true)
    try {
      const fetchedSidebarStats = await GetSidebarStats(expeditionId, currentFilter)
      if (
        requestSequence === statsRequestSequenceRef.current &&
        activeExpeditionIdRef.current === expeditionId
      ) {
        setSidebarStats(fetchedSidebarStats)
      }
    } catch (statsError) {
      console.error('Failed to load sidebar stats:', statsError)
    } finally {
      if (
        requestSequence === statsRequestSequenceRef.current &&
        activeExpeditionIdRef.current === expeditionId
      ) {
        setStatsLoading(false)
      }
    }
  }, [])

  const refreshAll = useCallback(() => {
    const prefetchedExpeditionId = setupCompletedRef.current ? activeExpeditionIdRef.current : 0
    if (prefetchedExpeditionId > 0) {
      void loadPlanOnly(prefetchedExpeditionId)
      void loadStatsOnly(prefetchedExpeditionId, filterRef.current)
    }
    void loadCoreState().then(({ settingsData, chosenId }) => {
      if (
        !settingsData ||
        !settingsData.setupCompleted ||
        chosenId <= 0 ||
        chosenId === prefetchedExpeditionId
      ) {
        return
      }
      const willTransitionEffectReload =
        bootStateRef.current.initialBootComplete &&
        lastEffectPlanExpeditionIdRef.current !== chosenId
      if (!willTransitionEffectReload) {
        void loadPlanOnly(chosenId)
        void loadStatsOnly(chosenId, filterRef.current)
      }
    })
  }, [loadCoreState, loadPlanOnly, loadStatsOnly])

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

  // Load plan immediately when expedition changes after initial boot
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

  // Reload sidebar stats when expedition or filter changes after initial boot
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

  // Subscribe to Wails backend events (live fsnotify journal tailing, coordinate resolution, etc.)
  useEffect(() => {
    if (typeof window === 'undefined' || !window.runtime?.EventsOnMultiple) {
      return
    }

    const offChanged = EventsOn('data:changed', (payload?: DataChangedPayload) => {
        routeDataChangedEvent(payload, {
          bootState: bootStateRef.current,
          currentActiveExpeditionId: activeExpeditionIdRef.current,
          currentFilter: filterRef.current,
          refreshExpeditionListOnly: () => {
            ListExpeditions()
              .then((expeditionList) => expeditionList && setExpeditions(expeditionList))
              .catch(console.error)
          },
          loadCoreState,
          loadPlanOnly,
          loadStatsOnly,
          refreshAll,
        })
      })
    const offIngest = EventsOn('ingest:status', () => {
      void loadIngestStatusOnly()
    })
    const offEdd = EventsOn('edd:progress', (progress) => {
      setEddProgress(progress)
    })
    return () => {
      offChanged()
      offIngest()
      offEdd()
    }
  }, [loadCoreState, loadIngestStatusOnly, loadPlanOnly, loadStatsOnly, refreshAll])

  const handleSelectExpedition = useCallback(async (selectedExpeditionId: number) => {
    if (!selectedExpeditionId) return
    setActiveExpeditionId(selectedExpeditionId)
    activeExpeditionIdRef.current = selectedExpeditionId
    try {
      await SetActiveExpedition(selectedExpeditionId)
    } catch (setActiveError) {
      console.error('Failed to set active expedition:', setActiveError)
    }
    const expeditionList = await ListExpeditions()
    if (expeditionList && expeditionList.length > 0) {
      setExpeditions(expeditionList)
    }
  }, [])

  const handleQuickSwitchMode = useCallback(
    async (modeId: number) => {
      setBusyAction('SWITCHING FLIGHT MODE')
      try {
        const nextModesState = await SwitchMode(modeId)
        setModesState(nextModesState)
        if (activeExpeditionId > 0) {
          loadPlanOnly(activeExpeditionId)
          loadStatsOnly(activeExpeditionId, filter)
        }
      } finally {
        setBusyAction(null)
      }
    },
    [activeExpeditionId, filter, loadPlanOnly, loadStatsOnly],
  )

  const refreshPlanAndStats = useCallback(() => {
    if (activeExpeditionIdRef.current > 0) {
      loadPlanOnly(activeExpeditionIdRef.current)
      loadStatsOnly(activeExpeditionIdRef.current, filter)
    }
  }, [loadPlanOnly, loadStatsOnly, filter])

  const handleRefreshPlan = useCallback(() => {
    if (activeExpeditionIdRef.current > 0) {
      loadPlanOnly(activeExpeditionIdRef.current)
    }
  }, [loadPlanOnly])

  const activeStatusMessage =
    busyAction ||
    (eddProgress?.active ? `EDD IMPORT ${eddProgress.percent}%` : null) ||
    (ingest?.scanning ? 'INGESTING JOURNALS' : null) ||
    (statsLoading ? 'COMPUTING TELEMETRY' : null)

  return {
    booting,
    setBusyAction,
    statsLoading,
    settings,
    ingest,
    modesState,
    expeditions,
    activeExpeditionId,
    plan,
    setPlan,
    sidebarStats,
    filter,
    setFilter,
    eddProgress,
    activeStatusMessage,
    handleSelectExpedition,
    handleQuickSwitchMode,
    refreshAll,
    refreshPlanAndStats,
    handleRefreshPlan,
  }
}

