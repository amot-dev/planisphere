import type { DragEvent } from 'react'
import { expedition } from '../../../api/models'

/**
 * PlannerCollapsedVisitedRowProps defines the metadata for a contiguous span of
 * chronologically invalid visited rows that are collapsed while a waypoint is grabbed.
 */
export interface PlannerCollapsedVisitedRowProps {
  visibleColumnCount: number
  waypointCount: number
  regionCount: number
  firstWaypointNumber: number
  firstSystemName: string
  lastWaypointNumber: number
  lastSystemName: string
  onDragOver?: (event: DragEvent<HTMLTableRowElement>) => void
}

/**
 * PlannerCollapsedVisitedRow renders a compact summary bar replacing a run of
 * visited rows that have no valid or FC-blocked drop slots adjacent to them.
 */
export function PlannerCollapsedVisitedRow({
  visibleColumnCount,
  waypointCount,
  regionCount,
  firstWaypointNumber,
  firstSystemName,
  lastWaypointNumber,
  lastSystemName,
  onDragOver,
}: PlannerCollapsedVisitedRowProps) {
  const firstLabel = `#${String(firstWaypointNumber).padStart(2, '0')} ${firstSystemName || 'Unnamed'}`
  const lastLabel = `#${String(lastWaypointNumber).padStart(2, '0')} ${lastSystemName || 'Unnamed'}`
  const rangeLabel =
    waypointCount > 1
      ? `${firstLabel} → ${lastLabel}`
      : waypointCount === 1
        ? firstLabel
        : 'Region Headers'

  const countParts: string[] = []
  if (waypointCount > 0) {
    countParts.push(
      `${waypointCount} visited ${waypointCount === 1 ? 'waypoint' : 'waypoints'} collapsed`,
    )
  }
  if (regionCount > 0 && waypointCount === 0) {
    countParts.push(`${regionCount} ${regionCount === 1 ? 'region' : 'regions'} collapsed`)
  }
  const summarySuffix = countParts.length > 0 ? ` (${countParts.join(', ')})` : ''

  return (
    <tr
      onDragOver={onDragOver}
      data-collapsed-visited="true"
      className="bg-[#0b0806] text-[#6e5b4b] select-none"
    >
      <td
        colSpan={visibleColumnCount}
        className="px-4 py-1 text-center font-mono-tabular text-[10px] tracking-wider border-b border-[#24180e] border-l-2 border-l-[#3d2816]"
      >
        <span className="text-[#a6907c] font-bold">{`⋯ ${rangeLabel}`}</span>
        <span className="text-[#6e5b4b]">{`${summarySuffix} ⋯`}</span>
      </td>
    </tr>
  )
}

/**
 * PlannerDropSlotRowProps defines the gap index, preview metadata, and drag
 * handlers for an expanded dashed-border drop slot row in the planner table.
 */
export interface PlannerDropSlotRowProps {
  gapIndex: number
  visibleColumnCount: number
  grabbedSystemName: string
  preview: expedition.DropSlotPreview
  isDragTarget: boolean
  onDragOver: (event: DragEvent<HTMLTableRowElement>) => void
  onDrop: (event: DragEvent<HTMLTableRowElement>) => void
}

/**
 * PlannerDropSlotRow renders a dashed-border drop target gap in the visited zone
 * (cyan for valid historical slots, orange for Fleet Carrier stop order conflicts).
 */
export function PlannerDropSlotRow({
  gapIndex,
  visibleColumnCount,
  grabbedSystemName,
  preview,
  isDragTarget,
  onDragOver,
  onDrop,
}: PlannerDropSlotRowProps) {
  const isBlockedFc = preview.status === 'blocked_fc'
  const isUnvisitedSlot = preview.status === 'valid_unvisited'
  const displaySystem = grabbedSystemName.trim() || 'WAYPOINT'
  const leftColSpan = Math.max(1, visibleColumnCount - 2)

  if (isBlockedFc) {
    return (
      <tr
        onDragOver={onDragOver}
        data-drop-gap={gapIndex}
        data-drop-status="blocked_fc"
        className="bg-[#211307]/45 text-[#FF7100] select-none cursor-not-allowed"
      >
        <td
          colSpan={leftColSpan}
          className="px-4 py-1.5 align-middle border-y border-l border-dashed border-[#FF7100]"
        >
          <div className="h-6 flex items-center gap-2 font-mono-tabular text-[10px] uppercase tracking-wider">
            <span className="font-extrabold text-[#FF7100]">CARRIER STOP ORDER CONFLICT</span>
            <span className="text-[#d6c7b8] normal-case tracking-normal">
              Pending Fleet Carrier stop cannot precede an arrived stop
            </span>
          </div>
        </td>
        <td className="px-3 py-1.5 align-middle whitespace-nowrap border-y border-dashed border-[#FF7100]">
          <div className="h-6 flex items-center font-mono-tabular text-[10px]">
            <span className="px-1.5 py-0.5 font-extrabold uppercase tracking-wider bg-[#211307] text-[#FF7100] border border-[#FF7100]">
              FC STOP ORDER BLOCKED
            </span>
          </div>
        </td>
        <td className="w-[320px] px-3 py-1.5 align-middle border-y border-r border-dashed border-[#FF7100]" />
      </tr>
    )
  }

  const borderStyleClass = isDragTarget
    ? 'border-solid border-[#00D2FF]'
    : 'border-dashed border-[#00D2FF]/80'
  const rowBgClass = isDragTarget ? 'bg-[#08151c]/90' : 'bg-[#08151c]/35 hover:bg-[#08151c]/60'

  return (
    <tr
      onDragOver={onDragOver}
      onDrop={onDrop}
      data-drop-gap={gapIndex}
      data-drop-status={preview.status}
      className={`${rowBgClass} text-[#00D2FF] select-none transition-colors`}
    >
      <td
        colSpan={leftColSpan}
        className={`px-4 py-1.5 align-middle border-y border-l ${borderStyleClass}`}
      >
        <div className="h-6 flex items-center gap-2 font-mono-tabular text-[10px] uppercase tracking-wider">
          <span className="font-extrabold text-[#00D2FF]">
            {`+ DROP ${displaySystem} HERE`}
          </span>
          {preview.resetsFcArrived && (
            <span className="px-1.5 py-0.5 text-[9px] font-bold bg-[#211307] text-[#FF7100] border border-[#FF7100]/70">
              RESETS FC TO PENDING
            </span>
          )}
        </div>
      </td>
      <td className={`px-3 py-1.5 align-middle whitespace-nowrap border-y ${borderStyleClass}`}>
        <div className="h-6 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px]">
          <span className="px-1.5 py-0.5 font-extrabold uppercase tracking-wider bg-[#08151c] text-[#00D2FF] border border-[#00D2FF] shrink-0">
            {isUnvisitedSlot ? 'UNVISITED ROUTE' : 'LOG VISIT'}
          </span>
          {!isUnvisitedSlot && (
            <span className="text-[#00D2FF] font-bold shrink-0">{preview.visitedAtGalactic}</span>
          )}
        </div>
      </td>
      <td
        className={`w-[320px] px-3 py-1.5 align-middle text-right font-mono-tabular text-[10px] uppercase tracking-wider border-y border-r ${borderStyleClass}`}
      >
        {isDragTarget && <span className="font-bold text-[#00D2FF]">RELEASE TO DROP</span>}
      </td>
    </tr>
  )
}

