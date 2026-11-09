import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { findScrollable, initScrollSpeedNormalizer } from './scrollNormalizer'

function createMockElement(classes: string[], dimensions: {
  clientHeight?: number
  scrollHeight?: number
  scrollTop?: number
  clientWidth?: number
  scrollWidth?: number
  scrollLeft?: number
} = {}, parent: any = null): HTMLElement {
  const classSet = new Set(classes)
  const mockElement: any = {
    classList: {
      contains: (c: string) => classSet.has(c),
    },
    hasAttribute: (_attr: string) => false,
    clientHeight: dimensions.clientHeight ?? 400,
    scrollHeight: dimensions.scrollHeight ?? 400,
    scrollTop: dimensions.scrollTop ?? 0,
    clientWidth: dimensions.clientWidth ?? 600,
    scrollWidth: dimensions.scrollWidth ?? 600,
    scrollLeft: dimensions.scrollLeft ?? 0,
    parentElement: parent,
    contains: (target: any) => target === mockElement || (parent && typeof parent.contains === 'function' && parent.contains(target)),
  }
  return mockElement as HTMLElement
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
    expect(findScrollable(globalThis as any, 10, 0)).toBeNull()
    const nonNode = { someProp: true }
    expect(findScrollable(nonNode as any, 10, 0)).toBeNull()
  })

  describe('Wheel Event Normalization and Scaling', () => {
    let mockWindow: any
    let listeners: Record<string, Function>
    let cleanup: () => void
    let originalWindow: any

    beforeEach(() => {
      listeners = {}
      originalWindow = (globalThis as any).window
      mockWindow = {
        addEventListener: (event: string, handler: Function) => {
          listeners[event] = handler
        },
        removeEventListener: (event: string, handler: Function) => {
          if (listeners[event] === handler) {
            delete listeners[event]
          }
        },
      }
      ;(globalThis as any).window = mockWindow
      cleanup = initScrollSpeedNormalizer()
    })

    afterEach(() => {
      cleanup()
      ;(globalThis as any).window = originalWindow
    })

    it('safely processes wheel event when target is not a Node (e.g. window)', () => {
      const wheelHandler = listeners['wheel']
      expect(typeof wheelHandler).toBe('function')

      // Wheel event targeted at Window object
      const fakeWheelEvent: any = {
        target: mockWindow,
        deltaY: 50,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: false,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {},
      }

      // Should not throw TypeError: parameter 1 is not of type 'Node'
      expect(() => wheelHandler(fakeWheelEvent)).not.toThrow()
    })

    it('scales discrete mouse wheel deltas by 2.8x on scrollable containers and prevents default', () => {
      const container = createMockElement(['overflow-y-auto'], {
        clientHeight: 400,
        scrollHeight: 1200,
        scrollTop: 50,
      })
      let prevented = false
      const wheelHandler = listeners['wheel']

      wheelHandler({
        target: container,
        deltaY: 20,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: false,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
      })

      expect(prevented).toBe(true)
      // 50 + (20 * 2.8) = 106
      expect(container.scrollTop).toBe(106)
    })

    it('bypasses continuous touchpad inputs (fractional or small deltas) to preserve hardware compositor momentum', () => {
      const container = createMockElement(['overflow-y-auto'], {
        clientHeight: 400,
        scrollHeight: 1200,
        scrollTop: 50,
      })
      let prevented = false
      const wheelHandler = listeners['wheel']

      // Fractional delta
      wheelHandler({
        target: container,
        deltaY: 4.5,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: false,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
      })
      expect(prevented).toBe(false)
      expect(container.scrollTop).toBe(50)

      // Small integer delta (|dy| < 12)
      wheelHandler({
        target: container,
        deltaY: 8,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: false,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
      })
      expect(prevented).toBe(false)
      expect(container.scrollTop).toBe(50)
    })

    it('scales DOM_DELTA_LINE events (deltaMode: 1) by 35px per line', () => {
      const container = createMockElement(['overflow-y-auto'], {
        clientHeight: 400,
        scrollHeight: 1200,
        scrollTop: 50,
      })
      let prevented = false
      const wheelHandler = listeners['wheel']

      wheelHandler({
        target: container,
        deltaY: 2,
        deltaX: 0,
        deltaMode: 1,
        ctrlKey: false,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
      })

      expect(prevented).toBe(true)
      // 50 + (2 * 35) = 120
      expect(container.scrollTop).toBe(120)
    })

    it('passes through browser zoom shortcuts when ctrlKey is pressed', () => {
      const container = createMockElement(['overflow-y-auto'], {
        clientHeight: 400,
        scrollHeight: 1200,
        scrollTop: 50,
      })
      let prevented = false
      const wheelHandler = listeners['wheel']

      wheelHandler({
        target: container,
        deltaY: 20,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: true,
        shiftKey: false,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
      })

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
      let prevented = false
      const wheelHandler = listeners['wheel']

      wheelHandler({
        target: container,
        deltaY: 20,
        deltaX: 0,
        deltaMode: 0,
        ctrlKey: false,
        shiftKey: true,
        cancelable: true,
        preventDefault: () => {
          prevented = true
        },
      })

      expect(prevented).toBe(true)
      // 10 + (20 * 2.8) = 66
      expect(container.scrollLeft).toBe(66)
    })
  })
})

