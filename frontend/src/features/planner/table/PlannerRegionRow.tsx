import { memo, useCallback, type CSSProperties, type DragEvent, type HTMLAttributes } from 'react'
import { expedition } from '../../../api/models'
import { RegionHeaderDropdown } from './widgets/RegionHeaderDropdown'
import { RowManipulationButtons } from './widgets/RowManipulationButtons'

/**
 * PlannerRegionRowProps defines the row view, column span, lock state, drag
 * attributes, and manipulation callbacks for a Region Header row.
 */
export interface PlannerRegionRowProps {
  expeditionId: number
  rowView: expedition.RowView
  index: number
  visibleColumnCount: number
  isLocked: boolean
  isBeingDragged: boolean
  topGapHighlight?: boolean
  bottomGapHighlight?: boolean
  /** Inherited leg accent border color rendered on the left edge when active (omitted while dragging). */
  leftBorderColor?: string
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
 * PlannerRegionRow renders a full-width Region Header row inside the
 * flight planner table with region selection and row controls.
 */
export const PlannerRegionRow = memo(function PlannerRegionRow({
  expeditionId,
  rowView,
  index,
  visibleColumnCount,
  isLocked,
  isBeingDragged,
  topGapHighlight = false,
  bottomGapHighlight = false,
  leftBorderColor,
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
  const allowInsertWaypoint = Boolean(rowView.allowInsertWaypoint)
  const gapHighlightClass = isBeingDragged
    ? 'bg-ed-amber-drag *:bg-ed-amber-drag *:shadow-[inset_0_2px_0_0_var(--color-ed-amber),inset_0_-2px_0_0_var(--color-ed-amber)]'
    : topGapHighlight
      ? '*:shadow-[inset_0_2px_0_0_var(--color-ed-cyan)]'
      : bottomGapHighlight
        ? '*:shadow-[inset_0_-2px_0_0_var(--color-ed-cyan)]'
        : ''

  return (
    <tr
      style={rowStyle}
      {...dropProps}
      data-row-id={row.id}
      data-row-index={index}
      data-being-dragged={isBeingDragged ? 'true' : undefined}
      className={`group ${isBeingDragged ? '' : 'ed-header-bar'} ${gapHighlightClass}`}
    >
      {/* Region Name and optional MOVING badge */}
      <td
        colSpan={visibleColumnCount - 1}
        style={!isBeingDragged && leftBorderColor ? { borderLeftColor: leftBorderColor } : undefined}
        className={`px-3 py-2 ${
          !isBeingDragged && leftBorderColor
            ? 'border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border)]'
            : 'border-b border-ed-border'
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
            <span className="px-1.5 py-0.5 text-[9px] font-mono-tabular font-extrabold uppercase tracking-wider bg-ed-amber text-ed-void shrink-0 select-none">
              MOVING
            </span>
          )}
        </div>
      </td>

      <td className="w-[320px] px-3 py-2 text-right whitespace-nowrap border-b border-ed-border">
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
                  ? 'bg-ed-amber text-ed-void border-ed-amber font-bold'
                  : 'text-ed-amber hover:text-ed-amber-bright border-ed-border-strong bg-ed-control hover:bg-ed-hover'
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

