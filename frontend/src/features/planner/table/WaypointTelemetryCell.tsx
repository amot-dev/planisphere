import { expedition } from '../../../api/models'
import { formatLy } from '../../../domain/galacticTime'

/**
 * WaypointTelemetryCellProps defines visit state, waypoint geometry, and
 * current commander location for the waypoint telemetry / timestamp column.
 */
export interface WaypointTelemetryCellProps {
  rowView: expedition.RowView
  isVisited: boolean
  isNext: boolean
  playerSystem?: string
}

function UnknownDistanceSpan({
  systemName,
  unknownTitle,
}: {
  systemName: string | undefined
  unknownTitle: string
}) {
  const hasSystemName = Boolean(systemName?.trim())
  return (
    <span
      className="text-ed-text-dim uppercase text-[10px]"
      title={hasSystemName ? unknownTitle : undefined}
    >
      {hasSystemName ? 'UNKNOWN' : '—'}
    </span>
  )
}

function renderDistanceValue(
  isNext: boolean,
  rowView: expedition.RowView,
  row: expedition.Row,
  playerSystem?: string,
) {
  if (isNext) {
    if (rowView.distFromCurrentLy != null) {
      const tooltip = playerSystem
        ? `Estimated straight-line distance from player location (${playerSystem})`
        : 'Estimated straight-line distance from player location'
      return (
        <span className="text-ed-cyan" title={tooltip}>
          +{formatLy(rowView.distFromCurrentLy, 1)}
        </span>
      )
    }
    return (
      <UnknownDistanceSpan
        systemName={row.systemName}
        unknownTitle="Coordinates unknown for this waypoint or player location"
      />
    )
  }

  if (rowView.number === 1) {
    if (row.systemName?.trim()) {
      return (
        <span className="text-ed-cyan" title="Starting Waypoint (Origin)">
          +0 Ly
        </span>
      )
    }
    return <span className="text-ed-text-dim">—</span>
  }

  if (rowView.distFromPrevLy != null) {
    return (
      <span
        className="text-ed-cyan"
        title="Estimated straight-line distance from previous waypoint"
      >
        +{formatLy(rowView.distFromPrevLy, 1)}
      </span>
    )
  }

  return (
    <UnknownDistanceSpan
      systemName={row.systemName}
      unknownTitle="Coordinates unknown for this waypoint or preceding waypoint"
    />
  )
}

/**
 * WaypointTelemetryCell renders either the logged galactic visit timestamp for
 * visited waypoints or straight-line distance estimates for upcoming waypoints.
 */
export function WaypointTelemetryCell({
  rowView,
  isVisited,
  isNext,
  playerSystem,
}: WaypointTelemetryCellProps) {
  const row = rowView.row

  return (
    <td className="px-3 py-1.5 align-top border-b border-ed-border-subtle whitespace-nowrap">
      {isVisited ? (
        <div className="h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5">
          <span className="font-extrabold uppercase tracking-wider text-ed-amber shrink-0">
            LOGGED VISIT
          </span>
          <span className="text-ed-text-muted shrink-0">{rowView.visitedAtGalactic}</span>
        </div>
      ) : (
        <div className="h-7 flex items-center whitespace-nowrap font-mono-tabular text-[11px] text-ed-text-muted leading-5">
          {renderDistanceValue(isNext, rowView, row, playerSystem)}
        </div>
      )}
    </td>
  )
}

