import { expedition, realization, stats } from '../../api/models'
import {
  computeShipDistanceLy,
  formatCredits,
  formatElapsedDuration,
} from '../../domain/galacticTime'
import {
  CartBlockItem,
  defaultMetricsForSection,
  DETAILS_SECTIONS,
  DetailsMetricDescriptor,
  DetailsSectionSelection,
  ExpeditionExportData,
  ExportBlockConfig,
  ExportCarrierCost,
  ExportScopeSpec,
  HIGHLIGHT_CARDS,
  WAYPOINTS_COLUMNS,
} from './exportTypes'

const MILLISECONDS_PER_DAY = 24 * 60 * 60 * 1000
const EXPORT_HEX_COLOR_PATTERN = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

/** Validates a CSS hex color (#RGB or #RRGGBB), returning fallback for malformed inputs. */
export function sanitizeExportHexColor(
  rawColor: string | null | undefined,
  fallback: string = '#FF7100',
): string {
  const trimmedColor = rawColor?.trim() ?? ''
  return EXPORT_HEX_COLOR_PATTERN.test(trimmedColor) ? trimmedColor : fallback
}

/** Formats carrier credit values with a tilde prefix when estimated and greater than zero. */
export function formatEstimatedCarrierCredits(
  amount: number,
  isEstimated: boolean = amount > 0,
): string {
  return isEstimated && amount > 0 ? `~${formatCredits(amount)}` : formatCredits(amount)
}

/** Visited non-origin leg enriched with its drawer details and attributed Galactic Region. */
export interface VisitedLegRecord {
  rowView: expedition.RowView
  details: expedition.WaypointDetails
  regionName: string
}

/** Distinct visited Galactic Region grouping its attributed visited legs and active flight modes. */
export interface VisitedRegionSummary {
  regionName: string
  legs: VisitedLegRecord[]
  distinctModes: ActiveModeTag[]
}

/** Lightweight flight mode identifier paired with display name and cockpit color. */
export interface ActiveModeTag {
  modeId: number
  modeName: string
  modeColor: string
}

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

/** One inline Career Diff entry comparing pre-expedition baseline, expedition delta, and post-expedition career. */
export interface CareerDiffItem {
  id:
    | 'distanceLy'
    | 'jumps'
    | 'estimatedValue'
    | 'realizedValue'
    | 'bodiesScanned'
    | 'bodiesMapped'
    | 'organicSamples'
  baseline: number
  delta: number
  postCareer: number
  growthPercent: number | null
  careerSharePercent: number | null
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

/** Returns an empty zeroed Metrics object structurally matching Wails stats.Metrics. */
export function createEmptyMetrics(): stats.Metrics {
  return {
    distanceLy: 0,
    shipDistanceLy: 0,
    carrierPassengerLy: 0,
    jumps: 0,
    carrierJumps: 0,
    carrierDistanceLy: 0,
    tritiumUsed: undefined,
    jetConeBoosts: 0,
    premiumBoosts: 0,
    standardBoosts: 0,
    basicBoosts: 0,
    bodiesScanned: 0,
    bodiesMapped: 0,
    efficientMappings: 0,
    scanValueTotal: 0,
    scanValueBase: 0,
    scanValueBonus: 0,
    scanValueEfficiencyBonus: 0,
    firstDiscoveries: 0,
    firstMapped: 0,
    organicSamples: 0,
    organicValueTotal: 0,
    organicValueBase: 0,
    organicValueBonus: 0,
    firstLogged: 0,
  }
}

/** Returns an empty zeroed RealizationBreakdown object. */
export function createEmptyRealization(): realization.Breakdown {
  return {
    totalRealized: 0,
    cartoLostDeath: 0,
    organicLostDeath: 0,
    cartoStolen: 0,
    organicStolen: 0,
    carrierTariffLoss: 0,
    npcCrewLoss: 0,
    pendingValue: 0,
  }
}

/** Sums two Metrics objects field-by-field. */
export function addMetrics(left: stats.Metrics, right: stats.Metrics): stats.Metrics {
  const hasTritium = left.tritiumUsed != null || right.tritiumUsed != null
  return {
    distanceLy: left.distanceLy + right.distanceLy,
    shipDistanceLy: left.shipDistanceLy + right.shipDistanceLy,
    carrierPassengerLy: left.carrierPassengerLy + right.carrierPassengerLy,
    jumps: left.jumps + right.jumps,
    carrierJumps: left.carrierJumps + right.carrierJumps,
    carrierDistanceLy: left.carrierDistanceLy + right.carrierDistanceLy,
    tritiumUsed: hasTritium ? (left.tritiumUsed ?? 0) + (right.tritiumUsed ?? 0) : undefined,
    jetConeBoosts: left.jetConeBoosts + right.jetConeBoosts,
    premiumBoosts: left.premiumBoosts + right.premiumBoosts,
    standardBoosts: left.standardBoosts + right.standardBoosts,
    basicBoosts: left.basicBoosts + right.basicBoosts,
    bodiesScanned: left.bodiesScanned + right.bodiesScanned,
    bodiesMapped: left.bodiesMapped + right.bodiesMapped,
    efficientMappings: left.efficientMappings + right.efficientMappings,
    scanValueTotal: left.scanValueTotal + right.scanValueTotal,
    scanValueBase: left.scanValueBase + right.scanValueBase,
    scanValueBonus: left.scanValueBonus + right.scanValueBonus,
    scanValueEfficiencyBonus: left.scanValueEfficiencyBonus + right.scanValueEfficiencyBonus,
    firstDiscoveries: left.firstDiscoveries + right.firstDiscoveries,
    firstMapped: left.firstMapped + right.firstMapped,
    organicSamples: left.organicSamples + right.organicSamples,
    organicValueTotal: left.organicValueTotal + right.organicValueTotal,
    organicValueBase: left.organicValueBase + right.organicValueBase,
    organicValueBonus: left.organicValueBonus + right.organicValueBonus,
    firstLogged: left.firstLogged + right.firstLogged,
  }
}

/** Sums two RealizationBreakdown objects field-by-field. */
export function addRealization(
  left: realization.Breakdown,
  right: realization.Breakdown,
): realization.Breakdown {
  return {
    totalRealized: left.totalRealized + right.totalRealized,
    cartoLostDeath: left.cartoLostDeath + right.cartoLostDeath,
    organicLostDeath: left.organicLostDeath + right.organicLostDeath,
    cartoStolen: left.cartoStolen + right.cartoStolen,
    organicStolen: left.organicStolen + right.organicStolen,
    carrierTariffLoss: left.carrierTariffLoss + right.carrierTariffLoss,
    npcCrewLoss: left.npcCrewLoss + right.npcCrewLoss,
    pendingValue: left.pendingValue + right.pendingValue,
  }
}

/** Returns ordered visited non-origin legs (WP #2+) paired with their drawer telemetry and attributed Galactic Region. */
export function getVisitedLegs(exportData: ExpeditionExportData): VisitedLegRecord[] {
  const detailsByRowId = new Map<number, expedition.WaypointDetails>()
  for (const detailEntry of exportData.rowDetails ?? []) {
    detailsByRowId.set(detailEntry.rowId, detailEntry)
  }

  let activeManualRegion = ''
  const visitedLegs: VisitedLegRecord[] = []
  for (const rowView of exportData.plan.rows ?? []) {
    if (rowView.row.rowType === 'region') {
      activeManualRegion = rowView.regionName || activeManualRegion
      continue
    }
    if (rowView.number <= 1 || !rowView.row.visitedAt) {
      continue
    }
    const details = detailsByRowId.get(rowView.row.id)
    if (!details) {
      continue
    }
    const attributedRegion = rowView.detectedRegion || activeManualRegion || 'Unknown Region'
    visitedLegs.push({ rowView, details, regionName: attributedRegion })
  }
  return visitedLegs
}

/** Groups visited legs by distinct Galactic Region in order of first arrival. */
export function getVisitedRegions(exportData: ExpeditionExportData): VisitedRegionSummary[] {
  const legs = getVisitedLegs(exportData)
  const regionMap = new Map<string, VisitedLegRecord[]>()
  for (const leg of legs) {
    const existing = regionMap.get(leg.regionName)
    if (existing) {
      existing.push(leg)
    } else {
      regionMap.set(leg.regionName, [leg])
    }
  }
  const regionSummaries: VisitedRegionSummary[] = []
  for (const [regionName, regionLegs] of regionMap.entries()) {
    regionSummaries.push({
      regionName,
      legs: regionLegs,
      distinctModes: extractDistinctModesFromLegs(regionLegs),
    })
  }
  return regionSummaries
}

/** Returns the distinct active flight modes spanning the requested scope. */
export function getDistinctModesForScope(
  exportData: ExpeditionExportData,
  scope: ExportScopeSpec,
): ActiveModeTag[] {
  switch (scope.kind) {
    case 'baseline':
    case 'leg':
    case 'allLegs':
      return []
    case 'full':
      return (exportData.modeBuckets ?? []).map((bucket) => ({
        modeId: bucket.modeId,
        modeName: bucket.modeName,
        modeColor: bucket.modeColor,
      }))
    case 'region': {
      const regionSummary = getVisitedRegions(exportData).find(
        (region) => region.regionName === scope.regionName,
      )
      return regionSummary ? regionSummary.distinctModes : []
    }
    case 'allRegions': {
      const allRegionModes = new Map<number, ActiveModeTag>()
      for (const region of getVisitedRegions(exportData)) {
        for (const modeTag of region.distinctModes) {
          allRegionModes.set(modeTag.modeId, modeTag)
        }
      }
      return Array.from(allRegionModes.values())
    }
  }
}

/** Reports whether the Combined / Separate mode control should appear for a scope and optional section. */
export function scopeSupportsModeSplit(
  exportData: ExpeditionExportData,
  scope: ExportScopeSpec,
  detailsSection?: DetailsSectionSelection,
): boolean {
  if (detailsSection === 'carrier') {
    return false
  }
  return getDistinctModesForScope(exportData, scope).length >= 2
}

/** Reports whether a Details metric checkbox is valid for the current scope, mode, and elapsed span. */
export function isDetailsMetricAvailable(
  descriptor: DetailsMetricDescriptor,
  context: ResolvedTelemetryContext,
  scopeKind: ExportScopeSpec['kind'],
): boolean {
  if (descriptor.fullExpeditionCombinedOnly && !context.isFullExpeditionCombined) {
    return false
  }
  if (descriptor.requiresVisitedRoute && scopeKind === 'baseline') {
    return false
  }
  if (descriptor.requiresElapsed24h && (context.elapsedDays == null || context.elapsedDays < 1)) {
    return false
  }
  return true
}

/** Computes all derived averages, percentages, and net profits with safe division and sub-24h guards. */
export function computeDerivedMetrics(
  metrics: stats.Metrics,
  realizationBreakdown: realization.Breakdown,
  carrierCost: ExportCarrierCost | null,
  elapsedDays: number | null,
): DerivedMetrics {
  const shipDistanceLy = computeShipDistanceLy(metrics)
  const totalBoosts =
    metrics.jetConeBoosts +
    metrics.premiumBoosts +
    metrics.standardBoosts +
    metrics.basicBoosts
  const combinedEstimatedCredits = metrics.scanValueTotal + metrics.organicValueTotal
  const totalLossesCredits =
    realizationBreakdown.cartoLostDeath +
    realizationBreakdown.organicLostDeath +
    realizationBreakdown.cartoStolen +
    realizationBreakdown.organicStolen +
    realizationBreakdown.carrierTariffLoss +
    realizationBreakdown.npcCrewLoss
  const settledEstimate = combinedEstimatedCredits - realizationBreakdown.pendingValue
  const validDays = elapsedDays != null && elapsedDays >= 1 ? elapsedDays : null

  return {
    shipDistanceLy,
    totalBoosts,
    avgJumpRangeLy: safeDivide(shipDistanceLy, metrics.jumps),
    boostRatePercent: safePercent(totalBoosts, metrics.jumps),
    avgDistancePerDayLy: validDays != null ? metrics.distanceLy / validDays : null,
    avgCarrierJumpRangeLy: safeDivide(metrics.carrierDistanceLy, metrics.carrierJumps),
    avgTritiumPerJump:
      metrics.tritiumUsed != null ? safeDivide(metrics.tritiumUsed, metrics.carrierJumps) : null,
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
    avgRealizedCreditsPer1000Ly: safePerThousand(
      realizationBreakdown.totalRealized,
      metrics.distanceLy,
    ),
  }
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
    case 'allLegs': {
      const firstLeg = getVisitedLegs(exportData)[0]
      return resolveLegContext(exportData, firstLeg?.rowView.row.id, nowMs)
    }
    case 'allRegions': {
      const firstRegion = getVisitedRegions(exportData)[0]
      return resolveRegionContext(exportData, firstRegion?.regionName, selectedModeId, nowMs)
    }
    case 'full':
    default:
      return resolveFullExpeditionContext(exportData, selectedModeId, nowMs)
  }
}

/** Expands a configurator block (including All Legs, All Regions, and Separate modes) into concrete cart items. */
export function expandConfigToCartItems(
  exportData: ExpeditionExportData,
  config: ExportBlockConfig,
  idPrefix: string,
): CartBlockItem[] {
  if (config.kind === 'waypoints') {
    const scopeText = config.waypointsScope === 'fullPlan' ? 'Full Plan' : 'Visited Only'
    return [
      {
        id: `${idPrefix}-waypoints`,
        title: `Waypoints (${scopeText})`,
        subtitle: `${config.selectedColumns.length} columns`,
        filenameSlug: `waypoints-${slugify(scopeText)}`,
        subfolder: 'expedition',
        config: { ...config, selectedColumns: [...config.selectedColumns] },
      },
    ]
  }
  if (config.kind === 'highlights') {
    return [
      {
        id: `${idPrefix}-highlights`,
        title: `Highlights (Top ${config.depth})`,
        subtitle: `${config.selectedCards.length} cards`,
        filenameSlug: `highlights-top-${config.depth}`,
        subfolder: 'expedition',
        config: { ...config, selectedCards: [...config.selectedCards] },
      },
    ]
  }
  if (config.kind === 'details' && config.section === 'all') {
    const items: CartBlockItem[] = []
    if (config.scope.kind === 'allLegs') {
      const applicableSections = DETAILS_SECTIONS.filter(
        (sectionMeta) => sectionMeta.id !== 'carrier',
      )
      for (const [legIndex, leg] of getVisitedLegs(exportData).entries()) {
        const legTitle = `Leg #${leg.details.number - 1} to #${leg.details.number}`
        const legSubtitle = `${leg.details.prevSystem || 'Origin'} to ${leg.rowView.row.systemName}`
        for (const sectionMeta of applicableSections) {
          const blockLabel = `Details: ${sectionMeta.label}`
          const blockSlug = `details-${slugify(sectionMeta.id)}`
          items.push({
            id: `${idPrefix}-leg-${leg.rowView.row.id}-${legIndex}-${sectionMeta.id}`,
            title: `${blockLabel} — ${legTitle}`,
            subtitle: legSubtitle,
            filenameSlug: `${String(legIndex + 1).padStart(2, '0')}-${blockSlug}-wp${leg.details.number}`,
            subfolder: 'by-leg',
            config: {
              kind: 'details',
              section: sectionMeta.id,
              scope: { kind: 'leg', rowId: leg.rowView.row.id },
              modeGrouping: 'combined',
              selectedMetrics: defaultMetricsForSection(sectionMeta.id),
            },
          })
        }
      }
      return items
    }
    for (const sectionMeta of DETAILS_SECTIONS) {
      if (sectionMeta.id === 'carrier' && config.scope.kind !== 'full') {
        continue
      }
      items.push(
        ...expandTelemetryBlockConfig(
          exportData,
          {
            ...config,
            section: sectionMeta.id,
            selectedMetrics: defaultMetricsForSection(sectionMeta.id),
          },
          `${idPrefix}-${sectionMeta.id}`,
        ),
      )
    }
    return items
  }
  return expandTelemetryBlockConfig(exportData, config, idPrefix)
}

/** Builds the Quick Share 3-card preset cart (Summary + Highlights Top 1 + Waypoints). */
export function createQuickShareCart(exportData: ExpeditionExportData): CartBlockItem[] {
  const items: CartBlockItem[] = []
  items.push(
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'summary',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        careerDiff: 'percentage',
      },
      'quick-1',
    ),
  )
  if ((exportData.plan.visitedWaypointCount ?? 0) >= 2) {
    items.push(
      ...expandConfigToCartItems(
        exportData,
        {
          kind: 'highlights',
          depth: 1,
          selectedCards: HIGHLIGHT_CARDS.filter((card) => card.defaultChecked).map(
            (card) => card.id,
          ),
          lucrativeMetric: 'estimated',
          distanceMetric: 'total',
        },
        'quick-2',
      ),
    )
  }
  items.push(
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'waypoints',
        waypointsScope: 'visited',
        selectedColumns: WAYPOINTS_COLUMNS.filter((column) => column.defaultChecked).map(
          (column) => column.id,
        ),
      },
      'quick-3',
    ),
  )
  return items
}

/** Converts a display title into a clean lowercase filename slug. */
export function slugify(rawText: string): string {
  const cleaned = rawText
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return cleaned || 'block'
}

function expandTelemetryBlockConfig(
  exportData: ExpeditionExportData,
  config: Extract<ExportBlockConfig, { kind: 'summary' | 'details' }>,
  idPrefix: string,
): CartBlockItem[] {
  const blockLabel =
    config.kind === 'summary'
      ? 'Summary'
      : `Details: ${DETAILS_SECTIONS.find((section) => section.id === config.section)?.label ?? config.section}`
  const blockSlug = config.kind === 'summary' ? 'summary' : `details-${slugify(config.section)}`

  if (config.scope.kind === 'allLegs') {
    return expandAllLegsItems(exportData, config, idPrefix, blockLabel, blockSlug)
  }
  if (config.scope.kind === 'allRegions') {
    return expandAllRegionsItems(exportData, config, idPrefix, blockLabel, blockSlug)
  }
  return expandSingleScopeItems(
    exportData,
    config,
    idPrefix,
    blockLabel,
    blockSlug,
  )
}

function expandAllLegsItems(
  exportData: ExpeditionExportData,
  config: Extract<ExportBlockConfig, { kind: 'summary' | 'details' }>,
  idPrefix: string,
  blockLabel: string,
  blockSlug: string,
): CartBlockItem[] {
  return getVisitedLegs(exportData).map((leg, index) => {
    const concreteConfig = cloneConcreteTelemetryConfig(
      config,
      { kind: 'leg', rowId: leg.rowView.row.id },
      undefined,
      'hide',
    )
    const legTitle = `Leg #${leg.details.number - 1} to #${leg.details.number}`
    return {
      id: `${idPrefix}-leg-${leg.rowView.row.id}-${index}`,
      title: `${blockLabel} — ${legTitle}`,
      subtitle: `${leg.details.prevSystem || 'Origin'} to ${leg.rowView.row.systemName}`,
      filenameSlug: `${String(index + 1).padStart(2, '0')}-${blockSlug}-wp${leg.details.number}`,
      subfolder: 'by-leg',
      config: concreteConfig,
    }
  })
}

function expandAllRegionsItems(
  exportData: ExpeditionExportData,
  config: Extract<ExportBlockConfig, { kind: 'summary' | 'details' }>,
  idPrefix: string,
  blockLabel: string,
  blockSlug: string,
): CartBlockItem[] {
  const items: CartBlockItem[] = []
  for (const region of getVisitedRegions(exportData)) {
    const splitModes =
      config.modeGrouping === 'separate' &&
      (config.kind !== 'details' || config.section !== 'carrier') &&
      region.distinctModes.length >= 2
    if (splitModes) {
      for (const modeTag of region.distinctModes) {
        items.push({
          id: `${idPrefix}-region-${slugify(region.regionName)}-mode-${modeTag.modeId}`,
          title: `${blockLabel} — ${region.regionName} (${modeTag.modeName})`,
          subtitle: `${region.legs.filter((leg) => leg.rowView.legModeId === modeTag.modeId).length} legs`,
          filenameSlug: `${blockSlug}-${slugify(region.regionName)}-${slugify(modeTag.modeName)}`,
          subfolder: 'by-region',
          config: cloneConcreteTelemetryConfig(
            config,
            { kind: 'region', regionName: region.regionName },
            modeTag.modeId,
            'hide',
          ),
        })
      }
      continue
    }
    items.push({
      id: `${idPrefix}-region-${slugify(region.regionName)}`,
      title: `${blockLabel} — ${region.regionName}`,
      subtitle: `${region.legs.length} legs`,
      filenameSlug: `${blockSlug}-${slugify(region.regionName)}`,
      subfolder: 'by-region',
      config: cloneConcreteTelemetryConfig(
        config,
        { kind: 'region', regionName: region.regionName },
        undefined,
        'hide',
      ),
    })
  }
  return items
}

function expandSingleScopeItems(
  exportData: ExpeditionExportData,
  config: Extract<ExportBlockConfig, { kind: 'summary' | 'details' }>,
  idPrefix: string,
  blockLabel: string,
  blockSlug: string,
): CartBlockItem[] {
  const availableModes = getDistinctModesForScope(exportData, config.scope)
  const shouldSplitByMode =
    config.modeGrouping === 'separate' &&
    (config.kind !== 'details' || config.section !== 'carrier') &&
    availableModes.length >= 2
  const scopeText = describeScopeShort(exportData, config.scope)

  if (shouldSplitByMode) {
    return availableModes.map((modeTag) => ({
      id: `${idPrefix}-mode-${modeTag.modeId}`,
      title: `${blockLabel} — ${scopeText} (${modeTag.modeName})`,
      subtitle: modeTag.modeName,
      filenameSlug: `${blockSlug}-${slugify(scopeText)}-${slugify(modeTag.modeName)}`,
      subfolder: 'by-mode',
      config: cloneConcreteTelemetryConfig(config, { ...config.scope }, modeTag.modeId, 'hide'),
    }))
  }

  const baseCareerDiff = config.kind === 'summary' ? config.careerDiff : undefined
  return [
    {
      id: `${idPrefix}-single`,
      title: `${blockLabel} — ${scopeText}`,
      subtitle: config.scope.kind === 'full' ? 'Combined' : scopeText,
      filenameSlug: `${blockSlug}-${slugify(scopeText)}`,
      subfolder:
        config.scope.kind === 'leg'
          ? 'by-leg'
          : config.scope.kind === 'region'
            ? 'by-region'
            : 'expedition',
      config: cloneConcreteTelemetryConfig(
        config,
        { ...config.scope },
        undefined,
        config.scope.kind === 'full' ? baseCareerDiff : 'hide',
      ),
    },
  ]
}

function cloneConcreteTelemetryConfig(
  config: Extract<ExportBlockConfig, { kind: 'summary' | 'details' }>,
  concreteScope: ExportScopeSpec,
  selectedModeId: number | undefined,
  careerDiffOverride: 'hide' | 'absolute' | 'percentage' | 'both' | undefined,
): Extract<ExportBlockConfig, { kind: 'summary' | 'details' }> {
  if (config.kind === 'summary') {
    return {
      kind: 'summary',
      scope: concreteScope,
      modeGrouping: selectedModeId ? 'separate' : 'combined',
      selectedModeId,
      careerDiff: careerDiffOverride ?? config.careerDiff,
    }
  }
  return {
    kind: 'details',
    section: config.section,
    scope: concreteScope,
    modeGrouping: selectedModeId ? 'separate' : 'combined',
    selectedModeId,
    selectedMetrics: [...config.selectedMetrics],
  }
}

function describeScopeShort(exportData: ExpeditionExportData, scope: ExportScopeSpec): string {
  switch (scope.kind) {
    case 'full':
      return 'Full Expedition'
    case 'baseline':
      return 'Pre-Expedition Baseline'
    case 'leg': {
      const leg = getVisitedLegs(exportData).find((item) => item.rowView.row.id === scope.rowId)
      return leg ? `Leg #${leg.details.number - 1} to #${leg.details.number}` : 'Selected Leg'
    }
    case 'region':
      return scope.regionName || 'Selected Region'
    case 'allLegs':
      return 'All Legs'
    case 'allRegions':
      return 'All Galactic Regions'
  }
}

function resolveFullExpeditionContext(
  exportData: ExpeditionExportData,
  selectedModeId: number | undefined,
  nowMs: number,
): ResolvedTelemetryContext {
  const modeBucket =
    selectedModeId != null
      ? exportData.modeBuckets.find((bucket) => bucket.modeId === selectedModeId)
      : undefined
  const isCombined = modeBucket == null
  const metrics = modeBucket ? modeBucket.metrics : exportData.expedition.metrics
  const realizationBreakdown = modeBucket
    ? modeBucket.realization
    : exportData.expedition.realization
  const carrierCost = isCombined ? exportData.carrierCost : null
  const visitedLegs = getVisitedLegs(exportData)
  const matchingLegs = modeBucket
    ? visitedLegs.filter((leg) => leg.rowView.legModeId === modeBucket.modeId)
    : visitedLegs
  const elapsedDays = computeElapsedDays(
    exportData.effectiveStart,
    exportData.effectiveEnd,
    nowMs,
  )
  const derived = computeDerivedMetrics(metrics, realizationBreakdown, carrierCost, elapsedDays)
  const careerDiff = isCombined ? buildCareerDiff(exportData) : null

  return {
    scopeLabel: modeBucket ? `Full Expedition — ${modeBucket.modeName}` : 'Full Expedition',
    routeTitle: exportData.plan.expedition.name,
    modeBadge: modeBucket
      ? { modeId: modeBucket.modeId, modeName: modeBucket.modeName, modeColor: modeBucket.modeColor }
      : null,
    fromGalactic: exportData.effectiveStartGalactic || 'Unknown',
    toGalactic: exportData.endIsNow ? 'ACTIVE' : exportData.effectiveEndGalactic || 'Unknown',
    inSystem: false,
    durationLabel: exportData.duration || formatElapsedDuration(exportData.effectiveStart, exportData.effectiveEnd, nowMs),
    elapsedDays,
    metrics,
    realization: realizationBreakdown,
    carrierCost,
    shipsFlown: collectUniqueShips(matchingLegs),
    distinctRegionCount: isCombined ? getVisitedRegions(exportData).length : null,
    visitedWaypoints: exportData.plan.visitedWaypointCount,
    plannedWaypoints: exportData.plan.waypointCount,
    coveredLy: exportData.plan.coveredLy,
    plannedLy: exportData.plan.plannedLy,
    derived,
    careerDiff,
    isFullExpeditionCombined: isCombined,
  }
}

function resolveBaselineContext(
  exportData: ExpeditionExportData,
  nowMs: number,
): ResolvedTelemetryContext {
  const baseline = exportData.originBaseline
  const metrics = baseline?.metrics ?? createEmptyMetrics()
  const realizationBreakdown = baseline?.realization ?? createEmptyRealization()
  const elapsedDays = computeElapsedDays(baseline?.from, baseline?.to, nowMs)
  return {
    scopeLabel: 'Pre-Expedition Baseline',
    routeTitle: exportData.plan.expedition.name,
    modeBadge: null,
    fromGalactic: baseline?.fromGalactic || 'Earliest Archive',
    toGalactic: baseline?.toGalactic || exportData.effectiveStartGalactic || 'Unknown',
    inSystem: Boolean(baseline?.inSystem),
    durationLabel: baseline?.elapsed || formatElapsedDuration(baseline?.from, baseline?.to, nowMs),
    elapsedDays,
    metrics,
    realization: realizationBreakdown,
    carrierCost: null,
    shipsFlown: [],
    distinctRegionCount: null,
    visitedWaypoints: null,
    plannedWaypoints: null,
    coveredLy: null,
    plannedLy: null,
    derived: computeDerivedMetrics(metrics, realizationBreakdown, null, elapsedDays),
    careerDiff: null,
    isFullExpeditionCombined: false,
  }
}

function resolveLegContext(
  exportData: ExpeditionExportData,
  rowId: number | undefined,
  nowMs: number,
): ResolvedTelemetryContext {
  const visitedLegs = getVisitedLegs(exportData)
  const leg = visitedLegs.find((item) => item.rowView.row.id === rowId) ?? visitedLegs[0]
  if (!leg) {
    const emptyMetrics = createEmptyMetrics()
    const emptyRealization = createEmptyRealization()
    return {
      scopeLabel: 'Selected Leg',
      routeTitle: exportData.plan.expedition.name,
      modeBadge: null,
      fromGalactic: '',
      toGalactic: '',
      inSystem: false,
      durationLabel: '',
      elapsedDays: null,
      metrics: emptyMetrics,
      realization: emptyRealization,
      carrierCost: null,
      shipsFlown: [],
      distinctRegionCount: null,
      visitedWaypoints: null,
      plannedWaypoints: null,
      coveredLy: null,
      plannedLy: null,
      derived: computeDerivedMetrics(emptyMetrics, emptyRealization, null, null),
      careerDiff: null,
      isFullExpeditionCombined: false,
    }
  }
  const elapsedDays = computeElapsedDays(leg.details.from, leg.details.to, nowMs)
  return {
    scopeLabel: `Leg #${leg.details.number - 1} to #${leg.details.number}`,
    routeTitle: `${leg.details.prevSystem || 'Origin'} -> ${leg.rowView.row.systemName}`,
    modeBadge: leg.rowView.legModeName
      ? {
          modeId: leg.rowView.legModeId,
          modeName: leg.rowView.legModeName,
          modeColor: leg.rowView.legModeColor || '#FF7100',
        }
      : null,
    fromGalactic: leg.details.fromGalactic || '',
    toGalactic: leg.details.toGalactic || (leg.details.inSystem ? 'IN SYSTEM' : 'IN PROGRESS'),
    inSystem: Boolean(leg.details.inSystem),
    durationLabel: leg.details.elapsed || formatElapsedDuration(leg.details.from, leg.details.to, nowMs),
    elapsedDays,
    metrics: leg.details.metrics,
    realization: leg.details.realization,
    carrierCost: null,
    shipsFlown: collectUniqueShips([leg]),
    distinctRegionCount: null,
    visitedWaypoints: null,
    plannedWaypoints: null,
    coveredLy: leg.rowView.distFromPrevLy ?? null,
    plannedLy: null,
    derived: computeDerivedMetrics(leg.details.metrics, leg.details.realization, null, elapsedDays),
    careerDiff: null,
    isFullExpeditionCombined: false,
  }
}

function resolveRegionContext(
  exportData: ExpeditionExportData,
  regionName: string | undefined,
  selectedModeId: number | undefined,
  nowMs: number,
): ResolvedTelemetryContext {
  const regions = getVisitedRegions(exportData)
  const region = regions.find((item) => item.regionName === regionName) ?? regions[0]
  const matchingLegs = region
    ? selectedModeId != null
      ? region.legs.filter((leg) => leg.rowView.legModeId === selectedModeId)
      : region.legs
    : []

  let metrics = createEmptyMetrics()
  let realizationBreakdown = createEmptyRealization()
  let totalElapsedMs = 0
  let firstFromGalactic = ''
  let lastToGalactic = ''
  let anyInSystem = false

  for (const leg of matchingLegs) {
    metrics = addMetrics(metrics, leg.details.metrics)
    realizationBreakdown = addRealization(realizationBreakdown, leg.details.realization)
    if (!firstFromGalactic && leg.details.fromGalactic) {
      firstFromGalactic = leg.details.fromGalactic
    }
    if (leg.details.toGalactic) {
      lastToGalactic = leg.details.toGalactic
    }
    if (leg.details.inSystem) {
      anyInSystem = true
      lastToGalactic = 'IN SYSTEM'
    }
    const legDays = computeElapsedDays(leg.details.from, leg.details.to, nowMs)
    if (legDays != null) {
      totalElapsedMs += legDays * MILLISECONDS_PER_DAY
    }
  }

  const elapsedDays = totalElapsedMs > 0 ? totalElapsedMs / MILLISECONDS_PER_DAY : null
  const modeTag =
    selectedModeId != null
      ? region?.distinctModes.find((mode) => mode.modeId === selectedModeId) ?? null
      : null
  const resolvedRegionName = region?.regionName ?? 'Selected Region'

  return {
    scopeLabel: modeTag ? `Region: ${resolvedRegionName} (${modeTag.modeName})` : `Region: ${resolvedRegionName}`,
    routeTitle: exportData.plan.expedition.name,
    modeBadge: modeTag,
    fromGalactic: firstFromGalactic,
    toGalactic: lastToGalactic,
    inSystem: anyInSystem,
    durationLabel:
      totalElapsedMs > 0
        ? formatElapsedDuration(new Date(0).toISOString(), new Date(totalElapsedMs).toISOString())
        : '',
    elapsedDays,
    metrics,
    realization: realizationBreakdown,
    carrierCost: null,
    shipsFlown: collectUniqueShips(matchingLegs),
    distinctRegionCount: null,
    visitedWaypoints: matchingLegs.length,
    plannedWaypoints: null,
    coveredLy: matchingLegs.reduce((sum, leg) => sum + (leg.rowView.distFromPrevLy ?? 0), 0),
    plannedLy: null,
    derived: computeDerivedMetrics(metrics, realizationBreakdown, null, elapsedDays),
    careerDiff: null,
    isFullExpeditionCombined: false,
  }
}

function buildCareerDiff(exportData: ExpeditionExportData): CareerDiffItem[] | null {
  const baseline = exportData.originBaseline
  if (!baseline) {
    return null
  }
  const baseMetrics = baseline.metrics
  const expMetrics = exportData.live
  const postMetrics = exportData.postCareer.metrics
  const baseRealized = baseline.realization.totalRealized
  const expRealized = exportData.expedition.realization.totalRealized
  const postRealized = exportData.postCareer.realization.totalRealized

  return [
    makeCareerDiffItem('distanceLy', baseMetrics.distanceLy, expMetrics.distanceLy, postMetrics.distanceLy),
    makeCareerDiffItem('jumps', baseMetrics.jumps, expMetrics.jumps, postMetrics.jumps),
    makeCareerDiffItem(
      'estimatedValue',
      baseMetrics.scanValueTotal + baseMetrics.organicValueTotal,
      expMetrics.scanValueTotal + expMetrics.organicValueTotal,
      postMetrics.scanValueTotal + postMetrics.organicValueTotal,
    ),
    makeCareerDiffItem('realizedValue', baseRealized, expRealized, postRealized),
    makeCareerDiffItem('bodiesScanned', baseMetrics.bodiesScanned, expMetrics.bodiesScanned, postMetrics.bodiesScanned),
    makeCareerDiffItem('bodiesMapped', baseMetrics.bodiesMapped, expMetrics.bodiesMapped, postMetrics.bodiesMapped),
    makeCareerDiffItem('organicSamples', baseMetrics.organicSamples, expMetrics.organicSamples, postMetrics.organicSamples),
  ]
}

function makeCareerDiffItem(
  id: CareerDiffItem['id'],
  baseline: number,
  delta: number,
  postCareer: number,
): CareerDiffItem {
  const isNewBaseline = baseline <= 0 && delta > 0
  return {
    id,
    baseline,
    delta,
    postCareer,
    growthPercent: baseline > 0 ? (delta / baseline) * 100 : null,
    careerSharePercent: postCareer > 0 ? (delta / postCareer) * 100 : null,
    isNewBaseline,
  }
}

function extractDistinctModesFromLegs(legs: VisitedLegRecord[]): ActiveModeTag[] {
  const modesById = new Map<number, ActiveModeTag>()
  for (const leg of legs) {
    if (leg.rowView.legModeId > 0 && !modesById.has(leg.rowView.legModeId)) {
      modesById.set(leg.rowView.legModeId, {
        modeId: leg.rowView.legModeId,
        modeName: leg.rowView.legModeName || 'Travel',
        modeColor: leg.rowView.legModeColor || '#FF7100',
      })
    }
  }
  return Array.from(modesById.values())
}

function collectUniqueShips(legs: VisitedLegRecord[]): string[] {
  const uniqueShips = new Set<string>()
  for (const leg of legs) {
    for (const shipName of leg.details.shipsUsed ?? []) {
      const trimmed = shipName.trim()
      if (trimmed) {
        uniqueShips.add(trimmed)
      }
    }
  }
  return Array.from(uniqueShips)
}

function computeElapsedDays(
  fromIso: string | undefined,
  toIso: string | undefined,
  nowMs: number,
): number | null {
  if (!fromIso) return null
  const startMs = Date.parse(fromIso)
  if (Number.isNaN(startMs)) return null
  const endMs = toIso ? Date.parse(toIso) : nowMs
  if (Number.isNaN(endMs) || endMs <= startMs) return null
  return (endMs - startMs) / MILLISECONDS_PER_DAY
}

function safeDivide(numerator: number, denominator: number): number | null {
  if (!Number.isFinite(denominator) || denominator <= 0) return null
  return numerator / denominator
}

function safePercent(part: number, whole: number): number | null {
  if (!Number.isFinite(whole) || whole <= 0) return null
  return (part / whole) * 100
}

function safePerThousand(value: number, distanceLy: number): number | null {
  if (!Number.isFinite(distanceLy) || distanceLy <= 0) return null
  return value / (distanceLy / 1000)
}
