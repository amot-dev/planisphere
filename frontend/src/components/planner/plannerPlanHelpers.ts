import { expedition } from '../../../wailsjs/go/models'

/**
 * PlannerTableItem represents a rendered row in the flattened expedition flight
 * plan table (galactic region header, waypoint row, or expanded telemetry drawer).
 */
export type PlannerTableItem =
  | { kind: 'region'; rowView: expedition.RowView; index: number; key: string }
  | { kind: 'waypoint'; rowView: expedition.RowView; index: number; key: string }
  | { kind: 'drawer'; rowView: expedition.RowView; index: number; key: string }

const ALWAYS_VISIBLE_COLUMN_COUNT = 5
const LEG_EXPAND_COLUMN_WIDTH_PIXELS = 32
const WAYPOINT_NUMBER_COLUMN_WIDTH_PIXELS = 40
const STAR_SYSTEM_COLUMN_WIDTH_PIXELS = 240
const POI_BODY_COLUMN_WIDTH_PIXELS = 192
const FLIGHT_NOTES_COLUMN_WIDTH_PIXELS = 224
const CARRIER_STOP_COLUMN_WIDTH_PIXELS = 160
const TELEMETRY_COLUMN_WIDTH_PIXELS = 240
const CONTROLS_COLUMN_WIDTH_PIXELS = 380

/**
 * computePlannerTableDimensions calculates the visible column count and minimum
 * horizontal table width in pixels based on planning state and column toggles.
 */
export function computePlannerTableDimensions(
  isPlanning: boolean,
  showNotes: boolean,
  showCarrierStop: boolean,
): { visibleColumnCount: number; tableMinWidthPixels: number } {
  const visibleColumnCount =
    ALWAYS_VISIBLE_COLUMN_COUNT +
    (isPlanning ? 0 : 1) +
    (showNotes ? 1 : 0) +
    (showCarrierStop ? 1 : 0)

  const tableMinWidthPixels =
    (isPlanning ? 0 : LEG_EXPAND_COLUMN_WIDTH_PIXELS) +
    WAYPOINT_NUMBER_COLUMN_WIDTH_PIXELS +
    STAR_SYSTEM_COLUMN_WIDTH_PIXELS +
    POI_BODY_COLUMN_WIDTH_PIXELS +
    (showNotes ? FLIGHT_NOTES_COLUMN_WIDTH_PIXELS : 0) +
    (showCarrierStop ? CARRIER_STOP_COLUMN_WIDTH_PIXELS : 0) +
    TELEMETRY_COLUMN_WIDTH_PIXELS +
    CONTROLS_COLUMN_WIDTH_PIXELS

  return { visibleColumnCount, tableMinWidthPixels }
}

/**
 * buildPlannerTableItems flattens an expedition plan's rows and any expanded
 * visited waypoint telemetry drawers into a single renderable sequence.
 */
export function buildPlannerTableItems(
  plan: expedition.PlanView | null,
  expandedRowIds: Record<number, boolean>,
  isPlanning: boolean,
): PlannerTableItem[] {
  if (!plan || !plan.rows) return []
  const items: PlannerTableItem[] = []
  ;(plan.rows ?? []).forEach((rowView, index) => {
    if (!rowView?.row) return
    const row = rowView.row
    if (row.rowType === 'region') {
      items.push({ kind: 'region', rowView, index, key: `region-${row.id}` })
    } else {
      const isVisited = rowView.visitState === 'visited' || rowView.visitState === 'current'
      items.push({ kind: 'waypoint', rowView, index, key: `row-${row.id}` })
      const isExpanded = Boolean(expandedRowIds[row.id])
      if (!isPlanning && isVisited && isExpanded) {
        items.push({ kind: 'drawer', rowView, index, key: `drawer-${row.id}` })
      }
    }
  })
  return items
}

function findNextUnarrivedCarrierStopRowId(rows: expedition.RowView[]): number {
  for (const rowView of rows) {
    if (rowView.row.rowType === 'waypoint' && rowView.row.fcStop && !rowView.row.fcArrived) {
      return rowView.row.id
    }
  }
  return 0
}

/**
 * buildOptimisticFcArrivedPlan returns an optimistic PlanView updating a
 * waypoint's Fleet Carrier arrival state and recomputing nextFcStopRowId.
 */
export function buildOptimisticFcArrivedPlan(
  plan: expedition.PlanView,
  rowId: number,
  arrived: boolean,
): expedition.PlanView {
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.row.id !== rowId) return rowView
    return expedition.RowView.createFrom({
      ...rowView,
      row: { ...rowView.row, fcArrived: arrived },
    })
  })
  return expedition.PlanView.createFrom({
    ...plan,
    rows: optimisticRows,
    nextFcStopRowId: findNextUnarrivedCarrierStopRowId(optimisticRows),
  })
}

/**
 * buildOptimisticFcStopTogglePlan returns an optimistic PlanView toggling a
 * waypoint's Fleet Carrier stop flag and recomputing nextFcStopRowId.
 */
export function buildOptimisticFcStopTogglePlan(
  plan: expedition.PlanView,
  row: expedition.Row,
  nextFcStop: boolean,
): expedition.PlanView {
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.row.id !== row.id) return rowView
    return expedition.RowView.createFrom({
      ...rowView,
      row: {
        ...rowView.row,
        fcStop: nextFcStop,
        fcArrived: nextFcStop ? rowView.row.fcArrived : false,
      },
    })
  })
  return expedition.PlanView.createFrom({
    ...plan,
    rows: optimisticRows,
    nextFcStopRowId: findNextUnarrivedCarrierStopRowId(optimisticRows),
  })
}

/**
 * buildOptimisticLegModePlan returns an optimistic PlanView assigning a flight
 * mode override to a visited waypoint leg.
 */
export function buildOptimisticLegModePlan(
  plan: expedition.PlanView,
  rowId: number,
  modeId: number,
): expedition.PlanView {
  const pickedMode = (plan.modes ?? []).find((mode) => mode.id === modeId)
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.row.id !== rowId) return rowView
    return expedition.RowView.createFrom({
      ...rowView,
      legModeId: modeId,
      legModeName: pickedMode?.name ?? rowView.legModeName,
      legModeColor: pickedMode?.color ?? rowView.legModeColor,
    })
  })
  return expedition.PlanView.createFrom({
    ...plan,
    rows: optimisticRows,
  })
}

/**
 * buildOptimisticActiveLegModePlan returns an optimistic PlanView updating the
 * active open-ended leg's flight mode.
 */
export function buildOptimisticActiveLegModePlan(
  plan: expedition.PlanView,
  modeId: number,
): expedition.PlanView {
  const pickedMode = (plan.modes ?? []).find((mode) => mode.id === modeId)
  return expedition.PlanView.createFrom({
    ...plan,
    activeModeId: modeId,
    activeModeName: pickedMode?.name ?? plan.activeModeName,
    activeModeColor: pickedMode?.color ?? plan.activeModeColor,
  })
}

/**
 * buildOptimisticDeletedRowPlan returns an optimistic PlanView removing a row
 * and renumbering remaining waypoints sequentially.
 */
export function buildOptimisticDeletedRowPlan(
  plan: expedition.PlanView,
  rowId: number,
): expedition.PlanView {
  const filteredRows = plan.rows.filter((rowView) => rowView.row.id !== rowId)
  let waypointCount = 0
  const renumberedRows = filteredRows.map((rowView) => {
    if (rowView.row.rowType === 'waypoint') {
      waypointCount++
      return expedition.RowView.createFrom({ ...rowView, number: waypointCount })
    }
    return rowView
  })
  return expedition.PlanView.createFrom({
    ...plan,
    rows: renumberedRows,
    waypointCount,
  })
}

/**
 * buildOptimisticMovedRowPlan returns an optimistic PlanView moving a row to a
 * new index and renumbering waypoint sequence numbers and row positions.
 */
export function buildOptimisticMovedRowPlan(
  plan: expedition.PlanView,
  fromIndex: number,
  toIndex: number,
): expedition.PlanView {
  const nextRows = [...plan.rows]
  const [movedRow] = nextRows.splice(fromIndex, 1)
  nextRows.splice(toIndex, 0, movedRow)
  let waypointCount = 0
  const renumberedRows = nextRows.map((rowView, rowIndex) => {
    const updatedRow = expedition.Row.createFrom({
      ...rowView.row,
      position: rowIndex + 1,
    })
    if (rowView.row.rowType === 'waypoint') {
      waypointCount++
      return expedition.RowView.createFrom({
        ...rowView,
        row: updatedRow,
        number: waypointCount,
      })
    }
    return expedition.RowView.createFrom({ ...rowView, row: updatedRow })
  })
  return expedition.PlanView.createFrom({
    ...plan,
    rows: renumberedRows,
  })
}

/**
 * PlanMutationContextCallbacks supplies the busy banner, optimistic PlanView
 * setter, and rollback refresh handler shared across planner mutations.
 */
export interface PlanMutationContextCallbacks {
  onBusyChange: (busyMessage: string | null) => void
  onPlanOptimisticUpdate: (nextPlan: expedition.PlanView) => void
  onRefreshPlan: () => void | Promise<void>
}

/**
 * PlanMutationBehaviorOptions configures optional optimistic PlanView state,
 * post-success refresh, and rollback behavior for a single planner mutation.
 */
export interface PlanMutationBehaviorOptions {
  optimisticPlan?: expedition.PlanView
  onSuccess?: () => void
  rollbackOnError?: boolean
}

/**
 * executePlanMutation applies an optional optimistic PlanView update, runs a
 * backend planner RPC under a busy status banner, and rolls back via
 * onRefreshPlan if the RPC rejects.
 */
export async function executePlanMutation(
  busyMessage: string,
  errorContext: string,
  mutationAction: () => Promise<unknown>,
  contextCallbacks: PlanMutationContextCallbacks,
  options?: PlanMutationBehaviorOptions,
): Promise<void> {
  if (options?.optimisticPlan) {
    contextCallbacks.onPlanOptimisticUpdate(options.optimisticPlan)
  }
  contextCallbacks.onBusyChange(busyMessage)
  try {
    await mutationAction()
    options?.onSuccess?.()
  } catch (mutationError) {
    console.error(`${errorContext}:`, mutationError)
    if (options?.optimisticPlan || options?.rollbackOnError) {
      await contextCallbacks.onRefreshPlan()
    }
  } finally {
    contextCallbacks.onBusyChange(null)
  }
}

