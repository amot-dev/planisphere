import { useCallback, useEffect, useRef, type RefObject } from 'react'
import { expedition } from '../../api/models'
import {
  alignContainerToRowSelector,
  computeDragAutoScrollDelta,
  measureVisualTop,
} from './plannerDragHelpers'
import type { PlannerTableItem } from './planTableItems'
import { useIsomorphicLayoutEffect } from './useInitialExpeditionScroll'

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
 * useDragScrollAnchors preserves the grabbed row's viewport vertical position
 * across visited-row collapse and uncollapse transitions.
 */
export function useDragScrollAnchors(
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
      const frameId = window.requestAnimationFrame(() =>
        alignContainerToRowSelector(container, selector, visualTop),
      )
      return () => window.cancelAnimationFrame(frameId)
    }
    if (heldOrDraggedIndex === null && uncollapseRowAnchorRef.current !== null) {
      const { rowId, visualTop } = uncollapseRowAnchorRef.current
      const selector = `tr[data-row-id="${rowId}"]`
      uncollapseRowAnchorRef.current = null
      alignContainerToRowSelector(container, selector, visualTop)
      const frameId = window.requestAnimationFrame(() =>
        alignContainerToRowSelector(container, selector, visualTop),
      )
      return () => window.cancelAnimationFrame(frameId)
    }
  }, [heldOrDraggedIndex, plan, scrollContainerRef, tableItems])

  return { grabbedRowAnchorRef, uncollapseRowAnchorRef, captureGrabAnchor, captureUncollapseAnchor }
}

/**
 * useDragEdgeAutoScroll drives requestAnimationFrame vertical viewport
 * scrolling while dragging a row near the top or bottom scroll edges.
 */
export function useDragEdgeAutoScroll(
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

