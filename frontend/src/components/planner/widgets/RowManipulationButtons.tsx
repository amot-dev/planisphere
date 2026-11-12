import { memo } from 'react'

/**
 * RowManipulationButtonsProps defines the row position boundaries, menu state,
 * and action callbacks for reordering, inserting, and deleting planner rows.
 */
export interface RowManipulationButtonsProps {
  index: number
  minIndex: number
  total: number
  rowId?: number
  insertMenuOpen: boolean
  onToggleInsertMenu: () => void
  onInsert: (type: 'waypoint' | 'region') => void
  onMove: (toIndex: number) => void
  onDelete: () => void
}

/**
 * RowManipulationButtons provides row management controls (up, down, insert, delete)
 * for unvisited or planning-phase rows.
 */
export const RowManipulationButtons = memo(function RowManipulationButtons({
  index,
  minIndex,
  total,
  insertMenuOpen,
  onToggleInsertMenu,
  onInsert,
  onMove,
  onDelete,
}: RowManipulationButtonsProps) {
  return (
    <div className="relative inline-flex items-center gap-0.5 opacity-80 group-hover:opacity-100 shrink-0 whitespace-nowrap">
      <button
        type="button"
        disabled={index <= minIndex}
        onClick={() => onMove(index - 1)}
        title="Move row up"
        className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#FF7100] text-[#a6907c] hover:text-[#050506] border border-[#2e1f12] disabled:opacity-25 cursor-pointer font-mono-tabular text-xs"
      >
        ↑
      </button>
      <button
        type="button"
        disabled={index >= total - 1}
        onClick={() => onMove(index + 1)}
        title="Move row down"
        className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#FF7100] text-[#a6907c] hover:text-[#050506] border border-[#2e1f12] disabled:opacity-25 cursor-pointer font-mono-tabular text-xs"
      >
        ↓
      </button>

      {/* Insert Menu Dropdown Anchor */}
      <div className="relative inline-block">
        <button
          type="button"
          onClick={onToggleInsertMenu}
          title="Insert row below"
          className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#2e1f12] font-mono-tabular font-bold text-xs cursor-pointer"
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
            <div className="absolute right-0 top-full mt-1 z-30 w-56 border border-[#FF7100] bg-[#0b0907] shadow-2xl shadow-black p-1 text-left text-xs flex flex-col whitespace-normal divide-y divide-[#24180e]">
              <button
                type="button"
                onClick={() => onInsert('waypoint')}
                className="w-full px-3 py-2 text-left hover:bg-[#FF7100] hover:text-[#050506] text-[#F4ECE4] font-bold uppercase tracking-wider cursor-pointer transition-colors"
              >
                + Insert Waypoint Below
              </button>
              <button
                type="button"
                onClick={() => onInsert('region')}
                className="w-full px-3 py-2 text-left hover:bg-[#FF7100] hover:text-[#050506] text-[#FF7100] font-bold uppercase tracking-wider cursor-pointer transition-colors"
              >
                + Insert Region Header Below
              </button>
            </div>
          </>
        )}
      </div>

      <button
        type="button"
        onClick={onDelete}
        title="Delete row"
        className="h-5 px-1.5 inline-flex items-center justify-center bg-[#140e09] hover:bg-[#ff3b2f] text-[#a6907c] hover:text-[#050506] border border-[#2e1f12] font-mono-tabular text-xs cursor-pointer"
      >
        ×
      </button>
    </div>
  )
})

