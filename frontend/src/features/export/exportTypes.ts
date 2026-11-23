import { expedition, realization, stats } from '../../api/models'

/**
 * ExportTelemetrySnapshot mirrors Go exporter.TelemetrySnapshot for canonical JSON payloads.
 */
export interface ExportTelemetrySnapshot {
  metrics: stats.Metrics
  realization: realization.Breakdown
}

/**
 * ExportCarrierCost mirrors Go expedition.CarrierCost for expedition carrier cost readouts.
 */
export interface ExportCarrierCost {
  carrierType: string
  weeklyUpkeep: number
  tritiumPrice: number
  tritiumCost: number
  jumpCost: number
  upkeepCost: number
  totalCost: number
  totalEstimated: boolean
}

/**
 * ExportModeBucket holds expedition telemetry aggregated for a single flight mode.
 */
export interface ExportModeBucket {
  modeId: number
  modeName: string
  modeColor: string
  metrics: stats.Metrics
  realization: realization.Breakdown
}

/**
 * ExpeditionExportData mirrors the canonical Go exporter.ExpeditionExport payload.
 */
export interface ExpeditionExportData {
  schemaVersion: number
  exportedAt: string
  exportedAtGalactic: string
  appVersion: string
  commanders: string[]
  departed: boolean
  effectiveStart: string
  effectiveStartGalactic: string
  effectiveEnd: string
  effectiveEndGalactic: string
  endIsNow: boolean
  duration: string
  careerStart: string
  careerStartGalactic: string
  careerDuration: string
  plan: expedition.PlanView
  expedition: ExportTelemetrySnapshot
  live: stats.Metrics
  adjustments: stats.Metrics
  carrierCost: ExportCarrierCost
  originBaseline?: expedition.WaypointDetails | null
  postCareer: ExportTelemetrySnapshot
  modeBuckets: ExportModeBucket[]
  rowDetails: expedition.WaypointDetails[]
}

/**
 * ExportBundleFile matches Go exporter.BundleFile for single-file and zip exports.
 */
export interface ExportBundleFile {
  name: string
  contentBase64?: string
  contentText?: string
}

/** Top-level export block categories available in Export Studio. */
export type ExportBlockKind = 'summary' | 'details' | 'waypoints' | 'highlights'

/** Pillar sections selectable inside a Details block. */
export type DetailsSection =
  | 'flight'
  | 'carrier'
  | 'explorationValue'
  | 'cartographics'
  | 'exobiology'
  | 'realized'

/** Time and spatial scopes selectable for Summary and Details blocks. */
export type ExportScopeKind =
  | 'full'
  | 'baseline'
  | 'leg'
  | 'allLegs'
  | 'region'
  | 'allRegions'

/** Flight mode grouping mode shown when a scope spans multiple active flight modes. */
export type ModeGrouping = 'combined' | 'separate'

/** Inline Career Diff display mode supported on Full Expedition Combined Summary cards. */
export type CareerDiffDisplay = 'hide' | 'absolute' | 'percentage' | 'both'

/** Leaderboard depth selectable on Highlights cards. */
export type HighlightDepth = 1 | 3 | 5

/** Row inclusion scope for the Waypoints table block. */
export type WaypointsScope = 'visited' | 'fullPlan'

/** Concrete scope specification attached to a Summary or Details block. */
export interface ExportScopeSpec {
  kind: ExportScopeKind
  rowId?: number
  regionName?: string
}

/** Toggleable metric identifiers across all six Details sections. */
export type DetailsMetricId =
  | 'distanceBreakdown'
  | 'jumps'
  | 'shipsFlown'
  | 'distinctRegions'
  | 'fsdBoosts'
  | 'avgJumpRange'
  | 'boostRate'
  | 'avgDistancePerDay'
  | 'carrierJumps'
  | 'carrierDistance'
  | 'avgCarrierJumpRange'
  | 'carrierTritium'
  | 'avgTritiumPerJump'
  | 'carrierCosts'
  | 'combinedEstimatedValue'
  | 'cartoValueCard'
  | 'exoValueCard'
  | 'discoveryCountsSummary'
  | 'realizedPendingSnapshot'
  | 'avgCombinedCreditsPerJump'
  | 'avgCombinedCreditsPer1000Ly'
  | 'avgCombinedCreditsPerDay'
  | 'cartoTotalValue'
  | 'cartoValueBreakdown'
  | 'bodiesScanned'
  | 'bodiesMapped'
  | 'dssEfficiencyRate'
  | 'firstDiscoveryRate'
  | 'avgCartoCreditsPerJump'
  | 'avgCartoCreditsPer1000Ly'
  | 'exoTotalValue'
  | 'exoValueBreakdown'
  | 'organicSamples'
  | 'firstLoggedRate'
  | 'avgCreditsPerSample'
  | 'avgExoCreditsPerJump'
  | 'avgExoCreditsPer1000Ly'
  | 'realizedTotal'
  | 'pendingValue'
  | 'estimatedValueTotal'
  | 'lossBreakdown'
  | 'realizationRetentionRate'
  | 'netProfitAfterCarrierCost'
  | 'avgRealizedCreditsPerJump'
  | 'avgRealizedCreditsPer1000Ly'

/** Optional column identifiers for the Waypoints route table block. */
export type WaypointsColumnId =
  | 'regionHeaders'
  | 'timestamps'
  | 'legDistance'
  | 'duration'
  | 'shipsFlown'
  | 'flightMode'
  | 'fcStops'
  | 'gecBadges'
  | 'notes'

/** Opt-in trophy card identifiers for the Highlights block. */
export type HighlightCardId =
  | 'mostLucrative'
  | 'longestDistance'
  | 'mostJumps'
  | 'longestDuration'
  | 'shortestDistance'
  | 'mostBodiesScanned'
  | 'mostOrganicSamples'
  | 'furthestFromSol'
  | 'costliestMistake'

/** Visual layout zone for a Details metric entry. */
export type DetailsMetricZone = 'kpi' | 'card'

/** Metadata describing one selectable checkbox in a Details section. */
export interface DetailsMetricDescriptor {
  id: DetailsMetricId
  label: string
  zone: DetailsMetricZone
  defaultChecked: boolean
  fullExpeditionCombinedOnly?: boolean
  requiresElapsed24h?: boolean
  requiresVisitedRoute?: boolean
}

/** Configuration for a Summary block. */
export interface SummaryBlockConfig {
  kind: 'summary'
  scope: ExportScopeSpec
  modeGrouping: ModeGrouping
  selectedModeId?: number
  careerDiff: CareerDiffDisplay
}

/** Configuration for a Details block. */
export interface DetailsBlockConfig {
  kind: 'details'
  section: DetailsSection
  scope: ExportScopeSpec
  modeGrouping: ModeGrouping
  selectedModeId?: number
  selectedMetrics: DetailsMetricId[]
}

/** Configuration for a Waypoints table block. */
export interface WaypointsBlockConfig {
  kind: 'waypoints'
  waypointsScope: WaypointsScope
  selectedColumns: WaypointsColumnId[]
}

/** Configuration for a Highlights trophy grid block. */
export interface HighlightsBlockConfig {
  kind: 'highlights'
  depth: HighlightDepth
  selectedCards: HighlightCardId[]
  lucrativeMetric: 'estimated' | 'realized'
  distanceMetric: 'total' | 'ship'
}

/** Discriminated union of all four Export Studio block configurations. */
export type ExportBlockConfig =
  | SummaryBlockConfig
  | DetailsBlockConfig
  | WaypointsBlockConfig
  | HighlightsBlockConfig

/** Concrete, single-card block queued inside the Export Cart. */
export interface CartBlockItem {
  id: string
  title: string
  subtitle: string
  filenameSlug: string
  subfolder: string
  config: ExportBlockConfig
}

/** Global output format toggles displayed in the Export Studio footer. */
export interface ExportOutputOptions {
  includePng: boolean
  includeHtml: boolean
  includeJson: boolean
}

/** Human-readable titles and accent identifiers for all six Details sections. */
export const DETAILS_SECTIONS: ReadonlyArray<{
  id: DetailsSection
  label: string
  accent: 'amber' | 'cyan' | 'emerald'
}> = [
  { id: 'flight', label: 'Flight Operations', accent: 'amber' },
  { id: 'carrier', label: 'Carrier Operations', accent: 'amber' },
  { id: 'explorationValue', label: 'Exploration Value', accent: 'cyan' },
  { id: 'cartographics', label: 'Cartographics', accent: 'cyan' },
  { id: 'exobiology', label: 'Exobiology', accent: 'emerald' },
  { id: 'realized', label: 'Realized Value', accent: 'amber' },
]

/** Per-section catalog of selectable Details checkboxes. */
export const DETAILS_SECTION_METRICS: Record<DetailsSection, ReadonlyArray<DetailsMetricDescriptor>> = {
  flight: [
    { id: 'distanceBreakdown', label: 'Distance Travelled (Ship / Carrier)', zone: 'card', defaultChecked: true },
    { id: 'jumps', label: 'Hyperspace Jumps', zone: 'kpi', defaultChecked: true },
    { id: 'shipsFlown', label: 'Ships Flown', zone: 'kpi', defaultChecked: true, requiresVisitedRoute: true },
    { id: 'distinctRegions', label: 'Distinct Galactic Regions', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'fsdBoosts', label: 'FSD Boosts (Jet / Pr / St / Ba)', zone: 'card', defaultChecked: true },
    { id: 'avgJumpRange', label: 'Avg Jump Range (Ly / Jump)', zone: 'kpi', defaultChecked: true },
    { id: 'boostRate', label: 'Boost Rate (%)', zone: 'kpi', defaultChecked: true },
    { id: 'avgDistancePerDay', label: 'Avg Distance per Day', zone: 'kpi', defaultChecked: true, requiresElapsed24h: true },
  ],
  carrier: [
    { id: 'carrierJumps', label: 'Expedition Carrier Jumps', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'carrierDistance', label: 'Expedition Carrier Distance (Ly)', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'avgCarrierJumpRange', label: 'Avg Carrier Jump Range', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'carrierTritium', label: 'Expedition Carrier Tritium (t)', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'avgTritiumPerJump', label: 'Avg Tritium per Carrier Jump', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'carrierCosts', label: 'Carrier Cost Breakdown', zone: 'card', defaultChecked: true, fullExpeditionCombinedOnly: true },
  ],
  explorationValue: [
    { id: 'combinedEstimatedValue', label: 'Combined Estimated Value', zone: 'card', defaultChecked: true },
    { id: 'cartoValueCard', label: 'Cartographics Value Card', zone: 'card', defaultChecked: true },
    { id: 'exoValueCard', label: 'Exobiology Value Card', zone: 'card', defaultChecked: true },
    { id: 'discoveryCountsSummary', label: 'Discovery Counts Summary', zone: 'kpi', defaultChecked: true },
    { id: 'realizedPendingSnapshot', label: 'Realized and Pending Snapshot', zone: 'kpi', defaultChecked: true },
    { id: 'avgCombinedCreditsPerJump', label: 'Avg Credits per Jump', zone: 'kpi', defaultChecked: true },
    { id: 'avgCombinedCreditsPer1000Ly', label: 'Avg Credits per 1,000 Ly', zone: 'kpi', defaultChecked: true },
    { id: 'avgCombinedCreditsPerDay', label: 'Avg Credits per Day', zone: 'kpi', defaultChecked: true, requiresElapsed24h: true },
  ],
  cartographics: [
    { id: 'cartoTotalValue', label: 'Cartographics Total Value', zone: 'kpi', defaultChecked: true },
    { id: 'cartoValueBreakdown', label: '3-Way Value Breakdown', zone: 'card', defaultChecked: true },
    { id: 'bodiesScanned', label: 'Bodies Scanned and Firsts', zone: 'kpi', defaultChecked: true },
    { id: 'bodiesMapped', label: 'Bodies Mapped and Firsts', zone: 'kpi', defaultChecked: true },
    { id: 'dssEfficiencyRate', label: 'DSS Probe Efficiency Rate (%)', zone: 'kpi', defaultChecked: true },
    { id: 'firstDiscoveryRate', label: 'First Discovery Rate (%)', zone: 'kpi', defaultChecked: true },
    { id: 'avgCartoCreditsPerJump', label: 'Avg Carto Credits per Jump', zone: 'kpi', defaultChecked: true },
    { id: 'avgCartoCreditsPer1000Ly', label: 'Avg Carto Credits per 1,000 Ly', zone: 'kpi', defaultChecked: true },
  ],
  exobiology: [
    { id: 'exoTotalValue', label: 'Exobiology Total Value', zone: 'kpi', defaultChecked: true },
    { id: 'exoValueBreakdown', label: '2-Way Value Breakdown', zone: 'card', defaultChecked: true },
    { id: 'organicSamples', label: 'Organic Samples and First Logged', zone: 'kpi', defaultChecked: true },
    { id: 'firstLoggedRate', label: 'First Logged Rate (%)', zone: 'kpi', defaultChecked: true },
    { id: 'avgCreditsPerSample', label: 'Avg Credits per Sample', zone: 'kpi', defaultChecked: true },
    { id: 'avgExoCreditsPerJump', label: 'Avg Exo Credits per Jump', zone: 'kpi', defaultChecked: true },
    { id: 'avgExoCreditsPer1000Ly', label: 'Avg Exo Credits per 1,000 Ly', zone: 'kpi', defaultChecked: true },
  ],
  realized: [
    { id: 'realizedTotal', label: 'Total Realized Net Value', zone: 'kpi', defaultChecked: true },
    { id: 'pendingValue', label: 'Pending Value (In Transit)', zone: 'kpi', defaultChecked: true },
    { id: 'estimatedValueTotal', label: 'Combined Estimated Value', zone: 'kpi', defaultChecked: true },
    { id: 'lossBreakdown', label: 'Loss and Deduction Breakdown', zone: 'card', defaultChecked: true },
    { id: 'realizationRetentionRate', label: 'Realization Retention Rate (%)', zone: 'kpi', defaultChecked: true },
    { id: 'netProfitAfterCarrierCost', label: 'Net Profit After Carrier Costs', zone: 'kpi', defaultChecked: true, fullExpeditionCombinedOnly: true },
    { id: 'avgRealizedCreditsPerJump', label: 'Avg Realized Credits per Jump', zone: 'kpi', defaultChecked: true },
    { id: 'avgRealizedCreditsPer1000Ly', label: 'Avg Realized Credits per 1,000 Ly', zone: 'kpi', defaultChecked: true },
  ],
}

/** Selectable Waypoints table columns and their default checked state. */
export const WAYPOINTS_COLUMNS: ReadonlyArray<{
  id: WaypointsColumnId
  label: string
  defaultChecked: boolean
}> = [
  { id: 'regionHeaders', label: 'Region Headers', defaultChecked: true },
  { id: 'timestamps', label: 'Galactic Timestamps', defaultChecked: true },
  { id: 'legDistance', label: 'Leg Distance (Ly)', defaultChecked: true },
  { id: 'duration', label: 'Leg Duration', defaultChecked: true },
  { id: 'shipsFlown', label: 'Ships Flown', defaultChecked: true },
  { id: 'flightMode', label: 'Flight Mode', defaultChecked: true },
  { id: 'fcStops', label: 'Fleet Carrier Stops', defaultChecked: true },
  { id: 'gecBadges', label: 'GEC POI Badges', defaultChecked: true },
  { id: 'notes', label: 'Waypoint Notes', defaultChecked: false },
]

/** Selectable Highlight trophy cards and their default checked state. */
export const HIGHLIGHT_CARDS: ReadonlyArray<{
  id: HighlightCardId
  label: string
  accent: 'amber' | 'cyan' | 'emerald' | 'danger'
  defaultChecked: boolean
}> = [
  { id: 'mostLucrative', label: 'Most Lucrative Leg', accent: 'amber', defaultChecked: true },
  { id: 'longestDistance', label: 'Longest Leg (Distance)', accent: 'amber', defaultChecked: true },
  { id: 'mostJumps', label: 'Most Jumps on a Leg', accent: 'amber', defaultChecked: true },
  { id: 'longestDuration', label: 'Longest Leg (Duration)', accent: 'cyan', defaultChecked: true },
  { id: 'shortestDistance', label: 'Shortest Leg (Distance)', accent: 'amber', defaultChecked: false },
  { id: 'mostBodiesScanned', label: 'Most Prolific Survey', accent: 'cyan', defaultChecked: true },
  { id: 'mostOrganicSamples', label: 'Most Prolific Exobiology', accent: 'emerald', defaultChecked: true },
  { id: 'furthestFromSol', label: 'Furthest From Sol', accent: 'cyan', defaultChecked: true },
  { id: 'costliestMistake', label: 'Costliest Mistake', accent: 'danger', defaultChecked: false },
]

/** Returns the default checked metric IDs for a given Details section. */
export function defaultMetricsForSection(section: DetailsSection): DetailsMetricId[] {
  return DETAILS_SECTION_METRICS[section]
    .filter((metric) => metric.defaultChecked)
    .map((metric) => metric.id)
}

/** Creates a default configuration object for the requested block kind. */
export function createDefaultBlockConfig(kind: ExportBlockKind): ExportBlockConfig {
  switch (kind) {
    case 'summary':
      return {
        kind: 'summary',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        careerDiff: 'percentage',
      }
    case 'details':
      return {
        kind: 'details',
        section: 'flight',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection('flight'),
      }
    case 'waypoints':
      return {
        kind: 'waypoints',
        waypointsScope: 'visited',
        selectedColumns: WAYPOINTS_COLUMNS.filter((column) => column.defaultChecked).map(
          (column) => column.id,
        ),
      }
    case 'highlights':
      return {
        kind: 'highlights',
        depth: 1,
        selectedCards: HIGHLIGHT_CARDS.filter((card) => card.defaultChecked).map((card) => card.id),
        lucrativeMetric: 'estimated',
        distanceMetric: 'total',
      }
  }
}

