import { useEffect, useRef, useState } from 'react'
import {
  ClearActiveCarrierRoute,
  GetActiveCarrierRoute,
  SaveActiveCarrierRoute,
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
 * restoration when switching expeditions, and debounced manual input persistence.
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

  function cancelPendingInputPersist() {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
  }

  function persistRouteState(updates: PersistedCarrierRouteUpdates) {
    cancelPendingInputPersist()
    const targetExpeditionId =
      updates.expeditionId !== undefined ? updates.expeditionId : expeditionId
    if (!targetExpeditionId || targetExpeditionId <= 0) return
    const route = updates.route !== undefined ? updates.route : activeCarrierRoute
    const targetRowId =
      updates.targetRowId !== undefined ? updates.targetRowId : activeCarrierTargetRowId
    if (!route) {
      ClearActiveCarrierRoute(targetExpeditionId).catch(console.error)
      return
    }
    const state = expedition.ActiveCarrierRouteState.createFrom({
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
    SaveActiveCarrierRoute(state).catch(console.error)
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
    manualDistanceInputRef.current = ''
    manualTritiumInputRef.current = ''
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
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
      flushPendingManualInputs(expeditionId)
    }
  }, [expeditionId])

  function activateCarrierRoute(
    route: expedition.CarrierRouteResult,
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

  function clearPersistedCarrierRoute() {
    cancelPendingInputPersist()
    if (expeditionId) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
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
    persistRouteState,
    activateCarrierRoute,
    clearPersistedCarrierRoute,
    handleChangeManualDistance,
    handleChangeManualTritium,
  }
}

