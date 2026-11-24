// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { exporter } from '../../api/models'
import { BlockConfigurator } from './BlockConfigurator'
import { ExportCartQueue } from './ExportCartQueue'
import { ExportExpeditionModal } from './ExportExpeditionModal'
import { ExportPreviewPane } from './ExportPreviewPane'
import { buildSampleExportData } from './exportTestFixtures'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

interface MockGoWindow {
  go?: {
    app?: {
      App?: Record<string, (...args: never[]) => unknown>
    }
  }
}

function findButtonByText(container: HTMLElement, textMatch: string): HTMLButtonElement {
  const button = Array.from(container.querySelectorAll('button')).find((element) =>
    element.textContent?.includes(textMatch),
  )
  if (!button) {
    throw new Error(`Expected button containing "${textMatch}" to exist`)
  }
  return button
}

function findButtonByExactText(container: HTMLElement, exactText: string): HTMLButtonElement {
  const button = Array.from(container.querySelectorAll('button')).find(
    (element) => element.textContent?.trim() === exactText,
  )
  if (!button) {
    throw new Error(`Expected button with exact text "${exactText}" to exist`)
  }
  return button
}

function setSelectValue(select: HTMLSelectElement, nextValue: string): void {
  const descriptor = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')
  descriptor?.set?.call(select, nextValue)
  select.dispatchEvent(new Event('change', { bubbles: true }))
}

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

describe('ExportExpeditionModal', () => {
  const originalResizeObserver = globalThis.ResizeObserver

  afterEach(() => {
    delete (window as unknown as MockGoWindow).go
    if (originalResizeObserver) {
      globalThis.ResizeObserver = originalResizeObserver
    } else {
      delete (globalThis as { ResizeObserver?: unknown }).ResizeObserver
    }
  })

  it('omits Custom Timeframe, isolates inspected cart context from BlockConfigurator, and locks controls while exporting', async () => {
    if (typeof globalThis.ResizeObserver === 'undefined') {
      class StubResizeObserver implements ResizeObserver {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      }
      globalThis.ResizeObserver = StubResizeObserver
    }

    const sampleExportData = buildSampleExportData()
    let resolveSavePath!: (path: string) => void
    const createPendingSavePath = () =>
      new Promise<string>((resolve) => {
        resolveSavePath = resolve
      })

    const mockGetExpeditionExportData = vi.fn(async () => sampleExportData)
    const mockPickExportSavePath = vi.fn(createPendingSavePath)
    const mockWriteExportOutput = vi.fn(async (targetPath: string) => targetPath)

    ;(window as unknown as MockGoWindow).go = {
      app: {
        App: {
          GetExpeditionExportData: mockGetExpeditionExportData as unknown as (
            ...args: never[]
          ) => unknown,
          PickExportSavePath: mockPickExportSavePath as unknown as (
            ...args: never[]
          ) => unknown,
          WriteExportOutput: mockWriteExportOutput as unknown as (...args: never[]) => unknown,
        },
      },
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          createElement(ExportExpeditionModal, {
            expeditionId: 7,
            onClose: () => {},
          }),
        )
        await flushMicrotasks()
      })

      expect(mockGetExpeditionExportData).toHaveBeenCalledWith(7)

      // Custom Timeframe must not appear in Scope options.
      const scopeSelect = container.querySelector('select') as HTMLSelectElement
      expect(scopeSelect).toBeTruthy()
      const scopeOptionValues = Array.from(scopeSelect.options).map((option) => option.value)
      expect(scopeOptionValues).not.toContain('customTime')
      expect(container.textContent).not.toContain('Custom Timeframe')

      // Queue a Pre-Expedition Baseline Summary block so we can inspect it while configuring Details.
      await act(async () => {
        setSelectValue(scopeSelect, 'baseline')
        await flushMicrotasks()
      })
      const addToCartButton = findButtonByText(container, '+ ADD TO CART')
      await act(async () => {
        addToCartButton.click()
        await flushMicrotasks()
      })

      // Switch configurator to Details -> Realized Value (Full Expedition Combined), isolate Net Profit After Carrier Costs.
      const detailsTab = findButtonByExactText(container, 'Details')
      await act(async () => {
        detailsTab.click()
        await flushMicrotasks()
      })

      const selects = Array.from(container.querySelectorAll('select'))
      expect(selects.length).toBeGreaterThanOrEqual(2)
      const sectionSelect = selects[1]
      await act(async () => {
        setSelectValue(sectionSelect, 'realized')
        await flushMicrotasks()
      })

      const noneButton = findButtonByExactText(container, '[None]')
      await act(async () => {
        noneButton.click()
        await flushMicrotasks()
      })

      const netProfitMetricButton = findButtonByText(container, 'Net Profit After Carrier Costs')
      expect(netProfitMetricButton.disabled).toBe(false)
      await act(async () => {
        netProfitMetricButton.click()
        await flushMicrotasks()
      })
      expect(findButtonByText(container, '+ ADD TO CART').disabled).toBe(false)

      // Inspecting the queued Pre-Expedition Baseline Summary must NOT leak baseline context into BlockConfigurator.
      const baselineCartItemButton = findButtonByText(
        container,
        'Summary — Pre-Expedition Baseline',
      )
      await act(async () => {
        baselineCartItemButton.click()
        await flushMicrotasks()
      })

      expect(container.textContent).toContain('Inspecting Cart Block')
      expect(findButtonByText(container, 'Net Profit After Carrier Costs').disabled).toBe(false)
      expect(findButtonByText(container, '+ ADD TO CART').disabled).toBe(false)

      // Trigger EXPORT and verify controls lock while PickExportSavePath is pending.
      const exportButton = findButtonByExactText(container, 'EXPORT')
      await act(async () => {
        exportButton.click()
        await flushMicrotasks()
      })

      expect(mockPickExportSavePath).toHaveBeenCalledTimes(1)
      expect(findButtonByExactText(container, 'EXPORTING...').disabled).toBe(true)

      // Footer output toggles must be disabled while exporting.
      expect(findButtonByText(container, 'PNG Images').disabled).toBe(true)
      expect(findButtonByText(container, 'HTML Report').disabled).toBe(true)
      expect(findButtonByText(container, 'JSON Data').disabled).toBe(true)

      // Preview bar [Back to Config] must be disabled while exporting.
      expect(findButtonByExactText(container, '[Back to Config]').disabled).toBe(true)

      // BlockConfigurator controls must be disabled while exporting.
      expect(findButtonByExactText(container, 'Summary').disabled).toBe(true)
      expect(findButtonByExactText(container, 'Details').disabled).toBe(true)
      expect(findButtonByExactText(container, 'Waypoints').disabled).toBe(true)
      expect(findButtonByExactText(container, 'Highlights').disabled).toBe(true)
      expect(findButtonByExactText(container, '[All]').disabled).toBe(true)
      expect(findButtonByExactText(container, '[None]').disabled).toBe(true)
      expect(findButtonByText(container, 'Net Profit After Carrier Costs').disabled).toBe(true)
      expect(findButtonByText(container, '+ ADD TO CART').disabled).toBe(true)
      for (const selectElement of Array.from(container.querySelectorAll('select'))) {
        expect(selectElement.disabled).toBe(true)
      }

      // ExportCartQueue controls must be disabled while exporting.
      expect(findButtonByExactText(container, 'Quick Share').disabled).toBe(true)
      expect(findButtonByExactText(container, 'Clear').disabled).toBe(true)
      expect(findButtonByText(container, 'Summary — Pre-Expedition Baseline').disabled).toBe(true)
      const upButtons = Array.from(container.querySelectorAll('button')).filter(
        (button) => button.textContent?.trim() === 'UP',
      )
      const downButtons = Array.from(container.querySelectorAll('button')).filter(
        (button) => button.textContent?.trim() === 'DN',
      )
      const removeButtons = Array.from(container.querySelectorAll('button')).filter(
        (button) => button.textContent?.trim() === 'X',
      )
      expect(upButtons.length).toBeGreaterThan(0)
      expect(downButtons.length).toBeGreaterThan(0)
      expect(removeButtons.length).toBeGreaterThan(0)
      for (const button of [...upButtons, ...downButtons, ...removeButtons]) {
        expect(button.disabled).toBe(true)
      }

      // Cancel save dialog so exporting resets and controls re-enable cleanly.
      await act(async () => {
        resolveSavePath('')
        await flushMicrotasks()
      })

      expect(findButtonByExactText(container, 'EXPORT').disabled).toBe(false)
      expect(findButtonByText(container, 'PNG Images').disabled).toBe(false)
      expect(findButtonByExactText(container, 'Quick Share').disabled).toBe(false)

      // Return to active config, switch to Summary with Separate mode split, and verify Prev/Next and Career/Mode controls lock too.
      await act(async () => {
        findButtonByExactText(container, '[Back to Config]').click()
        await flushMicrotasks()
      })
      await act(async () => {
        findButtonByExactText(container, 'Summary').click()
        await flushMicrotasks()
      })
      await act(async () => {
        findButtonByText(container, 'Separate (').click()
        await flushMicrotasks()
      })

      await act(async () => {
        findButtonByExactText(container, 'EXPORT').click()
        await flushMicrotasks()
      })

      expect(findButtonByExactText(container, 'Prev').disabled).toBe(true)
      expect(findButtonByExactText(container, 'Next').disabled).toBe(true)
      expect(findButtonByExactText(container, 'Combined').disabled).toBe(true)
      expect(findButtonByText(container, 'Separate (').disabled).toBe(true)

      await act(async () => {
        resolveSavePath('')
        await flushMicrotasks()
      })
      expect(findButtonByExactText(container, 'Next').disabled).toBe(false)
    } finally {
      await act(async () => {
        root.unmount()
      })
      container.remove()
    }
  })

  it('keeps 24h-gated Details metrics selectable and addable when Slice 0 is under 24h and a later slice reaches 24h', async () => {
    if (typeof globalThis.ResizeObserver === 'undefined') {
      globalThis.ResizeObserver = class implements ResizeObserver {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      }
    }

    const baseData = buildSampleExportData()
    const sub24hFirstLegData = buildSampleExportData({
      rowDetails: baseData.rowDetails.map((detail, index) =>
        index === 0
          ? ({ ...detail, from: '2026-04-01T10:00:00Z', to: '2026-04-01T16:00:00Z', elapsed: '06h 00m' } as typeof detail)
          : detail,
      ),
    })

    ;(window as unknown as MockGoWindow).go = {
      app: {
        App: {
          GetExpeditionExportData: vi.fn(async () => sub24hFirstLegData) as never,
          PickExportSavePath: vi.fn(async () => '') as never,
          WriteExportOutput: vi.fn(async (targetPath: string) => targetPath) as never,
        },
      },
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const clickAndFlush = async (element: HTMLElement) => {
      await act(async () => {
        element.click()
        await flushMicrotasks()
      })
    }

    try {
      await act(async () => {
        root.render(createElement(ExportExpeditionModal, { expeditionId: 7, onClose: () => {} }))
        await flushMicrotasks()
      })

      await clickAndFlush(findButtonByExactText(container, 'Details'))
      const [scopeSelect, sectionSelect] = Array.from(container.querySelectorAll('select'))
      await act(async () => {
        setSelectValue(scopeSelect, 'allLegs')
        await flushMicrotasks()
      })

      // Slice 0 lasted 6h (< 24h), while Slice 1 lasted 50h (>= 24h): Avg Distance per Day must stay enabled.
      expect(findButtonByText(container, 'Avg Distance per Day').disabled).toBe(false)
      await clickAndFlush(findButtonByExactText(container, '[None]'))
      expect(findButtonByText(container, '+ ADD 3 BLOCKS TO CART').disabled).toBe(true)
      await clickAndFlush(findButtonByText(container, 'Avg Distance per Day'))
      expect(findButtonByText(container, '+ ADD 3 BLOCKS TO CART').disabled).toBe(false)

      // Exploration Value likewise keeps Avg Credits per Day enabled and addable across All Legs.
      await act(async () => {
        setSelectValue(sectionSelect, 'explorationValue')
        await flushMicrotasks()
      })
      expect(findButtonByText(container, 'Avg Credits per Day').disabled).toBe(false)
      await clickAndFlush(findButtonByExactText(container, '[None]'))
      await clickAndFlush(findButtonByText(container, 'Avg Credits per Day'))
      await clickAndFlush(findButtonByText(container, '+ ADD 3 BLOCKS TO CART'))
      expect(container.textContent).toContain('Export Cart (6)')

      // Scoping down to the single 6h leg alone disables Avg Credits per Day and blocks + ADD TO CART.
      await act(async () => {
        setSelectValue(scopeSelect, 'leg')
        await flushMicrotasks()
      })
      expect(findButtonByText(container, 'Avg Credits per Day').disabled).toBe(true)
      expect(findButtonByText(container, '+ ADD TO CART').disabled).toBe(true)
    } finally {
      await act(async () => {
        root.unmount()
      })
      container.remove()
    }
  })

  it('accepts hydrated Wails ExpeditionExport instances directly and delegates canonical JSON assembly to Go via canonicalJsonExpeditionId', async () => {
    if (typeof globalThis.ResizeObserver === 'undefined') {
      globalThis.ResizeObserver = class implements ResizeObserver {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      }
    }

    const hydratedExport = exporter.ExpeditionExport.createFrom(buildSampleExportData())
    const mockWriteExportOutput = vi.fn(
      async (targetPath: string, _files: exporter.BundleFile[]) => targetPath,
    )

    ;(window as unknown as MockGoWindow).go = {
      app: {
        App: {
          GetExpeditionExportData: vi.fn(async () => hydratedExport) as never,
          PickExportSavePath: vi.fn(async () => '/tmp/orion-horizon-test-snapshot.json') as never,
          WriteExportOutput: mockWriteExportOutput as never,
        },
      },
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(createElement(ExportExpeditionModal, { expeditionId: 7, onClose: () => {} }))
        await flushMicrotasks()
      })

      await act(async () => {
        findButtonByText(container, 'PNG Images').click()
        findButtonByText(container, 'JSON Data').click()
        await flushMicrotasks()
      })

      await act(async () => {
        findButtonByExactText(container, 'EXPORT').click()
        await flushMicrotasks()
      })

      expect(mockWriteExportOutput).toHaveBeenCalledTimes(1)
      const [savedPath, wireFiles] = mockWriteExportOutput.mock.calls[0]
      expect(savedPath).toBe('/tmp/orion-horizon-test-snapshot.json')
      expect(wireFiles).toHaveLength(1)
      expect(wireFiles[0]).toMatchObject({
        name: 'orion-horizon-test-snapshot.json',
        canonicalJsonExpeditionId: 7,
        canonicalJsonExportedAt: hydratedExport.exportedAt,
      })
      expect(wireFiles[0].contentText).toBeUndefined()
    } finally {
      await act(async () => {
        root.unmount()
      })
      container.remove()
    }
  })

  it('updates leaf ExportStatusBanner during PNG progress ticks without re-rendering BlockConfigurator or ExportCartQueue', async () => {
    if (typeof globalThis.ResizeObserver === 'undefined') {
      globalThis.ResizeObserver = class implements ResizeObserver {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      }
    }

    const originalImage = globalThis.Image
    const pendingImageLoads: Array<() => void> = []
    class ControlledImage {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(_value: string) {
        pendingImageLoads.push(() => this.onload?.())
      }
    }
    const stubContext = {
      scale: vi.fn(),
      fillStyle: '',
      fillRect: vi.fn(),
      drawImage: vi.fn(),
    } as unknown as CanvasRenderingContext2D

    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, 'getContext')
      .mockReturnValue(stubContext as never)
    const toDataURLSpy = vi
      .spyOn(HTMLCanvasElement.prototype, 'toDataURL')
      .mockReturnValue('data:image/png;base64,iVBORw0KGgo=')
    const configuratorRenderSpy = vi.spyOn(
      BlockConfigurator as unknown as { type: (...args: never[]) => unknown },
      'type',
    )
    const cartQueueRenderSpy = vi.spyOn(
      ExportCartQueue as unknown as { type: (...args: never[]) => unknown },
      'type',
    )
    const previewPaneRenderSpy = vi.spyOn(
      ExportPreviewPane as unknown as { type: (...args: never[]) => unknown },
      'type',
    )
    globalThis.Image = ControlledImage as unknown as typeof Image

    ;(window as unknown as MockGoWindow).go = {
      app: {
        App: {
          GetExpeditionExportData: vi.fn(async () => buildSampleExportData()) as never,
          PickExportSavePath: vi.fn(async () => '/tmp/orion-export.zip') as never,
          WriteExportOutput: vi.fn(async (targetPath: string) => targetPath) as never,
        },
      },
    }

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(createElement(ExportExpeditionModal, { expeditionId: 7, onClose: () => {} }))
        await flushMicrotasks()
      })

      await act(async () => {
        findButtonByExactText(container, 'EXPORT').click()
        await flushMicrotasks()
      })

      expect(container.textContent).toContain('Rendering PNG 0 / 3...')
      expect(pendingImageLoads).toHaveLength(1)

      configuratorRenderSpy.mockClear()
      cartQueueRenderSpy.mockClear()
      previewPaneRenderSpy.mockClear()

      await act(async () => {
        pendingImageLoads.shift()?.()
        await flushMicrotasks()
      })
      expect(container.textContent).toContain('Rendering PNG 1 / 3...')
      expect(configuratorRenderSpy).toHaveBeenCalledTimes(0)
      expect(cartQueueRenderSpy).toHaveBeenCalledTimes(0)
      expect(previewPaneRenderSpy).toHaveBeenCalledTimes(0)

      await act(async () => {
        pendingImageLoads.shift()?.()
        await flushMicrotasks()
      })
      expect(container.textContent).toContain('Rendering PNG 2 / 3...')
      expect(configuratorRenderSpy).toHaveBeenCalledTimes(0)
      expect(cartQueueRenderSpy).toHaveBeenCalledTimes(0)
      expect(previewPaneRenderSpy).toHaveBeenCalledTimes(0)

      await act(async () => {
        pendingImageLoads.shift()?.()
        await flushMicrotasks()
      })
      expect(container.textContent).toContain('Saved to /tmp/orion-export.zip')
      expect(findButtonByExactText(container, 'EXPORT').disabled).toBe(false)
    } finally {
      await act(async () => {
        root.unmount()
      })
      container.remove()
      globalThis.Image = originalImage
      getContextSpy.mockRestore()
      toDataURLSpy.mockRestore()
      configuratorRenderSpy.mockRestore()
      cartQueueRenderSpy.mockRestore()
      previewPaneRenderSpy.mockRestore()
    }
  })
})
