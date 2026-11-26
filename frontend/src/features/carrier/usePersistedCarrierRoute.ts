import { useCallback, useEffect, useRef, useState } from 'react'
import {
  ClearActiveCarrierRoute,
  ClearActiveCarrierRouteWithUndo,
  GetActiveCarrierRoute,
  SaveActiveCarrierRoute,
  SaveActiveCarrierRouteWithUndo,
} from '../../api/backend'
import { expedition } from '../../api/models'

/**
 * PersistedCarrierRouteUpdates specifies partial field updates flushed to the
 * backend active carrier route record.
 */
export interface PersistedCarrierRouteUpdates {
  expeditionId?: number
  route?: expedition.CarrierRouteResult | null
  targetRowId?: number
  jumpIdx?: number
  manualMode?: boolean
  manualDistInput?: string
  manualTritInput?: string
  manualLegJumps?: number
  manualLegDist?: number
  manualLegTrit?: number
}

/**
 * usePersistedCarrierRoute manages active carrier route state, SQLite route
 * restoration when switching expeditions, on-demand route reloads, local state
 * clearing, debounced manual input persistence, and optional undo-labeled saves.
 */
export function usePersistedCarrierRoute(expeditionId: number | undefined) {
  const [activeCarrierRoute, setActiveCarrierRoute] =
    useState<expedition.CarrierRouteResult | null>(null)
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
  const mountedRef = useRef(true)
  const expeditionIdRef = useRef<number | undefined>(expeditionId)
  expeditionIdRef.current = expeditionId
  const loadSeqRef = useRef(0)

  const cancelPendingInputPersist = useCallback(() => {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
  }, [])

  const invalidatePersistedRouteLoad = useCallback(() => {
    loadSeqRef.current += 1
  }, [])

  const resetLocalCarrierRouteState = useCallback(() => {
    cancelPendingInputPersist()
    manualDistanceInputRef.current = ''
    manualTritiumInputRef.current = ''
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    setCarrierJumpIndex(0)
    setManualModeOverride(false)
    setManualDistanceInput('')
    setManualTritiumInput('')
    setManualLegJumps(0)
    setManualLegDistance(0)
    setManualLegTritium(0)
  }, [cancelPendingInputPersist])

  const applyLoadedCarrierRoute = useCallback(
    (saved: expedition.ActiveCarrierRouteState | null | undefined) => {
      cancelPendingInputPersist()
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
        const maxJumpIndex = saved.route.jumps ? saved.route.jumps.length : 0
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
        resetLocalCarrierRouteState()
      }
    },
    [cancelPendingInputPersist, resetLocalCarrierRouteState],
  )

  function buildPersistedRouteState(
    updates: PersistedCarrierRouteUpdates,
  ): expedition.ActiveCarrierRouteState | null {
    const targetExpeditionId =
      updates.expeditionId !== undefined ? updates.expeditionId : expeditionId
    if (!targetExpeditionId || targetExpeditionId <= 0) return null
    const route = updates.route !== undefined ? updates.route : activeCarrierRoute
    if (!route) return null
    const targetRowId =
      updates.targetRowId !== undefined ? updates.targetRowId : activeCarrierTargetRowId
    return expedition.ActiveCarrierRouteState.createFrom({
      expeditionId: targetExpeditionId,
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
      manualLegJumps:
        updates.manualLegJumps !== undefined ? updates.manualLegJumps : manualLegJumps,
      manualLegDist:
        updates.manualLegDist !== undefined ? updates.manualLegDist : manualLegDistance,
      manualLegTrit:
        updates.manualLegTrit !== undefined ? updates.manualLegTrit : manualLegTritium,
    })
  }

  function persistRouteState(updates: PersistedCarrierRouteUpdates, undoLabel?: string) {
    cancelPendingInputPersist()
    const targetExpeditionId =
      updates.expeditionId !== undefined ? updates.expeditionId : expeditionId
    if (!targetExpeditionId || targetExpeditionId <= 0) return
    if (targetExpeditionId === expeditionIdRef.current) {
      invalidatePersistedRouteLoad()
    }
    const routeState = buildPersistedRouteState(updates)
    if (!routeState) {
      const clearRequest = undoLabel
        ? ClearActiveCarrierRouteWithUndo(targetExpeditionId)
        : ClearActiveCarrierRoute(targetExpeditionId)
      clearRequest.catch(console.error)
      return
    }
    const saveRequest = undoLabel
      ? SaveActiveCarrierRouteWithUndo(routeState, undoLabel)
      : SaveActiveCarrierRoute(routeState)
    saveRequest.catch(console.error)
  }

  const persistRouteStateRef = useRef(persistRouteState)
  persistRouteStateRef.current = persistRouteState

  function flushPendingManualInputs(targetExpeditionId?: number) {
    if (!persistManualInputsTimerRef.current) return
    cancelPendingInputPersist()
    persistRouteStateRef.current({
      expeditionId: targetExpeditionId,
      manualDistInput: manualDistanceInputRef.current,
      manualTritInput: manualTritiumInputRef.current,
    })
  }

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      loadSeqRef.current += 1
    }
  }, [])

  const reloadPersistedCarrierRoute = useCallback(async (): Promise<void> => {
    const requestExpeditionId = expeditionIdRef.current
    if (!requestExpeditionId || requestExpeditionId <= 0) return
    cancelPendingInputPersist()
    const requestSeq = ++loadSeqRef.current
    try {
      const savedRoute = await GetActiveCarrierRoute(requestExpeditionId)
      if (
        !mountedRef.current ||
        expeditionIdRef.current !== requestExpeditionId ||
        loadSeqRef.current !== requestSeq
      ) {
        return
      }
      applyLoadedCarrierRoute(savedRoute)
    } catch (loadError) {
      console.error(loadError)
    }
  }, [applyLoadedCarrierRoute, cancelPendingInputPersist])

  useEffect(() => {
    resetLocalCarrierRouteState()
    if (!expeditionId || expeditionId <= 0) return
    void reloadPersistedCarrierRoute()
    return () => {
      loadSeqRef.current += 1
      flushPendingManualInputs(expeditionId)
    }
  }, [expeditionId, reloadPersistedCarrierRoute, resetLocalCarrierRouteState])

  function activateCarrierRoute(
    route: expedition.CarrierRouteResult,
    targetRowId: number,
    manualMode: boolean,
  ) {
    loadSeqRef.current += 1
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
    persistRouteState(
      {
        route,
        targetRowId,
        jumpIdx: 0,
        manualMode,
        manualLegJumps: 0,
        manualLegDist: 0,
        manualLegTrit: 0,
        manualDistInput: '',
        manualTritInput: '',
      },
      manualMode ? 'Open Manual Carrier Plotter' : 'Start Carrier Route',
    )
  }

  const clearLocalCarrierRoute = useCallback(() => {
    loadSeqRef.current += 1
    resetLocalCarrierRouteState()
  }, [resetLocalCarrierRouteState])

  function clearPersistedCarrierRoute(recordUndo = false) {
    loadSeqRef.current += 1
    resetLocalCarrierRouteState()
    if (expeditionId) {
      const clearRequest = recordUndo
        ? ClearActiveCarrierRouteWithUndo(expeditionId)
        : ClearActiveCarrierRoute(expeditionId)
      clearRequest.catch(console.error)
    }
  }

  function schedulePersistManualInputs() {
    cancelPendingInputPersist()
    const scheduledExpeditionId = expeditionId
    persistManualInputsTimerRef.current = setTimeout(() => {
      persistManualInputsTimerRef.current = null
      persistRouteStateRef.current({
        expeditionId: scheduledExpeditionId,
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

  return {
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
    invalidatePersistedRouteLoad,
    buildPersistedRouteState,
    persistRouteState,
    activateCarrierRoute,
    clearLocalCarrierRoute,
    clearPersistedCarrierRoute,
    reloadPersistedCarrierRoute,
    handleChangeManualDistance,
    handleChangeManualTritium,
  }
}

