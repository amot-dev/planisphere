// @vitest-environment happy-dom
import { act, type ComponentProps, type ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { exporter } from '../../api/models'
import type { DurationDisplayMode } from '../../domain/durationDisplay'
import { createDeferred, flushMicrotasks } from '../../test/async'
import { installCanvasPngMock, installResizeObserverMock } from '../../test/domMocks'
import { buildSampleExportData } from '../../test/fixtures/exportFixtures'
import { mountTestRoot, setFormControlValue, type MountedTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { BlockConfigurator } from './BlockConfigurator'
import { ExportCartQueue } from './ExportCartQueue'
import { ExportExpeditionModal } from './ExportExpeditionModal'
import { ExportPreviewPane } from './ExportPreviewPane'

interface MemoComponentInternal<TProps> {
  type: (props: TProps) => ReactElement | null
}

async function mountExportModal(
  expeditionId = 7,
  durationDisplayMode?: DurationDisplayMode,
): Promise<MountedTestRoot> {
  let mounted!: MountedTestRoot
  await act(async () => {
    mounted = mountTestRoot(
      <ExportExpeditionModal
        expeditionId={expeditionId}
        durationDisplayMode={durationDisplayMode}
        onClose={() => {}}
      />,
    )
    await flushMicrotasks()
  })
  return mounted
}

describe('ExportExpeditionModal', () => {
  it('isolates inspected cart context from BlockConfigurator and locks controls while exporting', async () => {
    installResizeObserverMock()

    const sampleExportData = buildSampleExportData()
    let savePathDeferred = createDeferred<string>()
    const { spies } = installWailsAppMock({
      GetExpeditionExportData: async () => sampleExportData,
      PickExportSavePath: () => {
        savePathDeferred = createDeferred<string>()
        return savePathDeferred.promise
      },
      WriteExportOutput: async (targetPath: string) => targetPath,
    })

    const mounted = await mountExportModal(7)
    expect(spies.GetExpeditionExportData).toHaveBeenCalledWith(7)

    // Queue a Pre-Expedition Baseline Summary block so we can inspect it while configuring Details.
    const scopeSelect = mounted.container.querySelector('select') as HTMLSelectElement
    expect(scopeSelect).toBeTruthy()
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
      savePathDeferred.resolve('')
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
      savePathDeferred.resolve('')
      await flushMicrotasks()
    })
    expect(mounted.findButtonByText('Next', { exact: true }).disabled).toBe(false)
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

  it('defaults Waypoints table duration column from durationDisplayMode setting', async () => {
    installResizeObserverMock()
    const baseData = buildSampleExportData()
    const playtimeData = buildSampleExportData({
      rowDetails: baseData.rowDetails.map((detail) =>
        detail.rowId === 2 ? { ...detail, inGameSeconds: 11700, inGameDuration: '3h 15m' } : detail,
      ) as typeof baseData.rowDetails,
    })
    installWailsAppMock({ GetExpeditionExportData: async () => playtimeData })

    const inGameMounted = await mountExportModal(7, 'in_game')
    // Preloaded Quick Share Waypoints cart item must align with in_game durationDisplayMode.
    await act(async () => {
      inGameMounted.findButtonByText('Waypoints (Visited Only)', { exact: false }).click()
      await flushMicrotasks()
    })
    const quickInGameHeaders = Array.from(inGameMounted.container.querySelectorAll('th')).map((cell) => cell.textContent)
    expect(quickInGameHeaders).toContain('In-Game')
    expect(quickInGameHeaders).not.toContain('Duration')

    await act(async () => {
      inGameMounted.findButtonByText('Waypoints', { exact: true }).click()
      await flushMicrotasks()
    })
    const inGameHeaders = Array.from(inGameMounted.container.querySelectorAll('th')).map((cell) => cell.textContent)
    expect(inGameHeaders).toContain('In-Game')
    expect(inGameHeaders).not.toContain('Duration')
    expect(inGameMounted.container.textContent).toContain('3h 15m')
    inGameMounted.unmount()

    const calendarMounted = await mountExportModal(7, 'calendar')
    // Preloaded Quick Share Waypoints cart item must align with calendar durationDisplayMode.
    await act(async () => {
      calendarMounted.findButtonByText('Waypoints (Visited Only)', { exact: false }).click()
      await flushMicrotasks()
    })
    const quickCalendarHeaders = Array.from(calendarMounted.container.querySelectorAll('th')).map((cell) => cell.textContent)
    expect(quickCalendarHeaders).toContain('Duration')
    expect(quickCalendarHeaders).not.toContain('In-Game')

    await act(async () => {
      calendarMounted.findButtonByText('Waypoints', { exact: true }).click()
      await flushMicrotasks()
    })
    const calendarHeaders = Array.from(calendarMounted.container.querySelectorAll('th')).map((cell) => cell.textContent)
    expect(calendarHeaders).toContain('Duration')
    expect(calendarHeaders).not.toContain('In-Game')
    calendarMounted.unmount()
  })
})
