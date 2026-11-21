import {
  useCallback,
  useMemo,
  type DragEvent,
  type HTMLAttributes,
  type MutableRefObject,
  type RefObject,
} from 'react'
import { expedition } from '../../../api/models'
import {
  measureVisualTop,
  resolveContainerTailDrop,
  resolveHoverGap,
} from './plannerDragDom'

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

/**
 * UsePlannerDropHandlersOptions extends row drop context with live pointer Y
 * tracking for container-level tail drops.
 */
export interface UsePlannerDropHandlersOptions extends CreateCachedRowDropPropsOptions {
  dragClientYRef: MutableRefObject<number | null>
}

/**
 * usePlannerDropHandlers provides cached per-row, historical drop-slot, and
 * scroll-container tail drop prop builders for the planner table.
 */
export function usePlannerDropHandlers(options: UsePlannerDropHandlersOptions) {
  const {
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
  } = options

  const getRowDropProps = useMemo(
    () =>
      createCachedRowDropPropsGetter({
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
      }),
    [
      activeDropPreviews,
      dragOverGapIndexRef,
      draggedIndex,
      dropSlotGaps,
      finishDropMove,
      isCompleted,
      isPlanning,
      lastVisitedIndex,
      plan,
      recordDragOverClientY,
      scrollContainerRef,
      updateDragOverGapIndex,
    ],
  )

  const getSlotDropProps = useCallback(
    (gapIndex: number, targetIndex: number, preview: expedition.DropSlotPreview) => {
      const isAllowed =
        !isCompleted && (preview.status === 'valid_visited' || preview.status === 'valid_unvisited')
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
          finishDropMove(
            draggedIndex,
            targetIndex,
            preview,
            measureVisualTop(scrollContainerRef.current, event.currentTarget),
          )
        },
      }
    },
    [
      dragOverGapIndexRef,
      draggedIndex,
      finishDropMove,
      isCompleted,
      recordDragOverClientY,
      scrollContainerRef,
      updateDragOverGapIndex,
    ],
  )

  const scrollContainerDropProps: HTMLAttributes<HTMLDivElement> = useMemo(
    () => ({
      onDragLeave: (event) => {
        const relatedTarget = event.relatedTarget as Node | null
        if (!relatedTarget || !scrollContainerRef.current?.contains(relatedTarget)) {
          dragClientYRef.current = null
        }
      },
      onDragOver: (event) => {
        recordDragOverClientY(event.clientY)
        const tailDrop = resolveContainerTailDrop(
          event,
          scrollContainerRef.current,
          plan,
          draggedIndex,
          isPlanning,
          isCompleted,
          dropSlotGaps,
          activeDropPreviews,
        )
        if (!tailDrop) {
          const eventTarget = event.target as HTMLElement | null
          if (!eventTarget?.closest('tbody') && dragOverGapIndexRef.current !== null) {
            updateDragOverGapIndex(null)
          }
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (dragOverGapIndexRef.current !== tailDrop.gapIndex) {
          updateDragOverGapIndex(tailDrop.gapIndex)
        }
      },
      onDrop: (event) => {
        const tailDrop = resolveContainerTailDrop(
          event,
          scrollContainerRef.current,
          plan,
          draggedIndex,
          isPlanning,
          isCompleted,
          dropSlotGaps,
          activeDropPreviews,
        )
        if (!tailDrop || draggedIndex === null) return
        event.preventDefault()
        finishDropMove(draggedIndex, tailDrop.targetIndex, tailDrop.preview, tailDrop.dropVisualTop)
      },
    }),
    [
      activeDropPreviews,
      dragClientYRef,
      dragOverGapIndexRef,
      draggedIndex,
      dropSlotGaps,
      finishDropMove,
      isCompleted,
      isPlanning,
      plan,
      recordDragOverClientY,
      scrollContainerRef,
      updateDragOverGapIndex,
    ],
  )

  return { getRowDropProps, getSlotDropProps, scrollContainerDropProps }
}

