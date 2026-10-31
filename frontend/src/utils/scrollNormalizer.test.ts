import { describe, expect, it } from 'vitest'
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
  const el: any = {
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
  }
  return el as HTMLElement
}

describe('Scroll Performance & Normalizer', () => {
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

  it('safely processes wheel event when target is not a Node (e.g. window)', () => {
    const listeners: Record<string, Function> = {}
    const originalWindow = (globalThis as any).window

    try {
      const mockWindow: any = {
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

      const cleanup = initScrollSpeedNormalizer()
      const wheelHandler = listeners['wheel']
      expect(typeof wheelHandler).toBe('function')

      // Wheel event targeted at Window object
      const fakeWheelEv: any = {
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
      expect(() => wheelHandler(fakeWheelEv)).not.toThrow()

      cleanup()
    } finally {
      ;(globalThis as any).window = originalWindow
    }
  })
})

