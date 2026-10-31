import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
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
  AutoExpandingTextarea,
  CarrierRouteBottomPanel,
  CoordStatusIndicator,
  HoverOpenInLinks,
  LegBanner,
  RegionHeaderDropdown,
  RowManipulationButtons,
  StatusBadge,
  WaypointDetailsDrawer,
} from './PlannerWidgets'

export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
  onPlanOptimisticUpdate: (next: expedition.PlanView) => void
  onOpenStartModal: () => void
  onRefreshPlan: () => void
  onRefreshAll: () => void
  onBusyChange: (msg: string | null) => void
}

interface CarrierRoutePromptState {
  targetRowId: number
  origin: string
  destination: string
  capacityUsed: string
  carrierType: 'player' | 'squadron'
  loading: boolean
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

  // Carrier Route Setup Modal & Bottom Panel state
  const [routePrompt, setRoutePrompt] = useState<CarrierRoutePromptState | null>(null)
  const [activeCarrierRoute, setActiveCarrierRoute] = useState<app.CarrierRouteResult | null>(null)
  const [activeCarrierTargetRowId, setActiveCarrierTargetRowId] = useState<number>(0)
  const [carrierJumpIdx, setCarrierJumpIdx] = useState(0)
  const [manualModeOverride, setManualModeOverride] = useState(false)
  const [manualDistInput, setManualDistInput] = useState('')
  const [manualTritInput, setManualTritInput] = useState('')
  const [manualLegJumps, setManualLegJumps] = useState(0)
  const [manualLegDist, setManualLegDist] = useState(0)
  const [manualLegTrit, setManualLegTrit] = useState(0)
  const [loggingJump, setLoggingJump] = useState(false)
  const [copiedNextJump, setCopiedNextJump] = useState(false)
  const [copiedSystemRowId, setCopiedSystemRowId] = useState<number | null>(null)

  // Load persisted carrier route state on mount or when active expedition changes
  useEffect(() => {
    const expId = plan?.expedition?.id
    if (!expId || expId <= 0) {
      setActiveCarrierRoute(null)
      setActiveCarrierTargetRowId(0)
      return
    }
    let cancelled = false
    GetActiveCarrierRoute(expId)
      .then((saved) => {
        if (cancelled) return
        if (saved && saved.route && saved.targetRowId > 0) {
          setActiveCarrierRoute(saved.route)
          setActiveCarrierTargetRowId(saved.targetRowId)
          setCarrierJumpIdx(saved.jumpIdx || 0)
          setManualModeOverride(Boolean(saved.manualMode))
          setManualDistInput(saved.manualDistInput || '')
          setManualTritInput(saved.manualTritInput || '')
          setManualLegJumps(saved.manualLegJumps || 0)
          setManualLegDist(saved.manualLegDist || 0)
          setManualLegTrit(saved.manualLegTrit || 0)
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
    const expId = plan?.expedition?.id
    if (!expId || expId <= 0) return
    const route = updates.route !== undefined ? updates.route : activeCarrierRoute
    const targetRowId = updates.targetRowId !== undefined ? updates.targetRowId : activeCarrierTargetRowId
    if (!route || targetRowId <= 0) {
      ClearActiveCarrierRoute(expId).catch(console.error)
      return
    }
    const state = app.ActiveCarrierRouteState.createFrom({
      expeditionId: expId,
      targetRowId,
      route,
      jumpIdx: updates.jumpIdx !== undefined ? updates.jumpIdx : carrierJumpIdx,
      manualMode: updates.manualMode !== undefined ? updates.manualMode : manualModeOverride,
      manualDistInput: updates.manualDistInput !== undefined ? updates.manualDistInput : manualDistInput,
      manualTritInput: updates.manualTritInput !== undefined ? updates.manualTritInput : manualTritInput,
      manualLegJumps: updates.manualLegJumps !== undefined ? updates.manualLegJumps : manualLegJumps,
      manualLegDist: updates.manualLegDist !== undefined ? updates.manualLegDist : manualLegDist,
      manualLegTrit: updates.manualLegTrit !== undefined ? updates.manualLegTrit : manualLegTrit,
    })
    SaveActiveCarrierRoute(state).catch(console.error)
  }

  function handleCancelCarrierRoute() {
    const expId = plan?.expedition?.id
    if (expId) {
      ClearActiveCarrierRoute(expId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    setCarrierJumpIdx(0)
    setManualModeOverride(false)
  }

  function handleCopySystem(rowId: number, sysName: string) {
    if (!sysName) return
    const text = sysName.trim()
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(text).catch(console.error)
    } else {
      const ta = document.createElement('textarea')
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    setCopiedSystemRowId(rowId)
    setTimeout(() => {
      setCopiedSystemRowId((curr) => (curr === rowId ? null : curr))
    }, 1500)
  }

  // Local draft inputs so typing in spreadsheet cells is instantaneous
  const [drafts, setDrafts] = useState<
    Record<number, { systemName: string; poiName: string; notes: string }>
  >({})

  useEffect(() => {
    if (!plan || !plan.rows) return
    const next: Record<number, { systemName: string; poiName: string; notes: string }> = {}
    for (const rv of plan.rows ?? []) {
      if (!rv?.row) continue
      next[rv.row.id] = {
        systemName: rv.row.systemName ?? '',
        poiName: rv.row.poiName ?? '',
        notes: rv.row.notes ?? '',
      }
    }
    setDrafts(next)
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
    let maxIdx = -1
    ;(plan.rows ?? []).forEach((rv, idx) => {
      if (rv && (rv.visitState === 'visited' || rv.visitState === 'current')) {
        maxIdx = idx
      }
    })
    return maxIdx
  }, [plan])

  function loadDetails(rowId: number) {
    setLoadingDetails((p) => ({ ...p, [rowId]: true }))
    GetWaypointDetails(rowId)
      .then((d) => setDetailsById((p) => ({ ...p, [rowId]: d })))
      .catch(console.error)
      .finally(() => setLoadingDetails((p) => ({ ...p, [rowId]: false })))
  }

  function toggleExpand(rowId: number) {
    const nextOpen = !expandedRowIds[rowId]
    setExpandedRowIds((p) => ({ ...p, [rowId]: nextOpen }))
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
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function toggleFcStop(row: expedition.Row) {
    if (!plan) return
    const nextFcStop = !row.fcStop
    const optimisticRows = plan.rows.map((rv) => {
      if (rv.row.id !== row.id) return rv
      return expedition.RowView.createFrom({
        ...rv,
        row: { ...rv.row, fcStop: nextFcStop, fcArrived: nextFcStop ? rv.row.fcArrived : false },
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
    const optimisticRows = plan.rows.map((rv) => {
      if (rv.row.id !== rowId) return rv
      return expedition.RowView.createFrom({
        ...rv,
        row: { ...rv.row, fcArrived: arrived },
      })
    })
    let nextFcId = 0
    for (const rv of optimisticRows) {
      if (rv.row.rowType === 'waypoint' && rv.row.fcStop && !rv.row.fcArrived) {
        nextFcId = rv.row.id
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
    const picked = (plan.modes ?? []).find((m) => m.id === modeId)
    const optimisticRows = plan.rows.map((rv) => {
      if (rv.row.id !== rowId) return rv
      return expedition.RowView.createFrom({
        ...rv,
        legModeId: modeId,
        legModeName: picked?.name ?? rv.legModeName,
        legModeColor: picked?.color ?? rv.legModeColor,
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
    const picked = (plan.modes ?? []).find((m) => m.id === modeId)
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
    const filtered = plan.rows.filter((rv) => rv.row.id !== rowId)
    let wpNum = 0
    const renumbered = filtered.map((rv) => {
      if (rv.row.rowType === 'waypoint') {
        wpNum++
        return expedition.RowView.createFrom({ ...rv, number: wpNum })
      }
      return rv
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
    const renumbered = nextRows.map((rv, i) => {
      const updatedRow = { ...rv.row, sortOrder: i }
      if (rv.row.rowType === 'waypoint') {
        wpNum++
        return expedition.RowView.createFrom({ ...rv, row: updatedRow, number: wpNum })
      }
      return expedition.RowView.createFrom({ ...rv, row: updatedRow })
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

  async function handleLifecycleAction(label: string, fn: () => Promise<void>) {
    onBusyChange(label)
    try {
      await fn()
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
    let defaultCap = 5000
    try {
      defaultCap = await GetDefaultCarrierCapacity()
    } catch {
      defaultCap = 5000
    }
    setRoutePrompt({
      targetRowId: targetRow.id,
      origin: prevSystem,
      destination: (drafts[targetRow.id]?.systemName ?? targetRow.systemName ?? '').trim(),
      capacityUsed: String(defaultCap),
      carrierType: 'player',
      loading: false,
    })
  }

  async function handleStartCarrierRoute(forceManual: boolean) {
    if (!routePrompt) return
    const targetId = routePrompt.targetRowId
    const cap = Math.max(0, Math.min(25000, parseInt(routePrompt.capacityUsed, 10) || 0))
    if (forceManual) {
      const r = app.CarrierRouteResult.createFrom({
        origin: routePrompt.origin.trim(),
        destination: routePrompt.destination.trim(),
        carrierType: routePrompt.carrierType,
        capacityUsed: cap,
        manualMode: true,
        totalJumps: 0,
        totalDistLy: 0,
        totalTritium: 0,
        jumps: [],
      })
      setActiveCarrierTargetRowId(targetId)
      setActiveCarrierRoute(r)
      setCarrierJumpIdx(0)
      setManualModeOverride(true)
      setManualLegJumps(0)
      setManualLegDist(0)
      setManualLegTrit(0)
      setManualDistInput('')
      setManualTritInput('')
      setRoutePrompt(null)
      persistRouteState({
        route: r,
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

    setRoutePrompt((p) => (p ? { ...p, loading: true } : null))
    onBusyChange('PLOTTING SPANSH CARRIER ROUTE')
    try {
      const res = await PlotCarrierRoute(
        routePrompt.origin.trim(),
        routePrompt.destination.trim(),
        cap,
        routePrompt.carrierType,
      )
      setActiveCarrierTargetRowId(targetId)
      setActiveCarrierRoute(res)
      setCarrierJumpIdx(0)
      setManualModeOverride(Boolean(res.manualMode))
      setManualLegJumps(0)
      setManualLegDist(0)
      setManualLegTrit(0)
      setManualDistInput('')
      setManualTritInput('')
      setRoutePrompt(null)
      persistRouteState({
        route: res,
        targetRowId: targetId,
        jumpIdx: 0,
        manualMode: Boolean(res.manualMode),
        manualLegJumps: 0,
        manualLegDist: 0,
        manualLegTrit: 0,
        manualDistInput: '',
        manualTritInput: '',
      })
    } catch (err: any) {
      const errRes = app.CarrierRouteResult.createFrom({
        origin: routePrompt.origin.trim(),
        destination: routePrompt.destination.trim(),
        carrierType: routePrompt.carrierType,
        capacityUsed: cap,
        manualMode: true,
        error: err?.message || String(err),
        totalJumps: 0,
        totalDistLy: 0,
        totalTritium: 0,
        jumps: [],
      })
      setActiveCarrierTargetRowId(targetId)
      setActiveCarrierRoute(errRes)
      setCarrierJumpIdx(0)
      setManualModeOverride(true)
      setManualLegJumps(0)
      setManualLegDist(0)
      setManualLegTrit(0)
      setRoutePrompt(null)
      persistRouteState({
        route: errRes,
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
    const hop = activeCarrierRoute.jumps?.[carrierJumpIdx]
    if (!hop) return
    setLoggingJump(true)
    onBusyChange('LOGGING CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, hop.distanceLy, hop.tritiumUsed)
      const nextIdx = carrierJumpIdx + 1
      setCarrierJumpIdx(nextIdx)
      persistRouteState({ jumpIdx: nextIdx })
      onRefreshAll()
    } catch (err) {
      console.error(err)
    } finally {
      setLoggingJump(false)
      onBusyChange(null)
    }
  }

  async function handleLogManualJump() {
    if (!plan || !activeCarrierRoute || loggingJump) return
    const dist = parseFloat(manualDistInput) || 0
    const trit = Math.max(0, parseInt(manualTritInput, 10) || 0)
    if (dist <= 0 && trit <= 0) return
    setLoggingJump(true)
    onBusyChange('LOGGING MANUAL CARRIER JUMP')
    try {
      await RecordCarrierJump(plan.expedition.id, dist, trit)
      const nextJumps = manualLegJumps + 1
      const nextDist = manualLegDist + Math.max(0, dist)
      const nextTrit = manualLegTrit + trit
      setManualLegJumps(nextJumps)
      setManualLegDist(nextDist)
      setManualLegTrit(nextTrit)
      setManualDistInput('')
      setManualTritInput('')
      persistRouteState({
        manualLegJumps: nextJumps,
        manualLegDist: nextDist,
        manualLegTrit: nextTrit,
        manualDistInput: '',
        manualTritInput: '',
      })
      onRefreshAll()
    } catch (err) {
      console.error(err)
    } finally {
      setLoggingJump(false)
      onBusyChange(null)
    }
  }

  async function handleCarrierArriveAtWaypoint() {
    const targetId = activeCarrierTargetRowId
    const expId = plan?.expedition?.id
    if (expId) {
      ClearActiveCarrierRoute(expId).catch(console.error)
    }
    setActiveCarrierRoute(null)
    setActiveCarrierTargetRowId(0)
    if (targetId > 0) {
      await setFcArrived(targetId, true)
    }
  }

  type TableItem =
    | { kind: 'region'; rv: expedition.RowView; index: number; key: string }
    | { kind: 'leg-banner'; rv: expedition.RowView; index: number; key: string }
    | { kind: 'waypoint'; rv: expedition.RowView; index: number; key: string }
    | { kind: 'drawer'; rv: expedition.RowView; index: number; key: string }

  const isPlanning = plan?.expedition?.status === 'planning'

  const tableItems: TableItem[] = useMemo(() => {
    if (!plan || !plan.rows) return []
    const items: TableItem[] = []
    ;(plan.rows ?? []).forEach((rv, index) => {
      if (!rv?.row) return
      const r = rv.row
      if (r.rowType === 'region') {
        items.push({ kind: 'region', rv, index, key: `region-${r.id}` })
      } else {
        const isVisited = rv.visitState === 'visited' || rv.visitState === 'current'
        const showLegBanner = rv.number > 1 && isVisited
        if (showLegBanner) {
          items.push({ kind: 'leg-banner', rv, index, key: `banner-${r.id}` })
        }
        items.push({ kind: 'waypoint', rv, index, key: `row-${r.id}` })
        const isExpanded = Boolean(expandedRowIds[r.id])
        if (!isPlanning && isExpanded) {
          items.push({ kind: 'drawer', rv, index, key: `drawer-${r.id}` })
        }
      }
    })
    return items
  }, [plan, expandedRowIds, isPlanning])

  const getRowDropProps = useCallback(
    (index: number, isDraggable: boolean) => {
      if (!isDraggable) return {}
      return {
        onDragOver: (e: React.DragEvent) => {
          e.preventDefault()
          if (index > lastVisitedIndex) {
            e.dataTransfer.dropEffect = 'move'
            if (dragOverIndex !== index) {
              setDragOverIndex(index)
            }
          }
        },
        onDrop: (e: React.DragEvent) => {
          e.preventDefault()
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

  const exp = plan.expedition
  const isActive = exp.status === 'active'
  const isCompleted = exp.status === 'completed'
  const modesList = plan.modes ?? []

  const firstWp = plan.rows?.find((rv) => rv.row.rowType === 'waypoint')
  const hasWp1System = Boolean(
    (drafts[firstWp?.row.id ?? 0]?.systemName ?? firstWp?.row.systemName ?? '').trim(),
  )
  const canComplete = (plan.visitedCount ?? 0) > 0

  return (
    <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-[#070605]">
      {/* Expedition Flight Plan Header Bar */}
      <div className="px-5 py-3 bg-[#0e0a07] border-b border-[#3d2816] flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-3.5">
          <StatusBadge status={exp.status} armedAt={exp.armedAt} />
          <div className="border-l-2 border-[#FF7100]/60 pl-3">
            <div className="flex items-center gap-2.5">
              <h2 className="text-base font-extrabold uppercase tracking-[0.1em] text-[#F4ECE4]">
                {exp.name}
              </h2>
              <span className="text-[11px] font-mono-tabular px-2 py-0.5 bg-[#16100b] border border-[#3d2816] text-[#FF7100]">
                {plan.visitedCount} / {plan.waypointCount} WAYPOINTS VISITED
              </span>
            </div>
            {exp.description && (
              <p className="text-xs text-[#a6907c] mt-0.5">{exp.description}</p>
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

          {/* Lifecycle action buttons (Start Time & Reset to Planning removed; use Clone in Plans menu) */}
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
                  handleLifecycleAction('COMPLETING EXPEDITION', () => CompleteExpedition(exp.id))
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
                ✓ Complete
              </button>
            </div>
          )}

          {isCompleted && (
            <div className="inline-flex items-center gap-1.5">
              <button
                type="button"
                onClick={() =>
                  handleLifecycleAction('REOPENING EXPEDITION', () => ReopenExpedition(exp.id))
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
                title="Fleet Carrier Stop, Arrival Status & Spansh Router"
              >
                Carrier Stop
              </th>
              <th className="w-48 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Telemetry / Timestamp</th>
              <th className="w-72 px-3 py-2.5 text-right whitespace-nowrap align-top border-b border-[#FF7100]/70">Controls</th>
            </tr>
          </thead>

          <tbody className="text-xs">
            {tableItems.map((item) => {
              const rv = item.rv
              const r = rv.row
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
                        expeditionId={exp.id}
                        rowId={r.id}
                        currentRegionId={r.regionId}
                        regionName={rv.regionName}
                        locked={isLocked}
                        onSelect={(regId) => changeRegion(r.id, regId)}
                      />
                    </td>

                    <td className="px-3 py-2 text-right whitespace-nowrap border-b border-[#3a2615]">
                      {!isLocked && (
                        <div className="inline-flex items-center justify-end gap-1.5 whitespace-nowrap">
                          <RowManipulationButtons
                            index={index}
                            minIndex={lastVisitedIndex + 1}
                            total={plan.rows?.length ?? 0}
                            rowId={r.id}
                            insertMenuOpen={insertMenuAfterId === r.id}
                            onToggleInsertMenu={() =>
                              setInsertMenuAfterId(insertMenuAfterId === r.id ? null : r.id)
                            }
                            onInsert={(type) => handleInsert(r.id, type)}
                            onMove={(to) => handleMoveRow(r.id, index, to)}
                            onDelete={() => handleDeleteRow(r.id)}
                          />
                          <button
                            type="button"
                            draggable={true}
                            onDragStart={(e) => {
                              setDraggedIndex(index)
                              e.dataTransfer.effectAllowed = 'move'
                              e.dataTransfer.setData('text/plain', String(r.id))
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
                        transitions={rv.transitions}
                        modeId={rv.legModeId}
                        modeColor={rv.legModeColor}
                        modesList={modesList}
                        onChangeMode={(nextModeId) => handleLegModeChange(r.id, nextModeId)}
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
                        details={detailsById[r.id] ?? null}
                        loading={Boolean(loadingDetails[r.id])}
                        rowView={rv}
                      />
                    </td>
                  </tr>
                )
              }

              const isExpanded = Boolean(expandedRowIds[r.id])
              const isVisited = rv.visitState === 'visited' || rv.visitState === 'current'
              const draft = drafts[r.id] ?? {
                systemName: r.systemName ?? '',
                poiName: r.poiName ?? '',
                notes: r.notes ?? '',
              }

              const isNextFcStop = r.fcStop && !r.fcArrived && r.id === plan.nextFcStopRowId

              return (
                <tr
                  key={item.key}
                  style={rowStyle}
                  {...getRowDropProps(index, isDraggable)}
                  className={`group align-top ${
                    isBeingDragged ? 'opacity-40' : ''
                  } ${isDragTarget ? 'outline-2 outline-[#00D2FF]' : ''} ${
                    isVisited
                      ? 'bg-[#120e09] hover:bg-[#1b140d] text-[#F4ECE4]'
                      : 'bg-[#090705] hover:bg-[#16100b] text-[#F4ECE4]'
                  }`}
                >
                  {/* Expand Chevron */}
                  {!isPlanning && (
                    <td
                      className={`px-1.5 py-1.5 text-center align-top border-b border-[#24180e] ${
                        isVisited ? 'border-l-2 border-l-[#FF7100]' : ''
                      }`}
                    >
                      <button
                        type="button"
                        onClick={() => toggleExpand(r.id)}
                        title="Toggle per-waypoint leg telemetry / distance drawer"
                        className={`p-1 hover:bg-[#24180e] cursor-pointer mt-0.5 ${
                          isExpanded ? 'text-[#FF7100]' : 'text-[#a6907c]'
                        }`}
                      >
                        <svg
                          className={`w-3.5 h-3.5 transition-transform ${
                            isExpanded ? 'rotate-90' : ''
                          }`}
                          viewBox="0 0 20 20"
                          fill="currentColor"
                        >
                          <path
                            fillRule="evenodd"
                            d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z"
                            clipRule="evenodd"
                          />
                        </svg>
                      </button>
                    </td>
                  )}

                  {/* Sequential Waypoint # (skips Region Headers) */}
                  <td
                    className={`px-2 py-1.5 text-right font-mono-tabular font-bold text-[#FF7100] align-top border-b border-[#24180e] ${
                      isPlanning && isVisited ? 'border-l-2 border-l-[#FF7100]' : ''
                    }`}
                  >
                    <div className="py-1">
                      {String(rv.number).padStart(2, '0')}
                    </div>
                  </td>

                  {/* System Name (EDSM indicator on left + system name + copy button) */}
                  <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
                    <div className="flex items-center gap-2 min-w-0">
                      <CoordStatusIndicator rowView={rv} />
                      <div className="flex-1 min-w-0 flex items-center gap-1">
                        {isLocked ? (
                          <span
                            title={r.systemName}
                            className="font-mono-tabular font-bold text-[#F4ECE4] px-1.5 py-1 select-text cursor-text truncate block"
                          >
                            {r.systemName || '—'}
                          </span>
                        ) : (
                          <input
                            type="text"
                            spellCheck={false}
                            value={draft.systemName}
                            placeholder="Star System"
                            onChange={(e) =>
                              setDrafts((p) => ({
                                ...p,
                                [r.id]: { ...draft, systemName: e.target.value },
                              }))
                            }
                            onBlur={(e) => commitField(r, 'systemName', e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                ;(e.target as HTMLInputElement).blur()
                              }
                            }}
                            className="w-full bg-transparent px-1.5 py-1 border border-transparent focus:border-[#FF7100] focus:bg-[#050506] focus:outline-none font-mono-tabular font-bold text-[#F4ECE4] placeholder:text-[#6e5b4b] select-text"
                          />
                        )}
                        {r.systemName && (
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation()
                              handleCopySystem(r.id, r.systemName)
                            }}
                            title={`Copy "${r.systemName}" to clipboard`}
                            className="p-1 text-[#6e5b4b] hover:text-[#FF7100] hover:bg-[#1f150d] cursor-pointer shrink-0"
                          >
                            {copiedSystemRowId === r.id ? (
                              <span className="text-[10px] font-mono-tabular font-bold text-[#00D2FF]">
                                ✓
                              </span>
                            ) : (
                              <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                                <path d="M7 3.5A1.5 1.5 0 018.5 2h3.879a1.5 1.5 0 011.06.44l3.122 3.12A1.5 1.5 0 0117 6.622V12.5a1.5 1.5 0 01-1.5 1.5h-1v-3.379a3 3 0 00-.879-2.121L10.5 5.379A3 3 0 008.379 4.5H7v-1z" />
                                <path d="M4.5 6A1.5 1.5 0 003 7.5v9A1.5 1.5 0 004.5 18h7a1.5 1.5 0 001.5-1.5v-5.879a1.5 1.5 0 00-.44-1.06L9.44 6.44A1.5 1.5 0 008.379 6H4.5z" />
                              </svg>
                            )}
                          </button>
                        )}
                      </div>
                    </div>
                  </td>

                  {/* POI Name (always editable, auto-expands up to 3 lines) */}
                  <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
                    <AutoExpandingTextarea
                      value={draft.poiName}
                      placeholder="POI Name"
                      maxLines={3}
                      onChange={(val) =>
                        setDrafts((p) => ({
                          ...p,
                          [r.id]: { ...draft, poiName: val },
                        }))
                      }
                      onBlur={(val) => commitField(r, 'poiName', val)}
                      className="text-[#F4ECE4] placeholder:text-[#6e5b4b]"
                    />
                  </td>

                  {/* Flight Notes (always editable, auto-expands up to 3 lines) */}
                  <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
                    <AutoExpandingTextarea
                      value={draft.notes}
                      placeholder="Flight notes, reminders..."
                      maxLines={3}
                      onChange={(val) =>
                        setDrafts((p) => ({
                          ...p,
                          [r.id]: { ...draft, notes: val },
                        }))
                      }
                      onBlur={(val) => commitField(r, 'notes', val)}
                      className="text-[#d6c7b8] placeholder:text-[#6e5b4b]"
                    />
                  </td>

                  {/* Carrier Stop (Moved here between notes and telemetry; no bare checkbox) */}
                  <td className="px-2 py-1.5 text-center align-top border-b border-[#24180e]">
                    <div className="inline-flex items-center justify-center gap-1.5 whitespace-nowrap py-0.5">
                      {r.fcStop ? (
                        <>
                          {isPlanning ? (
                            <span
                              title="Designated Fleet Carrier Stop for this expedition"
                              className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#120d09] text-[#a6907c] border border-[#3d2816] select-none"
                            >
                              FC STOP
                            </span>
                          ) : (
                            <button
                              type="button"
                              onClick={() => setFcArrived(r.id, !r.fcArrived)}
                              title={
                                r.fcArrived
                                  ? 'Carrier has arrived at this waypoint (click to mark pending)'
                                  : 'Carrier has not yet arrived (click to mark arrived)'
                              }
                              className={`h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider border cursor-pointer ${
                                r.fcArrived
                                  ? 'bg-[#211307] text-[#FF7100] border-[#FF7100]/70 hover:bg-[#FF7100] hover:text-[#050506]'
                                  : 'bg-[#120d09] text-[#a6907c] border-[#3d2816] hover:border-[#FF7100] hover:text-[#F4ECE4]'
                              }`}
                            >
                              {r.fcArrived ? '✓ ARRIVED' : 'PENDING'}
                            </button>
                          )}

                          {!isPlanning && isNextFcStop && (
                            activeCarrierRoute && activeCarrierTargetRowId === r.id ? (
                              <span
                                title="Fleet Carrier route is currently active for this waypoint in the bottom plotter."
                                className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]"
                              >
                                ROUTING
                              </span>
                            ) : (
                              <button
                                type="button"
                                onClick={() => openCarrierRoutePrompt(index, r)}
                                title="Plot Fleet Carrier route to this next carrier waypoint"
                                className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#00D2FF] cursor-pointer"
                              >
                                ROUTE
                              </button>
                            )
                          )}

                          {!isLocked && (
                            <button
                              type="button"
                              onClick={() => toggleFcStop(r)}
                              title="Remove Fleet Carrier stop designation"
                              className="h-5 w-4 inline-flex items-center justify-center text-xs text-[#6e5b4b] hover:text-[#ff3b2f] cursor-pointer"
                            >
                              ×
                            </button>
                          )}
                        </>
                      ) : !isLocked ? (
                        <button
                          type="button"
                          onClick={() => toggleFcStop(r)}
                          title="Designate this waypoint as a Fleet Carrier stop"
                          className="h-5 px-2 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider text-[#6e5b4b] hover:text-[#FF7100] border border-dashed border-[#2e1f12] hover:border-[#FF7100] bg-transparent hover:bg-[#16100b] cursor-pointer"
                        >
                          + FC STOP
                        </button>
                      ) : (
                        <span className="text-[#6e5b4b] font-mono-tabular text-[10px]">—</span>
                      )}
                    </div>
                  </td>

                  {/* Visit Status / Galactic Timestamp */}
                  <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
                    {isVisited ? (
                      <div className="space-y-0.5 py-0.5">
                        <span className="inline-flex items-center gap-1 text-[10px] font-extrabold uppercase tracking-wider text-[#FF7100]">
                          ■ LOGGED VISIT
                        </span>
                        <div className="font-mono-tabular text-[10px] text-[#a6907c]">
                          {rv.visitedAtGalactic}
                        </div>
                      </div>
                    ) : (
                      <div className="font-mono-tabular text-[11px] text-[#a6907c] py-0.5">
                        {rv.number === 1 ? (
                          r.systemName?.trim() ? (
                            <span
                              className="text-[#00D2FF]"
                              title="Starting Waypoint (Origin)"
                            >
                              +0 Ly
                            </span>
                          ) : (
                            <span className="text-[#6e5b4b]">—</span>
                          )
                        ) : rv.distFromPrevLy !== undefined ? (
                          <span
                            className="text-[#00D2FF]"
                            title="Estimated straight-line distance from previous waypoint"
                          >
                            +{formatLy(rv.distFromPrevLy, 1)}
                          </span>
                        ) : (
                          <span
                            className="text-[#6e5b4b] uppercase text-[10px]"
                            title={
                              r.systemName?.trim()
                                ? 'Coordinates unknown for this waypoint or preceding waypoint'
                                : undefined
                            }
                          >
                            {r.systemName?.trim() ? 'UNKNOWN' : '—'}
                          </span>
                        )}
                      </div>
                    )}
                  </td>

                  {/* External Links + Row Manipulation + Drag Handle on the far right */}
                  <td className="px-3 py-1.5 text-right whitespace-nowrap align-top border-b border-[#24180e]">
                    <div className="inline-flex items-center justify-end gap-1.5 whitespace-nowrap py-0.5">
                      <HoverOpenInLinks rowView={rv} />
                      {!isLocked && (
                        <>
                          <RowManipulationButtons
                            index={index}
                            minIndex={lastVisitedIndex + 1}
                            total={plan.rows?.length ?? 0}
                            rowId={r.id}
                            insertMenuOpen={insertMenuAfterId === r.id}
                            onToggleInsertMenu={() =>
                              setInsertMenuAfterId(insertMenuAfterId === r.id ? null : r.id)
                            }
                            onInsert={(type) => handleInsert(r.id, type)}
                            onMove={(to) => handleMoveRow(r.id, index, to)}
                            onDelete={() => handleDeleteRow(r.id)}
                          />
                          <button
                            type="button"
                            draggable={true}
                            onDragStart={(e) => {
                              setDraggedIndex(index)
                              e.dataTransfer.effectAllowed = 'move'
                              e.dataTransfer.setData('text/plain', String(r.id))
                            }}
                            onDragEnd={() => {
                              setDraggedIndex(null)
                              setDragOverIndex(null)
                            }}
                            title="Drag to reorder waypoint"
                            className="h-5 px-1.5 inline-flex items-center justify-center text-[#a6907c] hover:text-[#FF7100] cursor-grab active:cursor-grabbing font-mono-tabular select-none border border-[#2e1f12] bg-[#140e09] hover:bg-[#24180e] text-xs shrink-0"
                          >
                            ⋮⋮
                          </button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
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
                ? 'DRAG (⋮⋮) OR USE ARROWS TO REORDER WAYPOINTS & SECTORS'
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
          jumpIdx={carrierJumpIdx}
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
          manualDistInput={manualDistInput}
          onChangeManualDist={(val) => {
            setManualDistInput(val)
            persistRouteState({ manualDistInput: val })
          }}
          manualTritInput={manualTritInput}
          onChangeManualTrit={(val) => {
            setManualTritInput(val)
            persistRouteState({ manualTritInput: val })
          }}
          onLogManualJump={handleLogManualJump}
          manualLegJumps={manualLegJumps}
          manualLegDist={manualLegDist}
          manualLegTrit={manualLegTrit}
          loggingJump={loggingJump}
          copiedNextJump={copiedNextJump}
          onCopySystem={(sys) => {
            navigator.clipboard?.writeText(sys).catch(() => {})
            setCopiedNextJump(true)
            setTimeout(() => setCopiedNextJump(false), 1500)
          }}
          onArrive={handleCarrierArriveAtWaypoint}
        />
      )}

      {/* Carrier Route Setup Prompt Modal */}
      {routePrompt && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4">
          <div className="w-full max-w-md border border-[#3d2816] border-t-2 border-t-[#00D2FF] bg-[#0b0907] shadow-2xl overflow-hidden text-xs">
            <div className="px-4 py-3 bg-[#140e09] border-b border-[#3d2816] flex items-center justify-between">
              <h3 className="text-xs font-extrabold uppercase tracking-[0.15em] text-[#00D2FF]">
                Fleet Carrier Navigation Plotter
              </h3>
              <button
                type="button"
                disabled={routePrompt.loading}
                onClick={() => setRoutePrompt(null)}
                className="text-[#a6907c] hover:text-[#F4ECE4] text-base leading-none cursor-pointer"
              >
                ×
              </button>
            </div>

            <div className="p-4 space-y-3.5">
              <div className="grid grid-cols-2 gap-2.5">
                <div>
                  <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                    Origin System
                  </label>
                  <input
                    type="text"
                    value={routePrompt.origin}
                    onChange={(e) =>
                      setRoutePrompt((p) => (p ? { ...p, origin: e.target.value } : null))
                    }
                    placeholder="Origin system..."
                    className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
                  />
                </div>
                <div>
                  <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                    Destination Waypoint
                  </label>
                  <input
                    type="text"
                    value={routePrompt.destination}
                    onChange={(e) =>
                      setRoutePrompt((p) => (p ? { ...p, destination: e.target.value } : null))
                    }
                    placeholder="Destination system..."
                    className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
                  />
                </div>
              </div>

              <div>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                  Vessel Classification
                </label>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() =>
                      setRoutePrompt((p) => (p ? { ...p, carrierType: 'player' } : null))
                    }
                    className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
                      routePrompt.carrierType === 'player'
                        ? 'bg-[#08151c] text-[#00D2FF] border-[#00D2FF] border-l-3'
                        : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
                    }`}
                  >
                    Personal Carrier
                  </button>
                  <button
                    type="button"
                    onClick={() =>
                      setRoutePrompt((p) => (p ? { ...p, carrierType: 'squadron' } : null))
                    }
                    className={`px-3 py-2 font-bold uppercase tracking-wider border transition cursor-pointer ${
                      routePrompt.carrierType === 'squadron'
                        ? 'bg-[#211307] text-[#FF7100] border-[#FF7100] border-l-3'
                        : 'bg-[#120d09] text-[#a6907c] border-[#2e1f12] hover:text-[#F4ECE4]'
                    }`}
                  >
                    Squadron Carrier
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mb-1">
                  Carrier Used Capacity (Tonnes, 0 – 25,000)
                </label>
                <input
                  type="number"
                  min={0}
                  max={25000}
                  value={routePrompt.capacityUsed}
                  onChange={(e) =>
                    setRoutePrompt((p) => (p ? { ...p, capacityUsed: e.target.value } : null))
                  }
                  className="w-full px-2.5 py-1.5 bg-[#050506] border border-[#3d2816] font-mono-tabular text-[#F4ECE4] focus:border-[#00D2FF] focus:outline-none"
                />
                <p className="text-[10px] text-[#a6907c] mt-1">
                  Pre-filled from your latest CarrierStats event. Used by Spansh to compute per-jump
                  Tritium consumption.
                </p>
              </div>
            </div>

            <div className="px-4 py-3 bg-[#140e09] border-t border-[#3d2816] flex items-center justify-between gap-2">
              <button
                type="button"
                disabled={routePrompt.loading}
                onClick={() => handleStartCarrierRoute(true)}
                className="ed-btn"
              >
                Manual Entry Mode
              </button>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={routePrompt.loading}
                  onClick={() => setRoutePrompt(null)}
                  className="px-3 py-1.5 text-[#a6907c] hover:text-[#F4ECE4] uppercase font-bold tracking-wider cursor-pointer"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={routePrompt.loading}
                  onClick={() => handleStartCarrierRoute(false)}
                  className="ed-btn-cyan"
                >
                  {routePrompt.loading ? 'QUERYING SPANSH...' : 'PLOT VIA SPANSH'}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

