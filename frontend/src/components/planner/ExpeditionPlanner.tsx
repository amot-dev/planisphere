import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import {
  AddRow,
  DeleteRow,
  MoveRow,
  SetActiveLegMode,
  SetLegMode,
  UpdateRow,
} from '../../../wailsjs/go/app/App'
import { expedition } from '../../../wailsjs/go/models'
import { useLocalStorageBoolean } from '../../hooks/useLocalStorageBoolean'
import { CarrierRouteBottomPanel } from './CarrierRouteBottomPanel'
import { CarrierRouteModal } from './CarrierRouteModal'
import { PlannerActiveLegFooter } from './PlannerActiveLegFooter'
import { PlannerHeaderBar } from './PlannerHeaderBar'
import {
  buildOptimisticActiveLegModePlan,
  buildOptimisticDeletedRowPlan,
  buildOptimisticFcArrivedPlan,
  buildOptimisticFcStopTogglePlan,
  buildOptimisticLegModePlan,
  buildOptimisticMovedRowPlan,
  buildPlannerTableItems,
  computePlannerTableDimensions,
  executePlanMutation,
  type PlanMutationBehaviorOptions,
} from './plannerPlanHelpers'
import { PlannerRouteTable } from './PlannerRouteTable'
import { useCarrierRoute } from './useCarrierRoute'
import { useInitialExpeditionScroll } from './useInitialExpeditionScroll'
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
  const { expandedRowIds, detailsById, loadingDetails, toggleExpand } = useWaypointDetails(
    plan,
    isPlanning,
    archiveRevision,
  )

  const [insertMenuAfterId, setInsertMenuAfterId] = useState<number | null>(null)
  // Drag-and-drop state for unvisited / planning rows
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null)
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

  // Highest row index that has been visited; rows at or above this index are locked
  // against structural edits (reordering, deleting, or changing system name).
  const lastVisitedIndex = useMemo(() => {
    if (!plan || !plan.rows || isPlanning) return -1
    let maxIndex = -1
    ;(plan.rows ?? []).forEach((rowView, index) => {
      if (rowView && (rowView.visitState === 'visited' || rowView.visitState === 'current')) {
        maxIndex = index
      }
    })
    return maxIndex
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
    if (copiedTimerRef.current) {
      clearTimeout(copiedTimerRef.current)
    }
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
      // Manual logging leaves the target stop editable until arrival is logged; Spansh routes lock through the plotted stop.
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

  async function handleLegModeChange(rowId: number, modeId: number) {
    if (!plan) return
    await runPlanMutation(
      'UPDATING LEG MODE',
      'Failed to update leg flight mode',
      () => SetLegMode(rowId, modeId),
      { optimisticPlan: buildOptimisticLegModePlan(plan, rowId, modeId), onSuccess: onRefreshAll },
    )
  }

  async function handleActiveLegModeChange(modeId: number) {
    if (!plan) return
    await runPlanMutation(
      'UPDATING ACTIVE LEG MODE',
      'Failed to update active leg flight mode',
      () => SetActiveLegMode(plan.expedition.id, modeId),
      { optimisticPlan: buildOptimisticActiveLegModePlan(plan, modeId), onSuccess: onRefreshAll },
    )
  }

  async function changeRegion(rowId: number, regionId: number) {
    await runPlanMutation(
      'UPDATING GALACTIC REGION',
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

  async function handleDeleteRow(rowId: number) {
    if (!plan) return
    await runPlanMutation(
      'REMOVING ROW',
      'Failed to delete row',
      () => DeleteRow(rowId),
      { optimisticPlan: buildOptimisticDeletedRowPlan(plan, rowId), onSuccess: onRefreshPlan },
    )
  }

  const handleMoveRow = useCallback(
    async (rowId: number, fromIndex: number, toIndex: number) => {
      if (!plan) return
      const minAllowed = lastVisitedIndex + 1
      if (fromIndex < minAllowed || toIndex < minAllowed || toIndex >= plan.rows.length) {
        return
      }
      if (fromIndex === toIndex) return

      await runPlanMutation(
        'REORDERING ROUTE',
        'Failed to reorder route row',
        () => MoveRow(rowId, toIndex),
        { optimisticPlan: buildOptimisticMovedRowPlan(plan, fromIndex, toIndex), onSuccess: onRefreshPlan },
      )
    },
    [lastVisitedIndex, onRefreshPlan, plan, runPlanMutation],
  )

  async function handleLifecycleAction(label: string, actionFunction: () => Promise<void>) {
    await runPlanMutation(label, 'Failed to update expedition lifecycle state', actionFunction, {
      onSuccess: onRefreshAll,
      rollbackOnError: true,
    })
  }

  const { visibleColumnCount, tableMinWidthPixels } = computePlannerTableDimensions(
    isPlanning,
    showNotes,
    showCarrierStop,
  )

  const tableItems = useMemo(
    () => buildPlannerTableItems(plan, expandedRowIds, isPlanning),
    [plan, expandedRowIds, isPlanning],
  )

  const getRowDropProps = useCallback(
    (index: number, isDraggable: boolean) => {
      if (!isDraggable) return {}
      return {
        onDragOver: (event: DragEvent) => {
          event.preventDefault()
          if (index > lastVisitedIndex) {
            event.dataTransfer.dropEffect = 'move'
            if (dragOverIndex !== index) {
              setDragOverIndex(index)
            }
          }
        },
        onDrop: (event: DragEvent) => {
          event.preventDefault()
          const from = draggedIndex
          setDraggedIndex(null)
          setDragOverIndex(null)
          if (from !== null && from !== index && index > lastVisitedIndex && plan) {
            const draggedRow = plan.rows?.[from]?.row
            if (draggedRow) {
              handleMoveRow(draggedRow.id, from, index)
            }
          }
        },
      }
    },
    [lastVisitedIndex, dragOverIndex, draggedIndex, plan, handleMoveRow],
  )

  if (!plan) {
    return (
      <div className="flex-1 flex items-center justify-center p-12 text-[#a6907c] font-mono-tabular uppercase tracking-widest text-xs">
        [ LOADING EXPEDITION FLIGHT PLAN... ]
      </div>
    )
  }

  const currentExpedition = plan.expedition
  const isActive = currentExpedition.status === 'active'
  const isCompleted = currentExpedition.status === 'completed'
  const modesList = plan.modes ?? []
  const hasOriginWaypointSystem =
    originHasDraftSystem ?? Boolean((firstWaypoint?.row.systemName ?? '').trim())

  return (
    <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-[#070605]">
      <PlannerHeaderBar
        plan={plan}
        hasOriginWaypointSystem={hasOriginWaypointSystem}
        onOpenStartModal={onOpenStartModal}
        onLifecycleAction={handleLifecycleAction}
      />

      <div ref={scrollContainerRef} className="flex-1 overflow-y-auto overflow-x-auto bg-[#070605]">
        <PlannerRouteTable
          plan={plan}
          tableItems={tableItems}
          modesList={modesList}
          isPlanning={isPlanning}
          isCompleted={isCompleted}
          lastVisitedIndex={lastVisitedIndex}
          visibleColumnCount={visibleColumnCount}
          tableMinWidthPixels={tableMinWidthPixels}
          showNotes={showNotes}
          showCarrierStop={showCarrierStop}
          expandedRowIds={expandedRowIds}
          detailsById={detailsById}
          loadingDetails={loadingDetails}
          copiedSystemRowId={copiedSystemRowId}
          insertMenuAfterId={insertMenuAfterId}
          draggedIndex={draggedIndex}
          dragOverIndex={dragOverIndex}
          activeCarrierRoute={carrierRoute.activeCarrierRoute}
          activeCarrierTargetRowId={carrierRoute.activeCarrierTargetRowId}
          isManualCarrierRoute={carrierRoute.isManualCarrierRoute}
          nextFcStopEntry={carrierRoute.nextFcStopEntry}
          routedCarrierStopIndex={carrierRoute.routedCarrierStopIndex}
          getRowDropProps={getRowDropProps}
          onToggleShowNotes={toggleShowNotes}
          onToggleShowCarrierStop={toggleShowCarrierStop}
          onChangeRegion={changeRegion}
          onSetInsertMenuAfterId={setInsertMenuAfterId}
          onInsert={handleInsert}
          onMoveRow={handleMoveRow}
          onDeleteRow={handleDeleteRow}
          onLegModeChange={handleLegModeChange}
          onToggleExpand={toggleExpand}
          onOriginSystemFilledChange={setOriginHasDraftSystem}
          onFieldBlur={commitField}
          onCopySystem={handleCopySystem}
          onToggleFcStop={toggleFcStop}
          onOpenCarrierRoutePrompt={carrierRoute.openCarrierRoutePrompt}
          onRowDragStart={(dragRowIndex, dragRowId, event) => {
            setDraggedIndex(dragRowIndex)
            event.dataTransfer.effectAllowed = 'move'
            event.dataTransfer.setData('text/plain', String(dragRowId))
          }}
          onRowDragEnd={() => {
            setDraggedIndex(null)
            setDragOverIndex(null)
          }}
        />

        <PlannerActiveLegFooter
          plan={plan}
          isPlanning={isPlanning}
          isActive={isActive}
          isCompleted={isCompleted}
          modesList={modesList}
          onActiveLegModeChange={handleActiveLegModeChange}
          onAppendRow={(rowType) => handleInsert(0, rowType)}
        />
      </div>

      {/* Bottom Panel: Active Carrier Route (Spansh or Manual Mode) */}
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

      {/* Carrier Route Setup Prompt Modal */}
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
    </div>
  )
}
