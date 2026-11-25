import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react'

/**
 * PlannerScrollTarget identifies a waypoint row jump requested from waypoint
 * search so the planner centers that row instead of the default open position.
 */
export interface PlannerScrollTarget {
  expeditionId: number
  rowId: number
  nonce: number
}

/**
 * useIsomorphicLayoutEffect selects useLayoutEffect in browser environments and
 * falls back to useEffect when window is unavailable.
 */
export const useIsomorphicLayoutEffect = typeof window !== 'undefined' ? useLayoutEffect : useEffect

function centerRowInScrollContainer(
  scrollContainer: HTMLDivElement,
  targetRow: HTMLElement,
): void {
  if (typeof targetRow.scrollIntoView === 'function') {
    targetRow.scrollIntoView({ block: 'center', behavior: 'instant' })
  }
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
}

/**
 * useInitialExpeditionScroll positions the planner scroll container on initial
 * expedition open, stage change, or targeted waypoint jump (bottom for planning,
 * centered on last visited row for active, top for completed, or centered on a
 * requested search jump target).
 */
export function useInitialExpeditionScroll(
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  hasExpedition: boolean,
  expeditionId: number | undefined,
  status: string | undefined,
  lastVisitedIndex: number,
  scrollTarget?: PlannerScrollTarget | null,
  onScrollTargetApplied?: (rowId: number) => void,
  onScrollTargetMissed?: () => void,
) {
  const lastOpenedKeyRef = useRef<string | null>(null)
  const lastAppliedScrollNonceRef = useRef<number | null>(null)
  const pendingAnimationFrameRef = useRef<number | null>(null)
  const onScrollTargetAppliedRef = useRef(onScrollTargetApplied)
  onScrollTargetAppliedRef.current = onScrollTargetApplied
  const onScrollTargetMissedRef = useRef(onScrollTargetMissed)
  onScrollTargetMissedRef.current = onScrollTargetMissed

  useEffect(() => {
    return () => {
      if (pendingAnimationFrameRef.current !== null) {
        cancelAnimationFrame(pendingAnimationFrameRef.current)
        pendingAnimationFrameRef.current = null
      }
    }
  }, [])

  useIsomorphicLayoutEffect(() => {
    if (!hasExpedition) return
    const openKey = `${expeditionId}:${status}`
    const scheduleAdjustmentFrame = (callback: () => void) => {
      if (pendingAnimationFrameRef.current !== null) {
        cancelAnimationFrame(pendingAnimationFrameRef.current)
      }
      pendingAnimationFrameRef.current = requestAnimationFrame(() => {
        pendingAnimationFrameRef.current = null
        callback()
      })
    }

    const applyDefaultScroll = () => {
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
          centerRowInScrollContainer(scrollContainer, targetRow)
        } else {
          scrollContainer.scrollTop = 0
        }
        return true
      }
      if (status === 'completed') {
        scrollContainer.scrollTop = 0
        return true
      }
      return false
    }

    if (
      scrollTarget &&
      scrollTarget.expeditionId === expeditionId &&
      scrollTarget.nonce !== lastAppliedScrollNonceRef.current
    ) {
      const { rowId: targetRowId, nonce: targetNonce } = scrollTarget
      const applyTargetedScroll = () => {
        const scrollContainer = scrollContainerRef.current
        if (!scrollContainer || scrollContainer.clientHeight === 0) return false
        const targetRow = scrollContainer.querySelector<HTMLElement>(
          `tr[data-row-id="${targetRowId}"]`,
        )
        if (!targetRow) return false
        centerRowInScrollContainer(scrollContainer, targetRow)
        return true
      }

      if (applyTargetedScroll()) {
        lastOpenedKeyRef.current = openKey
        lastAppliedScrollNonceRef.current = targetNonce
        onScrollTargetAppliedRef.current?.(targetRowId)
      }

      // Keep next-frame micro-adjustment alive even after scrollTarget clears to null
      // so content-visibility: auto reflow does not drift the target row off-center,
      // and recover cleanly when a targeted row no longer exists.
      scheduleAdjustmentFrame(() => {
        if (applyTargetedScroll()) {
          lastOpenedKeyRef.current = openKey
          if (lastAppliedScrollNonceRef.current !== targetNonce) {
            lastAppliedScrollNonceRef.current = targetNonce
            onScrollTargetAppliedRef.current?.(targetRowId)
          }
          return
        }
        const scrollContainer = scrollContainerRef.current
        if (
          lastAppliedScrollNonceRef.current !== targetNonce &&
          scrollContainer &&
          scrollContainer.clientHeight > 0
        ) {
          lastAppliedScrollNonceRef.current = targetNonce
          onScrollTargetMissedRef.current?.()
          if (lastOpenedKeyRef.current !== openKey && applyDefaultScroll()) {
            lastOpenedKeyRef.current = openKey
          }
        }
      })
      return
    }

    if (lastOpenedKeyRef.current === openKey) return

    if (applyDefaultScroll()) {
      lastOpenedKeyRef.current = openKey
    }

    // Micro-adjustment on the next animation frame in case content-visibility: auto,
    // table layout reflow, or initial container measurement adjusted row heights
    scheduleAdjustmentFrame(() => {
      if (applyDefaultScroll()) {
        lastOpenedKeyRef.current = openKey
      }
    })
  }, [expeditionId, hasExpedition, lastVisitedIndex, scrollContainerRef, scrollTarget, status])
}

