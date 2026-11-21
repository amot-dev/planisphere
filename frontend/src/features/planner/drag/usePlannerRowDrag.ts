import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type RefObject,
} from 'react'
import { expedition } from '../../../api/models'
import { buildPlannerTableItems, type PlannerTableItem } from '../../../domain/planTableItems'
import { clearPendingHoldTimer } from './plannerDragDom'
import { useDragEdgeAutoScroll, useDragScrollAnchors } from './useDragViewportScroll'
import { usePlannerDropHandlers } from './usePlannerDropHandlers'
import { resolveActiveDragRowIndex, useCachedRowDropPreviews } from './useRowDropPreviewCache'

const HOLD_PREVIEW_DELAY_MS = 140
const EMPTY_DROP_SLOT_GAPS: Set<number> = new Set()

/**
 * UsePlannerRowDragOptions configures row hold/drag preview state, scroll
 * anchoring across collapsed visited stretches, and edge auto-scrolling.
 */
export interface UsePlannerRowDragOptions {
  plan: expedition.PlanView | null
  isPlanning: boolean
  isCompleted: boolean
  lastVisitedIndex: number
  lockedSpanshTargetRowId: number
  expandedRowIds: Record<number, boolean>
  scrollContainerRef: RefObject<HTMLDivElement | null>
  onMoveRow: (
    rowId: number,
    fromIndex: number,
    toIndex: number,
    preview: expedition.DropSlotPreview | null,
  ) => boolean
}

/**
 * usePlannerRowDrag manages hold-to-preview drop bounds, HTML5 row drag-and-drop,
 * scroll anchoring when visited stretches collapse and expand, and viewport auto-scroll.
 */
export function usePlannerRowDrag({
  plan,
  isPlanning,
  isCompleted,
  lastVisitedIndex,
  lockedSpanshTargetRowId,
  expandedRowIds,
  scrollContainerRef,
  onMoveRow,
}: UsePlannerRowDragOptions) {
  const [heldIndex, setHeldIndex] = useState<number | null>(null)
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dragOverGapIndex, setDragOverGapIndex] = useState<number | null>(null)
  const [activeDropPreviews, setActiveDropPreviews] = useState<expedition.DropSlotPreview[] | null>(
    null,
  )

  const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const interactionTokenRef = useRef(0)
  const activeGrabbedRowIdRef = useRef<number | null>(null)
  const dragOverGapIndexRef = useRef<number | null>(null)
  const loadedPreviewsContextRef = useRef<{
    plan: expedition.PlanView | null
    lockedSpanshTargetRowId: number
    rowId: number
  } | null>(null)
  const heldOrDraggedIndex = draggedIndex ?? heldIndex

  const updateDragOverGapIndex = useCallback((targetGapIndex: number | null) => {
    if (dragOverGapIndexRef.current === targetGapIndex) return
    dragOverGapIndexRef.current = targetGapIndex
    setDragOverGapIndex(targetGapIndex)
  }, [])

  const resetDragState = useCallback(() => {
    interactionTokenRef.current++
    activeGrabbedRowIdRef.current = null
    loadedPreviewsContextRef.current = null
    dragOverGapIndexRef.current = null
    clearPendingHoldTimer(holdTimerRef)
    setHeldIndex(null)
    setDraggedIndex(null)
    setDragOverGapIndex(null)
    setActiveDropPreviews(null)
  }, [])

  useEffect(() => () => clearPendingHoldTimer(holdTimerRef), [])

  const { getImmediatePreviews, ensureDropPreviews } = useCachedRowDropPreviews(
    plan,
    isPlanning,
    isCompleted,
    lockedSpanshTargetRowId,
  )

  useEffect(() => {
    resetDragState()
  }, [isCompleted, isPlanning, plan?.expedition?.id, resetDragState])

  useEffect(() => {
    const grabbedRowId = activeGrabbedRowIdRef.current
    if (heldOrDraggedIndex === null || isPlanning || isCompleted || grabbedRowId === null) return
    const foundIndex = resolveActiveDragRowIndex(plan?.rows, grabbedRowId)
    if (foundIndex < 0) {
      resetDragState()
      return
    }
    if (heldIndex !== null && heldIndex !== foundIndex) setHeldIndex(foundIndex)
    if (draggedIndex !== null && draggedIndex !== foundIndex) setDraggedIndex(foundIndex)
    const loadedContext = loadedPreviewsContextRef.current
    if (
      loadedContext &&
      loadedContext.plan === plan &&
      loadedContext.lockedSpanshTargetRowId === lockedSpanshTargetRowId &&
      loadedContext.rowId === grabbedRowId
    ) {
      return
    }
    loadedPreviewsContextRef.current = { plan, lockedSpanshTargetRowId, rowId: grabbedRowId }
    const token = interactionTokenRef.current
    void ensureDropPreviews(plan?.rows?.[foundIndex]?.row).then((previews) => {
      if (interactionTokenRef.current === token && previews !== null) {
        setActiveDropPreviews(previews)
      }
    })
  }, [
    draggedIndex,
    ensureDropPreviews,
    heldIndex,
    heldOrDraggedIndex,
    isCompleted,
    isPlanning,
    lockedSpanshTargetRowId,
    plan,
    resetDragState,
  ])

  const tableItems: PlannerTableItem[] = useMemo(
    () =>
      buildPlannerTableItems(
        plan,
        expandedRowIds,
        isPlanning,
        heldOrDraggedIndex,
        lastVisitedIndex,
        activeDropPreviews,
      ),
    [activeDropPreviews, expandedRowIds, heldOrDraggedIndex, isPlanning, lastVisitedIndex, plan],
  )

  const { dropSlotGaps, hasCollapsedRows } = useMemo(() => {
    let gaps: Set<number> | null = null
    let collapsed = false
    for (const item of tableItems) {
      if (item.kind === 'drop_slot') {
        if (!gaps) gaps = new Set<number>()
        gaps.add(item.gapIndex)
      } else if (item.kind === 'collapsed_visited') {
        collapsed = true
      }
    }
    return { dropSlotGaps: gaps ?? EMPTY_DROP_SLOT_GAPS, hasCollapsedRows: collapsed }
  }, [tableItems])

  const { grabbedRowAnchorRef, uncollapseRowAnchorRef, captureGrabAnchor, captureUncollapseAnchor } =
    useDragScrollAnchors(scrollContainerRef, plan, tableItems, heldOrDraggedIndex)

  const { dragStartClientYRef, dragClientYRef, recordDragOverClientY } =
    useDragEdgeAutoScroll(scrollContainerRef, draggedIndex)

  useEffect(() => {
    if (heldIndex === null || draggedIndex !== null || typeof window === 'undefined') return
    const clearHeld = () => {
      captureUncollapseAnchor(heldIndex)
      resetDragState()
    }
    window.addEventListener('pointerup', clearHeld)
    window.addEventListener('blur', clearHeld)
    return () => {
      window.removeEventListener('pointerup', clearHeld)
      window.removeEventListener('blur', clearHeld)
    }
  }, [captureUncollapseAnchor, draggedIndex, heldIndex, resetDragState])

  const finishDropMove = useCallback(
    (
      fromIndex: number,
      targetIndex: number,
      preview: expedition.DropSlotPreview | null,
      dropVisualTop: number | null,
    ) => {
      const draggedRow = plan?.rows?.[fromIndex]?.row
      captureUncollapseAnchor(fromIndex)
      resetDragState()
      if (!draggedRow) return
      const movedImmediately = onMoveRow(draggedRow.id, fromIndex, targetIndex, preview)
      if (movedImmediately && dropVisualTop !== null) {
        uncollapseRowAnchorRef.current = { rowId: draggedRow.id, visualTop: dropVisualTop }
      }
    },
    [captureUncollapseAnchor, onMoveRow, plan, resetDragState, uncollapseRowAnchorRef],
  )

  const { getRowDropProps, getSlotDropProps, scrollContainerDropProps } = usePlannerDropHandlers({
    plan,
    isPlanning,
    isCompleted,
    lastVisitedIndex,
    draggedIndex,
    dragOverGapIndexRef,
    updateDragOverGapIndex,
    dropSlotGaps,
    activeDropPreviews,
    scrollContainerRef,
    dragClientYRef,
    recordDragOverClientY,
    finishDropMove,
  })

  const handleRowHoldStart = useCallback(
    (holdRowIndex: number) => {
      const token = ++interactionTokenRef.current
      const targetRow = plan?.rows?.[holdRowIndex]?.row
      activeGrabbedRowIdRef.current = targetRow?.id ?? null
      loadedPreviewsContextRef.current = null
      const previewsPromise = ensureDropPreviews(targetRow)
      clearPendingHoldTimer(holdTimerRef)
      holdTimerRef.current = setTimeout(() => {
        holdTimerRef.current = null
        void previewsPromise.then((previews) => {
          if (interactionTokenRef.current !== token) return
          if (previews && previews.length > 0) captureGrabAnchor(holdRowIndex)
          if (previews !== null && targetRow) {
            loadedPreviewsContextRef.current = { plan, lockedSpanshTargetRowId, rowId: targetRow.id }
          }
          setActiveDropPreviews(previews)
          setHeldIndex(holdRowIndex)
        })
      }, HOLD_PREVIEW_DELAY_MS)
    },
    [captureGrabAnchor, ensureDropPreviews, lockedSpanshTargetRowId, plan],
  )

  const handleRowHoldEnd = useCallback(() => {
    clearPendingHoldTimer(holdTimerRef)
    if (draggedIndex === null) {
      captureUncollapseAnchor(heldIndex)
      resetDragState()
    }
  }, [captureUncollapseAnchor, draggedIndex, heldIndex, resetDragState])

  const handleRowDragStart = useCallback(
    (dragRowIndex: number, dragRowId: number, event: DragEvent) => {
      const token = ++interactionTokenRef.current
      clearPendingHoldTimer(holdTimerRef)
      activeGrabbedRowIdRef.current = dragRowId
      const targetRow = plan?.rows?.[dragRowIndex]?.row
      const immediatePreviews = getImmediatePreviews(targetRow)
      const previewsPromise = immediatePreviews !== null ? null : ensureDropPreviews(targetRow)
      dragStartClientYRef.current = event.clientY
      dragClientYRef.current = null
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', String(dragRowId))
      // Defer collapsing non-matching visited stretches until the next frame so
      // the browser snapshots the uncollapsed row drag ghost before layout shrinks.
      window.requestAnimationFrame(() => {
        if (interactionTokenRef.current !== token) return
        const alreadyCollapsed = activeDropPreviews !== null && heldIndex === dragRowIndex
        if (immediatePreviews !== null && immediatePreviews.length > 0 && !alreadyCollapsed) {
          captureGrabAnchor(dragRowIndex)
        }
        if (immediatePreviews !== null) {
          loadedPreviewsContextRef.current = { plan, lockedSpanshTargetRowId, rowId: dragRowId }
          setActiveDropPreviews(immediatePreviews)
        }
        setHeldIndex(dragRowIndex)
        setDraggedIndex(dragRowIndex)
        if (previewsPromise) {
          void previewsPromise.then((previews) => {
            if (interactionTokenRef.current !== token || !previews) return
            if (previews.length > 0) captureGrabAnchor(dragRowIndex)
            loadedPreviewsContextRef.current = { plan, lockedSpanshTargetRowId, rowId: dragRowId }
            setActiveDropPreviews(previews)
          })
        }
      })
    },
    [
      activeDropPreviews,
      captureGrabAnchor,
      dragClientYRef,
      dragStartClientYRef,
      ensureDropPreviews,
      getImmediatePreviews,
      heldIndex,
      lockedSpanshTargetRowId,
      plan,
    ],
  )

  const handleRowDragEnd = useCallback(() => {
    grabbedRowAnchorRef.current = null
    dragStartClientYRef.current = null
    dragClientYRef.current = null
    const activeRowIndex = draggedIndex ?? heldIndex
    if (activeRowIndex !== null && uncollapseRowAnchorRef.current === null) {
      captureUncollapseAnchor(activeRowIndex)
    }
    resetDragState()
  }, [
    captureUncollapseAnchor,
    dragClientYRef,
    dragStartClientYRef,
    draggedIndex,
    grabbedRowAnchorRef,
    heldIndex,
    resetDragState,
    uncollapseRowAnchorRef,
  ])

  const handleClearDragOverGap = useCallback(() => {
    if (dragOverGapIndexRef.current !== null) updateDragOverGapIndex(null)
  }, [updateDragOverGapIndex])

  return {
    tableItems,
    dropSlotGaps,
    hasCollapsedRows,
    heldOrDraggedIndex,
    draggedIndex,
    dragOverGapIndex,
    recordDragOverClientY,
    scrollContainerDropProps,
    getRowDropProps,
    getSlotDropProps,
    handleClearDragOverGap,
    handleRowHoldStart,
    handleRowHoldEnd,
    handleRowDragStart,
    handleRowDragEnd,
  }
}

