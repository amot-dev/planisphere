import { memo, useEffect, useRef, useState, type CSSProperties } from 'react'
import type React from 'react'
import { expedition } from '../../../api/models'
import { AutoExpandingTextarea } from '../../../components/ui/AutoExpandingTextarea'
import { ChevronRightIcon, CopyIcon } from '../../../components/ui/HudIcons'
import type { ExternalLinkVisibilityConfig } from '../../../domain/externalLinks'
import { WaypointCarrierStopCell } from './WaypointCarrierStopCell'
import { deriveWaypointRowPermissions } from './waypointRowPermissions'
import { WaypointTelemetryCell } from './WaypointTelemetryCell'
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
  const permissions = deriveWaypointRowPermissions({
    rowView,
    index,
    isPlanning,
    isLocked,
    isFcStopLocked,
    isActivelyRoutedFcStop,
    lastFcArrivedIndex,
    lastVisitedIndex,
    isBeingDragged,
    topGapHighlight,
    bottomGapHighlight,
    isNextWaypoint,
    hasOverrideFcArrived: Boolean(handlers.onOverrideFcArrived),
  })

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

  return (
    <tr
      style={rowStyle}
      {...dropProps}
      data-row-id={row.id}
      data-row-index={index}
      data-being-dragged={isBeingDragged ? 'true' : undefined}
      data-last-visited={!isPlanning && index === lastVisitedIndex ? 'true' : undefined}
      className={`group align-top ${
        permissions.isVisited
          ? 'bg-[#120e09] hover:bg-[#1b140d] text-[#F4ECE4]'
          : 'bg-[#090705] hover:bg-[#16100b] text-[#F4ECE4]'
      } ${permissions.rowDropHighlightClass}`}
    >
      {/* Expand Chevron */}
      {!isPlanning && (
        <td
          style={
            !isBeingDragged && permissions.hasLegBorder
              ? { borderLeftColor: permissions.legBorderColor }
              : undefined
          }
          className={`px-1 py-1.5 text-center align-top ${
            isBeingDragged
              ? 'border-l-2 border-l-[#FF7100] border-b border-[#24180e]'
              : permissions.hasLegBorder
                ? 'border-l-2 shadow-[inset_0_-1px_0_0_#24180e]'
                : 'border-b border-[#24180e]'
          }`}
        >
          <div className="h-7 flex items-center justify-center">
            {permissions.hasDrawer && (
              <button
                type="button"
                onClick={() => handlers.onToggleExpand(row.id)}
                aria-label="Toggle per-waypoint leg telemetry drawer"
                className={`h-5 w-5 flex items-center justify-center hover:bg-[#24180e] cursor-pointer ${
                  isExpanded ? 'text-[#FF7100]' : 'text-[#a6907c]'
                }`}
              >
                <ChevronRightIcon expanded={isExpanded} />
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
            {permissions.isSystemLocked ? (
              <span
                title={permissions.systemLockTitle}
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
                <CopyIcon />
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
        <WaypointCarrierStopCell
          rowView={rowView}
          index={index}
          draftSystemName={draft.systemName}
          isPlanning={isPlanning}
          isNextFcStop={isNextFcStop}
          isFcStopLocked={isFcStopLocked}
          hasActiveCarrierRoute={hasActiveCarrierRoute}
          canAddFcStop={permissions.canAddFcStop}
          canRemoveFcStop={permissions.canRemoveFcStop}
          canOverrideFcBadge={permissions.canOverrideFcBadge}
          onToggleFcStop={handlers.onToggleFcStop}
          onOverrideFcArrived={handlers.onOverrideFcArrived}
          onOpenCarrierRoutePrompt={handlers.onOpenCarrierRoutePrompt}
        />
      )}

      {/* Visit Status / Galactic Timestamp */}
      <WaypointTelemetryCell
        rowView={rowView}
        isVisited={permissions.isVisited}
        isNext={permissions.isNext}
        playerSystem={playerSystem}
      />

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
                allowInsertWaypoint={permissions.allowInsertWaypoint}
                deleteDisabledReason={permissions.deleteDisabledReason}
                onToggleInsertMenu={() => handlers.onToggleInsertMenu(row.id)}
                onInsert={(type) => handlers.onInsert(row.id, type)}
                onDelete={() => handlers.onDelete(row.id)}
              />
              <button
                type="button"
                disabled={!permissions.canReorder}
                draggable={permissions.canReorder}
                onPointerDown={() => {
                  if (permissions.canReorder) handlers.onHoldStart?.(index)
                }}
                onPointerUp={() => handlers.onHoldEnd?.()}
                onDragStart={(event) => {
                  if (permissions.canReorder) handlers.onDragStart(index, row.id, event)
                }}
                onDragEnd={handlers.onDragEnd}
                title={
                  permissions.reorderDisabledReason ||
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

