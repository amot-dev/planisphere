/**
 * Normalizes scroll speed across WebKitGTK on Linux to match native desktop applications
 * (Chrome, Firefox, GNOME, VS Code).
 *
 * In WebKitGTK on Linux, discrete mouse wheel deltas are dampened to ~20-33px per notch,
 * requiring 4-5x more wheel turns than native applications. This normalizer
 * intercepts discrete wheel notches on scrollable containers and scales them to the standard
 * desktop rate (~90-110px per wheel step).
 *
 * Performance considerations:
 * - Precision touchpads (smooth non-integer or low pixel deltas) pass through natively without
 *   interception, preserving hardware compositor momentum and 60-144 FPS smoothness.
 * - Fast path uses classList / selector lookups to find scroll containers, completely eliminating
 *   repeated window.getComputedStyle() calls and synchronous layout reflows during scrolling.
 * - Target scrollable element is cached during active scrolling gestures.
 */

function isHTMLElement(candidate: any): candidate is HTMLElement {
  if (!candidate || typeof candidate !== 'object') return false
  if (typeof HTMLElement !== 'undefined') {
    return candidate instanceof HTMLElement
  }
  return 'classList' in candidate && 'scrollHeight' in candidate
}

function isNode(candidate: any): candidate is Node {
  if (!candidate || typeof candidate !== 'object') return false
  if (typeof Node !== 'undefined') {
    return candidate instanceof Node
  }
  return 'nodeType' in candidate || 'parentElement' in candidate || 'classList' in candidate
}

export function findScrollable(
  start: EventTarget | null,
  deltaY: number,
  deltaX: number,
): HTMLElement | null {
  const hasDoc = typeof document !== 'undefined'
  if (!start) return null
  const initialElement: HTMLElement | null =
    isHTMLElement(start)
      ? start
      : isNode(start) && isHTMLElement((start as any).parentElement)
        ? (start as any).parentElement
        : null
  if (!initialElement || (hasDoc && (initialElement === document.body || initialElement === document.documentElement))) {
    return null
  }

  // Fast path: find closest declared scrollable ancestor without getComputedStyle
  let currentElement: HTMLElement | null = initialElement
  while (currentElement && (!hasDoc || (currentElement !== document.body && currentElement !== document.documentElement))) {
    const classList = currentElement.classList
    const isDeclaredScrollable = Boolean(
      classList &&
      (classList.contains('overflow-y-auto') ||
        classList.contains('overflow-auto') ||
        classList.contains('overflow-y-scroll') ||
        classList.contains('overflow-x-auto') ||
        classList.contains('overflow-x-scroll') ||
        (currentElement.hasAttribute && currentElement.hasAttribute('data-scrollable')))
    )

    if (isDeclaredScrollable) {
      const canScrollY =
        (deltaY > 0 && currentElement.scrollTop + currentElement.clientHeight < currentElement.scrollHeight - 1) ||
        (deltaY < 0 && currentElement.scrollTop > 0)
      const canScrollX =
        (deltaX > 0 && currentElement.scrollLeft + currentElement.clientWidth < currentElement.scrollWidth - 1) ||
        (deltaX < 0 && currentElement.scrollLeft > 0)

      if (canScrollY || canScrollX) {
        return currentElement
      }
    }

    currentElement = currentElement.parentElement
  }

  // Fallback path: check computed style only if no declared scroll class was found
  if (typeof window !== 'undefined' && typeof window.getComputedStyle === 'function') {
    currentElement = initialElement
    while (currentElement && (!hasDoc || (currentElement !== document.body && currentElement !== document.documentElement))) {
      const style = window.getComputedStyle(currentElement)
      const overflowY = style.overflowY
      const overflowX = style.overflowX

      const hasScrollY =
        (overflowY === 'auto' || overflowY === 'scroll') &&
        currentElement.scrollHeight > currentElement.clientHeight
      const hasScrollX =
        (overflowX === 'auto' || overflowX === 'scroll') &&
        currentElement.scrollWidth > currentElement.clientWidth

      const canScrollY =
        hasScrollY &&
        ((deltaY > 0 && currentElement.scrollTop + currentElement.clientHeight < currentElement.scrollHeight - 1) ||
          (deltaY < 0 && currentElement.scrollTop > 0))

      const canScrollX =
        hasScrollX &&
        ((deltaX > 0 && currentElement.scrollLeft + currentElement.clientWidth < currentElement.scrollWidth - 1) ||
          (deltaX < 0 && currentElement.scrollLeft > 0))

      if (canScrollY || canScrollX) {
        return currentElement
      }

      currentElement = currentElement.parentElement
    }
  }

  return null
}

export function initScrollSpeedNormalizer(): () => void {
  if (typeof window === 'undefined' || typeof window.addEventListener !== 'function') {
    return () => {}
  }

  let cachedScrollable: HTMLElement | null = null
  let cacheTimer: ReturnType<typeof setTimeout> | null = null

  function handleWheel(event: WheelEvent) {
    // Pass through browser zoom shortcuts (Ctrl + wheel)
    if (event.ctrlKey) return

    let deltaY = event.deltaY
    let deltaX = event.deltaX

    // Shift key converts vertical scroll into horizontal scroll
    if (event.shiftKey && deltaY !== 0 && deltaX === 0) {
      deltaX = deltaY
      deltaY = 0
    }

    // Touchpad detection on Linux/WebKitGTK:
    // Continuous precision touchpads produce non-integer (fractional) deltas or very small pixel deltas (|dy| < 12).
    // Discrete mouse wheels produce integer deltas (e.g. 20, 24, 30, 33, 40) or deltaMode === 1 (lines).
    // Let touchpads scroll natively via hardware compositor for 60-144 FPS smoothness!
    const isTouchpad = event.deltaMode === 0 && (!Number.isInteger(deltaY) || Math.abs(deltaY) < 12)
    if (isTouchpad) {
      return
    }

    let scrollable: HTMLElement | null = null
    const target = event.target

    if (
      cachedScrollable &&
      (cachedScrollable === target ||
        (isNode(target) &&
          typeof cachedScrollable.contains === 'function' &&
          cachedScrollable.contains(target as Node)))
    ) {
      const canScrollY = deltaCheck(cachedScrollable, deltaY, 'y')
      const canScrollX = deltaCheck(cachedScrollable, deltaX, 'x')
      if (canScrollY || canScrollX) {
        scrollable = cachedScrollable
      }
    }

    if (!scrollable) {
      scrollable = findScrollable(target, deltaY, deltaX)
      if (scrollable) {
        cachedScrollable = scrollable
      }
    }

    if (cacheTimer) clearTimeout(cacheTimer)
    cacheTimer = setTimeout(() => {
      cachedScrollable = null
    }, 150)

    if (!scrollable) return

    // Scale delta based on deltaMode and input magnitude
    if (event.deltaMode === 1) {
      // DOM_DELTA_LINE: default WebKit 1 line ~20px -> scale to ~35px per line
      deltaY *= 35
      deltaX *= 35
    } else if (event.deltaMode === 2) {
      // DOM_DELTA_PAGE
      deltaY *= scrollable.clientHeight * 0.8
      deltaX *= scrollable.clientWidth * 0.8
    } else {
      // DOM_DELTA_PIXEL:
      // Discrete mouse wheel notches on Linux libinput/WebKitGTK typically produce 20-33px.
      // Native Linux desktop apps scroll ~90-110px per notch (2.8x - 3x multiplier).
      const multiplier = Math.abs(deltaY) >= 15 || Math.abs(deltaX) >= 15 ? 2.8 : 2.0
      deltaY *= multiplier
      deltaX *= multiplier
    }

    if (event.cancelable) {
      event.preventDefault()
    }

    if (deltaY !== 0) {
      scrollable.scrollTop += deltaY
    }
    if (deltaX !== 0) {
      scrollable.scrollLeft += deltaX
    }
  }

  function deltaCheck(element: HTMLElement, delta: number, axis: 'y' | 'x'): boolean {
    if (delta === 0) return false
    if (axis === 'y') {
      return (
        (delta > 0 && element.scrollTop + element.clientHeight < element.scrollHeight - 1) ||
        (delta < 0 && element.scrollTop > 0)
      )
    }
    return (
      (delta > 0 && element.scrollLeft + element.clientWidth < element.scrollWidth - 1) ||
      (delta < 0 && element.scrollLeft > 0)
    )
  }

  window.addEventListener('wheel', handleWheel, { passive: false })

  return () => {
    window.removeEventListener('wheel', handleWheel)
    if (cacheTimer) clearTimeout(cacheTimer)
  }
}

