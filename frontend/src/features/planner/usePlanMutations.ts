import { useCallback, useState, type MutableRefObject } from 'react'
import {
  AddRow,
  DeleteRow,
  MoveRow,
  OverrideCarrierStopArrival,
  SetLegMode,
  UpdateRow,
} from '../../api/backend'
import { expedition } from '../../api/models'
import {
  buildOptimisticDeletedRowPlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
} from '../../domain/planOptimisticReducers'
import type { PlannerConfirmState } from './PlannerConfirmModal'

const DEFAULT_GALACTIC_REGION_ID = 18 // Inner Orion Spur

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

/**
 * CarrierMutationLockContext exposes active carrier router bounds checked when
 * toggling carrier stops or overriding arrival state.
 */
export interface CarrierMutationLockContext {
  activeCarrierRoute: expedition.CarrierRouteResult | null
  isManualCarrierRoute: boolean
  routedCarrierStopIndex: number
  onCancelCarrierRoute: () => void
}

/**
 * UsePlanMutationsOptions configures plan state, optimistic update callbacks,
 * and carrier lock state for expedition planner mutations.
 */
export interface UsePlanMutationsOptions {
  plan: expedition.PlanView | null
  isPlanning: boolean
  onBusyChange: (busyMessage: string | null) => void
  applyOptimisticPlan: (nextPlan: expedition.PlanView) => void
  onRefreshPlan: () => void
  carrierContextRef: MutableRefObject<CarrierMutationLockContext>
}

/**
 * usePlanMutations encapsulates planner row CRUD, reordering, carrier stop
 * toggles, confirmation modal state, and lifecycle RPC actions.
 */
export function usePlanMutations({
  plan,
  isPlanning,
  onBusyChange,
  applyOptimisticPlan,
  onRefreshPlan,
  carrierContextRef,
}: UsePlanMutationsOptions) {
  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)
  const [confirmState, setConfirmState] = useState<PlannerConfirmState | null>(null)

  const runPlanMutation = useCallback(
    (
      busyMessage: string,
      errorContext: string,
      mutationAction: () => Promise<unknown>,
      options?: PlanMutationBehaviorOptions,
    ) =>
      executePlanMutation(
        busyMessage,
        errorContext,
        mutationAction,
        { onBusyChange, onPlanOptimisticUpdate: applyOptimisticPlan, onRefreshPlan },
        options,
      ),
    [applyOptimisticPlan, onBusyChange, onRefreshPlan],
  )

  const setFcArrived = useCallback(
    async (rowId: number, arrived: boolean) => {
      if (!plan) return
      await runPlanMutation(
        'UPDATING CARRIER ARRIVAL',
        'Failed to update carrier arrival',
        () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, fcArrived: arrived })),
        { optimisticPlan: buildOptimisticFcArrivedPlan(plan, rowId, arrived) },
      )
    },
    [plan, runPlanMutation],
  )

  const commitField = useCallback(
    async (row: expedition.Row, field: 'systemName' | 'poiName' | 'notes', value: string) => {
      if ((row[field] ?? '') === value) return
      await runPlanMutation(
        'UPDATING WAYPOINT',
        `Failed to update waypoint ${field}`,
        () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, [field]: value })),
        { rollbackOnError: true },
      )
    },
    [runPlanMutation],
  )

  const toggleFcStop = useCallback(
    async (row: expedition.Row) => {
      if (!plan) return
      const carrierContext = carrierContextRef.current
      if (carrierContext.activeCarrierRoute && carrierContext.routedCarrierStopIndex >= 0) {
        const rowIndex = plan.rows.findIndex((rowView) => rowView?.row?.id === row.id)
        if (
          rowIndex >= 0 &&
          (carrierContext.isManualCarrierRoute
            ? rowIndex < carrierContext.routedCarrierStopIndex
            : rowIndex <= carrierContext.routedCarrierStopIndex)
        ) {
          return
        }
      }
      const nextFcStop = !row.fcStop
      await runPlanMutation(
        'UPDATING FC STOP',
        'Failed to toggle fleet carrier stop',
        () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, fcStop: nextFcStop })),
        { optimisticPlan: buildOptimisticFcStopTogglePlan(plan, row, nextFcStop) },
      )
    },
    [carrierContextRef, plan, runPlanMutation],
  )

  const handleRequestOverrideFcArrived = useCallback(
    (rowView: expedition.RowView) => {
      if (!plan) return
      const targetArrived = !rowView.row.fcArrived
      const rowIndex = plan.rows.findIndex((item) => item?.row?.id === rowView.row.id)
      let cascadedCount = 0
      ;(plan.rows ?? []).forEach((candidateView, candidateRowIndex) => {
        const candidateRow = candidateView?.row
        if (!candidateRow || candidateRow.rowType !== 'waypoint' || !candidateRow.fcStop) return
        if (targetArrived && candidateRowIndex < rowIndex && !candidateRow.fcArrived) cascadedCount++
        if (!targetArrived && candidateRowIndex > rowIndex && candidateRow.fcArrived) cascadedCount++
      })
      setConfirmState({
        kind: 'override_fc_arrival',
        rowId: rowView.row.id,
        waypointNumber: rowView.number,
        systemName: rowView.row.systemName ?? '',
        targetArrived,
        cascadedCount,
      })
    },
    [plan],
  )

  const handleLegModeChange = useCallback(
    async (rowId: number, modeId: number) => {
      if (!plan) return
      await runPlanMutation(
        'UPDATING FLIGHT MODE',
        'Failed to update leg flight mode',
        () => SetLegMode(rowId, modeId),
        { optimisticPlan: buildOptimisticLegModePlan(plan, rowId, modeId) },
      )
    },
    [plan, runPlanMutation],
  )

  const changeRegion = useCallback(
    async (rowId: number, regionId: number) => {
      await runPlanMutation(
        'UPDATING REGION',
        'Failed to update galactic region',
        () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, regionId })),
        { rollbackOnError: true },
      )
    },
    [runPlanMutation],
  )

  const handleInsert = useCallback(
    async (afterRowId: number, rowType: 'waypoint' | 'region') => {
      if (!plan) return
      setInsertMenuAfterId(null)
      await runPlanMutation(
        rowType === 'region' ? 'ADDING REGION HEADER' : 'ADDING WAYPOINT',
        `Failed to insert ${rowType}`,
        () => AddRow(plan.expedition.id, afterRowId, rowType, DEFAULT_GALACTIC_REGION_ID),
        { rollbackOnError: true },
      )
    },
    [plan, runPlanMutation],
  )

  const executeDeleteRow = useCallback(
    async (rowId: number) => {
      if (!plan) return
      const targetRow = plan.rows?.find((rowView) => rowView?.row?.id === rowId)?.row
      await runPlanMutation(
        targetRow?.rowType === 'region' ? 'REMOVING REGION HEADER' : 'REMOVING WAYPOINT',
        'Failed to delete row',
        () => DeleteRow(rowId),
        { optimisticPlan: buildOptimisticDeletedRowPlan(plan, rowId) },
      )
    },
    [plan, runPlanMutation],
  )

  const handleRequestDeleteRow = useCallback(
    (rowView: expedition.RowView) => {
      if (!isPlanning && rowView.row.rowType === 'waypoint') {
        const isVisited = rowView.visitState === 'visited'
        const isArrivedFcStop = Boolean(rowView.row.fcStop && rowView.row.fcArrived)
        if (isVisited || isArrivedFcStop) {
          setConfirmState({
            kind: 'delete_waypoint',
            rowId: rowView.row.id,
            waypointNumber: rowView.number,
            systemName: rowView.row.systemName ?? '',
            visitedAtGalactic: isVisited ? rowView.visitedAtGalactic : undefined,
            isArrivedFcStop,
          })
          return
        }
      }
      void executeDeleteRow(rowView.row.id)
    },
    [executeDeleteRow, isPlanning],
  )

  const executeMoveRow = useCallback(
    async (rowId: number, fromIndex: number, toIndex: number) => {
      if (!plan) return
      await runPlanMutation(
        'REORDERING ROUTE',
        'Failed to reorder route row',
        () => MoveRow(rowId, toIndex),
        { optimisticPlan: buildOptimisticMovedRowPlan(plan, fromIndex, toIndex) },
      )
    },
    [plan, runPlanMutation],
  )

  const handleMoveRow = useCallback(
    (
      rowId: number,
      fromIndex: number,
      toIndex: number,
      preview: expedition.DropSlotPreview | null,
    ): boolean => {
      if (!plan || fromIndex === toIndex || toIndex < 0 || toIndex >= plan.rows.length) return false
      const draggedRowView = plan.rows?.[fromIndex]
      if (!draggedRowView) return false
      if (!isPlanning && draggedRowView.row.rowType === 'waypoint') {
        if (preview && (preview.status === 'invalid' || preview.status === 'blocked_fc')) {
          return false
        }
        if (preview?.resetsFcArrived) {
          setConfirmState({
            kind: 'move_reset_fc',
            rowId,
            fromIndex,
            toIndex,
            waypointNumber: draggedRowView.number,
            systemName: draggedRowView.row.systemName ?? '',
          })
          return false
        }
      }
      void executeMoveRow(rowId, fromIndex, toIndex)
      return true
    },
    [executeMoveRow, isPlanning, plan],
  )

  const handleConfirmModalAction = useCallback(async () => {
    if (!confirmState) return
    const activeConfirm = confirmState
    setConfirmState(null)
    if (activeConfirm.kind === 'delete_waypoint') {
      await executeDeleteRow(activeConfirm.rowId)
    } else if (activeConfirm.kind === 'move_reset_fc') {
      await executeMoveRow(activeConfirm.rowId, activeConfirm.fromIndex, activeConfirm.toIndex)
    } else if (activeConfirm.kind === 'override_fc_arrival') {
      const carrierContext = carrierContextRef.current
      const shouldCloseSpanshRoute =
        activeConfirm.targetArrived &&
        Boolean(
          plan &&
            carrierContext.activeCarrierRoute &&
            !carrierContext.isManualCarrierRoute &&
            carrierContext.routedCarrierStopIndex >= 0 &&
            plan.rows.findIndex((item) => item?.row?.id === activeConfirm.rowId) >=
              carrierContext.routedCarrierStopIndex,
        )
      await runPlanMutation(
        'UPDATING CARRIER ARRIVAL',
        'Failed to override carrier arrival',
        () => OverrideCarrierStopArrival(activeConfirm.rowId, activeConfirm.targetArrived),
        {
          onSuccess: () => {
            if (shouldCloseSpanshRoute) {
              carrierContext.onCancelCarrierRoute()
            }
          },
          rollbackOnError: true,
        },
      )
    }
  }, [carrierContextRef, confirmState, executeDeleteRow, executeMoveRow, plan, runPlanMutation])

  const handleLifecycleAction = useCallback(
    async (label: string, actionFunction: () => Promise<void>) => {
      await runPlanMutation(label, 'Failed to update expedition lifecycle state', actionFunction, {
        rollbackOnError: true,
      })
    },
    [runPlanMutation],
  )

  return {
    insertMenuAfterId,
    setInsertMenuAfterId,
    confirmState,
    setConfirmState,
    setFcArrived,
    commitField,
    toggleFcStop,
    handleRequestOverrideFcArrived,
    handleLegModeChange,
    changeRegion,
    handleInsert,
    handleRequestDeleteRow,
    handleMoveRow,
    handleConfirmModalAction,
    handleLifecycleAction,
  }
}

