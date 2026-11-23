import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ExportBlockRenderer } from './blocks/ExportBlockRenderer'
import { EXPORT_CARD_WIDTH } from './exportCardStyles'
import { createQuickShareCart, expandConfigToCartItems } from './exportMetrics'
import { defaultMetricsForSection, ExportBlockConfig } from './exportTypes'
import { buildPngBundleFileName, buildSvgForeignObjectDocument } from './pngRenderer'
import { buildSampleExportData } from './exportTestFixtures'

describe('export visual blocks and PNG SVG serializer', () => {
  it('renders Summary with inline Career Diff and Details with empty-selection guard', () => {
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
    expect(summaryMarkup).not.toContain('ESTIMATED PROFIT:')
    expect(summaryMarkup).not.toContain('POST')
    expect(summaryMarkup).not.toContain('career')

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
    expect(realizedMarkup).toContain('129,000,000 Cr')
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

  it('builds valid XHTML-namespaced SVG foreignObject documents and places PNGs under images/ with dynamic zero-padding', () => {
    const svgDoc = buildSvgForeignObjectDocument('<div class="ed-exp-root">OK</div>', EXPORT_CARD_WIDTH, 320)
    expect(svgDoc).toContain('xmlns="http://www.w3.org/2000/svg"')
    expect(svgDoc).toContain('xmlns="http://www.w3.org/1999/xhtml"')
    expect(svgDoc).toContain('width="960" height="320"')
    expect(svgDoc).toContain('color:#f4ece4')

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
  })
})

