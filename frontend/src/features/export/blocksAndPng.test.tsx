// @vitest-environment happy-dom
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import {
  buildCartographicsBreakdownRows,
  buildExobiologyBreakdownRows,
  buildRealizationLossRows,
} from '../../domain/telemetryBreakdowns'
import { DetailsBlock } from './blocks/DetailsBlock'
import { ExportBlockRenderer } from './blocks/ExportBlockRenderer'
import { HighlightsBlock } from './blocks/HighlightsBlock'
import { SummaryBlock } from './blocks/SummaryBlock'
import { WaypointsBlock } from './blocks/WaypointsBlock'
import { EXPORT_CARD_CSS, EXPORT_CARD_WIDTH } from './exportCardStyles'
import { createQuickShareCart, expandConfigToCartItems } from './exportMetrics'
import { buildSampleExportData } from './exportTestFixtures'
import { defaultMetricsForSection, ExportBlockConfig } from './exportTypes'
import { buildHighlightsViewModel } from './exportViewModels'
import {
  buildPngBundleFileName,
  buildSvgForeignObjectDocument,
  renderCartBlockToPngBase64,
} from './pngRenderer'

describe('export visual blocks and PNG SVG serializer', () => {
  it('renders Summary with inline Career Diff and Details with empty-selection guard', () => {
    for (const blockComponent of [
      ExportBlockRenderer,
      SummaryBlock,
      DetailsBlock,
      WaypointsBlock,
      HighlightsBlock,
    ]) {
      expect((blockComponent as unknown as { $$typeof?: symbol }).$$typeof).toBe(
        Symbol.for('react.memo'),
      )
    }

    const data = buildSampleExportData()
    const summaryMarkup = renderToStaticMarkup(
      createElement(ExportBlockRenderer, {
        exportData: data,
        config: {
          kind: 'summary',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          careerDiff: 'both',
        },
      }),
    )
    expect(summaryMarkup).toContain('4,800.0 Ly')
    expect(summaryMarkup).toContain('PRE 10,000.0 Ly')
    expect(summaryMarkup).toContain('+48.0%')
    expect(summaryMarkup).toContain('Bodies Scanned')
    expect(summaryMarkup).toContain('Bodies Mapped')
    expect(summaryMarkup).toContain('Organic Samples')
    expect(summaryMarkup).toContain('NET PROFIT:')
    expect(summaryMarkup).toContain('~129,000,000 Cr')
    expect(summaryMarkup).toContain('ed-exp-grid-top')
    expect(summaryMarkup).toContain('ed-exp-grid-bottom')
    expect(summaryMarkup).toContain('ed-exp-strip-bottom')
    expect(summaryMarkup).not.toContain('#2e1f12')
    expect(summaryMarkup).not.toContain('padding-bottom')
    expect(summaryMarkup).not.toContain('padding-top')
    expect(EXPORT_CARD_CSS).toContain('.ed-exp-strip-bottom')
    expect(EXPORT_CARD_CSS).toContain('border-bottom: none;')
    expect(summaryMarkup).not.toContain('ESTIMATED PROFIT:')
    expect(summaryMarkup).not.toContain('POST')
    expect(summaryMarkup).not.toContain('career')

    const cartoRows = buildCartographicsBreakdownRows(data.expedition.metrics)
    expect(cartoRows).toHaveLength(3)
    expect(cartoRows[2].tooltip).toContain('30 of 34 mapped bodies')
    expect(buildExobiologyBreakdownRows(data.expedition.metrics)).toHaveLength(2)
    expect(buildRealizationLossRows(data.expedition.realization)).toHaveLength(6)

    const emptyDetailsMarkup = renderToStaticMarkup(
      createElement(ExportBlockRenderer, {
        exportData: data,
        config: {
          kind: 'details',
          section: 'flight',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          selectedMetrics: [],
        },
      }),
    )
    expect(emptyDetailsMarkup).toContain('Select at least one applicable data point')

    const realizedMarkup = renderToStaticMarkup(
      createElement(ExportBlockRenderer, {
        exportData: data,
        config: {
          kind: 'details',
          section: 'realized',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          selectedMetrics: defaultMetricsForSection('realized'),
        },
      }),
    )
    expect(realizedMarkup).toContain('Net Profit After Carrier')
    expect(realizedMarkup).toContain('~129,000,000 Cr')
  })

  it('renders Waypoints with DEPARTED origin and IN SYSTEM state, and Highlights at Top 1 and Top 3', () => {
    const data = buildSampleExportData()
    const waypointsMarkup = renderToStaticMarkup(
      createElement(ExportBlockRenderer, {
        exportData: data,
        config: {
          kind: 'waypoints',
          waypointsScope: 'fullPlan',
          selectedColumns: ['regionHeaders', 'timestamps', 'flightMode', 'notes'],
        },
      }),
    )
    expect(waypointsMarkup).toContain('DEPARTED 3312-04-01 10:00 UTC')
    expect(waypointsMarkup).toContain('(IN SYSTEM)')
    expect(waypointsMarkup).toContain('IN PROGRESS')

    const highlightsTop3Markup = renderToStaticMarkup(
      createElement(ExportBlockRenderer, {
        exportData: data,
        config: {
          kind: 'highlights',
          depth: 3,
          selectedCards: ['mostLucrative', 'furthestFromSol'],
          lucrativeMetric: 'estimated',
          distanceMetric: 'total',
        },
      }),
    )
    expect(highlightsTop3Markup).toContain('TOP 3')
    expect(highlightsTop3Markup).toContain('#1 Leg #3')
    expect(highlightsTop3Markup).toContain('#3 Leg #2')
    expect(highlightsTop3Markup).toContain('[IN SYSTEM]')
  })

  it('builds valid XHTML-namespaced SVG foreignObject documents, wraps CSS in CDATA, and rejects truncated canvas PNG payloads', async () => {
    const svgDoc = buildSvgForeignObjectDocument('<div class="ed-exp-root">OK</div>', EXPORT_CARD_WIDTH, 320)
    expect(svgDoc).toContain('xmlns="http://www.w3.org/2000/svg"')
    expect(svgDoc).toContain('xmlns="http://www.w3.org/1999/xhtml"')
    expect(svgDoc).toContain('width="960" height="320"')
    expect(svgDoc).toContain('color:#f4ece4')
    expect(svgDoc).toContain(`<style><![CDATA[${EXPORT_CARD_CSS}]]></style>`)

    const data = buildSampleExportData()
    const quickItems = createQuickShareCart(data)
    expect(buildPngBundleFileName(quickItems[0], 1, 3)).toBe('images/01-full-expedition-summary.png')

    const legItems = expandConfigToCartItems(
      data,
      {
        kind: 'summary',
        scope: { kind: 'allLegs' },
        modeGrouping: 'combined',
        careerDiff: 'hide',
      },
      'leg',
    )
    expect(buildPngBundleFileName(legItems[0], 1, 223)).toBe('images/001-wp2-summary.png')
    expect(buildPngBundleFileName(legItems[0], 115, 223)).toBe('images/115-wp2-summary.png')

    const originalImage = globalThis.Image
    class ImmediateLoadImage {
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(_value: string) {
        this.onload?.()
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
    const toDataURLSpy = vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL')
    globalThis.Image = ImmediateLoadImage as unknown as typeof Image

    try {
      toDataURLSpy.mockReturnValueOnce('data:,')
      await expect(renderCartBlockToPngBase64(data, quickItems[0])).rejects.toThrow(
        'Canvas PNG export failed or exceeded maximum dimensions',
      )

      toDataURLSpy.mockReturnValueOnce('data:image/png;base64,')
      await expect(renderCartBlockToPngBase64(data, quickItems[0])).rejects.toThrow(
        'Canvas PNG export failed or exceeded maximum dimensions',
      )

      toDataURLSpy.mockReturnValueOnce('data:image/png;base64,iVBORw0KGgo=')
      await expect(renderCartBlockToPngBase64(data, quickItems[0])).resolves.toBe('iVBORw0KGgo=')
    } finally {
      globalThis.Image = originalImage
      getContextSpy.mockRestore()
      toDataURLSpy.mockRestore()
    }
  })

  it('prefixes estimated carrier costs with tilde and sanitizes malformed mode colors in React blocks', () => {
    const baseData = buildSampleExportData()
    const renderCard = (exportData: typeof baseData, config: ExportBlockConfig) =>
      renderToStaticMarkup(createElement(ExportBlockRenderer, { exportData, config }))

    expect(renderCard(baseData, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'hide' })).toContain('~26,000,000 Cr')
    expect(renderCard(baseData, { kind: 'details', section: 'realized', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: ['netProfitAfterCarrierCost'] })).toContain('Carrier cost: ~26,000,000 Cr')

    const carrierEstimated = renderCard(baseData, { kind: 'details', section: 'carrier', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: ['carrierCosts'] })
    expect(carrierEstimated).toContain('~26,000,000 Cr')
    expect(carrierEstimated).toContain('~20,000,000 Cr')
    expect(carrierEstimated).toContain('~5,600,000 Cr')
    expect(carrierEstimated).toContain('400,000 Cr')
    expect(carrierEstimated).not.toContain('~400,000 Cr')

    const nonEstimatedAndUnsafeData = buildSampleExportData({
      carrierCost: { ...baseData.carrierCost, totalEstimated: false },
      modeBuckets: baseData.modeBuckets.map((bucket) =>
        bucket.modeId === 1 ? { ...bucket, modeColor: '#12;x:1' } : bucket,
      ),
      plan: {
        ...baseData.plan,
        rows: baseData.plan.rows.map((rowView) =>
          rowView.number === 2
            ? { ...rowView, legModeColor: '#zzzzzz' }
            : rowView.number === 3
              ? { ...rowView, legModeColor: 'red;bad:1' }
              : rowView,
        ),
      } as typeof baseData.plan,
    })

    const summaryNonEstimated = renderCard(nonEstimatedAndUnsafeData, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'hide' })
    expect(summaryNonEstimated).toContain('26,000,000 Cr')
    expect(summaryNonEstimated).not.toContain('~26,000,000 Cr')

    const realizedNonEstimated = renderCard(nonEstimatedAndUnsafeData, { kind: 'details', section: 'realized', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: ['netProfitAfterCarrierCost'] })
    expect(realizedNonEstimated).toContain('Carrier cost: 26,000,000 Cr')
    expect(realizedNonEstimated).not.toContain('~26,000,000 Cr')
    expect(realizedNonEstimated).toContain('ed-exp-amber">129,000,000 Cr')
    expect(realizedNonEstimated).not.toContain('~129,000,000 Cr')

    const summaryUnsafeColor = renderCard(nonEstimatedAndUnsafeData, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'separate', selectedModeId: 1, careerDiff: 'hide' })
    expect(summaryUnsafeColor).not.toContain('#12;x:1')
    expect(summaryUnsafeColor).toContain('#FF7100')

    const detailsUnsafeColor = renderCard(nonEstimatedAndUnsafeData, { kind: 'details', section: 'flight', scope: { kind: 'full' }, modeGrouping: 'separate', selectedModeId: 1, selectedMetrics: ['jumps'] })
    expect(detailsUnsafeColor).not.toContain('#12;x:1')
    expect(detailsUnsafeColor).toContain('#FF7100')

    const waypointsUnsafeColor = renderCard(nonEstimatedAndUnsafeData, { kind: 'waypoints', waypointsScope: 'visited', selectedColumns: ['flightMode'] })
    expect(waypointsUnsafeColor).not.toContain('#zzzzzz')
    expect(waypointsUnsafeColor).not.toContain('red;bad:1')
    expect(waypointsUnsafeColor).toContain('#FF7100')
    expect(waypointsUnsafeColor).toContain('#3d2816')

    const highlightsUnsafeViewModel = buildHighlightsViewModel(nonEstimatedAndUnsafeData, {
      kind: 'highlights',
      depth: 3,
      selectedCards: ['mostLucrative'],
      lucrativeMetric: 'estimated',
      distanceMetric: 'total',
    })
    const sanitizedEntryColors = highlightsUnsafeViewModel.boards[0].entries.map(
      (entry) => entry.modeColor,
    )
    expect(sanitizedEntryColors).not.toContain('#zzzzzz')
    expect(sanitizedEntryColors).not.toContain('red;bad:1')
    expect(sanitizedEntryColors).toContain('#FF7100')
  })

  it('gates Summary NET PROFIT strip strictly on Full Expedition Combined including ship-only expeditions', () => {
    const baseData = buildSampleExportData()
    const renderSummary = (
      exportData: typeof baseData,
      config: Extract<ExportBlockConfig, { kind: 'summary' }>,
    ) => renderToStaticMarkup(createElement(ExportBlockRenderer, { exportData, config }))

    const nonCombinedConfigs: Array<Extract<ExportBlockConfig, { kind: 'summary' }>> = [
      { kind: 'summary', scope: { kind: 'baseline' }, modeGrouping: 'combined', careerDiff: 'hide' },
      { kind: 'summary', scope: { kind: 'leg', rowId: 2 }, modeGrouping: 'combined', careerDiff: 'hide' },
      { kind: 'summary', scope: { kind: 'region', regionName: 'Inner Orion Spur' }, modeGrouping: 'combined', careerDiff: 'hide' },
      { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'separate', selectedModeId: 1, careerDiff: 'hide' },
    ]
    for (const summaryConfig of nonCombinedConfigs) {
      expect(renderSummary(baseData, summaryConfig)).not.toContain('NET PROFIT:')
    }

    const shipOnlyMetrics = {
      ...baseData.expedition.metrics,
      carrierJumps: 0,
      carrierDistanceLy: 0,
      tritiumUsed: 0,
    }
    const shipOnlyDatasets = [
      buildSampleExportData({
        expedition: { metrics: shipOnlyMetrics, realization: baseData.expedition.realization },
        carrierCost: {
          ...baseData.carrierCost,
          carrierType: 'none',
          weeklyUpkeep: 0,
          tritiumPrice: 0,
          tritiumCost: 0,
          jumpCost: 0,
          upkeepCost: 0,
          totalCost: 0,
          totalEstimated: false,
        },
      }),
      buildSampleExportData({
        expedition: { metrics: shipOnlyMetrics, realization: baseData.expedition.realization },
        carrierCost: null as unknown as typeof baseData.carrierCost,
      }),
    ]
    for (const shipOnlyData of shipOnlyDatasets) {
      const shipOnlyMarkup = renderSummary(shipOnlyData, {
        kind: 'summary',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        careerDiff: 'hide',
      })
      expect(shipOnlyMarkup).toMatch(/NET PROFIT:\s*<strong[^>]*>155,000,000 Cr<\/strong>/)
    }
  })

  it('renders negative net profit with leading minus before tilde and danger accent in Summary and Details', () => {
    const baseData = buildSampleExportData()
    const renderBlock = (exportData: typeof baseData, config: ExportBlockConfig) =>
      renderToStaticMarkup(createElement(ExportBlockRenderer, { exportData, config }))

    const negativeEstimatedData = buildSampleExportData({
      expedition: {
        metrics: baseData.expedition.metrics,
        realization: { ...baseData.expedition.realization, totalRealized: 10_000_000 },
      },
    })
    const negativeExactData = buildSampleExportData({
      expedition: {
        metrics: baseData.expedition.metrics,
        realization: { ...baseData.expedition.realization, totalRealized: 10_000_000 },
      },
      carrierCost: { ...baseData.carrierCost, totalEstimated: false },
    })

    const summaryConfig: ExportBlockConfig = {
      kind: 'summary',
      scope: { kind: 'full' },
      modeGrouping: 'combined',
      careerDiff: 'hide',
    }
    const detailsConfig: ExportBlockConfig = {
      kind: 'details',
      section: 'realized',
      scope: { kind: 'full' },
      modeGrouping: 'combined',
      selectedMetrics: ['netProfitAfterCarrierCost'],
    }

    const summaryNegativeEstimated = renderBlock(negativeEstimatedData, summaryConfig)
    expect(summaryNegativeEstimated).toContain(
      'NET PROFIT: <strong class="ed-exp-mono ed-exp-danger">-~16,000,000 Cr</strong>',
    )
    expect(summaryNegativeEstimated).not.toContain('~-')
    expect(summaryNegativeEstimated).not.toContain('ed-exp-cyan">-~16,000,000 Cr')

    const summaryNegativeExact = renderBlock(negativeExactData, summaryConfig)
    expect(summaryNegativeExact).toContain(
      'NET PROFIT: <strong class="ed-exp-mono ed-exp-danger">-16,000,000 Cr</strong>',
    )

    const detailsNegativeEstimated = renderBlock(negativeEstimatedData, detailsConfig)
    expect(detailsNegativeEstimated).toContain(
      '<div class="ed-exp-kpi-value ed-exp-mono ed-exp-danger">-~16,000,000 Cr</div>',
    )
    expect(detailsNegativeEstimated).not.toContain('~-')
    expect(detailsNegativeEstimated).not.toContain('ed-exp-amber">-~16,000,000 Cr')

    const detailsNegativeExact = renderBlock(negativeExactData, detailsConfig)
    expect(detailsNegativeExact).toContain(
      '<div class="ed-exp-kpi-value ed-exp-mono ed-exp-danger">-16,000,000 Cr</div>',
    )
    expect(detailsNegativeExact).not.toContain('ed-exp-amber">-16,000,000 Cr')

    const nullCarrierData = buildSampleExportData({
      carrierCost: null as unknown as typeof baseData.carrierCost,
    })
    const detailsNullCarrier = renderBlock(nullCarrierData, detailsConfig)
    expect(detailsNullCarrier).toContain(
      '<div class="ed-exp-kpi-value ed-exp-mono ed-exp-amber">—</div>',
    )
  })
})

