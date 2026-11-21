import { memo } from 'react'

/**
 * RowManipulationButtonsProps defines the menu state, insertion permissions,
 * and action callbacks for inserting and deleting planner rows.
 */
export interface RowManipulationButtonsProps {
  insertMenuOpen: boolean
  allowInsertWaypoint?: boolean
  deleteDisabledReason?: string
  onToggleInsertMenu: () => void
  onInsert: (type: 'waypoint' | 'region') => void
  onDelete: () => void
}

/**
 * RowManipulationButtons provides row management controls (insert below and delete)
 * for planner rows.
 */
export const RowManipulationButtons = memo(function RowManipulationButtons({
  insertMenuOpen,
  allowInsertWaypoint = true,
  deleteDisabledReason,
  onToggleInsertMenu,
  onInsert,
  onDelete,
}: RowManipulationButtonsProps) {
  return (
    <div className="relative inline-flex items-center gap-0.5 opacity-80 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      {/* Insert Menu Dropdown Anchor */}
      <div className="relative inline-block">
        <button
          type="button"
          onClick={onToggleInsertMenu}
          title="Insert row below"
          className="h-5 px-1.5 inline-flex items-center justify-center bg-ed-header hover:bg-ed-cyan text-ed-cyan hover:text-ed-void border border-ed-border-dim font-mono-tabular font-bold text-xs cursor-pointer"
        >
          +
        </button>

        {insertMenuOpen && (
          <>
            {/* Transparent backdrop to close dropdown when clicking outside */}
            <div
              className="fixed inset-0 z-20 cursor-default"
              onClick={onToggleInsertMenu}
            />
            {/* Vertically stacked menu aligning to the right of the '+' button (expanding safely leftward) */}
            <div className="absolute right-0 top-full mt-1 z-30 w-60 border border-ed-amber bg-ed-panel shadow-2xl shadow-black p-1 text-left text-xs flex flex-col whitespace-normal divide-y divide-ed-border-subtle">
              {allowInsertWaypoint ? (
                <button
                  type="button"
                  onClick={() => onInsert('waypoint')}
                  className="w-full px-3 py-2 text-left hover:bg-ed-amber hover:text-ed-void text-ed-text-main font-bold uppercase tracking-wider cursor-pointer transition-colors"
                >
                  + Insert Waypoint Below
                </button>
              ) : (
                <div className="px-3 py-2 text-[10px] text-ed-text-muted leading-4">
                  Add new waypoints in the unvisited section and drag them into valid past slots.
                </div>
              )}
              <button
                type="button"
                onClick={() => onInsert('region')}
                className="w-full px-3 py-2 text-left hover:bg-ed-amber hover:text-ed-void text-ed-amber font-bold uppercase tracking-wider cursor-pointer transition-colors"
              >
                + Insert Region Header Below
              </button>
            </div>
          </>
        )}
      </div>

      <button
        type="button"
        disabled={Boolean(deleteDisabledReason)}
        onClick={onDelete}
        title={deleteDisabledReason || 'Delete row'}
        className="h-5 px-1.5 inline-flex items-center justify-center bg-ed-header enabled:hover:bg-ed-danger text-ed-text-muted enabled:hover:text-ed-void border border-ed-border-dim disabled:opacity-25 disabled:cursor-not-allowed font-mono-tabular text-xs cursor-pointer"
      >
        ×
      </button>
    </div>
  )
})

