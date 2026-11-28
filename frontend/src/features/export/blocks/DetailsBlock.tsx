import { memo } from 'react'
import {
  DetailsBlockConfig,
  ExpeditionExportData,
} from '../exportTypes'
import {
  buildDetailsViewModel,
  DetailsBreakdownCardViewModel,
  DetailsKpiTileViewModel,
  formatTelemetryHeaderDurationLine,
  TelemetryHeaderViewModel,
} from '../exportViewModels'

/** Props for the self-contained Details export card component. */
export interface DetailsBlockProps {
  exportData: ExpeditionExportData
  config: DetailsBlockConfig
}

/**
 * DetailsBlock renders a single telemetry pillar using a Two-Zone Auto-Flow
 * layout: balanced scalar KPI tiles above stacked full-width breakdown cards.
 */
export const DetailsBlock = memo(function DetailsBlock({
  exportData,
  config,
}: DetailsBlockProps) {
  const viewModel = buildDetailsViewModel(exportData, config)
  const accentStyle = viewModel.header.modeBadge
    ? { borderLeftColor: viewModel.header.modeBadge.safeColor, borderLeftWidth: '3px' }
    : undefined

  return (
    <div className="ed-exp-root" style={accentStyle}>
      <DetailsCardHeader header={viewModel.header} />

      {viewModel.isEmpty ? (
        <div className="ed-exp-empty ed-exp-mono">{viewModel.emptyMessage}</div>
      ) : (
        <>
          {viewModel.kpiTiles.length > 0 && (
            <div className={`ed-exp-grid ed-exp-cols-${viewModel.kpiCols}`}>
              {viewModel.kpiTiles.map((tile) => (
                <DetailsKpiTile key={tile.id} tile={tile} />
              ))}
            </div>
          )}
          {viewModel.breakdownCards.length > 0 && (
            <div
              className={`ed-exp-stack ${viewModel.kpiTiles.length === 0 ? 'ed-exp-stack-only' : ''}`}
            >
              {viewModel.breakdownCards.map((card) => (
                <DetailsBreakdownCard key={card.id} card={card} />
              ))}
            </div>
          )}
        </>
      )}

      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {viewModel.header.exportedAtGalactic}</span>
      </div>
    </div>
  )
})

function DetailsCardHeader({ header }: { header: TelemetryHeaderViewModel }) {
  const headerAccentClass =
    header.accent === 'amber' ? 'ed-exp-header' : `ed-exp-header ed-exp-header-${header.accent}`
  const durationLine = formatTelemetryHeaderDurationLine(header)
  return (
    <div className={headerAccentClass}>
      <div>
        <div className="ed-exp-eyebrow">
          {header.eyebrowLabel} // {header.scopeLabel}
        </div>
        <div className="ed-exp-title-row">
          <h2 className="ed-exp-title">{header.routeTitle}</h2>
          {header.modeBadge && (
            <span
              className="ed-exp-badge ed-exp-mono"
              style={{
                borderColor: header.modeBadge.safeColor,
                color: header.modeBadge.safeColor,
              }}
            >
              {header.modeBadge.modeName}
            </span>
          )}
          {header.inSystem && (
            <span className="ed-exp-badge ed-exp-badge-cyan ed-exp-mono">IN SYSTEM</span>
          )}
        </div>
      </div>
      <div className="ed-exp-dates ed-exp-mono">
        <div>
          {header.fromGalactic} → {header.toGalactic}
        </div>
        {durationLine && <div className="ed-exp-muted">{durationLine}</div>}
      </div>
    </div>
  )
}

function DetailsKpiTile({ tile }: { tile: DetailsKpiTileViewModel }) {
  const valueClass =
    tile.valueTone === 'main'
      ? 'ed-exp-kpi-value ed-exp-mono'
      : `ed-exp-kpi-value ed-exp-mono ed-exp-${tile.valueTone}`
  return (
    <div className="ed-exp-tile">
      <div className="ed-exp-kpi-label">{tile.label}</div>
      <div className={valueClass}>{tile.value}</div>
      {tile.subValue && <div className="ed-exp-kpi-sub ed-exp-mono">{tile.subValue}</div>}
    </div>
  )
}

function DetailsBreakdownCard({ card }: { card: DetailsBreakdownCardViewModel }) {
  return (
    <div className={`ed-exp-bd-card ed-exp-left-${card.borderAccent}`}>
      <div className="ed-exp-bd-head">
        <span>{card.title}</span>
        <span className={`ed-exp-mono ed-exp-${card.headerTone}`}>{card.headerValue}</span>
      </div>
      {card.rows.map((row) => (
        <div key={row.label} className="ed-exp-bd-row">
          <span className="ed-exp-muted">{row.label}</span>
          <span className={`ed-exp-mono ed-exp-${row.valueTone}`}>{row.value}</span>
        </div>
      ))}
    </div>
  )
}
