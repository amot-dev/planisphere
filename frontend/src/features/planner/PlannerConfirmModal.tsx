import { ModalShell } from '../../components/ui/ModalShell'

/**
 * PlannerConfirmState describes the active confirmation dialog in the expedition
 * flight planner for deleting a visited waypoint or an unvisited ARRIVED carrier
 * stop, moving an arrived carrier stop past a pending stop, or manually
 * overriding carrier arrival status.
 */
export type PlannerConfirmState =
  | {
      kind: 'delete_waypoint'
      rowId: number
      waypointNumber: number
      systemName: string
      visitedAtGalactic?: string
      isArrivedFcStop: boolean
    }
  | {
      kind: 'move_reset_fc'
      rowId: number
      fromIndex: number
      toIndex: number
      waypointNumber: number
      systemName: string
    }
  | {
      kind: 'override_fc_arrival'
      rowId: number
      waypointNumber: number
      systemName: string
      targetArrived: boolean
      cascadedCount: number
    }

/**
 * PlannerConfirmModalProps defines the active confirmation dialog state and
 * confirm or cancel callbacks for the expedition planner.
 */
export interface PlannerConfirmModalProps {
  confirmState: PlannerConfirmState
  onConfirm: () => void
  onCancel: () => void
}

/**
 * PlannerConfirmModal renders a cockpit confirmation modal for destructive
 * visited-waypoint or unvisited ARRIVED carrier-stop deletions and Fleet
 * Carrier arrival state overrides.
 */
export function PlannerConfirmModal({
  confirmState,
  onConfirm,
  onCancel,
}: PlannerConfirmModalProps) {
  const displaySystem = confirmState.systemName.trim() || 'Unnamed System'
  const subtitle = `Waypoint #${String(confirmState.waypointNumber).padStart(2, '0')} // ${displaySystem}`

  if (confirmState.kind === 'delete_waypoint') {
    const hasLoggedVisit = Boolean(confirmState.visitedAtGalactic)
    const modalTitle =
      hasLoggedVisit && confirmState.isArrivedFcStop
        ? 'Delete Visited Carrier Stop'
        : hasLoggedVisit
          ? 'Delete Visited Waypoint'
          : 'Delete Arrived Carrier Stop'

    return (
      <ModalShell title={modalTitle} subtitle={subtitle} onClose={onCancel} maxWidth="max-w-md">
        <div className="space-y-3 text-xs text-ed-text-body">
          {hasLoggedVisit && confirmState.isArrivedFcStop ? (
            <p>
              This waypoint has a logged visit at{' '}
              <span className="font-mono-tabular font-bold text-ed-amber">
                {confirmState.visitedAtGalactic}
              </span>{' '}
              and is marked as an{' '}
              <span className="font-mono-tabular font-bold text-ed-amber">ARRIVED</span> Fleet
              Carrier stop.
            </p>
          ) : hasLoggedVisit ? (
            <p>
              This waypoint has a logged visit at{' '}
              <span className="font-mono-tabular font-bold text-ed-amber">
                {confirmState.visitedAtGalactic}
              </span>
              .
            </p>
          ) : (
            <p>
              This unvisited waypoint is marked as an{' '}
              <span className="font-mono-tabular font-bold text-ed-amber">ARRIVED</span> Fleet
              Carrier stop.
            </p>
          )}
          <p className="text-ed-text-muted">
            {hasLoggedVisit && confirmState.isArrivedFcStop
              ? 'Deleting this waypoint removes the stop from your expedition plan and merges its leg telemetry into the adjacent leg. Journal events and logged carrier jump telemetry are never deleted.'
              : hasLoggedVisit
                ? 'Deleting this waypoint removes the stop from your expedition plan and merges its leg telemetry into the adjacent leg. Journal events in your archive are never deleted.'
                : 'Deleting this waypoint removes the carrier stop from your expedition plan. Logged carrier jump telemetry is never deleted.'}
          </p>
        </div>

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-ed-border">
          <button type="button" onClick={onCancel} className="ed-btn">
            CANCEL
          </button>
          <button type="button" onClick={onConfirm} className="ed-btn-danger">
            DELETE WAYPOINT
          </button>
        </div>
      </ModalShell>
    )
  }

  if (confirmState.kind === 'move_reset_fc') {
    return (
      <ModalShell
        title="Reset Fleet Carrier Arrival"
        subtitle={subtitle}
        onClose={onCancel}
        maxWidth="max-w-md"
      >
        <div className="space-y-3 text-xs text-ed-text-body">
          <p>
            Moving <span className="font-mono-tabular font-bold text-ed-text-main">{displaySystem}</span>{' '}
            to this position places it after a pending Fleet Carrier stop.
          </p>
          <p className="text-ed-text-muted">
            Continuing will reset this waypoint's Fleet Carrier status from{' '}
            <span className="font-mono-tabular font-bold text-ed-amber">ARRIVED</span> back to{' '}
            <span className="font-mono-tabular font-bold text-ed-text-muted">PENDING</span>.
          </p>
        </div>

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-ed-border">
          <button type="button" onClick={onCancel} className="ed-btn">
            CANCEL
          </button>
          <button type="button" onClick={onConfirm} className="ed-btn-primary">
            MOVE AND RESET TO PENDING
          </button>
        </div>
      </ModalShell>
    )
  }

  const nextStatusLabel = confirmState.targetArrived ? 'ARRIVED' : 'PENDING'
  const currentStatusLabel = confirmState.targetArrived ? 'PENDING' : 'ARRIVED'

  return (
    <ModalShell
      title="Override Fleet Carrier Arrival Status"
      subtitle={subtitle}
      onClose={onCancel}
      maxWidth="max-w-md"
    >
      <div className="space-y-3 text-xs text-ed-text-body">
        <div className="p-2.5 border border-ed-amber bg-ed-amber-tint text-ed-text-main space-y-1">
          <div className="text-[10px] font-extrabold uppercase tracking-wider text-ed-amber">
            Manual State Repair Override
          </div>
          <p className="text-[11px] text-ed-text-body">
            Fleet Carrier arrival is normally updated automatically when completing a carrier route.
            Use this manual override at your own risk only to repair out-of-sync stop status. It
            does not add or remove logged carrier jump telemetry.
          </p>
        </div>

        <p>
          Switch <span className="font-mono-tabular font-bold text-ed-text-main">{displaySystem}</span>{' '}
          from <span className="font-mono-tabular font-bold text-ed-text-muted">{currentStatusLabel}</span>{' '}
          to <span className="font-mono-tabular font-bold text-ed-amber">{nextStatusLabel}</span>?
        </p>

        {confirmState.cascadedCount > 0 && (
          <p className="text-[11px] text-ed-text-muted">
            {confirmState.targetArrived
              ? `${confirmState.cascadedCount} preceding pending carrier ${
                  confirmState.cascadedCount === 1 ? 'stop' : 'stops'
                } will also be marked ARRIVED to preserve route order.`
              : `${confirmState.cascadedCount} subsequent arrived carrier ${
                  confirmState.cascadedCount === 1 ? 'stop' : 'stops'
                } will also be reverted to PENDING to preserve route order.`}
          </p>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 pt-2 border-t border-ed-border">
        <button type="button" onClick={onCancel} className="ed-btn">
          CANCEL
        </button>
        <button type="button" onClick={onConfirm} className="ed-btn-primary">
          {confirmState.targetArrived ? 'MARK AS ARRIVED' : 'REVERT TO PENDING'}
        </button>
      </div>
    </ModalShell>
  )
}

