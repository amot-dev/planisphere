import type { DragEvent, MutableRefObject } from 'react'
import { expedition } from '../../../api/models'
import { gapToTargetIndex } from '../../../domain/planTableItems'

const DRAG_AUTOSCROLL_ZONE_PIXELS = 72
const DRAG_AUTOSCROLL_MIN_SPEED = 2
const DRAG_AUTOSCROLL_MAX_SPEED = 20
const STICKY_TABLE_HEADER_PIXELS = 36
const BOTTOM_CONTENT_BREATHING_PIXELS = 24

/**
 * HoverGapResolution describes the table gap index, post-move target index,
 * and drop-slot preview resolved under the user's pointer during row drag.
 */
export interface HoverGapResolution {
  gapIndex: number
  targetIndex: number
  preview: expedition.DropSlotPreview | null
}

/**
 * clearPendingHoldTimer cancels and clears an active hold-to-preview timer ref.
 */
export function clearPendingHoldTimer(
  timerRef: MutableRefObject<ReturnType<typeof setTimeout> | null>,
): void {
  if (timerRef.current) {
    clearTimeout(timerRef.current)
    timerRef.current = null
  }
}

/**
 * measureVisualTop computes an element's top offset relative to its scroll container.
 */
export function measureVisualTop(
  container: HTMLElement | null,
  element: HTMLElement | null,
): number | null {
  if (!container || !element) return null
  return element.getBoundingClientRect().top - container.getBoundingClientRect().top
}

function canAutoScrollDown(container: HTMLElement): boolean {
  const bottomSpacer = container.querySelector<HTMLElement>('[data-planner-bottom-spacer="true"]')
  if (!bottomSpacer) {
    return container.scrollTop + container.clientHeight < container.scrollHeight - 1
  }
  const containerBottom = container.getBoundingClientRect().bottom
  const contentBottom = bottomSpacer.getBoundingClientRect().top + BOTTOM_CONTENT_BREATHING_PIXELS
  return contentBottom > containerBottom + 1
}

/**
 * alignContainerToRowSelector adjusts container.scrollTop so the matching row
 * sits at targetVisualTop relative to the scroll container.
 */
export function alignContainerToRowSelector(
  container: HTMLElement,
  selector: string,
  targetVisualTop: number,
): void {
  const rowElement = container.querySelector<HTMLElement>(selector)
  const currentVisualTop = measureVisualTop(container, rowElement)
  if (currentVisualTop === null) return
  const deltaY = currentVisualTop - targetVisualTop
  if (Math.abs(deltaY) > 0.5) {
    container.scrollTop = Math.max(0, Math.round(container.scrollTop + deltaY))
  }
}

/**
 * computeDragAutoScrollDelta returns the per-frame vertical scroll delta when
 * the pointer enters the top or bottom auto-scroll bands of container.
 */
export function computeDragAutoScrollDelta(
  container: HTMLElement | null,
  clientY: number | null,
): number {
  if (!container || clientY === null) return 0
  const rect = container.getBoundingClientRect()
  const topZoneBottom = rect.top + STICKY_TABLE_HEADER_PIXELS + DRAG_AUTOSCROLL_ZONE_PIXELS
  const bottomZoneTop = rect.bottom - DRAG_AUTOSCROLL_ZONE_PIXELS
  if (clientY >= rect.top - 32 && clientY < topZoneBottom && container.scrollTop > 0) {
    const rawDepth =
      (topZoneBottom - clientY) / (DRAG_AUTOSCROLL_ZONE_PIXELS + STICKY_TABLE_HEADER_PIXELS)
    const clampedDepth = Math.max(0, Math.min(1, rawDepth))
    return -(
      DRAG_AUTOSCROLL_MIN_SPEED +
      (DRAG_AUTOSCROLL_MAX_SPEED - DRAG_AUTOSCROLL_MIN_SPEED) * clampedDepth * clampedDepth
    )
  }
  if (clientY > bottomZoneTop && clientY <= rect.bottom + 32 && canAutoScrollDown(container)) {
    const rawDepth = (clientY - bottomZoneTop) / DRAG_AUTOSCROLL_ZONE_PIXELS
    const clampedDepth = Math.max(0, Math.min(1, rawDepth))
    return (
      DRAG_AUTOSCROLL_MIN_SPEED +
      (DRAG_AUTOSCROLL_MAX_SPEED - DRAG_AUTOSCROLL_MIN_SPEED) * clampedDepth * clampedDepth
    )
  }
  return 0
}

/**
 * resolveHoverGap resolves the insertion gap when dragging over a visible table row.
 */
export function resolveHoverGap(
  rowIndex: number,
  event: DragEvent<HTMLTableRowElement>,
  plan: expedition.PlanView | null,
  draggedIndex: number | null,
  isPlanning: boolean,
  isCompleted: boolean,
  lastVisitedIndex: number,
  dropSlotGaps: Set<number>,
  activeDropPreviews: expedition.DropSlotPreview[] | null,
): HoverGapResolution | null {
  if (isCompleted || draggedIndex === null || !plan?.rows) return null
  const draggedRow = plan.rows[draggedIndex]?.row
  if (!draggedRow) return null
  const isDraggingWaypointInActive = !isPlanning && draggedRow.rowType === 'waypoint'
  if (isDraggingWaypointInActive && rowIndex <= lastVisitedIndex) return null

  const rect = event.currentTarget.getBoundingClientRect()
  const isTopHalf = event.clientY < rect.top + rect.height / 2
  let candidateGapIndex = isTopHalf ? rowIndex : rowIndex + 1
  let targetIndex = gapToTargetIndex(draggedIndex, candidateGapIndex)
  if (targetIndex === null) {
    candidateGapIndex = isTopHalf ? rowIndex + 1 : rowIndex
    targetIndex = gapToTargetIndex(draggedIndex, candidateGapIndex)
    if (targetIndex === null) return null
  }

  if (isDraggingWaypointInActive) {
    if (candidateGapIndex <= lastVisitedIndex || dropSlotGaps.has(candidateGapIndex)) return null
    const finalPreview = activeDropPreviews?.[targetIndex] ?? null
    if (!finalPreview || finalPreview.status !== 'valid_unvisited') return null
    return { gapIndex: candidateGapIndex, targetIndex, preview: finalPreview }
  }
  return { gapIndex: candidateGapIndex, targetIndex, preview: null }
}

/**
 * resolveContainerTailDrop resolves dropping into the open region below tbody
 * as appending to the end of the route when valid.
 */
export function resolveContainerTailDrop(
  event: DragEvent<HTMLDivElement>,
  container: HTMLDivElement | null,
  plan: expedition.PlanView | null,
  draggedIndex: number | null,
  isPlanning: boolean,
  isCompleted: boolean,
  dropSlotGaps: Set<number>,
  activeDropPreviews: expedition.DropSlotPreview[] | null,
): (HoverGapResolution & { dropVisualTop: number }) | null {
  if (isCompleted || draggedIndex === null || !plan?.rows?.length) return null
  const eventTarget = event.target as HTMLElement | null
  if (eventTarget?.closest('tbody')) return null
  const tbodyElement = container?.querySelector('tbody') ?? null
  if (!container || !tbodyElement || event.clientY < tbodyElement.getBoundingClientRect().bottom) {
    return null
  }
  const tailGapIndex = plan.rows.length
  if (dropSlotGaps.has(tailGapIndex)) return null
  const tailTargetIndex = gapToTargetIndex(draggedIndex, tailGapIndex)
  if (tailTargetIndex === null) return null
  const draggedRow = plan.rows[draggedIndex]?.row
  let tailPreview: expedition.DropSlotPreview | null = null
  if (!isPlanning && draggedRow?.rowType === 'waypoint') {
    tailPreview = activeDropPreviews?.[tailTargetIndex] ?? null
    if (!tailPreview || tailPreview.status !== 'valid_unvisited') return null
  }
  const dropVisualTop = tbodyElement.getBoundingClientRect().bottom - container.getBoundingClientRect().top
  return { gapIndex: tailGapIndex, targetIndex: tailTargetIndex, preview: tailPreview, dropVisualTop }
}

