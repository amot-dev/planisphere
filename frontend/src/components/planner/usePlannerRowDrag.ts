import {
  useCallback,
  useEffect,
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
import { useIsomorphicLayoutEffect } from './useInitialExpeditionScroll'

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

/**
 * resolveActiveDragRowIndex returns the current row index matching grabbedRowId, or -1.
 */
export function resolveActiveDragRowIndex(
  rows: readonly (expedition.RowView | null | undefined)[] | null | undefined,
  grabbedRowId: number | null,
): number {
  if (!rows || grabbedRowId === null) return -1
  return rows.findIndex((rowView) => rowView?.row?.id === grabbedRowId)
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

const EMPTY_DROP_SLOT_GAPS: Set<number> = new Set()

/**
 * RowDropProps defines the row-level drag-over and drop handlers attached to
 * table rows during drag reordering.
 */
export interface RowDropProps {
  onDragOver: (event: DragEvent<HTMLTableRowElement>) => void
  onDrop: (event: DragEvent<HTMLTableRowElement>) => void
}

/**
 * CreateCachedRowDropPropsOptions configures the drag context and ref accessors
 * used to build cached per-row drop handlers.
 */
export interface CreateCachedRowDropPropsOptions {
  plan: expedition.PlanView | null
  isPlanning: boolean
  isCompleted: boolean
  lastVisitedIndex: number
  draggedIndex: number | null
  dragOverGapIndexRef: MutableRefObject<number | null>
  updateDragOverGapIndex: (gapIndex: number | null) => void
  dropSlotGaps: Set<number>
  activeDropPreviews: expedition.DropSlotPreview[] | null
  scrollContainerRef: RefObject<HTMLDivElement | null>
  recordDragOverClientY: (clientY: number) => void
  finishDropMove: (
    fromIndex: number,
    targetIndex: number,
    preview: expedition.DropSlotPreview | null,
    dropVisualTop: number | null,
  ) => void
}

/**
 * stepDragAutoScroll applies one frame of vertical edge auto-scroll and returns
 * true when scrolling occurred so idle animation frames stop scheduling.
 */
export function stepDragAutoScroll(
  container: HTMLElement | null,
  clientY: number | null,
): boolean {
  const scrollDelta = computeDragAutoScrollDelta(container, clientY)
  if (!container || scrollDelta === 0) return false
  container.scrollTop = Math.max(0, container.scrollTop + scrollDelta)
  return true
}

/**
 * createCachedRowDropPropsGetter returns a row-index lookup that caches
 * drop prop objects across gap-hover updates while reading live gap state via ref.
 */
export function createCachedRowDropPropsGetter({
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
  recordDragOverClientY,
  finishDropMove,
}: CreateCachedRowDropPropsOptions): (rowIndex: number) => RowDropProps {
  const cachedPropsByRowIndex = new Map<number, RowDropProps>()

  return (rowIndex: number): RowDropProps => {
    const cachedRowDropProps = cachedPropsByRowIndex.get(rowIndex)
    if (cachedRowDropProps) return cachedRowDropProps

    const rowDropProps: RowDropProps = {
      onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
        recordDragOverClientY(event.clientY)
        const resolved = resolveHoverGap(
          rowIndex,
          event,
          plan,
          draggedIndex,
          isPlanning,
          isCompleted,
          lastVisitedIndex,
          dropSlotGaps,
          activeDropPreviews,
        )
        if (!resolved) {
          if (dragOverGapIndexRef.current !== null) updateDragOverGapIndex(null)
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (dragOverGapIndexRef.current !== resolved.gapIndex) {
          updateDragOverGapIndex(resolved.gapIndex)
        }
      },
      onDrop: (event: DragEvent<HTMLTableRowElement>) => {
        const resolved = resolveHoverGap(
          rowIndex,
          event,
          plan,
          draggedIndex,
          isPlanning,
          isCompleted,
          lastVisitedIndex,
          dropSlotGaps,
          activeDropPreviews,
        )
        if (!resolved || draggedIndex === null) return
        event.preventDefault()
        finishDropMove(
          draggedIndex,
          resolved.targetIndex,
          resolved.preview,
          measureVisualTop(scrollContainerRef.current, event.currentTarget),
        )
      },
    }

    cachedPropsByRowIndex.set(rowIndex, rowDropProps)
    return rowDropProps
  }
}

function useDragEdgeAutoScroll(
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  draggedIndex: number | null,
) {
  const dragStartClientYRef = useRef<number | null>(null)
  const dragClientYRef = useRef<number | null>(null)
  const wakeAutoScrollRef = useRef<(() => void) | null>(null)
  const isDragging = draggedIndex !== null

  const recordDragOverClientY = useCallback((clientY: number) => {
    if (dragStartClientYRef.current !== null) {
      // Ignore sub-12px pointer jitter near the grab point so dragging a row
      // near a viewport edge does not engage auto-scroll before actual travel.
      if (Math.abs(clientY - dragStartClientYRef.current) < 12) return
      dragStartClientYRef.current = null
    }
    if (dragClientYRef.current === clientY) return
    dragClientYRef.current = clientY
    wakeAutoScrollRef.current?.()
  }, [])

  useEffect(() => {
    if (!isDragging || typeof window === 'undefined') {
      dragClientYRef.current = null
      dragStartClientYRef.current = null
      wakeAutoScrollRef.current = null
      return
    }
    let animationFrameId = 0
    const stepAutoScroll = () => {
      animationFrameId = 0
      if (stepDragAutoScroll(scrollContainerRef.current, dragClientYRef.current)) {
        animationFrameId = window.requestAnimationFrame(stepAutoScroll)
      }
    }
    const wakeAutoScroll = () => {
      if (animationFrameId === 0) {
        animationFrameId = window.requestAnimationFrame(stepAutoScroll)
      }
    }
    wakeAutoScrollRef.current = wakeAutoScroll

    const updateDragClientY = (event: globalThis.DragEvent) => {
      if (event.clientY > 0) recordDragOverClientY(event.clientY)
    }
    window.addEventListener('dragover', updateDragClientY)

    if (dragClientYRef.current !== null) {
      wakeAutoScroll()
    }

    return () => {
      wakeAutoScrollRef.current = null
      window.removeEventListener('dragover', updateDragClientY)
      if (animationFrameId !== 0) {
        window.cancelAnimationFrame(animationFrameId)
      }
    }
  }, [isDragging, recordDragOverClientY, scrollContainerRef])

  return { dragStartClientYRef, dragClientYRef, recordDragOverClientY }
}

interface UsePlannerDropHandlersOptions extends CreateCachedRowDropPropsOptions {
  dragClientYRef: MutableRefObject<number | null>
}

function usePlannerDropHandlers(options: UsePlannerDropHandlersOptions) {
  const {
    plan, isPlanning, isCompleted, lastVisitedIndex, draggedIndex, dragOverGapIndexRef,
    updateDragOverGapIndex, dropSlotGaps, activeDropPreviews, scrollContainerRef,
    dragClientYRef, recordDragOverClientY, finishDropMove,
  } = options

  const getRowDropProps = useMemo(
    () =>
      createCachedRowDropPropsGetter({
        plan, isPlanning, isCompleted, lastVisitedIndex, draggedIndex, dragOverGapIndexRef,
        updateDragOverGapIndex, dropSlotGaps, activeDropPreviews, scrollContainerRef,
        recordDragOverClientY, finishDropMove,
      }),
    [
      activeDropPreviews, dragOverGapIndexRef, draggedIndex, dropSlotGaps, finishDropMove,
      isCompleted, isPlanning, lastVisitedIndex, plan, recordDragOverClientY,
      scrollContainerRef, updateDragOverGapIndex,
    ],
  )

  const getSlotDropProps = useCallback(
    (gapIndex: number, targetIndex: number, preview: expedition.DropSlotPreview) => {
      const isAllowed = !isCompleted && (preview.status === 'valid_visited' || preview.status === 'valid_unvisited')
      return {
        onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
          recordDragOverClientY(event.clientY)
          if (!isAllowed) {
            if (dragOverGapIndexRef.current !== null) updateDragOverGapIndex(null)
            return
          }
          event.preventDefault()
          event.dataTransfer.dropEffect = 'move'
          if (dragOverGapIndexRef.current !== gapIndex) updateDragOverGapIndex(gapIndex)
        },
        onDrop: (event: DragEvent<HTMLTableRowElement>) => {
          if (!isAllowed || draggedIndex === null) return
          event.preventDefault()
          finishDropMove(draggedIndex, targetIndex, preview, measureVisualTop(scrollContainerRef.current, event.currentTarget))
        },
      }
    },
    [dragOverGapIndexRef, draggedIndex, finishDropMove, isCompleted, recordDragOverClientY, scrollContainerRef, updateDragOverGapIndex],
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
          if (!eventTarget?.closest('tbody') && dragOverGapIndexRef.current !== null) updateDragOverGapIndex(null)
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (dragOverGapIndexRef.current !== tailDrop.gapIndex) updateDragOverGapIndex(tailDrop.gapIndex)
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
    [activeDropPreviews, dragClientYRef, dragOverGapIndexRef, draggedIndex, dropSlotGaps, finishDropMove, isCompleted, isPlanning, plan, recordDragOverClientY, scrollContainerRef, updateDragOverGapIndex],
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
    plan, isPlanning, isCompleted, lockedSpanshTargetRowId,
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
      if (interactionTokenRef.current === token && previews !== null) setActiveDropPreviews(previews)
    })
  }, [draggedIndex, ensureDropPreviews, heldIndex, heldOrDraggedIndex, isCompleted, isPlanning, lockedSpanshTargetRowId, plan, resetDragState])

  const tableItems: PlannerTableItem[] = useMemo(
    () => buildPlannerTableItems(plan, expandedRowIds, isPlanning, heldOrDraggedIndex, lastVisitedIndex, activeDropPreviews),
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
    (fromIndex: number, targetIndex: number, preview: expedition.DropSlotPreview | null, dropVisualTop: number | null) => {
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
    plan, isPlanning, isCompleted, lastVisitedIndex, draggedIndex, dragOverGapIndexRef,
    updateDragOverGapIndex, dropSlotGaps, activeDropPreviews, scrollContainerRef,
    dragClientYRef, recordDragOverClientY, finishDropMove,
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
    [activeDropPreviews, captureGrabAnchor, dragClientYRef, dragStartClientYRef, ensureDropPreviews, getImmediatePreviews, heldIndex, lockedSpanshTargetRowId, plan],
  )

  const handleRowDragEnd = useCallback(() => {
    grabbedRowAnchorRef.current = null
    dragStartClientYRef.current = null
    dragClientYRef.current = null
    const activeRowIndex = draggedIndex ?? heldIndex
    if (activeRowIndex !== null && uncollapseRowAnchorRef.current === null) captureUncollapseAnchor(activeRowIndex)
    resetDragState()
  }, [captureUncollapseAnchor, dragClientYRef, dragStartClientYRef, draggedIndex, grabbedRowAnchorRef, heldIndex, resetDragState, uncollapseRowAnchorRef])

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
