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
  const initialBootCompleteRef = useRef(false)
  const ingestReqSeq = useRef(0)
  const planReqSeq = useRef(0)
  const statsReqSeq = useRef(0)
  const filterRef = useRef(filter)

  useEffect(() => {
    activeExpeditionIdRef.current = activeExpeditionId
  }, [activeExpeditionId])

  useEffect(() => {
    filterRef.current = filter
  }, [filter])

  const loadIngestStatusOnly = useCallback(async () => {
    const requestSeq = ++ingestReqSeq.current
    try {
      const ingestStatus = await GetIngestStatus()
      if (requestSeq === ingestReqSeq.current) {
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
    const ingestSeq = ++ingestReqSeq.current
    try {
      const [settingsData, ingestStatus, modesStateData, expeditionList] = await Promise.all([
        GetSettings(),
        GetIngestStatus(),
        GetModesState(),
        ListExpeditions(),
      ])
      setSettings(settingsData)
      if (ingestSeq === ingestReqSeq.current) {
        setIngest(ingestStatus)
      }
      setModesState(modesStateData)

      const list = expeditionList || []
      setExpeditions(list)

      let chosenId = 0
      const current = activeExpeditionIdRef.current
      if (current && list.some((expeditionItem) => expeditionItem.id === current)) {
        chosenId = current
      } else if (
        settingsData.activeExpeditionId &&
        list.some((expeditionItem) => expeditionItem.id === settingsData.activeExpeditionId)
      ) {
        chosenId = settingsData.activeExpeditionId
      } else if (list.length > 0) {
        chosenId = list[0].id
      }

      if (chosenId > 0) {
        setActiveExpeditionId(chosenId)
        activeExpeditionIdRef.current = chosenId
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
    const requestSeq = ++planReqSeq.current
    try {
      const fetchedPlan = await GetPlan(expeditionId)
      if (
        requestSeq === planReqSeq.current &&
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
    const requestSeq = ++statsReqSeq.current
    setStatsLoading(true)
    try {
      const fetchedSidebarStats = await GetSidebarStats(expeditionId, currentFilter)
      if (requestSeq === statsReqSeq.current && activeExpeditionIdRef.current === expeditionId) {
        setSidebarStats(fetchedSidebarStats)
      }
    } catch (statsError) {
      console.error('Failed to load sidebar stats:', statsError)
    } finally {
      if (requestSeq === statsReqSeq.current && activeExpeditionIdRef.current === expeditionId) {
        setStatsLoading(false)
      }
    }
  }, [])

  // Initial boot sequence: load core state, active expedition plan, and sidebar
  // telemetry before dismissing the boot screen so the main cockpit UI never
  // paints in an unready state.
  useEffect(() => {
    let mounted = true
    setBusyAction('INITIALIZING TERMINAL')
    ;(async () => {
      try {
        const { settingsData, chosenId } = await loadCoreState()
        if (mounted && settingsData?.setupCompleted && chosenId > 0) {
          await Promise.all([
            loadPlanOnly(chosenId),
            loadStatsOnly(chosenId, filterRef.current),
          ])
        }
      } finally {
        if (mounted) {
          initialBootCompleteRef.current = true
          setBooting(false)
          setBusyAction(null)
        }
      }
    })()
    return () => {
      mounted = false
    }
  }, [loadCoreState, loadPlanOnly, loadStatsOnly])

  // Load plan immediately when expedition changes after initial boot
  useEffect(() => {
    if (!initialBootCompleteRef.current) return
    if (activeExpeditionId > 0) {
      loadPlanOnly(activeExpeditionId)
    }
  }, [activeExpeditionId, loadPlanOnly])

  // Reload sidebar stats when expedition or filter changes after initial boot
  useEffect(() => {
    if (!initialBootCompleteRef.current) return
    if (activeExpeditionId > 0) {
      loadStatsOnly(activeExpeditionId, filter)
    }
  }, [activeExpeditionId, filter, loadStatsOnly])

  // Subscribe to Wails backend events (live fsnotify journal tailing, coordinate resolution, etc.)
  useEffect(() => {
    if (typeof window === 'undefined' || !(window as any).runtime?.EventsOnMultiple) {
      return
    }

    const offChanged = EventsOn(
      'data:changed',
      (payload?: { scope?: string; kind?: string; expeditionId?: number; expId?: number }) => {
        const scope = payload?.scope ?? payload?.kind ?? ''
        const currentActive = activeExpeditionIdRef.current
        const eventExpeditionId = payload?.expeditionId ?? payload?.expId

        if (eventExpeditionId && currentActive > 0 && eventExpeditionId !== currentActive) {
          ListExpeditions()
            .then((expeditionList) => expeditionList && setExpeditions(expeditionList))
            .catch(console.error)
          return
        }

        if (scope === 'plan') {
          if (currentActive > 0) {
            loadPlanOnly(currentActive)
          }
          return
        }

        if (scope === 'adjustments') {
          if (currentActive > 0) {
            loadStatsOnly(currentActive, filterRef.current)
          }
          return
        }

        void loadCoreState().then(({ chosenId }) => {
          const targetExpeditionId = chosenId || activeExpeditionIdRef.current
          if (targetExpeditionId > 0) {
            void loadPlanOnly(targetExpeditionId)
            void loadStatsOnly(targetExpeditionId, filterRef.current)
          }
        })
      },
    )
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
  }, [loadCoreState, loadIngestStatusOnly, loadPlanOnly, loadStatsOnly])

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

  const refreshAll = useCallback(() => {
    void loadCoreState().then(({ chosenId }) => {
      const targetExpeditionId = chosenId || activeExpeditionIdRef.current
      if (targetExpeditionId > 0) {
        void loadPlanOnly(targetExpeditionId)
        void loadStatsOnly(targetExpeditionId, filterRef.current)
      }
    })
  }, [loadCoreState, loadPlanOnly, loadStatsOnly])

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

  const handleRefreshStats = useCallback(() => {
    if (activeExpeditionIdRef.current > 0) {
      loadStatsOnly(activeExpeditionIdRef.current, filter)
    }
  }, [loadStatsOnly, filter])

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
    handleRefreshStats,
  }
}

