import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { PlotCarrierRoute, RecordCarrierJump } from '../../api/backend'
import { expedition } from '../../api/models'
import {
  CARRIER_MAX_DEPOT_TONNES,
  CARRIER_MAX_PERSONAL_CAPACITY_TONNES,
  CARRIER_MAX_SQUADRON_CAPACITY_TONNES,
  computeDepotRefill,
  computeRoutedCarrierStopIndex,
  findNextCarrierStopEntry,
  normalizeDepotTritium,
  recalculateCarrierRouteDepotFuel,
} from '../../domain/carrierRouting'
import { createBusyTaskTracker, type BusyTaskTracker } from '../../hooks/useBusyTasks'
import { useCopyFeedback } from '../../hooks/useCopyFeedback'
import { resolveCarrierDefaults, resolveCarrierStartSystem } from './carrierRouteDefaults'
import type { CarrierRoutePromptState } from './CarrierRouteModal'
import { usePersistedCarrierRoute } from './usePersistedCarrierRoute'

/**
 * UseCarrierRouteOptions defines the expedition plan context and callbacks
 * needed to manage Spansh and manual Fleet Carrier routing.
 */
export interface UseCarrierRouteOptions {
  plan: expedition.PlanView | null
  lastVisitedIndex: number
  manualPlotterRequestCount?: number
  onBusyChange: (busyMessage: string | null) => void
  busyTracker?: BusyTaskTracker
  onSetFcArrived: (rowId: number, arrived: boolean) => Promise<boolean | void>
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

/**
 * useCarrierRoute manages Fleet Carrier route setup, Spansh jump progression,
 * depot fuel recalculation, and manual jump logging.
 */
export function useCarrierRoute({
  plan,
  lastVisitedIndex,
  manualPlotterRequestCount = 0,
  onBusyChange,
  busyTracker: externalBusyTracker,
  onSetFcArrived,
  onCarrierRouteOpenChange,
}: UseCarrierRouteOptions) {
  const [routePrompt, setRoutePrompt] = useState<CarrierRoutePromptState | null>(null)
  const [loggingJump, setLoggingJump] = useState(false)
  const { copiedKey, copyWithFeedback } = useCopyFeedback<'next'>()
  const copiedNextJump = copiedKey === 'next'

  const persistedRoute = usePersistedCarrierRoute(plan?.expedition?.id)
  const {
    activeCarrierRoute,
    setActiveCarrierRoute,
    activeCarrierTargetRowId,
    carrierJumpIndex,
    setCarrierJumpIndex,
    manualModeOverride,
    setManualModeOverride,
    manualDistanceInput,
    setManualDistanceInput,
    manualTritiumInput,
    setManualTritiumInput,
    manualDistanceInputRef,
    manualTritiumInputRef,
    manualLegJumps,
    setManualLegJumps,
    manualLegDistance,
    setManualLegDistance,
    manualLegTritium,
    setManualLegTritium,
    cancelPendingInputPersist,
    persistRouteState,
    activateCarrierRoute,
    clearPersistedCarrierRoute,
    handleChangeManualDistance,
    handleChangeManualTritium,
  } = persistedRoute

  const currentExpeditionId = plan?.expedition?.id
  const expeditionIdRef = useRef<number | undefined>(currentExpeditionId)
  expeditionIdRef.current = currentExpeditionId
  const mountedRef = useRef(true)
  const routeRequestSeqRef = useRef(0)

  const onBusyChangeRef = useRef(onBusyChange)
  onBusyChangeRef.current = onBusyChange
  const fallbackBusyTracker = useMemo(
    () => createBusyTaskTracker((busyMessage) => onBusyChangeRef.current(busyMessage)),
    [],
  )
  const busyTracker = externalBusyTracker ?? fallbackBusyTracker
  const activePlotBusyEndRef = useRef<(() => void) | null>(null)

  const invalidateRouteRequests = useCallback(() => {
    routeRequestSeqRef.current += 1
    const endPlotBusy = activePlotBusyEndRef.current
    activePlotBusyEndRef.current = null
    endPlotBusy?.()
    return routeRequestSeqRef.current
  }, [])

  const isStaleRouteRequest = useCallback(
    (requestExpeditionId: number | undefined, requestSeq: number) =>
      !mountedRef.current ||
      expeditionIdRef.current !== requestExpeditionId ||
      routeRequestSeqRef.current !== requestSeq,
    [],
  )

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      invalidateRouteRequests()
    }
  }, [invalidateRouteRequests])

  useEffect(() => {
    invalidateRouteRequests()
    setRoutePrompt(null)
    setLoggingJump(false)
  }, [currentExpeditionId, invalidateRouteRequests])

  const nextFcStopEntry = useMemo(() => findNextCarrierStopEntry(plan), [plan])
  const nextFcStopSystem = nextFcStopEntry ? (nextFcStopEntry.row.systemName ?? '').trim() : ''

  const isManualCarrierRoute =
    manualModeOverride ||
    Boolean(activeCarrierRoute?.manualMode) ||
    !activeCarrierRoute?.jumps?.length

  const routedCarrierStopIndex = useMemo(
    () =>
      computeRoutedCarrierStopIndex(
        activeCarrierRoute,
        activeCarrierTargetRowId,
        isManualCarrierRoute,
        nextFcStopEntry,
        plan?.rows,
      ),
    [activeCarrierRoute, activeCarrierTargetRowId, isManualCarrierRoute, nextFcStopEntry, plan?.rows],
  )

  function handleCopyNextJumpSystem(systemName: string) {
    copyWithFeedback('next', systemName)
  }

  const handleCloseRoutePrompt = useCallback(() => {
    invalidateRouteRequests()
    setRoutePrompt(null)
  }, [invalidateRouteRequests])

  function handleCancelCarrierRoute() {
    invalidateRouteRequests()
    clearPersistedCarrierRoute()
    setCarrierJumpIndex(0)
    setManualModeOverride(false)
  }

  function switchActiveRouteToManualMode() {
    setManualModeOverride(true)
    let initialDistanceInput = ''
    let initialTritiumInput = ''
    if (activeCarrierRoute?.jumps && carrierJumpIndex < activeCarrierRoute.jumps.length) {
      const currentHop = activeCarrierRoute.jumps[carrierJumpIndex]
      if (currentHop?.distanceLy !== undefined && currentHop.distanceLy > 0) {
        initialDistanceInput = String(Math.round(currentHop.distanceLy * 100) / 100)
      }
      if (currentHop?.tritiumUsed !== undefined && currentHop.tritiumUsed > 0) {
        initialTritiumInput = String(currentHop.tritiumUsed)
      }
    }
    manualDistanceInputRef.current = initialDistanceInput
    manualTritiumInputRef.current = initialTritiumInput
    setManualDistanceInput(initialDistanceInput)
    setManualTritiumInput(initialTritiumInput)
    persistRouteState({
      manualMode: true,
      manualDistInput: initialDistanceInput,
      manualTritInput: initialTritiumInput,
    })
  }

  // Opens the bottom carrier plotter directly in manual jump-logging mode.
  // When no upcoming Fleet Carrier stop is scheduled in the plan, targetRowId
  // defaults to 0 (targetless manual session): jumps and tritium are still
  // recorded against expedition adjustments while arrival marking waits for an FC stop.
  async function openManualCarrierPlotter() {
    if (!plan || plan.expedition?.status === 'completed' || activeCarrierRoute) return

    const requestExpeditionId = plan.expedition?.id
    const requestSeq = invalidateRouteRequests()
    const upToIndex = nextFcStopEntry
      ? nextFcStopEntry.index
      : lastVisitedIndex >= 0
        ? lastVisitedIndex + 1
        : (plan.rows?.length ?? 0)
    const targetRowId = nextFcStopEntry ? nextFcStopEntry.row.id : 0
    const [startSystem, defaults] = await Promise.all([
      resolveCarrierStartSystem(plan, upToIndex),
      resolveCarrierDefaults(),
    ])
    if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return

    const defaultCarrierType: 'player' | 'squadron' =
      plan.expedition?.carrierType === 'squadron' ? 'squadron' : 'player'
    const manualRoute = expedition.CarrierRouteResult.createFrom({
      origin: startSystem,
      destination: nextFcStopSystem,
      carrierType: defaultCarrierType,
      capacityUsed: defaults.capacity,
      depotTritium: defaults.depot,
      manualMode: true,
      totalJumps: 0,
      totalDistLy: 0,
      totalTritium: 0,
      jumps: [],
    })
    activateCarrierRoute(manualRoute, targetRowId, true)
  }

  const openManualPlotterRef = useRef(openManualCarrierPlotter)
  openManualPlotterRef.current = openManualCarrierPlotter
  const handledManualPlotterRequestRef = useRef(manualPlotterRequestCount)

  useEffect(() => {
    if (manualPlotterRequestCount > handledManualPlotterRequestRef.current) {
      handledManualPlotterRequestRef.current = manualPlotterRequestCount
      void openManualPlotterRef.current()
    }
  }, [manualPlotterRequestCount])

  useEffect(() => {
    onCarrierRouteOpenChange?.(Boolean(activeCarrierRoute))
  }, [activeCarrierRoute, onCarrierRouteOpenChange])

  const latestPlanRef = useRef(plan)
  latestPlanRef.current = plan

  const openCarrierRoutePrompt = useCallback(
    async (rowIndex: number, targetRow: expedition.Row) => {
      const currentPlan = latestPlanRef.current
      if (!currentPlan) return
      const requestExpeditionId = currentPlan.expedition?.id
      const requestSeq = invalidateRouteRequests()
      const [startSystem, defaults] = await Promise.all([
        resolveCarrierStartSystem(currentPlan, rowIndex),
        resolveCarrierDefaults(),
      ])
      if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return
      const defaultCarrierType: 'player' | 'squadron' =
        currentPlan.expedition?.carrierType === 'squadron' ? 'squadron' : 'player'
      setRoutePrompt({
        targetRowId: targetRow.id,
        origin: startSystem,
        destination: (targetRow.systemName ?? '').trim(),
        capacityUsed: String(defaults.capacity),
        depotTritium: String(defaults.depot),
        carrierType: defaultCarrierType,
        loading: false,
      })
    },
    [invalidateRouteRequests, isStaleRouteRequest],
  )

  async function handleStartCarrierRoute() {
    if (!routePrompt || routePrompt.loading) return
    const requestExpeditionId = expeditionIdRef.current
    const requestSeq = invalidateRouteRequests()
    const targetWaypointRowId = routePrompt.targetRowId
    const maxCarrierCapacityTonnes =
      routePrompt.carrierType === 'squadron'
        ? CARRIER_MAX_SQUADRON_CAPACITY_TONNES
        : CARRIER_MAX_PERSONAL_CAPACITY_TONNES
    const carrierCapacity = Math.max(
      0,
      Math.min(maxCarrierCapacityTonnes, parseInt(routePrompt.capacityUsed, 10) || 0),
    )
    const carrierDepot = Math.max(
      0,
      Math.min(
        CARRIER_MAX_DEPOT_TONNES,
        parseInt(routePrompt.depotTritium, 10) || CARRIER_MAX_DEPOT_TONNES,
      ),
    )

    setRoutePrompt((previous) => (previous ? { ...previous, loading: true } : null))
    const endBusyTask = busyTracker.beginTask('PLOTTING SPANSH CARRIER ROUTE')
    activePlotBusyEndRef.current = endBusyTask
    try {
      const routeResult = await PlotCarrierRoute(
        routePrompt.origin.trim(),
        routePrompt.destination.trim(),
        carrierCapacity,
        routePrompt.carrierType,
        carrierDepot,
      )
      if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return
      setRoutePrompt(null)
      activateCarrierRoute(routeResult, targetWaypointRowId, Boolean(routeResult.manualMode))
    } catch (routeError: unknown) {
      if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return
      const errorRoute = expedition.CarrierRouteResult.createFrom({
        origin: routePrompt.origin.trim(),
        destination: routePrompt.destination.trim(),
        carrierType: routePrompt.carrierType,
        capacityUsed: carrierCapacity,
        depotTritium: carrierDepot,
        manualMode: true,
        error: routeError instanceof Error ? routeError.message : String(routeError),
        totalJumps: 0,
        totalDistLy: 0,
        totalTritium: 0,
        jumps: [],
      })
      setRoutePrompt(null)
      activateCarrierRoute(errorRoute, targetWaypointRowId, true)
    } finally {
      if (activePlotBusyEndRef.current === endBusyTask) activePlotBusyEndRef.current = null
      endBusyTask()
    }
  }

  function handleRefillDepot() {
    if (!activeCarrierRoute) return
    const currentDepot = normalizeDepotTritium(activeCarrierRoute.depotTritium)
    const refillResult = computeDepotRefill(currentDepot, activeCarrierRoute.capacityUsed || 0)
    if (!refillResult) return
    const updatedRoute = recalculateCarrierRouteDepotFuel(
      activeCarrierRoute,
      carrierJumpIndex,
      refillResult.nextDepot,
      refillResult.nextCapacity,
    )
    setActiveCarrierRoute(updatedRoute)
    persistRouteState({ route: updatedRoute })
  }

  async function handleCarrierArriveAtWaypoint() {
    const requestExpeditionId = currentExpeditionId
    const requestSeq = routeRequestSeqRef.current
    if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return
    const targetRowId = isManualCarrierRoute
      ? (nextFcStopEntry?.row.id ?? 0)
      : activeCarrierTargetRowId || (nextFcStopEntry?.row.id ?? 0)
    if (targetRowId > 0) {
      try {
        const arrivedSucceeded = await onSetFcArrived(targetRowId, true)
        if (arrivedSucceeded === false || isStaleRouteRequest(requestExpeditionId, requestSeq)) {
          return
        }
      } catch {
        return
      }
    }
    clearPersistedCarrierRoute()
  }

  async function handleCompleteSpanshJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const carrierHop = activeCarrierRoute.jumps?.[carrierJumpIndex]
    if (!carrierHop) return

    const currentDepot = normalizeDepotTritium(activeCarrierRoute.depotTritium)
    if (currentDepot < carrierHop.tritiumUsed) {
      return
    }

    const requestExpeditionId = currentExpeditionId
    const requestSeq = routeRequestSeqRef.current
    setLoggingJump(true)
    const endBusyTask = busyTracker.beginTask('LOGGING CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, carrierHop.distanceLy, carrierHop.tritiumUsed)
      if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return
      const nextJumpIndex = carrierJumpIndex + 1
      if (nextJumpIndex >= (activeCarrierRoute.jumps?.length ?? 0)) {
        await handleCarrierArriveAtWaypoint()
        return
      }
      setCarrierJumpIndex(nextJumpIndex)
      const nextDepot = Math.max(0, currentDepot - carrierHop.tritiumUsed)
      const updatedRoute = recalculateCarrierRouteDepotFuel(
        activeCarrierRoute,
        nextJumpIndex,
        nextDepot,
      )
      setActiveCarrierRoute(updatedRoute)
      persistRouteState({ route: updatedRoute, jumpIdx: nextJumpIndex })
    } catch (jumpError) {
      console.error(jumpError)
    } finally {
      if (mountedRef.current && expeditionIdRef.current === requestExpeditionId) {
        setLoggingJump(false)
      }
      endBusyTask()
    }
  }

  async function handleLogManualJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const jumpDistance = parseFloat(manualDistanceInputRef.current) || 0
    const tritiumAmount = Math.max(0, parseInt(manualTritiumInputRef.current, 10) || 0)
    if (jumpDistance <= 0 && tritiumAmount <= 0) return
    const requestExpeditionId = currentExpeditionId
    const requestSeq = routeRequestSeqRef.current
    cancelPendingInputPersist()
    setLoggingJump(true)
    const endBusyTask = busyTracker.beginTask('LOGGING CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, jumpDistance, tritiumAmount)
      if (isStaleRouteRequest(requestExpeditionId, requestSeq)) return
      const nextJumps = manualLegJumps + 1
      const nextTotalDistance = manualLegDistance + Math.max(0, jumpDistance)
      const nextTotalTritium = manualLegTritium + tritiumAmount
      manualDistanceInputRef.current = ''
      manualTritiumInputRef.current = ''
      setManualLegJumps(nextJumps)
      setManualLegDistance(nextTotalDistance)
      setManualLegTritium(nextTotalTritium)
      setManualDistanceInput('')
      setManualTritiumInput('')
      persistRouteState({
        manualLegJumps: nextJumps,
        manualLegDist: nextTotalDistance,
        manualLegTrit: nextTotalTritium,
        manualDistInput: '',
        manualTritInput: '',
      })
    } catch (jumpError) {
      console.error(jumpError)
    } finally {
      if (mountedRef.current && expeditionIdRef.current === requestExpeditionId) {
        setLoggingJump(false)
      }
      endBusyTask()
    }
  }

  return {
    routePrompt,
    setRoutePrompt,
    handleCloseRoutePrompt,
    activeCarrierRoute,
    activeCarrierTargetRowId,
    carrierJumpIndex,
    isManualCarrierRoute,
    nextFcStopEntry,
    nextFcStopSystem,
    routedCarrierStopIndex,
    manualDistanceInput,
    manualTritiumInput,
    manualLegJumps,
    manualLegDistance,
    manualLegTritium,
    loggingJump,
    copiedNextJump,
    handleChangeManualDistance,
    handleChangeManualTritium,
    handleCopyNextJumpSystem,
    handleCancelCarrierRoute,
    switchActiveRouteToManualMode,
    openCarrierRoutePrompt,
    handleStartCarrierRoute,
    handleRefillDepot,
    handleCompleteSpanshJump,
    handleLogManualJump,
    handleCarrierArriveAtWaypoint,
  }
}

