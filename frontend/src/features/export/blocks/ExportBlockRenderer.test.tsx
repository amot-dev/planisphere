import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { buildSampleExportData } from '../../../test/fixtures/exportFixtures'
import { defaultMetricsForSection, type ExportBlockConfig } from '../exportTypes'
import { buildHighlightsViewModel } from '../exportViewModels'
import { ExportBlockRenderer } from './ExportBlockRenderer'

describe('ExportBlockRenderer', () => {
  it('renders Summary with inline Career Diff and Details with empty-selection guard', () => {
    const data = buildSampleExportData()
    const summaryMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={data}
        config={{
          kind: 'summary',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          careerDiff: 'both',
        }}
      />,
    )
    expect(summaryMarkup).toContain('4,800.0 Ly')
    expect(summaryMarkup).toContain('PRE 10,000.0 Ly')
    expect(summaryMarkup).toContain('+48.0%')
    expect(summaryMarkup).toContain('Bodies Scanned')
    expect(summaryMarkup).toContain('Bodies Mapped')
    expect(summaryMarkup).toContain('Organic Samples')
    expect(summaryMarkup).toContain('NET PROFIT:')
    expect(summaryMarkup).toContain('~129,000,000 Cr')
    expect(summaryMarkup).toContain('~26,000,000 Cr')
    expect(summaryMarkup).toContain('ed-exp-grid-top')
    expect(summaryMarkup).toContain('ed-exp-grid-bottom')
    expect(summaryMarkup).toContain('ed-exp-strip-bottom')
    expect(summaryMarkup).not.toContain('ESTIMATED PROFIT:')
    expect(summaryMarkup).not.toContain('POST')
    expect(summaryMarkup).not.toContain('career')

    const emptyDetailsMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={data}
        config={{
          kind: 'details',
          section: 'flight',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          selectedMetrics: [],
        }}
      />,
    )
    expect(emptyDetailsMarkup).toContain('Select at least one applicable data point')

    const realizedMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={data}
        config={{
          kind: 'details',
          section: 'realized',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          selectedMetrics: defaultMetricsForSection('realized'),
        }}
      />,
    )
    expect(realizedMarkup).toContain('Net Profit After Carrier')
    expect(realizedMarkup).toContain('~129,000,000 Cr')
    expect(realizedMarkup).toContain('Carrier cost: ~26,000,000 Cr')
  })

  it('renders Waypoints with DEPARTED origin and IN SYSTEM state, and Highlights at Top 3', () => {
    const data = buildSampleExportData()
    const waypointsMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={data}
        config={{
          kind: 'waypoints',
          waypointsScope: 'fullPlan',
          selectedColumns: ['regionHeaders', 'timestamps', 'flightMode', 'notes'],
        }}
      />,
    )
    expect(waypointsMarkup).toContain('DEPARTED 3312-04-01 10:00 UTC')
    expect(waypointsMarkup).toContain('(IN SYSTEM)')
    expect(waypointsMarkup).toContain('IN PROGRESS')

    const highlightsTop3Markup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={data}
        config={{
          kind: 'highlights',
          depth: 3,
          selectedCards: ['mostLucrative', 'furthestFromSol'],
          lucrativeMetric: 'estimated',
          distanceMetric: 'total',
          durationMetric: 'elapsed',
        }}
      />,
    )
    expect(highlightsTop3Markup).toContain('TOP 3')
    expect(highlightsTop3Markup).toContain('#1 Leg #3')
    expect(highlightsTop3Markup).toContain('#3 Leg #2')
    expect(highlightsTop3Markup).toContain('[IN SYSTEM]')
  })

  it('prefixes estimated carrier costs with tilde and sanitizes malformed mode colors in React blocks', () => {
    const baseData = buildSampleExportData()
    const renderCard = (exportData: typeof baseData, config: ExportBlockConfig) =>
      renderToStaticMarkup(<ExportBlockRenderer exportData={exportData} config={config} />)

    const carrierEstimated = renderCard(baseData, {
      kind: 'details',
      section: 'carrier',
      scope: { kind: 'full' },
      modeGrouping: 'combined',
      selectedMetrics: ['carrierCosts'],
    })
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

    const summaryNonEstimated = renderCard(nonEstimatedAndUnsafeData, {
      kind: 'summary',
      scope: { kind: 'full' },
      modeGrouping: 'combined',
      careerDiff: 'hide',
    })
    expect(summaryNonEstimated).toContain('26,000,000 Cr')
    expect(summaryNonEstimated).not.toContain('~26,000,000 Cr')

    const realizedNonEstimated = renderCard(nonEstimatedAndUnsafeData, {
      kind: 'details',
      section: 'realized',
      scope: { kind: 'full' },
      modeGrouping: 'combined',
      selectedMetrics: ['netProfitAfterCarrierCost'],
    })
    expect(realizedNonEstimated).toContain('Carrier cost: 26,000,000 Cr')
    expect(realizedNonEstimated).not.toContain('~26,000,000 Cr')
    expect(realizedNonEstimated).toContain('ed-exp-amber">129,000,000 Cr')
    expect(realizedNonEstimated).not.toContain('~129,000,000 Cr')

    const summaryUnsafeColor = renderCard(nonEstimatedAndUnsafeData, {
      kind: 'summary',
      scope: { kind: 'full' },
      modeGrouping: 'separate',
      selectedModeId: 1,
      careerDiff: 'hide',
    })
    expect(summaryUnsafeColor).not.toContain('#12;x:1')
    expect(summaryUnsafeColor).toContain('#FF7100')

    const detailsUnsafeColor = renderCard(nonEstimatedAndUnsafeData, {
      kind: 'details',
      section: 'flight',
      scope: { kind: 'full' },
      modeGrouping: 'separate',
      selectedModeId: 1,
      selectedMetrics: ['jumps'],
    })
    expect(detailsUnsafeColor).not.toContain('#12;x:1')
    expect(detailsUnsafeColor).toContain('#FF7100')

    const waypointsUnsafeColor = renderCard(nonEstimatedAndUnsafeData, {
      kind: 'waypoints',
      waypointsScope: 'visited',
      selectedColumns: ['flightMode'],
    })
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
      durationMetric: 'elapsed',
    })
    const sanitizedEntryColors = highlightsUnsafeViewModel.boards[0].entries.map(
      (entry) => entry.modeColor,
    )
    expect(sanitizedEntryColors).not.toContain('#zzzzzz')
    expect(sanitizedEntryColors).not.toContain('red;bad:1')
    expect(sanitizedEntryColors).toContain('#FF7100')
  })

  it('renders ELAPSED and IN-GAME headers, 2-row In-Game Playtime card, and Waypoints In-Game column', () => {
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

    const summaryMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={playtimeData}
        config={{ kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'hide' }}
      />,
    )
    expect(summaryMarkup).toContain('ELAPSED: 4d · IN-GAME: 12h 30m')

    const inGameOnlyMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={buildSampleExportData({
          duration: '',
          effectiveStart: '',
          effectiveEnd: '',
          inGameSeconds: 45000,
          inGameDuration: '12h 30m',
        })}
        config={{ kind: 'summary', scope: { kind: 'full' }, modeGrouping: 'combined', careerDiff: 'hide' }}
      />,
    )
    expect(inGameOnlyMarkup).toContain('IN-GAME: 12h 30m')
    expect(inGameOnlyMarkup).not.toContain('ELAPSED:')

    const flightMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={playtimeData}
        config={{
          kind: 'details',
          section: 'flight',
          scope: { kind: 'full' },
          modeGrouping: 'combined',
          selectedMetrics: ['inGamePlaytimeBreakdown'],
        }}
      />,
    )
    expect(flightMarkup).toContain('ELAPSED: 4d · IN-GAME: 12h 30m')
    expect(flightMarkup).toContain('In-Game Playtime')
    expect(flightMarkup).toContain('12h 30m')
    expect(flightMarkup).toContain('In Transit (Between Waypoints)')
    expect(flightMarkup).toContain('10h 00m')
    expect(flightMarkup).toContain('In System (At Waypoints)')
    expect(flightMarkup).toContain('2h 30m')

    const baselineNoPlaytimeCard = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={playtimeData}
        config={{
          kind: 'details',
          section: 'flight',
          scope: { kind: 'baseline' },
          modeGrouping: 'combined',
          selectedMetrics: ['inGamePlaytimeBreakdown'],
        }}
      />,
    )
    expect(baselineNoPlaytimeCard).toContain('Select at least one applicable data point')

    const waypointsMarkup = renderToStaticMarkup(
      <ExportBlockRenderer
        exportData={playtimeData}
        config={{
          kind: 'waypoints',
          waypointsScope: 'visited',
          selectedColumns: ['duration', 'inGameDuration'],
        }}
      />,
    )
    expect(waypointsMarkup).toContain('<th>Duration</th><th>In-Game</th>')
    expect(waypointsMarkup).toContain('1d 02h 00m')
    expect(waypointsMarkup).toContain('3h 15m')
  })
})
