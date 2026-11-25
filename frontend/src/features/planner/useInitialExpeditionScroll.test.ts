// @vitest-environment happy-dom
import { act, createElement, useRef } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  useInitialExpeditionScroll,
  type PlannerScrollTarget,
} from './useInitialExpeditionScroll'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

interface ScrollHarnessProps {
  expeditionId: number
  status: string
  rowIds: number[]
  scrollTarget?: PlannerScrollTarget | null
  onScrollTargetApplied?: (rowId: number) => void
  onScrollTargetMissed?: () => void
  onContainerReady?: (container: HTMLDivElement) => void
}

function ScrollHarness({
  expeditionId,
  status,
  rowIds,
  scrollTarget = null,
  onScrollTargetApplied,
  onScrollTargetMissed,
  onContainerReady,
}: ScrollHarnessProps) {
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  useInitialExpeditionScroll(
    scrollContainerRef,
    true,
    expeditionId,
    status,
    -1,
    scrollTarget,
    onScrollTargetApplied,
    onScrollTargetMissed,
  )

  return createElement(
    'div',
    {
      ref: (element: HTMLDivElement | null) => {
        scrollContainerRef.current = element
        if (element) {
          onContainerReady?.(element)
        }
      },
    },
    createElement(
      'table',
      null,
      createElement('thead', null, createElement('tr', null, createElement('th', null, 'System'))),
      createElement(
        'tbody',
        null,
        rowIds.map((rowId, index) =>
          createElement(
            'tr',
            { key: rowId, 'data-row-id': rowId, 'data-row-index': index },
            createElement('td', null, `Row ${rowId}`),
          ),
        ),
      ),
    ),
  )
}

function configureGeometry(
  scrollContainer: HTMLDivElement,
  rowTopById: Record<number, number>,
): { scrollIntoViewByRowId: Map<number, ReturnType<typeof vi.fn>> } {
  Object.defineProperty(scrollContainer, 'clientHeight', { value: 400, configurable: true })
  Object.defineProperty(scrollContainer, 'scrollHeight', { value: 2000, configurable: true })
  scrollContainer.getBoundingClientRect = () =>
    ({
      top: 0,
      bottom: 400,
      left: 0,
      right: 800,
      width: 800,
      height: 400,
      x: 0,
      y: 0,
      toJSON: () => {},
    }) as DOMRect

  const thead = scrollContainer.querySelector('thead') as HTMLElement
  Object.defineProperty(thead, 'offsetHeight', { value: 40, configurable: true })

  const scrollIntoViewByRowId = new Map<number, ReturnType<typeof vi.fn>>()
  for (const [rawRowId, topOffset] of Object.entries(rowTopById)) {
    const rowId = Number(rawRowId)
    const rowElement = scrollContainer.querySelector(`tr[data-row-id="${rowId}"]`) as HTMLElement
    const scrollIntoViewMock = vi.fn()
    rowElement.scrollIntoView = scrollIntoViewMock
    scrollIntoViewByRowId.set(rowId, scrollIntoViewMock)
    rowElement.getBoundingClientRect = () =>
      ({
        top: topOffset,
        bottom: topOffset + 40,
        left: 0,
        right: 800,
        width: 800,
        height: 40,
        x: 0,
        y: topOffset,
        toJSON: () => {},
      }) as DOMRect
  }

  return { scrollIntoViewByRowId }
}

describe('useInitialExpeditionScroll targeted jump', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('centers target row accounting for sticky thead and suppresses default planning scroll on expedition switch', () => {
    const queuedFrames: FrameRequestCallback[] = []
    vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((callback) => {
      queuedFrames.push(callback)
      return queuedFrames.length
    })
    vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(() => {})

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    let scrollElement!: HTMLDivElement
    const onScrollTargetApplied = vi.fn()

    try {
      // Mount expedition 1 first.
      act(() => {
        root.render(
          createElement(ScrollHarness, {
            expeditionId: 1,
            status: 'completed',
            rowIds: [10, 11],
            onContainerReady: (element) => {
              scrollElement = element
              configureGeometry(element, { 10: 40, 11: 80 })
            },
          }),
        )
      })
      expect(scrollElement.scrollTop).toBe(0)

      // Switch to planning expedition 2 with a targeted jump to row 22.
      // Default planning scroll would jump to scrollHeight (2000); targeted jump must center row 22 instead.
      let geometry = { scrollIntoViewByRowId: new Map<number, ReturnType<typeof vi.fn>>() }
      scrollElement.scrollTop = 100
      act(() => {
        root.render(
          createElement(ScrollHarness, {
            expeditionId: 2,
            status: 'planning',
            rowIds: [21, 22, 23],
            scrollTarget: { expeditionId: 2, rowId: 22, nonce: 1 },
            onScrollTargetApplied,
            onContainerReady: (element) => {
              scrollElement = element
              // visibleCenter = 40 + (400 - 40) / 2 = 220; row 22 center = 300 + 20 = 320 -> delta = +100.
              geometry = configureGeometry(element, { 21: 100, 22: 300, 23: 500 })
            },
          }),
        )
      })

      expect(geometry.scrollIntoViewByRowId.get(22)).toHaveBeenCalledWith({
        block: 'center',
        behavior: 'instant',
      })
      expect(scrollElement.scrollTop).toBe(200)
      expect(onScrollTargetApplied).toHaveBeenCalledTimes(1)
      expect(onScrollTargetApplied).toHaveBeenCalledWith(22)

      // Clearing scrollTarget after consumption must NOT trigger the default planning scroll to bottom.
      act(() => {
        root.render(
          createElement(ScrollHarness, {
            expeditionId: 2,
            status: 'planning',
            rowIds: [21, 22, 23],
            scrollTarget: null,
            onScrollTargetApplied,
          }),
        )
      })
      expect(scrollElement.scrollTop).toBe(200)

      // Next-frame micro-adjustment still runs for content-visibility reflow and does not double-notify.
      const lastFrame = queuedFrames[queuedFrames.length - 1]
      expect(lastFrame).toBeTypeOf('function')
      act(() => {
        lastFrame(16)
      })
      expect(scrollElement.scrollTop).toBe(300)
      expect(onScrollTargetApplied).toHaveBeenCalledTimes(1)
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('consumes a missing scrollTarget on next-frame retry, falls back to default scroll, and preserves later stage scrolls', () => {
    const queuedFrames: FrameRequestCallback[] = []
    vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((callback) => {
      queuedFrames.push(callback)
      return queuedFrames.length
    })
    vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(() => {})

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    let scrollElement!: HTMLDivElement
    const onScrollTargetApplied = vi.fn()
    const onScrollTargetMissed = vi.fn()

    try {
      act(() => {
        root.render(
          createElement(ScrollHarness, {
            expeditionId: 1,
            status: 'completed',
            rowIds: [10],
            onContainerReady: (element) => {
              scrollElement = element
              configureGeometry(element, { 10: 40 })
            },
          }),
        )
      })
      scrollElement.scrollTop = 120

      // Switch to planning expedition 2 targeting missing row 999.
      act(() => {
        root.render(
          createElement(ScrollHarness, {
            expeditionId: 2,
            status: 'planning',
            rowIds: [21, 22],
            scrollTarget: { expeditionId: 2, rowId: 999, nonce: 1 },
            onScrollTargetApplied,
            onScrollTargetMissed,
            onContainerReady: (element) => {
              scrollElement = element
              configureGeometry(element, { 21: 40, 22: 80 })
            },
          }),
        )
      })
      expect(onScrollTargetApplied).not.toHaveBeenCalled()
      expect(onScrollTargetMissed).not.toHaveBeenCalled()

      // Next animation frame sees row 999 still missing with laid-out container:
      // calls onScrollTargetMissed (without highlighting) and falls back to planning bottom scroll.
      const retryFrame = queuedFrames[queuedFrames.length - 1]
      act(() => {
        retryFrame(16)
      })
      expect(onScrollTargetApplied).not.toHaveBeenCalled()
      expect(onScrollTargetMissed).toHaveBeenCalledTimes(1)
      expect(scrollElement.scrollTop).toBe(2000)

      // Later stage change on expedition 2 still applies default scroll normally.
      act(() => {
        root.render(
          createElement(ScrollHarness, {
            expeditionId: 2,
            status: 'completed',
            rowIds: [21, 22],
            scrollTarget: null,
            onScrollTargetApplied,
            onScrollTargetMissed,
          }),
        )
      })
      expect(scrollElement.scrollTop).toBe(0)
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})
