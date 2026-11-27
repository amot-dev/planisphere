import { vi, type MockInstance } from 'vitest'

const activeDomMockRestorers = new Set<() => void>()

/**
 * InstalledResizeObserverMock provides deterministic ResizeObserver inspection
 * and callback triggering in unit and component tests.
 */
export interface InstalledResizeObserverMock {
  readonly instanceCount: number
  readonly disconnectCount: number
  readonly observedTargets: ReadonlySet<Element>
  trigger: (entries: ResizeObserverEntry[]) => void
  restore: () => void
}

/**
 * CanvasImageRequest represents a queued offscreen Image load when
 * installCanvasPngMock runs in manual image load mode.
 */
export interface CanvasImageRequest {
  index: number
  src: string
  triggerLoad: () => void
  triggerError: () => void
}

/**
 * CanvasPngMockOptions configures Image loading and default canvas data URL
 * behavior for installCanvasPngMock.
 */
export interface CanvasPngMockOptions {
  dataUrl?: string
  imageLoadMode?: 'immediate' | 'manual'
  shouldFailImageAt?: (imageIndex: number) => boolean
}

/**
 * InstalledCanvasPngMock exposes canvas spies and manual image-load controls
 * for SVG-to-PNG rasterization tests.
 */
export interface InstalledCanvasPngMock {
  context: CanvasRenderingContext2D
  getContextSpy: MockInstance<typeof HTMLCanvasElement.prototype.getContext>
  toDataURLSpy: MockInstance<typeof HTMLCanvasElement.prototype.toDataURL>
  pendingImages: CanvasImageRequest[]
  flushNextImageLoad: () => void
  resetImageCounter: () => void
  restore: () => void
}

/**
 * InstalledAnimationFrameQueue captures requestAnimationFrame callbacks for
 * deterministic frame stepping in scroll and layout tests.
 */
export interface InstalledAnimationFrameQueue {
  readonly pendingCount: number
  flushNext: (timestamp?: number) => void
  flushLast: (timestamp?: number) => void
  flushAll: (timestamp?: number) => void
  restore: () => void
}

function registerDomRestorer(restoreAction: () => void): () => void {
  let isRestored = false
  const idempotentRestore = (): void => {
    if (isRestored) {
      return
    }
    isRestored = true
    activeDomMockRestorers.delete(idempotentRestore)
    restoreAction()
  }
  activeDomMockRestorers.add(idempotentRestore)
  return idempotentRestore
}

/**
 * restoreAllDomMocks restores every active ResizeObserver, canvas, and
 * animation-frame mock installed during the current test.
 */
export function restoreAllDomMocks(): void {
  for (const restoreCallback of Array.from(activeDomMockRestorers)) {
    restoreCallback()
  }
  activeDomMockRestorers.clear()
}

/**
 * installResizeObserverMock replaces globalThis.ResizeObserver with a
 * controllable test double and restores it automatically after each test.
 */
export function installResizeObserverMock(): InstalledResizeObserverMock {
  const globalRecord = globalThis as { ResizeObserver?: typeof ResizeObserver }
  const hadResizeObserver = Object.prototype.hasOwnProperty.call(globalRecord, 'ResizeObserver')
  const previousResizeObserver = globalRecord.ResizeObserver

  let constructorCallCount = 0
  let disconnectCallCount = 0
  let latestCallback: ResizeObserverCallback | null = null
  let latestInstance: ResizeObserver | null = null
  const observedTargets = new Set<Element>()

  class ControllableResizeObserver implements ResizeObserver {
    constructor(callback: ResizeObserverCallback) {
      constructorCallCount += 1
      latestCallback = callback
      latestInstance = this
    }

    observe(target: Element): void {
      observedTargets.add(target)
    }

    unobserve(target: Element): void {
      observedTargets.delete(target)
    }

    disconnect(): void {
      disconnectCallCount += 1
      observedTargets.clear()
    }
  }

  globalRecord.ResizeObserver = ControllableResizeObserver

  const restore = registerDomRestorer(() => {
    if (hadResizeObserver && previousResizeObserver !== undefined) {
      globalRecord.ResizeObserver = previousResizeObserver
    } else {
      delete globalRecord.ResizeObserver
    }
  })

  return {
    get instanceCount() {
      return constructorCallCount
    },
    get disconnectCount() {
      return disconnectCallCount
    },
    observedTargets,
    trigger: (entries: ResizeObserverEntry[]) => {
      if (!latestCallback || !latestInstance) {
        throw new Error('ResizeObserver callback was triggered before any observer mounted')
      }
      latestCallback(entries, latestInstance)
    },
    restore,
  }
}

/**
 * installCanvasPngMock stubs globalThis.Image and HTMLCanvasElement 2D capture
 * methods without unsafe type casts and restores them after each test.
 */
export function installCanvasPngMock(
  options: CanvasPngMockOptions = {},
): InstalledCanvasPngMock {
  const originalImage = globalThis.Image
  const pendingImages: CanvasImageRequest[] = []
  let loadedImageCounter = 0

  class ControllableCanvasImage {
    onload: (() => void) | null = null
    onerror: (() => void) | null = null

    set src(sourceUrl: string) {
      const currentImageIndex = loadedImageCounter
      loadedImageCounter += 1
      if (options.imageLoadMode === 'manual') {
        pendingImages.push({
          index: currentImageIndex,
          src: sourceUrl,
          triggerLoad: () => this.onload?.(),
          triggerError: () => this.onerror?.(),
        })
        return
      }
      if (options.shouldFailImageAt?.(currentImageIndex)) {
        this.onerror?.()
        return
      }
      this.onload?.()
    }
  }

  const stubContext = {
    scale: vi.fn(),
    fillStyle: '',
    fillRect: vi.fn(),
    drawImage: vi.fn(),
  } as unknown as CanvasRenderingContext2D

  const getContextImplementation = (() =>
    stubContext) as unknown as typeof HTMLCanvasElement.prototype.getContext
  const getContextSpy = vi
    .spyOn(HTMLCanvasElement.prototype, 'getContext')
    .mockImplementation(getContextImplementation)
  const toDataURLSpy = vi
    .spyOn(HTMLCanvasElement.prototype, 'toDataURL')
    .mockReturnValue(options.dataUrl ?? 'data:image/png;base64,iVBORw0KGgo=')
  globalThis.Image = ControllableCanvasImage as unknown as typeof Image

  const restore = registerDomRestorer(() => {
    globalThis.Image = originalImage
    getContextSpy.mockRestore()
    toDataURLSpy.mockRestore()
  })

  return {
    context: stubContext,
    getContextSpy,
    toDataURLSpy,
    pendingImages,
    flushNextImageLoad: () => {
      const nextImage = pendingImages.shift()
      if (!nextImage) {
        throw new Error('Expected a pending offscreen Image load in queue')
      }
      nextImage.triggerLoad()
    },
    resetImageCounter: () => {
      loadedImageCounter = 0
    },
    restore,
  }
}

/**
 * installAnimationFrameQueue spies on requestAnimationFrame and
 * cancelAnimationFrame for deterministic frame stepping in tests.
 */
export function installAnimationFrameQueue(): InstalledAnimationFrameQueue {
  const pendingCallbacksByHandle = new Map<number, FrameRequestCallback>()
  let nextHandle = 0

  const requestSpy = vi
    .spyOn(globalThis, 'requestAnimationFrame')
    .mockImplementation((callback: FrameRequestCallback) => {
      nextHandle += 1
      pendingCallbacksByHandle.set(nextHandle, callback)
      return nextHandle
    })
  const cancelSpy = vi
    .spyOn(globalThis, 'cancelAnimationFrame')
    .mockImplementation((handle: number) => {
      pendingCallbacksByHandle.delete(handle)
    })

  const restore = registerDomRestorer(() => {
    pendingCallbacksByHandle.clear()
    requestSpy.mockRestore()
    cancelSpy.mockRestore()
  })

  return {
    get pendingCount() {
      return pendingCallbacksByHandle.size
    },
    flushNext: (timestamp = 16) => {
      const firstEntry = pendingCallbacksByHandle.entries().next()
      if (firstEntry.done) {
        throw new Error('Expected at least one pending animation frame')
      }
      const [frameHandle, frameCallback] = firstEntry.value
      pendingCallbacksByHandle.delete(frameHandle)
      frameCallback(timestamp)
    },
    flushLast: (timestamp = 16) => {
      const entries = Array.from(pendingCallbacksByHandle.entries())
      const lastEntry = entries.at(-1)
      if (!lastEntry) {
        throw new Error('Expected at least one pending animation frame')
      }
      const [frameHandle, frameCallback] = lastEntry
      pendingCallbacksByHandle.delete(frameHandle)
      frameCallback(timestamp)
    },
    flushAll: (timestamp = 16) => {
      const entries = Array.from(pendingCallbacksByHandle.entries())
      pendingCallbacksByHandle.clear()
      for (const [, frameCallback] of entries) {
        frameCallback(timestamp)
      }
    },
    restore,
  }
}
