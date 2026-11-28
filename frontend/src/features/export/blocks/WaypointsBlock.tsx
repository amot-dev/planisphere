import { memo } from 'react'
import {
  ExpeditionExportData,
  WaypointsBlockConfig,
  WaypointsColumnId,
} from '../exportTypes'
import { buildWaypointsViewModel, WaypointTableRowViewModel } from '../exportViewModels'

/** Props for the self-contained Waypoints route manifest card component. */
export interface WaypointsBlockProps {
  exportData: ExpeditionExportData
  config: WaypointsBlockConfig
}

interface WaypointsTableRowProps {
  row: WaypointTableRowViewModel
  selectedColumns: ReadonlySet<WaypointsColumnId>
  columnCount: number
}

/**
 * WaypointsBlock renders the compact tabular route log mirroring Planisphere's
 * planner table with origin DEPARTED timestamps and IN SYSTEM / IN PROGRESS states.
 */
export const WaypointsBlock = memo(function WaypointsBlock({
  exportData,
  config,
}: WaypointsBlockProps) {
  const viewModel = buildWaypointsViewModel(exportData, config)

  return (
    <div className="ed-exp-root">
      <div className="ed-exp-header">
        <div>
          <div className="ed-exp-eyebrow">WAYPOINTS LOG // {viewModel.scopeUpperLabel}</div>
          <div className="ed-exp-title-row">
            <h2 className="ed-exp-title">{viewModel.expeditionName}</h2>
          </div>
        </div>
        <div className="ed-exp-dates ed-exp-mono">
          <div>{viewModel.visitedStatusLabel}</div>
          <div className="ed-exp-muted">{viewModel.coveredDistanceLabel}</div>
        </div>
      </div>
      <table className="ed-exp-table">
        <thead>
          <tr>
            {viewModel.columnHeaders.map((headerLabel) => (
              <th key={headerLabel}>{headerLabel}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {viewModel.rows.map((row) => (
            <WaypointsTableRow
              key={row.rowId}
              row={row}
              selectedColumns={viewModel.selectedColumns}
              columnCount={viewModel.columnCount}
            />
          ))}
        </tbody>
      </table>
      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {viewModel.exportedAtGalactic}</span>
      </div>
    </div>
  )
})

function WaypointsTableRow({ row, selectedColumns, columnCount }: WaypointsTableRowProps) {
  if (row.kind === 'region') {
    return (
      <tr className="ed-exp-region-row">
        <td colSpan={columnCount}>{row.regionTitle}</td>
      </tr>
    )
  }

  return (
    <tr style={{ borderLeft: `3px solid ${row.borderColor}` }}>
      <td className="ed-exp-mono">{row.number}</td>
      <td>
        <strong>{row.systemName}</strong>
        {row.poiName && <div className="ed-exp-muted" style={{ fontSize: '11px' }}>{row.poiName}</div>}
      </td>
      {selectedColumns.has('timestamps') && <td className="ed-exp-mono" style={{ fontSize: '11px' }}>{row.timestampText}</td>}
      {selectedColumns.has('legDistance') && <td className="ed-exp-mono" style={{ fontSize: '11px' }}>{row.legDistanceText}</td>}
      {selectedColumns.has('duration') && <td className="ed-exp-mono" style={{ fontSize: '11px' }}>{row.durationText}</td>}
      {selectedColumns.has('inGameDuration') && <td className="ed-exp-mono" style={{ fontSize: '11px' }}>{row.inGameDurationText}</td>}
      {selectedColumns.has('shipsFlown') && <td className="ed-exp-mono" style={{ fontSize: '11px' }}>{row.shipsText}</td>}
      {selectedColumns.has('flightMode') && (
        <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
          {row.modeBadge ? <span style={{ color: row.modeBadge.safeColor }}>{row.modeBadge.name}</span> : '—'}
        </td>
      )}
      {selectedColumns.has('fcStops') && <td className="ed-exp-mono" style={{ fontSize: '11px' }}>{row.fcStatusText}</td>}
      {selectedColumns.has('gecBadges') && <td style={{ fontSize: '11px' }}>{row.gecText}</td>}
      {selectedColumns.has('notes') && <td className="ed-exp-muted" style={{ fontSize: '11px' }}>{row.notesText}</td>}
    </tr>
  )
}
