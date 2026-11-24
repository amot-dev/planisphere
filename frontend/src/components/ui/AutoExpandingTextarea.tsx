import { memo, useLayoutEffect, useRef } from 'react'

const TEXTAREA_LINE_HEIGHT = 20 // 1.25rem = 20px (leading-5)
const TEXTAREA_VERTICAL_PAD = 8 // py-[3px] (6px) + border (2px) = 8px; total 1-line height = 28px (h-7)
const SINGLE_LINE_TEXT_THRESHOLD = 10

interface ObservedTextareaEntry {
  maxLines: number
  lastObservedWidth: number | null
  measuredBorderBoxWidth: number | null
  measuredSingleLine: boolean
}

interface PendingTextareaMeasurement {
  element: HTMLTextAreaElement
  minHeight: number
  maxHeight: number
}

interface ComputedTextareaLayout {
  element: HTMLTextAreaElement
  targetHeight: number
  targetOverflowY: 'auto' | 'hidden'
}

/**
 * TextareaResizeCoordinator batches DOM height measurements across mounted
 * textareas and shares a single ResizeObserver (or window resize listener).
 */
export interface TextareaResizeCoordinator {
  observe: (element: HTMLTextAreaElement, maxLines: number) => void
  unobserve: (element: HTMLTextAreaElement) => void
  scheduleMeasure: (element: HTMLTextAreaElement, maxLines: number) => void
  flushPendingMeasures: () => void
}

function applyTextareaDimensions(
  element: HTMLTextAreaElement,
  targetHeightStyle: string,
  targetOverflowY: 'auto' | 'hidden',
) {
  if (element.style.height !== targetHeightStyle) {
    element.style.height = targetHeightStyle
  }
  if (element.style.overflowY !== targetOverflowY) {
    element.style.overflowY = targetOverflowY
  }
}

function measureAndApplyTextareaBatch(
  elementsToMeasure: PendingTextareaMeasurement[],
  observedEntries: Map<HTMLTextAreaElement, ObservedTextareaEntry>,
) {
  // Reset candidate heights and overflowY together before reading any layout metrics
  // so shrinking a scrollable cell does not measure against scrollbar-narrowed width.
  for (const pending of elementsToMeasure) {
    pending.element.style.height = 'auto'
    pending.element.style.overflowY = 'hidden'
  }

  // Read all geometry in a single clean layout pass without interleaved writes.
  const computedLayouts: ComputedTextareaLayout[] = new Array(elementsToMeasure.length)
  for (let index = 0; index < elementsToMeasure.length; index++) {
    const pending = elementsToMeasure[index]
    const borderHeight = Math.max(pending.element.offsetHeight - pending.element.clientHeight, 2)
    const scrollAndBorderHeight = pending.element.scrollHeight + borderHeight
    const borderBoxWidth = pending.element.offsetWidth ?? pending.element.clientWidth
    const targetHeight = Math.min(Math.max(scrollAndBorderHeight, pending.minHeight), pending.maxHeight)
    const observedEntry = observedEntries.get(pending.element)
    if (observedEntry) {
      observedEntry.measuredSingleLine = false
      observedEntry.measuredBorderBoxWidth = borderBoxWidth > 0 ? borderBoxWidth : null
    }
    computedLayouts[index] = {
      element: pending.element,
      targetHeight,
      targetOverflowY: scrollAndBorderHeight > pending.maxHeight ? 'auto' : 'hidden',
    }
  }

  // Apply all computed heights together after every read has finished.
  for (const layout of computedLayouts) {
    applyTextareaDimensions(layout.element, `${layout.targetHeight}px`, layout.targetOverflowY)
  }
}

/**
 * createTextareaResizeCoordinator constructs a batched textarea height
 * coordinator that separates style resets, layout reads, and final height
 * writes into distinct phases so multi-row mounts and resizes trigger at most
 * one synchronous layout reflow.
 */
export function createTextareaResizeCoordinator(): TextareaResizeCoordinator {
  const observedEntries = new Map<HTMLTextAreaElement, ObservedTextareaEntry>()
  const pendingElements = new Map<HTMLTextAreaElement, number>()
  let isFlushScheduled = false
  let sharedResizeObserver: ResizeObserver | null = null
  let isWindowResizeAttached = false

  function flushPendingMeasures() {
    if (pendingElements.size === 0) return

    const batch = Array.from(pendingElements.entries())
    pendingElements.clear()

    const minHeight = TEXTAREA_LINE_HEIGHT + TEXTAREA_VERTICAL_PAD
    const singleLineHeightStyle = `${minHeight}px`
    const elementsToMeasure: PendingTextareaMeasurement[] = []

    for (const [element, maxLines] of batch) {
      if (element.isConnected === false) continue
      const currentText = element.value
      if (currentText.length <= SINGLE_LINE_TEXT_THRESHOLD && !currentText.includes('\n')) {
        applyTextareaDimensions(element, singleLineHeightStyle, 'hidden')
        const observedEntry = observedEntries.get(element)
        if (observedEntry) {
          observedEntry.measuredSingleLine = true
          observedEntry.measuredBorderBoxWidth = null
        }
        continue
      }
      elementsToMeasure.push({
        element,
        minHeight,
        maxHeight: TEXTAREA_LINE_HEIGHT * maxLines + TEXTAREA_VERTICAL_PAD,
      })
    }

    if (elementsToMeasure.length > 0) {
      measureAndApplyTextareaBatch(elementsToMeasure, observedEntries)
    }
  }

  function scheduleMeasure(element: HTMLTextAreaElement, maxLines: number) {
    const observedEntry = observedEntries.get(element)
    if (observedEntry) {
      observedEntry.maxLines = maxLines
    }
    pendingElements.set(element, maxLines)
    if (isFlushScheduled) return
    isFlushScheduled = true
    queueMicrotask(() => {
      isFlushScheduled = false
      flushPendingMeasures()
    })
  }

  function handleResizeEntries(entries: ResizeObserverEntry[]) {
    let hasQueuedResize = false
    for (const entry of entries) {
      const element = entry.target as HTMLTextAreaElement
      const observedEntry = observedEntries.get(element)
      if (!observedEntry) continue

      const nextWidth = Math.round(entry.borderBoxSize?.[0]?.inlineSize ?? entry.contentRect.width)
      if (nextWidth <= 0) continue

      const previousWidth = observedEntry.lastObservedWidth
      observedEntry.lastObservedWidth = nextWidth

      // Skip the initial observer delivery only when the pre-paint pass already
      // handled a single-line cell or measured at this exact border-box width.
      if (previousWidth === null) {
        if (
          observedEntry.measuredSingleLine ||
          (observedEntry.measuredBorderBoxWidth !== null && observedEntry.measuredBorderBoxWidth === nextWidth)
        ) {
          continue
        }
      } else if (nextWidth === previousWidth) {
        continue
      }

      pendingElements.set(element, observedEntry.maxLines)
      hasQueuedResize = true
    }
    if (hasQueuedResize) {
      flushPendingMeasures()
    }
  }

  function handleWindowResize() {
    for (const [element, observedEntry] of observedEntries) {
      pendingElements.set(element, observedEntry.maxLines)
    }
    flushPendingMeasures()
  }

  function observe(element: HTMLTextAreaElement, maxLines: number) {
    observedEntries.set(element, {
      maxLines,
      lastObservedWidth: null,
      measuredBorderBoxWidth: null,
      measuredSingleLine: false,
    })

    if (typeof ResizeObserver !== 'undefined') {
      if (!sharedResizeObserver) {
        sharedResizeObserver = new ResizeObserver(handleResizeEntries)
      }
      sharedResizeObserver.observe(element)
      return
    }

    if (typeof window !== 'undefined' && !isWindowResizeAttached) {
      window.addEventListener('resize', handleWindowResize)
      isWindowResizeAttached = true
    }
  }

  function unobserve(element: HTMLTextAreaElement) {
    observedEntries.delete(element)
    pendingElements.delete(element)
    if (sharedResizeObserver) {
      sharedResizeObserver.unobserve(element)
    }
    if (observedEntries.size > 0) return

    if (sharedResizeObserver) {
      sharedResizeObserver.disconnect()
      sharedResizeObserver = null
    }
    if (isWindowResizeAttached && typeof window !== 'undefined') {
      window.removeEventListener('resize', handleWindowResize)
      isWindowResizeAttached = false
    }
  }

  return {
    observe,
    unobserve,
    scheduleMeasure,
    flushPendingMeasures,
  }
}

const sharedTextareaResizeCoordinator = createTextareaResizeCoordinator()

/**
 * AutoExpandingTextareaProps defines the controlled text value, line bounds,
 * and focus/blur callbacks for a single-to-multi-line spreadsheet cell input.
 */
export interface AutoExpandingTextareaProps {
  value: string
  placeholder: string
  onChange: (value: string) => void
  onBlur: (value: string) => void
  onFocus?: () => void
  className?: string
  maxLines?: number
}

/**
 * AutoExpandingTextarea renders a borderless spreadsheet textarea that starts
 * at a single 28px row height and expands up to maxLines on wrap or newlines.
 */
export const AutoExpandingTextarea = memo(function AutoExpandingTextarea({
  value,
  placeholder,
  onChange,
  onBlur,
  onFocus,
  className = '',
  maxLines = 3,
}: AutoExpandingTextareaProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const maxLinesRef = useRef(maxLines)
  maxLinesRef.current = maxLines

  // Register with the shared observer before the batched pre-paint measure so
  // initial visibility is recorded before the first ResizeObserver tick.
  useLayoutEffect(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return
    sharedTextareaResizeCoordinator.observe(textareaElement, maxLinesRef.current)
    return () => {
      sharedTextareaResizeCoordinator.unobserve(textareaElement)
    }
  }, [])

  // Queue height measurement into a single pre-paint microtask batch across all
  // mounted cells to avoid interleaving DOM style writes and layout reads.
  useLayoutEffect(() => {
    const textareaElement = textareaRef.current
    if (!textareaElement) return
    sharedTextareaResizeCoordinator.scheduleMeasure(textareaElement, maxLines)
  }, [value, maxLines])

  return (
    <textarea
      ref={textareaRef}
      rows={1}
      spellCheck={false}
      autoComplete="off"
      value={value}
      placeholder={placeholder}
      onFocus={onFocus}
      onChange={(event) => onChange(event.target.value)}
      onBlur={(event) => onBlur(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === 'Enter' && !event.shiftKey) {
          event.preventDefault()
          event.currentTarget.blur()
        }
      }}
      className={`w-full bg-transparent px-1.5 py-[3px] border border-transparent focus:border-ed-amber focus:bg-ed-void focus:outline-none resize-none leading-5 text-xs block min-h-[28px] ${className}`}
    />
  )
})

