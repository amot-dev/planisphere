import { CompleteExpedition, ReopenExpedition } from '../../api/backend'
import { expedition } from '../../api/models'
import { formatLy } from '../../domain/galacticTime'
import { StatusBadge } from './table/widgets/StatusBadge'

/**
 * PlannerHeaderBarProps defines the active expedition plan, carrier plotter
 * open state, and lifecycle action callbacks for the top bar of the planner.
 */
export interface PlannerHeaderBarProps {
  plan: expedition.PlanView
  hasOriginWaypointSystem: boolean
  carrierRouteOpen?: boolean
  onOpenStartModal: () => void
  onLifecycleAction: (label: string, actionFunction: () => Promise<void>) => Promise<void>
}

/**
 * PlannerHeaderBar renders the expedition status badge, title, waypoint progress,
 * planned/covered/remaining distance strip, and Engage / Complete / Reopen buttons.
 */
export function PlannerHeaderBar({
  plan,
  hasOriginWaypointSystem,
  carrierRouteOpen = false,
  onOpenStartModal,
  onLifecycleAction,
}: PlannerHeaderBarProps) {
  const currentExpedition = plan.expedition
  const isPlanning = currentExpedition.status === 'planning'
  const isActive = currentExpedition.status === 'active'
  const isCompleted = currentExpedition.status === 'completed'
  const canComplete = (plan.visitedWaypointCount ?? 0) >= 2
  const completeDisabled = !canComplete || Boolean(carrierRouteOpen)

  const coveredLy =
    plan.coveredLy ??
    plan.rows?.reduce((sum, rowView) => {
      if (rowView.row?.rowType === 'waypoint' && rowView.row?.visitedAt && rowView.distFromPrevLy) {
        return sum + rowView.distFromPrevLy
      }
      return sum
    }, 0) ??
    0
  const completedLy = coveredLy > 0 ? coveredLy : plan.plannedLy

  return (
    <div className="px-5 py-3 bg-ed-card-alt border-b border-ed-border flex flex-wrap items-center justify-between gap-4">
      <div className="flex flex-wrap items-center gap-3.5">
        <StatusBadge status={currentExpedition.status} />
        <div className="border-l-2 border-ed-amber/60 pl-3">
          <div className="flex items-center gap-2.5">
            <h2 className="text-base font-extrabold uppercase tracking-[0.1em] text-ed-text-main">
              {currentExpedition.name}
            </h2>
            <span className="text-[11px] font-mono-tabular px-2 py-0.5 bg-ed-control border border-ed-border text-ed-amber">
              {plan.visitedWaypointCount} / {plan.waypointCount} WAYPOINTS VISITED
            </span>
          </div>
          {currentExpedition.description && (
            <p className="text-xs text-ed-text-muted mt-0.5">{currentExpedition.description}</p>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        {/* Route distance readout */}
        <div className="hidden xl:flex items-center gap-3 px-3 py-1.5 border border-ed-border border-l-2 border-l-ed-amber bg-ed-rail text-xs">
          {isCompleted ? (
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mr-1.5">
                COMPLETED ROUTE:
              </span>
              <span className="font-mono-tabular font-bold text-ed-text-main">
                {formatLy(completedLy, 1)}
              </span>
            </div>
          ) : isActive ? (
            <>
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mr-1.5">
                  COVERED:
                </span>
                <span className="font-mono-tabular font-bold text-ed-text-main">
                  {formatLy(coveredLy, 1)}
                </span>
              </div>
              <span className="text-ed-border">|</span>
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mr-1.5">
                  REMAINING:
                </span>
                <span className="font-mono-tabular font-bold text-ed-text-main">
                  {formatLy(plan.remainingLy, 1)}
                </span>
              </div>
            </>
          ) : (
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-ed-text-muted mr-1.5">
                PLANNED ROUTE:
              </span>
              <span className="font-mono-tabular font-bold text-ed-text-main">
                {formatLy(plan.plannedLy, 1)}
              </span>
            </div>
          )}
        </div>

        {isPlanning && (
          <button
            type="button"
            disabled={!hasOriginWaypointSystem}
            onClick={onOpenStartModal}
            title={
              hasOriginWaypointSystem
                ? 'Engage expedition (lock arrival at Waypoint #1)'
                : 'Enter a star system for Waypoint #1 before engaging the expedition'
            }
            className={`ed-btn-primary ${
              !hasOriginWaypointSystem ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
            }`}
          >
            <span>ENGAGE</span>
          </button>
        )}

        {isActive && (
          <div className="inline-flex items-center gap-1.5">
            <button
              type="button"
              disabled={completeDisabled}
              onClick={() =>
                onLifecycleAction('COMPLETING EXPEDITION', () =>
                  CompleteExpedition(currentExpedition.id),
                )
              }
              title={
                !canComplete
                  ? 'Waypoint #2 not visited yet — complete requires at least 2 visited waypoints (or delete expedition in Expeditions)'
                  : carrierRouteOpen
                    ? 'Close or finish the active Fleet Carrier route before completing the expedition'
                    : undefined
              }
              className={`ed-btn-primary ${
                completeDisabled ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
              }`}
            >
              COMPLETE
            </button>
          </div>
        )}

        {isCompleted && (
          <div className="inline-flex items-center gap-1.5">
            <button
              type="button"
              onClick={() =>
                onLifecycleAction('REOPENING EXPEDITION', () =>
                  ReopenExpedition(currentExpedition.id),
                )
              }
              className="ed-btn-cyan"
            >
              REOPEN
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

