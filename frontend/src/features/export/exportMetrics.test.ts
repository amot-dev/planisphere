import { describe, expect, it } from 'vitest'
import {
  computeDerivedMetrics,
  createEmptyMetrics,
  createEmptyRealization,
  createQuickShareCart,
  expandConfigToCartItems,
  getVisitedRegions,
  resolveTelemetryContext,
  scopeSupportsModeSplit,
} from './exportMetrics'
import { defaultMetricsForSection } from './exportTypes'
import { buildSampleExportData } from './exportTestFixtures'

describe('exportMetrics', () => {
  it('guards division by zero, sub-24h daily rates, and non-positive settled retention estimates', () => {
    const emptyDerived = computeDerivedMetrics(
      createEmptyMetrics(),
      createEmptyRealization(),
      null,
      0.5,
    )
    expect(emptyDerived.avgJumpRangeLy).toBeNull()
    expect(emptyDerived.boostRatePercent).toBeNull()
    expect(emptyDerived.avgDistancePerDayLy).toBeNull()
    expect(emptyDerived.avgCombinedCreditsPerDay).toBeNull()
    expect(emptyDerived.realizationRetentionPercent).toBeNull()
    expect(emptyDerived.netProfitAfterCarrierCost).toBeNull()

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
    expect(allLegItems[0].subfolder).toBe('by-leg')

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

    const quickShare = createQuickShareCart(data)
    expect(quickShare.map((item) => item.config.kind)).toEqual(['summary', 'highlights', 'waypoints'])
  })
})
