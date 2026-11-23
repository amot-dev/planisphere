import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { expedition, realization, stats } from '../api/models'
import { ExportBlockRenderer } from '../features/export/blocks/ExportBlockRenderer'
import { buildCanonicalJsonSnapshot } from '../features/export/exportHighlights'
import {
  addMetrics,
  addRealization,
  createEmptyMetrics,
  createEmptyRealization,
  createQuickShareCart,
  expandConfigToCartItems,
} from '../features/export/exportMetrics'
import {
  CartBlockItem,
  defaultMetricsForSection,
  ExpeditionExportData,
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

function normalizeMetrics(source?: stats.Metrics): stats.Metrics {
  const empty = createEmptyMetrics()
  if (!source) return empty
  return {
    distanceLy: source.distanceLy ?? empty.distanceLy,
    shipDistanceLy: source.shipDistanceLy ?? empty.shipDistanceLy,
    carrierPassengerLy: source.carrierPassengerLy ?? empty.carrierPassengerLy,
    jumps: source.jumps ?? empty.jumps,
    carrierJumps: source.carrierJumps ?? empty.carrierJumps,
    carrierDistanceLy: source.carrierDistanceLy ?? empty.carrierDistanceLy,
    tritiumUsed: source.tritiumUsed ?? empty.tritiumUsed,
    jetConeBoosts: source.jetConeBoosts ?? empty.jetConeBoosts,
    premiumBoosts: source.premiumBoosts ?? empty.premiumBoosts,
    standardBoosts: source.standardBoosts ?? empty.standardBoosts,
    basicBoosts: source.basicBoosts ?? empty.basicBoosts,
    bodiesScanned: source.bodiesScanned ?? empty.bodiesScanned,
    bodiesMapped: source.bodiesMapped ?? empty.bodiesMapped,
    efficientMappings: source.efficientMappings ?? empty.efficientMappings,
    scanValueTotal: source.scanValueTotal ?? empty.scanValueTotal,
    scanValueBase: source.scanValueBase ?? empty.scanValueBase,
    scanValueBonus: source.scanValueBonus ?? empty.scanValueBonus,
    scanValueEfficiencyBonus: source.scanValueEfficiencyBonus ?? empty.scanValueEfficiencyBonus,
    firstDiscoveries: source.firstDiscoveries ?? empty.firstDiscoveries,
    firstMapped: source.firstMapped ?? empty.firstMapped,
    organicSamples: source.organicSamples ?? empty.organicSamples,
    organicValueTotal: source.organicValueTotal ?? empty.organicValueTotal,
    organicValueBase: source.organicValueBase ?? empty.organicValueBase,
    organicValueBonus: source.organicValueBonus ?? empty.organicValueBonus,
    firstLogged: source.firstLogged ?? empty.firstLogged,
  }
}

function normalizeRealization(source?: realization.Breakdown): realization.Breakdown {
  const empty = createEmptyRealization()
  if (!source) return empty
  return {
    totalRealized: source.totalRealized ?? empty.totalRealized,
    cartoLostDeath: source.cartoLostDeath ?? empty.cartoLostDeath,
    organicLostDeath: source.organicLostDeath ?? empty.organicLostDeath,
    cartoStolen: source.cartoStolen ?? empty.cartoStolen,
    organicStolen: source.organicStolen ?? empty.organicStolen,
    carrierTariffLoss: source.carrierTariffLoss ?? empty.carrierTariffLoss,
    npcCrewLoss: source.npcCrewLoss ?? empty.npcCrewLoss,
    pendingValue: source.pendingValue ?? empty.pendingValue,
  }
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
  const detailsByRowId = new Map(rowDetails.map((detail) => [detail.rowId, detail]))

  const bucketByModeId = new Map<number, ExportModeBucket>()
  for (const modeItem of modesList) {
    bucketByModeId.set(modeItem.id, {
      modeId: modeItem.id,
      modeName: modeItem.name,
      modeColor: modeItem.color,
      metrics: createEmptyMetrics(),
      realization: createEmptyRealization(),
    })
  }
  for (const rowView of plan.rows ?? []) {
    const detail = detailsByRowId.get(rowView.row.id)
    const bucket = detail ? bucketByModeId.get(rowView.legModeId) : undefined
    if (detail && bucket) {
      bucket.metrics = addMetrics(bucket.metrics, detail.metrics)
      bucket.realization = addRealization(bucket.realization, detail.realization)
    }
  }
  const activeModeBuckets = Array.from(bucketByModeId.values()).filter(
    (bucket) => bucket.metrics.jumps > 0 || bucket.metrics.distanceLy > 0 || bucket.realization.totalRealized > 0,
  )

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
    postCareer: {
      metrics: normalizeMetrics(sidebar.allJournals),
      realization: normalizeRealization(sidebar.allJournalsRealization),
    },
    modeBuckets: activeModeBuckets,
    rowDetails,
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
 * rendering via ExportBlockRenderer, self-contained HTML report assembly, and
 * canonical JSON snapshot serialization under a deterministic pinned clock.
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
        'frontend/src/features/export/exportMetrics.ts:419',
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

    const tJson = performance.now()
    const canonicalJson = buildCanonicalJsonSnapshot(exportData)
    children.push(
      makeRenderSpan(
        'buildCanonicalJsonSnapshot',
        'frontend/src/features/export/exportHighlights.ts:89',
        1,
        exportData.rowDetails.length,
        'legs',
        msToNs(performance.now() - tJson),
      ),
    )

    return {
      summary: `Blocks=${cartItems.length} BlockBytes=${blockMarkup.length} HtmlBytes=${htmlReport.length} JsonBytes=${canonicalJson.length}`,
      hashInput: `${blockMarkup}\n${htmlReport}\n${canonicalJson}`,
      children,
    }
  } finally {
    Date.now = originalDateNow
  }
}
