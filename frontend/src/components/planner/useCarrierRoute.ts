import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ClearActiveCarrierRoute,
  GetActiveCarrierRoute,
  PlotCarrierRoute,
  RecordCarrierJump,
  SaveActiveCarrierRoute,
} from '../../../wailsjs/go/app/App'
import { app, expedition } from '../../../wailsjs/go/models'
import type { CarrierRoutePromptState } from './CarrierRouteModal'
import {
  computeRoutedCarrierStopIndex,
  findNextCarrierStopEntry,
  recalculateCarrierRouteDepotFuel,
  resolveCarrierDefaults,
  resolveCarrierStartSystem,
} from './carrierRouteHelpers'

/**
 * UseCarrierRouteOptions defines the expedition plan context and callbacks
 * needed to manage Spansh and manual Fleet Carrier routing.
 */
export interface UseCarrierRouteOptions {
  plan: expedition.PlanView | null
  lastVisitedIndex: number
  onBusyChange: (busyMessage: string | null) => void
  onSetFcArrived: (rowId: number, arrived: boolean) => Promise<void>
  onRegisterManualPlotter?: (openManualPlotter: () => void) => void
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

/**
 * useCarrierRoute manages Fleet Carrier route setup, persistence across restarts,
 * Spansh jump progression, depot fuel recalculation, and manual jump logging.
 */
export function useCarrierRoute({
  plan,
  lastVisitedIndex,
  onBusyChange,
  onSetFcArrived,
  onRegisterManualPlotter,
  onCarrierRouteOpenChange,
}: UseCarrierRouteOptions) {
  const [routePrompt, setRoutePrompt] = useState<CarrierRoutePromptState | null>(null)
  const [activeCarrierRoute, setActiveCarrierRoute] = useState<app.CarrierRouteResult | null>(null)
  const [activeCarrierTargetRowId, setActiveCarrierTargetRowId] = useState<number>(0)
  const [carrierJumpIndex, setCarrierJumpIndex] = useState(0)
  const [manualModeOverride, setManualModeOverride] = useState(false)
  const [manualDistanceInput, setManualDistanceInput] = useState('')
  const [manualTritiumInput, setManualTritiumInput] = useState('')
  const manualDistanceInputRef = useRef('')
  const manualTritiumInputRef = useRef('')
  const persistManualInputsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [manualLegJumps, setManualLegJumps] = useState(0)
  const [manualLegDistance, setManualLegDistance] = useState(0)
  const [manualLegTritium, setManualLegTritium] = useState(0)
  const [loggingJump, setLoggingJump] = useState(false)
  const [copiedNextJump, setCopiedNextJump] = useState(false)
  const copiedNextJumpTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (copiedNextJumpTimerRef.current) clearTimeout(copiedNextJumpTimerRef.current)
    }
  }, [])

  // Load persisted carrier route state on mount or when active expedition changes
  useEffect(() => {
    manualDistanceInputRef.current = ''
    manualTritiumInputRef.current = ''
    setRoutePrompt(null)
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    const expeditionId = plan?.expedition?.id
    if (!expeditionId || expeditionId <= 0) return
    let cancelled = false
    GetActiveCarrierRoute(expeditionId)
      .then((saved) => {
        if (cancelled) return
        if (
          saved &&
          saved.route &&
          (saved.targetRowId > 0 ||
            saved.manualMode ||
            saved.route.manualMode ||
            (saved.route.jumps && saved.route.jumps.length > 0))
        ) {
          const savedDistanceInput = saved.manualDistInput || ''
          const savedTritiumInput = saved.manualTritInput || ''
          manualDistanceInputRef.current = savedDistanceInput
          manualTritiumInputRef.current = savedTritiumInput
          const maxJumpIndex =
            saved.route.jumps && saved.route.jumps.length > 0
              ? saved.route.jumps.length - 1
              : 0
          setActiveCarrierRoute(saved.route)
          setActiveCarrierTargetRowId(saved.targetRowId)
          setCarrierJumpIndex(Math.min(Math.max(0, saved.jumpIdx || 0), maxJumpIndex))
          setManualModeOverride(Boolean(saved.manualMode))
          setManualDistanceInput(savedDistanceInput)
          setManualTritiumInput(savedTritiumInput)
          setManualLegJumps(saved.manualLegJumps || 0)
          setManualLegDistance(saved.manualLegDist || 0)
          setManualLegTritium(saved.manualLegTrit || 0)
        } else {
          setActiveCarrierRoute(null)
          setActiveCarrierTargetRowId(0)
        }
      })
      .catch(console.error)

    return () => {
      cancelled = true
      flushPendingManualInputs()
    }
  }, [plan?.expedition?.id])

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

  function persistRouteState(updates: {
    route?: app.CarrierRouteResult | null
    targetRowId?: number
    jumpIdx?: number
    manualMode?: boolean
    manualDistInput?: string
    manualTritInput?: string
    manualLegJumps?: number
    manualLegDist?: number
    manualLegTrit?: number
  }) {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
    const expeditionId = plan?.expedition?.id
    if (!expeditionId || expeditionId <= 0) return
    const route = updates.route !== undefined ? updates.route : activeCarrierRoute
    const targetRowId =
      updates.targetRowId !== undefined ? updates.targetRowId : activeCarrierTargetRowId
    if (!route) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
      return
    }
    const state = app.ActiveCarrierRouteState.createFrom({
      expeditionId,
      targetRowId,
      route,
      jumpIdx: updates.jumpIdx !== undefined ? updates.jumpIdx : carrierJumpIndex,
      manualMode: updates.manualMode !== undefined ? updates.manualMode : manualModeOverride,
      manualDistInput:
        updates.manualDistInput !== undefined
          ? updates.manualDistInput
          : manualDistanceInputRef.current,
      manualTritInput:
        updates.manualTritInput !== undefined
          ? updates.manualTritInput
          : manualTritiumInputRef.current,
      manualLegJumps: updates.manualLegJumps !== undefined ? updates.manualLegJumps : manualLegJumps,
      manualLegDist: updates.manualLegDist !== undefined ? updates.manualLegDist : manualLegDistance,
      manualLegTrit: updates.manualLegTrit !== undefined ? updates.manualLegTrit : manualLegTritium,
    })
    SaveActiveCarrierRoute(state).catch(console.error)
  }

  const persistRouteStateRef = useRef(persistRouteState)
  useEffect(() => {
    persistRouteStateRef.current = persistRouteState
  }, [
    plan?.expedition?.id,
    activeCarrierRoute,
    activeCarrierTargetRowId,
    carrierJumpIndex,
    manualModeOverride,
    manualLegJumps,
    manualLegDistance,
    manualLegTritium,
  ])

  function flushPendingManualInputs() {
    if (!persistManualInputsTimerRef.current) return
    clearTimeout(persistManualInputsTimerRef.current)
    persistManualInputsTimerRef.current = null
    persistRouteStateRef.current({
      manualDistInput: manualDistanceInputRef.current,
      manualTritInput: manualTritiumInputRef.current,
    })
  }

  // Resets per-leg counters and inputs and persists a newly started Spansh or manual carrier route.
  function activateCarrierRoute(
    route: app.CarrierRouteResult,
    targetRowId: number,
    manualMode: boolean,
  ) {
    manualDistanceInputRef.current = ''
    manualTritiumInputRef.current = ''
    setActiveCarrierTargetRowId(targetRowId)
    setActiveCarrierRoute(route)
    setCarrierJumpIndex(0)
    setManualModeOverride(manualMode)
    setManualLegJumps(0)
    setManualLegDistance(0)
    setManualLegTritium(0)
    setManualDistanceInput('')
    setManualTritiumInput('')
    persistRouteState({
      route,
      targetRowId,
      jumpIdx: 0,
      manualMode,
      manualLegJumps: 0,
      manualLegDist: 0,
      manualLegTrit: 0,
      manualDistInput: '',
      manualTritInput: '',
    })
  }

  function schedulePersistManualInputs() {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
    }
    persistManualInputsTimerRef.current = setTimeout(() => {
      persistManualInputsTimerRef.current = null
      persistRouteStateRef.current({
        manualDistInput: manualDistanceInputRef.current,
        manualTritInput: manualTritiumInputRef.current,
      })
    }, 250)
  }

  function handleChangeManualDistance(value: string) {
    manualDistanceInputRef.current = value
    schedulePersistManualInputs()
  }

  function handleChangeManualTritium(value: string) {
    manualTritiumInputRef.current = value
    schedulePersistManualInputs()
  }

  function handleCopyNextJumpSystem(systemName: string) {
    navigator.clipboard?.writeText(systemName).catch(() => {})
    if (copiedNextJumpTimerRef.current) {
      clearTimeout(copiedNextJumpTimerRef.current)
    }
    setCopiedNextJump(true)
    copiedNextJumpTimerRef.current = setTimeout(() => {
      setCopiedNextJump(false)
      copiedNextJumpTimerRef.current = null
    }, 1500)
  }

  function handleCancelCarrierRoute() {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
    const expeditionId = plan?.expedition?.id
    if (expeditionId) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
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
  // recorded against the expedition adjustments, while the Mark FC Stop Arrival
  // action remains disabled unless the commander later flags an upcoming waypoint
  // as a Carrier Stop.
  async function openManualCarrierPlotter() {
    if (!plan || plan.expedition?.status === 'completed' || activeCarrierRoute) return

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

    const defaultCarrierType: 'player' | 'squadron' =
      plan.expedition?.carrierType === 'squadron' ? 'squadron' : 'player'
    const manualRoute = app.CarrierRouteResult.createFrom({
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

  useEffect(() => {
    onRegisterManualPlotter?.(() => {
      void openManualPlotterRef.current()
    })
  }, [onRegisterManualPlotter])

  useEffect(() => {
    onCarrierRouteOpenChange?.(Boolean(activeCarrierRoute))
  }, [activeCarrierRoute, onCarrierRouteOpenChange])

  async function openCarrierRoutePrompt(rowIndex: number, targetRow: expedition.Row) {
    if (!plan) return
    const [startSystem, defaults] = await Promise.all([
      resolveCarrierStartSystem(plan, rowIndex),
      resolveCarrierDefaults(),
    ])
    const defaultCarrierType: 'player' | 'squadron' =
      plan.expedition?.carrierType === 'squadron' ? 'squadron' : 'player'
    setRoutePrompt({
      targetRowId: targetRow.id,
      origin: startSystem,
      destination: (targetRow.systemName ?? '').trim(),
      capacityUsed: String(defaults.capacity),
      depotTritium: String(defaults.depot),
      carrierType: defaultCarrierType,
      loading: false,
    })
  }

  async function handleStartCarrierRoute() {
    if (!routePrompt) return
    const targetWaypointRowId = routePrompt.targetRowId
    const maxCarrierCapacityTonnes = routePrompt.carrierType === 'squadron' ? 60000 : 25000
    const carrierCapacity = Math.max(
      0,
      Math.min(maxCarrierCapacityTonnes, parseInt(routePrompt.capacityUsed, 10) || 0),
    )
    const carrierDepot = Math.max(
      0,
      Math.min(1000, parseInt(routePrompt.depotTritium, 10) || 1000),
    )

    setRoutePrompt((previous) => (previous ? { ...previous, loading: true } : null))
    onBusyChange('PLOTTING SPANSH CARRIER ROUTE')
    try {
      const routeResult = await PlotCarrierRoute(
        routePrompt.origin.trim(),
        routePrompt.destination.trim(),
        carrierCapacity,
        routePrompt.carrierType,
        carrierDepot,
      )
      setRoutePrompt(null)
      activateCarrierRoute(routeResult, targetWaypointRowId, Boolean(routeResult.manualMode))
    } catch (routeError: unknown) {
      const errorRoute = app.CarrierRouteResult.createFrom({
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
      onBusyChange(null)
    }
  }

  function handleUpdateCarrierDepot(newDepot: number, newCapacityUsed?: number) {
    if (!activeCarrierRoute) return
    const updatedRoute = recalculateCarrierRouteDepotFuel(
      activeCarrierRoute,
      carrierJumpIndex,
      newDepot,
      newCapacityUsed,
    )
    setActiveCarrierRoute(updatedRoute)
    persistRouteState({ route: updatedRoute })
  }

  function handleRefillDepot() {
    if (!activeCarrierRoute) return
    // Refill depot to full (1000 or max possible) from storage.
    // If storage (used capacity) is available, transfer up to needed tonnes from storage into depot.
    const currentDepot =
      activeCarrierRoute.depotTritium !== undefined && activeCarrierRoute.depotTritium >= 0
        ? activeCarrierRoute.depotTritium
        : 1000
    const neededTonnes = 1000 - currentDepot
    if (neededTonnes <= 0) return

    const currentCapacity = activeCarrierRoute.capacityUsed || 0
    const transferTonnes =
      currentCapacity > 0 ? Math.min(neededTonnes, currentCapacity) : neededTonnes
    const nextDepot = currentDepot + transferTonnes
    const nextCapacity =
      currentCapacity > 0 ? Math.max(0, currentCapacity - transferTonnes) : currentCapacity

    handleUpdateCarrierDepot(nextDepot, nextCapacity)
  }

  async function handleCompleteSpanshJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const carrierHop = activeCarrierRoute.jumps?.[carrierJumpIndex]
    if (!carrierHop) return

    // Verify depot has sufficient tritium for this jump before proceeding
    const currentDepot =
      activeCarrierRoute.depotTritium !== undefined && activeCarrierRoute.depotTritium >= 0
        ? activeCarrierRoute.depotTritium
        : 1000
    if (currentDepot < carrierHop.tritiumUsed) {
      return
    }

    setLoggingJump(true)
    onBusyChange('LOGGING CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, carrierHop.distanceLy, carrierHop.tritiumUsed)
      const nextJumpIndex = carrierJumpIndex + 1
      if (nextJumpIndex >= (activeCarrierRoute.jumps?.length ?? 0)) {
        await handleCarrierArriveAtWaypoint()
        return
      }
      setCarrierJumpIndex(nextJumpIndex)
      // Deduct consumed fuel from the depot
      const nextDepot = Math.max(0, currentDepot - carrierHop.tritiumUsed)
      // Recalculate remaining hops based on the updated depot fuel level
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
      setLoggingJump(false)
      onBusyChange(null)
    }
  }

  async function handleLogManualJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const jumpDistance = parseFloat(manualDistanceInputRef.current) || 0
    const tritiumAmount = Math.max(0, parseInt(manualTritiumInputRef.current, 10) || 0)
    if (jumpDistance <= 0 && tritiumAmount <= 0) return
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
    setLoggingJump(true)
    onBusyChange('LOGGING MANUAL CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, jumpDistance, tritiumAmount)
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
      setLoggingJump(false)
      onBusyChange(null)
    }
  }

  async function handleCarrierArriveAtWaypoint() {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
    const targetRowId = isManualCarrierRoute
      ? (nextFcStopEntry?.row.id ?? 0)
      : activeCarrierTargetRowId || (nextFcStopEntry?.row.id ?? 0)
    const expeditionId = plan?.expedition?.id
    if (expeditionId) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    if (targetRowId > 0) {
      await onSetFcArrived(targetRowId, true)
    }
  }

  return {
    routePrompt,
    setRoutePrompt,
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
