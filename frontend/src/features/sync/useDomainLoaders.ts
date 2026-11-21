import { useCallback, useRef, useState, type MutableRefObject } from 'react'
import {
  GetIngestStatus,
  GetModesState,
  GetPlan,
  GetSettings,
  GetSidebarStats,
  ListExpeditions,
} from '../../api/backend'
import { app, expedition, stats } from '../../api/models'
import type { SyncBootLifecycleState } from './syncEventRouter'

/**
 * UseDomainLoadersOptions provides shared filter and boot lifecycle refs used
 * by sequence-guarded backend RPC loaders.
 */
export interface UseDomainLoadersOptions {
  filterRef: MutableRefObject<stats.Filter>
  bootStateRef: MutableRefObject<SyncBootLifecycleState>
  lastEffectPlanExpeditionIdRef: MutableRefObject<number>
}

/**
 * useDomainLoaders owns synchronized backend domain state and race-guarded
 * per-domain RPC loaders for ingest, settings, modes, expeditions, plan, and stats.
 */
export function useDomainLoaders({
  filterRef,
  bootStateRef,
  lastEffectPlanExpeditionIdRef,
}: UseDomainLoadersOptions) {
  const [statsLoading, setStatsLoading] = useState<boolean>(false)
  const [settings, setSettings] = useState<app.Settings | null>(null)
  const [ingest, setIngest] = useState<app.IngestStatus | null>(null)
  const [modesState, setModesState] = useState<app.ModesState | null>(null)
  const [expeditions, setExpeditions] = useState<expedition.Expedition[]>([])
  const [activeExpeditionId, setActiveExpeditionId] = useState<number>(0)
  const [plan, setPlan] = useState<expedition.PlanView | null>(null)
  const [sidebarStats, setSidebarStats] = useState<app.SidebarStats | null>(null)

  const activeExpeditionIdRef = useRef(0)
  const setupCompletedRef = useRef(false)
  const coreStateRequestSequenceRef = useRef(0)
  const ingestRequestSequenceRef = useRef(0)
  const settingsRequestSequenceRef = useRef(0)
  const modesRequestSequenceRef = useRef(0)
  const planRequestSequenceRef = useRef(0)
  const statsRequestSequenceRef = useRef(0)

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

  const loadSettingsOnly = useCallback(async () => {
    const requestSequence = ++settingsRequestSequenceRef.current
    try {
      const settingsData = await GetSettings()
      if (requestSequence === settingsRequestSequenceRef.current && settingsData) {
        setupCompletedRef.current = Boolean(settingsData.setupCompleted)
        setSettings(settingsData)
      }
      return settingsData ?? null
    } catch (settingsError) {
      console.error('Failed to load settings:', settingsError)
      return null
    }
  }, [])

  const loadModesOnly = useCallback(async () => {
    const requestSequence = ++modesRequestSequenceRef.current
    try {
      const modesStateData = await GetModesState()
      if (requestSequence === modesRequestSequenceRef.current && modesStateData) {
        setModesState(modesStateData)
      }
      return modesStateData ?? null
    } catch (modesError) {
      console.error('Failed to load modes state:', modesError)
      return null
    }
  }, [])

  const refreshExpeditionListOnly = useCallback(() => {
    ListExpeditions()
      .then((expeditionList) => {
        const loadedList = expeditionList || []
        setExpeditions(loadedList)
        if (
          activeExpeditionIdRef.current > 0 &&
          !loadedList.some((expeditionItem) => expeditionItem.id === activeExpeditionIdRef.current)
        ) {
          if (loadedList.length > 0) {
            activeExpeditionIdRef.current = loadedList[0].id
            setActiveExpeditionId(loadedList[0].id)
          } else {
            activeExpeditionIdRef.current = 0
            setActiveExpeditionId(0)
            setPlan(null)
            setSidebarStats(null)
            setStatsLoading(false)
          }
        }
      })
      .catch(console.error)
  }, [])

  const loadCoreState = useCallback(async (): Promise<{
    settingsData: app.Settings | null
    chosenId: number
  }> => {
    const coreRequestSequence = ++coreStateRequestSequenceRef.current
    const ingestRequestSequence = ++ingestRequestSequenceRef.current
    const settingsRequestSequence = ++settingsRequestSequenceRef.current
    const modesRequestSequence = ++modesRequestSequenceRef.current
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
      if (settingsRequestSequence === settingsRequestSequenceRef.current) {
        setupCompletedRef.current = Boolean(settingsData.setupCompleted)
        setSettings(settingsData)
      }
      if (ingestRequestSequence === ingestRequestSequenceRef.current) {
        setIngest(ingestStatus)
      }
      if (modesRequestSequence === modesRequestSequenceRef.current) {
        setModesState(modesStateData)
      }

      const loadedExpeditions = expeditionList || []
      setExpeditions(loadedExpeditions)

      let chosenId = 0
      const currentExpeditionId = activeExpeditionIdRef.current
      if (
        currentExpeditionId &&
        loadedExpeditions.some((expeditionItem) => expeditionItem.id === currentExpeditionId)
      ) {
        chosenId = currentExpeditionId
      } else if (
        settingsData.activeExpeditionId &&
        loadedExpeditions.some(
          (expeditionItem) => expeditionItem.id === settingsData.activeExpeditionId,
        )
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

  // Fast plan loader (< 5ms) never blocked by sidebar telemetry computation.
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
  }, [bootStateRef, filterRef, lastEffectPlanExpeditionIdRef, loadCoreState, loadPlanOnly, loadStatsOnly])

  return {
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
  }
}

