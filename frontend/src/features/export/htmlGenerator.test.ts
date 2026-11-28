import { describe, expect, it } from 'vitest'
import { createQuickShareCart } from './exportMetrics'
import { defaultMetricsForSection } from './exportTypes'
import { generateSelfContainedHtmlReport } from './htmlGenerator'
import { buildSampleExportData, expandCartItem } from '../../test/fixtures/exportFixtures'

describe('htmlGenerator', () => {
  it('renders a self-contained offline HTML report with escaped titles, print rules, departure badges, and no TOC bar', () => {
    const data = buildSampleExportData()
    const quickCart = createQuickShareCart(data)
    const detailsItem = expandCartItem(
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
    const html = generateSelfContainedHtmlReport(data, [...quickCart, detailsItem])

    expect(html).toContain('<!DOCTYPE html>')
    expect(html).toContain('@media print')
    // Expedition name containing angle brackets must be HTML-escaped.
    expect(html).not.toContain('Orion Horizon <Test>')
    expect(html).toContain('Orion Horizon')
    expect(html).toContain('<span>CMDR: <strong class="mono">CMDR Horizon</strong></span>')
    expect(html).toContain('<div class="kpi-val amber mono">~26,000,000 Cr</div>')
    expect(html).toContain('<div class="kpi-grid kpi-grid-top cols-4">')
    expect(html).toContain('<div class="kpi-grid kpi-grid-bottom cols-4">')
    expect(html).toContain('<div class="strip-bar strip-bar-bottom">')
    expect(html).toContain('NET PROFIT: <strong class="mono cyan">~129,000,000 Cr</strong>')
    expect(html).toContain('DEPARTED 3312-04-01 10:00 UTC')
    expect(html).toContain('COVERED 4,200.0 Ly')
    expect(html).toContain('(IN SYSTEM)')
    expect(html).toContain('EXPEDITION HIGHLIGHTS // TOP 1')
    expect(html).toContain('Combined Estimated Exploration Value')
    expect(html).not.toContain('toc-bar')
    expect(html).not.toContain('toc-link')
    expect(html).toContain('id="section-1"')
    expect(html).toContain('id="section-4"')
  })

  it('renders separate-mode accent borders, tilde carrier cost prefixes, and single-card highlight metadata', () => {
    const data = buildSampleExportData()
    const cartItems = [
      expandCartItem(data, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'separate', selectedModeId: 1, careerDiff: 'hide' }, 'sum-sep'),
      expandCartItem(data, { kind: 'details', section: 'carrier', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: defaultMetricsForSection('carrier') }, 'det-carrier'),
      expandCartItem(data, { kind: 'details', section: 'realized', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: defaultMetricsForSection('realized') }, 'det-realized'),
      expandCartItem(data, { kind: 'details', section: 'flight', scope: { kind: 'full' }, modeGrouping: 'separate', selectedModeId: 1, selectedMetrics: ['jumps'] }, 'det-flight-sep'),
      expandCartItem(data, { kind: 'highlights', depth: 1, selectedCards: ['mostLucrative'], lucrativeMetric: 'estimated', distanceMetric: 'total' }, 'hl'),
    ]

    const html = generateSelfContainedHtmlReport(data, cartItems)

    expect(html).toContain('<section id="section-1" class="card" style="border-left:3px solid #00D2FF">')
    expect(html).toContain('<section id="section-4" class="card" style="border-left:3px solid #00D2FF">')

    // Carrier cost estimation prefixes in Details realized subline and carrier breakdown card.
    expect(html).toContain('<div class="kpi-val mono amber">~129,000,000 Cr</div>')
    expect(html).toContain('Carrier cost: ~26,000,000 Cr')
    expect(html).toContain('>~20,000,000 Cr<')
    expect(html).toContain('>~5,600,000 Cr<')
    expect(html).toContain('>400,000 Cr<')
    expect(html).not.toContain('~400,000 Cr')

    expect(html).toContain('<div>1 TROPHY CARDS</div>')
    expect(html).toContain('<div class="muted">3312-04-01 10:00 UTC</div>')
  })

  it('handles empty Details or Highlights states, non-estimated carrier totals, and malformed mode colors safely', () => {
    const baseData = buildSampleExportData()
    const hardenedData = buildSampleExportData({
      carrierCost: { ...baseData.carrierCost, totalEstimated: false },
      modeBuckets: baseData.modeBuckets.map((bucket) =>
        bucket.modeId === 1 ? { ...bucket, modeColor: '#zzzzzz' } : bucket,
      ),
      plan: {
        ...baseData.plan,
        rows: baseData.plan.rows.map((rowView) =>
          rowView.number === 2
            ? { ...rowView, legModeColor: '#12;x:1' }
            : rowView.number === 3
              ? { ...rowView, legModeColor: 'javascript:alert(1)' }
              : rowView,
        ),
      } as typeof baseData.plan,
    })

    const cartItems = [
      expandCartItem(hardenedData, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'hide' }, 'sum-exact'),
      expandCartItem(hardenedData, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'separate', selectedModeId: 1, careerDiff: 'hide' }, 'sum-bad-color'),
      expandCartItem(hardenedData, { kind: 'details', section: 'flight', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: [] }, 'det-empty'),
      expandCartItem(hardenedData, { kind: 'details', section: 'realized', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: ['netProfitAfterCarrierCost'] }, 'det-exact-realized'),
      expandCartItem(hardenedData, { kind: 'waypoints', waypointsScope: 'visited', selectedColumns: ['flightMode'] }, 'wp-bad-color'),
      expandCartItem(hardenedData, { kind: 'highlights', depth: 1, selectedCards: [], lucrativeMetric: 'estimated', distanceMetric: 'total' }, 'hl-empty'),
    ]

    const html = generateSelfContainedHtmlReport(hardenedData, cartItems)

    expect(html).toContain('<div class="kpi-val amber mono">26,000,000 Cr</div>')
    expect(html).toContain('<div class="kpi-val mono amber">129,000,000 Cr</div>')
    expect(html).toContain('Carrier cost: 26,000,000 Cr')
    expect(html).not.toContain('~26,000,000 Cr')
    expect(html).not.toContain('~129,000,000 Cr')

    expect(html).toContain('<div class="empty-banner mono">Select at least one applicable data point to preview and add this block</div>')
    expect(html).toContain('<div class="empty-banner mono">Select at least one highlight card with recorded expedition data</div>')
    expect(html).toContain('<div>0 TROPHY CARDS</div>')
    expect(html).not.toContain('<div class="hl-grid">')

    expect(html).not.toContain('#zzzzzz')
    expect(html).not.toContain('#12;x:1')
    expect(html).not.toContain('javascript:alert(1)')
    expect(html).toContain('border-left:3px solid #FF7100')
    expect(html).toContain('border-left:3px solid #3d2816')
  })

  it('renders ELAPSED and IN-GAME headers, In-Game Playtime breakdown card, and Waypoints In-Game column in HTML reports', () => {
    const baseData = buildSampleExportData()
    const playtimeData = buildSampleExportData({
      inGameSeconds: 45000,
      inGameDuration: '12h 30m',
      transitInGameSeconds: 36000,
      transitInGameDuration: '10h 00m',
      inSystemInGameSeconds: 9000,
      inSystemInGameDuration: '2h 30m',
      rowDetails: baseData.rowDetails.map((detail) =>
        detail.rowId === 2 ? { ...detail, inGameSeconds: 11700, inGameDuration: '3h 15m' } : detail,
      ) as typeof baseData.rowDetails,
    })

    const cartItems = [
      expandCartItem(playtimeData, { kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'hide' }, 'sum'),
      expandCartItem(playtimeData, { kind: 'details', section: 'flight', scope: { kind: 'full' }, modeGrouping: 'combined', selectedMetrics: ['inGamePlaytimeBreakdown'] }, 'flight'),
      expandCartItem(playtimeData, { kind: 'waypoints', waypointsScope: 'visited', selectedColumns: ['duration', 'inGameDuration'] }, 'wp'),
    ]

    const html = generateSelfContainedHtmlReport(playtimeData, cartItems)

    expect(html).toContain('<div class="muted">ELAPSED: 4d · IN-GAME: 12h 30m</div>')
    expect(html).toContain('<span>In-Game Playtime</span><span class="mono amber">12h 30m</span>')
    expect(html).toContain('In Transit (Between Waypoints)')
    expect(html).toContain('10h 00m')
    expect(html).toContain('In System (At Waypoints)')
    expect(html).toContain('2h 30m')
    expect(html).toContain('<th>Duration</th><th>In-Game</th>')
    expect(html).toContain('<td class="mono small">3h 15m</td>')
  })
})
