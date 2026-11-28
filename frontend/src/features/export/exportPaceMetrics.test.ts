import { describe, expect, it } from 'vitest'
import { buildSampleExportData } from '../../test/fixtures/exportFixtures'
import {
  buildDetailsViewModel,
  formatOptionalCreditsPerHour,
  formatOptionalLyPerHour,
  formatOptionalRatePerHour,
} from './exportDetailsViewModel'
import {
  createQuickShareCart,
  isDetailsMetricAvailable,
  resolveTelemetryContext,
} from './exportMetrics'
import {
  createDefaultBlockConfig,
  DETAILS_SECTION_METRICS,
  DetailsSection,
  defaultMetricsForSection,
} from './exportTypes'

const HOURLY_METRICS_BY_SECTION: ReadonlyArray<{
  section: DetailsSection
  metricIds: ReadonlyArray<string>
}> = [
  { section: 'flight', metricIds: ['avgDistancePerHour', 'avgJumpsPerHour'] },
  { section: 'explorationValue', metricIds: ['avgCombinedCreditsPerHour'] },
  { section: 'cartographics', metricIds: ['avgCartoCreditsPerHour', 'avgBodiesScannedPerHour'] },
  { section: 'exobiology', metricIds: ['avgExoCreditsPerHour', 'avgOrganicSamplesPerHour'] },
  { section: 'realized', metricIds: ['avgRealizedCreditsPerHour'] },
]

describe('exportPaceMetrics', () => {
  it('enforces the 1-hour in-game floor (3599s vs 3600s) across all eight hourly Details metrics', () => {
    const subHourData = buildSampleExportData({ inGameSeconds: 3599, inGameDuration: '59m' })
    const exactHourData = buildSampleExportData({ inGameSeconds: 3600, inGameDuration: '1h 00m' })
    const missingPlaytimeData = buildSampleExportData()

    const subHourContext = resolveTelemetryContext(subHourData, { kind: 'full' })
    const exactHourContext = resolveTelemetryContext(exactHourData, { kind: 'full' })
    const missingContext = resolveTelemetryContext(missingPlaytimeData, { kind: 'full' })

    for (const { section, metricIds } of HOURLY_METRICS_BY_SECTION) {
      for (const metricId of metricIds) {
        const descriptor = DETAILS_SECTION_METRICS[section].find((item) => item.id === metricId)!
        expect(descriptor.requiresInGame1h).toBe(true)
        expect(isDetailsMetricAvailable(descriptor, missingContext, 'full')).toBe(false)
        expect(isDetailsMetricAvailable(descriptor, subHourContext, 'full')).toBe(false)
        expect(isDetailsMetricAvailable(descriptor, exactHourContext, 'full')).toBe(true)
      }
    }
  })

  it('formats all eight hourly Details KPI tiles with exact units, tones, and null placeholders', () => {
    expect(formatOptionalLyPerHour(null)).toBe('—')
    expect(formatOptionalRatePerHour(null)).toBe('—')
    expect(formatOptionalCreditsPerHour(null)).toBe('—')

    // 4h in-game (14400s) against sample expedition totals:
    // 4,800 Ly -> 1,200.0 Ly/h, 80 jumps -> 20.0 /h, 172M combined -> 43,000,000 Cr/h,
    // 42M carto -> 10,500,000 Cr/h, 115 bodies -> 28.8 /h, 130M exo -> 32,500,000 Cr/h,
    // 13 samples -> 3.3 /h, 155M realized -> 38,750,000 Cr/h.
    const fourHourData = buildSampleExportData({ inGameSeconds: 14_400, inGameDuration: '4h 00m' })

    const tilesById = new Map<string, { label: string; value: string; valueTone: string }>()
    for (const { section } of HOURLY_METRICS_BY_SECTION) {
      const viewModel = buildDetailsViewModel(fourHourData, {
        kind: 'details',
        section,
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection(section),
      })
      for (const tile of viewModel.kpiTiles) {
        tilesById.set(tile.id, { label: tile.label, value: tile.value, valueTone: tile.valueTone })
      }
    }

    expect(tilesById.get('avgDistancePerHour')).toEqual({
      label: 'Avg Distance / Hr',
      value: '1,200.0 Ly/h',
      valueTone: 'main',
    })
    expect(tilesById.get('avgJumpsPerHour')).toEqual({
      label: 'Avg Jumps / Hr',
      value: '20.0 /h',
      valueTone: 'main',
    })
    expect(tilesById.get('avgCombinedCreditsPerHour')).toEqual({
      label: 'Avg Credits / Hr',
      value: '43,000,000 Cr/h',
      valueTone: 'cyan',
    })
    expect(tilesById.get('avgCartoCreditsPerHour')).toEqual({
      label: 'Avg Carto Cr / Hr',
      value: '10,500,000 Cr/h',
      valueTone: 'cyan',
    })
    expect(tilesById.get('avgBodiesScannedPerHour')).toEqual({
      label: 'Avg Bodies / Hr',
      value: '28.8 /h',
      valueTone: 'cyan',
    })
    expect(tilesById.get('avgExoCreditsPerHour')).toEqual({
      label: 'Avg Exo Cr / Hr',
      value: '32,500,000 Cr/h',
      valueTone: 'emerald',
    })
    expect(tilesById.get('avgOrganicSamplesPerHour')).toEqual({
      label: 'Avg Samples / Hr',
      value: '3.3 /h',
      valueTone: 'emerald',
    })
    expect(tilesById.get('avgRealizedCreditsPerHour')).toEqual({
      label: 'Avg Realized Cr / Hr',
      value: '38,750,000 Cr/h',
      valueTone: 'amber',
    })
  })

  it('defaults Highlights durationMetric and Quick Share Highlights from durationDisplayMode', () => {
    const inGameDefaults = createDefaultBlockConfig('highlights', 'in_game')
    expect(inGameDefaults.kind === 'highlights' && inGameDefaults.durationMetric).toBe('inGame')
    expect(inGameDefaults.kind === 'highlights' && inGameDefaults.selectedCards).not.toContain('highestEarningPace')
    expect(inGameDefaults.kind === 'highlights' && inGameDefaults.selectedCards).not.toContain('fastestTransitPace')

    const calendarDefaults = createDefaultBlockConfig('highlights', 'calendar')
    expect(calendarDefaults.kind === 'highlights' && calendarDefaults.durationMetric).toBe('elapsed')

    const data = buildSampleExportData()
    const inGameQuickShare = createQuickShareCart(data, 'in_game')
    const highlightsItem = inGameQuickShare.find((item) => item.config.kind === 'highlights')
    expect(highlightsItem?.config.kind === 'highlights' && highlightsItem.config.durationMetric).toBe('inGame')
  })
})
