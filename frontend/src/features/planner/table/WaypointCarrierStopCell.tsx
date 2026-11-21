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
    <td className="px-2 py-1.5 text-center align-top border-b border-[#24180e]">
      <div className="h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap">
        {row.fcStop ? (
          <>
            {isPlanning ? (
              <span
                title="Designated Fleet Carrier Stop for this expedition"
                className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#120d09] text-[#a6907c] border border-[#3d2816] select-none"
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
                    ? 'bg-[#211307] text-[#FF7100] border-[#FF7100]/70 hover:border-[#FF7100]'
                    : 'bg-[#120d09] text-[#a6907c] border-[#3d2816] hover:border-[#FF7100] hover:text-[#F4ECE4]'
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
                    ? 'bg-[#211307] text-[#FF7100] border-[#FF7100]/70'
                    : 'bg-[#120d09] text-[#a6907c] border-[#3d2816]'
                }`}
              >
                {row.fcArrived ? 'ARRIVED' : 'PENDING'}
              </span>
            )}

            {!isPlanning && hasActiveCarrierRoute ? (
              <span
                title="Fleet Carrier route is currently active for this waypoint in the bottom router."
                className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]"
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
                  className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#00D2FF] cursor-pointer"
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
                className="h-5 w-4 inline-flex items-center justify-center text-xs text-[#6e5b4b] hover:text-[#ff3b2f] cursor-pointer"
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
            className="h-5 px-2 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider text-[#6e5b4b] hover:text-[#FF7100] border border-dashed border-[#2e1f12] hover:border-[#FF7100] bg-transparent hover:bg-[#16100b] cursor-pointer"
          >
            + FC STOP
          </button>
        ) : (
          <span className="h-5 inline-flex items-center justify-center text-[#6e5b4b] font-mono-tabular text-[10px]">
            —
          </span>
        )}
      </div>
    </td>
  )
}

