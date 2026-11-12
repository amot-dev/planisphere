import { expedition, modes } from '../../../wailsjs/go/models'
import { LegBanner } from './widgets/LegBanner'

/**
 * PlannerActiveLegFooterProps defines the active expedition state, in-progress
 * leg mode metadata, and row-append callbacks rendered below the waypoint table.
 */
export interface PlannerActiveLegFooterProps {
  plan: expedition.PlanView
  isPlanning: boolean
  isActive: boolean
  isCompleted: boolean
  modesList: modes.Mode[]
  onActiveLegModeChange: (modeId: number) => void
  onAppendRow: (rowType: 'waypoint' | 'region') => void
}

/**
 * PlannerActiveLegFooter renders the current in-progress leg banner, bottom
 * Add Waypoint / Add Galactic Region Header action bar, and scroll spacer.
 */
export function PlannerActiveLegFooter({
  plan,
  isPlanning,
  isActive,
  isCompleted,
  modesList,
  onActiveLegModeChange,
  onAppendRow,
}: PlannerActiveLegFooterProps) {
  return (
    <>
      {/* In-progress leg since the Last Visited Waypoint */}
      {isActive && plan.visitedCount > 0 && (
        <div className="border-t border-[#3d2816] bg-[#0b0907]">
          <div className="px-4 py-1 text-[10px] font-bold uppercase tracking-[0.14em] text-[#a6907c]">
            Current In-Progress Leg (Since Last Logged Waypoint)
          </div>
          <LegBanner
            transitions={plan.pendingTransitions}
            modeId={plan.activeModeId}
            modeColor={plan.activeModeColor}
            modesList={modesList}
            onChangeMode={onActiveLegModeChange}
          />
        </div>
      )}

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

