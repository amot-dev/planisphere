/**
 * PlannerActiveLegFooterProps defines the expedition status flags and
 * row-append callback rendered below the waypoint table.
 */
export interface PlannerActiveLegFooterProps {
  isPlanning: boolean
  isCompleted: boolean
  hasCollapsedRows?: boolean
  onAppendRow: (rowType: 'waypoint' | 'region') => void
}

/**
 * PlannerActiveLegFooter renders the bottom Add Waypoint and Add Region Header
 * action bar and scroll spacer.
 */
export function PlannerActiveLegFooter({
  isPlanning,
  isCompleted,
  hasCollapsedRows = false,
  onAppendRow,
}: PlannerActiveLegFooterProps) {
  return (
    <>
      {/* Bottom Route Action Bar: Add Waypoint / Add Region Header */}
      {!isCompleted && (
        <div className="px-5 py-3.5 border-t border-[#3d2816] bg-[#0b0907] flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={() => onAppendRow('waypoint')}
              className="ed-btn-cyan"
            >
              <span className="font-extrabold">+</span>
              <span>ADD WAYPOINT</span>
            </button>
            <button
              type="button"
              onClick={() => onAppendRow('region')}
              className="ed-btn"
            >
              <span className="font-extrabold text-[#FF7100]">+</span>
              <span>ADD REGION HEADER</span>
            </button>
          </div>

          <div className="text-[10px] font-mono-tabular uppercase tracking-wider text-[#a6907c]">
            {isPlanning
              ? 'HOLD AND DRAG (⋮⋮) TO REORDER WAYPOINTS AND REGION HEADERS'
              : 'HOLD (⋮⋮) TO PREVIEW VALID CHRONOLOGICAL BOUNDS · CLONE IN EXPEDITIONS MENU TO RE-PLAN'}
          </div>
        </div>
      )}

      {/* Breathing room to scroll past the bottom of the list */}
      <div
        data-planner-bottom-spacer="true"
        className={
          hasCollapsedRows
            ? 'h-[85vh] shrink-0 pointer-events-none'
            : 'h-48 shrink-0 pointer-events-none'
        }
        aria-hidden="true"
      />
    </>
  )
}

