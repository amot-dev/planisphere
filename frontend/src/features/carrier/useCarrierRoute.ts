import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import {
  CancelCarrierRoutePlot,
  ClearActiveCarrierRouteWithUndo,
  CompleteCarrierRouteArrival,
  CompleteSpanshCarrierJump,
  LogManualCarrierJump,
  OpenManualCarrierRoute,
  PrepareCarrierRoutePrompt,
  RefillCarrierDepot,
  SaveCarrierManualDraftInputs,
  StartCarrierRoute,
  SwitchCarrierRouteToManual,
} from '../../api/backend'
import { expedition } from '../../api/models'
import { createBusyTaskTracker, type BusyTaskTracker } from '../../hooks/useBusyTasks'
import { useCopyFeedback } from '../../hooks/useCopyFeedback'
import type { CarrierRoutePromptState } from './CarrierRouteModal'

/** NextCarrierStopEntry pairs the next unarrived Fleet Carrier stop row with its row index. */
export interface NextCarrierStopEntry {
  row: expedition.Row
  index: number
}

/** UseCarrierRouteOptions configures Fleet Carrier routing state and callbacks. */
export interface UseCarrierRouteOptions {
  plan: expedition.PlanView | null
  dbPath?: string
  manualPlotterRequestCount?: number
  onBusyChange: (busyMessage: string | null) => void
  busyTracker?: BusyTaskTracker
  onPlanOptimisticUpdate?: (next: expedition.PlanView) => void
  onRefreshPlan?: () => void
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

interface CarrierScopeSnapshot {
  plan: expedition.PlanView | null
  expeditionId: number | undefined
  dbPath: string
  onPlanOptimisticUpdate?: (next: expedition.PlanView) => void
  onRefreshPlan?: () => void
}

function isManualCarrierRouteState(state: expedition.ActiveCarrierRouteState | null | undefined): boolean {
  return Boolean(state && (state.manualMode || state.route?.manualMode || !state.route?.jumps?.length))
}

function buildOptimisticCarrierRoutePlan(
  currentPlan: expedition.PlanView,
  nextState: expedition.ActiveCarrierRouteState | null,
): expedition.PlanView {
  const isManual = isManualCarrierRouteState(nextState)
  const hasTargetRow = Boolean(nextState && nextState.targetRowId > 0 && currentPlan.rows?.some((rowView) => rowView?.row?.id === nextState.targetRowId))
  const routedStopId = !nextState ? 0 : !isManual && hasTargetRow ? nextState.targetRowId : (currentPlan.nextFcStopRowId || 0)
  return Object.assign(Object.create(Object.getPrototypeOf(currentPlan)) as expedition.PlanView, currentPlan, {
    activeCarrierRoute: nextState ?? undefined,
    routedCarrierStopRowId: routedStopId || undefined,
  })
}

function useCarrierManualDrafts(
  latestRef: MutableRefObject<CarrierScopeSnapshot>,
  mountedRef: MutableRefObject<boolean>,
  backendDistanceInput: string,
  backendTritiumInput: string,
) {
  const [draftInputs, setDraftInputs] = useState({ distance: backendDistanceInput, tritium: backendTritiumInput })
  const draftInputsRef = useRef(draftInputs)
  const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const inFlightPersistCountRef = useRef(0)
  const draftScopeTokenRef = useRef(0)

  const cancelPendingPersist = useCallback(() => {
    if (persistTimerRef.current !== null) {
      clearTimeout(persistTimerRef.current)
      persistTimerRef.current = null
    }
  }, [])

  const syncDrafts = useCallback((source: expedition.ActiveCarrierRouteState | null | undefined) => {
    draftScopeTokenRef.current += 1
    inFlightPersistCountRef.current = 0
    const nextDraft = { distance: source?.manualDistInput ?? '', tritium: source?.manualTritInput ?? '' }
    draftInputsRef.current = nextDraft
    setDraftInputs((previous) => (previous.distance === nextDraft.distance && previous.tritium === nextDraft.tritium ? previous : nextDraft))
  }, [])

  useEffect(() => {
    if (persistTimerRef.current === null && inFlightPersistCountRef.current === 0) {
      const nextDraft = { distance: backendDistanceInput, tritium: backendTritiumInput }
      draftInputsRef.current = nextDraft
      setDraftInputs((previous) => (previous.distance === backendDistanceInput && previous.tritium === backendTritiumInput ? previous : nextDraft))
    }
  }, [backendDistanceInput, backendTritiumInput])

  const handleScopeDraftTransition = useCallback((previousExpeditionId: number | undefined, dbChanged: boolean) => {
    const hadPendingTimer = persistTimerRef.current !== null
    const { distance, tritium } = draftInputsRef.current
    cancelPendingPersist()
    draftScopeTokenRef.current += 1
    inFlightPersistCountRef.current = 0
    if (!dbChanged && hadPendingTimer && previousExpeditionId && previousExpeditionId > 0) {
      SaveCarrierManualDraftInputs(previousExpeditionId, distance, tritium).catch(console.error)
    }
    syncDrafts(latestRef.current.plan?.activeCarrierRoute)
  }, [cancelPendingPersist, latestRef, syncDrafts])

  const updateManualInput = useCallback((field: 'distance' | 'tritium', value: string) => {
    const nextDraft = { ...draftInputsRef.current, [field]: value }
    draftInputsRef.current = nextDraft
    setDraftInputs(nextDraft)
    cancelPendingPersist()
    const { expeditionId: scheduledExpeditionId, dbPath: scheduledDbPath } = latestRef.current
    if (!scheduledExpeditionId || scheduledExpeditionId <= 0) return
    persistTimerRef.current = setTimeout(() => {
      persistTimerRef.current = null
      if (!mountedRef.current || latestRef.current.dbPath !== scheduledDbPath || latestRef.current.expeditionId !== scheduledExpeditionId) return
      const persistScopeToken = draftScopeTokenRef.current
      inFlightPersistCountRef.current += 1
      SaveCarrierManualDraftInputs(scheduledExpeditionId, draftInputsRef.current.distance, draftInputsRef.current.tritium)
        .catch(console.error)
        .finally(() => {
          if (draftScopeTokenRef.current === persistScopeToken) inFlightPersistCountRef.current = Math.max(0, inFlightPersistCountRef.current - 1)
        })
    }, 250)
  }, [cancelPendingPersist, latestRef, mountedRef])

  return {
    draftInputs,
    draftInputsRef,
    cancelPendingPersist,
    syncDrafts,
    handleScopeDraftTransition,
    handleChangeManualDistance: useCallback((value: string) => updateManualInput('distance', value), [updateManualInput]),
    handleChangeManualTritium: useCallback((value: string) => updateManualInput('tritium', value), [updateManualInput]),
  }
}

function useCarrierRouteController(
  latestRef: MutableRefObject<CarrierScopeSnapshot>,
  mountedRef: MutableRefObject<boolean>,
  busyTracker: BusyTaskTracker,
  cancelPendingPersist: () => void,
  syncDrafts: (source: expedition.ActiveCarrierRouteState | null | undefined) => void,
) {
  const [routePrompt, setRoutePrompt] = useState<CarrierRoutePromptState | null>(null)
  const routePromptRef = useRef<CarrierRoutePromptState | null>(null)
  routePromptRef.current = routePrompt
  const [loggingJump, setLoggingJump] = useState(false)
  const loggingJumpRef = useRef(false)
  const routeRequestSeqRef = useRef(0)
  const plotInFlightRef = useRef(false)
  const activePlotBusyEndRef = useRef<(() => void) | null>(null)

  const invalidateRequests = useCallback((cancelPlot = false) => {
    routeRequestSeqRef.current += 1
    activePlotBusyEndRef.current?.()
    activePlotBusyEndRef.current = null
    if (cancelPlot && plotInFlightRef.current) {
      plotInFlightRef.current = false
      CancelCarrierRoutePlot().catch(console.error)
    }
    return routeRequestSeqRef.current
  }, [])

  const resetControllerScope = useCallback(() => {
    invalidateRequests(true)
    loggingJumpRef.current = false
    setRoutePrompt(null)
    setLoggingJump(false)
  }, [invalidateRequests])

  const isStaleRequest = useCallback(
    (requestExpeditionId: number | undefined, requestDbPath: string, requestSeq: number) =>
      !mountedRef.current || latestRef.current.expeditionId !== requestExpeditionId || latestRef.current.dbPath !== requestDbPath || routeRequestSeqRef.current !== requestSeq,
    [latestRef, mountedRef],
  )

  const applyOptimisticState = useCallback((nextState: expedition.ActiveCarrierRouteState | null) => {
    const { plan: currentPlan, onPlanOptimisticUpdate: updatePlan } = latestRef.current
    if (!currentPlan || !updatePlan) return
    const nextPlan = buildOptimisticCarrierRoutePlan(currentPlan, nextState)
    latestRef.current.plan = nextPlan
    updatePlan(nextPlan)
  }, [latestRef])

  const runCarrierAction = useCallback(async (
    action: (expeditionId: number) => Promise<expedition.ActiveCarrierRouteState | null>,
    options: { busyMessage?: string; syncDrafts?: boolean; cancelPlot?: boolean; allowNull?: boolean } = {},
  ) => {
    const { plan: requestPlan, expeditionId: requestExpeditionId, dbPath: requestDbPath } = latestRef.current
    if (!requestExpeditionId || loggingJumpRef.current) return
    const requestSeq = options.cancelPlot ? invalidateRequests(true) : routeRequestSeqRef.current
    if (isStaleRequest(requestExpeditionId, requestDbPath, requestSeq)) return
    let endBusyTask: (() => void) | null = null
    if (options.busyMessage) {
      cancelPendingPersist()
      loggingJumpRef.current = true
      setLoggingJump(true)
      endBusyTask = busyTracker.beginTask(options.busyMessage)
    }
    try {
      const nextState = await action(requestExpeditionId)
      if (!isStaleRequest(requestExpeditionId, requestDbPath, requestSeq) && (nextState || options.allowNull)) {
        if (options.syncDrafts) syncDrafts(nextState)
        if (latestRef.current.plan === requestPlan) applyOptimisticState(nextState ?? null)
      }
    } catch (actionError) {
      console.error(actionError)
    } finally {
      if (endBusyTask) {
        if (latestRef.current.expeditionId === requestExpeditionId && latestRef.current.dbPath === requestDbPath) {
          loggingJumpRef.current = false
          if (mountedRef.current) setLoggingJump(false)
        }
        endBusyTask()
      }
    }
  }, [applyOptimisticState, busyTracker, cancelPendingPersist, invalidateRequests, isStaleRequest, latestRef, mountedRef, syncDrafts])

  const openCarrierRoutePrompt = useCallback(async (_rowIndex: number, targetRow: expedition.Row) => {
    const { plan: currentPlan, expeditionId: requestExpeditionId, dbPath: requestDbPath } = latestRef.current
    if (!currentPlan || !requestExpeditionId) return
    const requestSeq = invalidateRequests(true)
    try {
      const defaults = await PrepareCarrierRoutePrompt(requestExpeditionId, targetRow.id)
      if (!isStaleRequest(requestExpeditionId, requestDbPath, requestSeq)) {
        setRoutePrompt({
          targetRowId: targetRow.id,
          origin: defaults.origin,
          destination: (targetRow.systemName ?? '').trim() || defaults.destination,
          capacityUsed: String(defaults.capacityUsed),
          depotTritium: String(defaults.depotTritium),
          carrierType: defaults.carrierType === 'squadron' ? 'squadron' : 'player',
          loading: false,
        })
      }
    } catch (promptError) {
      console.error(promptError)
    }
  }, [invalidateRequests, isStaleRequest, latestRef])

  const handleCloseRoutePrompt = useCallback(() => {
    invalidateRequests(true)
    setRoutePrompt(null)
  }, [invalidateRequests])

  const handleStartCarrierRoute = useCallback(async () => {
    const currentPrompt = routePromptRef.current
    const { plan: requestPlan, expeditionId: requestExpeditionId, dbPath: requestDbPath } = latestRef.current
    if (!currentPrompt || currentPrompt.loading || !requestExpeditionId) return
    const requestSeq = invalidateRequests(true)
    setRoutePrompt((previous) => (previous ? { ...previous, loading: true } : null))
    plotInFlightRef.current = true
    const endBusyTask = busyTracker.beginTask('PLOTTING SPANSH CARRIER ROUTE')
    activePlotBusyEndRef.current = endBusyTask
    try {
      const savedState = await StartCarrierRoute(requestExpeditionId, currentPrompt.targetRowId, currentPrompt.origin.trim(), currentPrompt.destination.trim(), parseInt(currentPrompt.capacityUsed, 10) || 0, currentPrompt.carrierType, parseInt(currentPrompt.depotTritium, 10) || 1000)
      if (!isStaleRequest(requestExpeditionId, requestDbPath, requestSeq)) {
        if (savedState) {
          setRoutePrompt(null)
          syncDrafts(savedState)
          if (latestRef.current.plan === requestPlan) applyOptimisticState(savedState)
        } else {
          setRoutePrompt((previous) => (previous ? { ...previous, loading: false } : null))
        }
      }
    } catch (routeError) {
      console.error(routeError)
      if (!isStaleRequest(requestExpeditionId, requestDbPath, requestSeq)) setRoutePrompt((previous) => (previous ? { ...previous, loading: false } : null))
    } finally {
      if (routeRequestSeqRef.current === requestSeq) plotInFlightRef.current = false
      if (activePlotBusyEndRef.current === endBusyTask) activePlotBusyEndRef.current = null
      endBusyTask()
    }
  }, [applyOptimisticState, busyTracker, invalidateRequests, isStaleRequest, latestRef, syncDrafts])

  return { routePrompt, setRoutePrompt, loggingJump, invalidateRequests, resetControllerScope, applyOptimisticState, runCarrierAction, openCarrierRoutePrompt, handleCloseRoutePrompt, handleStartCarrierRoute }
}

function useCarrierPanelCallbacks(
  latestRef: MutableRefObject<CarrierScopeSnapshot>,
  mountedRef: MutableRefObject<boolean>,
  drafts: ReturnType<typeof useCarrierManualDrafts>,
  controller: ReturnType<typeof useCarrierRouteController>,
) {
  const { draftInputsRef, cancelPendingPersist, syncDrafts } = drafts
  const { invalidateRequests, applyOptimisticState, runCarrierAction } = controller

  const handleCancelCarrierRoute = useCallback(() => {
    const { expeditionId: requestExpeditionId, dbPath: requestDbPath } = latestRef.current
    if (!requestExpeditionId) return
    cancelPendingPersist()
    invalidateRequests(true)
    syncDrafts(null)
    applyOptimisticState(null)
    ClearActiveCarrierRouteWithUndo(requestExpeditionId).catch((cancelError) => {
      console.error(cancelError)
      if (mountedRef.current && latestRef.current.expeditionId === requestExpeditionId && latestRef.current.dbPath === requestDbPath) {
        latestRef.current.onRefreshPlan?.()
      }
    })
  }, [applyOptimisticState, cancelPendingPersist, invalidateRequests, latestRef, mountedRef, syncDrafts])

  const switchActiveRouteToManualMode = useCallback(() => {
    if (!latestRef.current.plan?.activeCarrierRoute?.route) return
    cancelPendingPersist()
    void runCarrierAction((expeditionId) => SwitchCarrierRouteToManual(expeditionId), { syncDrafts: true })
  }, [cancelPendingPersist, latestRef, runCarrierAction])

  const handleRefillDepot = useCallback(() => {
    if (latestRef.current.plan?.activeCarrierRoute?.route) void runCarrierAction((expeditionId) => RefillCarrierDepot(expeditionId))
  }, [latestRef, runCarrierAction])

  const handleCompleteSpanshJump = useCallback(async () => {
    const currentPlan = latestRef.current.plan
    const currentRouteState = currentPlan?.activeCarrierRoute
    const currentRoute = currentRouteState?.route
    const totalJumps = currentRoute?.jumps?.length ?? 0
    if (!currentPlan || !currentRoute || totalJumps === 0) return
    const currentJumpIndex = currentRouteState?.jumpIdx ?? 0
    const allRecorded = currentJumpIndex >= totalJumps
    const currentHop = allRecorded ? null : currentRoute.jumps[currentJumpIndex]
    if (currentHop && (currentRoute.depotTritium ?? 1000) < currentHop.tritiumUsed) return
    await runCarrierAction((expeditionId) => CompleteSpanshCarrierJump(expeditionId), {
      busyMessage: allRecorded ? 'MARKING CARRIER ARRIVAL' : 'LOGGING CARRIER JUMP',
      syncDrafts: true,
      allowNull: true,
    })
  }, [latestRef, runCarrierAction])

  const handleLogManualJump = useCallback(async () => {
    if (!latestRef.current.plan?.activeCarrierRoute?.route) return
    const jumpDistance = Math.max(0, parseFloat(draftInputsRef.current.distance) || 0)
    const tritiumAmount = Math.max(0, parseInt(draftInputsRef.current.tritium, 10) || 0)
    if (jumpDistance <= 0 && tritiumAmount <= 0) return
    await runCarrierAction((expeditionId) => LogManualCarrierJump(expeditionId, jumpDistance, tritiumAmount), {
      busyMessage: 'LOGGING CARRIER JUMP',
      syncDrafts: true,
    })
  }, [draftInputsRef, latestRef, runCarrierAction])

  const handleCarrierArriveAtWaypoint = useCallback(async () => {
    if (!latestRef.current.plan) return
    await runCarrierAction(async (expeditionId) => {
      await CompleteCarrierRouteArrival(expeditionId, 0, 0, 0)
      return null
    }, { busyMessage: 'MARKING CARRIER ARRIVAL', syncDrafts: true, allowNull: true })
  }, [latestRef, runCarrierAction])

  return { handleCancelCarrierRoute, switchActiveRouteToManualMode, handleRefillDepot, handleCompleteSpanshJump, handleLogManualJump, handleCarrierArriveAtWaypoint }
}

/** useCarrierRoute binds the Fleet Carrier UI to PlanView.activeCarrierRoute and backend RPCs. */
export function useCarrierRoute({
  plan,
  dbPath = '',
  manualPlotterRequestCount = 0,
  onBusyChange,
  busyTracker: externalBusyTracker,
  onPlanOptimisticUpdate,
  onRefreshPlan,
  onCarrierRouteOpenChange,
}: UseCarrierRouteOptions) {
  const routeState = plan?.activeCarrierRoute ?? null
  const activeCarrierRoute = routeState?.route ?? null
  const currentExpeditionId = plan?.expedition?.id

  const latestRef = useRef<CarrierScopeSnapshot>({ plan, expeditionId: currentExpeditionId, dbPath, onPlanOptimisticUpdate, onRefreshPlan })
  latestRef.current = { plan, expeditionId: currentExpeditionId, dbPath, onPlanOptimisticUpdate, onRefreshPlan }
  const mountedRef = useRef(true)

  const onBusyChangeRef = useRef(onBusyChange)
  onBusyChangeRef.current = onBusyChange
  const fallbackTracker = useMemo(() => createBusyTaskTracker((busyMessage) => onBusyChangeRef.current(busyMessage)), [])
  const busyTracker = externalBusyTracker ?? fallbackTracker

  const drafts = useCarrierManualDrafts(latestRef, mountedRef, routeState?.manualDistInput ?? '', routeState?.manualTritInput ?? '')
  const controller = useCarrierRouteController(latestRef, mountedRef, busyTracker, drafts.cancelPendingPersist, drafts.syncDrafts)
  const panelCallbacks = useCarrierPanelCallbacks(latestRef, mountedRef, drafts, controller)

  const { cancelPendingPersist, handleScopeDraftTransition } = drafts
  const { invalidateRequests, resetControllerScope, runCarrierAction } = controller

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      cancelPendingPersist()
      invalidateRequests(true)
    }
  }, [cancelPendingPersist, invalidateRequests])

  const previousScopeRef = useRef({ expeditionId: currentExpeditionId, dbPath })
  useEffect(() => {
    const previousScope = previousScopeRef.current
    const dbChanged = previousScope.dbPath !== dbPath
    if (!dbChanged && previousScope.expeditionId === currentExpeditionId) return
    previousScopeRef.current = { expeditionId: currentExpeditionId, dbPath }
    resetControllerScope()
    handleScopeDraftTransition(previousScope.expeditionId, dbChanged)
  }, [currentExpeditionId, dbPath, handleScopeDraftTransition, resetControllerScope])

  useEffect(() => onCarrierRouteOpenChange?.(Boolean(activeCarrierRoute)), [activeCarrierRoute, onCarrierRouteOpenChange])

  const handledManualRequestRef = useRef(manualPlotterRequestCount)
  useEffect(() => {
    if (manualPlotterRequestCount <= handledManualRequestRef.current) return
    handledManualRequestRef.current = manualPlotterRequestCount
    const currentPlan = latestRef.current.plan
    if (!currentPlan || currentPlan.expedition?.status === 'completed' || currentPlan.activeCarrierRoute) return
    void runCarrierAction((expeditionId) => OpenManualCarrierRoute(expeditionId), { syncDrafts: true, cancelPlot: true })
  }, [manualPlotterRequestCount, runCarrierAction])

  const { copiedKey, copyWithFeedback } = useCopyFeedback<'next'>()
  const handleCopyNextJumpSystem = useCallback((systemName: string) => copyWithFeedback('next', systemName), [copyWithFeedback])

  const nextFcStopEntry = useMemo<NextCarrierStopEntry | null>(() => {
    if (!plan?.rows || !(plan.nextFcStopRowId > 0)) return null
    const stopIndex = plan.rows.findIndex((rowView) => rowView?.row?.id === plan.nextFcStopRowId)
    return stopIndex >= 0 ? { row: plan.rows[stopIndex].row, index: stopIndex } : null
  }, [plan?.nextFcStopRowId, plan?.rows])

  const routedCarrierStopIndex = useMemo(() => {
    if (!plan?.rows || !routeState || !(plan.routedCarrierStopRowId && plan.routedCarrierStopRowId > 0)) return -1
    return plan.rows.findIndex((rowView) => rowView?.row?.id === plan.routedCarrierStopRowId)
  }, [plan?.routedCarrierStopRowId, plan?.rows, routeState])

  return {
    routePrompt: controller.routePrompt,
    setRoutePrompt: controller.setRoutePrompt,
    handleCloseRoutePrompt: controller.handleCloseRoutePrompt,
    activeCarrierRoute,
    activeCarrierTargetRowId: routeState?.targetRowId ?? 0,
    carrierJumpIndex: routeState?.jumpIdx ?? 0,
    isManualCarrierRoute: isManualCarrierRouteState(routeState),
    nextFcStopEntry,
    nextFcStopSystem: nextFcStopEntry ? (nextFcStopEntry.row.systemName ?? '').trim() : '',
    routedCarrierStopIndex,
    manualDistanceInput: drafts.draftInputs.distance,
    manualTritiumInput: drafts.draftInputs.tritium,
    manualLegJumps: routeState?.manualLegJumps ?? 0,
    manualLegDistance: routeState?.manualLegDist ?? 0,
    manualLegTritium: routeState?.manualLegTrit ?? 0,
    loggingJump: controller.loggingJump,
    copiedNextJump: copiedKey === 'next',
    handleChangeManualDistance: drafts.handleChangeManualDistance,
    handleChangeManualTritium: drafts.handleChangeManualTritium,
    handleCopyNextJumpSystem,
    openCarrierRoutePrompt: controller.openCarrierRoutePrompt,
    handleStartCarrierRoute: controller.handleStartCarrierRoute,
    ...panelCallbacks,
  }
}
