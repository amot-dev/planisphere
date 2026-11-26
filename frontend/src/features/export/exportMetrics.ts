import { realization, stats } from '../../api/models'
import { formatElapsedDuration } from '../../domain/galacticTime'
import { DEFAULT_MODE_COLOR } from '../../domain/theme'
import { ActiveModeTag, getExportDataIndex, getVisitedLegs, getVisitedRegions } from './exportDataIndex'
import { formatNetProfitValue, FormattedNetProfit } from './exportFormatting'
import {
  CartBlockItem, ConfiguratorBlockConfig, DETAILS_SECTION_METRICS,
  DetailsMetricDescriptor, DetailsMetricId, ExpeditionExportData,
  ExportCareerDiffItem, ExportCarrierCost, ExportDerivedMetrics, ExportScopeSpec,
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

const ZERO_METRICS: stats.Metrics = Object.freeze({
  distanceLy: 0, shipDistanceLy: 0, carrierPassengerLy: 0, jumps: 0,
  carrierJumps: 0, carrierDistanceLy: 0, tritiumUsed: undefined,
  jetConeBoosts: 0, premiumBoosts: 0, standardBoosts: 0, basicBoosts: 0,
  bodiesScanned: 0, bodiesMapped: 0, efficientMappings: 0,
  scanValueTotal: 0, scanValueBase: 0, scanValueBonus: 0, scanValueEfficiencyBonus: 0,
  firstDiscoveries: 0, firstMapped: 0, organicSamples: 0,
  organicValueTotal: 0, organicValueBase: 0, organicValueBonus: 0, firstLogged: 0,
})

const ZERO_REALIZATION: realization.Breakdown = Object.freeze({
  totalRealized: 0, cartoLostDeath: 0, organicLostDeath: 0, cartoStolen: 0,
  organicStolen: 0, carrierTariffLoss: 0, npcCrewLoss: 0, pendingValue: 0,
})

const ZERO_DERIVED_METRICS: DerivedMetrics = Object.freeze({
  shipDistanceLy: 0, totalBoosts: 0, avgJumpRangeLy: null, boostRatePercent: null,
  avgDistancePerDayLy: null, avgCarrierJumpRangeLy: null, avgTritiumPerJump: null,
  combinedEstimatedCredits: 0, dssEfficiencyPercent: null, firstDiscoveryRatePercent: null,
  firstLoggedRatePercent: null, avgCreditsPerSample: null, avgCombinedCreditsPerJump: null,
  avgCombinedCreditsPer1000Ly: null, avgCombinedCreditsPerDay: null,
  avgCartoCreditsPerJump: null, avgCartoCreditsPer1000Ly: null,
  avgExoCreditsPerJump: null, avgExoCreditsPer1000Ly: null, totalLossesCredits: 0,
  realizationRetentionPercent: null, netProfitAfterCarrierCost: null,
  avgRealizedCreditsPerJump: null, avgRealizedCreditsPer1000Ly: null,
})

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
      return resolveRegionContext(exportData, scope.regionName, selectedModeId)
    case 'allLegs':
      return resolveLegContext(exportData, getVisitedLegs(exportData)[0]?.rowView.row.id, nowMs)
    case 'allRegions':
      return resolveRegionContext(exportData, getVisitedRegions(exportData)[0]?.regionName, selectedModeId)
    case 'full':
    default:
      return resolveFullExpeditionContext(exportData, selectedModeId, nowMs)
  }
}

function normalizeDerivedMetrics(raw: ExportDerivedMetrics | null | undefined): DerivedMetrics {
  if (!raw) return ZERO_DERIVED_METRICS
  const normalized = { ...ZERO_DERIVED_METRICS }
  for (const key of Object.keys(ZERO_DERIVED_METRICS) as Array<keyof DerivedMetrics>) {
    normalized[key] = (raw[key] ?? ZERO_DERIVED_METRICS[key]) as never
  }
  return normalized
}

function normalizeCareerDiff(items: ReadonlyArray<ExportCareerDiffItem> | null | undefined): CareerDiffItem[] | null {
  if (!items) return null
  return items.map((item) => ({
    id: item.id as CareerDiffFieldId,
    baseline: item.baseline,
    growthPercent: item.growthPercent ?? null,
    isNewBaseline: item.isNewBaseline,
  }))
}

function buildContextBase(
  scopeLabel: string,
  routeTitle: string,
  metrics: stats.Metrics,
  realizationBreakdown: realization.Breakdown,
  carrierCost: ExportCarrierCost | null,
  elapsedDays: number | null,
  derived: DerivedMetrics,
  overrides: Partial<ResolvedTelemetryContext> = {},
): ResolvedTelemetryContext {
  return {
    scopeLabel, routeTitle, modeBadge: null, fromGalactic: '', toGalactic: '', inSystem: false,
    durationLabel: '', elapsedDays, metrics, realization: realizationBreakdown, carrierCost,
    shipsFlown: [], distinctRegionCount: null, visitedWaypoints: null, plannedWaypoints: null,
    coveredLy: null, plannedLy: null, careerDiff: null, isFullExpeditionCombined: false,
    derived,
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
  const slice = modeBucket ?? exportData.expedition
  const carrierCost = isCombined ? exportData.carrierCost : null
  const dataIndex = getExportDataIndex(exportData)
  const scopeLabel = modeBucket ? `Full Expedition — ${modeBucket.modeName}` : 'Full Expedition'

  return buildContextBase(scopeLabel, exportData.plan.expedition.name, slice.metrics, slice.realization, carrierCost, slice.elapsedDays ?? null, normalizeDerivedMetrics(slice.derived), {
    modeBadge: modeBucket ? { modeId: modeBucket.modeId, modeName: modeBucket.modeName, modeColor: modeBucket.modeColor } : null,
    fromGalactic: exportData.effectiveStartGalactic || 'Unknown',
    toGalactic: exportData.endIsNow ? 'ACTIVE' : exportData.effectiveEndGalactic || 'Unknown',
    durationLabel: exportData.duration || formatElapsedDuration(exportData.effectiveStart, exportData.effectiveEnd, nowMs),
    shipsFlown: slice.shipsFlown ?? [],
    distinctRegionCount: isCombined ? dataIndex.visitedRegions.length : null,
    visitedWaypoints: exportData.plan.visitedWaypointCount,
    plannedWaypoints: exportData.plan.waypointCount,
    coveredLy: exportData.plan.coveredLy,
    plannedLy: exportData.plan.plannedLy,
    careerDiff: isCombined ? normalizeCareerDiff(exportData.careerDiff) : null,
    isFullExpeditionCombined: isCombined,
  })
}

function resolveBaselineContext(exportData: ExpeditionExportData, nowMs: number): ResolvedTelemetryContext {
  const baseline = exportData.originBaseline
  const metrics = baseline?.metrics ?? ZERO_METRICS
  const realizationBreakdown = baseline?.realization ?? ZERO_REALIZATION
  const elapsedDays = baseline ? (exportData.originBaselineElapsedDays ?? null) : null
  const derived = baseline ? normalizeDerivedMetrics(exportData.originBaselineDerived) : ZERO_DERIVED_METRICS
  return buildContextBase('Pre-Expedition Baseline', exportData.plan.expedition.name, metrics, realizationBreakdown, null, elapsedDays, derived, {
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
    return buildContextBase('Selected Leg', exportData.plan.expedition.name, ZERO_METRICS, ZERO_REALIZATION, null, null, ZERO_DERIVED_METRICS)
  }
  const scopeLabel = `Leg #${leg.details.number - 1} to #${leg.details.number}`
  const routeTitle = `${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`
  return buildContextBase(scopeLabel, routeTitle, leg.details.metrics, leg.details.realization, null, leg.telemetry.elapsedDays ?? null, normalizeDerivedMetrics(leg.telemetry.derived), {
    modeBadge: leg.rowView.legModeName
      ? { modeId: leg.rowView.legModeId, modeName: leg.rowView.legModeName, modeColor: leg.rowView.legModeColor || DEFAULT_MODE_COLOR }
      : null,
    fromGalactic: leg.details.fromGalactic || '',
    toGalactic: leg.details.toGalactic || (leg.details.inSystem ? 'IN SYSTEM' : 'IN PROGRESS'),
    inSystem: Boolean(leg.details.inSystem),
    durationLabel: leg.details.elapsed || formatElapsedDuration(leg.details.from, leg.details.to, nowMs),
    shipsFlown: leg.telemetry.shipsFlown ?? [],
    coveredLy: leg.rowView.distFromPrevLy ?? null,
  })
}

function resolveRegionContext(
  exportData: ExpeditionExportData,
  regionName: string | undefined,
  selectedModeId: number | undefined,
): ResolvedTelemetryContext {
  const dataIndex = getExportDataIndex(exportData)
  const region =
    (regionName != null ? dataIndex.visitedRegionByName.get(regionName) : undefined) ?? dataIndex.visitedRegions[0]
  if (!region) {
    return buildContextBase('Region: Selected Region', exportData.plan.expedition.name, ZERO_METRICS, ZERO_REALIZATION, null, null, ZERO_DERIVED_METRICS, { visitedWaypoints: 0, coveredLy: 0 })
  }
  const modeBucket =
    selectedModeId != null ? region.bucket.modeBuckets.find((bucket) => bucket.modeId === selectedModeId) : undefined
  if (selectedModeId != null && !modeBucket) {
    return buildContextBase(`Region: ${region.regionName}`, exportData.plan.expedition.name, ZERO_METRICS, ZERO_REALIZATION, null, null, ZERO_DERIVED_METRICS, { visitedWaypoints: 0, coveredLy: 0 })
  }
  const slice = modeBucket ?? region.bucket
  const modeBadge = modeBucket ? { modeId: modeBucket.modeId, modeName: modeBucket.modeName, modeColor: modeBucket.modeColor } : null
  const scopeLabel = modeBucket ? `Region: ${region.regionName} (${modeBucket.modeName})` : `Region: ${region.regionName}`

  return buildContextBase(scopeLabel, exportData.plan.expedition.name, slice.metrics, slice.realization, null, slice.elapsedDays ?? null, normalizeDerivedMetrics(slice.derived), {
    modeBadge,
    fromGalactic: slice.fromGalactic,
    toGalactic: slice.toGalactic,
    inSystem: slice.inSystem,
    durationLabel: slice.duration,
    shipsFlown: slice.shipsFlown ?? [],
    visitedWaypoints: slice.legCount,
    coveredLy: slice.coveredLy,
  })
}
