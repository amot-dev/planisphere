import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { findScrollable, initScrollSpeedNormalizer } from './scrollNormalizer'

interface MockScrollableDimensions {
  clientHeight?: number
  scrollHeight?: number
  scrollTop?: number
  clientWidth?: number
  scrollWidth?: number
  scrollLeft?: number
}

interface MockScrollableElement {
  classList: { contains: (className: string) => boolean }
  hasAttribute: (attributeName: string) => boolean
  clientHeight: number
  scrollHeight: number
  scrollTop: number
  clientWidth: number
  scrollWidth: number
  scrollLeft: number
  parentElement: HTMLElement | null
  contains: (target: Node | null) => boolean
}

type WheelEventHandler = (event: WheelEvent) => void

interface MockWheelWindow {
  addEventListener: (eventName: string, handler: WheelEventHandler) => void
  removeEventListener: (eventName: string, handler: WheelEventHandler) => void
}

function createMockElement(
  classes: string[],
  dimensions: MockScrollableDimensions = {},
  parent: HTMLElement | null = null,
): HTMLElement {
  const classSet = new Set(classes)
  const mockElement: MockScrollableElement = {
    classList: {
      contains: (className: string) => classSet.has(className),
    },
    hasAttribute: (_attributeName: string) => false,
    clientHeight: dimensions.clientHeight ?? 400,
    scrollHeight: dimensions.scrollHeight ?? 400,
    scrollTop: dimensions.scrollTop ?? 0,
    clientWidth: dimensions.clientWidth ?? 600,
    scrollWidth: dimensions.scrollWidth ?? 600,
    scrollLeft: dimensions.scrollLeft ?? 0,
    parentElement: parent,
    contains: (target: Node | null) =>
      (target as unknown) === mockElement ||
      (parent !== null && typeof parent.contains === 'function' && parent.contains(target)),
  }
  return mockElement as unknown as HTMLElement
}

describe('Scroll Performance and Normalizer', () => {
  it('detects declared scrollable containers via class without forcing full style recalculation', () => {
    const parent = createMockElement(['flex-1', 'overflow-y-auto'], {
      clientHeight: 400,
      scrollHeight: 1200,
      scrollTop: 50,
    })
    const child = createMockElement([], {}, parent)

    const scrollable = findScrollable(child, 20, 0)
    expect(scrollable).toBe(parent)
  })

  it('returns null when element cannot scroll in the given direction', () => {
    const container = createMockElement(['overflow-y-auto'], {
      clientHeight: 400,
      scrollHeight: 400, // Not overflowing
      scrollTop: 0,
    })
    const child = createMockElement([], {}, container)

    const scrollable = findScrollable(child, 20, 0)
    expect(scrollable).toBeNull()
  })

  it('safely handles non-element targets like window or document', () => {
    expect(findScrollable(globalThis as unknown as EventTarget, 10, 0)).toBeNull()
    const nonNode = { someProp: true }
    expect(findScrollable(nonNode as unknown as EventTarget, 10, 0)).toBeNull()
  })

  describe('Wheel Event Normalization and Scaling', () => {
    let mockWindow: MockWheelWindow
    let listeners: Record<string, WheelEventHandler>
    let cleanup: () => void
    let originalWindow: (Window & typeof globalThis) | undefined

    beforeEach(() => {
      listeners = {}
      const globalRecord = globalThis as { window?: Window & typeof globalThis }
      originalWindow = globalRecord.window
      mockWindow = {
        addEventListener: (eventName: string, handler: WheelEventHandler) => {
          listeners[eventName] = handler
        },
        removeEventListener: (eventName: string, handler: WheelEventHandler) => {
          if (listeners[eventName] === handler) {
            delete listeners[eventName]
          }
        },
      }
      globalRecord.window = mockWindow as unknown as Window & typeof globalThis
      cleanup = initScrollSpeedNormalizer()
    })

    afterEach(() => {
      cleanup()
      const globalRecord = globalThis as { window?: Window & typeof globalThis }
      if (originalWindow === undefined) {
        delete globalRecord.window
      } else {
        globalRecord.window = originalWindow
      }
    })

    function createVerticalScrollContainer(): HTMLElement {
      return createMockElement(['overflow-y-auto'], {
        clientHeight: 400,
        scrollHeight: 1200,
        scrollTop: 50,
      })
    }

    function dispatchWheel(
      wheelHandler: WheelEventHandler,
      target: unknown,
      overrides: Partial<WheelEvent> = {},
    ): { prevented: boolean } {
      let prevented = false
      const syntheticWheelEvent = {
        target,
        deltaY: 0,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: false,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
        ...overrides,
      } as unknown as WheelEvent
      wheelHandler(syntheticWheelEvent)
      return { prevented }
    }

    it('safely processes wheel event when target is not a Node (e.g. window)', () => {
      const wheelHandler = listeners['wheel']
      expect(typeof wheelHandler).toBe('function')

      // Should not throw TypeError: parameter 1 is not of type 'Node'
      expect(() => dispatchWheel(wheelHandler, mockWindow, { deltaY: 50 })).not.toThrow()
    })

    it('scales discrete mouse wheel deltas by 2.8x on scrollable containers and prevents default', () => {
      const container = createVerticalScrollContainer()
      const { prevented } = dispatchWheel(listeners['wheel'], container, { deltaY: 20 })

      expect(prevented).toBe(true)
      // 50 + (20 * 2.8) = 106
      expect(container.scrollTop).toBe(106)
    })

    it('bypasses continuous touchpad inputs (fractional or small deltas) to preserve hardware compositor momentum', () => {
      const container = createVerticalScrollContainer()
      const wheelHandler = listeners['wheel']

      // Fractional delta
      const fractionalResult = dispatchWheel(wheelHandler, container, { deltaY: 4.5 })
      expect(fractionalResult.prevented).toBe(false)
      expect(container.scrollTop).toBe(50)

      // Small integer delta (|dy| < 12)
      const smallResult = dispatchWheel(wheelHandler, container, { deltaY: 8 })
      expect(smallResult.prevented).toBe(false)
      expect(container.scrollTop).toBe(50)
    })

    it('scales DOM_DELTA_LINE events (deltaMode: 1) by 35px per line', () => {
      const container = createVerticalScrollContainer()
      const { prevented } = dispatchWheel(listeners['wheel'], container, { deltaY: 2, deltaMode: 1 })

      expect(prevented).toBe(true)
      // 50 + (2 * 35) = 120
      expect(container.scrollTop).toBe(120)
    })

    it('passes through browser zoom shortcuts when ctrlKey is pressed', () => {
      const container = createVerticalScrollContainer()
      const { prevented } = dispatchWheel(listeners['wheel'], container, { deltaY: 20, ctrlKey: true })

      expect(prevented).toBe(false)
      expect(container.scrollTop).toBe(50)
    })

    it('converts vertical scroll into horizontal scroll when shiftKey is pressed', () => {
      const container = createMockElement(['overflow-x-auto'], {
        clientWidth: 400,
        scrollWidth: 1200,
        scrollLeft: 10,
        clientHeight: 400,
        scrollHeight: 400,
      })
      const { prevented } = dispatchWheel(listeners['wheel'], container, { deltaY: 20, shiftKey: true })

      expect(prevented).toBe(true)
      // 10 + (20 * 2.8) = 66
      expect(container.scrollLeft).toBe(66)
    })
  })
})
