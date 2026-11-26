import { useCallback, useMemo, useRef, useState } from 'react'
import {
  AddRow,
  DeleteRow,
  MoveRow,
  OverrideCarrierStopArrival,
  SetExpeditionFlightMode,
  SetLegMode,
  UpdateRow,
} from '../../api/backend'
import { expedition } from '../../api/models'
import {
  buildOptimisticDeletedRowPlan,
  buildOptimisticExpeditionModePlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
} from '../../domain/planOptimisticReducers'
import { createBusyTaskTracker, type BusyTaskTracker } from '../../hooks/useBusyTasks'
import type { PlannerConfirmState } from './PlannerConfirmModal'

const DEFAULT_GALACTIC_REGION_ID = 18 // Inner Orion Spur

/**
 * PlanMutationContextCallbacks supplies the busy banner, optional token-scoped
 * busy tracker, optimistic PlanView setter, and rollback refresh handler shared
 * across planner mutations.
 */
export interface PlanMutationContextCallbacks {
  onBusyChange: (busyMessage: string | null) => void
  busyTracker?: BusyTaskTracker
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
 * backend planner RPC under a busy status banner, rolls back via onRefreshPlan
 * if the RPC rejects, and resolves to true on success or false on rejection.
 */
export async function executePlanMutation(
  busyMessage: string,
  errorContext: string,
  mutationAction: () => Promise<unknown>,
  contextCallbacks: PlanMutationContextCallbacks,
  options?: PlanMutationBehaviorOptions,
): Promise<boolean> {
  if (options?.optimisticPlan) {
    contextCallbacks.onPlanOptimisticUpdate(options.optimisticPlan)
  }
  const endBusyTask = contextCallbacks.busyTracker
    ? contextCallbacks.busyTracker.beginTask(busyMessage)
    : null
  if (!endBusyTask) {
    contextCallbacks.onBusyChange(busyMessage)
  }
  try {
    await mutationAction()
    options?.onSuccess?.()
    return true
  } catch (mutationError) {
    console.error(`${errorContext}:`, mutationError)
    if (options?.optimisticPlan || options?.rollbackOnError) {
      await contextCallbacks.onRefreshPlan()
    }
    return false
  } finally {
    if (endBusyTask) {
      endBusyTask()
    } else {
      contextCallbacks.onBusyChange(null)
    }
  }
}

/**
 * UsePlanMutationsOptions configures plan state and optimistic update callbacks
 * for expedition planner mutations.
 */
export interface UsePlanMutationsOptions {
  plan: expedition.PlanView | null
  isPlanning: boolean
  onBusyChange: (busyMessage: string | null) => void
  busyTracker?: BusyTaskTracker
  applyOptimisticPlan: (nextPlan: expedition.PlanView) => void
  onRefreshPlan: () => void
}

/**
 * usePlanMutations encapsulates planner row CRUD, reordering, carrier stop
 * toggles, confirmation modal state, and lifecycle RPC actions.
 */
export function usePlanMutations({
  plan,
  isPlanning,
  onBusyChange,
  busyTracker: externalBusyTracker,
  applyOptimisticPlan,
  onRefreshPlan,
}: UsePlanMutationsOptions) {
  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)
  const [confirmState, setConfirmState] = useState<PlannerConfirmState | null>(null)

  const onBusyChangeRef = useRef(onBusyChange)
  onBusyChangeRef.current = onBusyChange
  const fallbackBusyTracker = useMemo(
    () => createBusyTaskTracker((busyMessage) => onBusyChangeRef.current(busyMessage)),
    [],
  )
  const busyTracker = externalBusyTracker ?? fallbackBusyTracker

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
        {
          onBusyChange,
          busyTracker,
          onPlanOptimisticUpdate: applyOptimisticPlan,
          onRefreshPlan,
        },
        options,
      ),
    [applyOptimisticPlan, busyTracker, onBusyChange, onRefreshPlan],
  )

  const setFcArrived = useCallback(
    async (rowId: number, arrived: boolean): Promise<boolean> => {
      if (!plan) return false
      return runPlanMutation(
        'UPDATING CARRIER ARRIVAL',
        'Failed to update carrier arrival',
        () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, fcArrived: arrived })),
        { optimisticPlan: buildOptimisticFcArrivedPlan(plan, rowId, arrived) },
      )
    },
    [plan, runPlanMutation],
  )

  const commitField = useCallback(
    async (
      row: expedition.Row,
      field: 'systemName' | 'poiName' | 'notes',
      value: string,
    ): Promise<boolean> => {
      if ((row[field] ?? '') === value) return true
      return runPlanMutation(
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
      const targetRowView = plan.rows?.find((rowView) => rowView?.row?.id === row.id)
      if (
        targetRowView?.permissions &&
        !(row.fcStop
          ? targetRowView.permissions.canRemoveFcStop
          : targetRowView.permissions.canAddFcStop)
      ) {
        return
      }
      const nextFcStop = !row.fcStop
      await runPlanMutation(
        'UPDATING FC STOP',
        'Failed to toggle fleet carrier stop',
        () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, fcStop: nextFcStop })),
        { optimisticPlan: buildOptimisticFcStopTogglePlan(plan, row, nextFcStop) },
      )
    },
    [plan, runPlanMutation],
  )

  const handleRequestOverrideFcArrived = useCallback(
    (rowView: expedition.RowView) => {
      if (!plan) return
      const targetArrived = !rowView.row.fcArrived
      const cascadedCount = rowView.permissions?.overrideFcCascadeCount ?? 0
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

  const handleActiveModeChange = useCallback(
    async (modeId: number) => {
      if (!plan) return
      await runPlanMutation(
        'SWITCHING FLIGHT MODE',
        'Failed to update expedition flight mode',
        () => SetExpeditionFlightMode(plan.expedition.id, modeId),
        { optimisticPlan: buildOptimisticExpeditionModePlan(plan, modeId) },
      )
    },
    [plan, runPlanMutation],
  )

  const handleLegModeChange = useCallback(
    async (rowId: number, modeId: number, cascade = false) => {
      if (!plan) return
      await runPlanMutation(
        'UPDATING FLIGHT MODE',
        'Failed to update leg flight mode',
        () => SetLegMode(rowId, modeId, cascade),
        { optimisticPlan: buildOptimisticLegModePlan(plan, rowId, modeId, cascade) },
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
      await runPlanMutation(
        'UPDATING CARRIER ARRIVAL',
        'Failed to override carrier arrival',
        () => OverrideCarrierStopArrival(activeConfirm.rowId, activeConfirm.targetArrived),
        { rollbackOnError: true },
      )
    }
  }, [confirmState, executeDeleteRow, executeMoveRow, runPlanMutation])

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
    handleActiveModeChange,
    handleLegModeChange,
    changeRegion,
    handleInsert,
    handleRequestDeleteRow,
    handleMoveRow,
    handleConfirmModalAction,
    handleLifecycleAction,
  }
}

