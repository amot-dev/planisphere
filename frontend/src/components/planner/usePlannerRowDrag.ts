import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type HTMLAttributes,
  type MutableRefObject,
  type RefObject,
} from 'react'
import { GetRowDropPreviews } from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'
import {
  alignContainerToRowSelector,
  clearPendingHoldTimer,
  computeDragAutoScrollDelta,
  measureVisualTop,
  resolveContainerTailDrop,
  resolveHoverGap,
} from './plannerDragHelpers'
import { buildPlannerTableItems, type PlannerTableItem } from './plannerPlanHelpers'

const useIsomorphicLayoutEffect = typeof window !== 'undefined' ? useLayoutEffect : useEffect
const HOLD_PREVIEW_DELAY_MS = 140

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

function useCachedRowDropPreviews(
  plan: expedition.PlanView | null,
  isPlanning: boolean,
  isCompleted: boolean,
  lockedSpanshTargetRowId: number,
) {
  const cachedPreviewsByRowIdRef = useRef<Record<number, expedition.DropSlotPreview[]>>({})
  const inflightByRowIdRef = useRef<Record<number, Promise<expedition.DropSlotPreview[] | null>>>({})
  const planRef = useRef(plan)
  const lockedTargetRef = useRef(lockedSpanshTargetRowId)

  if (planRef.current !== plan || lockedTargetRef.current !== lockedSpanshTargetRowId) {
    planRef.current = plan
    lockedTargetRef.current = lockedSpanshTargetRowId
    cachedPreviewsByRowIdRef.current = {}
    inflightByRowIdRef.current = {}
  }

  const getImmediatePreviews = useCallback(
    (row: expedition.Row | undefined): expedition.DropSlotPreview[] | null => {
      if (isPlanning || isCompleted || !row || row.rowType !== 'waypoint') return null
      return cachedPreviewsByRowIdRef.current[row.id] ?? null
    },
    [isCompleted, isPlanning],
  )

  const ensureDropPreviews = useCallback(
    (row: expedition.Row | undefined): Promise<expedition.DropSlotPreview[] | null> => {
      if (isPlanning || isCompleted || !row || row.rowType !== 'waypoint') {
        return Promise.resolve(null)
      }
      const cached = cachedPreviewsByRowIdRef.current[row.id]
      if (cached) return Promise.resolve(cached)
      const existingInflight = inflightByRowIdRef.current[row.id]
      if (existingInflight) return existingInflight

      const requestPlan = planRef.current
      const requestPromise = GetRowDropPreviews(row.id)
        .then((fetched) => {
          if (planRef.current === requestPlan && Array.isArray(fetched)) {
            cachedPreviewsByRowIdRef.current[row.id] = fetched
            return fetched
          }
          return null
        })
        .catch(() => null)
        .finally(() => {
          if (inflightByRowIdRef.current[row.id] === requestPromise) {
            delete inflightByRowIdRef.current[row.id]
          }
        })
      inflightByRowIdRef.current[row.id] = requestPromise
      return requestPromise
    },
    [isCompleted, isPlanning],
  )

  return { getImmediatePreviews, ensureDropPreviews }
}

function useDragScrollAnchors(
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  plan: expedition.PlanView | null,
  tableItems: PlannerTableItem[],
  heldOrDraggedIndex: number | null,
) {
  const grabbedRowAnchorRef = useRef<{ visualTop: number } | null>(null)
  const uncollapseRowAnchorRef = useRef<{ rowId: number; visualTop: number } | null>(null)

  const captureGrabAnchor = useCallback(
    (rowIndex: number) => {
      const container = scrollContainerRef.current
      const rowElement = container?.querySelector<HTMLElement>(`tr[data-row-index="${rowIndex}"]`) ?? null
      const visualTop = measureVisualTop(container, rowElement)
      if (visualTop !== null) grabbedRowAnchorRef.current = { visualTop }
    },
    [scrollContainerRef],
  )

  const captureUncollapseAnchor = useCallback(
    (rowIndex: number | null) => {
      if (rowIndex === null || !plan?.rows) return
      const rowId = plan.rows[rowIndex]?.row?.id
      const container = scrollContainerRef.current
      const rowElement = container?.querySelector<HTMLElement>(`tr[data-row-index="${rowIndex}"]`) ?? null
      const visualTop = measureVisualTop(container, rowElement)
      if (rowId && visualTop !== null) uncollapseRowAnchorRef.current = { rowId, visualTop }
    },
    [plan, scrollContainerRef],
  )

  useIsomorphicLayoutEffect(() => {
    const container = scrollContainerRef.current
    if (!container) return
    if (heldOrDraggedIndex !== null && grabbedRowAnchorRef.current !== null) {
      const { visualTop } = grabbedRowAnchorRef.current
      const selector = `tr[data-row-index="${heldOrDraggedIndex}"]`
      grabbedRowAnchorRef.current = null
      alignContainerToRowSelector(container, selector, visualTop)
      const frameId = window.requestAnimationFrame(() => alignContainerToRowSelector(container, selector, visualTop))
      return () => window.cancelAnimationFrame(frameId)
    }
    if (heldOrDraggedIndex === null && uncollapseRowAnchorRef.current !== null) {
      const { rowId, visualTop } = uncollapseRowAnchorRef.current
      const selector = `tr[data-row-id="${rowId}"]`
      uncollapseRowAnchorRef.current = null
      alignContainerToRowSelector(container, selector, visualTop)
      const frameId = window.requestAnimationFrame(() => alignContainerToRowSelector(container, selector, visualTop))
      return () => window.cancelAnimationFrame(frameId)
    }
  }, [heldOrDraggedIndex, plan, scrollContainerRef, tableItems])

  return { grabbedRowAnchorRef, uncollapseRowAnchorRef, captureGrabAnchor, captureUncollapseAnchor }
}

function useDragEdgeAutoScroll(
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  draggedIndex: number | null,
) {
  const dragStartClientYRef = useRef<number | null>(null)
  const dragClientYRef = useRef<number | null>(null)

  const recordDragOverClientY = useCallback((clientY: number) => {
    if (dragStartClientYRef.current !== null) {
      if (Math.abs(clientY - dragStartClientYRef.current) < 12) return
      dragStartClientYRef.current = null
    }
    dragClientYRef.current = clientY
  }, [])

  useEffect(() => {
    if (draggedIndex === null || typeof window === 'undefined') {
      dragClientYRef.current = null
      dragStartClientYRef.current = null
      return
    }
    const updateDragClientY = (event: globalThis.DragEvent) => {
      if (event.clientY > 0) recordDragOverClientY(event.clientY)
    }
    window.addEventListener('dragover', updateDragClientY)

    let animationFrameId = 0
    const stepAutoScroll = () => {
      const container = scrollContainerRef.current
      const delta = computeDragAutoScrollDelta(container, dragClientYRef.current)
      if (container && delta !== 0) container.scrollTop = Math.max(0, container.scrollTop + delta)
      animationFrameId = window.requestAnimationFrame(stepAutoScroll)
    }
    animationFrameId = window.requestAnimationFrame(stepAutoScroll)
    return () => {
      window.removeEventListener('dragover', updateDragClientY)
      window.cancelAnimationFrame(animationFrameId)
    }
  }, [draggedIndex, recordDragOverClientY, scrollContainerRef])

  return { dragStartClientYRef, dragClientYRef, recordDragOverClientY }
}

function usePlannerDropHandlers({
  plan,
  isPlanning,
  isCompleted,
  lastVisitedIndex,
  draggedIndex,
  dragOverGapIndex,
  setDragOverGapIndex,
  dropSlotGaps,
  activeDropPreviews,
  scrollContainerRef,
  dragClientYRef,
  recordDragOverClientY,
  finishDropMove,
}: {
  plan: expedition.PlanView | null
  isPlanning: boolean
  isCompleted: boolean
  lastVisitedIndex: number
  draggedIndex: number | null
  dragOverGapIndex: number | null
  setDragOverGapIndex: (gapIndex: number | null) => void
  dropSlotGaps: Set<number>
  activeDropPreviews: expedition.DropSlotPreview[] | null
  scrollContainerRef: RefObject<HTMLDivElement | null>
  dragClientYRef: MutableRefObject<number | null>
  recordDragOverClientY: (clientY: number) => void
  finishDropMove: (from: number, to: number, preview: expedition.DropSlotPreview | null, top: number | null) => void
}) {
  const getRowDropProps = useCallback(
    (rowIndex: number) => ({
      onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
        recordDragOverClientY(event.clientY)
        const resolved = resolveHoverGap(
          rowIndex, event, plan, draggedIndex, isPlanning, isCompleted, lastVisitedIndex, dropSlotGaps, activeDropPreviews,
        )
        if (!resolved) {
          if (dragOverGapIndex !== null) setDragOverGapIndex(null)
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (dragOverGapIndex !== resolved.gapIndex) setDragOverGapIndex(resolved.gapIndex)
      },
      onDrop: (event: DragEvent<HTMLTableRowElement>) => {
        const resolved = resolveHoverGap(
          rowIndex, event, plan, draggedIndex, isPlanning, isCompleted, lastVisitedIndex, dropSlotGaps, activeDropPreviews,
        )
        if (!resolved || draggedIndex === null) return
        event.preventDefault()
        finishDropMove(draggedIndex, resolved.targetIndex, resolved.preview, measureVisualTop(scrollContainerRef.current, event.currentTarget))
      },
    }),
    [activeDropPreviews, dragOverGapIndex, draggedIndex, dropSlotGaps, finishDropMove, isCompleted, isPlanning, lastVisitedIndex, plan, recordDragOverClientY, scrollContainerRef, setDragOverGapIndex],
  )

  const getSlotDropProps = useCallback(
    (gapIndex: number, targetIndex: number, preview: expedition.DropSlotPreview) => {
      const isAllowed = !isCompleted && (preview.status === 'valid_visited' || preview.status === 'valid_unvisited')
      return {
        onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
          recordDragOverClientY(event.clientY)
          if (!isAllowed) {
            if (dragOverGapIndex !== null) setDragOverGapIndex(null)
            return
          }
          event.preventDefault()
          event.dataTransfer.dropEffect = 'move'
          if (dragOverGapIndex !== gapIndex) setDragOverGapIndex(gapIndex)
        },
        onDrop: (event: DragEvent<HTMLTableRowElement>) => {
          if (!isAllowed || draggedIndex === null) return
          event.preventDefault()
          finishDropMove(draggedIndex, targetIndex, preview, measureVisualTop(scrollContainerRef.current, event.currentTarget))
        },
      }
    },
    [dragOverGapIndex, draggedIndex, finishDropMove, isCompleted, recordDragOverClientY, scrollContainerRef, setDragOverGapIndex],
  )

  const scrollContainerDropProps: HTMLAttributes<HTMLDivElement> = useMemo(
    () => ({
      onDragLeave: (event) => {
        const relatedTarget = event.relatedTarget as Node | null
        if (!relatedTarget || !scrollContainerRef.current?.contains(relatedTarget)) dragClientYRef.current = null
      },
      onDragOver: (event) => {
        recordDragOverClientY(event.clientY)
        const tailDrop = resolveContainerTailDrop(
          event, scrollContainerRef.current, plan, draggedIndex, isPlanning, isCompleted, dropSlotGaps, activeDropPreviews,
        )
        if (!tailDrop) {
          const eventTarget = event.target as HTMLElement | null
          if (!eventTarget?.closest('tbody') && dragOverGapIndex !== null) setDragOverGapIndex(null)
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (dragOverGapIndex !== tailDrop.gapIndex) setDragOverGapIndex(tailDrop.gapIndex)
      },
      onDrop: (event) => {
        const tailDrop = resolveContainerTailDrop(
          event, scrollContainerRef.current, plan, draggedIndex, isPlanning, isCompleted, dropSlotGaps, activeDropPreviews,
        )
        if (!tailDrop || draggedIndex === null) return
        event.preventDefault()
        finishDropMove(draggedIndex, tailDrop.targetIndex, tailDrop.preview, tailDrop.dropVisualTop)
      },
    }),
    [activeDropPreviews, dragClientYRef, dragOverGapIndex, draggedIndex, dropSlotGaps, finishDropMove, isCompleted, isPlanning, plan, recordDragOverClientY, scrollContainerRef, setDragOverGapIndex],
  )

  return { getRowDropProps, getSlotDropProps, scrollContainerDropProps }
}

/**
 * usePlannerRowDrag manages hold-to-preview drop bounds, HTML5 row drag-and-drop,
 * scroll anchoring when visited stretches collapse/expand, and viewport auto-scroll.
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
  const [activeDropPreviews, setActiveDropPreviews] = useState<expedition.DropSlotPreview[] | null>(null)

  const holdTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const interactionTokenRef = useRef(0)
  const heldOrDraggedIndex = draggedIndex ?? heldIndex

  useEffect(() => () => clearPendingHoldTimer(holdTimerRef), [])

  const { getImmediatePreviews, ensureDropPreviews } = useCachedRowDropPreviews(
    plan, isPlanning, isCompleted, lockedSpanshTargetRowId,
  )

  const tableItems: PlannerTableItem[] = useMemo(
    () => buildPlannerTableItems(plan, expandedRowIds, isPlanning, heldOrDraggedIndex, lastVisitedIndex, activeDropPreviews),
    [activeDropPreviews, expandedRowIds, heldOrDraggedIndex, isPlanning, lastVisitedIndex, plan],
  )

  const { dropSlotGaps, hasCollapsedRows } = useMemo(() => {
    const gaps = new Set<number>()
    let collapsed = false
    for (const item of tableItems) {
      if (item.kind === 'drop_slot') gaps.add(item.gapIndex)
      else if (item.kind === 'collapsed_visited') collapsed = true
    }
    return { dropSlotGaps: gaps, hasCollapsedRows: collapsed }
  }, [tableItems])

  const { grabbedRowAnchorRef, uncollapseRowAnchorRef, captureGrabAnchor, captureUncollapseAnchor } =
    useDragScrollAnchors(scrollContainerRef, plan, tableItems, heldOrDraggedIndex)

  const { dragStartClientYRef, dragClientYRef, recordDragOverClientY } =
    useDragEdgeAutoScroll(scrollContainerRef, draggedIndex)

  useEffect(() => {
    if (heldIndex === null || draggedIndex !== null || typeof window === 'undefined') return
    const clearHeld = () => {
      interactionTokenRef.current++
      clearPendingHoldTimer(holdTimerRef)
      captureUncollapseAnchor(heldIndex)
      setHeldIndex(null)
      setActiveDropPreviews(null)
    }
    window.addEventListener('pointerup', clearHeld)
    window.addEventListener('blur', clearHeld)
    return () => {
      window.removeEventListener('pointerup', clearHeld)
      window.removeEventListener('blur', clearHeld)
    }
  }, [captureUncollapseAnchor, draggedIndex, heldIndex])

  const finishDropMove = useCallback(
    (fromIndex: number, targetIndex: number, preview: expedition.DropSlotPreview | null, dropVisualTop: number | null) => {
      const draggedRow = plan?.rows?.[fromIndex]?.row
      captureUncollapseAnchor(fromIndex)
      setHeldIndex(null)
      setDraggedIndex(null)
      setDragOverGapIndex(null)
      setActiveDropPreviews(null)
      if (!draggedRow) return
      const movedImmediately = onMoveRow(draggedRow.id, fromIndex, targetIndex, preview)
      if (movedImmediately && dropVisualTop !== null) {
        uncollapseRowAnchorRef.current = { rowId: draggedRow.id, visualTop: dropVisualTop }
      }
    },
    [captureUncollapseAnchor, onMoveRow, plan, uncollapseRowAnchorRef],
  )

  const { getRowDropProps, getSlotDropProps, scrollContainerDropProps } = usePlannerDropHandlers({
    plan, isPlanning, isCompleted, lastVisitedIndex, draggedIndex, dragOverGapIndex,
    setDragOverGapIndex, dropSlotGaps, activeDropPreviews, scrollContainerRef,
    dragClientYRef, recordDragOverClientY, finishDropMove,
  })

  const handleRowHoldStart = useCallback(
    (holdRowIndex: number) => {
      const token = ++interactionTokenRef.current
      const previewsPromise = ensureDropPreviews(plan?.rows?.[holdRowIndex]?.row)
      clearPendingHoldTimer(holdTimerRef)
      holdTimerRef.current = setTimeout(() => {
        holdTimerRef.current = null
        void previewsPromise.then((previews) => {
          if (interactionTokenRef.current !== token) return
          if (previews && previews.length > 0) captureGrabAnchor(holdRowIndex)
          setActiveDropPreviews(previews)
          setHeldIndex(holdRowIndex)
        })
      }, HOLD_PREVIEW_DELAY_MS)
    },
    [captureGrabAnchor, ensureDropPreviews, plan],
  )

  const handleRowHoldEnd = useCallback(() => {
    clearPendingHoldTimer(holdTimerRef)
    if (draggedIndex === null) {
      interactionTokenRef.current++
      captureUncollapseAnchor(heldIndex)
      setHeldIndex(null)
      setActiveDropPreviews(null)
    }
  }, [captureUncollapseAnchor, draggedIndex, heldIndex])

  const handleRowDragStart = useCallback(
    (dragRowIndex: number, dragRowId: number, event: DragEvent) => {
      const token = ++interactionTokenRef.current
      clearPendingHoldTimer(holdTimerRef)
      const targetRow = plan?.rows?.[dragRowIndex]?.row
      const immediatePreviews = getImmediatePreviews(targetRow)
      const previewsPromise = immediatePreviews !== null ? null : ensureDropPreviews(targetRow)
      dragStartClientYRef.current = event.clientY
      dragClientYRef.current = null
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', String(dragRowId))
      window.requestAnimationFrame(() => {
        if (interactionTokenRef.current !== token) return
        const alreadyCollapsed = activeDropPreviews !== null && heldIndex === dragRowIndex
        if (immediatePreviews !== null && immediatePreviews.length > 0 && !alreadyCollapsed) {
          captureGrabAnchor(dragRowIndex)
        }
        if (immediatePreviews !== null) setActiveDropPreviews(immediatePreviews)
        setHeldIndex(dragRowIndex)
        setDraggedIndex(dragRowIndex)
        if (previewsPromise) {
          void previewsPromise.then((previews) => {
            if (interactionTokenRef.current !== token || !previews) return
            if (previews.length > 0) captureGrabAnchor(dragRowIndex)
            setActiveDropPreviews(previews)
          })
        }
      })
    },
    [activeDropPreviews, captureGrabAnchor, dragClientYRef, dragStartClientYRef, ensureDropPreviews, getImmediatePreviews, heldIndex, plan],
  )

  const handleRowDragEnd = useCallback(() => {
    interactionTokenRef.current++
    clearPendingHoldTimer(holdTimerRef)
    grabbedRowAnchorRef.current = null
    dragStartClientYRef.current = null
    dragClientYRef.current = null
    const activeRowIndex = draggedIndex ?? heldIndex
    if (activeRowIndex !== null && uncollapseRowAnchorRef.current === null) captureUncollapseAnchor(activeRowIndex)
    setHeldIndex(null)
    setDraggedIndex(null)
    setDragOverGapIndex(null)
    setActiveDropPreviews(null)
  }, [captureUncollapseAnchor, dragClientYRef, dragStartClientYRef, draggedIndex, grabbedRowAnchorRef, heldIndex, uncollapseRowAnchorRef])

  const handleClearDragOverGap = useCallback(() => {
    if (dragOverGapIndex !== null) setDragOverGapIndex(null)
  }, [dragOverGapIndex])

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
