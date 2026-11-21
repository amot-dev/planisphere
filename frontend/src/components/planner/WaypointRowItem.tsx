import { memo, useEffect, useRef, useState, type CSSProperties } from 'react'
import type React from 'react'
import { expedition } from '../../api/models'
import { formatLy } from '../../utils/galacticTime'
import { AutoExpandingTextarea } from '../common/AutoExpandingTextarea'
import {
  getWaypointLegBorderColor,
  type ExternalLinkVisibilityConfig,
} from './planTableItems'
import { CoordStatusIndicator } from './widgets/CoordStatusIndicator'
import { HoverOpenInLinks } from './widgets/HoverOpenInLinks'
import { RowManipulationButtons } from './widgets/RowManipulationButtons'

/**
 * WaypointRowHandlers bundles reference-stable row interaction callbacks so
 * memoized WaypointRowItem instances skip unaffected re-renders.
 */
export interface WaypointRowHandlers {
  onOriginSystemFilledChange?: (hasSystem: boolean) => void
  onToggleExpand: (id: number) => void
  onFieldBlur: (
    row: expedition.Row,
    field: 'systemName' | 'poiName' | 'notes',
    fieldValue: string,
  ) => void
  onCopySystem: (id: number, name: string) => void
  onToggleFcStop: (row: expedition.Row) => void
  onOverrideFcArrived?: (rowView: expedition.RowView) => void
  onOpenCarrierRoutePrompt: (index: number, row: expedition.Row) => void
  onToggleInsertMenu: (id: number) => void
  onInsert: (afterId: number, type: 'waypoint' | 'region') => void
  onDelete: (id: number) => void
  onHoldStart?: (index: number) => void
  onHoldEnd?: () => void
  onDragStart: (index: number, id: number, event: React.DragEvent) => void
  onDragEnd: () => void
}

/**
 * WaypointRowItemProps defines the row data, interaction flags, and shared
 * handler references for a single waypoint row in the flight planner table.
 */
export interface WaypointRowItemProps {
  rowView: expedition.RowView
  index: number
  isPlanning: boolean
  isLocked: boolean
  isFcStopLocked?: boolean
  isActivelyRoutedFcStop?: boolean
  lastFcArrivedIndex?: number
  isBeingDragged: boolean
  topGapHighlight?: boolean
  bottomGapHighlight?: boolean
  isExpanded: boolean
  isNextFcStop: boolean
  hasActiveCarrierRoute: boolean
  isNextWaypoint?: boolean
  playerSystem?: string
  isSystemCopied: boolean
  insertMenuOpen: boolean
  lastVisitedIndex: number
  rowStyle: CSSProperties
  dropProps: React.HTMLAttributes<HTMLTableRowElement>
  showNotes?: boolean
  showCarrierStop?: boolean
  externalLinks?: ExternalLinkVisibilityConfig
  handlers: WaypointRowHandlers
}

function UnknownDistanceSpan({
  systemName,
  unknownTitle,
}: {
  systemName: string | undefined
  unknownTitle: string
}) {
  const hasSystemName = Boolean(systemName?.trim())
  return (
    <span
      className="text-[#6e5b4b] uppercase text-[10px]"
      title={hasSystemName ? unknownTitle : undefined}
    >
      {hasSystemName ? 'UNKNOWN' : '—'}
    </span>
  )
}

function renderDistanceValue(
  isNext: boolean,
  rowView: expedition.RowView,
  row: expedition.Row,
  playerSystem?: string,
) {
  if (isNext) {
    if (rowView.distFromCurrentLy != null) {
      const tooltip = playerSystem
        ? `Estimated straight-line distance from player location (${playerSystem})`
        : 'Estimated straight-line distance from player location'
      return (
        <span className="text-[#00D2FF]" title={tooltip}>
          +{formatLy(rowView.distFromCurrentLy, 1)}
        </span>
      )
    }
    return (
      <UnknownDistanceSpan
        systemName={row.systemName}
        unknownTitle="Coordinates unknown for this waypoint or player location"
      />
    )
  }

  if (rowView.number === 1) {
    if (row.systemName?.trim()) {
      return (
        <span className="text-[#00D2FF]" title="Starting Waypoint (Origin)">
          +0 Ly
        </span>
      )
    }
    return <span className="text-[#6e5b4b]">—</span>
  }

  if (rowView.distFromPrevLy != null) {
    return (
      <span
        className="text-[#00D2FF]"
        title="Estimated straight-line distance from previous waypoint"
      >
        +{formatLy(rowView.distFromPrevLy, 1)}
      </span>
    )
  }

  return (
    <UnknownDistanceSpan
      systemName={row.systemName}
      unknownTitle="Coordinates unknown for this waypoint or preceding waypoint"
    />
  )
}

function renderTelemetryCellContent(
  isVisited: boolean,
  isNext: boolean,
  rowView: expedition.RowView,
  row: expedition.Row,
  playerSystem?: string,
) {
  if (isVisited) {
    return (
      <div className="h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5">
        <span className="font-extrabold uppercase tracking-wider text-[#FF7100] shrink-0">
          LOGGED VISIT
        </span>
        <span className="text-[#a6907c] shrink-0">{rowView.visitedAtGalactic}</span>
      </div>
    )
  }

  return (
    <div className="h-7 flex items-center whitespace-nowrap font-mono-tabular text-[11px] text-[#a6907c] leading-5">
      {renderDistanceValue(isNext, rowView, row, playerSystem)}
    </div>
  )
}

/**
 * WaypointRowItem renders a single waypoint row inside the expedition flight
 * planner table, supporting inline editing, Fleet Carrier stop badges, and drag-reorder.
 */
export const WaypointRowItem = memo(function WaypointRowItem({
  rowView,
  index,
  isPlanning,
  isLocked,
  isFcStopLocked = false,
  isActivelyRoutedFcStop = false,
  lastFcArrivedIndex = -1,
  isBeingDragged,
  topGapHighlight = false,
  bottomGapHighlight = false,
  isExpanded,
  isNextFcStop,
  hasActiveCarrierRoute,
  isNextWaypoint,
  playerSystem,
  isSystemCopied,
  insertMenuOpen,
  lastVisitedIndex,
  rowStyle,
  dropProps,
  showNotes = true,
  showCarrierStop = true,
  externalLinks,
  handlers,
}: WaypointRowItemProps) {
  const row = rowView.row
  const isVisited = rowView.visitState === 'visited'
  const isOriginRow = !isPlanning && rowView.number === 1
  const legBorderColor = getWaypointLegBorderColor(rowView, isPlanning)
  const hasLegBorder = Boolean(legBorderColor)
  const isFcArrivedStop = !isPlanning && Boolean(row.fcStop && row.fcArrived)
  const isSystemLocked =
    isLocked || isVisited || isOriginRow || isActivelyRoutedFcStop || isFcArrivedStop
  const allowInsertWaypoint = isPlanning || index >= lastVisitedIndex
  const canReorder = !isLocked && !isOriginRow && !isActivelyRoutedFcStop
  const activeRouteLockReason = isActivelyRoutedFcStop
    ? 'Locked while Fleet Carrier route to this waypoint is active'
    : undefined
  const deleteDisabledReason = isOriginRow
    ? 'Origin waypoint cannot be deleted'
    : activeRouteLockReason
  const reorderDisabledReason = isOriginRow
    ? 'Origin waypoint cannot be reordered'
    : activeRouteLockReason
  const systemLockTitle =
    activeRouteLockReason ||
    (isFcArrivedStop && !isVisited && !isOriginRow
      ? 'Locked while Fleet Carrier is marked arrived at this waypoint'
      : row.systemName)
  const canAddFcStop = !isLocked && !isFcStopLocked && (isPlanning || index > lastFcArrivedIndex)
  const canRemoveFcStop = !isLocked && !isFcStopLocked && (isPlanning || !row.fcArrived)
  const canOverrideFcBadge =
    !isPlanning && !isLocked && !isFcStopLocked && Boolean(handlers.onOverrideFcArrived)

  const hasDrawer = !isPlanning && rowView.visitState !== 'future'
  const isNext =
    Boolean(isNextWaypoint ?? (rowView.visitState === 'current' || rowView.isNextWaypoint)) &&
    !isPlanning
  const isEditingRef = useRef(false)
  const [draft, setDraft] = useState(() => ({
    systemName: row.systemName ?? '',
    poiName: row.poiName ?? '',
    notes: row.notes ?? '',
  }))

  useEffect(() => {
    if (isEditingRef.current) return
    setDraft({
      systemName: row.systemName ?? '',
      poiName: row.poiName ?? '',
      notes: row.notes ?? '',
    })
  }, [row.id, row.systemName, row.poiName, row.notes])

  function handleBlur(fieldName: 'systemName' | 'poiName' | 'notes', fieldValue: string) {
    isEditingRef.current = false
    handlers.onFieldBlur(row, fieldName, fieldValue)
  }

  const rowDropHighlightClass = isBeingDragged
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
      data-last-visited={!isPlanning && index === lastVisitedIndex ? 'true' : undefined}
      className={`group align-top ${
        isVisited
          ? 'bg-[#120e09] hover:bg-[#1b140d] text-[#F4ECE4]'
          : 'bg-[#090705] hover:bg-[#16100b] text-[#F4ECE4]'
      } ${rowDropHighlightClass}`}
    >
      {/* Expand Chevron */}
      {!isPlanning && (
        <td
          style={!isBeingDragged && hasLegBorder ? { borderLeftColor: legBorderColor } : undefined}
          className={`px-1 py-1.5 text-center align-top ${
            isBeingDragged
              ? 'border-l-2 border-l-[#FF7100] border-b border-[#24180e]'
              : hasLegBorder
                ? 'border-l-2 shadow-[inset_0_-1px_0_0_#24180e]'
                : 'border-b border-[#24180e]'
          }`}
        >
          <div className="h-7 flex items-center justify-center">
            {hasDrawer && (
              <button
                type="button"
                onClick={() => handlers.onToggleExpand(row.id)}
                aria-label="Toggle per-waypoint leg telemetry drawer"
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
          isPlanning && isBeingDragged
            ? 'border-l-2 border-l-[#FF7100] border-b border-[#24180e]'
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
            {isSystemLocked ? (
              <span
                title={systemLockTitle}
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
                onFocus={() => {
                  isEditingRef.current = true
                }}
                onChange={(event) => {
                  const nextValue = event.target.value
                  setDraft((previous) => ({ ...previous, systemName: nextValue }))
                  if (rowView.number === 1) {
                    handlers.onOriginSystemFilledChange?.(Boolean(nextValue.trim()))
                  }
                }}
                onBlur={(event) => {
                  handleBlur('systemName', event.target.value)
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    ;(event.target as HTMLInputElement).blur()
                  }
                }}
                className="w-full h-7 bg-transparent px-1.5 py-[3px] border border-transparent focus:border-[#FF7100] focus:bg-[#050506] focus:outline-none font-mono-tabular font-bold text-[#F4ECE4] placeholder:text-[#6e5b4b] select-text text-xs leading-5"
              />
            )}
            {isBeingDragged && (
              <span className="px-1.5 py-0.5 text-[9px] font-mono-tabular font-extrabold uppercase tracking-wider bg-[#FF7100] text-[#050506] shrink-0 select-none">
                MOVING
              </span>
            )}
            {row.systemName && (
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation()
                  handlers.onCopySystem(row.id, row.systemName)
                }}
                title={
                  isSystemCopied
                    ? `Copied "${row.systemName}"!`
                    : `Copy "${row.systemName}" to clipboard`
                }
                className={`h-5 w-5 flex items-center justify-center hover:bg-[#1f150d] cursor-pointer shrink-0 ${
                  isSystemCopied
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
          onFocus={() => {
            isEditingRef.current = true
          }}
          onChange={(value) => setDraft((previous) => ({ ...previous, poiName: value }))}
          onBlur={(value) => handleBlur('poiName', value)}
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
            onFocus={() => {
              isEditingRef.current = true
            }}
            onChange={(value) => setDraft((previous) => ({ ...previous, notes: value }))}
            onBlur={(value) => handleBlur('notes', value)}
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
                ) : canOverrideFcBadge ? (
                  <button
                    type="button"
                    onClick={() => handlers.onOverrideFcArrived?.(rowView)}
                    title={
                      row.fcArrived
                        ? 'Carrier has arrived at this waypoint (click to override)'
                        : 'Carrier arrival pending (click to override)'
                    }
                    className={`h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider border cursor-pointer transition-colors ${
                      row.fcArrived
                        ? 'bg-[#211307] text-[#FF7100] border-[#FF7100]/70 hover:border-[#FF7100]'
                        : 'bg-[#120d09] text-[#a6907c] border-[#3d2816] hover:border-[#FF7100] hover:text-[#F4ECE4]'
                    }`}
                  >
                    {row.fcArrived ? 'ARRIVED' : 'PENDING'}
                  </button>
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

                {!isPlanning && hasActiveCarrierRoute ? (
                  <span
                    title="Fleet Carrier route is currently active for this waypoint in the bottom router."
                    className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] text-[#00D2FF] border border-[#00D2FF]"
                  >
                    ROUTING
                  </span>
                ) : (
                  !isPlanning &&
                  isNextFcStop &&
                  !isFcStopLocked && (
                    <button
                      type="button"
                      onClick={() =>
                        handlers.onOpenCarrierRoutePrompt(
                          index,
                          expedition.Row.createFrom({ ...row, systemName: draft.systemName }),
                        )
                      }
                      title="Plot Fleet Carrier route to this next carrier stop"
                      className="h-5 px-1.5 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider bg-[#08151c] hover:bg-[#00D2FF] text-[#00D2FF] hover:text-[#050506] border border-[#00D2FF] cursor-pointer"
                    >
                      ROUTE
                    </button>
                  )
                )}

                {canRemoveFcStop && (
                  <button
                    type="button"
                    onClick={() => handlers.onToggleFcStop(row)}
                    title="Remove Fleet Carrier stop designation"
                    className="h-5 w-4 inline-flex items-center justify-center text-xs text-[#6e5b4b] hover:text-[#ff3b2f] cursor-pointer"
                  >
                    ×
                  </button>
                )}
              </>
            ) : canAddFcStop ? (
              <button
                type="button"
                onClick={() => handlers.onToggleFcStop(row)}
                title="Designate this waypoint as a Fleet Carrier stop"
                className="h-5 px-2 inline-flex items-center justify-center text-[9px] font-mono-tabular font-bold uppercase tracking-wider text-[#6e5b4b] hover:text-[#FF7100] border border-dashed border-[#2e1f12] hover:border-[#FF7100] bg-transparent hover:bg-[#16100b] cursor-pointer"
              >
                + FC STOP
              </button>
            ) : (
              <span className="h-5 inline-flex items-center justify-center text-[#6e5b4b] font-mono-tabular text-[10px]">
                —
              </span>
            )}
          </div>
        </td>
      )}

      {/* Visit Status / Galactic Timestamp */}
      <td className="px-3 py-1.5 align-top border-b border-[#24180e] whitespace-nowrap">
        {renderTelemetryCellContent(isVisited, isNext, rowView, row, playerSystem)}
      </td>

      {/* External Links + Row Manipulation + Drag Handle */}
      <td className="w-[320px] px-3 py-1.5 text-right whitespace-nowrap align-top border-b border-[#24180e]">
        <div className="h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap">
          <HoverOpenInLinks
            rowView={rowView}
            externalLinkInara={externalLinks?.inara}
            externalLinkEdsm={externalLinks?.edsm}
            externalLinkGec={externalLinks?.gec}
          />
          {!isLocked && (
            <>
              <RowManipulationButtons
                insertMenuOpen={insertMenuOpen}
                allowInsertWaypoint={allowInsertWaypoint}
                deleteDisabledReason={deleteDisabledReason}
                onToggleInsertMenu={() => handlers.onToggleInsertMenu(row.id)}
                onInsert={(type) => handlers.onInsert(row.id, type)}
                onDelete={() => handlers.onDelete(row.id)}
              />
              <button
                type="button"
                disabled={!canReorder}
                draggable={canReorder}
                onPointerDown={() => {
                  if (canReorder) handlers.onHoldStart?.(index)
                }}
                onPointerUp={() => handlers.onHoldEnd?.()}
                onDragStart={(event) => {
                  if (canReorder) handlers.onDragStart(index, row.id, event)
                }}
                onDragEnd={handlers.onDragEnd}
                title={
                  reorderDisabledReason ||
                  'Hold to preview valid drop positions, drag to reorder waypoint'
                }
                className={`h-5 px-1.5 inline-flex items-center justify-center disabled:opacity-25 disabled:cursor-not-allowed cursor-grab active:cursor-grabbing font-mono-tabular select-none border text-xs shrink-0 ${
                  isBeingDragged
                    ? 'bg-[#FF7100] text-[#050506] border-[#FF7100] font-bold'
                    : 'text-[#a6907c] enabled:hover:text-[#FF7100] border-[#2e1f12] bg-[#140e09] enabled:hover:bg-[#24180e]'
                }`}
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
