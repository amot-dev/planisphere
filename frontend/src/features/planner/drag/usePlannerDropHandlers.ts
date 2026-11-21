import {
  useCallback,
  useMemo,
  useRef,
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

type RowDropContextSource =
  | CreateCachedRowDropPropsOptions
  | { readonly current: CreateCachedRowDropPropsOptions }

function isContextRef(
  source: RowDropContextSource,
): source is { readonly current: CreateCachedRowDropPropsOptions } {
  return 'current' in source
}

/**
 * createCachedRowDropPropsGetter returns a row-index lookup that caches
 * drop prop objects across plan updates, drag transitions, and gap-hover changes
 * while reading the latest drag context via ref.
 */
export function createCachedRowDropPropsGetter(
  contextSource: RowDropContextSource,
): (rowIndex: number) => RowDropProps {
  const cachedPropsByRowIndex = new Map<number, RowDropProps>()
  const readContext = (): CreateCachedRowDropPropsOptions =>
    isContextRef(contextSource) ? contextSource.current : contextSource

  return (rowIndex: number): RowDropProps => {
    const cachedRowDropProps = cachedPropsByRowIndex.get(rowIndex)
    if (cachedRowDropProps) return cachedRowDropProps

    const rowDropProps: RowDropProps = {
      onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
        const context = readContext()
        context.recordDragOverClientY(event.clientY)
        const resolved = resolveHoverGap(
          rowIndex,
          event,
          context.plan,
          context.draggedIndex,
          context.isPlanning,
          context.isCompleted,
          context.lastVisitedIndex,
          context.dropSlotGaps,
          context.activeDropPreviews,
        )
        if (!resolved) {
          if (context.dragOverGapIndexRef.current !== null) {
            context.updateDragOverGapIndex(null)
          }
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (context.dragOverGapIndexRef.current !== resolved.gapIndex) {
          context.updateDragOverGapIndex(resolved.gapIndex)
        }
      },
      onDrop: (event: DragEvent<HTMLTableRowElement>) => {
        const context = readContext()
        const resolved = resolveHoverGap(
          rowIndex,
          event,
          context.plan,
          context.draggedIndex,
          context.isPlanning,
          context.isCompleted,
          context.lastVisitedIndex,
          context.dropSlotGaps,
          context.activeDropPreviews,
        )
        if (!resolved || context.draggedIndex === null) return
        event.preventDefault()
        context.finishDropMove(
          context.draggedIndex,
          resolved.targetIndex,
          resolved.preview,
          measureVisualTop(context.scrollContainerRef.current, event.currentTarget),
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

interface SlotDropTargetEntry {
  targetIndex: number
  preview: expedition.DropSlotPreview
}

/**
 * usePlannerDropHandlers provides lifetime-cached per-row, historical drop-slot,
 * and scroll-container tail drop prop builders backed by a live options ref.
 */
export function usePlannerDropHandlers(options: UsePlannerDropHandlersOptions) {
  const latestOptionsRef = useRef(options)
  latestOptionsRef.current = options

  const slotTargetByGapRef = useRef<Map<number, SlotDropTargetEntry>>(new Map())
  const cachedSlotPropsByGapRef = useRef<Map<number, RowDropProps>>(new Map())

  const getRowDropProps = useMemo(
    () => createCachedRowDropPropsGetter(latestOptionsRef),
    [],
  )

  const getSlotDropProps = useCallback(
    (gapIndex: number, targetIndex: number, preview: expedition.DropSlotPreview): RowDropProps => {
      slotTargetByGapRef.current.set(gapIndex, { targetIndex, preview })
      const existingProps = cachedSlotPropsByGapRef.current.get(gapIndex)
      if (existingProps) {
        return existingProps
      }

      const slotDropProps: RowDropProps = {
        onDragOver: (event: DragEvent<HTMLTableRowElement>) => {
          const context = latestOptionsRef.current
          const slotEntry = slotTargetByGapRef.current.get(gapIndex)
          context.recordDragOverClientY(event.clientY)
          const isAllowed =
            Boolean(slotEntry) &&
            !context.isCompleted &&
            (slotEntry!.preview.status === 'valid_visited' ||
              slotEntry!.preview.status === 'valid_unvisited')
          if (!isAllowed) {
            if (context.dragOverGapIndexRef.current !== null) {
              context.updateDragOverGapIndex(null)
            }
            return
          }
          event.preventDefault()
          event.dataTransfer.dropEffect = 'move'
          if (context.dragOverGapIndexRef.current !== gapIndex) {
            context.updateDragOverGapIndex(gapIndex)
          }
        },
        onDrop: (event: DragEvent<HTMLTableRowElement>) => {
          const context = latestOptionsRef.current
          const slotEntry = slotTargetByGapRef.current.get(gapIndex)
          const isAllowed =
            Boolean(slotEntry) &&
            !context.isCompleted &&
            (slotEntry!.preview.status === 'valid_visited' ||
              slotEntry!.preview.status === 'valid_unvisited')
          if (!isAllowed || context.draggedIndex === null || !slotEntry) return
          event.preventDefault()
          context.finishDropMove(
            context.draggedIndex,
            slotEntry.targetIndex,
            slotEntry.preview,
            measureVisualTop(context.scrollContainerRef.current, event.currentTarget),
          )
        },
      }

      cachedSlotPropsByGapRef.current.set(gapIndex, slotDropProps)
      return slotDropProps
    },
    [],
  )

  const scrollContainerDropProps: HTMLAttributes<HTMLDivElement> = useMemo(
    () => ({
      onDragLeave: (event) => {
        const context = latestOptionsRef.current
        const relatedTarget = event.relatedTarget as Node | null
        if (!relatedTarget || !context.scrollContainerRef.current?.contains(relatedTarget)) {
          context.dragClientYRef.current = null
        }
      },
      onDragOver: (event) => {
        const context = latestOptionsRef.current
        context.recordDragOverClientY(event.clientY)
        const tailDrop = resolveContainerTailDrop(
          event,
          context.scrollContainerRef.current,
          context.plan,
          context.draggedIndex,
          context.isPlanning,
          context.isCompleted,
          context.dropSlotGaps,
          context.activeDropPreviews,
        )
        if (!tailDrop) {
          const eventTarget = event.target as HTMLElement | null
          if (!eventTarget?.closest('tbody') && context.dragOverGapIndexRef.current !== null) {
            context.updateDragOverGapIndex(null)
          }
          return
        }
        event.preventDefault()
        event.dataTransfer.dropEffect = 'move'
        if (context.dragOverGapIndexRef.current !== tailDrop.gapIndex) {
          context.updateDragOverGapIndex(tailDrop.gapIndex)
        }
      },
      onDrop: (event) => {
        const context = latestOptionsRef.current
        const tailDrop = resolveContainerTailDrop(
          event,
          context.scrollContainerRef.current,
          context.plan,
          context.draggedIndex,
          context.isPlanning,
          context.isCompleted,
          context.dropSlotGaps,
          context.activeDropPreviews,
        )
        if (!tailDrop || context.draggedIndex === null) return
        event.preventDefault()
        context.finishDropMove(
          context.draggedIndex,
          tailDrop.targetIndex,
          tailDrop.preview,
          tailDrop.dropVisualTop,
        )
      },
    }),
    [],
  )

  return { getRowDropProps, getSlotDropProps, scrollContainerDropProps }
}
