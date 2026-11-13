import { CompleteExpedition, ReopenExpedition } from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'
import { formatLy } from '../../utils/galacticTime'
import { StatusBadge } from './widgets/StatusBadge'

/**
 * PlannerHeaderBarProps defines the active expedition plan, route distance
 * summaries, and lifecycle action callbacks for the top bar of the planner.
 */
export interface PlannerHeaderBarProps {
  plan: expedition.PlanView
  hasOriginWaypointSystem: boolean
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
  onOpenStartModal,
  onLifecycleAction,
}: PlannerHeaderBarProps) {
  const currentExpedition = plan.expedition
  const isPlanning = currentExpedition.status === 'planning'
  const isActive = currentExpedition.status === 'active'
  const isCompleted = currentExpedition.status === 'completed'
  const canComplete = (plan.visitedCount ?? 0) >= 2

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
    <div className="px-5 py-3 bg-[#0e0a07] border-b border-[#3d2816] flex flex-wrap items-center justify-between gap-4">
      <div className="flex flex-wrap items-center gap-3.5">
        <StatusBadge status={currentExpedition.status} armedAt={currentExpedition.armedAt} />
        <div className="border-l-2 border-[#FF7100]/60 pl-3">
          <div className="flex items-center gap-2.5">
            <h2 className="text-base font-extrabold uppercase tracking-[0.1em] text-[#F4ECE4]">
              {currentExpedition.name}
            </h2>
            <span className="text-[11px] font-mono-tabular px-2 py-0.5 bg-[#16100b] border border-[#3d2816] text-[#FF7100]">
              {plan.visitedCount} / {plan.waypointCount} WAYPOINTS VISITED
            </span>
          </div>
          {currentExpedition.description && (
            <p className="text-xs text-[#a6907c] mt-0.5">{currentExpedition.description}</p>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        {/* Route distance readout */}
        <div className="hidden xl:flex items-center gap-3 px-3 py-1.5 border border-[#3d2816] border-l-2 border-l-[#FF7100] bg-[#090705] text-xs">
          {isCompleted ? (
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                COMPLETED ROUTE:
              </span>
              <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                {formatLy(completedLy, 1)}
              </span>
            </div>
          ) : isActive ? (
            <>
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                  COVERED:
                </span>
                <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                  {formatLy(coveredLy, 1)}
                </span>
              </div>
              <span className="text-[#3d2816]">|</span>
              <div>
                <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                  REMAINING:
                </span>
                <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                  {formatLy(plan.remainingLy, 1)}
                </span>
              </div>
            </>
          ) : (
            <div>
              <span className="text-[10px] font-bold uppercase tracking-wider text-[#a6907c] mr-1.5">
                PLANNED ROUTE:
              </span>
              <span className="font-mono-tabular font-bold text-[#F4ECE4]">
                {formatLy(plan.plannedLy, 1)}
              </span>
            </div>
          )}
        </div>

        {/* Lifecycle action buttons (Start Time and Reset to Planning removed; use Clone in Plans menu) */}
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
            <span>ENGAGE EXPEDITION</span>
          </button>
        )}

        {isActive && (
          <div className="inline-flex items-center gap-1.5">
            <button
              type="button"
              disabled={!canComplete}
              onClick={() =>
                onLifecycleAction('COMPLETING EXPEDITION', () =>
                  CompleteExpedition(currentExpedition.id),
                )
              }
              title={
                canComplete
                  ? 'Complete this expedition and finalize flight logs'
                  : 'Waypoint #2 not visited yet — complete requires at least 2 visited waypoints (or delete flight plan in Plans)'
              }
              className={`ed-btn-primary ${
                !canComplete ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
              }`}
            >
              Complete
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
              Reopen Expedition
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

