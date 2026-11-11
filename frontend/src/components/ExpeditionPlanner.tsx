import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import {
  AddRow,
  ClearActiveCarrierRoute,
  CompleteExpedition,
  DeleteRow,
  GetActiveCarrierRoute,
  GetCarrierDepotInfo,
  GetCarrierLastLocation,
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
import { calculateCarrierJumpFuel } from '../utils/carrierRouting'
import {
  CarrierRouteBottomPanel,
  LegBanner,
  RegionHeaderDropdown,
  RowManipulationButtons,
  StatusBadge,
  WaypointDetailsDrawer,
} from './PlannerWidgets'
import { SquareCheckbox } from './SquareCheckbox'
import { CarrierRouteModal, type CarrierRoutePromptState } from './CarrierRouteModal'
import { WaypointRowItem } from './WaypointRowItem'

/**
 * ExpeditionPlannerProps defines the active expedition plan state, optimistic
 * update handlers, lifecycle modal triggers, and carrier plotter registration
 * callbacks for the main flight planner view.
 */
export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
  onPlanOptimisticUpdate: (next: expedition.PlanView) => void
  onOpenStartModal: () => void
  onRefreshPlan: () => void
  onRefreshAll: () => void
  onBusyChange: (busyMessage: string | null) => void
  onRegisterManualPlotter?: (openManualPlotter: () => void) => void
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

const rowStyle: CSSProperties = {
  contentVisibility: 'auto',
  containIntrinsicSize: '44px',
}

const useIsomorphicLayoutEffect = typeof window !== 'undefined' ? useLayoutEffect : useEffect

export function ExpeditionPlanner({
  plan,
  onPlanOptimisticUpdate,
  onOpenStartModal,
  onRefreshPlan,
  onRefreshAll,
  onBusyChange,
  onRegisterManualPlotter,
  onCarrierRouteOpenChange,
}: ExpeditionPlannerProps) {
  const [expandedRowIds, setExpandedRowIds] = useState<Record<number, boolean>>({})
  const expandedRowIdsRef = useRef<Record<number, boolean>>(expandedRowIds)
  expandedRowIdsRef.current = expandedRowIds
  useEffect(() => {
    expandedRowIdsRef.current = expandedRowIds
  }, [expandedRowIds])
  const [detailsById, setDetailsById] = useState<Record<number, expedition.WaypointDetails>>({})
  const detailsByIdRef = useRef<Record<number, expedition.WaypointDetails>>(detailsById)
  detailsByIdRef.current = detailsById
  useEffect(() => {
    detailsByIdRef.current = detailsById
  }, [detailsById])
  const [loadingDetails, setLoadingDetails] = useState<Record<number, boolean>>({})
  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)

  const isPlanning = plan?.expedition?.status === 'planning'

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
  const manualDistanceInputRef = useRef('')
  const manualTritiumInputRef = useRef('')
  const persistManualInputsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [manualLegJumps, setManualLegJumps] = useState(0)
  const [manualLegDistance, setManualLegDistance] = useState(0)
  const [manualLegTritium, setManualLegTritium] = useState(0)
  const [loggingJump, setLoggingJump] = useState(false)
  const [copiedNextJump, setCopiedNextJump] = useState(false)
  const [copiedSystemRowId, setCopiedSystemRowId] = useState<number | null>(null)
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const copiedNextJumpTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
      if (copiedNextJumpTimerRef.current) clearTimeout(copiedNextJumpTimerRef.current)
      if (persistManualInputsTimerRef.current) clearTimeout(persistManualInputsTimerRef.current)
    }
  }, [])

  const [showNotes, setShowNotes] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem('planisphere_show_notes')
      return saved !== null ? saved === 'true' : true
    } catch {
      return true
    }
  })

  const [showCarrierStop, setShowCarrierStop] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem('planisphere_show_carrier')
      return saved !== null ? saved === 'true' : true
    } catch {
      return true
    }
  })

  const toggleShowNotes = useCallback(() => {
    setShowNotes((prev) => {
      const next = !prev
      try {
        localStorage.setItem('planisphere_show_notes', String(next))
      } catch {}
      return next
    })
  }, [])

  const toggleShowCarrierStop = useCallback(() => {
    setShowCarrierStop((prev) => {
      const next = !prev
      try {
        localStorage.setItem('planisphere_show_carrier', String(next))
      } catch {}
      return next
    })
  }, [])

  const [columnMenuOpen, setColumnMenuOpen] = useState(false)

  // Load persisted carrier route state on mount or when active expedition changes
  useEffect(() => {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
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
          setActiveCarrierRoute(saved.route)
          setActiveCarrierTargetRowId(saved.targetRowId)
          setCarrierJumpIndex(saved.jumpIdx || 0)
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

  function schedulePersistManualInputs() {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
    }
    persistManualInputsTimerRef.current = setTimeout(() => {
      persistManualInputsTimerRef.current = null
      persistRouteState({
        manualDistInput: manualDistanceInputRef.current,
        manualTritInput: manualTritiumInputRef.current,
      })
    }, 250)
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
    if (copiedTimerRef.current) {
      clearTimeout(copiedTimerRef.current)
    }
    setCopiedSystemRowId(rowId)
    copiedTimerRef.current = setTimeout(() => {
      setCopiedSystemRowId((currentId) => (currentId === rowId ? null : currentId))
      copiedTimerRef.current = null
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
    if (!plan || isPlanning) return
    const currentExpandedRowIds = expandedRowIdsRef.current
    for (const idStr of Object.keys(currentExpandedRowIds)) {
      const id = Number(idStr)
      if (currentExpandedRowIds[id]) {
        const rowView = plan.rows?.find((r) => r.row.id === id)
        const isVisited = rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')
        if (isVisited) {
          const loadedDetails = detailsByIdRef.current[id]
          if (loadedDetails && loadedDetails.to === (rowView.row.visitedAt ?? '')) {
            continue
          }
          loadDetails(id)
        }
      }
    }
  }, [plan, isPlanning])

  // Highest row index that has been visited; rows at or above this index are locked
  // against structural edits (reordering, deleting, or changing system name).
  const lastVisitedIndex = useMemo(() => {
    if (!plan || !plan.rows || isPlanning) return -1
    let maxIndex = -1
    ;(plan.rows ?? []).forEach((rowView, index) => {
      if (rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')) {
        maxIndex = index
      }
    })
    return maxIndex
  }, [plan, isPlanning])

  const nextFcStopEntry = useMemo(() => {
    if (!plan?.rows) return null
    if (plan.nextFcStopRowId > 0) {
      const matchedRowIndex = plan.rows.findIndex(
        (rowView) => rowView?.row?.id === plan.nextFcStopRowId,
      )
      if (
        matchedRowIndex >= 0 &&
        plan.rows[matchedRowIndex].row.fcStop &&
        !plan.rows[matchedRowIndex].row.fcArrived
      ) {
        return { row: plan.rows[matchedRowIndex].row, index: matchedRowIndex }
      }
    }
    const fallbackRowIndex = plan.rows.findIndex(
      (rowView) =>
        rowView?.row?.rowType === 'waypoint' &&
        rowView.row.fcStop &&
        !rowView.row.fcArrived,
    )
    if (fallbackRowIndex >= 0) {
      return { row: plan.rows[fallbackRowIndex].row, index: fallbackRowIndex }
    }
    return null
  }, [plan])

  const nextFcStopSystem = nextFcStopEntry
    ? (drafts[nextFcStopEntry.row.id]?.systemName ?? nextFcStopEntry.row.systemName ?? '').trim()
    : ''

  const isManualCarrierRoute =
    manualModeOverride ||
    Boolean(activeCarrierRoute?.manualMode) ||
    !activeCarrierRoute?.jumps?.length

  const routedCarrierStopIndex = useMemo(() => {
    if (!activeCarrierRoute || !plan?.rows) return -1
    if (isManualCarrierRoute) {
      return nextFcStopEntry ? nextFcStopEntry.index : -1
    }
    if (activeCarrierTargetRowId > 0) {
      const targetRowIndex = plan.rows.findIndex(
        (rowView) => rowView?.row?.id === activeCarrierTargetRowId,
      )
      if (targetRowIndex >= 0) {
        return targetRowIndex
      }
    }
    return nextFcStopEntry ? nextFcStopEntry.index : -1
  }, [activeCarrierRoute, activeCarrierTargetRowId, isManualCarrierRoute, nextFcStopEntry, plan?.rows])

  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const lastOpenedKeyRef = useRef<string | null>(null)

  // Configure starting scroll position on initial open or expedition stage change:
  // - Planning: fully scrolled to the bottom
  // - Active: last visited waypoint centered in the viewport
  // - Completed: top of the list (scrollTop 0)
  useIsomorphicLayoutEffect(() => {
    if (!plan?.expedition) return
    const expeditionId = plan.expedition.id
    const status = plan.expedition.status
    const openKey = `${expeditionId}:${status}`

    if (lastOpenedKeyRef.current === openKey) {
      return
    }

    const applyScroll = () => {
      const scrollContainer = scrollContainerRef.current
      if (!scrollContainer || scrollContainer.clientHeight === 0) return false

      if (status === 'planning') {
        scrollContainer.scrollTop = scrollContainer.scrollHeight
        return true
      }

      if (status === 'active') {
        const targetRow =
          scrollContainer.querySelector<HTMLElement>('[data-last-visited="true"]') ??
          scrollContainer.querySelector<HTMLElement>('tr[data-row-index="0"]')

        if (targetRow) {
          targetRow.scrollIntoView({ block: 'center', behavior: 'instant' })
          const thead = scrollContainer.querySelector('thead')
          const theadHeight = thead ? thead.offsetHeight : 0
          const visibleHeight = scrollContainer.clientHeight - theadHeight
          const visibleCenter = theadHeight + visibleHeight / 2

          const containerRect = scrollContainer.getBoundingClientRect()
          const rowRect = targetRow.getBoundingClientRect()
          const rowCenter = rowRect.top - containerRect.top + rowRect.height / 2
          const delta = rowCenter - visibleCenter
          if (Math.abs(delta) > 1) {
            scrollContainer.scrollTop += delta
          }
          return true
        }
        scrollContainer.scrollTop = 0
        return true
      }

      if (status === 'completed') {
        scrollContainer.scrollTop = 0
        return true
      }

      return false
    }

    const initialApplied = applyScroll()
    if (initialApplied) {
      lastOpenedKeyRef.current = openKey
    }

    // Micro-adjustment on the next animation frame in case content-visibility: auto,
    // table layout reflow, or initial container measurement adjusted row heights
    const rafId = requestAnimationFrame(() => {
      if (applyScroll()) {
        lastOpenedKeyRef.current = openKey
      }
    })

    return () => {
      cancelAnimationFrame(rafId)
    }
  }, [plan?.expedition?.id, plan?.expedition?.status, lastVisitedIndex])

  function loadDetails(rowId: number) {
    setLoadingDetails((previous) => ({ ...previous, [rowId]: true }))
    GetWaypointDetails(rowId)
      .then((details) => setDetailsById((previous) => ({ ...previous, [rowId]: details })))
      .catch(console.error)
      .finally(() => setLoadingDetails((previous) => ({ ...previous, [rowId]: false })))
  }

  function toggleExpand(rowId: number) {
    const rowView = plan?.rows?.find((r) => r.row.id === rowId)
    const isVisited = rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')
    if (isPlanning || !isVisited) return

    const nextOpen = !expandedRowIdsRef.current[rowId]
    setExpandedRowIds((previous) => {
      const nextExpanded = { ...previous, [rowId]: nextOpen }
      expandedRowIdsRef.current = nextExpanded
      return nextExpanded
    })
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
    if (activeCarrierRoute && routedCarrierStopIndex >= 0) {
      const rowIndex = plan.rows.findIndex((rowView) => rowView?.row?.id === row.id)
      if (
        rowIndex >= 0 &&
        (isManualCarrierRoute
          ? rowIndex < routedCarrierStopIndex
          : rowIndex <= routedCarrierStopIndex)
      ) {
        return
      }
    }
    const nextFcStop = !row.fcStop
    const optimisticRows = plan.rows.map((rowView) => {
      if (rowView.row.id !== row.id) return rowView
      return expedition.RowView.createFrom({
        ...rowView,
        row: { ...rowView.row, fcStop: nextFcStop, fcArrived: nextFcStop ? rowView.row.fcArrived : false },
      })
    })
    let nextCarrierStopRowId = 0
    for (const rowView of optimisticRows) {
      if (rowView.row.rowType === 'waypoint' && rowView.row.fcStop && !rowView.row.fcArrived) {
        nextCarrierStopRowId = rowView.row.id
        break
      }
    }
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: optimisticRows,
        nextFcStopRowId: nextCarrierStopRowId,
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
    let nextCarrierStopRowId = 0
    for (const rowView of optimisticRows) {
      if (rowView.row.rowType === 'waypoint' && rowView.row.fcStop && !rowView.row.fcArrived) {
        nextCarrierStopRowId = rowView.row.id
        break
      }
    }
    onPlanOptimisticUpdate(
      expedition.PlanView.createFrom({
        ...plan,
        rows: optimisticRows,
        nextFcStopRowId: nextCarrierStopRowId,
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

  const handleMoveRow = useCallback(
    async (rowId: number, fromIndex: number, toIndex: number) => {
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
    },
    [lastVisitedIndex, onBusyChange, onPlanOptimisticUpdate, onRefreshPlan, plan],
  )

  async function handleLifecycleAction(label: string, actionFunction: () => Promise<void>) {
    onBusyChange(label)
    try {
      await actionFunction()
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
  }

  async function resolveCarrierStartSystem(upToIndex: number): Promise<string> {
    if (!plan) return ''
    let startSystem = ''
    try {
      const lastKnownCarrierLocation = await GetCarrierLastLocation()
      if (lastKnownCarrierLocation && lastKnownCarrierLocation.trim()) {
        startSystem = lastKnownCarrierLocation.trim()
      }
    } catch {
      startSystem = ''
    }

    const rows = plan.rows ?? []
    const searchFrom = Math.min(upToIndex - 1, rows.length - 1)

    // Fallback: most recent preceding Fleet Carrier stop in the expedition plan
    if (!startSystem) {
      for (let i = searchFrom; i >= 0; i--) {
        const candidate = rows[i]?.row
        if (candidate?.rowType === 'waypoint' && candidate.fcStop && candidate.systemName?.trim()) {
          startSystem = candidate.systemName.trim()
          break
        }
      }
    }

    // Secondary fallback: immediately preceding waypoint in the plan
    if (!startSystem) {
      for (let i = searchFrom; i >= 0; i--) {
        const candidate = rows[i]?.row
        if (candidate?.rowType === 'waypoint' && candidate.systemName?.trim()) {
          startSystem = candidate.systemName.trim()
          break
        }
      }
    }

    if (!startSystem && plan.playerSystem?.trim()) {
      startSystem = plan.playerSystem.trim()
    }

    return startSystem
  }

  async function resolveCarrierDefaults(): Promise<{ capacity: number; depot: number }> {
    let capacity = 5000
    let depot = 1000
    try {
      const depotInfo = await GetCarrierDepotInfo()
      if (depotInfo) {
        if (depotInfo.usedCapacity > 0) capacity = depotInfo.usedCapacity
        if (depotInfo.depotTritium > 0) depot = depotInfo.depotTritium
      }
    } catch {
      capacity = 5000
      depot = 1000
    }
    return { capacity, depot }
  }

  function switchActiveRouteToManualMode() {
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
    setManualModeOverride(true)
    let initialDistanceInput = ''
    let initialTritiumInput = ''
    if (
      activeCarrierRoute?.jumps &&
      carrierJumpIndex < activeCarrierRoute.jumps.length
    ) {
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
    const destination = nextFcStopSystem
    const [startSystem, defaults] = await Promise.all([
      resolveCarrierStartSystem(upToIndex),
      resolveCarrierDefaults(),
    ])

    const manualRoute = app.CarrierRouteResult.createFrom({
      origin: startSystem,
      destination,
      carrierType: 'player',
      capacityUsed: defaults.capacity,
      depotTritium: defaults.depot,
      manualMode: true,
      totalJumps: 0,
      totalDistLy: 0,
      totalTritium: 0,
      jumps: [],
    })
    if (persistManualInputsTimerRef.current) {
      clearTimeout(persistManualInputsTimerRef.current)
      persistManualInputsTimerRef.current = null
    }
    manualDistanceInputRef.current = ''
    manualTritiumInputRef.current = ''
    setActiveCarrierTargetRowId(targetRowId)
    setActiveCarrierRoute(manualRoute)
    setCarrierJumpIndex(0)
    setManualModeOverride(true)
    setManualLegJumps(0)
    setManualLegDistance(0)
    setManualLegTritium(0)
    setManualDistanceInput('')
    setManualTritiumInput('')
    persistRouteState({
      route: manualRoute,
      targetRowId,
      jumpIdx: 0,
      manualMode: true,
      manualLegJumps: 0,
      manualLegDist: 0,
      manualLegTrit: 0,
      manualDistInput: '',
      manualTritInput: '',
    })
  }

  useEffect(() => {
    onRegisterManualPlotter?.(openManualCarrierPlotter)
  })

  useEffect(() => {
    onCarrierRouteOpenChange?.(Boolean(activeCarrierRoute))
  }, [activeCarrierRoute, onCarrierRouteOpenChange])

  async function openCarrierRoutePrompt(rowIndex: number, targetRow: expedition.Row) {
    if (!plan) return
    const [startSystem, defaults] = await Promise.all([
      resolveCarrierStartSystem(rowIndex),
      resolveCarrierDefaults(),
    ])
    setRoutePrompt({
      targetRowId: targetRow.id,
      origin: startSystem,
      destination: (drafts[targetRow.id]?.systemName ?? targetRow.systemName ?? '').trim(),
      capacityUsed: String(defaults.capacity),
      depotTritium: String(defaults.depot),
      carrierType: 'player',
      loading: false,
    })
  }

  async function handleStartCarrierRoute() {
    if (!routePrompt) return
    const targetWaypointRowId = routePrompt.targetRowId
    const maxCarrierCapacityTonnes = routePrompt.carrierType === 'squadron' ? 60000 : 25000
    const carrierCapacity = Math.max(0, Math.min(maxCarrierCapacityTonnes, parseInt(routePrompt.capacityUsed, 10) || 0))
    const carrierDepot = Math.max(0, Math.min(1000, parseInt(routePrompt.depotTritium, 10) || 1000))

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
      if (persistManualInputsTimerRef.current) {
        clearTimeout(persistManualInputsTimerRef.current)
        persistManualInputsTimerRef.current = null
      }
      manualDistanceInputRef.current = ''
      manualTritiumInputRef.current = ''
      setActiveCarrierTargetRowId(targetWaypointRowId)
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
        targetRowId: targetWaypointRowId,
        jumpIdx: 0,
        manualMode: Boolean(routeResult.manualMode),
        manualLegJumps: 0,
        manualLegDist: 0,
        manualLegTrit: 0,
        manualDistInput: '',
        manualTritInput: '',
      })
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
      if (persistManualInputsTimerRef.current) {
        clearTimeout(persistManualInputsTimerRef.current)
        persistManualInputsTimerRef.current = null
      }
      manualDistanceInputRef.current = ''
      manualTritiumInputRef.current = ''
      setActiveCarrierTargetRowId(targetWaypointRowId)
      setActiveCarrierRoute(errorRoute)
      setCarrierJumpIndex(0)
      setManualModeOverride(true)
      setManualLegJumps(0)
      setManualLegDistance(0)
      setManualLegTritium(0)
      setManualDistanceInput('')
      setManualTritiumInput('')
      setRoutePrompt(null)
      persistRouteState({
        route: errorRoute,
        targetRowId: targetWaypointRowId,
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

  function handleUpdateCarrierDepot(newDepot: number, newCapacityUsed?: number) {
    if (!activeCarrierRoute) return
    const clampedDepot = Math.max(0, Math.min(1000, newDepot))
    const maxCarrierCapacityTonnes = activeCarrierRoute.carrierType === 'squadron' ? 60000 : 25000
    const updatedCapacity =
      newCapacityUsed !== undefined
        ? Math.max(0, Math.min(maxCarrierCapacityTonnes, newCapacityUsed))
        : (activeCarrierRoute.capacityUsed || 0)

    const currentJumps = activeCarrierRoute.jumps || []
    const recalculatedJumps = currentJumps.map((jump, hopIndex) => {
      if (hopIndex < carrierJumpIndex) {
        return jump
      }
      const recalculatedCost = calculateCarrierJumpFuel(
        jump.distanceLy,
        updatedCapacity,
        clampedDepot,
        maxCarrierCapacityTonnes
      )
      return {
        ...jump,
        tritiumUsed: recalculatedCost,
        fuelInTank: clampedDepot,
      }
    })

    const newTotalTritium = recalculatedJumps.reduce(
      (sum, carrierJump) => sum + (carrierJump.tritiumUsed || 0),
      0
    )

    const updatedRoute = app.CarrierRouteResult.createFrom({
      ...activeCarrierRoute,
      depotTritium: clampedDepot,
      capacityUsed: updatedCapacity,
      jumps: recalculatedJumps,
      totalTritium: newTotalTritium,
    })

    setActiveCarrierRoute(updatedRoute)
    persistRouteState({ route: updatedRoute })
  }

  function handleRefillDepot() {
    if (!activeCarrierRoute) return
    const currentDepot =
      activeCarrierRoute.depotTritium !== undefined && activeCarrierRoute.depotTritium >= 0
        ? activeCarrierRoute.depotTritium
        : 1000
    const needed = 1000 - currentDepot
    if (needed <= 0) return

    const currentCap = activeCarrierRoute.capacityUsed || 0
    // Refill depot to full (1000 or max possible) from storage.
    // If storage (used capacity) is available, transfer up to needed tonnes from storage into depot.
    const transfer = currentCap > 0 ? Math.min(needed, currentCap) : needed
    const newDepot = currentDepot + transfer
    const newCap = currentCap > 0 ? Math.max(0, currentCap - transfer) : currentCap

    handleUpdateCarrierDepot(newDepot, newCap)
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
      setCarrierJumpIndex(nextJumpIndex)

      // Deduct consumed fuel from the depot
      const nextDepot = Math.max(0, currentDepot - carrierHop.tritiumUsed)

      // Recalculate remaining hops based on the updated depot fuel level
      const currentJumps = activeCarrierRoute.jumps || []
      const recalculatedJumps = currentJumps.map((jump, hopIndex) => {
        if (hopIndex < nextJumpIndex) {
          return jump
        }
        const recalculatedCost = calculateCarrierJumpFuel(
          jump.distanceLy,
          activeCarrierRoute.capacityUsed || 0,
          nextDepot
        )
        return {
          ...jump,
          tritiumUsed: recalculatedCost,
          fuelInTank: nextDepot,
        }
      })
      const newTotalTritium = recalculatedJumps.reduce(
        (sum, carrierJump) => sum + (carrierJump.tritiumUsed || 0),
        0
      )

      const updatedRoute = app.CarrierRouteResult.createFrom({
        ...activeCarrierRoute,
        depotTritium: nextDepot,
        jumps: recalculatedJumps,
        totalTritium: newTotalTritium,
      })

      setActiveCarrierRoute(updatedRoute)
      persistRouteState({ route: updatedRoute, jumpIdx: nextJumpIndex })
    } catch (error) {
      console.error(error)
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
    } catch (error) {
      console.error(error)
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
      await setFcArrived(targetRowId, true)
    }
  }

  type TableItem =
    | { kind: 'region'; rowView: expedition.RowView; index: number; key: string }
    | { kind: 'waypoint'; rowView: expedition.RowView; index: number; key: string }
    | { kind: 'drawer'; rowView: expedition.RowView; index: number; key: string }

  const visibleColumnCount =
    (isPlanning ? 0 : 1) +
    1 +
    1 +
    1 +
    (showNotes ? 1 : 0) +
    (showCarrierStop ? 1 : 0) +
    1 +
    1

  const tableMinW =
    (isPlanning ? 0 : 32) +
    40 +
    240 +
    192 +
    (showNotes ? 224 : 0) +
    (showCarrierStop ? 160 : 0) +
    240 +
    380

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
        items.push({ kind: 'waypoint', rowView, index, key: `row-${row.id}` })
        const isExpanded = Boolean(expandedRowIds[row.id])
        if (!isPlanning && isVisited && isExpanded) {
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
    [lastVisitedIndex, dragOverIndex, draggedIndex, plan, handleMoveRow],
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

  const coveredLy =
    plan.coveredLy ??
    plan.rows?.reduce((sum, rowView) => {
      if (rowView.row?.rowType === 'waypoint' && rowView.row?.visitedAt && rowView.distFromPrevLy) {
        return sum + rowView.distFromPrevLy
      }
      return sum
    }, 0) ??
    0
  const completedLy = coveredLy > 0 ? coveredLy : plan.plannedLy

  const firstWaypoint = plan.rows?.find((rowView) => rowView.row.rowType === 'waypoint')
  const hasWp1System = Boolean(
    (drafts[firstWaypoint?.row.id ?? 0]?.systemName ?? firstWaypoint?.row.systemName ?? '').trim(),
  )
  const canComplete = (plan.visitedCount ?? 0) >= 2

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
            {isCompleted ? (
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                  COMPLETED ROUTE:
                </span>
                <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                  {formatLy(completedLy, 1)}
                </span>
              </div>
            ) : isActive ? (
              <>
                <div>
                  <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                    COVERED:
                  </span>
                  <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                    {formatLy(coveredLy, 1)}
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
              </>
            ) : (
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                  PLANNED ROUTE:
                </span>
                <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                  {formatLy(plan.plannedLy, 1)}
                </span>
              </div>
            )}
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
              <span>ENGAGE EXPEDITION</span>
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
                    : 'Waypoint #2 not visited yet — complete requires at least 2 visited waypoints (or delete flight plan in Plans)'
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
      <div ref={scrollContainerRef} className="flex-1 overflow-y-auto overflow-x-auto bg-[#070605]">
        <table
          style={{ minWidth: `${tableMinW}px` }}
          className="w-full table-fixed border-separate border-spacing-0 text-left"
        >
          <thead className="sticky top-0 z-20 bg-[#120d09] border-b-2 border-[#FF7100]/70 text-[10px] font-extrabold uppercase tracking-[0.14em] text-[#a6907c] select-none">
            <tr className="align-top">
              {!isPlanning && (
                <th className="w-8 px-1.5 py-2.5 text-center align-top border-b border-[#FF7100]/70" title="Expand Leg Telemetry">
                  Leg
                </th>
              )}
              <th className="w-10 px-1.5 py-2.5 text-right font-mono-tabular align-top border-b border-[#FF7100]/70">#</th>
              <th className="w-60 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Star System</th>
              <th className="w-48 px-3 py-2.5 align-top border-b border-[#FF7100]/70">POI / Body</th>
              {showNotes && (
                <th className="w-56 px-3 py-2.5 align-top border-b border-[#FF7100]/70">
                  Flight Notes
                </th>
              )}
              {showCarrierStop && (
                <th
                  className="w-40 px-2 py-2.5 text-center align-top border-b border-[#FF7100]/70"
                  title="Fleet Carrier Stop, Arrival Status and Spansh Router"
                >
                  Carrier Stop
                </th>
              )}
              <th className="w-60 px-3 py-2.5 align-top border-b border-[#FF7100]/70">Telemetry / Timestamp</th>
              <th className="w-[380px] px-3 py-2.5 text-right whitespace-nowrap align-top border-b border-[#FF7100]/70">
                <div className="inline-flex items-center justify-end gap-2">
                  <span>Controls</span>
                  <div className="relative inline-block text-left">
                    <button
                      type="button"
                      onClick={() => setColumnMenuOpen((prev) => !prev)}
                      title="Customize visible columns"
                      className="h-5 w-5 inline-flex items-center justify-center p-0 cursor-pointer bg-transparent transition-colors group"
                    >
                      <svg
                        viewBox="0 0 16 16"
                        className="w-4 h-4 pointer-events-none shrink-0"
                        fill="none"
                        xmlns="http://www.w3.org/2000/svg"
                      >
                        {/* Outer outline border matching SquareCheckbox */}
                        <rect
                          x="1"
                          y="1"
                          width="14"
                          height="14"
                          fill="#050506"
                          stroke={columnMenuOpen ? '#FF7100' : '#3d2816'}
                          strokeWidth="1.5"
                          className="group-hover:stroke-[#FF7100] transition-colors"
                        />
                        {/* Inner 4-square grid matching the 8x8 active footprint and ratio of SquareCheckbox */}
                        <rect
                          x="4"
                          y="4"
                          width="3.5"
                          height="3.5"
                          fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                          className="group-hover:fill-[#FF7100] transition-colors"
                        />
                        <rect
                          x="8.5"
                          y="4"
                          width="3.5"
                          height="3.5"
                          fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                          className="group-hover:fill-[#FF7100] transition-colors"
                        />
                        <rect
                          x="4"
                          y="8.5"
                          width="3.5"
                          height="3.5"
                          fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                          className="group-hover:fill-[#FF7100] transition-colors"
                        />
                        <rect
                          x="8.5"
                          y="8.5"
                          width="3.5"
                          height="3.5"
                          fill={columnMenuOpen ? '#FF7100' : '#a6907c'}
                          className="group-hover:fill-[#FF7100] transition-colors"
                        />
                      </svg>
                    </button>
                    {columnMenuOpen && (
                      <>
                        <div
                          className="fixed inset-0 z-30 cursor-default"
                          onClick={() => setColumnMenuOpen(false)}
                        />
                        <div className="absolute right-0 top-full mt-1 w-48 border border-[#FF7100] bg-[#140e09] shadow-2xl shadow-black z-40 p-2.5 text-left text-xs space-y-2 font-normal">
                          <div className="text-[9px] font-extrabold uppercase tracking-wider text-[#a6907c] pb-1 border-b border-[#2e1f12]">
                            Visible Columns
                          </div>
                          <label className="flex items-center gap-2 cursor-pointer text-[#F4ECE4] hover:text-[#FF7100] py-0.5 select-none">
                            <SquareCheckbox
                              checked={showCarrierStop}
                              onChange={toggleShowCarrierStop}
                              size="sm"
                              color="#FF7100"
                            />
                            <span className="text-[11px] font-mono-tabular uppercase">Carrier Stop</span>
                          </label>
                          <label className="flex items-center gap-2 cursor-pointer text-[#F4ECE4] hover:text-[#FF7100] py-0.5 select-none">
                            <SquareCheckbox
                              checked={showNotes}
                              onChange={toggleShowNotes}
                              size="sm"
                              color="#FF7100"
                            />
                            <span className="text-[11px] font-mono-tabular uppercase">Flight Notes</span>
                          </label>
                        </div>
                      </>
                    )}
                  </div>
                </div>
              </th>
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
                    <td colSpan={visibleColumnCount - 1} className="px-3 py-2 border-b border-[#3a2615]">
                      <RegionHeaderDropdown
                        expeditionId={currentExpedition.id}
                        rowId={row.id}
                        currentRegionId={row.regionId}
                        regionName={rowView.regionName}
                        locked={isLocked}
                        onSelect={(selectedRegionId) => changeRegion(row.id, selectedRegionId)}
                      />
                    </td>

                    <td className="w-[380px] px-3 py-2 text-right whitespace-nowrap border-b border-[#3a2615]">
                      {!isLocked && (
                        <div className="inline-flex items-center justify-end gap-1 whitespace-nowrap">
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

              if (item.kind === 'drawer') {
                return (
                  <tr key={item.key} style={rowStyle}>
                    <td colSpan={visibleColumnCount} className="p-0 border-b border-[#24180e]">
                      <WaypointDetailsDrawer
                        details={detailsById[row.id] ?? null}
                        loading={Boolean(loadingDetails[row.id])}
                        rowView={rowView}
                        modeId={rowView.legModeId}
                        modeColor={rowView.legModeColor}
                        modesList={modesList}
                        onChangeMode={(nextModeId) => handleLegModeChange(row.id, nextModeId)}
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
              const isNextFcStop = Boolean(
                row.fcStop && !row.fcArrived && nextFcStopEntry && row.id === nextFcStopEntry.row.id,
              )
              const isNextWaypoint = Boolean(rowView.isNextWaypoint || (plan.nextWaypointRowId && row.id === plan.nextWaypointRowId))
              const hasActiveCarrierRoute = Boolean(
                activeCarrierRoute &&
                  (isManualCarrierRoute ? isNextFcStop : activeCarrierTargetRowId === row.id),
              )
              const isFcStopLocked = Boolean(
                activeCarrierRoute &&
                  routedCarrierStopIndex >= 0 &&
                  (isManualCarrierRoute
                    ? index < routedCarrierStopIndex
                    : index <= routedCarrierStopIndex),
              )

              return (
                <WaypointRowItem
                  key={item.key}
                  rowView={rowView}
                  index={index}
                  isPlanning={isPlanning}
                  isLocked={isLocked}
                  isFcStopLocked={isFcStopLocked}
                  isBeingDragged={isBeingDragged}
                  isDragTarget={isDragTarget}
                  isExpanded={isExpanded}
                  isNextFcStop={isNextFcStop && !activeCarrierRoute}
                  isNextWaypoint={isNextWaypoint}
                  playerSystem={plan.playerSystem}
                  hasActiveCarrierRoute={hasActiveCarrierRoute}
                  draft={draft}
                  copiedSystemRowId={copiedSystemRowId}
                  insertMenuOpen={insertMenuAfterId === row.id}
                  lastVisitedIndex={lastVisitedIndex}
                  totalRows={plan.rows?.length ?? 0}
                  rowStyle={rowStyle}
                  dropProps={getRowDropProps(index, isDraggable)}
                  showNotes={showNotes}
                  showCarrierStop={showCarrierStop}
                  onToggleExpand={toggleExpand}
                  onDraftChange={(rowId, fieldName, fieldValue) =>
                    setDrafts((prev) => ({
                      ...prev,
                      [rowId]: { ...(prev[rowId] ?? draft), [fieldName]: fieldValue },
                    }))
                  }
                  onFieldFocus={(id) => {
                    editingRowIdRef.current = id
                  }}
                  onFieldBlur={(waypointRow, fieldName, fieldValue) => {
                    editingRowIdRef.current = null
                    commitField(waypointRow, fieldName, fieldValue)
                  }}
                  onCopySystem={handleCopySystem}
                  onToggleFcStop={toggleFcStop}
                  onOpenCarrierRoutePrompt={openCarrierRoutePrompt}
                  onToggleInsertMenu={(id) => setInsertMenuAfterId(insertMenuAfterId === id ? null : id)}
                  onInsert={handleInsert}
                  onMove={(id, targetIndex) => handleMoveRow(id, index, targetIndex)}
                  onDelete={handleDeleteRow}
                  onDragStart={(dragRowIndex, dragRowId, event) => {
                    setDraggedIndex(dragRowIndex)
                    event.dataTransfer.effectAllowed = 'move'
                    event.dataTransfer.setData('text/plain', String(dragRowId))
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
          manualMode={isManualCarrierRoute}
          hasNextFcStop={Boolean(nextFcStopEntry)}
          nextFcStopSystem={nextFcStopSystem}
          onCancelRoute={handleCancelCarrierRoute}
          onToggleManualMode={switchActiveRouteToManualMode}
          onCompleteSpanshJump={handleCompleteSpanshJump}
          manualDistanceInput={manualDistanceInput}
          onChangeManualDistance={(value) => {
            manualDistanceInputRef.current = value
            schedulePersistManualInputs()
          }}
          manualTritiumInput={manualTritiumInput}
          onChangeManualTritium={(value) => {
            manualTritiumInputRef.current = value
            schedulePersistManualInputs()
          }}
          onLogManualJump={handleLogManualJump}
          manualLegJumps={manualLegJumps}
          manualLegDistance={manualLegDistance}
          manualLegTritium={manualLegTritium}
          loggingJump={loggingJump}
          copiedNextJump={copiedNextJump}
          onCopySystem={(systemName) => {
            navigator.clipboard?.writeText(systemName).catch(() => {})
            if (copiedNextJumpTimerRef.current) {
              clearTimeout(copiedNextJumpTimerRef.current)
            }
            setCopiedNextJump(true)
            copiedNextJumpTimerRef.current = setTimeout(() => {
              setCopiedNextJump(false)
              copiedNextJumpTimerRef.current = null
            }, 1500)
          }}
          onArrive={handleCarrierArriveAtWaypoint}
          depotTritium={activeCarrierRoute.depotTritium !== undefined ? activeCarrierRoute.depotTritium : 1000}
          onRefillDepot={handleRefillDepot}
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

