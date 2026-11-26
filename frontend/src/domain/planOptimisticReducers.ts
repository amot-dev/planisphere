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

function computeOptimisticFrontiers(
  expeditionStatus: string | undefined,
  rows: expedition.RowView[],
): { lastVisitedIndex: number; lastFcArrivedIndex: number } {
  if (expeditionStatus === 'planning') {
    return { lastVisitedIndex: -1, lastFcArrivedIndex: -1 }
  }
  let lastVisitedIndex = -1
  let lastFcArrivedIndex = -1
  rows.forEach((rowView, rowIndex) => {
    if (rowView.visitState === 'visited') lastVisitedIndex = rowIndex
    if (rowView.row.rowType === 'waypoint' && rowView.row.fcStop && rowView.row.fcArrived) {
      lastFcArrivedIndex = rowIndex
    }
  })
  return { lastVisitedIndex, lastFcArrivedIndex }
}

function applyOptimisticFcRowUpdate(
  plan: expedition.PlanView,
  targetRowId: number,
  resolveFlags: (row: expedition.Row) => { fcStop: boolean; fcArrived: boolean },
): expedition.PlanView {
  const status = plan.expedition.status
  const isPlanning = status === 'planning'
  const isCompleted = status === 'completed'
  let lastFcArrivedIndex = -1
  let nextFcStopRowId = 0
  let resolvedTargetFlags: { fcStop: boolean; fcArrived: boolean } | null = null

  plan.rows.forEach((rowView, rowIndex) => {
    if (rowView.row.rowType !== 'waypoint') return
    let fcStop = Boolean(rowView.row.fcStop)
    let fcArrived = Boolean(rowView.row.fcArrived)
    if (rowView.row.id === targetRowId) {
      resolvedTargetFlags = resolveFlags(rowView.row)
      fcStop = resolvedTargetFlags.fcStop
      fcArrived = resolvedTargetFlags.fcArrived
    }
    if (!isPlanning && fcStop && fcArrived) {
      lastFcArrivedIndex = rowIndex
    }
    if (fcStop && !fcArrived && nextFcStopRowId === 0) {
      nextFcStopRowId = rowView.row.id
    }
  })

  if (!resolvedTargetFlags) {
    return plan
  }
  const { fcStop, fcArrived } = resolvedTargetFlags
  const optimisticRows = plan.rows.map((rowView, rowIndex) => {
    if (rowView.row.id !== targetRowId) return rowView
    const nextRow = cloneWailsInstance(rowView.row, { fcStop, fcArrived })
    if (!rowView.permissions) {
      return cloneWailsInstance(rowView, { row: nextRow })
    }
    const isVisited = rowView.visitState === 'visited'
    const isOriginRow = !isPlanning && rowView.number === 1
    const isSpanshRouted = Boolean(rowView.permissions.isSpanshRoutedFcStop)
    const isFcStopLocked = Boolean(rowView.permissions.isFcStopLocked)
    const isFcArrivedStop = !isPlanning && fcStop && fcArrived
    const systemLocked =
      isCompleted || isVisited || isOriginRow || isSpanshRouted || isFcArrivedStop
    const systemLockReason = isSpanshRouted
      ? 'Locked while Fleet Carrier route to this waypoint is active'
      : isFcArrivedStop && !isVisited && !isOriginRow
        ? 'Locked while Fleet Carrier is marked arrived at this waypoint'
        : undefined
    return cloneWailsInstance(rowView, {
      row: nextRow,
      permissions: cloneWailsInstance(rowView.permissions, {
        systemLocked,
        systemLockReason,
        canAddFcStop: !isCompleted && !isFcStopLocked && (isPlanning || rowIndex > lastFcArrivedIndex),
        canRemoveFcStop: !isCompleted && !isFcStopLocked && (isPlanning || !fcArrived),
        canOverrideFcBadge: !isPlanning && !isCompleted && !isFcStopLocked,
        isNextFcStop: Boolean(
          fcStop && !fcArrived && !plan.activeCarrierRoute && nextFcStopRowId === targetRowId,
        ),
      }),
    })
  })
  return cloneWailsInstance(plan, {
    rows: optimisticRows,
    nextFcStopRowId,
    lastFcArrivedIndex,
  })
}

/**
 * buildOptimisticFcArrivedPlan returns an optimistic PlanView updating a
 * waypoint's Fleet Carrier arrival state, carrier permissions, and frontier
 * indices while preserving reference identity on all unaffected rows.
 */
export function buildOptimisticFcArrivedPlan(
  plan: expedition.PlanView,
  rowId: number,
  arrived: boolean,
): expedition.PlanView {
  return applyOptimisticFcRowUpdate(plan, rowId, (currentRow) => ({
    fcStop: Boolean(currentRow.fcStop),
    fcArrived: arrived,
  }))
}

/**
 * buildOptimisticFcStopTogglePlan returns an optimistic PlanView toggling a
 * waypoint's Fleet Carrier stop flag, carrier permissions, and frontier indices
 * while preserving reference identity on all unaffected rows.
 */
export function buildOptimisticFcStopTogglePlan(
  plan: expedition.PlanView,
  row: expedition.Row,
  nextFcStop: boolean,
): expedition.PlanView {
  return applyOptimisticFcRowUpdate(plan, row.id, (currentRow) => ({
    fcStop: nextFcStop,
    fcArrived: nextFcStop ? Boolean(currentRow.fcArrived) : false,
  }))
}

/**
 * buildOptimisticExpeditionModePlan returns an optimistic PlanView updating the
 * expedition's active flight mode and any active in-progress leg row while
 * preserving reference identity on unaffected rows.
 */
export function buildOptimisticExpeditionModePlan(
  plan: expedition.PlanView,
  modeId: number,
): expedition.PlanView {
  const pickedMode = (plan.modes ?? []).find((mode) => mode.id === modeId)
  const optimisticRows = plan.rows.map((rowView) => {
    if (rowView.visitState !== 'current') return rowView
    return cloneWailsInstance(rowView, {
      legModeId: modeId,
      legModeName: pickedMode?.name ?? rowView.legModeName,
      legModeColor: pickedMode?.color ?? rowView.legModeColor,
    })
  })
  return cloneWailsInstance(plan, {
    expedition: cloneWailsInstance(plan.expedition, { activeModeId: modeId }),
    rows: optimisticRows,
  })
}

/**
 * buildOptimisticLegModePlan returns an optimistic PlanView assigning a flight
 * mode override to a waypoint leg (and all subsequent visited legs when cascade
 * is true) while preserving unaffected rows.
 */
export function buildOptimisticLegModePlan(
  plan: expedition.PlanView,
  rowId: number,
  modeId: number,
  cascade = false,
): expedition.PlanView {
  const pickedMode = (plan.modes ?? []).find((mode) => mode.id === modeId)
  const targetIndex = plan.rows.findIndex((rowView) => rowView.row.id === rowId)
  if (targetIndex < 0) return plan

  const targetRowView = plan.rows[targetIndex]
  const isCurrentLeg = targetRowView.visitState === 'current'
  const optimisticRows = plan.rows.map((rowView, rowIndex) => {
    const matchesSingle = rowView.row.id === rowId
    const matchesCascade =
      cascade &&
      !isCurrentLeg &&
      rowIndex >= targetIndex &&
      rowView.row.rowType === 'waypoint' &&
      rowView.visitState === 'visited' &&
      rowView.number > 1
    if (!matchesSingle && !matchesCascade) return rowView
    return cloneWailsInstance(rowView, {
      legModeId: modeId,
      legModeName: pickedMode?.name ?? rowView.legModeName,
      legModeColor: pickedMode?.color ?? rowView.legModeColor,
    })
  })
  return cloneWailsInstance(plan, {
    expedition: isCurrentLeg
      ? cloneWailsInstance(plan.expedition, { activeModeId: modeId })
      : plan.expedition,
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
    ...computeOptimisticFrontiers(plan.expedition.status, renumberedRows),
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
    ...computeOptimisticFrontiers(plan.expedition.status, renumberedRows),
  })
}
