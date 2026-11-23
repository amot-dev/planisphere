import { expedition } from '../../api/models'
import {
  formatCredits,
  formatEfficiencyBonusLabel,
  formatInt,
  formatLoss,
  formatLy,
} from '../../domain/galacticTime'
import { computeAllHighlightLeaderboards, selectHighlightCards } from './exportHighlights'
import {
  CareerDiffItem,
  formatEstimatedCarrierCredits,
  isDetailsMetricAvailable,
  ResolvedTelemetryContext,
  resolveTelemetryContext,
  sanitizeExportHexColor,
} from './exportMetrics'
import {
  CareerDiffDisplay,
  CartBlockItem,
  DETAILS_SECTION_METRICS,
  DETAILS_SECTIONS,
  DetailsBlockConfig,
  DetailsMetricId,
  DetailsSection,
  ExpeditionExportData,
  HighlightsBlockConfig,
  SummaryBlockConfig,
  WaypointsBlockConfig,
} from './exportTypes'

const HTML_ENTITY_PREFIX = String.fromCharCode(38)

/**
 * Generates a self-contained, offline-capable HTML report with inlined cockpit
 * styling and print-ready media rules matching the deterministic cart order.
 */
export function generateSelfContainedHtmlReport(
  exportData: ExpeditionExportData,
  cartItems: CartBlockItem[],
): string {
  const expeditionName = escapeHtml(exportData.plan.expedition.name)
  const navLinks = cartItems
    .map(
      (item, index) =>
        `<a class="toc-link" href="#section-${index + 1}">${index + 1}. ${escapeHtml(item.title)}</a>`,
    )
    .join('')
  const renderedSections = cartItems
    .map((item, index) => renderCartSection(exportData, item, index + 1))
    .join('\n')

  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>${expeditionName} — Planisphere Expedition Report</title>
<style>${REPORT_STYLESHEET}</style>
</head>
<body>
<header class="report-topbar">
  <div>
    <div class="eyebrow">PLANISPHERE // ASTROMETRIC TELEMETRY REPORT</div>
    <h1 class="report-title">${expeditionName}</h1>
  </div>
  <div class="topbar-meta mono">
    <span>STATUS: ${escapeHtml(exportData.plan.expedition.status.toUpperCase())}</span>
    <span>EXPORTED: ${escapeHtml(exportData.exportedAtGalactic)}</span>
  </div>
</header>
${cartItems.length > 1 ? `<nav class="toc-bar">${navLinks}</nav>` : ''}
<main class="report-main">
${renderedSections}
</main>
<footer class="report-footer mono">
  PLANISPHERE v${escapeHtml(exportData.appVersion || 'dev')} // EXPORTED ${escapeHtml(exportData.exportedAtGalactic)}
</footer>
</body>
</html>`
}

function escapeHtml(rawText: string | number | null | undefined): string {
  const text = String(rawText ?? '')
  return text
    .replaceAll(HTML_ENTITY_PREFIX, `${HTML_ENTITY_PREFIX}amp;`)
    .replaceAll('<', `${HTML_ENTITY_PREFIX}lt;`)
    .replaceAll('>', `${HTML_ENTITY_PREFIX}gt;`)
    .replaceAll('"', `${HTML_ENTITY_PREFIX}quot;`)
    .replaceAll("'", `${HTML_ENTITY_PREFIX}#39;`)
}

function renderCartSection(
  exportData: ExpeditionExportData,
  item: CartBlockItem,
  sectionIndex: number,
): string {
  switch (item.config.kind) {
    case 'summary':
      return renderSummarySection(exportData, item.config, sectionIndex)
    case 'details':
      return renderDetailsSection(exportData, item.config, sectionIndex)
    case 'waypoints':
      return renderWaypointsSection(exportData, item.config, sectionIndex)
    case 'highlights':
      return renderHighlightsSection(exportData, item.config, sectionIndex)
  }
}

function renderTelemetryHeader(
  eyebrow: string,
  title: string,
  context: ResolvedTelemetryContext,
  accentClass: string,
): string {
  const safeModeColor = context.modeBadge
    ? sanitizeExportHexColor(context.modeBadge.modeColor)
    : ''
  const modePill = context.modeBadge
    ? `<span class="badge" style="border-color:${safeModeColor};color:${safeModeColor}">${escapeHtml(context.modeBadge.modeName)}</span>`
    : ''
  const inSystemPill = context.inSystem
    ? `<span class="badge badge-cyan">IN SYSTEM</span>`
    : ''
  return `<div class="block-header ${accentClass}">
    <div>
      <div class="eyebrow">${escapeHtml(eyebrow)}</div>
      <div class="block-title-row">
        <h2 class="block-title">${escapeHtml(title)}</h2>
        ${modePill}${inSystemPill}
      </div>
    </div>
    <div class="header-dates mono">
      <div>${escapeHtml(context.fromGalactic)} -> ${escapeHtml(context.toGalactic)}</div>
      ${context.durationLabel ? `<div class="muted">ELAPSED: ${escapeHtml(context.durationLabel)}</div>` : ''}
    </div>
  </div>`
}

function renderSummarySection(
  exportData: ExpeditionExportData,
  config: SummaryBlockConfig,
  sectionIndex: number,
): string {
  const context = resolveTelemetryContext(
    exportData,
    config.scope,
    config.selectedModeId,
  )
  const diffMap = new Map<CareerDiffItem['id'], CareerDiffItem>()
  if (config.careerDiff !== 'hide' && context.careerDiff) {
    for (const diffItem of context.careerDiff) {
      diffMap.set(diffItem.id, diffItem)
    }
  }
  const hasCarrier =
    context.carrierCost != null &&
    (context.metrics.carrierJumps > 0 || context.carrierCost.totalCost > 0)
  const netProfit =
    context.derived.netProfitAfterCarrierCost ?? context.realization.totalRealized
  const netProfitIsEstimated = Boolean(
    context.carrierCost && context.carrierCost.totalCost > 0 && context.carrierCost.totalEstimated,
  )
  const formattedNetProfit = `${netProfitIsEstimated ? '~' : ''}${formatCredits(netProfit)}`

  const routeItems: string[] = []
  if (exportData.commanders.length > 0) {
    routeItems.push(`<span>CMDR: <strong class="mono">${escapeHtml(exportData.commanders.join(', '))}</strong></span>`)
  }
  if (context.visitedWaypoints != null && context.plannedWaypoints != null) {
    routeItems.push(`<span>WAYPOINTS: <strong class="mono">${context.visitedWaypoints} / ${context.plannedWaypoints}</strong></span>`)
  }
  if (context.distinctRegionCount != null) {
    routeItems.push(`<span>REGIONS: <strong class="mono">${context.distinctRegionCount}</strong></span>`)
  }
  if (context.shipsFlown.length > 0) {
    routeItems.push(`<span>SHIPS: <strong class="mono">${escapeHtml(context.shipsFlown.join(', '))}</strong></span>`)
  }
  const sectionStyle = context.modeBadge
    ? ` style="border-left:3px solid ${sanitizeExportHexColor(context.modeBadge.modeColor)}"`
    : ''

  return `<section id="section-${sectionIndex}" class="card"${sectionStyle}>
  ${renderTelemetryHeader('SUMMARY', `${context.routeTitle} — ${context.scopeLabel}`, context, 'accent-amber')}
  ${routeItems.length > 0 ? `<div class="strip-bar">${routeItems.join('<span class="sep">|</span>')}</div>` : ''}
  <div class="kpi-grid cols-4" style="padding-bottom:10px">
    <div class="kpi-tile accent-left-amber">
      <div class="kpi-label">Distance Travelled</div>
      <div class="kpi-val amber mono">${escapeHtml(formatLy(context.metrics.distanceLy, 1))}</div>
      <div class="kpi-sub mono">Ship ${escapeHtml(formatLy(context.derived.shipDistanceLy, 1))} · Carrier ${escapeHtml(formatLy(context.metrics.carrierPassengerLy, 1))}</div>
      ${renderInlineCareerDiff(diffMap.get('distanceLy'), config.careerDiff, (val) => formatLy(val, 1))}
    </div>
    <div class="kpi-tile accent-left-amber">
      <div class="kpi-label">Hyperspace Jumps</div>
      <div class="kpi-val mono">${escapeHtml(formatInt(context.metrics.jumps))}</div>
      <div class="kpi-sub mono">${context.derived.avgJumpRangeLy != null ? `${formatLy(context.derived.avgJumpRangeLy, 1)} avg` : 'No piloted jumps'}</div>
      ${renderInlineCareerDiff(diffMap.get('jumps'), config.careerDiff, formatInt)}
    </div>
    <div class="kpi-tile accent-left-cyan">
      <div class="kpi-label">Estimated Exploration</div>
      <div class="kpi-val cyan mono">${escapeHtml(formatCredits(context.derived.combinedEstimatedCredits))}</div>
      <div class="kpi-sub mono">Carto ${escapeHtml(formatCredits(context.metrics.scanValueTotal))} · Exo ${escapeHtml(formatCredits(context.metrics.organicValueTotal))}</div>
      ${renderInlineCareerDiff(diffMap.get('estimatedValue'), config.careerDiff, formatCredits)}
    </div>
    <div class="kpi-tile accent-left-emerald">
      <div class="kpi-label">Realized Net Value</div>
      <div class="kpi-val amber mono">${escapeHtml(formatCredits(context.realization.totalRealized))}</div>
      <div class="kpi-sub mono">${context.realization.pendingValue > 0 ? `Pending ${formatCredits(context.realization.pendingValue)}` : `Losses ${formatLoss(context.derived.totalLossesCredits)}`}</div>
      ${renderInlineCareerDiff(diffMap.get('realizedValue'), config.careerDiff, formatCredits)}
    </div>
  </div>
  <div class="kpi-grid ${hasCarrier ? 'cols-4' : 'cols-3'}" style="padding-top:0">
    <div class="kpi-tile accent-left-cyan">
      <div class="kpi-label">Bodies Scanned</div>
      <div class="kpi-val cyan mono">${formatInt(context.metrics.bodiesScanned)}</div>
      <div class="kpi-sub mono">${formatInt(context.metrics.firstDiscoveries)} first discovered</div>
      ${renderInlineCareerDiff(diffMap.get('bodiesScanned'), config.careerDiff, formatInt)}
    </div>
    <div class="kpi-tile accent-left-cyan">
      <div class="kpi-label">Bodies Mapped</div>
      <div class="kpi-val cyan mono">${formatInt(context.metrics.bodiesMapped)}</div>
      <div class="kpi-sub mono">${formatInt(context.metrics.firstMapped)} first mapped</div>
      ${renderInlineCareerDiff(diffMap.get('bodiesMapped'), config.careerDiff, formatInt)}
    </div>
    <div class="kpi-tile accent-left-emerald">
      <div class="kpi-label">Organic Samples</div>
      <div class="kpi-val emerald mono">${formatInt(context.metrics.organicSamples)}</div>
      <div class="kpi-sub mono">${formatInt(context.metrics.firstLogged)} first logged</div>
      ${renderInlineCareerDiff(diffMap.get('organicSamples'), config.careerDiff, formatInt)}
    </div>
    ${
      hasCarrier && context.carrierCost
        ? `<div class="kpi-tile accent-left-amber">
      <div class="kpi-label">Carrier</div>
      <div class="kpi-val amber mono">${escapeHtml(formatEstimatedCarrierCredits(context.carrierCost.totalCost, Boolean(context.carrierCost.totalEstimated)))}</div>
      <div class="kpi-sub mono">${formatInt(context.metrics.carrierJumps)} jumps · ${escapeHtml(formatLy(context.metrics.carrierDistanceLy, 1))} · ${formatInt(context.metrics.tritiumUsed ?? 0)} t</div>
    </div>`
        : ''
    }
  </div>
  <div class="strip-bar" style="border-top:1px solid #2e1f12">
    <span>NET PROFIT: <strong class="mono cyan">${escapeHtml(formattedNetProfit)}</strong></span>
  </div>
</section>`
}

function renderInlineCareerDiff(
  diffItem: CareerDiffItem | undefined,
  mode: CareerDiffDisplay,
  formatValue: (value: number) => string,
): string {
  if (!diffItem || mode === 'hide') return ''
  const parts: string[] = []
  if (mode === 'absolute' || mode === 'both') {
    parts.push(`PRE ${formatValue(diffItem.baseline)}`)
  }
  if (mode === 'percentage' || mode === 'both') {
    const growth = diffItem.isNewBaseline
      ? 'NEW'
      : diffItem.growthPercent != null
        ? `+${diffItem.growthPercent.toFixed(1)}%`
        : '—'
    parts.push(growth)
  }
  return `<div class="diff-pill mono">${escapeHtml(parts.join(' · '))}</div>`
}

function renderDetailsSection(
  exportData: ExpeditionExportData,
  config: DetailsBlockConfig,
  sectionIndex: number,
): string {
  const context = resolveTelemetryContext(
    exportData,
    config.scope,
    config.selectedModeId,
  )
  const concreteSection: DetailsSection = config.section === 'all' ? 'flight' : config.section
  const sectionMeta = DETAILS_SECTIONS.find((section) => section.id === concreteSection)!
  const selectedSet = new Set<DetailsMetricId>(config.selectedMetrics)
  const descriptors = DETAILS_SECTION_METRICS[concreteSection].filter(
    (descriptor) =>
      selectedSet.has(descriptor.id) &&
      isDetailsMetricAvailable(descriptor, context, config.scope.kind),
  )

  const kpiTiles = descriptors
    .filter((descriptor) => descriptor.zone === 'kpi')
    .map((descriptor) => renderDetailsKpiTile(descriptor.id, context))
    .join('')
  const breakdownCards = descriptors
    .filter((descriptor) => descriptor.zone === 'card')
    .map((descriptor) => renderDetailsBreakdownCard(descriptor.id, context))
    .join('')
  const kpiCount = descriptors.filter((descriptor) => descriptor.zone === 'kpi').length
  const colsClass = kpiCount <= 1 ? 'cols-1' : kpiCount === 2 || kpiCount === 4 ? 'cols-2' : 'cols-3'
  const sectionStyle = context.modeBadge
    ? ` style="border-left:3px solid ${sanitizeExportHexColor(context.modeBadge.modeColor)}"`
    : ''
  const bodyHtml =
    descriptors.length === 0
      ? `<div class="empty-banner mono">Select at least one applicable data point to preview and add this block</div>`
      : `${kpiCount > 0 ? `<div class="kpi-grid ${colsClass}">${kpiTiles}</div>` : ''}${breakdownCards ? `<div class="card-stack">${breakdownCards}</div>` : ''}`

  return `<section id="section-${sectionIndex}" class="card"${sectionStyle}>
  ${renderTelemetryHeader(sectionMeta.label.toUpperCase(), `${context.routeTitle} — ${context.scopeLabel}`, context, `accent-${sectionMeta.accent}`)}
  ${bodyHtml}
</section>`
}

function renderDetailsKpiTile(metricId: DetailsMetricId, context: ResolvedTelemetryContext): string {
  const { metrics, realization: real, derived, carrierCost } = context
  const tile = (label: string, value: string, sub = '', accent = '') =>
    `<div class="kpi-tile"><div class="kpi-label">${escapeHtml(label)}</div><div class="kpi-val mono ${accent}">${escapeHtml(value)}</div>${sub ? `<div class="kpi-sub mono">${escapeHtml(sub)}</div>` : ''}</div>`

  switch (metricId) {
    case 'jumps':
      return tile('Hyperspace Jumps', formatInt(metrics.jumps))
    case 'shipsFlown':
      return tile('Ships Flown', String(context.shipsFlown.length), context.shipsFlown.join(', ') || 'None')
    case 'distinctRegions':
      return tile('Distinct Galactic Regions', formatInt(context.distinctRegionCount ?? 0))
    case 'avgJumpRange':
      return tile('Avg Jump Range', formatOptionalLy(derived.avgJumpRangeLy))
    case 'boostRate':
      return tile('Boost Rate', formatOptionalPercent(derived.boostRatePercent))
    case 'avgDistancePerDay':
      return tile('Avg Distance per Day', formatOptionalLy(derived.avgDistancePerDayLy))
    case 'carrierJumps':
      return tile('Carrier Jumps', formatInt(metrics.carrierJumps))
    case 'carrierDistance':
      return tile('Carrier Distance', formatLy(metrics.carrierDistanceLy, 1))
    case 'avgCarrierJumpRange':
      return tile('Avg Carrier Jump Range', formatOptionalLy(derived.avgCarrierJumpRangeLy))
    case 'carrierTritium':
      return tile('Carrier Tritium Used', `${formatInt(metrics.tritiumUsed ?? 0)} t`)
    case 'avgTritiumPerJump':
      return tile('Avg Tritium / Jump', derived.avgTritiumPerJump != null ? `${derived.avgTritiumPerJump.toFixed(1)} t` : '—')
    case 'discoveryCountsSummary':
      return tile('Discovery Counts', `${formatInt(metrics.bodiesScanned)} bodies · ${formatInt(metrics.organicSamples)} samples`, `${formatInt(metrics.firstDiscoveries)} first disc · ${formatInt(metrics.firstLogged)} first logged`, 'cyan')
    case 'realizedPendingSnapshot':
      return tile('Realized / Pending', formatCredits(real.totalRealized), `Pending: ${formatCredits(real.pendingValue)}`, 'amber')
    case 'avgCombinedCreditsPerJump':
      return tile('Avg Credits / Jump', formatOptionalCredits(derived.avgCombinedCreditsPerJump), '', 'cyan')
    case 'avgCombinedCreditsPer1000Ly':
      return tile('Avg Credits / 1,000 Ly', formatOptionalCredits(derived.avgCombinedCreditsPer1000Ly), '', 'cyan')
    case 'avgCombinedCreditsPerDay':
      return tile('Avg Credits / Day', formatOptionalCredits(derived.avgCombinedCreditsPerDay), '', 'cyan')
    case 'cartoTotalValue':
      return tile('Cartographics Value', formatCredits(metrics.scanValueTotal), '', 'cyan')
    case 'bodiesScanned':
      return tile('Bodies Scanned', formatInt(metrics.bodiesScanned), `${formatInt(metrics.firstDiscoveries)} first discovered`, 'cyan')
    case 'bodiesMapped':
      return tile('Bodies Mapped', formatInt(metrics.bodiesMapped), `${formatInt(metrics.firstMapped)} first mapped`, 'cyan')
    case 'dssEfficiencyRate':
      return tile('DSS Probe Efficiency', formatOptionalPercent(derived.dssEfficiencyPercent), `${formatInt(metrics.efficientMappings)} efficient`, 'cyan')
    case 'firstDiscoveryRate':
      return tile('First Discovery Rate', formatOptionalPercent(derived.firstDiscoveryRatePercent), '', 'cyan')
    case 'avgCartoCreditsPerJump':
      return tile('Avg Carto Cr / Jump', formatOptionalCredits(derived.avgCartoCreditsPerJump), '', 'cyan')
    case 'avgCartoCreditsPer1000Ly':
      return tile('Avg Carto Cr / 1,000 Ly', formatOptionalCredits(derived.avgCartoCreditsPer1000Ly), '', 'cyan')
    case 'exoTotalValue':
      return tile('Exobiology Value', formatCredits(metrics.organicValueTotal), '', 'emerald')
    case 'organicSamples':
      return tile('Organic Samples', formatInt(metrics.organicSamples), `${formatInt(metrics.firstLogged)} first logged`, 'emerald')
    case 'firstLoggedRate':
      return tile('First Logged Rate', formatOptionalPercent(derived.firstLoggedRatePercent), '', 'emerald')
    case 'avgCreditsPerSample':
      return tile('Avg Credits / Sample', formatOptionalCredits(derived.avgCreditsPerSample), '', 'emerald')
    case 'avgExoCreditsPerJump':
      return tile('Avg Exo Cr / Jump', formatOptionalCredits(derived.avgExoCreditsPerJump), '', 'emerald')
    case 'avgExoCreditsPer1000Ly':
      return tile('Avg Exo Cr / 1,000 Ly', formatOptionalCredits(derived.avgExoCreditsPer1000Ly), '', 'emerald')
    case 'realizedTotal':
      return tile('Total Realized Net', formatCredits(real.totalRealized), '', 'amber')
    case 'pendingValue':
      return tile('Pending (In Transit)', formatCredits(real.pendingValue))
    case 'estimatedValueTotal':
      return tile('Combined Estimated', formatCredits(derived.combinedEstimatedCredits))
    case 'realizationRetentionRate':
      return tile('Realization Retention', formatOptionalPercent(derived.realizationRetentionPercent), '', 'amber')
    case 'netProfitAfterCarrierCost':
      return tile('Net Profit After Carrier', formatOptionalCredits(derived.netProfitAfterCarrierCost), carrierCost ? `Carrier cost: ${formatEstimatedCarrierCredits(carrierCost.totalCost, Boolean(carrierCost.totalEstimated))}` : '', 'amber')
    case 'avgRealizedCreditsPerJump':
      return tile('Avg Realized Cr / Jump', formatOptionalCredits(derived.avgRealizedCreditsPerJump), '', 'amber')
    case 'avgRealizedCreditsPer1000Ly':
      return tile('Avg Realized Cr / 1,000 Ly', formatOptionalCredits(derived.avgRealizedCreditsPer1000Ly), '', 'amber')
    default:
      return ''
  }
}

function renderDetailsBreakdownCard(
  metricId: DetailsMetricId,
  context: ResolvedTelemetryContext,
): string {
  const { metrics, realization: real, carrierCost, derived } = context
  const row = (label: string, value: string, valueClass = '') =>
    `<div class="bd-row"><span class="muted">${escapeHtml(label)}</span><span class="mono ${valueClass}">${escapeHtml(value)}</span></div>`

  switch (metricId) {
    case 'distanceBreakdown':
      return `<div class="bd-card accent-left-amber"><div class="bd-head"><span>Distance Travelled</span><span class="mono amber">${escapeHtml(formatLy(metrics.distanceLy))}</span></div>${row('By Ship (FSD)', formatLy(derived.shipDistanceLy))}${row('By Carrier (Passenger)', formatLy(metrics.carrierPassengerLy))}</div>`
    case 'fsdBoosts':
      return `<div class="bd-card accent-left-amber"><div class="bd-head"><span>FSD Boosts</span><span class="mono amber">${formatInt(derived.totalBoosts)} total</span></div>${row('Jet Cone Boosts', formatInt(metrics.jetConeBoosts))}${row('Premium Injections', formatInt(metrics.premiumBoosts))}${row('Standard Injections', formatInt(metrics.standardBoosts))}${row('Basic Injections', formatInt(metrics.basicBoosts))}</div>`
    case 'carrierCosts':
      return carrierCost
        ? `<div class="bd-card accent-left-amber"><div class="bd-head"><span>Carrier Cost Breakdown (${escapeHtml(carrierCost.carrierType)})</span><span class="mono amber">${escapeHtml(formatEstimatedCarrierCredits(carrierCost.totalCost, Boolean(carrierCost.totalEstimated)))}</span></div>${row('Tritium Cost', formatEstimatedCarrierCredits(carrierCost.tritiumCost))}${row('Jump Maintenance', formatCredits(carrierCost.jumpCost))}${row('Prorated Weekly Upkeep', formatEstimatedCarrierCredits(carrierCost.upkeepCost))}</div>`
        : ''
    case 'combinedEstimatedValue':
      return `<div class="bd-card accent-left-cyan"><div class="bd-head"><span>Combined Estimated Exploration Value</span><span class="mono cyan">${escapeHtml(formatCredits(derived.combinedEstimatedCredits))}</span></div>${row('Universal Cartographics', formatCredits(metrics.scanValueTotal), 'cyan')}${row('Vista Genomics (Exobiology)', formatCredits(metrics.organicValueTotal), 'emerald')}</div>`
    case 'cartoValueCard':
    case 'cartoValueBreakdown':
      return `<div class="bd-card accent-left-cyan"><div class="bd-head"><span>Cartographics Valuation</span><span class="mono cyan">${escapeHtml(formatCredits(metrics.scanValueTotal))}</span></div>${row('Base Scans and Mapping', formatCredits(metrics.scanValueBase))}${row('First Discovered / Mapped', `+${formatCredits(metrics.scanValueBonus)}`, 'cyan')}${row(formatEfficiencyBonusLabel(metrics.efficientMappings, metrics.bodiesMapped), `+${formatCredits(metrics.scanValueEfficiencyBonus)}`, 'cyan')}</div>`
    case 'exoValueCard':
    case 'exoValueBreakdown':
      return `<div class="bd-card accent-left-emerald"><div class="bd-head"><span>Exobiology Valuation</span><span class="mono emerald">${escapeHtml(formatCredits(metrics.organicValueTotal))}</span></div>${row('Base Organic Value', formatCredits(metrics.organicValueBase))}${row('First Logged Bonus', `+${formatCredits(metrics.organicValueBonus)}`, 'emerald')}</div>`
    case 'lossBreakdown':
      return `<div class="bd-card accent-left-danger"><div class="bd-head"><span>Realization Losses and Deductions</span><span class="mono danger">${escapeHtml(formatLoss(derived.totalLossesCredits))}</span></div>${row('Cartographics Lost (Death)', formatLoss(real.cartoLostDeath))}${row('Exobiology Lost (Death)', formatLoss(real.organicLostDeath))}${row('Cartographics Stolen', formatLoss(real.cartoStolen))}${row('Exobiology Stolen', formatLoss(real.organicStolen))}${row('Carrier Tariffs', formatLoss(real.carrierTariffLoss))}${row('NPC Crew Payouts', formatLoss(real.npcCrewLoss))}</div>`
    default:
      return ''
  }
}

function renderWaypointsSection(
  exportData: ExpeditionExportData,
  config: WaypointsBlockConfig,
  sectionIndex: number,
): string {
  const columns = new Set(config.selectedColumns)
  const detailsByRowId = new Map<number, expedition.WaypointDetails>()
  for (const detail of exportData.rowDetails ?? []) {
    detailsByRowId.set(detail.rowId, detail)
  }
  const rows = (exportData.plan.rows ?? []).filter((rowView) => {
    if (rowView.row.rowType === 'region') {
      return columns.has('regionHeaders')
    }
    if (config.waypointsScope === 'visited') {
      return Boolean(rowView.row.visitedAt)
    }
    return true
  })

  const headers: string[] = ['<th>#</th>', '<th>System</th>']
  if (columns.has('timestamps')) headers.push('<th>Galactic Time</th>')
  if (columns.has('legDistance')) headers.push('<th>Leg Ly</th>')
  if (columns.has('duration')) headers.push('<th>Duration</th>')
  if (columns.has('shipsFlown')) headers.push('<th>Ships</th>')
  if (columns.has('flightMode')) headers.push('<th>Mode</th>')
  if (columns.has('fcStops')) headers.push('<th>FC</th>')
  if (columns.has('gecBadges')) headers.push('<th>GEC</th>')
  if (columns.has('notes')) headers.push('<th>Notes</th>')

  const bodyRows = rows
    .map((rowView) => {
      if (rowView.row.rowType === 'region') {
        return `<tr class="region-row"><td colspan="${headers.length}">${escapeHtml(rowView.regionName || 'Galactic Region')}</td></tr>`
      }
      const detail = detailsByRowId.get(rowView.row.id)
      const borderColor =
        rowView.number === 1 ? '#3d2816' : sanitizeExportHexColor(rowView.legModeColor)
      const cells: string[] = [
        `<td class="mono">${rowView.number}</td>`,
        `<td><strong>${escapeHtml(rowView.row.systemName)}</strong>${rowView.row.poiName ? `<div class="muted small">${escapeHtml(rowView.row.poiName)}</div>` : ''}</td>`,
      ]
      if (columns.has('timestamps')) {
        const tsText =
          rowView.number === 1
            ? rowView.departedAtGalactic
              ? `DEPARTED ${rowView.departedAtGalactic}`
              : 'AWAITING DEPARTURE'
            : rowView.row.visitedAt
              ? detail?.inSystem
                ? `${rowView.visitedAtGalactic} (IN SYSTEM)`
                : rowView.visitedAtGalactic
              : rowView.visitState === 'current'
                ? 'IN PROGRESS'
                : ''
        cells.push(`<td class="mono small">${escapeHtml(tsText)}</td>`)
      }
      if (columns.has('legDistance')) {
        cells.push(`<td class="mono small">${rowView.distFromPrevLy != null ? escapeHtml(formatLy(rowView.distFromPrevLy, 1)) : '—'}</td>`)
      }
      if (columns.has('duration')) {
        cells.push(`<td class="mono small">${escapeHtml(detail?.elapsed || '—')}</td>`)
      }
      if (columns.has('shipsFlown')) {
        cells.push(`<td class="mono small">${escapeHtml((detail?.shipsUsed ?? []).join(', ') || '—')}</td>`)
      }
      if (columns.has('flightMode')) {
        const hasLegMode = rowView.number > 1 && Boolean(rowView.legModeName)
        cells.push(
          `<td class="mono small">${hasLegMode ? `<span style="color:${sanitizeExportHexColor(rowView.legModeColor)}">${escapeHtml(rowView.legModeName)}</span>` : '—'}</td>`,
        )
      }
      if (columns.has('fcStops')) {
        cells.push(`<td class="mono small">${rowView.row.fcStop ? (rowView.row.fcArrived ? 'ARRIVED' : 'STOP') : '—'}</td>`)
      }
      if (columns.has('gecBadges')) {
        cells.push(`<td class="small">${rowView.gec ? escapeHtml(rowView.gec.name) : '—'}</td>`)
      }
      if (columns.has('notes')) {
        cells.push(`<td class="small muted">${escapeHtml(rowView.row.notes || '')}</td>`)
      }
      return `<tr style="border-left:3px solid ${escapeHtml(borderColor)}">${cells.join('')}</tr>`
    })
    .join('')

  return `<section id="section-${sectionIndex}" class="card">
  <div class="block-header accent-amber">
    <div>
      <div class="eyebrow">WAYPOINTS LOG</div>
      <h2 class="block-title">${escapeHtml(exportData.plan.expedition.name)} — ${config.waypointsScope === 'fullPlan' ? 'Full Plan' : 'Visited Waypoints'}</h2>
    </div>
    <div class="header-dates mono"><div>${exportData.plan.visitedWaypointCount} / ${exportData.plan.waypointCount} VISITED</div><div class="muted">COVERED ${escapeHtml(formatLy(exportData.plan.coveredLy, 1))}</div></div>
  </div>
  <table class="wp-table"><thead><tr>${headers.join('')}</tr></thead><tbody>${bodyRows}</tbody></table>
</section>`
}

function renderHighlightsSection(
  exportData: ExpeditionExportData,
  config: HighlightsBlockConfig,
  sectionIndex: number,
): string {
  const allBoards = computeAllHighlightLeaderboards(exportData, {
    lucrativeMetric: config.lucrativeMetric,
    distanceMetric: config.distanceMetric,
  })
  const visibleBoards = selectHighlightCards(allBoards, config.selectedCards, config.depth)
  const cardsHtml = visibleBoards
    .map((board) => {
      const entriesHtml = board.entries
        .map(
          (entry) => `<div class="hl-entry">
          <div class="hl-row"><span class="mono muted">#${entry.rank} ${escapeHtml(entry.label)}</span><strong class="mono ${board.accent}">${escapeHtml(entry.formattedPrimary)}</strong></div>
          <div class="small">${escapeHtml(entry.routeLabel)}${entry.inSystem ? ' <span class="cyan">[IN SYSTEM]</span>' : ''}</div>
          <div class="small muted mono">${escapeHtml(entry.secondaryLabel)}</div>
        </div>`,
        )
        .join('')
      return `<div class="hl-card accent-top-${board.accent}">
        <div class="kpi-label">${escapeHtml(board.title)}</div>
        ${entriesHtml}
      </div>`
    })
    .join('')

  const bodyHtml =
    visibleBoards.length === 0
      ? `<div class="empty-banner mono">Select at least one highlight card with recorded expedition data</div>`
      : `<div class="hl-grid">${cardsHtml}</div>`

  return `<section id="section-${sectionIndex}" class="card">
  <div class="block-header accent-cyan">
    <div>
      <div class="eyebrow">EXPEDITION HIGHLIGHTS // TOP ${config.depth}</div>
      <h2 class="block-title">${escapeHtml(exportData.plan.expedition.name)}</h2>
    </div>
    <div class="header-dates mono"><div>${visibleBoards.length} TROPHY CARDS</div><div class="muted">${escapeHtml(exportData.effectiveStartGalactic)}</div></div>
  </div>
  ${bodyHtml}
</section>`
}

function formatOptionalLy(value: number | null): string {
  return value != null ? formatLy(value, 1) : '—'
}

function formatOptionalPercent(value: number | null): string {
  return value != null ? `${value.toFixed(1)}%` : '—'
}

function formatOptionalCredits(value: number | null): string {
  return value != null ? formatCredits(value) : '—'
}

const REPORT_STYLESHEET = `
*{box-sizing:border-box;border-radius:0}
body{margin:0;background:#050506;color:#f4ece4;font-family:"Rajdhani","Bahnschrift","DIN Alternate","Segoe UI",sans-serif;line-height:1.4}
.mono{font-family:"JetBrains Mono","Consolas",ui-monospace,monospace;font-variant-numeric:tabular-nums}
.amber{color:#ff7100}.cyan{color:#00d2ff}.emerald{color:#00e5a3}.danger{color:#ff3b2f}.muted{color:#a6907c}.small{font-size:11px}
.report-topbar{display:flex;justify-content:space-between;align-items:center;padding:18px 24px;background:#140e09;border-bottom:2px solid #ff7100}
.eyebrow{font-size:10px;font-weight:700;letter-spacing:0.14em;color:#a6907c;text-transform:uppercase}
.report-title{margin:2px 0 0;font-size:20px;text-transform:uppercase;letter-spacing:0.08em}
.topbar-meta{display:flex;gap:16px;font-size:11px;color:#d6c7b8}
.toc-bar{position:sticky;top:0;z-index:10;display:flex;flex-wrap:wrap;gap:8px;padding:10px 24px;background:#0b0907;border-bottom:1px solid #3d2816}
.toc-link{color:#d6c7b8;text-decoration:none;font-size:11px;padding:3px 8px;border:1px solid #3d2816;background:#110c08}
.toc-link:hover{border-color:#ff7100;color:#ff7100}
.report-main{max-width:1020px;margin:20px auto;padding:0 16px;display:flex;flex-direction:column;gap:20px}
.card{background:#0b0907;border:1px solid #3d2816;break-inside:avoid}
.block-header{display:flex;justify-content:space-between;align-items:center;padding:14px 18px;background:#140e09;border-bottom:1px solid #3d2816}
.accent-amber{border-top:2px solid #ff7100}.accent-cyan{border-top:2px solid #00d2ff}.accent-emerald{border-top:2px solid #00e5a3}
.accent-left-amber{border-left:2px solid #ff7100}.accent-left-cyan{border-left:2px solid #00d2ff}.accent-left-emerald{border-left:2px solid #00e5a3}.accent-left-danger{border-left:2px solid #ff3b2f}
.accent-top-amber{border-top:2px solid #ff7100}.accent-top-cyan{border-top:2px solid #00d2ff}.accent-top-emerald{border-top:2px solid #00e5a3}.accent-top-danger{border-top:2px solid #ff3b2f}
.block-title-row{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.block-title{margin:2px 0 0;font-size:15px;text-transform:uppercase;letter-spacing:0.08em}
.badge{font-size:10px;font-weight:700;padding:1px 6px;border:1px solid #3d2816;text-transform:uppercase}
.badge-cyan{border-color:#00d2ff;color:#00d2ff}
.header-dates{text-align:right;font-size:11px}
.strip-bar{display:flex;flex-wrap:wrap;gap:10px;padding:8px 18px;background:#110c08;border-bottom:1px solid #2e1f12;font-size:11px}
.sep{color:#3d2816}
.kpi-grid{display:grid;gap:10px;padding:14px 18px}
.cols-1{grid-template-columns:1fr}.cols-2{grid-template-columns:repeat(2,1fr)}.cols-3{grid-template-columns:repeat(3,1fr)}.cols-4{grid-template-columns:repeat(4,1fr)}
.kpi-tile{background:#110c08;border:1px solid #2e1f12;padding:10px 12px}
.kpi-label{font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:0.1em;color:#a6907c}
.kpi-val{font-size:16px;font-weight:700;margin-top:4px}
.kpi-sub{font-size:11px;color:#a6907c;margin-top:3px}
.diff-pill{font-size:10px;color:#00d2ff;margin-top:4px;padding-top:4px;border-top:1px solid #2e1f12}
.card-stack{display:flex;flex-direction:column;gap:10px;padding:0 18px 16px}
.bd-card{background:#110c08;border:1px solid #2e1f12;padding:10px 14px}
.bd-head{display:flex;justify-content:space-between;font-size:12px;font-weight:700;text-transform:uppercase;padding-bottom:6px;margin-bottom:6px;border-bottom:1px solid #2e1f12}
.bd-row{display:flex;justify-content:space-between;font-size:11px;padding:2px 0}
.wp-table{width:100%;border-collapse:collapse;font-size:12px}
.wp-table th{text-align:left;padding:8px 10px;background:#110c08;border-bottom:1px solid #3d2816;font-size:10px;text-transform:uppercase;color:#a6907c}
.wp-table td{padding:7px 10px;border-bottom:1px solid #24180e;vertical-align:top}
.region-row td{background:#140e09;color:#ff7100;font-weight:700;font-size:11px;text-transform:uppercase;letter-spacing:0.12em}
.hl-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:12px;padding:14px 18px}
.hl-card{background:#110c08;border:1px solid #2e1f12;padding:10px 12px}
.hl-entry{padding:6px 0;border-top:1px solid #24180e;margin-top:6px}
.hl-row{display:flex;justify-content:space-between;gap:8px;font-size:12px}
.empty-banner{padding:24px 18px;text-align:center;font-size:11px;color:#a6907c}
.report-footer{text-align:center;padding:16px;font-size:10px;color:#6e5b4b}
@media print{
  body{background:#fff;color:#111}
  .toc-bar{display:none}
  .card,.kpi-tile,.bd-card,.hl-card{background:#fff;border-color:#ccc;color:#111}
  .report-topbar,.block-header,.strip-bar,.wp-table th,.region-row td{background:#f5f5f5;color:#111}
  .amber,.cyan,.emerald,.danger,.muted{color:#222}
}
`
