import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
} from 'react'
import {
  AddRow,
  DeleteRow,
  MoveRow,
  OverrideCarrierStopArrival,
  SetLegMode,
  UpdateRow,
} from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'
import { useLocalStorageBoolean } from '../../hooks/useLocalStorageBoolean'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'
import { PlannerActiveLegFooter } from './PlannerActiveLegFooter'
import { PlannerConfirmModal, type PlannerConfirmState } from './PlannerConfirmModal'
import { PlannerHeaderBar } from './PlannerHeaderBar'
import {
  buildOptimisticDeletedRowPlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
  buildPlannerTableItems,
  computePlannerTableDimensions,
  executePlanMutation,
  gapToTargetIndex,
  type PlanMutationBehaviorOptions,
} from './plannerPlanHelpers'
import { PlannerRouteTable } from './PlannerRouteTable'
import { useCarrierRoute } from './useCarrierRoute'
import { useInitialExpeditionScroll } from './useInitialExpeditionScroll'
import { useWaypointDetails } from './useWaypointDetails'

const DEFAULT_GALACTIC_REGION_ID = 18 // Inner Orion Spur
const DRAG_AUTOSCROLL_ZONE_PIXELS = 72
const DRAG_AUTOSCROLL_MIN_SPEED = 2
const DRAG_AUTOSCROLL_MAX_SPEED = 20
const STICKY_TABLE_HEADER_PIXELS = 36
const HOLD_PREVIEW_DELAY_MS = 140

/**
 * ExpeditionPlannerProps defines the active expedition plan state, journal
 * archive revision token, optimistic update handlers, lifecycle modal triggers,
 * and carrier plotter registration callbacks for the main flight planner view.
 */
export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
  archiveRevision?: string
  onPlanOptimisticUpdate: (next: expedition.PlanView) => void
  onOpenStartModal: () => void
  onRefreshPlan: () => void
  onRefreshAll: () => void
  onBusyChange: (busyMessage: string | null) => void
  onRegisterManualPlotter?: (openManualPlotter: () => void) => void
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

/**
 * ExpeditionPlanner renders the primary flight plan table, inline cell drafts,
 * drag-and-drop row reordering, telemetry drawers, and Fleet Carrier plotter.
 */
export function ExpeditionPlanner({
  plan,
  archiveRevision = '',
  onPlanOptimisticUpdate,
  onOpenStartModal,
  onRefreshPlan,
  onRefreshAll,
  onBusyChange,
  onRegisterManualPlotter,
  onCarrierRouteOpenChange,
}: ExpeditionPlannerProps) {
  const isPlanning = plan?.expedition?.status === 'planning'
  const { expandedRowIds, detailsById, loadingDetails, toggleExpand } = useWaypointDetails(
    plan,
    isPlanning,
    archiveRevision,
  )

  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)
  const [heldIndex, setHeldIndex] = useState<number | null>(null)
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dragOverGapIndex, setDragOverGapIndex] = useState<number | null>(null)
  const [confirmState, setConfirmState] = useState<PlannerConfirmState | null>(null)
  const [copiedSystemRowId, setCopiedSystemRowId] = useState<number | null>(null)
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const grabbedRowAnchorRef = useRef<{ visualTop: number } | null>(null)
  const uncollapseRowAnchorRef = useRef<{ rowId: number; visualTop: number } | null>(null)
  const dragStartClientYRef = useRef<number | null>(null)
  const dragClientYRef = useRef<number | null>(null)

  const recordDragOverClientY = useCallback((clientY: number) => {
    if (dragStartClientYRef.current !== null) {
      if (Math.abs(clientY - dragStartClientYRef.current) < 12) {
        return
      }
      dragStartClientYRef.current = null
    }
    dragClientYRef.current = clientY
  }, [])

  const captureGrabAnchor = useCallback((rowIndex: number) => {
    const container = scrollContainerRef.current
    const rowElement = container?.querySelector<HTMLElement>(`tr[data-row-index="${rowIndex}"]`)
    if (container && rowElement) {
      grabbedRowAnchorRef.current = {
        visualTop: rowElement.getBoundingClientRect().top - container.getBoundingClientRect().top,
      }
    }
  }, [])

  const captureUncollapseAnchor = useCallback(
    (rowIndex: number | null) => {
      if (rowIndex === null || !plan?.rows) return
      const rowId = plan.rows[rowIndex]?.row?.id
      const container = scrollContainerRef.current
      const rowElement = container?.querySelector<HTMLElement>(`tr[data-row-index="${rowIndex}"]`)
      if (rowId && container && rowElement) {
        uncollapseRowAnchorRef.current = {
          rowId,
          visualTop: rowElement.getBoundingClientRect().top - container.getBoundingClientRect().top,
        }
      }
    },
    [plan],
  )

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
      if (holdTimerRef.current) clearTimeout(holdTimerRef.current)
    }
  }, [])

  useEffect(() => {
    if (heldIndex === null || draggedIndex !== null || typeof window === 'undefined') return
    const clearHeld = () => {
      if (holdTimerRef.current) {
        clearTimeout(holdTimerRef.current)
        holdTimerRef.current = null
      }
      captureUncollapseAnchor(heldIndex)
      setHeldIndex(null)
    }
    window.addEventListener('pointerup', clearHeld)
    window.addEventListener('blur', clearHeld)
    return () => {
      window.removeEventListener('pointerup', clearHeld)
      window.removeEventListener('blur', clearHeld)
    }
  }, [captureUncollapseAnchor, heldIndex, draggedIndex])

  const [showNotes, toggleShowNotes] = useLocalStorageBoolean('planisphere_show_notes', true)
  const [showCarrierStop, toggleShowCarrierStop] = useLocalStorageBoolean(
    'planisphere_show_carrier',
    true,
  )

  const firstWaypoint = plan?.rows?.find((rowView) => rowView?.row?.rowType === 'waypoint')
  const [originHasDraftSystem, setOriginHasDraftSystem] = useState<boolean | null>(null)
  useEffect(() => {
    setOriginHasDraftSystem(null)
  }, [firstWaypoint?.row.id, firstWaypoint?.row.systemName])

  const { lastVisitedIndex, lastFcArrivedIndex } = useMemo(() => {
    if (!plan || !plan.rows || isPlanning) {
      return { lastVisitedIndex: -1, lastFcArrivedIndex: -1 }
    }
    let maxVisited = -1
    let maxFcArrived = -1
    ;(plan.rows ?? []).forEach((rowView, index) => {
      if (!rowView) return
      if (rowView.visitState === 'visited') {
        maxVisited = index
      }
      if (rowView.row?.rowType === 'waypoint' && rowView.row.fcStop && rowView.row.fcArrived) {
        maxFcArrived = index
      }
    })
    return { lastVisitedIndex: maxVisited, lastFcArrivedIndex: maxFcArrived }
  }, [plan, isPlanning])

  const runPlanMutation = useCallback(
    (
      busyMessage: string,
      errorContext: string,
      mutationAction: () => Promise<unknown>,
      options?: PlanMutationBehaviorOptions,
    ) =>
      executePlanMutation(
        busyMessage,
        errorContext,
        mutationAction,
        { onBusyChange, onPlanOptimisticUpdate, onRefreshPlan },
        options,
      ),
    [onBusyChange, onPlanOptimisticUpdate, onRefreshPlan],
  )

  async function setFcArrived(rowId: number, arrived: boolean) {
    if (!plan) return
    await runPlanMutation(
      'UPDATING CARRIER ARRIVAL',
      'Failed to update carrier arrival',
      () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, fcArrived: arrived })),
      { optimisticPlan: buildOptimisticFcArrivedPlan(plan, rowId, arrived), onSuccess: onRefreshPlan },
    )
  }

  const carrierRoute = useCarrierRoute({
    plan,
    lastVisitedIndex,
    onBusyChange,
    onSetFcArrived: setFcArrived,
    onRegisterManualPlotter,
    onCarrierRouteOpenChange,
  })

  const scrollContainerRef = useRef<HTMLDivElement>(null)
  useInitialExpeditionScroll(
    scrollContainerRef,
    Boolean(plan?.expedition),
    plan?.expedition?.id,
    plan?.expedition?.status,
    lastVisitedIndex,
  )

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
    if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
    setCopiedSystemRowId(rowId)
    copiedTimerRef.current = setTimeout(() => {
      setCopiedSystemRowId((currentId) => (currentId === rowId ? null : currentId))
      copiedTimerRef.current = null
    }, 1500)
  }

  async function commitField(
    row: expedition.Row,
    field: 'systemName' | 'poiName' | 'notes',
    value: string,
  ) {
    if ((row[field] ?? '') === value) return
    await runPlanMutation(
      'UPDATING WAYPOINT',
      `Failed to update waypoint ${field}`,
      () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, [field]: value })),
      { rollbackOnError: true },
    )
  }

  async function toggleFcStop(row: expedition.Row) {
    if (!plan) return
    if (carrierRoute.activeCarrierRoute && carrierRoute.routedCarrierStopIndex >= 0) {
      const rowIndex = plan.rows.findIndex((rowView) => rowView?.row?.id === row.id)
      if (
        rowIndex >= 0 &&
        (carrierRoute.isManualCarrierRoute
          ? rowIndex < carrierRoute.routedCarrierStopIndex
          : rowIndex <= carrierRoute.routedCarrierStopIndex)
      ) {
        return
      }
    }
    const nextFcStop = !row.fcStop
    await runPlanMutation(
      'UPDATING FC STOP',
      'Failed to toggle fleet carrier stop',
      () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, fcStop: nextFcStop })),
      { optimisticPlan: buildOptimisticFcStopTogglePlan(plan, row, nextFcStop), onSuccess: onRefreshPlan },
    )
  }

  function handleRequestOverrideFcArrived(rowView: expedition.RowView) {
    if (!plan) return
    const targetArrived = !rowView.row.fcArrived
    const rowIndex = plan.rows.findIndex((item) => item?.row?.id === rowView.row.id)
    let cascadedCount = 0
    ;(plan.rows ?? []).forEach((candidateView, idx) => {
      const candidateRow = candidateView?.row
      if (!candidateRow || candidateRow.rowType !== 'waypoint' || !candidateRow.fcStop) return
      if (targetArrived && idx < rowIndex && !candidateRow.fcArrived) cascadedCount++
      if (!targetArrived && idx > rowIndex && candidateRow.fcArrived) cascadedCount++
    })
    setConfirmState({
      kind: 'override_fc_arrival',
      rowId: rowView.row.id,
      waypointNumber: rowView.number,
      systemName: rowView.row.systemName ?? '',
      targetArrived,
      cascadedCount,
    })
  }

  async function handleLegModeChange(rowId: number, modeId: number) {
    if (!plan) return
    await runPlanMutation(
      'UPDATING FLIGHT MODE',
      'Failed to update leg flight mode',
      () => SetLegMode(rowId, modeId),
      { optimisticPlan: buildOptimisticLegModePlan(plan, rowId, modeId), onSuccess: onRefreshAll },
    )
  }

  async function changeRegion(rowId: number, regionId: number) {
    await runPlanMutation(
      'UPDATING REGION',
      'Failed to update galactic region',
      () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, regionId })),
      { onSuccess: onRefreshPlan, rollbackOnError: true },
    )
  }

  async function handleInsert(afterRowId: number, rowType: 'waypoint' | 'region') {
    if (!plan) return
    setInsertMenuAfterId(null)
    await runPlanMutation(
      rowType === 'region' ? 'ADDING REGION HEADER' : 'ADDING WAYPOINT',
      `Failed to insert ${rowType}`,
      () => AddRow(plan.expedition.id, afterRowId, rowType, DEFAULT_GALACTIC_REGION_ID),
      { onSuccess: onRefreshPlan, rollbackOnError: true },
    )
  }

  async function executeDeleteRow(rowId: number) {
    if (!plan) return
    const targetRow = plan.rows?.find((rowView) => rowView?.row?.id === rowId)?.row
    await runPlanMutation(
      targetRow?.rowType === 'region' ? 'REMOVING REGION HEADER' : 'REMOVING WAYPOINT',
      'Failed to delete row',
      () => DeleteRow(rowId),
      { optimisticPlan: buildOptimisticDeletedRowPlan(plan, rowId), onSuccess: onRefreshPlan },
    )
  }

  function handleRequestDeleteRow(rowView: expedition.RowView) {
    if (!isPlanning && rowView.row.rowType === 'waypoint') {
      if (rowView.visitState === 'visited') {
        setConfirmState({
          kind: 'delete_visited',
          rowId: rowView.row.id,
          waypointNumber: rowView.number,
          systemName: rowView.row.systemName ?? '',
          visitedAtGalactic: rowView.visitedAtGalactic,
        })
        return
      }
      if (rowView.row.fcStop && rowView.row.fcArrived) {
        setConfirmState({
          kind: 'delete_arrived_fc',
          rowId: rowView.row.id,
          waypointNumber: rowView.number,
          systemName: rowView.row.systemName ?? '',
        })
        return
      }
    }
    void executeDeleteRow(rowView.row.id)
  }

  const executeMoveRow = useCallback(
    async (rowId: number, fromIndex: number, toIndex: number) => {
      if (!plan) return
      await runPlanMutation(
        'REORDERING ROUTE',
        'Failed to reorder route row',
        () => MoveRow(rowId, toIndex),
        { optimisticPlan: buildOptimisticMovedRowPlan(plan, fromIndex, toIndex), onSuccess: onRefreshPlan },
      )
    },
    [onRefreshPlan, plan, runPlanMutation],
  )

  const handleMoveRow = useCallback(
    (rowId: number, fromIndex: number, toIndex: number) => {
      if (!plan || fromIndex === toIndex || toIndex < 0 || toIndex >= plan.rows.length) return
      const draggedRowView = plan.rows?.[fromIndex]
      if (!draggedRowView) return
      if (!isPlanning && draggedRowView.row.rowType === 'waypoint') {
        const preview = draggedRowView.dropPreviews?.[toIndex]
        if (preview && (preview.status === 'invalid' || preview.status === 'blocked_fc')) {
          return
        }
        if (preview?.resetsFcArrived) {
          setConfirmState({
            kind: 'move_reset_fc',
            rowId,
            fromIndex,
            toIndex,
            waypointNumber: draggedRowView.number,
            systemName: draggedRowView.row.systemName ?? '',
          })
          return
        }
      }
      void executeMoveRow(rowId, fromIndex, toIndex)
    },
    [executeMoveRow, isPlanning, plan],
  )

  async function handleConfirmModalAction() {
    if (!confirmState) return
    const activeConfirm = confirmState
    setConfirmState(null)
    if (activeConfirm.kind === 'delete_visited' || activeConfirm.kind === 'delete_arrived_fc') {
      await executeDeleteRow(activeConfirm.rowId)
    } else if (activeConfirm.kind === 'move_reset_fc') {
      await executeMoveRow(activeConfirm.rowId, activeConfirm.fromIndex, activeConfirm.toIndex)
    } else if (activeConfirm.kind === 'override_fc_arrival') {
      await runPlanMutation(
        'UPDATING CARRIER ARRIVAL',
        'Failed to override carrier arrival',
        () => OverrideCarrierStopArrival(activeConfirm.rowId, activeConfirm.targetArrived),
        { onSuccess: onRefreshPlan, rollbackOnError: true },
      )
    }
  }

  async function handleLifecycleAction(label: string, actionFunction: () => Promise<void>) {
    await runPlanMutation(label, 'Failed to update expedition lifecycle state', actionFunction, {
      onSuccess: onRefreshAll,
      rollbackOnError: true,
    })
  }

  const { visibleColumnCount, tableMinWidthPixels } = computePlannerTableDimensions(
    isPlanning,
    showNotes,
    showCarrierStop,
  )

  const heldOrDraggedIndex = draggedIndex ?? heldIndex
  const isCompleted = plan?.expedition?.status === 'completed'

  const tableItems = useMemo(
    () =>
      buildPlannerTableItems(
        plan,
        expandedRowIds,
        Boolean(isPlanning),
        heldOrDraggedIndex,
        lastVisitedIndex,
      ),
    [plan, expandedRowIds, isPlanning, heldOrDraggedIndex, lastVisitedIndex],
  )

  useLayoutEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return

    const alignRowSelector = (selector: string, targetVisualTop: number) => {
      const rowElement = container.querySelector<HTMLElement>(selector)
      if (!rowElement) return
      const currentVisualTop =
        rowElement.getBoundingClientRect().top - container.getBoundingClientRect().top
      const deltaY = currentVisualTop - targetVisualTop
      if (Math.abs(deltaY) > 0.5) {
        container.scrollTop = Math.max(0, Math.round(container.scrollTop + deltaY))
      }
    }

    if (heldOrDraggedIndex !== null && grabbedRowAnchorRef.current !== null) {
      const { visualTop } = grabbedRowAnchorRef.current
      const selector = `tr[data-row-index="${heldOrDraggedIndex}"]`
      grabbedRowAnchorRef.current = null
      alignRowSelector(selector, visualTop)
      const frameId = window.requestAnimationFrame(() => {
        alignRowSelector(selector, visualTop)
      })
      return () => window.cancelAnimationFrame(frameId)
    }

    if (heldOrDraggedIndex === null && uncollapseRowAnchorRef.current !== null) {
      const { rowId, visualTop } = uncollapseRowAnchorRef.current
      const selector = `tr[data-row-id="${rowId}"]`
      uncollapseRowAnchorRef.current = null
      alignRowSelector(selector, visualTop)
      const frameId = window.requestAnimationFrame(() => {
        alignRowSelector(selector, visualTop)
      })
      return () => window.cancelAnimationFrame(frameId)
    }
  }, [heldOrDraggedIndex, plan])

  const computeAutoScrollDelta = useCallback((clientY: number | null): number => {
    const container = scrollContainerRef.current
    if (!container || clientY === null) return 0
    const rect = container.getBoundingClientRect()
    const topZoneBottom = rect.top + STICKY_TABLE_HEADER_PIXELS + DRAG_AUTOSCROLL_ZONE_PIXELS
    const bottomZoneTop = rect.bottom - DRAG_AUTOSCROLL_ZONE_PIXELS
    if (clientY >= rect.top - 32 && clientY < topZoneBottom && container.scrollTop > 0) {
      const rawDepth =
        (topZoneBottom - clientY) / (DRAG_AUTOSCROLL_ZONE_PIXELS + STICKY_TABLE_HEADER_PIXELS)
      const clampedDepth = Math.max(0, Math.min(1, rawDepth))
      const speed =
        DRAG_AUTOSCROLL_MIN_SPEED +
        (DRAG_AUTOSCROLL_MAX_SPEED - DRAG_AUTOSCROLL_MIN_SPEED) * clampedDepth * clampedDepth
      return -speed
    }
    if (
      clientY > bottomZoneTop &&
      clientY <= rect.bottom + 32 &&
      container.scrollTop + container.clientHeight < container.scrollHeight - 1
    ) {
      const rawDepth = (clientY - bottomZoneTop) / DRAG_AUTOSCROLL_ZONE_PIXELS
      const clampedDepth = Math.max(0, Math.min(1, rawDepth))
      const speed =
        DRAG_AUTOSCROLL_MIN_SPEED +
        (DRAG_AUTOSCROLL_MAX_SPEED - DRAG_AUTOSCROLL_MIN_SPEED) * clampedDepth * clampedDepth
      return speed
    }
    return 0
  }, [])

  useEffect(() => {
    if (draggedIndex === null || typeof window === 'undefined') {
      dragClientYRef.current = null
      dragStartClientYRef.current = null
      return
    }
    const updateDragClientY = (event: globalThis.DragEvent) => {
      if (event.clientY > 0) {
        recordDragOverClientY(event.clientY)
      }
    }
    window.addEventListener('dragover', updateDragClientY)

    let animationFrameId = 0
    const stepAutoScroll = () => {
      const container = scrollContainerRef.current
      const delta = computeAutoScrollDelta(dragClientYRef.current)
      if (container && delta !== 0) {
        container.scrollTop = Math.max(0, container.scrollTop + delta)
      }
      animationFrameId = window.requestAnimationFrame(stepAutoScroll)
    }
    animationFrameId = window.requestAnimationFrame(stepAutoScroll)

    return () => {
      window.removeEventListener('dragover', updateDragClientY)
      window.cancelAnimationFrame(animationFrameId)
    }
  }, [computeAutoScrollDelta, draggedIndex, recordDragOverClientY])

  const resolveHoverGapForRow = useCallback(
    (
      index: number,
      event: DragEvent<HTMLTableRowElement>,
    ): { gapIndex: number; targetIndex: number } | null => {
      if (isCompleted || draggedIndex === null || !plan?.rows) return null
      const draggedRowView = plan.rows[draggedIndex]
      if (!draggedRowView?.row) return null
      const isDraggingWaypointInActive =
        !isPlanning && draggedRowView.row.rowType === 'waypoint'
      if (isDraggingWaypointInActive && index <= lastVisitedIndex) {
        return null
      }

      const rect = event.currentTarget.getBoundingClientRect()
      const isTopHalf = event.clientY < rect.top + rect.height / 2
      let candidateGapIndex = isTopHalf ? index : index + 1
      if (isDraggingWaypointInActive && candidateGapIndex <= lastVisitedIndex) {
        candidateGapIndex = index + 1
      }

      let targetIndex = gapToTargetIndex(draggedIndex, candidateGapIndex)
      if (targetIndex === null) {
        candidateGapIndex = isTopHalf ? index + 1 : index
        if (isDraggingWaypointInActive && candidateGapIndex <= lastVisitedIndex) {
          return null
        }
        targetIndex = gapToTargetIndex(draggedIndex, candidateGapIndex)
        if (targetIndex === null) return null
      }

      if (isDraggingWaypointInActive) {
        const preview = draggedRowView.dropPreviews?.[targetIndex]
        if (preview?.status === 'valid_visited' || preview?.status === 'blocked_fc') {
          candidateGapIndex = index + 1
          targetIndex = gapToTargetIndex(draggedIndex, candidateGapIndex)
          if (targetIndex === null) return null
        }
        const finalPreview = draggedRowView.dropPreviews?.[targetIndex]
        if (!finalPreview || finalPreview.status !== 'valid_unvisited') {
          return null
        }
      }

      return { gapIndex: candidateGapIndex, targetIndex }
    },
    [draggedIndex, isCompleted, isPlanning, lastVisitedIndex, plan],
  )

  const getRowDropProps = useCallback(
    (index: number) => ({
      onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
        recordDragOverClientY(event.clientY)
        if (computeAutoScrollDelta(dragClientYRef.current) !== 0) {
          event.preventDefault()
          return
        }
        const resolved = resolveHoverGapForRow(index, event)
        if (!resolved) {
          if (dragOverGapIndex !== null) setDragOverGapIndex(null)
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (dragOverGapIndex !== resolved.gapIndex) {
          setDragOverGapIndex(resolved.gapIndex)
        }
      },
      onDrop: (event: DragEvent<HTMLTableRowElement>) => {
        const resolved = resolveHoverGapForRow(index, event)
        if (!resolved) return
        event.preventDefault()
        const from = draggedIndex
        const container = scrollContainerRef.current
        const dropTargetTop = container
          ? event.currentTarget.getBoundingClientRect().top -
            container.getBoundingClientRect().top
          : null
        setHeldIndex(null)
        setDraggedIndex(null)
        setDragOverGapIndex(null)
        if (from !== null && plan) {
          const draggedRow = plan.rows?.[from]?.row
          if (draggedRow) {
            if (dropTargetTop !== null) {
              uncollapseRowAnchorRef.current = { rowId: draggedRow.id, visualTop: dropTargetTop }
            }
            handleMoveRow(draggedRow.id, from, resolved.targetIndex)
          }
        }
      },
    }),
    [
      computeAutoScrollDelta,
      dragOverGapIndex,
      draggedIndex,
      handleMoveRow,
      plan,
      recordDragOverClientY,
      resolveHoverGapForRow,
    ],
  )

  const getSlotDropProps = useCallback(
    (gapIndex: number, targetIndex: number, preview: expedition.DropSlotPreview) => {
      const isAllowed =
        !isCompleted &&
        (preview.status === 'valid_visited' || preview.status === 'valid_unvisited')
      return {
        onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
          recordDragOverClientY(event.clientY)
          if (!isAllowed) {
            if (dragOverGapIndex !== null) setDragOverGapIndex(null)
            return
          }
          event.preventDefault()
          event.dataTransfer.dropEffect = 'move'
          if (dragOverGapIndex !== gapIndex) {
            setDragOverGapIndex(gapIndex)
          }
        },
        onDrop: (event: DragEvent<HTMLTableRowElement>) => {
          if (!isAllowed) return
          event.preventDefault()
          const from = draggedIndex
          const container = scrollContainerRef.current
          const dropTargetTop = container
            ? event.currentTarget.getBoundingClientRect().top -
              container.getBoundingClientRect().top
            : null
          setHeldIndex(null)
          setDraggedIndex(null)
          setDragOverGapIndex(null)
          if (from !== null && plan) {
            const draggedRow = plan.rows?.[from]?.row
            if (draggedRow) {
              if (dropTargetTop !== null) {
                uncollapseRowAnchorRef.current = { rowId: draggedRow.id, visualTop: dropTargetTop }
              }
              handleMoveRow(draggedRow.id, from, targetIndex)
            }
          }
        },
      }
    },
    [dragOverGapIndex, draggedIndex, handleMoveRow, isCompleted, plan, recordDragOverClientY],
  )

  const handleClearDragOverGap = useCallback(() => {
    if (dragOverGapIndex !== null) {
      setDragOverGapIndex(null)
    }
  }, [dragOverGapIndex])

  if (!plan) {
    return (
      <div className="flex-1 flex items-center justify-center p-12 text-[#a6907c] font-mono-tabular uppercase tracking-widest text-xs">
        [ LOADING EXPEDITION... ]
      </div>
    )
  }

  const currentExpedition = plan.expedition
  const modesList = plan.modes ?? []
  const hasOriginWaypointSystem =
    originHasDraftSystem ?? Boolean((firstWaypoint?.row.systemName ?? '').trim())

  return (
    <div
      onDragOver={(event) => {
        if (draggedIndex !== null) {
          recordDragOverClientY(event.clientY)
          event.preventDefault()
        }
      }}
      className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-[#070605]"
    >
      <PlannerHeaderBar
        plan={plan}
        hasOriginWaypointSystem={hasOriginWaypointSystem}
        onOpenStartModal={onOpenStartModal}
        onLifecycleAction={handleLifecycleAction}
      />

      <div
        ref={scrollContainerRef}
        onDragLeave={(event) => {
          const relatedTarget = event.relatedTarget as Node | null
          if (!relatedTarget || !scrollContainerRef.current?.contains(relatedTarget)) {
            dragClientYRef.current = null
          }
        }}
        onDragOver={(event) => {
          recordDragOverClientY(event.clientY)
          if (draggedIndex !== null) {
            event.preventDefault()
          }
          if (isCompleted || draggedIndex === null || !plan?.rows?.length) return
          const targetEl = event.target as HTMLElement | null
          if (targetEl?.closest('tbody')) return
          const tbodyEl = scrollContainerRef.current?.querySelector('tbody')
          if (!tbodyEl || event.clientY < tbodyEl.getBoundingClientRect().bottom) {
            if (dragOverGapIndex !== null) setDragOverGapIndex(null)
            return
          }
          const tailGapIndex = plan.rows.length
          const tailTargetIndex = gapToTargetIndex(draggedIndex, tailGapIndex)
          if (tailTargetIndex === null) return
          const draggedRowView = plan.rows[draggedIndex]
          if (!isPlanning && draggedRowView?.row?.rowType === 'waypoint') {
            const preview = draggedRowView.dropPreviews?.[tailTargetIndex]
            if (!preview || preview.status !== 'valid_unvisited') return
          }
          event.dataTransfer.dropEffect = 'move'
          if (dragOverGapIndex !== tailGapIndex) {
            setDragOverGapIndex(tailGapIndex)
          }
        }}
        onDrop={(event) => {
          if (isCompleted || draggedIndex === null || !plan?.rows?.length) return
          const targetEl = event.target as HTMLElement | null
          if (targetEl?.closest('tbody')) return
          const tbodyEl = scrollContainerRef.current?.querySelector('tbody')
          if (!tbodyEl || event.clientY < tbodyEl.getBoundingClientRect().bottom) return
          const tailGapIndex = plan.rows.length
          const tailTargetIndex = gapToTargetIndex(draggedIndex, tailGapIndex)
          if (tailTargetIndex === null) return
          const draggedRowView = plan.rows[draggedIndex]
          if (!isPlanning && draggedRowView?.row?.rowType === 'waypoint') {
            const preview = draggedRowView.dropPreviews?.[tailTargetIndex]
            if (!preview || preview.status !== 'valid_unvisited') return
          }
          event.preventDefault()
          const from = draggedIndex
          captureUncollapseAnchor(from)
          setHeldIndex(null)
          setDraggedIndex(null)
          setDragOverGapIndex(null)
          if (draggedRowView?.row) {
            handleMoveRow(draggedRowView.row.id, from, tailTargetIndex)
          }
        }}
        className="flex-1 overflow-y-auto overflow-x-auto bg-[#070605] [overflow-anchor:none]"
      >
        <PlannerRouteTable
          plan={plan}
          tableItems={tableItems}
          modesList={modesList}
          isPlanning={isPlanning}
          isCompleted={isCompleted}
          lastVisitedIndex={lastVisitedIndex}
          lastFcArrivedIndex={lastFcArrivedIndex}
          visibleColumnCount={visibleColumnCount}
          tableMinWidthPixels={tableMinWidthPixels}
          showNotes={showNotes}
          showCarrierStop={showCarrierStop}
          expandedRowIds={expandedRowIds}
          detailsById={detailsById}
          loadingDetails={loadingDetails}
          copiedSystemRowId={copiedSystemRowId}
          insertMenuAfterId={insertMenuAfterId}
          heldOrDraggedIndex={heldOrDraggedIndex}
          draggedIndex={draggedIndex}
          dragOverGapIndex={dragOverGapIndex}
          activeCarrierRoute={carrierRoute.activeCarrierRoute}
          activeCarrierTargetRowId={carrierRoute.activeCarrierTargetRowId}
          isManualCarrierRoute={carrierRoute.isManualCarrierRoute}
          nextFcStopEntry={carrierRoute.nextFcStopEntry}
          routedCarrierStopIndex={carrierRoute.routedCarrierStopIndex}
          getRowDropProps={getRowDropProps}
          getSlotDropProps={getSlotDropProps}
          onClearDragOverGap={handleClearDragOverGap}
          onToggleShowNotes={toggleShowNotes}
          onToggleShowCarrierStop={toggleShowCarrierStop}
          onChangeRegion={changeRegion}
          onSetInsertMenuAfterId={setInsertMenuAfterId}
          onInsert={handleInsert}
          onDeleteRow={handleRequestDeleteRow}
          onLegModeChange={handleLegModeChange}
          onToggleExpand={toggleExpand}
          onOriginSystemFilledChange={setOriginHasDraftSystem}
          onFieldBlur={commitField}
          onCopySystem={handleCopySystem}
          onToggleFcStop={toggleFcStop}
          onOverrideFcArrived={handleRequestOverrideFcArrived}
          onOpenCarrierRoutePrompt={carrierRoute.openCarrierRoutePrompt}
          onRowHoldStart={(holdRowIndex) => {
            captureGrabAnchor(holdRowIndex)
            if (holdTimerRef.current) clearTimeout(holdTimerRef.current)
            holdTimerRef.current = setTimeout(() => {
              holdTimerRef.current = null
              setHeldIndex(holdRowIndex)
            }, HOLD_PREVIEW_DELAY_MS)
          }}
          onRowHoldEnd={() => {
            if (holdTimerRef.current) {
              clearTimeout(holdTimerRef.current)
              holdTimerRef.current = null
            }
            if (draggedIndex === null) {
              captureUncollapseAnchor(heldIndex)
              setHeldIndex(null)
            }
          }}
          onRowDragStart={(dragRowIndex, dragRowId, event) => {
            if (holdTimerRef.current) {
              clearTimeout(holdTimerRef.current)
              holdTimerRef.current = null
            }
            if (heldIndex === null) {
              captureGrabAnchor(dragRowIndex)
            }
            dragStartClientYRef.current = event.clientY
            dragClientYRef.current = null
            event.dataTransfer.effectAllowed = 'move'
            event.dataTransfer.setData('text/plain', String(dragRowId))
            window.requestAnimationFrame(() => {
              setHeldIndex(dragRowIndex)
              setDraggedIndex(dragRowIndex)
            })
          }}
          onRowDragEnd={() => {
            if (holdTimerRef.current) {
              clearTimeout(holdTimerRef.current)
              holdTimerRef.current = null
            }
            grabbedRowAnchorRef.current = null
            dragStartClientYRef.current = null
            dragClientYRef.current = null
            const activeRowIndex = draggedIndex ?? heldIndex
            if (activeRowIndex !== null && uncollapseRowAnchorRef.current === null) {
              captureUncollapseAnchor(activeRowIndex)
            }
            setHeldIndex(null)
            setDraggedIndex(null)
            setDragOverGapIndex(null)
          }}
        />

        <PlannerActiveLegFooter
          isPlanning={isPlanning}
          isCompleted={isCompleted}
          isReordering={heldOrDraggedIndex !== null}
          onAppendRow={(rowType) => handleInsert(0, rowType)}
        />
      </div>

      {carrierRoute.activeCarrierRoute && (
        <CarrierRouteBottomPanel
          key={currentExpedition.id}
          route={carrierRoute.activeCarrierRoute}
          jumpIndex={carrierRoute.carrierJumpIndex}
          manualMode={carrierRoute.isManualCarrierRoute}
          hasNextFcStop={Boolean(carrierRoute.nextFcStopEntry)}
          nextFcStopSystem={carrierRoute.nextFcStopSystem}
          onCancelRoute={carrierRoute.handleCancelCarrierRoute}
          onToggleManualMode={carrierRoute.switchActiveRouteToManualMode}
          onCompleteSpanshJump={carrierRoute.handleCompleteSpanshJump}
          manualDistanceInput={carrierRoute.manualDistanceInput}
          onChangeManualDistance={carrierRoute.handleChangeManualDistance}
          manualTritiumInput={carrierRoute.manualTritiumInput}
          onChangeManualTritium={carrierRoute.handleChangeManualTritium}
          onLogManualJump={carrierRoute.handleLogManualJump}
          manualLegJumps={carrierRoute.manualLegJumps}
          manualLegDistance={carrierRoute.manualLegDistance}
          manualLegTritium={carrierRoute.manualLegTritium}
          loggingJump={carrierRoute.loggingJump}
          copiedNextJump={carrierRoute.copiedNextJump}
          onCopySystem={carrierRoute.handleCopyNextJumpSystem}
          onArrive={carrierRoute.handleCarrierArriveAtWaypoint}
          depotTritium={
            carrierRoute.activeCarrierRoute.depotTritium !== undefined
              ? carrierRoute.activeCarrierRoute.depotTritium
              : 1000
          }
          onRefillDepot={carrierRoute.handleRefillDepot}
        />
      )}

      {carrierRoute.routePrompt && (
        <CarrierRouteModal
          prompt={carrierRoute.routePrompt}
          onUpdate={(updater) =>
            carrierRoute.setRoutePrompt((previous) => (previous ? updater(previous) : null))
          }
          onStart={carrierRoute.handleStartCarrierRoute}
          onClose={() => carrierRoute.setRoutePrompt(null)}
        />
      )}

      {confirmState && (
        <PlannerConfirmModal
          confirmState={confirmState}
          onConfirm={() => void handleConfirmModalAction()}
          onCancel={() => setConfirmState(null)}
        />
      )}
    </div>
  )
}
