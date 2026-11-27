import { useCallback, useRef, useState, type MutableRefObject } from 'react'
import {
  GetIngestStatus,
  GetModesState,
  GetPlan,
  GetSettings,
  GetSidebarStats,
  ListExpeditions,
} from '../../api/backend'
import { app, expedition, modes, stats } from '../../api/models'
import type { RefreshAllOptions, SyncBootLifecycleState } from './syncEventRouter'

/**
 * reconcileModeFilter aligns the sidebar telemetry filter with the current
 * flight modes catalog, defaulting newly created custom modes to full
 * inclusion, dropping deleted modes, and preserving reference identity when no
 * effective selection changed.
 */
export function reconcileModeFilter(
  currentFilter: stats.Filter,
  availableModes: modes.Mode[] | undefined | null,
): stats.Filter {
  if (!availableModes) {
    return currentFilter
  }
  const existingByModeId = new Map(
    currentFilter.enabled ? (currentFilter.modes ?? []).map((item) => [item.modeId, item]) : [],
  )
  const reconciledModes = availableModes.map((mode) => {
    const existingSelection = existingByModeId.get(mode.id)
    return existingSelection
      ? {
          modeId: mode.id,
          travel: existingSelection.travel,
          discovery: existingSelection.discovery,
        }
      : { modeId: mode.id, travel: true, discovery: true }
  })
  const nextEnabled = reconciledModes.some((selection) => !selection.travel || !selection.discovery)
  const currentModes = currentFilter.modes ?? []
  if (
    nextEnabled === currentFilter.enabled &&
    currentModes.length === reconciledModes.length &&
    reconciledModes.every(
      (selection, index) =>
        currentModes[index]?.modeId === selection.modeId &&
        currentModes[index]?.travel === selection.travel &&
        currentModes[index]?.discovery === selection.discovery,
    )
  ) {
    return currentFilter
  }
  return stats.Filter.createFrom({
    ...(currentFilter.expeditionId !== undefined
      ? { expeditionId: currentFilter.expeditionId }
      : {}),
    enabled: nextEnabled,
    modes: reconciledModes,
  })
}

/**
 * UseDomainLoadersOptions provides shared filter, sidebar scope, and boot
 * lifecycle refs used by sequence-guarded backend RPC loaders.
 */
export interface UseDomainLoadersOptions {
  filterRef: MutableRefObject<stats.Filter>
  includeAllTimeRef?: MutableRefObject<boolean>
  bootStateRef: MutableRefObject<SyncBootLifecycleState>
  lastEffectPlanExpeditionIdRef: MutableRefObject<number>
  onRefreshAllFilterReconciled?: (
    nextFilter: stats.Filter,
    chosenId: number,
    willTransitionEffectReload: boolean,
  ) => void
}

/**
 * useDomainLoaders owns synchronized backend domain state and race-guarded
 * per-domain RPC loaders for ingest, settings, modes, expeditions, plan, and stats.
 */
export function useDomainLoaders({
  filterRef,
  includeAllTimeRef,
  bootStateRef,
  lastEffectPlanExpeditionIdRef,
  onRefreshAllFilterReconciled,
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
  const lastStatsRequestIncludedAllTimeRef = useRef(false)

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
            lastStatsRequestIncludedAllTimeRef.current = false
            setStatsLoading(false)
          }
        }
      })
      .catch(console.error)
  }, [])

  const loadCoreState = useCallback(async (): Promise<{
    settingsData: app.Settings | null
    modesStateData: app.ModesState | null
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
        return { settingsData: null, modesStateData: null, chosenId: 0 }
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
        lastStatsRequestIncludedAllTimeRef.current = false
        setStatsLoading(false)
      }
      return { settingsData, modesStateData, chosenId }
    } catch (coreStateError) {
      console.error('Failed to load core state:', coreStateError)
      return { settingsData: null, modesStateData: null, chosenId: 0 }
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

  const loadStatsOnly = useCallback(
    async (expeditionId: number, currentFilter: stats.Filter, includeAllTime?: boolean) => {
      if (!expeditionId) return
      const shouldIncludeAllTime = includeAllTime ?? Boolean(includeAllTimeRef?.current)
      const requestSequence = ++statsRequestSequenceRef.current
      lastStatsRequestIncludedAllTimeRef.current = shouldIncludeAllTime
      setStatsLoading(true)
      try {
        const fetchedSidebarStats = await GetSidebarStats(
          expeditionId,
          currentFilter,
          shouldIncludeAllTime,
        )
        if (
          requestSequence === statsRequestSequenceRef.current &&
          activeExpeditionIdRef.current === expeditionId
        ) {
          setSidebarStats(fetchedSidebarStats)
        }
      } catch (statsError) {
        if (requestSequence === statsRequestSequenceRef.current) {
          lastStatsRequestIncludedAllTimeRef.current = false
        }
        console.error('Failed to load sidebar stats:', statsError)
      } finally {
        if (
          requestSequence === statsRequestSequenceRef.current &&
          activeExpeditionIdRef.current === expeditionId
        ) {
          setStatsLoading(false)
        }
      }
    },
    [includeAllTimeRef],
  )

  const refreshAll = useCallback((options?: RefreshAllOptions) => {
    if (options?.skipPrefetch) {
      activeExpeditionIdRef.current = 0
      lastStatsRequestIncludedAllTimeRef.current = false
    }
    const prefetchedExpeditionId =
      !options?.skipPrefetch && setupCompletedRef.current ? activeExpeditionIdRef.current : 0
    if (prefetchedExpeditionId > 0) {
      void loadPlanOnly(prefetchedExpeditionId)
      void loadStatsOnly(prefetchedExpeditionId, filterRef.current)
    }
    void loadCoreState().then(({ settingsData, modesStateData, chosenId }) => {
      const previousFilter = filterRef.current
      const nextFilter = reconcileModeFilter(previousFilter, modesStateData?.modes)
      filterRef.current = nextFilter
      if (
        !settingsData ||
        !settingsData.setupCompleted ||
        chosenId <= 0 ||
        chosenId === prefetchedExpeditionId
      ) {
        if (nextFilter !== previousFilter) {
          onRefreshAllFilterReconciled?.(nextFilter, chosenId, true)
        }
        return
      }
      const willTransitionEffectReload =
        bootStateRef.current.initialBootComplete &&
        lastEffectPlanExpeditionIdRef.current !== chosenId
      onRefreshAllFilterReconciled?.(nextFilter, chosenId, willTransitionEffectReload)
      if (!willTransitionEffectReload) {
        void loadPlanOnly(chosenId)
        void loadStatsOnly(chosenId, nextFilter)
      }
    })
  }, [
    bootStateRef,
    filterRef,
    lastEffectPlanExpeditionIdRef,
    loadCoreState,
    loadPlanOnly,
    loadStatsOnly,
    onRefreshAllFilterReconciled,
  ])

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
    lastStatsRequestIncludedAllTimeRef,
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

