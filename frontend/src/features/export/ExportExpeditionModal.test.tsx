// @vitest-environment happy-dom
import { act, createElement, type ComponentProps, type ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { exporter } from '../../api/models'
import { installCanvasPngMock, installResizeObserverMock } from '../../test/domMocks'
import { mountTestRoot, setFormControlValue, type MountedTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { BlockConfigurator } from './BlockConfigurator'
import { ExportCartQueue } from './ExportCartQueue'
import { ExportExpeditionModal } from './ExportExpeditionModal'
import { ExportPreviewPane } from './ExportPreviewPane'
import { buildSampleExportData } from './exportTestFixtures'

interface MemoComponentInternal<TProps> {
  type: (props: TProps) => ReactElement | null
}

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

async function mountExportModal(expeditionId = 7): Promise<MountedTestRoot> {
  let mounted!: MountedTestRoot
  await act(async () => {
    mounted = mountTestRoot(
      createElement(ExportExpeditionModal, {
        expeditionId,
        onClose: () => {},
      }),
    )
    await flushMicrotasks()
  })
  return mounted
}

describe('ExportExpeditionModal', () => {
  it('omits Custom Timeframe, isolates inspected cart context from BlockConfigurator, and locks controls while exporting', async () => {
    installResizeObserverMock()

    const sampleExportData = buildSampleExportData()
    let resolveSavePath!: (path: string) => void
    const { spies } = installWailsAppMock({
      GetExpeditionExportData: async () => sampleExportData,
      PickExportSavePath: () =>
        new Promise<string>((resolve) => {
          resolveSavePath = resolve
        }),
      WriteExportOutput: async (targetPath: string) => targetPath,
    })

    const mounted = await mountExportModal(7)
    expect(spies.GetExpeditionExportData).toHaveBeenCalledWith(7)

    // Custom Timeframe must not appear in Scope options.
    const scopeSelect = mounted.container.querySelector('select') as HTMLSelectElement
    expect(scopeSelect).toBeTruthy()
    const scopeOptionValues = Array.from(scopeSelect.options).map((option) => option.value)
    expect(scopeOptionValues).not.toContain('customTime')
    expect(mounted.container.textContent).not.toContain('Custom Timeframe')

    // Queue a Pre-Expedition Baseline Summary block so we can inspect it while configuring Details.
    await act(async () => {
      setFormControlValue(scopeSelect, 'baseline')
      await flushMicrotasks()
    })
    await act(async () => {
      mounted.findButtonByText('+ ADD TO CART', { exact: false }).click()
      await flushMicrotasks()
    })

    // Switch configurator to Details -> Realized Value (Full Expedition Combined), isolate Net Profit After Carrier Costs.
    await act(async () => {
      mounted.findButtonByText('Details', { exact: true }).click()
      await flushMicrotasks()
    })

    const selects = Array.from(mounted.container.querySelectorAll('select'))
    expect(selects.length).toBeGreaterThanOrEqual(2)
    const sectionSelect = selects[1]
    await act(async () => {
      setFormControlValue(sectionSelect, 'realized')
      await flushMicrotasks()
    })

    await act(async () => {
      mounted.findButtonByText('[None]', { exact: true }).click()
      await flushMicrotasks()
    })

    const netProfitMetricButton = mounted.findButtonByText('Net Profit After Carrier Costs', {
      exact: false,
    })
    expect(netProfitMetricButton.disabled).toBe(false)
    await act(async () => {
      netProfitMetricButton.click()
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('+ ADD TO CART', { exact: false }).disabled).toBe(false)

    // Inspecting the queued Pre-Expedition Baseline Summary must NOT leak baseline context into BlockConfigurator.
    const baselineCartItemButton = mounted.findButtonByText(
      'Summary — Pre-Expedition Baseline',
      { exact: false },
    )
    await act(async () => {
      baselineCartItemButton.click()
      await flushMicrotasks()
    })

    expect(mounted.container.textContent).toContain('Inspecting Cart Block')
    expect(
      mounted.findButtonByText('Net Profit After Carrier Costs', { exact: false }).disabled,
    ).toBe(false)
    expect(mounted.findButtonByText('+ ADD TO CART', { exact: false }).disabled).toBe(false)

    // Trigger EXPORT and verify controls lock while PickExportSavePath is pending.
    await act(async () => {
      mounted.findButtonByText('EXPORT', { exact: true }).click()
      await flushMicrotasks()
    })

    expect(spies.PickExportSavePath).toHaveBeenCalledTimes(1)
    expect(mounted.findButtonByText('EXPORTING...', { exact: true }).disabled).toBe(true)

    // Footer output toggles must be disabled while exporting.
    expect(mounted.findButtonByText('PNG Images', { exact: false }).disabled).toBe(true)
    expect(mounted.findButtonByText('HTML Report', { exact: false }).disabled).toBe(true)
    expect(mounted.findButtonByText('JSON Data', { exact: false }).disabled).toBe(true)

    // Preview bar [Back to Config] must be disabled while exporting.
    expect(mounted.findButtonByText('[Back to Config]', { exact: true }).disabled).toBe(true)

    // BlockConfigurator controls must be disabled while exporting.
    expect(mounted.findButtonByText('Summary', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Details', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Waypoints', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Highlights', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('[All]', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('[None]', { exact: true }).disabled).toBe(true)
    expect(
      mounted.findButtonByText('Net Profit After Carrier Costs', { exact: false }).disabled,
    ).toBe(true)
    expect(mounted.findButtonByText('+ ADD TO CART', { exact: false }).disabled).toBe(true)
    for (const selectElement of Array.from(mounted.container.querySelectorAll('select'))) {
      expect(selectElement.disabled).toBe(true)
    }

    // ExportCartQueue controls must be disabled while exporting.
    expect(mounted.findButtonByText('Quick Share', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Clear', { exact: true }).disabled).toBe(true)
    expect(
      mounted.findButtonByText('Summary — Pre-Expedition Baseline', { exact: false }).disabled,
    ).toBe(true)
    const upButtons = Array.from(mounted.container.querySelectorAll('button')).filter(
      (button) => button.textContent?.trim() === 'UP',
    )
    const downButtons = Array.from(mounted.container.querySelectorAll('button')).filter(
      (button) => button.textContent?.trim() === 'DN',
    )
    const removeButtons = Array.from(mounted.container.querySelectorAll('button')).filter(
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

    expect(mounted.findButtonByText('EXPORT', { exact: true }).disabled).toBe(false)
    expect(mounted.findButtonByText('PNG Images', { exact: false }).disabled).toBe(false)
    expect(mounted.findButtonByText('Quick Share', { exact: true }).disabled).toBe(false)

    // Return to active config, switch to Summary with Separate mode split, and verify Prev/Next and Career/Mode controls lock too.
    await act(async () => {
      mounted.findButtonByText('[Back to Config]', { exact: true }).click()
      await flushMicrotasks()
    })
    await act(async () => {
      mounted.findButtonByText('Summary', { exact: true }).click()
      await flushMicrotasks()
    })
    await act(async () => {
      mounted.findButtonByText('Separate (', { exact: false }).click()
      await flushMicrotasks()
    })

    await act(async () => {
      mounted.findButtonByText('EXPORT', { exact: true }).click()
      await flushMicrotasks()
    })

    expect(mounted.findButtonByText('Prev', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Next', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Combined', { exact: true }).disabled).toBe(true)
    expect(mounted.findButtonByText('Separate (', { exact: false }).disabled).toBe(true)

    await act(async () => {
      resolveSavePath('')
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('Next', { exact: true }).disabled).toBe(false)
  })

  it('keeps 24h-gated Details metrics selectable and addable when Slice 0 is under 24h and a later slice reaches 24h', async () => {
    installResizeObserverMock()

    const baseData = buildSampleExportData()
    const sub24hFirstLegData = buildSampleExportData({
      rowDetails: baseData.rowDetails.map((detail, index) =>
        index === 0
          ? ({ ...detail, from: '2026-04-01T10:00:00Z', to: '2026-04-01T16:00:00Z', elapsed: '06h 00m' } as typeof detail)
          : detail,
      ),
    })

    installWailsAppMock({
      GetExpeditionExportData: async () => sub24hFirstLegData,
      PickExportSavePath: async () => '',
      WriteExportOutput: async (targetPath: string) => targetPath,
    })

    const mounted = await mountExportModal(7)
    const clickAndFlush = async (element: HTMLElement) => {
      await act(async () => {
        element.click()
        await flushMicrotasks()
      })
    }

    await clickAndFlush(mounted.findButtonByText('Details', { exact: true }))
    const [scopeSelect, sectionSelect] = Array.from(mounted.container.querySelectorAll('select'))
    await act(async () => {
      setFormControlValue(scopeSelect, 'allLegs')
      await flushMicrotasks()
    })

    // Slice 0 lasted 6h (< 24h), while Slice 1 lasted 50h (>= 24h): Avg Distance per Day must stay enabled.
    expect(mounted.findButtonByText('Avg Distance per Day', { exact: false }).disabled).toBe(false)
    await clickAndFlush(mounted.findButtonByText('[None]', { exact: true }))
    expect(mounted.findButtonByText('+ ADD 3 BLOCKS TO CART', { exact: false }).disabled).toBe(
      true,
    )
    await clickAndFlush(mounted.findButtonByText('Avg Distance per Day', { exact: false }))
    expect(mounted.findButtonByText('+ ADD 3 BLOCKS TO CART', { exact: false }).disabled).toBe(
      false,
    )

    // Exploration Value likewise keeps Avg Credits per Day enabled and addable across All Legs.
    await act(async () => {
      setFormControlValue(sectionSelect, 'explorationValue')
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('Avg Credits per Day', { exact: false }).disabled).toBe(false)
    await clickAndFlush(mounted.findButtonByText('[None]', { exact: true }))
    await clickAndFlush(mounted.findButtonByText('Avg Credits per Day', { exact: false }))
    await clickAndFlush(mounted.findButtonByText('+ ADD 3 BLOCKS TO CART', { exact: false }))
    expect(mounted.container.textContent).toContain('Export Cart (6)')

    // Scoping down to the single 6h leg alone disables Avg Credits per Day and blocks + ADD TO CART.
    await act(async () => {
      setFormControlValue(scopeSelect, 'leg')
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('Avg Credits per Day', { exact: false }).disabled).toBe(true)
    expect(mounted.findButtonByText('+ ADD TO CART', { exact: false }).disabled).toBe(true)
  })

  it('accepts hydrated Wails ExpeditionExport instances directly and delegates canonical JSON assembly to Go via canonicalJsonExpeditionId', async () => {
    installResizeObserverMock()

    const hydratedExport = exporter.ExpeditionExport.createFrom(buildSampleExportData())
    const { spies } = installWailsAppMock({
      GetExpeditionExportData: async () => hydratedExport,
      PickExportSavePath: async () => '/tmp/orion-horizon-test-snapshot.json',
      WriteExportOutput: async (targetPath: string, _files: exporter.BundleFile[]) => targetPath,
    })

    const mounted = await mountExportModal(7)

    await act(async () => {
      mounted.findButtonByText('PNG Images', { exact: false }).click()
      mounted.findButtonByText('JSON Data', { exact: false }).click()
      await flushMicrotasks()
    })

    await act(async () => {
      mounted.findButtonByText('EXPORT', { exact: true }).click()
      await flushMicrotasks()
    })

    expect(spies.WriteExportOutput).toHaveBeenCalledTimes(1)
    const [savedPath, wireFiles] = spies.WriteExportOutput.mock.calls[0]
    expect(savedPath).toBe('/tmp/orion-horizon-test-snapshot.json')
    expect(wireFiles).toHaveLength(1)
    expect(wireFiles[0]).toMatchObject({
      name: 'orion-horizon-test-snapshot.json',
      canonicalJsonExpeditionId: 7,
      canonicalJsonExportedAt: hydratedExport.exportedAt,
    })
    expect(wireFiles[0].contentText).toBeUndefined()
  })

  it('updates leaf ExportStatusBanner during PNG progress ticks without re-rendering BlockConfigurator or ExportCartQueue', async () => {
    installResizeObserverMock()
    const canvasMock = installCanvasPngMock({ imageLoadMode: 'manual' })

    const configuratorRenderSpy = vi.spyOn(
      BlockConfigurator as unknown as MemoComponentInternal<ComponentProps<typeof BlockConfigurator>>,
      'type',
    )
    const cartQueueRenderSpy = vi.spyOn(
      ExportCartQueue as unknown as MemoComponentInternal<ComponentProps<typeof ExportCartQueue>>,
      'type',
    )
    const previewPaneRenderSpy = vi.spyOn(
      ExportPreviewPane as unknown as MemoComponentInternal<ComponentProps<typeof ExportPreviewPane>>,
      'type',
    )

    installWailsAppMock({
      GetExpeditionExportData: async () => buildSampleExportData(),
      PickExportSavePath: async () => '/tmp/orion-export.zip',
      WriteExportOutput: async (targetPath: string) => targetPath,
    })

    try {
      const mounted = await mountExportModal(7)

      await act(async () => {
        mounted.findButtonByText('EXPORT', { exact: true }).click()
        await flushMicrotasks()
      })

      expect(mounted.container.textContent).toContain('Rendering PNG 0 / 3...')
      expect(canvasMock.pendingImages).toHaveLength(1)

      configuratorRenderSpy.mockClear()
      cartQueueRenderSpy.mockClear()
      previewPaneRenderSpy.mockClear()

      await act(async () => {
        canvasMock.flushNextImageLoad()
        await flushMicrotasks()
      })
      expect(mounted.container.textContent).toContain('Rendering PNG 1 / 3...')
      expect(configuratorRenderSpy).toHaveBeenCalledTimes(0)
      expect(cartQueueRenderSpy).toHaveBeenCalledTimes(0)
      expect(previewPaneRenderSpy).toHaveBeenCalledTimes(0)

      await act(async () => {
        canvasMock.flushNextImageLoad()
        await flushMicrotasks()
      })
      expect(mounted.container.textContent).toContain('Rendering PNG 2 / 3...')
      expect(configuratorRenderSpy).toHaveBeenCalledTimes(0)
      expect(cartQueueRenderSpy).toHaveBeenCalledTimes(0)
      expect(previewPaneRenderSpy).toHaveBeenCalledTimes(0)

      await act(async () => {
        canvasMock.flushNextImageLoad()
        await flushMicrotasks()
      })
      expect(mounted.container.textContent).toContain('Saved to /tmp/orion-export.zip')
      expect(mounted.findButtonByText('EXPORT', { exact: true }).disabled).toBe(false)
    } finally {
      configuratorRenderSpy.mockRestore()
      cartQueueRenderSpy.mockRestore()
      previewPaneRenderSpy.mockRestore()
    }
  })
})
