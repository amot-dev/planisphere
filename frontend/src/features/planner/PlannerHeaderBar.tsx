import { CompleteExpedition, ReopenExpedition } from '../../api/backend'
import { expedition } from '../../api/models'
import { formatLy } from '../../domain/galacticTime'
import { LegModeDropdown } from './table/widgets/LegModeDropdown'

/**
 * PlannerHeaderBarProps defines the active expedition plan, carrier plotter open state,
 * `exportEnabled` (whether export is enabled after departing Waypoint #1), flight mode handler,
 * optional `onOpenExportModal` callback, and lifecycle action callbacks for the top bar of the planner.
 */
export interface PlannerHeaderBarProps {
  plan: expedition.PlanView
  hasOriginWaypointSystem: boolean
  carrierRouteOpen?: boolean
  exportEnabled?: boolean
  onChangeActiveMode?: (modeId: number) => void
  onOpenStartModal: () => void
  onOpenExportModal?: () => void
  onLifecycleAction: (label: string, actionFunction: () => Promise<void>) => Promise<void>
}

/**
 * PlannerHeaderBar renders the expedition title, status-styled waypoint count badge,
 * per-expedition flight mode selector (hidden when completed), planned/covered/remaining
 * distance strip, optional EXPORT button (opens Export Studio when engaged and `exportEnabled`),
 * and Engage / Complete / Reopen buttons.
 */
export function PlannerHeaderBar({
  plan,
  hasOriginWaypointSystem,
  carrierRouteOpen = false,
  exportEnabled = false,
  onChangeActiveMode,
  onOpenStartModal,
  onOpenExportModal,
  onLifecycleAction,
}: PlannerHeaderBarProps) {
  const currentExpedition = plan.expedition
  const isPlanning = currentExpedition.status === 'planning'
  const isActive = currentExpedition.status === 'active'
  const isCompleted = currentExpedition.status === 'completed'
  const canComplete = (plan.visitedWaypointCount ?? 0) >= 2
  const completeDisabled = !canComplete || Boolean(carrierRouteOpen)

  const modesList = plan.modes ?? []
  const travelMode = modesList.find((modeItem) => modeItem.name.toLowerCase() === 'travel')
  const activeModeId = currentExpedition.activeModeId || travelMode?.id || modesList[0]?.id || 0
  const activeMode = modesList.find((modeItem) => modeItem.id === activeModeId) ?? modesList[0]

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

  const waypointCount = plan.waypointCount ?? 0
  const visitedWaypointCount = plan.visitedWaypointCount ?? 0

  return (
    <div className="px-5 py-3 bg-ed-card-alt border-b border-ed-border flex flex-wrap items-center justify-between gap-4">
      <div className="flex flex-wrap items-center gap-3.5">
        <div className="border-l-2 border-ed-amber/60 pl-3">
          <div className="flex items-center gap-2.5">
            <h2 className="text-base font-extrabold uppercase tracking-[0.1em] text-ed-text-main">
              {currentExpedition.name}
            </h2>
            {isActive ? (
              <span
                title="Active expedition"
                className="inline-flex items-center gap-1.5 text-[11px] font-mono-tabular font-extrabold uppercase tracking-wider px-2 py-0.5 bg-ed-amber-tint border border-ed-amber border-l-3 text-ed-amber"
              >
                <span className="w-1.5 h-1.5 bg-ed-amber animate-pulse" />
                {`${visitedWaypointCount} / ${waypointCount} WAYPOINTS VISITED`}
              </span>
            ) : isCompleted ? (
              <span
                title="Completed expedition"
                className="inline-flex items-center text-[11px] font-mono-tabular font-extrabold uppercase tracking-wider px-2 py-0.5 bg-ed-cyan-tint border border-ed-cyan border-l-3 text-ed-cyan"
              >
                {`${visitedWaypointCount} ${visitedWaypointCount === 1 ? 'WAYPOINT' : 'WAYPOINTS'} VISITED`}
              </span>
            ) : (
              <span
                title="Planning phase"
                className="inline-flex items-center text-[11px] font-mono-tabular font-extrabold uppercase tracking-wider px-2 py-0.5 bg-ed-control border border-ed-border-strong border-l-3 border-l-ed-cyan text-ed-text-main"
              >
                {`${waypointCount} ${waypointCount === 1 ? 'WAYPOINT' : 'WAYPOINTS'} PLANNED`}
              </span>
            )}
          </div>
          {currentExpedition.description && (
            <p className="text-xs text-ed-text-muted mt-0.5">{currentExpedition.description}</p>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        {/* Per-expedition Flight Mode switcher (planning and active only) */}
        {!isCompleted && modesList.length > 0 && (
          <LegModeDropdown
            variant="header"
            modeId={activeModeId}
            modeColor={activeMode?.color}
            modesList={modesList}
            title={
              isPlanning
                ? 'Default flight mode for this expedition when engaged'
                : 'Flight mode for the current in-progress leg and upcoming visits'
            }
            onChangeMode={(nextModeId) => onChangeActiveMode?.(nextModeId)}
          />
        )}

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

        {onOpenExportModal && (
          <button
            type="button"
            disabled={isPlanning || !exportEnabled}
            onClick={onOpenExportModal}
            title={
              isPlanning
                ? 'Engage the expedition to enable exports'
                : !exportEnabled
                  ? 'Depart Waypoint #1 to enable exports'
                  : 'Open Export Studio to generate PNG cards, HTML reports, or JSON snapshots'
            }
            className={`ed-btn-cyan ${
              isPlanning || !exportEnabled ? 'opacity-40 cursor-not-allowed hover:bg-transparent' : ''
            }`}
          >
            EXPORT
          </button>
        )}

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
