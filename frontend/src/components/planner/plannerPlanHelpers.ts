import { expedition } from '../../../wailsjs/go/models'

/**
 * PlannerTableItem represents a rendered row in the flattened expedition flight
 * plan table (galactic region header, waypoint row, expanded telemetry drawer,
 * collapsed visited-rows summary bar, or dashed drop-slot gap row).
 */
export type PlannerTableItem =
  | { kind: 'region'; rowView: expedition.RowView; index: number; key: string }
  | { kind: 'waypoint'; rowView: expedition.RowView; index: number; key: string }
  | { kind: 'drawer'; rowView: expedition.RowView; index: number; key: string }
  | {
      kind: 'collapsed_visited'
      startIndex: number
      endIndex: number
      waypointCount: number
      regionCount: number
      firstWaypointNumber: number
      firstSystemName: string
      lastWaypointNumber: number
      lastSystemName: string
      key: string
    }
  | {
      kind: 'drop_slot'
      gapIndex: number
      targetIndex: number
      preview: expedition.DropSlotPreview
      key: string
    }

/**
 * gapToTargetIndex converts a gap position (0..rows.length, where gap g sits
 * immediately before row g) into the 0-based target index expected by MoveRow
 * after removing the source row at fromIndex. Returns null if dropping into
 * gapIndex leaves the row at its current position.
 */
export function gapToTargetIndex(fromIndex: number, gapIndex: number): number | null {
  if (gapIndex === fromIndex || gapIndex === fromIndex + 1) return null
  return gapIndex < fromIndex ? gapIndex : gapIndex - 1
}

const ALWAYS_VISIBLE_COLUMN_COUNT = 5
const LEG_EXPAND_COLUMN_WIDTH_PIXELS = 32
const WAYPOINT_NUMBER_COLUMN_WIDTH_PIXELS = 40
const STAR_SYSTEM_COLUMN_WIDTH_PIXELS = 240
const POI_BODY_COLUMN_WIDTH_PIXELS = 192
const FLIGHT_NOTES_COLUMN_WIDTH_PIXELS = 224
const CARRIER_STOP_COLUMN_WIDTH_PIXELS = 160
const TELEMETRY_COLUMN_WIDTH_PIXELS = 240
const CONTROLS_COLUMN_WIDTH_PIXELS = 320

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

function buildCollapsedVisitedItem(
  rows: expedition.RowView[],
  startIndex: number,
  endIndex: number,
): PlannerTableItem {
  let waypointCount = 0
  let regionCount = 0
  let firstWaypointNumber = 0
  let firstSystemName = ''
  let lastWaypointNumber = 0
  let lastSystemName = ''

  for (let rowIndex = startIndex; rowIndex <= endIndex; rowIndex++) {
    const rowView = rows[rowIndex]
    if (!rowView?.row) continue
    if (rowView.row.rowType === 'waypoint') {
      waypointCount++
      if (firstWaypointNumber === 0) {
        firstWaypointNumber = rowView.number
        firstSystemName = rowView.row.systemName ?? ''
      }
      lastWaypointNumber = rowView.number
      lastSystemName = rowView.row.systemName ?? ''
    } else {
      regionCount++
    }
  }

  return {
    kind: 'collapsed_visited',
    startIndex,
    endIndex,
    waypointCount,
    regionCount,
    firstWaypointNumber,
    firstSystemName,
    lastWaypointNumber,
    lastSystemName,
    key: `collapsed-${startIndex}-${endIndex}`,
  }
}

function markFlankingVisitedWaypointsVisible(
  rows: expedition.RowView[],
  visibleSet: Set<number>,
  prevStartIndex: number,
  nextStartIndex: number,
  lastVisitedIndex: number,
): void {
  let previousWaypointIndex = Math.max(0, prevStartIndex)
  while (previousWaypointIndex > 0 && rows[previousWaypointIndex]?.row?.rowType !== 'waypoint') {
    previousWaypointIndex--
  }
  let nextWaypointIndex = Math.min(Math.max(0, nextStartIndex), lastVisitedIndex)
  while (nextWaypointIndex < lastVisitedIndex && rows[nextWaypointIndex]?.row?.rowType !== 'waypoint') {
    nextWaypointIndex++
  }
  for (
    let visibleIndex = Math.max(0, previousWaypointIndex);
    visibleIndex <= Math.min(lastVisitedIndex, nextWaypointIndex);
    visibleIndex++
  ) {
    visibleSet.add(visibleIndex)
  }
}

function collectVisitedDropSlotsAndVisibleRows(
  rows: expedition.RowView[],
  heldIndex: number,
  lastVisitedIndex: number,
  activeDropPreviews: expedition.DropSlotPreview[],
): {
  slotsByGap: Map<number, { targetIndex: number; preview: expedition.DropSlotPreview }>
  visibleVisitedRowIndices: Set<number>
} {
  const slotsByGap = new Map<number, { targetIndex: number; preview: expedition.DropSlotPreview }>()
  const visibleVisitedRowIndices = new Set<number>()
  if (heldIndex <= lastVisitedIndex) {
    markFlankingVisitedWaypointsVisible(
      rows,
      visibleVisitedRowIndices,
      heldIndex - 1,
      heldIndex + 1,
      lastVisitedIndex,
    )
  }

  const hasAnyUnvisitedWaypoint = rows.some(
    (rowView, rowIndex) => rowIndex > lastVisitedIndex && rowView?.row?.rowType === 'waypoint',
  )
  for (let gapIndex = 1; gapIndex <= rows.length; gapIndex++) {
    const targetIndex = gapToTargetIndex(heldIndex, gapIndex)
    if (targetIndex === null) continue
    const preview = activeDropPreviews[targetIndex]
    if (!preview) continue
    // When every waypoint is visited there are no unvisited rows below to host
    // normal hover insertion gaps (and visited rows reject hover-gap drops), so
    // an explicit tail drop slot lets a dragged visited waypoint append as
    // unvisited and keeps the final visited waypoint uncollapsed above it.
    const isEndOfAllVisitedTable =
      gapIndex === rows.length &&
      !hasAnyUnvisitedWaypoint &&
      preview.status === 'valid_unvisited'
    if (
      preview.status === 'valid_visited' ||
      preview.status === 'blocked_fc' ||
      isEndOfAllVisitedTable
    ) {
      slotsByGap.set(gapIndex, { targetIndex, preview })
      markFlankingVisitedWaypointsVisible(
        rows,
        visibleVisitedRowIndices,
        gapIndex - 1,
        gapIndex,
        lastVisitedIndex,
      )
    }
  }

  return { slotsByGap, visibleVisitedRowIndices }
}

function buildGrabbedWaypointTableItems(
  rows: expedition.RowView[],
  heldIndex: number,
  lastVisitedIndex: number,
  activeDropPreviews: expedition.DropSlotPreview[],
): PlannerTableItem[] {
  const { slotsByGap, visibleVisitedRowIndices } = collectVisitedDropSlotsAndVisibleRows(
    rows,
    heldIndex,
    lastVisitedIndex,
    activeDropPreviews,
  )
  const items: PlannerTableItem[] = []
  let collapseStart: number | null = null

  for (let index = 0; index < rows.length; index++) {
    const slotBefore = slotsByGap.get(index)
    if (slotBefore) {
      if (collapseStart !== null) {
        items.push(buildCollapsedVisitedItem(rows, collapseStart, index - 1))
        collapseStart = null
      }
      items.push({
        kind: 'drop_slot',
        gapIndex: index,
        targetIndex: slotBefore.targetIndex,
        preview: slotBefore.preview,
        key: `drop-slot-${index}`,
      })
    }

    const rowView = rows[index]
    if (!rowView?.row) continue
    const isVisitedZone = index <= lastVisitedIndex
    if (isVisitedZone && !visibleVisitedRowIndices.has(index)) {
      if (collapseStart === null) collapseStart = index
      continue
    }

    if (collapseStart !== null) {
      items.push(buildCollapsedVisitedItem(rows, collapseStart, index - 1))
      collapseStart = null
    }

    items.push({
      kind: rowView.row.rowType === 'region' ? 'region' : 'waypoint',
      rowView,
      index,
      key: `${rowView.row.rowType === 'region' ? 'region' : 'row'}-${rowView.row.id}`,
    })
  }

  if (collapseStart !== null) {
    items.push(buildCollapsedVisitedItem(rows, collapseStart, lastVisitedIndex))
  }

  const slotAtTableEnd = slotsByGap.get(rows.length)
  if (slotAtTableEnd) {
    items.push({
      kind: 'drop_slot',
      gapIndex: rows.length,
      targetIndex: slotAtTableEnd.targetIndex,
      preview: slotAtTableEnd.preview,
      key: `drop-slot-${rows.length}`,
    })
  }

  return items
}

/**
 * buildPlannerTableItems flattens an expedition plan's rows and any expanded
 * waypoint telemetry drawers into a single renderable sequence. When a waypoint
 * is grabbed in an active expedition, invalid visited stretches are collapsed
 * and dashed drop-slot rows are inserted at valid or FC-blocked historical gaps.
 */
export function buildPlannerTableItems(
  plan: expedition.PlanView | null,
  expandedRowIds: Record<number, boolean>,
  isPlanning: boolean,
  heldOrDraggedIndex: number | null = null,
  lastVisitedIndex = -1,
  activeDropPreviews: expedition.DropSlotPreview[] | null = null,
): PlannerTableItem[] {
  if (!plan || !plan.rows) return []
  const rows = plan.rows ?? []
  if (
    !isPlanning &&
    heldOrDraggedIndex !== null &&
    heldOrDraggedIndex >= 0 &&
    heldOrDraggedIndex < rows.length &&
    rows[heldOrDraggedIndex]?.row?.rowType === 'waypoint' &&
    lastVisitedIndex >= 0 &&
    activeDropPreviews !== null &&
    activeDropPreviews.length > 0
  ) {
    return buildGrabbedWaypointTableItems(
      rows,
      heldOrDraggedIndex,
      lastVisitedIndex,
      activeDropPreviews,
    )
  }

  const items: PlannerTableItem[] = []
  rows.forEach((rowView, index) => {
    if (!rowView?.row) return
    const row = rowView.row
    if (row.rowType === 'region') {
      items.push({ kind: 'region', rowView, index, key: `region-${row.id}` })
    } else {
      items.push({ kind: 'waypoint', rowView, index, key: `row-${row.id}` })
      const isExpanded = Boolean(expandedRowIds[row.id])
      if (!isPlanning && rowView.visitState !== 'future' && isExpanded) {
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

