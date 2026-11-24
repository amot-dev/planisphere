import { memo } from 'react'
import { ExpeditionExportData, SummaryBlockConfig } from '../exportTypes'
import {
  buildSummaryViewModel,
  SummaryKpiTileViewModel,
  SummaryRouteSegment,
  TelemetryHeaderViewModel,
} from '../exportViewModels'

/** Props for the self-contained Summary export card component. */
export interface SummaryBlockProps {
  exportData: ExpeditionExportData
  config: SummaryBlockConfig
}

/**
 * SummaryBlock renders the lean fixed-layout expedition overview card with
 * optional inline Career Diff delta pills when viewing Full Expedition Combined.
 */
export const SummaryBlock = memo(function SummaryBlock({ exportData, config }: SummaryBlockProps) {
  const viewModel = buildSummaryViewModel(exportData, config)
  const accentStyle = viewModel.header.modeBadge
    ? { borderLeftColor: viewModel.header.modeBadge.safeColor, borderLeftWidth: '3px' }
    : undefined

  return (
    <div className="ed-exp-root" style={accentStyle}>
      <SummaryCardHeader header={viewModel.header} />
      <SummaryRouteStrip segments={viewModel.routeSegments} />
      <div className="ed-exp-grid ed-exp-grid-top ed-exp-cols-4">
        {viewModel.topRowTiles.map((tile) => (
          <SummaryKpiTile key={tile.id} tile={tile} />
        ))}
      </div>
      <div className={`ed-exp-grid ed-exp-grid-bottom ed-exp-cols-${viewModel.bottomRowCols}`}>
        {viewModel.bottomRowTiles.map((tile) => (
          <SummaryKpiTile key={tile.id} tile={tile} />
        ))}
      </div>
      {viewModel.netProfit && (
        <div className="ed-exp-strip ed-exp-strip-bottom">
          <span>
            NET PROFIT:{' '}
            <strong className={`ed-exp-mono ${viewModel.netProfit.isNegative ? 'ed-exp-danger' : 'ed-exp-cyan'}`}>
              {viewModel.netProfit.text}
            </strong>
          </span>
        </div>
      )}
      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {viewModel.header.exportedAtGalactic}</span>
      </div>
    </div>
  )
})

function SummaryCardHeader({ header }: { header: TelemetryHeaderViewModel }) {
  return (
    <div className="ed-exp-header">
      <div>
        <div className="ed-exp-eyebrow">
          {header.eyebrowLabel} // {header.scopeLabel}
        </div>
        <div className="ed-exp-title-row">
          <h2 className="ed-exp-title">{header.routeTitle}</h2>
          {header.modeBadge && (
            <span
              className="ed-exp-badge ed-exp-mono"
              style={{ borderColor: header.modeBadge.safeColor, color: header.modeBadge.safeColor }}
            >
              {header.modeBadge.modeName}
            </span>
          )}
          {header.inSystem && <span className="ed-exp-badge ed-exp-badge-cyan ed-exp-mono">IN SYSTEM</span>}
        </div>
      </div>
      <div className="ed-exp-dates ed-exp-mono">
        <div>
          {header.fromGalactic} → {header.toGalactic}
        </div>
        {header.durationLabel && <div className="ed-exp-muted">ELAPSED: {header.durationLabel}</div>}
      </div>
    </div>
  )
}

function SummaryRouteStrip({ segments }: { segments: SummaryRouteSegment[] }) {
  if (segments.length === 0) return null
  return (
    <div className="ed-exp-strip">
      {segments.map((segment, index) => (
        <span key={segment.key}>
          {index > 0 && <span className="ed-exp-sep"> | </span>}
          {segment.label}: <strong className="ed-exp-mono ed-exp-main">{segment.value}</strong>
        </span>
      ))}
    </div>
  )
}

function SummaryKpiTile({ tile }: { tile: SummaryKpiTileViewModel }) {
  const valueToneClass = tile.valueTone === 'main' ? '' : `ed-exp-${tile.valueTone} `
  return (
    <div className={`ed-exp-tile ed-exp-left-${tile.borderAccent}`}>
      <div className="ed-exp-kpi-label">{tile.label}</div>
      <div className={`ed-exp-kpi-value ${valueToneClass}ed-exp-mono`}>{tile.value}</div>
      <div className="ed-exp-kpi-sub ed-exp-mono">{tile.subValue}</div>
      {tile.careerDiffText && <div className="ed-exp-diff ed-exp-mono">{tile.careerDiffText}</div>}
    </div>
  )
}
