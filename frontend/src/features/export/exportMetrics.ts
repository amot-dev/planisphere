import { realization, stats } from '../../api/models'
import { formatElapsedDuration } from '../../domain/galacticTime'
import { DEFAULT_MODE_COLOR } from '../../domain/theme'
import { ActiveModeTag, collectUniqueShips, getExportDataIndex, getVisitedLegs, getVisitedRegions, VisitedLegRecord } from './exportDataIndex'
import {
  addMetrics, addRealization, computeElapsedDays, createEmptyMetrics, createEmptyRealization,
  formatNetProfitValue, FormattedNetProfit, MILLISECONDS_PER_DAY, safeDivide, safePercent, safePerThousand,
} from './exportFormatting'
import {
  CartBlockItem, ConfiguratorBlockConfig, DETAILS_SECTION_METRICS,
  DetailsMetricDescriptor, DetailsMetricId, ExpeditionExportData, ExportCarrierCost, ExportScopeSpec,
} from './exportTypes'

export * from './exportCartExpansion'
export * from './exportDataIndex'
export * from './exportFormatting'

/** Derived averages, ratios, and profit metrics computed with zero-division and sub-24h guards. */
export interface DerivedMetrics {
  shipDistanceLy: number
  totalBoosts: number
  avgJumpRangeLy: number | null
  boostRatePercent: number | null
  avgDistancePerDayLy: number | null
  avgCarrierJumpRangeLy: number | null
  avgTritiumPerJump: number | null
  combinedEstimatedCredits: number
  dssEfficiencyPercent: number | null
  firstDiscoveryRatePercent: number | null
  firstLoggedRatePercent: number | null
  avgCreditsPerSample: number | null
  avgCombinedCreditsPerJump: number | null
  avgCombinedCreditsPer1000Ly: number | null
  avgCombinedCreditsPerDay: number | null
  avgCartoCreditsPerJump: number | null
  avgCartoCreditsPer1000Ly: number | null
  avgExoCreditsPerJump: number | null
  avgExoCreditsPer1000Ly: number | null
  totalLossesCredits: number
  realizationRetentionPercent: number | null
  netProfitAfterCarrierCost: number | null
  avgRealizedCreditsPerJump: number | null
  avgRealizedCreditsPer1000Ly: number | null
}

/** Identifier for an inline Career Diff comparison metric. */
export type CareerDiffFieldId = 'distanceLy' | 'jumps' | 'estimatedValue' | 'realizedValue' | 'bodiesScanned' | 'bodiesMapped' | 'organicSamples'

/** One inline Career Diff entry comparing pre-expedition baseline and expedition growth. */
export interface CareerDiffItem {
  id: CareerDiffFieldId
  baseline: number
  growthPercent: number | null
  isNewBaseline: boolean
}

/** Fully resolved telemetry context consumed identically by Preview blocks and the HTML generator. */
export interface ResolvedTelemetryContext {
  scopeLabel: string
  routeTitle: string
  modeBadge: ActiveModeTag | null
  fromGalactic: string
  toGalactic: string
  inSystem: boolean
  durationLabel: string
  elapsedDays: number | null
  metrics: stats.Metrics
  realization: realization.Breakdown
  carrierCost: ExportCarrierCost | null
  shipsFlown: string[]
  distinctRegionCount: number | null
  visitedWaypoints: number | null
  plannedWaypoints: number | null
  coveredLy: number | null
  plannedLy: number | null
  derived: DerivedMetrics
  careerDiff: CareerDiffItem[] | null
  isFullExpeditionCombined: boolean
}

/** Resolves the formatted NET PROFIT readout for Full Expedition Combined Summary cards. */
export function resolveFormattedNetProfit(context: ResolvedTelemetryContext): FormattedNetProfit | null {
  if (!context.isFullExpeditionCombined) return null
  const netProfit = context.derived.netProfitAfterCarrierCost ?? context.realization.totalRealized
  return formatNetProfitValue(netProfit, context.carrierCost)
}

/** Reports whether a Details metric checkbox is valid for the current scope, mode, and elapsed span. */
export function isDetailsMetricAvailable(
  descriptor: DetailsMetricDescriptor,
  context: ResolvedTelemetryContext,
  scopeKind: ExportScopeSpec['kind'],
): boolean {
  if (descriptor.fullExpeditionCombinedOnly && !context.isFullExpeditionCombined) return false
  if (descriptor.requiresVisitedRoute && scopeKind === 'baseline') return false
  if (descriptor.requiresElapsed24h && (context.elapsedDays == null || context.elapsedDays < 1)) return false
  return true
}

/** Computes the union of Details metric IDs available across expanded preview slices with early exit. */
export function computeAvailableDetailsMetricIds(
  exportData: ExpeditionExportData,
  config: ConfiguratorBlockConfig,
  expandedItems: ReadonlyArray<CartBlockItem>,
  nowMs: number = Date.now(),
): ReadonlySet<DetailsMetricId> | null {
  if (config.kind !== 'details' || config.section === 'all' || expandedItems.length === 0) return null
  const descriptors = DETAILS_SECTION_METRICS[config.section]
  const achievableCount = countAchievableDescriptors(descriptors, expandedItems)
  const availableMetricIds = new Set<DetailsMetricId>()
  if (achievableCount === 0) return availableMetricIds

  for (const item of expandedItems) {
    if (item.config.kind !== 'details') continue
    const sliceContext = resolveTelemetryContext(exportData, item.config.scope, item.config.selectedModeId, nowMs)
    for (const descriptor of descriptors) {
      if (!availableMetricIds.has(descriptor.id) && isDetailsMetricAvailable(descriptor, sliceContext, item.config.scope.kind)) {
        availableMetricIds.add(descriptor.id)
      }
    }
    if (availableMetricIds.size >= achievableCount) break
  }
  return availableMetricIds
}

function countAchievableDescriptors(
  descriptors: ReadonlyArray<DetailsMetricDescriptor>,
  expandedItems: ReadonlyArray<CartBlockItem>,
): number {
  const hasFullCombined = expandedItems.some(
    (item) => item.config.kind === 'details' && item.config.scope.kind === 'full' && item.config.selectedModeId == null,
  )
  const hasVisitedRoute = expandedItems.some(
    (item) => item.config.kind === 'details' && item.config.scope.kind !== 'baseline',
  )
  return descriptors.filter(
    (descriptor) =>
      (!descriptor.fullExpeditionCombinedOnly || hasFullCombined) &&
      (!descriptor.requiresVisitedRoute || hasVisitedRoute),
  ).length
}

/** Computes all derived averages, percentages, and net profits with safe division and sub-24h guards. */
export function computeDerivedMetrics(
  metrics: stats.Metrics,
  realizationBreakdown: realization.Breakdown,
  carrierCost: ExportCarrierCost | null,
  elapsedDays: number | null,
): DerivedMetrics {
  const shipDistanceLy = metrics.shipDistanceLy ?? 0
  const totalBoosts =
    metrics.jetConeBoosts + metrics.premiumBoosts + metrics.standardBoosts + metrics.basicBoosts
  const combinedEstimatedCredits = metrics.scanValueTotal + metrics.organicValueTotal
  const totalLossesCredits = computeTotalLosses(realizationBreakdown)
  const settledEstimate = combinedEstimatedCredits - realizationBreakdown.pendingValue
  const validDays = elapsedDays != null && elapsedDays >= 1 ? elapsedDays : null

  return {
    shipDistanceLy,
    totalBoosts,
    avgJumpRangeLy: safeDivide(shipDistanceLy, metrics.jumps),
    boostRatePercent: safePercent(totalBoosts, metrics.jumps),
    avgDistancePerDayLy: validDays != null ? metrics.distanceLy / validDays : null,
    avgCarrierJumpRangeLy: safeDivide(metrics.carrierDistanceLy, metrics.carrierJumps),
    avgTritiumPerJump: metrics.tritiumUsed != null ? safeDivide(metrics.tritiumUsed, metrics.carrierJumps) : null,
    combinedEstimatedCredits,
    dssEfficiencyPercent: safePercent(metrics.efficientMappings, metrics.bodiesMapped),
    firstDiscoveryRatePercent: safePercent(metrics.firstDiscoveries, metrics.bodiesScanned),
    firstLoggedRatePercent: safePercent(metrics.firstLogged, metrics.organicSamples),
    avgCreditsPerSample: safeDivide(metrics.organicValueTotal, metrics.organicSamples),
    avgCombinedCreditsPerJump: safeDivide(combinedEstimatedCredits, metrics.jumps),
    avgCombinedCreditsPer1000Ly: safePerThousand(combinedEstimatedCredits, metrics.distanceLy),
    avgCombinedCreditsPerDay: validDays != null ? combinedEstimatedCredits / validDays : null,
    avgCartoCreditsPerJump: safeDivide(metrics.scanValueTotal, metrics.jumps),
    avgCartoCreditsPer1000Ly: safePerThousand(metrics.scanValueTotal, metrics.distanceLy),
    avgExoCreditsPerJump: safeDivide(metrics.organicValueTotal, metrics.jumps),
    avgExoCreditsPer1000Ly: safePerThousand(metrics.organicValueTotal, metrics.distanceLy),
    totalLossesCredits,
    realizationRetentionPercent:
      settledEstimate > 0 ? (realizationBreakdown.totalRealized / settledEstimate) * 100 : null,
    netProfitAfterCarrierCost:
      carrierCost != null ? realizationBreakdown.totalRealized - carrierCost.totalCost : null,
    avgRealizedCreditsPerJump: safeDivide(realizationBreakdown.totalRealized, metrics.jumps),
    avgRealizedCreditsPer1000Ly: safePerThousand(realizationBreakdown.totalRealized, metrics.distanceLy),
  }
}

function computeTotalLosses(realizationBreakdown: realization.Breakdown): number {
  return (
    realizationBreakdown.cartoLostDeath +
    realizationBreakdown.organicLostDeath +
    realizationBreakdown.cartoStolen +
    realizationBreakdown.organicStolen +
    realizationBreakdown.carrierTariffLoss +
    realizationBreakdown.npcCrewLoss
  )
}

/** Resolves all telemetry, labels, derived metrics, and optional Career Diffs for a concrete block scope. */
export function resolveTelemetryContext(
  exportData: ExpeditionExportData,
  scope: ExportScopeSpec,
  selectedModeId?: number,
  nowMs: number = Date.now(),
): ResolvedTelemetryContext {
  switch (scope.kind) {
    case 'baseline':
      return resolveBaselineContext(exportData, nowMs)
    case 'leg':
      return resolveLegContext(exportData, scope.rowId, nowMs)
    case 'region':
      return resolveRegionContext(exportData, scope.regionName, selectedModeId, nowMs)
    case 'allLegs':
      return resolveLegContext(exportData, getVisitedLegs(exportData)[0]?.rowView.row.id, nowMs)
    case 'allRegions':
      return resolveRegionContext(exportData, getVisitedRegions(exportData)[0]?.regionName, selectedModeId, nowMs)
    case 'full':
    default:
      return resolveFullExpeditionContext(exportData, selectedModeId, nowMs)
  }
}

function buildContextBase(
  scopeLabel: string,
  routeTitle: string,
  metrics: stats.Metrics,
  realizationBreakdown: realization.Breakdown,
  carrierCost: ExportCarrierCost | null,
  elapsedDays: number | null,
  overrides: Partial<ResolvedTelemetryContext> = {},
): ResolvedTelemetryContext {
  return {
    scopeLabel, routeTitle, modeBadge: null, fromGalactic: '', toGalactic: '', inSystem: false,
    durationLabel: '', elapsedDays, metrics, realization: realizationBreakdown, carrierCost,
    shipsFlown: [], distinctRegionCount: null, visitedWaypoints: null, plannedWaypoints: null,
    coveredLy: null, plannedLy: null, careerDiff: null, isFullExpeditionCombined: false,
    derived: computeDerivedMetrics(metrics, realizationBreakdown, carrierCost, elapsedDays),
    ...overrides,
  }
}

function resolveFullExpeditionContext(
  exportData: ExpeditionExportData,
  selectedModeId: number | undefined,
  nowMs: number,
): ResolvedTelemetryContext {
  const modeBucket =
    selectedModeId != null ? exportData.modeBuckets.find((bucket) => bucket.modeId === selectedModeId) : undefined
  const isCombined = modeBucket == null
  const metrics = modeBucket ? modeBucket.metrics : exportData.expedition.metrics
  const realizationBreakdown = modeBucket ? modeBucket.realization : exportData.expedition.realization
  const carrierCost = isCombined ? exportData.carrierCost : null
  const dataIndex = getExportDataIndex(exportData)
  const matchingLegs = modeBucket
    ? dataIndex.visitedLegs.filter((leg) => leg.rowView.legModeId === modeBucket.modeId)
    : dataIndex.visitedLegs
  const elapsedDays = computeElapsedDays(exportData.effectiveStart, exportData.effectiveEnd, nowMs)
  const scopeLabel = modeBucket ? `Full Expedition — ${modeBucket.modeName}` : 'Full Expedition'

  return buildContextBase(scopeLabel, exportData.plan.expedition.name, metrics, realizationBreakdown, carrierCost, elapsedDays, {
    modeBadge: modeBucket ? { modeId: modeBucket.modeId, modeName: modeBucket.modeName, modeColor: modeBucket.modeColor } : null,
    fromGalactic: exportData.effectiveStartGalactic || 'Unknown',
    toGalactic: exportData.endIsNow ? 'ACTIVE' : exportData.effectiveEndGalactic || 'Unknown',
    durationLabel: exportData.duration || formatElapsedDuration(exportData.effectiveStart, exportData.effectiveEnd, nowMs),
    shipsFlown: collectUniqueShips(matchingLegs),
    distinctRegionCount: isCombined ? dataIndex.visitedRegions.length : null,
    visitedWaypoints: exportData.plan.visitedWaypointCount,
    plannedWaypoints: exportData.plan.waypointCount,
    coveredLy: exportData.plan.coveredLy,
    plannedLy: exportData.plan.plannedLy,
    careerDiff: isCombined ? buildCareerDiff(exportData) : null,
    isFullExpeditionCombined: isCombined,
  })
}

function resolveBaselineContext(exportData: ExpeditionExportData, nowMs: number): ResolvedTelemetryContext {
  const baseline = exportData.originBaseline
  const metrics = baseline?.metrics ?? createEmptyMetrics()
  const realizationBreakdown = baseline?.realization ?? createEmptyRealization()
  const elapsedDays = computeElapsedDays(baseline?.from, baseline?.to, nowMs)
  return buildContextBase('Pre-Expedition Baseline', exportData.plan.expedition.name, metrics, realizationBreakdown, null, elapsedDays, {
    fromGalactic: baseline?.fromGalactic || 'Earliest Archive',
    toGalactic: baseline?.toGalactic || exportData.effectiveStartGalactic || 'Unknown',
    inSystem: Boolean(baseline?.inSystem),
    durationLabel: baseline?.elapsed || formatElapsedDuration(baseline?.from, baseline?.to, nowMs),
  })
}

function resolveLegContext(exportData: ExpeditionExportData, rowId: number | undefined, nowMs: number): ResolvedTelemetryContext {
  const dataIndex = getExportDataIndex(exportData)
  const leg = (rowId != null ? dataIndex.visitedLegByRowId.get(rowId) : undefined) ?? dataIndex.visitedLegs[0]
  if (!leg) {
    return buildContextBase('Selected Leg', exportData.plan.expedition.name, createEmptyMetrics(), createEmptyRealization(), null, null)
  }
  const elapsedDays = computeElapsedDays(leg.details.from, leg.details.to, nowMs)
  const scopeLabel = `Leg #${leg.details.number - 1} to #${leg.details.number}`
  const routeTitle = `${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`
  return buildContextBase(scopeLabel, routeTitle, leg.details.metrics, leg.details.realization, null, elapsedDays, {
    modeBadge: leg.rowView.legModeName
      ? { modeId: leg.rowView.legModeId, modeName: leg.rowView.legModeName, modeColor: leg.rowView.legModeColor || DEFAULT_MODE_COLOR }
      : null,
    fromGalactic: leg.details.fromGalactic || '',
    toGalactic: leg.details.toGalactic || (leg.details.inSystem ? 'IN SYSTEM' : 'IN PROGRESS'),
    inSystem: Boolean(leg.details.inSystem),
    durationLabel: leg.details.elapsed || formatElapsedDuration(leg.details.from, leg.details.to, nowMs),
    shipsFlown: collectUniqueShips([leg]),
    coveredLy: leg.rowView.distFromPrevLy ?? null,
  })
}

function resolveRegionContext(
  exportData: ExpeditionExportData,
  regionName: string | undefined,
  selectedModeId: number | undefined,
  nowMs: number,
): ResolvedTelemetryContext {
  const dataIndex = getExportDataIndex(exportData)
  const region =
    (regionName != null ? dataIndex.visitedRegionByName.get(regionName) : undefined) ?? dataIndex.visitedRegions[0]
  const matchingLegs = region
    ? selectedModeId != null ? region.legs.filter((leg) => leg.rowView.legModeId === selectedModeId) : region.legs
    : []
  const aggregated = aggregateRegionLegs(matchingLegs, nowMs)
  const modeTag =
    selectedModeId != null ? region?.distinctModes.find((mode) => mode.modeId === selectedModeId) ?? null : null
  const resolvedName = region?.regionName ?? 'Selected Region'
  const scopeLabel = modeTag ? `Region: ${resolvedName} (${modeTag.modeName})` : `Region: ${resolvedName}`

  return buildContextBase(scopeLabel, exportData.plan.expedition.name, aggregated.metrics, aggregated.realizationBreakdown, null, aggregated.elapsedDays, {
    modeBadge: modeTag,
    fromGalactic: aggregated.firstFromGalactic,
    toGalactic: aggregated.lastToGalactic,
    inSystem: aggregated.anyInSystem,
    durationLabel: aggregated.durationLabel,
    shipsFlown: collectUniqueShips(matchingLegs),
    visitedWaypoints: matchingLegs.length,
    coveredLy: matchingLegs.reduce((sum, leg) => sum + (leg.rowView.distFromPrevLy ?? 0), 0),
  })
}

function aggregateRegionLegs(matchingLegs: VisitedLegRecord[], nowMs: number) {
  let metrics = createEmptyMetrics()
  let realizationBreakdown = createEmptyRealization()
  let totalElapsedMs = 0
  let firstFromGalactic = ''
  let lastToGalactic = ''
  let anyInSystem = false

  for (const leg of matchingLegs) {
    metrics = addMetrics(metrics, leg.details.metrics)
    realizationBreakdown = addRealization(realizationBreakdown, leg.details.realization)
    if (!firstFromGalactic && leg.details.fromGalactic) firstFromGalactic = leg.details.fromGalactic
    if (leg.details.toGalactic) lastToGalactic = leg.details.toGalactic
    if (leg.details.inSystem) {
      anyInSystem = true
      lastToGalactic = 'IN SYSTEM'
    }
    const legDays = computeElapsedDays(leg.details.from, leg.details.to, nowMs)
    if (legDays != null) totalElapsedMs += legDays * MILLISECONDS_PER_DAY
  }
  return {
    metrics, realizationBreakdown, firstFromGalactic, lastToGalactic, anyInSystem,
    elapsedDays: totalElapsedMs > 0 ? totalElapsedMs / MILLISECONDS_PER_DAY : null,
    durationLabel: totalElapsedMs > 0 ? formatElapsedDuration(new Date(0).toISOString(), new Date(totalElapsedMs).toISOString()) : '',
  }
}

function buildCareerDiff(exportData: ExpeditionExportData): CareerDiffItem[] | null {
  const baseline = exportData.originBaseline
  if (!baseline) return null
  const baselineMetrics = baseline.metrics
  const expeditionMetrics = exportData.live
  const baselineRealization = baseline.realization.totalRealized
  const expeditionRealization = exportData.expedition.realization.totalRealized
  const makeItem = (id: CareerDiffFieldId, baselineValue: number, delta: number): CareerDiffItem => ({
    id,
    baseline: baselineValue,
    growthPercent: baselineValue > 0 ? (delta / baselineValue) * 100 : null,
    isNewBaseline: baselineValue <= 0 && delta > 0,
  })
  return [
    makeItem('distanceLy', baselineMetrics.distanceLy, expeditionMetrics.distanceLy),
    makeItem('jumps', baselineMetrics.jumps, expeditionMetrics.jumps),
    makeItem('estimatedValue', baselineMetrics.scanValueTotal + baselineMetrics.organicValueTotal, expeditionMetrics.scanValueTotal + expeditionMetrics.organicValueTotal),
    makeItem('realizedValue', baselineRealization, expeditionRealization),
    makeItem('bodiesScanned', baselineMetrics.bodiesScanned, expeditionMetrics.bodiesScanned),
    makeItem('bodiesMapped', baselineMetrics.bodiesMapped, expeditionMetrics.bodiesMapped),
    makeItem('organicSamples', baselineMetrics.organicSamples, expeditionMetrics.organicSamples),
  ]
}
