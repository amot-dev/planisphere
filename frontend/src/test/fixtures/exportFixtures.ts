import { expedition, exporter, realization, stats } from '../../api/models'
import { formatCredits, formatElapsedDuration, formatInt, formatLy } from '../../domain/galacticTime'
import { expandConfigToCartItems } from '../../features/export/exportMetrics'
import {
  CartBlockItem, ConfiguratorBlockConfig, ExpeditionExportData, ExportCareerDiffItem,
  ExportCarrierCost, ExportDerivedMetrics, ExportLegTelemetry, ExportModeBucket,
  ExportRegionBucket, ExportRegionModeBucket, ExportTelemetrySnapshot, PlainWailsDto,
} from '../../features/export/exportTypes'
import { makeMetrics, makeMode, makeRealization, makeWaypointDetails } from '../fixtures'

const DAY_MS = 86_400_000

/** Partial overrides accepted by buildSampleExportData across export unit and UI tests. */
export type SampleExportDataOverrides = Omit<Partial<ExpeditionExportData>, 'expedition' | 'modeBuckets'> & {
  expedition?: Omit<ExportTelemetrySnapshot, 'elapsedDays' | 'shipsFlown' | 'derived'> & Partial<Pick<ExportTelemetrySnapshot, 'elapsedDays' | 'shipsFlown' | 'derived'>>
  modeBuckets?: Array<Omit<ExportModeBucket, 'elapsedDays' | 'shipsFlown' | 'derived'> & Partial<Pick<ExportModeBucket, 'elapsedDays' | 'shipsFlown' | 'derived'>>>
}

/** Expands a single-card configurator block into its concrete CartBlockItem for renderer and filename tests. */
export function expandCartItem(exportData: ExpeditionExportData, config: ConfiguratorBlockConfig, idPrefix = 'item'): CartBlockItem {
  return expandConfigToCartItems(exportData, config, idPrefix)[0]
}

function sumMetrics(left: stats.Metrics, right: stats.Metrics): stats.Metrics {
  const hasTritium = left.tritiumUsed != null || right.tritiumUsed != null
  const summed = makeMetrics({ tritiumUsed: hasTritium ? (left.tritiumUsed ?? 0) + (right.tritiumUsed ?? 0) : undefined })
  for (const key of Object.keys(summed) as Array<keyof stats.Metrics>) {
    if (key !== 'tritiumUsed') summed[key] = ((left[key] as number) ?? 0) + ((right[key] as number) ?? 0)
  }
  return summed
}

function sumRealization(left: realization.Breakdown, right: realization.Breakdown): realization.Breakdown {
  const summed = makeRealization()
  for (const key of Object.keys(summed) as Array<keyof realization.Breakdown>) {
    summed[key] = left[key] + right[key]
  }
  return summed
}

function elapsedDaysBetween(fromIso: string | undefined, toIso: string | undefined, nowMs: number): number | null {
  if (!fromIso) return null
  const startMs = Date.parse(fromIso)
  const endMs = toIso ? Date.parse(toIso) : nowMs
  return !Number.isNaN(startMs) && !Number.isNaN(endMs) && endMs > startMs ? (endMs - startMs) / DAY_MS : null
}

function computeFixtureDerived(
  metrics: stats.Metrics,
  rel: realization.Breakdown,
  carrierCost: ExportCarrierCost | null | undefined,
  elapsedDays: number | null,
): ExportDerivedMetrics {
  const div = (n: number, d: number) => (d > 0 ? n / d : null)
  const pct = (n: number, d: number) => (d > 0 ? (n / d) * 100 : null)
  const perK = (n: number, ly: number) => (ly > 0 ? n / (ly / 1000) : null)
  const shipDistanceLy = metrics.shipDistanceLy || Math.max(0, metrics.distanceLy - metrics.carrierPassengerLy)
  const totalBoosts = metrics.jetConeBoosts + metrics.premiumBoosts + metrics.standardBoosts + metrics.basicBoosts
  const combinedEstimatedCredits = metrics.scanValueTotal + metrics.organicValueTotal
  const totalLossesCredits =
    rel.cartoLostDeath + rel.organicLostDeath + rel.cartoStolen + rel.organicStolen + rel.carrierTariffLoss + rel.npcCrewLoss
  const settled = combinedEstimatedCredits - rel.pendingValue
  const validDays = elapsedDays != null && elapsedDays >= 1 ? elapsedDays : null

  return {
    shipDistanceLy,
    totalBoosts,
    avgJumpRangeLy: div(shipDistanceLy, metrics.jumps),
    boostRatePercent: pct(totalBoosts, metrics.jumps),
    avgDistancePerDayLy: validDays != null ? metrics.distanceLy / validDays : null,
    avgCarrierJumpRangeLy: div(metrics.carrierDistanceLy, metrics.carrierJumps),
    avgTritiumPerJump: metrics.tritiumUsed != null ? div(metrics.tritiumUsed, metrics.carrierJumps) : null,
    combinedEstimatedCredits,
    dssEfficiencyPercent: pct(metrics.efficientMappings, metrics.bodiesMapped),
    firstDiscoveryRatePercent: pct(metrics.firstDiscoveries, metrics.bodiesScanned),
    firstLoggedRatePercent: pct(metrics.firstLogged, metrics.organicSamples),
    avgCreditsPerSample: div(metrics.organicValueTotal, metrics.organicSamples),
    avgCombinedCreditsPerJump: div(combinedEstimatedCredits, metrics.jumps),
    avgCombinedCreditsPer1000Ly: perK(combinedEstimatedCredits, metrics.distanceLy),
    avgCombinedCreditsPerDay: validDays != null ? combinedEstimatedCredits / validDays : null,
    avgCartoCreditsPerJump: div(metrics.scanValueTotal, metrics.jumps),
    avgCartoCreditsPer1000Ly: perK(metrics.scanValueTotal, metrics.distanceLy),
    avgExoCreditsPerJump: div(metrics.organicValueTotal, metrics.jumps),
    avgExoCreditsPer1000Ly: perK(metrics.organicValueTotal, metrics.distanceLy),
    totalLossesCredits,
    realizationRetentionPercent: settled > 0 ? (rel.totalRealized / settled) * 100 : null,
    netProfitAfterCarrierCost: carrierCost != null ? rel.totalRealized - carrierCost.totalCost : null,
    avgRealizedCreditsPerJump: div(rel.totalRealized, metrics.jumps),
    avgRealizedCreditsPer1000Ly: perK(rel.totalRealized, metrics.distanceLy),
  }
}

function buildFixtureCareerDiff(
  baseline: expedition.WaypointDetails | null | undefined,
  live: stats.Metrics,
  expRealization: realization.Breakdown,
): ExportCareerDiffItem[] | undefined {
  if (!baseline) return undefined
  const bm = baseline.metrics
  const item = (id: string, baseVal: number, delta: number): ExportCareerDiffItem => ({
    id,
    baseline: baseVal,
    growthPercent: baseVal > 0 ? (delta / baseVal) * 100 : null,
    isNewBaseline: baseVal <= 0 && delta > 0,
  })
  return [
    item('distanceLy', bm.distanceLy, live.distanceLy),
    item('jumps', bm.jumps, live.jumps),
    item('estimatedValue', bm.scanValueTotal + bm.organicValueTotal, live.scanValueTotal + live.organicValueTotal),
    item('realizedValue', baseline.realization.totalRealized, expRealization.totalRealized),
    item('bodiesScanned', bm.bodiesScanned, live.bodiesScanned),
    item('bodiesMapped', bm.bodiesMapped, live.bodiesMapped),
    item('organicSamples', bm.organicSamples, live.organicSamples),
  ]
}

interface FixtureVisitedLeg {
  rowView: expedition.RowView
  details: expedition.WaypointDetails
  regionName: string
  shipsFlown: string[]
  elapsedDays: number | null
}

function collectFixtureVisitedLegs(
  plan: expedition.PlanView,
  rowDetails: expedition.WaypointDetails[],
  nowMs: number,
): FixtureVisitedLeg[] {
  const detailsById = new Map(rowDetails.map((d) => [d.rowId, d]))
  let manualRegion = ''
  const visited: FixtureVisitedLeg[] = []
  for (const rowView of plan.rows ?? []) {
    if (rowView.row.rowType === 'region') {
      manualRegion = rowView.regionName || manualRegion
      continue
    }
    if (rowView.number <= 1 || !rowView.row.visitedAt) continue
    const details = detailsById.get(rowView.row.id)
    if (!details) continue
    const shipsFlown = Array.from(new Set((details.shipsUsed ?? []).map((s) => s.trim()).filter(Boolean)))
    visited.push({
      rowView,
      details,
      regionName: rowView.detectedRegion || manualRegion || 'Unknown Region',
      shipsFlown,
      elapsedDays: elapsedDaysBetween(details.from, details.to, nowMs),
    })
  }
  return visited
}

function buildFixtureRegions(visited: FixtureVisitedLeg[]): ExportRegionBucket[] {
  const byRegion = new Map<string, FixtureVisitedLeg[]>()
  for (const leg of visited) {
    const list = byRegion.get(leg.regionName)
    if (list) list.push(leg)
    else byRegion.set(leg.regionName, [leg])
  }
  const summarize = (legs: FixtureVisitedLeg[]) => {
    let metrics = makeMetrics()
    let rel = makeRealization()
    let totalMs = 0
    let fromGalactic = ''
    let toGalactic = ''
    let inSystem = false
    let coveredLy = 0
    const ships = new Set<string>()
    for (const leg of legs) {
      metrics = sumMetrics(metrics, leg.details.metrics)
      rel = sumRealization(rel, leg.details.realization)
      if (!fromGalactic && leg.details.fromGalactic) fromGalactic = leg.details.fromGalactic
      if (leg.details.toGalactic) toGalactic = leg.details.toGalactic
      if (leg.details.inSystem) {
        inSystem = true
        toGalactic = 'IN SYSTEM'
      }
      coveredLy += leg.rowView.distFromPrevLy ?? 0
      for (const s of leg.shipsFlown) ships.add(s)
      if (leg.elapsedDays != null) totalMs += leg.elapsedDays * DAY_MS
    }
    const elapsedDays = totalMs > 0 ? totalMs / DAY_MS : null
    const duration = totalMs > 0 ? formatElapsedDuration(new Date(0).toISOString(), new Date(totalMs).toISOString()) : ''
    return {
      legCount: legs.length, fromGalactic, toGalactic, inSystem, duration, elapsedDays, coveredLy,
      shipsFlown: Array.from(ships), metrics, realization: rel, derived: computeFixtureDerived(metrics, rel, null, elapsedDays),
    }
  }
  const buckets: ExportRegionBucket[] = []
  for (const [regionName, legs] of byRegion.entries()) {
    const modeMap = new Map<number, FixtureVisitedLeg[]>()
    for (const leg of legs) {
      if (leg.rowView.legModeId <= 0) continue
      const existing = modeMap.get(leg.rowView.legModeId)
      if (existing) existing.push(leg)
      else modeMap.set(leg.rowView.legModeId, [leg])
    }
    const modeBuckets: ExportRegionModeBucket[] = Array.from(modeMap.entries()).map(([modeId, mLegs]) => ({
      modeId,
      modeName: mLegs[0].rowView.legModeName || 'Travel',
      modeColor: mLegs[0].rowView.legModeColor || '#FF7100',
      ...summarize(mLegs),
    }))
    buckets.push({ regionName, legRowIds: legs.map((l) => l.rowView.row.id), ...summarize(legs), modeBuckets })
  }
  return buckets
}

function buildFixtureHighlights(
  plan: expedition.PlanView,
  visited: FixtureVisitedLeg[],
  nowMs: number,
): { full: Array<PlainWailsDto<exporter.HighlightLeaderboard>>; variants: Array<PlainWailsDto<exporter.HighlightLeaderboard>> } {
  const rank = (
    cardId: string, title: string, accent: string, asc: boolean,
    scoreFn: (l: FixtureVisitedLeg) => number,
    fmtFn: (l: FixtureVisitedLeg, v: number) => [string, string],
  ): PlainWailsDto<exporter.HighlightLeaderboard> => {
    const items = visited.map((leg) => ({ leg, v: scoreFn(leg) })).filter((x) => x.v > 0)
    items.sort((a, b) => (asc ? a.v - b.v : b.v - a.v))
    return {
      cardId, title, accent,
      entries: items.map(({ leg, v }, i) => {
        const [formattedPrimary, secondaryLabel] = fmtFn(leg, v)
        return {
          rank: i + 1, label: `Leg #${leg.details.number - 1} -> #${leg.details.number}`,
          routeLabel: `${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`,
          primaryValue: v, formattedPrimary, secondaryLabel,
          modeName: leg.rowView.legModeName || undefined, modeColor: leg.rowView.legModeColor || undefined,
          inSystem: leg.details.inSystem || undefined, rowId: leg.rowView.row.id, waypointNumber: leg.details.number,
        }
      }),
    }
  }
  const distFmt = (l: FixtureVisitedLeg, v: number): [string, string] => [
    formatLy(v, 1),
    `${formatInt(l.details.metrics.jumps)} jumps${l.details.elapsed ? ` · ${l.details.elapsed}` : ''}`,
  ]
  const lucFmt = (l: FixtureVisitedLeg, v: number): [string, string] => [
    formatCredits(v),
    `Carto ${formatCredits(l.details.metrics.scanValueTotal)} · Exo ${formatCredits(l.details.metrics.organicValueTotal)}`,
  ]
  const shipLy = (l: FixtureVisitedLeg) =>
    l.details.metrics.shipDistanceLy || Math.max(0, l.details.metrics.distanceLy - l.details.metrics.carrierPassengerLy)

  const solRows = (plan.rows ?? [])
    .filter((r) => r.row.rowType === 'waypoint' && Boolean(r.row.visitedAt) && r.row.hasCoords)
    .map((r) => ({ r, d: Math.hypot(r.row.x, r.row.y, r.row.z) }))
    .sort((a, b) => b.d - a.d)
  const furthestFromSol: PlainWailsDto<exporter.HighlightLeaderboard> = {
    cardId: 'furthestFromSol', title: 'Furthest From Sol', accent: 'cyan',
    entries: solRows.map(({ r, d }, i) => ({
      rank: i + 1, label: `Waypoint #${r.number}`, routeLabel: r.row.systemName,
      primaryValue: d, formattedPrimary: formatLy(d, 1), secondaryLabel: r.detectedRegion || 'Unknown Region',
      modeName: r.legModeName || undefined, modeColor: r.legModeColor || undefined, rowId: r.row.id, waypointNumber: r.number,
    })),
  }

  return {
    full: [
      rank('mostLucrative', 'Most Lucrative Leg', 'amber', false, (l) => l.details.metrics.scanValueTotal + l.details.metrics.organicValueTotal, lucFmt),
      rank('longestDistance', 'Longest Leg (Distance)', 'amber', false, (l) => l.details.metrics.distanceLy, distFmt),
      rank('mostJumps', 'Most Jumps on a Leg', 'amber', false, (l) => l.details.metrics.jumps, (l, j) => [`${formatInt(j)} jumps`, `${formatLy(shipLy(l), 1)} · ${formatLy(shipLy(l) / j, 1)} avg`]),
      rank('longestDuration', 'Longest Leg (Duration)', 'cyan', false, (l) => (l.elapsedDays != null ? l.elapsedDays * DAY_MS : 0), (l) => [l.details.elapsed || formatElapsedDuration(l.details.from, l.details.to, nowMs), `${formatLy(l.details.metrics.distanceLy, 1)} · ${formatInt(l.details.metrics.jumps)} jumps`]),
      rank('shortestDistance', 'Shortest Leg (Distance)', 'amber', true, (l) => l.details.metrics.distanceLy, distFmt),
      rank('mostBodiesScanned', 'Most Prolific Survey', 'cyan', false, (l) => l.details.metrics.bodiesScanned, (l, b) => [`${formatInt(b)} bodies`, `${formatInt(l.details.metrics.bodiesMapped)} mapped · ${formatInt(l.details.metrics.firstDiscoveries)} first disc.`]),
      rank('mostOrganicSamples', 'Most Prolific Exobiology', 'emerald', false, (l) => l.details.metrics.organicSamples, (l, s) => [`${formatInt(s)} samples`, `${formatInt(l.details.metrics.firstLogged)} first logged · ${formatCredits(l.details.metrics.organicValueTotal)}`]),
      furthestFromSol,
      rank('costliestMistake', 'Costliest Mistake', 'danger', false, (l) => l.details.realization.cartoLostDeath + l.details.realization.organicLostDeath, (l, v) => [`-${formatCredits(v)}`, `Carto -${formatCredits(l.details.realization.cartoLostDeath)} · Exo -${formatCredits(l.details.realization.organicLostDeath)}`]),
    ],
    variants: [
      rank('mostLucrativeRealized', 'Most Lucrative Leg', 'amber', false, (l) => l.details.realization.totalRealized, lucFmt),
      rank('longestShipDistance', 'Longest Leg (Distance)', 'amber', false, shipLy, distFmt),
      rank('shortestShipDistance', 'Shortest Leg (Distance)', 'amber', true, shipLy, distFmt),
    ],
  }
}

function buildDefaultSamplePlan(): expedition.PlanView {
  return expedition.PlanView.createFrom({
    expedition: {
      id: 7, name: 'Orion Horizon <Test>', description: 'Deep rim survey', status: 'active',
      startTimestamp: '2026-04-01T08:00:00Z', endTimestamp: '',
      adjustments: { carrierJumps: 4, carrierDistanceLy: 1800, tritiumUsed: 400 },
      carrierType: 'player', carrierWeeklyUpkeep: 10_000_000, activeModeId: 1,
      createdAt: '2026-04-01T07:00:00Z', updatedAt: '2026-04-05T12:00:00Z',
    },
    startGalactic: '3312-04-01 08:00 UTC', endGalactic: '', waypointCount: 5, visitedWaypointCount: 4,
    plannedLy: 6000, coveredLy: 4200, remainingLy: 1800,
    modes: [
      makeMode({ id: 1, name: 'Exploration', color: '#00D2FF', sortOrder: 1 }),
      makeMode({ id: 2, name: 'Travel', color: '#FF7100', sortOrder: 2 }),
    ],
    rows: [
      { row: { id: 100, rowType: 'region', regionId: 18 }, number: 0, regionName: 'Inner Orion Spur' },
      {
        row: { id: 1, rowType: 'waypoint', systemName: 'Sol', hasCoords: true, x: 0, y: 0, z: 0, visitedAt: '2026-04-01T08:00:00Z', departedAt: '2026-04-01T10:00:00Z' },
        number: 1, detectedRegion: 'Inner Orion Spur', visitState: 'visited', visitedAtGalactic: '3312-04-01 08:00 UTC', departedAtGalactic: '3312-04-01 10:00 UTC',
      },
      {
        row: { id: 2, rowType: 'waypoint', systemName: 'Alpha Rim', poiName: 'Ringed Giant', notes: 'Great views', hasCoords: true, x: 1000, y: 0, z: 0, fcStop: true, fcArrived: true, visitedAt: '2026-04-02T10:00:00Z', departedAt: '2026-04-02T12:00:00Z', legModeId: 1 },
        number: 2, detectedRegion: 'Inner Orion Spur', visitState: 'visited', visitedAtGalactic: '3312-04-02 10:00 UTC', departedAtGalactic: '3312-04-02 12:00 UTC', distFromPrevLy: 1000, legModeId: 1, legModeName: 'Exploration', legModeColor: '#00D2FF',
        gec: { id: 11, name: 'Rim Jewel', type: 'Nebula', url: 'https://edastro.com', count: 1 },
      },
      {
        row: { id: 3, rowType: 'waypoint', systemName: 'Beta Crossing', hasCoords: true, x: 4000, y: 0, z: 0, visitedAt: '2026-04-04T12:00:00Z', departedAt: '2026-04-04T14:00:00Z', legModeId: 2 },
        number: 3, detectedRegion: 'Inner Orion Spur', visitState: 'visited', visitedAtGalactic: '3312-04-04 12:00 UTC', departedAtGalactic: '3312-04-04 14:00 UTC', distFromPrevLy: 3000, legModeId: 2, legModeName: 'Travel', legModeColor: '#FF7100',
      },
      {
        row: { id: 4, rowType: 'waypoint', systemName: 'Odin Beacon', hasCoords: true, x: 4000, y: 0, z: 3000, visitedAt: '2026-04-05T10:00:00Z', departedAt: '', legModeId: 1 },
        number: 4, detectedRegion: "Odin's Hold", visitState: 'visited', visitedAtGalactic: '3312-04-05 10:00 UTC', distFromPrevLy: 600, legModeId: 1, legModeName: 'Exploration', legModeColor: '#00D2FF',
      },
      {
        row: { id: 5, rowType: 'waypoint', systemName: 'Future Star', hasCoords: true, x: 6000, y: 0, z: 3000, visitedAt: '' },
        number: 5, detectedRegion: "Odin's Hold", visitState: 'current', distFromPrevLy: 1800, legModeId: 1, legModeName: 'Exploration', legModeColor: '#00D2FF',
      },
    ],
  })
}

/** Builds a rich multi-leg, multi-region, multi-mode ExpeditionExportData fixture for Export Studio tests. */
export function buildSampleExportData(overrides: SampleExportDataOverrides = {}): ExpeditionExportData {
  const leg1Metrics = makeMetrics({
    distanceLy: 1200, shipDistanceLy: 1000, carrierPassengerLy: 200, jumps: 20, jetConeBoosts: 4,
    bodiesScanned: 40, bodiesMapped: 10, efficientMappings: 8,
    scanValueTotal: 12_000_000, scanValueBase: 6_000_000, scanValueBonus: 4_000_000, scanValueEfficiencyBonus: 2_000_000,
    firstDiscoveries: 30, firstMapped: 8, organicSamples: 4,
    organicValueTotal: 40_000_000, organicValueBase: 8_000_000, organicValueBonus: 32_000_000, firstLogged: 4,
  })
  const leg1Realization = makeRealization({ totalRealized: 48_000_000, carrierTariffLoss: 2_000_000, pendingValue: 2_000_000 })

  const leg2Metrics = makeMetrics({
    distanceLy: 3000, shipDistanceLy: 3000, jumps: 50, jetConeBoosts: 10,
    bodiesScanned: 15, bodiesMapped: 4, efficientMappings: 4,
    scanValueTotal: 5_000_000, scanValueBase: 3_000_000, scanValueBonus: 1_500_000, scanValueEfficiencyBonus: 500_000,
    firstDiscoveries: 5, firstMapped: 2, organicSamples: 1,
    organicValueTotal: 10_000_000, organicValueBase: 2_000_000, organicValueBonus: 8_000_000, firstLogged: 1,
  })
  const leg2Realization = makeRealization({ totalRealized: 12_000_000, cartoLostDeath: 1_000_000, organicLostDeath: 2_000_000 })

  const leg3Metrics = makeMetrics({
    distanceLy: 600, shipDistanceLy: 600, jumps: 10,
    bodiesScanned: 60, bodiesMapped: 20, efficientMappings: 18,
    scanValueTotal: 25_000_000, scanValueBase: 10_000_000, scanValueBonus: 10_000_000, scanValueEfficiencyBonus: 5_000_000,
    firstDiscoveries: 50, firstMapped: 18, organicSamples: 8,
    organicValueTotal: 80_000_000, organicValueBase: 16_000_000, organicValueBonus: 64_000_000, firstLogged: 7,
  })
  const leg3Realization = makeRealization({ totalRealized: 95_000_000, pendingValue: 10_000_000 })

  const liveMetrics = overrides.live ?? makeMetrics({
    distanceLy: 4800, shipDistanceLy: 4600, carrierPassengerLy: 200, jumps: 80, jetConeBoosts: 14,
    bodiesScanned: 115, bodiesMapped: 34, efficientMappings: 30,
    scanValueTotal: 42_000_000, scanValueBase: 19_000_000, scanValueBonus: 15_500_000, scanValueEfficiencyBonus: 7_500_000,
    firstDiscoveries: 85, firstMapped: 28, organicSamples: 13,
    organicValueTotal: 130_000_000, organicValueBase: 26_000_000, organicValueBonus: 104_000_000, firstLogged: 12,
  })
  const adjustments = overrides.adjustments ?? makeMetrics({ carrierJumps: 4, carrierDistanceLy: 1800, tritiumUsed: 400 })
  const defaultExpMetrics = makeMetrics({ ...liveMetrics, carrierJumps: 4, carrierDistanceLy: 1800, tritiumUsed: 400 })
  const defaultExpRealization = makeRealization({
    totalRealized: 155_000_000, cartoLostDeath: 1_000_000, organicLostDeath: 2_000_000,
    carrierTariffLoss: 2_000_000, pendingValue: 12_000_000,
  })
  const defaultOriginBaseline = makeWaypointDetails({
    rowId: 1, kind: 'origin', number: 1, from: '2025-01-01T00:00:00Z', to: '2026-04-01T10:00:00Z',
    fromGalactic: '3311-01-01 00:00 UTC', toGalactic: '3312-04-01 10:00 UTC', elapsed: '1y 90d 10h 00m', shipsUsed: [],
    metrics: { distanceLy: 10_000, shipDistanceLy: 10_000, jumps: 200, bodiesScanned: 230, scanValueTotal: 84_000_000, organicSamples: 26, organicValueTotal: 260_000_000 },
    realization: { totalRealized: 310_000_000 },
  })
  const defaultRowDetails = [
    makeWaypointDetails({ rowId: 2, number: 2, prevSystem: 'Sol', from: '2026-04-01T10:00:00Z', to: '2026-04-02T12:00:00Z', fromGalactic: '3312-04-01 10:00 UTC', toGalactic: '3312-04-02 12:00 UTC', elapsed: '1d 02h 00m', shipsUsed: ['Mandalay'], metrics: leg1Metrics, realization: leg1Realization }),
    makeWaypointDetails({ rowId: 3, number: 3, prevSystem: 'Alpha Rim', from: '2026-04-02T12:00:00Z', to: '2026-04-04T14:00:00Z', fromGalactic: '3312-04-02 12:00 UTC', toGalactic: '3312-04-04 14:00 UTC', elapsed: '2d 02h 00m', shipsUsed: ['Anaconda'], metrics: leg2Metrics, realization: leg2Realization }),
    makeWaypointDetails({ rowId: 4, number: 4, prevSystem: 'Beta Crossing', from: '2026-04-04T14:00:00Z', to: '', fromGalactic: '3312-04-04 14:00 UTC', toGalactic: '', inSystem: true, elapsed: '22h 00m', shipsUsed: ['Mandalay'], metrics: leg3Metrics, realization: leg3Realization }),
    makeWaypointDetails({ rowId: 5, number: 5, prevSystem: 'Odin Beacon', from: '2026-04-05T12:00:00Z', to: '', fromGalactic: '3312-04-05 12:00 UTC', toGalactic: '', elapsed: '1h 00m', shipsUsed: ['Mandalay'], metrics: { distanceLy: 100, shipDistanceLy: 100, jumps: 2 }, realization: {} }),
  ]
  const defaultCarrierCost: ExportCarrierCost = {
    carrierType: 'player', weeklyUpkeep: 10_000_000, tritiumPrice: 50_000, tritiumCost: 20_000_000,
    jumpCost: 400_000, upkeepCost: 5_600_000, totalCost: 26_000_000, totalEstimated: true,
  }

  const exportedAt = overrides.exportedAt ?? '2026-04-05T12:00:00Z'
  const nowMs = Date.parse(exportedAt)
  const effectiveStart = overrides.effectiveStart ?? '2026-04-01T10:00:00Z'
  const effectiveEnd = overrides.effectiveEnd ?? '2026-04-05T12:00:00Z'
  const plan = overrides.plan ?? buildDefaultSamplePlan()
  const rowDetails = overrides.rowDetails ?? defaultRowDetails
  const originBaseline = 'originBaseline' in overrides ? overrides.originBaseline : defaultOriginBaseline
  const carrierCost = 'carrierCost' in overrides ? overrides.carrierCost! : defaultCarrierCost

  const visitedLegs = collectFixtureVisitedLegs(plan, rowDetails, nowMs)
  const allShips = Array.from(new Set(visitedLegs.flatMap((leg) => leg.shipsFlown)))
  const expElapsedDays = overrides.expedition?.elapsedDays !== undefined ? overrides.expedition.elapsedDays : elapsedDaysBetween(effectiveStart, effectiveEnd, nowMs)
  const expMetrics = overrides.expedition?.metrics ?? defaultExpMetrics
  const expRealization = overrides.expedition?.realization ?? defaultExpRealization
  const expeditionSnapshot: ExportTelemetrySnapshot = {
    metrics: expMetrics, realization: expRealization, elapsedDays: expElapsedDays,
    shipsFlown: overrides.expedition?.shipsFlown ?? allShips,
    derived: overrides.expedition?.derived ?? computeFixtureDerived(expMetrics, expRealization, carrierCost, expElapsedDays),
  }

  const rawModeBuckets = overrides.modeBuckets ?? [
    {
      modeId: 1, modeName: 'Exploration', modeColor: '#00D2FF',
      metrics: makeMetrics({
        distanceLy: 1800, shipDistanceLy: 1600, carrierPassengerLy: 200, jumps: 30, jetConeBoosts: 4,
        bodiesScanned: 100, bodiesMapped: 30, efficientMappings: 26, scanValueTotal: 37_000_000,
        organicSamples: 12, organicValueTotal: 120_000_000, firstDiscoveries: 80, firstMapped: 26, firstLogged: 11,
      }),
      realization: makeRealization({ totalRealized: 143_000_000, carrierTariffLoss: 2_000_000, pendingValue: 12_000_000 }),
    },
    { modeId: 2, modeName: 'Travel', modeColor: '#FF7100', metrics: leg2Metrics, realization: leg2Realization },
  ]
  const modeBuckets: ExportModeBucket[] = rawModeBuckets.map((bucket) => {
    const bucketElapsed = bucket.elapsedDays !== undefined ? bucket.elapsedDays : expElapsedDays
    const modeShips = Array.from(new Set(visitedLegs.filter((leg) => leg.rowView.legModeId === bucket.modeId).flatMap((leg) => leg.shipsFlown)))
    return {
      ...bucket, elapsedDays: bucketElapsed, shipsFlown: bucket.shipsFlown ?? modeShips,
      derived: bucket.derived ?? computeFixtureDerived(bucket.metrics, bucket.realization, null, bucketElapsed),
    }
  })

  const legTelemetry: ExportLegTelemetry[] = overrides.legTelemetry ?? visitedLegs.map((leg) => ({
    rowId: leg.rowView.row.id, regionName: leg.regionName, elapsedDays: leg.elapsedDays, shipsFlown: leg.shipsFlown,
    derived: computeFixtureDerived(leg.details.metrics, leg.details.realization, null, leg.elapsedDays),
  }))
  const regionBuckets = overrides.regionBuckets ?? buildFixtureRegions(visitedLegs)
  const highlights = buildFixtureHighlights(plan, visitedLegs, nowMs)
  const originElapsed = 'originBaselineElapsedDays' in overrides
    ? overrides.originBaselineElapsedDays
    : originBaseline ? elapsedDaysBetween(originBaseline.from, originBaseline.to, nowMs) : null
  const originDerived = 'originBaselineDerived' in overrides
    ? overrides.originBaselineDerived
    : originBaseline ? computeFixtureDerived(originBaseline.metrics, originBaseline.realization, null, originElapsed ?? null) : undefined
  const careerDiff = 'careerDiff' in overrides ? overrides.careerDiff : buildFixtureCareerDiff(originBaseline, liveMetrics, expRealization)

  return {
    schemaVersion: 1, exportedAt, exportedAtGalactic: '3312-04-05 12:00 UTC', appVersion: '0.6.2',
    commanders: ['CMDR Horizon'], departed: true,
    effectiveStart, effectiveStartGalactic: '3312-04-01 10:00 UTC',
    effectiveEnd, effectiveEndGalactic: '3312-04-05 12:00 UTC',
    endIsNow: false, duration: '4d',
    careerStart: '2025-01-01T00:00:00Z', careerStartGalactic: '3311-01-01 00:00 UTC', careerDuration: '1y 94d',
    ...overrides,
    plan, expedition: expeditionSnapshot, live: liveMetrics, adjustments, carrierCost,
    originBaseline, originBaselineElapsedDays: originElapsed, originBaselineDerived: originDerived, careerDiff,
    modeBuckets, rowDetails, legTelemetry, regionBuckets,
    highlightsFullOrder: overrides.highlightsFullOrder ?? highlights.full,
    highlightsVariants: overrides.highlightsVariants ?? highlights.variants,
  }
}
