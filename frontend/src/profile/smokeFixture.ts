import type { LiveFrontendFixture } from './profileHelpers'

const ZERO_SMOKE_METRICS = {
  distanceLy: 0, shipDistanceLy: 0, carrierPassengerLy: 0, jumps: 0,
  carrierJumps: 0, carrierDistanceLy: 0, tritiumUsed: undefined as number | undefined,
  jetConeBoosts: 0, premiumBoosts: 0, standardBoosts: 0, basicBoosts: 0,
  bodiesScanned: 0, bodiesMapped: 0, efficientMappings: 0,
  scanValueTotal: 0, scanValueBase: 0, scanValueBonus: 0, scanValueEfficiencyBonus: 0,
  firstDiscoveries: 0, firstMapped: 0, organicSamples: 0,
  organicValueTotal: 0, organicValueBase: 0, organicValueBonus: 0, firstLogged: 0,
}

const ZERO_SMOKE_REALIZATION = {
  totalRealized: 0, cartoLostDeath: 0, organicLostDeath: 0, cartoStolen: 0,
  organicStolen: 0, carrierTariffLoss: 0, npcCrewLoss: 0, pendingValue: 0,
}

function makeSmokeDerived(
  metrics: typeof ZERO_SMOKE_METRICS,
  realization: typeof ZERO_SMOKE_REALIZATION,
  totalCarrierCost: number | null,
  elapsedDays: number | null,
  inGameSeconds: number | null = null,
): Record<string, unknown> {
  const div = (n: number, d: number) => (d > 0 ? n / d : null)
  const pct = (n: number, d: number) => (d > 0 ? (n / d) * 100 : null)
  const perK = (n: number, ly: number) => (ly > 0 ? n / (ly / 1000) : null)
  const combined = metrics.scanValueTotal + metrics.organicValueTotal
  const settled = combined - realization.pendingValue
  const validDays = elapsedDays != null && elapsedDays >= 1 ? elapsedDays : null
  const validHours = inGameSeconds != null && inGameSeconds >= 3600 ? inGameSeconds / 3600 : null
  return {
    shipDistanceLy: metrics.shipDistanceLy,
    totalBoosts: 0,
    avgJumpRangeLy: div(metrics.shipDistanceLy, metrics.jumps),
    boostRatePercent: pct(0, metrics.jumps),
    avgDistancePerDayLy: validDays != null ? metrics.distanceLy / validDays : null,
    avgDistancePerHourLy: validHours != null ? metrics.distanceLy / validHours : null,
    avgJumpsPerHour: validHours != null ? metrics.jumps / validHours : null,
    avgCarrierJumpRangeLy: div(metrics.carrierDistanceLy, metrics.carrierJumps),
    avgTritiumPerJump: metrics.tritiumUsed != null ? div(metrics.tritiumUsed, metrics.carrierJumps) : null,
    combinedEstimatedCredits: combined,
    dssEfficiencyPercent: pct(metrics.efficientMappings, metrics.bodiesMapped),
    firstDiscoveryRatePercent: pct(metrics.firstDiscoveries, metrics.bodiesScanned),
    firstLoggedRatePercent: pct(metrics.firstLogged, metrics.organicSamples),
    avgCreditsPerSample: div(metrics.organicValueTotal, metrics.organicSamples),
    avgCombinedCreditsPerJump: div(combined, metrics.jumps),
    avgCombinedCreditsPer1000Ly: perK(combined, metrics.distanceLy),
    avgCombinedCreditsPerDay: validDays != null ? combined / validDays : null,
    avgCombinedCreditsPerHour: validHours != null ? combined / validHours : null,
    avgCartoCreditsPerJump: div(metrics.scanValueTotal, metrics.jumps),
    avgCartoCreditsPer1000Ly: perK(metrics.scanValueTotal, metrics.distanceLy),
    avgCartoCreditsPerHour: validHours != null ? metrics.scanValueTotal / validHours : null,
    avgBodiesScannedPerHour: validHours != null ? metrics.bodiesScanned / validHours : null,
    avgExoCreditsPerJump: div(metrics.organicValueTotal, metrics.jumps),
    avgExoCreditsPer1000Ly: perK(metrics.organicValueTotal, metrics.distanceLy),
    avgExoCreditsPerHour: validHours != null ? metrics.organicValueTotal / validHours : null,
    avgOrganicSamplesPerHour: validHours != null ? metrics.organicSamples / validHours : null,
    totalLossesCredits: 0,
    realizationRetentionPercent: settled > 0 ? (realization.totalRealized / settled) * 100 : null,
    netProfitAfterCarrierCost: totalCarrierCost != null ? realization.totalRealized - totalCarrierCost : null,
    avgRealizedCreditsPerJump: div(realization.totalRealized, metrics.jumps),
    avgRealizedCreditsPer1000Ly: perK(realization.totalRealized, metrics.distanceLy),
    avgRealizedCreditsPerHour: validHours != null ? realization.totalRealized / validHours : null,
  }
}

function buildSmokeExportData(): Record<string, unknown> {
  const originMetrics = { ...ZERO_SMOKE_METRICS, distanceLy: 1000, shipDistanceLy: 1000, jumps: 40, bodiesScanned: 50, scanValueTotal: 5_000_000 }
  const originRealization = { ...ZERO_SMOKE_REALIZATION, totalRealized: 4_500_000 }
  const leg2Metrics = {
    ...ZERO_SMOKE_METRICS, distanceLy: 4.24, shipDistanceLy: 4.24, jumps: 1, bodiesScanned: 8, bodiesMapped: 4, efficientMappings: 4,
    scanValueTotal: 1_200_000, scanValueBase: 600_000, scanValueBonus: 450_000, scanValueEfficiencyBonus: 150_000,
    firstDiscoveries: 6, firstMapped: 3, organicSamples: 2, organicValueTotal: 19_010_800, organicValueBase: 3_802_160, organicValueBonus: 15_208_640, firstLogged: 2,
  }
  const leg2Realization = { ...ZERO_SMOKE_REALIZATION, totalRealized: 20_210_800 }
  const leg3Metrics = {
    ...ZERO_SMOKE_METRICS, distanceLy: 6.1, shipDistanceLy: 6.1, jumps: 1, bodiesScanned: 4, bodiesMapped: 2, efficientMappings: 2,
    scanValueTotal: 800_000, scanValueBase: 500_000, scanValueBonus: 200_000, scanValueEfficiencyBonus: 100_000, firstDiscoveries: 2, firstMapped: 1,
  }
  const leg3Realization = { ...ZERO_SMOKE_REALIZATION, pendingValue: 800_000 }
  const liveMetrics = {
    ...ZERO_SMOKE_METRICS, distanceLy: 10.34, shipDistanceLy: 10.34, jumps: 2, bodiesScanned: 12, bodiesMapped: 6, efficientMappings: 6,
    scanValueTotal: 2_000_000, scanValueBase: 1_100_000, scanValueBonus: 650_000, scanValueEfficiencyBonus: 250_000,
    firstDiscoveries: 8, firstMapped: 4, organicSamples: 2, organicValueTotal: 19_010_800, organicValueBase: 3_802_160, organicValueBonus: 15_208_640, firstLogged: 2,
  }
  const adjustments = { ...ZERO_SMOKE_METRICS, carrierJumps: 1, carrierDistanceLy: 498.5, tritiumUsed: 112 }
  const expeditionMetrics = { ...liveMetrics, carrierJumps: 1, carrierDistanceLy: 498.5, tritiumUsed: 112 }
  const expeditionRealization = { ...ZERO_SMOKE_REALIZATION, totalRealized: 20_210_800, pendingValue: 800_000 }
  const expDays = 211.57777777777778
  const originDays = 731.4222222222222
  const leg2Days = 32 / 1440
  const leg3Days = 211 + 800 / 1440

  return {
    schemaVersion: 1,
    exportedAt: '2026-08-01T00:00:00Z',
    exportedAtGalactic: '3312-08-01 00:00 UTC',
    appVersion: 'profile',
    commanders: ['CmdrSmoke'],
    departed: true,
    effectiveStart: '2026-01-01T10:08:00Z',
    effectiveStartGalactic: '3312-01-01 10:08 UTC',
    effectiveEnd: '',
    effectiveEndGalactic: '3312-08-01 00:00 UTC',
    endIsNow: true,
    duration: '211d 13h 52m',
    careerStart: '2024-01-01T00:00:00Z',
    careerStartGalactic: '3310-01-01 00:00 UTC',
    careerDuration: '2y 212d 00h 00m',
    plan: buildSmokeExportPlan(),
    expedition: {
      metrics: expeditionMetrics,
      realization: expeditionRealization,
      elapsedDays: expDays,
      shipsFlown: ['Mandalay'],
      derived: makeSmokeDerived(expeditionMetrics, expeditionRealization, 755_700_000, expDays),
    },
    live: liveMetrics,
    adjustments,
    carrierCost: {
      carrierType: 'player', weeklyUpkeep: 25_000_000, tritiumPrice: 50_000, tritiumCost: 5_600_000,
      jumpCost: 100_000, upkeepCost: 750_000_000, totalCost: 755_700_000, totalEstimated: true,
    },
    originBaseline: {
      rowId: 11, kind: 'origin', number: 1, from: '2024-01-01T00:00:00Z', to: '2026-01-01T10:08:00Z',
      fromGalactic: '3310-01-01 00:00 UTC', toGalactic: '3312-01-01 10:08 UTC', elapsed: '2y 0d 10h 08m',
      shipsUsed: [], metrics: originMetrics, realization: originRealization,
    },
    originBaselineElapsedDays: originDays,
    originBaselineDerived: makeSmokeDerived(originMetrics, originRealization, null, originDays),
    careerDiff: [
      { id: 'distanceLy', baseline: 1000, growthPercent: 1.034, isNewBaseline: false },
      { id: 'jumps', baseline: 40, growthPercent: 5, isNewBaseline: false },
      { id: 'estimatedValue', baseline: 5_000_000, growthPercent: 420.216, isNewBaseline: false },
      { id: 'realizedValue', baseline: 4_500_000, growthPercent: 449.1288888888889, isNewBaseline: false },
      { id: 'bodiesScanned', baseline: 50, growthPercent: 24, isNewBaseline: false },
      { id: 'bodiesMapped', baseline: 0, growthPercent: null, isNewBaseline: true },
      { id: 'organicSamples', baseline: 0, growthPercent: null, isNewBaseline: true },
    ],
    modeBuckets: [
      { modeId: 1, modeName: 'Exploration', modeColor: '#FF7100', elapsedDays: expDays, shipsFlown: ['Mandalay'], metrics: leg2Metrics, realization: leg2Realization, derived: makeSmokeDerived(leg2Metrics, leg2Realization, null, expDays) },
      { modeId: 2, modeName: 'Travel', modeColor: '#00D2FF', elapsedDays: expDays, shipsFlown: ['Mandalay'], metrics: leg3Metrics, realization: leg3Realization, derived: makeSmokeDerived(leg3Metrics, leg3Realization, null, expDays) },
    ],
    rowDetails: [
      { rowId: 12, kind: 'leg', number: 2, prevSystem: 'Sol', from: '2026-01-01T10:08:00Z', to: '2026-01-01T10:40:00Z', fromGalactic: '3312-01-01 10:08 UTC', toGalactic: '3312-01-01 10:40 UTC', elapsed: '32m', shipsUsed: ['Mandalay'], metrics: leg2Metrics, realization: leg2Realization },
      { rowId: 13, kind: 'leg', number: 3, prevSystem: 'Alpha Centauri', from: '2026-01-01T10:40:00Z', to: '', fromGalactic: '3312-01-01 10:40 UTC', toGalactic: '', inSystem: true, elapsed: '211d 13h 20m', shipsUsed: ['Mandalay'], metrics: leg3Metrics, realization: leg3Realization },
    ],
    legTelemetry: [
      { rowId: 12, regionName: 'Inner Orion Spur', elapsedDays: leg2Days, shipsFlown: ['Mandalay'], derived: makeSmokeDerived(leg2Metrics, leg2Realization, null, leg2Days) },
      { rowId: 13, regionName: 'Inner Orion Spur', elapsedDays: leg3Days, shipsFlown: ['Mandalay'], derived: makeSmokeDerived(leg3Metrics, leg3Realization, null, leg3Days) },
    ],
    regionBuckets: [
      {
        regionName: 'Inner Orion Spur', legRowIds: [12, 13], legCount: 2, fromGalactic: '3312-01-01 10:08 UTC', toGalactic: 'IN SYSTEM', inSystem: true,
        duration: '211d 13h 52m', elapsedDays: expDays, coveredLy: 10.34, shipsFlown: ['Mandalay'], metrics: liveMetrics, realization: expeditionRealization,
        derived: makeSmokeDerived(liveMetrics, expeditionRealization, null, expDays),
        modeBuckets: [
          { modeId: 1, modeName: 'Exploration', modeColor: '#FF7100', legCount: 1, fromGalactic: '3312-01-01 10:08 UTC', toGalactic: '3312-01-01 10:40 UTC', inSystem: false, duration: '0h 32m', elapsedDays: leg2Days, coveredLy: 4.24, shipsFlown: ['Mandalay'], metrics: leg2Metrics, realization: leg2Realization, derived: makeSmokeDerived(leg2Metrics, leg2Realization, null, leg2Days) },
          { modeId: 2, modeName: 'Travel', modeColor: '#00D2FF', legCount: 1, fromGalactic: '3312-01-01 10:40 UTC', toGalactic: 'IN SYSTEM', inSystem: true, duration: '211d 13h 20m', elapsedDays: leg3Days, coveredLy: 6.1, shipsFlown: ['Mandalay'], metrics: leg3Metrics, realization: leg3Realization, derived: makeSmokeDerived(leg3Metrics, leg3Realization, null, leg3Days) },
        ],
      },
    ],
    highlightsFullOrder: [
      { cardId: 'mostLucrative', title: 'Most Lucrative Leg', accent: 'amber', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 20_210_800, formattedPrimary: '20,210,800 Cr', secondaryLabel: 'Carto 1,200,000 Cr · Exo 19,010,800 Cr', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'longestDistance', title: 'Longest Leg (Distance)', accent: 'amber', entries: [{ rank: 1, label: 'Leg #2 -> #3', routeLabel: 'Alpha Centauri -> Barnards Star', primaryValue: 6.1, formattedPrimary: '6.1 Ly', secondaryLabel: '1 jumps · 211d 13h 20m', modeName: 'Travel', modeColor: '#00D2FF', inSystem: true, rowId: 13, waypointNumber: 3 }] },
      { cardId: 'mostJumps', title: 'Most Jumps on a Leg', accent: 'amber', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 1, formattedPrimary: '1 jumps', secondaryLabel: '4.2 Ly · 4.2 Ly avg', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'longestDuration', title: 'Longest Leg (Duration)', accent: 'cyan', entries: [{ rank: 1, label: 'Leg #2 -> #3', routeLabel: 'Alpha Centauri -> Barnards Star', primaryValue: leg3Days * 86_400_000, formattedPrimary: '211d 13h 20m', secondaryLabel: '6.1 Ly · 1 jumps', modeName: 'Travel', modeColor: '#00D2FF', inSystem: true, rowId: 13, waypointNumber: 3 }] },
      { cardId: 'shortestDistance', title: 'Shortest Leg (Distance)', accent: 'amber', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 4.24, formattedPrimary: '4.2 Ly', secondaryLabel: '1 jumps · 32m', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'mostBodiesScanned', title: 'Most Prolific Survey', accent: 'cyan', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 8, formattedPrimary: '8 bodies', secondaryLabel: '4 mapped · 6 first disc.', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'mostOrganicSamples', title: 'Most Prolific Exobiology', accent: 'emerald', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 2, formattedPrimary: '2 samples', secondaryLabel: '2 first logged · 19,010,800 Cr', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'furthestFromSol', title: 'Furthest From Sol', accent: 'cyan', entries: [{ rank: 1, label: 'Waypoint #3', routeLabel: 'Barnards Star', primaryValue: 5.916, formattedPrimary: '5.9 Ly', secondaryLabel: 'Inner Orion Spur', modeName: 'Travel', modeColor: '#00D2FF', rowId: 13, waypointNumber: 3 }] },
      { cardId: 'costliestMistake', title: 'Costliest Mistake', accent: 'danger', entries: [] },
      { cardId: 'highestEarningPace', title: 'Highest Earning Pace', accent: 'amber', entries: [] },
      { cardId: 'fastestTransitPace', title: 'Fastest Transit Pace', accent: 'amber', entries: [] },
    ],
    highlightsVariants: [
      { cardId: 'mostLucrativeRealized', title: 'Most Lucrative Leg', accent: 'amber', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 20_210_800, formattedPrimary: '20,210,800 Cr', secondaryLabel: 'Carto 1,200,000 Cr · Exo 19,010,800 Cr', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'longestShipDistance', title: 'Longest Leg (Distance)', accent: 'amber', entries: [{ rank: 1, label: 'Leg #2 -> #3', routeLabel: 'Alpha Centauri -> Barnards Star', primaryValue: 6.1, formattedPrimary: '6.1 Ly', secondaryLabel: '1 jumps · 211d 13h 20m', modeName: 'Travel', modeColor: '#00D2FF', inSystem: true, rowId: 13, waypointNumber: 3 }] },
      { cardId: 'shortestShipDistance', title: 'Shortest Leg (Distance)', accent: 'amber', entries: [{ rank: 1, label: 'Leg #1 -> #2', routeLabel: 'Sol -> Alpha Centauri', primaryValue: 4.24, formattedPrimary: '4.2 Ly', secondaryLabel: '1 jumps · 32m', modeName: 'Exploration', modeColor: '#FF7100', rowId: 12, waypointNumber: 2 }] },
      { cardId: 'longestInGameDuration', title: 'Longest Leg (Duration)', accent: 'cyan', entries: [] },
      { cardId: 'highestEarningPaceRealized', title: 'Highest Earning Pace', accent: 'amber', entries: [] },
      { cardId: 'fastestTransitPaceShip', title: 'Fastest Transit Pace', accent: 'amber', entries: [] },
    ],
  }
}

function buildSmokeExportPlan(): Record<string, unknown> {
  return {
    expedition: {
      id: 1,
      name: 'Smoke Survey',
      status: 'active',
      startTimestamp: '2026-01-01T10:05:00Z',
      endTimestamp: '',
      adjustments: { carrierJumps: 1, carrierDistanceLy: 498.5, tritiumUsed: 112 },
      carrierType: 'player',
      carrierWeeklyUpkeep: 25_000_000,
      activeModeId: 1,
      createdAt: '2026-01-01T10:00:00Z',
      updatedAt: '2026-01-01T11:00:00Z',
    },
    startGalactic: '3312-01-01 10:08 UTC',
    endGalactic: '',
    rows: [
      { row: { id: 10, expeditionId: 1, position: 1, rowType: 'region', regionId: 18 }, number: 0, regionName: 'Inner Orion Spur' },
      {
        row: { id: 11, expeditionId: 1, position: 2, rowType: 'waypoint', systemName: 'Sol', hasCoords: true, x: 0, y: 0, z: 0, visitedAt: '2026-01-01T10:05:00Z', departedAt: '2026-01-01T10:08:00Z' },
        number: 1,
        detectedRegion: 'Inner Orion Spur',
        visitState: 'visited',
        visitedAtGalactic: '3312-01-01 10:05 UTC',
        departedAtGalactic: '3312-01-01 10:08 UTC',
      },
      {
        row: { id: 12, expeditionId: 1, position: 3, rowType: 'waypoint', systemName: 'Alpha Centauri', poiName: 'Hutton Orbital', fcStop: true, fcArrived: true, hasCoords: true, x: 3, y: 0, z: 3, visitedAt: '2026-01-01T10:15:00Z', departedAt: '2026-01-01T10:40:00Z', legModeId: 1 },
        number: 2,
        detectedRegion: 'Inner Orion Spur',
        visitState: 'visited',
        visitedAtGalactic: '3312-01-01 10:15 UTC',
        departedAtGalactic: '3312-01-01 10:40 UTC',
        distFromPrevLy: 4.24,
        legModeId: 1,
        legModeName: 'Exploration',
        legModeColor: '#FF7100',
      },
      {
        row: { id: 13, expeditionId: 1, position: 4, rowType: 'waypoint', systemName: 'Barnards Star', hasCoords: true, x: -3, y: 5, z: 1, visitedAt: '2026-01-01T11:00:00Z', departedAt: '', legModeId: 2 },
        number: 3,
        detectedRegion: 'Inner Orion Spur',
        visitState: 'visited',
        visitedAtGalactic: '3312-01-01 11:00 UTC',
        distFromPrevLy: 6.1,
        legModeId: 2,
        legModeName: 'Travel',
        legModeColor: '#00D2FF',
      },
    ],
    currentRowId: 13,
    waypointCount: 3,
    visitedWaypointCount: 3,
    plannedLy: 10.34,
    coveredLy: 10.34,
    remainingLy: 0,
    modes: [
      { id: 1, name: 'Exploration', color: '#FF7100', isDefault: true, sortOrder: 1 },
      { id: 2, name: 'Travel', color: '#00D2FF', isDefault: true, sortOrder: 2 },
    ],
  }
}

/**
 * buildSmokeFrontendFixture constructs a deterministic multi-row active expedition
 * fixture used by the normal Vitest suite when LIVE_PROFILE_FIXTURE is unset.
 */
export function buildSmokeFrontendFixture(): LiveFrontendFixture {
  return {
    plan: {
      expedition: {
        id: 1,
        name: 'Smoke Survey',
        status: 'active',
        startTimestamp: '2026-01-01T10:05:00Z',
        endTimestamp: '',
        adjustments: { carrierJumps: 1, carrierDistanceLy: 498.5, tritiumUsed: 112 },
        carrierType: 'player',
        carrierWeeklyUpkeep: 25000000,
        activeModeId: 1,
        createdAt: '2026-01-01T10:00:00Z',
        updatedAt: '2026-01-01T10:00:00Z',
      },
      startGalactic: '01 JAN 3312 10:08',
      endGalactic: '',
      rows: [
        {
          row: { id: 10, expeditionId: 1, position: 1, rowType: 'region', regionId: 18 },
          number: 0,
          regionName: 'Inner Orion Spur',
          allowInsertWaypoint: false,
        },
        {
          row: {
            id: 11,
            expeditionId: 1,
            position: 2,
            rowType: 'waypoint',
            systemName: 'Sol',
            poiName: 'Earth',
            notes: 'Origin launch point',
            hasCoords: true,
            x: 0,
            y: 0,
            z: 0,
            coordsSource: 'journal',
            visitedAt: '2026-01-01T10:05:00Z',
            departedAt: '2026-01-01T10:08:00Z',
          },
          number: 1,
          visitCount: 2,
          visitState: 'visited',
          visitedAtGalactic: '01 JAN 3312 10:05',
          departedAtGalactic: '01 JAN 3312 10:08',
          allowInsertWaypoint: false,
          permissions: { systemLocked: true, allowInsertWaypoint: false, canReorder: false, deleteDisabledReason: 'Origin waypoint cannot be deleted', reorderDisabledReason: 'Origin waypoint cannot be reordered', canAddFcStop: true, canRemoveFcStop: true, canOverrideFcBadge: true, canCascadeLegMode: true, hasDrawer: true },
        },
        {
          row: {
            id: 12,
            expeditionId: 1,
            position: 3,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            poiName: 'Hutton Orbital long supercruise run across binary system',
            notes: 'Bring mug and verify multi-line wrapping note measurement pass',
            fcStop: true,
            fcArrived: false,
            hasCoords: true,
            x: 3,
            y: 0,
            z: 3,
            coordsSource: 'journal',
            visitedAt: '2026-01-01T10:15:00Z',
            departedAt: '',
          },
          number: 2,
          visitCount: 1,
          visitState: 'visited',
          visitedAtGalactic: '01 JAN 3312 10:15',
          distFromPrevLy: 4.24,
          legModeId: 1,
          legModeName: 'Exploration',
          legModeColor: '#FF7100',
          allowInsertWaypoint: true,
          permissions: { systemLocked: true, allowInsertWaypoint: true, canReorder: true, canAddFcStop: true, canRemoveFcStop: true, canOverrideFcBadge: true, hasDrawer: true, isNextFcStop: true },
        },
      ],
      currentRowId: 12,
      nextFcStopRowId: 12,
      lastVisitedIndex: 2,
      lastFcArrivedIndex: -1,
      defaultModeId: 1,
      playerSystem: 'Alpha Centauri',
      waypointCount: 2,
      visitedWaypointCount: 2,
      plannedLy: 4.24,
      coveredLy: 4.24,
      remainingLy: 0,
      modes: [{ id: 1, name: 'Exploration', color: '#FF7100', isDefault: true, sortOrder: 1 }],
    },
    sidebarStats: {
      expeditionId: 1,
      status: 'active',
      started: true,
      departed: true,
      start: '2026-01-01T10:08:00Z',
      startGalactic: '01 JAN 3312 10:08',
      endGalactic: '20 NOV 3312 17:00',
      endIsNow: true,
      duration: '323d 6h',
      careerStart: '2024-01-01T00:00:00Z',
      careerStartGalactic: '01 JAN 3310 00:00',
      careerDuration: '2y 324d',
      live: { jumps: 1, distanceLy: 4.24, shipDistanceLy: 4.24, scanValueTotal: 1200000 },
      adjustments: { carrierJumps: 1, carrierDistanceLy: 498.5, tritiumUsed: 112 },
      expedition: {
        jumps: 1,
        distanceLy: 4.24,
        shipDistanceLy: 4.24,
        carrierJumps: 1,
        carrierDistanceLy: 498.5,
        scanValueTotal: 1200000,
      },
      allJournals: {
        jumps: 500,
        distanceLy: 25000,
        shipDistanceLy: 25000,
        carrierJumps: 12,
        carrierDistanceLy: 5800,
        scanValueTotal: 90000000,
      },
      realization: { totalRealized: 1200000 },
      allJournalsRealization: { totalRealized: 90000000 },
      carrierCost: {
        carrierType: 'player',
        weeklyUpkeep: 25000000,
        tritiumPrice: 50000,
        totalCost: 5700000,
        totalEstimated: true,
      },
    },
    modesState: {
      modes: [{ id: 1, name: 'Exploration', color: '#FF7100', isDefault: true, sortOrder: 1 }],
      defaultModeId: 1,
    },
    visitedDetails: [
      {
        rowId: 11,
        kind: 'origin',
        number: 1,
        from: '2024-01-01T00:00:00Z',
        to: '2026-01-01T10:08:00Z',
        arrivedAt: '2026-01-01T10:05:00Z',
        departedAt: '2026-01-01T10:08:00Z',
        shipsUsed: [],
        metrics: {},
        realization: {},
      },
      {
        rowId: 12,
        kind: 'leg',
        number: 2,
        from: '2026-01-01T10:08:00Z',
        to: '',
        arrivedAt: '2026-01-01T10:15:00Z',
        inSystem: true,
        prevSystem: 'Sol',
        shipsUsed: ['Mandalay'],
        metrics: { jumps: 1, distanceLy: 4.24, shipDistanceLy: 4.24 },
        realization: {},
      },
    ],
    heldRowIndex: 2,
    dropPreviews: [
      { targetIndex: 0, status: 'invalid' },
      { targetIndex: 1, status: 'invalid' },
      { targetIndex: 2, status: 'valid_visited', visitedAtGalactic: '01 JAN 3312 10:15' },
    ],
    exportData: buildSmokeExportData(),
  }
}
