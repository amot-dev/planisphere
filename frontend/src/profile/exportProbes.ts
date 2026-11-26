import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { expedition, realization, stats } from '../api/models'
import { ExportBlockRenderer } from '../features/export/blocks/ExportBlockRenderer'
import {
  createQuickShareCart,
  expandConfigToCartItems,
} from '../features/export/exportMetrics'
import {
  CartBlockItem,
  defaultMetricsForSection,
  ExpeditionExportData,
  ExportDerivedMetrics,
  ExportModeBucket,
  HIGHLIGHT_CARDS,
  WAYPOINTS_COLUMNS,
} from '../features/export/exportTypes'
import { generateSelfContainedHtmlReport } from '../features/export/htmlGenerator'
import {
  makeRenderSpan,
  msToNs,
  type HydratedFrontendFixture,
  type SpanNodeJSON,
} from './profileHelpers'

const PINNED_EXPORT_NOW_ISO = '2026-08-01T00:00:00Z'
const PINNED_EXPORT_NOW_GALACTIC = '3312-08-01 00:00 UTC'
const PINNED_EXPORT_NOW_MS = Date.parse(PINNED_EXPORT_NOW_ISO)

const EMPTY_METRICS: stats.Metrics = {
  distanceLy: 0, shipDistanceLy: 0, carrierPassengerLy: 0, jumps: 0,
  carrierJumps: 0, carrierDistanceLy: 0, tritiumUsed: undefined,
  jetConeBoosts: 0, premiumBoosts: 0, standardBoosts: 0, basicBoosts: 0,
  bodiesScanned: 0, bodiesMapped: 0, efficientMappings: 0,
  scanValueTotal: 0, scanValueBase: 0, scanValueBonus: 0, scanValueEfficiencyBonus: 0,
  firstDiscoveries: 0, firstMapped: 0, organicSamples: 0,
  organicValueTotal: 0, organicValueBase: 0, organicValueBonus: 0, firstLogged: 0,
}

const EMPTY_REALIZATION: realization.Breakdown = {
  totalRealized: 0, cartoLostDeath: 0, organicLostDeath: 0, cartoStolen: 0,
  organicStolen: 0, carrierTariffLoss: 0, npcCrewLoss: 0, pendingValue: 0,
}

const EMPTY_DERIVED: ExportDerivedMetrics = {
  shipDistanceLy: 0, totalBoosts: 0, avgJumpRangeLy: null, boostRatePercent: null,
  avgDistancePerDayLy: null, avgCarrierJumpRangeLy: null, avgTritiumPerJump: null,
  combinedEstimatedCredits: 0, dssEfficiencyPercent: null, firstDiscoveryRatePercent: null,
  firstLoggedRatePercent: null, avgCreditsPerSample: null, avgCombinedCreditsPerJump: null,
  avgCombinedCreditsPer1000Ly: null, avgCombinedCreditsPerDay: null,
  avgCartoCreditsPerJump: null, avgCartoCreditsPer1000Ly: null,
  avgExoCreditsPerJump: null, avgExoCreditsPer1000Ly: null, totalLossesCredits: 0,
  realizationRetentionPercent: null, netProfitAfterCarrierCost: null,
  avgRealizedCreditsPerJump: null, avgRealizedCreditsPer1000Ly: null,
}

function normalizeMetrics(source?: stats.Metrics): stats.Metrics {
  return source ? { ...EMPTY_METRICS, ...source } : { ...EMPTY_METRICS }
}

function normalizeRealization(source?: realization.Breakdown): realization.Breakdown {
  return source ? { ...EMPTY_REALIZATION, ...source } : { ...EMPTY_REALIZATION }
}

function synthesizeExportData(hydrated: HydratedFrontendFixture): ExpeditionExportData {
  const { plan, sidebar, modesList, visitedDetails } = hydrated
  const normalizedDetails = visitedDetails.map((detail) =>
    expedition.WaypointDetails.createFrom({
      ...detail,
      metrics: normalizeMetrics(detail.metrics),
      realization: normalizeRealization(detail.realization),
    }),
  )
  const originBaseline = normalizedDetails.find((detail) => detail.kind === 'origin')
  const rowDetails = normalizedDetails.filter((detail) => detail.kind !== 'origin')
  const activeModeBuckets: ExportModeBucket[] = modesList.map((modeItem) => ({
    modeId: modeItem.id,
    modeName: modeItem.name,
    modeColor: modeItem.color,
    elapsedDays: null,
    shipsFlown: [],
    metrics: normalizeMetrics(),
    realization: normalizeRealization(),
    derived: EMPTY_DERIVED,
  }))

  return {
    schemaVersion: 1,
    exportedAt: PINNED_EXPORT_NOW_ISO,
    exportedAtGalactic: PINNED_EXPORT_NOW_GALACTIC,
    appVersion: 'profile',
    commanders: [],
    departed: Boolean(sidebar.departed),
    effectiveStart: sidebar.start || plan.expedition.startTimestamp || PINNED_EXPORT_NOW_ISO,
    effectiveStartGalactic: sidebar.startGalactic || plan.startGalactic || '',
    effectiveEnd: sidebar.end || plan.expedition.endTimestamp || '',
    effectiveEndGalactic: sidebar.endGalactic || plan.endGalactic || '',
    endIsNow: Boolean(sidebar.endIsNow),
    duration: sidebar.duration || '',
    careerStart: sidebar.careerStart || '',
    careerStartGalactic: sidebar.careerStartGalactic || '',
    careerDuration: sidebar.careerDuration || '',
    plan,
    expedition: {
      metrics: normalizeMetrics(sidebar.expedition),
      realization: normalizeRealization(sidebar.realization),
      elapsedDays: null,
      shipsFlown: [],
      derived: EMPTY_DERIVED,
    },
    live: normalizeMetrics(sidebar.live),
    adjustments: normalizeMetrics(sidebar.adjustments),
    carrierCost: sidebar.carrierCost ?? {
      carrierType: plan.expedition.carrierType || 'none',
      weeklyUpkeep: plan.expedition.carrierWeeklyUpkeep || 0,
      tritiumPrice: 0,
      tritiumCost: 0,
      jumpCost: 0,
      upkeepCost: 0,
      totalCost: 0,
      totalEstimated: false,
    },
    originBaseline,
    originBaselineElapsedDays: null,
    originBaselineDerived: originBaseline ? EMPTY_DERIVED : undefined,
    modeBuckets: activeModeBuckets,
    rowDetails,
    legTelemetry: [],
    regionBuckets: [],
    highlightsFullOrder: [],
    highlightsVariants: [],
  }
}

function buildProbeExportCart(exportData: ExpeditionExportData): CartBlockItem[] {
  return [
    ...createQuickShareCart(exportData),
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'details',
        section: 'flight',
        scope: { kind: 'full' },
        modeGrouping: 'separate',
        selectedMetrics: defaultMetricsForSection('flight'),
      },
      'probe-details-flight',
    ),
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'details',
        section: 'realized',
        scope: { kind: 'full' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection('realized'),
      },
      'probe-details-realized',
    ),
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'details',
        section: 'cartographics',
        scope: { kind: 'allRegions' },
        modeGrouping: 'combined',
        selectedMetrics: defaultMetricsForSection('cartographics'),
      },
      'probe-details-regions',
    ),
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'highlights',
        depth: 3,
        selectedCards: HIGHLIGHT_CARDS.map((card) => card.id),
        lucrativeMetric: 'estimated',
        distanceMetric: 'total',
      },
      'probe-highlights-top3',
    ),
    ...expandConfigToCartItems(
      exportData,
      {
        kind: 'waypoints',
        waypointsScope: 'fullPlan',
        selectedColumns: WAYPOINTS_COLUMNS.map((column) => column.id),
      },
      'probe-waypoints-full',
    ),
  ]
}

/**
 * executeExportStudioPass benchmarks Export Studio cart expansion, SSR card
 * rendering via ExportBlockRenderer, and self-contained HTML report assembly
 * under a deterministic pinned clock.
 */
export function executeExportStudioPass(hydrated: HydratedFrontendFixture): {
  summary: string
  hashInput: string
  children: SpanNodeJSON[]
} {
  const exportData: ExpeditionExportData = hydrated.exportData ?? synthesizeExportData(hydrated)
  const children: SpanNodeJSON[] = []
  const originalDateNow = Date.now
  Date.now = () => PINNED_EXPORT_NOW_MS

  try {
    const tCart = performance.now()
    const cartItems = buildProbeExportCart(exportData)
    children.push(
      makeRenderSpan(
        'Export Cart Expansion (Quick Share, Details, Highlights, Waypoints)',
        'frontend/src/features/export/exportCartExpansion.ts',
        1,
        cartItems.length,
        'blocks',
        msToNs(performance.now() - tCart),
      ),
    )

    const tBlocks = performance.now()
    const blockMarkup = cartItems
      .map((item) =>
        renderToStaticMarkup(
          createElement(ExportBlockRenderer, { exportData, config: item.config }),
        ),
      )
      .join('')
    children.push(
      makeRenderSpan(
        'ExportBlockRenderer (SSR Block Batch)',
        'frontend/src/features/export/blocks/ExportBlockRenderer.tsx:20',
        cartItems.length,
        cartItems.length,
        'blocks',
        msToNs(performance.now() - tBlocks),
      ),
    )

    const tHtml = performance.now()
    const htmlReport = generateSelfContainedHtmlReport(exportData, cartItems)
    children.push(
      makeRenderSpan(
        'generateSelfContainedHtmlReport',
        'frontend/src/features/export/htmlGenerator.ts:37',
        1,
        cartItems.length,
        'sections',
        msToNs(performance.now() - tHtml),
      ),
    )

    return {
      summary: `Blocks=${cartItems.length} BlockBytes=${blockMarkup.length} HtmlBytes=${htmlReport.length}`,
      hashInput: `${blockMarkup}\n${htmlReport}`,
      children,
    }
  } finally {
    Date.now = originalDateNow
  }
}
