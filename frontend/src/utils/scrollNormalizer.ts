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

function isHTMLElement(val: any): val is HTMLElement {
  if (!val || typeof val !== 'object') return false
  if (typeof HTMLElement !== 'undefined') {
    return val instanceof HTMLElement
  }
  return 'classList' in val && 'scrollHeight' in val
}

function isNode(val: any): val is Node {
  if (!val || typeof val !== 'object') return false
  if (typeof Node !== 'undefined') {
    return val instanceof Node
  }
  return 'nodeType' in val || 'parentElement' in val || 'classList' in val
}

export function findScrollable(
  start: EventTarget | null,
  deltaY: number,
  deltaX: number,
): HTMLElement | null {
  const hasDoc = typeof document !== 'undefined'
  if (!start) return null
  const initialEl: HTMLElement | null =
    isHTMLElement(start)
      ? start
      : isNode(start) && isHTMLElement((start as any).parentElement)
        ? (start as any).parentElement
        : null
  if (!initialEl || (hasDoc && (initialEl === document.body || initialEl === document.documentElement))) {
    return null
  }

  // Fast path: find closest declared scrollable ancestor without getComputedStyle
  let curr: HTMLElement | null = initialEl
  while (curr && (!hasDoc || (curr !== document.body && curr !== document.documentElement))) {
    const cl = curr.classList
    const isDeclaredScrollable = Boolean(
      cl &&
      (cl.contains('overflow-y-auto') ||
        cl.contains('overflow-auto') ||
        cl.contains('overflow-y-scroll') ||
        cl.contains('overflow-x-auto') ||
        cl.contains('overflow-x-scroll') ||
        (curr.hasAttribute && curr.hasAttribute('data-scrollable')))
    )

    if (isDeclaredScrollable) {
      const canScrollY =
        (deltaY > 0 && curr.scrollTop + curr.clientHeight < curr.scrollHeight - 1) ||
        (deltaY < 0 && curr.scrollTop > 0)
      const canScrollX =
        (deltaX > 0 && curr.scrollLeft + curr.clientWidth < curr.scrollWidth - 1) ||
        (deltaX < 0 && curr.scrollLeft > 0)

      if (canScrollY || canScrollX) {
        return curr
      }
    }

    curr = curr.parentElement
  }

  // Fallback path: check computed style only if no declared scroll class was found
  if (typeof window !== 'undefined' && typeof window.getComputedStyle === 'function') {
    curr = initialEl
    while (curr && (!hasDoc || (curr !== document.body && curr !== document.documentElement))) {
      const style = window.getComputedStyle(curr)
      const overflowY = style.overflowY
      const overflowX = style.overflowX

      const hasScrollY =
        (overflowY === 'auto' || overflowY === 'scroll') &&
        curr.scrollHeight > curr.clientHeight
      const hasScrollX =
        (overflowX === 'auto' || overflowX === 'scroll') &&
        curr.scrollWidth > curr.clientWidth

      const canScrollY =
        hasScrollY &&
        ((deltaY > 0 && curr.scrollTop + curr.clientHeight < curr.scrollHeight - 1) ||
          (deltaY < 0 && curr.scrollTop > 0))

      const canScrollX =
        hasScrollX &&
        ((deltaX > 0 && curr.scrollLeft + curr.clientWidth < curr.scrollWidth - 1) ||
          (deltaX < 0 && curr.scrollLeft > 0))

      if (canScrollY || canScrollX) {
        return curr
      }

      curr = curr.parentElement
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

  function handleWheel(e: WheelEvent) {
    // Pass through browser zoom shortcuts (Ctrl + wheel)
    if (e.ctrlKey) return

    let dy = e.deltaY
    let dx = e.deltaX

    // Shift key converts vertical scroll into horizontal scroll
    if (e.shiftKey && dy !== 0 && dx === 0) {
      dx = dy
      dy = 0
    }

    // Touchpad detection on Linux/WebKitGTK:
    // Continuous precision touchpads produce non-integer (fractional) deltas or very small pixel deltas (|dy| < 12).
    // Discrete mouse wheels produce integer deltas (e.g. 20, 24, 30, 33, 40) or deltaMode === 1 (lines).
    // Let touchpads scroll natively via hardware compositor for 60-144 FPS smoothness!
    const isTouchpad = e.deltaMode === 0 && (!Number.isInteger(dy) || Math.abs(dy) < 12)
    if (isTouchpad) {
      return
    }

    let scrollable: HTMLElement | null = null
    const target = e.target

    if (
      cachedScrollable &&
      (cachedScrollable === target ||
        (isNode(target) &&
          typeof cachedScrollable.contains === 'function' &&
          cachedScrollable.contains(target as Node)))
    ) {
      const canY = deltaCheck(cachedScrollable, dy, 'y')
      const canX = deltaCheck(cachedScrollable, dx, 'x')
      if (canY || canX) {
        scrollable = cachedScrollable
      }
    }

    if (!scrollable) {
      scrollable = findScrollable(target, dy, dx)
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
    if (e.deltaMode === 1) {
      // DOM_DELTA_LINE: default WebKit 1 line ~20px -> scale to ~35px per line
      dy *= 35
      dx *= 35
    } else if (e.deltaMode === 2) {
      // DOM_DELTA_PAGE
      dy *= scrollable.clientHeight * 0.8
      dx *= scrollable.clientWidth * 0.8
    } else {
      // DOM_DELTA_PIXEL:
      // Discrete mouse wheel notches on Linux libinput/WebKitGTK typically produce 20-33px.
      // Native Linux desktop apps scroll ~90-110px per notch (2.8x - 3x multiplier).
      const multiplier = Math.abs(dy) >= 15 || Math.abs(dx) >= 15 ? 2.8 : 2.0
      dy *= multiplier
      dx *= multiplier
    }

    if (e.cancelable) {
      e.preventDefault()
    }

    if (dy !== 0) {
      scrollable.scrollTop += dy
    }
    if (dx !== 0) {
      scrollable.scrollLeft += dx
    }
  }

  function deltaCheck(el: HTMLElement, delta: number, axis: 'y' | 'x'): boolean {
    if (delta === 0) return false
    if (axis === 'y') {
      return (
        (delta > 0 && el.scrollTop + el.clientHeight < el.scrollHeight - 1) ||
        (delta < 0 && el.scrollTop > 0)
      )
    }
    return (
      (delta > 0 && el.scrollLeft + el.clientWidth < el.scrollWidth - 1) ||
      (delta < 0 && el.scrollLeft > 0)
    )
  }

  window.addEventListener('wheel', handleWheel, { passive: false })

  return () => {
    window.removeEventListener('wheel', handleWheel)
    if (cacheTimer) clearTimeout(cacheTimer)
  }
}

