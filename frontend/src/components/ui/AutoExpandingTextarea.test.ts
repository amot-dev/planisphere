import { afterEach, describe, expect, it } from 'vitest'
import { createTextareaResizeCoordinator } from './AutoExpandingTextarea'

interface MockTextareaOptions {
  id: string
  value: string
  scrollHeight: number | ((currentOverflowY: string) => number)
  offsetHeight?: number
  clientHeight?: number
  offsetWidth?: number
  clientWidth?: number
  isConnected?: boolean
}

function createMockTextarea(
  options: MockTextareaOptions,
  operationLog: string[],
): HTMLTextAreaElement {
  let heightValue = ''
  let overflowYValue = ''
  const resolvedOffsetWidth = options.offsetWidth ?? 200
  const resolvedClientWidth = options.clientWidth ?? Math.max(0, resolvedOffsetWidth - 2)
  const style = {
    get height() {
      return heightValue
    },
    set height(nextHeight: string) {
      heightValue = nextHeight
      operationLog.push(`write:height:${options.id}:${nextHeight}`)
    },
    get overflowY() {
      return overflowYValue
    },
    set overflowY(nextOverflow: string) {
      overflowYValue = nextOverflow
      operationLog.push(`write:overflowY:${options.id}:${nextOverflow}`)
    },
  }

  const mockElement = {
    value: options.value,
    isConnected: options.isConnected ?? true,
    style,
    get offsetHeight() {
      operationLog.push(`read:offsetHeight:${options.id}`)
      return options.offsetHeight ?? 28
    },
    get clientHeight() {
      operationLog.push(`read:clientHeight:${options.id}`)
      return options.clientHeight ?? 26
    },
    get scrollHeight() {
      operationLog.push(`read:scrollHeight:${options.id}`)
      return typeof options.scrollHeight === 'function'
        ? options.scrollHeight(overflowYValue)
        : options.scrollHeight
    },
    get offsetWidth() {
      operationLog.push(`read:offsetWidth:${options.id}`)
      return resolvedOffsetWidth
    },
    get clientWidth() {
      operationLog.push(`read:clientWidth:${options.id}`)
      return resolvedClientWidth
    },
  }
  return mockElement as unknown as HTMLTextAreaElement
}

function createResizeEntry(target: HTMLTextAreaElement, width: number): ResizeObserverEntry {
  return {
    target,
    borderBoxSize: [{ inlineSize: width, blockSize: 28 }],
    contentRect: { width: Math.max(0, width - 14) },
  } as unknown as ResizeObserverEntry
}

function expectResetsBeforeReads(operationLog: string[]): void {
  const lastAutoResetIndex = operationLog.reduce(
    (lastIndex, entry, index) =>
      entry.startsWith('write:height:') && entry.endsWith(':auto') ? index : lastIndex,
    -1,
  )
  const firstReadIndex = operationLog.findIndex((entry) => entry.startsWith('read:'))
  expect(lastAutoResetIndex).toBeGreaterThanOrEqual(0)
  expect(firstReadIndex).toBeGreaterThan(lastAutoResetIndex)
}

describe('createTextareaResizeCoordinator', () => {
  const originalResizeObserver = globalThis.ResizeObserver
  const originalWindow = globalThis.window

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver
    if (originalWindow === undefined) {
      delete (globalThis as { window?: unknown }).window
    } else {
      globalThis.window = originalWindow
    }
  })

  it('batches style resets, layout reads, and final height writes without interleaving', async () => {
    const operationLog: string[] = []
    const coordinator = createTextareaResizeCoordinator()
    const firstTextarea = createMockTextarea(
      { id: 'row1', value: 'A multi-word expedition waypoint note that wraps', scrollHeight: 46 },
      operationLog,
    )
    const secondTextarea = createMockTextarea(
      { id: 'row2', value: 'Another long note exceeding three wrapped lines in height', scrollHeight: 92 },
      operationLog,
    )
    const shortTextarea = createMockTextarea(
      { id: 'short', value: 'Sol', scrollHeight: 26 },
      operationLog,
    )

    coordinator.scheduleMeasure(firstTextarea, 3)
    coordinator.scheduleMeasure(secondTextarea, 3)
    coordinator.scheduleMeasure(shortTextarea, 3)

    // Microtask batch runs once after synchronous scheduling.
    await Promise.resolve()

    expect(shortTextarea.style.height).toBe('28px')
    expect(shortTextarea.style.overflowY).toBe('hidden')
    expect(firstTextarea.style.height).toBe('48px')
    expect(firstTextarea.style.overflowY).toBe('hidden')
    expect(secondTextarea.style.height).toBe('68px')
    expect(secondTextarea.style.overflowY).toBe('auto')

    // Short textareas never trigger layout reads.
    expect(operationLog.some((entry) => entry.startsWith('read:') && entry.includes(':short'))).toBe(false)

    // All auto-height resets happen before the first layout read, and all final
    // height writes happen after the last layout read.
    const lastReadIndex = operationLog.reduce(
      (lastIndex, entry, index) => (entry.startsWith('read:') ? index : lastIndex),
      -1,
    )
    const firstFinalHeightWriteIndex = operationLog.findIndex(
      (entry) =>
        entry.startsWith('write:height:') && !entry.endsWith(':auto') && !entry.includes(':short:'),
    )

    expectResetsBeforeReads(operationLog)
    expect(firstFinalHeightWriteIndex).toBeGreaterThan(lastReadIndex)
  })

  it('shares one ResizeObserver across textareas, skips redundant initial ticks, and disconnects on last unobserve', async () => {
    let observerConstructorCalls = 0
    let disconnectCalls = 0
    let capturedCallback: ResizeObserverCallback | null = null
    const observedTargets = new Set<Element>()

    class MockResizeObserver implements ResizeObserver {
      constructor(callback: ResizeObserverCallback) {
        observerConstructorCalls++
        capturedCallback = callback
      }
      observe(target: Element) {
        observedTargets.add(target)
      }
      unobserve(target: Element) {
        observedTargets.delete(target)
      }
      disconnect() {
        disconnectCalls++
        observedTargets.clear()
      }
    }

    globalThis.ResizeObserver = MockResizeObserver

    const operationLog: string[] = []
    const coordinator = createTextareaResizeCoordinator()
    const visibleTextarea = createMockTextarea(
      { id: 'visible', value: 'Visible long expedition note', scrollHeight: 46, offsetWidth: 240, clientWidth: 238 },
      operationLog,
    )
    const offscreenTextarea = createMockTextarea(
      { id: 'offscreen', value: 'Offscreen long expedition note', scrollHeight: 46, offsetWidth: 0, clientWidth: 0 },
      operationLog,
    )

    coordinator.observe(visibleTextarea, 3)
    coordinator.observe(offscreenTextarea, 3)
    expect(observerConstructorCalls).toBe(1)
    expect(observedTargets.size).toBe(2)

    coordinator.scheduleMeasure(visibleTextarea, 3)
    coordinator.scheduleMeasure(offscreenTextarea, 3)
    await Promise.resolve()
    operationLog.length = 0

    // Initial observer callback: visible cell was already measured while visible,
    // while offscreen cell mounted at width 0 and must measure once visible.
    capturedCallback!(
      [createResizeEntry(visibleTextarea, 240), createResizeEntry(offscreenTextarea, 240)],
      {} as ResizeObserver,
    )
    expect(operationLog.some((entry) => entry.includes(':visible'))).toBe(false)
    expect(operationLog.some((entry) => entry.includes(':offscreen'))).toBe(true)

    // Height-only notifications at unchanged width are ignored.
    operationLog.length = 0
    capturedCallback!(
      [createResizeEntry(visibleTextarea, 240), createResizeEntry(offscreenTextarea, 240)],
      {} as ResizeObserver,
    )
    expect(operationLog).toHaveLength(0)

    // Width changes trigger one batched pass across both textareas.
    capturedCallback!(
      [createResizeEntry(visibleTextarea, 180), createResizeEntry(offscreenTextarea, 180)],
      {} as ResizeObserver,
    )
    expectResetsBeforeReads(operationLog)

    coordinator.unobserve(visibleTextarea)
    expect(disconnectCalls).toBe(0)
    coordinator.unobserve(offscreenTextarea)
    expect(disconnectCalls).toBe(1)
  })

  it('re-measures on initial ResizeObserver scrollbar width shift while skipping single-line cells, and preserves lastObservedWidth across hide and unhide', async () => {
    let capturedCallback: ResizeObserverCallback | null = null
    globalThis.ResizeObserver = class {
      constructor(callback: ResizeObserverCallback) {
        capturedCallback = callback
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver

    const operationLog: string[] = []
    const coordinator = createTextareaResizeCoordinator()
    const shiftedTextarea = createMockTextarea(
      { id: 'shifted', value: 'Long note affected by container scrollbar shift', scrollHeight: 46, offsetWidth: 240, clientWidth: 238 },
      operationLog,
    )
    const singleLineTextarea = createMockTextarea(
      { id: 'single', value: 'Sol', scrollHeight: 26, offsetWidth: 240, clientWidth: 238 },
      operationLog,
    )

    coordinator.observe(shiftedTextarea, 3)
    coordinator.observe(singleLineTextarea, 3)
    coordinator.scheduleMeasure(shiftedTextarea, 3)
    coordinator.scheduleMeasure(singleLineTextarea, 3)
    await Promise.resolve()
    operationLog.length = 0

    // Container scrollbar narrows width from 240 to 226 before first observer tick:
    // multi-line shifted cell must re-measure, while short single-line cell skips.
    capturedCallback!(
      [createResizeEntry(shiftedTextarea, 226), createResizeEntry(singleLineTextarea, 226)],
      {} as ResizeObserver,
    )
    expect(operationLog.some((entry) => entry.includes(':shifted'))).toBe(true)
    expect(operationLog.some((entry) => entry.includes(':single'))).toBe(false)

    // Hiding (width 0) and unhiding at the same width (226) preserves lastObservedWidth and skips.
    operationLog.length = 0
    capturedCallback!([createResizeEntry(shiftedTextarea, 0)], {} as ResizeObserver)
    expect(operationLog).toHaveLength(0)
    capturedCallback!([createResizeEntry(shiftedTextarea, 226)], {} as ResizeObserver)
    expect(operationLog).toHaveLength(0)

    // Unhiding at a different width (200) triggers re-measurement because 226 was preserved.
    capturedCallback!([createResizeEntry(shiftedTextarea, 0)], {} as ResizeObserver)
    capturedCallback!([createResizeEntry(shiftedTextarea, 200)], {} as ResizeObserver)
    expect(operationLog.some((entry) => entry.includes(':shifted'))).toBe(true)
  })

  it('resets overflowY to hidden before reading scrollHeight when shrinking a scrollable cell', async () => {
    const operationLog: string[] = []
    const coordinator = createTextareaResizeCoordinator()
    let currentScrollableHeight = 92
    const shrinkableTextarea = createMockTextarea(
      {
        id: 'shrink',
        value: 'Initial multi-line note exceeding three wrapped lines in height',
        scrollHeight: (currentOverflowY) => (currentOverflowY === 'hidden' ? currentScrollableHeight : 46),
      },
      operationLog,
    )

    coordinator.scheduleMeasure(shrinkableTextarea, 3)
    await Promise.resolve()
    expect(shrinkableTextarea.style.height).toBe('68px')
    expect(shrinkableTextarea.style.overflowY).toBe('auto')

    // Shrink text so it fits on a single line only when overflowY scrollbar is cleared before reading scrollHeight.
    shrinkableTextarea.value = 'Shorter note text'
    currentScrollableHeight = 26
    operationLog.length = 0

    coordinator.scheduleMeasure(shrinkableTextarea, 3)
    await Promise.resolve()

    const hiddenResetIndex = operationLog.indexOf('write:overflowY:shrink:hidden')
    const scrollReadIndex = operationLog.indexOf('read:scrollHeight:shrink')
    expect(hiddenResetIndex).toBeGreaterThanOrEqual(0)
    expect(scrollReadIndex).toBeGreaterThan(hiddenResetIndex)
    expect(shrinkableTextarea.style.height).toBe('28px')
    expect(shrinkableTextarea.style.overflowY).toBe('hidden')
  })

  it('attaches and cleans up a single window resize listener when ResizeObserver is unavailable', () => {
    delete (globalThis as { ResizeObserver?: unknown }).ResizeObserver
    const listeners = new Map<string, Set<EventListener>>()
    ;(globalThis as { window?: unknown }).window = {
      addEventListener(eventName: string, listener: EventListener) {
        const set = listeners.get(eventName) ?? new Set()
        set.add(listener)
        listeners.set(eventName, set)
      },
      removeEventListener(eventName: string, listener: EventListener) {
        listeners.get(eventName)?.delete(listener)
      },
    }

    const operationLog: string[] = []
    const coordinator = createTextareaResizeCoordinator()
    const firstTextarea = createMockTextarea(
      { id: 'first', value: 'Fallback resize measurement cell one', scrollHeight: 46 },
      operationLog,
    )
    const secondTextarea = createMockTextarea(
      { id: 'second', value: 'Fallback resize measurement cell two', scrollHeight: 46 },
      operationLog,
    )

    coordinator.observe(firstTextarea, 3)
    coordinator.observe(secondTextarea, 3)
    expect(listeners.get('resize')?.size).toBe(1)

    for (const listener of listeners.get('resize') ?? []) {
      listener(new Event('resize'))
    }
    expect(firstTextarea.style.height).toBe('48px')
    expect(secondTextarea.style.height).toBe('48px')

    coordinator.unobserve(firstTextarea)
    expect(listeners.get('resize')?.size).toBe(1)
    coordinator.unobserve(secondTextarea)
    expect(listeners.get('resize')?.size).toBe(0)
  })
})

