import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ExportBlockRenderer } from '../blocks/ExportBlockRenderer'
import { EXPORT_CARD_WIDTH } from '../exportCardStyles'
import { createQuickShareCart, expandConfigToCartItems } from '../exportMetrics'
import { defaultMetricsForSection } from '../exportTypes'
import { buildPngBundleFileName, buildSvgForeignObjectDocument } from '../pngRenderer'
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
    expect(summaryMarkup).toContain('career')

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

  it('builds valid XHTML-namespaced SVG foreignObject documents and applies subfolder naming at 10+ PNGs', () => {
    const svgDoc = buildSvgForeignObjectDocument('<div class="ed-exp-root">OK</div>', EXPORT_CARD_WIDTH, 320)
    expect(svgDoc).toContain('xmlns="http://www.w3.org/2000/svg"')
    expect(svgDoc).toContain('xmlns="http://www.w3.org/1999/xhtml"')
    expect(svgDoc).toContain('width="960" height="320"')
    expect(svgDoc).toContain('color:#f4ece4')

    const data = buildSampleExportData()
    const quickItems = createQuickShareCart(data)
    expect(buildPngBundleFileName(quickItems[0], 1, 3)).toBe('01-summary-full-expedition.png')

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
    expect(buildPngBundleFileName(legItems[0], 1, 12)).toMatch(/^by-leg\/01-/)
  })
})

