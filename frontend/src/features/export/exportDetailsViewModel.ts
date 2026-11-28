import { formatCredits, formatInt, formatLoss, formatLy } from '../../domain/galacticTime'
import {
  buildCartographicsBreakdownRows,
  buildExobiologyBreakdownRows,
  buildRealizationLossRows,
} from '../../domain/telemetryBreakdowns'
import {
  formatEstimatedCarrierCredits,
  formatNetProfitValue,
  isDetailsMetricAvailable,
  ResolvedTelemetryContext,
  resolveTelemetryContext,
  sanitizeExportHexColor,
} from './exportMetrics'
import {
  DETAILS_SECTION_METRICS,
  DETAILS_SECTIONS,
  DetailsBlockConfig,
  DetailsMetricId,
  ExpeditionExportData,
} from './exportTypes'

/** Cockpit border and highlight accent palette shared by PNG export cards and HTML reports. */
export type ExportAccentTone = 'amber' | 'cyan' | 'emerald' | 'danger'

/** Text tone applied to formatted telemetry values inside KPI tiles and breakdown rows. */
export type ExportValueTone = 'main' | ExportAccentTone

/** Sanitized flight mode pill metadata displayed in Summary and Details headers. */
export interface TelemetryModeBadgeViewModel {
  modeName: string
  safeColor: string
}

/** Shared header view-model resolved identically for Summary and Details blocks. */
export interface TelemetryHeaderViewModel {
  eyebrowLabel: string
  scopeLabel: string
  routeTitle: string
  accent: 'amber' | 'cyan' | 'emerald'
  modeBadge: TelemetryModeBadgeViewModel | null
  inSystem: boolean
  fromGalactic: string
  toGalactic: string
  durationLabel: string
  inGameDurationLabel: string
  exportedAtGalactic: string
}

/** Scalar KPI tile view-model inside a Details section grid. */
export interface DetailsKpiTileViewModel {
  id: DetailsMetricId
  label: string
  value: string
  subValue?: string
  valueTone: ExportValueTone
}

/** Single label-value row inside a Details breakdown card. */
export interface DetailsBreakdownRowViewModel {
  label: string
  value: string
  valueTone: ExportValueTone
}

/** Full-width breakdown card view-model inside a Details section stack. */
export interface DetailsBreakdownCardViewModel {
  id: DetailsMetricId
  title: string
  headerValue: string
  borderAccent: ExportAccentTone
  headerTone: ExportAccentTone
  rows: DetailsBreakdownRowViewModel[]
}

/** Pure Details block view-model shared by DetailsBlock.tsx and htmlGenerator.ts. */
export interface DetailsViewModel {
  header: TelemetryHeaderViewModel
  isEmpty: boolean
  emptyMessage: string
  kpiCols: 1 | 2 | 3
  kpiTiles: DetailsKpiTileViewModel[]
  breakdownCards: DetailsBreakdownCardViewModel[]
}

const EMPTY_DETAILS_MESSAGE =
  'Select at least one applicable data point to preview and add this block'

/** Formats a nullable light-year value to one decimal place or an em-dash placeholder. */
export function formatOptionalLy(value: number | null): string {
  return value != null ? formatLy(value, 1) : '—'
}

/** Formats a nullable percentage to one decimal place or an em-dash placeholder. */
export function formatOptionalPercent(value: number | null): string {
  return value != null ? `${value.toFixed(1)}%` : '—'
}

/** Formats a nullable credit value or an em-dash placeholder. */
export function formatOptionalCredits(value: number | null): string {
  return value != null ? formatCredits(value) : '—'
}

/** Formats the combined elapsed and in-game duration metadata line for telemetry headers. */
export function formatTelemetryHeaderDurationLine(
  header: Pick<TelemetryHeaderViewModel, 'durationLabel' | 'inGameDurationLabel'>,
): string {
  const parts: string[] = []
  if (header.durationLabel) parts.push(`ELAPSED: ${header.durationLabel}`)
  if (header.inGameDurationLabel) parts.push(`IN-GAME: ${header.inGameDurationLabel}`)
  return parts.join(' · ')
}

/** Builds the sanitized telemetry header shared by Summary and Details view-models. */
export function buildTelemetryHeaderViewModel(
  eyebrowLabel: string,
  accent: 'amber' | 'cyan' | 'emerald',
  context: ResolvedTelemetryContext,
  exportedAtGalactic: string,
): TelemetryHeaderViewModel {
  return {
    eyebrowLabel,
    scopeLabel: context.scopeLabel,
    routeTitle: context.routeTitle,
    accent,
    modeBadge: context.modeBadge
      ? {
          modeName: context.modeBadge.modeName,
          safeColor: sanitizeExportHexColor(context.modeBadge.modeColor),
        }
      : null,
    inSystem: context.inSystem,
    fromGalactic: context.fromGalactic,
    toGalactic: context.toGalactic,
    durationLabel: context.durationLabel,
    inGameDurationLabel: context.inGameDurationLabel,
    exportedAtGalactic,
  }
}

/** Resolves the pure Details block view-model for both React PNG cards and HTML reports. */
export function buildDetailsViewModel(
  exportData: ExpeditionExportData,
  config: DetailsBlockConfig,
  nowMs?: number,
): DetailsViewModel {
  const context = resolveTelemetryContext(exportData, config.scope, config.selectedModeId, nowMs)
  const sectionMeta = DETAILS_SECTIONS.find((section) => section.id === config.section)!
  const selectedSet = new Set<DetailsMetricId>(config.selectedMetrics)
  const activeDescriptors = DETAILS_SECTION_METRICS[config.section].filter(
    (descriptor) =>
      selectedSet.has(descriptor.id) &&
      isDetailsMetricAvailable(descriptor, context, config.scope.kind),
  )

  const kpiTiles: DetailsKpiTileViewModel[] = []
  const breakdownCards: DetailsBreakdownCardViewModel[] = []
  for (const descriptor of activeDescriptors) {
    if (descriptor.zone === 'kpi') {
      const kpiBuilder = DETAILS_KPI_BUILDERS[descriptor.id]
      if (kpiBuilder) kpiTiles.push({ id: descriptor.id, ...kpiBuilder(context) })
    } else {
      const cardBuilder = DETAILS_BREAKDOWN_CARD_BUILDERS[descriptor.id]
      const builtCard = cardBuilder ? cardBuilder(context) : null
      if (builtCard) breakdownCards.push({ id: descriptor.id, ...builtCard })
    }
  }
  const kpiCount = kpiTiles.length
  const kpiCols: 1 | 2 | 3 = kpiCount <= 1 ? 1 : kpiCount === 2 || kpiCount === 4 ? 2 : 3

  return {
    header: buildTelemetryHeaderViewModel(
      sectionMeta.label.toUpperCase(),
      sectionMeta.accent,
      context,
      exportData.exportedAtGalactic,
    ),
    isEmpty: activeDescriptors.length === 0,
    emptyMessage: EMPTY_DETAILS_MESSAGE,
    kpiCols,
    kpiTiles,
    breakdownCards,
  }
}

type KpiContentBuilder = (context: ResolvedTelemetryContext) => Omit<DetailsKpiTileViewModel, 'id'>
type BreakdownCardBuilder = (
  context: ResolvedTelemetryContext,
) => Omit<DetailsBreakdownCardViewModel, 'id'> | null

function kpiTile(
  label: string,
  value: string,
  valueTone: ExportValueTone = 'main',
  subValue?: string,
): Omit<DetailsKpiTileViewModel, 'id'> {
  return { label, value, valueTone, subValue }
}

function bdRow(
  label: string,
  value: string,
  valueTone: ExportValueTone = 'main',
): DetailsBreakdownRowViewModel {
  return { label, value, valueTone }
}

function buildNetProfitKpiTile(context: ResolvedTelemetryContext): Omit<DetailsKpiTileViewModel, 'id'> {
  const { derived, carrierCost } = context
  const formattedProfit =
    derived.netProfitAfterCarrierCost != null
      ? formatNetProfitValue(derived.netProfitAfterCarrierCost, carrierCost)
      : null
  const subText = carrierCost
    ? `Carrier cost: ${formatEstimatedCarrierCredits(carrierCost.totalCost, Boolean(carrierCost.totalEstimated))}`
    : undefined
  return kpiTile(
    'Net Profit After Carrier',
    formattedProfit ? formattedProfit.text : '—',
    formattedProfit?.isNegative ? 'danger' : 'amber',
    subText,
  )
}

const DETAILS_KPI_BUILDERS: Partial<Record<DetailsMetricId, KpiContentBuilder>> = {
  jumps: (context) => kpiTile('Hyperspace Jumps', formatInt(context.metrics.jumps)),
  shipsFlown: (context) =>
    kpiTile('Ships Flown', String(context.shipsFlown.length), 'main', context.shipsFlown.join(', ') || 'None'),
  distinctRegions: (context) =>
    kpiTile('Distinct Galactic Regions', formatInt(context.distinctRegionCount ?? 0)),
  avgJumpRange: (context) => kpiTile('Avg Jump Range', formatOptionalLy(context.derived.avgJumpRangeLy)),
  boostRate: (context) => kpiTile('Boost Rate', formatOptionalPercent(context.derived.boostRatePercent)),
  avgDistancePerDay: (context) =>
    kpiTile('Avg Distance per Day', formatOptionalLy(context.derived.avgDistancePerDayLy)),
  carrierJumps: (context) => kpiTile('Carrier Jumps', formatInt(context.metrics.carrierJumps)),
  carrierDistance: (context) => kpiTile('Carrier Distance', formatLy(context.metrics.carrierDistanceLy, 1)),
  avgCarrierJumpRange: (context) =>
    kpiTile('Avg Carrier Jump Range', formatOptionalLy(context.derived.avgCarrierJumpRangeLy)),
  carrierTritium: (context) =>
    kpiTile('Carrier Tritium Used', `${formatInt(context.metrics.tritiumUsed ?? 0)} t`),
  avgTritiumPerJump: (context) =>
    kpiTile(
      'Avg Tritium / Jump',
      context.derived.avgTritiumPerJump != null ? `${context.derived.avgTritiumPerJump.toFixed(1)} t` : '—',
    ),
  discoveryCountsSummary: (context) =>
    kpiTile(
      'Discovery Counts',
      `${formatInt(context.metrics.bodiesScanned)} bodies · ${formatInt(context.metrics.organicSamples)} samples`,
      'cyan',
      `${formatInt(context.metrics.firstDiscoveries)} first disc · ${formatInt(context.metrics.firstLogged)} first logged`,
    ),
  realizedPendingSnapshot: (context) =>
    kpiTile(
      'Realized / Pending',
      formatCredits(context.realization.totalRealized),
      'amber',
      `Pending: ${formatCredits(context.realization.pendingValue)}`,
    ),
  avgCombinedCreditsPerJump: (context) =>
    kpiTile('Avg Credits / Jump', formatOptionalCredits(context.derived.avgCombinedCreditsPerJump), 'cyan'),
  avgCombinedCreditsPer1000Ly: (context) =>
    kpiTile('Avg Credits / 1,000 Ly', formatOptionalCredits(context.derived.avgCombinedCreditsPer1000Ly), 'cyan'),
  avgCombinedCreditsPerDay: (context) =>
    kpiTile('Avg Credits / Day', formatOptionalCredits(context.derived.avgCombinedCreditsPerDay), 'cyan'),
  cartoTotalValue: (context) =>
    kpiTile('Cartographics Value', formatCredits(context.metrics.scanValueTotal), 'cyan'),
  bodiesScanned: (context) =>
    kpiTile('Bodies Scanned', formatInt(context.metrics.bodiesScanned), 'cyan', `${formatInt(context.metrics.firstDiscoveries)} first discovered`),
  bodiesMapped: (context) =>
    kpiTile('Bodies Mapped', formatInt(context.metrics.bodiesMapped), 'cyan', `${formatInt(context.metrics.firstMapped)} first mapped`),
  dssEfficiencyRate: (context) =>
    kpiTile('DSS Probe Efficiency', formatOptionalPercent(context.derived.dssEfficiencyPercent), 'cyan', `${formatInt(context.metrics.efficientMappings)} efficient`),
  firstDiscoveryRate: (context) =>
    kpiTile('First Discovery Rate', formatOptionalPercent(context.derived.firstDiscoveryRatePercent), 'cyan'),
  avgCartoCreditsPerJump: (context) =>
    kpiTile('Avg Carto Cr / Jump', formatOptionalCredits(context.derived.avgCartoCreditsPerJump), 'cyan'),
  avgCartoCreditsPer1000Ly: (context) =>
    kpiTile('Avg Carto Cr / 1,000 Ly', formatOptionalCredits(context.derived.avgCartoCreditsPer1000Ly), 'cyan'),
  exoTotalValue: (context) =>
    kpiTile('Exobiology Value', formatCredits(context.metrics.organicValueTotal), 'emerald'),
  organicSamples: (context) =>
    kpiTile('Organic Samples', formatInt(context.metrics.organicSamples), 'emerald', `${formatInt(context.metrics.firstLogged)} first logged`),
  firstLoggedRate: (context) =>
    kpiTile('First Logged Rate', formatOptionalPercent(context.derived.firstLoggedRatePercent), 'emerald'),
  avgCreditsPerSample: (context) =>
    kpiTile('Avg Credits / Sample', formatOptionalCredits(context.derived.avgCreditsPerSample), 'emerald'),
  avgExoCreditsPerJump: (context) =>
    kpiTile('Avg Exo Cr / Jump', formatOptionalCredits(context.derived.avgExoCreditsPerJump), 'emerald'),
  avgExoCreditsPer1000Ly: (context) =>
    kpiTile('Avg Exo Cr / 1,000 Ly', formatOptionalCredits(context.derived.avgExoCreditsPer1000Ly), 'emerald'),
  realizedTotal: (context) =>
    kpiTile('Total Realized Net', formatCredits(context.realization.totalRealized), 'amber'),
  pendingValue: (context) =>
    kpiTile('Pending (In Transit)', formatCredits(context.realization.pendingValue)),
  estimatedValueTotal: (context) =>
    kpiTile('Combined Estimated', formatCredits(context.derived.combinedEstimatedCredits)),
  realizationRetentionRate: (context) =>
    kpiTile('Realization Retention', formatOptionalPercent(context.derived.realizationRetentionPercent), 'amber'),
  netProfitAfterCarrierCost: buildNetProfitKpiTile,
  avgRealizedCreditsPerJump: (context) =>
    kpiTile('Avg Realized Cr / Jump', formatOptionalCredits(context.derived.avgRealizedCreditsPerJump), 'amber'),
  avgRealizedCreditsPer1000Ly: (context) =>
    kpiTile('Avg Realized Cr / 1,000 Ly', formatOptionalCredits(context.derived.avgRealizedCreditsPer1000Ly), 'amber'),
}

const buildCartographicsCard: BreakdownCardBuilder = (context) => ({
  title: 'Cartographics Valuation',
  headerValue: formatCredits(context.metrics.scanValueTotal),
  borderAccent: 'cyan',
  headerTone: 'cyan',
  rows: buildCartographicsBreakdownRows(context.metrics).map((row) => bdRow(row.label, row.value, row.tone)),
})

const buildExobiologyCard: BreakdownCardBuilder = (context) => ({
  title: 'Exobiology Valuation',
  headerValue: formatCredits(context.metrics.organicValueTotal),
  borderAccent: 'emerald',
  headerTone: 'emerald',
  rows: buildExobiologyBreakdownRows(context.metrics).map((row) => bdRow(row.label, row.value, row.tone)),
})

const DETAILS_BREAKDOWN_CARD_BUILDERS: Partial<Record<DetailsMetricId, BreakdownCardBuilder>> = {
  distanceBreakdown: (context) => ({
    title: 'Distance Travelled',
    headerValue: formatLy(context.metrics.distanceLy),
    borderAccent: 'amber',
    headerTone: 'amber',
    rows: [
      bdRow('By Ship (FSD)', formatLy(context.derived.shipDistanceLy)),
      bdRow('By Carrier (Passenger)', formatLy(context.metrics.carrierPassengerLy)),
    ],
  }),
  inGamePlaytimeBreakdown: (context) => ({
    title: 'In-Game Playtime',
    headerValue: context.inGameDurationLabel || '0m',
    borderAccent: 'amber',
    headerTone: 'amber',
    rows: [
      bdRow('In Transit (Between Waypoints)', context.transitInGameDurationLabel || '0m'),
      bdRow('In System (At Waypoints)', context.inSystemInGameDurationLabel || '0m'),
    ],
  }),
  fsdBoosts: (context) => ({
    title: 'FSD Boosts',
    headerValue: `${formatInt(context.derived.totalBoosts)} total`,
    borderAccent: 'amber',
    headerTone: 'amber',
    rows: [
      bdRow('Jet Cone Boosts', formatInt(context.metrics.jetConeBoosts)),
      bdRow('Premium Injections', formatInt(context.metrics.premiumBoosts)),
      bdRow('Standard Injections', formatInt(context.metrics.standardBoosts)),
      bdRow('Basic Injections', formatInt(context.metrics.basicBoosts)),
    ],
  }),
  carrierCosts: (context) =>
    context.carrierCost
      ? {
          title: `Carrier Cost Breakdown (${context.carrierCost.carrierType})`,
          headerValue: formatEstimatedCarrierCredits(
            context.carrierCost.totalCost,
            Boolean(context.carrierCost.totalEstimated),
          ),
          borderAccent: 'amber',
          headerTone: 'amber',
          rows: [
            bdRow('Tritium Cost', formatEstimatedCarrierCredits(context.carrierCost.tritiumCost)),
            bdRow('Jump Maintenance', formatCredits(context.carrierCost.jumpCost)),
            bdRow('Prorated Weekly Upkeep', formatEstimatedCarrierCredits(context.carrierCost.upkeepCost)),
          ],
        }
      : null,
  combinedEstimatedValue: (context) => ({
    title: 'Combined Estimated Exploration Value',
    headerValue: formatCredits(context.derived.combinedEstimatedCredits),
    borderAccent: 'cyan',
    headerTone: 'cyan',
    rows: [
      bdRow('Universal Cartographics', formatCredits(context.metrics.scanValueTotal), 'cyan'),
      bdRow('Vista Genomics (Exobiology)', formatCredits(context.metrics.organicValueTotal), 'emerald'),
    ],
  }),
  cartoValueCard: buildCartographicsCard,
  cartoValueBreakdown: buildCartographicsCard,
  exoValueCard: buildExobiologyCard,
  exoValueBreakdown: buildExobiologyCard,
  lossBreakdown: (context) => ({
    title: 'Realization Losses and Deductions',
    headerValue: formatLoss(context.derived.totalLossesCredits),
    borderAccent: 'danger',
    headerTone: 'danger',
    rows: buildRealizationLossRows(context.realization).map((row) => bdRow(row.label, row.value)),
  }),
}
