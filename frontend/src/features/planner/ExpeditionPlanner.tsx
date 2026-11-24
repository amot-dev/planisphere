import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { expedition } from '../../api/models'
import type { ExternalLinkVisibilityConfig } from '../../domain/externalLinks'
import {
  computePlanFrontierIndices,
  computePlannerTableDimensions,
} from '../../domain/planTableItems'
import { createBusyTaskTracker } from '../../hooks/useBusyTasks'
import { useCopyFeedback } from '../../hooks/useCopyFeedback'
import { useLocalStorageBoolean } from '../../hooks/useLocalStorageBoolean'
import { CarrierRouteBottomPanel } from '../carrier/CarrierRouteBottomPanel'
import { CarrierRouteModal } from '../carrier/CarrierRouteModal'
import { useCarrierRoute } from '../carrier/useCarrierRoute'
import { usePlannerRowDrag } from './drag/usePlannerRowDrag'
import { useWaypointDetails } from './drawer/useWaypointDetails'
import { PlannerActiveLegFooter } from './PlannerActiveLegFooter'
import { PlannerConfirmModal } from './PlannerConfirmModal'
import { PlannerHeaderBar } from './PlannerHeaderBar'
import { PlannerRouteTable } from './table/PlannerRouteTable'
import { useInitialExpeditionScroll } from './useInitialExpeditionScroll'
import { usePlanMutations, type CarrierMutationLockContext } from './usePlanMutations'

/**
 * ExpeditionPlannerProps defines the active expedition plan state, journal
 * archive revision token, external-link visibility settings, optimistic update
 * handlers, lifecycle modal triggers, and carrier router trigger count.
 */
export interface ExpeditionPlannerProps {
  plan: expedition.PlanView | null
  archiveRevision?: string
  closedWindowRevision?: number
  clockSuspended?: boolean
  externalLinkInara?: string
  externalLinkEdsm?: string
  externalLinkGec?: string
  manualPlotterRequestCount?: number
  exportEnabled?: boolean
  onPlanOptimisticUpdate: (next: expedition.PlanView) => void
  onOpenStartModal: () => void
  onOpenExportModal?: () => void
  onRefreshPlan: () => void
  onBusyChange: (busyMessage: string | null) => void
  onCarrierRouteOpenChange?: (isOpen: boolean) => void
}

/**
 * ExpeditionPlanner renders the primary flight plan table, inline cell drafts,
 * drag-and-drop row reordering, telemetry drawers, and Fleet Carrier router.
 */
export const ExpeditionPlanner = memo(function ExpeditionPlanner({
  plan,
  archiveRevision = '',
  closedWindowRevision = 0,
  clockSuspended = false,
  externalLinkInara,
  externalLinkEdsm,
  externalLinkGec,
  manualPlotterRequestCount = 0,
  exportEnabled = false,
  onPlanOptimisticUpdate,
  onOpenStartModal,
  onOpenExportModal,
  onRefreshPlan,
  onBusyChange,
  onCarrierRouteOpenChange,
}: ExpeditionPlannerProps) {
  const isPlanning = plan?.expedition?.status === 'planning'
  const isCompleted = plan?.expedition?.status === 'completed'
  const optimisticPlanRef = useRef<expedition.PlanView | null>(null)
  const isOptimisticPlan = plan !== null && plan === optimisticPlanRef.current
  const drawerState = useWaypointDetails(
    plan,
    Boolean(isPlanning),
    archiveRevision,
    isOptimisticPlan,
    closedWindowRevision,
  )

  const { copiedKey: copiedSystemRowId, copyWithFeedback } = useCopyFeedback<number>()

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

  const frontier = useMemo(
    () => computePlanFrontierIndices(plan, Boolean(isPlanning)),
    [isPlanning, plan],
  )

  const applyOptimisticPlan = useCallback(
    (nextPlan: expedition.PlanView) => {
      optimisticPlanRef.current = nextPlan
      onPlanOptimisticUpdate(nextPlan)
    },
    [onPlanOptimisticUpdate],
  )

  const carrierContextRef = useRef<CarrierMutationLockContext>({
    activeCarrierRoute: null,
    isManualCarrierRoute: false,
    routedCarrierStopIndex: -1,
    onCancelCarrierRoute: () => {},
  })

  const onBusyChangeRef = useRef(onBusyChange)
  onBusyChangeRef.current = onBusyChange
  const sharedBusyTracker = useMemo(
    () => createBusyTaskTracker((busyMessage) => onBusyChangeRef.current(busyMessage)),
    [],
  )

  const mutations = usePlanMutations({
    plan,
    isPlanning: Boolean(isPlanning),
    onBusyChange,
    busyTracker: sharedBusyTracker,
    applyOptimisticPlan,
    onRefreshPlan,
    carrierContextRef,
  })

  const carrierRoute = useCarrierRoute({
    plan,
    lastVisitedIndex: frontier.lastVisitedIndex,
    manualPlotterRequestCount,
    onBusyChange,
    busyTracker: sharedBusyTracker,
    onSetFcArrived: mutations.setFcArrived,
    onCarrierRouteOpenChange,
  })

  carrierContextRef.current = {
    activeCarrierRoute: carrierRoute.activeCarrierRoute,
    isManualCarrierRoute: carrierRoute.isManualCarrierRoute,
    routedCarrierStopIndex: carrierRoute.routedCarrierStopIndex,
    onCancelCarrierRoute: carrierRoute.handleCancelCarrierRoute,
  }

  const scrollContainerRef = useRef<HTMLDivElement>(null)
  useInitialExpeditionScroll(
    scrollContainerRef,
    Boolean(plan?.expedition),
    plan?.expedition?.id,
    plan?.expedition?.status,
    frontier.lastVisitedIndex,
  )

  const handleCopySystem = useCallback(
    (rowId: number, systemName: string) => {
      if (!systemName) return
      copyWithFeedback(rowId, systemName.trim())
    },
    [copyWithFeedback],
  )

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
    lastVisitedIndex: frontier.lastVisitedIndex,
    lockedSpanshTargetRowId,
    expandedRowIds: drawerState.expandedRowIds,
    scrollContainerRef,
    onMoveRow: mutations.handleMoveRow,
  })

  const tableColumns = useMemo(
    () => ({
      visibleColumnCount,
      tableMinWidthPixels,
      showNotes,
      showCarrierStop,
      onToggleShowNotes: toggleShowNotes,
      onToggleShowCarrierStop: toggleShowCarrierStop,
    }),
    [
      showCarrierStop,
      showNotes,
      tableMinWidthPixels,
      toggleShowCarrierStop,
      toggleShowNotes,
      visibleColumnCount,
    ],
  )

  const externalLinks = useMemo<ExternalLinkVisibilityConfig>(
    () => ({
      inara: externalLinkInara,
      edsm: externalLinkEdsm,
      gec: externalLinkGec,
    }),
    [externalLinkEdsm, externalLinkGec, externalLinkInara],
  )

  const dragState = useMemo(
    () => ({
      tableItems: rowDrag.tableItems,
      dropSlotGaps: rowDrag.dropSlotGaps,
      heldOrDraggedIndex: rowDrag.heldOrDraggedIndex,
      draggedIndex: rowDrag.draggedIndex,
      dragOverGapIndex: rowDrag.dragOverGapIndex,
      getRowDropProps: rowDrag.getRowDropProps,
      getSlotDropProps: rowDrag.getSlotDropProps,
      onClearDragOverGap: rowDrag.handleClearDragOverGap,
      onRowHoldStart: rowDrag.handleRowHoldStart,
      onRowHoldEnd: rowDrag.handleRowHoldEnd,
      onRowDragStart: rowDrag.handleRowDragStart,
      onRowDragEnd: rowDrag.handleRowDragEnd,
    }),
    [
      rowDrag.dragOverGapIndex,
      rowDrag.draggedIndex,
      rowDrag.dropSlotGaps,
      rowDrag.getRowDropProps,
      rowDrag.getSlotDropProps,
      rowDrag.handleClearDragOverGap,
      rowDrag.handleRowDragEnd,
      rowDrag.handleRowDragStart,
      rowDrag.handleRowHoldEnd,
      rowDrag.handleRowHoldStart,
      rowDrag.heldOrDraggedIndex,
      rowDrag.tableItems,
    ],
  )

  const carrierState = useMemo(
    () => ({
      activeCarrierRoute: carrierRoute.activeCarrierRoute,
      activeCarrierTargetRowId: carrierRoute.activeCarrierTargetRowId,
      isManualCarrierRoute: carrierRoute.isManualCarrierRoute,
      nextFcStopEntry: carrierRoute.nextFcStopEntry,
      routedCarrierStopIndex: carrierRoute.routedCarrierStopIndex,
      onOpenCarrierRoutePrompt: carrierRoute.openCarrierRoutePrompt,
    }),
    [
      carrierRoute.activeCarrierRoute,
      carrierRoute.activeCarrierTargetRowId,
      carrierRoute.isManualCarrierRoute,
      carrierRoute.nextFcStopEntry,
      carrierRoute.openCarrierRoutePrompt,
      carrierRoute.routedCarrierStopIndex,
    ],
  )

  const rowActions = useMemo(
    () => ({
      copiedSystemRowId,
      insertMenuAfterId: mutations.insertMenuAfterId,
      onSetInsertMenuAfterId: mutations.setInsertMenuAfterId,
      onChangeRegion: mutations.changeRegion,
      onInsert: mutations.handleInsert,
      onDeleteRow: mutations.handleRequestDeleteRow,
      onLegModeChange: mutations.handleLegModeChange,
      onOriginSystemFilledChange: setOriginHasDraftSystem,
      onFieldBlur: mutations.commitField,
      onCopySystem: handleCopySystem,
      onToggleFcStop: mutations.toggleFcStop,
      onOverrideFcArrived: mutations.handleRequestOverrideFcArrived,
    }),
    [
      copiedSystemRowId,
      handleCopySystem,
      mutations.changeRegion,
      mutations.commitField,
      mutations.handleInsert,
      mutations.handleLegModeChange,
      mutations.handleRequestDeleteRow,
      mutations.handleRequestOverrideFcArrived,
      mutations.insertMenuAfterId,
      mutations.setInsertMenuAfterId,
      mutations.toggleFcStop,
    ],
  )

  const handleAppendRow = useCallback(
    (rowType: 'waypoint' | 'region') => {
      void mutations.handleInsert(0, rowType)
    },
    [mutations.handleInsert],
  )

  if (!plan) {
    return (
      <div className="flex-1 flex items-center justify-center p-12 text-ed-text-muted font-mono-tabular uppercase tracking-widest text-xs">
        [ LOADING EXPEDITION... ]
      </div>
    )
  }

  const currentExpedition = plan.expedition
  const hasOriginWaypointSystem =
    originHasDraftSystem ?? Boolean((firstWaypoint?.row.systemName ?? '').trim())

  return (
    <div
      onDragOver={(event) => {
        if (rowDrag.draggedIndex !== null) {
          rowDrag.recordDragOverClientY(event.clientY)
        }
      }}
      className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-ed-canvas"
    >
      <PlannerHeaderBar
        plan={plan}
        hasOriginWaypointSystem={hasOriginWaypointSystem}
        carrierRouteOpen={Boolean(carrierRoute.activeCarrierRoute || carrierRoute.routePrompt)}
        exportEnabled={exportEnabled}
        onChangeActiveMode={mutations.handleActiveModeChange}
        onOpenStartModal={onOpenStartModal}
        onOpenExportModal={onOpenExportModal}
        onLifecycleAction={mutations.handleLifecycleAction}
      />

      <div
        ref={scrollContainerRef}
        {...rowDrag.scrollContainerDropProps}
        className="flex-1 overflow-y-auto overflow-x-auto bg-ed-canvas [overflow-anchor:none]"
      >
        <PlannerRouteTable
          plan={plan}
          isPlanning={Boolean(isPlanning)}
          isCompleted={Boolean(isCompleted)}
          clockSuspended={clockSuspended}
          frontier={frontier}
          columns={tableColumns}
          externalLinks={externalLinks}
          drawerState={drawerState}
          dragState={dragState}
          carrierState={carrierState}
          rowActions={rowActions}
        />

        <PlannerActiveLegFooter
          isPlanning={Boolean(isPlanning)}
          isCompleted={Boolean(isCompleted)}
          hasCollapsedRows={rowDrag.hasCollapsedRows}
          onAppendRow={handleAppendRow}
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
          onClose={carrierRoute.handleCloseRoutePrompt}
        />
      )}

      {mutations.confirmState && (
        <PlannerConfirmModal
          confirmState={mutations.confirmState}
          onConfirm={() => void mutations.handleConfirmModalAction()}
          onCancel={() => mutations.setConfirmState(null)}
        />
      )}
    </div>
  )
})

