import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AddRow,
  DeleteRow,
  MoveRow,
  OverrideCarrierStopArrival,
  SetLegMode,
  UpdateRow,
} from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'
import { useLocalStorageBoolean } from '../../hooks/useLocalStorageBoolean'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'
import { PlannerActiveLegFooter } from './PlannerActiveLegFooter'
import { PlannerConfirmModal, type PlannerConfirmState } from './PlannerConfirmModal'
import { PlannerHeaderBar } from './PlannerHeaderBar'
import {
  buildOptimisticDeletedRowPlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
  computePlannerTableDimensions,
  executePlanMutation,
  type PlanMutationBehaviorOptions,
} from './plannerPlanHelpers'
import { PlannerRouteTable } from './PlannerRouteTable'
import { useCarrierRoute } from './useCarrierRoute'
import { useInitialExpeditionScroll } from './useInitialExpeditionScroll'
import { usePlannerRowDrag } from './usePlannerRowDrag'
import { useWaypointDetails } from './useWaypointDetails'

const DEFAULT_GALACTIC_REGION_ID = 18 // Inner Orion Spur

/**
 * ExpeditionPlannerProps defines the active expedition plan state, journal
 * archive revision token, optimistic update handlers, lifecycle modal triggers,
 * and carrier plotter registration callbacks for the main flight planner view.
 */
export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
  archiveRevision?: string
  onPlanOptimisticUpdate: (next: expedition.PlanView) => void
  onOpenStartModal: () => void
  onRefreshPlan: () => void
  onRefreshAll: () => void
  onBusyChange: (busyMessage: string | null) => void
  onRegisterManualPlotter?: (openManualPlotter: () => void) => void
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

/**
 * ExpeditionPlanner renders the primary flight plan table, inline cell drafts,
 * drag-and-drop row reordering, telemetry drawers, and Fleet Carrier plotter.
 */
export function ExpeditionPlanner({
  plan,
  archiveRevision = '',
  onPlanOptimisticUpdate,
  onOpenStartModal,
  onRefreshPlan,
  onRefreshAll,
  onBusyChange,
  onRegisterManualPlotter,
  onCarrierRouteOpenChange,
}: ExpeditionPlannerProps) {
  const isPlanning = plan?.expedition?.status === 'planning'
  const isCompleted = plan?.expedition?.status === 'completed'
  const { expandedRowIds, detailsById, loadingDetails, toggleExpand } = useWaypointDetails(
    plan,
    Boolean(isPlanning),
    archiveRevision,
  )

  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)
  const [confirmState, setConfirmState] = useState<PlannerConfirmState | null>(null)
  const [copiedSystemRowId, setCopiedSystemRowId] = useState<number | null>(null)
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
    }
  }, [])

  const [showNotes, toggleShowNotes] = useLocalStorageBoolean('planisphere_show_notes', true)
  const [showCarrierStop, toggleShowCarrierStop] = useLocalStorageBoolean(
    'planisphere_show_carrier',
    true,
  )

  const firstWaypoint = plan?.rows?.find((rowView) => rowView?.row?.rowType === 'waypoint')
  const [originHasDraftSystem, setOriginHasDraftSystem] = useState<boolean | null>(null)
  useEffect(() => {
    setOriginHasDraftSystem(null)
  }, [firstWaypoint?.row.id, firstWaypoint?.row.systemName])

  const { lastVisitedIndex, lastFcArrivedIndex } = useMemo(() => {
    if (!plan || !plan.rows || isPlanning) {
      return { lastVisitedIndex: -1, lastFcArrivedIndex: -1 }
    }
    let maxVisited = -1
    let maxFcArrived = -1
    ;(plan.rows ?? []).forEach((rowView, index) => {
      if (!rowView) return
      if (rowView.visitState === 'visited') {
        maxVisited = index
      }
      if (rowView.row?.rowType === 'waypoint' && rowView.row.fcStop && rowView.row.fcArrived) {
        maxFcArrived = index
      }
    })
    return { lastVisitedIndex: maxVisited, lastFcArrivedIndex: maxFcArrived }
  }, [plan, isPlanning])

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
        { onBusyChange, onPlanOptimisticUpdate, onRefreshPlan },
        options,
      ),
    [onBusyChange, onPlanOptimisticUpdate, onRefreshPlan],
  )

  async function setFcArrived(rowId: number, arrived: boolean) {
    if (!plan) return
    await runPlanMutation(
      'UPDATING CARRIER ARRIVAL',
      'Failed to update carrier arrival',
      () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, fcArrived: arrived })),
      { optimisticPlan: buildOptimisticFcArrivedPlan(plan, rowId, arrived), onSuccess: onRefreshPlan },
    )
  }

  const carrierRoute = useCarrierRoute({
    plan,
    lastVisitedIndex,
    onBusyChange,
    onSetFcArrived: setFcArrived,
    onRegisterManualPlotter,
    onCarrierRouteOpenChange,
  })

  const scrollContainerRef = useRef<HTMLDivElement>(null)
  useInitialExpeditionScroll(
    scrollContainerRef,
    Boolean(plan?.expedition),
    plan?.expedition?.id,
    plan?.expedition?.status,
    lastVisitedIndex,
  )

  function handleCopySystem(rowId: number, systemName: string) {
    if (!systemName) return
    const text = systemName.trim()
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(text).catch(console.error)
    } else {
      const textareaElement = document.createElement('textarea')
      textareaElement.value = text
      document.body.appendChild(textareaElement)
      textareaElement.select()
      document.execCommand('copy')
      document.body.removeChild(textareaElement)
    }
    if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current)
    setCopiedSystemRowId(rowId)
    copiedTimerRef.current = setTimeout(() => {
      setCopiedSystemRowId((currentId) => (currentId === rowId ? null : currentId))
      copiedTimerRef.current = null
    }, 1500)
  }

  async function commitField(
    row: expedition.Row,
    field: 'systemName' | 'poiName' | 'notes',
    value: string,
  ) {
    if ((row[field] ?? '') === value) return
    await runPlanMutation(
      'UPDATING WAYPOINT',
      `Failed to update waypoint ${field}`,
      () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, [field]: value })),
      { rollbackOnError: true },
    )
  }

  async function toggleFcStop(row: expedition.Row) {
    if (!plan) return
    if (carrierRoute.activeCarrierRoute && carrierRoute.routedCarrierStopIndex >= 0) {
      const rowIndex = plan.rows.findIndex((rowView) => rowView?.row?.id === row.id)
      if (
        rowIndex >= 0 &&
        (carrierRoute.isManualCarrierRoute
          ? rowIndex < carrierRoute.routedCarrierStopIndex
          : rowIndex <= carrierRoute.routedCarrierStopIndex)
      ) {
        return
      }
    }
    const nextFcStop = !row.fcStop
    await runPlanMutation(
      'UPDATING FC STOP',
      'Failed to toggle fleet carrier stop',
      () => UpdateRow(expedition.RowPatch.createFrom({ id: row.id, fcStop: nextFcStop })),
      { optimisticPlan: buildOptimisticFcStopTogglePlan(plan, row, nextFcStop), onSuccess: onRefreshPlan },
    )
  }

  function handleRequestOverrideFcArrived(rowView: expedition.RowView) {
    if (!plan) return
    const targetArrived = !rowView.row.fcArrived
    const rowIndex = plan.rows.findIndex((item) => item?.row?.id === rowView.row.id)
    let cascadedCount = 0
    ;(plan.rows ?? []).forEach((candidateView, idx) => {
      const candidateRow = candidateView?.row
      if (!candidateRow || candidateRow.rowType !== 'waypoint' || !candidateRow.fcStop) return
      if (targetArrived && idx < rowIndex && !candidateRow.fcArrived) cascadedCount++
      if (!targetArrived && idx > rowIndex && candidateRow.fcArrived) cascadedCount++
    })
    setConfirmState({
      kind: 'override_fc_arrival',
      rowId: rowView.row.id,
      waypointNumber: rowView.number,
      systemName: rowView.row.systemName ?? '',
      targetArrived,
      cascadedCount,
    })
  }

  async function handleLegModeChange(rowId: number, modeId: number) {
    if (!plan) return
    await runPlanMutation(
      'UPDATING FLIGHT MODE',
      'Failed to update leg flight mode',
      () => SetLegMode(rowId, modeId),
      { optimisticPlan: buildOptimisticLegModePlan(plan, rowId, modeId), onSuccess: onRefreshAll },
    )
  }

  async function changeRegion(rowId: number, regionId: number) {
    await runPlanMutation(
      'UPDATING REGION',
      'Failed to update galactic region',
      () => UpdateRow(expedition.RowPatch.createFrom({ id: rowId, regionId })),
      { onSuccess: onRefreshPlan, rollbackOnError: true },
    )
  }

  async function handleInsert(afterRowId: number, rowType: 'waypoint' | 'region') {
    if (!plan) return
    setInsertMenuAfterId(null)
    await runPlanMutation(
      rowType === 'region' ? 'ADDING REGION HEADER' : 'ADDING WAYPOINT',
      `Failed to insert ${rowType}`,
      () => AddRow(plan.expedition.id, afterRowId, rowType, DEFAULT_GALACTIC_REGION_ID),
      { onSuccess: onRefreshPlan, rollbackOnError: true },
    )
  }

  async function executeDeleteRow(rowId: number) {
    if (!plan) return
    const targetRow = plan.rows?.find((rowView) => rowView?.row?.id === rowId)?.row
    await runPlanMutation(
      targetRow?.rowType === 'region' ? 'REMOVING REGION HEADER' : 'REMOVING WAYPOINT',
      'Failed to delete row',
      () => DeleteRow(rowId),
      { optimisticPlan: buildOptimisticDeletedRowPlan(plan, rowId), onSuccess: onRefreshPlan },
    )
  }

  function handleRequestDeleteRow(rowView: expedition.RowView) {
    if (!isPlanning && rowView.row.rowType === 'waypoint') {
      if (rowView.visitState === 'visited') {
        setConfirmState({
          kind: 'delete_visited',
          rowId: rowView.row.id,
          waypointNumber: rowView.number,
          systemName: rowView.row.systemName ?? '',
          visitedAtGalactic: rowView.visitedAtGalactic,
        })
        return
      }
      if (rowView.row.fcStop && rowView.row.fcArrived) {
        setConfirmState({
          kind: 'delete_arrived_fc',
          rowId: rowView.row.id,
          waypointNumber: rowView.number,
          systemName: rowView.row.systemName ?? '',
        })
        return
      }
    }
    void executeDeleteRow(rowView.row.id)
  }

  const executeMoveRow = useCallback(
    async (rowId: number, fromIndex: number, toIndex: number) => {
      if (!plan) return
      await runPlanMutation(
        'REORDERING ROUTE',
        'Failed to reorder route row',
        () => MoveRow(rowId, toIndex),
        { optimisticPlan: buildOptimisticMovedRowPlan(plan, fromIndex, toIndex), onSuccess: onRefreshPlan },
      )
    },
    [onRefreshPlan, plan, runPlanMutation],
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

  async function handleConfirmModalAction() {
    if (!confirmState) return
    const activeConfirm = confirmState
    setConfirmState(null)
    if (activeConfirm.kind === 'delete_visited' || activeConfirm.kind === 'delete_arrived_fc') {
      await executeDeleteRow(activeConfirm.rowId)
    } else if (activeConfirm.kind === 'move_reset_fc') {
      await executeMoveRow(activeConfirm.rowId, activeConfirm.fromIndex, activeConfirm.toIndex)
    } else if (activeConfirm.kind === 'override_fc_arrival') {
      const shouldCloseSpanshRoute =
        activeConfirm.targetArrived &&
        Boolean(
          plan &&
            carrierRoute.activeCarrierRoute &&
            !carrierRoute.isManualCarrierRoute &&
            carrierRoute.routedCarrierStopIndex >= 0 &&
            plan.rows.findIndex((item) => item?.row?.id === activeConfirm.rowId) >=
              carrierRoute.routedCarrierStopIndex,
        )
      await runPlanMutation(
        'UPDATING CARRIER ARRIVAL',
        'Failed to override carrier arrival',
        () => OverrideCarrierStopArrival(activeConfirm.rowId, activeConfirm.targetArrived),
        {
          onSuccess: () => {
            if (shouldCloseSpanshRoute) {
              carrierRoute.handleCancelCarrierRoute()
            }
            onRefreshPlan()
          },
          rollbackOnError: true,
        },
      )
    }
  }

  async function handleLifecycleAction(label: string, actionFunction: () => Promise<void>) {
    await runPlanMutation(label, 'Failed to update expedition lifecycle state', actionFunction, {
      onSuccess: onRefreshAll,
      rollbackOnError: true,
    })
  }

  const { visibleColumnCount, tableMinWidthPixels } = computePlannerTableDimensions(
    Boolean(isPlanning),
    showNotes,
    showCarrierStop,
  )

  const lockedSpanshTargetRowId =
    carrierRoute.activeCarrierRoute && !carrierRoute.isManualCarrierRoute
      ? carrierRoute.activeCarrierTargetRowId
      : 0

  const rowDrag = usePlannerRowDrag({
    plan,
    isPlanning: Boolean(isPlanning),
    isCompleted: Boolean(isCompleted),
    lastVisitedIndex,
    lockedSpanshTargetRowId,
    expandedRowIds,
    scrollContainerRef,
    onMoveRow: handleMoveRow,
  })

  if (!plan) {
    return (
      <div className="flex-1 flex items-center justify-center p-12 text-[#a6907c] font-mono-tabular uppercase tracking-widest text-xs">
        [ LOADING EXPEDITION... ]
      </div>
    )
  }

  const currentExpedition = plan.expedition
  const modesList = plan.modes ?? []
  const hasOriginWaypointSystem =
    originHasDraftSystem ?? Boolean((firstWaypoint?.row.systemName ?? '').trim())

  return (
    <div
      onDragOver={(event) => {
        if (rowDrag.draggedIndex !== null) {
          rowDrag.recordDragOverClientY(event.clientY)
        }
      }}
      className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-[#070605]"
    >
      <PlannerHeaderBar
        plan={plan}
        hasOriginWaypointSystem={hasOriginWaypointSystem}
        onOpenStartModal={onOpenStartModal}
        onLifecycleAction={handleLifecycleAction}
      />

      <div
        ref={scrollContainerRef}
        {...rowDrag.scrollContainerDropProps}
        className="flex-1 overflow-y-auto overflow-x-auto bg-[#070605] [overflow-anchor:none]"
      >
        <PlannerRouteTable
          plan={plan}
          tableItems={rowDrag.tableItems}
          modesList={modesList}
          isPlanning={Boolean(isPlanning)}
          isCompleted={Boolean(isCompleted)}
          lastVisitedIndex={lastVisitedIndex}
          lastFcArrivedIndex={lastFcArrivedIndex}
          visibleColumnCount={visibleColumnCount}
          tableMinWidthPixels={tableMinWidthPixels}
          showNotes={showNotes}
          showCarrierStop={showCarrierStop}
          expandedRowIds={expandedRowIds}
          detailsById={detailsById}
          loadingDetails={loadingDetails}
          copiedSystemRowId={copiedSystemRowId}
          insertMenuAfterId={insertMenuAfterId}
          heldOrDraggedIndex={rowDrag.heldOrDraggedIndex}
          draggedIndex={rowDrag.draggedIndex}
          dragOverGapIndex={rowDrag.dragOverGapIndex}
          activeCarrierRoute={carrierRoute.activeCarrierRoute}
          activeCarrierTargetRowId={carrierRoute.activeCarrierTargetRowId}
          isManualCarrierRoute={carrierRoute.isManualCarrierRoute}
          nextFcStopEntry={carrierRoute.nextFcStopEntry}
          routedCarrierStopIndex={carrierRoute.routedCarrierStopIndex}
          getRowDropProps={rowDrag.getRowDropProps}
          getSlotDropProps={rowDrag.getSlotDropProps}
          onClearDragOverGap={rowDrag.handleClearDragOverGap}
          onToggleShowNotes={toggleShowNotes}
          onToggleShowCarrierStop={toggleShowCarrierStop}
          onChangeRegion={changeRegion}
          onSetInsertMenuAfterId={setInsertMenuAfterId}
          onInsert={handleInsert}
          onDeleteRow={handleRequestDeleteRow}
          onLegModeChange={handleLegModeChange}
          onToggleExpand={toggleExpand}
          onOriginSystemFilledChange={setOriginHasDraftSystem}
          onFieldBlur={commitField}
          onCopySystem={handleCopySystem}
          onToggleFcStop={toggleFcStop}
          onOverrideFcArrived={handleRequestOverrideFcArrived}
          onOpenCarrierRoutePrompt={carrierRoute.openCarrierRoutePrompt}
          onRowHoldStart={rowDrag.handleRowHoldStart}
          onRowHoldEnd={rowDrag.handleRowHoldEnd}
          onRowDragStart={rowDrag.handleRowDragStart}
          onRowDragEnd={rowDrag.handleRowDragEnd}
        />

        <PlannerActiveLegFooter
          isPlanning={Boolean(isPlanning)}
          isCompleted={Boolean(isCompleted)}
          hasCollapsedRows={rowDrag.hasCollapsedRows}
          onAppendRow={(rowType) => handleInsert(0, rowType)}
        />
      </div>

      {carrierRoute.activeCarrierRoute && (
        <CarrierRouteBottomPanel
          key={currentExpedition.id}
          route={carrierRoute.activeCarrierRoute}
          jumpIndex={carrierRoute.carrierJumpIndex}
          manualMode={carrierRoute.isManualCarrierRoute}
          hasNextFcStop={Boolean(carrierRoute.nextFcStopEntry)}
          nextFcStopSystem={carrierRoute.nextFcStopSystem}
          onCancelRoute={carrierRoute.handleCancelCarrierRoute}
          onToggleManualMode={carrierRoute.switchActiveRouteToManualMode}
          onCompleteSpanshJump={carrierRoute.handleCompleteSpanshJump}
          manualDistanceInput={carrierRoute.manualDistanceInput}
          onChangeManualDistance={carrierRoute.handleChangeManualDistance}
          manualTritiumInput={carrierRoute.manualTritiumInput}
          onChangeManualTritium={carrierRoute.handleChangeManualTritium}
          onLogManualJump={carrierRoute.handleLogManualJump}
          manualLegJumps={carrierRoute.manualLegJumps}
          manualLegDistance={carrierRoute.manualLegDistance}
          manualLegTritium={carrierRoute.manualLegTritium}
          loggingJump={carrierRoute.loggingJump}
          copiedNextJump={carrierRoute.copiedNextJump}
          onCopySystem={carrierRoute.handleCopyNextJumpSystem}
          onArrive={carrierRoute.handleCarrierArriveAtWaypoint}
          depotTritium={
            carrierRoute.activeCarrierRoute.depotTritium !== undefined
              ? carrierRoute.activeCarrierRoute.depotTritium
              : 1000
          }
          onRefillDepot={carrierRoute.handleRefillDepot}
        />
      )}

      {carrierRoute.routePrompt && (
        <CarrierRouteModal
          prompt={carrierRoute.routePrompt}
          onUpdate={(updater) =>
            carrierRoute.setRoutePrompt((previous) => (previous ? updater(previous) : null))
          }
          onStart={carrierRoute.handleStartCarrierRoute}
          onClose={() => carrierRoute.setRoutePrompt(null)}
        />
      )}

      {confirmState && (
        <PlannerConfirmModal
          confirmState={confirmState}
          onConfirm={() => void handleConfirmModalAction()}
          onCancel={() => setConfirmState(null)}
        />
      )}
    </div>
  )
}
