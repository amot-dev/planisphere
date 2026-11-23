import { expedition } from '../../../api/models'
import { formatLy } from '../../../domain/galacticTime'
import { ExpeditionExportData, WaypointsBlockConfig } from '../exportTypes'

/** Props for the self-contained Waypoints route manifest card component. */
export interface WaypointsBlockProps {
  exportData: ExpeditionExportData
  config: WaypointsBlockConfig
}

/**
 * WaypointsBlock renders the compact tabular route log mirroring Planisphere's
 * planner table with origin DEPARTED timestamps and IN SYSTEM / IN PROGRESS states.
 */
export function WaypointsBlock({ exportData, config }: WaypointsBlockProps) {
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

  const columnCount =
    2 +
    (columns.has('timestamps') ? 1 : 0) +
    (columns.has('legDistance') ? 1 : 0) +
    (columns.has('duration') ? 1 : 0) +
    (columns.has('shipsFlown') ? 1 : 0) +
    (columns.has('flightMode') ? 1 : 0) +
    (columns.has('fcStops') ? 1 : 0) +
    (columns.has('gecBadges') ? 1 : 0) +
    (columns.has('notes') ? 1 : 0)

  return (
    <div className="ed-exp-root">
      <div className="ed-exp-header">
        <div>
          <div className="ed-exp-eyebrow">
            WAYPOINTS LOG // {config.waypointsScope === 'fullPlan' ? 'FULL PLAN' : 'VISITED ONLY'}
          </div>
          <div className="ed-exp-title-row">
            <h2 className="ed-exp-title">{exportData.plan.expedition.name}</h2>
          </div>
        </div>
        <div className="ed-exp-dates ed-exp-mono">
          <div>
            {exportData.plan.visitedWaypointCount} / {exportData.plan.waypointCount} VISITED
          </div>
          <div className="ed-exp-muted">COVERED {formatLy(exportData.plan.coveredLy, 1)}</div>
        </div>
      </div>

      <table className="ed-exp-table">
        <thead>
          <tr>
            <th>#</th>
            <th>System</th>
            {columns.has('timestamps') && <th>Galactic Time</th>}
            {columns.has('legDistance') && <th>Leg Ly</th>}
            {columns.has('duration') && <th>Duration</th>}
            {columns.has('shipsFlown') && <th>Ships</th>}
            {columns.has('flightMode') && <th>Mode</th>}
            {columns.has('fcStops') && <th>FC</th>}
            {columns.has('gecBadges') && <th>GEC</th>}
            {columns.has('notes') && <th>Notes</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((rowView) => {
            if (rowView.row.rowType === 'region') {
              return (
                <tr key={rowView.row.id} className="ed-exp-region-row">
                  <td colSpan={columnCount}>{rowView.regionName || 'Galactic Region'}</td>
                </tr>
              )
            }
            const detail = detailsByRowId.get(rowView.row.id)
            const borderColor =
              rowView.number === 1 ? '#3d2816' : rowView.legModeColor || '#FF7100'
            return (
              <tr key={rowView.row.id} style={{ borderLeft: `3px solid ${borderColor}` }}>
                <td className="ed-exp-mono">{rowView.number}</td>
                <td>
                  <strong>{rowView.row.systemName}</strong>
                  {rowView.row.poiName && (
                    <div className="ed-exp-muted" style={{ fontSize: '11px' }}>
                      {rowView.row.poiName}
                    </div>
                  )}
                </td>
                {columns.has('timestamps') && (
                  <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
                    {formatWaypointTimestampCell(rowView, detail)}
                  </td>
                )}
                {columns.has('legDistance') && (
                  <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
                    {rowView.distFromPrevLy != null ? formatLy(rowView.distFromPrevLy, 1) : '—'}
                  </td>
                )}
                {columns.has('duration') && (
                  <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
                    {detail?.elapsed || '—'}
                  </td>
                )}
                {columns.has('shipsFlown') && (
                  <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
                    {(detail?.shipsUsed ?? []).join(', ') || '—'}
                  </td>
                )}
                {columns.has('flightMode') && (
                  <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
                    {rowView.number > 1 && rowView.legModeName ? (
                      <span style={{ color: rowView.legModeColor || '#FF7100' }}>
                        {rowView.legModeName}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                )}
                {columns.has('fcStops') && (
                  <td className="ed-exp-mono" style={{ fontSize: '11px' }}>
                    {rowView.row.fcStop ? (rowView.row.fcArrived ? 'ARRIVED' : 'STOP') : '—'}
                  </td>
                )}
                {columns.has('gecBadges') && (
                  <td style={{ fontSize: '11px' }}>{rowView.gec ? rowView.gec.name : '—'}</td>
                )}
                {columns.has('notes') && (
                  <td className="ed-exp-muted" style={{ fontSize: '11px' }}>
                    {rowView.row.notes || ''}
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>

      <div className="ed-exp-footer ed-exp-mono">
        <span>PLANISPHERE // ASTROMETRIC TELEMETRY TERMINAL</span>
        <span>EXPORTED {exportData.exportedAtGalactic}</span>
      </div>
    </div>
  )
}

function formatWaypointTimestampCell(
  rowView: expedition.RowView,
  detail: expedition.WaypointDetails | undefined,
): string {
  if (rowView.number === 1) {
    return rowView.departedAtGalactic
      ? `DEPARTED ${rowView.departedAtGalactic}`
      : 'AWAITING DEPARTURE'
  }
  if (rowView.row.visitedAt) {
    return detail?.inSystem
      ? `${rowView.visitedAtGalactic} (IN SYSTEM)`
      : rowView.visitedAtGalactic
  }
  if (rowView.visitState === 'current') {
    return 'IN PROGRESS'
  }
  return ''
}

