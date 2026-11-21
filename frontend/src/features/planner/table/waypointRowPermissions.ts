import { expedition } from '../../../api/models'
import { getWaypointLegBorderColor } from '../../../domain/planTableItems'

/**
 * WaypointRowPermissions encapsulates computed edit/drag/carrier permissions,
 * tooltip reasons, leg border styling, and row highlight classes for a waypoint row.
 */
export interface WaypointRowPermissions {
  isVisited: boolean
  isOriginRow: boolean
  legBorderColor: string | undefined
  hasLegBorder: boolean
  isSystemLocked: boolean
  allowInsertWaypoint: boolean
  canReorder: boolean
  deleteDisabledReason: string | undefined
  reorderDisabledReason: string | undefined
  systemLockTitle: string
  canAddFcStop: boolean
  canRemoveFcStop: boolean
  canOverrideFcBadge: boolean
  hasDrawer: boolean
  isNext: boolean
  rowDropHighlightClass: string
}

/**
 * DeriveWaypointRowPermissionsOptions supplies raw row flags, expedition phase,
 * and active carrier router state needed to compute waypoint row permissions.
 */
export interface DeriveWaypointRowPermissionsOptions {
  rowView: expedition.RowView
  index: number
  isPlanning: boolean
  isLocked: boolean
  isFcStopLocked: boolean
  isActivelyRoutedFcStop: boolean
  lastFcArrivedIndex: number
  lastVisitedIndex: number
  isBeingDragged: boolean
  topGapHighlight: boolean
  bottomGapHighlight: boolean
  isNextWaypoint?: boolean
  hasOverrideFcArrived: boolean
}

/**
 * deriveWaypointRowPermissions computes all chronological edit locks, carrier
 * stop permissions, tooltip explanations, and drop highlight classes for a waypoint row.
 */
export function deriveWaypointRowPermissions({
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
  hasOverrideFcArrived,
}: DeriveWaypointRowPermissionsOptions): WaypointRowPermissions {
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
  const canOverrideFcBadge = !isPlanning && !isLocked && !isFcStopLocked && hasOverrideFcArrived

  const hasDrawer = !isPlanning && rowView.visitState !== 'future'
  const isNext =
    Boolean(isNextWaypoint ?? (rowView.visitState === 'current' || rowView.isNextWaypoint)) &&
    !isPlanning

  const rowDropHighlightClass = isBeingDragged
    ? 'bg-ed-amber-drag *:bg-ed-amber-drag *:shadow-[inset_0_2px_0_0_var(--color-ed-amber),inset_0_-2px_0_0_var(--color-ed-amber)]'
    : topGapHighlight
      ? '*:shadow-[inset_0_2px_0_0_var(--color-ed-cyan)]'
      : bottomGapHighlight
        ? '*:shadow-[inset_0_-2px_0_0_var(--color-ed-cyan)]'
        : ''

  return {
    isVisited,
    isOriginRow,
    legBorderColor,
    hasLegBorder,
    isSystemLocked,
    allowInsertWaypoint,
    canReorder,
    deleteDisabledReason,
    reorderDisabledReason,
    systemLockTitle,
    canAddFcStop,
    canRemoveFcStop,
    canOverrideFcBadge,
    hasDrawer,
    isNext,
    rowDropHighlightClass,
  }
}

