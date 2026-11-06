import { memo, type CSSProperties } from 'react'
import type React from 'react'
import { expedition } from '../../wailsjs/go/models'
import { formatLy } from '../utils/galacticTime'
import {
  AutoExpandingTextarea,
  CoordStatusIndicator,
  HoverOpenInLinks,
  RowManipulationButtons,
} from './PlannerWidgets'

export interface WaypointRowItemProps {
  rowView: expedition.RowView
  index: number
  isPlanning: boolean
  isLocked: boolean
  isBeingDragged: boolean
  isDragTarget: boolean
  isExpanded: boolean
  isNextFcStop: boolean
  hasActiveCarrierRoute: boolean
  draft: { systemName: string; poiName: string; notes: string }
  copiedSystemRowId: number | null
  insertMenuOpen: boolean
  lastVisitedIndex: number
  totalRows: number
  rowStyle: CSSProperties
  dropProps: React.HTMLAttributes<HTMLTableRowElement>
  showNotes?: boolean
  showCarrierStop?: boolean
  onToggleExpand: (id: number) => void
  onDraftChange: (id: number, field: 'systemName' | 'poiName' | 'notes', fieldValue: string) => void
  onFieldFocus: (id: number) => void
  onFieldBlur: (row: expedition.Row, field: 'systemName' | 'poiName' | 'notes', fieldValue: string) => void
  onCopySystem: (id: number, name: string) => void
  onToggleFcStop: (row: expedition.Row) => void
  onOpenCarrierRoutePrompt: (index: number, row: expedition.Row) => void
  onToggleInsertMenu: (id: number) => void
  onInsert: (afterId: number, type: 'waypoint' | 'region') => void
  onMove: (id: number, targetIndex: number) => void
  onDelete: (id: number) => void
  onDragStart: (index: number, id: number, event: React.DragEvent) => void
  onDragEnd: () => void
}

export const WaypointRowItem = memo(function WaypointRowItem({
  rowView,
  index,
  isPlanning,
  isLocked,
  isBeingDragged,
  isDragTarget,
  isExpanded,
  isNextFcStop,
  hasActiveCarrierRoute,
  draft,
  copiedSystemRowId,
  insertMenuOpen,
  lastVisitedIndex,
  totalRows,
  rowStyle,
  dropProps,
  showNotes = true,
  showCarrierStop = true,
  onToggleExpand,
  onDraftChange,
  onFieldFocus,
  onFieldBlur,
  onCopySystem,
  onToggleFcStop,
  onOpenCarrierRoutePrompt,
  onToggleInsertMenu,
  onInsert,
  onMove,
  onDelete,
  onDragStart,
  onDragEnd,
}: WaypointRowItemProps) {
  const row = rowView.row
  const isVisited = rowView.visitState === 'visited' || rowView.visitState === 'current'

  return (
    <tr
      style={rowStyle}
      {...dropProps}
      data-row-id={row.id}
      data-row-index={index}
      data-last-visited={!isPlanning && index === lastVisitedIndex ? 'true' : undefined}
      className={`group align-top ${
        isBeingDragged ? 'opacity-40' : ''
      } ${isDragTarget ? 'outline-2 outline-[#00D2FF]' : ''} ${
        isVisited
          ? 'bg-[#120e09] hover:bg-[#1b140d] text-[#F4ECE4]'
          : 'bg-[#090705] hover:bg-[#16100b] text-[#F4ECE4]'
      }`}
    >
      {/* Expand Chevron */}
      {!isPlanning && (
        <td
          className={`px-1 py-1.5 text-center align-top ${
            isVisited
              ? 'border-l-2 border-l-[#FF7100] shadow-[inset_0_-1px_0_0_#24180e]'
              : 'border-b border-[#24180e]'
          }`}
        >
          <div className="h-7 flex items-center justify-center">
            {isVisited && (
              <button
                type="button"
                onClick={() => onToggleExpand(row.id)}
                title="Toggle per-waypoint leg telemetry / distance drawer"
                className={`h-5 w-5 flex items-center justify-center hover:bg-[#24180e] cursor-pointer ${
                  isExpanded ? 'text-[#FF7100]' : 'text-[#a6907c]'
                }`}
              >
                <svg
                  className={`w-3.5 h-3.5 transition-transform ${
                    isExpanded ? 'rotate-90' : ''
                  }`}
                  viewBox="0 0 20 20"
                  fill="currentColor"
                >
                  <path
                    fillRule="evenodd"
                    d="M7.21 14.77a.75.75 0 01.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.25 4.5a.75.75 0 01-1.06-.02z"
                    clipRule="evenodd"
                  />
                </svg>
              </button>
            )}
          </div>
        </td>
      )}

      {/* Sequential Waypoint # */}
      <td
        className={`px-1.5 py-1.5 text-right font-mono-tabular font-bold text-[#FF7100] align-top ${
          isPlanning && isVisited
            ? 'border-l-2 border-l-[#FF7100] shadow-[inset_0_-1px_0_0_#24180e]'
            : 'border-b border-[#24180e]'
        }`}
      >
        <div className="h-7 flex items-center justify-end font-mono-tabular font-bold text-[#FF7100] text-xs leading-5">
          {String(rowView.number).padStart(2, '0')}
        </div>
      </td>

      {/* System Name (EDSM indicator on left + system name + copy button) */}
      <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
        <div className="h-7 flex items-center gap-2 min-w-0">
          <CoordStatusIndicator rowView={rowView} />
          <div className="flex-1 min-w-0 flex items-center gap-1 h-7">
            {isLocked ? (
              <span
                title={row.systemName}
                className="w-full h-7 flex items-center font-mono-tabular font-bold text-[#F4ECE4] px-1.5 py-[3px] border border-transparent select-text cursor-text truncate text-xs leading-5"
              >
                {row.systemName || '—'}
              </span>
            ) : (
              <input
                type="text"
                spellCheck={false}
                value={draft.systemName}
                placeholder="Star System"
                onFocus={() => onFieldFocus(row.id)}
                onChange={(event) =>
                  onDraftChange(row.id, 'systemName', event.target.value)
                }
                onBlur={(event) => {
                  onFieldBlur(row, 'systemName', event.target.value)
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    ;(event.target as HTMLInputElement).blur()
                  }
                }}
                className="w-full h-7 bg-transparent px-1.5 py-[3px] border border-transparent focus:border-[#FF7100] focus:bg-[#050506] focus:outline-none font-mono-tabular font-bold text-[#F4ECE4] placeholder:text-[#6e5b4b] select-text text-xs leading-5"
              />
            )}
            {row.systemName && (
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation()
                  onCopySystem(row.id, row.systemName)
                }}
                title={
                  copiedSystemRowId === row.id
                    ? `Copied "${row.systemName}"!`
                    : `Copy "${row.systemName}" to clipboard`
                }
                className={`h-5 w-5 flex items-center justify-center hover:bg-[#1f150d] cursor-pointer shrink-0 ${
                  copiedSystemRowId === row.id
                    ? 'text-[#00D2FF]'
                    : 'text-[#6e5b4b] hover:text-[#FF7100]'
                }`}
              >
                <svg className="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                  <path d="M7 3.5A1.5 1.5 0 018.5 2h3.879a1.5 1.5 0 011.06.44l3.122 3.12A1.5 1.5 0 0117 6.622V12.5a1.5 1.5 0 01-1.5 1.5h-1v-3.379a3 3 0 00-.879-2.121L10.5 5.379A3 3 0 008.379 4.5H7v-1z" />
                  <path d="M4.5 6A1.5 1.5 0 003 7.5v9A1.5 1.5 0 004.5 18h7a1.5 1.5 0 001.5-1.5v-5.879a1.5 1.5 0 00-.44-1.06L9.44 6.44A1.5 1.5 0 008.379 6H4.5z" />
                </svg>
              </button>
            )}
          </div>
        </div>
      </td>

      {/* POI Name (always editable, auto-expands up to 3 lines) */}
      <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
        <AutoExpandingTextarea
          value={draft.poiName}
          placeholder="POI Name"
          maxLines={3}
          onFocus={() => onFieldFocus(row.id)}
          onChange={(value) => onDraftChange(row.id, 'poiName', value)}
          onBlur={(value) => onFieldBlur(row, 'poiName', value)}
          className="text-[#F4ECE4] placeholder:text-[#6e5b4b]"
        />
      </td>

      {/* Flight Notes (always editable, auto-expands up to 3 lines) */}
      {showNotes && (
        <td className="px-3 py-1.5 align-top border-b border-[#24180e]">
          <AutoExpandingTextarea
            value={draft.notes}
            placeholder="Flight notes, reminders..."
            maxLines={3}
            onFocus={() => onFieldFocus(row.id)}
            onChange={(value) => onDraftChange(row.id, 'notes', value)}
            onBlur={(value) => onFieldBlur(row, 'notes', value)}
            className="text-[#d6c7b8] placeholder:text-[#6e5b4b]"
          />
        </td>
      )}

      {/* Carrier Stop */}
      {showCarrierStop && (
        <td className="px-2 py-1.5 text-center align-top border-b border-[#24180e]">
          <div className="h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap">
            {row.fcStop ? (
              <>
                {isPlanning ? (
                  <span
                    title="Designated Fleet Carrier Stop for this expedition"
                    className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#120d09] text-[#a6907c] border border-[#3d2816] select-none"
                  >
                    FC STOP
                  </span>
                ) : (
                  <span
                    title={
                      row.fcArrived
                        ? 'Carrier has arrived at this waypoint'
                        : 'Carrier arrival pending'
                    }
                    className={`h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider border select-none ${
                      row.fcArrived
                        ? 'bg-[#211307] text-[#FF7100] border-[#FF7100]/70'
                        : 'bg-[#120d09] text-[#a6907c] border-[#3d2816]'
                    }`}
                  >
                    {row.fcArrived ? 'ARRIVED' : 'PENDING'}
                  </span>
                )}

                {!isPlanning && isNextFcStop && (
                  hasActiveCarrierRoute ? (
                    <span
                      title="Fleet Carrier route is currently active for this waypoint in the bottom plotter."
                      className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]"
                    >
                      ROUTING
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={() => onOpenCarrierRoutePrompt(index, row)}
                      title="Plot Fleet Carrier route to this next carrier waypoint"
                      className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#00D2FF] cursor-pointer"
                    >
                      ROUTE
                    </button>
                  )
                )}

                {!isLocked && (
                  <button
                    type="button"
                    onClick={() => onToggleFcStop(row)}
                    title="Remove Fleet Carrier stop designation"
                    className="h-5 w-4 inline-flex items-center justify-center text-xs text-[#6e5b4b] hover:text-[#ff3b2f] cursor-pointer"
                  >
                    ×
                  </button>
                )}
              </>
            ) : !isLocked ? (
              <button
                type="button"
                onClick={() => onToggleFcStop(row)}
                title="Designate this waypoint as a Fleet Carrier stop"
                className="h-5 px-2 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider text-[#6e5b4b] hover:text-[#FF7100] border border-dashed border-[#2e1f12] hover:border-[#FF7100] bg-transparent hover:bg-[#16100b] cursor-pointer"
              >
                + FC STOP
              </button>
            ) : (
              <span className="h-5 inline-flex items-center justify-center text-[#6e5b4b] font-mono-tabular text-[10px]">—</span>
            )}
          </div>
        </td>
      )}

      {/* Visit Status / Galactic Timestamp */}
      <td className="px-3 py-1.5 align-top border-b border-[#24180e] whitespace-nowrap">
        {isVisited ? (
          <div className="h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5">
            <span className="font-extrabold uppercase tracking-wider text-[#FF7100] shrink-0">
              LOGGED VISIT
            </span>
            <span className="text-[#a6907c] shrink-0">
              {rowView.visitedAtGalactic}
            </span>
          </div>
        ) : (
          <div className="h-7 flex items-center whitespace-nowrap font-mono-tabular text-[11px] text-[#a6907c] leading-5">
            {rowView.number === 1 ? (
              row.systemName?.trim() ? (
                <span
                  className="text-[#00D2FF]"
                  title="Starting Waypoint (Origin)"
                >
                  +0 Ly
                </span>
              ) : (
                <span className="text-[#6e5b4b]">—</span>
              )
            ) : rowView.distFromPrevLy !== undefined ? (
              <span
                className="text-[#00D2FF]"
                title="Estimated straight-line distance from previous waypoint"
              >
                +{formatLy(rowView.distFromPrevLy, 1)}
              </span>
            ) : (
              <span
                className="text-[#6e5b4b] uppercase text-[10px]"
                title={
                  row.systemName?.trim()
                    ? 'Coordinates unknown for this waypoint or preceding waypoint'
                    : undefined
                }
              >
                {row.systemName?.trim() ? 'UNKNOWN' : '—'}
              </span>
            )}
          </div>
        )}
      </td>

      {/* External Links + Row Manipulation + Drag Handle */}
      <td className="w-[380px] px-3 py-1.5 text-right whitespace-nowrap align-top border-b border-[#24180e]">
        <div className="h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap">
          <HoverOpenInLinks rowView={rowView} />
          {!isLocked && (
            <>
              <RowManipulationButtons
                index={index}
                minIndex={lastVisitedIndex + 1}
                total={totalRows}
                rowId={row.id}
                insertMenuOpen={insertMenuOpen}
                onToggleInsertMenu={() => onToggleInsertMenu(row.id)}
                onInsert={(type) => onInsert(row.id, type)}
                onMove={(targetIndex) => onMove(row.id, targetIndex)}
                onDelete={() => onDelete(row.id)}
              />
              <button
                type="button"
                draggable={true}
                onDragStart={(event) => onDragStart(index, row.id, event)}
                onDragEnd={onDragEnd}
                title="Drag to reorder waypoint"
                className="h-5 px-1.5 inline-flex items-center justify-center text-[#a6907c] hover:text-[#FF7100] cursor-grab active:cursor-grabbing font-mono-tabular select-none border border-[#2e1f12] bg-[#140e09] hover:bg-[#24180e] text-xs shrink-0"
              >
                ⋮⋮
              </button>
            </>
          )}
        </div>
      </td>
    </tr>
  )
})
