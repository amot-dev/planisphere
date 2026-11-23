import { describe, expect, it } from 'vitest'
import { createQuickShareCart, expandConfigToCartItems } from '../exportMetrics'
import { defaultMetricsForSection } from '../exportTypes'
import { generateSelfContainedHtmlReport } from '../htmlGenerator'
import { buildSampleExportData } from './exportTestFixtures'

describe('htmlGenerator', () => {
  it('renders a self-contained offline HTML report with escaped titles, TOC, print rules, and departure badges', () => {
    const data = buildSampleExportData()
    const quickCart = createQuickShareCart(data)
    const detailsItem = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'explorationValue',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection('explorationValue'),
      },
      'extra',
    )
    const html = generateSelfContainedHtmlReport(data, [...quickCart, ...detailsItem])

    expect(html).toContain('<!DOCTYPE html>')
    expect(html).toContain('@media print')
    // Expedition name containing angle brackets must be HTML-escaped.
    expect(html).not.toContain('Orion Horizon <Test>')
    expect(html).toContain('Orion Horizon')
    expect(html).toContain('DEPARTED 3312-04-01 10:00 UTC')
    expect(html).toContain('(IN SYSTEM)')
    expect(html).toContain('EXPEDITION HIGHLIGHTS // TOP 1')
    expect(html).toContain('Combined Estimated Exploration Value')
    expect(html).toContain('href="#section-1"')
    expect(html).toContain('href="#section-4"')
  })
})

