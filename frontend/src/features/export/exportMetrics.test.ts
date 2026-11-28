import { describe, expect, it } from 'vitest'
import {
  computeAvailableDetailsMetricIds,
  createQuickShareCart,
  describeScope,
  expandConfigToCartItems,
  formatSignedEstimatedCredits,
  getDistinctModesForScope,
  getExportDataIndex,
  getVisitedLegByRowId,
  getVisitedLegs,
  getVisitedRegions,
  isDetailsMetricAvailable,
  resolveFormattedNetProfit,
  resolveTelemetryContext,
  scopeSupportsModeSplit,
} from './exportMetrics'
import { CartBlockItem, createDefaultBlockConfig, DETAILS_SECTION_METRICS, defaultMetricsForSection } from './exportTypes'
import { buildSampleExportData } from '../../test/fixtures/exportFixtures'

describe('exportMetrics', () => {
  it('resolves Go-computed derived metrics, baseline fallbacks, and region buckets across scopes', () => {
    const data = buildSampleExportData()
    const fullContext = resolveTelemetryContext(data, { kind: 'full' })
    expect(fullContext.derived.avgJumpRangeLy).toBeCloseTo(4600 / 80, 4)
    expect(fullContext.derived.avgCarrierJumpRangeLy).toBeCloseTo(450, 4)
    expect(fullContext.derived.avgTritiumPerJump).toBeCloseTo(100, 4)
    expect(fullContext.derived.netProfitAfterCarrierCost).toBe(155_000_000 - 26_000_000)
    expect(fullContext.derived.realizationRetentionPercent).toBeCloseTo(
      (155_000_000 / (172_000_000 - 12_000_000)) * 100,
      4,
    )
    expect(fullContext.derived.avgDistancePerDayLy).not.toBeNull()

    const baselineContext = resolveTelemetryContext(data, { kind: 'baseline' })
    expect(baselineContext.derived.avgJumpRangeLy).toBeCloseTo(50, 4)
    expect(baselineContext.derived.netProfitAfterCarrierCost).toBeNull()

    const missingBaselineData = buildSampleExportData({ originBaseline: null })
    const emptyBaselineContext = resolveTelemetryContext(missingBaselineData, { kind: 'baseline' })
    expect(emptyBaselineContext.elapsedDays).toBeNull()
    expect(emptyBaselineContext.derived.avgJumpRangeLy).toBeNull()
    expect(emptyBaselineContext.derived.realizationRetentionPercent).toBeNull()

    const regionCombined = resolveTelemetryContext(data, {
      kind: 'region',
      regionName: 'Inner Orion Spur',
    })
    expect(regionCombined.visitedWaypoints).toBe(2)
    expect(regionCombined.coveredLy).toBe(4000)
    expect(regionCombined.shipsFlown).toEqual(['Mandalay', 'Anaconda'])
    expect(regionCombined.derived.netProfitAfterCarrierCost).toBeNull()

    const regionExploration = resolveTelemetryContext(
      data,
      { kind: 'region', regionName: 'Inner Orion Spur' },
      1,
    )
    expect(regionExploration.modeBadge?.modeName).toBe('Exploration')
    expect(regionExploration.visitedWaypoints).toBe(1)
    expect(regionExploration.coveredLy).toBe(1000)
    expect(regionExploration.shipsFlown).toEqual(['Mandalay'])
  })

  it('shows Combined/Separate mode split strictly for scopes spanning 2+ modes and never for legs or carrier', () => {
    const data = buildSampleExportData()
    expect(scopeSupportsModeSplit(data, { kind: 'full' })).toBe(true)
    expect(scopeSupportsModeSplit(data, { kind: 'full' }, 'carrier')).toBe(false)
    expect(scopeSupportsModeSplit(data, { kind: 'leg', rowId: 2 })).toBe(false)
    expect(scopeSupportsModeSplit(data, { kind: 'allLegs' })).toBe(false)
    expect(scopeSupportsModeSplit(data, { kind: 'baseline' })).toBe(false)
    // Inner Orion Spur has both Exploration and Travel legs; Odin's Hold has only Exploration.
    expect(scopeSupportsModeSplit(data, { kind: 'region', regionName: 'Inner Orion Spur' })).toBe(true)
    expect(scopeSupportsModeSplit(data, { kind: 'region', regionName: "Odin's Hold" })).toBe(false)
  })

  it('restricts Career Diff and Carrier Cost to Full Expedition Combined and surfaces IN SYSTEM on parked legs', () => {
    const data = buildSampleExportData()
    const combinedFull = resolveTelemetryContext(data, { kind: 'full' })
    expect(combinedFull.careerDiff).not.toBeNull()
    expect(combinedFull.carrierCost).not.toBeNull()
    expect(combinedFull.distinctRegionCount).toBe(2)
    expect(combinedFull.shipsFlown).toEqual(['Mandalay', 'Anaconda'])

    const separateMode = resolveTelemetryContext(data, { kind: 'full' }, 1)
    expect(separateMode.careerDiff).toBeNull()
    expect(separateMode.carrierCost).toBeNull()
    expect(separateMode.modeBadge?.modeName).toBe('Exploration')
    expect(separateMode.shipsFlown).toEqual(['Mandalay'])

    const fixedNowMs = Date.parse('2026-04-05T12:00:00Z')
    const parkedLeg = resolveTelemetryContext(data, { kind: 'leg', rowId: 4 }, undefined, fixedNowMs)
    expect(parkedLeg.inSystem).toBe(true)
    expect(parkedLeg.toGalactic).toBe('IN SYSTEM')
    expect(parkedLeg.modeBadge?.modeName).toBe('Exploration')
    expect(parkedLeg.derived.avgDistancePerDayLy).toBeNull()
  })

  it('expands All Legs, All Regions with Separate modes, and Quick Share preset deterministically', () => {
    const data = buildSampleExportData()
    expect(getVisitedRegions(data).map((region) => region.regionName)).toEqual([
      'Inner Orion Spur',
      "Odin's Hold",
    ])

    const allLegItems = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'flight',
        scope: { kind: 'allLegs' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection('flight'),
      },
      'batch',
    )
    expect(allLegItems).toHaveLength(3)
    expect(allLegItems[0].filenameSlug).toBe('wp2-flight')

    const allRegionSeparateItems = expandConfigToCartItems(
      data,
      {
        kind: 'summary',
        scope: { kind: 'allRegions' },
        modeGrouping: 'separate',
        careerDiff: 'percentage',
      },
      'reg',
    )
    // Inner Orion Spur expands into 2 modes; Odin's Hold stays 1 item (only 1 mode present).
    expect(allRegionSeparateItems).toHaveLength(3)
    expect(allRegionSeparateItems[0].filenameSlug).toBe('inner-orion-spur-exploration-summary')

    const quickShare = createQuickShareCart(data)
    expect(quickShare.map((item) => item.config.kind)).toEqual(['summary', 'highlights', 'waypoints'])
    expect(quickShare[0].filenameSlug).toBe('full-expedition-summary')

    const allFullSections = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'all',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
      },
      'all-sec',
    )
    expect(allFullSections).toHaveLength(6)
    expect(
      allFullSections.map((item) =>
        item.config.kind === 'details' ? item.config.section : item.config.kind,
      ),
    ).toEqual(['flight', 'carrier', 'explorationValue', 'cartographics', 'exobiology', 'realized'])

    const allLegSections = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'all',
        scope: { kind: 'leg', rowId: 2 },
        modeGrouping: 'combined',
      },
      'leg-sec',
    )
    // Carrier Operations is Full Expedition only, so leg scope expands to the 5 applicable sections.
    expect(allLegSections).toHaveLength(5)

    const allLegsAllSections = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'all',
        scope: { kind: 'allLegs' },
        modeGrouping: 'combined',
      },
      'all-legs-sec',
    )
    // 3 visited legs x 5 applicable sections, grouped entity-outer and section-inner.
    expect(allLegsAllSections).toHaveLength(15)
    expect(allLegsAllSections.slice(0, 6).map((item) => item.filenameSlug)).toEqual([
      'wp2-flight',
      'wp2-exploration-value',
      'wp2-cartographics',
      'wp2-exobiology',
      'wp2-realized',
      'wp3-flight',
    ])

    const shipOnlyData = buildSampleExportData({
      expedition: {
        metrics: {
          ...data.expedition.metrics,
          carrierJumps: 0,
          carrierDistanceLy: 0,
          tritiumUsed: 0,
        },
        realization: data.expedition.realization,
      },
      carrierCost: {
        ...data.carrierCost,
        carrierType: 'none',
        weeklyUpkeep: 0,
        tritiumPrice: 0,
        tritiumCost: 0,
        jumpCost: 0,
        upkeepCost: 0,
        totalCost: 0,
        totalEstimated: false,
      },
    })
    const shipOnlyAllSections = expandConfigToCartItems(
      shipOnlyData,
      {
        kind: 'details',
        section: 'all',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
      },
      'ship-only-sec',
    )
    expect(
      shipOnlyAllSections.map((item) =>
        item.config.kind === 'details' ? item.config.section : item.config.kind,
      ),
    ).toEqual(['flight', 'explorationValue', 'cartographics', 'exobiology', 'realized'])

    const fullSeparateAllSections = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'all',
        scope: { kind: 'full' },
        modeGrouping: 'separate',
      },
      'full-sep-sec',
    )
    // Multi-mode Separate omits carrier and groups all 5 sections per mode before advancing.
    expect(fullSeparateAllSections).toHaveLength(10)
    expect(fullSeparateAllSections.slice(0, 6).map((item) => item.filenameSlug)).toEqual([
      'full-expedition-exploration-flight',
      'full-expedition-exploration-exploration-value',
      'full-expedition-exploration-cartographics',
      'full-expedition-exploration-exobiology',
      'full-expedition-exploration-realized',
      'full-expedition-travel-flight',
    ])

    const allRegionsSeparateSections = expandConfigToCartItems(
      data,
      {
        kind: 'details',
        section: 'all',
        scope: { kind: 'allRegions' },
        modeGrouping: 'separate',
      },
      'reg-sep-sec',
    )
    expect(allRegionsSeparateSections).toHaveLength(15)
    expect(allRegionsSeparateSections.slice(0, 6).map((item) => item.filenameSlug)).toEqual([
      'inner-orion-spur-exploration-flight',
      'inner-orion-spur-exploration-exploration-value',
      'inner-orion-spur-exploration-cartographics',
      'inner-orion-spur-exploration-exobiology',
      'inner-orion-spur-exploration-realized',
      'inner-orion-spur-travel-flight',
    ])
    expect(allRegionsSeparateSections[10].filenameSlug).toBe('odin-s-hold-flight')
  })

  it('formats signed estimated credits and resolves Full Expedition Combined net profit safely', () => {
    expect(formatSignedEstimatedCredits(26_000_000, false)).toBe('26,000,000 Cr')
    expect(formatSignedEstimatedCredits(0, false)).toBe('0 Cr')
    expect(formatSignedEstimatedCredits(26_000_000, true)).toBe('~26,000,000 Cr')
    expect(formatSignedEstimatedCredits(-26_000_000, false)).toBe('-26,000,000 Cr')
    expect(formatSignedEstimatedCredits(-26_000_000, true)).toBe('-~26,000,000 Cr')
    expect(formatSignedEstimatedCredits(-0.4, false)).toBe('0 Cr')
    expect(formatSignedEstimatedCredits(-0.4, true)).toBe('~0 Cr')
    expect(formatSignedEstimatedCredits(-0.4, true)).not.toContain('-')

    const baseData = buildSampleExportData()
    const positiveEstimatedContext = resolveTelemetryContext(baseData, { kind: 'full' })
    expect(resolveFormattedNetProfit(positiveEstimatedContext)).toEqual({
      text: '~129,000,000 Cr',
      isNegative: false,
    })

    const negativeEstimatedData = buildSampleExportData({
      expedition: {
        metrics: baseData.expedition.metrics,
        realization: { ...baseData.expedition.realization, totalRealized: 10_000_000 },
      },
    })
    expect(
      resolveFormattedNetProfit(resolveTelemetryContext(negativeEstimatedData, { kind: 'full' })),
    ).toEqual({
      text: '-~16,000,000 Cr',
      isNegative: true,
    })

    const negativeExactData = buildSampleExportData({
      expedition: {
        metrics: baseData.expedition.metrics,
        realization: { ...baseData.expedition.realization, totalRealized: 10_000_000 },
      },
      carrierCost: { ...baseData.carrierCost, totalEstimated: false },
    })
    expect(
      resolveFormattedNetProfit(resolveTelemetryContext(negativeExactData, { kind: 'full' })),
    ).toEqual({
      text: '-16,000,000 Cr',
      isNegative: true,
    })

    const roundingToZeroData = buildSampleExportData({
      expedition: {
        metrics: baseData.expedition.metrics,
        realization: { ...baseData.expedition.realization, totalRealized: 25_999_999.6 },
      },
      carrierCost: { ...baseData.carrierCost, totalEstimated: false },
    })
    expect(
      resolveFormattedNetProfit(resolveTelemetryContext(roundingToZeroData, { kind: 'full' })),
    ).toEqual({
      text: '0 Cr',
      isNegative: false,
    })

    const nonCombinedScopes = [
      resolveTelemetryContext(baseData, { kind: 'full' }, 1),
      resolveTelemetryContext(baseData, { kind: 'baseline' }),
      resolveTelemetryContext(baseData, { kind: 'leg', rowId: 2 }),
      resolveTelemetryContext(baseData, { kind: 'region', regionName: 'Inner Orion Spur' }),
    ]
    for (const scopeContext of nonCombinedScopes) {
      expect(resolveFormattedNetProfit(scopeContext)).toBeNull()
    }

    const shipOnlyMetrics = {
      ...baseData.expedition.metrics,
      carrierJumps: 0,
      carrierDistanceLy: 0,
      tritiumUsed: 0,
    }
    const shipOnlyNoneCarrierData = buildSampleExportData({
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
    })
    const shipOnlyNullCarrierData = buildSampleExportData({
      expedition: { metrics: shipOnlyMetrics, realization: baseData.expedition.realization },
      carrierCost: null as unknown as typeof baseData.carrierCost,
    })
    const nullCarrierContext = resolveTelemetryContext(shipOnlyNullCarrierData, { kind: 'full' })
    expect(nullCarrierContext.derived.netProfitAfterCarrierCost).toBeNull()
    for (const shipOnlyData of [shipOnlyNoneCarrierData, shipOnlyNullCarrierData]) {
      expect(resolveFormattedNetProfit(resolveTelemetryContext(shipOnlyData, { kind: 'full' }))).toEqual({
        text: '155,000,000 Cr',
        isNegative: false,
      })
    }
  })

  it('caches ExportDataIndex in a WeakMap per exportData reference and early-exits achievable Details metrics', () => {
    const data = buildSampleExportData()
    expect(getExportDataIndex(data)).toBe(getExportDataIndex(data))
    expect(getVisitedLegs(data)).toBe(getVisitedLegs(data))
    expect(getVisitedRegions(data)).toBe(getVisitedRegions(data))
    expect(getDistinctModesForScope(data, { kind: 'full' })).toBe(
      getDistinctModesForScope(data, { kind: 'full' }),
    )
    expect(getVisitedLegByRowId(data, 3)?.rowView.row.systemName).toBe('Beta Crossing')
    expect(describeScope(data, { kind: 'leg', rowId: 3 })).toEqual({
      label: 'Leg #2 to #3',
      slug: 'wp3',
    })

    // Flight section has 11 descriptors, 1 of which (distinctRegions) is fullExpeditionCombinedOnly.
    // On allLegs, Slice 0 (>= 24h + 2h playtime) unlocks all 10 achievable metrics so Slice 1 rowId is never resolved.
    const dataWithPlaytime = buildSampleExportData({
      rowDetails: data.rowDetails.map((detail) =>
        detail.rowId === 2
          ? { ...detail, inGameSeconds: 7200, inGameDuration: '2h 00m', transitInGameSeconds: 5400, transitInGameDuration: '1h 30m', inSystemInGameSeconds: 1800, inSystemInGameDuration: '30m' }
          : detail,
      ) as typeof data.rowDetails,
    })
    let slice1RowIdReads = 0
    const slice1Scope = {
      kind: 'leg' as const,
      get rowId() {
        slice1RowIdReads++
        return 3
      },
    }
    const syntheticItems: CartBlockItem[] = [
      {
        id: 'leg-0',
        title: 'Leg 1',
        subtitle: '',
        filenameSlug: 'wp2-flight',
        config: {
          kind: 'details',
          section: 'flight',
          scope: { kind: 'leg', rowId: 2 },
          modeGrouping: 'combined',
          selectedMetrics: defaultMetricsForSection('flight'),
        },
      },
      {
        id: 'leg-1',
        title: 'Leg 2',
        subtitle: '',
        filenameSlug: 'wp3-flight',
        config: {
          kind: 'details',
          section: 'flight',
          scope: slice1Scope,
          modeGrouping: 'combined',
          selectedMetrics: defaultMetricsForSection('flight'),
        },
      },
    ]

    const available = computeAvailableDetailsMetricIds(
      dataWithPlaytime,
      {
        kind: 'details',
        section: 'flight',
        scope: { kind: 'allLegs' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection('flight'),
      },
      syntheticItems,
      Date.parse('2026-04-05T12:00:00Z'),
    )
    expect(available?.size).toBe(10)
    expect(available?.has('distinctRegions')).toBe(false)
    expect(available?.has('inGamePlaytimeBreakdown')).toBe(true)
    expect(available?.has('avgDistancePerDay')).toBe(true)
    expect(slice1RowIdReads).toBe(0)
  })

  it('resolves playtime context, gates inGamePlaytimeBreakdown, and aligns Waypoints default columns with durationDisplayMode', () => {
    const calendarDefaults = createDefaultBlockConfig('waypoints', 'calendar')
    const inGameDefaults = createDefaultBlockConfig('waypoints', 'in_game')
    expect(calendarDefaults.kind === 'waypoints' && calendarDefaults.selectedColumns).toContain('duration')
    expect(calendarDefaults.kind === 'waypoints' && calendarDefaults.selectedColumns).not.toContain('inGameDuration')
    expect(inGameDefaults.kind === 'waypoints' && inGameDefaults.selectedColumns).toContain('inGameDuration')
    expect(inGameDefaults.kind === 'waypoints' && inGameDefaults.selectedColumns).not.toContain('duration')

    const playtimeDescriptor = DETAILS_SECTION_METRICS.flight.find((metric) => metric.id === 'inGamePlaytimeBreakdown')!
    const baseData = buildSampleExportData()
    expect(isDetailsMetricAvailable(playtimeDescriptor, resolveTelemetryContext(baseData, { kind: 'full' }), 'full')).toBe(false)

    const withPlaytime = buildSampleExportData({
      inGameSeconds: 45000, inGameDuration: '12h 30m', transitInGameSeconds: 36000, transitInGameDuration: '10h 00m', inSystemInGameSeconds: 9000, inSystemInGameDuration: '2h 30m',
      originBaseline: { ...baseData.originBaseline!, inGameSeconds: 360000, inGameDuration: '100h 00m' } as typeof baseData.originBaseline,
    })
    const fullContext = resolveTelemetryContext(withPlaytime, { kind: 'full' })
    expect(fullContext.inGameDurationLabel).toBe('12h 30m')
    expect(fullContext.transitInGameDurationLabel).toBe('10h 00m')
    expect(fullContext.inSystemInGameDurationLabel).toBe('2h 30m')
    expect(isDetailsMetricAvailable(playtimeDescriptor, fullContext, 'full')).toBe(true)
    const baselineContext = resolveTelemetryContext(withPlaytime, { kind: 'baseline' })
    expect(baselineContext.inGameDurationLabel).toBe('100h 00m')
    expect(isDetailsMetricAvailable(playtimeDescriptor, baselineContext, 'baseline')).toBe(false)
  })
})
