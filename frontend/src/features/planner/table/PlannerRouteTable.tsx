import { memo, useCallback, useMemo, useRef, type CSSProperties, type DragEvent } from 'react'
import { expedition, modes } from '../../../api/models'
import type { NextCarrierStopEntry } from '../../../domain/carrierRouting'
import {
  computeRegionBorderColors,
  type ExternalLinkVisibilityConfig,
  type PlannerFrontierIndices,
  type PlannerTableItem,
} from '../../../domain/planTableItems'
import { PlannerCollapsedVisitedRow, PlannerDropSlotRow } from '../drag/PlannerDragPreviewRows'
import type { WaypointDetailsState } from '../drawer/useWaypointDetails'
import { WaypointDetailsDrawer } from '../drawer/WaypointDetailsDrawer'
import { PlannerRegionRow } from './PlannerRegionRow'
import { PlannerTableHeader } from './PlannerTableHeader'
import { WaypointRowItem, type WaypointRowHandlers } from './WaypointRowItem'

const rowStyle: CSSProperties = {
  contentVisibility: 'auto',
  containIntrinsicSize: '44px',
}

const EMPTY_MODES: modes.Mode[] = []

/**
 * PlannerColumnLayout defines table dimensions and optional column toggles.
 */
export interface PlannerColumnLayout {
  visibleColumnCount: number
  tableMinWidthPixels: number
  showNotes: boolean
  showCarrierStop: boolean
  onToggleShowNotes: () => void
  onToggleShowCarrierStop: () => void
}

/**
 * PlannerTableDragState bundles flattened table items, active drag/drop indices,
 * and drag-and-drop row/slot prop builders.
 */
export interface PlannerTableDragState {
  tableItems: PlannerTableItem[]
  dropSlotGaps: ReadonlySet<number>
  heldOrDraggedIndex: number | null
  draggedIndex: number | null
  dragOverGapIndex: number | null
  getRowDropProps: (index: number) => {
    onDragOver?: (event: DragEvent<HTMLTableRowElement>) => void
    onDrop?: (event: DragEvent<HTMLTableRowElement>) => void
  }
  getSlotDropProps: (
    gapIndex: number,
    targetIndex: number,
    preview: expedition.DropSlotPreview,
  ) => {
    onDragOver: (event: DragEvent<HTMLTableRowElement>) => void
    onDrop: (event: DragEvent<HTMLTableRowElement>) => void
  }
  onClearDragOverGap: () => void
  onRowHoldStart: (index: number) => void
  onRowHoldEnd: () => void
  onRowDragStart: (dragRowIndex: number, dragRowId: number, event: DragEvent) => void
  onRowDragEnd: () => void
}

/**
 * PlannerTableCarrierState bundles active Fleet Carrier routing state and the
 * Spansh prompt launch callback for planner rows.
 */
export interface PlannerTableCarrierState {
  activeCarrierRoute: expedition.CarrierRouteResult | null
  activeCarrierTargetRowId: number
  isManualCarrierRoute: boolean
  nextFcStopEntry: NextCarrierStopEntry | null
  routedCarrierStopIndex: number
  onOpenCarrierRoutePrompt: (rowIndex: number, targetRow: expedition.Row) => void
}

/**
 * PlannerTableRowActions bundles per-row UI transient flags and mutation handlers.
 */
export interface PlannerTableRowActions {
  copiedSystemRowId: number | null
  insertMenuAfterId: number | null
  onSetInsertMenuAfterId: (rowId: number | null) => void
  onChangeRegion: (rowId: number, regionId: number) => void
  onInsert: (afterRowId: number, rowType: 'waypoint' | 'region') => void
  onDeleteRow: (rowView: expedition.RowView) => void
  onLegModeChange: (rowId: number, modeId: number) => void
  onOriginSystemFilledChange?: (hasSystem: boolean) => void
  onFieldBlur: (
    row: expedition.Row,
    fieldName: 'systemName' | 'poiName' | 'notes',
    fieldValue: string,
  ) => void
  onCopySystem: (rowId: number, systemName: string) => void
  onToggleFcStop: (row: expedition.Row) => void
  onOverrideFcArrived?: (rowView: expedition.RowView) => void
}

/**
 * PlannerRouteTableProps defines grouped domain models rendered by the expedition
 * flight planner table.
 */
export interface PlannerRouteTableProps {
  plan: expedition.PlanView
  isPlanning: boolean
  isCompleted: boolean
  frontier: PlannerFrontierIndices
  columns: PlannerColumnLayout
  externalLinks: ExternalLinkVisibilityConfig
  drawerState: WaypointDetailsState
  dragState: PlannerTableDragState
  carrierState: PlannerTableCarrierState
  rowActions: PlannerTableRowActions
}

/**
 * PlannerRouteTable renders the expedition flight plan table header and rows,
 * dispatching region banners, waypoint rows, collapsed visited summaries,
 * dashed historical drop slots, and expanded leg telemetry drawers.
 */
export const PlannerRouteTable = memo(function PlannerRouteTable({
  plan,
  isPlanning,
  isCompleted,
  frontier,
  columns,
  externalLinks,
  drawerState,
  dragState,
  carrierState,
  rowActions,
}: PlannerRouteTableProps) {
  const { lastVisitedIndex, lastFcArrivedIndex } = frontier
  const {
    visibleColumnCount,
    tableMinWidthPixels,
    showNotes,
    showCarrierStop,
    onToggleShowNotes,
    onToggleShowCarrierStop,
  } = columns
  const { expandedRowIds, detailsById, loadingDetails, toggleExpand } = drawerState
  const {
    tableItems,
    dropSlotGaps,
    heldOrDraggedIndex,
    draggedIndex,
    dragOverGapIndex,
    getRowDropProps,
    getSlotDropProps,
    onClearDragOverGap,
    onRowHoldStart,
    onRowHoldEnd,
    onRowDragStart,
    onRowDragEnd,
  } = dragState
  const {
    activeCarrierRoute,
    activeCarrierTargetRowId,
    isManualCarrierRoute,
    nextFcStopEntry,
    routedCarrierStopIndex,
    onOpenCarrierRoutePrompt,
  } = carrierState
  const modesList = plan.modes ?? EMPTY_MODES

  const rowViewById = useMemo(() => {
    const rowMap = new Map<number, expedition.RowView>()
    for (const item of tableItems) {
      if (item.kind === 'region' || item.kind === 'waypoint') {
        rowMap.set(item.rowView.row.id, item.rowView)
      }
    }
    return rowMap
  }, [tableItems])

  const regionBorderColorByRowId = useMemo(
    () => computeRegionBorderColors(plan.rows, isPlanning),
    [isPlanning, plan.rows],
  )

  const latestCallbacksRef = useRef({
    rowViewById,
    insertMenuAfterId: rowActions.insertMenuAfterId,
    onSetInsertMenuAfterId: rowActions.onSetInsertMenuAfterId,
    onChangeRegion: rowActions.onChangeRegion,
    onLegModeChange: rowActions.onLegModeChange,
    onInsert: rowActions.onInsert,
    onDeleteRow: rowActions.onDeleteRow,
    onToggleExpand: toggleExpand,
    onOriginSystemFilledChange: rowActions.onOriginSystemFilledChange,
    onFieldBlur: rowActions.onFieldBlur,
    onCopySystem: rowActions.onCopySystem,
    onToggleFcStop: rowActions.onToggleFcStop,
    onOverrideFcArrived: rowActions.onOverrideFcArrived,
    onOpenCarrierRoutePrompt,
    onRowHoldStart,
    onRowHoldEnd,
    onRowDragStart,
    onRowDragEnd,
  })
  latestCallbacksRef.current = {
    rowViewById,
    insertMenuAfterId: rowActions.insertMenuAfterId,
    onSetInsertMenuAfterId: rowActions.onSetInsertMenuAfterId,
    onChangeRegion: rowActions.onChangeRegion,
    onLegModeChange: rowActions.onLegModeChange,
    onInsert: rowActions.onInsert,
    onDeleteRow: rowActions.onDeleteRow,
    onToggleExpand: toggleExpand,
    onOriginSystemFilledChange: rowActions.onOriginSystemFilledChange,
    onFieldBlur: rowActions.onFieldBlur,
    onCopySystem: rowActions.onCopySystem,
    onToggleFcStop: rowActions.onToggleFcStop,
    onOverrideFcArrived: rowActions.onOverrideFcArrived,
    onOpenCarrierRoutePrompt,
    onRowHoldStart,
    onRowHoldEnd,
    onRowDragStart,
    onRowDragEnd,
  }

  const hasOverrideFcArrived = Boolean(rowActions.onOverrideFcArrived)

  // Keep per-row callback identities constant so memoized rows skip unaffected re-renders.
  const stableRowHandlers = useMemo<WaypointRowHandlers>(
    () => ({
      onToggleExpand: (rowId: number) => latestCallbacksRef.current.onToggleExpand(rowId),
      onOriginSystemFilledChange: (hasSystem: boolean) =>
        latestCallbacksRef.current.onOriginSystemFilledChange?.(hasSystem),
      onFieldBlur: (
        row: expedition.Row,
        fieldName: 'systemName' | 'poiName' | 'notes',
        fieldValue: string,
      ) => latestCallbacksRef.current.onFieldBlur(row, fieldName, fieldValue),
      onCopySystem: (rowId: number, systemName: string) =>
        latestCallbacksRef.current.onCopySystem(rowId, systemName),
      onToggleFcStop: (row: expedition.Row) => latestCallbacksRef.current.onToggleFcStop(row),
      ...(hasOverrideFcArrived
        ? {
            onOverrideFcArrived: (rowView: expedition.RowView) =>
              latestCallbacksRef.current.onOverrideFcArrived?.(rowView),
          }
        : {}),
      onOpenCarrierRoutePrompt: (rowIndex: number, targetRow: expedition.Row) =>
        latestCallbacksRef.current.onOpenCarrierRoutePrompt(rowIndex, targetRow),
      onToggleInsertMenu: (rowId: number) => {
        const { insertMenuAfterId: openMenuRowId, onSetInsertMenuAfterId: setMenuAfterId } =
          latestCallbacksRef.current
        setMenuAfterId(openMenuRowId === rowId ? null : rowId)
      },
      onInsert: (afterRowId: number, rowType: 'waypoint' | 'region') =>
        latestCallbacksRef.current.onInsert(afterRowId, rowType),
      onDelete: (rowId: number) => {
        const targetRowView = latestCallbacksRef.current.rowViewById.get(rowId)
        if (targetRowView) latestCallbacksRef.current.onDeleteRow(targetRowView)
      },
      onHoldStart: (rowIndex: number) => latestCallbacksRef.current.onRowHoldStart(rowIndex),
      onHoldEnd: () => latestCallbacksRef.current.onRowHoldEnd(),
      onDragStart: (rowIndex: number, rowId: number, event: DragEvent) =>
        latestCallbacksRef.current.onRowDragStart(rowIndex, rowId, event),
      onDragEnd: () => latestCallbacksRef.current.onRowDragEnd(),
    }),
    [hasOverrideFcArrived],
  )

  const handleChangeRegion = useCallback(
    (rowId: number, regionId: number) =>
      latestCallbacksRef.current.onChangeRegion(rowId, regionId),
    [],
  )

  const handleChangeLegMode = useCallback(
    (rowId: number, nextModeId: number) =>
      latestCallbacksRef.current.onLegModeChange(rowId, nextModeId),
    [],
  )

  const currentExpedition = plan.expedition
  const totalRows = plan.rows?.length ?? 0
  const heldRowView = heldOrDraggedIndex !== null ? plan.rows?.[heldOrDraggedIndex] : null
  const grabbedSystemName = heldRowView?.row?.systemName ?? ''

  return (
    <table
      style={{ minWidth: `${tableMinWidthPixels}px` }}
      className="w-full table-fixed border-separate border-spacing-0 text-left"
    >
      <PlannerTableHeader
        isPlanning={isPlanning}
        showNotes={showNotes}
        showCarrierStop={showCarrierStop}
        onToggleShowNotes={onToggleShowNotes}
        onToggleShowCarrierStop={onToggleShowCarrierStop}
      />

      <tbody className="text-xs">
        {tableItems.map((item) => {
          if (item.kind === 'collapsed_visited') {
            return (
              <PlannerCollapsedVisitedRow
                key={item.key}
                visibleColumnCount={visibleColumnCount}
                waypointCount={item.waypointCount}
                regionCount={item.regionCount}
                firstWaypointNumber={item.firstWaypointNumber}
                firstSystemName={item.firstSystemName}
                lastWaypointNumber={item.lastWaypointNumber}
                lastSystemName={item.lastSystemName}
                onDragOver={onClearDragOverGap}
              />
            )
          }

          if (item.kind === 'drop_slot') {
            const slotDropProps = getSlotDropProps(item.gapIndex, item.targetIndex, item.preview)
            return (
              <PlannerDropSlotRow
                key={item.key}
                gapIndex={item.gapIndex}
                visibleColumnCount={visibleColumnCount}
                grabbedSystemName={grabbedSystemName}
                preview={item.preview}
                isDragTarget={dragOverGapIndex === item.gapIndex && draggedIndex !== null}
                onDragOver={slotDropProps.onDragOver}
                onDrop={slotDropProps.onDrop}
              />
            )
          }

          const rowView = item.rowView
          const row = rowView.row
          const index = item.index
          const isLocked = isCompleted
          const isBeingDragged = heldOrDraggedIndex === index
          const topGapHighlight =
            draggedIndex !== null &&
            dragOverGapIndex === index &&
            !dropSlotGaps.has(index)
          const bottomGapHighlight =
            draggedIndex !== null &&
            index === totalRows - 1 &&
            dragOverGapIndex === totalRows &&
            !dropSlotGaps.has(totalRows)

          if (item.kind === 'region') {
            return (
              <PlannerRegionRow
                key={item.key}
                expeditionId={currentExpedition.id}
                rowView={rowView}
                index={index}
                visibleColumnCount={visibleColumnCount}
                isPlanning={isPlanning}
                isLocked={isLocked}
                isBeingDragged={isBeingDragged}
                topGapHighlight={topGapHighlight}
                bottomGapHighlight={bottomGapHighlight}
                leftBorderColor={regionBorderColorByRowId.get(row.id)}
                lastVisitedIndex={lastVisitedIndex}
                insertMenuOpen={rowActions.insertMenuAfterId === row.id}
                rowStyle={rowStyle}
                dropProps={getRowDropProps(index)}
                onChangeRegion={handleChangeRegion}
                onToggleInsertMenu={stableRowHandlers.onToggleInsertMenu}
                onInsert={stableRowHandlers.onInsert}
                onDelete={stableRowHandlers.onDelete}
                onHoldStart={stableRowHandlers.onHoldStart!}
                onHoldEnd={stableRowHandlers.onHoldEnd!}
                onDragStart={stableRowHandlers.onDragStart}
                onDragEnd={stableRowHandlers.onDragEnd}
              />
            )
          }

          if (item.kind === 'drawer') {
            return (
              <tr key={item.key} style={rowStyle}>
                <td colSpan={visibleColumnCount} className="p-0">
                  <WaypointDetailsDrawer
                    details={detailsById[row.id] ?? null}
                    loading={Boolean(loadingDetails[row.id])}
                    rowView={rowView}
                    modeId={rowView.legModeId}
                    modeColor={rowView.legModeColor}
                    modesList={modesList}
                    onChangeLegMode={handleChangeLegMode}
                  />
                </td>
              </tr>
            )
          }

          const isExpanded = Boolean(expandedRowIds[row.id])
          const isNextFcStop = Boolean(
            row.fcStop && !row.fcArrived && nextFcStopEntry && row.id === nextFcStopEntry.row.id,
          )
          const isNextWaypoint = Boolean(
            rowView.isNextWaypoint || (plan.nextWaypointRowId && row.id === plan.nextWaypointRowId),
          )
          const hasActiveCarrierRoute = Boolean(
            activeCarrierRoute &&
              (isManualCarrierRoute ? isNextFcStop : activeCarrierTargetRowId === row.id),
          )
          // Manual logging leaves the target stop editable until arrival is logged;
          // an active Spansh route locks the target stop and all preceding carrier stops.
          const isSpanshRoutedFcStop = Boolean(
            activeCarrierRoute && !isManualCarrierRoute && activeCarrierTargetRowId === row.id,
          )
          const isFcStopLocked = Boolean(
            activeCarrierRoute &&
              routedCarrierStopIndex >= 0 &&
              (isManualCarrierRoute
                ? index < routedCarrierStopIndex
                : index <= routedCarrierStopIndex),
          )

          return (
            <WaypointRowItem
              key={item.key}
              rowView={rowView}
              index={index}
              isPlanning={isPlanning}
              isLocked={isLocked}
              isFcStopLocked={isFcStopLocked}
              isActivelyRoutedFcStop={isSpanshRoutedFcStop}
              lastFcArrivedIndex={lastFcArrivedIndex}
              isBeingDragged={isBeingDragged}
              topGapHighlight={topGapHighlight}
              bottomGapHighlight={bottomGapHighlight}
              isExpanded={isExpanded}
              isNextFcStop={isNextFcStop && !activeCarrierRoute}
              isNextWaypoint={isNextWaypoint}
              playerSystem={plan.playerSystem}
              hasActiveCarrierRoute={hasActiveCarrierRoute}
              isSystemCopied={rowActions.copiedSystemRowId === row.id}
              insertMenuOpen={rowActions.insertMenuAfterId === row.id}
              lastVisitedIndex={lastVisitedIndex}
              rowStyle={rowStyle}
              dropProps={getRowDropProps(index)}
              showNotes={showNotes}
              showCarrierStop={showCarrierStop}
              externalLinks={externalLinks}
              handlers={stableRowHandlers}
            />
          )
        })}
      </tbody>
    </table>
  )
})

