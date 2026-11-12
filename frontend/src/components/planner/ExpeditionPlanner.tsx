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
  type PlannerRowDraft,
} from './plannerPlanHelpers'
import { PlannerRouteTable } from './PlannerRouteTable'
import { useCarrierRoute } from './useCarrierRoute'
import { useInitialExpeditionScroll } from './useInitialExpeditionScroll'
import { useWaypointDetails } from './useWaypointDetails'

/**
 * ExpeditionPlannerProps defines the active expedition plan state, optimistic
 * update handlers, lifecycle modal triggers, and carrier plotter registration
 * callbacks for the main flight planner view.
 */
export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
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

  const [showNotes, setShowNotes] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem('planisphere_show_notes')
      return saved !== null ? saved === 'true' : true
    } catch {
      return true
    }
  })

  const [showCarrierStop, setShowCarrierStop] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem('planisphere_show_carrier')
      return saved !== null ? saved === 'true' : true
    } catch {
      return true
    }
  })

  const toggleShowNotes = useCallback(() => {
    setShowNotes((prev) => {
      const next = !prev
      try {
        localStorage.setItem('planisphere_show_notes', String(next))
      } catch {}
      return next
    })
  }, [])

  const toggleShowCarrierStop = useCallback(() => {
    setShowCarrierStop((prev) => {
      const next = !prev
      try {
        localStorage.setItem('planisphere_show_carrier', String(next))
      } catch {}
      return next
    })
  }, [])

  // Keep track of which row currently has user focus so background updates don't clobber active typing
  const editingRowIdRef = useRef<number | null>(null)
  // Local draft inputs so typing in spreadsheet cells is instantaneous
  const [drafts, setDrafts] = useState<Record<number, PlannerRowDraft>>({})

  useEffect(() => {
    if (!plan || !plan.rows) return
    setDrafts((prev) => {
      const next: Record<number, PlannerRowDraft> = { ...prev }
      const activeEditingId = editingRowIdRef.current
      for (const rowView of plan.rows ?? []) {
        if (!rowView?.row) continue
        const rowId = rowView.row.id
        // Never clobber the row that is currently focused and being typed into
        if (rowId === activeEditingId && prev[rowId]) {
          continue
        }
        next[rowId] = {
          systemName: rowView.row.systemName ?? '',
          poiName: rowView.row.poiName ?? '',
          notes: rowView.row.notes ?? '',
        }
      }
      return next
    })
  }, [plan])

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

  async function setFcArrived(rowId: number, arrived: boolean) {
    if (!plan) return
    onPlanOptimisticUpdate(buildOptimisticFcArrivedPlan(plan, rowId, arrived))
    onBusyChange('UPDATING CARRIER ARRIVAL')
    try {
      await UpdateRow(expedition.RowPatch.createFrom({ id: rowId, fcArrived: arrived }))
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  const carrierRoute = useCarrierRoute({
    plan,
    lastVisitedIndex,
    drafts,
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
    onBusyChange('UPDATING WAYPOINT')
    try {
      await UpdateRow(expedition.RowPatch.createFrom({ id: row.id, [field]: value }))
    } finally {
      onBusyChange(null)
    }
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
    onPlanOptimisticUpdate(buildOptimisticFcStopTogglePlan(plan, row, nextFcStop))
    onBusyChange('UPDATING FC STOP')
    try {
      await UpdateRow(expedition.RowPatch.createFrom({ id: row.id, fcStop: nextFcStop }))
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleLegModeChange(rowId: number, modeId: number) {
    if (!plan) return
    onPlanOptimisticUpdate(buildOptimisticLegModePlan(plan, rowId, modeId))
    onBusyChange('UPDATING LEG MODE')
    try {
      await SetLegMode(rowId, modeId)
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleActiveLegModeChange(modeId: number) {
    if (!plan) return
    onPlanOptimisticUpdate(buildOptimisticActiveLegModePlan(plan, modeId))
    onBusyChange('UPDATING ACTIVE LEG MODE')
    try {
      await SetActiveLegMode(plan.expedition.id, modeId)
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
  }

  async function changeRegion(rowId: number, regionId: number) {
    onBusyChange('UPDATING GALACTIC REGION')
    try {
      await UpdateRow(expedition.RowPatch.createFrom({ id: rowId, regionId }))
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleInsert(afterRowId: number, rowType: 'waypoint' | 'region') {
    if (!plan) return
    setInsertMenuAfterId(null)
    onBusyChange(rowType === 'region' ? 'ADDING REGION HEADER' : 'ADDING WAYPOINT')
    try {
      await AddRow(plan.expedition.id, afterRowId, rowType, 18)
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  async function handleDeleteRow(rowId: number) {
    if (!plan) return
    onPlanOptimisticUpdate(buildOptimisticDeletedRowPlan(plan, rowId))
    onBusyChange('REMOVING ROW')
    try {
      await DeleteRow(rowId)
      onRefreshPlan()
    } finally {
      onBusyChange(null)
    }
  }

  const handleMoveRow = useCallback(
    async (rowId: number, fromIndex: number, toIndex: number) => {
      if (!plan) return
      const minAllowed = lastVisitedIndex + 1
      if (fromIndex < minAllowed || toIndex < minAllowed || toIndex >= plan.rows.length) {
        return
      }
      if (fromIndex === toIndex) return

      onPlanOptimisticUpdate(buildOptimisticMovedRowPlan(plan, fromIndex, toIndex))
      onBusyChange('REORDERING ROUTE')
      try {
        await MoveRow(rowId, toIndex)
        onRefreshPlan()
      } finally {
        onBusyChange(null)
      }
    },
    [lastVisitedIndex, onBusyChange, onPlanOptimisticUpdate, onRefreshPlan, plan],
  )

  async function handleLifecycleAction(label: string, actionFunction: () => Promise<void>) {
    onBusyChange(label)
    try {
      await actionFunction()
      onRefreshAll()
    } finally {
      onBusyChange(null)
    }
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
  const firstWaypoint = plan.rows?.find((rowView) => rowView.row.rowType === 'waypoint')
  const hasWp1System = Boolean(
    (drafts[firstWaypoint?.row.id ?? 0]?.systemName ?? firstWaypoint?.row.systemName ?? '').trim(),
  )

  return (
    <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-[#070605]">
      <PlannerHeaderBar
        plan={plan}
        hasWp1System={hasWp1System}
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
          drafts={drafts}
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
          onDraftChange={(rowId, fallbackDraft, fieldName, fieldValue) =>
            setDrafts((prev) => ({
              ...prev,
              [rowId]: { ...(prev[rowId] ?? fallbackDraft), [fieldName]: fieldValue },
            }))
          }
          onFieldFocus={(id) => {
            editingRowIdRef.current = id
          }}
          onFieldBlur={(waypointRow, fieldName, fieldValue) => {
            editingRowIdRef.current = null
            commitField(waypointRow, fieldName, fieldValue)
          }}
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
