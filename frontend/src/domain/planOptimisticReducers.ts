import { expedition } from '../api/models'

function cloneWailsInstance<Model extends object>(
  instance: Model,
  overrides: Partial<Model>,
): Model {
  return Object.assign(
    Object.create(Object.getPrototypeOf(instance)) as Model,
    instance,
    overrides,
  )
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
 * waypoint's Fleet Carrier arrival state and recomputing nextFcStopRowId
 * while preserving reference identity on all unaffected rows.
 */
export function buildOptimisticFcArrivedPlan(
  plan: expedition.PlanView,
  rowId: number,
  arrived: boolean,
): expedition.PlanView {
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.row.id !== rowId) return rowView
    return cloneWailsInstance(rowView, {
      row: cloneWailsInstance(rowView.row, { fcArrived: arrived }),
    })
  })
  return cloneWailsInstance(plan, {
    rows: optimisticRows,
    nextFcStopRowId: findNextUnarrivedCarrierStopRowId(optimisticRows),
  })
}

/**
 * buildOptimisticFcStopTogglePlan returns an optimistic PlanView toggling a
 * waypoint's Fleet Carrier stop flag and recomputing nextFcStopRowId while
 * preserving reference identity on all unaffected rows.
 */
export function buildOptimisticFcStopTogglePlan(
  plan: expedition.PlanView,
  row: expedition.Row,
  nextFcStop: boolean,
): expedition.PlanView {
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.row.id !== row.id) return rowView
    return cloneWailsInstance(rowView, {
      row: cloneWailsInstance(rowView.row, {
        fcStop: nextFcStop,
        fcArrived: nextFcStop ? rowView.row.fcArrived : false,
      }),
    })
  })
  return cloneWailsInstance(plan, {
    rows: optimisticRows,
    nextFcStopRowId: findNextUnarrivedCarrierStopRowId(optimisticRows),
  })
}

/**
 * buildOptimisticLegModePlan returns an optimistic PlanView assigning a flight
 * mode override to a visited waypoint leg while preserving unaffected rows.
 */
export function buildOptimisticLegModePlan(
  plan: expedition.PlanView,
  rowId: number,
  modeId: number,
): expedition.PlanView {
  const pickedMode = (plan.modes ?? []).find((mode) => mode.id === modeId)
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.row.id !== rowId) return rowView
    return cloneWailsInstance(rowView, {
      legModeId: modeId,
      legModeName: pickedMode?.name ?? rowView.legModeName,
      legModeColor: pickedMode?.color ?? rowView.legModeColor,
    })
  })
  return cloneWailsInstance(plan, {
    rows: optimisticRows,
  })
}

/**
 * buildOptimisticDeletedRowPlan returns an optimistic PlanView removing a row
 * and renumbering only subsequent waypoints whose sequence numbers shift.
 */
export function buildOptimisticDeletedRowPlan(
  plan: expedition.PlanView,
  rowId: number,
): expedition.PlanView {
  const deletedIndex = plan.rows.findIndex((rowView) => rowView.row.id === rowId)
  if (deletedIndex < 0) {
    return plan
  }
  const deletedRowWasWaypoint = plan.rows[deletedIndex].row.rowType === 'waypoint'
  let waypointCount = 0
  const renumberedRows: expedition.RowView[] = new Array(plan.rows.length - 1)
  let targetIndex = 0

  for (let sourceIndex = 0; sourceIndex < plan.rows.length; sourceIndex++) {
    if (sourceIndex === deletedIndex) continue
    const rowView = plan.rows[sourceIndex]
    if (rowView.row.rowType === 'waypoint') {
      waypointCount++
      if (deletedRowWasWaypoint && sourceIndex > deletedIndex && rowView.number !== waypointCount) {
        renumberedRows[targetIndex++] = cloneWailsInstance(rowView, { number: waypointCount })
        continue
      }
    }
    renumberedRows[targetIndex++] = rowView
  }

  return cloneWailsInstance(plan, {
    rows: renumberedRows,
    waypointCount,
    nextFcStopRowId: findNextUnarrivedCarrierStopRowId(renumberedRows),
  })
}

/**
 * buildOptimisticMovedRowPlan returns an optimistic PlanView moving a row to a
 * new index and cloning only rows whose position or waypoint number changes.
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
    const expectedPosition = rowIndex + 1
    const isWaypoint = rowView.row.rowType === 'waypoint'
    if (isWaypoint) {
      waypointCount++
    }
    const expectedNumber = isWaypoint ? waypointCount : rowView.number
    if (rowView.row.position === expectedPosition && rowView.number === expectedNumber) {
      return rowView
    }
    const updatedRow =
      rowView.row.position === expectedPosition
        ? rowView.row
        : cloneWailsInstance(rowView.row, { position: expectedPosition })
    return cloneWailsInstance(rowView, {
      row: updatedRow,
      number: expectedNumber,
    })
  })
  return cloneWailsInstance(plan, {
    rows: renumberedRows,
    nextFcStopRowId: findNextUnarrivedCarrierStopRowId(renumberedRows),
  })
}
