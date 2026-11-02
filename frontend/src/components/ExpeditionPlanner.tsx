import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import {
  AddRow,
  ClearActiveCarrierRoute,
  CompleteExpedition,
  DeleteRow,
  GetActiveCarrierRoute,
  GetDefaultCarrierCapacity,
  GetWaypointDetails,
  MoveRow,
  PlotCarrierRoute,
  RecordCarrierJump,
  ReopenExpedition,
  SaveActiveCarrierRoute,
  SetActiveLegMode,
  SetLegMode,
  UpdateRow,
} from '../../wailsjs/go/app/App'
import { app, expedition } from '../../wailsjs/go/models'
import { formatLy } from '../utils/galacticTime'
import {
  CarrierRouteBottomPanel,
  LegBanner,
  RegionHeaderDropdown,
  RowManipulationButtons,
  StatusBadge,
  WaypointDetailsDrawer,
} from './PlannerWidgets'
import { CarrierRouteModal, type CarrierRoutePromptState } from './CarrierRouteModal'
import { WaypointRowItem } from './WaypointRowItem'

export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
  onPlanOptimisticUpdate: (next: expedition.PlanView) => void
  onOpenStartModal: () => void
  onRefreshPlan: () => void
  onRefreshAll: () => void
  onBusyChange: (busyMessage: string | null) => void
}

const rowStyle: CSSProperties = {
  contentVisibility: 'auto',
  containIntrinsicSize: '44px',
}

export function ExpeditionPlanner({
  plan,
  onPlanOptimisticUpdate,
  onOpenStartModal,
  onRefreshPlan,
  onRefreshAll,
  onBusyChange,
}: ExpeditionPlannerProps) {
  const [expandedRowIds, setExpandedRowIds] = useState<Record<number, boolean>>({})
  const [detailsById, setDetailsById] = useState<Record<number, expedition.WaypointDetails>>({})
  const [loadingDetails, setLoadingDetails] = useState<Record<number, boolean>>({})
  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)

  // Drag-and-drop state for unvisited / planning rows
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)

  // Carrier Route Setup Modal and Bottom Panel state
  const [routePrompt, setRoutePrompt] = useState<CarrierRoutePromptState | null>(null)
  const [activeCarrierRoute, setActiveCarrierRoute] = useState<app.CarrierRouteResult | null>(null)
  const [activeCarrierTargetRowId, setActiveCarrierTargetRowId] = useState<number>(0)
  const [carrierJumpIndex, setCarrierJumpIndex] = useState(0)
  const [manualModeOverride, setManualModeOverride] = useState(false)
  const [manualDistanceInput, setManualDistanceInput] = useState('')
  const [manualTritiumInput, setManualTritiumInput] = useState('')
  const [manualLegJumps, setManualLegJumps] = useState(0)
  const [manualLegDistance, setManualLegDistance] = useState(0)
  const [manualLegTritium, setManualLegTritium] = useState(0)
  const [loggingJump, setLoggingJump] = useState(false)
  const [copiedNextJump, setCopiedNextJump] = useState(false)
  const [copiedSystemRowId, setCopiedSystemRowId] = useState<number | null>(null)

  // Load persisted carrier route state on mount or when active expedition changes
  useEffect(() => {
    const expeditionId = plan?.expedition?.id
    if (!expeditionId || expeditionId <= 0) {
      setActiveCarrierRoute(null)
      setActiveCarrierTargetRowId(0)
      return
    }
    let cancelled = false
    GetActiveCarrierRoute(expeditionId)
      .then((saved) => {
        if (cancelled) return
        if (saved && saved.route && saved.targetRowId > 0) {
          setActiveCarrierRoute(saved.route)
          setActiveCarrierTargetRowId(saved.targetRowId)
          setCarrierJumpIndex(saved.jumpIdx || 0)
          setManualModeOverride(Boolean(saved.manualMode))
          setManualDistanceInput(saved.manualDistInput || '')
          setManualTritiumInput(saved.manualTritInput || '')
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
    }
  }, [plan?.expedition?.id])

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
    const expeditionId = plan?.expedition?.id
    if (!expeditionId || expeditionId <= 0) return
    const route = updates.route !== undefined ? updates.route : activeCarrierRoute
    const targetRowId = updates.targetRowId !== undefined ? updates.targetRowId : activeCarrierTargetRowId
    if (!route || targetRowId <= 0) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
      return
    }
    const state = app.ActiveCarrierRouteState.createFrom({
      expeditionId,
      targetRowId,
      route,
      jumpIdx: updates.jumpIdx !== undefined ? updates.jumpIdx : carrierJumpIndex,
      manualMode: updates.manualMode !== undefined ? updates.manualMode : manualModeOverride,
      manualDistInput: updates.manualDistInput !== undefined ? updates.manualDistInput : manualDistanceInput,
      manualTritInput: updates.manualTritInput !== undefined ? updates.manualTritInput : manualTritiumInput,
      manualLegJumps: updates.manualLegJumps !== undefined ? updates.manualLegJumps : manualLegJumps,
      manualLegDist: updates.manualLegDist !== undefined ? updates.manualLegDist : manualLegDistance,
      manualLegTrit: updates.manualLegTrit !== undefined ? updates.manualLegTrit : manualLegTritium,
    })
    SaveActiveCarrierRoute(state).catch(console.error)
  }

  function handleCancelCarrierRoute() {
    const expeditionId = plan?.expedition?.id
    if (expeditionId) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    setCarrierJumpIndex(0)
    setManualModeOverride(false)
  }

  function handleCopySystem(rowId: number, systemName: string) {
    if (!systemName) return
    const text = systemName.trim()
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(text).catch(console.error)
    } else {
      const textareaElement = document.createElement('textarea')
      textareaElement.value = text
      document.body.appendChild(textareaElement)
      textareaElement.select()
      document.execCommand('copy')
      document.body.removeChild(textareaElement)
    }
    setCopiedSystemRowId(rowId)
    setTimeout(() => {
      setCopiedSystemRowId((currentId) => (currentId === rowId ? null : currentId))
    }, 1500)
  }

  // Keep track of which row currently has user focus so background updates don't clobber active typing
  const editingRowIdRef = useRef<number | null>(null)

  // Local draft inputs so typing in spreadsheet cells is instantaneous
  const [drafts, setDrafts] = useState<
    Record<number, { systemName: string; poiName: string; notes: string }>
  >({})

  useEffect(() => {
    if (!plan || !plan.rows) return
    setDrafts((prev) => {
      const next: Record<number, { systemName: string; poiName: string; notes: string }> = { ...prev }
      const activeEditingId = editingRowIdRef.current
      for (const rowView of plan.rows ?? []) {
        if (!rowView?.row) continue
        const rowId = rowView.row.id
        // Never clobber the row that is currently focused and being typed into
        if (rowId === activeEditingId && prev[rowId]) {
          continue
        }
        next[rowId] = {
          systemName: rowView.row.systemName ?? '',
          poiName: rowView.row.poiName ?? '',
          notes: rowView.row.notes ?? '',
        }
      }
      return next
    })
  }, [plan])

  // Refresh open drawers when plan updates
  useEffect(() => {
    if (!plan) return
    for (const idStr of Object.keys(expandedRowIds)) {
      const id = Number(idStr)
      if (expandedRowIds[id]) {
        loadDetails(id)
      }
    }
  }, [plan])

  // Highest row index that has been visited; rows at or above this index are locked
  // against structural edits (reordering, deleting, or changing system name).
  const lastVisitedIndex = useMemo(() => {
    if (!plan || !plan.rows || plan.expedition?.status === 'planning') return -1
    let maxIndex = -1
    ;(plan.rows ?? []).forEach((rowView, index) => {
      if (rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')) {
        maxIndex = index
      }
    })
    return maxIndex
  }, [plan])

  function loadDetails(rowId: number) {
    setLoadingDetails((previous) => ({ ...previous, [rowId]: true }))
    GetWaypointDetails(rowId)
      .then((details) => setDetailsById((previous) => ({ ...previous, [rowId]: details })))
      .catch(console.error)
      .finally(() => setLoadingDetails((previous) => ({ ...previous, [rowId]: false })))
  }

  function toggleExpand(rowId: number) {
    const nextOpen = !expandedRowIds[rowId]
    setExpandedRowIds((previous) => ({ ...previous, [rowId]: nextOpen }))
    if (nextOpen) {
      loadDetails(rowId)
    }
  }

  async function commitField(
    row: expedition.Row,
    field: 'systemName' | 'poiName' | 'notes',
    value: string,
  ) {
    if ((row[field] ?? '') === value) return
    onBusyChange('UPDATING WAYPOINT')
    try {
      await UpdateRow(
        expedition.RowPatch.createFrom({
          id: row.id,
          [field]: value,
        }),
      )
    } finally {
      onBusyChange(null)
    }
  }

  async function toggleFcStop(row: expedition.Row) {
    if (!plan) return
    const nextFcStop = !row.fcStop
    const optimisticRows = plan.rows.map((rowView) => {
      if (rowView.row.id !== row.id) return rowView
      return expedition.RowView.createFrom({
        ...rowView,
        row: { ...rowView.row, fcStop: nextFcStop, fcArrived: nextFcStop ? rowView.row.fcArrived : false },
      })
    })
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: optimisticRows,
      }),
    )

    onBusyChange('UPDATING FC STOP')
    try {
      await UpdateRow(
        expedition.RowPatch.createFrom({
          id: row.id,
          fcStop: nextFcStop,
        }),
      )
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function setFcArrived(rowId: number, arrived: boolean) {
    if (!plan) return
    const optimisticRows = plan.rows.map((rowView) => {
      if (rowView.row.id !== rowId) return rowView
      return expedition.RowView.createFrom({
        ...rowView,
        row: { ...rowView.row, fcArrived: arrived },
      })
    })
    let nextFcId = 0
    for (const rowView of optimisticRows) {
      if (rowView.row.rowType === 'waypoint' && rowView.row.fcStop && !rowView.row.fcArrived) {
        nextFcId = rowView.row.id
        break
      }
    }
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: optimisticRows,
        nextFcStopRowId: nextFcId,
      }),
    )

    onBusyChange('UPDATING CARRIER ARRIVAL')
    try {
      await UpdateRow(
        expedition.RowPatch.createFrom({
          id: rowId,
          fcArrived: arrived,
        }),
      )
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleLegModeChange(rowId: number, modeId: number) {
    if (!plan) return
    const picked = (plan.modes ?? []).find((mode) => mode.id === modeId)
    const optimisticRows = plan.rows.map((rowView) => {
      if (rowView.row.id !== rowId) return rowView
      return expedition.RowView.createFrom({
        ...rowView,
        legModeId: modeId,
        legModeName: picked?.name ?? rowView.legModeName,
        legModeColor: picked?.color ?? rowView.legModeColor,
      })
    })
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: optimisticRows,
      }),
    )

    onBusyChange('UPDATING LEG MODE')
    try {
      await SetLegMode(rowId, modeId)
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleActiveLegModeChange(modeId: number) {
    if (!plan) return
    const picked = (plan.modes ?? []).find((mode) => mode.id === modeId)
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        activeModeId: modeId,
        activeModeName: picked?.name ?? plan.activeModeName,
        activeModeColor: picked?.color ?? plan.activeModeColor,
      }),
    )

    onBusyChange('UPDATING ACTIVE LEG MODE')
    try {
      await SetActiveLegMode(plan.expedition.id, modeId)
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
  }

  async function changeRegion(rowId: number, regionId: number) {
    onBusyChange('UPDATING GALACTIC REGION')
    try {
      await UpdateRow(
        expedition.RowPatch.createFrom({
          id: rowId,
          regionId,
        }),
      )
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleInsert(afterRowId: number, rowType: 'waypoint' | 'region') {
    if (!plan) return
    setInsertMenuAfterId(null)
    onBusyChange(rowType === 'region' ? 'ADDING REGION HEADER' : 'ADDING WAYPOINT')
    try {
      await AddRow(plan.expedition.id, afterRowId, rowType, 18)
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleDeleteRow(rowId: number) {
    if (!plan) return
    const filtered = plan.rows.filter((rowView) => rowView.row.id !== rowId)
    let wpNum = 0
    const renumbered = filtered.map((rowView) => {
      if (rowView.row.rowType === 'waypoint') {
        wpNum++
        return expedition.RowView.createFrom({ ...rowView, number: wpNum })
      }
      return rowView
    })
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: renumbered,
        waypointCount: wpNum,
      }),
    )

    onBusyChange('REMOVING ROW')
    try {
      await DeleteRow(rowId)
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleMoveRow(rowId: number, fromIndex: number, toIndex: number) {
    if (!plan) return
    const minAllowed = lastVisitedIndex + 1
    if (fromIndex < minAllowed || toIndex < minAllowed || toIndex >= plan.rows.length) {
      return
    }
    if (fromIndex === toIndex) return

    const nextRows = [...plan.rows]
    const [moved] = nextRows.splice(fromIndex, 1)
    nextRows.splice(toIndex, 0, moved)
    let wpNum = 0
    const renumbered = nextRows.map((rowView, i) => {
      const updatedRow = { ...rowView.row, sortOrder: i }
      if (rowView.row.rowType === 'waypoint') {
        wpNum++
        return expedition.RowView.createFrom({ ...rowView, row: updatedRow, number: wpNum })
      }
      return expedition.RowView.createFrom({ ...rowView, row: updatedRow })
    })
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: renumbered,
      }),
    )

    onBusyChange('REORDERING ROUTE')
    try {
      await MoveRow(rowId, toIndex)
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleLifecycleAction(label: string, actionFunction: () => Promise<void>) {
    onBusyChange(label)
    try {
      await actionFunction()
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
  }

  async function openCarrierRoutePrompt(rowIndex: number, targetRow: expedition.Row) {
    if (!plan) return
    // Prefer the most recent preceding FC stop as the carrier's origin, falling
    // back to the immediately preceding waypoint if no earlier FC stop exists.
    let prevSystem = ''
    for (let i = rowIndex - 1; i >= 0; i--) {
      const candidate = plan.rows[i].row
      if (candidate.rowType === 'waypoint' && candidate.fcStop && candidate.systemName?.trim()) {
        prevSystem = candidate.systemName.trim()
        break
      }
    }
    if (!prevSystem) {
      for (let i = rowIndex - 1; i >= 0; i--) {
        const candidate = plan.rows[i].row
        if (candidate.rowType === 'waypoint' && candidate.systemName?.trim()) {
          prevSystem = candidate.systemName.trim()
          break
        }
      }
    }
    let defaultCapacity = 5000
    try {
      defaultCapacity = await GetDefaultCarrierCapacity()
    } catch {
      defaultCapacity = 5000
    }
    setRoutePrompt({
      targetRowId: targetRow.id,
      origin: prevSystem,
      destination: (drafts[targetRow.id]?.systemName ?? targetRow.systemName ?? '').trim(),
      capacityUsed: String(defaultCapacity),
      carrierType: 'player',
      loading: false,
    })
  }

  async function handleStartCarrierRoute(forceManual: boolean) {
    if (!routePrompt) return
    const targetId = routePrompt.targetRowId
    const carrierCapacity = Math.max(0, Math.min(25000, parseInt(routePrompt.capacityUsed, 10) || 0))
    if (forceManual) {
      const manualRoute = app.CarrierRouteResult.createFrom({
        origin: routePrompt.origin.trim(),
        destination: routePrompt.destination.trim(),
        carrierType: routePrompt.carrierType,
        capacityUsed: carrierCapacity,
        manualMode: true,
        totalJumps: 0,
        totalDistLy: 0,
        totalTritium: 0,
        jumps: [],
      })
      setActiveCarrierTargetRowId(targetId)
      setActiveCarrierRoute(manualRoute)
      setCarrierJumpIndex(0)
      setManualModeOverride(true)
      setManualLegJumps(0)
      setManualLegDistance(0)
      setManualLegTritium(0)
      setManualDistanceInput('')
      setManualTritiumInput('')
      setRoutePrompt(null)
      persistRouteState({
        route: manualRoute,
        targetRowId: targetId,
        jumpIdx: 0,
        manualMode: true,
        manualLegJumps: 0,
        manualLegDist: 0,
        manualLegTrit: 0,
        manualDistInput: '',
        manualTritInput: '',
      })
      return
    }

    setRoutePrompt((previous) => (previous ? { ...previous, loading: true } : null))
    onBusyChange('PLOTTING SPANSH CARRIER ROUTE')
    try {
      const routeResult = await PlotCarrierRoute(
        routePrompt.origin.trim(),
        routePrompt.destination.trim(),
        carrierCapacity,
        routePrompt.carrierType,
      )
      setActiveCarrierTargetRowId(targetId)
      setActiveCarrierRoute(routeResult)
      setCarrierJumpIndex(0)
      setManualModeOverride(Boolean(routeResult.manualMode))
      setManualLegJumps(0)
      setManualLegDistance(0)
      setManualLegTritium(0)
      setManualDistanceInput('')
      setManualTritiumInput('')
      setRoutePrompt(null)
      persistRouteState({
        route: routeResult,
        targetRowId: targetId,
        jumpIdx: 0,
        manualMode: Boolean(routeResult.manualMode),
        manualLegJumps: 0,
        manualLegDist: 0,
        manualLegTrit: 0,
        manualDistInput: '',
        manualTritInput: '',
      })
    } catch (routeError: any) {
      const errorRoute = app.CarrierRouteResult.createFrom({
        origin: routePrompt.origin.trim(),
        destination: routePrompt.destination.trim(),
        carrierType: routePrompt.carrierType,
        capacityUsed: carrierCapacity,
        manualMode: true,
        error: routeError?.message || String(routeError),
        totalJumps: 0,
        totalDistLy: 0,
        totalTritium: 0,
        jumps: [],
      })
      setActiveCarrierTargetRowId(targetId)
      setActiveCarrierRoute(errorRoute)
      setCarrierJumpIndex(0)
      setManualModeOverride(true)
      setManualLegJumps(0)
      setManualLegDistance(0)
      setManualLegTritium(0)
      setRoutePrompt(null)
      persistRouteState({
        route: errorRoute,
        targetRowId: targetId,
        jumpIdx: 0,
        manualMode: true,
        manualLegJumps: 0,
        manualLegDist: 0,
        manualLegTrit: 0,
        manualDistInput: '',
        manualTritInput: '',
      })
    } finally {
      onBusyChange(null)
    }
  }

  async function handleCompleteSpanshJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const hop = activeCarrierRoute.jumps?.[carrierJumpIndex]
    if (!hop) return
    setLoggingJump(true)
    onBusyChange('LOGGING CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, hop.distanceLy, hop.tritiumUsed)
      const nextJumpIndex = carrierJumpIndex + 1
      setCarrierJumpIndex(nextJumpIndex)
      persistRouteState({ jumpIdx: nextJumpIndex })
      onRefreshAll()
    } catch (error) {
      console.error(error)
    } finally {
      setLoggingJump(false)
      onBusyChange(null)
    }
  }

  async function handleLogManualJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const jumpDistance = parseFloat(manualDistanceInput) || 0
    const tritiumAmount = Math.max(0, parseInt(manualTritiumInput, 10) || 0)
    if (jumpDistance <= 0 && tritiumAmount <= 0) return
    setLoggingJump(true)
    onBusyChange('LOGGING MANUAL CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, jumpDistance, tritiumAmount)
      const nextJumps = manualLegJumps + 1
      const nextTotalDistance = manualLegDistance + Math.max(0, jumpDistance)
      const nextTotalTritium = manualLegTritium + tritiumAmount
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
      onRefreshAll()
    } catch (error) {
      console.error(error)
    } finally {
      setLoggingJump(false)
      onBusyChange(null)
    }
  }

  async function handleCarrierArriveAtWaypoint() {
    const targetId = activeCarrierTargetRowId
    const expeditionId = plan?.expedition?.id
    if (expeditionId) {
      ClearActiveCarrierRoute(expeditionId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    if (targetId > 0) {
      await setFcArrived(targetId, true)
    }
  }

  type TableItem =
    | { kind: 'region'; rowView: expedition.RowView; index: number; key: string }
    | { kind: 'leg-banner'; rowView: expedition.RowView; index: number; key: string }
    | { kind: 'waypoint'; rowView: expedition.RowView; index: number; key: string }
    | { kind: 'drawer'; rowView: expedition.RowView; index: number; key: string }

  const isPlanning = plan?.expedition?.status === 'planning'

  const tableItems: TableItem[] = useMemo(() => {
    if (!plan || !plan.rows) return []
    const items: TableItem[] = []
    ;(plan.rows ?? []).forEach((rowView, index) => {
      if (!rowView?.row) return
      const row = rowView.row
      if (row.rowType === 'region') {
        items.push({ kind: 'region', rowView, index, key: `region-${row.id}` })
      } else {
        const isVisited = rowView.visitState === 'visited' || rowView.visitState === 'current'
        const showLegBanner = rowView.number > 1 && isVisited
        if (showLegBanner) {
          items.push({ kind: 'leg-banner', rowView, index, key: `banner-${row.id}` })
        }
        items.push({ kind: 'waypoint', rowView, index, key: `row-${row.id}` })
        const isExpanded = Boolean(expandedRowIds[row.id])
        if (!isPlanning && isExpanded) {
          items.push({ kind: 'drawer', rowView, index, key: `drawer-${row.id}` })
        }
      }
    })
    return items
  }, [plan, expandedRowIds, isPlanning])

  const getRowDropProps = useCallback(
    (index: number, isDraggable: boolean) => {
      if (!isDraggable) return {}
      return {
        onDragOver: (event: React.DragEvent) => {
          event.preventDefault()
          if (index > lastVisitedIndex) {
            event.dataTransfer.dropEffect = 'move'
            if (dragOverIndex !== index) {
              setDragOverIndex(index)
            }
          }
        },
        onDrop: (event: React.DragEvent) => {
          event.preventDefault()
          const from = draggedIndex
          setDraggedIndex(null)
          setDragOverIndex(null)
          if (from !== null && from !== index && index > lastVisitedIndex && plan) {
            const draggedRow = plan.rows?.[from]?.row
            if (draggedRow) {
              handleMoveRow(draggedRow.id, from, index)
            }
          }
        },
      }
    },
    [lastVisitedIndex, dragOverIndex, draggedIndex, plan],
  )

  // Conditional early return is now placed strictly AFTER all React hooks
  if (!plan) {
    return (
      <div className="flex-1 flex items-center justify-center p-12 text-[#a6907c] font-mono-tabular uppercase tracking-widest text-xs">
        [ LOADING EXPEDITION FLIGHT PLAN... ]
      </div>
    )
  }

  const currentExpedition = plan.expedition
  const isActive = currentExpedition.status === 'active'
  const isCompleted = currentExpedition.status === 'completed'
  const modesList = plan.modes ?? []

  const firstWaypoint = plan.rows?.find((rowView) => rowView.row.rowType === 'waypoint')
  const hasWp1System = Boolean(
    (drafts[firstWaypoint?.row.id ?? 0]?.systemName ?? firstWaypoint?.row.systemName ?? '').trim(),
  )
  const canComplete = (plan.visitedCount ?? 0) > 0

  return (
    <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-[#070605]">
      {/* Expedition Flight Plan Header Bar */}
      <div className="px-5 py-3 bg-[#0e0a07] border-b border-[#3d2816] flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-3.5">
          <StatusBadge status={currentExpedition.status} armedAt={currentExpedition.armedAt} />
          <div className="border-l-2 border-[#FF7100]/60 pl-3">
            <div className="flex items-center gap-2.5">
              <h2 className="text-base font-extrabold uppercase tracking-[0.1em] text-[#F4ECE4]">
                {currentExpedition.name}
              </h2>
              <span className="text-[11px] font-mono-tabular px-2 py-0.5 bg-[#16100b] border border-[#3d2816] text-[#FF7100]">
                {plan.visitedCount} / {plan.waypointCount} WAYPOINTS VISITED
              </span>
            </div>
            {currentExpedition.description && (
              <p className="text-xs text-[#a6907c] mt-0.5">{currentExpedition.description}</p>
            )}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {/* Route distance readout */}
          <div className="hidden xl:flex items-center gap-3 px-3 py-1.5 border border-[#3d2816] border-l-2 border-l-[#FF7100] bg-[#090705] text-xs">
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                PLANNED ROUTE:
              </span>
              <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                {formatLy(plan.plannedLy, 1)}
              </span>
            </div>
            <span className="text-[#3d2816]">|</span>
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                REMAINING:
              </span>
              <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                {formatLy(plan.remainingLy, 1)}
              </span>
            </div>
          </div>

          {/* Lifecycle action buttons (Start Time and Reset to Planning removed; use Clone in Plans menu) */}
          {isPlanning && (
            <button
              type="button"
              disabled={!hasWp1System}
              onClick={onOpenStartModal}
              title={
                hasWp1System
                  ? 'Engage expedition (lock arrival at Waypoint #1)'
                  : 'Enter a star system for Waypoint #1 before engaging the expedition'
              }
              className={`ed-btn-primary ${
                !hasWp1System ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
              }`}
            >
              <span>▶ ENGAGE EXPEDITION</span>
            </button>
          )}

          {isActive && (
            <div className="inline-flex items-center gap-1.5">
              <button
                type="button"
                disabled={!canComplete}
                onClick={() =>
                  handleLifecycleAction('COMPLETING EXPEDITION', () => CompleteExpedition(currentExpedition.id))
                }
                title={
                  canComplete
                    ? 'Complete this expedition and finalize flight logs'
                    : 'No waypoints visited yet — complete requires at least 1 visited waypoint (or delete flight plan in Plans)'
                }
                className={`ed-btn-primary ${
                  !canComplete ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
                }`}
              >
                Complete
              </button>
            </div>
          )}

          {isCompleted && (
            <div className="inline-flex items-center gap-1.5">
              <button
                type="button"
                onClick={() =>
                  handleLifecycleAction('REOPENING EXPEDITION', () => ReopenExpedition(currentExpedition.id))
                }
                className="ed-btn-cyan"
              >
                Reopen Expedition
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Main Waypoint Table + Bottom Add Row Bar */}
      <div className="flex-1 overflow-y-auto overflow-x-hidden bg-[#070605]">
        <table className="w-full table-fixed border-separate border-spacing-0 text-left">
          <thead className="sticky top-0 z-20 bg-[#120d09] border-b-2 border-[#FF7100]/70 text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#a6907c] select-none">
            <tr className="align-top">
              {!isPlanning && (
                <th className="w-8 px-1.5 py-2.5 text-center align-top border-b border-[#FF7100]/70" title="Expand Leg Telemetry">
                  Leg
                </th>
              )}
              <th className="w-11 px-2 py-2.5 text-right font-mono-tabular align-top border-b border-[#FF7100]/70">#</th>
              <th className="w-64 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Star System</th>
              <th className="w-52 px-3 py-2.5 align-top border-b border-[#FF7100]/70">POI / Body</th>
              <th className="px-3 py-2.5 align-top border-b border-[#FF7100]/70">Flight Notes</th>
              <th
                className="w-44 px-2 py-2.5 text-center align-top border-b border-[#FF7100]/70"
                title="Fleet Carrier Stop, Arrival Status and Spansh Router"
              >
                Carrier Stop
              </th>
              <th className="w-64 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Telemetry / Timestamp</th>
              <th className="w-64 px-3 py-2.5 text-right whitespace-nowrap align-top border-b border-[#FF7100]/70">Controls</th>
            </tr>
          </thead>

          <tbody className="text-xs">
            {tableItems.map((item) => {
              const rowView = item.rowView
              const row = rowView.row
              const index = item.index
              const isLocked = isCompleted || index <= lastVisitedIndex
              const isDraggable = !isLocked
              const isBeingDragged = draggedIndex === index
              const isDragTarget =
                dragOverIndex === index && draggedIndex !== null && draggedIndex !== index

              if (item.kind === 'region') {
                return (
                  <tr
                    key={item.key}
                    style={rowStyle}
                    {...getRowDropProps(index, isDraggable)}
                    className={`group ed-header-bar ${
                      isBeingDragged ? 'opacity-40' : ''
                    } ${isDragTarget ? 'outline-2 outline-[#00D2FF]' : ''}`}
                  >
                    {/* Just the Region Name and nothing else */}
                    <td colSpan={isPlanning ? 6 : 7} className="px-3 py-2 border-b border-[#3a2615]">
                      <RegionHeaderDropdown
                        expeditionId={currentExpedition.id}
                        rowId={row.id}
                        currentRegionId={row.regionId}
                        regionName={rowView.regionName}
                        locked={isLocked}
                        onSelect={(selectedRegionId) => changeRegion(row.id, selectedRegionId)}
                      />
                    </td>

                    <td className="px-3 py-2 text-right whitespace-nowrap border-b border-[#3a2615]">
                      {!isLocked && (
                        <div className="inline-flex items-center justify-end gap-1.5 whitespace-nowrap">
                          <RowManipulationButtons
                            index={index}
                            minIndex={lastVisitedIndex + 1}
                            total={plan.rows?.length ?? 0}
                            rowId={row.id}
                            insertMenuOpen={insertMenuAfterId === row.id}
                            onToggleInsertMenu={() =>
                              setInsertMenuAfterId(insertMenuAfterId === row.id ? null : row.id)
                            }
                            onInsert={(insertType: 'waypoint' | 'region') => handleInsert(row.id, insertType)}
                            onMove={(targetIndex: number) => handleMoveRow(row.id, index, targetIndex)}
                            onDelete={() => handleDeleteRow(row.id)}
                          />
                          <button
                            type="button"
                            draggable={true}
                            onDragStart={(event) => {
                              setDraggedIndex(index)
                              event.dataTransfer.effectAllowed = 'move'
                              event.dataTransfer.setData('text/plain', String(row.id))
                            }}
                            onDragEnd={() => {
                              setDraggedIndex(null)
                              setDragOverIndex(null)
                            }}
                            title="Drag to reposition Galactic Region Header"
                            className="h-5 px-1.5 inline-flex items-center justify-center text-[#FF7100] hover:text-[#ff9436] cursor-grab active:cursor-grabbing font-mono-tabular select-none border border-[#472e19] bg-[#16100b] hover:bg-[#24180e] text-xs shrink-0"
                          >
                            ⋮⋮
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                )
              }

              if (item.kind === 'leg-banner') {
                return (
                  <tr key={item.key} style={rowStyle}>
                    <td colSpan={isPlanning ? 7 : 8} className="p-0 border-b border-[#24180e]">
                      <LegBanner
                        transitions={rowView.transitions}
                        modeId={rowView.legModeId}
                        modeColor={rowView.legModeColor}
                        modesList={modesList}
                        onChangeMode={(nextModeId) => handleLegModeChange(row.id, nextModeId)}
                      />
                    </td>
                  </tr>
                )
              }

              if (item.kind === 'drawer') {
                return (
                  <tr key={item.key} style={rowStyle}>
                    <td colSpan={8} className="p-0 border-b border-[#24180e]">
                      <WaypointDetailsDrawer
                        details={detailsById[row.id] ?? null}
                        loading={Boolean(loadingDetails[row.id])}
                        rowView={rowView}
                      />
                    </td>
                  </tr>
                )
              }

              const isExpanded = Boolean(expandedRowIds[row.id])
              const draft = drafts[row.id] ?? {
                systemName: row.systemName ?? '',
                poiName: row.poiName ?? '',
                notes: row.notes ?? '',
              }
              const isNextFcStop = Boolean(row.fcStop && !row.fcArrived && row.id === plan.nextFcStopRowId)
              const hasActiveCarrierRoute = Boolean(activeCarrierRoute && activeCarrierTargetRowId === row.id)

              return (
                <WaypointRowItem
                  key={item.key}
                  rowView={rowView}
                  index={index}
                  isPlanning={isPlanning}
                  isLocked={isLocked}
                  isBeingDragged={isBeingDragged}
                  isDragTarget={isDragTarget}
                  isExpanded={isExpanded}
                  isNextFcStop={isNextFcStop}
                  hasActiveCarrierRoute={hasActiveCarrierRoute}
                  draft={draft}
                  copiedSystemRowId={copiedSystemRowId}
                  insertMenuOpen={insertMenuAfterId === row.id}
                  lastVisitedIndex={lastVisitedIndex}
                  totalRows={plan.rows?.length ?? 0}
                  rowStyle={rowStyle}
                  dropProps={getRowDropProps(index, isDraggable)}
                  onToggleExpand={toggleExpand}
                  onDraftChange={(id, field, val) =>
                    setDrafts((prev) => ({
                      ...prev,
                      [id]: { ...(prev[id] ?? draft), [field]: val },
                    }))
                  }
                  onFieldFocus={(id) => {
                    editingRowIdRef.current = id
                  }}
                  onFieldBlur={(r, field, val) => {
                    editingRowIdRef.current = null
                    commitField(r, field, val)
                  }}
                  onCopySystem={handleCopySystem}
                  onToggleFcStop={toggleFcStop}
                  onSetFcArrived={setFcArrived}
                  onOpenCarrierRoutePrompt={openCarrierRoutePrompt}
                  onToggleInsertMenu={(id) => setInsertMenuAfterId(insertMenuAfterId === id ? null : id)}
                  onInsert={handleInsert}
                  onMove={(id, targetIndex) => handleMoveRow(id, index, targetIndex)}
                  onDelete={handleDeleteRow}
                  onDragStart={(idx, id, event) => {
                    setDraggedIndex(idx)
                    event.dataTransfer.effectAllowed = 'move'
                    event.dataTransfer.setData('text/plain', String(id))
                  }}
                  onDragEnd={() => {
                    setDraggedIndex(null)
                    setDragOverIndex(null)
                  }}
                />
              )
            })}
          </tbody>
        </table>

        {/* In-progress leg since the Last Visited Waypoint */}
        {isActive && plan.visitedCount > 0 && (
          <div className="border-t border-[#3d2816] bg-[#0b0907]">
            <div className="px-4 py-1 text-[10px] font-bold uppercase tracking-[0.14em] text-[#a6907c]">
              Current In-Progress Leg (Since Last Logged Waypoint)
            </div>
            <LegBanner
              transitions={plan.pendingTransitions}
              modeId={plan.activeModeId}
              modeColor={plan.activeModeColor}
              modesList={modesList}
              onChangeMode={handleActiveLegModeChange}
            />
          </div>
        )}

        {/* Bottom Route Action Bar: Add Waypoint / Add Region Header */}
        {!isCompleted && (
          <div className="px-5 py-3.5 border-t border-[#3d2816] bg-[#0b0907] flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <button
                type="button"
                onClick={() => handleInsert(0, 'waypoint')}
                className="ed-btn-cyan"
              >
                <span className="font-extrabold">+</span>
                <span>Add Waypoint</span>
              </button>
              <button
                type="button"
                onClick={() => handleInsert(0, 'region')}
                className="ed-btn"
              >
                <span className="font-extrabold text-[#FF7100]">+</span>
                <span>Add Galactic Region Header</span>
              </button>
            </div>

            <div className="text-[10px] font-mono-tabular uppercase tracking-wider text-[#a6907c]">
              {isPlanning
                ? 'DRAG (⋮⋮) OR USE ARROWS TO REORDER WAYPOINTS AND SECTORS'
                : 'UNVISITED WAYPOINTS DRAGGABLE · CLONE IN PLANS MENU TO RE-PLAN'}
            </div>
          </div>
        )}

        {/* Breathing room to scroll past the bottom of the list */}
        <div className="h-48 shrink-0 pointer-events-none" aria-hidden="true" />
      </div>

      {/* Bottom Panel: Active Carrier Route (Spansh or Manual Mode) */}
      {activeCarrierRoute && (
        <CarrierRouteBottomPanel
          route={activeCarrierRoute}
          jumpIndex={carrierJumpIndex}
          manualMode={
            manualModeOverride ||
            Boolean(activeCarrierRoute.manualMode) ||
            !activeCarrierRoute.jumps?.length
          }
          onCancelRoute={handleCancelCarrierRoute}
          onToggleManualMode={() => {
            const next = !manualModeOverride
            setManualModeOverride(next)
            persistRouteState({ manualMode: next })
          }}
          onCompleteSpanshJump={handleCompleteSpanshJump}
          manualDistanceInput={manualDistanceInput}
          onChangeManualDistance={(value) => {
            setManualDistanceInput(value)
            persistRouteState({ manualDistInput: value })
          }}
          manualTritiumInput={manualTritiumInput}
          onChangeManualTritium={(value) => {
            setManualTritiumInput(value)
            persistRouteState({ manualTritInput: value })
          }}
          onLogManualJump={handleLogManualJump}
          manualLegJumps={manualLegJumps}
          manualLegDistance={manualLegDistance}
          manualLegTritium={manualLegTritium}
          loggingJump={loggingJump}
          copiedNextJump={copiedNextJump}
          onCopySystem={(systemName) => {
            navigator.clipboard?.writeText(systemName).catch(() => {})
            setCopiedNextJump(true)
            setTimeout(() => setCopiedNextJump(false), 1500)
          }}
          onArrive={handleCarrierArriveAtWaypoint}
        />
      )}

      {/* Carrier Route Setup Prompt Modal */}
      {routePrompt && (
        <CarrierRouteModal
          prompt={routePrompt}
          onClose={() => setRoutePrompt(null)}
          onUpdate={(updater) =>
            setRoutePrompt((prev) => (prev ? updater(prev) : null))
          }
          onStart={handleStartCarrierRoute}
        />
      )}
    </div>
  )
}

