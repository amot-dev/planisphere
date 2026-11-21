import { expedition } from '../../api/models'

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

