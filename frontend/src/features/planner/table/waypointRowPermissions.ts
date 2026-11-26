import { expedition } from '../../../api/models'
import { getWaypointLegBorderColor } from '../../../domain/planTableItems'

/**
 * WaypointRowPermissions encapsulates presentation styling, tooltip titles,
 * and backend-resolved row action flags for a waypoint row.
 */
export interface WaypointRowPermissions {
  isVisited: boolean
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
  isNextFcStop: boolean
  isFcStopLocked: boolean
  hasActiveCarrierRoute: boolean
  rowDropHighlightClass: string
}

/**
 * DeriveWaypointRowPermissionsOptions supplies row view state and transient
 * drag and hover flags needed to format waypoint row presentation.
 */
export interface DeriveWaypointRowPermissionsOptions {
  rowView: expedition.RowView
  isPlanning: boolean
  isBeingDragged: boolean
  topGapHighlight: boolean
  bottomGapHighlight: boolean
  isNextWaypoint?: boolean
  hasOverrideFcArrived: boolean
}

/**
 * deriveWaypointRowPermissions maps Go-computed WaypointPermissions and
 * transient drag highlight flags into render props for WaypointRowItem.
 */
export function deriveWaypointRowPermissions({
  rowView,
  isPlanning,
  isBeingDragged,
  topGapHighlight,
  bottomGapHighlight,
  isNextWaypoint,
  hasOverrideFcArrived,
}: DeriveWaypointRowPermissionsOptions): WaypointRowPermissions {
  const row = rowView.row
  const waypointPermissions = rowView.permissions
  const isVisited = rowView.visitState === 'visited'
  const legBorderColor = getWaypointLegBorderColor(rowView, isPlanning)
  const hasLegBorder = Boolean(legBorderColor)
  const systemLockTitle = waypointPermissions?.systemLockReason || row.systemName
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
    legBorderColor,
    hasLegBorder,
    isSystemLocked: Boolean(waypointPermissions?.systemLocked),
    allowInsertWaypoint: Boolean(
      waypointPermissions?.allowInsertWaypoint ?? rowView.allowInsertWaypoint,
    ),
    canReorder: Boolean(waypointPermissions?.canReorder),
    deleteDisabledReason: waypointPermissions?.deleteDisabledReason || undefined,
    reorderDisabledReason: waypointPermissions?.reorderDisabledReason || undefined,
    systemLockTitle,
    canAddFcStop: Boolean(waypointPermissions?.canAddFcStop),
    canRemoveFcStop: Boolean(waypointPermissions?.canRemoveFcStop),
    canOverrideFcBadge: Boolean(waypointPermissions?.canOverrideFcBadge && hasOverrideFcArrived),
    hasDrawer: Boolean(waypointPermissions?.hasDrawer),
    isNext,
    isNextFcStop: Boolean(waypointPermissions?.isNextFcStop),
    isFcStopLocked: Boolean(waypointPermissions?.isFcStopLocked),
    hasActiveCarrierRoute: Boolean(waypointPermissions?.hasActiveCarrierRoute),
    rowDropHighlightClass,
  }
}
