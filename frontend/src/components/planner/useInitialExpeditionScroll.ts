import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react'

const useIsomorphicLayoutEffect = typeof window !== 'undefined' ? useLayoutEffect : useEffect

/**
 * useInitialExpeditionScroll positions the planner scroll container on initial
 * expedition open or expedition stage change based on expedition status (bottom
 * for planning, centered on last visited row for active, top for completed).
 */
export function useInitialExpeditionScroll(
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  hasExpedition: boolean,
  expeditionId: number | undefined,
  status: string | undefined,
  lastVisitedIndex: number,
) {
  const lastOpenedKeyRef = useRef<string | null>(null)

  useIsomorphicLayoutEffect(() => {
    if (!hasExpedition) return
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

    if (applyScroll()) {
      lastOpenedKeyRef.current = openKey
    }

    // Micro-adjustment on the next animation frame in case content-visibility: auto,
    // table layout reflow, or initial container measurement adjusted row heights
    const animationFrameId = requestAnimationFrame(() => {
      if (applyScroll()) {
        lastOpenedKeyRef.current = openKey
      }
    })

    return () => {
      cancelAnimationFrame(animationFrameId)
    }
  }, [expeditionId, status, lastVisitedIndex, scrollContainerRef])
}

