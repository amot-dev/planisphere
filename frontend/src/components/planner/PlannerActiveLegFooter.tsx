/**
 * PlannerActiveLegFooterProps defines the expedition status flags and
 * row-append callback rendered below the waypoint table.
 */
export interface PlannerActiveLegFooterProps {
  isPlanning: boolean
  isCompleted: boolean
  onAppendRow: (rowType: 'waypoint' | 'region') => void
}

/**
 * PlannerActiveLegFooter renders the bottom Add Waypoint and Add Galactic Region
 * Header action bar and scroll spacer.
 */
export function PlannerActiveLegFooter({
  isPlanning,
  isCompleted,
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
              <span>Add Waypoint</span>
            </button>
            <button
              type="button"
              onClick={() => onAppendRow('region')}
              className="ed-btn"
            >
              <span className="font-extrabold text-[#FF7100]">+</span>
              <span>Add Galactic Region Header</span>
            </button>
          </div>

          <div className="text-[10px] font-mono-tabular uppercase tracking-wider text-[#a6907c]">
            {isPlanning
              ? 'DRAG (⋮⋮) OR USE ARROWS TO REORDER WAYPOINTS AND SECTORS'
              : 'UNVISITED WAYPOINTS DRAGGABLE · CLONE IN PLANS MENU TO RE-PLAN'}
          </div>
        </div>
      )}

      {/* Breathing room to scroll past the bottom of the list */}
      <div className="h-48 shrink-0 pointer-events-none" aria-hidden="true" />
    </>
  )
}

