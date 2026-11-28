import {
  CartBlockItem,
  DetailsBlockConfig,
  ExpeditionExportData,
  HighlightsBlockConfig,
  SummaryBlockConfig,
  WaypointsBlockConfig,
  WaypointsColumnId,
} from './exportTypes'
import {
  buildDetailsViewModel,
  buildHighlightsViewModel,
  buildSummaryViewModel,
  buildWaypointsViewModel,
  DetailsBreakdownCardViewModel,
  DetailsKpiTileViewModel,
  formatTelemetryHeaderDurationLine,
  SummaryKpiTileViewModel,
  TelemetryHeaderViewModel,
  WaypointTableRowViewModel,
} from './exportViewModels'
import { escapeHtml, renderHtmlReportDocument } from './htmlReportTemplate'

/**
 * Generates a self-contained, offline-capable HTML report with inlined cockpit
 * styling and print-ready media rules matching the deterministic cart order.
 */
export function generateSelfContainedHtmlReport(
  exportData: ExpeditionExportData,
  cartItems: CartBlockItem[],
): string {
  const renderedSections = cartItems
    .map((item, index) => renderCartSection(exportData, item, index + 1))
    .join('\n')
  return renderHtmlReportDocument(exportData, renderedSections)
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

function renderTelemetryHeader(header: TelemetryHeaderViewModel): string {
  const modePill = header.modeBadge
    ? `<span class="badge" style="border-color:${header.modeBadge.safeColor};color:${header.modeBadge.safeColor}">${escapeHtml(header.modeBadge.modeName)}</span>`
    : ''
  const inSystemPill = header.inSystem
    ? `<span class="badge badge-cyan">IN SYSTEM</span>`
    : ''
  const fullTitle = `${header.routeTitle} — ${header.scopeLabel}`
  const durationLine = formatTelemetryHeaderDurationLine(header)
  return `<div class="block-header accent-${header.accent}">
    <div>
      <div class="eyebrow">${escapeHtml(header.eyebrowLabel)}</div>
      <div class="block-title-row">
        <h2 class="block-title">${escapeHtml(fullTitle)}</h2>
        ${modePill}${inSystemPill}
      </div>
    </div>
    <div class="header-dates mono">
      <div>${escapeHtml(header.fromGalactic)} -> ${escapeHtml(header.toGalactic)}</div>
      ${durationLine ? `<div class="muted">${escapeHtml(durationLine)}</div>` : ''}
    </div>
  </div>`
}

function renderSummarySection(
  exportData: ExpeditionExportData,
  config: SummaryBlockConfig,
  sectionIndex: number,
): string {
  const viewModel = buildSummaryViewModel(exportData, config)
  const routeItems = viewModel.routeSegments.map(
    (segment) =>
      `<span>${escapeHtml(segment.label)}: <strong class="mono">${escapeHtml(segment.value)}</strong></span>`,
  )
  const sectionStyle = viewModel.header.modeBadge
    ? ` style="border-left:3px solid ${viewModel.header.modeBadge.safeColor}"`
    : ''
  const netProfitHtml = viewModel.netProfit
    ? `\n  <div class="strip-bar strip-bar-bottom">
    <span>NET PROFIT: <strong class="mono ${viewModel.netProfit.isNegative ? 'danger' : 'cyan'}">${escapeHtml(viewModel.netProfit.text)}</strong></span>
  </div>`
    : ''

  return `<section id="section-${sectionIndex}" class="card"${sectionStyle}>
  ${renderTelemetryHeader(viewModel.header)}
  ${routeItems.length > 0 ? `<div class="strip-bar">${routeItems.join('<span class="sep">|</span>')}</div>` : ''}
  <div class="kpi-grid kpi-grid-top cols-4">
    ${viewModel.topRowTiles.map(renderSummaryKpiTile).join('\n    ')}
  </div>
  <div class="kpi-grid kpi-grid-bottom cols-${viewModel.bottomRowCols}">
    ${viewModel.bottomRowTiles.map(renderSummaryKpiTile).join('\n    ')}
  </div>${netProfitHtml}
</section>`
}

function renderSummaryKpiTile(tile: SummaryKpiTileViewModel): string {
  const valueToneClass = tile.valueTone === 'main' ? '' : `${tile.valueTone} `
  const diffHtml = tile.careerDiffText
    ? `<div class="diff-pill mono">${escapeHtml(tile.careerDiffText)}</div>`
    : ''
  return `<div class="kpi-tile accent-left-${tile.borderAccent}">
      <div class="kpi-label">${escapeHtml(tile.label)}</div>
      <div class="kpi-val ${valueToneClass}mono">${escapeHtml(tile.value)}</div>
      <div class="kpi-sub mono">${escapeHtml(tile.subValue)}</div>
      ${diffHtml}
    </div>`
}

function renderDetailsSection(
  exportData: ExpeditionExportData,
  config: DetailsBlockConfig,
  sectionIndex: number,
): string {
  const viewModel = buildDetailsViewModel(exportData, config)
  const sectionStyle = viewModel.header.modeBadge
    ? ` style="border-left:3px solid ${viewModel.header.modeBadge.safeColor}"`
    : ''
  const kpiHtml =
    viewModel.kpiTiles.length > 0
      ? `<div class="kpi-grid cols-${viewModel.kpiCols}">${viewModel.kpiTiles.map(renderDetailsKpiTile).join('')}</div>`
      : ''
  const cardsHtml =
    viewModel.breakdownCards.length > 0
      ? `<div class="card-stack">${viewModel.breakdownCards.map(renderDetailsBreakdownCard).join('')}</div>`
      : ''
  const bodyHtml = viewModel.isEmpty
    ? `<div class="empty-banner mono">${escapeHtml(viewModel.emptyMessage)}</div>`
    : `${kpiHtml}${cardsHtml}`

  return `<section id="section-${sectionIndex}" class="card"${sectionStyle}>
  ${renderTelemetryHeader(viewModel.header)}
  ${bodyHtml}
</section>`
}

function renderDetailsKpiTile(tile: DetailsKpiTileViewModel): string {
  const valueClass = tile.valueTone === 'main' ? 'kpi-val mono' : `kpi-val mono ${tile.valueTone}`
  const subHtml = tile.subValue ? `<div class="kpi-sub mono">${escapeHtml(tile.subValue)}</div>` : ''
  return `<div class="kpi-tile"><div class="kpi-label">${escapeHtml(tile.label)}</div><div class="${valueClass}">${escapeHtml(tile.value)}</div>${subHtml}</div>`
}

function renderDetailsBreakdownCard(card: DetailsBreakdownCardViewModel): string {
  const rowsHtml = card.rows
    .map((row) => {
      const valueClass = row.valueTone === 'main' ? 'mono' : `mono ${row.valueTone}`
      return `<div class="bd-row"><span class="muted">${escapeHtml(row.label)}</span><span class="${valueClass}">${escapeHtml(row.value)}</span></div>`
    })
    .join('')
  return `<div class="bd-card accent-left-${card.borderAccent}"><div class="bd-head"><span>${escapeHtml(card.title)}</span><span class="mono ${card.headerTone}">${escapeHtml(card.headerValue)}</span></div>${rowsHtml}</div>`
}

function renderWaypointsSection(
  exportData: ExpeditionExportData,
  config: WaypointsBlockConfig,
  sectionIndex: number,
): string {
  const viewModel = buildWaypointsViewModel(exportData, config)
  const headersHtml = viewModel.columnHeaders
    .map((headerLabel) => `<th>${escapeHtml(headerLabel)}</th>`)
    .join('')
  const bodyRowsHtml = viewModel.rows
    .map((row) => renderWaypointTableRow(row, viewModel.selectedColumns, viewModel.columnCount))
    .join('')

  return `<section id="section-${sectionIndex}" class="card">
  <div class="block-header accent-amber">
    <div>
      <div class="eyebrow">WAYPOINTS LOG</div>
      <h2 class="block-title">${escapeHtml(viewModel.expeditionName)} — ${escapeHtml(viewModel.scopeTitleLabel)}</h2>
    </div>
    <div class="header-dates mono"><div>${escapeHtml(viewModel.visitedStatusLabel)}</div><div class="muted">${escapeHtml(viewModel.coveredDistanceLabel)}</div></div>
  </div>
  <table class="wp-table"><thead><tr>${headersHtml}</tr></thead><tbody>${bodyRowsHtml}</tbody></table>
</section>`
}

function renderWaypointTableRow(
  row: WaypointTableRowViewModel,
  selectedColumns: ReadonlySet<WaypointsColumnId>,
  columnCount: number,
): string {
  if (row.kind === 'region') {
    return `<tr class="region-row"><td colspan="${columnCount}">${escapeHtml(row.regionTitle)}</td></tr>`
  }
  const poiHtml = row.poiName ? `<div class="muted small">${escapeHtml(row.poiName)}</div>` : ''
  const cells: string[] = [
    `<td class="mono">${row.number}</td>`,
    `<td><strong>${escapeHtml(row.systemName)}</strong>${poiHtml}</td>`,
  ]
  if (selectedColumns.has('timestamps')) cells.push(`<td class="mono small">${escapeHtml(row.timestampText)}</td>`)
  if (selectedColumns.has('legDistance')) cells.push(`<td class="mono small">${escapeHtml(row.legDistanceText)}</td>`)
  if (selectedColumns.has('duration')) cells.push(`<td class="mono small">${escapeHtml(row.durationText)}</td>`)
  if (selectedColumns.has('inGameDuration')) cells.push(`<td class="mono small">${escapeHtml(row.inGameDurationText)}</td>`)
  if (selectedColumns.has('shipsFlown')) cells.push(`<td class="mono small">${escapeHtml(row.shipsText)}</td>`)
  if (selectedColumns.has('flightMode')) {
    const modeHtml = row.modeBadge
      ? `<span style="color:${row.modeBadge.safeColor}">${escapeHtml(row.modeBadge.name)}</span>`
      : '—'
    cells.push(`<td class="mono small">${modeHtml}</td>`)
  }
  if (selectedColumns.has('fcStops')) cells.push(`<td class="mono small">${escapeHtml(row.fcStatusText)}</td>`)
  if (selectedColumns.has('gecBadges')) cells.push(`<td class="small">${escapeHtml(row.gecText)}</td>`)
  if (selectedColumns.has('notes')) cells.push(`<td class="small muted">${escapeHtml(row.notesText)}</td>`)
  return `<tr style="border-left:3px solid ${escapeHtml(row.borderColor)}">${cells.join('')}</tr>`
}

function renderHighlightsSection(
  exportData: ExpeditionExportData,
  config: HighlightsBlockConfig,
  sectionIndex: number,
): string {
  const viewModel = buildHighlightsViewModel(exportData, config)
  const cardsHtml = viewModel.boards
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

  const bodyHtml = viewModel.isEmpty
    ? `<div class="empty-banner mono">${escapeHtml(viewModel.emptyMessage)}</div>`
    : `<div class="hl-grid">${cardsHtml}</div>`

  return `<section id="section-${sectionIndex}" class="card">
  <div class="block-header accent-cyan">
    <div>
      <div class="eyebrow">${escapeHtml(viewModel.eyebrow)}</div>
      <h2 class="block-title">${escapeHtml(viewModel.expeditionName)}</h2>
    </div>
    <div class="header-dates mono"><div>${escapeHtml(viewModel.trophyCountLabel)}</div><div class="muted">${escapeHtml(viewModel.effectiveStartGalactic)}</div></div>
  </div>
  ${bodyHtml}
</section>`
}
