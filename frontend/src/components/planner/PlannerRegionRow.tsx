import { memo, useCallback, type CSSProperties, type DragEvent, type HTMLAttributes } from 'react'
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
  isPlanning: boolean
  isLocked: boolean
  isBeingDragged: boolean
  topGapHighlight?: boolean
  bottomGapHighlight?: boolean
  leftBorderColor?: string
  lastVisitedIndex: number
  insertMenuOpen: boolean
  rowStyle: CSSProperties
  dropProps: HTMLAttributes<HTMLTableRowElement>
  onChangeRegion: (rowId: number, selectedRegionId: number) => void
  onToggleInsertMenu: (rowId: number) => void
  onInsert: (afterRowId: number, insertType: 'waypoint' | 'region') => void
  onDelete: (rowId: number) => void
  onHoldStart: (rowIndex: number) => void
  onHoldEnd: () => void
  onDragStart: (rowIndex: number, rowId: number, event: DragEvent<HTMLButtonElement>) => void
  onDragEnd: () => void
}

/**
 * PlannerRegionRow renders a full-width Galactic Region header row inside the
 * flight planner table with region selection and row controls.
 */
export const PlannerRegionRow = memo(function PlannerRegionRow({
  expeditionId,
  rowView,
  index,
  visibleColumnCount,
  isPlanning,
  isLocked,
  isBeingDragged,
  topGapHighlight = false,
  bottomGapHighlight = false,
  leftBorderColor,
  lastVisitedIndex,
  insertMenuOpen,
  rowStyle,
  dropProps,
  onChangeRegion,
  onToggleInsertMenu,
  onInsert,
  onDelete,
  onHoldStart,
  onHoldEnd,
  onDragStart,
  onDragEnd,
}: PlannerRegionRowProps) {
  const row = rowView.row
  const handleSelectRegion = useCallback(
    (selectedRegionId: number) => onChangeRegion(row.id, selectedRegionId),
    [onChangeRegion, row.id],
  )
  const allowInsertWaypoint = isPlanning || index >= lastVisitedIndex
  const gapHighlightClass = isBeingDragged
    ? 'bg-[#261608] *:bg-[#261608] *:shadow-[inset_0_2px_0_0_#FF7100,inset_0_-2px_0_0_#FF7100]'
    : topGapHighlight
      ? '*:shadow-[inset_0_2px_0_0_#00D2FF]'
      : bottomGapHighlight
        ? '*:shadow-[inset_0_-2px_0_0_#00D2FF]'
        : ''

  return (
    <tr
      style={rowStyle}
      {...dropProps}
      data-row-id={row.id}
      data-row-index={index}
      data-being-dragged={isBeingDragged ? 'true' : undefined}
      className={`group ed-header-bar ${gapHighlightClass}`}
    >
      {/* Region Name and optional MOVING badge */}
      <td
        colSpan={visibleColumnCount - 1}
        style={!isBeingDragged && leftBorderColor ? { borderLeftColor: leftBorderColor } : undefined}
        className={`px-3 py-2 ${
          !isBeingDragged && leftBorderColor
            ? 'border-l-2 shadow-[inset_0_-1px_0_0_#3a2615]'
            : 'border-b border-[#3a2615]'
        }`}
      >
        <div className="inline-flex items-center gap-2">
          <RegionHeaderDropdown
            expeditionId={expeditionId}
            rowId={row.id}
            currentRegionId={row.regionId}
            regionName={rowView.regionName}
            locked={isLocked}
            onSelect={handleSelectRegion}
          />
          {isBeingDragged && (
            <span className="px-1.5 py-0.5 text-[9px] font-mono-tabular font-extrabold uppercase tracking-wider bg-[#FF7100] text-[#050506] shrink-0 select-none">
              MOVING
            </span>
          )}
        </div>
      </td>

      <td className="w-[320px] px-3 py-2 text-right whitespace-nowrap border-b border-[#3a2615]">
        {!isLocked && (
          <div className="inline-flex items-center justify-end gap-1 whitespace-nowrap">
            <RowManipulationButtons
              insertMenuOpen={insertMenuOpen}
              allowInsertWaypoint={allowInsertWaypoint}
              onToggleInsertMenu={() => onToggleInsertMenu(row.id)}
              onInsert={(insertType) => onInsert(row.id, insertType)}
              onDelete={() => onDelete(row.id)}
            />
            <button
              type="button"
              draggable={true}
              onPointerDown={() => onHoldStart(index)}
              onPointerUp={onHoldEnd}
              onDragStart={(event) => onDragStart(index, row.id, event)}
              onDragEnd={onDragEnd}
              title="Drag to reposition Region Header"
              className={`h-5 px-1.5 inline-flex items-center justify-center cursor-grab active:cursor-grabbing font-mono-tabular select-none border text-xs shrink-0 ${
                isBeingDragged
                  ? 'bg-[#FF7100] text-[#050506] border-[#FF7100] font-bold'
                  : 'text-[#FF7100] hover:text-[#ff9436] border-[#472e19] bg-[#16100b] hover:bg-[#24180e]'
              }`}
            >
              ⋮⋮
            </button>
          </div>
        )}
      </td>
    </tr>
  )
})

