import type { CSSProperties, DragEvent, HTMLAttributes } from 'react'
import { expedition } from '../../../wailsjs/go/models'
import { RegionHeaderDropdown } from './widgets/RegionHeaderDropdown'
import { RowManipulationButtons } from './widgets/RowManipulationButtons'

/**
 * PlannerRegionRowProps defines the row view, column span, lock state, drag
 * attributes, and manipulation callbacks for a Galactic Region divider row.
 */
export interface PlannerRegionRowProps {
  expeditionId: number
  rowView: expedition.RowView
  index: number
  visibleColumnCount: number
  isLocked: boolean
  isBeingDragged: boolean
  isDragTarget: boolean
  lastVisitedIndex: number
  totalRows: number
  insertMenuOpen: boolean
  rowStyle: CSSProperties
  dropProps: HTMLAttributes<HTMLTableRowElement>
  onChangeRegion: (rowId: number, selectedRegionId: number) => void
  onToggleInsertMenu: () => void
  onInsert: (insertType: 'waypoint' | 'region') => void
  onMove: (targetIndex: number) => void
  onDelete: () => void
  onDragStart: (event: DragEvent<HTMLButtonElement>) => void
  onDragEnd: () => void
}

/**
 * PlannerRegionRow renders a full-width Galactic Region header row inside the
 * flight planner table with region selection and unvisited row controls.
 */
export function PlannerRegionRow({
  expeditionId,
  rowView,
  index,
  visibleColumnCount,
  isLocked,
  isBeingDragged,
  isDragTarget,
  lastVisitedIndex,
  totalRows,
  insertMenuOpen,
  rowStyle,
  dropProps,
  onChangeRegion,
  onToggleInsertMenu,
  onInsert,
  onMove,
  onDelete,
  onDragStart,
  onDragEnd,
}: PlannerRegionRowProps) {
  const row = rowView.row

  return (
    <tr
      style={rowStyle}
      {...dropProps}
      className={`group ed-header-bar ${
        isBeingDragged ? 'opacity-40' : ''
      } ${isDragTarget ? 'outline-2 outline-[#00D2FF]' : ''}`}
    >
      {/* Just the Region Name and nothing else */}
      <td colSpan={visibleColumnCount - 1} className="px-3 py-2 border-b border-[#3a2615]">
        <RegionHeaderDropdown
          expeditionId={expeditionId}
          rowId={row.id}
          currentRegionId={row.regionId}
          regionName={rowView.regionName}
          locked={isLocked}
          onSelect={(selectedRegionId) => onChangeRegion(row.id, selectedRegionId)}
        />
      </td>

      <td className="w-[380px] px-3 py-2 text-right whitespace-nowrap border-b border-[#3a2615]">
        {!isLocked && (
          <div className="inline-flex items-center justify-end gap-1 whitespace-nowrap">
            <RowManipulationButtons
              index={index}
              minIndex={lastVisitedIndex + 1}
              total={totalRows}
              rowId={row.id}
              insertMenuOpen={insertMenuOpen}
              onToggleInsertMenu={onToggleInsertMenu}
              onInsert={onInsert}
              onMove={onMove}
              onDelete={onDelete}
            />
            <button
              type="button"
              draggable={true}
              onDragStart={onDragStart}
              onDragEnd={onDragEnd}
              title="Drag to reposition Galactic Region Header"
              className="h-5 px-1.5 inline-flex items-center justify-center text-[#FF7100] hover:text-[#ff9436] cursor-grab active:cursor-grabbing font-mono-tabular select-none border border-[#472e19] bg-[#16100b] hover:bg-[#24180e] text-xs shrink-0"
            >
              ⋮⋮
            </button>
          </div>
        )}
      </td>
    </tr>
  )
}

