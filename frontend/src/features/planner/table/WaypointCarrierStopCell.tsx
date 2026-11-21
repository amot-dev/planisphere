import { expedition } from '../../../api/models'

/**
 * WaypointCarrierStopCellProps defines Fleet Carrier stop designation state,
 * override permissions, and routing callbacks for a single waypoint table cell.
 */
export interface WaypointCarrierStopCellProps {
  rowView: expedition.RowView
  index: number
  draftSystemName: string
  isPlanning: boolean
  isNextFcStop: boolean
  isFcStopLocked: boolean
  hasActiveCarrierRoute: boolean
  canAddFcStop: boolean
  canRemoveFcStop: boolean
  canOverrideFcBadge: boolean
  onToggleFcStop: (row: expedition.Row) => void
  onOverrideFcArrived?: (rowView: expedition.RowView) => void
  onOpenCarrierRoutePrompt: (index: number, row: expedition.Row) => void
}

/**
 * WaypointCarrierStopCell renders Fleet Carrier stop designation badges, arrival
 * state override controls, and Spansh ROUTE / ROUTING action buttons.
 */
export function WaypointCarrierStopCell({
  rowView,
  index,
  draftSystemName,
  isPlanning,
  isNextFcStop,
  isFcStopLocked,
  hasActiveCarrierRoute,
  canAddFcStop,
  canRemoveFcStop,
  canOverrideFcBadge,
  onToggleFcStop,
  onOverrideFcArrived,
  onOpenCarrierRoutePrompt,
}: WaypointCarrierStopCellProps) {
  const row = rowView.row

  return (
    <td className="px-2 py-1.5 text-center align-top border-b border-ed-border-subtle">
      <div className="h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap">
        {row.fcStop ? (
          <>
            {isPlanning ? (
              <span
                title="Designated Fleet Carrier Stop for this expedition"
                className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-ed-surface text-ed-text-muted border border-ed-border select-none"
              >
                FC STOP
              </span>
            ) : canOverrideFcBadge ? (
              <button
                type="button"
                onClick={() => onOverrideFcArrived?.(rowView)}
                title={
                  row.fcArrived
                    ? 'Carrier has arrived at this waypoint (click to override)'
                    : 'Carrier arrival pending (click to override)'
                }
                className={`h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider border cursor-pointer transition-colors ${
                  row.fcArrived
                    ? 'bg-ed-amber-tint text-ed-amber border-ed-amber/70 hover:border-ed-amber'
                    : 'bg-ed-surface text-ed-text-muted border-ed-border hover:border-ed-amber hover:text-ed-text-main'
                }`}
              >
                {row.fcArrived ? 'ARRIVED' : 'PENDING'}
              </button>
            ) : (
              <span
                title={
                  row.fcArrived
                    ? 'Carrier has arrived at this waypoint'
                    : 'Carrier arrival pending'
                }
                className={`h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider border select-none ${
                  row.fcArrived
                    ? 'bg-ed-amber-tint text-ed-amber border-ed-amber/70'
                    : 'bg-ed-surface text-ed-text-muted border-ed-border'
                }`}
              >
                {row.fcArrived ? 'ARRIVED' : 'PENDING'}
              </span>
            )}

            {!isPlanning && hasActiveCarrierRoute ? (
              <span
                title="Fleet Carrier route is currently active for this waypoint in the bottom router."
                className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-ed-cyan-tint text-ed-cyan border border-ed-cyan"
              >
                ROUTING
              </span>
            ) : (
              !isPlanning &&
              isNextFcStop &&
              !isFcStopLocked && (
                <button
                  type="button"
                  onClick={() =>
                    onOpenCarrierRoutePrompt(
                      index,
                      expedition.Row.createFrom({ ...row, systemName: draftSystemName }),
                    )
                  }
                  title="Plot Fleet Carrier route to this next carrier stop"
                  className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-ed-cyan-tint hover:bg-ed-cyan text-ed-cyan hover:text-ed-void border border-ed-cyan cursor-pointer"
                >
                  ROUTE
                </button>
              )
            )}

            {canRemoveFcStop && (
              <button
                type="button"
                onClick={() => onToggleFcStop(row)}
                title="Remove Fleet Carrier stop designation"
                className="h-5 w-4 inline-flex items-center justify-center text-xs text-ed-text-dim hover:text-ed-danger cursor-pointer"
              >
                ×
              </button>
            )}
          </>
        ) : canAddFcStop ? (
          <button
            type="button"
            onClick={() => onToggleFcStop(row)}
            title="Designate this waypoint as a Fleet Carrier stop"
            className="h-5 px-2 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider text-ed-text-dim hover:text-ed-amber border border-dashed border-ed-border-dim hover:border-ed-amber bg-transparent hover:bg-ed-control cursor-pointer"
          >
            + FC STOP
          </button>
        ) : (
          <span className="h-5 inline-flex items-center justify-center text-ed-text-dim font-mono-tabular text-[10px]">
            —
          </span>
        )}
      </div>
    </td>
  )
}

